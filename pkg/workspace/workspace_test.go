package workspace_test

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/BGLuis/slime-rpg/pkg/workspace"
)

func newWorkspace(t *testing.T, input, output string) *workspace.Workspace {
	t.Helper()
	ws, err := workspace.New(input, output)
	if err != nil {
		t.Fatalf("falha ao criar workspace: %v", err)
	}
	t.Cleanup(func() { _ = ws.Cleanup() })
	return ws
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("falha ao ler %s: %v", path, err)
	}
	return string(b)
}

func TestNewRejectsInvalidInput(t *testing.T) {
	if _, err := workspace.New("", ""); !errors.Is(err, workspace.ErrEmptyInputDir) {
		t.Errorf("entrada vazia: esperava ErrEmptyInputDir, obteve %v", err)
	}
	if _, err := workspace.New(filepath.Join(t.TempDir(), "nao-existe"), ""); !errors.Is(err, workspace.ErrInputDirNotFound) {
		t.Errorf("entrada inexistente: esperava ErrInputDirNotFound, obteve %v", err)
	}
	file := filepath.Join(t.TempDir(), "arquivo.txt")
	writeFile(t, file, "x")
	if _, err := workspace.New(file, ""); !errors.Is(err, workspace.ErrInputNotADir) {
		t.Errorf("entrada arquivo: esperava ErrInputNotADir, obteve %v", err)
	}
}

func TestDefaultOutputIsInPlace(t *testing.T) {
	input := t.TempDir()
	ws := newWorkspace(t, input, "")

	if ws.InputDir() != input || ws.OutputDir() != input {
		t.Errorf("sem saída explícita o destino deve ser a entrada: input=%q output=%q", ws.InputDir(), ws.OutputDir())
	}
	if !ws.IsInPlace() {
		t.Error("IsInPlace deveria ser true")
	}
	if _, err := os.Stat(ws.ProcessDir()); err != nil {
		t.Errorf("processDir deveria existir: %v", err)
	}
}

// Sem saída explícita a tradução precisa sobreviver ao Cleanup: antes o destino
// padrão ficava dentro do diretório temporário e era apagado junto com ele.
func TestInPlaceCommitSurvivesCleanup(t *testing.T) {
	input := t.TempDir()
	writeFile(t, filepath.Join(input, "Map001.json"), "original")

	ws, err := workspace.New(input, "")
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, ws.ProcessPath("Map001.json"), "traduzido")
	if _, err := ws.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := ws.Cleanup(); err != nil {
		t.Fatal(err)
	}

	if got := readFile(t, filepath.Join(input, "Map001.json")); got != "traduzido" {
		t.Errorf("tradução perdida após Cleanup, conteúdo: %q", got)
	}
}

func TestSeparateOutput(t *testing.T) {
	input := t.TempDir()
	output := filepath.Join(t.TempDir(), "translated")
	ws := newWorkspace(t, input, output)

	if ws.OutputDir() != output {
		t.Errorf("esperava output %q, obteve %q", output, ws.OutputDir())
	}
	if ws.IsInPlace() {
		t.Error("IsInPlace deveria ser false")
	}
}

func TestCommitWritesNestedFilesAndReportsThem(t *testing.T) {
	output := filepath.Join(t.TempDir(), "output_game")
	ws := newWorkspace(t, t.TempDir(), output)

	writeFile(t, ws.ProcessPath("map.json"), `{"id": 1}`)
	writeFile(t, ws.ProcessPath(filepath.Join("sub", "event.json")), `{"event": "start"}`)

	committed, err := ws.Commit()
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(committed)
	if want := []string{"map.json", filepath.Join("sub", "event.json")}; !slices.Equal(committed, want) {
		t.Errorf("arquivos commitados = %v, esperava %v", committed, want)
	}
	if got := readFile(t, filepath.Join(output, "map.json")); got != `{"id": 1}` {
		t.Errorf("conteúdo inesperado: %q", got)
	}
	if got := readFile(t, filepath.Join(output, "sub", "event.json")); got != `{"event": "start"}` {
		t.Errorf("conteúdo inesperado: %q", got)
	}
}

