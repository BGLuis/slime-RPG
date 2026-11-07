package tui

import (
	"fmt"
	"strings"

	"github.com/BGLuis/slime-rpg/pkg/event"
	"github.com/BGLuis/slime-rpg/pkg/job"
	"github.com/BGLuis/slime-rpg/pkg/ui/theme"
)

const maxFileRows = 8

func checkbox(on bool) string {
	if on {
		return "[x]"
	}
	return "[ ]"
}

func (m *Model) providerLine() string {
	parts := make([]string, len(m.providers))
	for i, p := range m.providers {
		parts[i] = "  " + p
		if i == m.selectedProvider {
			parts[i] = "› " + p
		}
	}
	return fmt.Sprintf("%-52s", strings.Join(parts, " "))
}

func (m *Model) renderTranslating() string {
	snap := m.snapshot
	var s strings.Builder

	mode := m.providers[m.selectedProvider]
	s.WriteString("   " + theme.StyleHeader.Render("Traduzindo "+snap.GameName) +
		fmt.Sprintf("   %s · %s → %s\n", mode, snap.SourceLang, snap.TargetLang))
	phase := string(snap.Phase)
	if m.paused {
		phase += " (pausado)"
	}
	s.WriteString("     Fase: " + phase + "\n\n")

	pct := 0.0
	if snap.SegmentsTotal > 0 {
		pct = float64(snap.SegmentsDone) / float64(snap.SegmentsTotal)
	}
	s.WriteString("   " + theme.RenderProgressBar(pct, 70) + fmt.Sprintf("  %d%%\n", int(pct*100)))
	s.WriteString(fmt.Sprintf("   %d de %d segmentos · %d da memória · %d traduzidos agora · %d de %d arquivos\n\n",
		snap.SegmentsDone, snap.SegmentsTotal, snap.SegmentsCached, snap.SegmentsNew, snap.FilesDone, snap.FilesTotal))

	if m.throttleMsg != "" {
		s.WriteString("    " + theme.StyleTagWarning.Render(theme.SymWarning+" "+m.throttleMsg) + "\n\n")
	}

	s.WriteString(fmt.Sprintf("   ╭─ Arquivos · %d ─────────────────────────────────────────────────────────────────────────────╮\n", len(snap.Files)))
	rows := orderedFiles(snap.Files)
	for i, f := range rows {
		if i == maxFileRows {
			s.WriteString(fmt.Sprintf("   │ + %-92s│\n", fmt.Sprintf("%d outros arquivos", len(rows)-maxFileRows)))
			break
		}
		s.WriteString(fmt.Sprintf("   │ %-38s %-9s %-28s %5d/%-6d     │\n",
			truncate(f.Path, 38), f.Status, truncate(f.Message, 28), f.Done, f.Total))
	}
	s.WriteString("   ╰──────────────────────────────────────────────────────────────────────────────────────────────╯\n")
	return s.String()
}

// orderedFiles põe os arquivos em andamento ou com problema à frente dos demais.
func orderedFiles(files []job.FileProgress) []job.FileProgress {
	rank := func(st event.StatusType) int {
		switch st {
		case event.StatusProcessing:
			return 0
		case event.StatusError, event.StatusPartial:
			return 1
		case event.StatusWaiting:
			return 2
		}
		return 3
	}
	out := make([]job.FileProgress, 0, len(files))
	for r := 0; r <= 3; r++ {
		for _, f := range files {
			if rank(f.Status) == r {
				out = append(out, f)
			}
		}
	}
	return out
}

