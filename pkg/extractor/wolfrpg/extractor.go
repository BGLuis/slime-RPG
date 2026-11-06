package wolfrpg

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"

	"github.com/BGLuis/slime-rpg/pkg/model"
)

// Extractor é o extrator da engine WOLF RPG: mapas (.mps), eventos comuns
// (CommonEvent.dat), bancos de dados (*.dat + .project), Game.dat e scripts de
// texto (Evtext/*.txt).
type Extractor struct {
	// Dir é a raiz do jogo, usada para achar o .project de cada banco e os
	// comandos que consultam o banco pelo nome do dado.
	Dir string
	// PreserveTitle mantém o título do Game.dat: em alguns jogos trocá-lo
	// invalida os saves existentes.
	PreserveTitle bool

	lookupOnce  sync.Once
	lookupTypes map[string]bool
}

func NewExtractor() *Extractor {
	return &Extractor{}
}

var (
	databaseFiles = []string{"database.dat", "cdatabase.dat", "sysdatabase.dat"}
	ignoredTexts  = []string{"readme.txt", "license.txt", "patch-config.txt", "patch-config.example.txt", "game_errorlog.txt"}
)

// CanHandle verifica se o arquivo é suportado pelo extrator Wolf RPG.
func (e *Extractor) CanHandle(filePath string) bool {
	base := strings.ToLower(filepath.Base(filePath))
	switch {
	case strings.HasSuffix(base, ".mps"):
		return true
	case base == "commonevent.dat" || base == "game.dat" || slices.Contains(databaseFiles, base):
		return true
	case strings.HasSuffix(base, ".txt"):
		return !slices.Contains(ignoredTexts, base)
	}
	return false
}

// textSlot é uma string traduzível de um arquivo WOLF.
type textSlot struct {
	id   string
	text string
	kind model.SegmentKind
	ctx  map[string]any
	set  func(string)
}

// Extract extrai os diálogos, escolhas e strings visíveis de arquivos Wolf RPG.
func (e *Extractor) Extract(filePath string, data []byte, srcLang string) ([]*model.Segment, error) {
	base := filepath.Base(filePath)
	doc, err := e.open(filePath, data)
	if err != nil || doc == nil {
		return nil, err
	}

	var segments []*model.Segment
	doc.walk(func(s textSlot) {
		if !IsTranslatable(s.text) {
			return
		}
		ctx := s.ctx
		ctx["file"] = base
		seg, err := model.NewSegment(base+"#"+s.id, UnwrapRuby(s.text), s.kind, ctx, nil)
		if err == nil {
			segments = append(segments, seg)
		}
	})
	return segments, nil
}

// Inject reinjeta as traduções nos arquivos binários do Wolf RPG.
func (e *Extractor) Inject(filePath string, originalData []byte, segments []*model.Segment, tgtLang string) ([]byte, error) {
	base := filepath.Base(filePath)
	trMap := make(map[string]string, len(segments))
	for _, s := range segments {
		if s.IsResolved() && s.Target() != "" && s.Target() != s.Source() {
			trMap[s.ID()] = s.Target()
		}
	}
	if len(trMap) == 0 {
		return originalData, nil
	}

	doc, err := e.open(filePath, originalData)
	if err != nil || doc == nil {
		return nil, err
	}
	changed := false
	doc.walk(func(s textSlot) {
		tr, ok := trMap[base+"#"+s.id]
		if !ok {
			return
		}
		if !doc.utf8 {
			// Fontes legadas dos jogos Shift-JIS não exibem acentos.
			tr = NormalizeWesternChars(tr)
		}
		s.set(tr)
		changed = true
	})
	if !changed {
		return originalData, nil
	}
	return doc.save(), nil
}

// document é um arquivo WOLF aberto: walk percorre as strings traduzíveis numa
// ordem estável (Extract e Inject geram os mesmos IDs) e save o serializa.
type document struct {
	utf8 bool
	walk func(fn func(textSlot))
	save func() []byte
}

