package engine

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/BGLuis/slime-rpg/pkg/provider"
)

type fakeProvider struct {
	caps      provider.Capabilities
	translate func(texts []string) ([]string, error)
	delay     time.Duration

	mu     sync.Mutex
	calls  [][]string
	active atomic.Int32
	peak   atomic.Int32
}

func (f *fakeProvider) ID() string                          { return "fake" }
func (f *fakeProvider) Capabilities() provider.Capabilities { return f.caps }

func (f *fakeProvider) TranslateBatch(_ context.Context, texts []string, _, _ string) ([]string, error) {
	n := f.active.Add(1)
	defer f.active.Add(-1)
	for {
		p := f.peak.Load()
		if n <= p || f.peak.CompareAndSwap(p, n) {
			break
		}
	}
	f.mu.Lock()
	f.calls = append(f.calls, slices.Clone(texts))
	f.mu.Unlock()
	time.Sleep(f.delay)
	return f.translate(texts)
}

func (f *fakeProvider) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func prefix(texts []string) ([]string, error) {
	out := make([]string, len(texts))
	for i, t := range texts {
		out[i] = "pt:" + t
	}
	return out, nil
}

func newTestEngine(p provider.Provider, opts Options) *Engine {
	e := New(p, opts)
	e.sleep = func(context.Context, time.Duration) error { return nil }
	return e
}

func TestDeduplicatesAndSkipsBlankTexts(t *testing.T) {
	p := &fakeProvider{caps: provider.Capabilities{MaxChars: 5000, Concurrency: 2}, translate: prefix}
	e := newTestEngine(p, Options{})

	got, err := e.TranslateBatch(context.Background(), []string{"a", " ", "b", "a"}, "ja", "pt")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"pt:a", " ", "pt:b", "pt:a"}; !slices.Equal(got, want) {
		t.Errorf("resultado = %q, esperava %q", got, want)
	}
	if len(p.calls) != 1 || !slices.Equal(p.calls[0], []string{"a", "b"}) {
		t.Errorf("o provedor deveria receber cada texto uma única vez, recebeu %q", p.calls)
	}
}

func TestBatchesRespectProviderLimits(t *testing.T) {
	texts := make([]string, 7)
	for i := range texts {
		texts[i] = fmt.Sprintf("t%d", i)
	}
	if got := makeBatches(texts, provider.Capabilities{MaxChars: 5000, MaxItems: 3}); len(got) != 3 {
		t.Errorf("MaxItems=3 com 7 textos deveria gerar 3 lotes, gerou %d", len(got))
	}

	long := strings.Repeat("x", 3000)
	got := makeBatches([]string{long, long, "c"}, provider.Capabilities{MaxChars: 5000})
	if len(got) != 2 || len(got[0]) != 1 {
		t.Errorf("lotes devem fechar antes de passar de MaxChars: %d lotes", len(got))
	}

	huge := strings.Repeat("y", 9000)
	if got := makeBatches([]string{huge}, provider.Capabilities{MaxChars: 5000}); len(got) != 1 {
		t.Error("um texto maior que o limite ainda precisa ser enviado sozinho")
	}
}

func TestPeakConcurrencyNeverExceedsProviderLimit(t *testing.T) {
	p := &fakeProvider{caps: provider.Capabilities{MaxChars: 5000, MaxItems: 1, Concurrency: 3}, translate: prefix, delay: 5 * time.Millisecond}
	e := newTestEngine(p, Options{})

	texts := make([]string, 30)
	for i := range texts {
		texts[i] = fmt.Sprintf("t%d", i)
	}
	if _, err := e.TranslateBatch(context.Background(), texts, "ja", "pt"); err != nil {
		t.Fatal(err)
	}
	if peak := p.peak.Load(); peak > 3 {
		t.Errorf("pico de concorrência %d acima do limite 3", peak)
	}
	if p.peak.Load() < 2 {
		t.Error("os lotes deveriam ser enviados em paralelo")
	}
}

// Um provedor sempre em falha não pode multiplicar as chamadas: o circuit breaker
// abre após MaxRetries falhas consecutivas e os textos voltam sem tradução.
func TestAlwaysFailingProviderOpensCircuit(t *testing.T) {
	p := &fakeProvider{
		caps:      provider.Capabilities{MaxChars: 5000, MaxItems: 1, Concurrency: 1},
		translate: func([]string) ([]string, error) { return nil, provider.ErrUnavailable },
	}
	e := newTestEngine(p, Options{})

	texts := []string{"a", "b", "c", "d"}
	got, err := e.TranslateBatch(context.Background(), texts, "ja", "pt")
	if err != nil {
		t.Fatalf("falha transitória não deve virar erro: %v", err)
	}
	if !slices.Equal(got, texts) {
		t.Errorf("textos sem tradução devem voltar como estavam: %q", got)
	}
	if calls := p.callCount(); calls > 5 {
		t.Errorf("esperava no máximo 5 chamadas, foram %d", calls)
	}
	if s := e.Stats(); !s.CircuitOpen || s.Failed != 4 {
		t.Errorf("stats = %+v", s)
	}
}

