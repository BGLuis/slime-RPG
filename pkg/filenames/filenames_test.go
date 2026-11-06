package filenames

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

type dictTranslator map[string]string

func (d dictTranslator) TranslateBatch(_ context.Context, texts []string, _, _ string) ([]string, error) {
	out := make([]string, len(texts))
	for i, t := range texts {
		if v, ok := d[t]; ok {
			out[i] = v
		} else {
			out[i] = t
		}
	}
	return out, nil
}

func write(t *testing.T, path, content string) {
	t.Helper()
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return string(b)
}

func TestSanitize(t *testing.T) {
	cases := [][3]string{
		{"simple", ".txt", "simple.txt"},
		{"A/B", ".txt", "A_B.txt"},
		{`A\B:C*D?E<F>G|H`, ".dat", "A_B_C_D_E_F_G_H.dat"},
		{"   ...   ", ".json", "unnamed.json"},
		{"CON", ".txt", "_CON.txt"},
		{"con", ".txt", "_con.txt"},
		{"NUL", "", "_NUL"},
		{"COM1.tar", ".gz", "_COM1.tar.gz"},
	}
	for _, c := range cases {
		if got := Sanitize(c[0], c[1]); got != c[2] {
			t.Errorf("Sanitize(%q, %q) = %q, want %q", c[0], c[1], got, c[2])
		}
	}
}

func TestCollisionsGetDistinctNames(t *testing.T) {
	in, out := t.TempDir(), filepath.Join(t.TempDir(), "out")
	write(t, filepath.Join(in, "first.txt"), "Content of first file")
	write(t, filepath.Join(in, "second.txt"), "Content of second file")

	res, err := Process(context.Background(), dictTranslator{"first": "item", "second": "item"}, in, out, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if res.TotalFiles != 2 || res.TranslatedCount != 2 || res.CollisionsResolved != 1 {
		t.Errorf("result = %+v", res)
	}
	if read(t, filepath.Join(out, "item.txt")) != "Content of first file" || read(t, filepath.Join(out, "item_1.txt")) != "Content of second file" {
		t.Error("conteúdos trocados ou perdidos na colisão")
	}
}

// Um "/" traduzido não pode criar subpastas nem escapar da saída.
func TestSeparatorsAndReservedNamesAreSanitized(t *testing.T) {
	in, out := t.TempDir(), filepath.Join(t.TempDir(), "out")
	write(t, filepath.Join(in, "slash.txt"), "x")
	write(t, filepath.Join(in, "con_test.txt"), "x")
	write(t, filepath.Join(in, "colon_test.txt"), "x")
	write(t, filepath.Join(in, "escape.txt"), "x")

	_, err := Process(context.Background(), dictTranslator{"slash": "A/B", "con_test": "CON", "colon_test": "foo:bar", "escape": "../../fora"}, in, out, Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"A_B.txt", "_CON.txt", "foo_bar.txt", "_.._fora.txt"} {
		if _, err := os.Stat(filepath.Join(out, name)); err != nil {
			t.Errorf("esperava %s: %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(out, "A")); err == nil {
		t.Error("subpasta A não deveria existir")
	}
}

func TestKeepOriginalAndSubfolders(t *testing.T) {
	in, out := t.TempDir(), filepath.Join(t.TempDir(), "out")
	write(t, filepath.Join(in, "sub", "orig.txt"), "Original content")

	res, err := Process(context.Background(), dictTranslator{"orig": "traduzido"}, in, out, Options{KeepOriginal: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.TranslatedCount != 1 || res.CopiedOriginals != 1 {
		t.Errorf("result = %+v", res)
	}
	for _, name := range []string{"traduzido.txt", "orig.txt"} {
		if read(t, filepath.Join(out, "sub", name)) != "Original content" {
			t.Errorf("%s ausente na subpasta", name)
		}
	}
}

func TestMissingAndEmptyInput(t *testing.T) {
	if _, err := Process(context.Background(), dictTranslator{}, filepath.Join(t.TempDir(), "nope"), t.TempDir(), Options{}); err == nil {
		t.Error("pasta inexistente deveria falhar")
	}
	res, err := Process(context.Background(), dictTranslator{}, t.TempDir(), t.TempDir(), Options{})
	if err != nil || res.TotalFiles != 0 {
		t.Errorf("pasta vazia: %+v %v", res, err)
	}
}
