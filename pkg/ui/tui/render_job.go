package tui

import (
	"fmt"
	"strings"

	"github.com/BGLuis/slime-rpg/pkg/event"
	"github.com/BGLuis/slime-rpg/pkg/i18n"
	"github.com/BGLuis/slime-rpg/pkg/job"
	"github.com/BGLuis/slime-rpg/pkg/ui/theme"
	"github.com/charmbracelet/x/ansi"
)

const maxFileRows = 8

func checkbox(on bool) string {
	if on {
		return "[x]"
	}
	return "[ ]"
}

func (m *Model) renderTranslating() string {
	snap := m.snapshot
	w := m.contentWidth() - len(indent) - 1
	phase := string(snap.Phase)
	if m.paused {
		phase = i18n.T("tui_trans_paused", phase)
	} else if m.phaseMsg != "" {
		phase += " · " + truncate(m.phaseMsg, max(12, w-20))
	}
	mode := fmt.Sprintf("%s · %s → %s", m.providers[m.selectedProvider], snap.SourceLang, snap.TargetLang)
	lines := []string{
		theme.StyleHeader.Render(i18n.T("tui_trans_title", snap.GameName)) + "   " + mode,
		"  " + i18n.T("tui_trans_phase", phase),
		"",
	}

	pct := 0.0
	if snap.SegmentsTotal > 0 {
		pct = float64(snap.SegmentsDone) / float64(snap.SegmentsTotal)
	}
	progress := i18n.T("tui_trans_progress",
		snap.SegmentsDone, snap.SegmentsTotal, snap.SegmentsCached, snap.SegmentsNew, snap.FilesDone, snap.FilesTotal)
	lines = append(lines, theme.RenderProgressBar(pct, min(70, w-6))+fmt.Sprintf("  %d%%", int(pct*100)))
	lines = append(append(lines, wrapJoin(strings.Split(progress, " · "), " · ", w)...), "")
	if m.throttleMsg != "" {
		lines = append(lines, theme.StyleTagWarning.Render(theme.SymWarning+" "+truncate(m.throttleMsg, w-4)), "")
	}

	// Colunas: caminho | status | mensagem | feitos/total; a mensagem some em telas estreitas.
	inner := w - 4
	const statusW, countW = 11, 13
	pathW := inner - statusW - countW - 2
	msgW := 0
	if inner >= 70 {
		msgW = pathW * 2 / 5
		pathW -= msgW + 1
	}
	var rows []string
	files := orderedFiles(snap.Files)
	for i, f := range files {
		if i == maxFileRows {
			rows = append(rows, "+ "+i18n.T("tui_trans_more", len(files)-maxFileRows))
			break
		}
		row := fit(tail(f.Path, pathW), pathW) + " " + fit(string(f.Status), statusW-1) + " "
		if msgW > 0 {
			row += fit(f.Message, msgW) + " "
		}
		rows = append(rows, row+fmt.Sprintf("%*s", countW, fmt.Sprintf("%d/%d", f.Done, f.Total)))
	}
	lines = append(lines, box(i18n.T("tui_trans_files", len(snap.Files)), rows, w)...)
	return indentLines(lines)
}

