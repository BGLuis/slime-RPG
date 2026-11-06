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
