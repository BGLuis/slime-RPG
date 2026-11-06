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

// Masker gerencia a proteção e desmascaramento de trechos protegidos (tokens) em textos.
type Masker struct {
	patterns []*regexp.Regexp
	prefix   string
}

// NewMasker cria um novo mascarador com os padrões de regex especificados.
func NewMasker(patterns []*regexp.Regexp) *Masker {
	if len(patterns) == 0 {
		patterns = DefaultRPGPatterns()
	}
	return &Masker{
		patterns: patterns,
		prefix:   "__XTOK_",
	}
}

// Mask substitui todos os trechos do texto que correspondem aos padrões configurados
// por placeholders determinísticos sequenciais (__XTOK_0__, __XTOK_1__, etc.).
// Retorna a string mascarada e o mapa [placeholder] -> tokenOriginal.
func (m *Masker) Mask(text string) (string, map[string]string) {
	tokenMap := make(map[string]string)
	if text == "" {
		return "", tokenMap
	}

	counter := 0
	result := text

	for _, pattern := range m.patterns {
		result = pattern.ReplaceAllStringFunc(result, func(match string) string {
			// Se o trecho já for exatamente um placeholder gerado nesta rodada, preserva
			if strings.HasPrefix(match, m.prefix) && strings.HasSuffix(match, "__") {
				if _, ok := tokenMap[match]; ok {
					return match
				}
			}

			// Gera placeholder único
			var ph string
			for {
				ph = fmt.Sprintf("%s%d__", m.prefix, counter)
				counter++
				// Garante que o placeholder não exista no texto original nem no mapa
				if !strings.Contains(text, ph) {
					if _, exists := tokenMap[ph]; !exists {
						break
					}
				}
			}

			tokenMap[ph] = match
			return ph
		})
	}

	return result, tokenMap
}

// Unmask restaura os placeholders substituindo-os pelos tokens originais e valida a integridade.
// Retorna ErrMaskIntegrity se algum placeholder foi perdido, corrompido ou sobrou no texto traduzido.
func (m *Masker) Unmask(translatedText string, tokenMap map[string]string) (string, error) {
	if len(tokenMap) == 0 {
		// Se não havia tokens mapeados, mas o texto traduzido contém o prefixo de placeholder, houve vazamento
		if strings.Contains(translatedText, m.prefix) {
			return "", fmt.Errorf("%w: unexpected placeholder detected in output: %s", ErrMaskIntegrity, translatedText)
		}
		return translatedText, nil
	}

	result := translatedText

	// Substitui cada placeholder mapeado pelo seu valor original
	for ph, originalToken := range tokenMap {
		if !strings.Contains(result, ph) {
			// O tradutor removeu ou corrompeu o placeholder
			return "", fmt.Errorf("%w: placeholder %s missing in translated text: %q", ErrMaskIntegrity, ph, translatedText)
		}
		result = strings.ReplaceAll(result, ph, originalToken)
	}

	// Verificação de segurança: nenhum resquício de placeholder pode sobrar
	if strings.Contains(result, m.prefix) {
		return "", fmt.Errorf("%w: residual placeholder detected after unmasking: %q", ErrMaskIntegrity, result)
	}

	return result, nil
}
