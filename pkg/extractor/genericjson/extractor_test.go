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
