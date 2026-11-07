package tui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// Larguras medem colunas visíveis (ignoram ANSI e contam CJK como 2), porque
// nomes de jogos e segmentos em japonês ocupam o dobro de colunas por rune.

const (
	maxContentWidth = 100
	minContentWidth = 40
	indent          = "   "
)

// contentWidth é a largura útil da tela, limitada para não esticar demais em monitores largos.
func (m *Model) contentWidth() int {
	return min(max(m.width, minContentWidth), maxContentWidth)
}

// truncate corta s para caber em n colunas, terminando em "…".
func truncate(s string, n int) string {
	if ansi.StringWidth(s) <= n {
		return s
	}
	return ansi.Truncate(s, max(n, 0), "…")
}

// fit corta ou completa s para ocupar exatamente n colunas.
func fit(s string, n int) string {
	s = truncate(s, n)
	return s + strings.Repeat(" ", max(n-ansi.StringWidth(s), 0))
}

// tail mantém o fim de s em até n colunas: em campos de digitação o cursor fica sempre visível.
func tail(s string, n int) string {
	if ansi.StringWidth(s) <= n {
		return s
	}
	r := []rune(s)
	for len(r) > 0 && ansi.StringWidth(string(r)) > n-1 {
		r = r[1:]
	}
	return "…" + string(r)
}

// boxTop desenha a borda superior de uma caixa com título, completada até width colunas.
func boxTop(title string, width int) string {
	head := "╭─ " + truncate(title, max(width-6, 1)) + " "
	return head + strings.Repeat("─", max(width-ansi.StringWidth(head)-1, 0)) + "╮"
}

// box emoldura lines numa caixa de width colunas; linhas longas são cortadas, nunca quebradas.
func box(title string, lines []string, width int) []string {
	inner := max(width-4, 1)
	out := make([]string, 0, len(lines)+2)
	out = append(out, boxTop(title, width))
	for _, l := range lines {
		out = append(out, "│ "+fit(l, inner)+" │")
	}
	return append(out, "╰"+strings.Repeat("─", max(width-2, 0))+"╯")
}

// wrapJoin junta parts com sep, abrindo uma nova linha sempre que a próxima parte não cabe em width.
func wrapJoin(parts []string, sep string, width int) []string {
	var lines []string
	cur := ""
	for _, part := range parts {
		if cur != "" && ansi.StringWidth(cur+sep+part) > width {
			lines, cur = append(lines, cur), ""
		}
		if cur != "" {
			cur += sep
		}
		cur += part
	}
	return append(lines, cur)
}

func indentLines(lines []string) string {
	var b strings.Builder
	for _, l := range lines {
		b.WriteString(indent + l + "\n")
	}
	return b.String()
}
