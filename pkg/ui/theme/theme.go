package theme

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Tokens de cor semânticos exatos definidos no Figma (WCAG 2.2 AA).
const (
	BgBase          = lipgloss.Color("#12131A")
	BgSurface       = lipgloss.Color("#1A1B26")
	BgRaised        = lipgloss.Color("#24263A")
	BgAccentSubtle  = lipgloss.Color("#06281D")
	BgWarningSubtle = lipgloss.Color("#2A1D05")
	BgErrorSubtle   = lipgloss.Color("#2B0F10")
	BgInfoSubtle    = lipgloss.Color("#0F1B2D")

	BorderDefault = lipgloss.Color("#33364D")
	BorderStrong  = lipgloss.Color("#6B6F88")
	BorderFocus   = lipgloss.Color("#22C98A")

	TextPrimary  = lipgloss.Color("#E6E7EE")
	TextMuted    = lipgloss.Color("#9A9DB0")
	TextDisabled = lipgloss.Color("#6B6F88")
	TextOnAccent = lipgloss.Color("#0B0C10") // Contraste 7,3:1 sobre o verde

	AccentSlime      = lipgloss.Color("#04B575")
	AccentSlimeHover = lipgloss.Color("#22C98A")

	StatusSuccess = lipgloss.Color("#22C98A")
	StatusWarning = lipgloss.Color("#F59E0B")
	StatusError   = lipgloss.Color("#F87171")
	StatusInfo    = lipgloss.Color("#60A5FA")
)

// Símbolos seguros para terminal com largura monoestável (1 célula).
const (
	SymBullet     = "●"
	SymCheck      = "✓"
	SymCross      = "✗"
	SymWarning    = "▲"
	SymPlay       = "▶"
	SymArrow      = "›"
	SymBarH       = "━"
	SymBarThin    = "─"
	SymBlockFull  = "█"
	SymBlockLight = "░"
	SymBarV       = "┃"
	SymLock       = "⚿"
)

var (
	// Estilos Tipográficos e de Layout
	StyleApp = lipgloss.NewStyle().
			Background(BgBase).
			Foreground(TextPrimary)

	StyleHeader = lipgloss.NewStyle().
			Bold(true).
			Foreground(TextPrimary)

	StyleTitle = lipgloss.NewStyle().
			Bold(true).
			Foreground(TextPrimary).
			MarginBottom(1)

	StyleSubtitle = lipgloss.NewStyle().
			Foreground(TextMuted).
			MarginBottom(1)

	StyleMuted = lipgloss.NewStyle().
			Foreground(TextMuted)

	StyleAccent = lipgloss.NewStyle().
			Foreground(AccentSlime).
			Bold(true)

	// Caixas e Painéis
	StylePanel = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(BorderDefault).
			Background(BgSurface).
			Padding(1, 2)

	StylePanelActive = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(BorderFocus).
				Background(BgSurface).
				Padding(1, 2)

	// Botões
	StyleBtnPrimary = lipgloss.NewStyle().
			Background(AccentSlime).
			Foreground(TextOnAccent).
			Bold(true).
			Padding(0, 2)

	StyleBtnSecondary = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(BorderStrong).
				Foreground(TextPrimary).
				Padding(0, 1)

	// Tags de Status
	StyleTagSuccess = lipgloss.NewStyle().
			Foreground(StatusSuccess).
			Background(BgAccentSubtle).
			Padding(0, 1)

	StyleTagProcess = lipgloss.NewStyle().
			Foreground(StatusInfo).
			Background(BgInfoSubtle).
			Padding(0, 1)

	StyleTagWarning = lipgloss.NewStyle().
			Foreground(StatusWarning).
			Background(BgWarningSubtle).
			Padding(0, 1)

	StyleTagError = lipgloss.NewStyle().
			Foreground(StatusError).
			Background(BgErrorSubtle).
			Padding(0, 1)

	StyleTagMuted = lipgloss.NewStyle().
			Foreground(TextMuted).
			Background(BgRaised).
			Padding(0, 1)
)

// RenderProgressBar renderiza uma barra de progresso elegante compatível com TUI.
func RenderProgressBar(percent float64, width int) string {
	if width <= 0 {
		width = 20
	}
	if percent < 0 {
		percent = 0
	}
	if percent > 1.0 {
		percent = 1.0
	}

	filledLen := int(percent * float64(width))
	emptyLen := width - filledLen

	filled := strings.Repeat(SymBlockFull, filledLen)
	empty := strings.Repeat(SymBlockLight, emptyLen)

	return lipgloss.NewStyle().Foreground(AccentSlime).Render(filled) +
		lipgloss.NewStyle().Foreground(BorderDefault).Render(empty)
}
