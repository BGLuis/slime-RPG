package job

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/BGLuis/slime-rpg/pkg/event"
	"github.com/BGLuis/slime-rpg/pkg/memory"
	"github.com/BGLuis/slime-rpg/pkg/metadata"
	"github.com/BGLuis/slime-rpg/pkg/provider"
)

type fakeProvider struct {
	mu    sync.Mutex
	seen  []string
	fail  error
	calls int
}

func (f *fakeProvider) ID() string { return "google" }

func (f *fakeProvider) TranslateBatch(_ context.Context, texts []string, _, _ string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.fail != nil {
		return nil, f.fail
	}
	f.seen = append(f.seen, texts...)
	out := make([]string, len(texts))
	for i, t := range texts {
		out[i] = "PT[" + t + "]"
	}
	return out, nil
}

func (f *fakeProvider) received() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.seen)
}

const (
	systemJSON = `{"gameTitle":"勇者の旅","currencyUnit":"G","terms":{"messages":{"victory":"勝利！"}}}`
	mapJSON    = `{"events":[null,{"id":1,"pages":[{"list":[` +
		`{"code":401,"indent":0,"parameters":["こんにちは"]},` +
		`{"code":401,"indent":0,"parameters":["勝利！"]},` +
		`{"code":0,"indent":0,"parameters":[]}]}]}]}`
)

