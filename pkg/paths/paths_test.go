package paths_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BGLuis/slime-rpg/pkg/paths"
)

func TestDataDir_Default(t *testing.T) {
	orig := os.Getenv("SLIME_DATA_DIR")
	defer os.Setenv("SLIME_DATA_DIR", orig)
	_ = os.Unsetenv("SLIME_DATA_DIR")

	dir, err := paths.DataDir()
	if err != nil {
		t.Fatalf("erro ao obter DataDir padrão: %v", err)
	}
	if !strings.Contains(dir, paths.AppName) {
		t.Errorf("esperava que DataDir contivesse %q, obteve %q", paths.AppName, dir)
	}
}

func TestDataDir_Override(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "test_slime_data_*")
	if err != nil {
		t.Fatalf("falha ao criar temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	orig := os.Getenv("SLIME_DATA_DIR")
	defer os.Setenv("SLIME_DATA_DIR", orig)

	_ = os.Setenv("SLIME_DATA_DIR", tempDir)

	dir, err := paths.DataDir()
	if err != nil {
		t.Fatalf("erro ao obter DataDir sobrescrito: %v", err)
	}
	if dir != tempDir {
		t.Errorf("esperava %q, obteve %q", tempDir, dir)
	}
}

func TestMemoryDBPath_EnvOverride(t *testing.T) {
	customDB := filepath.Join(t.TempDir(), "custom_test_memory.db")
	orig := os.Getenv("SLIME_MEMORY_DB")
	defer os.Setenv("SLIME_MEMORY_DB", orig)

	_ = os.Setenv("SLIME_MEMORY_DB", customDB)

	p, err := paths.MemoryDBPath()
	if err != nil {
		t.Fatalf("erro ao obter MemoryDBPath: %v", err)
	}
	if p != customDB {
		t.Errorf("esperava %q, obteve %q", customDB, p)
	}
}

func TestMemoryDBPath_PortableMode(t *testing.T) {
	_ = os.Unsetenv("SLIME_MEMORY_DB")

	// Cria estrutura temporária simulando pasta de execução local
	tempCwd, err := os.MkdirTemp("", "test_cwd_*")
	if err != nil {
		t.Fatalf("falha ao criar temp cwd: %v", err)
	}
	defer os.RemoveAll(tempCwd)

	cacheDir := filepath.Join(tempCwd, "cache")
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		t.Fatalf("falha ao criar pasta cache: %v", err)
	}
	localDB := filepath.Join(cacheDir, "memory.db")
	if err := os.WriteFile(localDB, []byte("fake sqlite"), 0644); err != nil {
		t.Fatalf("falha ao criar arquivo de teste local: %v", err)
	}

	origCwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("falha ao obter cwd atual: %v", err)
	}
	defer os.Chdir(origCwd)

	if err := os.Chdir(tempCwd); err != nil {
		t.Fatalf("falha ao trocar cwd: %v", err)
	}

	p, err := paths.MemoryDBPath()
	if err != nil {
		t.Fatalf("erro ao resolver MemoryDBPath em modo portátil: %v", err)
	}

	expected := localDB
	if p != expected {
		t.Errorf("esperava caminho portátil %q, obteve %q", expected, p)
	}
}

func TestEnsureDirAndFileDir(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "test_ensure_*")
	if err != nil {
		t.Fatalf("falha ao criar temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	subDir := filepath.Join(tempDir, "sub1", "sub2")
	if err := paths.EnsureDir(subDir); err != nil {
		t.Fatalf("falha no EnsureDir: %v", err)
	}
	if fi, err := os.Stat(subDir); err != nil || !fi.IsDir() {
		t.Fatalf("subDir não foi criado corretamente")
	}

	targetFile := filepath.Join(tempDir, "another", "nested", "file.txt")
	if err := paths.EnsureFileDir(targetFile); err != nil {
		t.Fatalf("falha no EnsureFileDir: %v", err)
	}
	parent := filepath.Dir(targetFile)
	if fi, err := os.Stat(parent); err != nil || !fi.IsDir() {
		t.Fatalf("diretório pai do arquivo não foi criado")
	}
}

func TestSettingsDBPath(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("SLIME_SETTINGS_DB", "")
	t.Setenv("SLIME_CONFIG_DIR", cfg)
	got, err := paths.SettingsDBPath()
	if err != nil || got != filepath.Join(cfg, "settings.db") {
		t.Errorf("SettingsDBPath = %q, %v", got, err)
	}
	override := filepath.Join(cfg, "outro.db")
	t.Setenv("SLIME_SETTINGS_DB", override)
	if got, _ := paths.SettingsDBPath(); got != override {
		t.Errorf("override = %q, quer %q", got, override)
	}
}
