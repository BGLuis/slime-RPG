package dxa

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// SLIME_WOLF_ARCHIVE aponta para um .wolf real e SLIME_WOLF_EXPECTED para a pasta
// extraída por uma implementação de referência; sem elas o teste é ignorado.
func TestRealArchiveMatchesReference(t *testing.T) {
	path, ref := os.Getenv("SLIME_WOLF_ARCHIVE"), os.Getenv("SLIME_WOLF_EXPECTED")
	if path == "" || ref == "" {
		t.Skip("defina SLIME_WOLF_ARCHIVE e SLIME_WOLF_EXPECTED")
	}
	a, err := Open(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if len(a.Entries) == 0 {
		t.Fatal("nenhuma entrada")
	}
	t.Logf("%d entradas", len(a.Entries))
	for _, e := range a.Entries {
		got, err := a.Read(e)
		if err != nil {
			t.Fatalf("%s: %v", e.Path, err)
		}
		want, err := os.ReadFile(filepath.Join(ref, filepath.FromSlash(e.Path)))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s diverge da referência (%d vs %d bytes)", e.Path, len(got), len(want))
		}
	}
}
