package metadata

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, base, rel, content string) {
	t.Helper()
	path := filepath.Join(base, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func legacyGame(t *testing.T) (original, translated string) {
	root := t.TempDir()
	original = filepath.Join(root, "Data-en")
	translated = filepath.Join(root, "Data-pt")
	write(t, original, "a.txt", "hello")
	write(t, translated, "a.txt", "ola")
	write(t, original, "b.txt", "same")
	write(t, translated, "b.txt", "same")
	write(t, original, "img.png", "binary")
	write(t, translated, "img.png", "binary")
	return original, translated
}

func adopt(t *testing.T, original, translated string) *Meta {
	t.Helper()
	m := Build(translated, original, BuildOptions{
		Origin: OriginLegacy, Extractor: "Wolf RPG", LangSource: "en", LangTarget: "pt",
		Extensions: []string{"txt"},
	})
	if _, err := Save(translated, m); err != nil {
		t.Fatal(err)
	}
	return m
}

func states(t *testing.T, translated string) map[string]string {
	t.Helper()
	report, err := Verify(translated, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	out := make(map[string]string, len(report))
	for _, r := range report {
		out[r.File] = r.State
	}
	return out
}

func TestAdoptRecordsOnlyTranslatableExtensions(t *testing.T) {
	original, translated := legacyGame(t)
	m := adopt(t, original, translated)

	if len(m.Files) != 2 {
		t.Fatalf("esperava só a.txt e b.txt, obteve %v", m.Files)
	}
	if m.Files["a.txt"].Status != StatusSuccess || m.Files["b.txt"].Status != StatusUnchanged {
		t.Errorf("status = %+v", m.Files)
	}
	if m.Origin != OriginLegacy || *m.OriginalDir != "../Data-en" {
		t.Errorf("origin=%q original_dir=%v", m.Origin, *m.OriginalDir)
	}
}

func TestVerifyWithoutMeta(t *testing.T) {
	if _, err := Verify(t.TempDir(), "", nil); err != ErrNoMeta {
		t.Errorf("esperava ErrNoMeta, obteve %v", err)
	}
}

func TestVerifyDetectsEditGameUpdateAndNewFile(t *testing.T) {
	original, translated := legacyGame(t)
	adopt(t, original, translated)
	if s := states(t, translated); s["a.txt"] != StateIntact || s["b.txt"] != StateUnchanged || len(s) != 2 {
		t.Fatalf("estados iniciais = %v", s)
	}

	write(t, translated, "a.txt", "olá revisado")
	if s := states(t, translated); s["a.txt"] != StateEdited {
		t.Errorf("edição manual: %v", s)
	}

	write(t, original, "b.txt", "game update")
	write(t, original, "c.txt", "new file")
	s := states(t, translated)
	if s["b.txt"] != StateOriginalChanged || s["c.txt"] != StateNew {
		t.Errorf("atualização do jogo: %v", s)
	}
}

func TestOriginalUpdateTakesPrecedenceOverManualEdit(t *testing.T) {
	original, translated := legacyGame(t)
	adopt(t, original, translated)
	write(t, original, "a.txt", "hello v2")
	write(t, translated, "a.txt", "editado")
	if s := states(t, translated); s["a.txt"] != StateOriginalChanged {
		t.Errorf("estado = %v", s)
	}
}

func TestVerifyDetectsRemovedTranslationAndMissingOriginal(t *testing.T) {
	original, translated := legacyGame(t)
	adopt(t, original, translated)
	_ = os.Remove(filepath.Join(translated, "a.txt"))
	_ = os.Remove(filepath.Join(original, "b.txt"))
	s := states(t, translated)
	if s["a.txt"] != StateTranslationMissing || s["b.txt"] != StateOriginalMissing {
		t.Errorf("estados = %v", s)
	}
}

func TestMetaSurvivesMovingTheGame(t *testing.T) {
	original, translated := legacyGame(t)
	adopt(t, original, translated)
	moved := filepath.Join(t.TempDir(), "moved")
	_ = os.MkdirAll(moved, 0o755)
	if err := os.Rename(original, filepath.Join(moved, "Data-en")); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(translated, filepath.Join(moved, "Data-pt")); err != nil {
		t.Fatal(err)
	}
	if s := states(t, filepath.Join(moved, "Data-pt")); s["a.txt"] != StateIntact {
		t.Errorf("estado após mover = %v", s)
	}
}

func TestMetaDirIsNotHashed(t *testing.T) {
	original, translated := legacyGame(t)
	adopt(t, original, translated)
	for rel := range ScanHashes(translated, nil) {
		if strings.HasPrefix(rel, DirName) {
			t.Errorf("%s não deveria entrar no hash", rel)
		}
	}
}

func TestFutureSchemaIsIgnored(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, ".slime/meta.json", `{"schema_version": 999, "files": {}}`)
	if Load(dir) != nil {
		t.Error("metadado de schema futuro deveria ser ignorado")
	}
}

func TestBuildWithoutBackup(t *testing.T) {
	translated := t.TempDir()
	write(t, translated, "Map001.mps", "mapa traduzido")
	write(t, translated, "Game.dat", "titulo traduzido")

	m := Build(translated, "", BuildOptions{Origin: OriginJob, Extensions: []string{"mps", "dat"}})
	if m.OriginalDir != nil || len(m.Files) != 2 || m.Files["Map001.mps"].OriginalSHA256 != nil {
		t.Fatalf("meta = %+v", m)
	}
	if _, err := Save(translated, m); err != nil {
		t.Fatal(err)
	}
	if s := states(t, translated); s["Map001.mps"] != StateIntact || s["Game.dat"] != StateIntact {
		t.Errorf("estados = %v", s)
	}
	write(t, translated, "Map001.mps", "mapa modificado")
	if s := states(t, translated); s["Map001.mps"] != StateEdited {
		t.Errorf("edição sem backup: %v", s)
	}
}

func TestJobStatusOverridesHashComparison(t *testing.T) {
	original, translated := legacyGame(t)
	m := Build(translated, original, BuildOptions{
		Origin: OriginJob, Extensions: []string{"txt"},
		FileStatus: map[string]string{"a.txt": StatusPartial},
	})
	if _, err := Save(translated, m); err != nil {
		t.Fatal(err)
	}
	s := states(t, translated)
	if s["a.txt"] != StatePartial || !NeedsTranslation(s["a.txt"]) {
		t.Errorf("arquivo parcial precisa ser retraduzido no modo incremental: %v", s)
	}
	if NeedsTranslation(s["b.txt"]) {
		t.Error("arquivo inalterado não deve ser retraduzido")
	}
}

// O metadado é compartilhado com a versão Python, que usa as mesmas chaves.
func TestSavedJSONUsesPythonKeys(t *testing.T) {
	original, translated := legacyGame(t)
	adopt(t, original, translated)
	data, err := os.ReadFile(Path(translated))
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{`"schema_version": 1`, `"original_sha256"`, `"translated_sha256"`, `"lang_source": "en"`, `"translator": null`, `"extensions": [`} {
		if !strings.Contains(string(data), key) {
			t.Errorf("meta.json sem %s:\n%s", key, data)
		}
	}
}
