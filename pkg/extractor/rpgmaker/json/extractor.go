package json

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/BGLuis/slime-rpg/pkg/model"
)

// OpCodes do RPG Maker MV/MZ
const (
	CodeShowText     = 401
	CodeShowTextMore = 405
	CodeShowChoices  = 102
	CodeShowName     = 101 // MZ adicionou nome do locutor no parâmetro índice 4
	CodeScrollText   = 105
)

var (
	rxMapFile    = regexp.MustCompile(`(?i)^Map\d+\.json$`)
	rxActorsFile = regexp.MustCompile(`(?i)^Actors\.json$`)
	rxTroopsFile = regexp.MustCompile(`(?i)^Troops\.json$`)
	rxCommonFile = regexp.MustCompile(`(?i)^CommonEvents\.json$`)
	rxSystemFile = regexp.MustCompile(`(?i)^System\.json$`)
	rxDbFiles    = regexp.MustCompile(`(?i)^(Classes|Skills|Items|Weapons|Armors|Enemies|States)\.json$`)
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
		rxTroopsFile.MatchString(base) ||
		rxSystemFile.MatchString(base) ||
		rxPluginsFile.MatchString(base)
}

// Extract extrai os segmentos traduzíveis do arquivo JSON.
func (e *MVExtractor) Extract(filePath string, data []byte) ([]*model.Segment, error) {
	base := filepath.Base(filePath)
	if rxPluginsFile.MatchString(base) {
		return e.extractPluginsJs(base, data)
	}

	var raw any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse json %s: %w", base, err)
	}

	var segments []*model.Segment

	switch {
	case rxMapFile.MatchString(base):
		segs, err := e.extractMap(base, raw)
		if err != nil {
			return nil, err
		}
		segments = append(segments, segs...)

	case rxCommonFile.MatchString(base):
		segs, err := e.extractCommonEvents(base, raw)
		if err != nil {
			return nil, err
		}
		segments = append(segments, segs...)

	case rxActorsFile.MatchString(base):
		segs, err := e.extractActors(base, raw)
		if err != nil {
			return nil, err
		}
		segments = append(segments, segs...)

	case rxDbFiles.MatchString(base):
		segs, err := e.extractGenericDB(base, raw)
		if err != nil {
			return nil, err
		}
		segments = append(segments, segs...)

	case rxSystemFile.MatchString(base):
		segs, err := e.extractSystem(base, raw)
		if err != nil {
			return nil, err
		}
		segments = append(segments, segs...)
	}

	return segments, nil
}

// Inject reinjeta as traduções resolvidas no JSON original.
func (e *MVExtractor) Inject(filePath string, originalData []byte, segments []*model.Segment) ([]byte, error) {
	base := filepath.Base(filePath)
	if rxPluginsFile.MatchString(base) {
		return e.injectPluginsJs(base, originalData, segments)
	}

	var raw any
	if err := json.Unmarshal(originalData, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse json %s: %w", base, err)
	}

	translationMap := make(map[string]string, len(segments))
	for _, s := range segments {
		if s.IsResolved() && s.Target() != "" {
			translationMap[s.ID()] = s.Target()
		}
	}

	if len(translationMap) == 0 {
		return originalData, nil
	}

	switch {
	case rxMapFile.MatchString(base):
		e.injectMap(base, raw, translationMap)
	case rxCommonFile.MatchString(base):
		e.injectCommonEvents(base, raw, translationMap)
	case rxActorsFile.MatchString(base):
		e.injectActors(base, raw, translationMap)
	case rxDbFiles.MatchString(base):
		e.injectGenericDB(base, raw, translationMap)
	case rxSystemFile.MatchString(base):
		e.injectSystem(base, raw, translationMap)
	}

	return json.MarshalIndent(raw, "", "  ")
}

// --- Map Extraction & Injection ---

func (e *MVExtractor) extractMap(base string, raw any) ([]*model.Segment, error) {
	m, ok := raw.(map[string]any)
	if !ok {
		return nil, nil
	}

	var segments []*model.Segment

	// Nome de exibição do mapa (displayName)
	if disp, ok := m["displayName"].(string); ok && strings.TrimSpace(disp) != "" {
		id := fmt.Sprintf("%s#map:displayName", base)
		seg, err := model.NewSegment(id, disp, model.KindMenu, map[string]any{"field": "displayName"}, nil)
		if err == nil {
			segments = append(segments, seg)
		}
	}

	events, ok := m["events"].([]any)
	if !ok {
		return segments, nil
	}

	for evIdx, evRaw := range events {
		ev, ok := evRaw.(map[string]any)
		if !ok || ev == nil {
			continue
		}
		pages, ok := ev["pages"].([]any)
		if !ok {
			continue
		}

		for pageIdx, pageRaw := range pages {
			page, ok := pageRaw.(map[string]any)
			if !ok || page == nil {
				continue
			}
			list, ok := page["list"].([]any)
			if !ok {
				continue
			}

			segs := e.extractCommandList(base, fmt.Sprintf("event:%d/page:%d", evIdx, pageIdx), list)
			segments = append(segments, segs...)
		}
	}

	return segments, nil
}

