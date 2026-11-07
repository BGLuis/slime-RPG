package google_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/BGLuis/slime-rpg/pkg/provider"
	"github.com/BGLuis/slime-rpg/pkg/provider/google"
	"github.com/BGLuis/slime-rpg/pkg/provider/proxy"
)

// gtxFake responde como o endpoint GTX, traduzindo cada parte do lote e
// preservando o delimitador, e conta as requisições recebidas.
func gtxFake(t *testing.T, translate func(string) string, requests *atomic.Int32) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		_ = r.ParseForm()
		parts := strings.Split(r.Form.Get("q"), google.Delimiter)
		for i, p := range parts {
			parts[i] = translate(p)
		}
		_ = json.NewEncoder(w).Encode([]any{[]any{[]any{strings.Join(parts, google.Delimiter), r.Form.Get("q")}}, nil, "en"})
	}))
	t.Cleanup(ts.Close)
	return ts
}

// offline aponta os fallbacks para um servidor local que sempre falha, evitando rede real.
func offline(t *testing.T, tr *google.GoogleTranslator) {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "down", http.StatusInternalServerError)
	}))
	t.Cleanup(ts.Close)
	tr.SetWebEndpoint(ts.URL)
	tr.SetMyMemoryEndpoint(ts.URL)
}

func TestGoogleTranslator_SendsBatchInOneRequest(t *testing.T) {
	var requests atomic.Int32
	dict := map[string]string{"Hello": "Olá", "World": "Mundo"}
	ts := gtxFake(t, func(s string) string { return dict[s] }, &requests)

	tr := google.NewGoogleTranslator(nil)
	tr.SetEndpoint(ts.URL)

	results, err := tr.TranslateBatch(context.Background(), []string{"Hello", "  ", "World"}, "en", "pt")
	if err != nil {
		t.Fatalf("falha ao traduzir lote: %v", err)
	}
	if want := []string{"Olá", "  ", "Mundo"}; !slices.Equal(results, want) {
		t.Errorf("traduções = %q, esperava %q", results, want)
	}
	if got := requests.Load(); got != 1 {
		t.Errorf("o lote deveria ir numa única requisição, foram %d", got)
	}
}

// Se o Google engolir o delimitador, dividir a resposta desalinharia as traduções:
// os textos precisam ser traduzidos individualmente.
func TestGoogleTranslator_FallsBackPerItemWhenDelimiterIsLost(t *testing.T) {
	var requests atomic.Int32
	ts := gtxFake(t, func(s string) string { return "pt:" + s }, &requests)
	ts.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		_ = r.ParseForm()
		q := strings.ReplaceAll(r.Form.Get("q"), google.Delimiter, " ")
		_ = json.NewEncoder(w).Encode([]any{[]any{[]any{"pt:" + q, q}}, nil, "en"})
	})

	tr := google.NewGoogleTranslator(nil)
	tr.SetEndpoint(ts.URL)

	results, err := tr.TranslateBatch(context.Background(), []string{"a", "b"}, "en", "pt")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"pt:a", "pt:b"}; !slices.Equal(results, want) {
		t.Errorf("traduções = %q, esperava %q", results, want)
	}
	if got := requests.Load(); got != 3 {
		t.Errorf("esperava 1 requisição em lote + 2 individuais, foram %d", got)
	}
}

func TestGoogleTranslator_RateLimit429(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
	}))
	defer ts.Close()

	tr := google.NewGoogleTranslator(nil)
	tr.SetEndpoint(ts.URL)

	_, err := tr.TranslateBatch(context.Background(), []string{"Hello"}, "en", "pt")
	if err == nil {
		t.Fatal("esperava erro de rate limit 429")
	}
	if !errors.Is(err, provider.ErrRateLimited) {
		t.Errorf("esperava ErrRateLimited, obteve: %v", err)
	}
}

func TestGoogleTranslator_RateLimitThroughProxyEvictsProxyNotDirect(t *testing.T) {
	proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
	}))
	defer proxyServer.Close()

	pm := proxy.NewManager(true, []string{proxyServer.URL})
	pm.ReportDirectRateLimit()

	tr := google.NewGoogleTranslator(pm)
	tr.SetEndpoint("http://translate.invalid/translate_a/single")
	offline(t, tr)

	_, err := tr.TranslateBatch(context.Background(), []string{"Hello"}, "en", "pt")
	if !errors.Is(err, provider.ErrRateLimited) {
		t.Fatalf("esperava ErrRateLimited, obteve %v", err)
	}
	if pm.PoolSize() != 0 {
		t.Errorf("o proxy bloqueado deveria sair do pool, restam %d", pm.PoolSize())
	}
}

func TestGoogleTranslator_DirectRateLimitBlocksDirectIP(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
	}))
	defer ts.Close()

	pm := proxy.NewManager(true, nil)
	tr := google.NewGoogleTranslator(pm)
	tr.SetEndpoint(ts.URL)
	offline(t, tr)

	_, _ = tr.TranslateBatch(context.Background(), []string{"Hello"}, "en", "pt")
	if !pm.DirectBlocked() {
		t.Error("429 no IP direto deveria colocá-lo em cooldown")
	}
}
