package json

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/BGLuis/slime-rpg/pkg/extractor/jsonedit"
	"github.com/BGLuis/slime-rpg/pkg/extractor/rpgmaker/rpgtext"
	"github.com/BGLuis/slime-rpg/pkg/model"
)

var (
	rxPluginsFile = regexp.MustCompile(`(?i)^plugins\.js$`)

	// Chaves de parâmetros técnicos que nunca contêm texto visível (as mesmas do Python).
	ignoreParamKeys = map[string]struct{}{
		"script": {}, "filterscript": {}, "datascript": {}, "mappingscript": {},
		"itemdrawscript": {}, "isenablescript": {}, "decisionscript": {},
		"filename": {}, "image": {}, "imagename": {}, "bgm": {}, "se": {},
		"variableid": {}, "switchid": {}, "defaultvalue": {}, "hiddenflag": {},
		"addposition": {}, "paddingtop": {},
	}

	cjkRE = regexp.MustCompile(`[\x{3040}-\x{30ff}\x{3400}-\x{4dbf}\x{4e00}-\x{9fff}\x{f900}-\x{faff}\x{ac00}-\x{d7af}]`)

	errNotPluginsFile = errors.New("plugins.js: $plugins array not found")
)

// pluginsArray localiza o array JSON de "var $plugins = [...];".
func pluginsArray(data []byte) (start, end int, err error) {
	content := string(data)
	start = strings.Index(content, "[")
	end = strings.LastIndex(content, "]")
	if start == -1 || end < start || !strings.Contains(strings.ToLower(content[:start]), "plugins") {
		return 0, 0, errNotPluginsFile
	}
	return start, end + 1, nil
}

// extractPluginsJs extrai os parâmetros de texto dos plugins ativos. Cada segmento
// guarda em "pointers" o caminho até a string: um JSON Pointer no array de plugins
// e, para parâmetros struct do MZ (JSON serializado dentro de uma string), um
// ponteiro adicional por nível de aninhamento.
func (e *MVExtractor) extractPluginsJs(base string, data []byte) ([]*model.Segment, error) {
	start, end, err := pluginsArray(data)
	if err != nil {
		return nil, err
	}
	var plugins []any
	if err := json.Unmarshal(data[start:end], &plugins); err != nil {
		return nil, fmt.Errorf("failed to parse plugins.js array: %w", err)
	}

	var segments []*model.Segment
	for idx, raw := range plugins {
		plugin, ok := raw.(map[string]any)
		if !ok || plugin["status"] != true {
			continue
		}
		params, ok := plugin["parameters"].(map[string]any)
		if !ok {
			continue
		}
		name, _ := plugin["name"].(string)
		if name == "" {
			name = strconv.Itoa(idx)
		}
		w := pluginWalker{base: base, plugin: name, index: idx}
		for _, key := range sortedKeys(params) {
			if ignoredParam(key) {
				continue
			}
			w.walk(nil, fmt.Sprintf("/%d/parameters/%s", idx, jsonedit.EscapePointer(key)), key, key, params[key])
		}
		segments = append(segments, w.segments...)
	}
	return segments, nil
}

type pluginWalker struct {
	base     string
	plugin   string
	index    int
	segments []*model.Segment
}

func (w *pluginWalker) walk(chain []string, ptr, key, path string, val any) {
	switch v := val.(type) {
	case string:
		if sub, ok := nestedJSON(v); ok {
			w.walk(appendChain(chain, ptr), "", key, path, sub)
			return
		}
		if !isTranslatablePluginText(v, key) {
			return
		}
		pointers := appendChain(chain, ptr)
		id := fmt.Sprintf("%s#plugin:%d:%s/param:%s", w.base, w.index, w.plugin, path)
		seg, err := model.NewSegment(id, v, model.KindMenu, map[string]any{
			"plugin":   w.plugin,
			"param":    path,
			"pointers": pointers,
		}, nil)
		if err == nil {
			w.segments = append(w.segments, seg)
		}
	case map[string]any:
		for _, k := range sortedKeys(v) {
			if ignoredParam(k) {
				continue
			}
			w.walk(chain, ptr+"/"+jsonedit.EscapePointer(k), k, path+"/"+k, v[k])
		}
	case []any:
		for i, item := range v {
			w.walk(chain, fmt.Sprintf("%s/%d", ptr, i), key, fmt.Sprintf("%s/%d", path, i), item)
		}
	}
}

