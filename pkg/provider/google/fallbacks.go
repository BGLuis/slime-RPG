package google

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/BGLuis/slime-rpg/pkg/provider"
)

const (
	webMaxChars        = 5000
	myMemoryChunkChars = 450
)

var (
	// A página mobile devolve o resultado em div.result-container (ou div.t0 em layouts antigos).
	webResultRE = regexp.MustCompile(`(?s)<div[^>]*class="(?:result-container|t0)"[^>]*>(.*?)</div>`)
	htmlTagRE   = regexp.MustCompile(`(?s)<[^>]*>`)

	myMemoryLangMap = map[string]string{
		"ja": "ja-JP", "pt": "pt-BR", "en": "en-GB", "es": "es-ES", "fr": "fr-FR",
		"de": "de-DE", "it": "it-IT", "ko": "ko-KR", "zh-cn": "zh-CN", "zh": "zh-CN",
	}
)

// blockedWithoutPool indica o estado em que nenhum fallback adianta: o IP direto está
// bloqueado e o pool de proxies está desativado.
func (g *GoogleTranslator) blockedWithoutPool() bool {
	return g.proxyManager.DirectBlocked() && !g.proxyManager.IsEnabled()
}

// translateRaw tenta gtx, depois a página web do Google e por fim o MyMemory, na
// mesma ordem da versão Python. Um HTTP 429 com o IP direto bloqueado e o pool
// desativado interrompe a cadeia como ErrRateLimited.
//
// Diferença deliberada: o Python devolve o texto original quando tudo falha. Aqui
// devolvemos erro (priorizando rate limit), para que o engine conte a falha e tente
// de novo em vez de tratar o texto original como tradução.
func (g *GoogleTranslator) translateRaw(ctx context.Context, text, srcLang, tgtLang string) (string, error) {
	if strings.TrimSpace(text) == "" {
		return text, nil
	}
	if g.blockedWithoutPool() {
		return "", provider.ErrRateLimited
	}

	var errs []error
	res, err := g.translateGTX(ctx, text, srcLang, tgtLang)
	if err == nil && res != "" {
		return res, nil
	}
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if errors.Is(err, provider.ErrRateLimited) && g.blockedWithoutPool() {
		return "", err
	}
	if err != nil {
		errs = append(errs, err)
	}
	if g.blockedWithoutPool() {
		return "", provider.ErrRateLimited
	}

	res, err = g.translateWeb(ctx, text, srcLang, tgtLang)
	if err == nil && res != "" {
		return res, nil
	}
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if err != nil {
		errs = append(errs, err)
	}

	res, err = g.translateMyMemory(ctx, text, srcLang, tgtLang)
	if err == nil {
		return res, nil
	}
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	return "", joinFallbackErrors(append(errs, err))
}

// joinFallbackErrors escolhe o erro que o engine deve ver: rate limit e falha de
// autenticação têm tratamento próprio lá.
func joinFallbackErrors(errs []error) error {
	for _, target := range []error{provider.ErrRateLimited, provider.ErrAuthFailed} {
		for _, err := range errs {
			if errors.Is(err, target) {
				return err
			}
		}
	}
	return fmt.Errorf("%w: %w", provider.ErrUnavailable, errors.Join(errs...))
}

// get faz uma requisição GET com pacing e proxy. Diferente do gtx, um 429 aqui não
// coloca o IP direto em cooldown (o Python também não o faz nos fallbacks).
func (g *GoogleTranslator) get(ctx context.Context, rawURL string, params url.Values) ([]byte, error) {
	client, proxyURL := g.proxyManager.Client(10 * time.Second)
	if proxyURL == "" {
		g.rateLimitPacing()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL+"?"+params.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", UserAgent)

	fail := func(err error) ([]byte, error) {
		if proxyURL != "" && ctx.Err() == nil {
			g.proxyManager.ReportProxyFailure(proxyURL)
		}
		return nil, err
	}

	resp, err := client.Do(req)
	if err != nil {
		return fail(err)
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
		return fail(provider.ErrRateLimited)
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return fail(provider.ErrAuthFailed)
	case resp.StatusCode < 200 || resp.StatusCode > 299:
		return fail(fmt.Errorf("HTTP %d", resp.StatusCode))
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fail(err)
	}
	return body, nil
}

// translateWeb usa a página mobile translate.google.com/m, como o GoogleTranslator
// do deep_translator. Devolve "" quando o Google ecoa o texto sem traduzi-lo
// (alfanumérico vazio), o que leva ao próximo fallback.
func (g *GoogleTranslator) translateWeb(ctx context.Context, text, srcLang, tgtLang string) (string, error) {
	text = strings.TrimSpace(text)
	if utf8.RuneCountInString(text) > webMaxChars {
		return "", fmt.Errorf("texto excede %d caracteres para o fallback web", webMaxChars)
	}
	params := url.Values{}
	params.Set("sl", srcLang)
	params.Set("tl", tgtLang)
	params.Set("q", text)

	body, err := g.get(ctx, g.webEndpoint, params)
	if err != nil {
		return "", err
	}
	m := webResultRE.FindSubmatch(body)
	if m == nil {
		return "", errors.New("resultado não encontrado na página do Google")
	}
	translated := strings.TrimSpace(html.UnescapeString(htmlTagRE.ReplaceAllString(string(m[1]), "")))
	if translated == text && alnum(text) == "" {
		return "", nil
	}
	return translated, nil
}

func alnum(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			return r
		}
		return -1
	}, s)
}

