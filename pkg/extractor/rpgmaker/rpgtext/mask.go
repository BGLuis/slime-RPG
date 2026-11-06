package rpgtext

import (
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/BGLuis/slime-rpg/pkg/pipeline/masking"
)

var (
	placeholderRE = regexp.MustCompile(`(?i)__\s*XTOK[_\s]+[a-z0-9]+\s*__`)
	codeHeadRE    = regexp.MustCompile(`(?i)(if|en|pt|ja|es|fr|show_if|hide_if)\s*\(`)
	proseHeadRE   = regexp.MustCompile(`(?i)(show_if|hide_if|if|en)(\s*)\(`)
)

// MaskMatchers devolve os trechos protegidos em textos do RPG Maker, na ordem da
// versão Python: códigos de formato, condições inline, $game*, variáveis v[1]/s[2],
// literais e operadores (só em código), $variavelCamel e tags HTML. Os escapes de
// um caractere (\. \| \! \^ \$ \{ \} \G) também ficam protegidos.
func MaskMatchers() []masking.Matcher {
	return []masking.Matcher{
		regexp.MustCompile(`(?i)\\[A-Za-z]+\s*\[[^\]]*\]`),
		balancedCondition{},
		regexp.MustCompile(`(?i)\$game(?:variables|switches|party|actors|player|map|system|screen|timer|message|temp|troop|interpreter)\b`),
		masking.NotPrecededBy(regexp.MustCompile(`!?(?:[vVsS]|ss|sv)\[\s*\d+\s*\]`), "!", masking.IsWordOrBackslash),
		codeOnly{regexp.MustCompile(`(?i)\b(?:true|false|null|undefined|NaN)\b`)},
		codeOnly{regexp.MustCompile(`(?:&&|\|\||>=|<=|!==|===|!=|==)`)},
		regexp.MustCompile(`\$\s*[a-z]+[A-Z][a-zA-Z]*`),
		regexp.MustCompile(`(?i)</?[a-zA-Z][a-zA-Z0-9]*(?:\s+[^>]*)?/?>`),
		regexp.MustCompile(`\\[.|!^${}G#+\-]`),
	}
}

// codeOnly só casa quando o texto inteiro é código: "true" ou "==" em prosa são
// palavras e pontuação comuns.
type codeOnly struct{ re *regexp.Regexp }

func (m codeOnly) FindAllStringIndex(s string, n int) [][]int {
	if !isCodeIgnoringPlaceholders(s) {
		return nil
	}
	return m.re.FindAllStringIndex(s, n)
}

// isCodeIgnoringPlaceholders avalia o texto já mascarado: cada placeholder vira
// "$game", que mantém a natureza de código do trecho que ele substituiu.
func isCodeIgnoringPlaceholders(s string) bool {
	if IsTechnicalOrCode(s, Dialogue) {
		return true
	}
	if strings.Contains(strings.ToLower(s), "__xtok_") {
		return IsTechnicalOrCode(placeholderRE.ReplaceAllString(s, "$$game"), Dialogue)
	}
	return false
}

// balancedCondition reconhece condições com parênteses balanceados, inclusive
// aninhados ("if (a(b) && c)"). Em código aceita if/en/pt/ja/es/fr/show_if/hide_if;
// em prosa só show_if/hide_if, ou if(...)/en(...) colados ao parêntese e com
// sintaxe de código dentro, para não mascarar "If (you) go" ou "es (sic)".
type balancedCondition struct{}

func (balancedCondition) FindAllStringIndex(s string, n int) [][]int {
	isCode := IsTechnicalOrCode(s, Dialogue)
	head := proseHeadRE
	if isCode {
		head = codeHeadRE
	}
	var out [][]int
	for pos := 0; pos < len(s) && (n < 0 || len(out) < n); {
		m := head.FindStringSubmatchIndex(s[pos:])
		if m == nil {
			break
		}
		start, headEnd := pos+m[0], pos+m[1]
		if start > 0 {
			if prev, _ := utf8.DecodeLastRuneInString(s[:start]); isWordRune(prev) {
				pos = start + 1
				continue
			}
		}
		open := headEnd - 1
		end := FindBalancedEnd(s, open)
		if end == -1 {
			pos = headEnd
			continue
		}
		if !isCode {
			kw := strings.ToLower(s[pos+m[2] : pos+m[3]])
			ws := s[pos+m[4] : pos+m[5]]
			if (kw == "if" || kw == "en") && (ws != "" || !inlineCode.MatchString(s[open+1:end-1])) {
				pos = headEnd
				continue
			}
		}
		out = append(out, []int{start, end})
		pos = end
	}
	return out
}

func isWordRune(r rune) bool {
	return r == '_' || ('a' <= r && r <= 'z') || ('A' <= r && r <= 'Z') || ('0' <= r && r <= '9')
}
