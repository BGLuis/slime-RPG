package tui

import (
	"cmp"
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/BGLuis/slime-rpg/pkg/detector"
	"github.com/BGLuis/slime-rpg/pkg/event"
	"github.com/BGLuis/slime-rpg/pkg/i18n"
	"github.com/BGLuis/slime-rpg/pkg/job"
	"github.com/BGLuis/slime-rpg/pkg/memory"
	"github.com/BGLuis/slime-rpg/pkg/ui/theme"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// ViewState representa a tela ativa do Stepper na TUI.
type ViewState int

const (
	ViewOpenGame ViewState = iota
	ViewConfigure
	ViewTranslating
	ViewReviewing
	ViewResult
)

// Model é o modelo de estado Bubble Tea unificado para a TUI.
type Model struct {
	view      ViewState
	width     int
	height    int
	job       *job.Job
	snapshot  job.Snapshot
	eventChan <-chan event.Envelope

	// Execução do job
	gamePath    string
	newProvider ProviderFactory
	openMemory  MemoryOpener
	mem         *memory.TranslationMemory
	cancel      context.CancelFunc
	runErr      error
	finished    bool
	paused      bool
	phaseMsg    string
	throttleMsg string

	// Estado da Etapa 1 (Abrir Jogo)
	detectedGame        *detector.DetectionResult
	selectedFolderIndex int
	folderOptions       []string
	pathInput           bool
	pathBuf             string

	// Estado da Etapa 2 (Configurar)
	selectedProvider int
	providers        []string
	sourceLang       string
	targetLang       string
	incremental      bool
	enableProxies    bool
	fieldValues      map[string]string // opções do extrator e chave do DeepL, por ConfigSchemaField.Key
	focus            int               // índice em configRows()
	estimate         *event.EstimateEvent
	estimating       bool
	estimateErr      string
	estimateSeq      int
	estimateKey      string

	// Estado da Etapa 3.5 (Revisar)
	selectedFilter     int
	filterLabels       []string
	reviewItems        []job.ReviewItem
	selectedSegmentIdx int
	editing            bool
	editBuf            string

	// Mensagens de status / rodapé
	statusMsg string
	quitting  bool
}

// NewModel inicializa a TUI com detecção automática na pasta atual.
func NewModel(initialDir string, opts ...Option) *Model {
	if initialDir == "" {
		initialDir = "."
	}
	m := &Model{
		newProvider:  defaultProvider,
		openMemory:   defaultMemory,
		view:         ViewOpenGame,
		width:        100,
		height:       32,
		providers:    []string{"Google Translate", "DeepL", "Ollama", i18n.T("tui_provider_memory")},
		targetLang:   "pt",
		incremental:  true,
		filterLabels: []string{i18n.T("tui_filter_all"), i18n.T("tui_filter_partial")},
	}
	det, _ := detector.Detect(initialDir)
	m.setGame(initialDir, det)
	for _, opt := range opts {
		opt(m)
	}
	return m
}

// hasGame diz se há um jogo reconhecido na pasta escolhida.
func (m *Model) hasGame() bool {
	return m.detectedGame != nil && m.detectedGame.Engine != detector.EngineUnknown
}

// setGame troca o jogo escolhido e zera o que dependia do anterior.
func (m *Model) setGame(path string, det *detector.DetectionResult) {
	m.gamePath, m.detectedGame = path, det
	m.fieldValues = map[string]string{}
	m.focus = 0
	m.sourceLang = "ja"
	if det != nil && det.ProbableLang != "" {
		m.sourceLang = det.ProbableLang
	}
	m.folderOptions = []string{i18n.T("tui_open_type_path")}
	if m.hasGame() {
		m.folderOptions = append([]string{i18n.T("tui_open_detected", det.DataDir, det.Engine)}, m.folderOptions...)
	}
	m.selectedFolderIndex = 0
}

// Init inicializa os comandos iniciais do Bubble Tea.
func (m *Model) Init() tea.Cmd {
	return nil
}

// Update processa teclas e mensagens de eventos.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case eventMsg:
		return m, m.handleEvent(msg)

	case estimateMsg:
		m.handleEstimate(msg)
		return m, nil

	case runDoneMsg:
		m.handleRunDone(msg)
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			m.stopJob()
			m.quitting = true
			return m, tea.Quit
		case "q":
			if (m.view == ViewOpenGame && !m.pathInput) || m.view == ViewResult {
				m.stopJob()
				m.quitting = true
				return m, tea.Quit
			}
		case "esc":
			switch {
			case m.editing:
				m.editing = false
				return m, nil
			case m.pathInput:
				m.pathInput = false
				return m, nil
			case m.view == ViewReviewing:
				m.view = ViewResult
				return m, nil
			case m.view == ViewConfigure:
				m.view = ViewOpenGame
				return m, nil
			}
		}

		// Roteia teclas por tela
		switch m.view {
		case ViewOpenGame:
			return m.updateOpenGame(msg)
		case ViewConfigure:
			return m.updateConfigure(msg)
		case ViewTranslating:
			return m.updateTranslating(msg)
		case ViewReviewing:
			return m.updateReviewing(msg)
		case ViewResult:
			return m.updateResult(msg)
		}
	}

	return m, nil
}

