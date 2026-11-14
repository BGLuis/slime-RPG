package tui

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/BGLuis/slime-rpg/pkg/event"
	"github.com/BGLuis/slime-rpg/pkg/formats"
	"github.com/BGLuis/slime-rpg/pkg/i18n"
	"github.com/BGLuis/slime-rpg/pkg/job"
	"github.com/BGLuis/slime-rpg/pkg/memory"
	"github.com/BGLuis/slime-rpg/pkg/paths"
	"github.com/BGLuis/slime-rpg/pkg/provider"
	"github.com/BGLuis/slime-rpg/pkg/provider/deepl"
	"github.com/BGLuis/slime-rpg/pkg/provider/google"
	"github.com/BGLuis/slime-rpg/pkg/provider/ollama"
	"github.com/BGLuis/slime-rpg/pkg/provider/proxy"
	tea "github.com/charmbracelet/bubbletea"
)

// providerIDs acompanha Model.providers; o último item é o modo só-memória, sem provedor.
var providerIDs = []string{"google", "deepl", "ollama", ""}

// providerHints são as chaves i18n da dica de cada provedor, na mesma ordem de providerIDs.
var providerHints = []string{"ui_hint_google", "ui_hint_deepl", "ui_hint_ollama", "tui_hint_memory"}

// ProviderFactory cria o provedor escolhido na tela Configurar.
type ProviderFactory func(p ProviderParams) (provider.Provider, error)

// ProviderParams reúne o que a tela Configurar coletou para criar o provedor.
type ProviderParams struct {
	ID, APIKey, Model, Host string
	Proxies                 bool
}

// MemoryOpener abre a Memória de Tradução usada pelo job.
type MemoryOpener func() (*memory.TranslationMemory, error)

// Option personaliza as dependências do Model (os testes injetam provedor falso).
type Option func(*Model)

func WithProviderFactory(f ProviderFactory) Option { return func(m *Model) { m.newProvider = f } }

func WithMemoryOpener(f MemoryOpener) Option { return func(m *Model) { m.openMemory = f } }

type eventMsg struct {
	ch  <-chan event.Envelope
	env event.Envelope
}

type runDoneMsg struct {
	job *job.Job
	err error
}

func defaultProvider(p ProviderParams) (provider.Provider, error) {
	switch p.ID {
	case "deepl":
		key := cmp.Or(p.APIKey, os.Getenv("DEEPL_API_KEY"))
		if key == "" {
			return nil, errors.New(i18n.T("tui_err_deepl_key"))
		}
		return deepl.NewDeepLTranslator(key), nil
	case "ollama":
		model := cmp.Or(p.Model, os.Getenv("OLLAMA_MODEL"))
		if model == "" {
			return nil, errors.New(i18n.T("tui_err_ollama_model"))
		}
		endpoint := cmp.Or(p.Host, os.Getenv("OLLAMA_HOST"), ollama.DefaultEndpoint)
		return ollama.NewOllamaTranslator(endpoint, model, ""), nil
	case "google":
		pm := proxy.NewManager(p.Proxies, nil)
		pm.UsePublicSources()
		return google.NewGoogleTranslator(pm), nil
	}
	return nil, errors.New(i18n.T("tui_err_unknown_provider", p.ID))
}

func defaultMemory() (*memory.TranslationMemory, error) {
	path, err := paths.MemoryDBPath()
	if err != nil {
		return nil, fmt.Errorf("%s: %w", i18n.T("tui_err_memory_path"), err)
	}
	mem, err := memory.New(path)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", i18n.T("tui_err_memory_open", path), err)
	}
	return mem, nil
}

func (m *Model) memoryOnly() bool { return m.selectedProvider == len(providerIDs)-1 }

// extractorOptions repassa só os campos do extrator preenchidos (a chave do DeepL fica de fora).
func (m *Model) extractorOptions() map[string]string {
	if m.detectedGame == nil {
		return nil
	}
	opts := map[string]string{}
	for _, f := range job.ExtractorSchema(string(m.detectedGame.Engine)).Fields {
		if v := strings.TrimSpace(m.fieldValues[f.Key]); v != "" {
			opts[f.Key] = v
		}
	}
	return opts
}

