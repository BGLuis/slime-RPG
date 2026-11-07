package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/BGLuis/slime-rpg/pkg/event"
	"github.com/BGLuis/slime-rpg/pkg/i18n"
	"github.com/BGLuis/slime-rpg/pkg/job"
	"github.com/BGLuis/slime-rpg/pkg/ui/theme"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func ollamaFields() []job.ConfigSchemaField {
	return []job.ConfigSchemaField{
		{Key: "ollama_model", Label: i18n.T("tui_ollama_model_label"), Type: "text", Help: i18n.T("tui_ollama_model_help")},
		{Key: "ollama_host", Label: i18n.T("tui_ollama_host_label"), Type: "text", Help: i18n.T("tui_ollama_host_help")},
	}
}

// stepLang anda delta posições na lista de idiomas, dando a volta nas pontas.
func stepLang(code string, delta int) string {
	langs := i18n.TranslationLanguages
	for i, l := range langs {
		if l == code {
			return langs[((i+delta)%len(langs)+len(langs))%len(langs)]
		}
	}
	return langs[0]
}

func langLabel(code string) string {
	for _, l := range i18n.TranslationLanguages {
		if l == code {
			return fmt.Sprintf("%s (%s)", i18n.T("lang_"+l), code)
		}
	}
	return code
}

type estimateMsg struct {
	seq int
	est *event.EstimateEvent
	err error
}

// currentEstimateKey resume o que influencia a estimativa.
func (m *Model) currentEstimateKey() string {
	return fmt.Sprint(m.gamePath, m.selectedProvider, m.sourceLang, m.targetLang, m.incremental, m.extractorOptions())
}

func (m *Model) enterConfigure() tea.Cmd {
	m.view = ViewConfigure
	m.estimateKey = ""
	return m.reestimate()
}

// reestimate dispara uma nova estimativa assíncrona se as opções mudaram e nenhum campo está em edição.
func (m *Model) reestimate() tea.Cmd {
	key := m.currentEstimateKey()
	if key == m.estimateKey || m.editingText() || !m.hasGame() {
		return nil
	}
	m.estimateKey = key
	m.estimateSeq++
	m.estimating, m.estimateErr = true, ""

	seq, openMemory := m.estimateSeq, m.openMemory
	cfg := job.Config{
		GamePath:         m.gamePath,
		ProviderName:     providerIDs[m.selectedProvider],
		SourceLang:       m.sourceLang,
		TargetLang:       m.targetLang,
		Incremental:      m.incremental,
		BackupEnabled:    true,
		MemoryOnly:       m.memoryOnly(),
		ExtractorOptions: m.extractorOptions(),
	}
	return func() tea.Msg {
		mem, err := openMemory()
		if err != nil {
			return estimateMsg{seq: seq, err: err}
		}
		defer mem.Close()
		// Sem provedor: a estimativa só precisa de arquivos e memória, e não deve tocar a rede.
		est, err := job.NewJob(cfg, nil, mem, nil).Estimate(context.Background())
		return estimateMsg{seq: seq, est: est, err: err}
	}
}

func (m *Model) handleEstimate(msg estimateMsg) {
	if msg.seq != m.estimateSeq {
		return
	}
	m.estimating = false
	m.estimate = msg.est
	if msg.err != nil {
		m.estimate, m.estimateErr = nil, msg.err.Error()
	}
}

// renderEstimate devolve o conteúdo do painel de estimativa com inner colunas úteis.
func (m *Model) renderEstimate(inner int) []string {
	switch e := m.estimate; {
	case m.estimating:
		return []string{i18n.T("tui_est_calc")}
	case m.estimateErr != "":
		return append([]string{i18n.T("tui_est_fail")}, wrapJoin(strings.Fields(m.estimateErr), " ", inner)...)
	case e != nil:
		toTranslate := max(e.SegmentsTotal-e.SegmentsCached, 0)
		pct := 0.0
		if e.SegmentsTotal > 0 {
			pct = float64(e.SegmentsCached) / float64(e.SegmentsTotal)
		}
		label := i18n.T("tui_est_backup")
		return []string{
			fmt.Sprintf("%d %s", toTranslate, i18n.T("ui_est_to_translate")),
			"",
			theme.RenderProgressBar(pct, inner),
			i18n.T("ui_est_cache", e.SegmentsCached, e.SegmentsTotal, int(pct*100)),
			"",
			i18n.T("tui_est_files", e.FilesToProcess, e.FilesTotal),
			label + tail(e.BackupPath, inner-ansi.StringWidth(label)),
		}
	}
	return nil
}
