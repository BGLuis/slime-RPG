package rvdata

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"github.com/BGLuis/slime-rpg/pkg/model"
)

// testdata/parity foi gerado com o rubymarshal e o RPGMakerExtractor da versão
// Python: o .expected traz os textos extraídos e o arquivo normalizado após
// traduzir cada segmento como "PT<origem>".
func TestParityWithPythonRVData(t *testing.T) {
	for _, name := range []string{"Map001.rvdata2", "Items.rvdata2"} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join("testdata", "parity", name)
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			raw, err := os.ReadFile(path + ".expected")
			if err != nil {
				t.Fatal(err)
			}
			var exp struct {
				Sources []string `json:"sources"`
				Result  any      `json:"result"`
			}
			if err := json.Unmarshal(raw, &exp); err != nil {
				t.Fatal(err)
			}

			ext := NewExtractor()
			segs, err := ext.Extract(name, data, "ja")
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			var resolved []*model.Segment
			for _, s := range segs {
				got = append(got, s.Source())
				resolved = append(resolved, s.WithTarget("PT<"+s.Source()+">"))
			}
			slices.Sort(got)
			if !slices.Equal(got, exp.Sources) {
				t.Errorf("textos extraídos divergem do Python\n got: %q\nwant: %q", got, exp.Sources)
			}

			out, err := ext.Inject(name, data, resolved, "pt")
			if err != nil {
				t.Fatal(err)
			}
			root, err := decode(name, out)
			if err != nil {
				t.Fatal(err)
			}
			if result := normalize(root); !reflect.DeepEqual(result, exp.Result) {
				g, _ := json.Marshal(result)
				w, _ := json.Marshal(exp.Result)
				t.Errorf("resultado diverge do Python\n got: %s\nwant: %s", g, w)
			}
		})
	}
}

// normalize reproduz o to_normalized do Python para mapas e itens.
func normalize(root any) any {
	str := func(v any) any {
		if s, ok := stringVal(v); ok {
			return s
		}
		return nil
	}
	num := func(v any) any {
		n, _ := intVal(v)
		return float64(n)
	}
	params := func(list []any) []any {
		out := make([]any, len(list))
		for i, p := range list {
			switch v := p.(type) {
			case []any:
				out[i] = params2(v)
			default:
				if s, ok := stringVal(p); ok {
					out[i] = s
				} else {
					out[i] = num(p)
				}
			}
		}
		return out
	}
	switch v := root.(type) {
	case *RubyObject: // RPG::Map
		events := v.Attributes["@events"].(map[any]any)
		byID := map[int64]*RubyObject{}
		for k, e := range events {
			id, _ := intVal(k)
			byID[id] = e.(*RubyObject)
		}
		list := []any{nil}
		for id := int64(1); id <= int64(len(byID)); id++ {
			ev := byID[id]
			var pages []any
			for pIdx, pRaw := range ev.Attributes["@pages"].([]any) {
				var cmds []any
				for _, cRaw := range pRaw.(*RubyObject).Attributes["@list"].([]any) {
					c := cRaw.(*RubyObject)
					cmds = append(cmds, map[string]any{
						"code": num(c.Attributes["@code"]), "indent": num(c.Attributes["@indent"]),
						"parameters": params(c.Attributes["@parameters"].([]any)),
					})
				}
				pages = append(pages, map[string]any{"id": float64(pIdx), "list": cmds})
			}
			list = append(list, map[string]any{"id": num(ev.Attributes["@id"]), "name": str(ev.Attributes["@name"]), "pages": pages})
		}
		return map[string]any{"displayName": str(v.Attributes["@display_name"]), "events": list}
	case []any: // itens
		out := make([]any, len(v))
		for i, it := range v {
			obj, ok := it.(*RubyObject)
			if !ok {
				continue
			}
			out[i] = map[string]any{
				"id": num(obj.Attributes["@id"]), "name": str(obj.Attributes["@name"]),
				"description": str(obj.Attributes["@description"]), "note": str(obj.Attributes["@note"]),
			}
		}
		return out
	}
	return nil
}

func params2(list []any) []any {
	out := make([]any, len(list))
	for i, p := range list {
		if s, ok := stringVal(p); ok {
			out[i] = s
		}
	}
	return out
}