func appendChain(chain []string, ptr string) []string {
	out := make([]string, len(chain), len(chain)+1)
	copy(out, chain)
	return append(out, ptr)
}

// nestedJSON reconhece parâmetros struct/list do MZ, serializados como string JSON.
func nestedJSON(s string) (any, bool) {
	t := strings.TrimSpace(s)
	if !(strings.HasPrefix(t, "{") && strings.HasSuffix(t, "}")) && !(strings.HasPrefix(t, "[") && strings.HasSuffix(t, "]")) {
		return nil, false
	}
	var sub any
	if json.Unmarshal([]byte(t), &sub) != nil {
		return nil, false
	}
	switch sub.(type) {
	case map[string]any, []any:
		return sub, true
	}
	return nil, false
}

type pluginEdit struct {
	pointers []string
	target   string
}

// injectPluginsJs reescreve só as strings traduzidas, preservando o cabeçalho,
// a formatação do array e o JSON aninhado que não foi alterado.
func (e *MVExtractor) injectPluginsJs(base string, originalData []byte, segments []*model.Segment) ([]byte, error) {
	start, end, err := pluginsArray(originalData)
	if err != nil {
		return nil, err
	}

	var edits []pluginEdit
	for _, s := range segments {
		if !s.IsResolved() || s.Target() == s.Source() || !strings.HasPrefix(s.ID(), base+"#") {
			continue
		}
		raw, ok := s.ContextValue("pointers")
		if !ok {
			continue
		}
		if pointers, ok := raw.([]string); ok && len(pointers) > 0 {
			edits = append(edits, pluginEdit{pointers: pointers, target: s.Target()})
		}
	}
	if len(edits) == 0 {
		return originalData, nil
	}

	array, err := applyPluginEdits(originalData[start:end], edits)
	if err != nil {
		return nil, fmt.Errorf("failed to inject plugins.js: %w", err)
	}
	out := make([]byte, 0, len(originalData)+len(array)-(end-start))
	out = append(out, originalData[:start]...)
	out = append(out, array...)
	return append(out, originalData[end:]...), nil
}

// applyPluginEdits troca as strings de doc; edições em JSON aninhado são aplicadas
// primeiro no documento interno, que então substitui a string que o contém.
func applyPluginEdits(doc []byte, edits []pluginEdit) ([]byte, error) {
	direct := make(map[string]string)
	nested := make(map[string][]pluginEdit)
	for _, ed := range edits {
		if len(ed.pointers) == 1 {
			direct[ed.pointers[0]] = ed.target
		} else {
			nested[ed.pointers[0]] = append(nested[ed.pointers[0]], pluginEdit{pointers: ed.pointers[1:], target: ed.target})
		}
	}

	if len(nested) > 0 {
		values, err := jsonedit.Strings(doc)
		if err != nil {
			return nil, err
		}
		byPointer := make(map[string]string, len(values))
		for _, v := range values {
			byPointer[v.Pointer] = v.Value
		}
		for ptr, sub := range nested {
			inner, ok := byPointer[ptr]
			if !ok {
				continue
			}
			updated, err := applyPluginEdits([]byte(inner), sub)
			if err != nil {
				return nil, err
			}
			direct[ptr] = string(updated)
		}
	}
	return jsonedit.ReplaceStrings(doc, direct)
}

func ignoredParam(key string) bool {
	_, ignored := ignoreParamKeys[strings.ToLower(key)]
	return ignored
}

// isTranslatablePluginText segue PluginParametersProcessor.is_translatable do Python.
func isTranslatablePluginText(text, keyName string) bool {
	if strings.TrimSpace(text) == "" || ignoredParam(keyName) {
		return false
	}
	if rpgtext.IsTechnicalOrCode(text, rpgtext.Param) {
		return false
	}
	if cjkRE.MatchString(text) {
		return true
	}
	if strings.ContainsAny(text, "{};()$=<>") {
		return false
	}
	if strings.Contains(text, " ") {
		return true
	}
	runes := []rune(text)
	return len(runes) > 2 && unicode.IsUpper(runes[0]) && isAlpha(runes)
}

func isAlpha(runes []rune) bool {
	for _, r := range runes {
		if !unicode.IsLetter(r) {
			return false
		}
	}
	return true
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
