// Package engine envia textos ao provedor de tradução com as garantias do
// TranslationEngine da versão Python: deduplicação, lotes dimensionados pelas
// capacidades do provedor, concorrência limitada, uma única camada de retry com
// backoff exponencial, circuit breaker e fallback item a item.
package engine

import (
	"context"
	"errors"
	"math/rand/v2"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/BGLuis/slime-rpg/pkg/provider"
)

// Options ajusta o comportamento do motor. Campos zerados usam os padrões do Python.
type Options struct {
	MaxRetries int           // tentativas por lote (padrão 5); também é o limiar do circuit breaker
	BaseDelay  time.Duration // atraso inicial do backoff (padrão 500ms)
	MaxDelay   time.Duration // teto do backoff (padrão 30s)

	// MemoryOnly devolve o texto original sem chamar o provedor (modo offline).
	MemoryOnly bool

	// CanBypassRateLimit informa se um HTTP 429 pode ser contornado, por exemplo
	// com o pool de proxies ativo. Sem isso o motor para de chamar o provedor, como
	// o Python faz quando o IP direto é bloqueado e não há proxies.
	CanBypassRateLimit func() bool

	// Wait bloqueia antes de cada lote enquanto o job estiver pausado.
	Wait func(ctx context.Context) error

	// OnProgress recebe a quantidade de textos únicos concluídos e o total.
	OnProgress func(done, total int)

	// OnRateLimited é chamado a cada HTTP 429 recebido.
	OnRateLimited func()
}

// Stats resume a última execução.
type Stats struct {
	Requests    int  // chamadas feitas ao provedor
	Failed      int  // textos devolvidos sem tradução após esgotar as tentativas
	RateLimited bool // o provedor bloqueou o IP e não havia como contornar
	CircuitOpen bool // falhas consecutivas desligaram o provedor
}

// Engine implementa pipeline.Translator sobre um provider.Provider.
type Engine struct {
	prov  provider.Provider
	opts  Options
	sleep func(ctx context.Context, d time.Duration) error

	consecutiveFailures atomic.Int32
	circuitOpen         atomic.Bool
	rateLimited         atomic.Bool
	requests            atomic.Int32
	failed              atomic.Int32
}

// New cria um motor para o provedor informado.
func New(p provider.Provider, opts Options) *Engine {
	if opts.MaxRetries <= 0 {
		opts.MaxRetries = 5
	}
	if opts.BaseDelay <= 0 {
		opts.BaseDelay = 500 * time.Millisecond
	}
	if opts.MaxDelay <= 0 {
		opts.MaxDelay = 30 * time.Second
	}
	return &Engine{prov: p, opts: opts, sleep: sleepCtx}
}

// Stats devolve os contadores acumulados.
func (e *Engine) Stats() Stats {
	return Stats{
		Requests:    int(e.requests.Load()),
		Failed:      int(e.failed.Load()),
		RateLimited: e.rateLimited.Load(),
		CircuitOpen: e.circuitOpen.Load(),
	}
}

// TranslateBatch traduz os textos preservando a ordem. Textos repetidos são
// enviados uma vez e textos em branco não são enviados. Um lote que falha devolve
// os textos originais: só autenticação inválida e cancelamento viram erro, pois
// nenhuma tentativa posterior teria sucesso.
func (e *Engine) TranslateBatch(ctx context.Context, texts []string, srcLang, tgtLang string) ([]string, error) {
	results := make([]string, len(texts))
	copy(results, texts)

	positions := make(map[string][]int)
	var unique []string
	for i, text := range texts {
		if strings.TrimSpace(text) == "" {
			continue
		}
		if _, seen := positions[text]; !seen {
			unique = append(unique, text)
		}
		positions[text] = append(positions[text], i)
	}
	if len(unique) == 0 || e.opts.MemoryOnly || e.prov == nil {
		return results, nil
	}

	batches := makeBatches(unique, provider.CapabilitiesOf(e.prov))
	workers := min(provider.CapabilitiesOf(e.prov).Concurrency, len(batches))

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var (
		mu       sync.Mutex
		done     int
		fatalErr error
		next     = make(chan []string)
		wg       sync.WaitGroup
	)
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for batch := range next {
				out, err := e.runBatch(ctx, batch, srcLang, tgtLang)
				mu.Lock()
				if err != nil {
					if fatalErr == nil {
						fatalErr = err
					}
					cancel()
					mu.Unlock()
					continue
				}
				for i, src := range batch {
					for _, pos := range positions[src] {
						results[pos] = out[i]
					}
				}
				done += len(batch)
				if e.opts.OnProgress != nil {
					e.opts.OnProgress(done, len(unique))
				}
				mu.Unlock()
			}
		}()
	}

