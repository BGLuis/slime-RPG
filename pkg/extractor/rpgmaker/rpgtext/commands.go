package rpgtext

import (
	"regexp"
	"sort"
	"strings"
)

// Span é um trecho [start, end) de uma string do JSON que vira um segmento. Os
// comandos do RPG Maker misturam texto e sintaxe na mesma string (título <Nome>,
// prefixo "name:", condição show_if(...), literais dentro de JavaScript); só o
// trecho de texto é traduzido e o resto da string é preservado na injeção.
type Span struct {
	Start, End int
	Source     string // texto enviado à tradução (literais JS sem o escape das aspas)
	Mode       string // como codificar a tradução: "" (texto puro), modeScript ou modeControlVar
	Quote      string // delimitador do literal JS
}

const (
	modeScript     = "script"
	modeControlVar = "cvar"
)

// Whole é a string inteira como um único trecho.
func Whole(s string) Span { return Span{Start: 0, End: len(s), Source: s} }

func sub(s string, start, end int) Span { return Span{Start: start, End: end, Source: s[start:end]} }

// --- Mostrar texto (101, 401, 405) e mudanças de nome/perfil (320, 324, 325) ---

var (
	titleRE  = regexp.MustCompile(`^(?:<<([^<>]*)>>|<([^<>]*)>)(\s*)`)
	textKeys = []string{"icon:", "secret:", "title:", "name:", "text:", "message:", "text1:", "text2:", "secretText:", "id:"}
)

// ShowTextSpans segue o ShowTextStrategy do Python: "<Nome>fala" separa título e
// fala; "name: valor" traduz só o valor; o resto é traduzido inteiro.
func ShowTextSpans(s string) []Span {
	if IsTechnicalOrCode(s, Dialogue) {
		return nil
	}
	if m := titleRE.FindStringSubmatchIndex(s); m != nil && !isMarkupTitle(s, m) {
		ts, te := m[2], m[3]
		if ts < 0 {
			ts, te = m[4], m[5]
		}
		titleOK := !IsTechnicalOrCode(s[ts:te], Dialogue)
		bs, be := trimSpan(s, m[1], len(s))
		if bs == be {
			if titleOK {
				return []Span{sub(s, ts, te)}
			}
			return nil
		}
		if IsTechnicalOrCode(s[bs:be], Dialogue) {
			return nil
		}
		if titleOK {
			return []Span{sub(s, ts, te), sub(s, bs, be)}
		}
		return []Span{sub(s, bs, be)}
	}
	for _, key := range textKeys {
		if !strings.HasPrefix(s, key) {
			continue
		}
		colon := strings.Index(s, ":") + 1
		bs, be := trimSpan(s, colon, len(s))
		rest := s[bs:be]
		lower := strings.ToLower(rest)
		if bs == be || IsTechnicalOrCode(rest, Dialogue) || IsNumeric(rest) || lower == "true" || lower == "false" {
			return nil
		}
		return []Span{sub(s, bs, be)}
	}
	return []Span{Whole(s)}
}

// markupTags são tags de formatação de mensagem (HTML e plugins como o YEP
// Message Core) que aparecem no início da fala e não são nome de locutor.
var markupTags = toSet("b", "i", "u", "s", "br", "center", "left", "right", "wordwrap", "font", "color", "size", "small", "big")

// isMarkupTitle evita tratar "<b>Oi</b>" ou "<WordWrap>" como título "<Nome>".
func isMarkupTitle(s string, m []int) bool {
	if m[2] >= 0 {
		return false
	}
	title := s[m[4]:m[5]]
	if strings.HasPrefix(title, "/") || strings.Contains(title, "=") || markupTags[strings.ToLower(title)] {
		return true
	}
	return strings.Contains(strings.ToLower(s[m[1]:]), "</"+strings.ToLower(title)+">")
}

func trimSpan(s string, start, end int) (int, int) {
	for start < end && isSpace(s[start]) {
		start++
	}
	for end > start && isSpace(s[end-1]) {
		end--
	}
	return start, end
}

func isSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r' || b == '\f' || b == '\v'
}

// --- Escolhas (102, 402) ---

// ChoiceSpan traduz só o texto da escolha, preservando uma condição inline como
// show_if(v[1] > 0).
func ChoiceSpan(s string) (Span, bool) {
	prefix, body := ParseChoiceCondition(s)
	if IsTechnicalOrCode(body, Dialogue) {
		return Span{}, false
	}
	start := len(prefix)
	return sub(s, start, start+len(body)), true
}

// --- Scripts (355, 655, passo de rota 45) ---

