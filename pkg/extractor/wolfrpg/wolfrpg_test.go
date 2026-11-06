package wolfrpg

import (
	"bytes"
	"strings"
	"testing"

	"github.com/BGLuis/slime-rpg/pkg/model"
)

func TestLZ4_Roundtrip(t *testing.T) {
	original := []byte("The quick brown fox jumps over the lazy dog. Repeat this string to trigger LZ4 matches! The quick brown fox jumps over the lazy dog.")
	compressed := CompressLZ4(original)
	if len(compressed) == 0 {
		t.Fatalf("compression failed, returned 0 bytes")
	}

	decompressed, err := DecompressLZ4(compressed, len(original))
	if err != nil {
		t.Fatalf("decompression failed: %v", err)
	}

	if !bytes.Equal(original, decompressed) {
		t.Fatalf("decompressed data does not match original")
	}
}

func TestMSVCRandom_And_Crypto(t *testing.T) {
	rng := NewMSVCRandom(12345)
	v1 := rng.Rand(0xFFFF)
	v2 := rng.Rand(0xFFFF)
	if v1 == v2 {
		t.Fatalf("random numbers should vary")
	}

	payload := []byte("secret wolf data block")
	seeds := []uint32{12345, 67890}
	intervals := []int{1, 2}

	encrypted := DecryptDatV1(payload, seeds, intervals)
	decrypted := DecryptDatV1(encrypted, seeds, intervals) // XOR symmetric roundtrip

	if !bytes.Equal(payload, decrypted) {
		t.Fatalf("crypto roundtrip failed")
	}
}

func TestWolfTextFilters(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"Hello, heroic adventurer!", true},
		{"\\s[1]Wait for me!", true},
		{"BGM/battle_01.ogg", false},
		{"system/window.png", false},
		{"<<GET_MAPEVENT_ID_FROM_NAME>>", false},
		{"se_attack_01", false},
		{"12345", false},
		{"true", false},
		{"■", false},
		{"\\c[2]", false}, // Apenas código de controle
	}

	for _, tt := range tests {
		got := IsTranslatable(tt.input)
		if got != tt.expected {
			t.Errorf("IsTranslatable(%q) = %v, want %v", tt.input, got, tt.expected)
		}
	}
}

func TestWolfMap_ExtractAndInject(t *testing.T) {
	m := &WolfMap{
		EncodingType: 85, // UTF-8
		IsUTF8:       true,
		Attributes:   0,
		Version:      1,
		Width:        10,
		Height:       10,
		NoTiles:      true,
		Events: []*WolfMapEvent{
			{
				ID:   1,
				Name: "Guide NPC",
				X:    5,
				Y:    5,
				Pages: []*WolfMapPage{
					{
						ID: 0,
						Commands: []*WolfCommand{
							{
								CID:        CodeShowMessage,
								Args:       []uint32{0},
								StringArgs: []string{"Welcome to the town of Slimes!"},
							},
							{
								CID:        CodeShowChoices,
								Args:       []uint32{0},
								StringArgs: []string{"Stay a while", "Leave immediately"},
							},
						},
					},
				},
			},
		},
	}

	rawMap, err := m.Save()
	if err != nil {
		t.Fatalf("failed to save wolf map: %v", err)
	}

	ext := NewExtractor()
	if !ext.CanHandle("Map001.mps") {
		t.Fatalf("expected to handle Map001.mps")
	}

	segments, err := ext.Extract("Map001.mps", rawMap, "en")
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	if len(segments) != 3 {
		t.Fatalf("expected 3 segments (1 message, 2 choices), got %d", len(segments))
	}

	// Resolve translations
	var resolved []*model.Segment
	for _, s := range segments {
		var tr string
		switch s.Source() {
		case "Welcome to the town of Slimes!":
			tr = "Bem-vindo à cidade dos Slimes!"
		case "Stay a while":
			tr = "Ficar um pouco"
		case "Leave immediately":
			tr = "Sair imediatamente"
		}
		resolved = append(resolved, s.WithTarget(tr))
	}

	injectedBytes, err := ext.Inject("Map001.mps", rawMap, resolved, "pt-br")
	if err != nil {
		t.Fatalf("Inject failed: %v", err)
	}

	// Load injected map back
	reloadedMap, err := LoadWolfMap(injectedBytes)
	if err != nil {
		t.Fatalf("failed to load injected map: %v", err)
	}

	cmds := reloadedMap.Events[0].Pages[0].Commands
	if cmds[0].StringArgs[0] != "Bem-vindo à cidade dos Slimes!" {
		t.Errorf("dialogue translation not applied: got %q", cmds[0].StringArgs[0])
	}
	if cmds[1].StringArgs[0] != "Ficar um pouco" || cmds[1].StringArgs[1] != "Sair imediatamente" {
		t.Errorf("choice translations not applied: got %v", cmds[1].StringArgs)
	}
}

func TestWolfMap_LZ4Compression(t *testing.T) {
	m := &WolfMap{
		EncodingType: 85,
		IsUTF8:       true,
		Attributes:   0x66, // Aciona compressão LZ4
		Version:      1,
		Width:        5,
		Height:       5,
		NoTiles:      true,
		Events: []*WolfMapEvent{
			{
				ID:   10,
				Name: "Chest",
				Pages: []*WolfMapPage{
					{
						ID: 0,
						Commands: []*WolfCommand{
							{
								CID:        CodeShowMessage,
								StringArgs: []string{"You found a magical potion!"},
							},
						},
					},
				},
			},
		},
	}

	saved, err := m.Save()
	if err != nil {
		t.Fatalf("failed to save compressed map: %v", err)
	}

	loaded, err := LoadWolfMap(saved)
	if err != nil {
		t.Fatalf("failed to load compressed map: %v", err)
	}

	if len(loaded.Events) != 1 || loaded.Events[0].Name != "Chest" {
		t.Fatalf("loaded map event corrupted after LZ4 roundtrip")
	}
	if loaded.Events[0].Pages[0].Commands[0].StringArgs[0] != "You found a magical potion!" {
		t.Fatalf("loaded command text corrupted after LZ4 roundtrip")
	}
}

func TestWolfFileCoder_ShiftJIS(t *testing.T) {
	writer := NewWriterCoder(false) // ShiftJIS mode
	japaneseText := "スライムRPG"   // "Slime RPG"
	writer.WriteString(japaneseText)

	reader := NewReaderCoder(writer.Bytes(), false)
	readText, err := reader.ReadString()
	if err != nil {
		t.Fatalf("failed to read ShiftJIS string: %v", err)
	}

	if !strings.Contains(readText, "スライムRPG") {
		t.Errorf("ShiftJIS decode mismatch: got %q, want %q", readText, japaneseText)
	}
}
