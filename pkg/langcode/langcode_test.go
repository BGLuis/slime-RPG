package langcode_test

import (
	"testing"

	"github.com/BGLuis/slime-rpg/pkg/langcode"
)

func TestNormalize(t *testing.T) {
	cases := map[string]string{
		"en": "en", "EN": "en", "english": "en", "Inglês": "en",
		"pt": "pt", "pt-br": "pt", "pt_BR": "pt", "Portuguese": "pt", "Português": "pt",
		"es": "es", "Spanish": "es", "Español": "es",
		"ja": "ja", "Japanese": "ja",
		"desconhecido_xyz": "", "": "",
	}
	for in, want := range cases {
		if got := langcode.Normalize(in); got != want {
			t.Errorf("Normalize(%q) = %q, esperava %q", in, got, want)
		}
	}
}

func TestFromFilename(t *testing.T) {
	cases := []struct {
		file    string
		code    string
		pattern langcode.Pattern
		ok      bool
	}{
		{"en.csv", "en", langcode.CodeExact, true},
		{"EN.csv", "en", langcode.CodeExact, true},
		{"English.csv", "en", langcode.NameExact, true},
		{"portuguese.csv", "pt", langcode.NameExact, true},
		{"strings_en.csv", "en", langcode.Suffix, true},
		{"dialogue_pt_br.csv", "pt", langcode.SuffixComposite, true},
		{"es_subtitles.csv", "es", langcode.Prefix, true},
		{"items.csv", "", "", false},
		{"data_01.csv", "", "", false},
	}
	for _, c := range cases {
		code, pattern, ok := langcode.FromFilename(c.file)
		if code != c.code || pattern != c.pattern || ok != c.ok {
			t.Errorf("FromFilename(%q) = (%q, %q, %v), esperava (%q, %q, %v)", c.file, code, pattern, ok, c.code, c.pattern, c.ok)
		}
	}
}

func TestTargetFilename(t *testing.T) {
	cases := []struct{ source, lang, want string }{
		{"en.csv", "pt", "pt.csv"},
		{"EN.csv", "pt", "PT.csv"},
		{"English.csv", "pt", "Portuguese.csv"},
		{"english.csv", "pt", "portuguese.csv"},
		{"strings_en.csv", "pt", "strings_pt.csv"},
		{"loc_en.csv", "es", "loc_es.csv"},
		{"text_en_US.csv", "pt", "text_pt.csv"},
		{"items.csv", "pt", "items_pt.csv"},
		{"lang/en.csv", "pt", "lang/pt.csv"},
	}
	for _, c := range cases {
		if got := langcode.TargetFilename(c.source, c.lang); got != c.want {
			t.Errorf("TargetFilename(%q, %q) = %q, esperava %q", c.source, c.lang, got, c.want)
		}
	}
}

func TestIsLanguageColumn(t *testing.T) {
	cases := map[string]string{
		"en": "en", "EN": "en", "English": "en", "pt": "pt", "pt-BR": "pt", "Português": "pt",
		"id": "", "key": "", "value": "",
	}
	for in, want := range cases {
		if got := langcode.IsLanguageColumn(in); got != want {
			t.Errorf("IsLanguageColumn(%q) = %q, esperava %q", in, got, want)
		}
	}
}

func TestTargetFilenameCompositeSuffixReplacesBothSegments(t *testing.T) {
	if got := langcode.TargetFilename("dialogue_pt_br.csv", "es"); got != "dialogue_es.csv" {
		t.Errorf("obteve %q, esperava dialogue_es.csv", got)
	}
}
