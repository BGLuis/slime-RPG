package json_test

import (
	json_std "encoding/json"
	"slices"
	"strings"
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

func extractSources(t *testing.T, file, raw string) []string {
	t.Helper()
	segs, err := json.NewMVExtractor().Extract(file, []byte(raw))
	if err != nil {
		t.Fatalf("Extract(%s): %v", file, err)
	}
	var out []string
	for _, s := range segs {
		out = append(out, s.Source())
	}
	return out
}

func mapWithCommands(commands string) string {
	return `{"events":[null,{"id":1,"pages":[{"list":[` + commands + `,{"code":0,"parameters":[]}]}]}]}`
}

func TestExtractsShowText(t *testing.T) {
	got := extractSources(t, "Map001.json", mapWithCommands(`{"code":401,"parameters":["Olá Mundo!"]}`))
	if !slices.Equal(got, []string{"Olá Mundo!"}) {
		t.Errorf("obteve %v", got)
	}
}

// Comentários (108/408) e labels (118) são diretivas de plugin e alvos de salto:
// traduzi-los quebra mecânicas do jogo.
func TestCommentsAndLabelsAreNotExtracted(t *testing.T) {
	got := extractSources(t, "Map001.json", mapWithCommands(
		`{"code":108,"parameters":["tool_duration : 15"]},{"code":408,"parameters":["tool_position : user"]},`+
			`{"code":118,"parameters":["loop_start"]},{"code":401,"parameters":["Texto de diálogo real"]}`))
	if !slices.Equal(got, []string{"Texto de diálogo real"}) {
		t.Errorf("só o 401 deveria ser extraído, obteve %v", got)
	}
}

func TestMapEventsShapes(t *testing.T) {
	cases := map[string]struct {
		raw  string
		want []string
	}{
		"events false": {`{"displayName":"","events":false,"width":17}`, nil},
		"events null":  {`{"displayName":"","events":null}`, nil},
		"events objeto": {
			`{"events":{"1":{"id":1,"pages":[{"list":[{"code":401,"parameters":["Texto em dict"]}]}]}}}`,
			[]string{"Texto em dict"},
		},
		"pages e list malformados": {
			`{"events":[null,{"id":1,"pages":false},{"id":2,"pages":[{"list":null}]},` +
				`{"id":3,"pages":[{"list":[false,{"code":401,"parameters":["Ok"]}]}]}]}`,
			[]string{"Ok"},
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			if got := extractSources(t, "Map040.json", c.raw); !slices.Equal(got, c.want) {
				t.Errorf("obteve %v, esperava %v", got, c.want)
			}
		})
	}
}

func TestEventsAsObjectRoundTrip(t *testing.T) {
	raw := `{"events":{"1":{"id":1,"pages":[{"list":[{"code":401,"parameters":["Hi"]}]}]}}}`
	ext := json.NewMVExtractor()
	segs, _ := ext.Extract("Map042.json", []byte(raw))
	out, err := ext.Inject("Map042.json", []byte(raw), []*model.Segment{segs[0].WithTarget("Oi")})
	if err != nil {
		t.Fatal(err)
	}
	if want := strings.Replace(raw, `"Hi"`, `"Oi"`, 1); string(out) != want {
		t.Errorf("obteve %s", out)
	}
}

func TestPictureOnlyChoicesAreSkippedAndMixedChoicesKeepTags(t *testing.T) {
	if got := extractSources(t, "Map001.json", mapWithCommands(`{"code":102,"parameters":[["\\picture[yes]","\\picture[no]"],-1,0,2,0]}`)); len(got) != 0 {
		t.Errorf("escolhas só com imagem não têm texto, obteve %v", got)
	}

	raw := mapWithCommands(`{"code":102,"parameters":[["\\picture[yes]","Option Two"],-1,0,2,0]}`)
	ext := json.NewMVExtractor()
	segs, _ := ext.Extract("Map001.json", []byte(raw))
	if len(segs) != 1 || segs[0].Source() != "Option Two" {
		t.Fatalf("só a opção com texto deveria ser extraída: %v", segs)
	}
	out, _ := ext.Inject("Map001.json", []byte(raw), []*model.Segment{segs[0].WithTarget("Opção Dois")})
	if !strings.Contains(string(out), `["\\picture[yes]","Opção Dois"]`) {
		t.Errorf("tag de imagem precisa ser preservada: %s", out)
	}
}

func TestSystemExtractsPlayerFacingFieldsOnly(t *testing.T) {
	raw := `{"gameTitle":"Meu Jogo","currencyUnit":"G","switches":["","Debug Switch","Intro Completed"],` +
		`"variables":["","Gold Count"],"armorTypes":["","Escudo"],"elements":["","Fogo"],` +
		`"terms":{"commands":["Fight","Escape"],"basic":["Level"],"messages":{"victory":"%1 won!"}}}`
	got := extractSources(t, "System.json", raw)
	for _, want := range []string{"Meu Jogo", "G", "Escudo", "Fogo", "Fight", "Escape", "Level", "%1 won!"} {
		if !slices.Contains(got, want) {
			t.Errorf("%q deveria ser extraído; obteve %v", want, got)
		}
	}
	for _, internal := range []string{"Debug Switch", "Intro Completed", "Gold Count"} {
		if slices.Contains(got, internal) {
			t.Errorf("%q é nome interno do editor e não pode ser traduzido", internal)
		}
	}
}

func TestTroopsBattleEventsAreExtracted(t *testing.T) {
	raw := `[null,{"id":1,"name":"Slime*2","pages":[{"list":[{"code":401,"parameters":["The slimes attack!"]}]}]}]`
	if got := extractSources(t, "Troops.json", raw); !slices.Equal(got, []string{"The slimes attack!"}) {
		t.Errorf("obteve %v", got)
	}
}

// Reescrever via MarshalIndent transformava um Map minificado de 132 bytes em 360,
// reordenava chaves e escapava <, > e & como \u003c, \u003e e \u0026.
func TestInjectChangesOnlyTranslatedStrings(t *testing.T) {
	raw := "\xef\xbb\xbf" + `{"events":[null,{"id":1,"pages":[{"list":[{"code":401,"indent":0,"parameters":["<b>Hi</b> & bye"]}]}]}],"displayName":"","width":17}`
	ext := json.NewMVExtractor()
	segs, err := ext.Extract("Map001.json", []byte(raw))
	if err != nil {
		t.Fatalf("arquivo com BOM deve ser aceito: %v", err)
	}
	out, err := ext.Inject("Map001.json", []byte(raw), []*model.Segment{segs[0].WithTarget("<b>Oi</b> & tchau")})
	if err != nil {
		t.Fatal(err)
	}
	if want := strings.Replace(raw, "<b>Hi</b> & bye", "<b>Oi</b> & tchau", 1); string(out) != want {
		t.Errorf("obteve\n%s\nesperava\n%s", out, want)
	}
}

func TestInjectIgnoresSegmentsFromOtherFiles(t *testing.T) {
	raw := mapWithCommands(`{"code":401,"parameters":["Hello"]}`)
	ext := json.NewMVExtractor()
	segs, _ := ext.Extract("Map001.json", []byte(raw))
	out, _ := ext.Inject("Map002.json", []byte(raw), []*model.Segment{segs[0].WithTarget("Olá")})
	if string(out) != raw {
		t.Errorf("segmento de outro arquivo não pode ser aplicado: %s", out)
	}
}
