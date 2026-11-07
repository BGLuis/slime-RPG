// Package settings guarda preferências simples chave/valor em SQLite, no mesmo
// arquivo e esquema do settings.db da versão Python.
package settings

import (
	"database/sql"
	"fmt"
	"os"

	"github.com/BGLuis/slime-rpg/pkg/i18n"
	"github.com/BGLuis/slime-rpg/pkg/paths"
	_ "modernc.org/sqlite"
)

// Store é o repositório de preferências.
type Store struct{ db *sql.DB }

// Open abre (criando se preciso) o settings.db em path.
func Open(path string) (*Store, error) {
	if err := paths.EnsureFileDir(path); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec("CREATE TABLE IF NOT EXISTS settings (key TEXT PRIMARY KEY, value TEXT NOT NULL)"); err != nil {
		db.Close()
		return nil, fmt.Errorf("falha ao preparar settings.db: %w", err)
	}
	return &Store{db: db}, nil
}

// Close libera o banco.
func (s *Store) Close() error { return s.db.Close() }

// Get devolve o valor de key ou def se não existir.
func (s *Store) Get(key, def string) string {
	var v string
	if err := s.db.QueryRow("SELECT value FROM settings WHERE key = ?", key).Scan(&v); err != nil {
		return def
	}
	return v
}

// Set grava value em key.
func (s *Store) Set(key, value string) error {
	_, err := s.db.Exec(
		"INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value",
		key, value)
	return err
}

// StoredLanguage lê a preferência de idioma em path; vazio se ausente ou ilegível.
func StoredLanguage(path string) string {
	s, err := Open(path)
	if err != nil {
		return ""
	}
	defer s.Close()
	return s.Get(i18n.SettingKey, "")
}

// SaveLanguage persiste l em path.
func SaveLanguage(path string, l i18n.Lang) error {
	if _, ok := i18n.Normalize(string(l)); !ok {
		return fmt.Errorf("idioma não suportado: %q", l)
	}
	s, err := Open(path)
	if err != nil {
		return err
	}
	defer s.Close()
	return s.Set(i18n.SettingKey, string(l))
}

// ResolveLanguage resolve o idioma efetivo usando o settings.db padrão.
// Não cria o banco quando ele ainda não existe.
func ResolveLanguage(explicit string, getenv func(string) string) i18n.Lang {
	stored := ""
	if path, err := paths.SettingsDBPath(); err == nil && fileExists(path) {
		stored = StoredLanguage(path)
	}
	return i18n.Resolve(explicit, stored, getenv)
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
