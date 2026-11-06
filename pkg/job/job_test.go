package job

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/BGLuis/slime-rpg/pkg/event"
	"github.com/BGLuis/slime-rpg/pkg/model"
)

func TestJob_SnapshotAndEstimate(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "slime-job-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	dataDir := filepath.Join(tempDir, "www", "data")
	_ = os.MkdirAll(dataDir, 0755)
	_ = os.WriteFile(filepath.Join(dataDir, "System.json"), []byte(`{"gameTitle": "Hero Quest"}`), 0644)

	bus := event.NewBus()
	defer bus.Close()

	cfg := Config{
		GamePath:     tempDir,
		ProviderName: "google",
		SourceLang:   "ja",
		TargetLang:   "pt",
	}

	j := NewJob(cfg, bus, nil, nil)
	snap := j.Snapshot()
	if snap.Provider != "google" {
		t.Errorf("expected provider google, got %s", snap.Provider)
	}

	est, err := j.Estimate(context.Background())
	if err != nil {
		t.Fatalf("Estimate failed: %v", err)
	}

	if est.FilesTotal != 1 {
		t.Errorf("expected 1 file total, got %d", est.FilesTotal)
	}

	snapAfter := j.Snapshot()
	if snapAfter.Phase != event.PhaseEstimating {
		t.Errorf("expected phase estimating, got %v", snapAfter.Phase)
	}
	if snapAfter.GameName != filepath.Base(tempDir) {
		t.Errorf("expected game name %s, got %s", filepath.Base(tempDir), snapAfter.GameName)
	}
}

func TestJob_ApplyCorrection(t *testing.T) {
	bus := event.NewBus()
	defer bus.Close()

	cfg := Config{
		SourceLang: "ja",
		TargetLang: "pt",
	}
	j := NewJob(cfg, bus, nil, nil)

	seg, _ := model.NewSegment("Map001.json#cmd:1", "こんにちは", model.KindDialogue, nil, nil)
	j.segments = append(j.segments, seg)
	j.segmentsMap[seg.ID()] = seg

	err := j.ApplyCorrection(seg.ID(), "Olá aventureiro!", true)
	if err != nil {
		t.Fatalf("ApplyCorrection failed: %v", err)
	}

	reviewList := j.SegmentsForReview()
	if len(reviewList) != 1 || reviewList[0].Target() != "Olá aventureiro!" {
		t.Errorf("correction was not applied properly: %v", reviewList)
	}
}

func TestJob_PauseResume(t *testing.T) {
	bus := event.NewBus()
	defer bus.Close()

	j := NewJob(Config{}, bus, nil, nil)
	j.phase = event.PhaseTranslating

	j.Pause()
	if !j.isPaused {
		t.Errorf("expected job to be paused")
	}

	j.Resume()
	if j.isPaused {
		t.Errorf("expected job to be resumed")
	}
}
