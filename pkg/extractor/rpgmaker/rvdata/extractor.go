package rvdata

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/BGLuis/slime-rpg/pkg/model"
)

// Extractor é o extrator especializado em arquivos Ruby Marshal legados (.rvdata2, .rvdata, .rxdata).
type Extractor struct{}

func NewExtractor() *Extractor {
	return &Extractor{}
}

// CanHandle verifica se o arquivo é um formato serializado do Ruby Marshal RPG Maker.
func (e *Extractor) CanHandle(filePath string) bool {
	ext := strings.ToLower(filepath.Ext(filePath))
	return ext == ".rvdata2" || ext == ".rvdata" || ext == ".rxdata"
}

// Extract extrai diálogos, escolhas e termos dos objetos Ruby Marshal.
func (e *Extractor) Extract(filePath string, data []byte, srcLang string) ([]*model.Segment, error) {
	reader, err := NewReader(data)
	if err != nil {
		return nil, fmt.Errorf("failed to read ruby marshal %s: %w", filepath.Base(filePath), err)
	}

	root, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("failed to decode ruby marshal %s: %w", filepath.Base(filePath), err)
	}

	base := filepath.Base(filePath)
	var segments []*model.Segment

	e.walkExtract(base, root, "", &segments)
	return segments, nil
}

// Inject aplica as traduções nos objetos Ruby e serializa novamente com NewWriter.
func (e *Extractor) Inject(filePath string, originalData []byte, segments []*model.Segment, tgtLang string) ([]byte, error) {
	reader, err := NewReader(originalData)
	if err != nil {
		return nil, fmt.Errorf("failed to read ruby marshal %s: %w", filepath.Base(filePath), err)
	}

	root, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("failed to decode ruby marshal %s: %w", filepath.Base(filePath), err)
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

	base := filepath.Base(filePath)
	e.walkInject(base, root, "", trMap)

	writer := NewWriter()
	if err := writer.Write(root); err != nil {
		return nil, fmt.Errorf("failed to write ruby marshal %s: %w", base, err)
	}

	return writer.Bytes(), nil
}

func (e *Extractor) walkExtract(base string, node any, path string, segments *[]*model.Segment) {
	switch v := node.(type) {
	case *RubyObject:
		switch v.ClassName {
		case "RPG::EventCommand":
			code := getIntAttr(v, "@code")
			if code == 401 { // Show Text
				if params, ok := v.Attributes["@parameters"].([]any); ok && len(params) > 0 {
					text := stringVal(params[0])
					if strings.TrimSpace(text) != "" {
						id := fmt.Sprintf("%s#%s/p:0", base, path)
						seg, err := model.NewSegment(id, text, model.KindDialogue, map[string]any{
							"file": base,
							"code": code,
						}, nil)
						if err == nil {
							*segments = append(*segments, seg)
						}
					}
				}
			} else if code == 102 { // Show Choices
				if params, ok := v.Attributes["@parameters"].([]any); ok && len(params) > 0 {
					if choices, ok := params[0].([]any); ok {
						for idx, ch := range choices {
							text := stringVal(ch)
							if strings.TrimSpace(text) != "" {
								id := fmt.Sprintf("%s#%s/choice:%d", base, path, idx)
								seg, err := model.NewSegment(id, text, model.KindChoice, map[string]any{
									"file":   base,
									"code":   code,
									"choice": idx,
								}, nil)
								if err == nil {
									*segments = append(*segments, seg)
								}
							}
						}
					}
				}
			}
		default:
			// Extrai atributos de texto padrão (@name, @description, @display_name)
			translatableAttrs := []string{"@name", "@description", "@display_name", "@game_title", "@profile"}
			for _, attr := range translatableAttrs {
				if val, found := v.Attributes[attr]; found {
					str := stringVal(val)
					if strings.TrimSpace(str) != "" {
						kind := model.KindMenu
						if attr == "@description" || attr == "@profile" {
							kind = model.KindDB
						}
						id := fmt.Sprintf("%s#%s/%s", base, path, attr)
						seg, err := model.NewSegment(id, str, kind, map[string]any{
							"file": base,
							"attr": attr,
						}, nil)
						if err == nil {
							*segments = append(*segments, seg)
						}
					}
				}
			}
			for k, attrVal := range v.Attributes {
				e.walkExtract(base, attrVal, fmt.Sprintf("%s/%s", path, k), segments)
			}
		}
	case []any:
		for idx, elem := range v {
			e.walkExtract(base, elem, fmt.Sprintf("%s/%d", path, idx), segments)
		}
	case map[any]any:
		for k, val := range v {
			e.walkExtract(base, val, fmt.Sprintf("%s/%v", path, k), segments)
		}
	}
}

func (e *Extractor) walkInject(base string, node any, path string, trMap map[string]string) {
	switch v := node.(type) {
	case *RubyObject:
		switch v.ClassName {
		case "RPG::EventCommand":
			code := getIntAttr(v, "@code")
			if code == 401 {
				if params, ok := v.Attributes["@parameters"].([]any); ok && len(params) > 0 {
					id := fmt.Sprintf("%s#%s/p:0", base, path)
					if tr, found := trMap[id]; found {
						params[0] = tr
					}
				}
			} else if code == 102 {
				if params, ok := v.Attributes["@parameters"].([]any); ok && len(params) > 0 {
					if choices, ok := params[0].([]any); ok {
						for idx := range choices {
							id := fmt.Sprintf("%s#%s/choice:%d", base, path, idx)
							if tr, found := trMap[id]; found {
								choices[idx] = tr
							}
						}
					}
				}
			}
		default:
			translatableAttrs := []string{"@name", "@description", "@display_name", "@game_title", "@profile"}
			for _, attr := range translatableAttrs {
				id := fmt.Sprintf("%s#%s/%s", base, path, attr)
				if tr, found := trMap[id]; found {
					v.Attributes[attr] = tr
				}
			}
			for k, attrVal := range v.Attributes {
				e.walkInject(base, attrVal, fmt.Sprintf("%s/%s", path, k), trMap)
			}
		}
	case []any:
		for idx, elem := range v {
			e.walkInject(base, elem, fmt.Sprintf("%s/%d", path, idx), trMap)
		}
	case map[any]any:
		for k, val := range v {
			e.walkInject(base, val, fmt.Sprintf("%s/%v", path, k), trMap)
		}
	}
}

func stringVal(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	if rs, ok := v.(*RubyString); ok {
		return rs.Value
	}
	return ""
}

func getIntAttr(ro *RubyObject, attr string) int64 {
	val, ok := ro.Attributes[attr]
	if !ok {
		return 0
	}
	switch v := val.(type) {
	case int:
		return int64(v)
	case int32:
		return int64(v)
	case int64:
		return v
	default:
		return 0
	}
}
