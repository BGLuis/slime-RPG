package wolfrpg

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/BGLuis/slime-rpg/pkg/extractor/wolfrpg/dxa"
)

// mediaPrefixes são os contêineres multimídia (áudio, imagens, fontes), que não
// têm texto traduzível e podem ocupar gigabytes: nunca são abertos.
var mediaPrefixes = []string{
	"bgm", "se", "seh", "sound", "voice", "graphic", "picture", "cg",
	"hcg", "samune", "hsamune", "face", "charachip", "enemychip", "enemygraphic",
	"eventgraphic", "systemgraphic", "battlegraphic", "battleeffect",
	"mapchip", "parallax", "fog", "font", "hfont", "icon", "movie", "video",
}

type packedRef struct {
	archive *dxa.Archive
	entry   *dxa.Entry
}

// Packed reúne os contêineres .wolf de um jogo. Os arquivos continuam dentro
// dos contêineres: são lidos sob demanda e nada é gravado na pasta do jogo.
type Packed struct {
	root     string
	archives []*dxa.Archive
	files    map[string]packedRef // rel (relativo à raiz do jogo, com '/') -> entrada
}

// OpenPacked abre os contêineres .wolf encontrados sob dirs. Um contêiner
// Nome.wolf corresponde à pasta Nome/ ao lado dele; Data.wolf, à própria pasta.
func OpenPacked(root string, dirs []string) (*Packed, error) {
	p := &Packed{root: root, files: map[string]packedRef{}}
	var found []string
	for _, dir := range dirs {
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
			if strings.EqualFold(filepath.Ext(path), ".wolf") && !isMediaContainer(d.Name()) {
				found = append(found, path)
			}
			return nil
		})
	}
	slices.Sort(found)
	found = slices.Compact(found)

	for _, path := range found {
		a, err := dxa.Open(path, nil)
		if err != nil {
			p.Close()
			return nil, fmt.Errorf("failed to open WOLF archive %s: %w", filepath.Base(path), err)
		}
		p.archives = append(p.archives, a)

		dest := filepath.Dir(path)
		if stem := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)); !strings.EqualFold(stem, "data") {
			dest = filepath.Join(dest, stem)
		}
		base, err := filepath.Rel(root, dest)
		if err != nil || strings.HasPrefix(base, "..") {
			p.Close()
			return nil, fmt.Errorf("WOLF archive %s lies outside the game folder", path)
		}
		for _, e := range a.Entries {
			rel := filepath.ToSlash(filepath.Join(base, filepath.FromSlash(e.Path)))
			if _, taken := p.files[rel]; !taken {
				p.files[rel] = packedRef{a, e}
			}
		}
	}
	return p, nil
}

func isMediaContainer(name string) bool {
	stem := strings.ToLower(strings.TrimSuffix(name, filepath.Ext(name)))
	return slices.ContainsFunc(mediaPrefixes, func(p string) bool { return strings.HasPrefix(stem, p) })
}

// Close fecha os contêineres.
func (p *Packed) Close() {
	for _, a := range p.archives {
		_ = a.Close()
	}
	p.archives = nil
}

// Len é o número de arquivos dentro dos contêineres.
func (p *Packed) Len() int { return len(p.files) }

// Pending lista os arquivos que existem só dentro dos contêineres: os que já
// têm uma versão solta na pasta do jogo têm prioridade, como na engine.
func (p *Packed) Pending() []string {
	var rels []string
	for rel := range p.files {
		if _, err := os.Stat(filepath.Join(p.root, filepath.FromSlash(rel))); err != nil {
			rels = append(rels, rel)
		}
	}
	slices.Sort(rels)
	return rels
}

// Read devolve o conteúdo de um arquivo do contêiner.
func (p *Packed) Read(rel string) ([]byte, error) {
	ref, ok := p.files[rel]
	if !ok {
		return nil, fs.ErrNotExist
	}
	return ref.archive.Read(ref.entry)
}

// Repack grava em outPath um único Data.wolf com o conteúdo de dataDir (relativo
// à raiz do jogo): o que está nos contêineres, sobreposto pelos arquivos soltos
// e, por último, pelos traduzidos em overlayDir (que espelha a raiz do jogo).
// Os próprios contêineres (.wolf, .wolf.bak) não entram.
func (p *Packed) Repack(dataDir, overlayDir, outPath string) error {
	prefix := ""
	if dataDir != "" {
		prefix = filepath.ToSlash(dataDir) + "/"
	}
	sources := map[string]func() ([]byte, error){}
	for rel := range p.files {
		if inner, ok := strings.CutPrefix(rel, prefix); ok {
			sources[inner] = func() ([]byte, error) { return p.Read(rel) }
		}
	}
	add := func(base string) {
		_ = filepath.WalkDir(filepath.Join(base, filepath.FromSlash(dataDir)), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				if strings.HasPrefix(d.Name(), ".") && d.Name() != "." {
					return filepath.SkipDir
				}
				return nil
			}
			lower := strings.ToLower(d.Name())
			if strings.HasSuffix(lower, ".wolf") || strings.HasSuffix(lower, ".wolf.bak") {
				return nil
			}
			inner, err := filepath.Rel(filepath.Join(base, filepath.FromSlash(dataDir)), path)
			if err != nil {
				return nil
			}
			sources[filepath.ToSlash(inner)] = func() ([]byte, error) { return os.ReadFile(path) }
			return nil
		})
	}
	add(p.root)
	add(overlayDir)

	files := make([]dxa.File, 0, len(sources))
	for path, read := range sources {
		files = append(files, dxa.File{Path: path, Read: read})
	}
	if len(files) == 0 {
		return errors.New("nothing to repack")
	}

	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return err
	}
	tmp := outPath + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	err = dxa.Write(f, files)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, outPath)
}
