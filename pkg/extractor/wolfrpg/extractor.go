package wolfrpg

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/BGLuis/slime-rpg/pkg/model"
)

// Extractor é o extrator da engine WOLF RPG para mapas (.mps), eventos comuns e arquivos de dados.
type Extractor struct{}

func NewExtractor() *Extractor {
	return &Extractor{}
}

// CanHandle verifica se o arquivo é suportado pelo extrator Wolf RPG.
func (e *Extractor) CanHandle(filePath string) bool {
	ext := strings.ToLower(filepath.Ext(filePath))
	base := strings.ToLower(filepath.Base(filePath))

	if ext == ".mps" {
		return true
	}
	if base == "commonevent.dat" || base == "database.dat" || base == "game.dat" {
		return true
	}
	return false
}

// Extract extrai os diálogos, escolhas e strings visíveis de arquivos Wolf RPG.
func (e *Extractor) Extract(filePath string, data []byte, srcLang string) ([]*model.Segment, error) {
	ext := strings.ToLower(filepath.Ext(filePath))
	base := filepath.Base(filePath)

	if ext == ".mps" {
		m, err := LoadWolfMap(data)
		if err != nil {
			return nil, fmt.Errorf("failed to parse wolf map %s: %w", base, err)
		}

		var segments []*model.Segment
		for _, ev := range m.Events {
			for _, page := range ev.Pages {
				for cmdIdx, cmd := range page.Commands {
					for _, sIdx := range translatableArgs(cmd) {
						str := cmd.StringArgs[sIdx]
						if !IsTranslatable(str) {
							continue
						}
						kind := model.KindDialogue
						switch cmd.CID {
						case CodeShowChoices:
							kind = model.KindChoice
						case CodePicture:
							kind = model.KindMenu
						}

						id := fmt.Sprintf("%s#ev:%d/p:%d/cmd:%d/str:%d", base, ev.ID, page.ID, cmdIdx, sIdx)
						seg, err := model.NewSegment(id, str, kind, map[string]any{
							"file":     base,
							"event_id": ev.ID,
							"page_id":  page.ID,
							"cmd_idx":  cmdIdx,
							"cid":      int(cmd.CID),
						}, nil)
						if err == nil {
							segments = append(segments, seg)
						}
					}
				}
			}
		}
		return segments, nil
	}

	return nil, nil
}

// Inject reinjeta as traduções nos arquivos binários do Wolf RPG.
func (e *Extractor) Inject(filePath string, originalData []byte, segments []*model.Segment, tgtLang string) ([]byte, error) {
	ext := strings.ToLower(filepath.Ext(filePath))
	base := filepath.Base(filePath)

	if ext == ".mps" {
		m, err := LoadWolfMap(originalData)
		if err != nil {
			return nil, fmt.Errorf("failed to parse wolf map %s: %w", base, err)
		}

		trMap := make(map[string]string, len(segments))
		for _, s := range segments {
			if s.IsResolved() && s.Target() != "" {
				trMap[s.ID()] = s.Target()
			}
		}

		if len(trMap) == 0 {
			return originalData, nil
		}

		changedAny := false
		for _, ev := range m.Events {
			for _, page := range ev.Pages {
				for cmdIdx, cmd := range page.Commands {
					for _, sIdx := range translatableArgs(cmd) {
						id := fmt.Sprintf("%s#ev:%d/p:%d/cmd:%d/str:%d", base, ev.ID, page.ID, cmdIdx, sIdx)
						if target, found := trMap[id]; found {
							cmd.StringArgs[sIdx] = target
							changedAny = true
						}
					}
				}
			}
		}

		if !changedAny {
			return originalData, nil
		}

		return m.Save()
	}

	return originalData, nil
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
