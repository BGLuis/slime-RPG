package rvdata

import (
	"bytes"
	"slices"
	"testing"

	"github.com/BGLuis/slime-rpg/pkg/model"
)

func TestMarshal_PrimitivesAndLinks(t *testing.T) {
	w := NewWriter()

	sharedCmd := &RubyObject{
		ClassName: "RPG::MoveCommand",
		Attributes: map[string]any{
			"@code":       int64(44),
			"@parameters": []any{},
		},
	}

	data := []any{
		"primeira_string",
		"segunda_string",
		sharedCmd,
		sharedCmd, // Gera TYPE_LINK (@)
	}

	if err := w.Write(data); err != nil {
		t.Fatalf("failed to write marshal: %v", err)
	}

	marshaledBytes := w.Bytes()

	r, err := NewReader(marshaledBytes)
	if err != nil {
		t.Fatalf("failed to create reader: %v", err)
	}

	unmarshaled, err := r.Read()
	if err != nil {
		t.Fatalf("failed to read unmarshaled data: %v", err)
	}

	arr, ok := unmarshaled.([]any)
	if !ok || len(arr) != 4 {
		t.Fatalf("expected array of 4 elements, got %T: %v", unmarshaled, unmarshaled)
	}

	if arr[0] != "primeira_string" || arr[1] != "segunda_string" {
		t.Errorf("strings did not match: %v, %v", arr[0], arr[1])
	}

	obj1, ok1 := arr[2].(*RubyObject)
	obj2, ok2 := arr[3].(*RubyObject)
	if !ok1 || !ok2 {
		t.Fatalf("expected RubyObject instances in slots 2 and 3")
	}

	// Verifica se a referência apontou exatamente para a mesma instância (TYPE_LINK resolvido)
	if obj1 != obj2 {
		t.Errorf("expected shared object reference via TYPE_LINK, got different instances")
	}
}

func TestMarshal_UserDefTable(t *testing.T) {
	tableBinary := []byte{0x02, 0x00, 0x00, 0x00, 0x02, 0x00, 0x00, 0x00, 0xFF, 0xFE}
	ud := &RubyUserDef{
		ClassName: "Table",
		Data:      tableBinary,
	}

	w := NewWriter()
	if err := w.Write(ud); err != nil {
		t.Fatalf("write userdef failed: %v", err)
	}

	r, err := NewReader(w.Bytes())
	if err != nil {
		t.Fatalf("new reader failed: %v", err)
	}

	res, err := r.Read()
	if err != nil {
		t.Fatalf("read userdef failed: %v", err)
	}

	udRes, ok := res.(*RubyUserDef)
	if !ok || udRes.ClassName != "Table" || !bytes.Equal(udRes.Data, tableBinary) {
		t.Fatalf("UserDef data mismatch after marshal roundtrip: %v", udRes)
	}
}

