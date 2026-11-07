package tui

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/BGLuis/slime-rpg/pkg/detector"
	"github.com/BGLuis/slime-rpg/pkg/event"
	"github.com/BGLuis/slime-rpg/pkg/job"
	"github.com/BGLuis/slime-rpg/pkg/memory"
	"github.com/BGLuis/slime-rpg/pkg/ui/theme"
	tea "github.com/charmbracelet/bubbletea"
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
	throttleMsg string

	// Estado da Etapa 1 (Abrir Jogo)
	detectedGame        *detector.DetectionResult
	selectedFolderIndex int
	folderOptions       []string

	// Estado da Etapa 2 (Configurar)
	selectedProvider int
	providers        []string
	sourceLang       string
	targetLang       string
	incremental      bool
	enableProxies    bool
	fieldValues      map[string]string // opções do extrator e chave do DeepL, por ConfigSchemaField.Key
	fieldFocus       int               // índice em configFields(); -1 = nenhum campo em edição

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
	det, _ := detector.Detect(initialDir)
	sourceLang := "ja"
	if det != nil && det.ProbableLang != "" {
		sourceLang = det.ProbableLang
	}

	folderOpts := []string{"Digitar caminho…", "Navegar nas pastas…"}
	if det != nil && det.Engine != detector.EngineUnknown {
		folderOpts = append([]string{fmt.Sprintf("› Jogo detectado aqui   %s   %s", det.DataDir, det.Engine)}, folderOpts...)
	}

	m := &Model{
		gamePath:      initialDir,
		newProvider:   defaultProvider,
		openMemory:    defaultMemory,
		view:          ViewOpenGame,
		width:         100,
		height:        32,
		detectedGame:  det,
		folderOptions: folderOpts,
		providers:     []string{"Google Translate", "DeepL", "Ollama", "Só memória"},
		sourceLang:    sourceLang,
		targetLang:    "pt",
		incremental:   true,
		fieldValues:   map[string]string{},
		fieldFocus:    -1,
		filterLabels:  []string{"Todos", "Parciais"},
	}
	for _, opt := range opts {
		opt(m)
	}
	return m
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
			if m.view == ViewOpenGame || m.view == ViewResult {
				m.stopJob()
				m.quitting = true
				return m, tea.Quit
			}
		case "esc":
			switch {
			case m.editing:
				m.editing = false
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
		// Avança para o Passo 2: Configurar
		m.view = ViewConfigure
	}
	return m, nil
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
	return fields
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

