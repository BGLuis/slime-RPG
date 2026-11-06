package proxy

import (
	"context"
	"net/http"
	"net/url"
	"slices"
	"sync"
	"time"
)

const (
	directCooldownBase = 30 * time.Second
	directCooldownMax  = 5 * time.Minute

	// minPool é o tamanho abaixo do qual o pool busca novos proxies.
	minPool = 3
	// maxWaitForProxy limita a espera por um proxy validado enquanto o IP direto
	// está bloqueado: insistir no IP recém-bloqueado só prolonga o bloqueio.
	maxWaitForProxy = 15 * time.Second
)

// Manager decide entre IP direto e pool de proxies rotativos. O pool só é usado
// quando está habilitado e o IP direto está em cooldown após um HTTP 429.
type Manager struct {
	mu          sync.Mutex
	enabled     bool
	blockStreak int
	unblockAt   time.Time
	proxies     []string
	proxyIndex  int
	now         func() time.Time

	fetch    func(ctx context.Context) []string
	fetching bool
	refilled chan struct{} // fechado ao fim de cada busca
	waitStep time.Duration

	// Transports são reaproveitados entre requisições: criar um por chamada
	// impede o reuso de conexões e acumula conexões ociosas abertas.
	direct     *http.Transport
	transports map[string]*http.Transport
}

// NewManager cria uma nova instância do gerenciador de proxies.
func NewManager(enabled bool, initialProxies []string) *Manager {
	m := &Manager{
		enabled:    enabled,
		now:        time.Now,
		waitStep:   250 * time.Millisecond,
		direct:     newTransport(nil),
		transports: make(map[string]*http.Transport),
	}
	m.AddProxies(initialProxies...)
	return m
}

func newTransport(proxyURL *url.URL) *http.Transport {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.MaxIdleConnsPerHost = 20
	if proxyURL != nil {
		t.Proxy = http.ProxyURL(proxyURL)
	}
	return t
}

// SetFetcher define de onde o pool busca proxies quando o IP direto é
// bloqueado. Sem fetcher o pool usa só os proxies adicionados manualmente.
func (m *Manager) SetFetcher(fetch func(ctx context.Context) []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.fetch = fetch
}

// UsePublicSources faz o pool buscar e validar proxies públicos, como a versão Python.
func (m *Manager) UsePublicSources() {
	m.SetFetcher(FetchPublicProxies)
}

// SetEnabled habilita ou desabilita o uso de proxies rotativos.
func (m *Manager) SetEnabled(enabled bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.enabled = enabled
}

// IsEnabled verifica se o pool de proxies está ativado.
func (m *Manager) IsEnabled() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.enabled
}

// AddProxies adiciona novos proxies ao pool, ignorando vazios e URLs inválidas.
func (m *Manager) AddProxies(newProxies ...string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, p := range newProxies {
		if p == "" || slices.Contains(m.proxies, p) {
			continue
		}
		parsed, err := url.Parse(p)
		if err != nil || parsed.Host == "" {
			continue
		}
		m.proxies = append(m.proxies, p)
		m.transports[p] = newTransport(parsed)
	}
}

// CanRotate indica se um bloqueio do IP direto pode ser contornado: o pool está
// habilitado e tem proxies ou uma fonte para buscá-los.
func (m *Manager) CanRotate() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.enabled && (len(m.proxies) > 0 || m.fetch != nil)
}

// PoolSize retorna a quantidade de proxies ativos cadastrados.
func (m *Manager) PoolSize() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.proxies)
}

// DirectBlocked indica se o IP direto ainda está em cooldown.
func (m *Manager) DirectBlocked() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.directBlockedLocked()
}

func (m *Manager) directBlockedLocked() bool {
	return m.now().Before(m.unblockAt)
}

// ReportDirectRateLimit registra um HTTP 429 no IP direto. O cooldown dobra a cada
// bloqueio consecutivo (30s, 60s, ... até 5min) e volta à base após um sucesso.
func (m *Manager) ReportDirectRateLimit() {
	m.mu.Lock()
	defer m.mu.Unlock()

	cooldown := directCooldownMax
	if m.blockStreak < 10 {
		cooldown = min(directCooldownBase<<m.blockStreak, directCooldownMax)
	}
	m.blockStreak++
	if until := m.now().Add(cooldown); until.After(m.unblockAt) {
		m.unblockAt = until
	}
	m.refillLocked()
}

// ReportDirectSuccess registra uma requisição bem-sucedida pelo IP direto.
func (m *Manager) ReportDirectSuccess() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.blockStreak = 0
	m.unblockAt = time.Time{}
}

// ReportProxyFailure remove do pool um proxy que falhou ou foi bloqueado: proxies
// públicos instáveis raramente se recuperam a tempo de valer uma nova tentativa.
func (m *Manager) ReportProxyFailure(proxyURL string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	idx := slices.Index(m.proxies, proxyURL)
	if idx < 0 {
		return
	}
	m.proxies = slices.Delete(m.proxies, idx, idx+1)
	if t, ok := m.transports[proxyURL]; ok {
		t.CloseIdleConnections()
		delete(m.transports, proxyURL)
	}
	m.refillLocked()
}

// refillLocked inicia, em segundo plano, a busca de proxies quando o pool está
// habilitado e abaixo do mínimo. Chamado com m.mu travado.
func (m *Manager) refillLocked() {
	if !m.enabled || m.fetch == nil || m.fetching || len(m.proxies) >= minPool {
		return
	}
	m.fetching = true
	done := make(chan struct{})
	m.refilled = done
	fetch := m.fetch
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		found := fetch(ctx)
		m.AddProxies(found...)
		m.mu.Lock()
		m.fetching = false
		m.mu.Unlock()
		close(done)
	}()
}

// Client devolve o cliente HTTP para a próxima requisição e o proxy escolhido
// (vazio quando a conexão é direta), para que o chamador reporte o resultado.
// Com o IP direto bloqueado e o pool vazio, espera até 15s por uma busca em
// andamento antes de voltar ao IP direto.
func (m *Manager) Client(timeout time.Duration) (*http.Client, string) {
	m.waitForPool()

	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.enabled || !m.directBlockedLocked() || len(m.proxies) == 0 {
		return &http.Client{Timeout: timeout, Transport: m.direct}, ""
	}

	proxyURL := m.proxies[m.proxyIndex%len(m.proxies)]
	m.proxyIndex++
	return &http.Client{Timeout: timeout, Transport: m.transports[proxyURL]}, proxyURL
}

func (m *Manager) waitForPool() {
	deadline := time.Now().Add(maxWaitForProxy)
	for {
		m.mu.Lock()
		waiting := m.enabled && m.fetching && len(m.proxies) == 0 && m.directBlockedLocked()
		done, step := m.refilled, m.waitStep
		m.mu.Unlock()
		if !waiting || time.Now().After(deadline) {
			return
		}
		select {
		case <-done:
			return
		case <-time.After(step):
		}
	}
}
