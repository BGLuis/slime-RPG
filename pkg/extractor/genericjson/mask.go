package genericjson

import (
	"regexp"

	"github.com/BGLuis/slime-rpg/pkg/pipeline/masking"
)

// MaskMatchers devolve os trechos protegidos em JSON de jogos (mesmos padrões da
// versão Python): códigos de formato, condições @if/@else, expressões de template
// e de $game*, funções seta, variáveis v[1]/s[2], printf, {variavel} e tags de
// rich text da Unity.
func MaskMatchers() []masking.Matcher {
	return []masking.Matcher{
		regexp.MustCompile(`(?i)\\[A-Za-z]+\s*\[[^\]]*\]`),
		regexp.MustCompile(`(?i)\\[A-Za-z]+\s*<.*?>`),
		regexp.MustCompile(`(?i)@(?:if|else)[^\n]*`),
		regexp.MustCompile(`\$\{[^}]*\}`),
		regexp.MustCompile(`(?i)\$game(?:Variables|Switches|Party|Actors|Player|Map|System|Screen)(?:\.[a-zA-Z0-9_]+(?:\([^)]*\))?)*`),
		regexp.MustCompile(`(?s)^\s*\(?[a-zA-Z0-9_,\s]*\)?\s*=>.*$`),
		masking.NotPrecededBy(regexp.MustCompile(`!?(?:[vVsS]|ss|sv)\[\s*\d+\s*\]`), "!", masking.IsWordOrBackslash),
		regexp.MustCompile(`%[0-9]*\.?[0-9]*[sdfoxX]`),
		regexp.MustCompile(`\{[a-zA-Z0-9_]+\}`),
		regexp.MustCompile(`(?i)</?[a-zA-Z][a-zA-Z0-9=_\-#]*(?:\s+[^>]*)?/?>`),
	}
}
