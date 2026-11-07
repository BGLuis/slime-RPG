package job

import (
	"bytes"
	"crypto/sha256"
	"os"
	"path/filepath"
	"testing"

	"github.com/BGLuis/slime-rpg/pkg/extractor/wolfrpg"
	"github.com/BGLuis/slime-rpg/pkg/extractor/wolfrpg/dxa"
	"github.com/BGLuis/slime-rpg/pkg/metadata"
)

func parity(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "extractor", "wolfrpg", "testdata", "parity", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// writePackedGame cria um jogo WOLF RPG que só tem Data/BasicData.wolf.
func writePackedGame(t *testing.T) (game, archive string) {
	t.Helper()
	game = filepath.Join(t.TempDir(), "Lecia")
	archive = filepath.Join(game, "Data", "BasicData.wolf")
	if err := os.MkdirAll(filepath.Dir(archive), 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{
		"CommonEvent.dat": parity(t, "CommonEvent.dat"),
		"Game.dat":        parity(t, "Game.dat"),
	}
	var list []dxa.File
	for p, d := range files {
		list = append(list, dxa.File{Path: p, Read: func() ([]byte, error) { return d, nil }})
	}
	f, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := dxa.Write(f, list); err != nil {
		t.Fatal(err)
	}
	return game, archive
}

func hashFile(t *testing.T, path string) [32]byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return sha256.Sum256(b)
}

// O jogo só com .wolf é traduzido: o texto sai em arquivos soltos, o contêiner
// original não é tocado e o backup guarda o arquivo original extraído dele.
func TestRunTranslatesPackedWolfGameIntoLooseFiles(t *testing.T) {
	game, archive := writePackedGame(t)
	before := hashFile(t, archive)
	original := parity(t, "CommonEvent.dat")

	prov := &fakeProvider{}
	if _, err := runJob(t, baseConfig(game), newMemory(t), prov); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(prov.received()) == 0 {
		t.Fatal("nada foi enviado ao provedor: o conteúdo do contêiner não foi extraído")
	}

	loose := filepath.Join(game, "Data", "BasicData", "CommonEvent.dat")
	translated, err := os.ReadFile(loose)
	if err != nil {
		t.Fatalf("arquivo solto traduzido ausente: %v", err)
	}
	if bytes.Equal(translated, original) {
		t.Error("o CommonEvent.dat solto deveria estar traduzido")
	}
	if _, err := wolfrpg.LoadWolfCommonEvents(translated); err != nil {
		t.Errorf("arquivo traduzido inválido: %v", err)
	}
	if hashFile(t, archive) != before {
		t.Error("o contêiner .wolf do usuário não pode ser alterado")
	}

	backupFile := filepath.Join(game+"-ja", "Data", "BasicData", "CommonEvent.dat")
	if got, err := os.ReadFile(backupFile); err != nil || !bytes.Equal(got, original) {
		t.Errorf("o backup deveria guardar o original extraído do contêiner (%v)", err)
	}
	meta := metadata.Load(game)
	if meta == nil || meta.Files["Data/BasicData/CommonEvent.dat"].Status != metadata.StatusSuccess {
		t.Errorf("metadado deveria registrar o arquivo traduzido: %+v", meta)
	}
	// Game.dat mantém o título (padrão) e portanto não é gravado solto.
	if _, err := os.Stat(filepath.Join(game, "Data", "BasicData", "Game.dat")); err == nil {
		t.Error("com o título preservado e sem fonte, o Game.dat não deve ser reescrito")
	}
}