func (m *Model) updateOpenGame(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.pathInput {
		switch msg.String() {
		case "enter":
			return m, m.openTypedPath()
		case "backspace":
			if r := []rune(m.pathBuf); len(r) > 0 {
				m.pathBuf = string(r[:len(r)-1])
			}
		default:
			if msg.Type == tea.KeyRunes || msg.Type == tea.KeySpace {
				m.pathBuf += string(msg.Runes)
			}
		}
		return m, nil
	}
	switch msg.String() {
	case "up", "k":
		if m.selectedFolderIndex > 0 {
			m.selectedFolderIndex--
		}
	case "down", "j":
		if m.selectedFolderIndex < len(m.folderOptions)-1 {
			m.selectedFolderIndex++
		}
	case "enter":
		if m.hasGame() && m.selectedFolderIndex == 0 {
			m.statusMsg = ""
			return m, m.enterConfigure()
		}
		m.pathInput, m.pathBuf, m.statusMsg = true, "", ""
	}
	return m, nil
}

// openTypedPath detecta o jogo no caminho digitado e, se houver, segue para Configurar.
func (m *Model) openTypedPath() tea.Cmd {
	path := strings.TrimSpace(m.pathBuf)
	if path == "" {
		return nil
	}
	det, err := detector.Detect(path)
	if err != nil || det == nil || det.Engine == detector.EngineUnknown {
		m.statusMsg = i18n.T("tui_no_game_found", path)
		return nil
	}
	m.setGame(path, det)
	m.pathInput, m.statusMsg = false, ""
	return m.enterConfigure()
}

// configFields reúne os campos extras da tela Configurar: opções do extrator
// detectado e, no DeepL sem DEEPL_API_KEY no ambiente, a chave da API.
func (m *Model) configFields() []job.ConfigSchemaField {
	var fields []job.ConfigSchemaField
	if m.detectedGame != nil {
		fields = append(fields, job.ExtractorSchema(string(m.detectedGame.Engine)).Fields...)
	}
	if m.providers[m.selectedProvider] == "DeepL" && os.Getenv("DEEPL_API_KEY") == "" {
		fields = append(fields, job.ProviderSchema("deepl").Fields...)
	}
	if providerIDs[m.selectedProvider] == "ollama" {
		fields = append(fields, ollamaFields()...)
	}
	return fields
}

// rowKind identifica cada linha navegável da tela Configurar.
type rowKind int

const (
	rowProvider rowKind = iota
	rowSource
	rowTarget
	rowIncremental
	rowProxies
	rowField
)

