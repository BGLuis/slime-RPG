package proxy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"
)

type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time          { return c.t }
func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }

func newTestManager(enabled bool, proxies ...string) (*Manager, *fakeClock) {
	clock := &fakeClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	m := NewManager(enabled, proxies)
	m.now = clock.now
	return m, clock
}

func TestDirectConnectionUntilBlocked(t *testing.T) {
	m, _ := newTestManager(true, "http://1.1.1.1:8080")
	if _, p := m.Client(time.Second); p != "" {
		t.Errorf("sem bloqueio o IP direto deve ser usado mesmo com pool habilitado, obteve proxy %q", p)
	}
}

func TestRotatesProxiesWhileBlockedAndEvictsFailures(t *testing.T) {
	m, _ := newTestManager(true, "http://1.1.1.1:8080", "http://2.2.2.2:8080")
	m.ReportDirectRateLimit()

	_, p1 := m.Client(time.Second)
	_, p2 := m.Client(time.Second)
	if p1 != "http://1.1.1.1:8080" || p2 != "http://2.2.2.2:8080" {
		t.Errorf("rotação round-robin esperada, obteve %q e %q", p1, p2)
	}

	m.ReportProxyFailure(p1)
	if m.PoolSize() != 1 {
		t.Errorf("proxy que falhou deve sair do pool, restam %d", m.PoolSize())
	}
	if _, p := m.Client(time.Second); p != "http://2.2.2.2:8080" {
		t.Errorf("só o proxy saudável deve ser usado, obteve %q", p)
	}
}

func TestDisabledPoolNeverUsesProxies(t *testing.T) {
	m, _ := newTestManager(false, "http://1.1.1.1:8080")
	m.ReportDirectRateLimit()
	if _, p := m.Client(time.Second); p != "" {
		t.Errorf("pool desabilitado não pode usar proxy, obteve %q", p)
	}
	m.SetEnabled(true)
	if _, p := m.Client(time.Second); p == "" {
		t.Error("após habilitar, o bloqueio ativo deve usar o pool")
	}
}

func TestDirectBlockExpiresAfterCooldown(t *testing.T) {
	m, clock := newTestManager(true, "http://1.1.1.1:8080")
	m.ReportDirectRateLimit()
	if !m.DirectBlocked() {
		t.Fatal("429 deveria bloquear o IP direto")
	}
	clock.advance(directCooldownBase + time.Second)
	if m.DirectBlocked() {
		t.Error("bloqueio deveria expirar após o cooldown")
	}
	if _, p := m.Client(time.Second); p != "" {
		t.Errorf("após o cooldown o IP direto volta a ser usado, obteve %q", p)
	}
}

func TestCooldownGrowsOnRepeatedBlocksAndResetsOnSuccess(t *testing.T) {
	m, clock := newTestManager(true)
	remaining := func() time.Duration { return m.unblockAt.Sub(clock.now()) }

	m.ReportDirectRateLimit()
	first := remaining()
	clock.advance(first)
	m.ReportDirectRateLimit()
	if second := remaining(); second <= first {
		t.Errorf("cooldown deveria crescer: %v -> %v", first, second)
	}

	for range 20 {
		m.ReportDirectRateLimit()
	}
	if got := remaining(); got > directCooldownMax {
		t.Errorf("cooldown não pode passar do teto: %v", got)
	}

	m.ReportDirectSuccess()
	m.ReportDirectRateLimit()
	if got := remaining(); got != first {
		t.Errorf("após sucesso o cooldown volta à base: %v, esperava %v", got, first)
	}
}

func TestIgnoresInvalidAndDuplicateProxies(t *testing.T) {
	m, _ := newTestManager(true, "", "http://1.1.1.1:8080", "http://1.1.1.1:8080", "::not-a-url")
	if m.PoolSize() != 1 {
		t.Errorf("esperava 1 proxy válido, obteve %d", m.PoolSize())
	}
}

func TestTransportsAreReusedAndVerifyTLS(t *testing.T) {
	m, _ := newTestManager(true, "http://1.1.1.1:8080")
	c1, _ := m.Client(time.Second)
	c2, _ := m.Client(time.Second)
	if c1.Transport != c2.Transport {
		t.Error("requisições diretas devem compartilhar o mesmo transport")
	}

	m.ReportDirectRateLimit()
	c3, _ := m.Client(time.Second)
	c4, _ := m.Client(time.Second)
	if c3.Transport != c4.Transport {
		t.Error("requisições pelo mesmo proxy devem compartilhar o transport")
	}
	for _, tr := range m.transports {
		if tr.TLSClientConfig != nil && tr.TLSClientConfig.InsecureSkipVerify {
			t.Error("proxies públicos não podem desativar a verificação TLS")
		}
	}
}

// Um 429 no IP direto com o pool vazio dispara a busca de proxies, e a próxima
// requisição espera por ela em vez de insistir no IP bloqueado.
func TestRateLimitRefillsPoolAndClientWaitsForIt(t *testing.T) {
	m, _ := newTestManager(true)
	m.waitStep = time.Millisecond
	release := make(chan struct{})
	m.SetFetcher(func(context.Context) []string {
		<-release
		return []string{"http://9.9.9.9:3128"}
	})
	if !m.CanRotate() {
		t.Fatal("pool habilitado com fonte de proxies deveria poder rotacionar")
	}

	m.ReportDirectRateLimit()
	go func() {
		time.Sleep(10 * time.Millisecond)
		close(release)
	}()
	if _, p := m.Client(time.Second); p != "http://9.9.9.9:3128" {
		t.Errorf("esperava o proxy recém-validado, obteve %q", p)
	}
}

func TestDisabledPoolNeverFetches(t *testing.T) {
	m, _ := newTestManager(false)
	called := false
	m.SetFetcher(func(context.Context) []string { called = true; return nil })
	m.ReportDirectRateLimit()
	time.Sleep(5 * time.Millisecond)
	if called || m.CanRotate() {
		t.Error("pool desabilitado não pode buscar proxies")
	}
}

func TestParseListNormalizesEntries(t *testing.T) {
	got := parseList(strings.NewReader("# comentário\n1.2.3.4:80\n\n<html>\nhttp://5.6.7.8:8080\n1.2.3.4:80\nlixo\n"))
	want := []string{"http://1.2.3.4:80", "http://5.6.7.8:8080"}
	if !slices.Equal(got, want) {
		t.Errorf("parseList = %q, esperava %q", got, want)
	}
}

// Só passam proxies que de fato encaminham ao endpoint e devolvem o JSON do Google.
func TestValidateKeepsOnlyWorkingProxies(t *testing.T) {
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[[["teste"]]]`))
	}))
	defer good.Close()
	blocked := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "blocked", http.StatusTooManyRequests)
	}))
	defer blocked.Close()

	got := validate(context.Background(), []string{blocked.URL, good.URL, "http://127.0.0.1:1"}, "http://translate.invalid/translate_a/single")
	if !slices.Equal(got, []string{good.URL}) {
		t.Errorf("validate = %q, esperava só %q", got, good.URL)
	}
}
