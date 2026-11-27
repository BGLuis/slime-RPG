package validator

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BGLuis/slime-rpg/pkg/extractor/jsonedit"
)

// HealOptions configura a auto-recuperação cirúrgica após a detecção de falhas.
type HealOptions struct {
	OriginalDir    string
	CandidateFiles []string
	RunRuntime     bool
	SceneTour      bool
	Timeout        Options
	Log            func(string)
}

// HealAction descreve uma reversão ou correção aplicada em um arquivo.
type HealAction struct {
	File        string `json:"file"`
	Description string `json:"description"`
	Reverted    int    `json:"reverted"`
}

// HealResult resume o processo de auto-recuperação.
type HealResult struct {
	Clean   bool         `json:"clean"`
	Actions []HealAction `json:"actions"`
	Report  *Report      `json:"report"`
}

// Heal tenta corrigir automaticamente erros detectados pela validação revertendo
// apenas as partes específicas que quebraram o jogo (usando OriginalDir ou .slime/meta.json).
func Heal(ctx context.Context, gameDir string, opts Options, candidateFiles []string, logFn func(string)) (*HealResult, error) {
	origDir := resolveOriginalDir(gameDir, opts.OriginalDir)
	opts.OriginalDir = origDir

	rep, err := Validate(ctx, gameDir, opts)
	if err != nil {
		return nil, err
	}
	res := &HealResult{Report: rep}
	if !rep.HasErrors() {
		res.Clean = true
		return res, nil
	}
	if origDir == "" {
		return res, nil
	}

	webRoot := findWebRoot(gameDir)
	origWebRoot := findWebRoot(origDir)
	if webRoot == "" || origWebRoot == "" {
		return res, nil
	}

	emit := func(msg string) {
		if logFn != nil {
			logFn(msg)
		}
	}

	// 1. Correção cirúrgica pelos problemas estáticos já localizados.
	actions := healStaticIssues(webRoot, origWebRoot, rep.Issues, emit)
	res.Actions = append(res.Actions, actions...)

	// Revalida após cura estática.
	rep, err = Validate(ctx, gameDir, opts)
	if err != nil {
		return res, err
	}
	res.Report = rep
	if !rep.HasErrors() {
		res.Clean = true
		return res, nil
	}

	// 2. Se ainda houver erro de runtime (sem localização estática), isola o arquivo
	// e os trechos exatos via busca binária contra o backup original.
	if opts.RunRuntime && rep.RuntimeRan {
		rtActions := healRuntimeByBisect(ctx, gameDir, webRoot, origWebRoot, opts, candidateFiles, emit)
		res.Actions = append(res.Actions, rtActions...)
		rep, err = Validate(ctx, gameDir, opts)
		if err != nil {
			return res, err
		}
		res.Report = rep
	}

	res.Clean = !res.Report.HasErrors()
	return res, nil
}

func healStaticIssues(webRoot, origWebRoot string, issues []Issue, emit func(string)) []HealAction {
	var actions []HealAction
	byFile := make(map[string][]Issue)
	for _, it := range issues {
		if it.Severity != SeverityError || it.File == "" {
			continue
		}
		byFile[it.File] = append(byFile[it.File], it)
	}

	for rel, fileIssues := range byFile {
		curPath := filepath.Join(webRoot, filepath.FromSlash(rel))
		origPath := filepath.Join(origWebRoot, filepath.FromSlash(rel))
		origData, err := os.ReadFile(origPath)
		if err != nil {
			continue
		}
		curData, err := os.ReadFile(curPath)
		if err != nil {
			continue
		}

		switch {
		case strings.EqualFold(filepath.Ext(rel), ".csv"):
			fixed, desc, count, ok := healCSVFile(curData, origData)
			if ok && !bytes.Equal(fixed, curData) {
				if os.WriteFile(curPath, fixed, 0o644) == nil {
					emit(fmt.Sprintf("Auto-correção em %s: %s", rel, desc))
					actions = append(actions, HealAction{File: rel, Description: desc, Reverted: count})
				}
			}

		case rel == "js/plugins.js":
			if hasCategory(fileIssues, CategoryJSON) {
				if os.WriteFile(curPath, origData, 0o644) == nil {
					emit("Auto-correção em js/plugins.js: restaurado original devido a JSON inválido")
					actions = append(actions, HealAction{File: rel, Description: "restaurado original (JSON inválido)", Reverted: 1})
				}
				continue
			}
			fixed, count := healPluginsJSByIssues(curData, origData, fileIssues)
			if count > 0 && !bytes.Equal(fixed, curData) {
				if os.WriteFile(curPath, fixed, 0o644) == nil {
					desc := fmt.Sprintf("%d parâmetro(s) revertido(s) ao original", count)
					emit("Auto-correção em js/plugins.js: " + desc)
					actions = append(actions, HealAction{File: rel, Description: desc, Reverted: count})
				}
			}

		case strings.HasPrefix(rel, "data/") && strings.EqualFold(filepath.Ext(rel), ".json"):
			if hasCategory(fileIssues, CategoryJSON) {
				if os.WriteFile(curPath, origData, 0o644) == nil {
					emit(fmt.Sprintf("Auto-correção em %s: restaurado original devido a JSON inválido", rel))
					actions = append(actions, HealAction{File: rel, Description: "restaurado original (JSON inválido)", Reverted: 1})
				}
				continue
			}
			fixed, count := healJSONByIssues(curData, origData, fileIssues)
			if count > 0 && !bytes.Equal(fixed, curData) {
				if os.WriteFile(curPath, fixed, 0o644) == nil {
					desc := fmt.Sprintf("%d trecho(s) revertido(s) ao original", count)
					emit(fmt.Sprintf("Auto-correção em %s: %s", rel, desc))
					actions = append(actions, HealAction{File: rel, Description: desc, Reverted: count})
				}
			}
		}
	}
	return actions
}

