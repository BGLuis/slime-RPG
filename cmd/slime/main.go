package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/BGLuis/slime-rpg/pkg/detector"
	"github.com/BGLuis/slime-rpg/pkg/engine"
	"github.com/BGLuis/slime-rpg/pkg/event"
	"github.com/BGLuis/slime-rpg/pkg/filenames"
	"github.com/BGLuis/slime-rpg/pkg/formats"
	"github.com/BGLuis/slime-rpg/pkg/i18n"
	"github.com/BGLuis/slime-rpg/pkg/job"
	"github.com/BGLuis/slime-rpg/pkg/memory"
	"github.com/BGLuis/slime-rpg/pkg/metadata"
	"github.com/BGLuis/slime-rpg/pkg/paths"
	"github.com/BGLuis/slime-rpg/pkg/provider"
	"github.com/BGLuis/slime-rpg/pkg/provider/deepl"
	"github.com/BGLuis/slime-rpg/pkg/provider/google"
	"github.com/BGLuis/slime-rpg/pkg/provider/ollama"
	"github.com/BGLuis/slime-rpg/pkg/provider/proxy"
	"github.com/BGLuis/slime-rpg/pkg/settings"
	"github.com/BGLuis/slime-rpg/pkg/ui/desktop"
	"github.com/BGLuis/slime-rpg/pkg/ui/tui"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-isatty"
)

var (
	version = "0.1.0-dev"
	commit  = "none"
	date    = "unknown"
)

func printUsage() {
	fmt.Print(i18n.T("usage", version))
}

func main() {
	args, langFlag := extractLangFlag(os.Args[1:])
	i18n.Set(settings.ResolveLanguage(langFlag, os.Getenv))
	cmd := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		cmd, args = args[0], args[1:]
	}

	switch cmd {
	case "version":
		printVersion()
		return
	case "help":
		printUsage()
		return
	case "verify":
		os.Exit(runVerify(args))
	case "adopt":
		os.Exit(runAdopt(args))
	case "filenames":
		os.Exit(runFilenames(args))
	case "tm":
		os.Exit(runTM(args))
	case "list":
		printLists()
		return
	case "lang":
		os.Exit(runLang(args))
	case "", "translate":
	default:
		fmt.Fprint(os.Stderr, i18n.T("unknown_command", cmd))
		printUsage()
		os.Exit(2)
	}
	if len(args) > 0 && (args[0] == "--version" || args[0] == "-v") {
		printVersion()
		return
	}

	var o translateOptions
	fs := flag.NewFlagSet("slime", flag.ExitOnError)
	fs.Usage = printUsage
	o.register(fs)
	_ = fs.Parse(args)

	if o.gui {
		if err := desktop.Run(o.input); err != nil {
			fmt.Fprint(os.Stderr, i18n.T("err_gui_start", err))
			os.Exit(1)
		}
		return
	}

	isTerminal := isatty.IsTerminal(os.Stdin.Fd()) || isatty.IsCygwinTerminal(os.Stdin.Fd())
	if (o.tui || (isTerminal && len(args) == 0 && cmd == "")) && !o.dryRun {
		p := tea.NewProgram(tui.NewModel(o.input), tea.WithAltScreen())
		if _, err := p.Run(); err != nil {
			fmt.Fprint(os.Stderr, i18n.T("err_tui_start", err))
			os.Exit(1)
		}
		return
	}

	os.Exit(runHeadless(o))
}

func printVersion() {
	fmt.Printf("Slime RPG (Go) v%s (commit: %s, date: %s)\n", version, commit, date)
}

type translateOptions struct {
	input, output, translator, source, target string
	synopsis, model                           string
	noBackup, memoryOnly, switchEngine        bool
	incremental, noProxy, dryRun, jsonOutput  bool
	tui, gui, review                          bool
	extractor                                 string
	extOpts                                   optionList
}

// optionList acumula flags --opt chave=valor repetidas.
type optionList map[string]string

func (l *optionList) String() string { return fmt.Sprint(map[string]string(*l)) }

func (l *optionList) Set(v string) error {
	key, value, ok := strings.Cut(v, "=")
	if !ok || strings.TrimSpace(key) == "" {
		return errors.New(i18n.T("err_option_format", v))
	}
	if *l == nil {
		*l = optionList{}
	}
	(*l)[strings.TrimSpace(key)] = value
	return nil
}

