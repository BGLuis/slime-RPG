package dxa

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
)

// Os valores esperados vêm da implementação Python (WolfArchive.py/huffman.py):
// se alguém alterar um passo da cifra ou da árvore, arquivos reais deixam de abrir.
func TestHuffmanEncodeMatchesPython(t *testing.T) {
	src := []byte("hello hello hello wolf rpg \x00\x01\x01\x01 translate translate")
	enc := huffmanEncode(src)
	sum := sha256.Sum256(enc)
	if got := hex.EncodeToString(sum[:]); got != "9197eb1a926b49a9ec0c5fb2b413fc90626e6bf5131269f55de709cacf6c82de" {
		t.Errorf("sha256 do fluxo = %s", got)
	}
	dec, err := huffmanDecode(enc)
	if err != nil || !bytes.Equal(dec, src) {
		t.Errorf("round trip falhou: %v", err)
	}
}

func TestCryptPrimitivesMatchPython(t *testing.T) {
	pwd := make([]byte, 15)
	for i := range pwd {
		pwd[i] = byte(3 + i)
	}
	sum := func(k *posKey) string { s := sha256.Sum256(k[:]); return hex.EncodeToString(s[:]) }
	if got := sum(newPosKey(pwd, []byte("abcdefgh"), true)); got != "46173aadcc24176b54c0624c004de06b75792b788c0a66c58fea62d8b89f5d01" {
		t.Errorf("posKey (variante com chave) = %s", got)
	}
	if got := sum(newPosKey(pwd, []byte("abcdefgh"), false)); got != "f5ee1a34cb6164eca1d7bd661719278d049064df4454a26afa98f037241079ce" {
		t.Errorf("posKey (variante simples) = %s", got)
	}

	buf := make([]byte, 40)
	newAESCTR(pwd).xor(buf)
	if got := hex.EncodeToString(buf); got != "a88e7c9089b76419298d8ad69004d73ec6f01c466db9a8af934d9ebc072cf0ff51a6dfabec1cc6a7" {
		t.Errorf("fluxo AES = %s", got)
	}

	head := make([]byte, 64)
	for i := range head {
		head[i] = byte(i)
	}
	scrambleHeader(head, pwd)
	if got := hex.EncodeToString(head[:32]); got != "0001020304050607143e920c0d3b140d8c39c877ff4d3f2000116d253a000d2a" {
		t.Errorf("cabeçalho = %s", got)
	}

	if k := makeKey(defaultKey); hex.EncodeToString(k[:]) != "fcd96eb023b3d9" {
		t.Errorf("chave padrão = %x", k)
	}
}

func TestLZRoundTripAndEscapes(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	cases := map[string][]byte{
		"repetitivo": bytes.Repeat([]byte("abcabcabd"), 5000),
		"todos os bytes": func() []byte {
			b := make([]byte, 4096)
			for i := range b {
				b[i] = byte(i)
			}
			return b
		}(),
		"ruído": func() []byte { b := make([]byte, 20000); rng.Read(b); return b }(),
		"curto": []byte("abc"),
	}
	for name, src := range cases {
		got, err := lzDecode(lzEncode(src))
		if err != nil || !bytes.Equal(got, src) {
			t.Errorf("%s: round trip falhou (%v)", name, err)
		}
	}
}

func TestLZDecodeRejectsTruncatedStream(t *testing.T) {
	enc := lzEncode(bytes.Repeat([]byte("xyz1"), 100))
	if _, err := lzDecode(enc[:len(enc)-3]); err == nil {
		t.Error("fluxo truncado deveria falhar")
	}
}

func writeArchive(t *testing.T, files map[string][]byte) string {
	t.Helper()
	var list []File
	for p, d := range files {
		list = append(list, File{Path: p, Read: func() ([]byte, error) { return d, nil }})
	}
	path := filepath.Join(t.TempDir(), "x.wolf")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := Write(f, list); err != nil {
		t.Fatal(err)
	}
	f.Close()
	return path
}

func TestWriteThenOpenRoundTrip(t *testing.T) {
	big := make([]byte, 100_000)
	rand.New(rand.NewSource(2)).Read(big[:1000])
	files := map[string][]byte{
		"CommonEvent.dat":        bytes.Repeat([]byte("evento "), 3000),
		"MapData/Map001.mps":     big,
		"MapData/Sub/deep.txt":   []byte("日本語のテキスト"),
		"MapData/empty.bin":      {},
		"BasicData/DataBase.dat": []byte("xy"),
	}
	path := writeArchive(t, files)
	a, err := Open(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if len(a.Entries) != len(files) {
		t.Fatalf("entradas = %d, esperava %d", len(a.Entries), len(files))
	}
	for _, e := range a.Entries {
		want, ok := files[e.Path]
		if !ok {
			t.Fatalf("entrada inesperada %q", e.Path)
		}
		got, err := a.Read(e)
		if err != nil || !bytes.Equal(got, want) {
			t.Errorf("%s: conteúdo diverge (%v)", e.Path, err)
		}
	}
	if fi, _ := os.Stat(path); fi.Size() >= 100_000+int64(len(files["CommonEvent.dat"])) {
		t.Error("o arquivo gravado deveria estar comprimido")
	}
}

func TestOpenRejectsNonArchiveAndUnsafeNames(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.wolf")
	if err := os.WriteFile(p, bytes.Repeat([]byte{1}, 200), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(p, nil); err == nil {
		t.Error("arquivo qualquer não pode abrir como DXA")
	}

	path := writeArchive(t, map[string][]byte{"aaaa.txt": []byte("conteudo conteudo conteudo")})
	data, _ := os.ReadFile(path)
	// o nome fica no fim da tabela comprimida; basta validar que name() recusa "..".
	a, err := Open(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	_ = data
	a.nameTable = append([]byte{0, 0, 2, 0}, []byte("..\x00")...)
	if _, err := a.name(0); err == nil {
		t.Error("nome '..' deveria ser recusado")
	}
}
