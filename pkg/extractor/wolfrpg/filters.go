package wolfrpg

import (
	"regexp"
	"strings"
	"unicode"

	"github.com/BGLuis/slime-rpg/pkg/pipeline/masking"
)

var (
	mediaExtensions = []string{
		".png", ".jpg", ".jpeg", ".bmp", ".gif",
		".ogg", ".wav", ".mp3", ".mid", ".midi", ".flac", ".aac",
		".txt", ".csv", ".dat", ".mps", ".wolf", ".project",
	}

	technicalPrefixes = []string{
		"data/", "system/", "graphic/", "picture/", "sound/", "bgm/", "bgs/", "se/", "mapdata/", "basicdata/", "chara/", "chipset/",
		"data\\", "system\\", "graphic\\", "picture\\", "sound\\", "bgm\\", "bgs\\", "se\\", "mapdata\\", "basicdata\\", "chara\\", "chipset\\",
	}

	reIdentifierWithUnderscore = regexp.MustCompile(`^[A-Za-z0-9]*_[A-Za-z0-9_]*$`)
	reUIPair                   = regexp.MustCompile(`^\pL+(?:\s*/\s*\pL+)+$`)
	reSlashPath                = regexp.MustCompile(`^[A-Za-z0-9_\-.]*[/\\]+[A-Za-z0-9_\-./\\]*$`)
	reMathOrSymbols            = regexp.MustCompile(`^[0-9+\-*/=<>!&|~^%()\[\]\s]+$`)
	reOnlyNumbers              = regexp.MustCompile(`^[0-9]+$`)
	reControlCode              = regexp.MustCompile(`\\[A-Za-z]+(?:\[[^\]]*\])?|\\[^A-Za-z]`)
)

func isAlphanumericWithLetterAndDigit(s string) bool {
	hasLetter := false
	hasDigit := false
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			hasLetter = true
		} else if r >= '0' && r <= '9' {
			hasDigit = true
		} else {
			return false
		}
	}
	return hasLetter && hasDigit
}

// IsMediaPath verifica se a string representa um arquivo de mídia.
func IsMediaPath(text string) bool {
	clean := strings.ToLower(strings.TrimSpace(text))
	for _, ext := range mediaExtensions {
		if strings.HasSuffix(clean, ext) {
			return true
		}
	}
	return false
}

// ContainsMediaPath verifica se alguma das linhas contém caminho de arquivo.
func ContainsMediaPath(text string) bool {
	lines := strings.Split(text, "\n")
	for _, line := range lines {
		if IsMediaPath(line) {
			return true
		}
	}
	return false
}

// IsTechnicalOrCode avalia se a string é identificador técnico, fórmula ou comando interno.
func IsTechnicalOrCode(text string) bool {
	cleaned := strings.TrimSpace(text)
	if cleaned == "" || cleaned == "■" {
		return true
	}

	if IsMediaPath(cleaned) {
		return true
	}

	// Comandos internos de engine/script WOLF (ex.: <<GET_MAPEVENT_ID_FROM_NAME>>)
	if strings.HasPrefix(cleaned, "<<") && strings.Contains(cleaned, ">>") {
		return true
	}

	lower := strings.ToLower(cleaned)
	if lower == "true" || lower == "false" || lower == "null" || lower == "nil" || lower == "none" {
		return true
	}

	for _, prefix := range technicalPrefixes {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}

	// Expressões numéricas ou puramente simbólicas
	if reMathOrSymbols.MatchString(cleaned) || reOnlyNumbers.MatchString(cleaned) {
		return true
	}

	// Pares de interface como Yes/No ou Recruit/Remove são texto visível. Pastas
	// de recursos (Data/BGM, SE/...) já caíram nos prefixos técnicos acima.
	if reUIPair.MatchString(cleaned) {
		return false
	}

	// Identificadores curtos (ex: SE_01, bgm02, ev_flag) sem caracteres CJK nem espaços
	if len(cleaned) <= 24 && !strings.Contains(cleaned, " ") && !hasCJK(cleaned) {
		if reIdentifierWithUnderscore.MatchString(cleaned) ||
			isAlphanumericWithLetterAndDigit(cleaned) ||
			reSlashPath.MatchString(cleaned) {
			return true
		}
	}

	return false
}

// IsTranslatable verifica se o texto deve ser extraído para tradução.
func IsTranslatable(text string) bool {
	if IsTechnicalOrCode(text) || ContainsMediaPath(text) {
		return false
	}

	// Remove códigos de escape/controle (ex: \s[1], \c[2])
	stripped := reControlCode.ReplaceAllString(text, "")
	hasLetters := false
	for _, r := range stripped {
		if unicode.IsLetter(r) {
			hasLetters = true
			break
		}
	}
	if !hasLetters {
		return false
	}

	cleaned := strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(text, "\r", ""), "\n", ""))
	return len(cleaned) > 0
}

func hasCJK(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hiragana, r) || unicode.Is(unicode.Katakana, r) {
			return true
		}
	}
	return false
}

// MaskMatchers devolve os códigos de controle do WOLF RPG protegidos na tradução
// (mesmos padrões da versão Python). \r[base,leitura] (ruby) fica de fora: é texto
// visível e precisa ser traduzido.
func MaskMatchers() []masking.Matcher {
	return []masking.Matcher{
		regexp.MustCompile(`(?i)\\[cvs]\s*\[[^\]]*\]`),
		regexp.MustCompile(`(?i)\\(?:cself|self)\s*\[[^\]]*\]`),
		regexp.MustCompile(`(?i)\\font\s*\[[^\]]*\]`),
		regexp.MustCompile(`(?i)\\[wt]\s*\[[^\]]*\]`),
		regexp.MustCompile(`(?i)\\(?:[a-qs-z][a-z]{0,4}|r[a-z]{1,4})\s*\[[^\]]*\]`),
		regexp.MustCompile(`(?i)</?[a-z][a-z0-9]*(?:\s+[^>]*)?/?>`),
		regexp.MustCompile(`(?i)%[0-9]*[sdif]`),
	}
}
