package workspace

import (
	"errors"
	"fmt"
	"io"
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
	inputDir       string
	processDir     string
	outputDir      string
	tempDir        string
	isCustomOutput bool
}

// New inicializa uma nova área de trabalho isolada.
// Se outputDir for vazio, o destino final será atribuído ao mesmo diretório temporário
// ou pode ser especificado posteriormente no momento do commit.
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

	var absOutput string
	isCustom := false
	if outputDir != "" {
		absOutput, err = filepath.Abs(outputDir)
		if err != nil {
			_ = os.RemoveAll(tempDir)
			return nil, fmt.Errorf("failed to resolve output path: %w", err)
		}
		isCustom = true
	} else {
		absOutput = filepath.Join(tempDir, "output")
		if err := os.MkdirAll(absOutput, 0755); err != nil {
			_ = os.RemoveAll(tempDir)
			return nil, fmt.Errorf("failed to create output directory: %w", err)
		}
	}

	return &Workspace{
		inputDir:       absInput,
		processDir:     processDir,
		outputDir:      absOutput,
		tempDir:        tempDir,
		isCustomOutput: isCustom,
	}, nil
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

// CreateBackup cria uma cópia completa da pasta de entrada para backupDir.
// Utiliza hardlinks (os.Link) para eficiência máxima (zero espaço extra e zero I/O de disco para arquivos grandes de mídia),
// realizando cópia física convencional como fallback caso o sistema de arquivos não permita hardlink.
func (w *Workspace) CreateBackup(backupDir string) error {
	absBackup, err := filepath.Abs(backupDir)
	if err != nil {
		return fmt.Errorf("failed to resolve backup directory: %w", err)
	}

	return filepath.Walk(w.inputDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(w.inputDir, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return os.MkdirAll(absBackup, 0755)
		}

		target := filepath.Join(absBackup, rel)

		if info.IsDir() {
			return os.MkdirAll(target, info.Mode().Perm())
		}

		// Tenta criar hardlink primeiro
		linkErr := os.Link(path, target)
		if linkErr == nil {
			return nil
		}

		// Se o hardlink falhar (ex: cross-device EXDEV ou sistema sem suporte), copia os bytes
		return copyFile(path, target, info.Mode().Perm())
	})
}

// Commit move atomicamente todos os arquivos gerados em processDir para outputDir.
// Garante que, se a aplicação for interrompida no meio, nenhum arquivo de jogo fique corrompido.
func (w *Workspace) Commit() error {
	if err := os.MkdirAll(w.outputDir, 0755); err != nil {
		return fmt.Errorf("failed to ensure output directory: %w", err)
	}

	return filepath.Walk(w.processDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}

		rel, err := filepath.Rel(w.processDir, path)
		if err != nil {
			return err
		}

		destFile := filepath.Join(w.outputDir, rel)
		if err := os.MkdirAll(filepath.Dir(destFile), 0755); err != nil {
			return fmt.Errorf("failed to create destination directory: %w", err)
		}

		// Gravação atômica: cria um arquivo temporário no mesmo diretório de destino
		// e renomeia atomicamente via os.Rename (mesmo filesystem)
		tmpDest := fmt.Sprintf("%s.tmp.%d", destFile, os.Getpid())

		if err := copyFile(path, tmpDest, info.Mode().Perm()); err != nil {
			_ = os.Remove(tmpDest)
			return fmt.Errorf("failed to stage file %s: %w", rel, err)
		}

		if err := os.Rename(tmpDest, destFile); err != nil {
			_ = os.Remove(tmpDest)
			return fmt.Errorf("failed to atomically commit file %s: %w", rel, err)
		}

		return nil
	})
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
