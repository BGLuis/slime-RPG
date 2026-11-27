package validator

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/BGLuis/slime-rpg/pkg/detector"
	"github.com/BGLuis/slime-rpg/pkg/extractor/jsonedit"
	"github.com/BGLuis/slime-rpg/pkg/metadata"
)

var (
	controlCodeRE    = regexp.MustCompile(`\\(?:[A-Za-z]+\s*\[[^\]]*\]|(?i:LANGF?END))`)
	xtokRE           = regexp.MustCompile(`__XTOK_[A-Za-z0-9_]+__`)
	rxMapJSON        = regexp.MustCompile(`(?i)^Map\d+\.json$`)
	rxCommonJSON     = regexp.MustCompile(`(?i)^CommonEvents\.json$`)
	rxTroopsJSON     = regexp.MustCompile(`(?i)^Troops\.json$`)
	rxFormulaDBJSON  = regexp.MustCompile(`(?i)^(Skills|Items)\.json$`)
	nonAssetKeywords = map[string]bool{
		"true": true, "false": true, "null": true, "none": true, "auto": true, "default": true,
	}
)

// ValidateStatic executa apenas a validação estática (Camada B) sobre gameDir.
func ValidateStatic(ctx context.Context, gameDir, originalDir string) (*Report, error) {
	rep, _, err := validateStaticInternal(ctx, gameDir, originalDir)
	return rep, err
}

func validateStaticInternal(ctx context.Context, gameDir, originalDir string) (*Report, string, error) {
	det, err := detector.Detect(gameDir)
	if err != nil {
		return nil, "", err
	}

	resolvedOrig := resolveOriginalDir(gameDir, originalDir)
	report := &Report{
		GamePath:      gameDir,
		OriginalDir:   resolvedOrig,
		Engine:        string(det.Engine),
		StaticChecked: true,
	}

	webRoot := findWebRoot(gameDir)
	if webRoot == "" {
		validateGenericJSONDir(ctx, det.DataDir, report)
		return report, "", nil
	}

	origWebRoot := ""
	if resolvedOrig != "" {
		origWebRoot = findWebRoot(resolvedOrig)
	}

	assets := NewAssetIndex(webRoot)
	validateRPGDataDir(ctx, webRoot, origWebRoot, report)
	validateRPGCSVFiles(ctx, webRoot, report)
	validatePluginsJS(webRoot, origWebRoot, assets, report)
	return report, webRoot, nil
}

func resolveOriginalDir(gameDir, explicit string) string {
	if explicit != "" {
		if info, err := os.Stat(explicit); err == nil && info.IsDir() {
			return explicit
		}
		return ""
	}
	if meta := metadata.Load(gameDir); meta != nil {
		if resolved := metadata.ResolveOriginalDir(gameDir, meta); resolved != "" {
			if info, err := os.Stat(resolved); err == nil && info.IsDir() {
				return resolved
			}
		}
	}
	return ""
}

func findWebRoot(dir string) string {
	for _, cand := range []string{dir, filepath.Join(dir, "www")} {
		dataDir := filepath.Join(cand, "data")
		jsDir := filepath.Join(cand, "js")
		if isDirectory(dataDir) && isDirectory(jsDir) {
			return cand
		}
	}
	return ""
}

func isDirectory(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}

func validateGenericJSONDir(ctx context.Context, dir string, report *Report) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if ctx.Err() != nil {
			return
		}
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".json") {
			continue
		}
		full := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(full)
		if err != nil {
			continue
		}
		var v any
		if err := json.Unmarshal(jsonedit.StripBOM(data), &v); err != nil {
			report.Add(Issue{
				Severity: SeverityError,
				Category: CategoryJSON,
				File:     e.Name(),
				Message:  fmt.Sprintf("JSON inválido: %v", err),
			})
		}
		if xtokRE.Match(data) {
			report.Add(Issue{
				Severity: SeverityError,
				Category: CategoryPlaceholder,
				File:     e.Name(),
				Message:  "placeholder __XTOK_ residual encontrado no arquivo",
			})
		}
	}
}

