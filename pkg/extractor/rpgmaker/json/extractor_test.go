package json_test

import (
	json_std "encoding/json"
	"testing"

	"github.com/BGLuis/slime-rpg/pkg/extractor/rpgmaker/json"
	"github.com/BGLuis/slime-rpg/pkg/model"
)

func TestMVExtractor_CanHandle(t *testing.T) {
	ext := json.NewMVExtractor()

	valid := []string{"Map001.json", "Map999.json", "CommonEvents.json", "Actors.json", "Items.json", "Skills.json", "System.json"}
	for _, f := range valid {
		if !ext.CanHandle(f) {
			t.Errorf("deveria reconhecer %q", f)
		}
	}

	invalid := []string{"image.png", "script.js", "readme.txt", "Map_invalid.json"}
	for _, f := range invalid {
		if ext.CanHandle(f) {
			t.Errorf("não deveria reconhecer %q", f)
		}
	}
}

func TestMVExtractor_MapRoundtrip(t *testing.T) {
	ext := json.NewMVExtractor()

	mapContent := `{
  "displayName": "Initial Town",
  "events": [
    null,
    {
      "id": 1,
      "name": "Villager",
      "pages": [
        {
          "list": [
            {
              "code": 101,
              "parameters": ["Actor1", 0, 0, 2, "Elder"]
            },
            {
              "code": 401,
              "parameters": ["Welcome, hero!"]
            },
            {
              "code": 102,
              "parameters": [["Yes", "No"], 1, 0, 2, 0]
            },
            {
              "code": 0,
              "parameters": []
            }
          ]
        }
      ]
    }
  ]
}`

	segments, err := ext.Extract("Map001.json", []byte(mapContent))
	if err != nil {
		t.Fatalf("falha ao extrair mapa: %v", err)
	}

	// 1 displayName + 1 speaker (Elder) + 1 dialogue (Welcome) + 2 choices (Yes, No) = 5
	if len(segments) != 5 {
		t.Fatalf("esperava 5 segmentos, obteve %d", len(segments))
	}

	// Traduz os segmentos
	var translatedSegs []*model_test_alias
	for _, s := range segments {
		switch s.Source() {
		case "Initial Town":
			segments = replaceTarget(segments, s.ID(), "Vila Inicial")
		case "Elder":
			segments = replaceTarget(segments, s.ID(), "Ancião")
		case "Welcome, hero!":
			segments = replaceTarget(segments, s.ID(), "Bem-vindo, herói!")
		case "Yes":
			segments = replaceTarget(segments, s.ID(), "Sim")
		case "No":
			segments = replaceTarget(segments, s.ID(), "Não")
		}
	}
	_ = translatedSegs

	injectedBytes, err := ext.Inject("Map001.json", []byte(mapContent), segments)
	if err != nil {
		t.Fatalf("falha ao injetar mapa: %v", err)
	}

	var parsed map[string]any
	if err := json_std.Unmarshal(injectedBytes, &parsed); err != nil {
		t.Fatalf("JSON gerado inválido: %v", err)
	}

	if parsed["displayName"] != "Vila Inicial" {
		t.Errorf("displayName incorreto: %v", parsed["displayName"])
	}

	// Verifica se o JSON injetado pode ser re-extraído e traz as strings traduzidas
	reExtracted, err := ext.Extract("Map001.json", injectedBytes)
	if err != nil {
		t.Fatalf("falha ao re-extrair mapa: %v", err)
	}

	foundHero := false
	for _, s := range reExtracted {
		if s.Source() == "Bem-vindo, herói!" {
			foundHero = true
		}
	}
	if !foundHero {
		t.Errorf("não encontrou 'Bem-vindo, herói!' na re-extração do mapa injetado")
	}
}

func TestMVExtractor_ItemsRoundtrip(t *testing.T) {
	ext := json.NewMVExtractor()

	itemsContent := `[
  null,
  {
    "id": 1,
    "name": "Potion",
    "description": "Restores 500 HP.",
    "message1": "drinks a potion!"
  }
]`

	segments, err := ext.Extract("Items.json", []byte(itemsContent))
	if err != nil {
		t.Fatalf("falha ao extrair itens: %v", err)
	}

	// name, description, message1 = 3
	if len(segments) != 3 {
		t.Fatalf("esperava 3 segmentos, obteve %d", len(segments))
	}

	for _, s := range segments {
		switch s.Source() {
		case "Potion":
			segments = replaceTarget(segments, s.ID(), "Poção")
		case "Restores 500 HP.":
			segments = replaceTarget(segments, s.ID(), "Restaura 500 HP.")
		case "drinks a potion!":
			segments = replaceTarget(segments, s.ID(), "toma uma poção!")
		}
	}

	injectedBytes, err := ext.Inject("Items.json", []byte(itemsContent), segments)
	if err != nil {
		t.Fatalf("falha ao injetar itens: %v", err)
	}

	reExtracted, err := ext.Extract("Items.json", injectedBytes)
	if err != nil {
		t.Fatalf("falha ao re-extrair itens: %v", err)
	}

	foundPotion := false
	for _, s := range reExtracted {
		if s.Source() == "Poção" {
			foundPotion = true
		}
	}
	if !foundPotion {
		t.Errorf("esperava encontrar 'Poção' re-extraída")
	}
}

type model_test_alias = model.Segment

func replaceTarget(segs []*model.Segment, id, target string) []*model.Segment {
	for i, s := range segs {
		if s.ID() == id {
			segs[i] = s.WithTarget(target)
		}
	}
	return segs
}