func (o *translateOptions) register(fs *flag.FlagSet) {
	str := func(p *string, def, usage string, names ...string) {
		for _, n := range names {
			fs.StringVar(p, n, def, usage)
		}
	}
	boolean := func(p *bool, def bool, usage string, names ...string) {
		for _, n := range names {
			fs.BoolVar(p, n, def, usage)
		}
	}
	str(&o.input, ".", "Pasta do jogo ou arquivo", "i", "input")
	str(&o.output, "", "Pasta de destino", "o", "output")
	str(&o.extractor, "", "Força o extrator: rpgmaker, wolfrpg, csv, json", "e", "extractor")
	fs.Var(&o.extOpts, "opt", "Opção do extrator chave=valor (repetível)")
	str(&o.translator, "google", "Provedor de tradução", "t", "translator", "service")
	str(&o.source, "ja", "Idioma de origem", "s", "source", "from")
	str(&o.target, "pt", "Idioma de destino", "d", "target", "to")
	str(&o.synopsis, "", "Sinopse do jogo", "synopsis", "sinopse")
	str(&o.model, os.Getenv("OLLAMA_MODEL"), "Modelo do Ollama", "model")
	boolean(&o.noBackup, false, "Não criar backup", "no-backup")
	boolean(&o.memoryOnly, false, "Só a Memória de Tradução", "memory-only", "offline")
	boolean(&o.switchEngine, false, "Trocar de provedor", "switch-engine")
	boolean(&o.incremental, true, "Só arquivos alterados", "incremental")
	boolean(&o.noProxy, !proxy.EnabledByEnv(), "Sem pool de proxies", "no-proxy")
	boolean(&o.dryRun, false, "Estimativa sem alterar arquivos", "dry-run")
	boolean(&o.jsonOutput, false, "Saída em JSON Lines", "json")
	boolean(&o.review, false, "Pausa para revisar a pasta de processamento antes de gravar", "review")
	boolean(&o.tui, false, "Forçar Terminal UI", "tui")
	boolean(&o.gui, false, "Forçar Desktop UI", "gui")
}

func newProvider(o translateOptions) (provider.Provider, error) {
	switch memory.NormalizeEngine(o.translator) {
	case "deepl":
		key := os.Getenv("DEEPL_API_KEY")
		if key == "" {
			return nil, errors.New(i18n.T("err_deepl_key"))
		}
		return deepl.NewDeepLTranslator(key), nil
	case "ollama":
		endpoint := os.Getenv("OLLAMA_HOST")
		if endpoint == "" {
			endpoint = ollama.DefaultEndpoint
		}
		return ollama.NewOllamaTranslator(endpoint, o.model, o.synopsis), nil
	case "google":
		pm := proxy.NewManager(!o.noProxy, nil)
		pm.UsePublicSources()
		return google.NewGoogleTranslator(pm), nil
	}
	return nil, errors.New(i18n.T("err_unknown_translator", o.translator))
}

func runHeadless(o translateOptions) int {
	if err := formats.ValidateOptions(o.extOpts); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	det, err := detector.Detect(o.input)
	if err != nil {
		fmt.Fprint(os.Stderr, i18n.T("err_detect", o.input, err))
		return 1
	}
	if o.extractor != "" {
		if det, err = formats.ForceExtractor(det, o.extractor); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
	}
	if !o.jsonOutput {
		fmt.Print(i18n.T("detected_game", det.GameName, det.Engine))
		fmt.Print(i18n.T("detected_data", det.DataDir, det.FileCount))
		fmt.Print(i18n.T("detected_service", o.translator, o.source, o.target))
	}

	var prov provider.Provider
	if !o.memoryOnly {
		if prov, err = newProvider(o); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
	}

	mem, err := openMemory()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer mem.Close()

	bus := event.NewBus()
	j := job.NewJob(job.Config{
		GamePath:      o.input,
		ProviderName:  memory.NormalizeEngine(o.translator),
		SourceLang:    o.source,
		TargetLang:    o.target,
		Incremental:   o.incremental,
		EnableProxies: !o.noProxy,
		BackupEnabled: !o.noBackup,
		OutputDir:     o.output,
		MemoryOnly:    o.memoryOnly,
		SwitchEngine:  o.switchEngine,
		Review:        reviewHook(o),

		Extractor:        o.extractor,
		ExtractorOptions: o.extOpts,
	}, bus, mem, prov)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	if o.dryRun {
		est, err := j.Estimate(ctx)
		if err != nil {
			fmt.Fprint(os.Stderr, i18n.T("err_estimate", err))
			return 1
		}
		if o.jsonOutput {
			_ = json.NewEncoder(os.Stdout).Encode(est)
		} else {
			fmt.Println(i18n.T("estimate_title"))
			fmt.Print(i18n.T("estimate_files", est.FilesTotal, est.FilesToProcess, est.SegmentsTotal, est.SegmentsCached))
			fmt.Print(i18n.T("estimate_time", est.EstimatedDuration, est.BackupPath))
		}
		return 0
	}

	done := make(chan struct{})
	ch := bus.SubscribeEvents(4096)
	go func() {
		defer close(done)
		for env := range ch {
			if o.jsonOutput {
				data, _ := json.Marshal(env)
				fmt.Println(string(data))
			} else {
				printEvent(env)
			}
			if env.Type == event.EventJobFinished {
				return
			}
		}
	}()

	runErr := j.Run(ctx)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
	}
	bus.UnsubscribeEvents(ch)

	if runErr != nil {
		fmt.Fprint(os.Stderr, i18n.T("err_run", runErr))
	}
	snap := j.Snapshot()
	if !o.jsonOutput {
		printSummary(snap)
	}
	return snap.ExitCode
}

