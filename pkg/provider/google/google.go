package google

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
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

// Delimiter separa os textos de um lote numa única requisição. O Google preserva a
// tag <span> intacta, o que permite dividir a resposta de volta em itens.
const Delimiter = "\n<span> </span>\n"

var mangledDelimiterRE = regexp.MustCompile(`\s*<span[^>]*>\s*</span>\s*`)

// GoogleTranslator implementa o provedor de tradução via endpoint público Google GTX com suporte a proxies rotativos.
type GoogleTranslator struct {
	endpoint        string
	proxyManager    *proxy.Manager
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
		pacingInterval: 200 * time.Millisecond,
	}
}

func (g *GoogleTranslator) ID() string {
	return "google"
}

// Capabilities segue os limites do endpoint GTX usados pela versão Python. Com o
// pool de proxies ativo e o IP direto bloqueado, cada proxy aguenta uma requisição
// própria, então a concorrência cresce com o pool (até 16).
func (g *GoogleTranslator) Capabilities() provider.Capabilities {
	concurrency := 4
	if g.proxyManager.IsEnabled() && g.proxyManager.DirectBlocked() {
		concurrency = max(concurrency, min(16, g.proxyManager.PoolSize()))
	}
	return provider.Capabilities{MaxChars: 5000, Concurrency: concurrency}
}

// CanBypassRateLimit indica se um HTTP 429 pode ser contornado pelo pool de proxies.
func (g *GoogleTranslator) CanBypassRateLimit() bool {
	return g.proxyManager.CanRotate()
}

// SetEndpoint permite sobrescrever o endpoint para testes locais ou proxies reversos.
func (g *GoogleTranslator) SetEndpoint(endpoint string) {
	g.endpoint = endpoint
}

// TranslateBatch envia o lote numa única requisição, unindo os textos com
// Delimiter. Se o Google deformar o delimitador a ponto de a contagem não bater,
// os textos são traduzidos um a um para não desalinhar as traduções.
func (g *GoogleTranslator) TranslateBatch(ctx context.Context, texts []string, srcLang, tgtLang string) ([]string, error) {
	results := make([]string, len(texts))
	var pending []int
	for i, text := range texts {
		results[i] = text
		if strings.TrimSpace(text) != "" {
			pending = append(pending, i)
		}
	}
	if len(pending) == 0 {
		return results, nil
	}

	joined := make([]string, len(pending))
	for i, idx := range pending {
		joined[i] = texts[idx]
	}
	translated, err := g.translateSingle(ctx, strings.Join(joined, Delimiter), srcLang, tgtLang)
	if err != nil {
		return nil, err
	}

	parts := strings.Split(translated, Delimiter)
	if len(parts) != len(pending) {
		parts = mangledDelimiterRE.Split(translated, -1)
	}
	if len(parts) != len(pending) {
		for _, idx := range pending {
			single, err := g.translateSingle(ctx, texts[idx], srcLang, tgtLang)
			if err != nil {
				return nil, err
			}
			results[idx] = single
		}
		return results, nil
	}

	for i, idx := range pending {
		results[idx] = parts[i]
	}
	return results, nil
}

func (g *GoogleTranslator) translateSingle(ctx context.Context, text, srcLang, tgtLang string) (string, error) {
	client, proxyURL := g.proxyManager.Client(10 * time.Second)
	if proxyURL == "" {
		// O pacing protege só o IP direto; cada proxy tem seu próprio limite.
		g.rateLimitPacing()
	}

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

	resp, err := client.Do(req)
	if err != nil {
		if errors.Is(ctx.Err(), context.Canceled) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return "", ctx.Err()
		}
		if proxyURL != "" {
			g.proxyManager.ReportProxyFailure(proxyURL)
		}
		return "", fmt.Errorf("network error during google translate: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests {
		if proxyURL != "" {
			g.proxyManager.ReportProxyFailure(proxyURL)
		} else {
			g.proxyManager.ReportDirectRateLimit()
		}
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

	if proxyURL == "" {
		g.proxyManager.ReportDirectSuccess()
	}
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