func (m *Model) updateConfigure(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	fields := m.configFields()
	if msg.String() == "tab" && len(fields) > 0 {
		m.fieldFocus = (m.fieldFocus + 1) % (len(fields) + 1)
		if m.fieldFocus == len(fields) {
			m.fieldFocus = -1
		}
		return m, nil
	}
	if m.fieldFocus >= 0 && m.fieldFocus < len(fields) && m.editField(msg, fields[m.fieldFocus].Key) {
		return m, nil
	}
	switch msg.String() {
	case "left", "h":
		if m.selectedProvider > 0 {
			m.selectedProvider--
		}
	case "right", "l":
		if m.selectedProvider < len(m.providers)-1 {
			m.selectedProvider++
		}
	case " ":
		m.incremental = !m.incremental
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
	return m, nil
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
	m.statusMsg = "Correção salva na memória; traduza de novo para aplicá-la ao jogo."
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

// View renderiza a tela atual em formato de grade 100x32.
func (m *Model) View() string {
	if m.quitting {
		return "Slime RPG finalizado.\n"
	}

	var b strings.Builder

	// 1. Cabeçalho Superior com Stepper de 4 passos
	b.WriteString(m.renderStepperHeader())
	b.WriteString("\n" + strings.Repeat(theme.SymBarThin, 100) + "\n\n")

	// 2. Conteúdo da Tela
	switch m.view {
	case ViewOpenGame:
		b.WriteString(m.renderOpenGame())
	case ViewConfigure:
		b.WriteString(m.renderConfigure())
	case ViewTranslating:
		b.WriteString(m.renderTranslating())
	case ViewReviewing:
		b.WriteString(m.renderReviewing())
	case ViewResult:
		b.WriteString(m.renderResult())
	}

	// 3. Rodapé Fixo com Atalhos
	b.WriteString("\n" + strings.Repeat(theme.SymBarThin, 100) + "\n")
	if m.statusMsg != "" {
		b.WriteString("  " + theme.StyleTagWarning.Render(theme.SymWarning+" "+m.statusMsg) + "\n")
	}
	b.WriteString(m.renderFooter())

	return theme.StyleApp.Render(b.String())
}

func (m *Model) renderStepperHeader() string {
	step1 := "1 Jogo"
	step2 := "2 Configurar"
	step3 := "3 Tradução"
	step4 := "4 Resultado"

	switch m.view {
	case ViewOpenGame:
		step1 = theme.StyleAccent.Render(theme.SymBullet + " Jogo")
	case ViewConfigure:
		step1 = theme.StyleMuted.Render(theme.SymCheck + " Jogo")
		step2 = theme.StyleAccent.Render(theme.SymBullet + " Configurar")
	case ViewTranslating, ViewReviewing:
		step1 = theme.StyleMuted.Render(theme.SymCheck + " Jogo")
		step2 = theme.StyleMuted.Render(theme.SymCheck + " Configurar")
		step3 = theme.StyleAccent.Render(theme.SymBullet + " Tradução")
	case ViewResult:
		step1 = theme.StyleMuted.Render(theme.SymCheck + " Jogo")
		step2 = theme.StyleMuted.Render(theme.SymCheck + " Configurar")
		step3 = theme.StyleMuted.Render(theme.SymCheck + " Tradução")
		step4 = theme.StyleAccent.Render(theme.SymBullet + " Resultado")
	}

	return fmt.Sprintf(" Slime    %s  %s  %s  %s  %s  %s  %s",
		step1, theme.SymArrow, step2, theme.SymArrow, step3, theme.SymArrow, step4)
}

func (m *Model) renderOpenGame() string {
	var s strings.Builder
	s.WriteString("   " + theme.StyleTitle.Render("Qual jogo vamos traduzir?") + "\n")
	s.WriteString("   " + theme.StyleSubtitle.Render("O Slime procurou na pasta atual, no clipboard e nos jogos recentes.") + "\n\n")

	// Caixa de seleção de pasta
	s.WriteString("   ╭─ Escolha uma pasta ──────────────────────────────────────────────────────────────────────────╮\n")
	for i, opt := range m.folderOptions {
		prefix := "   "
		if i == m.selectedFolderIndex {
			prefix = " › "
		}
		s.WriteString(fmt.Sprintf("   │%s%-89s│\n", prefix, opt))
	}
	s.WriteString("   ╰──────────────────────────────────────────────────────────────────────────────────────────────╯\n\n")

	// Recentes
	s.WriteString("   " + theme.StyleHeader.Render("RECENTES") + "\n")
	s.WriteString("   ╭──────────────────────────────────────────────────────────────────────────────────────────────╮\n")
	s.WriteString("   │ Aventura do Slime      ~/Jogos/Aventura do Slime        RPG Maker MZ   ja→pt · há 2 dias     │\n")
	s.WriteString("   │ Torre de Cristal       ~/Jogos/torre-cristal            WOLF RPG       ja→pt · 3 parciais    │\n")
	s.WriteString("   │ Planilha de diálogos   ~/Projetos/mod/dialogos.csv      CSV            en→pt · nunca         │\n")
	s.WriteString("   ╰──────────────────────────────────────────────────────────────────────────────────────────────╯\n\n")
	s.WriteString("   " + theme.StyleMuted.Render("Dica: slime translate -i <pasta> faz o mesmo sem perguntas."))
	return s.String()
}

func (m *Model) renderConfigure() string {
	var s strings.Builder
	gameTitle := "Aventura do Slime"
	if m.detectedGame != nil {
		gameTitle = m.detectedGame.GameName
	}

	left := []string{
		fmt.Sprintf("╭─ Configurar · %s ───────────────────────╮", gameTitle),
		"│ RPG Maker MZ · www/data · 142 arquivos                 │",
		"│                                                        │",
		"│ " + theme.SymBarV + " Serviço                                              │",
		"│ " + theme.SymBarV + " " + m.providerLine() + " │",
		"│ " + theme.SymBarV + " Grátis e sem chave; pode limitar requisições.        │",
		"│                                                        │",
		"│   Idioma de origem                                     │",
		"│   Japonês (detectado em 96% das falas)                 │",
		"│                                                        │",
		"│   Idioma de destino                                    │",
		"│   Português (Brasil)                                   │",
		"│                                                        │",
		fmt.Sprintf("│   %-52s │", checkbox(m.incremental)+" Só o que mudou (espaço)"),
		fmt.Sprintf("│   %-52s │", checkbox(m.enableProxies)+" Usar proxies se o Google bloquear o IP (x)"),
		"│                                                        │",
		"│   ▸ Avançado   backup ligado · saída na própria pasta  │",
		"╰────────────────────────────────────────────────────────╯",
	}

	right := []string{
		"╭─ Estimativa ─────────────────────╮",
		"│ 230 textos para traduzir         │",
		"│                                  │",
		"│ " + theme.RenderProgressBar(0.80, 32) + " │",
		"│ 950 de 1.180 já na memória (80%) │",
		"│                                  │",
		"│ Arquivos  12 de 142              │",
		"│ Tempo     menos de 1 min         │",
		"│ Custo     grátis                 │",
		"│ Backup    …/" + gameTitle + "-ja │",
		"│                                  │",
		"│ O tempo é uma faixa: o Google    │",
		"│ pode limitar no meio do caminho. │",
		"│                                  │",
		"│  " + theme.StyleBtnPrimary.Render("▶ Traduzir 230 textos") + "           │",
		"│    ctrl+enter                    │",
		"╰──────────────────────────────────╯",
	}

	left = append(left[:len(left)-1], m.renderFieldRows()...)
	left = append(left, "╰────────────────────────────────────────────────────────╯")

	for i := 0; i < len(left); i++ {
		rLine := ""
		if i < len(right) {
			rLine = right[i]
		}
		s.WriteString(fmt.Sprintf("   %-57s  %s\n", left[i], rLine))
	}

	return s.String()
}

func (m *Model) renderFieldRows() []string {
	var rows []string
	for i, f := range m.configFields() {
		value := m.fieldValues[f.Key]
		if f.Type == "secret" {
			value = strings.Repeat("•", len([]rune(value)))
		}
		cursor := ""
		if i == m.fieldFocus {
			cursor = "▌"
		}
		rows = append(rows,
			"│                                                        │",
			fmt.Sprintf("│   %-52s │", f.Label),
			fmt.Sprintf("│   %-52s │", "› "+value+cursor),
			fmt.Sprintf("│   %-52s │", truncate(f.Help, 52)),
		)
	}
	return rows
}

func truncate(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}

func (m *Model) renderFooter() string {
	switch m.view {
	case ViewOpenGame:
		return "  ↑↓ mover · enter escolher · tab recentes · ctrl+v colar · ctrl+p comandos · q sair"
	case ViewConfigure:
		return "  tab próximo campo · ←→ serviço · espaço só o que mudou · x proxies · enter traduzir · esc voltar"
	case ViewTranslating:
		return "  p pausar/retomar · c cancelar · ctrl+c sair"
	case ViewReviewing:
		if m.editing {
			return "  digite a tradução · enter salvar · esc cancelar"
		}
		return "  ↑↓ segmento · tab filtro · e editar · b bloquear · esc/enter voltar ao resultado"
	case ViewResult:
		return "  r revisar · n outro jogo · q sair"
	default:
		return "  q sair"
	}
}
