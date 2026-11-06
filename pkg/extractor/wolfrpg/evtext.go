package wolfrpg

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/BGLuis/slime-rpg/pkg/model"
	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/transform"
)

// openEvtext abre um script de texto (Data/Evtext/*.txt). Cada linha que não é
// vazia nem comando ("/...") é um segmento; a tradução vira uma única linha, com
// a indentação e a quebra de linha originais.
func openEvtext(data []byte) *document {
	isUTF8 := false
	content, _, err := transform.String(japanese.ShiftJIS.NewDecoder(), string(data))
	if err != nil || (strings.ContainsRune(content, utf8.RuneError) && utf8.Valid(data)) {
		content, isUTF8 = string(data), true
	}
	terminator := "\n"
	if strings.Contains(content, "\r\n") {
		terminator = "\r\n"
	}
	lines := strings.SplitAfter(content, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	return &document{
		utf8: isUTF8,
		walk: func(fn func(textSlot)) {
			for i, line := range lines {
				stripped := strings.TrimSpace(line)
				if stripped == "" || strings.HasPrefix(stripped, "/") {
					continue
				}
				fn(textSlot{
					id: fmt.Sprintf("line:%d", i), text: stripped, kind: model.KindDialogue,
					ctx: map[string]any{"line": i},
					set: func(tr string) { lines[i] = evtextLine(line, tr, terminator) },
				})
			}
		},
		save: func() []byte {
			out := strings.Join(lines, "")
			if isUTF8 {
				return []byte(out)
			}
			return encodeShiftJIS(out)
		},
	}
}

// evtextLine monta a linha traduzida como a versão Python: uma linha só, com a
// indentação e o terminador originais, e o "：" final de rótulos preservado.
func evtextLine(orig, translated, defaultTerminator string) string {
	tr := strings.Join(strings.Fields(strings.ReplaceAll(strings.TrimSpace(translated), "\r", "")), " ")
	if tr == strings.TrimSpace(orig) {
		return orig
	}
	terminator := ""
	switch {
	case strings.HasSuffix(orig, "\r\n"):
		terminator = "\r\n"
	case strings.HasSuffix(orig, "\n"):
		terminator = "\n"
	default:
		terminator = defaultTerminator
	}
	prefix := orig[:len(orig)-len(strings.TrimLeft(orig, " \t"))]
	if strings.HasSuffix(strings.TrimRight(orig, "\r\n \t"), "：") {
		switch {
		case strings.HasSuffix(tr, ":"):
			tr = strings.TrimSuffix(tr, ":") + "："
		case !strings.HasSuffix(tr, "："):
			tr += "："
		}
	}
	return prefix + tr + terminator
}
