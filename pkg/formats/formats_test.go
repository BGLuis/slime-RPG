package formats

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/BGLuis/slime-rpg/pkg/detector"
	"github.com/BGLuis/slime-rpg/pkg/extractor/wolfrpg/dxa"
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

// packWolf grava dentro de game/Data/<name>.wolf os arquivos informados.
func packWolf(t *testing.T, game, name string, files map[string][]byte) string {
	t.Helper()
	var list []dxa.File
	for p, d := range files {
		list = append(list, dxa.File{Path: p, Read: func() ([]byte, error) { return d, nil }})
	}
	path := filepath.Join(game, "Data", name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := dxa.Write(f, list); err != nil {
		t.Fatal(err)
	}
	return path
}

// Um jogo só com .wolf deixa de ser recusado: os arquivos do contêiner entram no
// plano, mas só os que não existem soltos (o arquivo solto tem prioridade) e só
// os que o extrator trata; contêineres multimídia nem são abertos.
func TestPackedWolfFilesJoinThePlan(t *testing.T) {
	game := t.TempDir()
	packWolf(t, game, "BasicData.wolf", map[string][]byte{
		"CommonEvent.dat": []byte("a"), "Game.dat": []byte("b"), "icon.png": []byte("c"),
	})
	packWolf(t, game, "MapData.wolf", map[string][]byte{"Map001.mps": []byte("d")})
	touch(t, filepath.Join(game, "Data", "BGM.wolf"))
	touch(t, filepath.Join(game, "Data", "BasicData", "Game.dat"))

	det, err := detector.Detect(game)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := ForDetection(det, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer plan.Close()

	want := []string{"Data/BasicData/CommonEvent.dat", "Data/BasicData/Game.dat", "Data/MapData/Map001.mps"}
	if got := rels(plan); !slices.Equal(got, want) {
		t.Fatalf("arquivos = %v, esperava %v", got, want)
	}
	for _, f := range plan.Files {
		loose := f.Rel == "Data/BasicData/Game.dat"
		if loose != (f.Read == nil) {
			t.Errorf("%s: Read definido=%v, mas solto=%v", f.Rel, f.Read != nil, loose)
		}
	}
	data, err := plan.Files[0].Read()
	if err != nil || string(data) != "a" {
		t.Errorf("conteúdo do contêiner = %q, %v", data, err)
	}
}

func TestCorruptWolfArchiveFailsInsteadOfBeingIgnored(t *testing.T) {
	game := t.TempDir()
	touch(t, filepath.Join(game, "Data", "BasicData.wolf"))
	det, _ := detector.Detect(game)
	if _, err := ForDetection(det, nil); err == nil {
		t.Error("contêiner inválido deveria gerar erro")
	}
}

func TestWolfDefaultsKeepTitleAndFont(t *testing.T) {
	x := WolfOptions(ForEngine(detector.EngineWolfRPG, nil))
	if !x.PreserveTitle || x.TargetFont != "" || x.RepackMode {
		t.Errorf("padrões = %+v", x)
	}
}

// O reempacotamento só acontece com RepackMode e junta o conteúdo dos
// contêineres com os arquivos traduzidos, que prevalecem.
func TestPlanRepackOnlyWhenRepackModeIsOn(t *testing.T) {
	game := t.TempDir()
	packWolf(t, game, "BasicData.wolf", map[string][]byte{"CommonEvent.dat": []byte("original"), "Game.dat": []byte("game")})
	det, _ := detector.Detect(game)
	plan, err := ForDetection(det, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer plan.Close()

	process := t.TempDir()
	touchContent(t, filepath.Join(process, "Data", "BasicData", "CommonEvent.dat"), "traduzido")

	if rel, err := plan.Repack(process); err != nil || rel != "" {
		t.Fatalf("sem RepackMode nada deve ser gerado: %q %v", rel, err)
	}
	WolfOptions(plan.Format).RepackMode = true
	rel, err := plan.Repack(process)
	if err != nil || rel != "Data/Data.wolf" {
		t.Fatalf("Repack = %q, %v", rel, err)
	}
	a, err := dxa.Open(filepath.Join(process, filepath.FromSlash(rel)), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	got := map[string]string{}
	for _, e := range a.Entries {
		b, _ := a.Read(e)
		got[e.Path] = string(b)
	}
	want := map[string]string{"BasicData/CommonEvent.dat": "traduzido", "BasicData/Game.dat": "game"}
	if len(got) != 2 || got["BasicData/CommonEvent.dat"] != want["BasicData/CommonEvent.dat"] || got["BasicData/Game.dat"] != "game" {
		t.Errorf("Data.wolf = %v", got)
	}
}

func touchContent(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestWolfOptionsReachTheExtractor(t *testing.T) {
	x := WolfOptions(ForEngine(detector.EngineWolfRPG, map[string]string{
		OptWolfPreserveTit: "não", OptWolfTargetFont: "MS Gothic", OptWolfRepackMode: "sim",
	}))
	if x.PreserveTitle || x.TargetFont != "MS Gothic" || !x.RepackMode {
		t.Errorf("opções = %+v", x)
	}
}

func TestWolfBooleanOptionsRejectUnknownValues(t *testing.T) {
	for _, k := range []string{OptWolfPreserveTit, OptWolfRepackMode} {
		if err := ValidateOptions(map[string]string{k: "talvez"}); err == nil {
			t.Errorf("%s=talvez deveria ser rejeitado", k)
		}
	}
}
