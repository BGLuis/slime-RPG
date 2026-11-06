package wolfrpg

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/BGLuis/slime-rpg/pkg/model"
)

// testdata/parity foi gerado com as classes da versão Python (WolfBinaryAdapter):
// cada arquivo vem com os textos que o Python extrai (.sources.json) e o binário
// que ele grava ao traduzir cada segmento como "PT<origem>" (.translated).
func TestParityWithPythonWolf(t *testing.T) {
	dir := filepath.Join("testdata", "parity")
	pythonOnly := map[string][]string{
		// No comando 300 o Python extrai o nome do evento chamado e o descarta ao inserir.
		"CommonEvent.dat": {"共通イベント名"},
	}

	for _, name := range []string{"CommonEvent.dat", "DataBase.dat", "Game.dat"} {
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(dir, name))
			if err != nil {
				t.Fatal(err)
			}
			ext := NewExtractor()
			ext.Dir = dir

			doc, err := ext.open(name, data)
			if err != nil {
				t.Fatal(err)
			}
			if got := doc.save(); !bytes.Equal(got, data) {
				t.Fatalf("ler e gravar sem traduzir deveria reproduzir o arquivo (%d bytes vs %d)", len(got), len(data))
			}

			segs, err := ext.Extract(name, data, "ja")
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			var resolved []*model.Segment
			for _, s := range segs {
				got = append(got, s.Source())
				resolved = append(resolved, s.WithTarget("PT<"+s.Source()+">"))
			}
			var want []string
			raw, _ := os.ReadFile(filepath.Join(dir, name+".sources.json"))
			if err := json.Unmarshal(raw, &want); err != nil {
				t.Fatal(err)
			}
			want = slices.DeleteFunc(want, func(s string) bool { return slices.Contains(pythonOnly[name], s) })
			slices.Sort(got)
			if !slices.Equal(got, want) {
				t.Errorf("textos extraídos divergem do Python\n got: %q\nwant: %q", got, want)
			}

			out, err := ext.Inject(name, data, resolved, "pt")
			if err != nil {
				t.Fatal(err)
			}
			expected, _ := os.ReadFile(filepath.Join(dir, name+".translated"))
			if !bytes.Equal(out, expected) {
				t.Errorf("binário traduzido diverge do Python (%d bytes vs %d)", len(out), len(expected))
			}
		})
	}
}

// O DataBase.dat traz nomes de dado usados como chave pelo comando 250
// ("衣装"/"髪1"): esse nome não pode ser traduzido, os demais sim.
func TestDatabaseKeepsNamesUsedAsLookupKeys(t *testing.T) {
	dir := filepath.Join("testdata", "parity")
	data, _ := os.ReadFile(filepath.Join(dir, "DataBase.dat"))
	ext := NewExtractor()
	ext.Dir = dir
	segs, err := ext.Extract("DataBase.dat", data, "ja")
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range segs {
		if s.Source() == "髪1" {
			t.Error("nome de dado consultado por nome não pode ser traduzido")
		}
	}
	found := false
	for _, s := range segs {
		found = found || s.Source() == "薬草"
	}
	if !found {
		t.Error("nomes de dado de tipos não consultados por nome devem ser traduzidos")
	}
}

// Jogos Shift-JIS usam fontes sem acentos: a tradução precisa chegar em ASCII, e
// não como bytes UTF-8 crus (mojibake).
func TestShiftJISTranslationsLoseAccentsNotBytes(t *testing.T) {
	data, _ := os.ReadFile(filepath.Join("testdata", "parity", "CommonEvent.dat"))
	ext := NewExtractor()
	segs, _ := ext.Extract("CommonEvent.dat", data, "ja")
	var resolved []*model.Segment
	for _, s := range segs {
		resolved = append(resolved, s.WithTarget("Ação “rápida”… ok"))
	}
	out, err := ext.Inject("CommonEvent.dat", data, resolved, "pt")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(out, []byte(`Acao "rapida"... ok`)) {
		t.Error("acentos deveriam virar ASCII")
	}
	if bytes.Contains(out, []byte("ção")) {
		t.Error("UTF-8 cru gravado num arquivo Shift-JIS")
	}
	back, err := LoadWolfCommonEvents(out)
	if err != nil {
		t.Fatalf("arquivo inválido após a injeção: %v", err)
	}
	if got := back.Events[0].Commands[0].StringArgs[0]; got != `Acao "rapida"... ok` {
		t.Errorf("releitura = %q", got)
	}
}

func TestEvtextKeepsIndentationTerminatorAndLabelColon(t *testing.T) {
	src := encodeShiftJIS("/コメント\r\n  こんにちは\r\nアリス：\r\n\r\n")
	ext := NewExtractor()
	segs, err := ext.Extract("Evtext/scene.txt", src, "ja")
	if err != nil || len(segs) != 2 {
		t.Fatalf("esperava 2 linhas traduzíveis: %v %v", segs, err)
	}
	tr := map[string]string{"こんにちは": "Olá\nmundo", "アリス：": "Alice:"}
	var resolved []*model.Segment
	for _, s := range segs {
		resolved = append(resolved, s.WithTarget(tr[s.Source()]))
	}
	out, err := ext.Inject("Evtext/scene.txt", src, resolved, "pt")
	if err != nil {
		t.Fatal(err)
	}
	want := encodeShiftJIS("/コメント\r\n  Ola mundo\r\nAlice：\r\n\r\n")
	if !bytes.Equal(out, want) {
		t.Errorf("got %q\nwant %q", out, want)
	}
}