type configRow struct {
	kind  rowKind
	field job.ConfigSchemaField
}

// configRows lista as linhas navegáveis com ↑↓; proxies só existem para o Google.
func (m *Model) configRows() []configRow {
	rows := []configRow{{kind: rowProvider}, {kind: rowSource}, {kind: rowTarget}, {kind: rowIncremental}}
	if providerIDs[m.selectedProvider] == "google" {
		rows = append(rows, configRow{kind: rowProxies})
	}
	for _, f := range m.configFields() {
		rows = append(rows, configRow{kind: rowField, field: f})
	}
	return rows
}

func (m *Model) focusedRow() configRow {
	rows := m.configRows()
	return rows[min(m.focus, len(rows)-1)]
}

// editingText diz se o foco está num campo de texto, onde as teclas viram digitação.
func (m *Model) editingText() bool {
	r := m.focusedRow()
	return r.kind == rowField && r.field.Type != "bool"
}

func (m *Model) fieldBool(f job.ConfigSchemaField) bool {
	return cmp.Or(m.fieldValues[f.Key], f.Default) == "true"
}

// editField trata a digitação no campo em foco; devolve false se a tecla não é de edição.
func (m *Model) editField(msg tea.KeyMsg, key string) bool {
	switch msg.Type {
	case tea.KeyBackspace:
		if r := []rune(m.fieldValues[key]); len(r) > 0 {
			m.fieldValues[key] = string(r[:len(r)-1])
		}
	case tea.KeyRunes, tea.KeySpace:
		m.fieldValues[key] += string(msg.Runes)
	default:
		return false
	}
	return true
}

// changeRow altera o valor da linha: delta escolhe o vizinho nas listas e alterna as caixas.
func (m *Model) changeRow(r configRow, delta int) {
	switch r.kind {
	case rowProvider:
		n := len(m.providers)
		m.selectedProvider = (m.selectedProvider + delta + n) % n
	case rowSource:
		m.sourceLang = stepLang(m.sourceLang, delta)
	case rowTarget:
		m.targetLang = stepLang(m.targetLang, delta)
	case rowIncremental:
		m.incremental = !m.incremental
	case rowProxies:
		m.enableProxies = !m.enableProxies
	case rowField:
		if r.field.Type == "bool" {
			m.fieldValues[r.field.Key] = strconv.FormatBool(!m.fieldBool(r.field))
		}
	}
}

func (m *Model) updateConfigure(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	row := m.focusedRow()
	// Num campo de texto as letras são digitadas; os atalhos de uma letra abaixo só valem fora dele.
	if m.editingText() && m.editField(msg, row.field.Key) {
		return m, nil
	}
	n := len(m.configRows())
	switch msg.String() {
	case "up", "k", "shift+tab":
		m.focus = (m.focus - 1 + n) % n
	case "down", "j", "tab":
		m.focus = (m.focus + 1) % n
	case "left", "h":
		m.changeRow(row, -1)
	case "right", "l":
		m.changeRow(row, 1)
	case " ":
		if row.kind == rowProxies || row.kind == rowField {
			m.changeRow(row, 1)
		} else {
			m.incremental = !m.incremental
		}
	case "s":
		m.sourceLang = stepLang(m.sourceLang, 1)
	case "d":
		m.targetLang = stepLang(m.targetLang, 1)
	case "x":
		m.enableProxies = !m.enableProxies
	case "ctrl+enter", "enter":
		cmd, err := m.startJob()
		if err != nil {
			m.statusMsg = err.Error()
			return m, nil
		}
		m.statusMsg = ""
		m.view = ViewTranslating
		return m, cmd
	}
	m.focus = min(m.focus, len(m.configRows())-1)
	return m, m.reestimate()
}

func (m *Model) updateTranslating(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "p":
		m.togglePause()
	case "c":
		if m.cancel != nil {
			m.cancel()
		}
		if m.job != nil {
			m.job.Cancel()
		}
	}
	return m, nil
}