func (m *Model) renderResult() string {
	snap := m.snapshot
	var s strings.Builder
	title := "Jogo traduzido"
	if m.runErr != nil {
		title = "Tradução não concluída"
	}
	s.WriteString("     " + theme.StyleTitle.Render(title) + "\n")
	if m.runErr != nil {
		s.WriteString("   " + theme.StyleTagError.Render(theme.SymCross+" "+runErrorText(m.runErr)) + "\n\n")
	} else {
		s.WriteString(fmt.Sprintf("   %s foi processado em %s.\n\n", snap.GameName, snap.Duration.Round(10_000_000)))
	}

	counts := map[event.StatusType]int{}
	for _, f := range snap.Files {
		counts[f.Status]++
	}
	s.WriteString(fmt.Sprintf("   %s %d completos   %s %d parciais   %s %d com erro   %s %d novos na memória   %s %d da memória\n\n",
		theme.SymBullet, counts[event.StatusSuccess]+counts[event.StatusCached],
		theme.SymBullet, counts[event.StatusPartial],
		theme.SymBullet, counts[event.StatusError],
		theme.SymBullet, snap.SegmentsNew,
		theme.SymBullet, snap.SegmentsCached))

	listed := 0
	for _, f := range snap.Files {
		if f.Status != event.StatusError && f.Status != event.StatusPartial {
			continue
		}
		if listed == maxFileRows {
			break
		}
		listed++
		tag := theme.StyleTagWarning.Render(theme.SymWarning + " parcial")
		if f.Status == event.StatusError {
			tag = theme.StyleTagError.Render(theme.SymCross + " erro   ")
		}
		s.WriteString(fmt.Sprintf("    %s %s: %s\n", tag, truncate(f.Path, 40), truncate(f.Message, 60)))
	}
	if listed > 0 {
		s.WriteString("\n")
	}

	s.WriteString("   ╭─ Onde ficou cada coisa ──────────────────────────────────────────────────────────────────────╮\n")
	s.WriteString(fmt.Sprintf("   │ %-20s  %-71s│\n", "Jogo traduzido", truncate(snap.GamePath, 70)))
	s.WriteString(fmt.Sprintf("   │ %-20s  %-71s│\n", "Original (backup)", truncate(snap.BackupPath, 70)))
	s.WriteString(fmt.Sprintf("   │ %-20s  %-71s│\n", "Registro", truncate(snap.MetaPath, 70)))
	s.WriteString("   ╰──────────────────────────────────────────────────────────────────────────────────────────────╯\n\n")
	s.WriteString(fmt.Sprintf("   Código de saída %d · na CLI, use --json para automação\n", snap.ExitCode))
	return s.String()
}

func (m *Model) renderReviewing() string {
	var s strings.Builder
	s.WriteString("   " + theme.StyleHeader.Render("Revisar traduções") + "   correções vão para a Memória de Tradução\n")

	chips := ""
	for i, f := range m.filterLabels {
		if i == m.selectedFilter {
			chips += fmt.Sprintf("[%s]  ", f)
		} else {
			chips += f + "  "
		}
	}
	s.WriteString("   " + chips + "\n\n")

	if len(m.reviewItems) == 0 {
		s.WriteString("   Nenhum segmento neste filtro.\n")
		return s.String()
	}

	const window = 8
	start := max(0, min(m.selectedSegmentIdx-window/2, len(m.reviewItems)-window))
	for i := start; i < min(start+window, len(m.reviewItems)); i++ {
		it := m.reviewItems[i]
		cursor := "  "
		if i == m.selectedSegmentIdx {
			cursor = "› "
		}
		tag := theme.SymBullet + " ok     "
		if it.Status == "partial" {
			tag = theme.StyleTagWarning.Render(theme.SymBullet + " parcial")
		}
		s.WriteString(fmt.Sprintf("   %s%s  %s\n", cursor, tag, truncate(oneLine(it.Source), 80)))
	}

	it := m.reviewItems[m.selectedSegmentIdx]
	s.WriteString("\n   Arquivo:   " + truncate(it.File, 85) + "\n")
	s.WriteString("   Origem:    " + truncate(oneLine(it.Source), 85) + "\n")
	if m.editing {
		s.WriteString("   Tradução:  " + truncate(oneLine(m.editBuf), 85) + "▌\n")
	} else {
		s.WriteString("   Tradução:  " + truncate(oneLine(it.Target), 85) + "\n")
	}
	return s.String()
}

func oneLine(s string) string { return strings.ReplaceAll(s, "\n", "⏎") }
