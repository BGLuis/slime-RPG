package json

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/BGLuis/slime-rpg/pkg/extractor/jsonedit"
	"github.com/BGLuis/slime-rpg/pkg/extractor/rpgmaker/rpgtext"
	"github.com/BGLuis/slime-rpg/pkg/model"
	"github.com/BGLuis/slime-rpg/pkg/pipeline/masking"
)

// OpCodes do RPG Maker MV/MZ tratados pelo extrator (os mesmos da versão Python).
// Comentários (108/408) e rótulos (118) ficam de fora de propósito: são anotações
// de desenvolvedor e diretivas de plugins, nunca texto visível.
const (
	CodeShowText        = 401
	CodeScrollTextLine  = 405
	CodeShowChoices     = 102
	CodeShowName        = 101 // MZ adicionou nome do locutor no parâmetro índice 4
	CodeScrollText      = 105
	CodeControlVars     = 122
	CodeSetMoveRoute    = 205
	CodeChangeName      = 320
	CodeChangeNickname  = 324
	CodeChangeProfile   = 325
	CodeScript          = 355
	CodePluginMV        = 356
	CodePluginMZ        = 357
	CodeChoiceBranch    = 402
	CodeRouteStep       = 505
	CodeScriptMore      = 655
	routeScriptStepCode = 45
)

var (
	rxMapFile    = regexp.MustCompile(`(?i)^Map\d+\.json$`)
	rxActorsFile = regexp.MustCompile(`(?i)^Actors\.json$`)
	rxTroopsFile = regexp.MustCompile(`(?i)^Troops\.json$`)
	rxCommonFile = regexp.MustCompile(`(?i)^CommonEvents\.json$`)
	rxSystemFile = regexp.MustCompile(`(?i)^System\.json$`)
	rxDbFiles    = regexp.MustCompile(`(?i)^(Classes|Skills|Items|Weapons|Armors|Enemies|States)\.json$`)
	rxMapInfos   = regexp.MustCompile(`(?i)^MapInfos\.json$`)

	formatCodePatterns = masking.DefaultRPGPatterns()
)

// MVExtractor é o extrator especializado em arquivos JSON do RPG Maker MV e MZ.
type MVExtractor struct{}

func NewMVExtractor() *MVExtractor {
	return &MVExtractor{}
}

// CanHandle verifica se o arquivo é um formato JSON ou plugins.js suportado pelo RPG Maker MV/MZ.
func (e *MVExtractor) CanHandle(filePath string) bool {
	base := filepath.Base(filePath)
	return rxMapFile.MatchString(base) ||
		rxCommonFile.MatchString(base) ||
		rxActorsFile.MatchString(base) ||
		rxDbFiles.MatchString(base) ||
		rxMapInfos.MatchString(base) ||
		rxTroopsFile.MatchString(base) ||
		rxSystemFile.MatchString(base) ||
		rxPluginsFile.MatchString(base)
}

// collector acumula segmentos guardando no contexto o JSON Pointer do texto,
// que o Inject usa para reescrever só aquela string.
type collector struct {
	base     string
	segments []*model.Segment
}

// add extrai a string inteira.
func (c *collector) add(id, pointer, text string, kind model.SegmentKind, ctx map[string]any) {
	if rpgtext.IsTechnicalOrCode(text, rpgtext.Dialogue) {
		return
	}
	c.addSpan(id, pointer, rpgtext.Whole(text), kind, ctx)
}

// addSpans extrai trechos de uma string; com mais de um trecho, cada um ganha um
// sufixo no id.
func (c *collector) addSpans(id, pointer string, spans []rpgtext.Span, kind model.SegmentKind, ctx func() map[string]any) {
	for i, sp := range spans {
		spanID := id
		if len(spans) > 1 {
			spanID = fmt.Sprintf("%s/%d", id, i)
		}
		c.addSpan(spanID, pointer, sp, kind, ctx())
	}
}

func (c *collector) addSpan(id, pointer string, sp rpgtext.Span, kind model.SegmentKind, ctx map[string]any) {
	if !isTranslatableText(sp.Source) {
		return
	}
	if ctx == nil {
		ctx = map[string]any{}
	}
	ctx["pointer"] = pointer
	ctx["span"] = sp
	seg, err := model.NewSegment(fmt.Sprintf("%s#%s", c.base, id), sp.Source, kind, ctx, nil)
	if err == nil {
		c.segments = append(c.segments, seg)
	}
}

// isTranslatableText descarta textos vazios ou compostos só de códigos de
// formato (ex.: escolhas "\picture[yes]"), que não têm nada a traduzir.
func isTranslatableText(text string) bool {
	rest := text
	for _, p := range formatCodePatterns {
		rest = p.ReplaceAllString(rest, "")
	}
	return strings.TrimSpace(rest) != ""
}

