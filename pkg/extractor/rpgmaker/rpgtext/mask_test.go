package rpgtext

import (
	"encoding/json"
	"maps"
	"os"
	"strings"
	"testing"

	"github.com/BGLuis/slime-rpg/pkg/pipeline/masking"
)

// python_mask_reference.json foi gerado com mask_tokens_in_structure e os
// _DEFAULT_MASK_PATTERNS do RPGMakerExtractor (versão Python).
func TestMaskMatchesPythonReference(t *testing.T) {
	data, err := os.ReadFile("testdata/python_mask_reference.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Text    string            `json:"t"`
		Masked  string            `json:"m"`
		Mapping map[string]string `json:"map"`
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}

	// O último matcher (escapes de um caractere) é uma proteção extra da versão Go.
	pythonEquivalent := MaskMatchers()
	pythonEquivalent = pythonEquivalent[:len(pythonEquivalent)-1]
	m := masking.NewMaskerWith(pythonEquivalent...)

	for _, c := range cases {
		// O Python deixa no mapa placeholders engolidos por um padrão posterior
		// (ex.: \C[2] dentro de <font color=\C[2]>); o Go os remove, já que não
		// aparecem mais no texto.
		maps.DeleteFunc(c.Mapping, func(ph, _ string) bool { return !strings.Contains(c.Masked, ph) })
		masked, mapping := m.Mask(c.Text)
		if masked != c.Masked || !maps.Equal(mapping, c.Mapping) {
			t.Errorf("Mask(%q)\n got: %q %v\nwant: %q %v", c.Text, masked, mapping, c.Masked, c.Mapping)
		}
	}
}

func TestMaskPreservesKRDAndLiteralNewlines(t *testing.T) {
	m := masking.NewMaskerWith(MaskMatchers()...)
	cases := []struct {
		in   string
		want string
	}{
		{
			in:   `\LANG[0]はい\LANGEND\LANG[1]Yes\LANGEND`,
			want: "__XTOK_0__はい__XTOK_2____XTOK_1__Yes__XTOK_3__",
		},
		{
			in:   `\LANGF[S0031_01_None]回想\n（※戻せません）\LANGFEND`,
			want: "__XTOK_0__回想__XTOK_1__（※戻せません）__XTOK_2__",
		},
		{
			in:   `\LANGF[battle_1]Test/LANGFEND`,
			want: "__XTOK_0__Test__XTOK_1__",
		},
		{
			in:   `Hello\nbut not \N[1]`,
			want: "Hello__XTOK_1__but not __XTOK_0__",
		},
	}

	for _, c := range cases {
		masked, mapping := m.Mask(c.in)
		if masked != c.want {
			t.Errorf("Mask(%q) = %q, want %q", c.in, masked, c.want)
		}
		restored, err := m.Unmask(masked, mapping)
		if err != nil || restored != c.in {
			t.Errorf("Unmask(%q) = (%q, %v), want %q", masked, restored, err, c.in)
		}
	}
}
