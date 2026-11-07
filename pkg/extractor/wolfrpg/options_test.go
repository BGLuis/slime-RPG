package wolfrpg

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/BGLuis/slime-rpg/pkg/extractor/wolfrpg/dxa"
	"github.com/BGLuis/slime-rpg/pkg/model"
)

func gameDat(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "parity", "Game.dat"))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestApplyOptionsFollowsPythonParsing(t *testing.T) {
	x := NewExtractor()
	x.PreserveTitle = true
	x.ApplyOptions(map[string]string{
		"preserve_title": "Não, traduzir o título do jogo",
		"target_font":    "Tahoma",
		"repack_mode":    "Empacotar arquivo Data.wolf (.wolf repack)",
	})
	if x.PreserveTitle || x.TargetFont != "Tahoma" || !x.RepackMode {
		t.Errorf("opções = %+v", x)
	}

	y := NewExtractor()
	y.ApplyOptions(map[string]string{
		"preserve_title": "(Recomendado) Sim, manter título original (evita invalidar saves existentes)",
		"target_font":    "(Recomendado) Manter padrão do jogo",
		"repack_mode":    "(Recomendado) Arquivos descompactados na pasta Data/ (Carregamento nativo prioritário)",
	})
	if !y.PreserveTitle || y.TargetFont != "" || y.RepackMode {
		t.Errorf("as respostas recomendadas deveriam manter os padrões: %+v", y)
	}
}

func TestPreserveTitleControlsGameDatExtraction(t *testing.T) {
	data := gameDat(t)
	x := NewExtractor()
	x.PreserveTitle = true
	if segs, _ := x.Extract("Game.dat", data, "ja"); len(segs) != 0 {
		t.Errorf("com PreserveTitle o título não pode ser extraído: %d segmentos", len(segs))
	}
	x.PreserveTitle = false
	segs, _ := x.Extract("Game.dat", data, "ja")
	if len(segs) != 1 || segs[0].Source() != "勇者の旅" {
		t.Fatalf("segmentos = %v", segs)
	}
	out, err := x.Inject("Game.dat", data, []*model.Segment{segs[0].WithTarget("Jornada")}, "pt")
	if err != nil {
		t.Fatal(err)
	}
	g, err := LoadWolfGameDat(out)
	if err != nil || g.Title != "Jornada" {
		t.Errorf("título gravado = %q, %v", g.Title, err)
	}
}

func TestTargetFontReplacesGameFontWithOrWithoutTitle(t *testing.T) {
	data := gameDat(t)
	orig, _ := LoadWolfGameDat(data)

	x := NewExtractor()
	x.PreserveTitle = true
	if out, changed, err := x.Rewrite("Data/BasicData/Game.dat", data); err != nil || changed || !bytes.Equal(out, data) {
		t.Fatal("sem TargetFont o Game.dat não pode mudar")
	}

	x.TargetFont = "Tahoma"
	out, changed, err := x.Rewrite("Data/BasicData/Game.dat", data)
	if err != nil || !changed {
		t.Fatalf("Rewrite: changed=%v err=%v", changed, err)
	}
	g, err := LoadWolfGameDat(out)
	if err != nil || g.Font != "Tahoma" || g.Title != orig.Title {
		t.Errorf("fonte=%q título=%q (original %q) %v", g.Font, g.Title, orig.Title, err)
	}
	if _, changed, _ := x.Rewrite("Data/MapData/Map001.mps", data); changed {
		t.Error("só o Game.dat recebe a fonte")
	}

	// Com o título traduzido, a fonte também é aplicada.
	x.PreserveTitle = false
	segs, _ := x.Extract("Game.dat", data, "ja")
	out, err = x.Inject("Game.dat", data, []*model.Segment{segs[0].WithTarget("Jornada")}, "pt")
	if err != nil {
		t.Fatal(err)
	}
	if g, _ := LoadWolfGameDat(out); g.Font != "Tahoma" || g.Title != "Jornada" {
		t.Errorf("fonte=%q título=%q", g.Font, g.Title)
	}
}

