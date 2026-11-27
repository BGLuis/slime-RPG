package validator

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/BGLuis/slime-rpg/pkg/model"
)

// GuardSegments verifica segmentos já traduzidos (antes de gravar no jogo) e reverte
// automaticamente para o original qualquer tradução que quebre sintaxe JavaScript ou
// traduza um nome de arquivo de asset existente no disco.
func GuardSegments(gameDir, rel string, segments []*model.Segment, assets *AssetIndex) ([]*model.Segment, []string) {
	if len(segments) == 0 {
		return segments, nil
	}
	base := strings.ToLower(filepath.Base(rel))
	isPlugins := base == "plugins.js"
	var warnings []string
	out := make([]*model.Segment, len(segments))
	copy(out, segments)

	for i, s := range out {
		if !s.IsResolved() || s.Target() == s.Source() {
			continue
		}
		src := s.Source()
		tgt := s.Target()

		if isPlugins {
			paramKey := ""
			if p, ok := s.ContextValue("param"); ok {
				paramStr := fmt.Sprintf("%v", p)
				if idx := strings.LastIndexByte(paramStr, '/'); idx >= 0 {
					paramKey = paramStr[idx+1:]
				} else {
					paramKey = paramStr
				}
			}

			if !assets.IsEmpty() && (IsAssetParamKey(paramKey) || assets.Exists(src)) {
				if assets.Exists(src) && !assets.Exists(tgt) {
					out[i] = s.WithTarget(src)
					warnings = append(warnings, fmt.Sprintf("%s (%s): mantido asset original %q em vez de %q", rel, paramKey, src, tgt))
					continue
				}
			}

			if (IsScriptParamKey(paramKey) || IsLikelyJS(src)) && ValidateJS(src) == nil {
				if reason := CheckTranslatedJS(src, tgt); reason != "" {
					out[i] = s.WithTarget(src)
					warnings = append(warnings, fmt.Sprintf("%s (%s): mantido código JS original (%s)", rel, paramKey, reason))
					continue
				}
			}
		}

		// Se o segmento veio de uma fórmula/script puro (não de um literal de string dentro de script),
		// verifica se continuou JS válido.
		if s.Kind() == model.KindScript {
			if IsLikelyJS(src) && ValidateJS(src) == nil {
				if reason := CheckTranslatedJS(src, tgt); reason != "" {
					out[i] = s.WithTarget(src)
					warnings = append(warnings, fmt.Sprintf("%s (%s): mantido script original (%s)", rel, s.ID(), reason))
					continue
				}
			}
		}
	}
	return out, warnings
}

// FindWebRootForGame retorna a raiz web (com data/ e js/) de gameDir ou vazio se não for RPG Maker MV/MZ.
func FindWebRootForGame(gameDir string) string {
	return findWebRoot(gameDir)
}
