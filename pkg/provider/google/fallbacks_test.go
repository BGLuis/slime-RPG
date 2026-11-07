package google_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"

	"github.com/BGLuis/slime-rpg/pkg/provider"
	"github.com/BGLuis/slime-rpg/pkg/provider/google"
	"github.com/BGLuis/slime-rpg/pkg/provider/proxy"
)

// callLog registra, em ordem, qual backend recebeu cada requisição.
type callLog struct {
	mu    sync.Mutex
	calls []string
}

func (c *callLog) add(name string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls = append(c.calls, name)
}

func (c *callLog) String() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return strings.Join(c.calls, ",")
}

func server(t *testing.T, log *callLog, name string, h http.HandlerFunc) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.add(name)
		h(w, r)
	}))
	t.Cleanup(ts.Close)
	return ts
}

func status(code int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) { http.Error(w, "x", code) }
}

func webOK(translate func(string) string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `<html><div class="result-container">%s</div></html>`, translate(r.URL.Query().Get("q")))
	}
}

func myMemoryOK(translate func(string) string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"responseData": map[string]string{"translatedText": translate(r.URL.Query().Get("q"))},
		})
	}
}

func newChain(t *testing.T, pm *proxy.Manager, gtx, web, mm http.HandlerFunc) (*google.GoogleTranslator, *callLog) {
	t.Helper()
	log := &callLog{}
	tr := google.NewGoogleTranslator(pm)
	tr.SetEndpoint(server(t, log, "gtx", gtx).URL)
	tr.SetWebEndpoint(server(t, log, "web", web).URL)
	tr.SetMyMemoryEndpoint(server(t, log, "mm", mm).URL)
	return tr, log
}

func upper(s string) string { return "PT:" + s }

func TestFallback_GTXFailureUsesWebAndStops(t *testing.T) {
	tr, log := newChain(t, nil, status(500), webOK(upper), myMemoryOK(upper))

	got, err := tr.TranslateBatch(context.Background(), []string{"Hello"}, "en", "pt")
	if err != nil || got[0] != "PT:Hello" {
		t.Fatalf("got %q, err %v", got, err)
	}
	if log.String() != "gtx,web" {
		t.Errorf("ordem das chamadas = %s, esperava gtx,web", log)
	}
}

func TestFallback_WebFailureUsesMyMemory(t *testing.T) {
	tr, log := newChain(t, nil, status(500), status(500), myMemoryOK(func(s string) string { return "MM:" + s }))

	got, err := tr.TranslateBatch(context.Background(), []string{"Hello"}, "en", "pt")
	if err != nil || got[0] != "MM:Hello" {
		t.Fatalf("got %q, err %v", got, err)
	}
	if log.String() != "gtx,web,mm" {
		t.Errorf("ordem das chamadas = %s, esperava gtx,web,mm", log)
	}
}

func TestFallback_WebMissingResultFallsThroughToMyMemory(t *testing.T) {
	tr, log := newChain(t, nil, status(500),
		func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "<html>captcha</html>") },
		myMemoryOK(upper))

	got, err := tr.TranslateBatch(context.Background(), []string{"Hello"}, "en", "pt")
	if err != nil || got[0] != "PT:Hello" || log.String() != "gtx,web,mm" {
		t.Fatalf("got %q, err %v, calls %s", got, err, log)
	}
}

func TestFallback_WebEchoWithoutLettersFallsThroughButWithLettersIsKept(t *testing.T) {
	echo := webOK(func(s string) string { return s })

	tr, log := newChain(t, nil, status(500), echo, myMemoryOK(upper))
	got, _ := tr.TranslateBatch(context.Background(), []string{"!!!"}, "en", "pt")
	if got[0] != "PT:!!!" || log.String() != "gtx,web,mm" {
		t.Errorf("eco sem letras deveria cair no MyMemory: %q, %s", got, log)
	}

	tr, log = newChain(t, nil, status(500), echo, myMemoryOK(upper))
	got, _ = tr.TranslateBatch(context.Background(), []string{"Hello"}, "en", "pt")
	if got[0] != "Hello" || log.String() != "gtx,web" {
		t.Errorf("eco com letras (nome próprio) deveria ser aceito: %q, %s", got, log)
	}
}

func TestFallback_MyMemoryLanguageMappingAndQuery(t *testing.T) {
	var langpair, q string
	tr, _ := newChain(t, nil, status(500), status(500), func(w http.ResponseWriter, r *http.Request) {
		langpair, q = r.URL.Query().Get("langpair"), r.URL.Query().Get("q")
		_ = json.NewEncoder(w).Encode(map[string]any{"responseData": map[string]string{"translatedText": "ok"}})
	})
	if _, err := tr.TranslateBatch(context.Background(), []string{"こんにちは"}, "ja", "pt"); err != nil {
		t.Fatal(err)
	}
	if langpair != "ja-JP|pt-BR" || q != "こんにちは" {
		t.Errorf("langpair=%q q=%q", langpair, q)
	}
}

func TestFallback_AllFailReturnsErrorInsteadOfSourceText(t *testing.T) {
	tr, log := newChain(t, nil, status(500), status(500), status(500))

	got, err := tr.TranslateBatch(context.Background(), []string{"Hello"}, "en", "pt")
	if !errors.Is(err, provider.ErrUnavailable) {
		t.Fatalf("esperava ErrUnavailable, got %q, err %v", got, err)
	}
	if log.String() != "gtx,web,mm" {
		t.Errorf("ordem das chamadas = %s", log)
	}
}

