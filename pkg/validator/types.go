// Package validator implementa verificações estáticas (Camada B) e de execução
// headless (Camada A) para detectar traduções que quebram o jogo antes do play.
package validator

import (
	"context"
	"time"
)

// Severity indica a gravidade de um problema encontrado na validação.
type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
)

// Category classifica a origem do problema detectado.
type Category string

const (
	CategoryJSON        Category = "json"
	CategoryCSV         Category = "csv"
	CategoryScript      Category = "script"
	CategoryAsset       Category = "asset"
	CategoryControlCode Category = "control_code"
	CategoryPlaceholder Category = "placeholder"
	CategoryRuntime     Category = "runtime"
)

// Issue descreve um problema específico detectado em um arquivo ou na execução do jogo.
type Issue struct {
	Severity   Severity `json:"severity"`
	Category   Category `json:"category"`
	File       string   `json:"file"`
	Location   string   `json:"location,omitempty"`
	Source     string   `json:"source,omitempty"`
	Translated string   `json:"translated,omitempty"`
	Message    string   `json:"message"`
}

// Report consolida o resultado das verificações estática e runtime.
type Report struct {
	GamePath             string        `json:"game_path"`
	OriginalDir          string        `json:"original_dir,omitempty"`
	Engine               string        `json:"engine"`
	StaticChecked        bool          `json:"static_checked"`
	RuntimeRan           bool          `json:"runtime_ran"`
	RuntimeSkippedReason string        `json:"runtime_skipped_reason,omitempty"`
	SceneReached         string        `json:"scene_reached,omitempty"`
	TourScenesTested     []string      `json:"tour_scenes_tested,omitempty"`
	Duration             time.Duration `json:"duration"`
	Issues               []Issue       `json:"issues"`
}

// Add registra um novo problema no relatório.
func (r *Report) Add(issue Issue) {
	r.Issues = append(r.Issues, issue)
}

// ErrorCount retorna o total de problemas com severidade error.
func (r *Report) ErrorCount() int {
	n := 0
	for _, it := range r.Issues {
		if it.Severity == SeverityError {
			n++
		}
	}
	return n
}

// WarningCount retorna o total de avisos com severidade warning.
func (r *Report) WarningCount() int {
	n := 0
	for _, it := range r.Issues {
		if it.Severity == SeverityWarning {
			n++
		}
	}
	return n
}

// HasErrors indica se houve pelo menos um erro crítico.
func (r *Report) HasErrors() bool {
	return r.ErrorCount() > 0
}

// Options configura a execução da validação.
type Options struct {
	OriginalDir string
	RunRuntime  bool
	SceneTour   bool
	Timeout     time.Duration
	BrowserPath string
}

// Validate executa a validação estática (Camada B) e, quando aplicável e habilitado,
// o smoke test em navegador headless (Camada A) sobre gameDir.
func Validate(ctx context.Context, gameDir string, opts Options) (*Report, error) {
	start := time.Now()
	report, webRoot, err := validateStaticInternal(ctx, gameDir, opts.OriginalDir)
	if err != nil {
		return nil, err
	}

	if opts.RunRuntime {
		if webRoot == "" {
			report.RuntimeSkippedReason = "teste headless disponível apenas para jogos HTML5/NW.js (RPG Maker MV/MZ)"
		} else {
			rtRes, err := runRuntimeInternal(ctx, webRoot, opts)
			if err != nil {
				report.RuntimeSkippedReason = err.Error()
			} else {
				report.RuntimeRan = rtRes.Ran
				report.RuntimeSkippedReason = rtRes.SkippedReason
				report.SceneReached = rtRes.SceneReached
				report.TourScenesTested = rtRes.TourScenesTested
				report.Issues = append(report.Issues, rtRes.Issues...)
			}
		}
	}

	report.Duration = time.Since(start)
	return report, nil
}
