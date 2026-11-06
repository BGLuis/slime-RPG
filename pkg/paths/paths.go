package paths

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// AppName é o identificador padrão da aplicação nos diretórios do sistema.
const AppName = "slime-rpg"

var (
	ErrHomeDirNotFound = errors.New("failed to determine user home directory")
)

// DataDir retorna o diretório de dados persistentes do usuário para o Slime RPG.
// Respeita a variável de ambiente SLIME_DATA_DIR.
// - Linux: $XDG_DATA_HOME/slime-rpg ou ~/.local/share/slime-rpg
// - Windows: %APPDATA%\slime-rpg
// - macOS: ~/Library/Application Support/slime-rpg
func DataDir() (string, error) {
	if env := os.Getenv("SLIME_DATA_DIR"); env != "" {
		abs, err := filepath.Abs(env)
		if err != nil {
			return "", fmt.Errorf("invalid SLIME_DATA_DIR: %w", err)
		}
		return abs, nil
	}

	switch runtime.GOOS {
	case "windows":
		configDir, err := os.UserConfigDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(configDir, AppName), nil

	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("%w: %v", ErrHomeDirNotFound, err)
		}
		return filepath.Join(home, "Library", "Application Support", AppName), nil

	default: // Linux, BSD, etc.
		if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
			return filepath.Join(xdg, AppName), nil
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("%w: %v", ErrHomeDirNotFound, err)
		}
		return filepath.Join(home, ".local", "share", AppName), nil
	}
}

// ConfigDir retorna o diretório de configurações do usuário.
// Respeita a variável SLIME_CONFIG_DIR ou os.UserConfigDir().
func ConfigDir() (string, error) {
	if env := os.Getenv("SLIME_CONFIG_DIR"); env != "" {
		abs, err := filepath.Abs(env)
		if err != nil {
			return "", fmt.Errorf("invalid SLIME_CONFIG_DIR: %w", err)
		}
		return abs, nil
	}

	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, AppName), nil
}

// CacheDir retorna o diretório de cache do usuário.
// Respeita a variável SLIME_CACHE_DIR ou os.UserCacheDir().
func CacheDir() (string, error) {
	if env := os.Getenv("SLIME_CACHE_DIR"); env != "" {
		abs, err := filepath.Abs(env)
		if err != nil {
			return "", fmt.Errorf("invalid SLIME_CACHE_DIR: %w", err)
		}
		return abs, nil
	}

	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, AppName), nil
}

// LogDir retorna o diretório onde os arquivos de log devem ser gravados.
// Respeita SLIME_LOG_DIR ou subpasta logs dentro de DataDir.
func LogDir() (string, error) {
	if env := os.Getenv("SLIME_LOG_DIR"); env != "" {
		abs, err := filepath.Abs(env)
		if err != nil {
			return "", fmt.Errorf("invalid SLIME_LOG_DIR: %w", err)
		}
		return abs, nil
	}

	if runtime.GOOS == "linux" {
		if xdgState := os.Getenv("XDG_STATE_HOME"); xdgState != "" {
			return filepath.Join(xdgState, AppName, "log"), nil
		}
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, ".local", "state", AppName, "log"), nil
		}
	}

	data, err := DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(data, "logs"), nil
}

// MemoryDBPath resolve o caminho canônico para o arquivo SQLite memory.db:
// 1. Variável de ambiente SLIME_MEMORY_DB (prioridade máxima).
// 2. Modo Portátil: se existir ./cache/memory.db na pasta atual, utiliza-o.
// 3. Caminho padrão do sistema operacional em DataDir()/memory.db.
func MemoryDBPath() (string, error) {
	if env := os.Getenv("SLIME_MEMORY_DB"); env != "" {
		abs, err := filepath.Abs(env)
		if err != nil {
			return "", fmt.Errorf("invalid SLIME_MEMORY_DB: %w", err)
		}
		return abs, nil
	}

	// Verifica se há uma base local legada ou portátil no diretório corrente
	cwd, err := os.Getwd()
	if err == nil {
		localCandidate := filepath.Join(cwd, "cache", "memory.db")
		if fi, err := os.Stat(localCandidate); err == nil && !fi.IsDir() {
			return localCandidate, nil
		}
	}

	dataDir, err := DataDir()
	if err != nil {
		return "", err
	}

	return filepath.Join(dataDir, "memory.db"), nil
}

// LogFilePath resolve o caminho completo para o arquivo processamento.log.
// Respeita SLIME_LOG_FILE ou LogDir()/processamento.log.
func LogFilePath() (string, error) {
	if env := os.Getenv("SLIME_LOG_FILE"); env != "" {
		abs, err := filepath.Abs(env)
		if err != nil {
			return "", fmt.Errorf("invalid SLIME_LOG_FILE: %w", err)
		}
		return abs, nil
	}

	logDir, err := LogDir()
	if err != nil {
		return "", err
	}

	return filepath.Join(logDir, "processamento.log"), nil
}

// EnsureDir garante que o diretório especificado exista criando-o se necessário.
func EnsureDir(path string) error {
	return os.MkdirAll(path, 0755)
}

// EnsureFileDir garante que o diretório pai do arquivo especificado exista.
func EnsureFileDir(filePath string) error {
	dir := filepath.Dir(filePath)
	return EnsureDir(dir)
}
