// Package langcode reconhece códigos e nomes de idioma em nomes de arquivos e
// colunas de localização (en.csv, strings_pt_br.csv, coluna "English").
package langcode

import (
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

type language struct {
	code        string
	englishName string
	aliases     []string
}

// A ordem importa: a primeira entrada cujo alias casar vence.
var languages = []language{
	{"en", "English", []string{"en", "eng", "en_us", "en-us", "en_gb", "en-gb", "english", "ingles"}},
	{"pt", "Portuguese", []string{"pt", "por", "pt_br", "pt-br", "pt_pt", "pt-pt", "portuguese", "portugues", "brazilian"}},
	{"es", "Spanish", []string{"es", "spa", "es_es", "es-es", "es_419", "es-419", "spanish", "espanol", "castellano"}},
	{"fr", "French", []string{"fr", "fra", "fre", "french", "francais"}},
	{"de", "German", []string{"de", "deu", "ger", "german", "deutsch"}},
	{"it", "Italian", []string{"it", "ita", "italian", "italiano"}},
	{"ja", "Japanese", []string{"ja", "jp", "jpn", "japanese", "japones", "nihongo"}},
	{"ko", "Korean", []string{"ko", "kor", "korean", "coreano", "hangul"}},
	{"ru", "Russian", []string{"ru", "rus", "russian", "russo"}},
	{"zh", "Chinese", []string{"zh", "chi", "zho", "zh_cn", "zh-cn", "zh_tw", "zh-tw", "chinese", "chines", "schinese", "tchinese"}},
	{"pl", "Polish", []string{"pl", "pol", "polish", "polones"}},
	{"tr", "Turkish", []string{"tr", "tur", "turkish", "turco"}},
	{"uk", "Ukrainian", []string{"uk", "ukr", "ukrainian", "ucraniano"}},
	{"vi", "Vietnamese", []string{"vi", "vie", "vietnamese", "vietnamita"}},
}

var separatorRE = regexp.MustCompile(`[-_]`)

func stripAccents(s string) string {
	out, _, err := transform.String(transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC), s)
	if err != nil {
		return s
	}
	return out
}

func dashless(s string) string {
	return strings.NewReplacer("-", "", "_", "").Replace(s)
}

// Normalize converte um código ou nome de idioma ("pt_BR", "Português", "english")
// para o código canônico ("pt", "en"). Devolve "" quando não reconhece.
func Normalize(text string) string {
	if strings.TrimSpace(text) == "" {
		return ""
	}
	cleaned := stripAccents(strings.ToLower(strings.TrimSpace(text)))
	cleaned = strings.NewReplacer("_", "-", " ", "-").Replace(cleaned)

	for _, l := range languages {
		if cleaned == l.code {
			return l.code
		}
	}
	for _, l := range languages {
		for _, alias := range l.aliases {
			if cleaned == alias || dashless(cleaned) == dashless(alias) {
				return l.code
			}
		}
	}
	return ""
}

// EnglishName devolve o nome em inglês do idioma ("pt" -> "Portuguese").
func EnglishName(code string) string {
	n := Normalize(code)
	for _, l := range languages {
		if l.code == n {
			return l.englishName
		}
	}
	if code == "" {
		return ""
	}
	return strings.ToUpper(code[:1]) + strings.ToLower(code[1:])
}

// IsLanguageColumn devolve o código do idioma que o cabeçalho de coluna representa, ou "".
func IsLanguageColumn(column string) string {
	return Normalize(column)
}

// Pattern descreve onde o idioma aparece no nome do arquivo.
type Pattern string

const (
	CodeExact       Pattern = "code_exact"       // en.csv
	NameExact       Pattern = "name_exact"       // English.csv
	Suffix          Pattern = "suffix"           // strings_en.csv
	SuffixComposite Pattern = "suffix_composite" // dialogue_pt_br.csv
	Prefix          Pattern = "prefix"           // es_subtitles.csv
)

func stem(filename string) string {
	base := filepath.Base(filename)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

// FromFilename identifica o idioma de um arquivo de localização pelo nome.
func FromFilename(filename string) (code string, pattern Pattern, ok bool) {
	name := stem(filename)

	if code := Normalize(name); code != "" {
		for _, l := range languages {
			if strings.EqualFold(name, l.englishName) {
				return code, NameExact, true
			}
		}
		return code, CodeExact, true
	}

	parts := separatorRE.Split(name, -1)
	if len(parts) < 2 {
		return "", "", false
	}
	if code := Normalize(parts[len(parts)-1]); code != "" {
		return code, Suffix, true
	}
	if len(parts) >= 3 {
		if code := Normalize(parts[len(parts)-2] + "-" + parts[len(parts)-1]); code != "" {
			return code, SuffixComposite, true
		}
	}
	if code := Normalize(parts[0]); code != "" {
		return code, Prefix, true
	}
	return "", "", false
}

func isLower(s string) bool { return s == strings.ToLower(s) && s != strings.ToUpper(s) }
func isUpper(s string) bool { return s == strings.ToUpper(s) && s != strings.ToLower(s) }

// splitKeepingSeparators divide em torno de '-'/'_' mantendo os separadores,
// como re.split com grupo de captura em Python.
func splitKeepingSeparators(name string) []string {
	var parts []string
	last := 0
	for _, loc := range separatorRE.FindAllStringIndex(name, -1) {
		parts = append(parts, name[last:loc[0]], name[loc[0]:loc[1]])
		last = loc[1]
	}
	return append(parts, name[last:])
}

// TargetFilename gera o nome do arquivo traduzido preservando o padrão do original:
// en.csv -> pt.csv, English.csv -> Portuguese.csv, strings_en.csv -> strings_pt.csv.
// Arquivos sem idioma no nome recebem o código como sufixo (items.csv -> items_pt.csv).
func TargetFilename(source, targetLang string) string {
	dir := filepath.Dir(source)
	base := filepath.Base(source)
	ext := filepath.Ext(base)
	name := strings.TrimSuffix(base, ext)

	join := func(newBase string) string {
		if dir == "." && !strings.HasPrefix(source, "."+string(filepath.Separator)) {
			return newBase
		}
		return filepath.Join(dir, newBase)
	}

	_, pattern, ok := FromFilename(base)
	if !ok {
		return join(name + "_" + targetLang + ext)
	}

	target := Normalize(targetLang)
	if target == "" {
		target = strings.ToLower(targetLang)
	}

	switch pattern {
	case NameExact:
		targetName := EnglishName(target)
		if isLower(name) {
			targetName = strings.ToLower(targetName)
		} else if isUpper(name) {
			targetName = strings.ToUpper(targetName)
		}
		return join(targetName + ext)
	case CodeExact:
		if isUpper(name) {
			target = strings.ToUpper(target)
		}
		return join(target + ext)
	case Suffix, SuffixComposite:
		parts := splitKeepingSeparators(name)
		var newName string
		if pattern == SuffixComposite && len(parts) >= 5 {
			// O idioma ocupa os dois últimos segmentos (en_US): ambos são substituídos.
			// A versão Python mantinha parts[-3] e gerava "text_enpt.csv".
			newName = strings.Join(parts[:len(parts)-3], "") + target
		} else {
			newName = strings.Join(parts[:len(parts)-1], "") + target
		}
		return join(newName + ext)
	case Prefix:
		parts := splitKeepingSeparators(name)
		return join(target + strings.Join(parts[1:], "") + ext)
	}
	return join(name + "_" + target + ext)
}
