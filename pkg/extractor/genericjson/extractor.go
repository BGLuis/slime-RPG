package genericjson

import (
	"bytes"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/BGLuis/slime-rpg/pkg/extractor/jsonedit"
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

// Extract extrai, na ordem do documento, as strings traduzíveis do arquivo JSON.
func (e *JSONExtractor) Extract(filePath string, data []byte, srcLang string) ([]*model.Segment, error) {
	if len(bytes.TrimSpace(jsonedit.StripBOM(data))) == 0 {
		return nil, ErrEmptyJSON
	}
	values, err := jsonedit.Strings(data)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidJSON, err)
	}

	base := filepath.Base(filePath)
	var segments []*model.Segment
	for _, v := range values {
		if !e.isTranslatable(v.Key, v.Value) {
			continue
		}
		kind := model.KindDialogue
		switch strings.ToLower(v.Key) {
		case "name", "title", "label", "caption":
			kind = model.KindMenu
		}
		seg, err := model.NewSegment(fmt.Sprintf("%s#%s", base, v.Pointer), v.Value, kind, map[string]any{
			"file":    base,
			"pointer": v.Pointer,
			"key":     v.Key,
		}, nil)
		if err == nil {
			segments = append(segments, seg)
		}
	}
	return segments, nil
}

// Inject grava as traduções substituindo apenas as strings traduzidas: ordem das
// chaves, indentação, números e BOM do arquivo original são preservados.
func (e *JSONExtractor) Inject(filePath string, originalData []byte, segments []*model.Segment, tgtLang string) ([]byte, error) {
	replacements := make(map[string]string, len(segments))
	for _, s := range segments {
		if !s.IsResolved() || s.Target() == "" {
			continue
		}
		if ptr, ok := s.ContextValue("pointer"); ok {
			replacements[ptr.(string)] = s.Target()
		}
	}
	if len(replacements) == 0 {
		return originalData, nil
	}

	out, err := jsonedit.ReplaceStrings(originalData, replacements)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidJSON, err)
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

	if looksLikePath(str) {
		return false
	}

	// Códigos de formato não são texto: "\C[2]" sozinho não tem o que traduzir.
	clean := strings.TrimSpace(escapeCodeRE.ReplaceAllString(str, ""))
	if clean == "" {
		return false
	}

	lowerStr := strings.ToLower(clean)
	for _, ext := range technicalExtensions {
		if strings.HasSuffix(lowerStr, ext) {
			return false
		}
	}
	if looksLikePath(clean) {
		return false
	}

	// Expressões JS ($gameVariables.value(1)) e URLs; "$100 for this sword!" é prosa.
	if jsIdentifierRE.MatchString(str) || strings.HasPrefix(str, "http://") || strings.HasPrefix(str, "https://") {
		return false
	}

	// Se for puramente números ou códigos de controle
	if isNumeric(str) {
		return false
	}

	return true
}

var (
	escapeCodeRE      = regexp.MustCompile(`\\[A-Za-z]{1,4}\[[^\]]*\]|\\[nrtfvbae0\\]`)
	cjkRE             = regexp.MustCompile(`[\x{3040}-\x{30ff}\x{3400}-\x{4dbf}\x{4e00}-\x{9fff}\x{f900}-\x{faff}]`)
	uiSlashPairRE     = regexp.MustCompile(`^\pL+(?:\s*/\s*\pL+)+$`)
	knownPathPrefixRE = regexp.MustCompile(`(?i)^(?:audio|img|image|images|pictures|graphic|graphics|sound|bgm|bgs|se|me|data|movies|system|fonts)[/\\]`)
	jsIdentifierRE    = regexp.MustCompile(`^\$[A-Za-z_]`)
)

// looksLikePath reconhece caminhos de asset ("audio/bgm/theme", `C:\Users\hero`)
// sem confundir pares de opções de interface ("Yes/No", "はい/いいえ") ou prosa com barra.
func looksLikePath(s string) bool {
	if !strings.ContainsAny(s, `/\`) {
		return false
	}
	if knownPathPrefixRE.MatchString(s) {
		return true
	}
	if strings.Contains(s, " ") || cjkRE.MatchString(s) {
		return false
	}
	return strings.Count(s, "/")+strings.Count(s, `\`) >= 2 || !uiSlashPairRE.MatchString(s)
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
