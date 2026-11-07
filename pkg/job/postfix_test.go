package job

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/BGLuis/slime-rpg/pkg/extractor/rpgmaker/rpgtext"
	"github.com/BGLuis/slime-rpg/pkg/pipeline/masking"
)

// damageProvider devolve, para cada texto recebido, o que um tradutor ruim
// devolveria: a função damage recebe o texto já mascarado e simula o estrago.
type damageProvider struct {
	mu     sync.Mutex
	seen   []string
	damage func(string) string
}

func (d *damageProvider) ID() string { return "google" }

func (d *damageProvider) TranslateBatch(_ context.Context, texts []string, _, _ string) ([]string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]string, len(texts))
	for i, t := range texts {
		d.seen = append(d.seen, t)
		out[i] = d.damage(t)
	}
	return out, nil
}

func mapWithCommand(code int, params string) string {
	return fmt.Sprintf(`{"events":[null,{"id":1,"pages":[{"list":[{"code":%d,"indent":0,"parameters":%s},{"code":0,"indent":0,"parameters":[]}]}]}]}`, code, params)
}

// runScenario extrai, traduz com o tradutor danificado e injeta; devolve os
// parâmetros do comando como ficaram no arquivo e os textos enviados ao provedor.
func runScenario(t *testing.T, code int, params string, damage func(string) string) (string, []string) {
	t.Helper()
	game := writeGame(t)
	path := filepath.Join(game, "www", "data", "Map001.json")
	writeFile(t, path, mapWithCommand(code, params))
	prov := &damageProvider{damage: damage}
	if _, err := runJob(t, baseConfig(game), newMemory(t), prov); err != nil {
		t.Fatalf("Run: %v", err)
	}
	var doc struct {
		Events []*struct {
			Pages []struct {
				List []struct {
					Parameters json.RawMessage `json:"parameters"`
				} `json:"list"`
			} `json:"pages"`
		} `json:"events"`
	}
	if err := json.Unmarshal([]byte(read(t, path)), &doc); err != nil {
		t.Fatalf("saída inválida: %v", err)
	}
	return string(doc.Events[1].Pages[0].List[0].Parameters), prov.seen
}

type fixCase struct {
	Name       string  `json:"name"`
	Translated string  `json:"translated"`
	Original   *string `json:"original"`
	Python     string  `json:"python"`
}

type insertCase struct {
	Name       string `json:"name"`
	Kind       string `json:"kind"`
	Source     string `json:"source"`
	Original   string `json:"original"`
	Translated string `json:"translated"`
	Python     string `json:"python"`
}

func loadReference(t *testing.T) (fixes []fixCase, inserts []insertCase) {
	t.Helper()
	raw, err := os.ReadFile("testdata/python_fix_reference.json")
	if err != nil {
		t.Fatal(err)
	}
	var ref struct {
		Fix     []fixCase    `json:"fix"`
		Inserts []insertCase `json:"inserts"`
	}
	if err := json.Unmarshal(raw, &ref); err != nil {
		t.Fatal(err)
	}
	return ref.Fix, ref.Inserts
}