var (
	originalMessageRE = regexp.MustCompile(`(?i)\$gameScreen\.OriginalMessage\s*\(\s*`)
	safeCallRE        = []*regexp.Regexp{
		regexp.MustCompile(`(?i)(?:\$gameMessage\.add|this\.addCommand|this\.setHelpWindowText|TickerManager\.show)\s*\(\s*`),
		regexp.MustCompile(`(?i)BattleManager\._logWindow\.push\s*\(\s*['"]addText['"]\s*,\s*`),
		regexp.MustCompile(`(?i)(?:mes|text)\s*=\s*`),
	}
	commaRE     = regexp.MustCompile(`^\s*,\s*`)
	closeCallRE = regexp.MustCompile(`^\s*\)`)
	subjectRE   = regexp.MustCompile(`\d_t=\d{2,}_subject=`)
)

// ScriptSpans extrai, de um script JavaScript, só os literais de texto passados a
// funções conhecidas de mensagem (a safelist do ScriptStrategy do Python).
func ScriptSpans(script string) []Span {
	var spans []Span
	taken := func(start, end int) bool {
		for _, sp := range spans {
			if start < sp.End && sp.Start < end {
				return true
			}
		}
		return false
	}
	addLiteral := func(q byte, start, end int) {
		text := unescapeQuotes(script[start:end])
		if taken(start, end) || IsTechnicalOrCode(text, Dialogue) {
			return
		}
		spans = append(spans, Span{Start: start, End: end, Source: text, Mode: modeScript, Quote: string(q)})
	}

	for _, loc := range originalMessageRE.FindAllStringIndex(script, -1) {
		q1, s1, e1, ok := jsLiteral(script, loc[1])
		if !ok {
			continue
		}
		c := commaRE.FindStringIndex(script[e1+1:])
		if c == nil {
			continue
		}
		q2, s2, e2, ok := jsLiteral(script, e1+1+c[1])
		if !ok {
			continue
		}
		addLiteral(q1, s1, e1)
		addLiteral(q2, s2, e2)
	}

	for i, re := range safeCallRE {
		for _, loc := range re.FindAllStringIndex(script, -1) {
			q, s, e, ok := jsLiteral(script, loc[1])
			if !ok {
				continue
			}
			if i == 1 && closeCallRE.FindStringIndex(script[e+1:]) == nil {
				continue
			}
			addLiteral(q, s, e)
		}
	}

	if len(spans) == 0 && subjectRE.MatchString(script) {
		start := strings.Index(script, "_subject=") + len("_subject=")
		end := len(script)
		if next := strings.Index(script[start:], "_subject="); next >= 0 {
			end = start + next
		}
		for start < end && script[start] == '"' {
			start++
		}
		for end > start && script[end-1] == '"' {
			end--
		}
		if text := script[start:end]; text != "" && !IsTechnicalOrCode(text, Dialogue) {
			spans = append(spans, Span{Start: start, End: end, Source: text, Mode: modeScript, Quote: `"`})
		}
	}

	sort.Slice(spans, func(i, j int) bool { return spans[i].Start < spans[j].Start })
	return spans
}

// jsLiteral lê o literal JS que abre em s[i] e devolve o conteúdo [start, end),
// sem as aspas. Como no Python, o literal não atravessa quebras de linha.
func jsLiteral(s string, i int) (quote byte, start, end int, ok bool) {
	if i >= len(s) || (s[i] != '"' && s[i] != '\'') {
		return 0, 0, 0, false
	}
	quote = s[i]
	for j := i + 1; j < len(s); j++ {
		switch s[j] {
		case '\n':
			return 0, 0, 0, false
		case '\\':
			if j+1 < len(s) && s[j+1] != '\n' {
				j++
				continue
			}
			return 0, 0, 0, false
		case quote:
			return quote, i + 1, j, true
		}
	}
	return 0, 0, 0, false
}

