package ollama_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/BGLuis/slime-rpg/pkg/provider/ollama"
)

func TestOllamaTranslator_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Prompt string `json:"prompt"`
			Model  string `json:"model"`
			System string `json:"system"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)

		translated := "Olá mundo"
		if req.Prompt == "Good morning" {
			translated = "Bom dia"
		}

		resp := map[string]any{
			"response": translated,
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	tr := ollama.NewOllamaTranslator(ts.URL, "test-model", "Sinopse do Jogo")

	results, err := tr.TranslateBatch(context.Background(), []string{"Hello world", "Good morning"}, "en", "pt")
	if err != nil {
		t.Fatalf("falha ao traduzir via ollama: %v", err)
	}

	if len(results) != 2 || results[0] != "Olá mundo" || results[1] != "Bom dia" {
		t.Errorf("traduções incorretas: %+v", results)
	}
}
