package job

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/BGLuis/slime-rpg/pkg/detector"
	"github.com/BGLuis/slime-rpg/pkg/engine"
	"github.com/BGLuis/slime-rpg/pkg/event"
	"github.com/BGLuis/slime-rpg/pkg/formats"
	"github.com/BGLuis/slime-rpg/pkg/memory"
	"github.com/BGLuis/slime-rpg/pkg/metadata"
	"github.com/BGLuis/slime-rpg/pkg/model"
	"github.com/BGLuis/slime-rpg/pkg/pipeline"
	"github.com/BGLuis/slime-rpg/pkg/provider"
	"github.com/BGLuis/slime-rpg/pkg/workspace"
)

var (
	ErrJobAlreadyRunning = errors.New("job is already running")
	ErrJobNotPaused      = errors.New("job is not paused")
	ErrNoGameSelected    = errors.New("no game path specified")
	ErrSameLanguage      = errors.New("source and target languages must differ")
	ErrNoProvider        = errors.New("a translation provider is required unless memory-only mode is enabled")
	ErrRateLimited       = errors.New("provider rate limit reached: enable the proxy pool or use a VPN and run again")
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

	// MemoryOnly traduz só com a Memória de Tradução, sem chamar o provedor (offline).
	MemoryOnly bool `json:"memory_only"`
	// SwitchEngine descarta traduções de máquina de outros provedores na memória.
	SwitchEngine bool `json:"switch_engine"`
}

// Validate aplica as mesmas regras do JobConfig da versão Python.
func (c Config) Validate() error {
	if c.GamePath == "" {
		return ErrNoGameSelected
	}
	if c.TargetLang == "" {
		return errors.New("target language is required")
	}
	if c.SourceLang != "" && c.SourceLang != "auto" && strings.EqualFold(c.SourceLang, c.TargetLang) {
		return ErrSameLanguage
	}
	return nil
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
	mu        sync.RWMutex
	cfg       Config
	bus       event.Bus
	mem       *memory.TranslationMemory
	prov      provider.Provider
	detection *detector.DetectionResult

	phase      event.PhaseType
	running    bool
	startTime  time.Time
	endTime    time.Time
	cancelFn   context.CancelFunc
	isPaused   bool
	resumeChan chan struct{}

	files         []FileProgress
	fileIndex     map[string]int
	segments      []*model.Segment
	segmentsMap   map[string]*model.Segment
	segmentsTotal int
	cachedCount   int
	newCount      int
	rateLimited   bool
	exitCode      int
	backupPath    string
	metaPath      string
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
		fileIndex:   make(map[string]int),
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
		switch f.Status {
		case event.StatusSuccess, event.StatusCached, event.StatusPartial, event.StatusIgnore, event.StatusError:
			filesDone++
		}
	}

	metaPath := j.metaPath
	if metaPath == "" {
		metaPath = metadata.Path(j.cfg.GamePath)
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
		SegmentsTotal:  max(j.segmentsTotal, len(j.segments)),
		SegmentsDone:   j.cachedCount + j.newCount,
		SegmentsCached: j.cachedCount,
		SegmentsNew:    j.newCount,
		RateLimited:    j.rateLimited,
		Files:          filesCopy,
		Duration:       dur,
		BackupPath:     j.backupPath,
		MetaPath:       metaPath,
		ExitCode:       j.exitCode,
	}
}

