// Package formats une os extratores sob um contrato único e decide, a partir da
// detecção do jogo, quais arquivos cada um processa.
package formats

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/BGLuis/slime-rpg/pkg/detector"
	csvx "github.com/BGLuis/slime-rpg/pkg/extractor/csv"
	"github.com/BGLuis/slime-rpg/pkg/extractor/genericjson"
	rpgjson "github.com/BGLuis/slime-rpg/pkg/extractor/rpgmaker/json"
	"github.com/BGLuis/slime-rpg/pkg/extractor/rpgmaker/rpgtext"
	"github.com/BGLuis/slime-rpg/pkg/extractor/rpgmaker/rvdata"
	"github.com/BGLuis/slime-rpg/pkg/extractor/wolfrpg"
	"github.com/BGLuis/slime-rpg/pkg/model"
	"github.com/BGLuis/slime-rpg/pkg/pipeline/masking"
)

var (
	// ErrUnsupported indica que a detecção não encontrou um formato traduzível.
	ErrUnsupported = errors.New("no supported game format found")
)

// Format é o contrato comum dos extratores. rel é o caminho relativo à raiz do
// job, com '/'.
type Format interface {
	// Name é o nome do extrator gravado no metadado (o mesmo da versão Python).
	Name() string
	Extensions() []string
	Handles(rel string) bool
	Extract(rel string, data []byte, srcLang, tgtLang string) ([]*model.Segment, error)
	Inject(rel string, data []byte, segments []*model.Segment, srcLang, tgtLang string) ([]byte, error)
	// Masker devolve o mascarador dos trechos protegidos do formato.
	Masker() *masking.Masker
}

// File é um arquivo do job.
type File struct {
	Rel    string // relativo à raiz, com '/'
	Format Format
	// Read, quando definido, lê o conteúdo de um arquivo que só existe dentro de
	// um contêiner (ex.: .wolf) e não na pasta do jogo.
	Read func() ([]byte, error)
}

// Rewriter é implementado pelos formatos que alteram arquivos mesmo sem
// segmentos traduzíveis (ex.: a fonte do Game.dat do WOLF RPG).
type Rewriter interface {
	Rewrite(rel string, data []byte) (out []byte, changed bool, err error)
}

// Plan descreve o que um job processa.
type Plan struct {
	Root   string // pasta de trabalho: entrada do workspace e base do backup e do metadado
	Format Format
	Files  []File

	packed  *wolfrpg.Packed
	wolf    *wolfrpg.Extractor
	dataRel string
}

// Close libera os contêineres abertos pelo plano.
func (p *Plan) Close() {
	if p.packed != nil {
		p.packed.Close()
		p.packed = nil
	}
}

// Repack gera o Data.wolf com os arquivos traduzidos em processDir quando o
// modo de reempacotamento do WOLF RPG está ligado. Sem o modo, não faz nada.
// Devolve o caminho relativo à raiz do que foi gerado.
func (p *Plan) Repack(processDir string) (string, error) {
	if p.wolf == nil || !p.wolf.RepackMode {
		return "", nil
	}
	rel := filepath.ToSlash(filepath.Join(p.dataRel, "Data.wolf"))
	if p.packed == nil {
		return "", nil
	}
	return rel, p.packed.Repack(p.dataRel, processDir, filepath.Join(processDir, filepath.FromSlash(rel)))
}

// Includes indica se o arquivo faz parte do plano.
func (p *Plan) Includes(rel string) bool {
	_, found := slices.BinarySearchFunc(p.Files, rel, func(f File, r string) int { return strings.Compare(f.Rel, r) })
	return found
}

// ForDetection monta o plano para o jogo detectado. Um único arquivo vira um
// plano com raiz na pasta que o contém.
func ForDetection(det *detector.DetectionResult) (*Plan, error) {
	gamePath, err := filepath.Abs(det.GamePath)
	if err != nil {
		return nil, err
	}
	dataDir, err := filepath.Abs(det.DataDir)
	if err != nil {
		return nil, err
	}
	abs := *det
	abs.GamePath, abs.DataDir = gamePath, dataDir
	det = &abs

	info, err := os.Stat(det.GamePath)
	if err != nil {
		return nil, err
	}

	format := ForEngine(det.Engine)
	if format == nil {
		return nil, fmt.Errorf("%w: %s", ErrUnsupported, det.Engine)
	}

	if !info.IsDir() {
		root := filepath.Dir(det.GamePath)
		rel := filepath.Base(det.GamePath)
		if !format.Handles(rel) {
			return nil, fmt.Errorf("%w: %s", ErrUnsupported, rel)
		}
		if w, ok := format.(wolf); ok {
			w.x.Dir = root
		}
		return &Plan{Root: root, Format: format, Files: []File{{Rel: rel, Format: format}}}, nil
	}

	plan := &Plan{Root: det.GamePath, Format: format}
	for _, dir := range scanDirs(det) {
		plan.addTree(dir, format)
	}
	if det.Engine == detector.EngineRPGMakerMV || det.Engine == detector.EngineRPGMakerMZ {
		plan.addPluginsJS(det)
	}
	if w, ok := format.(wolf); ok {
		w.x.Dir = plan.Root
		plan.wolf = w.x
		if err := plan.addPacked(scanDirs(det), format, w.x); err != nil {
			return nil, err
		}
	}
	slices.SortFunc(plan.Files, func(a, b File) int { return strings.Compare(a.Rel, b.Rel) })
	plan.Files = slices.CompactFunc(plan.Files, func(a, b File) bool { return a.Rel == b.Rel })
	return plan, nil
}

