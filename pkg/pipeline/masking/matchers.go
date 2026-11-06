package masking

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

type notPrecededBy struct {
	re     *regexp.Regexp
	lead   string
	reject func(prev rune) bool
}

// NotPrecededBy aceita só as ocorrências de re cujo caractere anterior não é
// rejeitado, emulando o lookbehind negativo dos padrões da versão Python. Se a
// ocorrência começar por lead (o "!?" de "!?(?<![A-Za-z0-9_])v[1]"), o teste vale
// para o caractere logo antes do que vem depois de lead, como no Python.
func NotPrecededBy(re *regexp.Regexp, lead string, reject func(prev rune) bool) Matcher {
	return notPrecededBy{re: re, lead: lead, reject: reject}
}

func (m notPrecededBy) FindAllStringIndex(s string, n int) [][]int {
	var out [][]int
	for pos := 0; pos <= len(s) && (n < 0 || len(out) < n); {
		loc := m.re.FindStringIndex(s[pos:])
		if loc == nil {
			break
		}
		start, end := pos+loc[0], pos+loc[1]
		guard := start
		if m.lead != "" && strings.HasPrefix(s[start:end], m.lead) {
			guard += len(m.lead)
		}
		if guard > 0 {
			if prev, _ := utf8.DecodeLastRuneInString(s[:guard]); m.reject(prev) {
				_, size := utf8.DecodeRuneInString(s[start:])
				pos = start + max(size, 1)
				continue
			}
		}
		out = append(out, []int{start, end})
		pos = max(end, start+1)
	}
	return out
}

// IsWordOrBackslash rejeita letras, dígitos, '_' e '\', o lookbehind
// (?<!\\)(?<![A-Za-z0-9_]) usado nas variáveis v[1]/s[2] do RPG Maker.
func IsWordOrBackslash(r rune) bool {
	return r == '\\' || r == '_' || ('a' <= r && r <= 'z') || ('A' <= r && r <= 'Z') || ('0' <= r && r <= '9')
}
