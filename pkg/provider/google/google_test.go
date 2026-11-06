package google_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/BGLuis/slime-rpg/pkg/provider"
	"github.com/BGLuis/slime-rpg/pkg/provider/google"
	"github.com/BGLuis/slime-rpg/pkg/provider/proxy"
)

func TestGoogleTranslator_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		q := r.Form.Get("q")
		if q == "Hello" {
			// Formato padrão Google GTX
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[[["Olá","Hello",null,null,10]],null,"en",null,null,null,null,[]]`))
			return
		}
		if q == "World" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[[["Mundo","World",null,null,10]],null,"en",null,null,null,null,[]]`))
			return
		}
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer ts.Close()

	tr := google.NewGoogleTranslator(nil)
	tr.SetEndpoint(ts.URL)

	results, err := tr.TranslateBatch(context.Background(), []string{"Hello", "World"}, "en", "pt")
	if err != nil {
		t.Fatalf("falha ao traduzir lote: %v", err)
	}

	if len(results) != 2 || results[0] != "Olá" || results[1] != "Mundo" {
		t.Errorf("traduções incorretas: %+v", results)
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

	_, _ = tr.TranslateBatch(context.Background(), []string{"Hello"}, "en", "pt")
	if !pm.DirectBlocked() {
		t.Error("429 no IP direto deveria colocá-lo em cooldown")
	}
}