func writeGame(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "Hero")
	data := filepath.Join(root, "www", "data")
	for name, content := range map[string]string{
		"System.json": systemJSON,
		"Map001.json": mapJSON,
		"Map002.json": `{"events":[]}`,
	} {
		path := filepath.Join(data, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func newMemory(t *testing.T) *memory.TranslationMemory {
	t.Helper()
	mem, err := memory.New(filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = mem.Close() })
	return mem
}

func runJob(t *testing.T, cfg Config, mem *memory.TranslationMemory, prov provider.Provider) (*Job, error) {
	t.Helper()
	bus := event.NewBus()
	t.Cleanup(bus.Close)
	j := NewJob(cfg, bus, mem, prov)
	return j, j.Run(context.Background())
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func baseConfig(game string) Config {
	return Config{GamePath: game, ProviderName: "google", SourceLang: "ja", TargetLang: "pt", BackupEnabled: true}
}

func TestRunTranslatesGameInPlaceWithBackupAndMeta(t *testing.T) {
	game := writeGame(t)
	prov := &fakeProvider{}
	j, err := runJob(t, baseConfig(game), newMemory(t), prov)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	mapOut := read(t, filepath.Join(game, "www", "data", "Map001.json"))
	if !strings.Contains(mapOut, "PT[こんにちは]") || !strings.Contains(mapOut, "PT[勝利！]") {
		t.Errorf("mapa não traduzido: %s", mapOut)
	}
	if !strings.Contains(read(t, filepath.Join(game, "www", "data", "System.json")), `"gameTitle":"PT[勇者の旅]"`) {
		t.Error("System.json não traduzido")
	}

	// "勝利！" aparece no mapa e no System.json: deve ir ao provedor uma vez só.
	got := prov.received()
	slices.Sort(got)
	if want := []string{"G", "こんにちは", "勇者の旅", "勝利！"}; !slices.Equal(got, want) {
		t.Errorf("textos enviados ao provedor = %q, esperava %q", got, want)
	}

	backup := game + "-ja"
	if read(t, filepath.Join(backup, "www", "data", "Map001.json")) != mapJSON {
		t.Error("o backup deveria guardar o mapa original")
	}

	meta := metadata.Load(game)
	if meta == nil {
		t.Fatal("meta.json não gravado")
	}
	if meta.Files["www/data/Map001.json"].Status != metadata.StatusSuccess {
		t.Errorf("status do mapa no metadado = %+v", meta.Files["www/data/Map001.json"])
	}
	if *meta.OriginalDir != "../Hero-ja" || *meta.Extractor != "rpgmaker" {
		t.Errorf("original_dir=%v extractor=%v", *meta.OriginalDir, *meta.Extractor)
	}

	snap := j.Snapshot()
	if snap.ExitCode != 0 || snap.SegmentsTotal != 5 || snap.SegmentsNew != 5 || snap.FilesTotal != 3 {
		t.Errorf("snapshot = %+v", snap)
	}
	if snap.Files[1].Status != event.StatusIgnore {
		t.Errorf("Map002.json sem texto deveria ser ignorado: %+v", snap.Files)
	}
}

// Numa segunda execução in-place os arquivos já estão traduzidos: o texto de
// origem precisa vir do backup, senão a tradução seria retraduzida.
func TestSecondInPlaceRunReadsOriginalsFromBackup(t *testing.T) {
	game := writeGame(t)
	mem := newMemory(t)
	if _, err := runJob(t, baseConfig(game), mem, &fakeProvider{}); err != nil {
		t.Fatal(err)
	}

	again := &fakeProvider{}
	j, err := runJob(t, baseConfig(game), mem, again)
	if err != nil {
		t.Fatal(err)
	}
	if again.calls != 0 {
		t.Errorf("tudo deveria vir da memória, provedor recebeu %q", again.received())
	}
	mapOut := read(t, filepath.Join(game, "www", "data", "Map001.json"))
	if strings.Contains(mapOut, "PT[PT[") {
		t.Errorf("tradução aplicada sobre tradução: %s", mapOut)
	}
	for _, f := range j.Snapshot().Files {
		if f.Path == "www/data/Map001.json" && f.Status != event.StatusCached {
			t.Errorf("mapa deveria sair da memória: %+v", f)
		}
	}
}

func TestIncrementalRunOnlyTranslatesChangedFiles(t *testing.T) {
	game := writeGame(t)
	out := filepath.Join(t.TempDir(), "out")
	cfg := baseConfig(game)
	cfg.OutputDir = out
	cfg.BackupEnabled = false
	mem := newMemory(t)
	if _, err := runJob(t, cfg, mem, &fakeProvider{}); err != nil {
		t.Fatal(err)
	}

	changed := strings.Replace(mapJSON, "こんにちは", "さようなら", 1)
	if err := os.WriteFile(filepath.Join(game, "www", "data", "Map001.json"), []byte(changed), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg.Incremental = true
	prov := &fakeProvider{}
	j, err := runJob(t, cfg, mem, prov)
	if err != nil {
		t.Fatal(err)
	}
	files := j.Snapshot().Files
	if len(files) != 1 || files[0].Path != "www/data/Map001.json" {
		t.Errorf("só o mapa alterado deveria ser processado: %+v", files)
	}
	if got := prov.received(); !slices.Equal(got, []string{"さようなら"}) {
		t.Errorf("provedor recebeu %q", got)
	}
	if !strings.Contains(read(t, filepath.Join(out, "www", "data", "Map001.json")), "PT[さようなら]") {
		t.Error("mapa alterado não foi traduzido na saída")
	}
}

func TestRateLimitWritesPartialFilesAndReportsIt(t *testing.T) {
	game := writeGame(t)
	j, err := runJob(t, baseConfig(game), newMemory(t), &fakeProvider{fail: provider.ErrRateLimited})
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("esperava ErrRateLimited, obteve %v", err)
	}
	snap := j.Snapshot()
	if !snap.RateLimited || snap.ExitCode != 1 {
		t.Errorf("snapshot = %+v", snap)
	}
	meta := metadata.Load(game)
	if meta == nil || meta.Files["www/data/Map001.json"].Status != metadata.StatusPartial {
		t.Fatalf("arquivo sem tradução deveria constar como parcial: %+v", meta)
	}
	if read(t, filepath.Join(game, "www", "data", "Map001.json")) != mapJSON {
		t.Error("sem traduções o mapa deveria continuar igual ao original")
	}
}

func TestMemoryOnlyRunUsesOnlyTheMemory(t *testing.T) {
	game := writeGame(t)
	mem := newMemory(t)
	if err := mem.StoreBatch(context.Background(), []memory.Entry{{
		SrcLang: "ja", TgtLang: "pt", SourceText: "こんにちは", TargetText: "Olá", Engine: "google", Status: memory.StatusMachine,
	}}); err != nil {
		t.Fatal(err)
	}

	cfg := baseConfig(game)
	cfg.MemoryOnly = true
	if _, err := runJob(t, cfg, mem, nil); err != nil {
		t.Fatal(err)
	}
	mapOut := read(t, filepath.Join(game, "www", "data", "Map001.json"))
	if !strings.Contains(mapOut, `"Olá"`) || !strings.Contains(mapOut, `"勝利！"`) {
		t.Errorf("modo offline deveria aplicar só a memória: %s", mapOut)
	}
}

func TestRunRejectsInvalidConfig(t *testing.T) {
	game := writeGame(t)
	cfg := baseConfig(game)
	cfg.TargetLang = "ja"
	if _, err := runJob(t, cfg, nil, &fakeProvider{}); !errors.Is(err, ErrSameLanguage) {
		t.Errorf("esperava ErrSameLanguage, obteve %v", err)
	}
	if _, err := runJob(t, baseConfig(game), nil, nil); !errors.Is(err, ErrNoProvider) {
		t.Errorf("esperava ErrNoProvider, obteve %v", err)
	}
}

func TestEstimateCountsCachedSegments(t *testing.T) {
	game := writeGame(t)
	mem := newMemory(t)
	if err := mem.StoreBatch(context.Background(), []memory.Entry{{
		SrcLang: "ja", TgtLang: "pt", SourceText: "勝利！", TargetText: "Vitória!", Engine: "google", Status: memory.StatusMachine,
	}}); err != nil {
		t.Fatal(err)
	}
	j := NewJob(baseConfig(game), nil, mem, &fakeProvider{})
	est, err := j.Estimate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if est.SegmentsTotal != 5 || est.SegmentsCached != 2 || est.FilesTotal != 3 {
		t.Errorf("estimativa = %+v", est)
	}
	if _, err := os.Stat(game + "-ja"); !os.IsNotExist(err) {
		t.Error("a estimativa não pode criar o backup")
	}
}

func TestReviewRejectionWritesNothingAndApprovalCommits(t *testing.T) {
	for _, tc := range []struct {
		name    string
		approve bool
	}{{"rejeitado", false}, {"aprovado", true}} {
		t.Run(tc.name, func(t *testing.T) {
			game := writeGame(t)
			cfg := baseConfig(game)
			var seenDir string
			cfg.Review = func(dir string) bool {
				seenDir = dir
				return tc.approve
			}
			_, err := runJob(t, cfg, newMemory(t), &fakeProvider{})

			mapOut := read(t, filepath.Join(game, "www", "data", "Map001.json"))
			if seenDir == "" {
				t.Fatal("Review não foi chamado")
			}
			if tc.approve {
				if err != nil || !strings.Contains(mapOut, "PT[こんにちは]") {
					t.Errorf("err=%v mapa=%s", err, mapOut)
				}
				return
			}
			if !errors.Is(err, ErrReviewRejected) {
				t.Errorf("err = %v, esperava ErrReviewRejected", err)
			}
			if mapOut != mapJSON {
				t.Errorf("rejeição não deveria gravar nada: %s", mapOut)
			}
		})
	}
}
