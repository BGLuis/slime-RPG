package csv

import (
	"strings"
	"testing"

	"github.com/BGLuis/slime-rpg/pkg/model"
)

func TestCSVExtractor_CanHandle(t *testing.T) {
	ext := NewCSVExtractor()
	if !ext.CanHandle("data/dialogues.csv") {
		t.Fatalf("expected to handle .csv file")
	}
	if !ext.CanHandle("data/DIALOGUES.CSV") {
		t.Fatalf("expected to handle .CSV file case-insensitive")
	}
	if ext.CanHandle("data/dialogues.json") {
		t.Fatalf("should not handle .json file")
	}
}

func TestCSVExtractor_SniffDelimiter(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected rune
	}{
		{"comma", "id,name,text\n1,Hero,Hello\n", ','},
		{"semicolon", "id;name;text\n1;Hero;Hello\n", ';'},
		{"tab", "id\tname\ttext\n1\tHero\tHello\n", '\t'},
		{"pipe", "id|name|text\n1|Hero|Hello\n", '|'},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SniffDelimiter([]byte(tt.input))
			if got != tt.expected {
				t.Errorf("SniffDelimiter() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestCSVExtractor_SniffBOMAndLineEnding(t *testing.T) {
	t.Run("with BOM and CRLF", func(t *testing.T) {
		input := append([]byte{0xEF, 0xBB, 0xBF}, []byte("id,text\r\n1,hello\r\n")...)
		hasBOM, clean, term := SniffBOMAndLineEnding(input)
		if !hasBOM {
			t.Errorf("expected hasBOM true")
		}
		if term != "\r\n" {
			t.Errorf("expected CRLF, got %q", term)
		}
		if string(clean) != "id,text\r\n1,hello\r\n" {
			t.Errorf("unexpected clean bytes: %s", string(clean))
		}
	})

	t.Run("without BOM and LF", func(t *testing.T) {
		input := []byte("id,text\n1,hello\n")
		hasBOM, clean, term := SniffBOMAndLineEnding(input)
		if hasBOM {
			t.Errorf("expected hasBOM false")
		}
		if term != "\n" {
			t.Errorf("expected LF, got %q", term)
		}
		if string(clean) != "id,text\n1,hello\n" {
			t.Errorf("unexpected clean bytes: %s", string(clean))
		}
	})
}

func TestCSVExtractor_ExtractAndInject(t *testing.T) {
	rawCSV := "id;speaker;text\r\n" +
		"1;Slime;Hello, traveler!\r\n" +
		"2;Slime;Are you ready?\r\n" +
		"3;42;100\r\n"

	ext := NewCSVExtractor()
	segments, err := ext.Extract("dialogue.csv", []byte(rawCSV), "en", "pt")
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	// Should extract 2 segments from "text" column (row 3 is pure numbers/codes)
	if len(segments) != 2 {
		t.Fatalf("expected 2 segments, got %d", len(segments))
	}

	if segments[0].Source() != "Hello, traveler!" {
		t.Errorf("unexpected segment 0 source: %s", segments[0].Source())
	}
	if segments[1].Source() != "Are you ready?" {
		t.Errorf("unexpected segment 1 source: %s", segments[1].Source())
	}

	// Resolve translations
	var resolved []*model.Segment
	for _, s := range segments {
		var tr string
		if strings.Contains(s.Source(), "Hello") {
			tr = "Olá, viajante!"
		} else {
			tr = "Você está pronto?"
		}
		resolved = append(resolved, s.WithTarget(tr))
	}

	injected, err := ext.Inject("dialogue.csv", []byte(rawCSV), resolved, "pt-br")
	if err != nil {
		t.Fatalf("Inject failed: %v", err)
	}

	injectedStr := string(injected)
	if !strings.Contains(injectedStr, "Olá, viajante!") {
		t.Errorf("expected injected text to contain 'Olá, viajante!', got:\n%s", injectedStr)
	}
	if !strings.Contains(injectedStr, "Você está pronto?") {
		t.Errorf("expected injected text to contain 'Você está pronto?', got:\n%s", injectedStr)
	}
	if !strings.Contains(injectedStr, "\r\n") {
		t.Errorf("expected CRLF line endings to be preserved")
	}
	if !strings.Contains(injectedStr, ";") {
		t.Errorf("expected semicolon delimiter to be preserved")
	}
}

func TestCSVExtractor_ExplicitColumns(t *testing.T) {
	rawCSV := "id,name,description,notes\n" +
		"1,Potion,Heals 50 HP,Internal test note\n"

	ext := NewCSVExtractor()
	ext.Columns = []string{"name", "description"}

	segments, err := ext.Extract("items.csv", []byte(rawCSV), "en", "pt")
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	if len(segments) != 2 {
		t.Fatalf("expected 2 segments (name and description), got %d", len(segments))
	}

	foundName := false
	foundDesc := false
	for _, s := range segments {
		if s.Source() == "Potion" {
			foundName = true
		}
		if s.Source() == "Heals 50 HP" {
			foundDesc = true
		}
	}

	if !foundName || !foundDesc {
		t.Errorf("did not extract expected explicit columns")
	}
}

func translateAll(t *testing.T, ext *CSVExtractor, file, raw string, fn func(string) string) string {
	t.Helper()
	segs, err := ext.Extract(file, []byte(raw), "en", "pt")
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	var resolved []*model.Segment
	for _, s := range segs {
		resolved = append(resolved, s.WithTarget(fn(s.Source())))
	}
	out, err := ext.Inject(file, []byte(raw), resolved, "pt")
	if err != nil {
		t.Fatalf("Inject: %v", err)
	}
	return string(out)
}

func identity(s string) string { return s }
func prefixPT(s string) string { return "[PT] " + s }

func sources(t *testing.T, ext *CSVExtractor, file, raw string) []string {
	t.Helper()
	segs, err := ext.Extract(file, []byte(raw), "en", "pt")
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	var out []string
	for _, s := range segs {
		out = append(out, s.Source())
	}
	return out
}

func TestRoundTripWithIdentityTranslationIsByteIdentical(t *testing.T) {
	for name, raw := range map[string]string{
		"LF sem BOM":   "id,text\n1,Hello there\n2,Good bye\n",
		"CRLF com BOM": "\xef\xbb\xbfid,text\r\n1,Hello there\r\n2,Good bye\r\n",
	} {
		t.Run(name, func(t *testing.T) {
			if got := translateAll(t, NewCSVExtractor(), "sample.csv", raw, identity); got != raw {
				t.Errorf("saída difere da entrada:\n%q\n%q", got, raw)
			}
		})
	}
}

func TestEachFileKeepsItsOwnDelimiter(t *testing.T) {
	ext := NewCSVExtractor()
	comma := translateAll(t, ext, "en/strings.csv", "id,text\n10,Attack\n20,Defend\n", identity)
	semi := translateAll(t, ext, "menu/strings.csv", "id;text\n1;Start Game\n2;Options\n", identity)
	if !strings.Contains(comma, ",") || strings.Contains(comma, ";") {
		t.Errorf("arquivo com vírgula alterado: %q", comma)
	}
	if !strings.Contains(semi, ";") || strings.Contains(semi, ",") {
		t.Errorf("arquivo com ponto e vírgula alterado: %q", semi)
	}
}

func TestTranslatesInPlaceWithoutOldColumns(t *testing.T) {
	got := translateAll(t, NewCSVExtractor(), "dialogues.csv", "id,text\n1,Good morning\n", prefixPT)
	if want := "id,text\n1,[PT] Good morning\n"; got != want {
		t.Errorf("obteve %q, esperava %q", got, want)
	}
}

// Numa tabela multilíngue a coluna do idioma de origem é dado do jogo: sobrescrevê-la
// apagaria o inglês. A tradução vai para a coluna do idioma de destino.
func TestMultilanguageTableWritesTargetColumnAndKeepsSource(t *testing.T) {
	raw := "\xef\xbb\xbfkey,en,es\nHELLO,Hello,Hola\nBYE,Goodbye,Adios\n"
	ext := NewCSVExtractor()
	if got := sources(t, ext, "localization.csv", raw); strings.Join(got, "|") != "Hello|Goodbye" {
		t.Fatalf("deve extrair só da coluna en, obteve %v", got)
	}

	tr := map[string]string{"Hello": "Olá", "Goodbye": "Adeus"}
	got := translateAll(t, ext, "localization.csv", raw, func(s string) string { return tr[s] })
	want := "\xef\xbb\xbfkey,en,es,pt\nHELLO,Hello,Hola,Olá\nBYE,Goodbye,Adios,Adeus\n"
	if got != want {
		t.Errorf("obteve %q, esperava %q", got, want)
	}
}

func TestMultilanguageTableReusesExistingTargetColumnAndSkipsFilledRows(t *testing.T) {
	raw := "key,en,pt\nHELLO,Hello,Oi\nBYE,Goodbye,\n"
	ext := NewCSVExtractor()
	if got := sources(t, ext, "loc.csv", raw); strings.Join(got, "|") != "Goodbye" {
		t.Fatalf("linha já traduzida não deve ser retraduzida, extraiu %v", got)
	}
	got := translateAll(t, ext, "loc.csv", raw, func(string) string { return "Adeus" })
	if want := "key,en,pt\nHELLO,Hello,Oi\nBYE,Goodbye,Adeus\n"; got != want {
		t.Errorf("obteve %q, esperava %q", got, want)
	}
}

func TestMultilanguageTargetHeaderFollowsSourceStyle(t *testing.T) {
	for raw, wantHeader := range map[string]string{
		"key,EN\nA,Hi\n":      "key,EN,PT",
		"key,English\nA,Hi\n": "key,English,Portuguese",
	} {
		got := translateAll(t, NewCSVExtractor(), "loc.csv", raw, prefixPT)
		if !strings.HasPrefix(got, wantHeader+"\n") {
			t.Errorf("cabeçalho esperado %q, saída %q", wantHeader, got)
		}
	}
}

func TestHeaderAndIdentifierColumnsAreNeverTranslated(t *testing.T) {
	ext := NewCSVExtractor()
	if got := sources(t, ext, "pairs.csv", "key,val\nmsg_hi,hi there\n"); strings.Join(got, "|") != "hi there" {
		t.Errorf("planilha de duas colunas deve traduzir só a coluna de texto, obteve %v", got)
	}
	if got := sources(t, ext, "pairs.csv", "phrase,key\nHello world,greet\n"); strings.Join(got, "|") != "Hello world" {
		t.Errorf("a coluna identificadora pode vir primeiro, obteve %v", got)
	}
	if got := sources(t, ext, "table.csv", "alpha,beta,gamma\nHello,World,Again\n"); len(got) != 0 {
		t.Errorf("sem coluna reconhecível nada deve ser traduzido (nem o cabeçalho), obteve %v", got)
	}
}

func TestSkipsSectionTitles(t *testing.T) {
	if got := sources(t, NewCSVExtractor(), "d.csv", "id,text\n1,■ Chapter 1\n2,Hello\n"); strings.Join(got, "|") != "Hello" {
		t.Errorf("títulos com ■ devem ser ignorados, obteve %v", got)
	}
}

func TestIsTranslatableFile(t *testing.T) {
	cases := map[string]bool{
		"en.csv": true, "English.csv": true, "strings_en.csv": true, "dialogue.csv": true,
		"es.csv": false, "fr.csv": false, "Spanish.csv": false,
	}
	for file, want := range cases {
		if got := IsTranslatableFile(file, "en"); got != want {
			t.Errorf("IsTranslatableFile(%q) = %v, esperava %v", file, got, want)
		}
	}
}

func TestMaskPatternsProtectLocalizationTokens(t *testing.T) {
	sample := `Hello \c[2]hero\c[0], you found \item<42>! Name: {hero_name}, score: %d.`
	matched := map[string]bool{}
	for _, p := range MaskPatterns() {
		for _, m := range p.FindAllString(sample, -1) {
			matched[m] = true
		}
	}
	for _, want := range []string{`\c[2]`, `\c[0]`, `\item<42>`, "{hero_name}", "%d"} {
		if !matched[want] {
			t.Errorf("%q deveria ser protegido; protegidos: %v", want, matched)
		}
	}
}