func TestRVDataExtractor_ExtractAndInject(t *testing.T) {
	cmd1 := &RubyObject{
		ClassName: "RPG::EventCommand",
		Attributes: map[string]any{
			"@code":       int64(401),
			"@indent":     int64(0),
			"@parameters": []any{"Olá aventureiro do RPG Maker!"},
		},
	}
	cmd2 := &RubyObject{
		ClassName: "RPG::EventCommand",
		Attributes: map[string]any{
			"@code":       int64(102),
			"@indent":     int64(0),
			"@parameters": []any{[]any{"Lutar", "Fugir"}, int64(1)},
		},
	}
	cmdEnd := &RubyObject{
		ClassName: "RPG::EventCommand",
		Attributes: map[string]any{
			"@code":       int64(0),
			"@indent":     int64(0),
			"@parameters": []any{},
		},
	}

	page := &RubyObject{
		ClassName: "RPG::Event::Page",
		Attributes: map[string]any{
			"@list": []any{cmd1, cmd2, cmdEnd},
		},
	}

	event := &RubyObject{
		ClassName: "RPG::Event",
		Attributes: map[string]any{
			"@id":    int64(1),
			"@name":  "EV001",
			"@pages": []any{page},
		},
	}

	rvMap := &RubyObject{
		ClassName: "RPG::Map",
		Attributes: map[string]any{
			"@display_name": "Vila Inicial",
			"@events":       map[any]any{int64(1): event},
		},
	}

	w := NewWriter()
	if err := w.Write(rvMap); err != nil {
		t.Fatalf("failed to write synthetic map: %v", err)
	}
	rawBytes := w.Bytes()

	ext := NewExtractor()
	if !ext.CanHandle("Map001.rvdata2") {
		t.Fatalf("expected to handle .rvdata2")
	}

	segments, err := ext.Extract("Map001.rvdata2", rawBytes, "pt")
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	// O nome do evento ("EV001") é interno do editor e não é extraído.
	var got []string
	for _, s := range segments {
		got = append(got, s.Source())
	}
	if want := []string{"Vila Inicial", "Olá aventureiro do RPG Maker!", "Lutar", "Fugir"}; !slices.Equal(got, want) {
		t.Fatalf("segmentos = %q, esperava %q", got, want)
	}

	var resolved []*model.Segment
	for _, s := range segments {
		var tr string
		switch s.Source() {
		case "Vila Inicial":
			tr = "Starting Village"
		case "Olá aventureiro do RPG Maker!":
			tr = "Hello RPG Maker adventurer!"
		case "Lutar":
			tr = "Fight"
		case "Fugir":
			tr = "Flee"
		}
		resolved = append(resolved, s.WithTarget(tr))
	}

	injectedBytes, err := ext.Inject("Map001.rvdata2", rawBytes, resolved, "en")
	if err != nil {
		t.Fatalf("Inject failed: %v", err)
	}

	// Reload injected map and assert translated values
	r, err := NewReader(injectedBytes)
	if err != nil {
		t.Fatalf("NewReader on injected bytes failed: %v", err)
	}

	reloaded, err := r.Read()
	if err != nil {
		t.Fatalf("read injected failed: %v", err)
	}

	reloadedMap, ok := reloaded.(*RubyObject)
	if !ok || reloadedMap.ClassName != "RPG::Map" {
		t.Fatalf("unexpected reloaded map: %T", reloaded)
	}

	if reloadedMap.Attributes["@display_name"] != "Starting Village" {
		t.Errorf("display_name not translated: %v", reloadedMap.Attributes["@display_name"])
	}

	eventsMap := reloadedMap.Attributes["@events"].(map[any]any)
	reloadedEv := eventsMap[int64(1)].(*RubyObject)
	pages := reloadedEv.Attributes["@pages"].([]any)
	cmds := pages[0].(*RubyObject).Attributes["@list"].([]any)

	textCmd := cmds[0].(*RubyObject)
	if textCmd.Attributes["@parameters"].([]any)[0] != "Hello RPG Maker adventurer!" {
		t.Errorf("text command not translated: %v", textCmd.Attributes["@parameters"])
	}

	choicesCmd := cmds[1].(*RubyObject)
	choicesList := choicesCmd.Attributes["@parameters"].([]any)[0].([]any)
	if choicesList[0] != "Fight" || choicesList[1] != "Flee" {
		t.Errorf("choices not translated: %v", choicesList)
	}
}

func marshal(t *testing.T, v any) []byte {
	t.Helper()
	w := NewWriter()
	if err := w.Write(v); err != nil {
		t.Fatalf("Write: %v", err)
	}
	return w.Bytes()
}

func unmarshal(t *testing.T, data []byte) any {
	t.Helper()
	r, err := NewReader(data)
	if err != nil {
		t.Fatal(err)
	}
	v, err := r.Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	return v
}

func command(code int64, params ...any) *RubyObject {
	return &RubyObject{ClassName: "RPG::EventCommand", Attributes: map[string]any{"@code": code, "@indent": int64(0), "@parameters": params}}
}

// translate extrai, aplica fn e devolve o arquivo regravado e as origens extraídas.
func translate(t *testing.T, file string, data []byte, fn func(string) string) ([]byte, []string) {
	t.Helper()
	ext := NewExtractor()
	segs, err := ext.Extract(file, data, "en")
	if err != nil {
		t.Fatal(err)
	}
	var srcs []string
	var resolved []*model.Segment
	for _, s := range segs {
		srcs = append(srcs, s.Source())
		resolved = append(resolved, s.WithTarget(fn(s.Source())))
	}
	out, err := ext.Inject(file, data, resolved, "pt")
	if err != nil {
		t.Fatal(err)
	}
	return out, srcs
}

func upper(s string) string { return "TR:" + s }

// Ruby só cria TYPE_LINK para o mesmo objeto. Ligar strings iguais ou arrays vazios
// faria objetos independentes do jogo passarem a ser um só após a tradução.
func TestWriterKeepsEqualValuesAsDistinctObjects(t *testing.T) {
	cases := map[string]struct {
		value any
		ruby  []byte // Marshal.dump do Ruby para o mesmo valor
	}{
		"arrays vazios":  {[]any{[]any{}, []any{}}, []byte{0x04, 0x08, '[', 0x07, '[', 0x00, '[', 0x00}},
		"strings iguais": {[]any{"a", "a"}, []byte("\x04\x08[\x07I\"\x06a\x06:\x06ETI\"\x06a\x06;\x00T")},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			if got := marshal(t, c.value); !bytes.Equal(got, c.ruby) {
				t.Errorf("obteve % x, esperava % x", got, c.ruby)
			}
		})
	}
}