// translateMyMemory traduz em pedaços de até 450 caracteres (limite da API),
// preservando o Delimiter. Pedaços que falham mantêm o texto original, como no
// Python; se nenhum pedaço traduzível for traduzido, devolve erro.
func (g *GoogleTranslator) translateMyMemory(ctx context.Context, text, srcLang, tgtLang string) (string, error) {
	src, tgt := myMemoryLang(srcLang), myMemoryLang(tgtLang)
	var attempted, succeeded int
	var errs []error

	translateChunk := func(chunk string) string {
		attempted++
		out, err := g.myMemoryRequest(ctx, chunk, src, tgt)
		if err != nil || out == "" {
			if err != nil {
				errs = append(errs, err)
			}
			return chunk
		}
		succeeded++
		return out
	}

	translateLong := func(chunk string) string {
		if strings.TrimSpace(chunk) == "" {
			return chunk
		}
		if utf8.RuneCountInString(chunk) <= myMemoryChunkChars {
			return translateChunk(chunk)
		}
		var parts []string
		for _, sub := range splitForMyMemory(chunk) {
			if strings.TrimSpace(sub) == "" {
				parts = append(parts, sub)
			} else {
				parts = append(parts, translateChunk(sub))
			}
		}
		return strings.Join(parts, "\n")
	}

	pieces := strings.Split(text, Delimiter)
	for i, p := range pieces {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		pieces[i] = translateLong(p)
	}
	if attempted > 0 && succeeded == 0 {
		return "", joinFallbackErrors(append(errs, errors.New("mymemory não traduziu nenhum trecho")))
	}
	return strings.Join(pieces, Delimiter), nil
}

func myMemoryLang(lang string) string {
	if mapped, ok := myMemoryLangMap[strings.ToLower(lang)]; ok {
		return mapped
	}
	return lang
}

// splitForMyMemory quebra o texto em pedaços de até 450 caracteres, preferindo
// a última quebra de linha da janela.
func splitForMyMemory(text string) []string {
	var chunks []string
	rem := []rune(text)
	for len(rem) > 0 {
		if len(rem) <= myMemoryChunkChars {
			chunks = append(chunks, string(rem))
			break
		}
		idx := -1
		for i := myMemoryChunkChars - 1; i > 0; i-- {
			if rem[i] == '\n' {
				idx = i
				break
			}
		}
		if idx > 0 {
			chunks = append(chunks, string(rem[:idx]))
			rem = rem[idx+1:]
		} else {
			chunks = append(chunks, string(rem[:myMemoryChunkChars]))
			rem = rem[myMemoryChunkChars:]
		}
	}
	return chunks
}

func (g *GoogleTranslator) myMemoryRequest(ctx context.Context, chunk, src, tgt string) (string, error) {
	chunk = strings.TrimSpace(chunk)
	params := url.Values{}
	params.Set("q", chunk)
	params.Set("langpair", src+"|"+tgt)

	body, err := g.get(ctx, g.myMemoryURL, params)
	if err != nil {
		return "", err
	}
	var payload struct {
		ResponseData struct {
			TranslatedText string `json:"translatedText"`
		} `json:"responseData"`
		Matches []struct {
			Translation string `json:"translation"`
		} `json:"matches"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", fmt.Errorf("resposta inválida do mymemory: %w", err)
	}
	if payload.ResponseData.TranslatedText != "" {
		return payload.ResponseData.TranslatedText, nil
	}
	if len(payload.Matches) > 0 {
		return payload.Matches[0].Translation, nil
	}
	return "", nil
}
