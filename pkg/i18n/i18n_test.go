package i18n

import (
	"regexp"
	"strings"
	"testing"
)

func env(vars map[string]string) func(string) string {
	return func(k string) string { return vars[k] }
}

func TestResolveOrder(t *testing.T) {
	tests := []struct {
		name             string
		explicit, stored string
		vars             map[string]string
		want             Lang
	}{
		{"flag vence tudo", "en", "pt", map[string]string{"SLIME_LANG": "pt", "LANG": "pt_BR.UTF-8"}, EN},
		{"SLIME_LANG vence o armazenado", "", "pt", map[string]string{"SLIME_LANG": "en", "LANG": "pt_BR.UTF-8"}, EN},
		{"armazenado vence o locale", "", "en", map[string]string{"LANG": "pt_BR.UTF-8"}, EN},
		{"locale do sistema", "", "", map[string]string{"LANG": "en_US.UTF-8"}, EN},
		{"LANGUAGE antes de LANG", "", "", map[string]string{"LANGUAGE": "en", "LANG": "pt_BR"}, EN},
		{"padrão PT", "", "", nil, PT},
		{"locale não suportado cai para PT", "", "", map[string]string{"LANG": "de_DE.UTF-8"}, PT},
		{"valores inválidos são ignorados", "xx", "yy", map[string]string{"SLIME_LANG": "zz", "LANG": "en_GB"}, EN},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Resolve(tt.explicit, tt.stored, env(tt.vars)); got != tt.want {
				t.Errorf("Resolve = %q, quer %q", got, tt.want)
			}
		})
	}
}

// Toda chave precisa existir nos dois idiomas, com os mesmos verbos de formatação,
// senão a interface mostraria a chave crua ou quebraria o Sprintf.
func TestTablesHaveSameKeysAndVerbs(t *testing.T) {
	verbs := regexp.MustCompile(`%[#+\-0-9.]*[a-zA-Z%]`)
	for key, ptText := range pt {
		enText, ok := en[key]
		if !ok {
			t.Errorf("chave %q ausente em EN", key)
			continue
		}
		if got, want := strings.Join(verbs.FindAllString(enText, -1), ","), strings.Join(verbs.FindAllString(ptText, -1), ","); got != want {
			t.Errorf("chave %q: verbos EN %q != PT %q", key, got, want)
		}
	}
	for key := range en {
		if _, ok := pt[key]; !ok {
			t.Errorf("chave %q ausente em PT", key)
		}
	}
}

func TestTrInFormatsAndFallsBack(t *testing.T) {
	if got := TrIn(EN, "tm_locked", "a", "b"); got != "Locked: \"a\" -> \"b\"\n" {
		t.Errorf("EN = %q", got)
	}
	if got := TrIn(PT, "tm_locked", "a", "b"); got != "Travado: \"a\" -> \"b\"\n" {
		t.Errorf("PT = %q", got)
	}
	if got := TrIn(EN, "chave_inexistente"); got != "chave_inexistente" {
		t.Errorf("chave ausente = %q", got)
	}
}

func TestTableFallsBackToPT(t *testing.T) {
	if len(Table(EN)) != len(pt) {
		t.Errorf("Table(EN) deveria cobrir todas as chaves de PT")
	}
}
