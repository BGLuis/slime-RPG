package genericjson

import (
	"strings"
	"testing"

	"github.com/BGLuis/slime-rpg/pkg/model"
)

func TestJSONExtractor_CanHandle(t *testing.T) {
	ext := NewJSONExtractor()
	if !ext.CanHandle("data/dialogues.json") {
		t.Fatalf("expected to handle .json file")
	}
	if !ext.CanHandle("data/DIALOGUES.JSON") {
		t.Fatalf("expected to handle uppercase .JSON file")
	}
	if ext.CanHandle("data/dialogues.csv") {
		t.Fatalf("should not handle .csv file")
	}
}

func TestJSONExtractor_Pointers(t *testing.T) {
	raw := "hello/world~test"
	escaped := EscapePointer(raw)
	if escaped != "hello~1world~0test" {
		t.Fatalf("unexpected escaped pointer: %s", escaped)
	}
	unescaped := UnescapePointer(escaped)
	if unescaped != raw {
		t.Fatalf("unexpected unescaped pointer: %s", unescaped)
	}
}

func TestJSONExtractor_ExtractAndInject(t *testing.T) {
	rawJSON := `{
  "id": 101,
  "face": "Actor1",
  "image": "img/characters/hero.png",
  "audio": "se/attack.ogg",
  "stats": [10, 20, 30],
  "technical_flag": "$gameVariables.value(1)",
  "actors": [
    {
      "id": 1,
      "name": "Harold",
      "description": "A brave warrior",
      "sprite": "hero.png"
    },
    {
      "id": 2,
      "name": "Therese",
      "description": "A wise mage",
      "sprite": "mage.png"
    }
  ],
  "dialogue": [
    "Welcome to the kingdom!",
    "Beware of monsters."
  ]
}`

	ext := NewJSONExtractor()
	segments, err := ext.Extract("complex.json", []byte(rawJSON), "en")
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	// Should extract:
	// actors/0/name (Harold)
	// actors/0/description (A brave warrior)
	// actors/1/name (Therese)
	// actors/1/description (A wise mage)
	// dialogue/0 (Welcome to the kingdom!)
	// dialogue/1 (Beware of monsters.)
	// Total: 6 segments
	if len(segments) != 6 {
		t.Fatalf("expected 6 segments, got %d", len(segments))
	}

	var resolved []*model.Segment
	for _, s := range segments {
		var tr string
		switch s.Source() {
		case "Harold":
			tr = "Haroldo"
		case "A brave warrior":
			tr = "Um guerreiro corajoso"
		case "Therese":
			tr = "Teresa"
		case "A wise mage":
			tr = "Uma maga sábia"
		case "Welcome to the kingdom!":
			tr = "Bem-vindo ao reino!"
		case "Beware of monsters.":
			tr = "Cuidado com os monstros."
		default:
			t.Fatalf("unexpected segment source: %s", s.Source())
		}
		resolved = append(resolved, s.WithTarget(tr))
	}

	injected, err := ext.Inject("complex.json", []byte(rawJSON), resolved, "pt-br")
	if err != nil {
		t.Fatalf("Inject failed: %v", err)
	}

	injectedStr := string(injected)

	// Verify translations are present
	if !strings.Contains(injectedStr, `"Haroldo"`) {
		t.Errorf("missing Haroldo in:\n%s", injectedStr)
	}
	if !strings.Contains(injectedStr, `"Um guerreiro corajoso"`) {
		t.Errorf("missing Um guerreiro corajoso in:\n%s", injectedStr)
	}
	if !strings.Contains(injectedStr, `"Bem-vindo ao reino!"`) {
		t.Errorf("missing Bem-vindo ao reino! in:\n%s", injectedStr)
	}

	// Verify technical values were untouched
	if !strings.Contains(injectedStr, `"img/characters/hero.png"`) {
		t.Errorf("technical image modified")
	}
	if !strings.Contains(injectedStr, `"$gameVariables.value(1)"`) {
		t.Errorf("technical flag modified")
	}
	if !strings.Contains(injectedStr, `"id": 101`) {
		t.Errorf("technical id modified")
	}
}

func TestJSONExtractor_CustomKeysAndWildcard(t *testing.T) {
	rawJSON := `{
  "custom_tag": "Special Event",
  "text": "Ignored text",
  "title": "King of Kings"
}`

	ext := NewJSONExtractor()
	ext.SetKeys([]string{"custom_tag", "title"})

	segments, err := ext.Extract("tags.json", []byte(rawJSON), "en")
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	if len(segments) != 2 {
		t.Fatalf("expected 2 segments for custom keys, got %d", len(segments))
	}

	// Wildcard '*' should extract all non-technical keys
	ext.SetKeys([]string{"*"})
	segmentsAll, err := ext.Extract("tags.json", []byte(rawJSON), "en")
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}
	if len(segmentsAll) != 3 {
		t.Fatalf("expected 3 segments for wildcard, got %d", len(segmentsAll))
	}
}

func TestJSONExtractor_NoChangesReturnsOriginal(t *testing.T) {
	rawJSON := []byte(`{"id": 42, "code": "ABC"}`)
	ext := NewJSONExtractor()
	injected, err := ext.Inject("untouched.json", rawJSON, nil, "pt-br")
	if err != nil {
		t.Fatalf("Inject failed: %v", err)
	}
	if string(injected) != string(rawJSON) {
		t.Fatalf("expected byte-for-byte identical output for untouched json")
	}
}

