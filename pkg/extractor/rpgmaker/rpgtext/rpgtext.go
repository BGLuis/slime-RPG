// Package rpgtext porta os filtros de texto do RPG Maker da versão Python
// (RPGTextFilters): decide se um texto é código/configuração ou texto visível ao
// jogador, e separa condições inline de escolhas (show_if(...) Opção).
package rpgtext

import (
	"regexp"
	"strconv"
	"strings"
)

// Context indica onde o texto aparece.
type Context int

const (
	// Dialogue é texto potencialmente visível (fala, escolha, nome, termo de sistema):
	// só literais de código inequívocos são descartados.
	Dialogue Context = iota
	// Param é valor de parâmetro de plugin/script, onde "Start", "Auto" ou "Center"
	// são valores de configuração e não texto de interface.
	Param
)

var (
	ignorePrefixes = []string{
		"D_TEXT", `"+`, `'+`, "voice_", "ChoiceVariableId", "PLM", `"00`,
		"let result", "const ", "this.", "super(", "extends ",
		"async ", "await ", "function", "=>",
	}
	ignoreParams = []string{"&&", "||", "==", "!=", "===", "!==", "$gamevariables.value", "Math."}

	jsDollarRE  = regexp.MustCompile(`^\$[A-Za-z_]`)
	jsNewRE     = regexp.MustCompile(`^new\s+[A-Z_][A-Za-z0-9_]*\b`)
	jsClassRE   = regexp.MustCompile(`^class\s+[A-Za-z_$][\w$]*\s*(?:extends\s+[\w$]+\s*)?\{`)
	jsImportRE  = regexp.MustCompile(`^import\s+(?:[\w${}\*,\s]+from\s+['"]|['"])`)
	jsExportRE  = regexp.MustCompile(`^export\s+(?:default\s+|const\s+|let\s+|var\s+|function\s+|class\s+|{[\w\s,]+})`)
	inlineHead  = regexp.MustCompile(`(?i)^(show_if|hide_if|if|en|pt|ja|es|fr)(\s*)\(`)
	inlineCode  = regexp.MustCompile(`[\[\]\$=<>!&|]`)
	assignRE    = regexp.MustCompile(`[\w$]\s*=[^=]`)
	controlStmt = regexp.MustCompile(`(?i)^(?:else\s*if|else|try|catch|finally)\s*[\(\{]`)
	controlPar  = regexp.MustCompile(`(?i)^(if|while|for|switch)\s*\(`)
	varDecl     = regexp.MustCompile(`(?i)^var\s+[A-Za-z_$][\w$]*\s*=`)
	pureEscape  = regexp.MustCompile(`^\s*(?:\\[A-Za-z]+(?:\s*\[[^\]]*\])?\s*)+$`)

	technicalLiterals = map[string]bool{"TRUE": true, "FALSE": true, "NULL": true, "UNDEFINED": true, "NAN": true}

	technicalParamValues = map[string]bool{
		"YES": true, "NO": true, "OK": true, "CANCEL": true,
		"END": true, "START": true, "STOP": true, "PLAY": true, "PAUSE": true, "NOCHANNEL": true, "CHANNEL": true,
		"LEFT": true, "RIGHT": true, "CENTER": true, "TOP": true, "BOTTOM": true, "MIDDLE": true, "ON": true, "OFF": true,
		"NO CHANGE": true, "NONE": true, "AUTO": true, "DEFAULT": true, "ALWAYS": true, "NEVER": true,
		"ENABLE": true, "DISABLE": true, "ENABLED": true, "DISABLED": true,
	}
)

// FindBalancedEnd devolve o índice logo após o parêntese que fecha o aberto em
// open, ignorando parênteses dentro de aspas; -1 se não fechar.
func FindBalancedEnd(s string, open int) int {
	depth := 0
	var quote byte
	escaped := false
	for i := open; i < len(s); i++ {
		ch := s[i]
		switch {
		case escaped:
			escaped = false
		case ch == '\\':
			escaped = true
		case quote != 0:
			if ch == quote {
				quote = 0
			}
		case ch == '"' || ch == '\'':
			quote = ch
		case ch == '(':
			depth++
		case ch == ')':
			depth--
			if depth == 0 {
				return i + 1
			}
		}
	}
	return -1
}

