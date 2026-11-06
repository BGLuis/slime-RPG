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
					if !isTranslatableCommand(cmd.CID) {
						continue
					}
					for sIdx, str := range cmd.StringArgs {
						if !IsTranslatable(str) {
							continue
						}
						kind := model.KindDialogue
						if cmd.CID == CodeShowChoices {
							kind = model.KindChoice
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
					if !isTranslatableCommand(cmd.CID) {
						continue
					}
					for sIdx, str := range cmd.StringArgs {
						id := fmt.Sprintf("%s#ev:%d/p:%d/cmd:%d/str:%d", base, ev.ID, page.ID, cmdIdx, sIdx)
						if target, found := trMap[id]; found {
							cmd.StringArgs[sIdx] = target
							changedAny = true
						} else {
							_ = str
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

func isTranslatableCommand(cid WolfEventCode) bool {
	switch cid {
	case CodeShowMessage, CodeShowChoices, CodeStringCondition, CodeSetString, CodePicture:
		return true
	default:
		return false
	}
}
