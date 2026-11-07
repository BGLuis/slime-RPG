package detector

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetect_RPGMakerMZ(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "slime-detect-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	dataDir := filepath.Join(tempDir, "www", "data")
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		t.Fatalf("failed to create data dir: %v", err)
	}

	// Create System.json with Japanese text
	systemJSON := `{"gameTitle": "スライムの大冒険", "currencyUnit": "G"}`
	if err := os.WriteFile(filepath.Join(dataDir, "System.json"), []byte(systemJSON), 0644); err != nil {
		t.Fatalf("failed to write System.json: %v", err)
	}

	res, err := Detect(tempDir)
	if err != nil {
		t.Fatalf("Detect failed: %v", err)
	}

	if res.Engine != EngineRPGMakerMV && res.Engine != EngineRPGMakerMZ {
		t.Errorf("expected RPG Maker engine, got %v", res.Engine)
	}
	if res.DataDir != dataDir {
		t.Errorf("expected dataDir %s, got %s", dataDir, res.DataDir)
	}
	if res.ProbableLang != "ja" {
		t.Errorf("expected probable language 'ja', got %s", res.ProbableLang)
	}
}

func TestDetect_WolfRPG(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "slime-wolf-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	dataDir := filepath.Join(tempDir, "Data")
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		t.Fatalf("failed to create data dir: %v", err)
	}

	if err := os.WriteFile(filepath.Join(dataDir, "CommonEvent.dat"), []byte("WOLF"), 0644); err != nil {
		t.Fatalf("failed to write CommonEvent.dat: %v", err)
	}

	res, err := Detect(tempDir)
	if err != nil {
		t.Fatalf("Detect failed: %v", err)
	}

	if res.Engine != EngineWolfRPG {
		t.Errorf("expected WOLF RPG, got %v", res.Engine)
	}
}

func TestDetect_DangerousPath(t *testing.T) {
	_, err := Detect("/")
	if err != ErrDangerousPath {
		t.Errorf("expected ErrDangerousPath for root directory, got %v", err)
	}
}

func TestRestrictedPaths(t *testing.T) {
	cases := map[string]bool{
		"/home/u/.local/share/Trash/files/Game": true,
		"/mnt/hdd/.Trash-1000/files/Game":       true,
		"/home/u/.trash/Game":                   true,
		`C:/$Recycle.Bin/Game`:                  true,
		"/home/u/games/Trashman":                false,
		"/home/u/games/Trash Panda Quest":       false,
	}
	for path, want := range cases {
		if got := isRestrictedPath(path); got != want {
			t.Errorf("isRestrictedPath(%q) = %v, want %v", path, got, want)
		}
	}
	if !isRestrictedPath(filepath.Join("..", "..")) {
		t.Error("a raiz do repositório do Slime deveria ser recusada")
	}
}

func TestGameNameSkipsDataFolders(t *testing.T) {
	root := filepath.Join(t.TempDir(), "MeuJogo")
	data := filepath.Join(root, "www", "data")
	if err := os.MkdirAll(data, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(data, "System.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{root, filepath.Join(root, "www"), data} {
		res, err := Detect(path)
		if err != nil {
			t.Fatal(err)
		}
		if res.GameName != "MeuJogo" {
			t.Errorf("Detect(%s).GameName = %q, esperava MeuJogo (o Python grava \"www\")", path, res.GameName)
		}
	}
}