// Extract extrai os segmentos traduzíveis do arquivo JSON.
func (e *MVExtractor) Extract(filePath string, data []byte) ([]*model.Segment, error) {
	base := filepath.Base(filePath)
	if rxPluginsFile.MatchString(base) {
		return e.extractPluginsJs(base, data)
	}

	var raw any
	if err := json.Unmarshal(jsonedit.StripBOM(data), &raw); err != nil {
		return nil, fmt.Errorf("failed to parse json %s: %w", base, err)
	}

	c := &collector{base: base}
	switch {
	case rxMapFile.MatchString(base):
		e.extractMap(c, raw)
	case rxCommonFile.MatchString(base):
		e.extractCommonEvents(c, raw)
	case rxTroopsFile.MatchString(base):
		e.extractTroops(c, raw)
	case rxActorsFile.MatchString(base):
		e.extractDB(c, raw, actorFields)
	case rxDbFiles.MatchString(base):
		e.extractDB(c, raw, dbFields)
	case rxMapInfos.MatchString(base):
		e.extractDB(c, raw, mapInfoFields)
	case rxSystemFile.MatchString(base):
		e.extractSystem(c, raw)
	}
	return c.segments, nil
}

// Inject reinjeta as traduções resolvidas substituindo apenas as strings (ou os
// trechos de string) traduzidos: formatação, ordem das chaves, números e BOM
// ficam intactos.
func (e *MVExtractor) Inject(filePath string, originalData []byte, segments []*model.Segment) ([]byte, error) {
	base := filepath.Base(filePath)
	if rxPluginsFile.MatchString(base) {
		return e.injectPluginsJs(base, originalData, segments)
	}

	type edit struct {
		sp     rpgtext.Span
		target string
	}
	edits := make(map[string][]edit)
	for _, s := range segments {
		if !s.IsResolved() || s.Target() == s.Source() || !strings.HasPrefix(s.ID(), base+"#") {
			continue
		}
		ptr, ok := s.ContextValue("pointer")
		if !ok {
			continue
		}
		sp, _ := s.ContextValue("span")
		ed := edit{target: s.Target()}
		if v, ok := sp.(rpgtext.Span); ok {
			ed.sp = v
		} else {
			ed.sp = rpgtext.Whole(s.Source())
		}
		edits[ptr.(string)] = append(edits[ptr.(string)], ed)
	}
	if len(edits) == 0 {
		return originalData, nil
	}

	values, err := jsonedit.Strings(originalData)
	if err != nil {
		return nil, fmt.Errorf("failed to inject json %s: %w", base, err)
	}
	original := make(map[string]string, len(values))
	for _, v := range values {
		original[v.Pointer] = v.Value
	}

	replacements := make(map[string]string, len(edits))
	for ptr, list := range edits {
		value, ok := original[ptr]
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
		replacements[ptr] = value
	}

	out, err := jsonedit.ReplaceStrings(originalData, replacements)
	if err != nil {
		return nil, fmt.Errorf("failed to inject json %s: %w", base, err)
	}
	return out, nil
}

// --- Mapas, eventos comuns e tropas ---

func (e *MVExtractor) extractMap(c *collector, raw any) {
	m, ok := raw.(map[string]any)
	if !ok {
		return
	}

	if disp, ok := m["displayName"].(string); ok {
		c.add("map:displayName", "/displayName", disp, model.KindMenu, map[string]any{"field": "displayName"})
	}

	// "events" costuma ser um array com null no índice 0, mas mapas vazios trazem
	// false/null e alguns editores gravam um objeto indexado por id.
	forEachEntry(m["events"], func(key string, evRaw any) {
		ev, ok := evRaw.(map[string]any)
		if !ok {
			return
		}
		e.extractPages(c, fmt.Sprintf("event:%s", key), "/events/"+jsonedit.EscapePointer(key), ev["pages"])
	})
}

func (e *MVExtractor) extractCommonEvents(c *collector, raw any) {
	forEachEntry(raw, func(key string, ceRaw any) {
		ce, ok := ceRaw.(map[string]any)
		if !ok {
			return
		}
		if list, ok := ce["list"].([]any); ok {
			e.extractCommandList(c, fmt.Sprintf("commonevent:%s", key), "/"+key+"/list", list)
		}
	})
}

func (e *MVExtractor) extractTroops(c *collector, raw any) {
	forEachEntry(raw, func(key string, troopRaw any) {
		troop, ok := troopRaw.(map[string]any)
		if !ok {
			return
		}
		e.extractPages(c, fmt.Sprintf("troop:%s", key), "/"+key, troop["pages"])
	})
}