// startJob monta o job com o estado da tela Configurar e o dispara em segundo plano.
func (m *Model) startJob() (tea.Cmd, error) {
	if !m.hasGame() {
		return nil, errors.New(i18n.T("tui_no_game_detected", m.gamePath))
	}
	opts := m.extractorOptions()
	if err := formats.ValidateOptions(opts); err != nil {
		return nil, err
	}
	id := providerIDs[m.selectedProvider]
	cfg := job.Config{
		GamePath:      m.gamePath,
		ProviderName:  "google",
		SourceLang:    m.sourceLang,
		TargetLang:    m.targetLang,
		Incremental:   m.incremental,
		EnableProxies: m.enableProxies,
		BackupEnabled: true,
		MemoryOnly:    m.memoryOnly(),

		ExtractorOptions: opts,
	}
	if id != "" {
		cfg.ProviderName = id
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	var prov provider.Provider
	if !cfg.MemoryOnly {
		var err error
		if prov, err = m.newProvider(ProviderParams{
			ID: id, APIKey: m.fieldValues["api_key"], Model: strings.TrimSpace(m.fieldValues["ollama_model"]),
			Host: strings.TrimSpace(m.fieldValues["ollama_host"]), Proxies: m.enableProxies,
		}); err != nil {
			return nil, err
		}
	}
	mem, err := m.openMemory()
	if err != nil {
		return nil, err
	}

	bus := event.NewBus()
	j := job.NewJob(cfg, bus, mem, prov)
	ctx, cancel := context.WithCancel(context.Background())
	ch := bus.SubscribeEvents(4096)

	m.job, m.mem, m.cancel, m.eventChan = j, mem, cancel, ch
	m.snapshot, m.runErr, m.paused, m.phaseMsg, m.throttleMsg, m.statusMsg = j.Snapshot(), nil, false, "", "", ""

	run := func() tea.Msg { return runDoneMsg{job: j, err: j.Run(ctx)} }
	return tea.Batch(run, waitEvent(ch)), nil
}

func waitEvent(ch <-chan event.Envelope) tea.Cmd {
	return func() tea.Msg {
		env, ok := <-ch
		if !ok {
			return nil
		}
		return eventMsg{ch: ch, env: env}
	}
}

func (m *Model) applyEvent(env event.Envelope) {
	switch e := env.Data.(type) {
	case event.PhaseChangedEvent:
		m.phaseMsg = e.Message
	case event.ProviderThrottledEvent:
		m.throttleMsg = e.Message
	}
}

func (m *Model) handleEvent(msg eventMsg) tea.Cmd {
	if msg.ch != m.eventChan {
		return nil
	}
	m.applyEvent(msg.env)
	closed := false
	for !closed {
		select {
		case env, ok := <-msg.ch:
			if !ok {
				closed = true
				break
			}
			m.applyEvent(env)
		default:
			m.snapshot = m.job.Snapshot()
			return waitEvent(msg.ch)
		}
	}
	m.snapshot = m.job.Snapshot()
	return nil
}

func (m *Model) handleRunDone(msg runDoneMsg) {
	if msg.job != m.job {
		return
	}
	m.snapshot = m.job.Snapshot()
	m.runErr = msg.err
	m.finished = true
	m.paused = false
	if m.cancel != nil {
		m.cancel()
	}
	m.job.Bus().UnsubscribeEvents(m.eventChan)
	m.eventChan = nil
	if m.view == ViewTranslating {
		m.view = ViewResult
	}
}

// stopJob cancela o job em andamento e libera a memória; chamado ao sair ou trocar de jogo.
func (m *Model) stopJob() {
	if m.cancel != nil {
		m.cancel()
	}
	if m.job != nil {
		m.job.Cancel()
	}
	if m.mem != nil {
		_ = m.mem.Close()
		m.mem = nil
	}
}

func (m *Model) togglePause() {
	if m.job == nil || m.finished || m.snapshot.Phase != event.PhaseTranslating {
		return
	}
	if m.paused {
		m.job.Resume()
	} else {
		m.job.Pause()
	}
	m.paused = !m.paused
}

// runErrorText traduz o erro do job para a tela de resultado.
func runErrorText(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, context.Canceled):
		return i18n.T("tui_run_canceled")
	case errors.Is(err, job.ErrRateLimited):
		return i18n.T("tui_run_rate_limited")
	case errors.Is(err, provider.ErrAuthFailed):
		return i18n.T("tui_run_auth")
	case errors.Is(err, provider.ErrUnavailable), errors.Is(err, provider.ErrCircuitBreakerOpen):
		return i18n.T("tui_run_unavailable", err.Error())
	}
	return err.Error()
}