// reviewHook devolve a pausa de revisão manual, ativa só com --review e stdin em
// terminal para não travar automações nem a saída --json.
func reviewHook(o translateOptions) func(string) bool {
	if !o.review || !(isatty.IsTerminal(os.Stdin.Fd()) || isatty.IsCygwinTerminal(os.Stdin.Fd())) {
		return nil
	}
	return func(dir string) bool {
		return confirmReview(os.Stdin, os.Stderr, dir)
	}
}

func confirmReview(in io.Reader, out io.Writer, dir string) bool {
	fmt.Fprint(out, i18n.T("review_prompt", dir))
	line, _ := bufio.NewReader(in).ReadString('\n')
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer != "n" && answer != "no" && answer != "nao" && answer != "não"
}

func printEvent(env event.Envelope) {
	switch e := env.Data.(type) {
	case event.PhaseChangedEvent:
		fmt.Print(i18n.T("event_phase", e.Phase, e.Message))
	case event.FileStatusEvent:
		if e.Status == event.StatusWaiting || e.Status == event.StatusProcessing {
			return
		}
		fmt.Printf("[%s] %s - %s\n", strings.ToUpper(string(e.Status)), e.File, e.Message)
	case event.ProviderThrottledEvent:
		fmt.Print(i18n.T("event_warn", e.Provider, e.Message))
	case event.JobFinishedEvent:
		fmt.Print(i18n.T("event_end", e.FilesComplete, e.FilesPartial, e.FilesError, e.Duration.Round(time.Millisecond)))
	}
}

// printSummary repete no fim os arquivos com falha ou tradução parcial, como o Python.
func printSummary(snap job.Snapshot) {
	var failed, partial []job.FileProgress
	for _, f := range snap.Files {
		switch f.Status {
		case event.StatusError:
			failed = append(failed, f)
		case event.StatusPartial:
			partial = append(partial, f)
		}
	}
	if len(failed) > 0 {
		fmt.Println(i18n.T("summary_failed"))
		for _, f := range failed {
			fmt.Printf("  ✗ %s: %s\n", f.Path, f.Message)
		}
	}
	if len(partial) > 0 {
		fmt.Println(i18n.T("summary_partial"))
		for _, f := range partial {
			fmt.Printf("  • %s: %s\n", f.Path, f.Message)
		}
	}
	fmt.Print(i18n.T("summary_line", snap.SegmentsTotal, snap.SegmentsCached, snap.SegmentsNew, snap.BackupPath, snap.MetaPath))
}

func openMemory() (*memory.TranslationMemory, error) {
	memPath, err := paths.MemoryDBPath()
	if err != nil {
		return nil, errors.New(i18n.T("err_memory_resolve", err))
	}
	mem, err := memory.New(memPath)
	if err != nil {
		return nil, errors.New(i18n.T("err_memory_open", memPath, err))
	}
	return mem, nil
}