// addPacked inclui no plano os arquivos que só existem dentro de contêineres
// .wolf. Eles são lidos sob demanda; a pasta do jogo não é alterada.
func (p *Plan) addPacked(dirs []string, format Format, x *wolfrpg.Extractor) error {
	packed, err := wolfrpg.OpenPacked(p.Root, dirs)
	if err != nil {
		return err
	}
	p.packed = packed
	x.Packed = packed
	if rel, err := filepath.Rel(p.Root, dirs[0]); err == nil {
		if rel != "." {
			p.dataRel = filepath.ToSlash(rel)
		}
	}
	for _, rel := range packed.Pending() {
		if format.Handles(rel) {
			p.Files = append(p.Files, File{Rel: rel, Format: format, Read: func() ([]byte, error) { return packed.Read(rel) }})
		}
	}
	return nil
}

// ForEngine devolve o formato da engine detectada.
func ForEngine(engine detector.GameEngine) Format {
	switch engine {
	case detector.EngineRPGMakerMV, detector.EngineRPGMakerMZ:
		return rpgMaker{x: rpgjson.NewMVExtractor()}
	case detector.EngineRPGMakerVXAce, detector.EngineRPGMakerXP:
		return rvData{x: rvdata.NewExtractor()}
	case detector.EngineWolfRPG:
		x := wolfrpg.NewExtractor()
		x.PreserveTitle = true
		return wolf{x: x}
	case detector.EngineCSV:
		return csvFormat{x: csvx.NewCSVExtractor()}
	case detector.EngineJSON:
		return jsonFormat{x: genericjson.NewJSONExtractor()}
	}
	return nil
}

// scanDirs devolve as pastas varridas. No WOLF RPG a detecção pode apontar para
// Data/MapData; os eventos comuns e o banco de dados ficam em Data/BasicData.
func scanDirs(det *detector.DetectionResult) []string {
	dir := det.DataDir
	if det.Engine == detector.EngineWolfRPG {
		switch strings.ToLower(filepath.Base(dir)) {
		case "mapdata", "basicdata":
			dir = filepath.Dir(dir)
		}
	}
	return []string{dir}
}

func (p *Plan) addTree(dir string, format Format) {
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if path != dir && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(p.Root, path)
		if err != nil || strings.HasPrefix(rel, "..") {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if format.Handles(rel) {
			p.Files = append(p.Files, File{Rel: rel, Format: format})
		}
		return nil
	})
}

// addPluginsJS inclui o js/plugins.js do MV/MZ, que fica fora da pasta data.
// Só entra se estiver dentro da raiz do job.
func (p *Plan) addPluginsJS(det *detector.DetectionResult) {
	candidates := []string{
		filepath.Join(filepath.Dir(det.DataDir), "js", "plugins.js"),
		filepath.Join(p.Root, "js", "plugins.js"),
		filepath.Join(p.Root, "www", "js", "plugins.js"),
	}
	for _, c := range candidates {
		if info, err := os.Stat(c); err != nil || info.IsDir() {
			continue
		}
		rel, err := filepath.Rel(p.Root, c)
		if err != nil || strings.HasPrefix(rel, "..") {
			continue
		}
		p.Files = append(p.Files, File{Rel: filepath.ToSlash(rel), Format: p.Format})
		return
	}
}

type rpgMaker struct{ x *rpgjson.MVExtractor }

func (rpgMaker) Name() string              { return "rpgmaker" }
func (rpgMaker) Extensions() []string      { return []string{"json", "js"} }
func (f rpgMaker) Handles(rel string) bool { return f.x.CanHandle(rel) }
func (f rpgMaker) Extract(rel string, data []byte, _, _ string) ([]*model.Segment, error) {
	return f.x.Extract(rel, data)
}
func (f rpgMaker) Inject(rel string, data []byte, segs []*model.Segment, _, _ string) ([]byte, error) {
	return f.x.Inject(rel, data, segs)
}
func (rpgMaker) Masker() *masking.Masker { return masking.NewMaskerWith(rpgtext.MaskMatchers()...) }

