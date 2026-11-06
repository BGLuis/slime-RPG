package workspace

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

var (
	ErrEmptyInputDir    = errors.New("input directory cannot be empty")
	ErrInputDirNotFound = errors.New("input directory does not exist")
	ErrInputNotADir     = errors.New("input path is not a directory")
)

// Workspace gerencia a área de trabalho temporária e a escrita atômica dos arquivos traduzidos.
type Workspace struct {
	inputDir   string
	processDir string
	outputDir  string
	tempDir    string
}

// New inicializa uma nova área de trabalho isolada.
// Se outputDir for vazio, o destino final é a própria pasta de entrada (tradução in-place).
func New(inputDir, outputDir string) (*Workspace, error) {
	if inputDir == "" {
		return nil, ErrEmptyInputDir
	}

	absInput, err := filepath.Abs(inputDir)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve input path: %w", err)
	}

	info, err := os.Stat(absInput)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: %s", ErrInputDirNotFound, absInput)
		}
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%w: %s", ErrInputNotADir, absInput)
	}

	tempDir, err := os.MkdirTemp("", "slime_ws_*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temporary workspace directory: %w", err)
	}

	processDir := filepath.Join(tempDir, "process")
	if err := os.MkdirAll(processDir, 0755); err != nil {
		_ = os.RemoveAll(tempDir)
		return nil, fmt.Errorf("failed to create process directory: %w", err)
	}

	absOutput := absInput
	if outputDir != "" {
		absOutput, err = filepath.Abs(outputDir)
		if err != nil {
			_ = os.RemoveAll(tempDir)
			return nil, fmt.Errorf("failed to resolve output path: %w", err)
		}
	}

	return &Workspace{
		inputDir:   absInput,
		processDir: processDir,
		outputDir:  absOutput,
		tempDir:    tempDir,
	}, nil
}

// IsInPlace indica se o destino final é a própria pasta de entrada.
func (w *Workspace) IsInPlace() bool {
	return filepath.Clean(w.inputDir) == filepath.Clean(w.outputDir)
}

// InputDir retorna o caminho absoluto da pasta de origem.
func (w *Workspace) InputDir() string {
	return w.inputDir
}

// ProcessDir retorna o caminho absoluto da pasta de arquivos em processamento.
func (w *Workspace) ProcessDir() string {
	return w.processDir
}

// OutputDir retorna o caminho absoluto da pasta de destino final.
func (w *Workspace) OutputDir() string {
	return w.outputDir
}

// TempDir retorna o diretório raiz temporário da sessão.
func (w *Workspace) TempDir() string {
	return w.tempDir
}

// ProcessPath retorna o caminho absoluto de um arquivo dentro da pasta de processamento.
func (w *Workspace) ProcessPath(relPath string) string {
	return filepath.Join(w.processDir, relPath)
}

// OutputPath retorna o caminho absoluto de um arquivo dentro da pasta de saída.
func (w *Workspace) OutputPath(relPath string) string {
	return filepath.Join(w.outputDir, relPath)
}

// CreateBackup cria uma cópia da pasta de entrada em backupDir usando hardlinks
// (sem espaço extra em disco), com cópia física como fallback.
func (w *Workspace) CreateBackup(backupDir string) error {
	_, err := w.EnsureBackup(backupDir, nil)
	return err
}

// EnsureBackup garante que todo arquivo da entrada exista em backupDir e devolve
// os caminhos relativos copiados nesta chamada. Arquivos aceitos por copyFilter
// (os que serão traduzidos) recebem cópia independente; os demais, hardlink.
// Arquivos já presentes no backup nunca são sobrescritos: numa segunda tradução
// in-place a entrada já contém texto traduzido, e o backup é o único original.
func (w *Workspace) EnsureBackup(backupDir string, copyFilter func(rel string) bool) ([]string, error) {
	absBackup, err := filepath.Abs(backupDir)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve backup directory: %w", err)
	}

	var created []string
	err = filepath.WalkDir(w.inputDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(w.inputDir, path)
		if err != nil {
			return err
		}
		target := filepath.Join(absBackup, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if _, err := os.Lstat(target); err == nil {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}

		if copyFilter == nil || !copyFilter(filepath.ToSlash(rel)) {
			if os.Link(path, target) == nil {
				created = append(created, rel)
				return nil
			}
		}
		if err := copyFile(path, target, info.Mode().Perm()); err != nil {
			return err
		}
		created = append(created, rel)
		return nil
	})
	return created, err
}

// Commit grava atomicamente os arquivos de processDir em outputDir e devolve os
// caminhos relativos gravados. Se a aplicação for interrompida no meio, nenhum
// arquivo de jogo fica truncado: cada destino é substituído por rename.
func (w *Workspace) Commit() ([]string, error) {
	var committed []string
	err := filepath.WalkDir(w.processDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}

		rel, err := filepath.Rel(w.processDir, path)
		if err != nil {
			return err
		}
		if err := AtomicReplace(path, filepath.Join(w.outputDir, rel)); err != nil {
			return fmt.Errorf("failed to commit file %s: %w", rel, err)
		}
		committed = append(committed, rel)
		return nil
	})
	return committed, err
}

// AtomicReplace substitui dst pelo conteúdo de src gravando um arquivo temporário
// no diretório de destino e renomeando-o, o que é atômico no mesmo filesystem.
// Por nunca reescrever o inode de dst, também preserva backups feitos por hardlink.
func AtomicReplace(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	info, err := os.Stat(src)
	if err != nil {
		return err
	}

	tmp, err := os.CreateTemp(filepath.Dir(dst), "."+filepath.Base(dst)+".tmp*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	_ = tmp.Close()

	if err := copyFile(src, tmpPath, info.Mode().Perm()); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := os.Chmod(tmpPath, info.Mode().Perm()); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, dst); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return nil
}

// CleanProcess esvazia a pasta de processamento mantendo-a existente.
func (w *Workspace) CleanProcess() error {
	entries, err := os.ReadDir(w.processDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, e := range entries {
		if err := os.RemoveAll(filepath.Join(w.processDir, e.Name())); err != nil {
			return err
		}
	}
	return nil
}

// Cleanup remove o diretório temporário da área de trabalho e seus arquivos intermediários.
func (w *Workspace) Cleanup() error {
	if w.tempDir != "" {
		return os.RemoveAll(w.tempDir)
	}
	return nil
}

func copyFile(src, dst string, perm os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}

	return out.Sync()
}