func (e *Extractor) open(filePath string, data []byte) (*document, error) {
	base := filepath.Base(filePath)
	lower := strings.ToLower(base)
	switch {
	case strings.HasSuffix(lower, ".mps"):
		m, err := LoadWolfMap(data)
		if err != nil {
			return nil, fmt.Errorf("failed to parse wolf map %s: %w", base, err)
		}
		return &document{
			utf8: m.IsUTF8,
			walk: func(fn func(textSlot)) {
				for _, ev := range m.Events {
					for _, page := range ev.Pages {
						for cmdIdx, cmd := range page.Commands {
							walkCommand(fmt.Sprintf("ev:%d/p:%d/cmd:%d", ev.ID, page.ID, cmdIdx), cmd,
								map[string]any{"event_id": ev.ID, "page_id": page.ID, "cmd_idx": cmdIdx}, fn)
						}
					}
				}
			},
			save: func() []byte {
				out, err := m.Save()
				if err != nil {
					return data
				}
				return out
			},
		}, nil

	case lower == "commonevent.dat":
		ce, err := LoadWolfCommonEvents(data)
		if err != nil {
			return nil, fmt.Errorf("failed to parse %s: %w", base, err)
		}
		return &document{
			utf8: ce.IsUTF8,
			walk: func(fn func(textSlot)) {
				for _, ev := range ce.Events {
					for cmdIdx, cmd := range ev.Commands {
						walkCommand(fmt.Sprintf("ce:%d/cmd:%d", ev.ID, cmdIdx), cmd,
							map[string]any{"common_event": ev.ID, "cmd_idx": cmdIdx}, fn)
					}
				}
			},
			save: ce.Save,
		}, nil

	case slices.Contains(databaseFiles, lower):
		db, err := LoadWolfDatabase(data, e.project(filePath))
		if err != nil {
			return nil, fmt.Errorf("failed to parse %s: %w", base, err)
		}
		lookup := e.nameLookupTypes()
		return &document{
			utf8: db.IsUTF8,
			walk: func(fn func(textSlot)) {
				for tIdx, t := range db.Types {
					// O jogo busca dados pelo nome (ex.: tipo Costume 'Hair 1'); como o
					// nome do dado não é traduzido, o valor igual a ele também não pode ser.
					keepsNames := lookup == nil || lookup[t.Name]
					for dIdx, d := range t.Data {
						for sIdx, val := range d.StringValues {
							if keepsNames && val == d.Name {
								continue
							}
							fn(textSlot{
								id: fmt.Sprintf("type:%d/datum:%d/str:%d", tIdx, dIdx, sIdx), text: val, kind: model.KindDB,
								ctx: map[string]any{"type_id": tIdx, "datum_id": dIdx, "string_index": sIdx},
								set: func(tr string) { d.StringValues[sIdx] = tr },
							})
						}
					}
				}
			},
			save: db.Save,
		}, nil

	case lower == "game.dat":
		g, err := LoadWolfGameDat(data)
		if err != nil {
			return nil, fmt.Errorf("failed to parse %s: %w", base, err)
		}
		return &document{
			utf8: g.IsUTF8,
			walk: func(fn func(textSlot)) {
				if e.PreserveTitle {
					return
				}
				fn(textSlot{id: "game/title", text: g.Title, kind: model.KindMenu,
					ctx: map[string]any{"field": "title"}, set: func(tr string) { g.Title = tr }})
			},
			save: g.Save,
		}, nil

	case strings.HasSuffix(lower, ".txt"):
		return openEvtext(data), nil
	}
	return nil, nil
}

// walkCommand emite as strings traduzíveis de um comando de evento.
func walkCommand(prefix string, cmd *WolfCommand, base map[string]any, fn func(textSlot)) {
	kind := model.KindDialogue
	switch cmd.CID {
	case CodeShowChoices:
		kind = model.KindChoice
	case CodePicture:
		kind = model.KindMenu
	}
	for _, sIdx := range translatableArgs(cmd) {
		ctx := make(map[string]any, len(base)+1)
		for k, v := range base {
			ctx[k] = v
		}
		ctx["cid"] = int(cmd.CID)
		fn(textSlot{
			id: fmt.Sprintf("%s/str:%d", prefix, sIdx), text: cmd.StringArgs[sIdx], kind: kind, ctx: ctx,
			set: func(tr string) { cmd.StringArgs[sIdx] = tr },
		})
	}
}