func validateRPGDataDir(ctx context.Context, webRoot, origWebRoot string, report *Report) {
	dataDir := filepath.Join(webRoot, "data")
	entries, err := os.ReadDir(dataDir)
	if err != nil {
		report.Add(Issue{
			Severity: SeverityError,
			Category: CategoryJSON,
			File:     "data",
			Message:  fmt.Sprintf("não foi possível ler a pasta data/: %v", err),
		})
		return
	}

	for _, entry := range entries {
		if ctx.Err() != nil {
			return
		}
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".json") {
			continue
		}
		rel := "data/" + entry.Name()
		raw, err := os.ReadFile(filepath.Join(dataDir, entry.Name()))
		if err != nil {
			report.Add(Issue{Severity: SeverityError, Category: CategoryJSON, File: rel, Message: err.Error()})
			continue
		}

		var parsed any
		if err := json.Unmarshal(jsonedit.StripBOM(raw), &parsed); err != nil {
			report.Add(Issue{
				Severity: SeverityError,
				Category: CategoryJSON,
				File:     rel,
				Message:  fmt.Sprintf("JSON inválido: %v", err),
			})
			continue
		}

		if xtokRE.Match(raw) {
			report.Add(Issue{
				Severity: SeverityError,
				Category: CategoryPlaceholder,
				File:     rel,
				Message:  "placeholder __XTOK_ não restaurado encontrado",
			})
		}

		var origParsed any
		if origWebRoot != "" {
			if origRaw, err := os.ReadFile(filepath.Join(origWebRoot, "data", entry.Name())); err == nil {
				_ = json.Unmarshal(jsonedit.StripBOM(origRaw), &origParsed)
			}
		}

		validateDataScriptsAndRefs(rel, entry.Name(), parsed, origParsed, report)
		if origParsed != nil {
			compareDataControlCodes(rel, parsed, origParsed, "", report)
		}
	}
}

func validateDataScriptsAndRefs(rel, base string, parsed, origParsed any, report *Report) {
	switch {
	case rxMapJSON.MatchString(base):
		m, _ := parsed.(map[string]any)
		origM, _ := origParsed.(map[string]any)
		if m == nil {
			return
		}
		events := indexEntries(m["events"])
		origEvents := map[string]any(nil)
		if origM != nil {
			origEvents = indexEntries(origM["events"])
		}
		for _, k := range sortedMapKeys(events) {
			ev, _ := events[k].(map[string]any)
			if ev == nil {
				continue
			}
			var origEv map[string]any
			if origEvents != nil {
				origEv, _ = origEvents[k].(map[string]any)
			}
			validatePagesScripts(rel, "event:"+k, ev["pages"], getPages(origEv), report)
		}
	case rxCommonJSON.MatchString(base):
		items := indexEntries(parsed)
		origItems := indexEntries(origParsed)
		for _, k := range sortedMapKeys(items) {
			ce, _ := items[k].(map[string]any)
			if ce == nil {
				continue
			}
			list, _ := ce["list"].([]any)
			var origList []any
			if origItems != nil {
				if oce, ok := origItems[k].(map[string]any); ok {
					origList, _ = oce["list"].([]any)
				}
			}
			validateCommandListScripts(rel, "commonevent:"+k, list, origList, report)
		}
	case rxTroopsJSON.MatchString(base):
		items := indexEntries(parsed)
		origItems := indexEntries(origParsed)
		for _, k := range sortedMapKeys(items) {
			tr, _ := items[k].(map[string]any)
			if tr == nil {
				continue
			}
			var origTr map[string]any
			if origItems != nil {
				origTr, _ = origItems[k].(map[string]any)
			}
			validatePagesScripts(rel, "troop:"+k, tr["pages"], getPages(origTr), report)
		}
	case rxFormulaDBJSON.MatchString(base):
		items := indexEntries(parsed)
		origItems := indexEntries(origParsed)
		for _, k := range sortedMapKeys(items) {
			it, _ := items[k].(map[string]any)
			if it == nil {
				continue
			}
			dmg, _ := it["damage"].(map[string]any)
			if dmg == nil {
				continue
			}
			formula, _ := dmg["formula"].(string)
			if strings.TrimSpace(formula) == "" {
				continue
			}
			var origFormula string
			if origItems != nil {
				if oit, ok := origItems[k].(map[string]any); ok {
					if odmg, ok := oit["damage"].(map[string]any); ok {
						origFormula, _ = odmg["formula"].(string)
					}
				}
			}
			if origFormula != "" && formula != origFormula && ValidateJS(origFormula) == nil {
				if msg := CheckTranslatedJS(origFormula, formula); msg != "" {
					report.Add(Issue{
						Severity:   SeverityError,
						Category:   CategoryScript,
						File:       rel,
						Location:   "item:" + k + "/damage.formula",
						Source:     origFormula,
						Translated: formula,
						Message:    msg,
					})
				}
			}
		}
	}
}