func TestCommitPreservesUnrelatedOutputFiles(t *testing.T) {
	output := t.TempDir()
	writeFile(t, filepath.Join(output, "keep_me.txt"), "keep")
	ws := newWorkspace(t, t.TempDir(), output)
	writeFile(t, ws.ProcessPath("map.json"), "x")

	if _, err := ws.Commit(); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, filepath.Join(output, "keep_me.txt")); got != "keep" {
		t.Errorf("arquivo fora do processamento não pode ser alterado: %q", got)
	}
}

func TestAtomicReplace(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "new.txt")
	dst := filepath.Join(dir, "target_sub", "file.txt")

	writeFile(t, src, "novo conteúdo")
	if err := workspace.AtomicReplace(src, dst); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, dst); got != "novo conteúdo" {
		t.Errorf("criação: %q", got)
	}

	writeFile(t, src, "conteúdo atualizado")
	if err := workspace.AtomicReplace(src, dst); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, dst); got != "conteúdo atualizado" {
		t.Errorf("substituição: %q", got)
	}

	entries, _ := os.ReadDir(filepath.Dir(dst))
	if len(entries) != 1 {
		t.Errorf("temporários não podem sobrar no destino: %v", entries)
	}
}

func TestCleanProcessEmptiesButKeepsDir(t *testing.T) {
	ws := newWorkspace(t, t.TempDir(), "")
	writeFile(t, ws.ProcessPath("test.txt"), "temporary data")
	writeFile(t, ws.ProcessPath(filepath.Join("sub", "x.txt")), "x")

	if err := ws.CleanProcess(); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(ws.ProcessDir())
	if err != nil {
		t.Fatalf("processDir deveria continuar existindo: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("processDir deveria estar vazio: %v", entries)
	}
}

func TestCleanupIsIdempotent(t *testing.T) {
	ws, err := workspace.New(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	if err := ws.Cleanup(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(ws.TempDir()); !os.IsNotExist(err) {
		t.Error("tempDir deveria ter sido removido")
	}
	if err := ws.Cleanup(); err != nil {
		t.Errorf("segundo Cleanup não deve falhar: %v", err)
	}
}

func TestBackupCopiesTree(t *testing.T) {
	input := t.TempDir()
	writeFile(t, filepath.Join(input, "data", "map.json"), `{"name": "test"}`)
	ws := newWorkspace(t, input, "")

	backup := filepath.Join(t.TempDir(), "game-backup")
	if err := ws.CreateBackup(backup); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, filepath.Join(backup, "data", "map.json")); got != `{"name": "test"}` {
		t.Errorf("conteúdo do backup incorreto: %q", got)
	}
}

// O backup usa hardlinks: ele só continua íntegro se o commit substituir o
// arquivo por rename em vez de reescrever o inode compartilhado.
func TestBackupSurvivesInPlaceCommit(t *testing.T) {
	input := t.TempDir()
	writeFile(t, filepath.Join(input, "Map001.json"), "original")
	ws := newWorkspace(t, input, "")

	backup := filepath.Join(t.TempDir(), "backup")
	if err := ws.CreateBackup(backup); err != nil {
		t.Fatal(err)
	}
	writeFile(t, ws.ProcessPath("Map001.json"), "traduzido")
	if _, err := ws.Commit(); err != nil {
		t.Fatal(err)
	}

	if got := readFile(t, filepath.Join(input, "Map001.json")); got != "traduzido" {
		t.Errorf("entrada deveria conter a tradução: %q", got)
	}
	if got := readFile(t, filepath.Join(backup, "Map001.json")); got != "original" {
		t.Errorf("backup foi corrompido pelo commit: %q", got)
	}
}
