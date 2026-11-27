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

	// Chaves de parâmetros técnicos que nunca contêm texto visível.
	ignoreParamKeys = map[string]struct{}{
		"script": {}, "filterscript": {}, "datascript": {}, "mappingscript": {},
		"itemdrawscript": {}, "isenablescript": {}, "decisionscript": {},
		"listscript": {}, "sortscript": {},
		"filename": {}, "image": {}, "imagename": {}, "bgm": {}, "basebgm": {}, "se": {},
		"bgs": {}, "me": {}, "skin": {}, "windowskin": {}, "displacementimage": {},
		"variableid": {}, "switchid": {}, "defaultvalue": {}, "hiddenflag": {},
		"addposition": {}, "paddingtop": {}, "hidebutton": {},
		"width": {}, "height": {}, "startwidth": {}, "startheight": {},
	}

	ignoreParamSuffixes = []string{
		"script", "formula", "eval",
		"skin", "image", "imagename", "picture", "picturename",
		"bgm", "bgs", "parallax", "battleback", "filename", "fontfilename",
		"width", "height", "button", "hotkey",
	}

	textParamKeyHints = []string{
		"name", "title", "label", "text", "message", "messages", "desc", "description",
		"help", "caption", "tooltip", "hint", "word", "command", "commands",
		"list", "items", "options", "choices",
	}

	krdTextParams = map[string]bool{
		"arglanguage":     true,
		"argoptiontext":   true,
		"arggametitle":    true,
		"argcurrencyunit": true,
		"argelements":     true,
		"argskilltypes":   true,
		"argequiptypes":   true,
		"argbasic":        true,
		"argparams":       true,
		"argcommands":     true,
		"argmessages":     true,
	}

	jsCommentOrCodeRE = regexp.MustCompile(`(?://|/\*|\bGraphics\.|\bSceneManager\.|\bDataManager\.|\bAudioManager\.|\bBattleManager\.|\$game[A-Z]|\bES\.[a-z])`)
	jsExprRE          = regexp.MustCompile(`^[A-Za-z_$][\w$.]*\s*[+\-*/%]\s*[A-Za-z0-9_$.]+(?:\s*[+\-*/%]\s*[A-Za-z0-9_$.]+)*$`)

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
		isKRD := strings.EqualFold(name, "KRD_MZ_Multilingual")
		for _, key := range sortedKeys(params) {
			if ignoredParam(key) {
				continue
			}
			forceText := isKRD && krdTextParams[strings.ToLower(key)]
			w.walk(nil, fmt.Sprintf("/%d/parameters/%s", idx, jsonedit.EscapePointer(key)), key, key, params[key], forceText)
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

func (w *pluginWalker) walk(chain []string, ptr, key, path string, val any, forceText bool) {
	switch v := val.(type) {
	case string:
		if sub, ok := nestedJSON(v); ok {
			w.walk(appendChain(chain, ptr), "", key, path, sub, forceText)
			return
		}
		if spans, ok := rpgtext.KRDLangSpans(v); ok {
			for i, sp := range spans {
				if !w.acceptPluginString(sp.Source, key, forceText) {
					continue
				}
				spanID := fmt.Sprintf("%s#plugin:%d:%s/param:%s", w.base, w.index, w.plugin, path)
				if len(spans) > 1 {
					spanID = fmt.Sprintf("%s/%d", spanID, i)
				}
				w.appendSegment(spanID, sp.Source, appendChain(chain, ptr), path, &sp)
			}
			return
		}
		if !w.acceptPluginString(v, key, forceText) {
			return
		}
		id := fmt.Sprintf("%s#plugin:%d:%s/param:%s", w.base, w.index, w.plugin, path)
		w.appendSegment(id, v, appendChain(chain, ptr), path, nil)
	case map[string]any:
		for _, k := range sortedKeys(v) {
			if ignoredParam(k) {
				continue
			}
			w.walk(chain, ptr+"/"+jsonedit.EscapePointer(k), k, path+"/"+k, v[k], forceText)
		}
	case []any:
		for i, item := range v {
			w.walk(chain, fmt.Sprintf("%s/%d", ptr, i), key, fmt.Sprintf("%s/%d", path, i), item, forceText)
		}
	}
}

func (w *pluginWalker) acceptPluginString(v, key string, forceText bool) bool {
	if forceText {
		return !rpgtext.IsTechnicalOrCode(v, rpgtext.Dialogue) && isTranslatableText(v)
	}
	return isTranslatablePluginText(v, key)
}

func (w *pluginWalker) appendSegment(id, source string, pointers []string, path string, sp *rpgtext.Span) {
	ctx := map[string]any{
		"plugin":   w.plugin,
		"param":    path,
		"pointers": pointers,
	}
	if sp != nil {
		ctx["span"] = *sp
	}
	seg, err := model.NewSegment(id, source, model.KindMenu, ctx, nil)
	if err == nil {
		w.segments = append(w.segments, seg)
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
	sp       *rpgtext.Span
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
			ed := pluginEdit{pointers: pointers, target: s.Target()}
			if spRaw, hasSp := s.ContextValue("span"); hasSp {
				if sp, ok := spRaw.(rpgtext.Span); ok {
					ed.sp = &sp
				}
			}
			edits = append(edits, ed)
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
	directSpans := make(map[string][]pluginEdit)
	nested := make(map[string][]pluginEdit)
	for _, ed := range edits {
		if len(ed.pointers) == 1 {
			if ed.sp != nil {
				directSpans[ed.pointers[0]] = append(directSpans[ed.pointers[0]], ed)
			} else {
				direct[ed.pointers[0]] = ed.target
			}
		} else {
			nested[ed.pointers[0]] = append(nested[ed.pointers[0]], pluginEdit{
				pointers: ed.pointers[1:],
				target:   ed.target,
				sp:       ed.sp,
			})
		}
	}

	if len(nested) > 0 || len(directSpans) > 0 {
		values, err := jsonedit.Strings(doc)
		if err != nil {
			return nil, err
		}
		byPointer := make(map[string]string, len(values))
		for _, v := range values {
			byPointer[v.Pointer] = v.Value
		}
		for ptr, list := range directSpans {
			value, ok := byPointer[ptr]
			if !ok {
				continue
			}
			sort.Slice(list, func(i, j int) bool { return list[i].sp.Start > list[j].sp.Start })
			for _, ed := range list {
				if ed.sp.End > len(value) || ed.sp.Start > ed.sp.End {
					continue
				}
				value = value[:ed.sp.Start] + ed.sp.Encode(ed.target) + value[ed.sp.End:]
			}
			direct[ptr] = value
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
	k := strings.ToLower(strings.TrimSpace(key))
	if _, ignored := ignoreParamKeys[k]; ignored {
		return true
	}
	for _, sfx := range ignoreParamSuffixes {
		if strings.HasSuffix(k, sfx) {
			return true
		}
	}
	return false
}

// isTranslatablePluginText filtra parâmetros de plugins para extrair apenas texto visível,
// evitando nomes de assets, expressões JavaScript e identificadores técnicos.
func isTranslatablePluginText(text, keyName string) bool {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" || ignoredParam(keyName) {
		return false
	}
	if rpgtext.IsTechnicalOrCode(text, rpgtext.Param) {
		return false
	}
	if strings.ContainsAny(text, "{};()$=<>") || jsCommentOrCodeRE.MatchString(text) || jsExprRE.MatchString(trimmed) {
		return false
	}
	if cjkRE.MatchString(text) {
		return true
	}
	if strings.Contains(text, " ") {
		return true
	}
	runes := []rune(text)
	if len(runes) <= 2 || !unicode.IsUpper(runes[0]) || !isAlpha(runes) || hasInternalUpper(runes) {
		return false
	}
	return hasNonASCII(runes) || isTextParamKey(keyName)
}

func hasNonASCII(runes []rune) bool {
	for _, r := range runes {
		if r > 127 {
			return true
		}
	}
	return false
}

func hasInternalUpper(runes []rune) bool {
	for _, r := range runes[1:] {
		if unicode.IsUpper(r) {
			return true
		}
	}
	return false
}

func isTextParamKey(key string) bool {
	k := strings.ToLower(strings.TrimSpace(key))
	for _, h := range textParamKeyHints {
		if k == h || strings.HasSuffix(k, h) {
			return true
		}
	}
	return false
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