func translateJSON(t *testing.T, ext *JSONExtractor, raw string, fn func(string) string) (string, []string) {
	t.Helper()
	segs, err := ext.Extract("file.json", []byte(raw), "en")
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	var resolved []*model.Segment
	var srcs []string
	for _, s := range segs {
		srcs = append(srcs, s.Source())
		resolved = append(resolved, s.WithTarget(fn(s.Source())))
	}
	out, err := ext.Inject("file.json", []byte(raw), resolved, "pt")
	if err != nil {
		t.Fatalf("Inject: %v", err)
	}
	return string(out), srcs
}

func prefixPT(s string) string { return "[PT] " + s }

func TestTranslatesAllowedKeysAndKeepsTechnicalFields(t *testing.T) {
	raw := `{"id":101,"face":"Actor1","image":"img/characters/hero.png","audio":"se/attack.ogg","stats":[10,20,30],` +
		`"actors":[{"id":1,"faceName":"Actor1_2","name":"Harold","description":"A brave warrior","sprite":"hero.png"}],` +
		`"dialogue":["Welcome to the kingdom!","Beware of monsters."],"technical_flag":"$gameVariables.value(1)","version":"1.0.0"}`
	out, _ := translateJSON(t, NewJSONExtractor(), raw, prefixPT)

	want := `{"id":101,"face":"Actor1","image":"img/characters/hero.png","audio":"se/attack.ogg","stats":[10,20,30],` +
		`"actors":[{"id":1,"faceName":"Actor1_2","name":"[PT] Harold","description":"[PT] A brave warrior","sprite":"hero.png"}],` +
		`"dialogue":["[PT] Welcome to the kingdom!","[PT] Beware of monsters."],"technical_flag":"$gameVariables.value(1)","version":"1.0.0"}`
	if out != want {
		t.Errorf("obteve\n%s\nesperava\n%s", out, want)
	}
}

// Reescrever via map[string]any reordenava as chaves, indentava arquivos minificados
// e escapava <, > e & como \u003c, \u003e e \u0026.
func TestInjectPreservesFormattingKeyOrderAndBOM(t *testing.T) {
	raw := "\xef\xbb\xbf{\"zeta\":\"<b>Hello</b>\",\n    \"alpha\": 1.50, \"text\": \"Fish & chips\"}"
	out, srcs := translateJSON(t, NewJSONExtractor(), raw, func(s string) string {
		return map[string]string{"Fish & chips": "Peixe & fritas"}[s]
	})
	if want := "\xef\xbb\xbf{\"zeta\":\"<b>Hello</b>\",\n    \"alpha\": 1.50, \"text\": \"Peixe & fritas\"}"; out != want {
		t.Errorf("obteve %q, esperava %q", out, want)
	}
	if len(srcs) != 1 {
		t.Errorf("só a chave text deve ser extraída: %v", srcs)
	}
}

func TestExtractFollowsDocumentOrder(t *testing.T) {
	ext := NewJSONExtractor()
	ext.SetKeys([]string{"*"})
	for range 5 {
		_, srcs := translateJSON(t, ext, `{"z":"Zebra","a":"Apple","m":["Mango","Melon"]}`, prefixPT)
		if strings.Join(srcs, "|") != "Zebra|Apple|Mango|Melon" {
			t.Fatalf("ordem deve seguir o documento, obteve %v", srcs)
		}
	}
}

func TestNoTranslatableTextReturnsOriginalBytes(t *testing.T) {
	raw := `{"id": 999, "texture": "system/window.png", "bgm": "theme.ogg", "code": 404, "flag": true}`
	out, srcs := translateJSON(t, NewJSONExtractor(), raw, prefixPT)
	if len(srcs) != 0 || out != raw {
		t.Errorf("nada deveria ser extraído nem alterado: %v %q", srcs, out)
	}
}

func TestConfigurableAllowlist(t *testing.T) {
	raw := `{"custom_tag":"Special Event","text":"Ignored text","title":"King of Kings","desc":"Ignored desc"}`
	ext := NewJSONExtractor()
	ext.SetKeys([]string{"custom_tag", "title"})
	if _, srcs := translateJSON(t, ext, raw, prefixPT); strings.Join(srcs, "|") != "Special Event|King of Kings" {
		t.Errorf("allowlist explícita: %v", srcs)
	}
	ext.SetKeys([]string{"*"})
	if _, srcs := translateJSON(t, ext, raw, prefixPT); len(srcs) != 4 {
		t.Errorf("'*' deve liberar todas as chaves: %v", srcs)
	}
}

func TestStringFilterSeparatesProseFromPaths(t *testing.T) {
	ext := NewJSONExtractor()
	for _, s := range []string{`\C[2]魔法の剣\C[0]`, `こんにちは\n世界`, "Yes/No", "はい/いいえ", "Press Attack/Defend to choose your action.", "Choose sword and/or shield", "$100 for this sword!"} {
		if !ext.isTranslatable("text", s) {
			t.Errorf("%q deveria ser traduzível", s)
		}
	}
	for _, s := range []string{"img/pictures/hero.png", "audio/bgm/theme", "custom/folder/file", "some_folder/asset", `img\pictures\hero`, `C:\Users\hero`, `data\Map001`, `\C[2]`, "$gameVariables.value(1)", "https://example.com"} {
		if ext.isTranslatable("text", s) {
			t.Errorf("%q não deveria ser traduzível", s)
		}
	}
}
