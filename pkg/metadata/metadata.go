// Package metadata registra, em .slime/meta.json, o estado de cada arquivo
// traduzido em relação ao original. O formato é o mesmo da versão Python
// (services/GameMetadata.py), para que as duas versões leiam o histórico uma da outra.
package metadata

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

const (
	SchemaVersion = 1
	DirName       = ".slime"
	FileName      = "meta.json"

	OriginJob    = "job"
	OriginLegacy = "legacy-adopted"
)

// Estados de um arquivo na verificação.
const (
	StateIntact             = "intact"
	StateUnchanged          = "unchanged"
	StateEdited             = "edited"
	StateOriginalChanged    = "original_changed"
	StateOriginalMissing    = "original_missing"
	StateTranslationMissing = "translation_missing"
	StatePartial            = "partial"
	StateNew                = "new"
)

// Status de tradução gravados por arquivo.
const (
	StatusSuccess   = "success"
	StatusPartial   = "partial"
	StatusUnchanged = "unchanged"
)

// File descreve um arquivo registrado.
type File struct {
	OriginalSHA256   *string `json:"original_sha256"`
	TranslatedSHA256 string  `json:"translated_sha256"`
	Status           string  `json:"status"`
}

// Meta é o conteúdo de .slime/meta.json.
type Meta struct {
	SchemaVersion int             `json:"schema_version"`
	Origin        string          `json:"origin"`
	CreatedAt     string          `json:"created_at"`
	UpdatedAt     string          `json:"updated_at"`
	Extractor     *string         `json:"extractor"`
	Translator    *string         `json:"translator"`
	LangSource    *string         `json:"lang_source"`
	LangTarget    *string         `json:"lang_target"`
	OriginalDir   *string         `json:"original_dir"`
	Extensions    []string        `json:"extensions"`
	Files         map[string]File `json:"files"`

	// Campos extras da versão Go; a versão Python ignora chaves desconhecidas.
	GameName string `json:"game_name,omitempty"`
	Engine   string `json:"engine,omitempty"`
}

// Report é o resultado da verificação de um arquivo.
type Report struct {
	File  string `json:"file"`
	State string `json:"state"`
}

// Filter decide quais arquivos (caminho relativo com '/') entram no metadado.
// Nil aceita todos.
type Filter func(rel string) bool

// ExtensionFilter aceita arquivos com uma das extensões (com ou sem ponto).
func ExtensionFilter(extensions []string) Filter {
	exts := NormalizeExtensions(extensions)
	if len(exts) == 0 {
		return nil
	}
	return func(rel string) bool {
		lower := strings.ToLower(rel)
		for _, e := range exts {
			if strings.HasSuffix(lower, e) {
				return true
			}
		}
		return false
	}
}

// NormalizeExtensions devolve as extensões em minúsculas, com ponto, ordenadas.
func NormalizeExtensions(extensions []string) []string {
	out := make([]string, 0, len(extensions))
	for _, e := range extensions {
		e = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(e), "."))
		if e != "" {
			out = append(out, "."+e)
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// Path devolve o caminho do metadado de uma pasta traduzida.
func Path(translatedDir string) string {
	return filepath.Join(translatedDir, DirName, FileName)
}

// SHA256File calcula o hash do arquivo; devolve "" se não puder lê-lo.
func SHA256File(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return ""
	}
	return hex.EncodeToString(h.Sum(nil))
}

// ScanHashes calcula o hash de cada arquivo aceito pelo filtro sob root,
// indexado pelo caminho relativo com '/'. A pasta .slime é ignorada.
func ScanHashes(root string, filter Filter) map[string]string {
	hashes := make(map[string]string)
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if d.Name() == DirName && path != root {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if filter != nil && !filter(rel) {
			return nil
		}
		if digest := SHA256File(path); digest != "" {
			hashes[rel] = digest
		}
		return nil
	})
	return hashes
}

// Load lê o metadado; devolve nil se não existir, estiver corrompido ou vier de
// uma versão de schema mais nova.
func Load(translatedDir string) *Meta {
	data, err := os.ReadFile(Path(translatedDir))
	if err != nil {
		return nil
	}
	var m Meta
	if json.Unmarshal(data, &m) != nil || m.SchemaVersion > SchemaVersion {
		return nil
	}
	if m.Files == nil {
		m.Files = map[string]File{}
	}
	return &m
}

// Save grava o metadado de forma atômica, para não deixar um JSON truncado se o
// processo cair no meio da escrita.
func Save(translatedDir string, m *Meta) (string, error) {
	path := Path(translatedDir)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".meta_*.tmp")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	return path, os.Rename(tmp.Name(), path)
}

// ResolveOriginalDir devolve o caminho absoluto da pasta original registrada.
func ResolveOriginalDir(translatedDir string, m *Meta) string {
	if m == nil || m.OriginalDir == nil || *m.OriginalDir == "" {
		return ""
	}
	return filepath.Clean(filepath.Join(translatedDir, filepath.FromSlash(*m.OriginalDir)))
}

// BuildOptions descreve a execução que gerou o metadado.
type BuildOptions struct {
	Origin     string
	Extractor  string
	Translator string
	LangSource string
	LangTarget string
	Extensions []string
	Filter     Filter            // nil usa as extensões
	FileStatus map[string]string // status por arquivo vindo do job
	Previous   *Meta
	GameName   string
	Engine     string
	Now        func() time.Time
}