// unescapeQuotes remove a barra de aspas escapadas (\' e \"), como o
// _unescape_quotes do Python: os demais escapes JS ficam como estão.
func unescapeQuotes(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) && (s[i+1] == '"' || s[i+1] == '\'') && (i == 0 || s[i-1] != '\\') {
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// sanitizeScriptLiteral prepara a tradução para voltar a um literal JS delimitado
// por quote. A origem já é conteúdo de literal (só as aspas foram desescapadas),
// então só o delimitador e quebras de linha reais são escapados; escapes como \n
// e \\C[2] vindos da origem continuam válidos. Aspas que o tradutor colocou em
// volta de uma fala entre 「」 ou 『』 são removidas (issue #36 da versão Python).
func sanitizeScriptLiteral(text, quote, original string) string {
	s := strings.TrimSpace(text)
	orig := strings.TrimSpace(original)
	if (strings.HasPrefix(orig, "「") && strings.HasSuffix(orig, "」")) || (strings.HasPrefix(orig, "『") && strings.HasSuffix(orig, "』")) {
		for {
			stripped := stripQuotePair(s)
			if stripped == s {
				break
			}
			s = strings.TrimSpace(stripped)
		}
	}

	if preEscapedCodeRE.MatchString(original) {
		s = doublePreEscapedCodes(s)
	}

	var b strings.Builder
	backslashes := 0
	for _, r := range s {
		switch {
		case string(r) == quote && backslashes%2 == 0:
			b.WriteString(`\` + quote)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r == ' ':
			b.WriteString(` `)
		case r == ' ':
			b.WriteString(` `)
		default:
			b.WriteRune(r)
		}
		if r == '\\' {
			backslashes++
		} else {
			backslashes = 0
		}
	}
	return b.String()
}

var (
	preEscapedCodeRE = regexp.MustCompile(`\\\\[A-Za-z]+\[[^\]]*\]`)
	escapedCodeRunRE = regexp.MustCompile(`\\+[A-Za-z]+\[[^\]]*\]`)
)

// doublePreEscapedCodes devolve a barra dupla de códigos como \\C[2] quando o
// tradutor a reduziu a uma barra só: dentro de um literal JS "\C[2]" vira "C[2]"
// em execução e o código de cor se perde (ScriptStrategy._PRE_ESCAPED_FORMAT_CODE).
func doublePreEscapedCodes(s string) string {
	return escapedCodeRunRE.ReplaceAllStringFunc(s, func(m string) string {
		if strings.HasPrefix(m, `\\`) {
			return m
		}
		return `\` + m
	})
}

func stripQuotePair(s string) string {
	for _, pair := range [][2]string{{`"`, `"`}, {`'`, `'`}, {"“", "”"}, {"‘", "’"}} {
		if len(s) >= len(pair[0])+len(pair[1]) && strings.HasPrefix(s, pair[0]) && strings.HasSuffix(s, pair[1]) {
			return s[len(pair[0]) : len(s)-len(pair[1])]
		}
	}
	return s
}

// Encode devolve o texto que substitui o trecho original.
func (sp Span) Encode(target string) string {
	switch sp.Mode {
	case modeScript:
		return sanitizeScriptLiteral(target, sp.Quote, sp.Source)
	case modeControlVar:
		t := strings.NewReplacer("“", `"`, "”", `"`, "‘", "'", "’", "'").Replace(target)
		t = strings.TrimSpace(t)
		if sp.Quote != "" {
			if len(t) >= 2 && strings.HasPrefix(t, sp.Quote) && strings.HasSuffix(t, sp.Quote) {
				t = t[1 : len(t)-1]
			}
			return sanitizeScriptLiteral(t, sp.Quote, sp.Source)
		}
		return t
	}
	return target
}

// --- Comando de plugin MV (356) ---

var pluginMVPrefixes = []*regexp.Regexp{
	regexp.MustCompile(`^D_TEXT`),
	regexp.MustCompile(`^addLog`),
	regexp.MustCompile(`^InformationWindow \d+ Text:`),
	regexp.MustCompile(`^mes =`),
}

func PluginMVSpan(s string) (Span, bool) {
	for _, re := range pluginMVPrefixes {
		prefix := re.FindString(s)
		if prefix == "" {
			continue
		}
		idx := strings.Index(s, prefix+" ")
		if idx < 0 {
			continue
		}
		start := idx + len(prefix) + 1
		if IsTechnicalOrCode(s[start:], Dialogue) {
			return Span{}, false
		}
		return sub(s, start, len(s)), true
	}
	return Span{}, false
}

// --- Comando de plugin MZ (357) ---

var (
	mzTechnicalKeys = toSet(
		"switchTypeEX", "switchIdEX", "targetIdEX", "variableIdEX",
		"loop", "smooth", "volume", "reload", "autoplay", "finishSwitch",
		"fileName", "pitch", "pan", "wait", "repeat", "skippable",
		"WordWrap", "FontFace", "FontSize", "TextSpeed", "AutoColor",
		"MessageRows", "MessageWidth", "ChoiceLineHeight", "ChoiceRows",
		"switchType", "variableId", "value", "operation", "operand",
		"image", "picture", "icon", "se", "bgm", "bgs", "me",
		"windowPosition", "direction", "id", "animationType", "customPattern",
		"cellNumber", "frameNumber", "fade", "pictureNumber", "align",
		"position", "type", "mode", "easing", "color", "opacity", "blendMode",
	)
	mzTextKeys     = toSet("text", "message", "description", "title", "name", "label", "tooltip", "help", "content", "body")
	mzTextSuffixes = []string{"text", "message", "description", "title", "label", "tooltip", "help", "content", "body"}
	mzCJKRE        = regexp.MustCompile(`[\x{3040}-\x{9fff}]`)
)

func toSet(items ...string) map[string]bool {
	m := make(map[string]bool, len(items))
	for _, it := range items {
		m[it] = true
	}
	return m
}

func mzIsTextKey(key string) bool {
	k := strings.ToLower(key)
	if mzTextKeys[k] {
		return true
	}
	for _, sfx := range mzTextSuffixes {
		if strings.HasSuffix(k, sfx) {
			return true
		}
	}
	return false
}

// MZPluginTextKeys segue o PluginStrategyMZ do Python: chaves de texto ou valores
// com cara de frase (espaço, pontuação ou CJK), nunca chaves técnicas.
func MZPluginTextKeys(args map[string]any) []string {
	var keys []string
	for _, key := range sortedKeys(args) {
		if mzTechnicalKeys[key] {
			continue
		}
		val, ok := args[key].(string)
		if !ok {
			continue
		}
		ctx := Param
		if mzIsTextKey(key) {
			ctx = Dialogue
		}
		if IsTechnicalOrCode(val, ctx) {
			continue
		}
		if mzIsTextKey(key) || strings.Contains(val, " ") || strings.ContainsAny(val, ".,;:!?　、。！？") || mzCJKRE.MatchString(val) {
			keys = append(keys, key)
		}
	}
	return keys
}

// --- Controle de variáveis (122) com script ---

var (
	difficultyRE      = regexp.MustCompile(`(?i)^['"]?\s*(?:easy|normal|hard|very\s*hard|expert|nightmare|hell|beginner|insane|lunatic)\s*['"]?;?$`)
	asciiIdentifierRE = regexp.MustCompile(`^[A-Za-z0-9_.\-]+$`)
)

// ControlVarSpan segue o ControlVariablesStrategy do Python: no operando script
// (params[3] == 4) só literais de texto e frases soltas são traduzidos; enums de
// dificuldade, identificadores e expressões ficam como estão.
func ControlVarSpan(val string) (Span, bool) {
	trimmedAll := strings.TrimSpace(val)
	if trimmedAll == "" || difficultyRE.MatchString(trimmedAll) {
		return Span{}, false
	}
	lower := strings.ToLower(trimmedAll)
	if IsTechnicalOrCode(val, Dialogue) || IsNumeric(val) || lower == "true" || lower == "false" {
		return Span{}, false
	}

	start, end := trimSpan(val, 0, len(val))
	for end > start && val[end-1] == ';' {
		end--
	}
	trimmed := val[start:end]

	if len(trimmed) >= 2 && strings.ContainsRune(`"'`+"`", rune(trimmed[0])) && trimmed[len(trimmed)-1] == trimmed[0] {
		quote := trimmed[:1]
		inner := unescapeQuotes(trimmed[1 : len(trimmed)-1])
		if IsTechnicalOrCode(inner, Dialogue) {
			return Span{}, false
		}
		if !cjkRE.MatchString(inner) && isASCII(inner) && (asciiIdentifierRE.MatchString(inner) || !strings.Contains(inner, " ")) {
			return Span{}, false
		}
		return Span{Start: start + 1, End: end - 1, Source: inner, Mode: modeControlVar, Quote: quote}, true
	}

	if strings.Contains(trimmed, " ") && !strings.ContainsAny(trimmed, "$()=;+[]<>{}") {
		if cjkRE.MatchString(trimmed) || (!asciiIdentifierRE.MatchString(trimmed) && !IsTechnicalOrCode(trimmed, Dialogue)) {
			return Span{Start: start, End: end, Source: trimmed, Mode: modeControlVar}, true
		}
	}
	return Span{}, false
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] > 127 {
			return false
		}
	}
	return true
}

// --- Notas do banco de dados ---

var noteTagRE = regexp.MustCompile(`(?i)<(Desc|Help|Text|Profile|Message|Info[^:>]*):\s*([^>]+)>`)

// NoteSpans traduz só o conteúdo de tags de nota de texto (<Desc: ...>, <Help: ...>);
// o resto da nota são diretivas de plugin.
func NoteSpans(note string) []Span {
	var spans []Span
	for _, m := range noteTagRE.FindAllStringSubmatchIndex(note, -1) {
		s, e := trimSpan(note, m[4], m[5])
		if s < e && !IsTechnicalOrCode(note[s:e], Dialogue) {
			spans = append(spans, sub(note, s, e))
		}
	}
	return spans
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

var cjkRE = regexp.MustCompile(`[\x{3040}-\x{30ff}\x{3400}-\x{4dbf}\x{4e00}-\x{9fff}\x{f900}-\x{faff}\x{ac00}-\x{d7af}]`)
