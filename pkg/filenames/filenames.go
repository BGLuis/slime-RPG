// Package filenames traduz nomes de arquivos de uma pasta (modo "filenames" da
// versão Python): cada arquivo é copiado para a saída com o nome traduzido,
// mantendo extensão e subpastas.
package filenames

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// Translator traduz um lote de textos (pipeline.Translator ou engine.Engine).
type Translator interface {
	TranslateBatch(ctx context.Context, texts []string, srcLang, tgtLang string) ([]string, error)
}

// Result resume a execução.
type Result struct {
	TotalFiles         int
	TranslatedCount    int
	CopiedOriginals    int
	CollisionsResolved int
	OutputDir          string
	FilesMap           map[string]string // origem -> destino
}

// Options configura a tradução.
type Options struct {
	SrcLang, TgtLang string
	// KeepOriginal também copia o arquivo com o nome original para a saída.
	KeepOriginal bool
	Log          func(msg string)
}

var (
	invalidChars    = regexp.MustCompile(`[/\\<>:"|?*\x00-\x1f]`)
	windowsReserved = []string{
		"CON", "PRN", "AUX", "NUL",
		"COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9",
		"LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9",
	}

	// ErrTranslationFailed indica que o tradutor não devolveu um nome por arquivo.
	ErrTranslationFailed = errors.New("failed to translate file names")
)

// Sanitize torna o nome válido em qualquer sistema: troca separadores e
// caracteres proibidos por "_", remove espaços e pontos das pontas e prefixa
// nomes reservados do Windows (CON, NUL, COM1...).
func Sanitize(name, ext string) string {
	stem := strings.Trim(invalidChars.ReplaceAllString(name, "_"), " .")
	if stem == "" {
		stem = "unnamed"
	}
	upper := strings.ToUpper(stem)
	if slices.Contains(windowsReserved, upper) || slices.Contains(windowsReserved, strings.SplitN(upper, ".", 2)[0]) {
		stem = "_" + stem
	}
	return stem + invalidChars.ReplaceAllString(ext, "_")
}

type fileInfo struct {
	src, relDir, name, stem, ext string
}

// Process traduz os nomes dos arquivos de inputDir e grava as cópias em outputDir.
func Process(ctx context.Context, tr Translator, inputDir, outputDir string, opts Options) (*Result, error) {
	logf := func(format string, args ...any) {
		if opts.Log != nil {
			opts.Log(fmt.Sprintf(format, args...))
		}
	}
	res := &Result{OutputDir: outputDir, FilesMap: map[string]string{}}
	if info, err := os.Stat(inputDir); err != nil || !info.IsDir() {
		return res, fmt.Errorf("input folder not found: %s", inputDir)
	}

	var files []fileInfo
	err := filepath.WalkDir(inputDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(inputDir, filepath.Dir(path))
		if err != nil {
			return err
		}
		if rel == "." {
			rel = ""
		}
		ext := filepath.Ext(d.Name())
		files = append(files, fileInfo{src: path, relDir: rel, name: d.Name(), stem: strings.TrimSuffix(d.Name(), ext), ext: ext})
		return nil
	})
	if err != nil {
		return res, err
	}
	res.TotalFiles = len(files)
	if len(files) == 0 {
		logf("Nenhum arquivo encontrado na pasta ou subpastas.")
		return res, nil
	}

	logf("Traduzindo %d nomes de arquivos...", len(files))
	stems := make([]string, len(files))
	for i, f := range files {
		stems[i] = f.stem
	}
	translated, err := tr.TranslateBatch(ctx, stems, opts.SrcLang, opts.TgtLang)
	if err != nil {
		return res, fmt.Errorf("%w: %v", ErrTranslationFailed, err)
	}
	if len(translated) != len(stems) {
		return res, ErrTranslationFailed
	}

	allocated := map[string]map[string]bool{}
	for i, f := range files {
		targetDir := filepath.Join(outputDir, f.relDir)
		if allocated[targetDir] == nil {
			allocated[targetDir] = map[string]bool{}
		}
		used := allocated[targetDir]
		inUse := func(name string) bool {
			if used[strings.ToLower(name)] {
				return true
			}
			p := filepath.Join(targetDir, name)
			if _, err := os.Lstat(p); err == nil {
				return !samePath(p, f.src)
			}
			return false
		}

		stem := Sanitize(translated[i], "")
		ext := invalidChars.ReplaceAllString(f.ext, "_")
		candidate := stem + ext
		if inUse(candidate) {
			res.CollisionsResolved++
			for n := 1; ; n++ {
				candidate = fmt.Sprintf("%s_%d%s", stem, n, ext)
				if !inUse(candidate) {
					break
				}
			}
		}
		used[strings.ToLower(candidate)] = true

		dst := filepath.Join(targetDir, candidate)
		if err := os.MkdirAll(targetDir, 0o755); err != nil {
			return res, err
		}
		if !samePath(dst, f.src) {
			if err := copyFile(f.src, dst); err != nil {
				logf("Erro ao processar arquivo '%s': %v", f.name, err)
				continue
			}
		}
		res.TranslatedCount++
		res.FilesMap[f.src] = dst

		if opts.KeepOriginal && !strings.EqualFold(candidate, f.name) {
			orig := f.name
			if samePath(filepath.Join(targetDir, orig), f.src) {
				continue
			}
			if inUse(orig) {
				origExt := filepath.Ext(orig)
				origStem := strings.TrimSuffix(orig, origExt)
				for n := 1; ; n++ {
					orig = fmt.Sprintf("%s_%d%s", origStem, n, origExt)
					if !inUse(orig) {
						break
					}
				}
				res.CollisionsResolved++
			}
			used[strings.ToLower(orig)] = true
			if err := copyFile(f.src, filepath.Join(targetDir, orig)); err != nil {
				logf("Erro ao copiar o original '%s': %v", f.name, err)
				continue
			}
			res.CopiedOriginals++
		}
	}
	logf("Arquivos traduzidos: %d | Colisões resolvidas: %d | Localização: %s", res.TranslatedCount, res.CollisionsResolved, outputDir)
	return res, nil
}

func samePath(a, b string) bool {
	ia, errA := os.Stat(a)
	ib, errB := os.Stat(b)
	return errA == nil && errB == nil && os.SameFile(ia, ib)
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Chtimes(dst, info.ModTime(), info.ModTime())
}
