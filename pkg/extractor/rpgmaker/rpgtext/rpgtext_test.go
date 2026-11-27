package rpgtext

import (
	"encoding/json"
	"os"
	"slices"
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

func TestKRDLangSpansAndControlCodes(t *testing.T) {
	if got := StripNestedControlCodes(`\LANGF[S0005_01_None]\LANGFEND`); got != "" {
		t.Errorf("StripNestedControlCodes deixou %q", got)
	}
	if !IsTechnicalOrCode(`\LANGF[S0005_01_None]\LANGFEND`, Dialogue) {
		t.Error("bloco LANGF vazio deveria ser ignorado")
	}

	// Bloco \LANG[...] separa cada idioma em seu próprio span
	spans := ShowTextSpans(`\LANG[0]はい\LANGEND\LANG[1]Yes\LANGEND`)
	if len(spans) != 2 || spans[0].Source != "はい" || spans[1].Source != "Yes" {
		t.Fatalf("ShowTextSpans LANG = %+v", spans)
	}

	// Bloco \LANGF[...] isola apenas o texto fallback (aceita \LANGFEND e /LANGFEND)
	fSpans := ShowTextSpans(`\LANGF[S0008_01]今日は終わり？\LANGFEND`)
	if len(fSpans) != 1 || fSpans[0].Source != "今日は終わり？" {
		t.Fatalf("ShowTextSpans LANGF = %+v", fSpans)
	}
	slashSpans := ShowTextSpans(`\LANGF[battle_1]テストです。/LANGFEND`)
	if len(slashSpans) != 1 || slashSpans[0].Source != "テストです。" {
		t.Fatalf("ShowTextSpans /LANGFEND = %+v", slashSpans)
	}

	// Prosa fora dos blocos cai no fluxo de string inteira
	mixed := ShowTextSpans(`Outside \LANG[0]はい\LANGEND`)
	if len(mixed) != 1 || mixed[0].Source != `Outside \LANG[0]はい\LANGEND` {
		t.Fatalf("ShowTextSpans misto = %+v", mixed)
	}

	// ChoiceSpans preserva show_if(...) e separa blocos \LANG[...]
	choices := ChoiceSpans(`show_if(v[1]>0) \LANG[0]はい\LANGEND\LANG[1]Yes\LANGEND`)
	if len(choices) != 2 || choices[0].Source != "はい" || choices[1].Source != "Yes" {
		t.Fatalf("ChoiceSpans = %+v", choices)
	}
	if choices[0].Start != len(`show_if(v[1]>0) \LANG[0]`) {
		t.Errorf("offset inesperado em ChoiceSpans: %+v", choices[0])
	}

	// NoteSpans reconhece tags multilíngues do KRD_MZ_Multilingual
	note := NoteSpans(`<name_1:Hero><desc_1:Brave soul><message1_1:attacks!>`)
	var got []string
	for _, sp := range note {
		got = append(got, sp.Source)
	}
	want := []string{"Hero", "Brave soul", "attacks!"}
	if !slices.Equal(got, want) {
		t.Errorf("NoteSpans = %v, want %v", got, want)
	}
}