// runVerify equivale ao --verify-game da versão Python.
func runVerify(args []string) int {
	fs := flag.NewFlagSet("verify", flag.ExitOnError)
	original := fs.String("original", "", i18n.T("flag_original"))
	dir, rest := splitPositional(args)
	_ = fs.Parse(rest)
	if dir == "" {
		dir = "."
	}

	report, err := metadata.Verify(dir, *original, nil)
	if errors.Is(err, metadata.ErrNoMeta) {
		fmt.Print(i18n.T("verify_no_meta", dir))
		return 1
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	for _, r := range report {
		if r.State != metadata.StateIntact && r.State != metadata.StateUnchanged {
			fmt.Printf("  [%s] %s\n", r.State, r.File)
		}
	}
	fmt.Print(i18n.T("verify_summary", formatSummary(metadata.Summarize(report))))
	return 0
}

// runAdopt equivale ao --adopt da versão Python: registra uma tradução feita
// fora do Slime comparando-a com a pasta original.
func runAdopt(args []string) int {
	fs := flag.NewFlagSet("adopt", flag.ExitOnError)
	original := fs.String("original", "", i18n.T("flag_original"))
	var extractor, source, target string
	for _, n := range []string{"e", "extractor"} {
		fs.StringVar(&extractor, n, "", i18n.T("flag_extractor"))
	}
	for _, n := range []string{"s", "source"} {
		fs.StringVar(&source, n, "", i18n.T("flag_source"))
	}
	for _, n := range []string{"d", "target"} {
		fs.StringVar(&target, n, "", i18n.T("flag_target"))
	}
	dir, rest := splitPositional(args)
	_ = fs.Parse(rest)

	name, exts := formats.ExtensionsFor(extractor)
	if dir == "" || *original == "" || exts == nil {
		fmt.Fprintln(os.Stderr, i18n.T("adopt_usage"))
		return 1
	}
	for _, folder := range []string{dir, *original} {
		if info, err := os.Stat(folder); err != nil || !info.IsDir() {
			fmt.Fprint(os.Stderr, i18n.T("adopt_no_dir", folder))
			return 1
		}
	}

	meta := metadata.Build(dir, *original, metadata.BuildOptions{
		Origin: metadata.OriginLegacy, Extractor: name, LangSource: source, LangTarget: target,
		Extensions: exts, Previous: metadata.Load(dir),
	})
	path, err := metadata.Save(dir, meta)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	states := make(map[string]int)
	for _, f := range meta.Files {
		states[f.Status]++
	}
	fmt.Print(i18n.T("adopt_saved", path, len(meta.Files), formatSummary(states)))
	return 0
}

// splitPositional separa o primeiro argumento posicional das flags, já que o
// pacote flag para de ler flags no primeiro argumento posicional.
func splitPositional(args []string) (string, []string) {
	for i, a := range args {
		if strings.HasPrefix(a, "-") {
			continue
		}
		if i > 0 && strings.HasPrefix(args[i-1], "-") && !strings.Contains(args[i-1], "=") {
			continue
		}
		return a, append(append([]string{}, args[:i]...), args[i+1:]...)
	}
	return "", args
}

func formatSummary(summary map[string]int) string {
	keys := make([]string, 0, len(summary))
	for k := range summary {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = fmt.Sprintf("%s=%d", k, summary[k])
	}
	return strings.Join(parts, ", ")
}

// runTM reúne as ferramentas de memória da versão Python (scripts/tm_*.py).
func runTM(args []string) int {
	if len(args) > 0 {
		switch args[0] {
		case "repair":
			return runTMRepair(args[1:])
		case "migrate-legacy":
			return runTMMigrateLegacy(args[1:])
		}
	}
	mem, err := openMemory()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer mem.Close()
	ctx := context.Background()

	sub := ""
	if len(args) > 0 {
		sub, args = args[0], args[1:]
	}
	fs := flag.NewFlagSet("tm "+sub, flag.ExitOnError)
	src := fs.String("src", "", "Idioma de origem")
	tgt := fs.String("tgt", "", "Idioma de destino")
	out := fs.String("out", "", "Arquivo .tmx de saída")
	file := fs.String("file", "", "Arquivo .tmx a importar")
	engineName := fs.String("engine", "human", "Rótulo da origem das traduções importadas")
	status := fs.String("status", "", "Status (export: filtro separado por vírgula; import: reviewed, locked ou machine)")
	source := fs.String("source", "", "Termo de origem a travar")
	target := fs.String("target", "", "Tradução fixa do termo")
	list := fs.Bool("list", false, "Lista os termos travados")
	_ = fs.Parse(args)

	switch sub {
	case "":
		count, err := mem.Count(ctx)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		path, _ := paths.MemoryDBPath()
		fmt.Print(i18n.T("tm_count", path, count))
		return 0

	case "export":
		if *src == "" || *tgt == "" || *out == "" {
			fmt.Fprintln(os.Stderr, i18n.T("tm_export_need"))
			return 2
		}
		var statuses []memory.Status
		for _, st := range strings.Split(*status, ",") {
			if st = strings.TrimSpace(st); st != "" {
				statuses = append(statuses, memory.Status(st))
			}
		}
		pairs, err := mem.Pairs(ctx, *src, *tgt, statuses...)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		f, err := os.Create(*out)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		defer f.Close()
		if err := memory.WriteTMX(f, *src, *tgt, pairs); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		fmt.Print(i18n.T("tm_export_done", len(pairs), *out))
		return 0

	case "import":
		st := memory.Status(*status)
		if st == "" {
			st = memory.StatusReviewed
		}
		if *file == "" || (st != memory.StatusReviewed && st != memory.StatusLocked && st != memory.StatusMachine) {
			fmt.Fprintln(os.Stderr, i18n.T("tm_import_need"))
			return 2
		}
		f, err := os.Open(*file)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		defer f.Close()
		units, skipped, err := memory.ReadTMX(f)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		entries := make([]memory.Entry, len(units))
		for i, u := range units {
			entries[i] = memory.Entry{SrcLang: u.SrcLang, TgtLang: u.TgtLang, SourceText: u.Source, TargetText: u.Target, Engine: *engineName, Status: st}
		}
		if err := mem.StoreBatch(ctx, entries); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		fmt.Print(i18n.T("tm_import_done", len(units), st, skipped))
		return 0

	case "glossary":
		if *src == "" || *tgt == "" {
			fmt.Fprintln(os.Stderr, i18n.T("tm_gloss_need"))
			return 2
		}
		if *list {
			pairs, err := mem.Pairs(ctx, *src, *tgt, memory.StatusLocked)
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				return 1
			}
			for _, p := range pairs {
				fmt.Printf("%s -> %s\n", p.Source, p.Target)
			}
			return 0
		}
		if *source == "" || *target == "" {
			fmt.Fprintln(os.Stderr, i18n.T("tm_gloss_terms"))
			return 2
		}
		if err := mem.LockTerm(ctx, *src, *tgt, "glossary", *source, *target); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		fmt.Print(i18n.T("tm_locked", *source, *target))
		return 0
	}
	fmt.Fprint(os.Stderr, i18n.T("tm_unknown_sub", sub))
	return 2
}

