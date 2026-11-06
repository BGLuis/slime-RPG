package csv

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/BGLuis/slime-rpg/pkg/langcode"
	"github.com/BGLuis/slime-rpg/pkg/model"
)

var (
	ErrEmptyCSV = errors.New("csv file is empty")
)

// CSVExtractor é o extrator especializado em arquivos CSV tabulares e de localização.
type CSVExtractor struct {
	Columns   []string // Colunas específicas a traduzir (se vazio, auto-detecta)
	Delimiter rune     // Delimitador forçado (se 0, auto-detecta)
}

func NewCSVExtractor() *CSVExtractor {
	return &CSVExtractor{}
}

// CanHandle verifica se o arquivo possui extensão .csv.
func (e *CSVExtractor) CanHandle(filePath string) bool {
	return strings.ToLower(filepath.Ext(filePath)) == ".csv"
}

// SniffDelimiter detecta automaticamente o delimitador predominante entre vírgula, ponto e vírgula, tab e pipe.
func SniffDelimiter(data []byte) rune {
	delimiters := []rune{',', ';', '\t', '|'}
	lines := strings.Split(string(data), "\n")
	sampleLines := lines
	if len(sampleLines) > 5 {
		sampleLines = sampleLines[:5]
	}

	bestDelim := ','
	maxCount := 0

	for _, d := range delimiters {
		count := 0
		for _, line := range sampleLines {
			count += strings.Count(line, string(d))
		}
		if count > maxCount {
			maxCount = count
			bestDelim = d
		}
	}

	return bestDelim
}

// SniffBOM e LineTerminator detectam cabeçalho UTF-8 BOM e tipo de quebra de linha (CRLF vs LF).
func SniffBOMAndLineEnding(data []byte) (hasBOM bool, clean []byte, lineTerminator string) {
	clean = data
	hasBOM = false
	if len(data) >= 3 && data[0] == 0xEF && data[1] == 0xBB && data[2] == 0xBF {
		hasBOM = true
		clean = data[3:]
	}

	lineTerminator = "\n"
	if bytes.Contains(clean, []byte("\r\n")) {
		lineTerminator = "\r\n"
	} else if bytes.Contains(clean, []byte("\r")) && !bytes.Contains(clean, []byte("\n")) {
		lineTerminator = "\r"
	}

	return hasBOM, clean, lineTerminator
}

// Extract extrai os segmentos traduzíveis do CSV. A primeira linha é sempre o
// cabeçalho. Numa tabela multilíngue (colunas "key,en,es") lê da coluna do idioma
// de origem e pula linhas cuja coluna do idioma de destino já está preenchida.
func (e *CSVExtractor) Extract(filePath string, data []byte, srcLang, tgtLang string) ([]*model.Segment, error) {
	base := filepath.Base(filePath)
	records, _, err := e.parse(base, data)
	if err != nil {
		return nil, err
	}

	header := records[0]
	plan := e.planColumns(header, srcLang, tgtLang)

	var segments []*model.Segment
	for rowIdx := 1; rowIdx < len(records); rowIdx++ {
		row := records[rowIdx]
		if plan.targetCol >= 0 && plan.targetCol < len(row) && strings.TrimSpace(row[plan.targetCol]) != "" {
			continue
		}
		for _, colIdx := range plan.sourceCols {
			if colIdx >= len(row) {
				continue
			}
			val := row[colIdx]
			// Linhas iniciadas por '■' são títulos de seção das planilhas de localização.
			if strings.TrimSpace(val) == "" || strings.HasPrefix(val, "■") || isNumericOrCode(val) {
				continue
			}

			ctx := map[string]any{"row": rowIdx, "col": colIdx, "header": header[colIdx]}
			if plan.targetHeader != "" {
				ctx["targetHeader"] = plan.targetHeader
			}
			seg, err := model.NewSegment(fmt.Sprintf("%s#row:%d/col:%d", base, rowIdx, colIdx), val, model.KindDB, ctx, nil)
			if err == nil {
				segments = append(segments, seg)
			}
		}
	}

	return segments, nil
}

