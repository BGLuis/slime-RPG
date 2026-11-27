package validator

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BGLuis/slime-rpg/pkg/model"
)

func TestValidateJSAndCheckTranslated(t *testing.T) {
	valid := []string{
		`null; // comentário`,
		`a.id - b.id; // ordenação`,
		`(() => { const itemName = ES.v(19); return "『" + itemName + "』"; })();`,
		`width * 0.8`,
		`Graphics.width + 100`,
	}
	for _, code := range valid {
		if err := ValidateJS(code); err != nil {
			t.Errorf("ValidateJS(%q) falhou inesperadamente: %v", code, err)
		}
		if !IsLikelyJS(code) {
			t.Errorf("IsLikelyJS(%q) deveria retornar true", code)
		}
	}

	if IsLikelyJS("Olá, mundo!") {
		t.Error("frase normal não deveria ser detectada como JS")
	}

	cases := []struct {
		name string
		orig string
		bad  string
	}{
		{"null traduzido", "null; // なし", "nulo; // なし"},
		{"return traduzido", "(() => { return 'ok'; })()", "(() => { retornar 'ok'; })()"},
		{"decimal com vírgula", "width * 0.8", "width * 0,8"},
		{"Graphics traduzido", "Graphics.width + 100", "Largura gráfica + 100"},
		{"this traduzido", "[this._actor]; // actor", "[este._ator]; // actor"},
	}
	for _, tc := range cases {
		if msg := CheckTranslatedJS(tc.orig, tc.bad); msg == "" {
			t.Errorf("%s: esperava detectar quebra entre %q e %q", tc.name, tc.orig, tc.bad)
		}
	}
}

func TestAssetIndexAndCaseInsensitive(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "img/system/Window.png_"), "x")
	mustWrite(t, filepath.Join(dir, "img/parallaxes/blackback.png_"), "x")
	mustWrite(t, filepath.Join(dir, "audio/bgm/Town.ogg_"), "x")

	idx := NewAssetIndex(dir)
	for _, okRef := range []string{"Window", "window", "img/system/Window.png", "Town", "blackBack"} {
		if !idx.Exists(okRef) {
			t.Errorf("AssetIndex.Exists(%q) deveria ser true", okRef)
		}
	}
	for _, badRef := range []string{"Janela", "Cidade", "Inexistente"} {
		if idx.Exists(badRef) {
			t.Errorf("AssetIndex.Exists(%q) deveria ser false", badRef)
		}
	}

	if p, ok := resolveCaseInsensitive(dir, "img/parallaxes/blackBack.png_"); !ok || !strings.HasSuffix(p, "blackback.png_") {
		t.Errorf("resolveCaseInsensitive falhou: got (%q, %v)", p, ok)
	}
}

func TestValidateStatic_CatchesBrokenGame(t *testing.T) {
	origDir := t.TempDir()
	gameDir := t.TempDir()

	setupMiniMZ := func(root, skin, listScript, mapScript, dialogue string) {
		mustWrite(t, filepath.Join(root, "index.html"), "<html></html>")
		mustWrite(t, filepath.Join(root, "img/system/Window.png_"), "png")
		mustWrite(t, filepath.Join(root, "audio/bgm/Town.ogg_"), "ogg")
		mustWrite(t, filepath.Join(root, "data/System.json"), `{"gameTitle":"Test"}`)
		mustWrite(t, filepath.Join(root, "data/MapInfos.json"), `[null,{"id":1,"name":"Map1"}]`)
		mustWrite(t, filepath.Join(root, "data/Map001.json"), `{
			"displayName":"Inicio",
			"events":[null,{"id":1,"pages":[{"list":[
				{"code":401,"parameters":[`+quoteJSON(dialogue)+`]},
				{"code":355,"parameters":[`+quoteJSON(mapScript)+`]}
			]}]}]
		}`)
		mustWrite(t, filepath.Join(root, "js/plugins.js"), `var $plugins = [
			{"name":"UskMessageSceneEx","status":true,"parameters":{
				"list":"[\"{\\\"skin\\\":\\\"`+skin+`\\\",\\\"label\\\":\\\"Mensagem\\\"}\"]"
			}},
			{"name":"SceneCustomMenu","status":true,"parameters":{
				"ListScript":`+quoteJSON(listScript)+`,
				"baseBgm":`+quoteJSON(condStr(skin == "Window", "Town", "Cidade"))+`
			}}
		];`)
	}

	setupMiniMZ(origDir, "Window", "null; // なし", "this.moveTowardPlayer();", `Olá \V[1]!`)
	setupMiniMZ(gameDir, "Janela", "nulo; // なし", "este.moveTowardPlayer();", "Olá!")

	rep, err := ValidateStatic(context.Background(), gameDir, origDir)
	if err != nil {
		t.Fatalf("ValidateStatic falhou: %v", err)
	}
	if !rep.HasErrors() {
		t.Fatalf("esperava erros críticos, obteve issues=%v", rep.Issues)
	}

	cats := map[Category]int{}
	for _, it := range rep.Issues {
		cats[it.Category]++
	}
	if cats[CategoryAsset] < 2 {
		t.Errorf("esperava pelo menos 2 erros de asset (Window->Janela e Town->Cidade), issues=%+v", rep.Issues)
	}
	if cats[CategoryScript] < 2 {
		t.Errorf("esperava pelo menos 2 erros de script (plugins.js e Map001.json), issues=%+v", rep.Issues)
	}
	if cats[CategoryControlCode] < 1 {
		t.Errorf("esperava aviso de código de controle \\V[1] perdido, issues=%+v", rep.Issues)
	}
}

