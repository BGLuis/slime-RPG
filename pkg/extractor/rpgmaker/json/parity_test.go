package json

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"github.com/BGLuis/slime-rpg/pkg/model"
)

// Os arquivos de testdata/parity foram gerados com o RPGMakerExtractor da versão
// Python: cada .expected traz os textos extraídos e o JSON resultante após
// traduzir cada segmento como "PT<origem>". O porte precisa produzir o mesmo
// resultado, salvo as divergências deliberadas listadas em goOnly.
func TestParityWithPythonExtractor(t *testing.T) {
	// Divergências deliberadas da versão Go, por arquivo.
	type divergence struct {
		pythonOnlySources []string       // extraídos só pelo Python
		goOnlySources     []string       // extraídos só pelo Go
		pointer           map[string]any // valores esperados que diferem do resultado Python
	}
	divergences := map[string]divergence{
		"Map001.json": {
			// "\picture[yes]" é só código: o Python extrai e descarta na inserção.
			// "<WordWrap>" é tag do YEP Message Core, não nome de locutor.
			pythonOnlySources: []string{`\picture[yes]`, "WordWrap", "長い文章です"},
			goOnlySources:     []string{"<WordWrap>長い文章です"},
			pointer: map[string]any{
				"/events/1/pages/0/list/6/parameters/0": "PT<<WordWrap>長い文章です>",
			},
		},
	}

	files, _ := filepath.Glob("testdata/parity/*.json")
	if len(files) == 0 {
		t.Fatal("testdata/parity vazio")
	}
	for _, path := range files {
		name := filepath.Base(path)
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			expRaw, err := os.ReadFile(path + ".expected")
			if err != nil {
				t.Fatal(err)
			}
			var exp struct {
				Sources []string `json:"sources"`
				Result  any      `json:"result"`
			}
			if err := json.Unmarshal(expRaw, &exp); err != nil {
				t.Fatal(err)
			}
			div := divergences[name]

			ext := NewMVExtractor()
			segs, err := ext.Extract(name, data)
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, s := range segs {
				got = append(got, s.Source())
			}
			want := slices.Clone(exp.Sources)
			for _, s := range div.pythonOnlySources {
				if i := slices.Index(want, s); i >= 0 {
					want = slices.Delete(want, i, i+1)
				}
			}
			want = append(want, div.goOnlySources...)
			slices.Sort(got)
			slices.Sort(want)
			if !slices.Equal(got, want) {
				t.Errorf("textos extraídos divergem do Python\n got: %q\nwant: %q", got, want)
			}

			var resolved []*model.Segment
			for _, s := range segs {
				resolved = append(resolved, s.WithTarget("PT<"+s.Source()+">"))
			}
			out, err := ext.Inject(name, data, resolved)
			if err != nil {
				t.Fatal(err)
			}
			var result any
			if err := json.Unmarshal(out, &result); err != nil {
				t.Fatalf("JSON inválido após a injeção: %v", err)
			}
			for ptr, v := range div.pointer {
				setPointer(t, exp.Result, ptr, v)
			}
			if !reflect.DeepEqual(result, exp.Result) {
				gotJSON, _ := json.MarshalIndent(result, "", " ")
				wantJSON, _ := json.MarshalIndent(exp.Result, "", " ")
				t.Errorf("resultado diverge do Python\n got: %s\nwant: %s", gotJSON, wantJSON)
			}
		})
	}
}

func setPointer(t *testing.T, doc any, ptr string, value any) {
	t.Helper()
	parts := splitPointer(ptr)
	cur := doc
	for i, p := range parts {
		last := i == len(parts)-1
		switch node := cur.(type) {
		case map[string]any:
			if last {
				node[p] = value
				return
			}
			cur = node[p]
		case []any:
			idx := 0
			for _, ch := range p {
				idx = idx*10 + int(ch-'0')
			}
			if last {
				node[idx] = value
				return
			}
			cur = node[idx]
		default:
			t.Fatalf("ponteiro inválido %s", ptr)
		}
	}
}

func splitPointer(ptr string) []string {
	var parts []string
	cur := ""
	for _, ch := range ptr[1:] {
		if ch == '/' {
			parts = append(parts, cur)
			cur = ""
			continue
		}
		cur += string(ch)
	}
	return append(parts, cur)
}