func TestTransientErrorIsRetried(t *testing.T) {
	var attempts atomic.Int32
	p := &fakeProvider{
		caps: provider.Capabilities{MaxChars: 5000, Concurrency: 1},
		translate: func(texts []string) ([]string, error) {
			if attempts.Add(1) < 3 {
				return nil, provider.ErrUnavailable
			}
			return prefix(texts)
		},
	}
	e := newTestEngine(p, Options{})

	got, err := e.TranslateBatch(context.Background(), []string{"a"}, "ja", "pt")
	if err != nil || got[0] != "pt:a" {
		t.Fatalf("got %q, %v", got, err)
	}
	if attempts.Load() != 3 {
		t.Errorf("esperava 3 tentativas, foram %d", attempts.Load())
	}
}

func TestAuthErrorStopsTheRun(t *testing.T) {
	p := &fakeProvider{
		caps:      provider.Capabilities{MaxChars: 5000, Concurrency: 1},
		translate: func([]string) ([]string, error) { return nil, provider.ErrAuthFailed },
	}
	e := newTestEngine(p, Options{})

	if _, err := e.TranslateBatch(context.Background(), []string{"a"}, "ja", "pt"); !errors.Is(err, provider.ErrAuthFailed) {
		t.Fatalf("esperava ErrAuthFailed, obteve %v", err)
	}
	if p.callCount() != 1 {
		t.Errorf("credencial inválida não deve ser retentada: %d chamadas", p.callCount())
	}
}

// Sem pool de proxies, insistir após um 429 só prolonga o bloqueio do IP.
func TestRateLimitWithoutBypassStopsCallingProvider(t *testing.T) {
	p := &fakeProvider{
		caps:      provider.Capabilities{MaxChars: 5000, MaxItems: 1, Concurrency: 1},
		translate: func([]string) ([]string, error) { return nil, provider.ErrRateLimited },
	}
	var throttled atomic.Int32
	e := newTestEngine(p, Options{OnRateLimited: func() { throttled.Add(1) }})

	got, err := e.TranslateBatch(context.Background(), []string{"a", "b", "c"}, "ja", "pt")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, []string{"a", "b", "c"}) {
		t.Errorf("got %q", got)
	}
	if p.callCount() != 1 || throttled.Load() != 1 {
		t.Errorf("esperava parar após o primeiro 429: %d chamadas", p.callCount())
	}
	if !e.Stats().RateLimited {
		t.Error("Stats.RateLimited deveria ser true")
	}
}

func TestRateLimitWithBypassKeepsRetrying(t *testing.T) {
	var attempts atomic.Int32
	p := &fakeProvider{
		caps: provider.Capabilities{MaxChars: 5000, Concurrency: 1},
		translate: func(texts []string) ([]string, error) {
			if attempts.Add(1) == 1 {
				return nil, provider.ErrRateLimited
			}
			return prefix(texts)
		},
	}
	e := newTestEngine(p, Options{CanBypassRateLimit: func() bool { return true }})

	got, err := e.TranslateBatch(context.Background(), []string{"a"}, "ja", "pt")
	if err != nil || got[0] != "pt:a" {
		t.Fatalf("com proxies ativos o 429 deveria ser retentado: %q, %v", got, err)
	}
}

func TestCountMismatchFallsBackPerItem(t *testing.T) {
	p := &fakeProvider{
		caps: provider.Capabilities{MaxChars: 5000, Concurrency: 1},
		translate: func(texts []string) ([]string, error) {
			if len(texts) > 1 {
				return []string{"merged"}, nil
			}
			return prefix(texts)
		},
	}
	e := newTestEngine(p, Options{})

	got, err := e.TranslateBatch(context.Background(), []string{"a", "b"}, "ja", "pt")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"pt:a", "pt:b"}; !slices.Equal(got, want) {
		t.Errorf("got %q, esperava %q", got, want)
	}
}

func TestMemoryOnlyNeverCallsProvider(t *testing.T) {
	p := &fakeProvider{caps: provider.DefaultCapabilities, translate: prefix}
	e := newTestEngine(p, Options{MemoryOnly: true})

	got, _ := e.TranslateBatch(context.Background(), []string{"a"}, "ja", "pt")
	if got[0] != "a" || p.callCount() != 0 {
		t.Errorf("modo offline chamou o provedor: %q, %d chamadas", got, p.callCount())
	}
}

func TestProgressReportsUniqueTexts(t *testing.T) {
	p := &fakeProvider{caps: provider.Capabilities{MaxChars: 5000, MaxItems: 1, Concurrency: 2}, translate: prefix}
	var last atomic.Int64
	e := newTestEngine(p, Options{OnProgress: func(done, total int) {
		if total != 2 {
			t.Errorf("total deveria contar textos únicos: %d", total)
		}
		last.Store(int64(done))
	}})

	if _, err := e.TranslateBatch(context.Background(), []string{"a", "b", "a"}, "ja", "pt"); err != nil {
		t.Fatal(err)
	}
	if last.Load() != 2 {
		t.Errorf("progresso final = %d, esperava 2", last.Load())
	}
}

func TestCancelledContextReturnsError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	p := &fakeProvider{
		caps: provider.Capabilities{MaxChars: 5000, Concurrency: 1},
		translate: func([]string) ([]string, error) {
			cancel()
			return nil, context.Canceled
		},
	}
	e := newTestEngine(p, Options{})
	if _, err := e.TranslateBatch(ctx, []string{"a"}, "ja", "pt"); !errors.Is(err, context.Canceled) {
		t.Errorf("esperava context.Canceled, obteve %v", err)
	}
}