// Inject grava as traduções preservando delimitador, BOM e quebra de linha. Em
// tabelas multilíngues a coluna de origem fica intacta e a tradução vai para a
// coluna do idioma de destino, criada ao final do cabeçalho se não existir.
func (e *CSVExtractor) Inject(filePath string, originalData []byte, segments []*model.Segment, tgtLang string) ([]byte, error) {
	base := filepath.Base(filePath)

	var resolved []*model.Segment
	for _, s := range segments {
		if s.IsResolved() && s.Target() != "" {
			resolved = append(resolved, s)
		}
	}
	if len(resolved) == 0 {
		return originalData, nil
	}

	records, meta, err := e.parse(base, originalData)
	if err != nil {
		return nil, err
	}

	for _, s := range resolved {
		rowVal, _ := s.ContextValue("row")
		colVal, _ := s.ContextValue("col")
		row, okRow := rowVal.(int)
		col, okCol := colVal.(int)
		if !okRow || !okCol || row <= 0 || row >= len(records) {
			continue
		}
		if targetHeader, ok := s.ContextValue("targetHeader"); ok {
			col = ensureColumn(records, targetHeader.(string))
		}
		for len(records[row]) <= col {
			records[row] = append(records[row], "")
		}
		records[row][col] = s.Target()
	}

	var buf bytes.Buffer
	if meta.hasBOM {
		buf.Write([]byte{0xEF, 0xBB, 0xBF})
	}

	w := csv.NewWriter(&buf)
	w.Comma = meta.delimiter
	w.UseCRLF = meta.lineTerminator == "\r\n"
	if err := w.WriteAll(records); err != nil {
		return nil, fmt.Errorf("failed to write csv: %w", err)
	}

	return buf.Bytes(), nil
}

type csvMeta struct {
	hasBOM         bool
	lineTerminator string
	delimiter      rune
}

func (e *CSVExtractor) parse(base string, data []byte) ([][]string, csvMeta, error) {
	hasBOM, cleanData, lineTerminator := SniffBOMAndLineEnding(data)
	if len(bytes.TrimSpace(cleanData)) == 0 {
		return nil, csvMeta{}, ErrEmptyCSV
	}

	delim := e.Delimiter
	if delim == 0 {
		delim = SniffDelimiter(cleanData)
	}

	r := csv.NewReader(bytes.NewReader(cleanData))
	r.Comma = delim
	r.LazyQuotes = true
	r.FieldsPerRecord = -1

	records, err := r.ReadAll()
	if err != nil {
		return nil, csvMeta{}, fmt.Errorf("failed to parse csv %s: %w", base, err)
	}
	if len(records) == 0 {
		return nil, csvMeta{}, ErrEmptyCSV
	}
	return records, csvMeta{hasBOM: hasBOM, lineTerminator: lineTerminator, delimiter: delim}, nil
}

// ensureColumn devolve o índice da coluna com o cabeçalho dado, criando-a (com
// células vazias em todas as linhas) quando não existe.
func ensureColumn(records [][]string, header string) int {
	for i, h := range records[0] {
		if h == header {
			return i
		}
	}
	width := len(records[0])
	for i := range records {
		for len(records[i]) < width {
			records[i] = append(records[i], "")
		}
		if i == 0 {
			records[i] = append(records[i], header)
		} else {
			records[i] = append(records[i], "")
		}
	}
	return width
}

type columnPlan struct {
	sourceCols   []int
	targetCol    int    // coluna de destino existente numa tabela multilíngue, ou -1
	targetHeader string // cabeçalho que recebe a tradução numa tabela multilíngue
}

var (
	commonTextColumns = []string{"text", "value", "translation", "content", "message", "msg", "dialogue", "string", "desc", "description", "target"}
	commonIDColumns   = []string{"id", "key", "code", "name", "identifier", "tag", "label", "index"}
)