func getPages(m map[string]any) any {
	if m == nil {
		return nil
	}
	return m["pages"]
}

func validatePagesScripts(rel, prefix string, pagesRaw, origPagesRaw any, report *Report) {
	pages, _ := pagesRaw.([]any)
	origPages, _ := origPagesRaw.([]any)
	for i, pRaw := range pages {
		p, _ := pRaw.(map[string]any)
		if p == nil {
			continue
		}
		list, _ := p["list"].([]any)
		var origList []any
		if i < len(origPages) {
			if op, ok := origPages[i].(map[string]any); ok {
				origList, _ = op["list"].([]any)
			}
		}
		validateCommandListScripts(rel, fmt.Sprintf("%s/page:%d", prefix, i), list, origList, report)
	}
}

func validateCommandListScripts(rel, prefix string, list, origList []any, report *Report) {
	blocks := collectScriptBlocks(list)
	origBlocks := collectScriptBlocks(origList)
	for i, blk := range blocks {
		if strings.TrimSpace(blk.code) == "" {
			continue
		}
		var origCode string
		if i < len(origBlocks) && origBlocks[i].loc == blk.loc {
			origCode = origBlocks[i].code
		}
		if origCode != "" && origCode == blk.code {
			continue
		}
		if origCode != "" && ValidateJS(origCode) == nil {
			if msg := CheckTranslatedJS(origCode, blk.code); msg != "" {
				report.Add(Issue{
					Severity:   SeverityError,
					Category:   CategoryScript,
					File:       rel,
					Location:   prefix + "/" + blk.loc,
					Source:     origCode,
					Translated: blk.code,
					Message:    msg,
				})
			}
		}
	}
}

type scriptBlock struct {
	loc  string
	code string
}

func collectScriptBlocks(list []any) []scriptBlock {
	var blocks []scriptBlock
	for i := 0; i < len(list); i++ {
		cmd, _ := list[i].(map[string]any)
		if cmd == nil {
			continue
		}
		codeFloat, ok := cmd["code"].(float64)
		if !ok {
			continue
		}
		code := int(codeFloat)
		params, _ := cmd["parameters"].([]any)
		if code == 355 && len(params) > 0 {
			line, _ := params[0].(string)
			var lines = []string{line}
			start := i
			for i+1 < len(list) {
				next, _ := list[i+1].(map[string]any)
				if next == nil || next["code"] != float64(655) {
					break
				}
				np, _ := next["parameters"].([]any)
				if len(np) > 0 {
					if nl, ok := np[0].(string); ok {
						lines = append(lines, nl)
					}
				}
				i++
			}
			blocks = append(blocks, scriptBlock{
				loc:  fmt.Sprintf("cmd:%d", start),
				code: strings.Join(lines, "\n"),
			})
		}
	}
	return blocks
}

