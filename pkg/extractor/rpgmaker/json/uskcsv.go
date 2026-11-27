package json

import (
	"bytes"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BGLuis/slime-rpg/pkg/extractor/jsonedit"
	"github.com/BGLuis/slime-rpg/pkg/extractor/rpgmaker/rpgtext"
	"github.com/BGLuis/slime-rpg/pkg/model"
)

func isCSVFile(filePath string) bool {
	return strings.EqualFold(filepath.Ext(filePath), ".csv")
}

type csvRecord struct {
	key       string
	bodyStart int // offset em bytes dentro de clean (sem aspas externas)
	bodyEnd   int
}

// extractUskCSV extrai blocos de diálogo de arquivos CSV do RPG Maker MZ (como os
// usados pelo UskCsvLoader/ExternMessage em data/unique/*.csv), preservando a
// coluna 0 (chave ID), blocos ":script ... :end" e diretivas ":voice[...]", etc.
func (e *MVExtractor) extractUskCSV(rel string, data []byte) ([]*model.Segment, error) {
	clean := jsonedit.StripBOM(data)
	records := scanUskCSV(clean)
	base := filepath.Base(rel)
	var segments []*model.Segment

	for rowIdx, rec := range records {
		body := string(clean[rec.bodyStart:rec.bodyEnd])
		if rec.key == "" || strings.TrimSpace(body) == "" {
			continue
		}
		if strings.Contains(body, "CSVファイル側で有効なコマンド") {
			continue
		}

		spans := csvDialogueSpans(body)
		for i, sp := range spans {
			if !isTranslatableText(sp.Source) || rpgtext.IsTechnicalOrCode(sp.Source, rpgtext.Dialogue) {
				continue
			}
			id := fmt.Sprintf("%s#row:%d:%s/span:%d", base, rowIdx, rec.key, i)
			seg, err := model.NewSegment(id, sp.Source, model.KindDialogue, map[string]any{
				"file": rel,
				"row":  rowIdx,
				"col":  1,
				"span": sp,
			}, nil)
			if err == nil {
				segments = append(segments, seg)
			}
		}
	}
	return segments, nil
}

// injectUskCSV substitui apenas os trechos traduzidos pelos seus offsets exatos no
// arquivo original, mantendo intactos blocos ":script ... :end", aspas originais,
// diretivas e quebras de linha finais.
func (e *MVExtractor) injectUskCSV(rel string, originalData []byte, segments []*model.Segment) ([]byte, error) {
	type spanEdit struct {
		sp     rpgtext.Span
		target string
	}
	base := filepath.Base(rel)
	editsByRow := make(map[int][]spanEdit)
	for _, s := range segments {
		if !s.IsResolved() || s.Target() == s.Source() {
			continue
		}
		if !strings.HasPrefix(s.ID(), base+"#") {
			continue
		}
		if f, ok := s.ContextValue("file"); ok && fmt.Sprintf("%v", f) != rel {
			continue
		}
		rowVal, okRow := s.ContextValue("row")
		spVal, okSp := s.ContextValue("span")
		row, okInt := rowVal.(int)
		sp, okSpan := spVal.(rpgtext.Span)
		if !okRow || !okInt || !okSp || !okSpan {
			continue
		}
		editsByRow[row] = append(editsByRow[row], spanEdit{sp: sp, target: s.Target()})
	}
	if len(editsByRow) == 0 {
		return originalData, nil
	}

	clean := jsonedit.StripBOM(originalData)
	hasBOM := len(clean) != len(originalData)
	records := scanUskCSV(clean)

	type absEdit struct {
		start, end int
		repl       string
	}
	var abs []absEdit
	for row, list := range editsByRow {
		if row < 0 || row >= len(records) {
			continue
		}
		rec := records[row]
		cellLen := rec.bodyEnd - rec.bodyStart
		for _, ed := range list {
			if ed.sp.Start < 0 || ed.sp.End > cellLen || ed.sp.Start > ed.sp.End {
				continue
			}
			abs = append(abs, absEdit{
				start: rec.bodyStart + ed.sp.Start,
				end:   rec.bodyStart + ed.sp.End,
				repl:  sanitizeCSVQuotes(ed.target),
			})
		}
	}
	if len(abs) == 0 {
		return originalData, nil
	}

	sort.Slice(abs, func(i, j int) bool { return abs[i].start > abs[j].start })
	out := make([]byte, len(clean))
	copy(out, clean)
	for _, ed := range abs {
		if ed.end > len(out) || ed.start > ed.end {
			continue
		}
		updated := make([]byte, 0, len(out)-(ed.end-ed.start)+len(ed.repl))
		updated = append(updated, out[:ed.start]...)
		updated = append(updated, ed.repl...)
		updated = append(updated, out[ed.end:]...)
		out = updated
	}
	if hasBOM {
		return append([]byte{0xEF, 0xBB, 0xBF}, out...), nil
	}
	return out, nil
}