// Build cruza a pasta original e a traduzida. Sem FileStatus (adoção de uma
// tradução legada), todo arquivo que difere do original conta como traduzido.
func Build(translatedDir, originalDir string, opts BuildOptions) *Meta {
	filter := opts.Filter
	if filter == nil {
		filter = ExtensionFilter(opts.Extensions)
	}
	now := time.Now
	if opts.Now != nil {
		now = opts.Now
	}
	stamp := now().UTC().Format("2006-01-02T15:04:05+00:00")

	translated := ScanHashes(translatedDir, filter)
	prevStatus := func(rel string) string {
		if opts.Previous == nil {
			return ""
		}
		return opts.Previous.Files[rel].Status
	}

	files := make(map[string]File)
	var originalRel *string
	if isDir(originalDir) {
		if rel, err := filepath.Rel(translatedDir, originalDir); err == nil {
			rel = filepath.ToSlash(rel)
			originalRel = &rel
		}
		for rel, origHash := range ScanHashes(originalDir, filter) {
			transHash, ok := translated[rel]
			if !ok {
				continue
			}
			status := firstNonEmpty(opts.FileStatus[rel], prevStatus(rel))
			if status == "" {
				status = StatusUnchanged
				if transHash != origHash {
					status = StatusSuccess
				}
			}
			files[rel] = File{OriginalSHA256: ptr(origHash), TranslatedSHA256: transHash, Status: status}
		}
	} else {
		for rel, transHash := range translated {
			status := firstNonEmpty(opts.FileStatus[rel], prevStatus(rel), StatusSuccess)
			files[rel] = File{TranslatedSHA256: transHash, Status: status}
		}
	}

	createdAt := stamp
	if opts.Previous != nil && opts.Previous.CreatedAt != "" {
		createdAt = opts.Previous.CreatedAt
	}
	return &Meta{
		SchemaVersion: SchemaVersion,
		Origin:        opts.Origin,
		CreatedAt:     createdAt,
		UpdatedAt:     stamp,
		Extractor:     optional(opts.Extractor),
		Translator:    optional(opts.Translator),
		LangSource:    optional(opts.LangSource),
		LangTarget:    optional(opts.LangTarget),
		OriginalDir:   originalRel,
		Extensions:    NormalizeExtensions(opts.Extensions),
		Files:         files,
		GameName:      opts.GameName,
		Engine:        opts.Engine,
	}
}

// ErrNoMeta indica que a pasta não tem metadado.
var ErrNoMeta = errors.New("no .slime/meta.json found")

// Verify classifica cada arquivo registrado e aponta os arquivos novos no
// original. originalDir vazio usa a pasta registrada no metadado. filter nil
// usa as extensões registradas.
func Verify(translatedDir, originalDir string, filter Filter) ([]Report, error) {
	m := Load(translatedDir)
	if m == nil {
		return nil, ErrNoMeta
	}
	if originalDir == "" {
		originalDir = ResolveOriginalDir(translatedDir, m)
	}
	if filter == nil {
		filter = ExtensionFilter(m.Extensions)
	}

	rels := make([]string, 0, len(m.Files))
	for rel := range m.Files {
		rels = append(rels, rel)
	}
	slices.Sort(rels)

	hasOriginal := isDir(originalDir)
	report := make([]Report, 0, len(rels))
	for _, rel := range rels {
		entry := m.Files[rel]
		translatedNow := SHA256File(filepath.Join(translatedDir, filepath.FromSlash(rel)))

		var state string
		switch {
		case entry.OriginalSHA256 == nil:
			// Arquivo registrado sem pasta de backup (--no-backup).
			state = translatedState(translatedNow, entry)
		default:
			originalNow := ""
			if hasOriginal {
				originalNow = SHA256File(filepath.Join(originalDir, filepath.FromSlash(rel)))
			}
			switch {
			case originalNow == "":
				state = StateOriginalMissing
			case originalNow != *entry.OriginalSHA256:
				state = StateOriginalChanged
			default:
				state = translatedState(translatedNow, entry)
			}
		}
		report = append(report, Report{File: rel, State: state})
	}

	if hasOriginal {
		current := ScanHashes(originalDir, filter)
		var added []string
		for rel := range current {
			if _, known := m.Files[rel]; !known {
				added = append(added, rel)
			}
		}
		slices.Sort(added)
		for _, rel := range added {
			report = append(report, Report{File: rel, State: StateNew})
		}
	}
	return report, nil
}

// NeedsTranslation indica se o estado exige retraduzir o arquivo no modo incremental.
func NeedsTranslation(state string) bool {
	switch state {
	case StateOriginalChanged, StateNew, StatePartial, StateTranslationMissing:
		return true
	}
	return false
}

// Summarize conta os arquivos por estado.
func Summarize(report []Report) map[string]int {
	summary := make(map[string]int)
	for _, r := range report {
		summary[r.State]++
	}
	return summary
}

func translatedState(translatedNow string, entry File) string {
	switch {
	case translatedNow == "":
		return StateTranslationMissing
	case translatedNow != entry.TranslatedSHA256:
		return StateEdited
	case entry.Status == StatusPartial:
		return StatePartial
	case entry.Status == StatusUnchanged:
		return StateUnchanged
	default:
		return StateIntact
	}
}

func isDir(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func ptr(s string) *string { return &s }

func optional(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