func compareDataControlCodes(rel string, cur, orig any, ptr string, report *Report) {
	switch c := cur.(type) {
	case string:
		o, ok := orig.(string)
		if !ok || c == o {
			return
		}
		origCodes := normalizeCodes(controlCodeRE.FindAllString(o, -1))
		if len(origCodes) == 0 {
			return
		}
		curCodes := normalizeCodes(controlCodeRE.FindAllString(c, -1))
		for code, count := range origCodes {
			if curCodes[code] < count {
				report.Add(Issue{
					Severity:   SeverityWarning,
					Category:   CategoryControlCode,
					File:       rel,
					Location:   ptr,
					Source:     o,
					Translated: c,
					Message:    fmt.Sprintf("código de controle %s ausente ou alterado após tradução", code),
				})
				break
			}
		}
	case []any:
		o, ok := orig.([]any)
		if !ok {
			return
		}
		for i := 0; i < len(c) && i < len(o); i++ {
			compareDataControlCodes(rel, c[i], o[i], ptr+"/"+strconv.Itoa(i), report)
		}
	case map[string]any:
		o, ok := orig.(map[string]any)
		if !ok {
			return
		}
		for _, k := range sortedMapKeys(c) {
			if ov, exists := o[k]; exists {
				compareDataControlCodes(rel, c[k], ov, ptr+"/"+k, report)
			}
		}
	}
}

func normalizeCodes(list []string) map[string]int {
	m := make(map[string]int, len(list))
	for _, c := range list {
		norm := strings.ToUpper(strings.ReplaceAll(c, " ", ""))
		m[norm]++
	}
	return m
}

func validatePluginsJS(webRoot, origWebRoot string, assets *AssetIndex, report *Report) {
	rel := "js/plugins.js"
	curPath := filepath.Join(webRoot, "js", "plugins.js")
	curRaw, err := os.ReadFile(curPath)
	if err != nil {
		return
	}
	curPlugins, err := parsePluginsArray(curRaw)
	if err != nil {
		report.Add(Issue{
			Severity: SeverityError,
			Category: CategoryJSON,
			File:     rel,
			Message:  fmt.Sprintf("plugins.js inválido: %v", err),
		})
		return
	}

	var origByName map[string]map[string]any
	if origWebRoot != "" {
		if origRaw, err := os.ReadFile(filepath.Join(origWebRoot, "js", "plugins.js")); err == nil {
			if origPlugins, err := parsePluginsArray(origRaw); err == nil {
				origByName = make(map[string]map[string]any, len(origPlugins))
				for _, p := range origPlugins {
					if name, _ := p["name"].(string); name != "" {
						origByName[name] = p
					}
				}
			}
		}
	}

	for idx, plugin := range curPlugins {
		if plugin["status"] != true {
			continue
		}
		name, _ := plugin["name"].(string)
		if name == "" {
			name = strconv.Itoa(idx)
		}
		params, _ := plugin["parameters"].(map[string]any)
		var origParams map[string]any
		if origByName != nil {
			if op, ok := origByName[name]; ok {
				origParams, _ = op["parameters"].(map[string]any)
			}
		}
		for _, k := range sortedMapKeys(params) {
			var origVal any
			if origParams != nil {
				origVal = origParams[k]
			}
			validatePluginValue(rel, name+"."+k, k, params[k], origVal, assets, report)
		}
	}
}

