package google

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/BGLuis/slime-rpg/pkg/provider"
	"github.com/BGLuis/slime-rpg/pkg/provider/proxy"
)

const (
	EndpointGTX = "https://translate.googleapis.com/translate_a/single"
	UserAgent   = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/128.0.0.0 Safari/537.36"
)

// GoogleTranslator implementa o provedor de tradução via endpoint público Google GTX com suporte a proxies rotativos.
type GoogleTranslator struct {
	endpoint        string
	proxyManager    *proxy.Manager
	concurrency     int
	pacingInterval  time.Duration
	lastRequestTime time.Time
	rateMu          sync.Mutex
}

// NewGoogleTranslator inicializa um novo tradutor Google.
func NewGoogleTranslator(pm *proxy.Manager) *GoogleTranslator {
	if pm == nil {
		pm = proxy.NewManager(false, nil)
	}
	return &GoogleTranslator{
		endpoint:       EndpointGTX,
		proxyManager:   pm,
		concurrency:    4,
		pacingInterval: 150 * time.Millisecond,
	}
}

func (g *GoogleTranslator) ID() string {
	return "google"
}

// SetEndpoint permite sobrescrever o endpoint para testes locais ou proxies reversos.
func (g *GoogleTranslator) SetEndpoint(endpoint string) {
	g.endpoint = endpoint
}

// TranslateBatch traduz uma lista de textos de forma concorrente respeitando limites de taxa e pooling de proxies.
func (g *GoogleTranslator) TranslateBatch(ctx context.Context, texts []string, srcLang, tgtLang string) ([]string, error) {
	if len(texts) == 0 {
		return nil, provider.ErrEmptyBatch
	}

	results := make([]string, len(texts))
	errs := make([]error, len(texts))

	// Canal semafórico para controle de concorrência
	sem := make(chan struct{}, g.concurrency)
	var wg sync.WaitGroup

	for i, text := range texts {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		wg.Add(1)
		sem <- struct{}{}

		go func(idx int, txt string) {
			defer wg.Done()
			defer func() { <-sem }()

			if strings.TrimSpace(txt) == "" {
				results[idx] = txt
				return
			}

			translated, err := g.translateSingle(ctx, txt, srcLang, tgtLang)
			if err != nil {
				errs[idx] = err
				return
			}
			results[idx] = translated
		}(i, text)
	}

	wg.Wait()

	// Retorna o primeiro erro encontrado, se houver
	for _, err := range errs {
		if err != nil {
			return nil, err
		}
	}

	return results, nil
}

func (g *GoogleTranslator) translateSingle(ctx context.Context, text, srcLang, tgtLang string) (string, error) {
	// Pacing suave para não sobrecarregar o IP direto
	g.rateLimitPacing()

	params := url.Values{}
	params.Set("client", "gtx")
	params.Set("sl", srcLang)
	params.Set("tl", tgtLang)
	params.Set("dt", "t")
	params.Set("q", text)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.endpoint, strings.NewReader(params.Encode()))
	if err != nil {
		return "", fmt.Errorf("failed to create google request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded;charset=utf-8")
	req.Header.Set("User-Agent", UserAgent)

	client := g.proxyManager.GetClient(10 * time.Second)
	resp, err := client.Do(req)
	if err != nil {
		if errors.Is(ctx.Err(), context.Canceled) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return "", ctx.Err()
		}
		return "", fmt.Errorf("network error during google translate: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests {
		g.proxyManager.ReportDirectRateLimit()
		return "", provider.ErrRateLimited
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return "", provider.ErrAuthFailed
	}
	if resp.StatusCode >= 500 {
		return "", fmt.Errorf("%w: HTTP %d", provider.ErrUnavailable, resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("google gtx error HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read google response: %w", err)
	}

	// Parsing do formato aninhado de resposta: [[["traduzido", "original", ...], ...], ...]
	var rawResponse []any
	if err := json.Unmarshal(body, &rawResponse); err != nil {
		return "", fmt.Errorf("invalid json response from google: %w", err)
	}

	if len(rawResponse) == 0 {
		return "", errors.New("empty response array from google")
	}

	segmentsArray, ok := rawResponse[0].([]any)
	if !ok || len(segmentsArray) == 0 {
		return "", errors.New("unexpected payload structure from google")
	}

	var sb strings.Builder
	for _, segRaw := range segmentsArray {
		segTuple, ok := segRaw.([]any)
		if ok && len(segTuple) > 0 {
			if part, ok := segTuple[0].(string); ok {
				sb.WriteString(part)
			}
		}
	}

	g.proxyManager.ReportDirectSuccess()
	return sb.String(), nil
}

func (g *GoogleTranslator) rateLimitPacing() {
	g.rateMu.Lock()
	defer g.rateMu.Unlock()

	now := time.Now()
	elapsed := now.Sub(g.lastRequestTime)
	if elapsed < g.pacingInterval {
		time.Sleep(g.pacingInterval - elapsed)
	}
	g.lastRequestTime = time.Now()
}
