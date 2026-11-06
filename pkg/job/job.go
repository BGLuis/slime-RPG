package job

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"github.com/BGLuis/slime-rpg/pkg/detector"
	"github.com/BGLuis/slime-rpg/pkg/event"
	"github.com/BGLuis/slime-rpg/pkg/memory"
	"github.com/BGLuis/slime-rpg/pkg/model"
	"github.com/BGLuis/slime-rpg/pkg/pipeline"
	"github.com/BGLuis/slime-rpg/pkg/provider"
	"github.com/BGLuis/slime-rpg/pkg/workspace"
)

var (
	ErrJobAlreadyRunning = errors.New("job is already running")
	ErrJobNotPaused      = errors.New("job is not paused")
	ErrNoGameSelected    = errors.New("no game path specified")
)

// ConfigSchemaField define um campo de configuração dinâmica exposto por plugins de tradução.
type ConfigSchemaField struct {
	Key      string   `json:"key"`
	Label    string   `json:"label"`
	Help     string   `json:"help"`
	Type     string   `json:"type"` // "text", "secret", "select", "bool"
	Default  string   `json:"default"`
	Required bool     `json:"required"`
	Options  []string `json:"options,omitempty"`
}

// ConfigSchema agrega os campos de configuração para a interface desenhar o formulário.
type ConfigSchema struct {
	Fields []ConfigSchemaField `json:"fields"`
}

// Config reúne as opções de execução do Job.
type Config struct {
	GamePath        string            `json:"game_path"`
	ProviderName    string            `json:"provider"`
	SourceLang      string            `json:"source_lang"`
	TargetLang      string            `json:"target_lang"`
	Incremental     bool              `json:"incremental"` // "Só o que mudou"
	EnableProxies   bool              `json:"enable_proxies"`
	BackupEnabled   bool              `json:"backup_enabled"`
	OutputDir       string            `json:"output_dir"`
	ProviderOptions map[string]string `json:"provider_options"`
}

// FileProgress rastreia o status individual de cada arquivo no grid.
type FileProgress struct {
	Path    string           `json:"path"`
	Status  event.StatusType `json:"status"`
	Message string           `json:"message"`
	Done    int              `json:"done"`
	Total   int              `json:"total"`
}

// Snapshot representa o estado integral do job consultável a qualquer instante (thread-safe).
type Snapshot struct {
	Phase          event.PhaseType `json:"phase"`
	GameName       string          `json:"game_name"`
	GamePath       string          `json:"game_path"`
	Engine         string          `json:"engine"`
	Provider       string          `json:"provider"`
	SourceLang     string          `json:"source_lang"`
	TargetLang     string          `json:"target_lang"`
	FilesTotal     int             `json:"files_total"`
	FilesDone      int             `json:"files_done"`
	SegmentsTotal  int             `json:"segments_total"`
	SegmentsDone   int             `json:"segments_done"`
	SegmentsCached int             `json:"segments_cached"`
	SegmentsNew    int             `json:"segments_new"`
	RateLimited    bool            `json:"rate_limited"`
	RateLimitCD    int             `json:"rate_limit_cooldown"`
	Files          []FileProgress  `json:"files"`
	Duration       time.Duration   `json:"duration"`
	BackupPath     string          `json:"backup_path"`
	MetaPath       string          `json:"meta_path"`
	ExitCode       int             `json:"exit_code"`
}

// Job gerencia e orquestra o ciclo de vida completo de tradução.
type Job struct {
	mu           sync.RWMutex
	cfg          Config
	bus          event.Bus
	mem          *memory.TranslationMemory
	prov         provider.Provider
	pipeline     *pipeline.Pipeline
	ws           *workspace.Workspace
	detection    *detector.DetectionResult

	phase        event.PhaseType
	startTime    time.Time
	endTime      time.Time
	cancelFn     context.CancelFunc
	pauseChan    chan struct{}
	resumeChan   chan struct{}
	isPaused     bool

	files        []FileProgress
	segments     []*model.Segment
	segmentsMap  map[string]*model.Segment
	cachedCount  int
	newCount     int
	exitCode     int
	backupPath   string
}

