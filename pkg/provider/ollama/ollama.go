package ollama

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
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

// Capabilities envia um texto por requisição: o modelo traduz melhor um texto
// isolado do que uma lista. A concorrência pode ser ajustada por
// OLLAMA_MAX_REQUESTS, como na versão Python.
func (o *OllamaTranslator) Capabilities() provider.Capabilities {
	concurrency := 10
	if n, err := strconv.Atoi(os.Getenv("OLLAMA_MAX_REQUESTS")); err == nil && n > 0 {
		concurrency = n
	}
	return provider.Capabilities{MaxChars: 10000, MaxItems: 1, Concurrency: concurrency}
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
		return []string{}, nil
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
	systemPrompt := systemPrompt(srcLang, tgtLang, o.synopsis)

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
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		switch {
		case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
			return "", fmt.Errorf("%w: ollama HTTP %d", provider.ErrAuthFailed, resp.StatusCode)
		case resp.StatusCode == http.StatusTooManyRequests:
			return "", fmt.Errorf("%w: ollama", provider.ErrRateLimited)
		case resp.StatusCode >= 500:
			return "", fmt.Errorf("%w: ollama HTTP %d: %s", provider.ErrUnavailable, resp.StatusCode, body)
		}
		return "", fmt.Errorf("ollama HTTP %d: %s", resp.StatusCode, body)
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

// Instruções do tradutor Ollama da versão Python; OLLAMA_CONTEXT_FUNCTION e
// OLLAMA_CONTEXT_ADDITIONAL substituem as duas primeiras, como lá.
const (
	defaultContextFunction   = "Reply with only the translated text, without any additional comments, explanations, or notes."
	defaultContextAdditional = "Act as an expert game localizer. Your mission is to translate text for a video game, ensuring the translation is engaging and immersive for the player. The text could be character dialogue, an item description, a quest objective, or a UI menu element. Adapt the translation to fit the gaming context, using appropriate and common gaming jargon. Maintain the original tone, whether it's serious, humorous, or epic. Do not provide a literal, word-for-word translation if a more natural, context-aware alternative exists."
	placeholderInstruction   = "Tokens like __XTOK_12345678__ are placeholders for game control codes: copy each one into the translation exactly as written, without translating, reformatting, or removing it."
)

func systemPrompt(srcLang, tgtLang, synopsis string) string {
	function := cmp.Or(os.Getenv("OLLAMA_CONTEXT_FUNCTION"), defaultContextFunction)
	additional := cmp.Or(os.Getenv("OLLAMA_CONTEXT_ADDITIONAL"), defaultContextAdditional)
	parts := []string{function, additional, placeholderInstruction}
	if synopsis != "" {
		parts = append(parts, "Game Synopsis/Context: "+synopsis+"\n\nUse this synopsis to better understand the game's context and provide more accurate, context-aware translations.")
	}
	parts = append(parts, fmt.Sprintf("Translate the user's message from %s to %s.", srcLang, tgtLang))
	return strings.Join(parts, "\n\n")
}
