package validator

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"github.com/dop251/goja/parser"
)

// ValidateJS verifica se code é sintaticamente válido em JavaScript usando o parser puro Go do goja.
func ValidateJS(code string) error {
	trimmed := strings.TrimSpace(code)
	if trimmed == "" {
		return nil
	}
	_, err := parser.ParseFile(nil, "", trimmed, 0)
	if err == nil {
		return nil
	}
	// Alguns parâmetros guardam objeto literal ou função anônima que só parseiam como expressão.
	if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "(") || strings.HasPrefix(trimmed, "function") {
		if _, err2 := parser.ParseFile(nil, "", "("+trimmed+")", 0); err2 == nil {
			return nil
		}
	}
	return err
}

var (
	jsTokenRE        = regexp.MustCompile(`\$?[A-Za-z_][A-Za-z0-9_]*`)
	decimalCommaRE   = regexp.MustCompile(`\d,\d`)
	decimalDotRE     = regexp.MustCompile(`\d\.\d`)
	jsOperatorHintRE = regexp.MustCompile(`(?:;|=>|\b(?:return|null|const|let|var|function|this|true|false|new|typeof)\b|\.[A-Za-z_]\w*\s*(?:\(|$|[+\-*/%&|<>=!])|\$\w+\s*\.)`)
	jsMathExprRE     = regexp.MustCompile(`[A-Za-z_$][\w$.]*\s*[+\-*/%<>=!]+\s*(?:[A-Za-z0-9_$.]+|\()`)
)

// criticalJSTokens são palavras-chave e globais do RPG Maker que nunca podem ser traduzidas dentro de código JS.
var criticalJSTokens = map[string]bool{
	"return": true, "null": true, "true": true, "false": true, "undefined": true,
	"this": true, "const": true, "let": true, "var": true, "function": true,
	"if": true, "else": true, "for": true, "while": true, "switch": true, "new": true,
	"Graphics": true, "SceneManager": true, "DataManager": true, "AudioManager": true,
	"BattleManager": true, "ImageManager": true, "ConfigManager": true, "SoundManager": true,
	"$gameVariables": true, "$gameSwitches": true, "$gameSystem": true,
	"$gameActors": true, "$gameParty": true, "$gameMap": true, "$gamePlayer": true,
	"$gameScreen": true, "$gameMessage": true, "$gameTemp": true, "$gameTroop": true,
	"$dataActors": true, "$dataItems": true, "$dataSkills": true, "$dataWeapons": true,
	"$dataArmors": true, "$dataEnemies": true, "$dataStates": true, "$dataSystem": true,
}

// IsLikelyJS identifica strings que representam código ou expressão JS (e não texto natural).
func IsLikelyJS(s string) bool {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" || ValidateJS(trimmed) != nil {
		return false
	}
	// Frases em CJK fora de comentários/strings normalmente não são código puro,
	// a menos que contenham sinais claros antes de comentário //.
	if hasCJK(trimmed) {
		beforeComment, _, hasComment := strings.Cut(trimmed, "//")
		if !hasComment {
			return false
		}
		trimmed = strings.TrimSpace(beforeComment)
		if trimmed == "" || ValidateJS(trimmed) != nil {
			return false
		}
	}
	return jsOperatorHintRE.MatchString(trimmed) || jsMathExprRE.MatchString(trimmed)
}

// CheckTranslatedJS compara um trecho JS original com a sua versão traduzida.
// Retorna uma descrição do problema se a tradução quebrou a sintaxe ou traduziu tokens de código.
func CheckTranslatedJS(orig, trans string) string {
	if strings.TrimSpace(trans) == "" {
		return "código JavaScript ficou vazio após tradução"
	}
	if err := ValidateJS(trans); err != nil {
		return fmt.Sprintf("sintaxe JavaScript inválida: %v", err)
	}
	if decimalDotRE.MatchString(orig) && decimalCommaRE.MatchString(trans) && !decimalCommaRE.MatchString(orig) {
		return "separador decimal '.' foi trocado por ',' em expressão JavaScript"
	}

	origCode := stripJSStringAndComments(orig)
	transCode := stripJSStringAndComments(trans)
	origCounts := countTokens(origCode, criticalJSTokens)
	transCounts := countTokens(transCode, criticalJSTokens)
	for tok, n := range origCounts {
		if transCounts[tok] < n {
			return fmt.Sprintf("token JavaScript %q foi alterado ou removido na tradução", tok)
		}
	}
	return ""
}

func countTokens(code string, allow map[string]bool) map[string]int {
	counts := make(map[string]int)
	for _, tok := range jsTokenRE.FindAllString(code, -1) {
		if allow[tok] {
			counts[tok]++
		}
	}
	return counts
}

// stripJSStringAndComments remove literais de string e comentários // ou /* */ para
// inspecionar apenas identificadores e palavras-chave reais do código JS.
func stripJSStringAndComments(src string) string {
	var b strings.Builder
	var quote byte
	inLineComment := false
	inBlockComment := false
	escaped := false

	for i := 0; i < len(src); i++ {
		ch := src[i]
		switch {
		case inLineComment:
			if ch == '\n' {
				inLineComment = false
				b.WriteByte('\n')
			}
		case inBlockComment:
			if ch == '*' && i+1 < len(src) && src[i+1] == '/' {
				inBlockComment = false
				i++
				b.WriteByte(' ')
			}
		case quote != 0:
			if escaped {
				escaped = false
			} else if ch == '\\' {
				escaped = true
			} else if ch == quote {
				quote = 0
				b.WriteByte(' ')
			}
		default:
			if ch == '/' && i+1 < len(src) {
				if src[i+1] == '/' {
					inLineComment = true
					i++
					continue
				}
				if src[i+1] == '*' {
					inBlockComment = true
					i++
					continue
				}
			}
			if ch == '"' || ch == '\'' || ch == '`' {
				quote = ch
				b.WriteByte(' ')
				continue
			}
			b.WriteByte(ch)
		}
	}
	return b.String()
}

func hasCJK(s string) bool {
	for _, r := range s {
		if unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul) {
			return true
		}
	}
	return false
}

var scriptParamKeyRE = regexp.MustCompile(`(?i)(?:script|formula|eval|condition|width|height)$`)

// IsScriptParamKey indica se a chave de parâmetro de plugin contém código/expressão JS.
func IsScriptParamKey(key string) bool {
	return scriptParamKeyRE.MatchString(strings.TrimSpace(key))
}