func TestSharedMoveCommandsSurviveTranslation(t *testing.T) {
	move1 := &RubyObject{ClassName: "RPG::MoveCommand", Attributes: map[string]any{"@code": int64(37), "@parameters": []any{}}}
	move2 := &RubyObject{ClassName: "RPG::MoveCommand", Attributes: map[string]any{"@code": int64(39), "@parameters": []any{}}}
	route := &RubyObject{ClassName: "RPG::MoveRoute", Attributes: map[string]any{"@list": []any{move1, move2}, "@repeat": false}}
	list := []any{
		command(401, "Hello"),
		command(205, int64(-1), route),
		command(505, move1),
		command(505, move2),
		command(401, "World"),
		command(0),
	}
	out, _ := translate(t, "CommonEvents.rvdata2", marshal(t, []any{nil, &RubyObject{ClassName: "RPG::CommonEvent", Attributes: map[string]any{"@list": list}}}), upper)

	reloaded := unmarshal(t, out).([]any)[1].(*RubyObject).Attributes["@list"].([]any)
	gotRoute := reloaded[1].(*RubyObject).Attributes["@parameters"].([]any)[1].(*RubyObject).Attributes["@list"].([]any)
	if reloaded[2].(*RubyObject).Attributes["@parameters"].([]any)[0] != gotRoute[0] ||
		reloaded[3].(*RubyObject).Attributes["@parameters"].([]any)[0] != gotRoute[1] {
		t.Error("comandos 505 devem continuar apontando para os mesmos RPG::MoveCommand da rota")
	}
	if reloaded[0].(*RubyObject).Attributes["@parameters"].([]any)[0] != "TR:Hello" {
		t.Errorf("texto não traduzido: %v", reloaded[0].(*RubyObject).Attributes["@parameters"])
	}
}

func TestBinaryStringsRoundTripByteExact(t *testing.T) {
	payload := []byte("compressed_code_\x00\xff\\uZZ_invalid_escape_binary_data")
	data := marshal(t, []any{int64(1), "Script1", &RubyString{RawBytes: payload, Value: string(payload)}})
	got := unmarshal(t, marshal(t, unmarshal(t, data))).([]any)[2].(*RubyString)
	if !bytes.Equal(got.RawBytes, payload) {
		t.Errorf("bytes binários alterados: %q", got.RawBytes)
	}
}

func TestTranslatedStringKeepsOriginalEncodingIvars(t *testing.T) {
	xpString := &RubyString{Value: "Potion", RawBytes: []byte("Potion")}
	item := &RubyObject{ClassName: "RPG::Item", Attributes: map[string]any{"@name": xpString}}
	out, _ := translate(t, "Items.rxdata", marshal(t, []any{nil, item}), upper)

	got := unmarshal(t, out).([]any)[1].(*RubyObject).Attributes["@name"]
	rs, ok := got.(*RubyString)
	if !ok || rs.Value != "TR:Potion" || len(rs.Attributes) != 0 {
		t.Errorf("string do XP (sem :E) deve continuar sem ivar de encoding: %#v", got)
	}
}

func TestDatabaseFilesExtractPlayerFacingFields(t *testing.T) {
	actor := &RubyObject{ClassName: "RPG::Actor", Attributes: map[string]any{
		"@name": "Hero", "@nickname": "The Brave", "@description": "A young warrior.", "@character_name": "Actor1",
	}}
	out, srcs := translate(t, "Actors.rvdata2", marshal(t, []any{nil, actor}), upper)
	if want := []string{"Hero", "The Brave", "A young warrior."}; !slices.Equal(srcs, want) {
		t.Errorf("Actors: extraiu %q, esperava %q", srcs, want)
	}
	saved := unmarshal(t, out).([]any)[1].(*RubyObject).Attributes
	if saved["@nickname"] != "TR:The Brave" || saved["@character_name"] != "Actor1" {
		t.Errorf("Actors após injeção: %v", saved)
	}

	mapInfo := &RubyObject{ClassName: "RPG::MapInfo", Attributes: map[string]any{"@name": "Castle Map", "@parent_id": int64(0)}}
	if _, srcs := translate(t, "MapInfos.rvdata2", marshal(t, map[any]any{int64(1): mapInfo}), upper); !slices.Equal(srcs, []string{"Castle Map"}) {
		t.Errorf("MapInfos: extraiu %q", srcs)
	}
}

