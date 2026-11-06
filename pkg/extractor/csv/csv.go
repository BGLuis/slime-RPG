package csv

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

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

// Extract extrai os segmentos traduzíveis das colunas do CSV.
func (e *CSVExtractor) Extract(filePath string, data []byte, srcLang string) ([]*model.Segment, error) {
	base := filepath.Base(filePath)
	_, cleanData, _ := SniffBOMAndLineEnding(data)
	if len(cleanData) == 0 {
		return nil, ErrEmptyCSV
	}

	delim := e.Delimiter
	if delim == 0 {
		delim = SniffDelimiter(cleanData)
	}

	r := csv.NewReader(bytes.NewReader(cleanData))
	r.Comma = delim
	r.LazyQuotes = true
	r.FieldsPerRecord = -1 // Permite linhas variáveis

	records, err := r.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("failed to parse csv %s: %w", base, err)
	}
	if len(records) == 0 {
		return nil, ErrEmptyCSV
	}

	// Detecta se a primeira linha é cabeçalho
	header := records[0]
	translatableCols := e.findTranslatableColumns(header, srcLang)

	var segments []*model.Segment
	startRow := 1
	if len(translatableCols) == 0 {
		// Sem cabeçalho reconhecido, tenta traduzir todas as colunas de texto a partir da linha 0
		startRow = 0
		for c := 0; c < len(header); c++ {
			translatableCols = append(translatableCols, c)
		}
	}

	for rowIdx := startRow; rowIdx < len(records); rowIdx++ {
		row := records[rowIdx]
		for _, colIdx := range translatableCols {
			if colIdx >= len(row) {
				continue
			}
			val := row[colIdx]
			if strings.TrimSpace(val) == "" || isNumericOrCode(val) {
				continue
			}

			id := fmt.Sprintf("%s#row:%d/col:%d", base, rowIdx, colIdx)
			colName := ""
			if startRow == 1 && colIdx < len(header) {
				colName = header[colIdx]
			}

			seg, err := model.NewSegment(id, val, model.KindDB, map[string]any{
				"row":    rowIdx,
				"col":    colIdx,
				"header": colName,
			}, nil)
			if err == nil {
				segments = append(segments, seg)
			}
		}
	}

	return segments, nil
}

// Inject atualiza o CSV substituindo os textos pelos valores traduzidos e preservando BOM e CRLF.
func (e *CSVExtractor) Inject(filePath string, originalData []byte, segments []*model.Segment, tgtLang string) ([]byte, error) {
	base := filepath.Base(filePath)
	hasBOM, cleanData, lineTerminator := SniffBOMAndLineEnding(originalData)
	if len(cleanData) == 0 {
		return originalData, nil
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
		return nil, fmt.Errorf("failed to parse csv %s: %w", base, err)
	}

	// Mapeia traduções por ID canônico
	trMap := make(map[string]string, len(segments))
	for _, s := range segments {
		if s.IsResolved() && s.Target() != "" {
			trMap[s.ID()] = s.Target()
		}
	}

	if len(trMap) == 0 {
		return originalData, nil
	}

	// Substitui valores na matriz de records
	for rowIdx := range records {
		for colIdx := range records[rowIdx] {
			id := fmt.Sprintf("%s#row:%d/col:%d", base, rowIdx, colIdx)
			if target, found := trMap[id]; found {
				records[rowIdx][colIdx] = target
			}
		}
	}

	// Serializa de volta respeitando o delimitador, BOM e terminação de linha
	var buf bytes.Buffer
	if hasBOM {
		buf.Write([]byte{0xEF, 0xBB, 0xBF})
	}

	w := csv.NewWriter(&buf)
	w.Comma = delim
	w.UseCRLF = (lineTerminator == "\r\n")

	for _, row := range records {
		if err := w.Write(row); err != nil {
			return nil, fmt.Errorf("failed to write csv row: %w", err)
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return nil, fmt.Errorf("failed to flush csv writer: %w", err)
	}

	return buf.Bytes(), nil
}

func (e *CSVExtractor) findTranslatableColumns(header []string, srcLang string) []int {
	var cols []int

	// 1. Colunas configuradas explicitamente
	if len(e.Columns) > 0 {
		for colIdx, h := range header {
			cleanH := strings.TrimSpace(strings.ToLower(h))
			for _, conf := range e.Columns {
				if cleanH == strings.TrimSpace(strings.ToLower(conf)) {
					cols = append(cols, colIdx)
				}
			}
		}
		if len(cols) > 0 {
			return cols
		}
	}

	// 2. Colunas nomeadas por idioma (ex: "en", "ja", "source", "text", "dialogue", "value")
	srcNormalized := strings.ToLower(strings.TrimSpace(srcLang))
	defaultTranslatableNames := map[string]bool{
		"text": true, "dialogue": true, "value": true, "message": true,
		"string": true, "content": true, "desc": true, "description": true,
		"source": true,
	}

	for colIdx, h := range header {
		cleanH := strings.ToLower(strings.TrimSpace(h))
		if cleanH == srcNormalized || defaultTranslatableNames[cleanH] {
			cols = append(cols, colIdx)
		}
	}

	return cols
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
