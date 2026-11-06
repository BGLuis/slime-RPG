package deepl

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
	EndpointFree = "https://api-free.deepl.com/v2/translate"
	EndpointPro  = "https://api.deepl.com/v2/translate"
)

// DeepLTranslator implementa a integração com a API oficial da DeepL com suporte a lotes nativos.
type DeepLTranslator struct {
	apiKey   string
	endpoint string
	client   *http.Client
}

// NewDeepLTranslator inicializa o tradutor DeepL selecionando automaticamente o endpoint Free ou Pro
// com base no sufixo da API key (:fx indica conta gratuita).
func NewDeepLTranslator(apiKey string) *DeepLTranslator {
	endpoint := EndpointPro
	if strings.HasSuffix(apiKey, ":fx") {
		endpoint = EndpointFree
	}

	return &DeepLTranslator{
		apiKey:   apiKey,
		endpoint: endpoint,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (d *DeepLTranslator) ID() string {
	return "deepl"
}

// SetEndpoint permite sobrescrever o endpoint para testes mock.
func (d *DeepLTranslator) SetEndpoint(endpoint string) {
	d.endpoint = endpoint
}

type translateRequest struct {
	Text       []string `json:"text"`
	SourceLang string   `json:"source_lang,omitempty"`
	TargetLang string   `json:"target_lang"`
}

type translationItem struct {
	DetectedSourceLanguage string `json:"detected_source_language"`
	Text                   string `json:"text"`
}

type translateResponse struct {
	Translations []translationItem `json:"translations"`
	Message      string            `json:"message,omitempty"`
}

// TranslateBatch despacha o lote nativamente em uma única requisição HTTP para a API DeepL.
func (d *DeepLTranslator) TranslateBatch(ctx context.Context, texts []string, srcLang, tgtLang string) ([]string, error) {
	if len(texts) == 0 {
		return nil, provider.ErrEmptyBatch
	}

	// Normaliza códigos de idioma para o formato DeepL (ex: "pt" -> "PT-BR" ou "PT-PT", "en" -> "EN")
	normalizedTarget := strings.ToUpper(tgtLang)
	if normalizedTarget == "PT" {
		normalizedTarget = "PT-BR"
	}
	normalizedSource := strings.ToUpper(srcLang)

	reqPayload := translateRequest{
		Text:       texts,
		TargetLang: normalizedTarget,
	}
	if normalizedSource != "" && normalizedSource != "AUTO" {
		reqPayload.SourceLang = normalizedSource
	}

	jsonData, err := json.Marshal(reqPayload)
	if err != nil {
		return nil, fmt.Errorf("failed to encode deepl request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.endpoint, bytes.NewReader(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create deepl request: %w", err)
	}

	req.Header.Set("Authorization", fmt.Sprintf("DeepL-Auth-Key %s", d.apiKey))
	req.Header.Set("Content-Type", "application/json")

	resp, err := d.client.Do(req)
	if err != nil {
		if errors.Is(ctx.Err(), context.Canceled) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("network error during deepl translate: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, provider.ErrRateLimited
	}
	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusUnauthorized {
		return nil, provider.ErrAuthFailed
	}
	if resp.StatusCode >= 500 {
		return nil, fmt.Errorf("%w: DeepL HTTP %d", provider.ErrUnavailable, resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("deepl error HTTP %d: %s", resp.StatusCode, string(body))
	}

	var res translateResponse
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, fmt.Errorf("failed to decode deepl response: %w", err)
	}

	if len(res.Translations) != len(texts) {
		return nil, fmt.Errorf("deepl returned mismatched translations count: expected %d, got %d", len(texts), len(res.Translations))
	}

	output := make([]string, len(res.Translations))
	for i, item := range res.Translations {
		output[i] = item.Text
	}

	return output, nil
}