type rvData struct{ x *rvdata.Extractor }

func (rvData) Name() string              { return "rpgmaker" }
func (rvData) Extensions() []string      { return []string{"rvdata2", "rvdata", "rxdata"} }
func (f rvData) Handles(rel string) bool { return f.x.CanHandle(rel) }
func (f rvData) Extract(rel string, data []byte, src, _ string) ([]*model.Segment, error) {
	return f.x.Extract(rel, data, src)
}
func (f rvData) Inject(rel string, data []byte, segs []*model.Segment, _, tgt string) ([]byte, error) {
	return f.x.Inject(rel, data, segs, tgt)
}
func (rvData) Masker() *masking.Masker { return masking.NewMaskerWith(rpgtext.MaskMatchers()...) }

type wolf struct{ x *wolfrpg.Extractor }

func (wolf) Name() string              { return "wolfrpg" }
func (wolf) Extensions() []string      { return []string{"mps", "dat", "txt"} }
func (f wolf) Handles(rel string) bool { return f.x.CanHandle(rel) }
func (f wolf) Extract(rel string, data []byte, src, _ string) ([]*model.Segment, error) {
	return f.x.Extract(rel, data, src)
}
func (f wolf) Inject(rel string, data []byte, segs []*model.Segment, _, tgt string) ([]byte, error) {
	return f.x.Inject(rel, data, segs, tgt)
}
func (f wolf) Rewrite(rel string, data []byte) ([]byte, bool, error) { return f.x.Rewrite(rel, data) }
func (wolf) Masker() *masking.Masker                                 { return masking.NewMaskerWith(wolfrpg.MaskMatchers()...) }

// WolfOptions devolve o extrator WOLF RPG para configurar PreserveTitle, TargetFont
// e RepackMode; nil para os outros formatos.
func WolfOptions(f Format) *wolfrpg.Extractor {
	if w, ok := f.(wolf); ok {
		return w.x
	}
	return nil
}

type csvFormat struct{ x *csvx.CSVExtractor }

func (csvFormat) Name() string              { return "csv" }
func (csvFormat) Extensions() []string      { return []string{"csv"} }
func (f csvFormat) Handles(rel string) bool { return f.x.CanHandle(rel) }

// Extract ignora arquivos de outro idioma num conjunto en.csv/es.csv/pt.csv.
func (f csvFormat) Extract(rel string, data []byte, src, tgt string) ([]*model.Segment, error) {
	if !csvx.IsTranslatableFile(filepath.Base(rel), src) {
		return nil, nil
	}
	return f.x.Extract(rel, data, src, tgt)
}
func (f csvFormat) Inject(rel string, data []byte, segs []*model.Segment, _, tgt string) ([]byte, error) {
	return f.x.Inject(rel, data, segs, tgt)
}
func (csvFormat) Masker() *masking.Masker { return masking.NewMasker(csvx.MaskPatterns()) }

type jsonFormat struct{ x *genericjson.JSONExtractor }

func (jsonFormat) Name() string              { return "json" }
func (jsonFormat) Extensions() []string      { return []string{"json"} }
func (f jsonFormat) Handles(rel string) bool { return f.x.CanHandle(rel) }
func (f jsonFormat) Extract(rel string, data []byte, src, _ string) ([]*model.Segment, error) {
	return f.x.Extract(rel, data, src)
}
func (f jsonFormat) Inject(rel string, data []byte, segs []*model.Segment, _, tgt string) ([]byte, error) {
	return f.x.Inject(rel, data, segs, tgt)
}
func (jsonFormat) Masker() *masking.Masker {
	return masking.NewMaskerWith(genericjson.MaskMatchers()...)
}

// ExtensionsFor devolve o nome canônico e as extensões de um extrator da versão
// Python (files_types), usados ao adotar uma tradução legada.
func ExtensionsFor(name string) (string, []string) {
	switch strings.ToLower(strings.NewReplacer(" ", "", "_", "", "-", "").Replace(name)) {
	case "rpgmaker":
		return "rpgmaker", []string{"json", "rvdata2", "js"}
	case "wolfrpg", "wolf":
		return "wolfrpg", []string{"mps", "dat", "project", "txt"}
	case "csv":
		return "csv", []string{"csv"}
	case "json":
		return "json", []string{"json"}
	}
	return "", nil
}
