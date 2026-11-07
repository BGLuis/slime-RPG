package formats

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/BGLuis/slime-rpg/pkg/detector"
)

func touch(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func rels(p *Plan) []string {
	var out []string
	for _, f := range p.Files {
		out = append(out, f.Rel)
	}
	return out
}

// O plano do MV/MZ inclui o js/plugins.js, que fica fora da pasta data, e
// ignora arquivos de dados sem texto (Tilesets, Animations).
func TestRPGMakerPlanIncludesPluginsJS(t *testing.T) {
	game := t.TempDir()
	for _, f := range []string{"www/data/System.json", "www/data/Map001.json", "www/data/Tilesets.json", "www/js/plugins.js", "www/js/rpg_core.js"} {
		touch(t, filepath.Join(game, f))
	}
	det, err := detector.Detect(game)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := ForDetection(det, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"www/data/Map001.json", "www/data/System.json", "www/js/plugins.js"}
	if got := rels(plan); !slices.Equal(got, want) {
		t.Errorf("arquivos = %v, esperava %v", got, want)
	}
	if !plan.Includes("www/js/plugins.js") || plan.Includes("www/js/rpg_core.js") {
		t.Error("Includes inconsistente com o plano")
	}
}

// Na detecção do WOLF a pasta de dados pode ser Data/MapData; os eventos comuns
// e bancos de Data/BasicData precisam entrar no plano.
func TestWolfPlanCoversBasicDataAndMapData(t *testing.T) {
	game := t.TempDir()
	for _, f := range []string{"Data/MapData/Map001.mps", "Data/BasicData/CommonEvent.dat", "Data/BasicData/DataBase.dat", "Data/BasicData/DataBase.project", "Data/BasicData/Game.dat", "Data/Evtext/a.txt", "Data/readme.txt"} {
		touch(t, filepath.Join(game, f))
	}
	det, _ := detector.Detect(game)
	plan, err := ForDetection(det, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Data/BasicData/CommonEvent.dat", "Data/BasicData/DataBase.dat", "Data/BasicData/Game.dat", "Data/Evtext/a.txt", "Data/MapData/Map001.mps"}
	if got := rels(plan); !slices.Equal(got, want) {
		t.Errorf("arquivos = %v, esperava %v", got, want)
	}
}

func TestPackedWolfGameReportsWhatToDo(t *testing.T) {
	game := t.TempDir()
	touch(t, filepath.Join(game, "Data", "BasicData.wolf"))
	det, err := detector.Detect(game)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ForDetection(det, nil); !errors.Is(err, ErrPackedWolf) {
		t.Errorf("esperava ErrPackedWolf, obteve %v", err)
	}
}
