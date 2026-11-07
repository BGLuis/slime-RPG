package settings

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/BGLuis/slime-rpg/pkg/i18n"
)

func TestRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "settings.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := s.Get("k", "padrão"); got != "padrão" {
		t.Errorf("Get ausente = %q", got)
	}
	for _, v := range []string{"a", "b"} {
		if err := s.Set("k", v); err != nil {
			t.Fatal(err)
		}
	}
	s.Close()

	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if got := s.Get("k", ""); got != "b" {
		t.Errorf("Get após reabrir = %q, quer b (upsert)", got)
	}
}

// O settings.db criado pelo Python (mesmo DDL, sqlite3) deve ser lido e
// atualizado sem migração.
func TestCompatibleWithPythonDB(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("CREATE TABLE IF NOT EXISTS settings (key TEXT PRIMARY KEY, value TEXT NOT NULL)"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO settings (key, value) VALUES ('ui_language', 'en')"); err != nil {
		t.Fatal(err)
	}
	db.Close()

	if got := StoredLanguage(path); got != "en" {
		t.Fatalf("StoredLanguage = %q, quer en", got)
	}
	if err := SaveLanguage(path, i18n.PT); err != nil {
		t.Fatal(err)
	}
	db, _ = sql.Open("sqlite", path)
	defer db.Close()
	var v string
	if err := db.QueryRow("SELECT value FROM settings WHERE key = 'ui_language'").Scan(&v); err != nil || v != "pt" {
		t.Errorf("valor gravado = %q (%v), quer pt", v, err)
	}
}

func TestSaveLanguageRejectsUnsupported(t *testing.T) {
	if err := SaveLanguage(filepath.Join(t.TempDir(), "s.db"), "xx"); err == nil {
		t.Error("esperava erro para idioma não suportado")
	}
}

func TestResolveLanguageUsesStoredAndDoesNotCreateDB(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.db")
	t.Setenv("SLIME_SETTINGS_DB", path)
	none := func(string) string { return "" }

	if got := ResolveLanguage("", none); got != i18n.PT {
		t.Errorf("sem banco = %q, quer pt", got)
	}
	if fileExists(path) {
		t.Error("ResolveLanguage não deve criar o settings.db")
	}
	if err := SaveLanguage(path, i18n.EN); err != nil {
		t.Fatal(err)
	}
	if got := ResolveLanguage("", none); got != i18n.EN {
		t.Errorf("com banco = %q, quer en", got)
	}
	if got := ResolveLanguage("pt", none); got != i18n.PT {
		t.Errorf("flag deve vencer o armazenado, got %q", got)
	}
}
