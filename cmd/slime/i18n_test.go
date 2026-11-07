package main

import (
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/BGLuis/slime-rpg/pkg/i18n"
	"github.com/BGLuis/slime-rpg/pkg/settings"
)

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	fn()
	w.Close()
	os.Stdout = old
	out, _ := io.ReadAll(r)
	return string(out)
}

func TestUsageSwitchesLanguage(t *testing.T) {
	t.Cleanup(func() { i18n.Set(i18n.PT) })
	i18n.Set(i18n.PT)
	if out := captureStdout(t, printUsage); !strings.Contains(out, "USO:") || !strings.Contains(out, "COMANDOS:") {
		t.Errorf("ajuda PT inesperada:\n%s", out)
	}
	i18n.Set(i18n.EN)
	if out := captureStdout(t, printUsage); !strings.Contains(out, "USAGE:") || strings.Contains(out, "USO:") {
		t.Errorf("ajuda EN inesperada:\n%s", out)
	}
}

func TestExtractLangFlag(t *testing.T) {
	tests := []struct {
		in       []string
		wantRest []string
		wantLang string
	}{
		{[]string{"--lang", "en", "list"}, []string{"list"}, "en"},
		{[]string{"list", "--lang=en"}, []string{"list"}, "en"},
		{[]string{"-lang", "pt", "-i", "x"}, []string{"-i", "x"}, "pt"},
		{[]string{"translate", "-i", "x"}, []string{"translate", "-i", "x"}, ""},
	}
	for _, tt := range tests {
		rest, lang := extractLangFlag(tt.in)
		if !reflect.DeepEqual(rest, tt.wantRest) || lang != tt.wantLang {
			t.Errorf("extractLangFlag(%v) = %v, %q", tt.in, rest, lang)
		}
	}
}

func TestRunLangPersists(t *testing.T) {
	t.Cleanup(func() { i18n.Set(i18n.PT) })
	path := filepath.Join(t.TempDir(), "settings.db")
	t.Setenv("SLIME_SETTINGS_DB", path)
	captureStdout(t, func() {
		if code := runLang([]string{"en"}); code != 0 {
			t.Errorf("runLang code = %d", code)
		}
	})
	if got := settings.StoredLanguage(path); got != "en" {
		t.Errorf("armazenado = %q, quer en", got)
	}
	if code := runLang([]string{"xx"}); code != 2 {
		t.Errorf("idioma inválido deveria retornar 2, got %d", code)
	}
}
