package tui

import (
	"fmt"
	"os"
	"strings"

	"github.com/BGLuis/slime-rpg/pkg/detector"
	"github.com/BGLuis/slime-rpg/pkg/event"
	"github.com/BGLuis/slime-rpg/pkg/job"
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
	selectedSegmentIdx int
	searchQuery        string

	// Mensagens de status / rodapé
	statusMsg string
	quitting  bool
}

// NewModel inicializa a TUI com detecção automática na pasta atual.
func NewModel(initialDir string) *Model {
	det, _ := detector.Detect(initialDir)

	folderOpts := []string{"Digitar caminho…", "Navegar nas pastas…"}
	if det != nil && det.Engine != detector.EngineUnknown {
		folderOpts = append([]string{fmt.Sprintf("› Jogo detectado aqui   %s   %s", det.DataDir, det.Engine)}, folderOpts...)
	}

	return &Model{
		view:          ViewOpenGame,
		width:         100,
		height:        32,
		detectedGame:  det,
		folderOptions: folderOpts,
		providers:     []string{"Google Translate", "DeepL", "Ollama"},
		sourceLang:    "Japonês (detectado)",
		targetLang:    "Português (Brasil)",
		incremental:   true,
		fieldValues:   map[string]string{},
		fieldFocus:    -1,
		filterLabels:  []string{"Todos 1.180", "Com problema 3", "Parciais 2", "Editados 1", "Bloqueados 4", "Da memória 950"},
	}
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

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		case "q":
			if m.view == ViewOpenGame || m.view == ViewResult {
				m.quitting = true
				return m, tea.Quit
			}
		case "esc":
			if m.view > ViewOpenGame && m.view != ViewTranslating {
				m.view--
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
	case "ctrl+enter", "enter":
		// Inicia tradução (Passo 3)
		m.view = ViewTranslating
	}
	return m, nil
}

func (m *Model) updateTranslating(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "p":
		if m.job != nil {
			m.job.Pause()
		}
	case "r":
		m.view = ViewReviewing
	case "enter":
		m.view = ViewResult
	}
	return m, nil
}

func (m *Model) updateReviewing(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "tab":
		m.selectedFilter = (m.selectedFilter + 1) % len(m.filterLabels)
	case "up", "k":
		if m.selectedSegmentIdx > 0 {
			m.selectedSegmentIdx--
		}
	case "down", "j":
		m.selectedSegmentIdx++
	case "ctrl+s", "enter":
		m.view = ViewResult
	}
	return m, nil
}