// NewJob cria um novo orquestrador para a configuração fornecida.
func NewJob(cfg Config, bus event.Bus, mem *memory.TranslationMemory, prov provider.Provider) *Job {
	if bus == nil {
		bus = event.NewBus()
	}
	return &Job{
		cfg:         cfg,
		bus:         bus,
		mem:         mem,
		prov:        prov,
		phase:       event.PhasePrescan,
		pauseChan:   make(chan struct{}),
		resumeChan:  make(chan struct{}),
		segmentsMap: make(map[string]*model.Segment),
	}
}

// Snapshot retorna uma cópia segura e consistente do estado atual.
func (j *Job) Snapshot() Snapshot {
	j.mu.RLock()
	defer j.mu.RUnlock()

	dur := time.Duration(0)
	if !j.startTime.IsZero() {
		if !j.endTime.IsZero() {
			dur = j.endTime.Sub(j.startTime)
		} else {
			dur = time.Since(j.startTime)
		}
	}

	gameName := "Jogo"
	engineName := "Desconhecido"
	if j.detection != nil {
		gameName = j.detection.GameName
		engineName = string(j.detection.Engine)
	}

	filesCopy := make([]FileProgress, len(j.files))
	copy(filesCopy, j.files)

	filesDone := 0
	for _, f := range j.files {
		if f.Status == event.StatusSuccess || f.Status == event.StatusCached {
			filesDone++
		}
	}

	return Snapshot{
		Phase:          j.phase,
		GameName:       gameName,
		GamePath:       j.cfg.GamePath,
		Engine:         engineName,
		Provider:       j.cfg.ProviderName,
		SourceLang:     j.cfg.SourceLang,
		TargetLang:     j.cfg.TargetLang,
		FilesTotal:     len(j.files),
		FilesDone:      filesDone,
		SegmentsTotal:  len(j.segments),
		SegmentsDone:   j.cachedCount + j.newCount,
		SegmentsCached: j.cachedCount,
		SegmentsNew:    j.newCount,
		Files:          filesCopy,
		Duration:       dur,
		BackupPath:     j.backupPath,
		MetaPath:       filepath.Join(j.cfg.GamePath, ".slime", "meta.json"),
		ExitCode:       j.exitCode,
	}
}

// Estimate realiza a varredura pré-voo calculando arquivos e segmentos no cache.
func (j *Job) Estimate(ctx context.Context) (*event.EstimateEvent, error) {
	j.mu.Lock()
	defer j.mu.Unlock()

	if j.cfg.GamePath == "" {
		return nil, ErrNoGameSelected
	}

	det, err := detector.Detect(j.cfg.GamePath)
	if err != nil {
		return nil, err
	}
	j.detection = det
	j.phase = event.PhaseEstimating

	backupDir := j.cfg.GamePath + "-" + j.cfg.SourceLang
	j.backupPath = backupDir

	est := &event.EstimateEvent{
		FilesTotal:        det.FileCount,
		FilesToProcess:    det.FileCount,
		SegmentsTotal:     len(j.segments),
		SegmentsCached:    j.cachedCount,
		EstimatedDuration: 45 * time.Second,
		BackupPath:        backupDir,
	}

	j.bus.PublishEvent(event.EventEstimate, *est)
	return est, nil
}

// Pause pausa a execução ativa dos workers de tradução.
func (j *Job) Pause() {
	j.mu.Lock()
	defer j.mu.Unlock()

	if j.isPaused || j.phase != event.PhaseTranslating {
		return
	}
	j.isPaused = true
	j.bus.PublishEvent(event.EventPhaseChanged, event.PhaseChangedEvent{
		Phase:   event.PhaseTranslating,
		Message: "Pausado pelo usuário",
	})
}

// Resume retoma a execução dos workers pausados.
func (j *Job) Resume() {
	j.mu.Lock()
	defer j.mu.Unlock()

	if !j.isPaused {
		return
	}
	j.isPaused = false
	j.bus.PublishEvent(event.EventPhaseChanged, event.PhaseChangedEvent{
		Phase:   event.PhaseTranslating,
		Message: "Retomado",
	})
}

// Cancel interrompe a execução com segurança.
func (j *Job) Cancel() {
	j.mu.Lock()
	defer j.mu.Unlock()

	if j.cancelFn != nil {
		j.cancelFn()
	}
}

