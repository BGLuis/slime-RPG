package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
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
	"github.com/BGLuis/slime-rpg/pkg/job"
	"github.com/BGLuis/slime-rpg/pkg/memory"
	"github.com/BGLuis/slime-rpg/pkg/metadata"
	"github.com/BGLuis/slime-rpg/pkg/paths"
	"github.com/BGLuis/slime-rpg/pkg/provider"
	"github.com/BGLuis/slime-rpg/pkg/provider/deepl"
	"github.com/BGLuis/slime-rpg/pkg/provider/google"
	"github.com/BGLuis/slime-rpg/pkg/provider/ollama"
	"github.com/BGLuis/slime-rpg/pkg/provider/proxy"
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

// languages são os idiomas oferecidos pela versão Python.
var languages = [][2]string{
	{"pt", "Português"}, {"en", "Inglês"}, {"es", "Espanhol"}, {"fr", "Francês"},
	{"it", "Italiano"}, {"ja", "Japonês"}, {"auto", "Automático (Detectar)"},
}

func printUsage() {
	fmt.Printf(`Slime RPG (Go Edition) v%s

USO:
  slime [comando] [opções]

COMANDOS:
  translate       Extrai, traduz e reinjeta textos no jogo (padrão)
  verify          Confere a pasta traduzida contra o .slime/meta.json
  adopt           Registra o metadado de um jogo já traduzido
  filenames       Traduz os nomes dos arquivos de uma pasta
  tm              Memória de Tradução: contagem, export/import TMX e glossário
  list            Lista extratores, tradutores e idiomas suportados
  version         Exibe informações detalhadas de versão e build

OPÇÕES DE TRADUÇÃO:
  -i, --input        Pasta raiz do jogo ou arquivo (padrão: pasta atual)
  -o, --output       Pasta de destino (padrão: a própria entrada)
  -t, --translator   Provedor: google, deepl, ollama (padrão: google)
  -s, --source       Idioma de origem (padrão: ja)
  -d, --target       Idioma de destino (padrão: pt)
  --synopsis         Sinopse do jogo para dar contexto ao Ollama
  --model            Modelo do Ollama (padrão: $OLLAMA_MODEL ou qwen2.5)
  --no-backup        Não criar o backup <pasta>-<origem>
  --memory-only      Aplica só a Memória de Tradução, sem rede (alias: --offline)
  --switch-engine    Ignora traduções automáticas de outros provedores na memória
  --incremental      Traduz só arquivos novos ou alterados (padrão: true)
  --no-proxy         Não usar o pool de proxies quando o Google bloquear o IP
  --dry-run          Calcula a estimativa pré-voo sem alterar arquivos
  --json             Emite eventos em JSON Lines para automação
  --tui / --gui      Força a interface de terminal ou desktop
  -h, --help         Exibe esta mensagem

MEMÓRIA DE TRADUÇÃO:
  slime tm                                              # total de entradas
  slime tm export --src ja --tgt pt --out f.tmx [--status reviewed,locked]
  slime tm import --file f.tmx [--engine human] [--status reviewed|locked|machine]
  slime tm glossary --src ja --tgt pt --source "ルイ" --target "Rui"
  slime tm glossary --src ja --tgt pt --list
  slime tm repair [--db memory.db] [--apply]            # dry-run por padrão; --apply faz backup e grava
  slime tm migrate-legacy --dir cache [--db memory.db]  # importa cache/<engine>/cache_<src>_<tgt>.json

NOMES DE ARQUIVO:
  slime filenames -i <pasta> [-o <saída>] [-t google -s ja -d pt] [--keep-original]

VERIFICAÇÃO E ADOÇÃO:
  slime verify <pasta-traduzida> [--original <pasta>]
  slime adopt <pasta-traduzida> --original <pasta> -e <extrator> [-s en -d pt]

EXEMPLOS:
  slime                                   # TUI interativa na pasta atual
  slime --gui                             # Janela desktop
  slime translate -i ./MeuJogo -d pt      # Tradução headless
  slime -i ./MeuJogo -t deepl --no-backup
`, version)
}

func main() {
	args := os.Args[1:]
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
	case "", "translate":
	default:
		fmt.Fprintf(os.Stderr, "Comando desconhecido: %s\n\n", cmd)
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
			fmt.Fprintf(os.Stderr, "Erro ao iniciar interface desktop: %v\n", err)
			os.Exit(1)
		}
		return
	}

	isTerminal := isatty.IsTerminal(os.Stdin.Fd()) || isatty.IsCygwinTerminal(os.Stdin.Fd())
	if (o.tui || (isTerminal && len(args) == 0 && cmd == "")) && !o.dryRun {
		p := tea.NewProgram(tui.NewModel(o.input), tea.WithAltScreen())
		if _, err := p.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "Erro ao iniciar TUI: %v\n", err)
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
	tui, gui                                  bool
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
	boolean(&o.tui, false, "Forçar Terminal UI", "tui")
	boolean(&o.gui, false, "Forçar Desktop UI", "gui")
}