func TestFallback_MyMemoryChunksLongTextOnNewlines(t *testing.T) {
	var mu sync.Mutex
	var sizes []int
	tr, _ := newChain(t, nil, status(500), status(500), func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("q")
		mu.Lock()
		sizes = append(sizes, utf8.RuneCountInString(q))
		mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{"responseData": map[string]string{"translatedText": "T(" + q + ")"}})
	})

	line := strings.Repeat("a", 300)
	text := line + "\n" + line + "\n" + line
	got, err := tr.TranslateBatch(context.Background(), []string{text}, "en", "pt")
	if err != nil {
		t.Fatal(err)
	}
	if len(sizes) != 3 {
		t.Fatalf("esperava 3 pedaços (quebra em \\n), foram %v", sizes)
	}
	for _, n := range sizes {
		if n > 450 {
			t.Errorf("pedaço com %d caracteres excede 450", n)
		}
	}
	want := "T(" + line + ")\nT(" + line + ")\nT(" + line + ")"
	if got[0] != want {
		t.Errorf("pedaços devem ser reunidos com \\n: %q", got[0])
	}
}

func TestFallback_MyMemoryHardSplitsWithoutNewlines(t *testing.T) {
	var mu sync.Mutex
	var sizes []int
	tr, _ := newChain(t, nil, status(500), status(500), func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("q")
		mu.Lock()
		sizes = append(sizes, utf8.RuneCountInString(q))
		mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{"responseData": map[string]string{"translatedText": q}})
	})
	if _, err := tr.TranslateBatch(context.Background(), []string{strings.Repeat("é", 1000)}, "en", "pt"); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(sizes) != "[450 450 100]" {
		t.Errorf("tamanhos = %v, esperava [450 450 100]", sizes)
	}
}

func TestFallback_MyMemoryKeepsDelimiterAndSourceOnPartialFailure(t *testing.T) {
	tr, _ := newChain(t, nil, status(500), status(500), func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("q")
		if q == "bad" {
			http.Error(w, "x", http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"responseData": map[string]string{"translatedText": "PT:" + q}})
	})
	got, err := tr.TranslateBatch(context.Background(), []string{"ok", "bad", "fine"}, "en", "pt")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, "|") != "PT:ok|bad|PT:fine" {
		t.Errorf("got %q", got)
	}
}

// Com o IP direto bloqueado e proxies desativados, nenhum fallback pode ser tentado:
// o erro tem de subir como rate limit (o engine pausa em vez de gastar o MyMemory).
func TestFallback_Gtx429WithoutProxyPoolStopsChain(t *testing.T) {
	tr, log := newChain(t, nil, status(429), webOK(upper), myMemoryOK(upper))

	_, err := tr.TranslateBatch(context.Background(), []string{"Hello"}, "en", "pt")
	if !errors.Is(err, provider.ErrRateLimited) {
		t.Fatalf("esperava ErrRateLimited, got %v", err)
	}
	if log.String() != "gtx" {
		t.Errorf("fallbacks não deveriam ser chamados: %s", log)
	}
}

func TestFallback_AlreadyBlockedWithoutPoolMakesNoRequests(t *testing.T) {
	pm := proxy.NewManager(false, nil)
	pm.ReportDirectRateLimit()
	tr, log := newChain(t, pm, webOK(upper), webOK(upper), myMemoryOK(upper))

	_, err := tr.TranslateBatch(context.Background(), []string{"Hello"}, "en", "pt")
	if !errors.Is(err, provider.ErrRateLimited) || log.String() != "" {
		t.Errorf("err=%v calls=%q", err, log)
	}
}

// Com o pool habilitado, o 429 do gtx não é fatal: a cadeia continua.
func TestFallback_Gtx429WithProxyPoolEnabledContinuesToWeb(t *testing.T) {
	pm := proxy.NewManager(true, nil)
	tr, log := newChain(t, pm, status(429), webOK(upper), myMemoryOK(upper))

	got, err := tr.TranslateBatch(context.Background(), []string{"Hello"}, "en", "pt")
	if err != nil || got[0] != "PT:Hello" || log.String() != "gtx,web" {
		t.Errorf("got %q err=%v calls=%s", got, err, log)
	}
}

func TestFallback_AllRateLimitedWithPoolEnabledSurfacesRateLimit(t *testing.T) {
	pm := proxy.NewManager(true, nil)
	tr, _ := newChain(t, pm, status(429), status(429), status(429))

	_, err := tr.TranslateBatch(context.Background(), []string{"Hello"}, "en", "pt")
	if !errors.Is(err, provider.ErrRateLimited) {
		t.Errorf("esperava ErrRateLimited, got %v", err)
	}
}

func TestFallback_BatchPathStillUsesGtxDelimiterOnSuccess(t *testing.T) {
	tr, log := newChain(t, nil, gtxHandler(), status(500), status(500))

	got, err := tr.TranslateBatch(context.Background(), []string{"a", "b"}, "en", "pt")
	if err != nil || strings.Join(got, "|") != "PT:a|PT:b" || log.String() != "gtx" {
		t.Errorf("got %q err=%v calls=%s", got, err, log)
	}
}

func gtxHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		parts := strings.Split(r.Form.Get("q"), google.Delimiter)
		for i, p := range parts {
			parts[i] = upper(p)
		}
		_ = json.NewEncoder(w).Encode([]any{[]any{[]any{strings.Join(parts, google.Delimiter), ""}}})
	}
}