var reviewFilters = []string{"all", "partial"}

func (m *Model) loadReview() {
	if m.job == nil {
		return
	}
	m.reviewItems = m.job.SegmentsForReviewFiltered(reviewFilters[m.selectedFilter])
	m.selectedSegmentIdx = min(m.selectedSegmentIdx, max(len(m.reviewItems)-1, 0))
}

func (m *Model) selectedItem() *job.ReviewItem {
	if m.selectedSegmentIdx >= len(m.reviewItems) {
		return nil
	}
	return &m.reviewItems[m.selectedSegmentIdx]
}

func (m *Model) applyCorrection(text string, locked bool) {
	item := m.selectedItem()
	if item == nil {
		return
	}
	if err := m.job.ApplyCorrection(item.ID, text, locked); err != nil {
		m.statusMsg = err.Error()
		return
	}
	m.statusMsg = i18n.T("tui_review_saved")
	m.loadReview()
}

func (m *Model) updateReviewing(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.editing {
		switch msg.String() {
		case "enter":
			m.editing = false
			m.applyCorrection(m.editBuf, false)
		case "backspace":
			if r := []rune(m.editBuf); len(r) > 0 {
				m.editBuf = string(r[:len(r)-1])
			}
		default:
			if msg.Type == tea.KeyRunes || msg.Type == tea.KeySpace {
				m.editBuf += string(msg.Runes)
			}
		}
		return m, nil
	}
	switch msg.String() {
	case "tab":
		m.selectedFilter = (m.selectedFilter + 1) % len(m.filterLabels)
		m.selectedSegmentIdx = 0
		m.loadReview()
	case "up", "k":
		if m.selectedSegmentIdx > 0 {
			m.selectedSegmentIdx--
		}
	case "down", "j":
		if m.selectedSegmentIdx < len(m.reviewItems)-1 {
			m.selectedSegmentIdx++
		}
	case "e":
		if item := m.selectedItem(); item != nil {
			m.editing, m.editBuf = true, item.Target
		}
	case "b":
		if item := m.selectedItem(); item != nil {
			m.applyCorrection(item.Target, true)
		}
	case "ctrl+s", "enter":
		m.view = ViewResult
	}
	return m, nil
}

func (m *Model) updateResult(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "n":
		m.stopJob()
		m.job, m.finished, m.runErr, m.statusMsg = nil, false, nil, ""
		m.view = ViewOpenGame
	case "r":
		if m.job != nil && m.finished {
			m.selectedSegmentIdx = 0
			m.loadReview()
			m.view = ViewReviewing
		}
	}
	return m, nil
}

// View renderiza a tela atual ajustada a m.width x m.height.
func (m *Model) View() string {
	if m.quitting {
		return i18n.T("tui_quit")
	}
	w := m.contentWidth()
	rule := strings.Repeat(theme.SymBarThin, w)

	head := []string{m.renderStepperHeader(w), rule, ""}
	var body string
	switch m.view {
	case ViewOpenGame:
		body = m.renderOpenGame()
	case ViewConfigure:
		body = m.renderConfigure()
	case ViewTranslating:
		body = m.renderTranslating()
	case ViewReviewing:
		body = m.renderReviewing()
	case ViewResult:
		body = m.renderResult()
	}
	foot := []string{"", rule}
	if m.statusMsg != "" {
		foot = append(foot, "  "+theme.StyleTagWarning.Render(theme.SymWarning+" "+truncate(m.statusMsg, w-6)))
	}
	for _, l := range wrapJoin(strings.Split(strings.TrimSpace(m.renderFooter()), " · "), " · ", w-2) {
		foot = append(foot, "  "+l)
	}

	// Sem o corte, o terminal descarta as primeiras linhas e o cabeçalho some.
	lines := strings.Split(strings.TrimRight(body, "\n"), "\n")
	if room := m.height - len(head) - len(foot); room > 0 && len(lines) > room {
		lines = lines[:room]
	}
	all := append(append(head, lines...), foot...)
	for i, l := range all {
		all[i] = truncate(l, m.width)
	}
	return theme.StyleApp.Render(strings.Join(all, "\n"))
}

