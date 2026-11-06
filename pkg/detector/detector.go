package detector

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

var (
	ErrDangerousPath = errors.New("dangerous or restricted path")
	ErrPathNotFound  = errors.New("path does not exist")
)

// GameEngine representa a engine ou formato estruturado identificado no jogo.
type GameEngine string

const (
	EngineRPGMakerMZ    GameEngine = "RPG Maker MZ"
	EngineRPGMakerMV    GameEngine = "RPG Maker MV"
	EngineRPGMakerVXAce GameEngine = "RPG Maker VX Ace"
	EngineRPGMakerXP    GameEngine = "RPG Maker XP"
	EngineWolfRPG       GameEngine = "WOLF RPG"
	EngineCSV           GameEngine = "CSV"
	EngineJSON          GameEngine = "JSON"
	EngineUnknown       GameEngine = "Desconhecido"
)

// MetaRecord reflete o arquivo .slime/meta.json criado após cada tradução.
type MetaRecord struct {
	GameName       string   `json:"game_name"`
	Engine         string   `json:"engine"`
	SourceLang     string   `json:"source_lang"`
	TargetLang     string   `json:"target_lang"`
	LastTranslated string   `json:"last_translated"`
	FilesCount     int      `json:"files_count"`
	FilesChanged   []string `json:"files_changed,omitempty"`
}

// DetectionResult consolida as informações da Etapa 1 e Detecção.
type DetectionResult struct {
	GamePath               string     `json:"game_path"`
	GameName               string     `json:"game_name"`
	DataDir                string     `json:"data_dir"`
	Engine                 GameEngine `json:"engine"`
	ProbableLang           string     `json:"probable_lang"`
	LangDetectionConfidence int       `json:"lang_confidence"`
	HasPreviousTranslation bool       `json:"has_previous_translation"`
	PreviousMeta           *MetaRecord `json:"previous_meta,omitempty"`
	FileCount              int        `json:"file_count"`
	Description            string     `json:"description"`
}

// Detect inspeciona a pasta ou arquivo indicado e descobre engine, dataDir e idioma provável.
func Detect(targetPath string) (*DetectionResult, error) {
	cleanPath := filepath.Clean(targetPath)
	info, err := os.Stat(cleanPath)
	if err != nil {
		return nil, ErrPathNotFound
	}

	// Bloqueia diretórios perigosos ou lixeiras
	if isRestrictedPath(cleanPath) {
		return nil, ErrDangerousPath
	}

	gameName := filepath.Base(cleanPath)
	if gameName == "." || gameName == "/" {
		gameName = "Jogo"
	}

	// 1. Arquivo direto
	if !info.IsDir() {
		ext := strings.ToLower(filepath.Ext(cleanPath))
		switch ext {
		case ".csv":
			return &DetectionResult{
				GamePath:     cleanPath,
				GameName:     gameName,
				DataDir:      filepath.Dir(cleanPath),
				Engine:       EngineCSV,
				ProbableLang: "en",
				FileCount:    1,
				Description:  "Planilha de localização CSV",
			}, nil
		case ".json":
			return &DetectionResult{
				GamePath:     cleanPath,
				GameName:     gameName,
				DataDir:      filepath.Dir(cleanPath),
				Engine:       EngineJSON,
				ProbableLang: "en",
				FileCount:    1,
				Description:  "Arquivo de textos JSON",
			}, nil
		case ".mps":
			return &DetectionResult{
				GamePath:     cleanPath,
				GameName:     gameName,
				DataDir:      filepath.Dir(cleanPath),
				Engine:       EngineWolfRPG,
				ProbableLang: "ja",
				FileCount:    1,
				Description:  "Mapa binário do WOLF RPG",
			}, nil
		case ".rvdata2":
			return &DetectionResult{
				GamePath:     cleanPath,
				GameName:     gameName,
				DataDir:      filepath.Dir(cleanPath),
				Engine:       EngineRPGMakerVXAce,
				ProbableLang: "ja",
				FileCount:    1,
				Description:  "Arquivo binário RPG Maker VX Ace",
			}, nil
		}
	}

	// 2. Diretório: inspeciona candidatos a pastas de dados
	res := &DetectionResult{
		GamePath: cleanPath,
		GameName: gameName,
		DataDir:  cleanPath,
		Engine:   EngineUnknown,
	}

	// Verifica se há tradução anterior em .slime/meta.json
	metaFile := filepath.Join(cleanPath, ".slime", "meta.json")
	if metaBytes, err := os.ReadFile(metaFile); err == nil {
		var meta MetaRecord
		if json.Unmarshal(metaBytes, &meta) == nil {
			res.HasPreviousTranslation = true
			res.PreviousMeta = &meta
			if meta.GameName != "" {
				res.GameName = meta.GameName
			}
		}
	}

	// Detecta RPG Maker MV / MZ
	candidatesMVMZ := []string{
		filepath.Join(cleanPath, "www", "data"),
		filepath.Join(cleanPath, "data"),
		cleanPath,
	}
	for _, dir := range candidatesMVMZ {
		if hasFile(dir, "System.json") {
			res.DataDir = dir
			if hasFile(filepath.Join(cleanPath, "js"), "rmmz_core.js") || hasFile(cleanPath, "package.json") {
				res.Engine = EngineRPGMakerMZ
			} else {
				res.Engine = EngineRPGMakerMV
			}
			res.FileCount = countFiles(dir, ".json")
			res.ProbableLang, res.LangDetectionConfidence = sampleLanguage(dir, ".json")
			res.Description = string(res.Engine) + " · dados em " + filepath.Base(dir)
			return res, nil
		}
	}

	// Detecta WOLF RPG
	candidatesWolf := []string{
		filepath.Join(cleanPath, "Data", "MapData"),
		filepath.Join(cleanPath, "Data", "BasicData"),
		filepath.Join(cleanPath, "Data"),
		cleanPath,
	}
	for _, dir := range candidatesWolf {
		if hasFile(dir, "CommonEvent.dat") || hasFile(dir, "DataBase.dat") || countFiles(dir, ".mps") > 0 || countFiles(dir, ".wolf") > 0 {
			res.DataDir = dir
			res.Engine = EngineWolfRPG
			res.FileCount = countFiles(dir, ".mps") + countFiles(dir, ".dat") + countFiles(dir, ".wolf")
			res.ProbableLang = "ja"
			res.LangDetectionConfidence = 95
			res.Description = "WOLF RPG · dados em " + filepath.Base(dir)
			return res, nil
		}
	}

	// Detecta RPG Maker VX Ace / XP / VX
	candidatesRV := []string{
		filepath.Join(cleanPath, "Data"),
		cleanPath,
	}
	for _, dir := range candidatesRV {
		if countFiles(dir, ".rvdata2") > 0 {
			res.DataDir = dir
			res.Engine = EngineRPGMakerVXAce
			res.FileCount = countFiles(dir, ".rvdata2")
			res.ProbableLang = "ja"
			res.LangDetectionConfidence = 95
			res.Description = "RPG Maker VX Ace · dados em " + filepath.Base(dir)
			return res, nil
		}
		if countFiles(dir, ".rxdata") > 0 {
			res.DataDir = dir
			res.Engine = EngineRPGMakerXP
			res.FileCount = countFiles(dir, ".rxdata")
			res.ProbableLang = "ja"
			res.LangDetectionConfidence = 95
			res.Description = "RPG Maker XP · dados em " + filepath.Base(dir)
			return res, nil
		}
	}

	// Detecta CSVs ou JSONs soltos
	csvCount := countFiles(cleanPath, ".csv")
	if csvCount > 0 {
		res.Engine = EngineCSV
		res.FileCount = csvCount
		res.ProbableLang = "en"
		res.Description = "Diretório de planilhas CSV"
		return res, nil
	}

	jsonCount := countFiles(cleanPath, ".json")
	if jsonCount > 0 {
		res.Engine = EngineJSON
		res.FileCount = jsonCount
		res.ProbableLang = "en"
		res.Description = "Diretório de arquivos JSON"
		return res, nil
	}

	return res, nil
}