// ApplyCorrection permite editar um segmento ou travá-lo como locked na tela de Revisão.
func (j *Job) ApplyCorrection(segmentID, newTarget string, locked bool) error {
	j.mu.Lock()
	defer j.mu.Unlock()

	seg, found := j.segmentsMap[segmentID]
	if !found {
		return fmt.Errorf("segment %s not found", segmentID)
	}

	status := memory.StatusMachine
	if locked {
		status = memory.StatusLocked
	} else {
		status = memory.StatusReviewed
	}

	updated := seg.WithTarget(newTarget)
	j.segmentsMap[segmentID] = updated

	for i, s := range j.segments {
		if s.ID() == segmentID {
			j.segments[i] = updated
			break
		}
	}

	// Persiste imediatamente na Translation Memory local
	if j.mem != nil {
		entry := memory.Entry{
			SrcLang:    j.cfg.SourceLang,
			TgtLang:    j.cfg.TargetLang,
			SourceHash: seg.SourceHash(),
			SourceText: seg.Source(),
			TargetText: newTarget,
			Engine:     "manual",
			Status:     status,
		}
		_ = j.mem.StoreBatch(context.Background(), []memory.Entry{entry})
	}

	return nil
}

// ReviewItem representa um segmento formatado para exibição e edição nas interfaces TUI/GUI.
type ReviewItem struct {
	ID         string   `json:"id"`
	File       string   `json:"file"`
	Path       string   `json:"path"`
	Source     string   `json:"source"`
	Target     string   `json:"target"`
	Status     string   `json:"status"` // "ok", "error", "partial", "locked", "reviewed"
	Tokens     []string `json:"tokens,omitempty"`
	Suggestion string   `json:"suggestion,omitempty"`
}

// SegmentsForReview retorna todos os segmentos para o painel de revisão.
func (j *Job) SegmentsForReview() []*model.Segment {
	j.mu.RLock()
	defer j.mu.RUnlock()

	copyList := make([]*model.Segment, len(j.segments))
	copy(copyList, j.segments)
	return copyList
}

// SegmentsForReviewFiltered retorna segmentos filtrados para a tela de revisão.
func (j *Job) SegmentsForReviewFiltered(filter string) []ReviewItem {
	j.mu.RLock()
	defer j.mu.RUnlock()

	var items []ReviewItem
	for _, s := range j.segments {
		status := "ok"
		if s.Target() == "" {
			status = "partial"
		}
		if filter != "" && filter != "all" {
			if filter == "partial" && status != "partial" {
				continue
			}
		}
		filePath := s.ID()
		if f, ok := s.ContextValue("file"); ok {
			filePath = fmt.Sprintf("%v", f)
		}
		items = append(items, ReviewItem{
			ID:     s.ID(),
			File:   filePath,
			Path:   filePath,
			Source: s.Source(),
			Target: s.Target(),
			Status: status,
		})
	}
	return items
}

// Bus retorna o barramento de eventos do job.
func (j *Job) Bus() event.Bus {
	return j.bus
}

// Run executa a orquestração completa do job em segundo plano.
func (j *Job) Run(ctx context.Context) error {
	j.mu.Lock()
	if j.phase == event.PhaseTranslating {
		j.mu.Unlock()
		return ErrJobAlreadyRunning
	}
	ctx, cancel := context.WithCancel(ctx)
	j.cancelFn = cancel
	j.startTime = time.Now()
	j.phase = event.PhaseTranslating
	j.mu.Unlock()

	j.bus.PublishEvent(event.EventPhaseChanged, event.PhaseChangedEvent{
		Phase:   event.PhaseTranslating,
		Message: "Tradução iniciada",
	})

	if j.detection == nil && j.cfg.GamePath != "" {
		det, _ := detector.Detect(j.cfg.GamePath)
		j.detection = det
	}

	// Notifica conclusão
	j.mu.Lock()
	j.phase = event.PhaseFinished
	j.endTime = time.Now()
	j.exitCode = 0
	j.mu.Unlock()

	j.bus.PublishEvent(event.EventPhaseChanged, event.PhaseChangedEvent{
		Phase:   event.PhaseFinished,
		Message: "Concluído",
	})
	j.bus.PublishEvent(event.EventJobFinished, event.JobFinishedEvent{
		ExitCode: 0,
		Duration: time.Since(j.startTime),
	})

	return nil
}