func hasCategory(issues []Issue, cat Category) bool {
	for _, it := range issues {
		if it.Category == cat {
			return true
		}
	}
	return false
}

// healCSVFile corrige aspas incompatíveis com ExternMessage.js ("") preservando
// as traduções com aspas tipográficas e restaurando apenas linhas que ainda quebrem.
func healCSVFile(curData, origData []byte) ([]byte, string, int, bool) {
	// Se o arquivo atual tem "" que não existia no original (gerado por csv.Writer),
	// tenta primeiro substituir "" dentro de blocos :script por " e fora por “/”.
	curStr := string(curData)
	origStr := string(origData)
	if strings.Contains(curStr, `""`) && !strings.Contains(origStr, `""`) {
		fixedStr, fixedCount := fixDoubledCSVQuotes(curStr)
		if csvPassesExternMessage([]byte(fixedStr)) && !strings.Contains(fixedStr, `""`) {
			return []byte(fixedStr), fmt.Sprintf("aspas dobradas corrigidas em %d ocorrência(s)", fixedCount), fixedCount, true
		}
	}

	curClean := jsonedit.StripBOM(curData)
	origClean := jsonedit.StripBOM(origData)
	hasBOM := len(curClean) != len(curData)

	curRecs, err1 := readAllCSV(curClean)
	origRecs, err2 := readAllCSV(origClean)
	if err1 != nil || err2 != nil {
		return origData, "CSV inválido restaurado do original", 1, true
	}

	sanitized := 0
	for r := range curRecs {
		for c := range curRecs[r] {
			if strings.ContainsRune(curRecs[r][c], '"') {
				origVal := ""
				if r < len(origRecs) && c < len(origRecs[r]) {
					origVal = origRecs[r][c]
				}
				if curRecs[r][c] != origVal && !strings.ContainsRune(origVal, '"') {
					curRecs[r][c] = replaceASCIIQuotes(curRecs[r][c])
					sanitized++
				}
			}
		}
	}

	out, err := writeAllCSV(curRecs, hasBOM, origData)
	if err == nil && csvPassesExternMessage(out) && !strings.Contains(string(out), `""`) {
		return out, fmt.Sprintf("aspas incompatíveis com ExternMessage corrigidas em %d célula(s)", sanitized), sanitized, true
	}

	return origData, "CSV restaurado do original por falha de compatibilidade", len(curRecs), true
}

func fixDoubledCSVQuotes(s string) (string, int) {
	lines := strings.Split(s, "\n")
	inScript := false
	count := 0
	for i, line := range lines {
		t := strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if strings.HasPrefix(strings.ToLower(t), ":script") {
			inScript = true
		} else if strings.EqualFold(t, ":end") {
			inScript = false
		}
		if !strings.Contains(line, `""`) {
			continue
		}
		if inScript {
			count += strings.Count(line, `""`)
			lines[i] = strings.ReplaceAll(line, `""`, `"`)
		} else {
			count += strings.Count(line, `""`)
			lines[i] = replaceDoubledQuotes(line)
		}
	}
	return strings.Join(lines, "\n"), count
}