func (e *MVExtractor) injectMap(base string, raw any, trMap map[string]string) {
	m, ok := raw.(map[string]any)
	if !ok {
		return
	}

	if target, ok := trMap[fmt.Sprintf("%s#map:displayName", base)]; ok {
		m["displayName"] = target
	}

	events, ok := m["events"].([]any)
	if !ok {
		return
	}

	for evIdx, evRaw := range events {
		ev, ok := evRaw.(map[string]any)
		if !ok || ev == nil {
			continue
		}
		pages, ok := ev["pages"].([]any)
		if !ok {
			continue
		}

		for pageIdx, pageRaw := range pages {
			page, ok := pageRaw.(map[string]any)
			if !ok || page == nil {
				continue
			}
			list, ok := page["list"].([]any)
			if !ok {
				continue
			}

			e.injectCommandList(base, fmt.Sprintf("event:%d/page:%d", evIdx, pageIdx), list, trMap)
		}
	}
}

// --- CommonEvents Extraction & Injection ---

func (e *MVExtractor) extractCommonEvents(base string, raw any) ([]*model.Segment, error) {
	list, ok := raw.([]any)
	if !ok {
		return nil, nil
	}

	var segments []*model.Segment
	for ceIdx, ceRaw := range list {
		ce, ok := ceRaw.(map[string]any)
		if !ok || ce == nil {
			continue
		}
		cmdList, ok := ce["list"].([]any)
		if !ok {
			continue
		}

		segs := e.extractCommandList(base, fmt.Sprintf("commonevent:%d", ceIdx), cmdList)
		segments = append(segments, segs...)
	}

	return segments, nil
}

func (e *MVExtractor) injectCommonEvents(base string, raw any, trMap map[string]string) {
	list, ok := raw.([]any)
	if !ok {
		return
	}

	for ceIdx, ceRaw := range list {
		ce, ok := ceRaw.(map[string]any)
		if !ok || ce == nil {
			continue
		}
		cmdList, ok := ce["list"].([]any)
		if !ok {
			continue
		}

		e.injectCommandList(base, fmt.Sprintf("commonevent:%d", ceIdx), cmdList, trMap)
	}
}

// --- Command List Handler (OpCodes) ---