func jsonString(t *testing.T, s string) string {
	t.Helper()
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// fixtureTexts são os textos de System.json/Map001.json de writeGame; o que sobra
// em "seen" foi enviado pelo cenário.
var fixtureTexts = []string{"勇者の旅", "G", "勝利！"}

func sentBy(seen []string) []string {
	var out []string
	for _, s := range seen {
		if !slices.Contains(fixtureTexts, s) {
			out = append(out, s)
		}
	}
	return out
}

func replyMasked(masker *masking.Masker, translated string) func(string) string {
	return func(string) string {
		masked, _ := masker.Mask(translated)
		return masked
	}
}

// Cada caso de python_fix_reference.json traz a saída real do fix_text_translate
// do Python. No Go o texto de código nunca chega ao tradutor, e os códigos de
// formato voltam exatamente como na origem; os casos listados em goDiffers
// divergem de propósito.
func TestPostTranslationParityWithPythonText(t *testing.T) {
	fixes, _ := loadReference(t)
	masker := masking.NewMaskerWith(rpgtext.MaskMatchers()...)

	goDiffers := map[string]string{
		// bug do Python: rebaixa códigos de plugin (\F -> \f)
		"plugin_F":  `["\\F[Alice]Olá"]`,
		"plugin_FH": `["\\FH[x]Olá"]`,
		// o Python só repara a condição se o tradutor a devolver; o Go nunca a envia
		"choice_cond": `[["show_if(v[1] > 0) Sim"],0,0,1,0]`,
	}
	// o tradutor nunca vê "\ C[2]": o código vira placeholder antes
	skip := map[string]bool{"backslash_space": true}

	for _, c := range fixes {
		if c.Original == nil || skip[c.Name] {
			continue
		}
		t.Run(c.Name, func(t *testing.T) {
			code, params, reply := 401, "["+jsonString(t, *c.Original)+"]", replyMasked(masker, c.Translated)
			if strings.HasPrefix(*c.Original, "show_if") || strings.HasPrefix(*c.Original, "if(") {
				code, params = 102, "[["+jsonString(t, *c.Original)+"],0,0,1,0]"
				reply = func(string) string { return "Sim" }
			}
			got, seen := runScenario(t, code, params, reply)
			if len(sentBy(seen)) == 0 {
				if code == 401 && !jsonEqual(got, "["+jsonString(t, *c.Original)+"]") {
					t.Errorf("código não enviado deveria ficar intacto: %s", got)
				}
				return
			}
			want := "[" + jsonString(t, c.Python) + "]"
			if code == 102 {
				want = "[[" + jsonString(t, c.Python) + "],0,0,1,0]"
			}
			if w, ok := goDiffers[c.Name]; ok {
				want = w
			}
			if !jsonEqual(got, want) {
				t.Errorf("Go = %s, esperado %s (Python = %q)", got, want, c.Python)
			}
		})
	}
}

func jsonEqual(a, b string) bool {
	var x, y any
	if json.Unmarshal([]byte(a), &x) != nil || json.Unmarshal([]byte(b), &y) != nil {
		return a == b
	}
	return reflect.DeepEqual(x, y)
}

// Casos de inserção em scripts (355) e controle de variáveis (122), com a saída
// do Python (fix_text_translate seguido de insert).
func TestPostTranslationParityWithPythonInserts(t *testing.T) {
	_, inserts := loadReference(t)
	masker := masking.NewMaskerWith(rpgtext.MaskMatchers()...)

	goDiffers := map[string]string{
		// o Go converte aspas tipográficas em ASCII em todo segmento de script
		"script_smart_prose":   `$gameMessage.add("\"Espada\" é boa");`,
		"script_apostrophe_sq": `$gameMessage.add('It\'s a sword');`,
		"cv_bare":              `Espada 'do' Herói`,
		// o Python não conserta \\c minúsculo atrás de barra; o Go restaura a origem
		"script_codes_lower": `$gameMessage.add("\\C[2]Espada\\C[0]");`,
	}

	for _, c := range inserts {
		t.Run(c.Name, func(t *testing.T) {
			code, params := 355, "["+jsonString(t, c.Source)+"]"
			if c.Kind == "122" {
				code, params = 122, "[1,1,0,4,"+jsonString(t, c.Source)+"]"
			}
			got, _ := runScenario(t, code, params, replyMasked(masker, c.Translated))
			var gotParams []any
			if err := json.Unmarshal([]byte(got), &gotParams); err != nil {
				t.Fatal(err)
			}
			want := c.Python
			if w, ok := goDiffers[c.Name]; ok {
				want = w
			}
			if gotParams[len(gotParams)-1] != want {
				t.Errorf("Go = %q, esperado %q (Python = %q)", gotParams[len(gotParams)-1], want, c.Python)
			}
		})
	}
}

// Dano em placeholders: o tradutor muda caixa/espaços (o Go reconhece) ou perde
// um placeholder (o Go devolve a origem em vez de gravar um código quebrado).
func TestMangledPlaceholdersInDialogue(t *testing.T) {
	const src = `\C[2]勇者\C[0]は\V[1]G持っている`
	params := "[" + jsonString(t, src) + "]"
	cases := map[string]struct{ reply, want string }{
		"caixa baixa":           {"__xtok_0__Herói__xtok_1__ tem __xtok_2__ moedas", `\C[2]Herói\C[0] tem \V[1] moedas`},
		"espaços":               {"__ XTOK_0 __Herói__ XTOK_1 __ tem __ XTOK_2 __ moedas", `\C[2]Herói\C[0] tem \V[1] moedas`},
		"placeholder perdido":   {"__XTOK_0__Herói tem __XTOK_2__ moedas", src},
		"placeholder duplicado": {"__XTOK_0__Herói__XTOK_0__ __XTOK_1__ __XTOK_2__", src},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			got, _ := runScenario(t, 401, params, func(string) string { return c.reply })
			if !jsonEqual(got, "["+jsonString(t, c.want)+"]") {
				t.Errorf("got %s, esperado %q", got, c.want)
			}
		})
	}
}

// Parâmetros de plugin MZ (357) e plugin MV (356) com códigos de formato.
func TestPluginCommandsKeepFormatCodes(t *testing.T) {
	masker := masking.NewMaskerWith(rpgtext.MaskMatchers()...)
	t.Run("357 text", func(t *testing.T) {
		src := `\C[2]剣\C[0]を拾った`
		got, _ := runScenario(t, 357, `["Plug","cmd","c",{"text":`+jsonString(t, src)+`}]`, replyMasked(masker, `\c[2]Pegou a espada\c[0]`))
		want := `["Plug","cmd","c",{"text":"\\C[2]Pegou a espada\\C[0]"}]`
		if !jsonEqual(got, want) {
			t.Errorf("got %s, esperado %s", got, want)
		}
	})
	t.Run("356 D_TEXT", func(t *testing.T) {
		got, _ := runScenario(t, 356, `["D_TEXT \\C[2]剣 20"]`, replyMasked(masker, `\c[2]Espada 20`))
		if !strings.Contains(got, `\\C[2]Espada`) && !strings.Contains(got, `\\c[2]Espada`) {
			t.Errorf("got %s", got)
		}
	})
}

// Texto de código que chega ao tradutor por um extrator sem filtro: o mascarador
// precisa esconder o que o Python consertava depois (booleanos, $game*, operadores).
func TestCodeLikeTextIsMaskedBeforeTranslation(t *testing.T) {
	fixes, _ := loadReference(t)
	masker := masking.NewMaskerWith(rpgtext.MaskMatchers()...)
	protected := regexp.MustCompile(`(?i)\b(true|false|null|undefined)\b|\$game[a-z]+|===|==|!=|&&|\|\||>=|<=`)
	for _, c := range fixes {
		if c.Original == nil || !rpgtext.IsTechnicalOrCode(*c.Original, rpgtext.Dialogue) {
			continue
		}
		masked, _ := masker.Mask(*c.Original)
		if m := protected.FindString(masked); m != "" {
			t.Errorf("%s: %q sobrou sem máscara em %q", c.Name, m, masked)
		}
	}
}
