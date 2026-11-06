package ollama

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/BGLuis/slime-rpg/pkg/provider"
)

func TestHTTPErrorsAreClassified(t *testing.T) {
	cases := map[int]error{
		http.StatusUnauthorized:       provider.ErrAuthFailed,
		http.StatusTooManyRequests:    provider.ErrRateLimited,
		http.StatusServiceUnavailable: provider.ErrUnavailable,
	}
	for status, want := range cases {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "x", status)
		}))
		tr := NewOllamaTranslator(ts.URL, "m", "")
		_, err := tr.TranslateBatch(context.Background(), []string{"oi"}, "pt", "en")
		ts.Close()
		if !errors.Is(err, want) {
			t.Errorf("HTTP %d: esperava %v, obteve %v", status, want, err)
		}
	}
}

func TestSystemPromptKeepsPlaceholderRuleAndSynopsis(t *testing.T) {
	p := systemPrompt("ja", "pt", "Um RPG de fantasia")
	for _, want := range []string{"__XTOK_", "Um RPG de fantasia", "from ja to pt", "Reply with only the translated text"} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt sem %q:\n%s", want, p)
		}
	}
}