func TestSystemExtractsTitleCurrencyListsAndTerms(t *testing.T) {
	terms := &RubyObject{ClassName: "RPG::System::Terms", Attributes: map[string]any{
		"@basic": []any{"Level", "Lv"}, "@commands": []any{"Fight", "Escape"},
	}}
	sys := &RubyObject{ClassName: "RPG::System", Attributes: map[string]any{
		"@game_title": "My Adventure", "@currency_unit": "Gold", "@elements": []any{"", "Fire"},
		"@switches": []any{"", "Intro Done"}, "@variables": []any{"", "Gold Count"}, "@terms": terms,
	}}
	out, srcs := translate(t, "System.rvdata2", marshal(t, sys), upper)
	for _, want := range []string{"My Adventure", "Gold", "Fire", "Level", "Lv", "Fight", "Escape"} {
		if !slices.Contains(srcs, want) {
			t.Errorf("%q deveria ser extraído; obteve %q", want, srcs)
		}
	}
	for _, internal := range []string{"Intro Done", "Gold Count"} {
		if slices.Contains(srcs, internal) {
			t.Errorf("%q é nome interno de switch/variável", internal)
		}
	}
	reloaded := unmarshal(t, out).(*RubyObject)
	if reloaded.Attributes["@terms"].(*RubyObject).Attributes["@basic"].([]any)[0] != "TR:Level" {
		t.Errorf("termo não injetado: %v", reloaded.Attributes["@terms"])
	}
}

func TestEventTextsFromCommonEventsAndTroops(t *testing.T) {
	ce := &RubyObject{ClassName: "RPG::CommonEvent", Attributes: map[string]any{
		"@name": "INTERNAL_SETUP", "@list": []any{command(401, "Common event text"), command(405, "Scrolling line"), command(0)},
	}}
	if _, srcs := translate(t, "CommonEvents.rvdata2", marshal(t, []any{nil, ce}), upper); !slices.Equal(srcs, []string{"Common event text", "Scrolling line"}) {
		t.Errorf("CommonEvents: extraiu %q (nome interno não deve ser extraído)", srcs)
	}

	page := &RubyObject{ClassName: "RPG::Troop::Page", Attributes: map[string]any{"@list": []any{command(401, "Battle text"), command(0)}}}
	troop := &RubyObject{ClassName: "RPG::Troop", Attributes: map[string]any{"@name": "Slime*2", "@pages": []any{page}}}
	if _, srcs := translate(t, "Troops.rvdata2", marshal(t, []any{nil, troop}), upper); !slices.Equal(srcs, []string{"Battle text"}) {
		t.Errorf("Troops: extraiu %q", srcs)
	}
}

func TestMapTablePreservedAndExtractionOrderIsStable(t *testing.T) {
	table := &RubyUserDef{ClassName: "Table", Data: []byte{0x03, 0x00, 0x00, 0x00, 0x14, 0x00}}
	build := func() []byte {
		ev := &RubyObject{ClassName: "RPG::Event", Attributes: map[string]any{"@name": "EV001", "@pages": []any{
			&RubyObject{ClassName: "RPG::Event::Page", Attributes: map[string]any{"@list": []any{command(401, "A"), command(401, "B"), command(102, []any{"C", "D"}, int64(1))}}},
		}}}
		return marshal(t, &RubyObject{ClassName: "RPG::Map", Attributes: map[string]any{"@display_name": "Town", "@data": table, "@events": map[any]any{int64(2): ev, int64(1): ev}}})
	}

	out, first := translate(t, "Map001.rvdata2", build(), upper)
	for range 5 {
		if _, again := translate(t, "Map001.rvdata2", build(), upper); !slices.Equal(again, first) {
			t.Fatalf("ordem de extração mudou entre execuções: %q vs %q", first, again)
		}
	}
	got := unmarshal(t, out).(*RubyObject).Attributes["@data"].(*RubyUserDef)
	if got.ClassName != "Table" || !bytes.Equal(got.Data, table.Data) {
		t.Errorf("Table do mapa deve ser preservada: %#v", got)
	}
}

func TestLinksAfterFloatPointToTheRightObject(t *testing.T) {
	shared := []any{"x"}
	// Marshal.dump([1.5, s, s]) com s = ["x"]: o link referencia o índice 2 (o float é o 1).
	ruby := []byte("\x04\x08[\x08f\x081.5[\x06I\"\x06x\x06:\x06ET@\x07")
	got := marshal(t, []any{1.5, shared, shared})
	if !bytes.Equal(got, ruby) {
		t.Fatalf("obteve % x, esperava % x", got, ruby)
	}
	back := unmarshal(t, got).([]any)
	if _, isArray := back[2].([]any); !isArray {
		t.Errorf("o link deveria resolver para o array compartilhado, obteve %T", back[2])
	}
}
