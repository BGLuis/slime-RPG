package desktop

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BGLuis/slime-rpg/pkg/detector"
	"github.com/BGLuis/slime-rpg/pkg/job"
	"github.com/BGLuis/slime-rpg/pkg/settings"
)

func TestSlimeService_DetectAndLifecycle(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "slime-desktop-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Cria estrutura simulada do RPG Maker MZ
	dataDir := filepath.Join(tmpDir, "www", "data")
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		t.Fatalf("failed to create data dir: %v", err)
	}
	systemJSON := `{"gameTitle":"Teste Desktop","locale":"ja_JP"}`
	if err := os.WriteFile(filepath.Join(dataDir, "System.json"), []byte(systemJSON), 0644); err != nil {
		t.Fatalf("failed to write System.json: %v", err)
	}

	// Cria arquivo temporário para a base SQLite de testes
	dbFile := filepath.Join(tmpDir, "test-memory.db")
	t.Setenv("SLIME_MEMORY_DB", dbFile)

	svc := NewSlimeService(tmpDir)

	// 1. DetectGame
	det, err := svc.DetectGame(tmpDir)
	if err != nil {
		t.Fatalf("DetectGame failed: %v", err)
	}
	if det.Engine != detector.EngineRPGMakerMV && det.Engine != detector.EngineRPGMakerMZ {
		t.Fatalf("expected EngineRPGMakerMV or EngineRPGMakerMZ, got %v", det.Engine)
	}

	// 2. Estimate
	est, err := svc.Estimate(JobStartOptions{
		Directory:   tmpDir,
		Provider:    "google",
		SourceLang:  "ja",
		TargetLang:  "pt",
		Incremental: true,
	})
	if err != nil {
		t.Fatalf("Estimate failed: %v", err)
	}
	if est.EstimatedTimeLabel == "" {
		t.Errorf("expected estimated time label, got empty")
	}

	// 3. StartJob
	snap, err := svc.StartJob(JobStartOptions{
		Directory:   tmpDir,
		Provider:    "google",
		SourceLang:  "ja",
		TargetLang:  "pt",
		Incremental: true,
	})
	if err != nil {
		t.Fatalf("StartJob failed: %v", err)
	}
	if snap.GamePath != tmpDir {
		t.Errorf("expected snap game path %s, got %s", tmpDir, snap.GamePath)
	}

	// 4. Pause / Resume / Cancel
	if err := svc.PauseJob(); err != nil {
		t.Errorf("PauseJob failed: %v", err)
	}
	if err := svc.ResumeJob(); err != nil {
		t.Errorf("ResumeJob failed: %v", err)
	}
	if err := svc.CancelJob(); err != nil {
		t.Errorf("CancelJob failed: %v", err)
	}

	// 5. GetSnapshot
	currentSnap := svc.GetSnapshot()
	if currentSnap.GamePath != tmpDir {
		t.Errorf("expected current snap game path %s, got %s", tmpDir, currentSnap.GamePath)
	}

	// 6. GetSegmentsForReview
	items := svc.GetSegmentsForReview("all")
	if items == nil {
		items = []job.ReviewItem{}
	}

	// 7. ApplyCorrection em id inexistente deve falhar graciosamente
	if err := svc.ApplyCorrection("non-existent-id", "Novo texto", false); err == nil {
		t.Errorf("expected error applying correction on non existent id, got nil")
	}
}

func TestSlimeService_Language(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.db")
	t.Setenv("SLIME_SETTINGS_DB", path)
	svc := NewSlimeService("")

	info := svc.GetLanguage()
	if len(info.Options) != 2 || info.Strings["ui_step_game"] == "" {
		t.Fatalf("GetLanguage incompleto: %+v", info.Options)
	}

	msg, err := svc.SetLanguage("en")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(msg, "Restart") {
		t.Errorf("mensagem deveria vir em inglês: %q", msg)
	}
	if got := settings.StoredLanguage(path); got != "en" {
		t.Errorf("armazenado = %q, quer en", got)
	}
	if _, err := svc.SetLanguage("xx"); err == nil {
		t.Error("esperava erro para idioma inválido")
	}
}