func (e *MVExtractor) extractPages(c *collector, idPrefix, ptrPrefix string, pagesRaw any) {
	pages, ok := pagesRaw.([]any)
	if !ok {
		return
	}
	for pageIdx, pageRaw := range pages {
		page, ok := pageRaw.(map[string]any)
		if !ok {
			continue
		}
		if list, ok := page["list"].([]any); ok {
			e.extractCommandList(c,
				fmt.Sprintf("%s/page:%d", idPrefix, pageIdx),
				fmt.Sprintf("%s/pages/%d/list", ptrPrefix, pageIdx),
				list)
		}
	}
}

// forEachEntry percorre um array (chave = índice) ou um objeto (chaves em ordem
// numérica); qualquer outro valor é ignorado.
func forEachEntry(container any, fn func(key string, value any)) {
	switch v := container.(type) {
	case []any:
		for i, item := range v {
			fn(strconv.Itoa(i), item)
		}
	case map[string]any:
		keys := make([]string, 0, len(v))
		for k := range v {
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
		for _, k := range keys {
			fn(k, v[k])
		}
	}
}

func (e *MVExtractor) extractCommandList(c *collector, idPrefix, ptrPrefix string, list []any) {
	for cmdIdx, cmdRaw := range list {
		cmd, ok := cmdRaw.(map[string]any)
		if !ok {
			continue
		}
		codeFloat, ok := cmd["code"].(float64)
		if !ok {
			continue
		}
		code := int(codeFloat)
		params, ok := cmd["parameters"].([]any)
		if !ok || len(params) == 0 {
			continue
		}

		id := fmt.Sprintf("%s/cmd:%d", idPrefix, cmdIdx)
		ptr := fmt.Sprintf("%s/%d/parameters", ptrPrefix, cmdIdx)
		ctx := func() map[string]any { return map[string]any{"code": code} }
		param := func(i int) (string, bool) {
			if i >= len(params) {
				return "", false
			}
			s, ok := params[i].(string)
			return s, ok
		}

		switch code {
		case CodeShowText, CodeScrollTextLine:
			if text, ok := param(0); ok {
				c.addSpans(id, ptr+"/0", rpgtext.ShowTextSpans(text), model.KindDialogue, ctx)
			}
		case CodeShowName:
			// O parâmetro 0 é o arquivo da face; o MZ guarda o locutor no índice 4.
			for i := 1; i < len(params); i++ {
				if text, ok := param(i); ok {
					sid := fmt.Sprintf("%s/param:%d", id, i)
					if i == 4 {
						sid = id + "/speaker"
					}
					c.addSpans(sid, fmt.Sprintf("%s/%d", ptr, i), rpgtext.ShowTextSpans(text), model.KindDialogue, ctx)
				}
			}
		case CodeChangeName, CodeChangeNickname, CodeChangeProfile:
			if text, ok := param(1); ok {
				c.addSpans(id, ptr+"/1", rpgtext.ShowTextSpans(text), model.KindDialogue, ctx)
			}
		case CodeShowChoices:
			if choices, ok := params[0].([]any); ok {
				for cIdx, cRaw := range choices {
					if text, ok := cRaw.(string); ok {
						if sp, ok := rpgtext.ChoiceSpan(text); ok {
							c.addSpan(fmt.Sprintf("%s/choice:%d", id, cIdx), fmt.Sprintf("%s/0/%d", ptr, cIdx), sp, model.KindChoice, ctx())
						}
					}
				}
			}
		case CodeChoiceBranch:
			if text, ok := param(1); ok {
				if sp, ok := rpgtext.ChoiceSpan(text); ok {
					c.addSpan(id, ptr+"/1", sp, model.KindChoice, ctx())
				}
			}
		case CodeScript, CodeScriptMore:
			if text, ok := param(0); ok {
				c.addSpans(id, ptr+"/0", rpgtext.ScriptSpans(text), model.KindScript, ctx)
			}
		case CodePluginMV:
			if text, ok := param(0); ok {
				if sp, ok := rpgtext.PluginMVSpan(text); ok {
					c.addSpan(id, ptr+"/0", sp, model.KindScript, ctx())
				}
			}
		case CodePluginMZ:
			if len(params) > 3 {
				if args, ok := params[3].(map[string]any); ok {
					for _, key := range rpgtext.MZPluginTextKeys(args) {
						text := args[key].(string)
						c.addSpan(id+"/"+key, ptr+"/3/"+jsonedit.EscapePointer(key), rpgtext.Whole(text), model.KindScript, ctx())
					}
				}
			}
		case CodeControlVars:
			if len(params) >= 5 && params[3] == float64(4) {
				if text, ok := param(4); ok {
					if sp, ok := rpgtext.ControlVarSpan(text); ok {
						c.addSpan(id, ptr+"/4", sp, model.KindScript, ctx())
					}
				}
			}
		case CodeSetMoveRoute:
			if len(params) > 1 {
				if route, ok := params[1].(map[string]any); ok {
					if steps, ok := route["list"].([]any); ok {
						for sIdx, stepRaw := range steps {
							if script, ok := routeScript(stepRaw); ok {
								c.addSpans(fmt.Sprintf("%s/step:%d", id, sIdx), fmt.Sprintf("%s/1/list/%d/parameters/0", ptr, sIdx), rpgtext.ScriptSpans(script), model.KindScript, ctx)
							}
						}
					}
				}
			}
		case CodeRouteStep:
			if script, ok := routeScript(params[0]); ok {
				c.addSpans(id, ptr+"/0/parameters/0", rpgtext.ScriptSpans(script), model.KindScript, ctx)
			}
		}
	}
}

// routeScript devolve o script de um passo de rota "Script" (código 45).
func routeScript(stepRaw any) (string, bool) {
	step, ok := stepRaw.(map[string]any)
	if !ok || step["code"] != float64(routeScriptStepCode) {
		return "", false
	}
	params, ok := step["parameters"].([]any)
	if !ok || len(params) == 0 {
		return "", false
	}
	script, ok := params[0].(string)
	return script, ok
}

// --- Banco de dados (Actors, Items, Skills etc.) ---

type dbField struct {
	key  string
	kind model.SegmentKind
}

var (
	actorFields = []dbField{
		{"name", model.KindDB},
		{"nickname", model.KindDB},
		{"profile", model.KindDialogue},
		{"note", model.KindDB},
	}
	dbFields = []dbField{
		{"name", model.KindDB},
		{"description", model.KindDB},
		{"message1", model.KindDialogue},
		{"message2", model.KindDialogue},
		{"message3", model.KindDialogue},
		{"message4", model.KindDialogue},
		{"note", model.KindDB},
	}
	mapInfoFields = []dbField{
		{"name", model.KindDB},
	}
)

// extractDB extrai os campos de texto de cada registro com nome. Na nota, só o
// conteúdo de tags de texto (<Desc: ...>, <Help: ...>) é traduzido.
func (e *MVExtractor) extractDB(c *collector, raw any, fields []dbField) {
	forEachEntry(raw, func(key string, itemRaw any) {
		item, ok := itemRaw.(map[string]any)
		if !ok {
			return
		}
		if name, _ := item["name"].(string); name == "" {
			return
		}
		for _, f := range fields {
			val, ok := item[f.key].(string)
			if !ok {
				continue
			}
			id := fmt.Sprintf("item:%s/field:%s", key, f.key)
			ptr := fmt.Sprintf("/%s/%s", key, f.key)
			ctx := func() map[string]any { return map[string]any{"field": f.key} }
			if f.key == "note" {
				c.addSpans(id, ptr, rpgtext.NoteSpans(val), f.kind, ctx)
				continue
			}
			c.add(id, ptr, val, f.kind, ctx())
		}
	})
}

// --- System.json ---

// Listas de System.json visíveis ao jogador. switches e variables ficam de fora:
// são nomes internos do editor.
var systemLists = []string{"elements", "skillTypes", "weaponTypes", "armorTypes", "equipTypes"}

func (e *MVExtractor) extractSystem(c *collector, raw any) {
	sys, ok := raw.(map[string]any)
	if !ok {
		return
	}

	for _, key := range []string{"gameTitle", "currencyUnit"} {
		if val, ok := sys[key].(string); ok {
			c.add(key, "/"+key, val, model.KindMenu, nil)
		}
	}
	for _, key := range systemLists {
		e.extractStringList(c, key, "/"+key, sys[key])
	}

	terms, ok := sys["terms"].(map[string]any)
	if !ok {
		return
	}
	for _, key := range []string{"basic", "commands", "params"} {
		e.extractStringList(c, "terms/"+key, "/terms/"+key, terms[key])
	}
	if messages, ok := terms["messages"].(map[string]any); ok {
		forEachEntry(messages, func(k string, vRaw any) {
			if msg, ok := vRaw.(string); ok {
				c.add("terms/messages/"+k, "/terms/messages/"+jsonedit.EscapePointer(k), msg, model.KindMenu, nil)
			}
		})
	}
}

func (e *MVExtractor) extractStringList(c *collector, idPrefix, ptrPrefix string, listRaw any) {
	list, ok := listRaw.([]any)
	if !ok {
		return
	}
	for i, vRaw := range list {
		if val, ok := vRaw.(string); ok {
			c.add(fmt.Sprintf("%s/%d", idPrefix, i), fmt.Sprintf("%s/%d", ptrPrefix, i), val, model.KindMenu, nil)
		}
	}
}
