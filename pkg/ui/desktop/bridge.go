package desktop

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"sync"

	"github.com/BGLuis/slime-rpg/pkg/detector"
	"github.com/BGLuis/slime-rpg/pkg/event"
	"github.com/BGLuis/slime-rpg/pkg/job"
	"github.com/BGLuis/slime-rpg/pkg/memory"
	"github.com/BGLuis/slime-rpg/pkg/paths"
	"github.com/BGLuis/slime-rpg/pkg/provider"
	"github.com/BGLuis/slime-rpg/pkg/provider/deepl"
	"github.com/BGLuis/slime-rpg/pkg/provider/google"
	"github.com/BGLuis/slime-rpg/pkg/provider/ollama"
	"github.com/BGLuis/slime-rpg/pkg/provider/proxy"
	"github.com/egoist/mygo"
)

// Eventos tipados para comunicação reativa via MyGo IPC.
var (
	JobProgressEvent = mygo.NewEvent[job.Snapshot]("job:progress")
	JobPhaseEvent    = mygo.NewEvent[event.PhaseChangedEvent]("job:phase")
)

// JobStartOptions encapsula as opções enviadas pelo frontend TypeScript/HTML.
type JobStartOptions struct {
	Directory   string `json:"directory"`
	Provider    string `json:"provider"`
	SourceLang  string `json:"sourceLang"`
	TargetLang  string `json:"targetLang"`
	Incremental bool   `json:"incremental"`
	DryRun      bool   `json:"dryRun"`
	ApiKey      string `json:"apiKey"`
	Model       string `json:"model"`
	Endpoint    string `json:"endpoint"`
}

// EstimateResult encapsula a estimativa pré-voo calculada para a tela Configurar.
type EstimateResult struct {
	TotalSegments      int    `json:"totalSegments"`
	CachedSegments     int    `json:"cachedSegments"`
	ToTranslate        int    `json:"toTranslate"`
	CacheHitPercent    int    `json:"cacheHitPercent"`
	TotalFiles         int    `json:"totalFiles"`
	ChangedFiles       int    `json:"changedFiles"`
	EstimatedTimeLabel string `json:"estimatedTimeLabel"`
	CostLabel          string `json:"costLabel"`
	BackupPath         string `json:"backupPath"`
}

// SlimeService é o serviço de ponte exposto para a UI MyGo via IPC fortemente tipado.
type SlimeService struct {
	mu           sync.Mutex
	currentDir   string
	activeJob    *job.Job
	jobCancel    context.CancelFunc
	mem          *memory.TranslationMemory
}

// NewSlimeService inicializa o serviço bridge.
func NewSlimeService(initialDir string) *SlimeService {
	return &SlimeService{
		currentDir: initialDir,
	}
}

// DetectGame analisa o diretório fornecido e retorna o motor, data dir e idioma provável.
func (s *SlimeService) DetectGame(dir string) (*detector.DetectionResult, error) {
	if dir == "" {
		dir = s.currentDir
	}
	if dir == "" {
		dir = "."
	}
	return detector.Detect(dir)
}

// Estimate calcula o resumo de segmentos e arquivos antes de iniciar a tradução.
func (s *SlimeService) Estimate(opts JobStartOptions) (EstimateResult, error) {
	dir := opts.Directory
	if dir == "" {
		dir = s.currentDir
	}
	if dir == "" {
		dir = "."
	}

	memPath, err := paths.MemoryDBPath()
	if err != nil {
		return EstimateResult{}, fmt.Errorf("falha ao resolver caminho da memória: %w", err)
	}
	mem, err := memory.New(memPath)
	if err != nil {
		return EstimateResult{}, fmt.Errorf("falha ao abrir memória de tradução: %w", err)
	}
	defer mem.Close()

	prov := s.resolveProvider(opts)
	j := job.NewJob(job.Config{
		GamePath:      dir,
		ProviderName:  opts.Provider,
		SourceLang:    opts.SourceLang,
		TargetLang:    opts.TargetLang,
		Incremental:   opts.Incremental,
		BackupEnabled: true,
	}, nil, mem, prov)

	snap, err := j.Estimate(context.Background())
	if err != nil {
		return EstimateResult{}, err
	}

	cachePercent := 0
	if snap.SegmentsTotal > 0 {
		cachePercent = (snap.SegmentsCached * 100) / snap.SegmentsTotal
	}

	toTranslate := snap.SegmentsTotal - snap.SegmentsCached
	if toTranslate < 0 {
		toTranslate = 0
	}

	timeEst := "menos de 1 min"
	if toTranslate > 500 {
		timeEst = fmt.Sprintf("~%d min", (toTranslate/300)+1)
	}

	cost := "grátis"
	if opts.Provider == "deepl" && opts.ApiKey != "" {
		cost = "cota DeepL"
	}

	return EstimateResult{
		TotalSegments:      snap.SegmentsTotal,
		CachedSegments:     snap.SegmentsCached,
		ToTranslate:        toTranslate,
		CacheHitPercent:    cachePercent,
		TotalFiles:         snap.FilesTotal,
		ChangedFiles:       snap.FilesToProcess,
		EstimatedTimeLabel: timeEst,
		CostLabel:          cost,
		BackupPath:         snap.BackupPath,
	}, nil
}