feed:
	for _, b := range batches {
		select {
		case next <- b:
		case <-ctx.Done():
			break feed
		}
	}
	close(next)
	wg.Wait()

	if fatalErr != nil {
		return nil, fatalErr
	}
	return results, nil
}

// makeBatches agrupa textos até o limite de caracteres e de itens do provedor,
// contando o tamanho do delimitador que o provedor usa para uni-los.
func makeBatches(texts []string, caps provider.Capabilities) [][]string {
	const delimiterLen = len("\n<span> </span>\n")
	var (
		batches [][]string
		current []string
		size    int
	)
	for _, text := range texts {
		n := len([]rune(text)) + delimiterLen
		itemLimit := caps.MaxItems > 0 && len(current) >= caps.MaxItems
		charLimit := len(current) > 0 && size+n >= caps.MaxChars
		if itemLimit || charLimit {
			batches = append(batches, current)
			current, size = nil, 0
		}
		current = append(current, text)
		size += n
	}
	if len(current) > 0 {
		batches = append(batches, current)
	}
	return batches
}

// runBatch tenta o lote até MaxRetries vezes. Erros transitórios recebem backoff
// exponencial com jitter; resposta com contagem divergente cai para o fallback
// item a item; esgotadas as tentativas, devolve os textos originais.
func (e *Engine) runBatch(ctx context.Context, batch []string, srcLang, tgtLang string) ([]string, error) {
	for attempt := range e.opts.MaxRetries {
		if e.unavailable() {
			break
		}
		if e.opts.Wait != nil {
			if err := e.opts.Wait(ctx); err != nil {
				return nil, err
			}
		}

		e.requests.Add(1)
		out, err := e.prov.TranslateBatch(ctx, batch, srcLang, tgtLang)
		if err == nil {
			e.consecutiveFailures.Store(0)
			if len(out) == len(batch) {
				return out, nil
			}
			return e.perItem(ctx, batch, srcLang, tgtLang)
		}

		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		if errors.Is(err, provider.ErrAuthFailed) {
			return nil, err
		}
		e.recordFailure()
		if errors.Is(err, provider.ErrRateLimited) {
			if e.opts.OnRateLimited != nil {
				e.opts.OnRateLimited()
			}
			if e.opts.CanBypassRateLimit == nil || !e.opts.CanBypassRateLimit() {
				e.rateLimited.Store(true)
				break
			}
		}

		if attempt < e.opts.MaxRetries-1 {
			backoff := min(e.opts.MaxDelay, e.opts.BaseDelay<<attempt) + time.Duration(rand.Int64N(int64(50*time.Millisecond)))
			if err := e.sleep(ctx, backoff); err != nil {
				return nil, err
			}
		}
	}

	e.failed.Add(int32(len(batch)))
	return batch, nil
}

// perItem traduz um texto por requisição quando o provedor devolve um lote com
// tamanho diferente do enviado, o que tornaria impossível alinhar as traduções.
func (e *Engine) perItem(ctx context.Context, batch []string, srcLang, tgtLang string) ([]string, error) {
	out := make([]string, len(batch))
	for i, text := range batch {
		out[i] = text
		if e.unavailable() {
			e.failed.Add(1)
			continue
		}
		e.requests.Add(1)
		res, err := e.prov.TranslateBatch(ctx, []string{text}, srcLang, tgtLang)
		switch {
		case err == nil && len(res) == 1:
			out[i] = res[0]
		case ctx.Err() != nil:
			return nil, ctx.Err()
		case errors.Is(err, provider.ErrAuthFailed):
			return nil, err
		default:
			if errors.Is(err, provider.ErrRateLimited) && (e.opts.CanBypassRateLimit == nil || !e.opts.CanBypassRateLimit()) {
				e.rateLimited.Store(true)
			}
			e.failed.Add(1)
		}
	}
	return out, nil
}

func (e *Engine) recordFailure() {
	if int(e.consecutiveFailures.Add(1)) >= e.opts.MaxRetries {
		e.circuitOpen.Store(true)
	}
}

func (e *Engine) unavailable() bool {
	return e.circuitOpen.Load() || e.rateLimited.Load()
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