func validatePluginValue(rel, loc, key string, cur, orig any, assets *AssetIndex, report *Report) {
	switch c := cur.(type) {
	case string:
		if xtokRE.MatchString(c) {
			report.Add(Issue{
				Severity:   SeverityError,
				Category:   CategoryPlaceholder,
				File:       rel,
				Location:   loc,
				Translated: c,
				Message:    "placeholder __XTOK_ residual encontrado em parâmetro de plugin",
			})
		}

		origStr, hasOrig := orig.(string)
		if hasOrig {
			if origSub, ok := parseNestedJSON(origStr); ok {
				curSub, okCur := parseNestedJSON(c)
				if !okCur {
					report.Add(Issue{
						Severity:   SeverityError,
						Category:   CategoryJSON,
						File:       rel,
						Location:   loc,
						Source:     origStr,
						Translated: c,
						Message:    "JSON aninhado de parâmetro de plugin foi corrompido",
					})
					return
				}
				validatePluginValue(rel, loc, key, curSub, origSub, assets, report)
				return
			}
		} else if curSub, ok := parseNestedJSON(c); ok {
			validatePluginValue(rel, loc, key, curSub, nil, assets, report)
			return
		}

		// Se o valor mudou em relação ao original (ou mesmo sem original), checa scripts e assets.
		changed := hasOrig && c != origStr

		// 1. Validação de Script/Expressão JS
		if hasOrig {
			if changed && (IsScriptParamKey(key) || IsLikelyJS(origStr)) && ValidateJS(origStr) == nil {
				if msg := CheckTranslatedJS(origStr, c); msg != "" {
					report.Add(Issue{
						Severity:   SeverityError,
						Category:   CategoryScript,
						File:       rel,
						Location:   loc,
						Source:     origStr,
						Translated: c,
						Message:    msg,
					})
					return
				}
			}
		} else if IsScriptParamKey(key) && strings.TrimSpace(c) != "" {
			if err := ValidateJS(c); err != nil {
				report.Add(Issue{
					Severity:   SeverityError,
					Category:   CategoryScript,
					File:       rel,
					Location:   loc,
					Translated: c,
					Message:    fmt.Sprintf("sintaxe JavaScript inválida em parâmetro %s: %v", key, err),
				})
				return
			}
		}

		// 2. Validação de Asset (imagens/áudios/skins)
		if !assets.IsEmpty() {
			if hasOrig {
				if changed && (IsAssetParamKey(key) || assets.Exists(origStr)) && assets.Exists(origStr) && !assets.Exists(c) {
					report.Add(Issue{
						Severity:   SeverityError,
						Category:   CategoryAsset,
						File:       rel,
						Location:   loc,
						Source:     origStr,
						Translated: c,
						Message:    fmt.Sprintf("asset %q referenciado pelo parâmetro foi traduzido para %q e não existe no disco", origStr, c),
					})
					return
				}
			} else if IsAssetParamKey(key) && looksLikeAssetName(c) && !assets.Exists(c) {
				report.Add(Issue{
					Severity:   SeverityWarning,
					Category:   CategoryAsset,
					File:       rel,
					Location:   loc,
					Translated: c,
					Message:    fmt.Sprintf("arquivo de asset %q não encontrado em img/ ou audio/", c),
				})
				return
			}
		}

	case []any:
		origSlice, _ := orig.([]any)
		for i, item := range c {
			var origItem any
			if i < len(origSlice) {
				origItem = origSlice[i]
			}
			validatePluginValue(rel, fmt.Sprintf("%s[%d]", loc, i), key, item, origItem, assets, report)
		}
	case map[string]any:
		origMap, _ := orig.(map[string]any)
		for _, k := range sortedMapKeys(c) {
			var origItem any
			if origMap != nil {
				origItem = origMap[k]
			}
			validatePluginValue(rel, loc+"."+k, k, c[k], origItem, assets, report)
		}
	}
}

func looksLikeAssetName(s string) bool {
	t := strings.TrimSpace(s)
	if t == "" || nonAssetKeywords[strings.ToLower(t)] {
		return false
	}
	if _, err := strconv.ParseFloat(t, 64); err == nil {
		return false
	}
	if strings.HasPrefix(t, "{") || strings.HasPrefix(t, "[") {
		return false
	}
	return true
}

func parsePluginsArray(data []byte) ([]map[string]any, error) {
	s := string(jsonedit.StripBOM(data))
	start := strings.Index(s, "[")
	end := strings.LastIndex(s, "]")
	if start == -1 || end < start || !strings.Contains(strings.ToLower(s[:start]), "plugins") {
		return nil, fmt.Errorf("array $plugins não encontrado")
	}
	var out []map[string]any
	if err := json.Unmarshal([]byte(s[start:end+1]), &out); err != nil {
		return nil, err
	}
	return out, nil
}

