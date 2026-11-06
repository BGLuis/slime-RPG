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
