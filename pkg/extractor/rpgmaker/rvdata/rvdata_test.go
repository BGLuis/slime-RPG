package rvdata

import (
	"bytes"
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

	// Segments expected:
	// 1. @display_name: "Vila Inicial"
	// 2. @name: "EV001"
	// 3. Show Text 401: "Olá aventureiro do RPG Maker!"
	// 4. Show Choices 102: "Lutar"
	// 5. Show Choices 102: "Fugir"
	if len(segments) != 5 {
		t.Fatalf("expected 5 segments, got %d", len(segments))
	}

	var resolved []*model.Segment
	for _, s := range segments {
		var tr string
		switch s.Source() {
		case "Vila Inicial":
			tr = "Starting Village"
		case "EV001":
			tr = "EV001"
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
