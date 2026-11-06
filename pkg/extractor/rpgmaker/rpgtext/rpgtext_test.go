package rpgtext

import (
	"encoding/json"
	"os"
	"testing"
)

// python_reference.json foi gerado rodando RPGTextFilters (versão Python) sobre o
// corpus hostil e as strings dos testes Python: o porte precisa concordar em todos.
func TestMatchesPythonReference(t *testing.T) {
	data, err := os.ReadFile("testdata/python_reference.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Text     string `json:"t"`
		Dialogue bool   `json:"d"`
		Param    bool   `json:"p"`
		Prefix   string `json:"pre"`
		Body     string `json:"body"`
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		if got := IsTechnicalOrCode(c.Text, Dialogue); got != c.Dialogue {
			t.Errorf("IsTechnicalOrCode(%q, Dialogue) = %v, Python = %v", c.Text, got, c.Dialogue)
		}
		if got := IsTechnicalOrCode(c.Text, Param); got != c.Param {
			t.Errorf("IsTechnicalOrCode(%q, Param) = %v, Python = %v", c.Text, got, c.Param)
		}
		if pre, body := ParseChoiceCondition(c.Text); pre != c.Prefix || body != c.Body {
			t.Errorf("ParseChoiceCondition(%q) = (%q, %q), Python = (%q, %q)", c.Text, pre, body, c.Prefix, c.Body)
		}
	}
}

func TestParamContextRejectsConfigWordsOnly(t *testing.T) {
	if IsTechnicalOrCode("Start", Dialogue) {
		t.Error(`"Start" pode ser um botão de menu e deve ser traduzido em diálogo`)
	}
	if !IsTechnicalOrCode("Start", Param) {
		t.Error(`"Start" como parâmetro de plugin é configuração`)
	}
}
