package event

import (
	"sync"
	"time"
)

// StatusType define o estado de processamento de um arquivo ou lote.
type StatusType string

const (
	StatusWaiting    StatusType = "waiting" // Aguardando início ou vaga na fila
	StatusProcessing StatusType = "process" // Em processamento ativo
	StatusCached     StatusType = "cached"  // Resolvido integralmente pela Memória de Tradução
	StatusSuccess    StatusType = "success" // Concluído com êxito
	StatusPartial    StatusType = "partial" // Concluído parcialmente
	StatusError      StatusType = "error"   // Erro durante o processamento
	StatusIgnore     StatusType = "ignore"  // Ignorado deliberadamente (sem textos ou protegido)
)

// ProgressEvent carrega dados em tempo real sobre o ciclo de vida da extração e tradução.
type ProgressEvent struct {
	File      string     `json:"file"`
	Status    StatusType `json:"status"`
	Message   string     `json:"message"`
	Current   int        `json:"current"`
	Total     int        `json:"total"`
	Timestamp time.Time  `json:"timestamp"`
}

// Bus define o contrato do barramento de eventos desacoplado entre motor e UIs.
type Bus interface {
	// Publish envia um evento para todos os assinantes ativos.
	Publish(event ProgressEvent)

	// Subscribe cria e registra um novo canal de leitura para receber eventos.
	Subscribe(bufferSize int) <-chan ProgressEvent

	// Unsubscribe desregistra e fecha o canal especificado.
	Unsubscribe(ch <-chan ProgressEvent)

	// Close encerra o barramento e fecha todos os canais de assinantes ativos.
	Close()
}

type memoryBus struct {
	mu          sync.RWMutex
	subscribers map[chan ProgressEvent]struct{}
	closed      bool
}

// NewBus inicializa um novo barramento de eventos concorrente.
func NewBus() Bus {
	return &memoryBus{
		subscribers: make(map[chan ProgressEvent]struct{}),
	}
}

func (b *memoryBus) Publish(event ProgressEvent) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if b.closed {
		return
	}

	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}

	for ch := range b.subscribers {
		select {
		case ch <- event:
		default:
			// Não bloqueia os workers caso um assinante esteja ocupado
		}
	}
}

func (b *memoryBus) Subscribe(bufferSize int) <-chan ProgressEvent {
	b.mu.Lock()
	defer b.mu.Unlock()

	if bufferSize <= 0 {
		bufferSize = 64
	}

	ch := make(chan ProgressEvent, bufferSize)
	if b.closed {
		close(ch)
		return ch
	}

	b.subscribers[ch] = struct{}{}
	return ch
}

func (b *memoryBus) Unsubscribe(ch <-chan ProgressEvent) {
	b.mu.Lock()
	defer b.mu.Unlock()

	for sub := range b.subscribers {
		if sub == ch {
			delete(b.subscribers, sub)
			close(sub)
			return
		}
	}
}

func (b *memoryBus) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return
	}

	b.closed = true
	for ch := range b.subscribers {
		close(ch)
	}
	b.subscribers = make(map[chan ProgressEvent]struct{})
}