// isRestrictedPath recusa a raiz do sistema, lixeiras (com os marcadores da
// versão Python, mais a lixeira do Windows) e o próprio repositório do Slime.
func isRestrictedPath(path string) bool {
	if path == "/" || path == "\\" || len(path) <= 3 && strings.HasSuffix(path, ":\\") {
		return true
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	lower := strings.ToLower(filepath.ToSlash(abs)) + "/"
	for _, marker := range []string{"/.trash/", "/.trash-", "/.local/share/trash/", "/trash/", "/$recycle.bin/"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return isSlimeRepoRoot(abs)
}

// isSlimeRepoRoot evita "traduzir" o código-fonte do próprio Slime quando ele é
// executado sem argumentos na raiz do repositório.
func isSlimeRepoRoot(dir string) bool {
	data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		return false
	}
	return strings.Contains(string(data), "module github.com/BGLuis/slime-rpg") && hasFile(filepath.Join(dir, "cmd"), "slime")
}

func hasFile(dir, filename string) bool {
	_, err := os.Stat(filepath.Join(dir, filename))
	return err == nil
}

func countFiles(dir, ext string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	count := 0
	for _, e := range entries {
		if !e.IsDir() && strings.EqualFold(filepath.Ext(e.Name()), ext) {
			count++
		}
	}
	return count
}

func sampleLanguage(dir, ext string) (string, int) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "en", 50
	}

	cjkCount := 0
	latinCount := 0

	for _, e := range entries {
		if !e.IsDir() && strings.EqualFold(filepath.Ext(e.Name()), ext) {
			data, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil {
				continue
			}
			sample := string(data)
			if len(sample) > 5000 {
				sample = sample[:5000]
			}
			for _, r := range sample {
				if unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hiragana, r) || unicode.Is(unicode.Katakana, r) {
					cjkCount++
				} else if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
					latinCount++
				}
			}
			if cjkCount >= 5 || latinCount > 50 {
				break
			}
		}
	}

	if cjkCount >= 5 {
		return "ja", 96
	}
	return "en", 80
}
