package jsonedit_test

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/BGLuis/slime-rpg/pkg/extractor/jsonedit"
)

func TestStringsInDocumentOrderWithPointersAndInheritedKeys(t *testing.T) {
	doc := []byte(`{"zeta":"Z","a/b":{"x~y":"Q"},"dialogue":["one",2,{"text":"two"}],"n":null,"t":true}`)
	got, err := jsonedit.Strings(doc)
	if err != nil {
		t.Fatal(err)
	}
	want := []struct{ ptr, key, val string }{
		{"/zeta", "zeta", "Z"},
		{"/a~1b/x~0y", "x~y", "Q"},
		{"/dialogue/0", "dialogue", "one"},
		{"/dialogue/2/text", "text", "two"},
	}
	if len(got) != len(want) {
		t.Fatalf("obteve %d strings: %+v", len(got), got)
	}
	for i, w := range want {
		if got[i].Pointer != w.ptr || got[i].Key != w.key || got[i].Value != w.val {
			t.Errorf("[%d] = (%q, %q, %q), esperava (%q, %q, %q)", i, got[i].Pointer, got[i].Key, got[i].Value, w.ptr, w.key, w.val)
		}
	}
}

func TestReplaceStringsPreservesEveryOtherByte(t *testing.T) {
	doc := []byte("\xef\xbb\xbf{\"z\":1.50,\"b\":\"Hi \\\"there\\\"\" ,\n  \"a\":[ \"keep\", \"x\" ],\"big\":12345678901234567890}")
	out, err := jsonedit.ReplaceStrings(doc, map[string]string{"/b": "<b>Olá</b> & tchau", "/a/1": "y"})
	if err != nil {
		t.Fatal(err)
	}
	want := "\xef\xbb\xbf{\"z\":1.50,\"b\":\"<b>Olá</b> & tchau\" ,\n  \"a\":[ \"keep\", \"y\" ],\"big\":12345678901234567890}"
	if string(out) != want {
		t.Errorf("obteve\n%s\nesperava\n%s", out, want)
	}
	var check map[string]any
	if err := json.Unmarshal(jsonedit.StripBOM(out), &check); err != nil {
		t.Errorf("saída precisa continuar JSON válido: %v", err)
	}
}

func TestReplaceStringsEscapesSpecialCharacters(t *testing.T) {
	out, err := jsonedit.ReplaceStrings([]byte(`["a"]`), map[string]string{"/0": "line\n\"q\" \\ end"})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	if err := json.Unmarshal(out, &got); err != nil || got[0] != "line\n\"q\" \\ end" {
		t.Errorf("valor não sobreviveu ao round-trip: %s (%v)", out, err)
	}
}

func TestReplaceStringsIgnoresKeysAndUnknownPointers(t *testing.T) {
	doc := []byte(`{"text":"text"}`)
	out, err := jsonedit.ReplaceStrings(doc, map[string]string{"/missing": "x", "/text/0": "y"})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out, doc) {
		t.Errorf("documento não deveria mudar: %s", out)
	}
	out, _ = jsonedit.ReplaceStrings(doc, map[string]string{"/text": "texto"})
	if string(out) != `{"text":"texto"}` {
		t.Errorf("só o valor deve mudar, nunca a chave: %s", out)
	}
}

func TestStringsRejectsInvalidJSON(t *testing.T) {
	if _, err := jsonedit.Strings([]byte(`{"a":`)); err == nil {
		t.Error("JSON truncado deve falhar")
	}
}