// ParseChoiceCondition separa a condição inline de uma escolha ("show_if(v[1]>0) Sim")
// do texto visível. Sem condição, prefix é vazio e body é o texto inteiro.
func ParseChoiceCondition(text string) (prefix, body string) {
	stripped := strings.TrimSpace(text)
	m := inlineHead.FindStringSubmatchIndex(stripped)
	if m == nil {
		return "", text
	}
	kw := strings.ToLower(stripped[m[2]:m[3]])
	ws := stripped[m[4]:m[5]]
	open := m[1] - 1
	end := FindBalancedEnd(stripped, open)
	if end == -1 {
		return "", text
	}
	inner := stripped[open+1 : end-1]
	if kw != "show_if" && kw != "hide_if" {
		if ws != "" || !inlineCode.MatchString(inner) {
			return "", text
		}
	}

	rest := stripped[end:]
	if strings.Contains(rest, "{") || strings.HasSuffix(strings.TrimRight(rest, " \t\r\n"), ";") || assignRE.MatchString(rest) {
		return "", text
	}

	trimmed := strings.TrimLeft(rest, " \t\r\n")
	spaceAfter := rest[:len(rest)-len(trimmed)]
	leading := text[:strings.Index(text, stripped)]
	return leading + stripped[:end] + spaceAfter, trimmed
}

// IsNumeric indica se o texto é um número (aceita vírgula decimal).
func IsNumeric(text string) bool {
	_, err := strconv.ParseFloat(strings.ReplaceAll(strings.TrimSpace(text), ",", "."), 64)
	return err == nil
}

// IsTechnicalOrCode indica se o texto é código, configuração ou literal técnico
// que não deve ser traduzido.
func IsTechnicalOrCode(text string, ctx Context) bool {
	if text == "" {
		return true
	}
	upper := strings.ToUpper(text)
	if technicalLiterals[upper] {
		return true
	}
	if ctx == Param && technicalParamValues[upper] {
		return true
	}

	clean := strings.NewReplacer(".", "", ",", "", "-", "", "+", "", " ", "").Replace(text)
	if clean != "" && isDigits(clean) {
		return true
	}

	if prefix, body := ParseChoiceCondition(text); prefix != "" {
		lower := strings.ToLower(strings.TrimSpace(prefix))
		if strings.HasPrefix(lower, "show_if") || strings.HasPrefix(lower, "hide_if") {
			if body == "" {
				return true
			}
			return IsTechnicalOrCode(body, ctx)
		}
	}

	for _, p := range ignorePrefixes {
		if strings.HasPrefix(text, p) {
			return true
		}
	}
	if jsDollarRE.MatchString(text) || jsNewRE.MatchString(text) || jsClassRE.MatchString(text) ||
		jsImportRE.MatchString(text) || jsExportRE.MatchString(text) {
		return true
	}
	if controlStmt.MatchString(text) || varDecl.MatchString(text) {
		return true
	}
	if m := controlPar.FindStringIndex(text); m != nil {
		end := FindBalancedEnd(text, m[1]-1)
		if end == -1 {
			return true
		}
		inner := text[m[1] : end-1]
		after := strings.TrimSpace(text[end:])
		if inlineCode.MatchString(inner) || strings.HasPrefix(after, "{") || strings.HasPrefix(after, "return") || strings.Contains(after, ";") {
			return true
		}
	}
	for _, p := range ignoreParams {
		if strings.Contains(text, p) {
			return true
		}
	}
	return pureEscape.MatchString(text)
}

// isDigits imita str.isdigit() do Python para o texto já sem separadores.
func isDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			if r < '０' || r > '９' {
				return false
			}
		}
	}
	return true
}