func TestGuardSegments_RevertsUnsafeTranslations(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "img/system/Window.png_"), "png")
	assets := NewAssetIndex(dir)

	sAsset, _ := model.NewSegment("plugins.js#p1", "Window", model.KindMenu, map[string]any{"param": "list/0/skin"}, nil)
	sAsset = sAsset.WithTarget("Janela")

	sScript, _ := model.NewSegment("plugins.js#p2", "null; // comment", model.KindMenu, map[string]any{"param": "ListScript"}, nil)
	sScript = sScript.WithTarget("nulo; // comment")

	sSafe, _ := model.NewSegment("plugins.js#p3", "New Game", model.KindMenu, map[string]any{"param": "Title"}, nil)
	sSafe = sSafe.WithTarget("Novo Jogo")

	guarded, warnings := GuardSegments(dir, "js/plugins.js", []*model.Segment{sAsset, sScript, sSafe}, assets)
	if len(warnings) != 2 {
		t.Fatalf("esperava 2 avisos de proteção, obteve %d: %v", len(warnings), warnings)
	}
	if guarded[0].Target() != "Window" {
		t.Errorf("asset deveria ter sido revertido para Window, got %q", guarded[0].Target())
	}
	if guarded[1].Target() != "null; // comment" {
		t.Errorf("script deveria ter sido revertido para original, got %q", guarded[1].Target())
	}
	if guarded[2].Target() != "Novo Jogo" {
		t.Errorf("texto normal não deveria ser revertido, got %q", guarded[2].Target())
	}
}

func TestHeal_FixesBrokenCSVQuotesAndPluginsSelectively(t *testing.T) {
	origDir := t.TempDir()
	gameDir := t.TempDir()

	writeGame := func(root, csvBody, skin, title string) {
		mustWrite(t, filepath.Join(root, "index.html"), "<html></html>")
		mustWrite(t, filepath.Join(root, "img/system/Window.png_"), "png")
		mustWrite(t, filepath.Join(root, "data/System.json"), `{"gameTitle":"Test"}`)
		mustWrite(t, filepath.Join(root, "data/unique/eventsOpening.csv"), csvBody)
		mustWrite(t, filepath.Join(root, "js/plugins.js"), `var $plugins = [
			{"name":"UskMessageSceneEx","status":true,"parameters":{
				"skin":`+quoteJSON(skin)+`,
				"title":`+quoteJSON(title)+`
			}}
		];`)
	}

	origCSV := "op01,\"\n:voice[mira/1]\nこんにちは\n\"\n\n\n"
	brokenCSV := "op01,\"\n:voice[mira/1]\nEla disse \"\"Olá\"\" hoje\n\"\nop02,\"\n:voice[mira/2]\nTudo bem\n\"\n"
	origCSV2 := origCSV + "op02,\"\n:voice[mira/2]\n大丈夫\n\"\n\n\n"

	writeGame(origDir, origCSV2, "Window", "Original Title")
	writeGame(gameDir, brokenCSV, "Janela", "Título Traduzido")

	res, err := Heal(context.Background(), gameDir, Options{
		OriginalDir: origDir,
		RunRuntime:  false,
	}, nil, nil)
	if err != nil {
		t.Fatalf("Heal falhou: %v", err)
	}
	if !res.Clean || res.Report.HasErrors() {
		t.Fatalf("esperava jogo limpo após Heal, issues=%+v", res.Report.Issues)
	}
	if len(res.Actions) != 2 {
		t.Fatalf("esperava 2 ações (CSV e plugins.js), obteve %+v", res.Actions)
	}

	fixedCSV, _ := os.ReadFile(filepath.Join(gameDir, "data/unique/eventsOpening.csv"))
	if !strings.Contains(string(fixedCSV), "Ela disse “Olá” hoje") {
		t.Errorf("tradução do CSV deveria ter sido preservada com aspas tipográficas, got:\n%s", fixedCSV)
	}
	fixedPlugins, _ := os.ReadFile(filepath.Join(gameDir, "js/plugins.js"))
	if !strings.Contains(string(fixedPlugins), `"skin":"Window"`) || !strings.Contains(string(fixedPlugins), `"title":"Título Traduzido"`) {
		t.Errorf("plugins.js deveria reverter apenas 'skin' e manter 'title', got:\n%s", fixedPlugins)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func quoteJSON(s string) string {
	b, _ := os.ReadFile("/dev/null")
	_ = b
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`)
	return `"` + r.Replace(s) + `"`
}

func condStr(cond bool, a, b string) string {
	if cond {
		return a
	}
	return b
}