func replaceDoubledQuotes(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	open := true
	for i := 0; i < len(s); i++ {
		if s[i] == '"' && i+1 < len(s) && s[i+1] == '"' {
			if open {
				b.WriteRune('“')
			} else {
				b.WriteRune('”')
			}
			open = !open
			i++
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func csvPassesExternMessage(raw []byte) bool {
	text := strings.ReplaceAll(string(jsonedit.StripBOM(raw)), "\r\n", "\n")
	for _, row := range parseExternMessageCSV(text) {
		if len(row) > 0 && row[0] != "" && strings.ContainsRune(row[0], '[') {
			return false
		}
	}
	return true
}

func readAllCSV(clean []byte) ([][]string, error) {
	r := csv.NewReader(bytes.NewReader(clean))
	r.Comma = ','
	r.LazyQuotes = true
	r.FieldsPerRecord = -1
	return r.ReadAll()
}

func writeAllCSV(records [][]string, hasBOM bool, origTemplate []byte) ([]byte, error) {
	var buf bytes.Buffer
	if hasBOM {
		buf.Write([]byte{0xEF, 0xBB, 0xBF})
	}
	crlf := bytes.Contains(origTemplate, []byte("\r\n"))
	w := csv.NewWriter(&buf)
	w.UseCRLF = crlf
	if err := w.WriteAll(records); err != nil {
		return nil, err
	}
	if len(origTemplate) > 0 {
		s := string(jsonedit.StripBOM(origTemplate))
		nl := "\n"
		if crlf {
			nl = "\r\n"
		}
		extra := 0
		for strings.HasSuffix(s, nl) {
			s = strings.TrimSuffix(s, nl)
			extra++
		}
		if extra > 1 {
			buf.WriteString(strings.Repeat(nl, extra-1))
		}
	}
	return buf.Bytes(), nil
}

func replaceASCIIQuotes(s string) string {
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

// healPluginsJSByIssues restaura no plugins.js apenas os valores que tiveram
// problemas reportados (comparando Source vs Translated).
func healPluginsJSByIssues(curData, origData []byte, issues []Issue) ([]byte, int) {
	badTargets := make(map[string]string)
	for _, it := range issues {
		if it.Source != "" && it.Translated != "" && it.Source != it.Translated {
			badTargets[it.Translated] = it.Source
		}
	}
	if len(badTargets) == 0 {
		return curData, 0
	}

	s := string(curData)
	start := strings.Index(s, "[")
	end := strings.LastIndex(s, "]")
	if start < 0 || end < start {
		return curData, 0
	}
	values, err := jsonedit.Strings(curData[start : end+1])
	if err != nil {
		return curData, 0
	}

	repl := make(map[string]string)
	reverted := 0
	for _, v := range values {
		if orig, ok := badTargets[v.Value]; ok {
			repl[v.Pointer] = orig
			reverted++
			continue
		}
		// Caso de JSON aninhado em string do MZ: substitui apenas as strings internas problemáticas.
		if sub, ok := parseNestedJSON(v.Value); ok {
			updated, n := revertNestedStrings(sub, badTargets)
			if n > 0 {
				if encoded, err := json.Marshal(updated); err == nil {
					repl[v.Pointer] = string(encoded)
					reverted += n
				}
			}
		}
	}
	if len(repl) == 0 {
		return curData, 0
	}
	arr, err := jsonedit.ReplaceStrings(curData[start:end+1], repl)
	if err != nil {
		return origData, 1
	}
	out := make([]byte, 0, len(curData))
	out = append(out, curData[:start]...)
	out = append(out, arr...)
	out = append(out, curData[end+1:]...)
	return out, reverted
}

func revertNestedStrings(v any, badTargets map[string]string) (any, int) {
	count := 0
	switch val := v.(type) {
	case string:
		if orig, ok := badTargets[val]; ok {
			return orig, 1
		}
		if sub, ok := parseNestedJSON(val); ok {
			updated, n := revertNestedStrings(sub, badTargets)
			if n > 0 {
				if encoded, err := json.Marshal(updated); err == nil {
					return string(encoded), n
				}
			}
		}
		return val, 0
	case []any:
		out := make([]any, len(val))
		for i, item := range val {
			u, n := revertNestedStrings(item, badTargets)
			out[i] = u
			count += n
		}
		return out, count
	case map[string]any:
		out := make(map[string]any, len(val))
		for k, item := range val {
			u, n := revertNestedStrings(item, badTargets)
			out[k] = u
			count += n
		}
		return out, count
	default:
		return val, 0
	}
}

func healJSONByIssues(curData, origData []byte, issues []Issue) ([]byte, int) {
	badTargets := make(map[string]string)
	for _, it := range issues {
		if it.Source != "" && it.Translated != "" && it.Source != it.Translated {
			badTargets[it.Translated] = it.Source
		}
	}
	if len(badTargets) == 0 {
		return curData, 0
	}
	values, err := jsonedit.Strings(curData)
	if err != nil {
		return origData, 1
	}
	repl := make(map[string]string)
	for _, v := range values {
		if orig, ok := badTargets[v.Value]; ok {
			repl[v.Pointer] = orig
		}
	}
	if len(repl) == 0 {
		return curData, 0
	}
	out, err := jsonedit.ReplaceStrings(curData, repl)
	if err != nil {
		return curData, 0
	}
	return out, len(repl)
}

// healRuntimeByBisect usa busca binária nos arquivos modificados (e dentro do arquivo
// culpado) para isolar erros que só aparecem durante o boot headless.
func healRuntimeByBisect(ctx context.Context, gameDir, webRoot, origWebRoot string, opts Options, candidateFiles []string, emit func(string)) []HealAction {
	candidates := collectChangedCandidates(webRoot, origWebRoot, candidateFiles)
	if len(candidates) == 0 {
		return nil
	}

	type pair struct {
		rel  string
		cur  []byte
		orig []byte
	}
	files := make([]pair, 0, len(candidates))
	for _, rel := range candidates {
		c, err1 := os.ReadFile(filepath.Join(webRoot, filepath.FromSlash(rel)))
		o, err2 := os.ReadFile(filepath.Join(origWebRoot, filepath.FromSlash(rel)))
		if err1 == nil && err2 == nil && !bytes.Equal(c, o) {
			files = append(files, pair{rel: rel, cur: c, orig: o})
		}
	}
	if len(files) == 0 {
		return nil
	}

	applyMask := func(useOrig []bool) {
		for i, f := range files {
			data := f.cur
			if useOrig[i] {
				data = f.orig
			}
			_ = os.WriteFile(filepath.Join(webRoot, filepath.FromSlash(f.rel)), data, 0o644)
		}
	}
	testClean := func(useOrig []bool) bool {
		applyMask(useOrig)
		rt, err := runRuntimeInternal(ctx, webRoot, opts)
		return err == nil && rt != nil && rt.Ran && len(rt.Issues) == 0
	}

	// Se nem tudo revertido passa no runtime (ex.: erro próprio do jogo sem NW.js),
	// restaura a tradução e não desfaz nada sem evidência.
	allOrig := make([]bool, len(files))
	for i := range allOrig {
		allOrig[i] = true
	}
	if !testClean(allOrig) {
		applyMask(make([]bool, len(files)))
		emit("Auto-correção runtime: erro também ocorre nos arquivos originais; traduções preservadas")
		return nil
	}

	// Isola o menor conjunto de arquivos culpados usando busca binária iterativa.
	culprits := make([]bool, len(files))
	for {
		if testClean(culprits) {
			break
		}
		var active []int
		for i, c := range culprits {
			if !c {
				active = append(active, i)
			}
		}
		if len(active) == 0 {
			break
		}
		lo, hi := 0, len(active)-1
		for lo < hi {
			mid := (lo + hi) / 2
			probe := append([]bool(nil), culprits...)
			for k := 0; k <= mid; k++ {
				probe[active[k]] = true
			}
			if testClean(probe) {
				hi = mid
			} else {
				lo = mid + 1
			}
		}
		culprits[active[lo]] = true
	}

	var actions []HealAction
	applyMask(culprits)
	for i, isCulprit := range culprits {
		if !isCulprit {
			continue
		}
		f := files[i]
		// Tenta cura granular dentro do arquivo culpado (CSV ou JSON strings) antes de deixar inteiro revertido.
		desc := "arquivo revertido ao original por erro em runtime"
		reverted := 1
		if strings.HasSuffix(strings.ToLower(f.rel), ".json") || f.rel == "js/plugins.js" {
			if n, ok := bisectJSONStrings(ctx, webRoot, f.rel, f.cur, f.orig, opts); ok {
				desc = fmt.Sprintf("%d string(s) isolada(s) e revertida(s) por busca binária", n)
				reverted = n
			}
		}
		emit(fmt.Sprintf("Auto-correção runtime em %s: %s", f.rel, desc))
		actions = append(actions, HealAction{File: f.rel, Description: desc, Reverted: reverted})
	}
	return actions
}

func bisectJSONStrings(ctx context.Context, webRoot, rel string, curData, origData []byte, opts Options) (int, bool) {
	if rel != "js/plugins.js" && !strings.HasSuffix(strings.ToLower(rel), ".json") {
		return 0, false
	}
	curSlice, origSlice := curData, origData
	prefix, suffix := []byte(nil), []byte(nil)
	if rel == "js/plugins.js" {
		cs, ce := bytes.IndexByte(curData, '['), bytes.LastIndexByte(curData, ']')
		osIdx, oeIdx := bytes.IndexByte(origData, '['), bytes.LastIndexByte(origData, ']')
		if cs < 0 || ce < cs || osIdx < 0 || oeIdx < osIdx {
			return 0, false
		}
		prefix, suffix = curData[:cs], curData[ce+1:]
		curSlice, origSlice = curData[cs:ce+1], origData[osIdx:oeIdx+1]
	}

	curVals, err1 := jsonedit.Strings(curSlice)
	origVals, err2 := jsonedit.Strings(origSlice)
	if err1 != nil || err2 != nil {
		return 0, false
	}
	origByPtr := make(map[string]string, len(origVals))
	for _, v := range origVals {
		origByPtr[v.Pointer] = v.Value
	}
	type diff struct {
		ptr, cur, orig string
	}
	var diffs []diff
	for _, v := range curVals {
		if ov, ok := origByPtr[v.Pointer]; ok && ov != v.Value {
			diffs = append(diffs, diff{ptr: v.Pointer, cur: v.Value, orig: ov})
		}
	}
	if len(diffs) <= 1 {
		return len(diffs), len(diffs) == 1
	}

	targetPath := filepath.Join(webRoot, filepath.FromSlash(rel))
	testSubset := func(revertMask []bool) bool {
		repl := make(map[string]string)
		for i, d := range diffs {
			if revertMask[i] {
				repl[d.ptr] = d.orig
			}
		}
		body := curSlice
		if len(repl) > 0 {
			var err error
			body, err = jsonedit.ReplaceStrings(curSlice, repl)
			if err != nil {
				return false
			}
		}
		full := body
		if prefix != nil {
			full = append(append(append([]byte(nil), prefix...), body...), suffix...)
		}
		if os.WriteFile(targetPath, full, 0o644) != nil {
			return false
		}
		rt, err := runRuntimeInternal(ctx, webRoot, opts)
		return err == nil && rt != nil && rt.Ran && len(rt.Issues) == 0
	}

	reverted := make([]bool, len(diffs))
	for {
		if testSubset(reverted) {
			break
		}
		var active []int
		for i, r := range reverted {
			if !r {
				active = append(active, i)
			}
		}
		if len(active) == 0 {
			return 0, false
		}
		lo, hi := 0, len(active)-1
		for lo < hi {
			mid := (lo + hi) / 2
			probe := append([]bool(nil), reverted...)
			for k := 0; k <= mid; k++ {
				probe[active[k]] = true
			}
			if testSubset(probe) {
				hi = mid
			} else {
				lo = mid + 1
			}
		}
		reverted[active[lo]] = true
	}

	count := 0
	for _, r := range reverted {
		if r {
			count++
		}
	}
	return count, count > 0
}

func collectChangedCandidates(webRoot, origWebRoot string, explicit []string) []string {
	if len(explicit) > 0 {
		var out []string
		for _, rel := range explicit {
			out = append(out, filepath.ToSlash(rel))
		}
		return out
	}
	var out []string
	if _, err := os.Stat(filepath.Join(webRoot, "js", "plugins.js")); err == nil {
		out = append(out, "js/plugins.js")
	}
	_ = filepath.WalkDir(filepath.Join(webRoot, "data"), func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(d.Name()))
		if ext != ".csv" && ext != ".json" {
			return nil
		}
		rel, _ := filepath.Rel(webRoot, p)
		rel = filepath.ToSlash(rel)
		if _, err := os.Stat(filepath.Join(origWebRoot, filepath.FromSlash(rel))); err == nil {
			out = append(out, rel)
		}
		return nil
	})
	return out
}