func memoryDBFlag(fs *flag.FlagSet) *string {
	return fs.String("db", "", "Caminho do memory.db (padrão: SLIME_MEMORY_DB ou o banco do usuário)")
}

func resolveDB(flagValue string) (string, error) {
	if flagValue != "" {
		return filepath.Abs(flagValue)
	}
	return paths.MemoryDBPath()
}

// runTMRepair corrige a caixa de códigos de controle e limpa lixo do memory.db;
// sem --apply apenas simula.
func runTMRepair(args []string) int {
	fs := flag.NewFlagSet("tm repair", flag.ExitOnError)
	db := memoryDBFlag(fs)
	apply := fs.Bool("apply", false, "Aplica as correções (cria backup antes); sem a flag é dry-run")
	_ = fs.Parse(args)
	path, err := resolveDB(*db)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	rep, err := memory.Repair(context.Background(), path, *apply)
	if err != nil {
		fmt.Fprintln(os.Stderr, "tm repair:", err)
		return 1
	}
	verb := map[bool]string{false: i18n.T("tm_verb_would"), true: i18n.T("tm_verb_applied")}[*apply]
	fmt.Print(i18n.T("tm_repair_db", path))
	labels := map[string]string{
		memory.RepairCase:   i18n.T("tm_repair_case"),
		memory.RepairName:   i18n.T("tm_repair_name"),
		memory.RepairXTOK:   i18n.T("tm_repair_xtok"),
		memory.RepairEngine: i18n.T("tm_repair_engine"),
	}
	for _, c := range memory.RepairCategories {
		cat := rep.Categories[c]
		fmt.Print(i18n.T("tm_repair_rows", labels[c], cat.Rows, verb))
		for _, ex := range cat.Examples {
			fmt.Printf("  #%d  %q\n", ex.ID, ex.Before)
			if ex.After != "" {
				fmt.Printf("   -> %q\n", ex.After)
			}
		}
	}
	fmt.Print(i18n.T("tm_repair_prot", verb, rep.ProtectedRows))
	if rep.Applied {
		fmt.Print(i18n.T("tm_repair_backup", rep.BackupPath))
	} else {
		fmt.Println(i18n.T("tm_repair_dry"))
	}
	return 0
}

