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

// PhaseType define a etapa atual do ciclo de vida da tradução no Stepper.
type PhaseType string

const (
	PhasePrescan     PhaseType = "prescan"     // Varredura de arquivos e extração
	PhaseEstimating  PhaseType = "estimating"  // Cálculo de cache, custo e tempo pré-voo
	PhaseTranslating PhaseType = "translating" // Tradução em lote ativa
	PhaseReviewing   PhaseType = "reviewing"   // Revisão interativa (job pausado)
	PhaseCommitting  PhaseType = "committing"  // Salvando arquivos traduzidos no jogo
	PhaseFinished    PhaseType = "finished"    // Concluído
)

// EventType identifica a taxonomia de cada evento emitido pelo motor.
type EventType string

const (
	EventPhaseChanged      EventType = "phase_changed"
	EventEstimate          EventType = "estimate"
	EventSegmentProgress   EventType = "segment_progress"
	EventFileStatus        EventType = "file_status"
	EventProviderThrottled EventType = "provider_throttled"
	EventReviewRequested   EventType = "review_requested"
	EventJobFinished       EventType = "job_finished"
	EventProgressLegacy    EventType = "progress_legacy"
)

// PhaseChangedEvent é emitido quando o motor transita entre as fases do stepper.
type PhaseChangedEvent struct {
	Phase   PhaseType `json:"phase"`
	Message string    `json:"message"`
}

// EstimateEvent resume os dados pré-voo para o painel de estimativa do Figma.
type EstimateEvent struct {
	FilesTotal        int           `json:"files_total"`
	FilesToProcess    int           `json:"files_to_process"`
	SegmentsTotal     int           `json:"segments_total"`
	SegmentsCached    int           `json:"segments_cached"`
	EstimatedDuration time.Duration `json:"estimated_duration"`
	BackupPath        string        `json:"backup_path"`
}

// SegmentProgressEvent reporta o avanço global por segmento.
type SegmentProgressEvent struct {
	Done          int           `json:"done"`
	Cached        int           `json:"cached"`
	NewTranslated int           `json:"new_translated"`
	Total         int           `json:"total"`
	ETA           time.Duration `json:"eta"`
}

// FileStatusEvent atualiza a linha de um arquivo na tabela.
type FileStatusEvent struct {
	File    string     `json:"file"`
	Status  StatusType `json:"status"`
	Message string     `json:"message"`
	Current int        `json:"current"`
	Total   int        `json:"total"`
}

// ProviderThrottledEvent sinaliza HTTP 429 ou rate-limit com sugestão de proxy.
type ProviderThrottledEvent struct {
	Provider        string `json:"provider"`
	CooldownSeconds int    `json:"cooldown_seconds"`
	CanEnableProxy  bool   `json:"can_enable_proxy"`
	Message         string `json:"message"`
}

// ReviewRequestedEvent indica que a tradução foi pausada para inspeção opcional.
type ReviewRequestedEvent struct {
	TotalSegments       int `json:"total_segments"`
	NeedsAttentionCount int `json:"needs_attention_count"`
}

// JobFinishedEvent resume o encerramento da tradução para a tela de Resultado.
type JobFinishedEvent struct {
	Duration       time.Duration `json:"duration"`
	FilesComplete  int           `json:"files_complete"`
	FilesPartial   int           `json:"files_partial"`
	FilesError     int           `json:"files_error"`
	SegmentsTotal  int           `json:"segments_total"`
	SegmentsCached int           `json:"segments_cached"`
	SegmentsNew    int           `json:"segments_new"`
	SegmentsEdited int           `json:"segments_edited"`
	BackupPath     string        `json:"backup_path"`
	MetaPath       string        `json:"meta_path"`
	ExitCode       int           `json:"exit_code"`
}

// ProgressEvent carrega dados legados sobre o ciclo de vida.
type ProgressEvent struct {
	File      string     `json:"file"`
	Status    StatusType `json:"status"`
	Message   string     `json:"message"`
	Current   int        `json:"current"`
	Total     int        `json:"total"`
	Timestamp time.Time  `json:"timestamp"`
}

// Envelope envelopa qualquer evento tipado com timestamp e tipo.
type Envelope struct {
	Type      EventType `json:"type"`
	Data      any       `json:"data"`
	Timestamp time.Time `json:"timestamp"`
}

// Bus define o contrato do barramento de eventos desacoplado entre motor e UIs.
type Bus interface {
	Publish(event ProgressEvent)
	PublishEvent(eventType EventType, data any)
	Subscribe(bufferSize int) <-chan ProgressEvent
	SubscribeEvents(bufferSize int) <-chan Envelope
	Unsubscribe(ch <-chan ProgressEvent)
	UnsubscribeEvents(ch <-chan Envelope)
	Close()
}

type memoryBus struct {
	mu            sync.RWMutex
	subscribers   map[chan ProgressEvent]struct{}
	evSubscribers map[chan Envelope]struct{}
	closed        bool
}

// NewBus inicializa um novo barramento de eventos concorrente.
func NewBus() Bus {
	return &memoryBus{
		subscribers:   make(map[chan ProgressEvent]struct{}),
		evSubscribers: make(map[chan Envelope]struct{}),
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
		}
	}
}

func (b *memoryBus) PublishEvent(eventType EventType, data any) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if b.closed {
		return
	}

	env := Envelope{
		Type:      eventType,
		Data:      data,
		Timestamp: time.Now(),
	}

	for ch := range b.evSubscribers {
		select {
		case ch <- env:
		default:
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

func (b *memoryBus) SubscribeEvents(bufferSize int) <-chan Envelope {
	b.mu.Lock()
	defer b.mu.Unlock()

	if bufferSize <= 0 {
		bufferSize = 64
	}

	ch := make(chan Envelope, bufferSize)
	if b.closed {
		close(ch)
		return ch
	}

	b.evSubscribers[ch] = struct{}{}
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

func (b *memoryBus) UnsubscribeEvents(ch <-chan Envelope) {
	b.mu.Lock()
	defer b.mu.Unlock()

	for sub := range b.evSubscribers {
		if sub == ch {
			delete(b.evSubscribers, sub)
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

	for ch := range b.evSubscribers {
		close(ch)
	}
	b.evSubscribers = make(map[chan Envelope]struct{})
}
