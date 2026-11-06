package proxy

import (
	"net/http"
	"net/url"
	"slices"
	"sync"
	"time"
)

const (
	directCooldownBase = 30 * time.Second
	directCooldownMax  = 5 * time.Minute
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
}

// Client devolve o cliente HTTP para a próxima requisição e o proxy escolhido
// (vazio quando a conexão é direta), para que o chamador reporte o resultado.
func (m *Manager) Client(timeout time.Duration) (*http.Client, string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.enabled || !m.directBlockedLocked() || len(m.proxies) == 0 {
		return &http.Client{Timeout: timeout, Transport: m.direct}, ""
	}

	proxyURL := m.proxies[m.proxyIndex%len(m.proxies)]
	m.proxyIndex++
	return &http.Client{Timeout: timeout, Transport: m.transports[proxyURL]}, proxyURL
}
