package genericjson

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/BGLuis/slime-rpg/pkg/model"
)

var (
	ErrEmptyJSON   = errors.New("json file is empty")
	ErrInvalidJSON = errors.New("invalid json format")
)

var defaultTranslatableKeys = map[string]bool{
	"text":        true,
	"message":     true,
	"name":        true,
	"description": true,
	"title":       true,
	"dialogue":    true,
	"content":     true,
	"summary":     true,
	"caption":     true,
	"label":       true,
	"desc":        true,
	"bio":         true,
	"note":        true,
	"hint":        true,
	"help":        true,
}

var technicalExtensions = []string{
	".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp", ".svg",
	".ogg", ".mp3", ".wav", ".m4a", ".flac", ".mid",
	".json", ".js", ".csv", ".dat", ".mps", ".project",
	".ttf", ".otf", ".woff", ".woff2",
}

// JSONExtractor extrai e injeta segmentos em arquivos JSON genéricos.
type JSONExtractor struct {
	TranslatableKeys map[string]bool
	AllowAllKeys     bool
}

// NewJSONExtractor cria uma instância com as chaves traduzíveis padrão.
func NewJSONExtractor() *JSONExtractor {
	keys := make(map[string]bool, len(defaultTranslatableKeys))
	for k, v := range defaultTranslatableKeys {
		keys[k] = v
	}
	return &JSONExtractor{
		TranslatableKeys: keys,
		AllowAllKeys:     false,
	}
}

// SetKeys configura a safelist de chaves a traduzir (ou "*" para todas as chaves).
func (e *JSONExtractor) SetKeys(keys []string) {
	e.TranslatableKeys = make(map[string]bool)
	e.AllowAllKeys = false

	for _, k := range keys {
		clean := strings.TrimSpace(strings.ToLower(k))
		if clean == "*" || clean == "all" {
			e.AllowAllKeys = true
			return
		}
		if clean != "" {
			e.TranslatableKeys[clean] = true
		}
	}
}

// CanHandle verifica se o arquivo é um arquivo JSON.
func (e *JSONExtractor) CanHandle(filePath string) bool {
	return strings.ToLower(filepath.Ext(filePath)) == ".json"
}

// EscapePointer aplica o escape de JSON Pointer (RFC 6901): '~' -> '~0', '/' -> '~1'.
func EscapePointer(token string) string {
	token = strings.ReplaceAll(token, "~", "~0")
	token = strings.ReplaceAll(token, "/", "~1")
	return token
}

// UnescapePointer reverte o escape de JSON Pointer (RFC 6901).
func UnescapePointer(token string) string {
	token = strings.ReplaceAll(token, "~1", "/")
	token = strings.ReplaceAll(token, "~0", "~")
	return token
}

// SniffIndentation detecta a indentação predominante do JSON (espaços ou tabs).
func SniffIndentation(data []byte) (prefix string, indent string) {
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "\t") {
			return "", "\t"
		}
		if strings.HasPrefix(line, "    ") {
			return "", "    "
		}
		if strings.HasPrefix(line, "  ") {
			return "", "  "
		}
	}
	return "", "  " // Default padrão elegante de 2 espaços
}

// Extract extrai os segmentos traduzíveis do arquivo JSON.
func (e *JSONExtractor) Extract(filePath string, data []byte, srcLang string) ([]*model.Segment, error) {
	cleanData := bytes.TrimSpace(data)
	if len(cleanData) == 0 {
		return nil, ErrEmptyJSON
	}

	dec := json.NewDecoder(bytes.NewReader(cleanData))
	dec.UseNumber()

	var root any
	if err := dec.Decode(&root); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidJSON, err)
	}

	base := filepath.Base(filePath)
	var segments []*model.Segment

	var walk func(node any, ptr string, parentKey string)
	walk = func(node any, ptr string, parentKey string) {
		switch v := node.(type) {
		case map[string]any:
			for key, val := range v {
				childPtr := ptr + "/" + EscapePointer(key)
				walk(val, childPtr, key)
			}
		case []any:
			for idx, item := range v {
				childPtr := ptr + "/" + strconv.Itoa(idx)
				walk(item, childPtr, parentKey)
			}
		case string:
			if !e.isTranslatable(parentKey, v) {
				return
			}
			id := fmt.Sprintf("%s#%s", base, ptr)
			kind := model.KindDialogue
			lowerKey := strings.ToLower(parentKey)
			if lowerKey == "name" || lowerKey == "title" || lowerKey == "label" || lowerKey == "caption" {
				kind = model.KindMenu
			}

			seg, err := model.NewSegment(id, v, kind, map[string]any{
				"file":    base,
				"pointer": ptr,
				"key":     parentKey,
			}, nil)
			if err == nil {
				segments = append(segments, seg)
			}
		}
	}

	walk(root, "", "")
	return segments, nil
}

