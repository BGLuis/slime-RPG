package deepl_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/BGLuis/slime-rpg/pkg/provider"
	"github.com/BGLuis/slime-rpg/pkg/provider/deepl"
)

func TestDeepLTranslator_BatchSuccess(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "DeepL-Auth-Key test-key:fx" {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}

		var req struct {
			Text       []string `json:"text"`
			TargetLang string   `json:"target_lang"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)

		resp := map[string]any{
			"translations": []map[string]string{
				{"detected_source_language": "EN", "text": "Olá"},
				{"detected_source_language": "EN", "text": "Mundo"},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	tr := deepl.NewDeepLTranslator("test-key:fx")
	tr.SetEndpoint(ts.URL)

	results, err := tr.TranslateBatch(context.Background(), []string{"Hello", "World"}, "en", "pt")
	if err != nil {
		t.Fatalf("falha ao traduzir via deepl: %v", err)
	}

	if len(results) != 2 || results[0] != "Olá" || results[1] != "Mundo" {
		t.Errorf("traduções incorretas: %+v", results)
	}
}

func TestDeepLTranslator_AuthError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Invalid auth", http.StatusForbidden)
	}))
	defer ts.Close()

	tr := deepl.NewDeepLTranslator("invalid-key")
	tr.SetEndpoint(ts.URL)

	_, err := tr.TranslateBatch(context.Background(), []string{"Test"}, "en", "pt")
	if err == nil {
		t.Fatal("esperava erro de autenticação")
	}
	if !errors.Is(err, provider.ErrAuthFailed) {
		t.Errorf("esperava ErrAuthFailed, obteve: %v", err)
	}
}

func TestMapLangCode(t *testing.T) {
	tests := []struct {
		code     string
		isTarget bool
		want     string
	}{
		{"ja", false, "JA"},
		{"pt", true, "PT-BR"},
		{"pt", false, "PT"},
		{"en", true, "EN-US"},
		{"en", false, "EN"},
		{"pt-BR", false, "PT"},
		{"en-GB", true, "EN-GB"},
		{"auto", false, ""},
		{"", false, ""},
		{"", true, "EN-US"},
	}
	for _, tt := range tests {
		if got := deepl.MapLangCode(tt.code, tt.isTarget); got != tt.want {
			t.Errorf("MapLangCode(%q, target=%v) = %q, esperava %q", tt.code, tt.isTarget, got, tt.want)
		}
	}
}

func TestDeepLTranslator_SendsMappedLanguagesAndFormattingOptions(t *testing.T) {
	var got map[string]any
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		_ = json.NewEncoder(w).Encode(map[string]any{"translations": []map[string]string{{"text": "x"}}})
	}))
	defer ts.Close()

	tr := deepl.NewDeepLTranslator("k:fx")
	tr.SetEndpoint(ts.URL)
	if _, err := tr.TranslateBatch(context.Background(), []string{"a"}, "pt", "en"); err != nil {
		t.Fatal(err)
	}
	if got["source_lang"] != "PT" || got["target_lang"] != "EN-US" {
		t.Errorf("idiomas mapeados incorretamente: %v", got)
	}
	if got["preserve_formatting"] != true || got["tag_handling"] != "xml" {
		t.Errorf("opções de formatação ausentes: %v", got)
	}
}

func TestDeepLTranslator_QuotaExceededIsAuthError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Quota exceeded", 456)
	}))
	defer ts.Close()

	tr := deepl.NewDeepLTranslator("k:fx")
	tr.SetEndpoint(ts.URL)
	if _, err := tr.TranslateBatch(context.Background(), []string{"a"}, "en", "pt"); !errors.Is(err, provider.ErrAuthFailed) {
		t.Errorf("HTTP 456 deve ser tratado como falha de autenticação/cota, obteve %v", err)
	}
}