func (m *Model) updateResult(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "n":
		m.view = ViewOpenGame
	case "r":
		m.view = ViewReviewing
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
		"│ " + theme.SymBarV + " › Google Translate   DeepL   Ollama                  │",
		"│ " + theme.SymBarV + " Grátis e sem chave; pode limitar requisições.        │",
		"│                                                        │",
		"│   Idioma de origem                                     │",
		"│   Japonês (detectado em 96% das falas)                 │",
		"│                                                        │",
		"│   Idioma de destino                                    │",
		"│   Português (Brasil)                                   │",
		"│                                                        │",
		"│   [x] Só o que mudou   12 de 142 arquivos              │",
		"│   [ ] Usar proxies se o Google bloquear o IP           │",
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

func (m *Model) renderTranslating() string {
	var s strings.Builder
	s.WriteString("   " + theme.StyleHeader.Render("Traduzindo Aventura do Slime") + "   Google · ja → pt · só o que mudou\n")
	s.WriteString("     " + theme.SymCheck + " Varredura     " + theme.SymBullet + " Tradução     3 Salvar no jogo\n\n")

	// Barra de progresso
	s.WriteString("   " + theme.RenderProgressBar(0.86, 70) + "  86%\n")
	s.WriteString("   1.020 de 1.180 segmentos · 950 da memória · 70 traduzidos agora · faltam ~40 s\n\n")

	// Alerta de rate limit
	s.WriteString("    " + theme.StyleTagWarning.Render(theme.SymWarning+" O Google limitou as requisições (HTTP 429). Nova tentativa em 30 s.") + "     p ativar proxies\n\n")

	// Linha de contadores
	s.WriteString("   " + theme.SymBullet + " 5 concluídos   " + theme.SymBullet + " 3 cache   " + theme.SymBullet + " 2 processando   " + theme.SymBullet + " 1 na fila   " + theme.SymBullet + " 1 parcial   " + theme.SymBullet + " 0 erros\n\n")

	// Tabela de arquivos
	s.WriteString("   ╭─ Arquivos · 12 ──────────────────────────────────────────────────────────────────────────────╮\n")
	s.WriteString("   │ ARQUIVO                     STATUS         MENSAGEM                     PROGRESSO            │\n")
	s.WriteString("   │ www/data/Map014.json        ● processando  esperando liberar (429)      ━━━━━━━─── 31/48     │\n")
	s.WriteString("   │ www/data/CommonEvents.json  ● processando  212 de 340 segmentos         ━━━━━━──── 212/340   │\n")
	s.WriteString("   │ www/data/Map020.json        ● na fila      na fila                      ────────── 0/22      │\n")
	s.WriteString("   │ www/data/Map003.json        ● parcial      2 falas com problema         ━━━━━━━━━━ 55/57     │\n")
	s.WriteString("   │ www/data/Items.json         ● concluído    380 vieram da memória        ━━━━━━━━━━ 410/410   │\n")
	s.WriteString("   │ + 7 concluídos ou resolvidos pela memória                                                    │\n")
	s.WriteString("   ╰──────────────────────────────────────────────────────────────────────────────────────────────╯\n")

	return s.String()
}

func (m *Model) renderReviewing() string {
	var s strings.Builder
	s.WriteString("   " + theme.StyleHeader.Render("Revisar traduções") + "   opcional · nada vai para o jogo até você concluir\n")

	// Chips de filtros
	filtersLine := ""
	for i, f := range m.filterLabels {
		if i == m.selectedFilter {
			filtersLine += fmt.Sprintf("[%s]  ", f)
		} else {
			filtersLine += fmt.Sprintf("%s  ", f)
		}
	}
	s.WriteString("   " + filtersLine + " / buscar\n\n")

	// Colunas Mestre-Detalhe (Segmentos x Detalhe)
	s.WriteString("   ╭─ Segmentos ────────────────────────────────────╮  ╭─ Map003.json#event:3/page:0/cmd:12 ──────╮\n")
	s.WriteString("   │ " + theme.StyleTagError.Render("● erro") + "     Map003 · ev 3 · cmd 12              │  │ Diálogo · falante \\N[1] (herói)          │\n")
	s.WriteString("   │   \\N[1]  \\V[12]                                │  │                                          │\n")
	s.WriteString("   │   \\N[1] conseguiu 12 moedas de ouro!           │  │ Origem                                   │\n")
	s.WriteString("   │                                                │  │ \\N[1]は\\V[12]ゴールドを手に入れた！      │\n")
	s.WriteString("   │ " + theme.StyleTagWarning.Render("● parcial") + "  Map003 · ev 7 · cmd 4               │  │                                          │\n")
	s.WriteString("   │   \\C[2]伝説の剣\\C[0]                           │  │ Tokens    \\N[1] " + theme.SymCheck + "     \\V[12] " + theme.SymCross + " ausente   │\n")
	s.WriteString("   │   — sem tradução —                             │  │                                          │\n")
	s.WriteString("   │                                                │  │ Tradução                                 │\n")
	s.WriteString("   │ " + theme.StyleTagWarning.Render("● parcial") + "  Map014 · ev 1 · cmd 9               │  │ " + theme.SymBarV + " \\N[1] conseguiu 12 moedas de ouro!     │\n")
	s.WriteString("   │   \\V[3]                                        │  │   o provedor trocou \\V[12] por \"12\"      │\n")
	s.WriteString("   │   — sem tradução (rate limit) —                │  │                                          │\n")
	s.WriteString("   │                                                │  │ Da memória · Map001                      │\n")
	s.WriteString("   │                                                │  │ \\N[1] recebeu \\V[12] moedas de ouro!     │\n")
	s.WriteString("   │                                                │  │ u usar esta sugestão                     │\n")
	s.WriteString("   │                                                │  │                                          │\n")
	s.WriteString("   │                                                │  │ [ ] Bloquear este segmento  b            │\n")
	s.WriteString("   ╰────────────────────────────────────────────────╯  ╰──────────────────────────────────────────╯\n")

	return s.String()
}

func (m *Model) renderResult() string {
	var s strings.Builder
	s.WriteString("     " + theme.StyleTitle.Render("Jogo traduzido") + "\n")
	s.WriteString("   Aventura do Slime está em português. 11 dos 12 arquivos alterados ficaram completos.\n\n")

	s.WriteString("   " + theme.SymBullet + " 11 completos   " + theme.SymBullet + " 1 parcial   " + theme.SymBullet + " 0 com erro   " + theme.SymBullet + " 228 novos na memória   " + theme.SymBullet + " 1 editado\n\n")
	s.WriteString("    " + theme.StyleTagWarning.Render(theme.SymWarning+" Map003.json ficou parcial: 1 fala sem tradução (o jogo mostra o original).") + "   r revisar\n\n")

	s.WriteString("   ╭─ Onde ficou cada coisa ──────────────────────────────────────────────────────────────────────╮\n")
	s.WriteString("   │ Jogo traduzido        ~/Jogos/Aventura do Slime                               o abrir        │\n")
	s.WriteString("   │ Original (backup)     ~/Jogos/Aventura do Slime-ja                            a abrir        │\n")
	s.WriteString("   │ Registro              ~/Jogos/Aventura do Slime/.slime/meta.json              e exportar     │\n")
	s.WriteString("   ╰──────────────────────────────────────────────────────────────────────────────────────────────╯\n\n")
	s.WriteString("   Código de saída 3 (concluído com arquivo parcial) · na CLI, use --json para automação\n")

	return s.String()
}

func (m *Model) renderFooter() string {
	switch m.view {
	case ViewOpenGame:
		return "  ↑↓ mover · enter escolher · tab recentes · ctrl+v colar · ctrl+p comandos · q sair"
	case ViewConfigure:
		return "  tab próximo campo · ←→ opção · espaço marcar · ctrl+enter traduzir · esc voltar"
	case ViewTranslating:
		return "  p pausar · c cancelar · l log · enter detalhes · r revisar (ao terminar) · q sair"
	case ViewReviewing:
		return "  ↑↓ segmento · tab filtro · / buscar · e editar · u sugestão · esc voltar · ctrl+s concluir"
	case ViewResult:
		return "  o abrir jogo · v verificar integridade · r revisar · n outro jogo · q sair"
	default:
		return "  q sair"
	}
}