// StartJob inicia um processo de tradução em background.
func (s *SlimeService) StartJob(opts JobStartOptions) (job.Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	dir := opts.Directory
	if dir == "" {
		dir = s.currentDir
	}
	if dir == "" {
		dir = "."
	}

	memPath, err := paths.MemoryDBPath()
	if err != nil {
		return job.Snapshot{}, fmt.Errorf("falha ao resolver caminho da memória: %w", err)
	}
	mem, err := memory.New(memPath)
	if err != nil {
		return job.Snapshot{}, fmt.Errorf("falha ao abrir memória: %w", err)
	}
	s.mem = mem

	prov := s.resolveProvider(opts)
	bus := event.NewBus()
	j := job.NewJob(job.Config{
		GamePath:      dir,
		ProviderName:  opts.Provider,
		SourceLang:    opts.SourceLang,
		TargetLang:    opts.TargetLang,
		Incremental:   opts.Incremental,
		BackupEnabled: true,
	}, bus, mem, prov)
	s.activeJob = j

	ctx, cancel := context.WithCancel(context.Background())
	s.jobCancel = cancel

	// Escuta eventos e faz broadcast para a webview
	ch := bus.SubscribeEvents(100)
	go func() {
		defer bus.UnsubscribeEvents(ch)
		for env := range ch {
			switch e := env.Data.(type) {
			case event.PhaseChangedEvent:
				_ = JobPhaseEvent.Broadcast(e)
			default:
				_ = JobProgressEvent.Broadcast(j.Snapshot())
			}
		}
	}()

	// Executa em goroutine isolada
	go func() {
		_ = j.Run(ctx)
		_ = JobProgressEvent.Broadcast(j.Snapshot())
	}()

	return j.Snapshot(), nil
}

// PauseJob pausa o job em andamento.
func (s *SlimeService) PauseJob() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.activeJob == nil {
		return fmt.Errorf("nenhum job ativo")
	}
	s.activeJob.Pause()
	return nil
}

// ResumeJob retoma o job pausado.
func (s *SlimeService) ResumeJob() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.activeJob == nil {
		return fmt.Errorf("nenhum job ativo")
	}
	s.activeJob.Resume()
	return nil
}

// CancelJob cancela o job em andamento.
func (s *SlimeService) CancelJob() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.jobCancel != nil {
		s.jobCancel()
	}
	if s.activeJob != nil {
		s.activeJob.Cancel()
	}
	return nil
}

// GetSnapshot retorna o estado instantâneo do job atual.
func (s *SlimeService) GetSnapshot() job.Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.activeJob == nil {
		return job.Snapshot{}
	}
	return s.activeJob.Snapshot()
}

// GetSegmentsForReview retorna segmentos para inspeção na tela de revisão.
func (s *SlimeService) GetSegmentsForReview(filter string) []job.ReviewItem {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.activeJob == nil {
		return nil
	}
	return s.activeJob.SegmentsForReviewFiltered(filter)
}

// ApplyCorrection aplica uma edição manual a um segmento com integridade verificada.
func (s *SlimeService) ApplyCorrection(id string, text string, locked bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.activeJob == nil {
		return fmt.Errorf("nenhum job ativo")
	}
	return s.activeJob.ApplyCorrection(id, text, locked)
}

// OpenFolder abre a pasta especificada no explorador de arquivos do sistema.
func (s *SlimeService) OpenFolder(path string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("explorer", path)
	case "darwin":
		cmd = exec.Command("open", path)
	default:
		cmd = exec.Command("xdg-open", path)
	}
	return cmd.Start()
}

func (s *SlimeService) resolveProvider(opts JobStartOptions) provider.Provider {
	switch opts.Provider {
	case "deepl":
		return deepl.NewDeepLTranslator(opts.ApiKey)
	case "ollama":
		model := opts.Model
		if model == "" {
			model = "llama3"
		}
		endpoint := opts.Endpoint
		if endpoint == "" {
			endpoint = "http://localhost:11434"
		}
		return ollama.NewOllamaTranslator(endpoint, model, "")
	default:
		pm := proxy.NewManager(proxy.EnabledByEnv(), nil)
		pm.UsePublicSources()
		return google.NewGoogleTranslator(pm)
	}
}