// planColumns escolhe as colunas a traduzir, na ordem de prioridade da versão Python:
// tabela multilíngue, colunas configuradas, nomes comuns de texto e, para planilhas
// de duas colunas, a coluna que não é identificador.
func (e *CSVExtractor) planColumns(header []string, srcLang, tgtLang string) columnPlan {
	plan := columnPlan{targetCol: -1}

	src, tgt := langcode.Normalize(srcLang), langcode.Normalize(tgtLang)
	if tgt == "" {
		tgt = strings.ToLower(tgtLang)
	}
	sourceCol := -1
	for i, h := range header {
		switch lang := langcode.IsLanguageColumn(h); {
		case lang == "":
		case lang == src && sourceCol < 0:
			sourceCol = i
		case lang == tgt && plan.targetCol < 0:
			plan.targetCol = i
		}
	}
	if src != "" && sourceCol >= 0 {
		plan.sourceCols = []int{sourceCol}
		if plan.targetCol >= 0 {
			plan.targetHeader = header[plan.targetCol]
		} else {
			srcHeader := header[sourceCol]
			switch {
			case isLower(srcHeader):
				plan.targetHeader = tgt
			case isUpper(srcHeader):
				plan.targetHeader = strings.ToUpper(tgt)
			default:
				plan.targetHeader = langcode.EnglishName(tgt)
			}
		}
		return plan
	}

	if cols := matchColumns(header, e.Columns); len(cols) > 0 {
		plan.sourceCols = cols
		return plan
	}
	if cols := matchColumns(header, commonTextColumns); len(cols) > 0 {
		plan.sourceCols = cols
		return plan
	}
	if len(header) == 2 {
		first, second := isIDColumn(header[0]), isIDColumn(header[1])
		if second && !first {
			plan.sourceCols = []int{0}
		} else {
			plan.sourceCols = []int{1}
		}
	}
	return plan
}

func matchColumns(header, names []string) []int {
	var cols []int
	for i, h := range header {
		clean := strings.ToLower(strings.TrimSpace(h))
		for _, n := range names {
			if clean == strings.ToLower(strings.TrimSpace(n)) {
				cols = append(cols, i)
				break
			}
		}
	}
	return cols
}

func isIDColumn(h string) bool {
	return len(matchColumns([]string{h}, commonIDColumns)) > 0
}

func isLower(s string) bool { return s == strings.ToLower(s) && s != strings.ToUpper(s) }
func isUpper(s string) bool { return s == strings.ToUpper(s) && s != strings.ToLower(s) }

// IsTranslatableFile indica se o arquivo deve ser traduzido: numa pasta com um CSV
// por idioma (en.csv, es.csv), só o do idioma de origem é processado.
func IsTranslatableFile(fileName, srcLang string) bool {
	fileLang, _, ok := langcode.FromFilename(fileName)
	if !ok {
		return true
	}
	src := langcode.Normalize(srcLang)
	return src == "" || fileLang == src
}

// MaskPatterns devolve os padrões protegidos em CSVs de localização: códigos de
// formato (\c[2], \item<42>), printf (%d), variáveis ({hero_name}), tags de
// rich text e quebras de linha escapadas.
func MaskPatterns() []*regexp.Regexp {
	return []*regexp.Regexp{
		regexp.MustCompile(`(?i)\\[A-Za-z]+\s*\[[^\]]*\]`),
		regexp.MustCompile(`(?i)\\[A-Za-z]+\s*<[^>]*>`),
		regexp.MustCompile(`%[0-9]*\.?[0-9]*[sdfoxX]`),
		regexp.MustCompile(`\{[a-zA-Z0-9_]+\}`),
		regexp.MustCompile(`(?i)</?[a-zA-Z][a-zA-Z0-9=_\-#]*(?:\s+[^>]*)?/?>`),
		regexp.MustCompile(`\\n|\\r|\\t`),
	}
}

func isNumericOrCode(val string) bool {
	clean := strings.TrimSpace(val)
	if clean == "" {
		return true
	}
	// Se for puramente dígitos e pontuação matemática
	numericChars := 0
	for _, r := range clean {
		if (r >= '0' && r <= '9') || r == '.' || r == ',' || r == '-' || r == '+' {
			numericChars++
		}
	}
	return numericChars == len([]rune(clean))
}