// translatableArgs devolve os índices de StringArgs que podem conter texto do
// jogador, com as mesmas regras da versão Python. Condições por texto (112) ficam
// de fora: traduzir o valor comparado quebraria a lógica do evento.
func translatableArgs(cmd *WolfCommand) []int {
	n := len(cmd.StringArgs)
	if n == 0 {
		return nil
	}
	all := func(from int) []int {
		var idx []int
		for i := from; i < n; i++ {
			idx = append(idx, i)
		}
		return idx
	}
	switch cmd.CID {
	case CodeShowMessage, CodeSetString:
		return []int{0}
	case CodeShowChoices, CodeCallCommonEvent:
		return all(0)
	case CodeCallCommonByName:
		// O primeiro argumento é o nome do evento comum chamado.
		return all(1)
	case CodePicture:
		// Só o modo "texto" (tipo 2) exibe string; nos outros o argumento é um arquivo.
		if len(cmd.Args) > 0 && (cmd.Args[0]>>4)&0x07 == 2 {
			return []int{0}
		}
	}
	return nil
}

// project lê o .project ao lado do banco de dados, que traz os nomes do esquema.
func (e *Extractor) project(filePath string) []byte {
	if e.Dir == "" {
		return nil
	}
	rel := strings.TrimSuffix(filePath, filepath.Ext(filePath)) + ".project"
	data, err := os.ReadFile(filepath.Join(e.Dir, filepath.FromSlash(rel)))
	if err != nil {
		return nil
	}
	return data
}

// nameLookupTypes devolve os tipos de banco que os eventos consultam pelo nome
// literal do dado (comando 250 com nome de tipo e de dado). Devolve nil quando não
// é possível ter certeza; aí todos os tipos preservam os nomes.
func (e *Extractor) nameLookupTypes() map[string]bool {
	e.lookupOnce.Do(func() {
		if e.Dir == "" {
			return
		}
		found := map[string]bool{}
		scanned := 0
		scan := func(cmds []*WolfCommand) {
			for _, cmd := range cmds {
				if cmd.CID == CodeDatabase && len(cmd.StringArgs) >= 3 && cmd.StringArgs[1] != "" && cmd.StringArgs[2] != "" {
					found[cmd.StringArgs[1]] = true
				}
			}
		}
		failed := false
		_ = filepath.WalkDir(e.Dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			name := strings.ToLower(d.Name())
			switch {
			case name == "commonevent.dat":
				data, err := os.ReadFile(path)
				ce, perr := LoadWolfCommonEvents(data)
				if err != nil || perr != nil {
					failed = true
					return filepath.SkipAll
				}
				scanned++
				for _, ev := range ce.Events {
					scan(ev.Commands)
				}
			case strings.HasSuffix(name, ".mps"):
				data, err := os.ReadFile(path)
				m, perr := LoadWolfMap(data)
				if err != nil || perr != nil {
					failed = true
					return filepath.SkipAll
				}
				scanned++
				for _, ev := range m.Events {
					for _, p := range ev.Pages {
						scan(p.Commands)
					}
				}
			}
			return nil
		})
		if !failed && scanned > 0 {
			e.lookupTypes = found
		}
	})
	return e.lookupTypes
}

const rubyPart = `(?:(?:\\[A-Za-z]+\[[^\]]*\])|[^,\]])+`

var rubyRE = regexp.MustCompile(`(?i)\\r\s*\[\s*(` + rubyPart + `)\s*,\s*(?:(?:\\[A-Za-z]+\[[^\]]*\])|[^\]])+\s*\]`)

// UnwrapRuby troca \r[kanji,leitura] pelo kanji antes da tradução: a leitura em
// furigana não tem equivalente no idioma de destino.
func UnwrapRuby(text string) string {
	return rubyRE.ReplaceAllString(text, "$1")
}
