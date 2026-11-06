package json

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"github.com/BGLuis/slime-rpg/pkg/model"
)

var (
	rxPluginsFile = regexp.MustCompile(`(?i)^plugins\.js$`)

	// Chaves de parâmetros técnicos que nunca contêm texto visível
	ignoreParamKeys = map[string]struct{}{
		"script": {}, "filterscript": {}, "datascript": {}, "mappingscript": {},
		"itemdrawscript": {}, "isenablescript": {}, "decisionscript": {},
		"filename": {}, "image": {}, "imagename": {}, "bgm": {}, "se": {},
		"variableid": {}, "switchid": {}, "defaultvalue": {}, "hiddenflag": {},
		"addposition": {}, "paddingtop": {}, "struct": {},
	}

	// Extensões de arquivos de asset que nunca devem ser traduzidos
	assetExtensions = []string{".png", ".jpg", ".jpeg", ".webp", ".ogg", ".wav", ".mp3", ".m4a"}
)

// PluginEntry define a estrutura de um plugin no array $plugins.
type PluginEntry struct {
	Name        string         `json:"name"`
	Status      bool           `json:"status"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

func (e *MVExtractor) extractPluginsJs(base string, data []byte) ([]*model.Segment, error) {
	content := string(data)
	startIdx := strings.Index(content, "[")
	endIdx := strings.LastIndex(content, "]")
	if startIdx == -1 || endIdx == -1 || endIdx < startIdx {
		return nil, fmt.Errorf("invalid plugins.js structure: array brackets not found")
	}

	jsonArrayBytes := []byte(content[startIdx : endIdx+1])
	var plugins []PluginEntry
	if err := json.Unmarshal(jsonArrayBytes, &plugins); err != nil {
		return nil, fmt.Errorf("failed to parse plugins.js array: %w", err)
	}

	var segments []*model.Segment

	for _, p := range plugins {
		if !p.Status || p.Parameters == nil {
			continue
		}
		pName := p.Name
		if pName == "" {
			pName = "unnamed"
		}

		for k, v := range p.Parameters {
			if _, ignored := ignoreParamKeys[strings.ToLower(k)]; ignored {
				continue
			}
			segs := e.extractPluginParam(base, pName, k, v)
			segments = append(segments, segs...)
		}
	}

	return segments, nil
}

func (e *MVExtractor) extractPluginParam(base, pluginName, path string, val any) []*model.Segment {
	var segs []*model.Segment

	switch v := val.(type) {
	case string:
		trimmed := strings.TrimSpace(v)
		if (strings.HasPrefix(trimmed, "{") && strings.HasSuffix(trimmed, "}")) ||
			(strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]")) {
			// Sub-JSON serializado como string
			var sub any
			if err := json.Unmarshal([]byte(trimmed), &sub); err == nil {
				return e.extractPluginParam(base, pluginName, path, sub)
			}
		}

		if isTranslatablePluginText(trimmed, path) {
			id := fmt.Sprintf("%s#plugin:%s/param:%s", base, pluginName, path)
			seg, err := model.NewSegment(id, trimmed, model.KindMenu, map[string]any{
				"plugin": pluginName,
				"param":  path,
			}, nil)
			if err == nil {
				segs = append(segs, seg)
			}
		}

	case map[string]any:
		for k, subVal := range v {
			if _, ignored := ignoreParamKeys[strings.ToLower(k)]; ignored {
				continue
			}
			subSegs := e.extractPluginParam(base, pluginName, fmt.Sprintf("%s/%s", path, k), subVal)
			segs = append(segs, subSegs...)
		}

	case []any:
		for idx, item := range v {
			subSegs := e.extractPluginParam(base, pluginName, fmt.Sprintf("%s/%d", path, idx), item)
			segs = append(segs, subSegs...)
		}
	}

	return segs
}

func (e *MVExtractor) injectPluginsJs(base string, originalData []byte, segments []*model.Segment) ([]byte, error) {
	content := string(originalData)
	startIdx := strings.Index(content, "[")
	endIdx := strings.LastIndex(content, "]")
	if startIdx == -1 || endIdx == -1 || endIdx < startIdx {
		return originalData, fmt.Errorf("invalid plugins.js structure")
	}

	header := content[:startIdx]
	footer := content[endIdx+1:]
	jsonArrayBytes := []byte(content[startIdx : endIdx+1])

	var plugins []PluginEntry
	if err := json.Unmarshal(jsonArrayBytes, &plugins); err != nil {
		return originalData, fmt.Errorf("failed to unmarshal plugins: %w", err)
	}

	trMap := make(map[string]string)
	for _, s := range segments {
		if s.IsResolved() {
			trMap[s.ID()] = s.Target()
		}
	}

	for pIdx, p := range plugins {
		if !p.Status || p.Parameters == nil {
			continue
		}
		pName := p.Name
		if pName == "" {
			pName = "unnamed"
		}

		for k, v := range p.Parameters {
			updated := e.injectPluginParam(base, pName, k, v, trMap)
			plugins[pIdx].Parameters[k] = updated
		}
	}

	newJSONBytes, err := json.MarshalIndent(plugins, "", "  ")
	if err != nil {
		return originalData, fmt.Errorf("failed to marshal translated plugins: %w", err)
	}

	result := fmt.Sprintf("%s%s%s", header, string(newJSONBytes), footer)
	return []byte(result), nil
}

func (e *MVExtractor) injectPluginParam(base, pluginName, path string, val any, trMap map[string]string) any {
	switch v := val.(type) {
	case string:
		trimmed := strings.TrimSpace(v)
		if (strings.HasPrefix(trimmed, "{") && strings.HasSuffix(trimmed, "}")) ||
			(strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]")) {
			var sub any
			if err := json.Unmarshal([]byte(trimmed), &sub); err == nil {
				updatedSub := e.injectPluginParam(base, pluginName, path, sub, trMap)
				packed, err := json.Marshal(updatedSub)
				if err == nil {
					return string(packed)
				}
			}
		}

		id := fmt.Sprintf("%s#plugin:%s/param:%s", base, pluginName, path)
		if tr, ok := trMap[id]; ok && tr != "" {
			return tr
		}
		return v

	case map[string]any:
		res := make(map[string]any)
		for k, subVal := range v {
			res[k] = e.injectPluginParam(base, pluginName, fmt.Sprintf("%s/%s", path, k), subVal, trMap)
		}
		return res

	case []any:
		res := make([]any, len(v))
		for idx, item := range v {
			res[idx] = e.injectPluginParam(base, pluginName, fmt.Sprintf("%s/%d", path, idx), item, trMap)
		}
		return res

	default:
		return val
	}
}

func isTranslatablePluginText(text, keyName string) bool {
	if text == "" {
		return false
	}

	lower := strings.ToLower(text)
	for _, ext := range assetExtensions {
		if strings.HasSuffix(lower, ext) {
			return false
		}
	}

	// Verifica se é puro código, booleano ou numérico
	if lower == "true" || lower == "false" || lower == "null" {
		return false
	}
	if strings.ContainsAny(text, "{};()$=<>") {
		return false
	}

	// Contém caracteres CJK
	for _, r := range text {
		if unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hiragana, r) || unicode.Is(unicode.Katakana, r) {
			return true
		}
	}

	// Se for texto ocidental: requer espaço ou múltiplas palavras
	if strings.Contains(text, " ") {
		return true
	}

	// Palavra capitalizada de tamanho razoável (>3)
	if len(text) > 3 && unicode.IsUpper(rune(text[0])) && !strings.ContainsAny(text, "_-") {
		return true
	}

	return false
}
