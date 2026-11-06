package rvdata

import (
	"fmt"
	"path/filepath"
	"slices"
	"sort"
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

// Atributos traduzíveis por classe. Nomes de eventos, eventos comuns, tilesets e
// animações ficam de fora: são internos do editor e scripts às vezes os usam como chave.
var classAttributes = map[string][]string{
	"RPG::Actor":   {"@name", "@nickname", "@description", "@profile"},
	"RPG::Class":   {"@name", "@description"},
	"RPG::Item":    {"@name", "@description"},
	"RPG::Weapon":  {"@name", "@description"},
	"RPG::Armor":   {"@name", "@description"},
	"RPG::Skill":   {"@name", "@description", "@message1", "@message2"},
	"RPG::State":   {"@name", "@description", "@message1", "@message2", "@message3", "@message4"},
	"RPG::Enemy":   {"@name", "@description"},
	"RPG::MapInfo": {"@name"},
	"RPG::Map":     {"@display_name"},
	"RPG::System":  {"@game_title", "@currency_unit", "@elements", "@skill_types", "@weapon_types", "@armor_types"},
}

// Termos de interface do VX Ace (Terms), do VX (Terms) e do XP (Words): todo
// atributo de texto, isolado ou em lista, é visível ao jogador.
var allStringAttributeClasses = []string{"RPG::System::Terms", "RPG::System::Words"}

// slot é um texto traduzível localizado no grafo de objetos Ruby.
type slot struct {
	id   string
	text string
	kind model.SegmentKind
	ctx  map[string]any
	set  func(translated string)
}

// Extract extrai diálogos, escolhas e termos dos objetos Ruby Marshal.
func (e *Extractor) Extract(filePath string, data []byte, srcLang string) ([]*model.Segment, error) {
	base := filepath.Base(filePath)
	root, err := decode(base, data)
	if err != nil {
		return nil, err
	}

	var segments []*model.Segment
	visit(root, "", func(s slot) {
		if strings.TrimSpace(s.text) == "" {
			return
		}
		ctx := s.ctx
		ctx["file"] = base
		seg, err := model.NewSegment(fmt.Sprintf("%s#%s", base, s.id), s.text, s.kind, ctx, nil)
		if err == nil {
			segments = append(segments, seg)
		}
	})
	return segments, nil
}

// Inject aplica as traduções nos objetos Ruby e serializa novamente com NewWriter.
func (e *Extractor) Inject(filePath string, originalData []byte, segments []*model.Segment, tgtLang string) ([]byte, error) {
	base := filepath.Base(filePath)
	trMap := make(map[string]string, len(segments))
	for _, s := range segments {
		if s.IsResolved() && s.Target() != "" {
			trMap[s.ID()] = s.Target()
		}
	}
	if len(trMap) == 0 {
		return originalData, nil
	}

	root, err := decode(base, originalData)
	if err != nil {
		return nil, err
	}
	visit(root, "", func(s slot) {
		if tr, ok := trMap[fmt.Sprintf("%s#%s", base, s.id)]; ok {
			s.set(tr)
		}
	})

	writer := NewWriter()
	if err := writer.Write(root); err != nil {
		return nil, fmt.Errorf("failed to write ruby marshal %s: %w", base, err)
	}
	return writer.Bytes(), nil
}

func decode(base string, data []byte) (any, error) {
	reader, err := NewReader(data)
	if err != nil {
		return nil, fmt.Errorf("failed to read ruby marshal %s: %w", base, err)
	}
	root, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("failed to decode ruby marshal %s: %w", base, err)
	}
	return root, nil
}

// visit percorre o grafo em ordem determinística e chama fn para cada texto
// traduzível. Extract e Inject usam o mesmo percurso, então os IDs sempre batem.
func visit(node any, path string, fn func(slot)) {
	switch v := node.(type) {
	case *RubyObject:
		visitObject(v, path, fn)
		for _, k := range sortedKeys(v.Attributes) {
			visit(v.Attributes[k], fmt.Sprintf("%s/%s", path, k), fn)
		}
	case []any:
		for idx, elem := range v {
			visit(elem, fmt.Sprintf("%s/%d", path, idx), fn)
		}
	case map[any]any:
		keys := make([]any, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool { return fmt.Sprint(keys[i]) < fmt.Sprint(keys[j]) })
		for _, k := range keys {
			visit(v[k], fmt.Sprintf("%s/%v", path, keyLabel(k)), fn)
		}
	}
}

func keyLabel(k any) any {
	if rs, ok := k.(*RubyString); ok {
		return rs.Value
	}
	return k
}

func visitObject(obj *RubyObject, path string, fn func(slot)) {
	if obj.ClassName == "RPG::EventCommand" {
		visitCommand(obj, path, fn)
		return
	}

	attrs := classAttributes[obj.ClassName]
	if slices.Contains(allStringAttributeClasses, obj.ClassName) {
		attrs = sortedKeys(obj.Attributes)
	}
	for _, attr := range attrs {
		val, found := obj.Attributes[attr]
		if !found {
			continue
		}
		kind := model.KindMenu
		if attr == "@description" || attr == "@profile" {
			kind = model.KindDB
		}
		if list, ok := val.([]any); ok {
			for i := range list {
				stringSlot(list, i, fmt.Sprintf("%s/%s/%d", path, attr, i), kind, map[string]any{"attr": attr}, fn)
			}
			continue
		}
		if text, ok := stringVal(val); ok {
			fn(slot{
				id: fmt.Sprintf("%s/%s", path, attr), text: text, kind: kind,
				ctx: map[string]any{"attr": attr},
				set: func(tr string) { obj.Attributes[attr] = withText(val, tr) },
			})
		}
	}
}

func visitCommand(cmd *RubyObject, path string, fn func(slot)) {
	code := getIntAttr(cmd, "@code")
	params, ok := cmd.Attributes["@parameters"].([]any)
	if !ok || len(params) == 0 {
		return
	}
	switch code {
	case 401, 405: // Show Text e Scroll Text
		stringSlot(params, 0, path+"/p:0", model.KindDialogue, map[string]any{"code": code}, fn)
	case 102: // Show Choices
		if choices, ok := params[0].([]any); ok {
			for idx := range choices {
				stringSlot(choices, idx, fmt.Sprintf("%s/choice:%d", path, idx), model.KindChoice, map[string]any{"code": code, "choice": idx}, fn)
			}
		}
	}
}

func stringSlot(list []any, idx int, id string, kind model.SegmentKind, ctx map[string]any, fn func(slot)) {
	text, ok := stringVal(list[idx])
	if !ok {
		return
	}
	orig := list[idx]
	fn(slot{id: id, text: text, kind: kind, ctx: ctx, set: func(tr string) { list[idx] = withText(orig, tr) }})
}

// withText devolve a tradução no mesmo tipo da string original, preservando os
// ivars de encoding (strings do RPG Maker XP não têm :E e não podem ganhá-lo).
func withText(orig any, text string) any {
	if rs, ok := orig.(*RubyString); ok {
		return &RubyString{Value: text, Attributes: rs.Attributes}
	}
	return text
}

func stringVal(v any) (string, bool) {
	switch s := v.(type) {
	case string:
		return s, true
	case *RubyString:
		return s.Value, true
	}
	return "", false
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
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
