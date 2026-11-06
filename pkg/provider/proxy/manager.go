package proxy

import (
	"crypto/tls"
	"net/http"
	"net/url"
	"sync"
	"time"
)

// Manager gerencia o pool de proxies rotativos e o cooldown do IP direto em caso de rate-limit (HTTP 429).
type Manager struct {
	mu             sync.RWMutex
	enabled        bool
	directBlocked  bool
	directCooldown time.Duration
	unblockAt      time.Time
	proxies        []string
	proxyIndex     int
	failedCounts   map[string]int
}

// NewManager cria uma nova instância do gerenciador de proxies.
func NewManager(enabled bool, initialProxies []string) *Manager {
	cleanProxies := make([]string, 0, len(initialProxies))
	for _, p := range initialProxies {
		if p != "" {
			cleanProxies = append(cleanProxies, p)
		}
	}

	return &Manager{
		enabled:        enabled,
		directCooldown: 30 * time.Second,
		proxies:        cleanProxies,
		failedCounts:   make(map[string]int),
	}
}

// SetEnabled habilita ou desabilita o uso de proxies rotativos.
func (m *Manager) SetEnabled(enabled bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.enabled = enabled
}

// IsEnabled verifica se o pool de proxies está ativado.
func (m *Manager) IsEnabled() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.enabled
}

// AddProxies adiciona novos proxies à lista.
func (m *Manager) AddProxies(newProxies ...string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, p := range newProxies {
		if p != "" {
			m.proxies = append(m.proxies, p)
		}
	}
}

// PoolSize retorna a quantidade de proxies ativos cadastrados.
func (m *Manager) PoolSize() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.proxies)
}

// ReportDirectRateLimit notifica que o IP direto recebeu HTTP 429 e entra em cooldown progressivo.
func (m *Manager) ReportDirectRateLimit() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.directBlocked = true
	m.unblockAt = time.Now().Add(m.directCooldown)

	// Backoff exponencial no cooldown (até 5 minutos)
	m.directCooldown *= 2
	if m.directCooldown > 5*time.Minute {
		m.directCooldown = 5 * time.Minute
	}
}

// ReportDirectSuccess notifica uma requisição bem-sucedida pelo IP direto, resetando o cooldown.
func (m *Manager) ReportDirectSuccess() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.directBlocked = false
	m.directCooldown = 30 * time.Second
}

// ReportProxyFailure registra a falha de um proxy específico para expurgá-lo se falhar repetidamente.
func (m *Manager) ReportProxyFailure(proxyURL string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.failedCounts[proxyURL]++
	if m.failedCounts[proxyURL] >= 3 {
		// Remove proxy instável da lista
		for i, p := range m.proxies {
			if p == proxyURL {
				m.proxies = append(m.proxies[:i], m.proxies[i+1:]...)
				break
			}
		}
	}
}

// GetClient retorna um http.Client configurado para conexão direta ou com proxy rotativo.
func (m *Manager) GetClient(timeout time.Duration) *http.Client {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Se o cooldown expirou, tenta o IP direto novamente
	if m.directBlocked && time.Now().After(m.unblockAt) {
		m.directBlocked = false
	}

	// Usa IP direto se permitido ou se não houver proxies configurados
	if (!m.directBlocked && !m.enabled) || len(m.proxies) == 0 {
		return &http.Client{
			Timeout: timeout,
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 20,
				IdleConnTimeout:     90 * time.Second,
			},
		}
	}

	// Seleciona o próximo proxy no formato Round-Robin
	proxyStr := m.proxies[m.proxyIndex%len(m.proxies)]
	m.proxyIndex++

	parsedURL, err := url.Parse(proxyStr)
	if err != nil {
		// Fallback para direto se url for inválida
		return &http.Client{Timeout: timeout}
	}

	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			Proxy:               http.ProxyURL(parsedURL),
			TLSClientConfig:     &tls.Config{InsecureSkipVerify: true},
			MaxIdleConns:        50,
			MaxIdleConnsPerHost: 10,
		},
	}
}
