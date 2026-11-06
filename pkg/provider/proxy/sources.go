package proxy

import (
	"bufio"
	"context"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

// Listas públicas de proxies HTTP usadas pela versão Python.
var publicSources = []string{
	"https://raw.githubusercontent.com/monosans/proxy-list/main/proxies/http.txt",
	"https://api.proxyscrape.com/v2/?request=displayproxies&protocol=http&timeout=4000&country=all&ssl=all&anonymity=all",
	"https://raw.githubusercontent.com/TheSpeedX/SOCKS-List/master/http.txt",
}

const validationURL = "https://translate.googleapis.com/translate_a/single?client=gtx&sl=ja&tl=pt&dt=t&q=%E3%83%86%E3%82%B9%E3%83%88"

// FetchPublicProxies junta os proxies de proxies.txt (pasta atual) e das listas
// públicas e devolve só os que conseguem falar com o endpoint do Google.
func FetchPublicProxies(ctx context.Context) []string {
	candidates := readLines(localProxiesFile())
	if len(candidates) < 30 {
		client := &http.Client{Timeout: 4 * time.Second}
		for _, src := range publicSources {
			candidates = appendUnique(candidates, download(ctx, client, src)...)
			if len(candidates) >= 60 {
				break
			}
		}
	}
	if len(candidates) > 50 {
		candidates = candidates[:50]
	}
	return validate(ctx, candidates, validationURL)
}

func localProxiesFile() string { return "proxies.txt" }

func readLines(path string) []string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	return parseList(f)
}

func download(ctx context.Context, client *http.Client, src string) []string {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, src, nil)
	if err != nil {
		return nil
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	return parseList(io.LimitReader(resp.Body, 4<<20))
}

// parseList lê uma lista "host:porta" por linha, ignorando comentários e HTML.
func parseList(r io.Reader) []string {
	var out []string
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "<") || !strings.Contains(line, ":") {
			continue
		}
		if !strings.Contains(line, "://") {
			line = "http://" + line
		}
		out = appendUnique(out, line)
	}
	return out
}

func appendUnique(list []string, items ...string) []string {
	for _, it := range items {
		dup := false
		for _, existing := range list {
			if existing == it {
				dup = true
				break
			}
		}
		if !dup {
			list = append(list, it)
		}
	}
	return list
}

// validate testa os candidatos em paralelo (25 por vez) e mantém a ordem.
func validate(ctx context.Context, candidates []string, testURL string) []string {
	ok := make([]bool, len(candidates))
	sem := make(chan struct{}, 25)
	var wg sync.WaitGroup
	for i, c := range candidates {
		parsed, err := url.Parse(c)
		if err != nil || parsed.Host == "" {
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			ok[i] = probe(ctx, parsed, testURL)
		}()
	}
	wg.Wait()

	var working []string
	for i, c := range candidates {
		if ok[i] {
			working = append(working, c)
		}
	}
	return working
}

func probe(ctx context.Context, proxyURL *url.URL, testURL string) bool {
	client := &http.Client{Timeout: 5 * time.Second, Transport: newTransport(proxyURL)}
	defer client.CloseIdleConnections()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, testURL, nil)
	if err != nil {
		return false
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	head := make([]byte, 1)
	n, _ := resp.Body.Read(head)
	return resp.StatusCode == http.StatusOK && n == 1 && head[0] == '['
}

// EnabledByEnv aplica as variáveis da versão Python: o pool fica ativo por
// padrão, salvo DISABLE_PROXY_POOL verdadeiro ou USE_PROXY_POOL falso.
func EnabledByEnv() bool {
	isTrue := func(v string) bool {
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "true", "1", "yes":
			return true
		}
		return false
	}
	if isTrue(os.Getenv("DISABLE_PROXY_POOL")) {
		return false
	}
	if v := os.Getenv("USE_PROXY_POOL"); v != "" && !isTrue(v) {
		return false
	}
	return true
}