func (e *MVExtractor) extractCommandList(base, prefix string, list []any) []*model.Segment {
	var segments []*model.Segment

	for cmdIdx, cmdRaw := range list {
		cmd, ok := cmdRaw.(map[string]any)
		if !ok || cmd == nil {
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

		switch code {
		case CodeShowText, CodeShowTextMore:
			if text, ok := params[0].(string); ok && strings.TrimSpace(text) != "" {
				id := fmt.Sprintf("%s#%s/cmd:%d", base, prefix, cmdIdx)
				seg, err := model.NewSegment(id, text, model.KindDialogue, map[string]any{"code": code}, nil)
				if err == nil {
					segments = append(segments, seg)
				}
			}

		case CodeShowName:
			// No RPG Maker MZ, o índice 4 contém o nome do personagem locutor
			if len(params) >= 5 {
				if name, ok := params[4].(string); ok && strings.TrimSpace(name) != "" {
					id := fmt.Sprintf("%s#%s/cmd:%d/speaker", base, prefix, cmdIdx)
					seg, err := model.NewSegment(id, name, model.KindDialogue, map[string]any{"code": code}, nil)
					if err == nil {
						segments = append(segments, seg)
					}
				}
			}

		case CodeShowChoices:
			if len(params) > 0 {
				if choices, ok := params[0].([]any); ok {
					for cIdx, cRaw := range choices {
						if cText, ok := cRaw.(string); ok && strings.TrimSpace(cText) != "" {
							id := fmt.Sprintf("%s#%s/cmd:%d/choice:%d", base, prefix, cmdIdx, cIdx)
							seg, err := model.NewSegment(id, cText, model.KindChoice, map[string]any{"code": code}, nil)
							if err == nil {
								segments = append(segments, seg)
							}
						}
					}
				}
			}
		}
	}

	return segments
}

func (e *MVExtractor) injectCommandList(base, prefix string, list []any, trMap map[string]string) {
	for cmdIdx, cmdRaw := range list {
		cmd, ok := cmdRaw.(map[string]any)
		if !ok || cmd == nil {
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

		switch code {
		case CodeShowText, CodeShowTextMore:
			id := fmt.Sprintf("%s#%s/cmd:%d", base, prefix, cmdIdx)
			if target, found := trMap[id]; found {
				params[0] = target
			}

		case CodeShowName:
			if len(params) >= 5 {
				id := fmt.Sprintf("%s#%s/cmd:%d/speaker", base, prefix, cmdIdx)
				if target, found := trMap[id]; found {
					params[4] = target
				}
			}

		case CodeShowChoices:
			if len(params) > 0 {
				if choices, ok := params[0].([]any); ok {
					for cIdx := range choices {
						id := fmt.Sprintf("%s#%s/cmd:%d/choice:%d", base, prefix, cmdIdx, cIdx)
						if target, found := trMap[id]; found {
							choices[cIdx] = target
						}
					}
				}
			}
		}
	}
}

// --- Database Extraction & Injection (Actors, Items, Skills, etc.) ---

func (e *MVExtractor) extractActors(base string, raw any) ([]*model.Segment, error) {
	list, ok := raw.([]any)
	if !ok {
		return nil, nil
	}

	var segments []*model.Segment
	fields := []struct {
		key  string
		kind model.SegmentKind
	}{
		{"name", model.KindDB},
		{"nickname", model.KindDB},
		{"profile", model.KindDialogue},
	}

	for idx, itemRaw := range list {
		item, ok := itemRaw.(map[string]any)
		if !ok || item == nil {
			continue
		}

		for _, f := range fields {
			if val, ok := item[f.key].(string); ok && strings.TrimSpace(val) != "" {
				id := fmt.Sprintf("%s#item:%d/field:%s", base, idx, f.key)
				seg, err := model.NewSegment(id, val, f.kind, map[string]any{"field": f.key}, nil)
				if err == nil {
					segments = append(segments, seg)
				}
			}
		}
	}

	return segments, nil
}

func (e *MVExtractor) injectActors(base string, raw any, trMap map[string]string) {
	list, ok := raw.([]any)
	if !ok {
		return
	}

	fields := []string{"name", "nickname", "profile"}
	for idx, itemRaw := range list {
		item, ok := itemRaw.(map[string]any)
		if !ok || item == nil {
			continue
		}

		for _, f := range fields {
			id := fmt.Sprintf("%s#item:%d/field:%s", base, idx, f)
			if target, found := trMap[id]; found {
				item[f] = target
			}
		}
	}
}

func (e *MVExtractor) extractGenericDB(base string, raw any) ([]*model.Segment, error) {
	list, ok := raw.([]any)
	if !ok {
		return nil, nil
	}

	var segments []*model.Segment
	fields := []struct {
		key  string
		kind model.SegmentKind
	}{
		{"name", model.KindDB},
		{"description", model.KindDB},
		{"message1", model.KindDialogue},
		{"message2", model.KindDialogue},
		{"message3", model.KindDialogue},
		{"message4", model.KindDialogue},
	}

	for idx, itemRaw := range list {
		item, ok := itemRaw.(map[string]any)
		if !ok || item == nil {
			continue
		}

		for _, f := range fields {
			if val, ok := item[f.key].(string); ok && strings.TrimSpace(val) != "" {
				id := fmt.Sprintf("%s#item:%d/field:%s", base, idx, f.key)
				seg, err := model.NewSegment(id, val, f.kind, map[string]any{"field": f.key}, nil)
				if err == nil {
					segments = append(segments, seg)
				}
			}
		}
	}

	return segments, nil
}

func (e *MVExtractor) injectGenericDB(base string, raw any, trMap map[string]string) {
	list, ok := raw.([]any)
	if !ok {
		return
	}

	fields := []string{"name", "description", "message1", "message2", "message3", "message4"}
	for idx, itemRaw := range list {
		item, ok := itemRaw.(map[string]any)
		if !ok || item == nil {
			continue
		}

		for _, f := range fields {
			id := fmt.Sprintf("%s#item:%d/field:%s", base, idx, f)
			if target, found := trMap[id]; found {
				item[f] = target
			}
		}
	}
}

// --- System.json Extraction & Injection ---

func (e *MVExtractor) extractSystem(base string, raw any) ([]*model.Segment, error) {
	sys, ok := raw.(map[string]any)
	if !ok {
		return nil, nil
	}

	var segments []*model.Segment
	if title, ok := sys["gameTitle"].(string); ok && strings.TrimSpace(title) != "" {
		id := fmt.Sprintf("%s#gameTitle", base)
		seg, err := model.NewSegment(id, title, model.KindMenu, nil, nil)
		if err == nil {
			segments = append(segments, seg)
		}
	}

	if terms, ok := sys["terms"].(map[string]any); ok {
		if messages, ok := terms["messages"].(map[string]any); ok {
			for k, vRaw := range messages {
				if msg, ok := vRaw.(string); ok && strings.TrimSpace(msg) != "" {
					id := fmt.Sprintf("%s#terms/messages/%s", base, k)
					seg, err := model.NewSegment(id, msg, model.KindMenu, nil, nil)
					if err == nil {
						segments = append(segments, seg)
					}
				}
			}
		}
	}

	return segments, nil
}

func (e *MVExtractor) injectSystem(base string, raw any, trMap map[string]string) {
	sys, ok := raw.(map[string]any)
	if !ok {
		return
	}

	if target, found := trMap[fmt.Sprintf("%s#gameTitle", base)]; found {
		sys["gameTitle"] = target
	}

	if terms, ok := sys["terms"].(map[string]any); ok {
		if messages, ok := terms["messages"].(map[string]any); ok {
			for k := range messages {
				id := fmt.Sprintf("%s#terms/messages/%s", base, k)
				if target, found := trMap[id]; found {
					messages[k] = target
				}
			}
		}
	}
}