// runTMMigrateLegacy equivale ao scripts/migrate_cache_to_db.py da versão Python.
func runTMMigrateLegacy(args []string) int {
	fs := flag.NewFlagSet("tm migrate-legacy", flag.ExitOnError)
	db := memoryDBFlag(fs)
	dir := fs.String("dir", "", "Pasta de caches legados (<engine>/cache_<src>_<tgt>.json)")
	_ = fs.Parse(args)
	if *dir == "" {
		fmt.Fprintln(os.Stderr, i18n.T("tm_migrate_need"))
		return 2
	}
	path, err := resolveDB(*db)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	mem, err := memory.New(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer mem.Close()
	rep, err := memory.MigrateLegacy(context.Background(), mem, *dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "tm migrate-legacy:", err)
		return 1
	}
	fmt.Print(i18n.T("tm_migrate_done", rep.Files, rep.Imported, path))
	fmt.Print(i18n.T("tm_migrate_skip", rep.SkippedNull, rep.Identity))
	return 0
}

// runFilenames equivale ao --mode filenames da versão Python. Sem -o, as cópias
// com nome traduzido ficam na própria pasta, ao lado dos originais.
func runFilenames(args []string) int {
	var o translateOptions
	var keepOriginal bool
	fs := flag.NewFlagSet("filenames", flag.ExitOnError)
	o.register(fs)
	fs.BoolVar(&keepOriginal, "keep-original", false, "Também copia o arquivo com o nome original")
	_ = fs.Parse(args)

	prov, err := newProvider(o)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	output := o.output
	if output == "" {
		output = o.input
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	res, err := filenames.Process(ctx, engine.New(prov, engine.Options{}), o.input, output, filenames.Options{
		SrcLang: o.source, TgtLang: o.target, KeepOriginal: keepOriginal,
		Log: func(msg string) { fmt.Println(msg) },
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if keepOriginal {
		fmt.Print(i18n.T("filenames_kept", res.CopiedOriginals))
	}
	return 0
}

func printLists() {
	fmt.Println(i18n.T("list_extractors"))
	for _, e := range []string{"rpgmaker", "wolfrpg", "csv", "json"} {
		fmt.Println("  -", i18n.T("extractor_"+e))
	}
	fmt.Println(i18n.T("list_translators"))
	for _, t := range []string{"google", "deepl", "ollama"} {
		fmt.Println("  -", t)
	}
	fmt.Println(i18n.T("list_languages"))
	for _, l := range i18n.TranslationLanguages {
		fmt.Printf("  - %s: %s\n", l, i18n.T("lang_"+l))
	}
}

// extractLangFlag remove --lang/-lang de args (global a todos os comandos) e
// devolve o valor informado.
func extractLangFlag(args []string) ([]string, string) {
	rest := make([]string, 0, len(args))
	lang := ""
	for i := 0; i < len(args); i++ {
		a := strings.TrimLeft(args[i], "-")
		switch {
		case strings.HasPrefix(args[i], "-") && a == "lang" && i+1 < len(args):
			lang = args[i+1]
			i++
		case strings.HasPrefix(args[i], "-") && strings.HasPrefix(a, "lang="):
			lang = strings.TrimPrefix(a, "lang=")
		default:
			rest = append(rest, args[i])
		}
	}
	return rest, lang
}

// runLang mostra o idioma da interface ou, com um código, grava a preferência
// em settings.db (compartilhado com a versão Python).
func runLang(args []string) int {
	path, err := paths.SettingsDBPath()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if len(args) == 0 {
		cur := i18n.Current()
		fmt.Print(i18n.T("lang_current", cur, i18n.Names[cur]))
		return 0
	}
	l, ok := i18n.Normalize(args[0])
	if !ok {
		fmt.Fprint(os.Stderr, i18n.T("lang_invalid", args[0]))
		return 2
	}
	if err := settings.SaveLanguage(path, l); err != nil {
		fmt.Fprint(os.Stderr, i18n.T("lang_save_fail", err))
		return 1
	}
	i18n.Set(l)
	fmt.Print(i18n.T("lang_saved", l))
	return 0
}