// Estimate faz a varredura pré-voo: extrai os textos e consulta a Memória de
// Tradução para dizer quantos segmentos ainda precisam do provedor.
func (j *Job) Estimate(ctx context.Context) (*event.EstimateEvent, error) {
	if err := j.cfg.Validate(); err != nil {
		return nil, err
	}

	det, plan, err := j.prepare()
	if err != nil {
		return nil, err
	}
	backupDir := j.backupDirFor(plan)
	files := plan.Files
	if j.cfg.Incremental {
		files = j.incrementalFiles(plan, plan.Root, backupDir)
	}

	extracted := j.extractAll(ctx, plan.Root, backupDir, files)
	var sources []string
	for _, f := range extracted {
		for _, s := range f.segments {
			sources = append(sources, s.Source())
		}
	}

	cached := 0
	uncachedChars := 0
	hits := j.lookup(ctx, sources)
	for _, src := range sources {
		if _, ok := hits[src]; ok {
			cached++
		} else {
			uncachedChars += len([]rune(src))
		}
	}

	j.mu.Lock()
	j.detection = det
	j.backupPath = backupDir
	j.phase = event.PhaseEstimating
	j.mu.Unlock()

	est := &event.EstimateEvent{
		FilesTotal:        len(plan.Files),
		FilesToProcess:    len(files),
		SegmentsTotal:     len(sources),
		SegmentsCached:    cached,
		EstimatedDuration: j.estimateDuration(uncachedChars),
		BackupPath:        backupDir,
	}
	j.bus.PublishEvent(event.EventEstimate, *est)
	return est, nil
}

// estimateDuration supõe ~1,5s por lote cheio, dividido pela concorrência do provedor.
func (j *Job) estimateDuration(chars int) time.Duration {
	if chars == 0 || j.cfg.MemoryOnly || j.prov == nil {
		return 0
	}
	caps := provider.CapabilitiesOf(j.prov)
	batches := (chars + caps.MaxChars - 1) / caps.MaxChars
	waves := (batches + caps.Concurrency - 1) / caps.Concurrency
	return time.Duration(waves) * 1500 * time.Millisecond
}

// Pause pausa a execução ativa dos workers de tradução.
func (j *Job) Pause() {
	j.mu.Lock()
	defer j.mu.Unlock()

	if j.isPaused || j.phase != event.PhaseTranslating {
		return
	}
	j.isPaused = true
	j.resumeChan = make(chan struct{})
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
	if j.resumeChan != nil {
		close(j.resumeChan)
		j.resumeChan = nil
	}
	j.bus.PublishEvent(event.EventPhaseChanged, event.PhaseChangedEvent{
		Phase:   event.PhaseTranslating,
		Message: "Retomado",
	})
}

