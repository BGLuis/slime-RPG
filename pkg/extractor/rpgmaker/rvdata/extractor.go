package rvdata

import (
	"fmt"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/BGLuis/slime-rpg/pkg/extractor/rpgmaker/rpgtext"
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

// Atributos traduzíveis por classe, os mesmos que a versão Python normaliza do
// RVData. Nomes de eventos, eventos comuns, tilesets e animações ficam de fora:
// são internos do editor e scripts às vezes os usam como chave.
var (
	dbAttributes    = []string{"@name", "@nickname", "@description", "@profile", "@message1", "@message2", "@message3", "@message4", "@note"}
	dbClasses       = []string{"RPG::Actor", "RPG::Class", "RPG::Item", "RPG::Weapon", "RPG::Armor", "RPG::Skill", "RPG::State", "RPG::Enemy"}
	classAttributes = map[string][]string{
		"RPG::MapInfo": {"@name"},
		"RPG::Map":     {"@display_name"},
		"RPG::System":  {"@game_title", "@currency_unit", "@elements", "@skill_types", "@weapon_types", "@armor_types"},
	}
)

// Termos de interface do VX Ace (Terms), do VX (Terms) e do XP (Words): todo
// atributo de texto, isolado ou em lista, é visível ao jogador.
var allStringAttributeClasses = []string{"RPG::System::Terms", "RPG::System::Words"}

// slot é um trecho traduzível de uma string do grafo de objetos Ruby. Vários
// trechos podem estar na mesma string (loc), como título e fala em "<Nome>fala".
type slot struct {
	id   string
	loc  string
	text string
	sp   rpgtext.Span
	kind model.SegmentKind
	ctx  map[string]any
	set  func(full string)
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
		if strings.TrimSpace(s.sp.Source) == "" {
			return
		}
		ctx := s.ctx
		ctx["file"] = base
		seg, err := model.NewSegment(fmt.Sprintf("%s#%s", base, s.id), s.sp.Source, s.kind, ctx, nil)
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
		if s.IsResolved() && s.Target() != "" && s.Target() != s.Source() {
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

	type pending struct {
		text  string
		set   func(string)
		edits []slot
	}
	byLoc := make(map[string]*pending)
	var order []string
	visit(root, "", func(s slot) {
		if _, ok := trMap[fmt.Sprintf("%s#%s", base, s.id)]; !ok {
			return
		}
		p, ok := byLoc[s.loc]
		if !ok {
			p = &pending{text: s.text, set: s.set}
			byLoc[s.loc] = p
			order = append(order, s.loc)
		}
		p.edits = append(p.edits, s)
	})
	for _, loc := range order {
		p := byLoc[loc]
		sort.Slice(p.edits, func(i, j int) bool { return p.edits[i].sp.Start > p.edits[j].sp.Start })
		text := p.text
		for _, ed := range p.edits {
			tr := trMap[fmt.Sprintf("%s#%s", base, ed.id)]
			text = text[:ed.sp.Start] + ed.sp.Encode(tr) + text[ed.sp.End:]
		}
		p.set(text)
	}

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
	isDB := slices.Contains(dbClasses, obj.ClassName)
	switch {
	case isDB:
		// Como no Python, registros sem nome são entradas vazias do banco.
		if name, _ := stringVal(obj.Attributes["@name"]); name == "" {
			return
		}
		attrs = dbAttributes
	case slices.Contains(allStringAttributeClasses, obj.ClassName):
		attrs = sortedKeys(obj.Attributes)
	}
	for _, attr := range attrs {
		val, found := obj.Attributes[attr]
		if !found {
			continue
		}
		kind := model.KindMenu
		if attr == "@description" || attr == "@profile" || attr == "@note" {
			kind = model.KindDB
		}
		ctx := func() map[string]any { return map[string]any{"attr": attr} }
		spansOf := func(text string) []rpgtext.Span {
			if attr == "@note" {
				return rpgtext.NoteSpans(text)
			}
			if rpgtext.IsTechnicalOrCode(text, rpgtext.Dialogue) {
				return nil
			}
			return []rpgtext.Span{rpgtext.Whole(text)}
		}
		if list, ok := val.([]any); ok {
			for i := range list {
				listSlots(list, i, fmt.Sprintf("%s/%s/%d", path, attr, i), kind, ctx, spansOf, fn)
			}
			continue
		}
		if text, ok := stringVal(val); ok {
			emit(fn, fmt.Sprintf("%s/%s", path, attr), text, spansOf(text), kind, ctx,
				func(tr string) { obj.Attributes[attr] = withText(val, tr) })
		}
	}
}

// visitCommand aplica aos comandos de evento as mesmas estratégias do MV/MZ, como
// a versão Python faz ao normalizar o RVData.
func visitCommand(cmd *RubyObject, path string, fn func(slot)) {
	code := getIntAttr(cmd, "@code")
	params, ok := cmd.Attributes["@parameters"].([]any)
	if !ok || len(params) == 0 {
		return
	}
	ctx := func() map[string]any { return map[string]any{"code": code} }
	param := func(i int, id string, kind model.SegmentKind, spans func(string) []rpgtext.Span) {
		if i < len(params) {
			listSlots(params, i, id, kind, ctx, spans, fn)
		}
	}
	one := func(f func(string) (rpgtext.Span, bool)) func(string) []rpgtext.Span {
		return func(s string) []rpgtext.Span {
			if sp, ok := f(s); ok {
				return []rpgtext.Span{sp}
			}
			return nil
		}
	}

	switch code {
	case 401, 405: // Mostrar texto e texto de rolagem
		param(0, path+"/p:0", model.KindDialogue, rpgtext.ShowTextSpans)
	case 101: // O parâmetro 0 é o arquivo da face
		for i := 1; i < len(params); i++ {
			param(i, fmt.Sprintf("%s/p:%d", path, i), model.KindDialogue, rpgtext.ShowTextSpans)
		}
	case 320, 324, 325: // Mudar nome, apelido e perfil
		param(1, path+"/p:1", model.KindDialogue, rpgtext.ShowTextSpans)
	case 102: // Mostrar escolhas
		if choices, ok := params[0].([]any); ok {
			for idx := range choices {
				listSlots(choices, idx, fmt.Sprintf("%s/choice:%d", path, idx), model.KindChoice, ctx, one(rpgtext.ChoiceSpan), fn)
			}
		}
	case 402: // Ramo de escolha
		param(1, path+"/p:1", model.KindChoice, one(rpgtext.ChoiceSpan))
	case 355, 655: // Script
		param(0, path+"/p:0", model.KindScript, rpgtext.ScriptSpans)
	case 356: // Comando de plugin
		param(0, path+"/p:0", model.KindScript, one(rpgtext.PluginMVSpan))
	case 122: // Controle de variáveis com operando script
		if len(params) >= 5 {
			if op, ok := intVal(params[3]); ok && op == 4 {
				param(4, path+"/p:4", model.KindScript, one(rpgtext.ControlVarSpan))
			}
		}
	}
}

// listSlots emite os trechos da string list[idx].
func listSlots(list []any, idx int, id string, kind model.SegmentKind, ctx func() map[string]any, spans func(string) []rpgtext.Span, fn func(slot)) {
	text, ok := stringVal(list[idx])
	if !ok {
		return
	}
	orig := list[idx]
	emit(fn, id, text, spans(text), kind, ctx, func(tr string) { list[idx] = withText(orig, tr) })
}

func emit(fn func(slot), id, text string, spans []rpgtext.Span, kind model.SegmentKind, ctx func() map[string]any, set func(string)) {
	for i, sp := range spans {
		sid := id
		if len(spans) > 1 {
			sid = fmt.Sprintf("%s/%d", id, i)
		}
		fn(slot{id: sid, loc: id, text: text, sp: sp, kind: kind, ctx: ctx(), set: set})
	}
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
	n, _ := intVal(ro.Attributes[attr])
	return n
}

func intVal(v any) (int64, bool) {
	switch n := v.(type) {
	case int:
		return int64(n), true
	case int32:
		return int64(n), true
	case int64:
		return n, true
	}
	return 0, false
}
