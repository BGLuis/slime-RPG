package masking

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

var (
	// ErrMaskIntegrity indica que placeholders foram corrompidos, perdidos ou vazaram na tradução.
	ErrMaskIntegrity = errors.New("mask integrity check failed")
)

const placeholderPrefix = "__XTOK_"

var (
	placeholderRE = regexp.MustCompile(`__XTOK_[A-Za-z0-9]+__`)
	// Tradutores costumam deformar o placeholder: "__xtok_0__", "__ XTOK_0 __", "__XTOK 0__".
	mangledPlaceholderRE = regexp.MustCompile(`(?i)__\s*XTOK[_\s]+([a-z0-9]+)\s*__`)
)

// DefaultRPGPatterns retorna as expressões regulares padrão para proteção de tags e variáveis no RPG Maker.
func DefaultRPGPatterns() []*regexp.Regexp {
	return []*regexp.Regexp{
		// Comandos com colchetes: \V[1], \N[2], \C[16], \I[42], \w[10]
		regexp.MustCompile(`\\[A-Za-z]+\[[^\]]*\]`),
		// Comandos de caractere único: \., \|, \!, \^, \$, \{, \}, \G, \#, \+, \-
		regexp.MustCompile(`\\[.|\!^${}G#+\-]`),
		// Tags estilo HTML/XML utilizadas por plugins (Yanfly, MOG, etc.): <center>, </center>, <br>
		regexp.MustCompile(`</?[A-Za-z0-9_]+(?:\s+[^>]*)?>`),
	}
}

// Matcher localiza trechos protegidos num texto. *regexp.Regexp já o satisfaz;
// os demais cobrem o que o RE2 não expressa (lookbehind, parênteses balanceados).
type Matcher interface {
	FindAllStringIndex(s string, n int) [][]int
}

// Masker gerencia a proteção e desmascaramento de trechos protegidos (tokens) em textos.
type Masker struct {
	patterns []Matcher
}

// NewMasker cria um novo mascarador com os padrões de regex especificados.
func NewMasker(patterns []*regexp.Regexp) *Masker {
	if len(patterns) == 0 {
		patterns = DefaultRPGPatterns()
	}
	matchers := make([]Matcher, len(patterns))
	for i, p := range patterns {
		matchers[i] = p
	}
	return &Masker{patterns: matchers}
}

// NewMaskerWith cria um mascarador a partir de matchers arbitrários.
func NewMaskerWith(matchers ...Matcher) *Masker {
	if len(matchers) == 0 {
		return NewMasker(nil)
	}
	return &Masker{patterns: matchers}
}

// Mask substitui todos os trechos do texto que correspondem aos padrões configurados
// por placeholders determinísticos sequenciais (__XTOK_0__, __XTOK_1__, etc.).
// Retorna a string mascarada e o mapa [placeholder] -> tokenOriginal, cujas chaves
// são exatamente os placeholders presentes no texto mascarado.
func (m *Masker) Mask(text string) (string, map[string]string) {
	tokenMap := make(map[string]string)
	if text == "" {
		return "", tokenMap
	}

	counter := 0
	result := text

	for _, pattern := range m.patterns {
		result = replaceAll(pattern, result, func(match string) string {
			if _, ok := tokenMap[match]; ok {
				return match
			}

			// Um padrão posterior pode engolir placeholders já criados
			// (ex.: <font color=\C[2]>): o trecho é restaurado e vira um único
			// placeholder, para que nenhum valor do mapa contenha outro placeholder.
			original := placeholderRE.ReplaceAllStringFunc(match, func(ph string) string {
				inner, ok := tokenMap[ph]
				if !ok {
					return ph
				}
				delete(tokenMap, ph)
				return inner
			})

			var ph string
			for {
				ph = fmt.Sprintf("%s%d__", placeholderPrefix, counter)
				counter++
				if _, exists := tokenMap[ph]; !exists && !strings.Contains(text, ph) {
					break
				}
			}

			tokenMap[ph] = original
			return ph
		})
	}

	return result, tokenMap
}

func replaceAll(m Matcher, s string, repl func(string) string) string {
	locs := m.FindAllStringIndex(s, -1)
	if len(locs) == 0 {
		return s
	}
	var b strings.Builder
	last := 0
	for _, loc := range locs {
		if loc[0] < last || loc[1] <= loc[0] {
			continue
		}
		b.WriteString(s[last:loc[0]])
		b.WriteString(repl(s[loc[0]:loc[1]]))
		last = loc[1]
	}
	b.WriteString(s[last:])
	return b.String()
}

// Unmask restaura os placeholders do texto traduzido. Aceita placeholders com caixa
// ou espaçamento alterados pelo tradutor, mas exige que cada placeholder do mapa
// apareça exatamente uma vez e que não haja nenhum desconhecido; caso contrário
// retorna ErrMaskIntegrity.
func (m *Masker) Unmask(translatedText string, tokenMap map[string]string) (string, error) {
	normalized := mangledPlaceholderRE.ReplaceAllStringFunc(translatedText, func(s string) string {
		id := mangledPlaceholderRE.FindStringSubmatch(s)[1]
		return placeholderPrefix + strings.ToUpper(id) + "__"
	})

	found := make(map[string]int)
	for _, ph := range placeholderRE.FindAllString(normalized, -1) {
		if _, known := tokenMap[ph]; !known {
			return "", fmt.Errorf("%w: unexpected placeholder %s in translated text: %q", ErrMaskIntegrity, ph, translatedText)
		}
		found[ph]++
	}
	for ph := range tokenMap {
		switch found[ph] {
		case 1:
		case 0:
			return "", fmt.Errorf("%w: placeholder %s missing in translated text: %q", ErrMaskIntegrity, ph, translatedText)
		default:
			return "", fmt.Errorf("%w: placeholder %s duplicated in translated text: %q", ErrMaskIntegrity, ph, translatedText)
		}
	}

	return placeholderRE.ReplaceAllStringFunc(normalized, func(ph string) string {
		return tokenMap[ph]
	}), nil
}