// waitIfPaused bloqueia o envio de novos lotes enquanto o job estiver pausado.
func (j *Job) waitIfPaused(ctx context.Context) error {
	j.mu.RLock()
	ch := j.resumeChan
	j.mu.RUnlock()
	if ch == nil {
		return nil
	}
	select {
	case <-ch:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
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

	status := memory.StatusReviewed
	if locked {
		status = memory.StatusLocked
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
		if s.Target() == "" || j.untranslated(s) {
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

// extractedFile guarda o resultado da extração de um arquivo.
type extractedFile struct {
	file     formats.File
	data     []byte
	segments []*model.Segment
	err      error
}

// Run executa a tradução completa: backup, seleção incremental, extração,
// tradução deduplicada entre arquivos, reinjeção, gravação atômica e registro em
// .slime/meta.json. Falhas por arquivo não interrompem os demais.
func (j *Job) Run(ctx context.Context) (err error) {
	if err := j.cfg.Validate(); err != nil {
		return err
	}
	if j.prov == nil && !j.cfg.MemoryOnly {
		return ErrNoProvider
	}

	j.mu.Lock()
	if j.running {
		j.mu.Unlock()
		return ErrJobAlreadyRunning
	}
	ctx, cancel := context.WithCancel(ctx)
	j.running = true
	j.cancelFn = cancel
	j.startTime = time.Now()
	j.endTime = time.Time{}
	j.mu.Unlock()

	defer func() {
		cancel()
		j.finish(err)
	}()

	j.setPhase(event.PhasePrescan, "Varrendo arquivos")

	det, plan, err := j.prepare()
	if err != nil {
		return err
	}
	defer plan.Close()
	j.mu.Lock()
	j.detection = det
	j.mu.Unlock()

	ws, err := workspace.New(plan.Root, j.cfg.OutputDir)
	if err != nil {
		return err
	}
	defer ws.Cleanup()

	backupDir := ""
	if j.cfg.BackupEnabled {
		backupDir = j.backupDirFor(plan)
		if _, err := ws.EnsureBackup(backupDir, plan.Includes); err != nil {
			return fmt.Errorf("failed to create backup at %s: %w", backupDir, err)
		}
	}
	j.mu.Lock()
	j.backupPath = backupDir
	j.mu.Unlock()

	files := plan.Files
	if j.cfg.Incremental {
		files = j.incrementalFiles(plan, ws.OutputDir(), backupDir)
	}
	j.initFiles(files)

	extracted := j.extractAll(ctx, ws.InputDir(), backupDir, files)
	if err := ctx.Err(); err != nil {
		return err
	}

	j.setPhase(event.PhaseTranslating, "Tradução iniciada")
	translations, cached, eng, err := j.translate(ctx, plan, det, extracted)
	if err != nil {
		return err
	}

	j.setPhase(event.PhaseCommitting, "Gravando arquivos traduzidos")
	statuses := j.inject(ws, extracted, translations, cached)
	j.repack(plan, ws)

	if _, err := ws.Commit(); err != nil {
		return fmt.Errorf("failed to commit translated files: %w", err)
	}

	j.recordMetadata(ws, plan, det, backupDir, statuses)

	if eng.Stats().RateLimited {
		j.mu.Lock()
		j.rateLimited = true
		j.mu.Unlock()
		return ErrRateLimited
	}
	return nil
}

// prepare detecta o jogo e monta o plano de arquivos.
func (j *Job) prepare() (*detector.DetectionResult, *formats.Plan, error) {
	det, err := detector.Detect(j.cfg.GamePath)
	if err != nil {
		return nil, nil, err
	}
	plan, err := formats.ForDetection(det)
	if err != nil {
		return nil, nil, err
	}
	return det, plan, nil
}

// backupDirFor segue a convenção da versão Python: <pasta>-<idioma de origem>.
func (j *Job) backupDirFor(plan *formats.Plan) string {
	return filepath.Clean(plan.Root) + "-" + j.cfg.SourceLang
}

// incrementalFiles devolve só os arquivos cujo estado no metadado exige nova
// tradução (original alterado, novo, parcial ou tradução ausente). Sem metadado,
// processa tudo.
func (j *Job) incrementalFiles(plan *formats.Plan, outputDir, backupDir string) []formats.File {
	metaDir := outputDir
	meta := metadata.Load(metaDir)
	if meta == nil {
		metaDir = plan.Root
		meta = metadata.Load(metaDir)
	}
	if meta == nil {
		j.bus.PublishEvent(event.EventPhaseChanged, event.PhaseChangedEvent{
			Phase:   event.PhasePrescan,
			Message: "Nenhum .slime/meta.json encontrado: executando tradução completa",
		})
		return plan.Files
	}

	originalDir := ""
	if isDir(backupDir) {
		originalDir = backupDir
	} else if resolved := metadata.ResolveOriginalDir(metaDir, meta); isDir(resolved) {
		originalDir = resolved
	} else if filepath.Clean(outputDir) != filepath.Clean(plan.Root) {
		originalDir = plan.Root
	}

	report, err := metadata.Verify(metaDir, originalDir, plan.Format.Handles)
	if err != nil {
		return plan.Files
	}
	targets := make(map[string]bool)
	for _, r := range report {
		if metadata.NeedsTranslation(r.State) {
			targets[r.File] = true
		}
	}
	var files []formats.File
	for _, f := range plan.Files {
		if targets[f.Rel] {
			files = append(files, f)
		}
	}
	return files
}

func (j *Job) initFiles(files []formats.File) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.files = make([]FileProgress, len(files))
	j.fileIndex = make(map[string]int, len(files))
	for i, f := range files {
		j.files[i] = FileProgress{Path: f.Rel, Status: event.StatusWaiting, Message: "Na fila de processamento..."}
		j.fileIndex[f.Rel] = i
	}
}

// extractAll lê e extrai os arquivos em paralelo.
func (j *Job) extractAll(ctx context.Context, inputDir, backupDir string, files []formats.File) []*extractedFile {
	meta := metadata.Load(inputDir)
	out := make([]*extractedFile, len(files))
	sem := make(chan struct{}, 8)
	var wg sync.WaitGroup
	for i, f := range files {
		if ctx.Err() != nil {
			break
		}
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			ef := &extractedFile{file: f}
			out[i] = ef
			if f.Read != nil {
				ef.data, ef.err = f.Read()
				if ef.err == nil {
					preserveOriginal(backupDir, f.Rel, ef.data)
				}
			} else {
				ef.data, ef.err = os.ReadFile(sourcePath(inputDir, backupDir, f.Rel, meta))
			}
			if ef.err == nil {
				ef.segments, ef.err = f.Format.Extract(f.Rel, ef.data, j.cfg.SourceLang, j.cfg.TargetLang)
			}
			if ef.err != nil {
				j.updateFile(f.Rel, event.StatusError, "Erro na extração: "+ef.err.Error(), 0, 0)
			}
		}()
	}
	wg.Wait()

	var done []*extractedFile
	for _, ef := range out {
		if ef != nil {
			done = append(done, ef)
		}
	}
	return done
}

// preserveOriginal guarda no backup um arquivo que só existia dentro de um
// contêiner, para que o original continue disponível depois que a tradução
// solta o substituir. Nunca sobrescreve um arquivo já presente.
func preserveOriginal(backupDir, rel string, data []byte) {
	if backupDir == "" {
		return
	}
	path := filepath.Join(backupDir, filepath.FromSlash(rel))
	if _, err := os.Stat(path); err == nil {
		return
	}
	if os.MkdirAll(filepath.Dir(path), 0o755) == nil {
		_ = os.WriteFile(path, data, 0o644)
	}
}

// sourcePath escolhe de onde ler o texto original. Numa tradução in-place
// repetida a entrada já contém a tradução anterior: se ela é exatamente o que o
// último job gravou, o original é lido do backup. Qualquer outra mudança
// (atualização do jogo, edição manual) é lida da própria entrada.
func sourcePath(inputDir, backupDir, rel string, meta *metadata.Meta) string {
	input := filepath.Join(inputDir, filepath.FromSlash(rel))
	if backupDir == "" || meta == nil {
		return input
	}
	entry, ok := meta.Files[rel]
	if !ok || entry.Status == metadata.StatusUnchanged || entry.TranslatedSHA256 == "" {
		return input
	}
	backup := filepath.Join(backupDir, filepath.FromSlash(rel))
	if _, err := os.Stat(backup); err != nil {
		return input
	}
	if metadata.SHA256File(input) != entry.TranslatedSHA256 {
		return input
	}
	return backup
}

type uniqueKey struct {
	kind   model.SegmentKind
	source string
}

// translate deduplica os segmentos de todos os arquivos e os traduz numa única
// passada do pipeline, para que um texto repetido em vários arquivos custe uma
// só consulta à memória e uma só requisição ao provedor.
func (j *Job) translate(ctx context.Context, plan *formats.Plan, det *detector.DetectionResult, files []*extractedFile) (map[uniqueKey]string, map[uniqueKey]bool, *engine.Engine, error) {
	index := make(map[uniqueKey]int)
	var unique []*model.Segment
	total := 0
	for _, f := range files {
		for _, s := range f.segments {
			total++
			k := uniqueKey{s.Kind(), s.Source()}
			if _, seen := index[k]; seen {
				continue
			}
			seg, err := model.NewSegment(fmt.Sprintf("u#%d", len(unique)), s.Source(), s.Kind(), nil, nil)
			if err != nil {
				continue
			}
			index[k] = len(unique)
			unique = append(unique, seg)
		}
	}

	bypass := func() bool { return false }
	if b, ok := j.prov.(interface{ CanBypassRateLimit() bool }); ok {
		bypass = b.CanBypassRateLimit
	}
	var throttleOnce sync.Once
	eng := engine.New(j.prov, engine.Options{
		MemoryOnly:         j.cfg.MemoryOnly,
		CanBypassRateLimit: bypass,
		Wait:               j.waitIfPaused,
		OnProgress: func(done, _ int) {
			j.mu.Lock()
			j.newCount = done
			j.mu.Unlock()
			j.publishProgress()
		},
		OnRateLimited: func() {
			throttleOnce.Do(func() {
				j.bus.PublishEvent(event.EventProviderThrottled, event.ProviderThrottledEvent{
					Provider:       j.cfg.ProviderName,
					CanEnableProxy: !bypass(),
					Message:        "O provedor limitou as requisições (HTTP 429)",
				})
			})
		},
	})

	j.mu.Lock()
	j.segmentsTotal = total
	j.cachedCount, j.newCount = 0, 0
	j.mu.Unlock()

	if len(unique) == 0 {
		return map[uniqueKey]string{}, map[uniqueKey]bool{}, eng, nil
	}

	engineID := j.cfg.ProviderName
	if j.prov != nil {
		engineID = j.prov.ID()
	}
	pctx := pipeline.NewContext(unique, j.cfg.SourceLang, j.cfg.TargetLang, engineID, nil, eng)
	if j.mem != nil {
		if j.cfg.SwitchEngine {
			j.mem.SetPolicy(memory.Policy{SwitchEngine: true})
		}
		pctx.Memory = j.mem
	}
	pctx.Masker = plan.Format.Masker()
	pctx.Game = det.GameName

	if err := pipeline.NewPipeline(&pipeline.CacheLookupStep{}).Execute(ctx, pctx); err != nil {
		return nil, nil, eng, err
	}
	cachedUnique := make(map[int]bool)
	for i, s := range pctx.Segments {
		if s.IsResolved() {
			cachedUnique[i] = true
		}
	}

	cachedSegs := 0
	for _, f := range files {
		for _, s := range f.segments {
			if cachedUnique[index[uniqueKey{s.Kind(), s.Source()}]] {
				cachedSegs++
			}
		}
	}
	j.mu.Lock()
	j.cachedCount = cachedSegs
	j.mu.Unlock()
	j.publishProgress()

	rest := pipeline.NewPipeline(
		&pipeline.PatternDetectionStep{},
		&pipeline.MaskingStep{},
		&pipeline.TranslationStep{},
		&pipeline.UnmaskingStep{},
		&pipeline.FixingStep{},
		&pipeline.CacheStoreStep{},
	)
	if err := rest.Execute(ctx, pctx); err != nil {
		return nil, nil, eng, err
	}

	translations := make(map[uniqueKey]string, len(unique))
	cached := make(map[uniqueKey]bool, len(cachedUnique))
	for k, i := range index {
		translations[k] = pctx.Segments[i].Target()
		if cachedUnique[i] {
			cached[k] = true
		}
	}
	return translations, cached, eng, nil
}

// inject reinsere as traduções em cada arquivo e grava o resultado na pasta de
// processamento. Devolve o status final de cada arquivo para o metadado.
func (j *Job) inject(ws *workspace.Workspace, files []*extractedFile, translations map[uniqueKey]string, cached map[uniqueKey]bool) map[string]string {
	statuses := make(map[string]string)
	var all []*model.Segment

	for _, f := range files {
		rel := f.file.Rel
		if f.err != nil {
			continue
		}
		if len(f.segments) == 0 {
			data, rewritten := f.data, false
			if rw, ok := f.file.Format.(formats.Rewriter); ok {
				if out, changed, err := rw.Rewrite(rel, f.data); err == nil && changed {
					data, rewritten = out, true
				}
			}
			if !ws.IsInPlace() || rewritten {
				if err := writeProcess(ws, rel, data); err != nil {
					j.updateFile(rel, event.StatusError, "Erro ao salvar arquivo: "+err.Error(), 0, 0)
					continue
				}
			}
			if rewritten {
				statuses[rel] = metadata.StatusSuccess
				j.updateFile(rel, event.StatusSuccess, "Fonte do jogo substituída", 0, 0)
				continue
			}
			statuses[rel] = metadata.StatusUnchanged
			j.updateFile(rel, event.StatusIgnore, "Sem textos para traduzir", 0, 0)
			continue
		}

		resolved := make([]*model.Segment, len(f.segments))
		missing := 0
		allCached := true
		for i, s := range f.segments {
			k := uniqueKey{s.Kind(), s.Source()}
			target := translations[k]
			if target == "" {
				target = s.Source()
			}
			resolved[i] = s.WithTarget(target)
			if !cached[k] {
				allCached = false
				if j.untranslated(resolved[i]) {
					missing++
				}
			}
		}
		all = append(all, resolved...)

		out, err := f.file.Format.Inject(rel, f.data, resolved, j.cfg.SourceLang, j.cfg.TargetLang)
		if err == nil {
			err = writeProcess(ws, rel, out)
		}
		if err != nil {
			j.updateFile(rel, event.StatusError, "Erro ao salvar arquivo: "+err.Error(), 0, len(resolved))
			continue
		}

		total := len(resolved)
		switch {
		case missing > 0:
			statuses[rel] = metadata.StatusPartial
			j.updateFile(rel, event.StatusPartial, fmt.Sprintf("Tradução parcial (%d/%d)", total-missing, total), total-missing, total)
		case allCached:
			statuses[rel] = metadata.StatusSuccess
			j.updateFile(rel, event.StatusCached, "Resolvido pela Memória de Tradução", total, total)
		default:
			statuses[rel] = metadata.StatusSuccess
			j.updateFile(rel, event.StatusSuccess, "Processado com sucesso", total, total)
		}
	}

	j.mu.Lock()
	j.segments = all
	j.segmentsMap = make(map[string]*model.Segment, len(all))
	for _, s := range all {
		j.segmentsMap[s.ID()] = s
	}
	j.mu.Unlock()
	return statuses
}

// untranslated indica um segmento que voltou igual ao original: entre idiomas
// diferentes isso é falha do provedor ou da máscara. Traduções idênticas vindas
// da memória (ex.: nome próprio revisado) não passam por aqui.
func (j *Job) untranslated(s *model.Segment) bool {
	if strings.EqualFold(j.cfg.SourceLang, j.cfg.TargetLang) {
		return false
	}
	return strings.TrimSpace(s.Target()) == strings.TrimSpace(s.Source())
}

// repack gera o Data.wolf quando o modo de reempacotamento está ligado. Falhar
// aqui só perde o contêiner: os arquivos soltos traduzidos continuam sendo gravados.
func (j *Job) repack(plan *formats.Plan, ws *workspace.Workspace) {
	rel, err := plan.Repack(ws.ProcessDir())
	if err == nil {
		return
	}
	_ = os.Remove(filepath.Join(ws.ProcessDir(), filepath.FromSlash(rel)))
	j.bus.PublishEvent(event.EventPhaseChanged, event.PhaseChangedEvent{
		Phase:   event.PhaseCommitting,
		Message: "Aviso: não foi possível reempacotar " + rel + ": " + err.Error(),
	})
}

func writeProcess(ws *workspace.Workspace, rel string, data []byte) error {
	path := ws.ProcessPath(filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// recordMetadata grava .slime/meta.json cruzando original e tradução. Uma falha
// aqui nunca invalida a tradução já gravada.
func (j *Job) recordMetadata(ws *workspace.Workspace, plan *formats.Plan, det *detector.DetectionResult, backupDir string, statuses map[string]string) {
	translatedDir := ws.OutputDir()
	previous := metadata.Load(translatedDir)

	originalDir := ""
	switch {
	case isDir(backupDir):
		originalDir = backupDir
	case !ws.IsInPlace():
		originalDir = ws.InputDir()
	case previous != nil:
		if resolved := metadata.ResolveOriginalDir(translatedDir, previous); isDir(resolved) {
			originalDir = resolved
		}
	}

	meta := metadata.Build(translatedDir, originalDir, metadata.BuildOptions{
		Origin:     metadata.OriginJob,
		Extractor:  plan.Format.Name(),
		Translator: j.cfg.ProviderName,
		LangSource: j.cfg.SourceLang,
		LangTarget: j.cfg.TargetLang,
		Extensions: plan.Format.Extensions(),
		Filter:     plan.Format.Handles,
		FileStatus: statuses,
		Previous:   previous,
		GameName:   det.GameName,
		Engine:     string(det.Engine),
	})
	path, err := metadata.Save(translatedDir, meta)
	if err != nil {
		j.bus.PublishEvent(event.EventPhaseChanged, event.PhaseChangedEvent{
			Phase:   event.PhaseCommitting,
			Message: "Aviso: não foi possível gravar o metadado do jogo: " + err.Error(),
		})
		return
	}
	j.mu.Lock()
	j.metaPath = path
	j.mu.Unlock()
}

func (j *Job) setPhase(phase event.PhaseType, msg string) {
	j.mu.Lock()
	j.phase = phase
	j.mu.Unlock()
	j.bus.PublishEvent(event.EventPhaseChanged, event.PhaseChangedEvent{Phase: phase, Message: msg})
}

func (j *Job) updateFile(rel string, status event.StatusType, msg string, done, total int) {
	j.mu.Lock()
	if i, ok := j.fileIndex[rel]; ok {
		j.files[i] = FileProgress{Path: rel, Status: status, Message: msg, Done: done, Total: total}
	}
	j.mu.Unlock()
	j.bus.PublishEvent(event.EventFileStatus, event.FileStatusEvent{File: rel, Status: status, Message: msg, Current: done, Total: total})
}

func (j *Job) publishProgress() {
	j.mu.RLock()
	ev := event.SegmentProgressEvent{
		Done:          j.cachedCount + j.newCount,
		Cached:        j.cachedCount,
		NewTranslated: j.newCount,
		Total:         j.segmentsTotal,
	}
	j.mu.RUnlock()
	j.bus.PublishEvent(event.EventSegmentProgress, ev)
}

// finish publica o encerramento com o resumo por arquivo.
func (j *Job) finish(runErr error) {
	j.mu.Lock()
	j.running = false
	j.cancelFn = nil
	j.isPaused = false
	j.phase = event.PhaseFinished
	j.endTime = time.Now()

	summary := event.JobFinishedEvent{
		Duration:       j.endTime.Sub(j.startTime),
		SegmentsTotal:  j.segmentsTotal,
		SegmentsCached: j.cachedCount,
		SegmentsNew:    j.newCount,
		BackupPath:     j.backupPath,
		MetaPath:       j.metaPath,
	}
	for _, f := range j.files {
		switch f.Status {
		case event.StatusSuccess, event.StatusCached:
			summary.FilesComplete++
		case event.StatusPartial:
			summary.FilesPartial++
		case event.StatusError:
			summary.FilesError++
		}
	}
	j.exitCode = 0
	if runErr != nil || summary.FilesError > 0 {
		j.exitCode = 1
	}
	summary.ExitCode = j.exitCode
	j.mu.Unlock()

	msg := "Concluído"
	if runErr != nil {
		msg = "Interrompido: " + runErr.Error()
	}
	j.bus.PublishEvent(event.EventPhaseChanged, event.PhaseChangedEvent{Phase: event.PhaseFinished, Message: msg})
	j.bus.PublishEvent(event.EventJobFinished, summary)
}

func (j *Job) lookup(ctx context.Context, sources []string) map[string]string {
	if j.mem == nil || len(sources) == 0 {
		return nil
	}
	engineID := j.cfg.ProviderName
	if j.prov != nil {
		engineID = j.prov.ID()
	}
	hits, err := j.mem.LookupBatch(ctx, j.cfg.SourceLang, j.cfg.TargetLang, engineID, sources)
	if err != nil {
		return nil
	}
	return hits
}

func isDir(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
