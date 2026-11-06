package workspace_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/BGLuis/slime-rpg/pkg/workspace"
)

func TestWorkspace_Creation(t *testing.T) {
	// Erro com diretório vazio
	_, err := workspace.New("", "")
	if err == nil {
		t.Fatal("esperava erro ao passar diretório de entrada vazio")
	}

	// Erro com diretório inexistente
	_, err = workspace.New("/caminho/completamente/inexistente/12345", "")
	if err == nil {
		t.Fatal("esperava erro ao passar diretório inexistente")
	}

	// Sucesso com diretório temporário válido
	tempInput, err := os.MkdirTemp("", "test_input_*")
	if err != nil {
		t.Fatalf("falha ao criar input temp: %v", err)
	}
	defer os.RemoveAll(tempInput)

	ws, err := workspace.New(tempInput, "")
	if err != nil {
		t.Fatalf("falha ao instanciar Workspace: %v", err)
	}
	defer ws.Cleanup()

	if ws.InputDir() != tempInput {
		t.Errorf("esperava inputDir %q, obteve %q", tempInput, ws.InputDir())
	}
	if ws.ProcessDir() == "" || ws.OutputDir() == "" {
		t.Errorf("processDir e outputDir devem ser preenchidos")
	}
}

func TestWorkspace_Backup(t *testing.T) {
	tempInput, err := os.MkdirTemp("", "test_input_*")
	if err != nil {
		t.Fatalf("falha ao criar input temp: %v", err)
	}
	defer os.RemoveAll(tempInput)

	// Cria estrutura de arquivos de teste
	subDir := filepath.Join(tempInput, "data")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatalf("falha ao criar subpasta: %v", err)
	}
	testFile := filepath.Join(subDir, "map.json")
	if err := os.WriteFile(testFile, []byte(`{"name": "test"}`), 0644); err != nil {
		t.Fatalf("falha ao criar arquivo de teste: %v", err)
	}

	ws, err := workspace.New(tempInput, "")
	if err != nil {
		t.Fatalf("falha ao criar workspace: %v", err)
	}
	defer ws.Cleanup()

	tempBackup, err := os.MkdirTemp("", "test_backup_*")
	if err != nil {
		t.Fatalf("falha ao criar backup temp: %v", err)
	}
	defer os.RemoveAll(tempBackup)

	backupDir := filepath.Join(tempBackup, "game-backup")
	if err := ws.CreateBackup(backupDir); err != nil {
		t.Fatalf("falha ao criar backup: %v", err)
	}

	backedFile := filepath.Join(backupDir, "data", "map.json")
	content, err := os.ReadFile(backedFile)
	if err != nil {
		t.Fatalf("falha ao ler arquivo do backup: %v", err)
	}
	if string(content) != `{"name": "test"}` {
		t.Errorf("conteúdo do backup incorreto: %s", string(content))
	}
}

func TestWorkspace_Commit(t *testing.T) {
	tempInput, err := os.MkdirTemp("", "test_input_*")
	if err != nil {
		t.Fatalf("falha ao criar input temp: %v", err)
	}
	defer os.RemoveAll(tempInput)

	tempOutput, err := os.MkdirTemp("", "test_output_*")
	if err != nil {
		t.Fatalf("falha ao criar output temp: %v", err)
	}
	defer os.RemoveAll(tempOutput)

	ws, err := workspace.New(tempInput, tempOutput)
	if err != nil {
		t.Fatalf("falha ao criar workspace: %v", err)
	}
	defer ws.Cleanup()

	// Escreve um arquivo simulando o processo na pasta process/
	procFile := ws.ProcessPath(filepath.Join("data", "translated.json"))
	if err := os.MkdirAll(filepath.Dir(procFile), 0755); err != nil {
		t.Fatalf("falha ao criar subpasta em process: %v", err)
	}
	if err := os.WriteFile(procFile, []byte(`{"text": "olá"}`), 0644); err != nil {
		t.Fatalf("falha ao gravar arquivo em process: %v", err)
	}

	// Executa o commit
	if err := ws.Commit(); err != nil {
		t.Fatalf("falha ao executar commit: %v", err)
	}

	// Verifica se o arquivo foi atômica e corretamente transferido para outputDir
	committedFile := ws.OutputPath(filepath.Join("data", "translated.json"))
	content, err := os.ReadFile(committedFile)
	if err != nil {
		t.Fatalf("arquivo não encontrado no output após commit: %v", err)
	}
	if string(content) != `{"text": "olá"}` {
		t.Errorf("conteúdo inesperado no arquivo commitado: %s", string(content))
	}
}

func TestWorkspace_Cleanup(t *testing.T) {
	tempInput, err := os.MkdirTemp("", "test_input_*")
	if err != nil {
		t.Fatalf("falha ao criar input temp: %v", err)
	}
	defer os.RemoveAll(tempInput)

	ws, err := workspace.New(tempInput, "")
	if err != nil {
		t.Fatalf("falha ao criar workspace: %v", err)
	}

	tempDir := ws.TempDir()
	if _, err := os.Stat(tempDir); os.IsNotExist(err) {
		t.Fatal("tempDir deveria existir antes do cleanup")
	}

	if err := ws.Cleanup(); err != nil {
		t.Fatalf("falha ao executar cleanup: %v", err)
	}

	if _, err := os.Stat(tempDir); !os.IsNotExist(err) {
		t.Errorf("tempDir deveria ter sido removido após cleanup")
	}
}
