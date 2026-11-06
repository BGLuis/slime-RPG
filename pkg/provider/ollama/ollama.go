package ollama

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/BGLuis/slime-rpg/pkg/provider"
)

const (
	DefaultEndpoint = "http://localhost:11434"
	DefaultModel    = "qwen2.5"
)

// OllamaTranslator implementa a tradução local via LLM do Ollama com suporte a sinopse e contexto do jogo.
type OllamaTranslator struct {
	endpoint string
	model    string
	synopsis string
	client   *http.Client
}

// NewOllamaTranslator inicializa o tradutor Ollama local.
func NewOllamaTranslator(endpoint, modelName, synopsis string) *OllamaTranslator {
	if endpoint == "" {
		endpoint = DefaultEndpoint
	}
	if modelName == "" {
		modelName = DefaultModel
	}
	return &OllamaTranslator{
		endpoint: endpoint,
		model:    modelName,
		synopsis: synopsis,
		client: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

func (o *OllamaTranslator) ID() string {
	return "ollama"
}

// SetEndpoint permite alterar o endpoint do Ollama.
func (o *OllamaTranslator) SetEndpoint(endpoint string) {
	o.endpoint = endpoint
}

type generateRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
	System string `json:"system,omitempty"`
	Stream bool   `json:"stream"`
}

type generateResponse struct {
	Response string `json:"response"`
	Error    string `json:"error,omitempty"`
}

// TranslateBatch traduz uma lista de strings enviando-as individualmente com contexto preservado.
func (o *OllamaTranslator) TranslateBatch(ctx context.Context, texts []string, srcLang, tgtLang string) ([]string, error) {
	if len(texts) == 0 {
		return nil, provider.ErrEmptyBatch
	}

	results := make([]string, len(texts))
	for i, txt := range texts {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		if strings.TrimSpace(txt) == "" {
			results[i] = txt
			continue
		}

		translated, err := o.translateSingle(ctx, txt, srcLang, tgtLang)
		if err != nil {
			return nil, err
		}
		results[i] = translated
	}

	return results, nil
}

func (o *OllamaTranslator) translateSingle(ctx context.Context, text, srcLang, tgtLang string) (string, error) {
	systemPrompt := fmt.Sprintf(
		"You are a professional video game localizer and translator from %s to %s.\n"+
			"CRITICAL RULES:\n"+
			"1. You MUST preserve any placeholders matching `__XTOK_<n>__` EXACTLY as they appear without translating or altering them.\n"+
			"2. Translate dialogue naturally for players.\n"+
			"3. Output ONLY the translated text, with no explanations, notes, or quotes.",
		srcLang, tgtLang,
	)

	if o.synopsis != "" {
		systemPrompt += fmt.Sprintf("\nGame Synopsis and Lore Context:\n%s", o.synopsis)
	}

	reqPayload := generateRequest{
		Model:  o.model,
		System: systemPrompt,
		Prompt: text,
		Stream: false,
	}

	jsonData, err := json.Marshal(reqPayload)
	if err != nil {
		return "", fmt.Errorf("failed to encode ollama request: %w", err)
	}

	apiURL := fmt.Sprintf("%s/api/generate", strings.TrimRight(o.endpoint, "/"))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewReader(jsonData))
	if err != nil {
		return "", fmt.Errorf("failed to create ollama request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := o.client.Do(req)
	if err != nil {
		if errors.Is(ctx.Err(), context.Canceled) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return "", ctx.Err()
		}
		return "", fmt.Errorf("%w: failed to connect to local ollama: %v", provider.ErrUnavailable, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("ollama HTTP %d: %s", resp.StatusCode, string(body))
	}

	var res generateResponse
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return "", fmt.Errorf("failed to decode ollama response: %w", err)
	}

	if res.Error != "" {
		return "", fmt.Errorf("ollama model error: %s", res.Error)
	}

	cleaned := strings.TrimSpace(res.Response)
	// Remove aspas adicionadas pelo modelo se envolverem a frase inteira
	if (strings.HasPrefix(cleaned, "\"") && strings.HasSuffix(cleaned, "\"")) ||
		(strings.HasPrefix(cleaned, "“") && strings.HasSuffix(cleaned, "”")) {
		if len(cleaned) >= 2 {
			cleaned = cleaned[1 : len(cleaned)-1]
		}
	}

	return cleaned, nil
}