func newProvider(o translateOptions) (provider.Provider, error) {
	switch memory.NormalizeEngine(o.translator) {
	case "deepl":
		key := os.Getenv("DEEPL_API_KEY")
		if key == "" {
			return nil, errors.New("defina DEEPL_API_KEY para usar o DeepL")
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
	return nil, fmt.Errorf("tradutor desconhecido: %s (use google, deepl ou ollama)", o.translator)
}

func runHeadless(o translateOptions) int {
	det, err := detector.Detect(o.input)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Erro ao detectar jogo em %s: %v\n", o.input, err)
		return 1
	}
	if !o.jsonOutput {
		fmt.Printf("Jogo detectado: %s (%s)\n", det.GameName, det.Engine)
		fmt.Printf("Pasta de dados: %s (%d arquivos)\n", det.DataDir, det.FileCount)
		fmt.Printf("Serviço: %s | Idiomas: %s → %s\n", o.translator, o.source, o.target)
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
	}, bus, mem, prov)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	if o.dryRun {
		est, err := j.Estimate(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Erro ao calcular estimativa: %v\n", err)
			return 1
		}
		if o.jsonOutput {
			_ = json.NewEncoder(os.Stdout).Encode(est)
		} else {
			fmt.Println("\n--- Estimativa Pré-Voo ---")
			fmt.Printf("Arquivos: %d (a processar: %d) | Segmentos: %d (na memória: %d)\n",
				est.FilesTotal, est.FilesToProcess, est.SegmentsTotal, est.SegmentsCached)
			fmt.Printf("Tempo estimado: %s | Backup: %s\n", est.EstimatedDuration, est.BackupPath)
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
		fmt.Fprintf(os.Stderr, "Erro durante a tradução: %v\n", runErr)
	}
	snap := j.Snapshot()
	if !o.jsonOutput {
		printSummary(snap)
	}
	return snap.ExitCode
}

func printEvent(env event.Envelope) {
	switch e := env.Data.(type) {
	case event.PhaseChangedEvent:
		fmt.Printf("[FASE] %s: %s\n", e.Phase, e.Message)
	case event.FileStatusEvent:
		if e.Status == event.StatusWaiting || e.Status == event.StatusProcessing {
			return
		}
		fmt.Printf("[%s] %s - %s\n", strings.ToUpper(string(e.Status)), e.File, e.Message)
	case event.ProviderThrottledEvent:
		fmt.Printf("[AVISO] %s: %s\n", e.Provider, e.Message)
	case event.JobFinishedEvent:
		fmt.Printf("[FIM] %d concluído(s), %d parcial(is), %d com erro em %s\n",
			e.FilesComplete, e.FilesPartial, e.FilesError, e.Duration.Round(time.Millisecond))
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
		fmt.Println("--- Arquivos com falha de processamento ---")
		for _, f := range failed {
			fmt.Printf("  ✗ %s: %s\n", f.Path, f.Message)
		}
	}
	if len(partial) > 0 {
		fmt.Println("--- Arquivos com tradução parcial (<100%) ---")
		for _, f := range partial {
			fmt.Printf("  • %s: %s\n", f.Path, f.Message)
		}
	}
	fmt.Printf("Segmentos: %d (memória: %d, novos: %d) | Backup: %s | Metadado: %s\n",
		snap.SegmentsTotal, snap.SegmentsCached, snap.SegmentsNew, snap.BackupPath, snap.MetaPath)
}

func openMemory() (*memory.TranslationMemory, error) {
	memPath, err := paths.MemoryDBPath()
	if err != nil {
		return nil, fmt.Errorf("erro ao resolver banco de memória: %w", err)
	}
	mem, err := memory.New(memPath)
	if err != nil {
		return nil, fmt.Errorf("erro ao abrir banco de memória %s: %w", memPath, err)
	}
	return mem, nil
}

// runVerify equivale ao --verify-game da versão Python.
func runVerify(args []string) int {
	fs := flag.NewFlagSet("verify", flag.ExitOnError)
	original := fs.String("original", "", "Pasta com os arquivos originais")
	dir, rest := splitPositional(args)
	_ = fs.Parse(rest)
	if dir == "" {
		dir = "."
	}

	report, err := metadata.Verify(dir, *original, nil)
	if errors.Is(err, metadata.ErrNoMeta) {
		fmt.Printf("Sem metadado em %s. Use 'slime adopt' para registrar o jogo.\n", dir)
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
	fmt.Printf("Resumo: %s\n", formatSummary(metadata.Summarize(report)))
	return 0
}

// runAdopt equivale ao --adopt da versão Python: registra uma tradução feita
// fora do Slime comparando-a com a pasta original.
func runAdopt(args []string) int {
	fs := flag.NewFlagSet("adopt", flag.ExitOnError)
	original := fs.String("original", "", "Pasta com os arquivos originais")
	var extractor, source, target string
	for _, n := range []string{"e", "extractor"} {
		fs.StringVar(&extractor, n, "", "Extrator: rpgmaker, wolfrpg, csv, json")
	}
	for _, n := range []string{"s", "source"} {
		fs.StringVar(&source, n, "", "Idioma de origem")
	}
	for _, n := range []string{"d", "target"} {
		fs.StringVar(&target, n, "", "Idioma de destino")
	}
	dir, rest := splitPositional(args)
	_ = fs.Parse(rest)

	name, exts := formats.ExtensionsFor(extractor)
	if dir == "" || *original == "" || exts == nil {
		fmt.Fprintln(os.Stderr, "adopt exige a pasta traduzida, --original e -e/--extractor (rpgmaker, wolfrpg, csv, json).")
		return 1
	}
	for _, folder := range []string{dir, *original} {
		if info, err := os.Stat(folder); err != nil || !info.IsDir() {
			fmt.Fprintf(os.Stderr, "Pasta não existe: %s\n", folder)
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
	fmt.Printf("Metadado gravado em %s\n  %d arquivo(s) registrados: %s\n", path, len(meta.Files), formatSummary(states))
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
		fmt.Printf("Memória de Tradução (%s): %d entradas registradas.\n", path, count)
		return 0

	case "export":
		if *src == "" || *tgt == "" || *out == "" {
			fmt.Fprintln(os.Stderr, "tm export exige --src, --tgt e --out")
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
		fmt.Printf("%d segmentos exportados para %s\n", len(pairs), *out)
		return 0

	case "import":
		st := memory.Status(*status)
		if st == "" {
			st = memory.StatusReviewed
		}
		if *file == "" || (st != memory.StatusReviewed && st != memory.StatusLocked && st != memory.StatusMachine) {
			fmt.Fprintln(os.Stderr, "tm import exige --file e --status reviewed, locked ou machine")
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
		fmt.Printf("%d segmentos importados (status=%s), %d ignorados (incompletos)\n", len(units), st, skipped)
		return 0

	case "glossary":
		if *src == "" || *tgt == "" {
			fmt.Fprintln(os.Stderr, "tm glossary exige --src e --tgt")
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
			fmt.Fprintln(os.Stderr, "Use --source e --target para travar um termo, ou --list para listar.")
			return 2
		}
		if err := mem.LockTerm(ctx, *src, *tgt, "glossary", *source, *target); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		fmt.Printf("Travado: %q -> %q\n", *source, *target)
		return 0
	}
	fmt.Fprintf(os.Stderr, "Subcomando desconhecido: tm %s (use export, import ou glossary)\n", sub)
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
	verb := map[bool]string{false: "seriam afetadas", true: "afetadas"}[*apply]
	fmt.Printf("Banco: %s\n", path)
	labels := map[string]string{
		memory.RepairCase:   "Caixa de códigos de controle restaurada",
		memory.RepairName:   "Tags \\NAME[...] normalizadas",
		memory.RepairXTOK:   "Linhas com __XTOK_ removidas",
		memory.RepairEngine: "Linhas de engines de teste removidas",
	}
	for _, c := range memory.RepairCategories {
		cat := rep.Categories[c]
		fmt.Printf("%s: %d linhas %s\n", labels[c], cat.Rows, verb)
		for _, ex := range cat.Examples {
			fmt.Printf("  #%d  %q\n", ex.ID, ex.Before)
			if ex.After != "" {
				fmt.Printf("   -> %q\n", ex.After)
			}
		}
	}
	fmt.Printf("Linhas reviewed/locked %s: %d\n", verb, rep.ProtectedRows)
	if rep.Applied {
		fmt.Printf("Backup: %s\n", rep.BackupPath)
	} else {
		fmt.Println("Dry-run: nada foi alterado. Use --apply para gravar.")
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
		fmt.Fprintln(os.Stderr, "tm migrate-legacy exige --dir")
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
	fmt.Printf("%d arquivos, %d entradas importadas em %s\n", rep.Files, rep.Imported, path)
	fmt.Printf("Puladas (tradução nula): %d; com source == target: %d\n", rep.SkippedNull, rep.Identity)
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
		fmt.Printf("Originais mantidos: %d\n", res.CopiedOriginals)
	}
	return 0
}

func printLists() {
	fmt.Println("Extratores disponíveis:")
	for _, e := range []string{"rpgmaker (RPG Maker MV/MZ/VX Ace/XP)", "wolfrpg (Wolf RPG)", "csv (CSV)", "json (Json)"} {
		fmt.Println("  -", e)
	}
	fmt.Println("Tradutores disponíveis:")
	for _, t := range []string{"google", "deepl", "ollama"} {
		fmt.Println("  -", t)
	}
	fmt.Println("Idiomas suportados:")
	for _, l := range languages {
		fmt.Printf("  - %s: %s\n", l[0], l[1])
	}
}
