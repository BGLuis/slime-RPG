package tui

import (
	"context"
	"fmt"

	"github.com/BGLuis/slime-rpg/pkg/event"
	"github.com/BGLuis/slime-rpg/pkg/job"
	"github.com/BGLuis/slime-rpg/pkg/ui/theme"
	tea "github.com/charmbracelet/bubbletea"
)

// languages repete os idiomas de `slime list` (código, nome), com "auto" no fim.
var languages = [][2]string{
	{"pt", "Português"}, {"en", "Inglês"}, {"es", "Espanhol"}, {"fr", "Francês"},
	{"it", "Italiano"}, {"ja", "Japonês"}, {"auto", "Automático (Detectar)"},
}

var ollamaFields = []job.ConfigSchemaField{
	{Key: "ollama_model", Label: "Modelo do Ollama", Type: "text", Help: "Vazio usa a variável OLLAMA_MODEL."},
	{Key: "ollama_host", Label: "Endereço do Ollama", Type: "text", Help: "Vazio usa OLLAMA_HOST ou o endereço local."},
}

func nextLang(code string) string {
	for i, l := range languages {
		if l[0] == code {
			return languages[(i+1)%len(languages)][0]
		}
	}
	return languages[0][0]
}

func langLabel(code string) string {
	for _, l := range languages {
		if l[0] == code {
			return fmt.Sprintf("%s (%s)", l[1], code)
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
	if key == m.estimateKey || m.fieldFocus >= 0 || !m.hasGame() {
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

func (m *Model) renderEstimate() []string {
	row := func(s string) string { return fmt.Sprintf("│ %-32s │", truncate(s, 32)) }
	blank := row("")
	lines := []string{"╭─ Estimativa ─────────────────────╮"}

	switch e := m.estimate; {
	case m.estimating:
		lines = append(lines, row("Calculando…"))
	case m.estimateErr != "":
		lines = append(lines, row("Não foi possível estimar:"), row(m.estimateErr))
	case e != nil:
		toTranslate := max(e.SegmentsTotal-e.SegmentsCached, 0)
		pct := 0.0
		if e.SegmentsTotal > 0 {
			pct = float64(e.SegmentsCached) / float64(e.SegmentsTotal)
		}
		lines = append(lines,
			row(fmt.Sprintf("%d textos para traduzir", toTranslate)),
			blank,
			"│ "+theme.RenderProgressBar(pct, 32)+" │",
			row(fmt.Sprintf("%d de %d já na memória (%d%%)", e.SegmentsCached, e.SegmentsTotal, int(pct*100))),
			blank,
			row(fmt.Sprintf("Arquivos  %d de %d", e.FilesToProcess, e.FilesTotal)),
			row("Backup    "+truncate(e.BackupPath, 22)),
		)
	}
	for len(lines) < 16 {
		lines = append(lines, blank)
	}
	return append(lines, "╰──────────────────────────────────╯")
}