func (m *Model) renderStepperHeader(width int) string {
	names := []string{i18n.T("tui_step_game"), i18n.T("tui_step_config"), i18n.T("tui_step_translate"), i18n.T("tui_step_result")}
	current := map[ViewState]int{ViewOpenGame: 0, ViewConfigure: 1, ViewTranslating: 2, ViewReviewing: 2, ViewResult: 3}[m.view]

	steps := make([]string, len(names))
	for i, name := range names {
		switch {
		case i < current:
			steps[i] = theme.StyleMuted.Render(theme.SymCheck + " " + name)
		case i == current:
			steps[i] = theme.StyleAccent.Render(theme.SymBullet + " " + name)
		default:
			steps[i] = fmt.Sprintf("%d %s", i+1, name)
		}
	}
	full := " Slime    " + strings.Join(steps, "  "+theme.SymArrow+"  ")
	if ansi.StringWidth(full) <= width {
		return full
	}
	return fmt.Sprintf(" Slime  %s  %s", steps[current], theme.StyleMuted.Render(fmt.Sprintf("%d/%d", current+1, len(names))))
}

func (m *Model) renderOpenGame() string {
	w := m.contentWidth() - len(indent) - 1
	lines := []string{
		theme.StyleHeader.Render(i18n.T("tui_open_title")),
		theme.StyleMuted.Render(i18n.T("tui_open_sub")),
		"",
	}
	opts := make([]string, len(m.folderOptions))
	for i, opt := range m.folderOptions {
		opts[i] = "  " + opt
		if i == m.selectedFolderIndex {
			opts[i] = theme.StyleAccent.Render(theme.SymArrow+" ") + opt
		}
	}
	lines = append(lines, box(i18n.T("tui_open_box"), opts, w)...)
	lines = append(lines, "")
	if m.pathInput {
		prompt := i18n.T("tui_open_path_prompt")
		lines = append(lines, prompt+tail(m.pathBuf, w-ansi.StringWidth(prompt)-1)+"▌", "")
	}
	lines = append(lines, theme.StyleMuted.Render(truncate(i18n.T("tui_open_tip"), w)))
	return indentLines(lines)
}

// Abaixo desta largura a estimativa vai para baixo do formulário em vez de ficar ao lado.
const sideBySideWidth = 92

const estimateWidth = 36

func (m *Model) renderConfigure() string {
	w := m.contentWidth() - len(indent) - 1
	sideBySide := w+len(indent)+1 >= sideBySideWidth
	formW := w
	if sideBySide {
		formW = w - estimateWidth - 2
	}

	gameTitle, info := i18n.T("tui_cfg_none"), ""
	if m.detectedGame != nil {
		gameTitle = m.detectedGame.GameName
		info = i18n.T("tui_cfg_info", m.detectedGame.Engine, m.detectedGame.DataDir, m.detectedGame.FileCount)
	}
	form := append([]string{theme.StyleMuted.Render(info), ""}, m.renderConfigRows(formW-4)...)
	form = append(form, "", theme.StyleMuted.Render(i18n.T("tui_cfg_backup")))
	estW := formW
	if sideBySide {
		estW = estimateWidth
	}
	est := m.renderEstimate(estW - 4)

	title := i18n.T("tui_cfg_title") + " · " + gameTitle
	if !sideBySide {
		return indentLines(append(append(box(title, form, formW), ""), box(i18n.T("tui_est_title"), est, estW)...))
	}
	for len(est) < len(form) {
		est = append(est, "")
	}
	for len(form) < len(est) {
		form = append(form, "")
	}
	left, right := box(title, form, formW), box(i18n.T("tui_est_title"), est, estimateWidth)
	rows := make([]string, len(left))
	for i := range left {
		rows[i] = left[i] + "  " + right[i]
	}
	return indentLines(rows)
}