func TestPackedProjectIsReadFromTheArchive(t *testing.T) {
	root := t.TempDir()
	dbData, _ := os.ReadFile(filepath.Join("testdata", "parity", "DataBase.dat"))
	project, _ := os.ReadFile(filepath.Join("testdata", "parity", "DataBase.project"))
	write := func(name string, files map[string][]byte) {
		var list []dxa.File
		for p, d := range files {
			list = append(list, dxa.File{Path: p, Read: func() ([]byte, error) { return d, nil }})
		}
		path := filepath.Join(root, "Data", name)
		_ = os.MkdirAll(filepath.Dir(path), 0o755)
		f, _ := os.Create(path)
		defer f.Close()
		if err := dxa.Write(f, list); err != nil {
			t.Fatal(err)
		}
	}
	write("BasicData.wolf", map[string][]byte{"DataBase.dat": dbData, "DataBase.project": project})

	packed, err := OpenPacked(root, []string{filepath.Join(root, "Data")})
	if err != nil {
		t.Fatal(err)
	}
	defer packed.Close()
	x := NewExtractor()
	x.Dir, x.Packed = root, packed
	if got := x.project("Data/BasicData/DataBase.dat"); !bytes.Equal(got, project) {
		t.Errorf("o .project deveria vir do contêiner (%d bytes)", len(got))
	}
}

func TestRepackMergesArchiveLooseAndTranslatedFiles(t *testing.T) {
	root := t.TempDir()
	data := filepath.Join(root, "Data")
	var list []dxa.File
	for p, d := range map[string]string{"CommonEvent.dat": "original", "Game.dat": "game"} {
		list = append(list, dxa.File{Path: p, Read: func() ([]byte, error) { return []byte(d), nil }})
	}
	_ = os.MkdirAll(data, 0o755)
	f, _ := os.Create(filepath.Join(data, "BasicData.wolf"))
	if err := dxa.Write(f, list); err != nil {
		t.Fatal(err)
	}
	f.Close()
	_ = os.MkdirAll(filepath.Join(data, "MapData"), 0o755)
	_ = os.WriteFile(filepath.Join(data, "MapData", "Map001.mps"), []byte("solto"), 0o644)
	_ = os.WriteFile(filepath.Join(data, "BasicData.wolf.bak"), []byte("lixo"), 0o644)

	overlay := t.TempDir()
	_ = os.MkdirAll(filepath.Join(overlay, "Data", "BasicData"), 0o755)
	_ = os.WriteFile(filepath.Join(overlay, "Data", "BasicData", "CommonEvent.dat"), []byte("traduzido"), 0o644)

	packed, err := OpenPacked(root, []string{data})
	if err != nil {
		t.Fatal(err)
	}
	defer packed.Close()
	out := filepath.Join(overlay, "Data", "Data.wolf")
	if err := packed.Repack("Data", overlay, out); err != nil {
		t.Fatal(err)
	}

	a, err := dxa.Open(out, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	got := map[string]string{}
	for _, e := range a.Entries {
		b, err := a.Read(e)
		if err != nil {
			t.Fatal(err)
		}
		got[e.Path] = string(b)
	}
	want := map[string]string{
		"BasicData/CommonEvent.dat": "traduzido", // o traduzido vence o do contêiner
		"BasicData/Game.dat":        "game",      // intocado, vem do contêiner
		"MapData/Map001.mps":        "solto",
	}
	if len(got) != len(want) {
		t.Fatalf("entradas = %v, esperava %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, esperava %q", k, got[k], v)
		}
	}
}

// Game.dat sem a assinatura conhecida (formato antigo ou cifrado, como o do Holy
// Sword Cecilia) não tem texto a traduzir e fica intacto, como na versão Python.
// Só é erro quando se pede para trocar a fonte, que não teria como ser gravada.
func TestUnrecognizedGameDatIsSkippedUnlessFontIsRequested(t *testing.T) {
	data := []byte{0x2c, 0x68, 0x1c, 0x00, 0x00, 0x36, 0xc2, 0x16, 0x69, 0x65}
	x := NewExtractor()
	segs, err := x.Extract("Data/BasicData/Game.dat", data, "ja")
	if err != nil || len(segs) != 0 {
		t.Fatalf("Extract = %d segmentos, %v", len(segs), err)
	}
	out, err := x.Inject("Data/BasicData/Game.dat", data, nil, "pt")
	if err != nil || !bytes.Equal(out, data) {
		t.Fatalf("Inject alterou o arquivo: %v", err)
	}

	x.TargetFont = "Tahoma"
	if _, _, err := x.Rewrite("Data/BasicData/Game.dat", data); !errors.Is(err, ErrInvalidDatabase) {
		t.Errorf("trocar a fonte de um Game.dat não reconhecido deveria falhar, err = %v", err)
	}
}
