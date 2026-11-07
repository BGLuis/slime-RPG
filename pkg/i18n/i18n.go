// Package i18n fornece as tabelas de strings PT e EN da interface e a
// resolução do idioma ativo. As chaves são as mesmas para todos os idiomas;
// uma chave ausente em EN cai para PT e, se não existir, devolve a própria chave.
package i18n

import (
	"fmt"
	"strings"
	"sync/atomic"
)

// Lang é o código de idioma da interface.
type Lang string

const (
	PT      Lang = "pt"
	EN      Lang = "en"
	Default      = PT
)

// SettingKey é a chave em settings.db compartilhada com a versão Python.
const SettingKey = "ui_language"

// Supported lista os idiomas de interface disponíveis.
var Supported = []Lang{PT, EN}

// Names são os nomes de cada idioma na própria língua (seletores de UI).
var Names = map[Lang]string{PT: "Português", EN: "English"}

var tables = map[Lang]map[string]string{PT: pt, EN: en}

var current atomic.Value

// Normalize converte códigos como "pt_BR.UTF-8" ou "en-US" em um idioma suportado.
func Normalize(code string) (Lang, bool) {
	code = strings.ToLower(strings.TrimSpace(code))
	for _, sep := range []string{".", "@", "_", "-", ":"} {
		code, _, _ = strings.Cut(code, sep)
	}
	for _, l := range Supported {
		if code == string(l) {
			return l, true
		}
	}
	return "", false
}

// DetectSystem lê o locale do sistema (mesma ordem de variáveis do Python).
func DetectSystem(getenv func(string) string) (Lang, bool) {
	for _, name := range []string{"LANGUAGE", "LC_ALL", "LC_MESSAGES", "LANG"} {
		if l, ok := Normalize(getenv(name)); ok {
			return l, true
		}
	}
	return "", false
}

// Resolve aplica a ordem: valor explícito (flag), SLIME_LANG, preferência
// persistida, locale do sistema e, por fim, PT.
func Resolve(explicit, stored string, getenv func(string) string) Lang {
	for _, candidate := range []string{explicit, getenv("SLIME_LANG"), stored} {
		if l, ok := Normalize(candidate); ok {
			return l
		}
	}
	if l, ok := DetectSystem(getenv); ok {
		return l
	}
	return Default
}

// Set define o idioma global usado por T. Códigos inválidos são ignorados.
func Set(l Lang) {
	if _, ok := Normalize(string(l)); ok {
		current.Store(l)
	}
}

// Current devolve o idioma global ativo.
func Current() Lang {
	if l, ok := current.Load().(Lang); ok {
		return l
	}
	return Default
}

// TrIn traduz key para o idioma l, formatando com fmt.Sprintf quando há args.
func TrIn(l Lang, key string, args ...any) string {
	text, ok := tables[l][key]
	if !ok {
		if text, ok = tables[Default][key]; !ok {
			return key
		}
	}
	if len(args) == 0 {
		return text
	}
	return fmt.Sprintf(text, args...)
}

// T traduz key para o idioma global.
func T(key string, args ...any) string { return TrIn(Current(), key, args...) }

// Table devolve uma cópia da tabela de l já com fallback para PT, para a UI web.
func Table(l Lang) map[string]string {
	out := make(map[string]string, len(pt))
	for k, v := range pt {
		out[k] = v
	}
	for k, v := range tables[l] {
		out[k] = v
	}
	return out
}
