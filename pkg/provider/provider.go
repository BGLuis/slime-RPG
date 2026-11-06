package provider

import (
	"context"
	"errors"
)

var (
	ErrRateLimited        = errors.New("provider rate limit exceeded (HTTP 429)")
	ErrAuthFailed         = errors.New("provider authentication failed (HTTP 401/403)")
	ErrUnavailable        = errors.New("provider service temporarily unavailable")
	ErrCircuitBreakerOpen = errors.New("circuit breaker is open due to consecutive failures")
)

// Provider define o contrato comum para todos os serviços de tradução externa e local.
type Provider interface {
	// ID retorna o identificador único do provedor (ex: "google", "deepl", "ollama").
	ID() string

	// TranslateBatch traduz um lote de strings de forma concorrente ou nativa.
	TranslateBatch(ctx context.Context, texts []string, srcLang, tgtLang string) ([]string, error)
}

// Capabilities descreve os limites de um lote enviado ao provedor.
type Capabilities struct {
	MaxChars    int // soma máxima de caracteres dos textos de um lote
	MaxItems    int // máximo de textos por lote; 0 = sem limite
	Concurrency int // lotes enviados simultaneamente
}

// DefaultCapabilities é usado por provedores que não declaram os próprios limites.
var DefaultCapabilities = Capabilities{MaxChars: 5000, Concurrency: 4}

// CapabilitiesOf devolve os limites declarados pelo provedor ou os padrões.
func CapabilitiesOf(p Provider) Capabilities {
	c, ok := p.(interface{ Capabilities() Capabilities })
	if !ok {
		return DefaultCapabilities
	}
	caps := c.Capabilities()
	if caps.MaxChars <= 0 {
		caps.MaxChars = DefaultCapabilities.MaxChars
	}
	if caps.Concurrency <= 0 {
		caps.Concurrency = DefaultCapabilities.Concurrency
	}
	return caps
}