// Inject atualiza o arquivo JSON com as traduções fornecidas.
func (e *JSONExtractor) Inject(filePath string, originalData []byte, segments []*model.Segment, tgtLang string) ([]byte, error) {
	cleanData := bytes.TrimSpace(originalData)
	if len(cleanData) == 0 {
		return originalData, nil
	}

	trMap := make(map[string]string, len(segments))
	for _, s := range segments {
		if s.IsResolved() && s.Target() != "" {
			trMap[s.ID()] = s.Target()
		}
	}

	if len(trMap) == 0 {
		return originalData, nil
	}

	dec := json.NewDecoder(bytes.NewReader(cleanData))
	dec.UseNumber()

	var root any
	if err := dec.Decode(&root); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidJSON, err)
	}

	base := filepath.Base(filePath)
	changedAny := false

	var walkUpdate func(node any, ptr string) any
	walkUpdate = func(node any, ptr string) any {
		switch v := node.(type) {
		case map[string]any:
			for key, val := range v {
				childPtr := ptr + "/" + EscapePointer(key)
				v[key] = walkUpdate(val, childPtr)
			}
			return v
		case []any:
			for idx, item := range v {
				childPtr := ptr + "/" + strconv.Itoa(idx)
				v[idx] = walkUpdate(item, childPtr)
			}
			return v
		case string:
			id := fmt.Sprintf("%s#%s", base, ptr)
			if target, found := trMap[id]; found {
				changedAny = true
				return target
			}
			return v
		default:
			return v
		}
	}

	updated := walkUpdate(root, "")
	if !changedAny {
		return originalData, nil
	}

	prefix, indent := SniffIndentation(originalData)
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent(prefix, indent)

	if err := enc.Encode(updated); err != nil {
		return nil, fmt.Errorf("failed to encode updated json: %w", err)
	}

	out := buf.Bytes()
	// Preserva se o arquivo original terminava ou não com quebra de linha
	if !bytes.HasSuffix(originalData, []byte("\n")) && bytes.HasSuffix(out, []byte("\n")) {
		out = bytes.TrimSuffix(out, []byte("\n"))
	}
	return out, nil
}

func (e *JSONExtractor) isTranslatable(key, text string) bool {
	str := strings.TrimSpace(text)
	if str == "" {
		return false
	}

	// Chave permitida
	if !e.AllowAllKeys && len(e.TranslatableKeys) > 0 {
		lowerKey := strings.ToLower(key)
		if !e.TranslatableKeys[lowerKey] {
			return false
		}
	}

	// Filtra extensões técnicas de arquivos
	lowerStr := strings.ToLower(str)
	for _, ext := range technicalExtensions {
		if strings.HasSuffix(lowerStr, ext) {
			return false
		}
	}

	// Filtra expressões de script e código (ex: $gameVariables.value(1), javascript, etc.)
	if strings.HasPrefix(str, "$") || strings.HasPrefix(str, "http://") || strings.HasPrefix(str, "https://") {
		return false
	}

	// Se for puramente números ou códigos de controle
	if isNumeric(str) {
		return false
	}

	return true
}

func isNumeric(s string) bool {
	numCount := 0
	for _, r := range s {
		if (r >= '0' && r <= '9') || r == '.' || r == ',' || r == '-' || r == '+' {
			numCount++
		}
	}
	return numCount == len([]rune(s))
}