func parseNestedJSON(s string) (any, bool) {
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

func indexEntries(container any) map[string]any {
	out := make(map[string]any)
	switch v := container.(type) {
	case []any:
		for i, item := range v {
			out[strconv.Itoa(i)] = item
		}
	case map[string]any:
		return v
	}
	return out
}

func sortedMapKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		ni, errI := strconv.Atoi(keys[i])
		nj, errJ := strconv.Atoi(keys[j])
		if errI == nil && errJ == nil {
			return ni < nj
		}
		return keys[i] < keys[j]
	})
	return keys
}

// validateRPGCSVFiles verifica se CSVs dentro de data/ (ex.: data/unique/*.csv)
// são compatíveis com o parser do ExternMessage.js (que não suporta aspas dobradas ""
// dentro de células e rejeita '[' ou números puros na coluna 0 de MessageID).
func validateRPGCSVFiles(ctx context.Context, webRoot string, report *Report) {
	dataDir := filepath.Join(webRoot, "data")
	_ = filepath.WalkDir(dataDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || ctx.Err() != nil {
			return nil
		}
		if !strings.EqualFold(filepath.Ext(d.Name()), ".csv") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(webRoot, path)
		rel = filepath.ToSlash(rel)

		text := strings.ReplaceAll(string(jsonedit.StripBOM(raw)), "\r\n", "\n")
		rows := parseExternMessageCSV(text)
		reported := false
		for i, row := range rows {
			if len(row) == 0 || row[0] == "" {
				continue
			}
			id := row[0]
			if strings.ContainsRune(id, '[') {
				report.Add(Issue{
					Severity: SeverityError,
					Category: CategoryCSV,
					File:     rel,
					Location: fmt.Sprintf("row:%d", i),
					Message:  fmt.Sprintf("MessageID inválido para ExternMessage (provável aspa dupla \"\" quebrando célula CSV): %q", truncateStr(id, 60)),
				})
				reported = true
				break
			}
		}
		if !reported && strings.Contains(text, `""`) {
			report.Add(Issue{
				Severity: SeverityError,
				Category: CategoryCSV,
				File:     rel,
				Message:  `aspas dobradas "" detectadas em CSV (incompatível com parser de ExternMessage.js)`,
			})
		}
		return nil
	})
}

// parseExternMessageCSV replica exatamente a lógica de CsvImportor.parseFromCSV do ExternMessage.js
// para detectar quebras antes de o jogo ser aberto.
func parseExternMessageCSV(text string) [][]string {
	var result [][]string
	var currentLine []string
	begin := 0
	pushToken := func(index int) {
		if begin >= len(text) {
			currentLine = append(currentLine, "")
			begin = index + 1
			return
		}
		var tok string
		if text[begin] == '"' && index-begin >= 2 {
			tok = text[begin+1 : index-1]
		} else {
			tok = text[begin:index]
		}
		currentLine = append(currentLine, tok)
		begin = index + 1
	}
	for i := 0; i < len(text); i++ {
		switch text[i] {
		case ',':
			pushToken(i)
		case '"':
			j := i + 1
			for j < len(text) {
				idx := strings.IndexByte(text[j:], '"')
				if idx < 0 {
					j = len(text) - 1
					break
				}
				pos := j + idx
				if pos > 0 && text[pos-1] == '\\' {
					j = pos + 1
					continue
				}
				j = pos
				break
			}
			i = j
		case '\n':
			pushToken(i)
			result = append(result, currentLine)
			currentLine = nil
		}
	}
	if begin < len(text) || len(currentLine) > 0 {
		pushToken(len(text))
		result = append(result, currentLine)
	}
	return result
}

func truncateStr(s string, maxLen int) string {
	r := []rune(s)
	if len(r) <= maxLen {
		return s
	}
	return string(r[:maxLen]) + "…"
}