// orderedFiles põe os arquivos em andamento, com problema ou recém-processados à frente dos demais.
func orderedFiles(files []job.FileProgress) []job.FileProgress {
	rank := func(st event.StatusType) int {
		switch st {
		case event.StatusProcessing:
			return 0
		case event.StatusError, event.StatusPartial:
			return 1
		case event.StatusSuccess, event.StatusCached:
			return 2
		case event.StatusWaiting:
			return 3
		}
		return 4
	}
	out := make([]job.FileProgress, 0, len(files))
	for r := 0; r <= 4; r++ {
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
	w := m.contentWidth() - len(indent) - 1
	title := i18n.T("tui_result_ok")
	if m.runErr != nil {
		title = i18n.T("tui_result_fail")
	}
	lines := []string{theme.StyleHeader.Render(title)}
	if m.runErr != nil {
		lines = append(lines, theme.StyleTagError.Render(theme.SymCross+" "+truncate(runErrorText(m.runErr), w-4)))
	} else {
		lines = append(lines, i18n.T("tui_result_done", snap.GameName, snap.Duration.Round(10_000_000)))
	}
	lines = append(lines, "")

	counts := map[event.StatusType]int{}
	for _, f := range snap.Files {
		counts[f.Status]++
	}
	// A linha de contagens é longa; quebra nos grupos "● n rótulo" em vez de cortar.
	summary := strings.TrimSpace(i18n.T("tui_result_counts",
		theme.SymBullet, counts[event.StatusSuccess]+counts[event.StatusCached],
		theme.SymBullet, counts[event.StatusPartial],
		theme.SymBullet, counts[event.StatusError],
		theme.SymBullet, snap.SegmentsNew,
		theme.SymBullet, snap.SegmentsCached))
	lines = append(append(lines, wrapJoin(strings.Split(summary, "   "), "   ", w)...), "")

	listed := 0
	for _, f := range snap.Files {
		if f.Status != event.StatusError && f.Status != event.StatusPartial {
			continue
		}
		if listed == maxFileRows {
			break
		}
		listed++
		tag := theme.StyleTagWarning.Render(theme.SymWarning + " " + i18n.T("tui_tag_partial"))
		if f.Status == event.StatusError {
			tag = theme.StyleTagError.Render(theme.SymCross + " " + i18n.T("tui_tag_error"))
		}
		lines = append(lines, truncate(" "+tag+" "+f.Path+": "+f.Message, w))
	}
	if listed > 0 {
		lines = append(lines, "")
	}

	labelW := min(20, (w-4)/3)
	where := func(label, path string) string { return fit(label, labelW) + "  " + tail(path, w-4-labelW-2) }
	lines = append(lines, box(i18n.T("ui_result_where"), []string{
		where(i18n.T("tui_result_ok"), snap.GamePath),
		where(i18n.T("tui_where_backup"), snap.BackupPath),
		where(i18n.T("tui_where_log"), snap.MetaPath),
	}, w)...)
	lines = append(lines, "", strings.TrimSpace(i18n.T("tui_result_exit", snap.ExitCode)))
	return indentLines(lines)
}

func (m *Model) renderReviewing() string {
	w := m.contentWidth() - len(indent) - 1
	lines := []string{truncate(theme.StyleHeader.Render(i18n.T("ui_review_title"))+"   "+i18n.T("tui_review_sub"), w)}

	chips := make([]string, len(m.filterLabels))
	for i, f := range m.filterLabels {
		chips[i] = " " + f + " "
		if i == m.selectedFilter {
			chips[i] = theme.StyleAccent.Render("[" + f + "]")
		}
	}
	lines = append(lines, strings.Join(chips, " "), "")

	if len(m.reviewItems) == 0 {
		return indentLines(append(lines, i18n.T("tui_review_empty")))
	}

	const window = 8
	start := max(0, min(m.selectedSegmentIdx-window/2, len(m.reviewItems)-window))
	for i := start; i < min(start+window, len(m.reviewItems)); i++ {
		it := m.reviewItems[i]
		cursor := "  "
		if i == m.selectedSegmentIdx {
			cursor = theme.StyleAccent.Render(theme.SymArrow + " ")
		}
		tag := theme.StyleMuted.Render(fit(theme.SymBullet+" ok", 10))
		if it.Status == "partial" {
			tag = theme.StyleTagWarning.Render(fit(theme.SymBullet+" "+i18n.T("tui_tag_partial"), 8))
		}
		lines = append(lines, cursor+tag+"  "+truncate(oneLine(it.Source), w-14))
	}

	it := m.reviewItems[m.selectedSegmentIdx]
	detail := func(label, value string) string { return label + truncate(value, w-ansi.StringWidth(label)) }
	lines = append(lines, "",
		detail(i18n.T("tui_review_file"), it.File),
		detail(i18n.T("tui_review_source"), oneLine(it.Source)))
	if m.editing {
		label := i18n.T("tui_review_target")
		lines = append(lines, label+tail(oneLine(m.editBuf), w-ansi.StringWidth(label)-1)+"▌")
	} else {
		lines = append(lines, detail(i18n.T("tui_review_target"), oneLine(it.Target)))
	}
	return indentLines(lines)
}

func oneLine(s string) string { return strings.ReplaceAll(s, "\n", "⏎") }
