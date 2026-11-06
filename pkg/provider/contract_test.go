package provider_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/BGLuis/slime-rpg/pkg/provider"
	"github.com/BGLuis/slime-rpg/pkg/provider/deepl"
	"github.com/BGLuis/slime-rpg/pkg/provider/google"
	"github.com/BGLuis/slime-rpg/pkg/provider/ollama"
)

func googleFake(t *testing.T) provider.Provider {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		q := r.Form.Get("q")
		parts := strings.Split(q, google.Delimiter)
		for i, p := range parts {
			parts[i] = "TR:" + p
		}
		_ = json.NewEncoder(w).Encode([]any{[]any{[]any{strings.Join(parts, google.Delimiter), q}}, nil, "en"})
	}))
	t.Cleanup(ts.Close)
	g := google.NewGoogleTranslator(nil)
	g.SetEndpoint(ts.URL)
	return g
}

func deeplFake(t *testing.T) provider.Provider {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Text []string `json:"text"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		var out []map[string]string
		for _, txt := range req.Text {
			out = append(out, map[string]string{"text": "TR:" + txt})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"translations": out})
	}))
	t.Cleanup(ts.Close)
	d := deepl.NewDeepLTranslator("dummy:fx")
	d.SetEndpoint(ts.URL)
	return d
}

func ollamaFake(t *testing.T) provider.Provider {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Prompt string `json:"prompt"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		_ = json.NewEncoder(w).Encode(map[string]string{"response": "TR:" + req.Prompt})
	}))
	t.Cleanup(ts.Close)
	return ollama.NewOllamaTranslator(ts.URL, "dummy-model", "")
}

// Todo provedor precisa cumprir o mesmo contrato para ser intercambiável no pipeline.
func TestProviderContract(t *testing.T) {
	providers := map[string]func(*testing.T) provider.Provider{
		"google": googleFake,
		"deepl":  deeplFake,
		"ollama": ollamaFake,
	}
	for wantID, build := range providers {
		t.Run(wantID, func(t *testing.T) {
			p := build(t)
			if p.ID() != wantID {
				t.Errorf("ID() = %q, esperava %q", p.ID(), wantID)
			}

			empty, err := p.TranslateBatch(context.Background(), nil, "en", "pt")
			if err != nil || len(empty) != 0 {
				t.Errorf("lote vazio deve devolver lista vazia sem erro, obteve %v, %v", empty, err)
			}

			got, err := p.TranslateBatch(context.Background(), []string{"Hello", "World"}, "en", "pt")
			if err != nil {
				t.Fatal(err)
			}
			if want := []string{"TR:Hello", "TR:World"}; !slices.Equal(got, want) {
				t.Errorf("tradução deve preservar tamanho e ordem: %v, esperava %v", got, want)
			}
		})
	}
}