// sanitizeCSVQuotes converte aspas ASCII (") em aspas tipográficas para evitar que
// parsers como ExternMessage.js interpretem uma aspa interna como fim de célula CSV.
func sanitizeCSVQuotes(s string) string {
	if !strings.ContainsRune(s, '"') {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	open := true
	for _, r := range s {
		if r == '"' {
			if open {
				b.WriteRune('“')
			} else {
				b.WriteRune('”')
			}
			open = !open
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// scanUskCSV localiza registros (chave, intervalo [bodyStart, bodyEnd)) em bytes
// sem alterar nem re-escapar aspas existentes em blocos :script.
func scanUskCSV(data []byte) []csvRecord {
	var recs []csvRecord
	n := len(data)
	pos := 0
	for pos < n {
		// Pula linhas vazias entre registros
		for pos < n && (data[pos] == '\n' || data[pos] == '\r') {
			pos++
		}
		if pos >= n {
			break
		}
		comma := bytes.IndexByte(data[pos:], ',')
		if comma < 0 {
			break
		}
		nlInLine := bytes.IndexByte(data[pos:pos+comma], '\n')
		if nlInLine >= 0 {
			pos += nlInLine + 1
			continue
		}
		key := strings.TrimSpace(strings.Trim(string(data[pos:pos+comma]), `"`))
		valStart := pos + comma + 1
		if valStart >= n {
			break
		}

		if data[valStart] == '"' {
			bodyStart := valStart + 1
			closeQuote := findClosingCSVQuote(data, bodyStart)
			if closeQuote < 0 {
				break
			}
			recs = append(recs, csvRecord{key: key, bodyStart: bodyStart, bodyEnd: closeQuote})
			pos = closeQuote + 1
			for pos < n && data[pos] != '\n' {
				pos++
			}
			if pos < n && data[pos] == '\n' {
				pos++
			}
			continue
		}

		lineEnd := bytes.IndexByte(data[valStart:], '\n')
		if lineEnd < 0 {
			lineEnd = n - valStart
		}
		bodyEnd := valStart + lineEnd
		if bodyEnd > valStart && data[bodyEnd-1] == '\r' {
			bodyEnd--
		}
		recs = append(recs, csvRecord{key: key, bodyStart: valStart, bodyEnd: bodyEnd})
		pos = valStart + lineEnd
		if pos < n && data[pos] == '\n' {
			pos++
		}
	}
	return recs
}

// findClosingCSVQuote encontra o '"' que realmente fecha a célula, ignorando aspas
// internas de :script (como `"vagina"`) verificando se após o '"' vem fim de linha
// seguido de EOF ou de um novo cabeçalho `key,`.
func findClosingCSVQuote(data []byte, start int) int {
	n := len(data)
	for i := start; i < n; i++ {
		if data[i] != '"' {
			continue
		}
		if i > start && data[i-1] == '\\' {
			continue
		}
		// Se há outra '"' colada (""), pula o par escapado
		if i+1 < n && data[i+1] == '"' {
			i++
			continue
		}
		j := i + 1
		for j < n && (data[j] == ' ' || data[j] == '\t' || data[j] == '\r') {
			j++
		}
		if j >= n {
			return i
		}
		if data[j] == '\n' {
			if nextRecordStartsHere(data, j+1) {
				return i
			}
		}
	}
	return -1
}

func nextRecordStartsHere(data []byte, pos int) bool {
	n := len(data)
	for pos < n && (data[pos] == '\n' || data[pos] == '\r') {
		pos++
	}
	if pos >= n {
		return true
	}
	// Nova chave de registro nunca começa com espaço, ':', '\', '[' ou '"'
	first := data[pos]
	if first == ' ' || first == '\t' || first == ':' || first == '\\' || first == '[' || first == '"' {
		return false
	}
	nl := bytes.IndexByte(data[pos:], '\n')
	lineEnd := n
	if nl >= 0 {
		lineEnd = pos + nl
	}
	line := data[pos:lineEnd]
	comma := bytes.IndexByte(line, ',')
	if comma <= 0 {
		return false
	}
	keyPart := line[:comma]
	return !bytes.ContainsAny(keyPart, " \t[](){};\"")
}

// csvDialogueSpans divide o texto de uma célula em trechos traduzíveis contíguos,
// mantendo blocos ":script ... :end" e linhas de diretiva (":voice[...]", ":page", etc.) fora.
func csvDialogueSpans(body string) []rpgtext.Span {
	var spans []rpgtext.Span
	inScript := false
	blockStart := -1
	blockEnd := -1

	flush := func() {
		if blockStart >= 0 && blockEnd > blockStart {
			txt := body[blockStart:blockEnd]
			if strings.TrimSpace(txt) != "" {
				spans = append(spans, rpgtext.Span{Start: blockStart, End: blockEnd, Source: txt})
			}
		}
		blockStart, blockEnd = -1, -1
	}

	pos := 0
	for pos <= len(body) {
		nl := strings.IndexByte(body[pos:], '\n')
		lineEnd := len(body)
		nextPos := len(body) + 1
		if nl >= 0 {
			lineEnd = pos + nl
			nextPos = lineEnd + 1
		}
		rawLine := body[pos:lineEnd]
		trimmed := strings.TrimSpace(strings.TrimSuffix(rawLine, "\r"))

		switch {
		case inScript:
			if strings.EqualFold(trimmed, ":end") {
				inScript = false
			}
		case strings.EqualFold(trimmed, ":script"):
			flush()
			inScript = true
		case strings.HasPrefix(trimmed, ":"):
			flush()
		case trimmed == "":
			flush()
		default:
			cleanEnd := lineEnd
			if cleanEnd > pos && body[cleanEnd-1] == '\r' {
				cleanEnd--
			}
			if blockStart == -1 {
				blockStart = pos
			}
			blockEnd = cleanEnd
		}

		if nl < 0 {
			break
		}
		pos = nextPos
	}
	flush()
	return spans
}