// renderConfigRows desenha uma linha por item navegável: rótulo à esquerda, valor à direita.
func (m *Model) renderConfigRows(inner int) []string {
	rows := m.configRows()
	labelOf := func(r configRow) string {
		switch r.kind {
		case rowProvider:
			return i18n.T("tui_cfg_service")
		case rowSource:
			return i18n.T("tui_cfg_source")
		case rowTarget:
			return i18n.T("tui_cfg_target")
		case rowIncremental:
			return i18n.T("tui_cfg_incremental")
		case rowProxies:
			return i18n.T("tui_cfg_proxies")
		}
		return r.field.Label
	}
	labelW := 0
	for _, r := range rows {
		if r.kind != rowIncremental && r.kind != rowProxies {
			labelW = max(labelW, ansi.StringWidth(labelOf(r)))
		}
	}
	labelW = min(labelW, inner/2)
	valueW := inner - 2 - labelW - 1
	under := strings.Repeat(" ", 2+labelW+1)

	var out []string
	for i, r := range rows {
		focused := i == min(m.focus, len(rows)-1)
		marker, label := "  ", labelOf(r)
		if focused {
			marker = theme.StyleAccent.Render(theme.SymArrow + " ")
		}
		var value, note string
		switch r.kind {
		case rowProvider:
			value = choice(m.providers[m.selectedProvider], focused)
			note = i18n.T(providerHints[m.selectedProvider])
		case rowSource:
			value = choice(langLabel(m.sourceLang), focused)
		case rowTarget:
			value = choice(langLabel(m.targetLang), focused)
		case rowIncremental:
			label = checkbox(m.incremental) + " " + label
		case rowProxies:
			label = checkbox(m.enableProxies) + " " + label
		case rowField:
			switch v := m.fieldValues[r.field.Key]; {
			case r.field.Type == "bool":
				value = checkbox(m.fieldBool(r.field))
			case r.field.Type == "secret":
				value = strings.Repeat("•", len([]rune(v)))
			default:
				value = v
			}
			if focused && r.field.Type != "bool" {
				value = tail(value, valueW-1) + "▌"
			} else if value == "" {
				value = theme.StyleMuted.Render("—")
			}
			if focused {
				note = r.field.Help
			}
		}
		if focused {
			label = theme.StyleAccent.Render(truncate(label, max(inner-2, 1)))
		}
		if value == "" {
			out = append(out, marker+label)
		} else {
			out = append(out, marker+fit(label, labelW)+" "+truncate(value, valueW))
		}
		if note != "" {
			for _, l := range wrapJoin(strings.Fields(note), " ", valueW) {
				out = append(out, under+theme.StyleMuted.Render(l))
			}
		}
	}
	return out
}

// choice mostra as setas ‹ › só na linha em foco, onde ←→ trocam o valor.
func choice(v string, focused bool) string {
	if focused {
		return theme.StyleAccent.Render("‹ ") + v + theme.StyleAccent.Render(" ›")
	}
	return v
}

func (m *Model) renderFooter() string {
	switch m.view {
	case ViewOpenGame:
		if m.pathInput {
			return i18n.T("tui_foot_path")
		}
		return i18n.T("tui_foot_open")
	case ViewConfigure:
		return i18n.T("tui_foot_config")
	case ViewTranslating:
		return i18n.T("tui_foot_translating")
	case ViewReviewing:
		if m.editing {
			return i18n.T("tui_foot_edit")
		}
		return i18n.T("tui_foot_review")
	case ViewResult:
		return i18n.T("tui_foot_result")
	default:
		return i18n.T("tui_foot_default")
	}
}
