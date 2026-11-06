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
	segments, err := ext.Extract("dialogue.csv", []byte(rawCSV), "en")
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

	segments, err := ext.Extract("items.csv", []byte(rawCSV), "en")
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
