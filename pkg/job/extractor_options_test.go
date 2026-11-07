package job

import (
	"github.com/BGLuis/slime-rpg/pkg/formats"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Com colunas explícitas só elas são traduzidas, mesmo havendo uma coluna
// "text" que a detecção automática escolheria.
func TestCSVColumnsOptionRestrictsTranslation(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sheet")
	writeFile(t, filepath.Join(dir, "lines.csv"), "id,text,note\n1,こんにちは,メモ\n")

	prov := &fakeProvider{}
	cfg := baseConfig(dir)
	cfg.ExtractorOptions = map[string]string{"csv.columns": "note"}
	if _, err := runJob(t, cfg, newMemory(t), prov); err != nil {
		t.Fatal(err)
	}
	if got := prov.received(); !slices.Equal(got, []string{"メモ"}) {
		t.Errorf("provedor recebeu %v, esperava só a coluna note", got)
	}
	out := read(t, filepath.Join(dir, "lines.csv"))
	if !strings.Contains(out, "PT[メモ]") || !strings.Contains(out, "こんにちは") {
		t.Errorf("saída inesperada: %q", out)
	}
}

// O delimitador forçado vale na leitura e na escrita; um ';' dentro de campo
// com vírgulas só é lido certo se o delimitador for respeitado.
func TestCSVDelimiterOptionIsHonoured(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sheet")
	writeFile(t, filepath.Join(dir, "lines.csv"), "id;text\n1;こんにちは, みなさん, 元気\n")

	prov := &fakeProvider{}
	cfg := baseConfig(dir)
	cfg.ExtractorOptions = map[string]string{"csv.delimiter": ";"}
	if _, err := runJob(t, cfg, newMemory(t), prov); err != nil {
		t.Fatal(err)
	}
	if got := prov.received(); !slices.Equal(got, []string{"こんにちは, みなさん, 元気"}) {
		t.Errorf("provedor recebeu %v", got)
	}
	if out := read(t, filepath.Join(dir, "lines.csv")); !strings.HasPrefix(out, "id;text\n1;PT[") {
		t.Errorf("a saída perdeu o delimitador ';': %q", out)
	}
}

// json.keys substitui a safelist padrão: só as chaves listadas são extraídas.
func TestJSONKeysOptionRestrictsExtraction(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "texts")
	writeFile(t, filepath.Join(dir, "a.json"), `{"name":"勇者","description":"説明文","title":"題名"}`)

	prov := &fakeProvider{}
	cfg := baseConfig(dir)
	cfg.ExtractorOptions = map[string]string{"json.keys": "title"}
	if _, err := runJob(t, cfg, newMemory(t), prov); err != nil {
		t.Fatal(err)
	}
	if got := prov.received(); !slices.Equal(got, []string{"題名"}) {
		t.Errorf("provedor recebeu %v, esperava só title", got)
	}
}

// -e json numa pasta de jogo RPG Maker trata os arquivos como JSON genérico
// (safelist de chaves, aqui json.keys), não com o extrator de comandos de evento.
func TestForcedExtractorOverridesDetection(t *testing.T) {
	game := writeGame(t)
	auto := &fakeProvider{}
	if _, err := runJob(t, baseConfig(game), newMemory(t), auto); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(auto.received(), "こんにちは") {
		t.Fatalf("pré-condição: o RPG Maker devia extrair o diálogo, recebeu %v", auto.received())
	}

	forcedGame := writeGame(t)
	forced := &fakeProvider{}
	cfg := baseConfig(forcedGame)
	cfg.Extractor = "json"
	cfg.ExtractorOptions = map[string]string{"json.keys": "gameTitle"}
	if _, err := runJob(t, cfg, newMemory(t), forced); err != nil {
		t.Fatal(err)
	}
	if slices.Contains(forced.received(), "こんにちは") {
		t.Errorf("com -e json o diálogo de evento não devia ser extraído: %v", forced.received())
	}
	if !slices.Contains(forced.received(), "勇者の旅") {
		t.Errorf("com -e json e json.keys=gameTitle esperava o título, recebeu %v", forced.received())
	}
}

func TestUnknownExtractorAndOptionAreRejected(t *testing.T) {
	game := writeGame(t)
	cfg := baseConfig(game)
	cfg.Extractor = "nope"
	if _, err := runJob(t, cfg, newMemory(t), &fakeProvider{}); err == nil {
		t.Error("extrator desconhecido devia falhar")
	}
	cfg = baseConfig(game)
	cfg.ExtractorOptions = map[string]string{"csv.delimiter": "ab"}
	if _, err := runJob(t, cfg, newMemory(t), &fakeProvider{}); err == nil {
		t.Error("delimitador de dois caracteres devia falhar")
	}
}

func TestExtractorSchemaKeysAreAccepted(t *testing.T) {
	for _, name := range []string{"CSV", "json", "csv"} {
		for _, f := range ExtractorSchema(name).Fields {
			if err := validateKnown(f.Key); err != nil {
				t.Errorf("%s: %v", name, err)
			}
		}
		if len(ExtractorSchema(name).Fields) == 0 {
			t.Errorf("%s sem campos no schema", name)
		}
	}
}

func validateKnown(key string) error { return formats.ValidateOptions(map[string]string{key: ""}) }
