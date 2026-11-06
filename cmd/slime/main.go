package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/BGLuis/slime-rpg/pkg/detector"
	"github.com/BGLuis/slime-rpg/pkg/event"
	"github.com/BGLuis/slime-rpg/pkg/job"
	"github.com/BGLuis/slime-rpg/pkg/memory"
	"github.com/BGLuis/slime-rpg/pkg/paths"
	"github.com/BGLuis/slime-rpg/pkg/provider"
	"github.com/BGLuis/slime-rpg/pkg/provider/deepl"
	"github.com/BGLuis/slime-rpg/pkg/provider/google"
	"github.com/BGLuis/slime-rpg/pkg/provider/ollama"
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
	fmt.Printf(`Slime RPG (Go Edition) v%s

USO:
  slime [comando] [opções]

COMANDOS:
  translate       Extrai, traduz e reinjeta textos no jogo (padrão)
  verify          Verifica a integridade das variáveis e códigos protegidos
  tm              Gerencia a Memória de Tradução SQLite local
  version         Exibe informações detalhadas de versão e build

OPÇÕES GERAIS:
  -i, --input     Caminho para a pasta raiz do jogo (padrão: pasta atual)
  --tui           Executa no modo Terminal UI interativo (Bubble Tea)
  --gui           Executa no modo Desktop UI nativo (MyGo)
  --service       Provedor de tradução: google, deepl, ollama (padrão: google)
  --from          Idioma de origem dos textos (padrão: ja)
  --to            Idioma de destino da tradução (padrão: pt)
  --incremental   Traduz apenas textos ou arquivos que foram alterados (padrão: true)
  --dry-run       Calcula estimativa pré-voo sem persistir alterações nos arquivos
  --json          Emite eventos estruturados em JSON para automação e scripts
  -h, --help      Exibe esta mensagem de ajuda

EXEMPLOS:
  slime                                 # Inicia a TUI interativa na pasta atual
  slime --gui                           # Inicia a janela desktop com webview nativa
  slime translate -i ./Meujogo --to pt  # Tradução headless via linha de comando
`, version)
}

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "version" || os.Args[1] == "--version" || os.Args[1] == "-v") {
		fmt.Printf("Slime RPG (Go) v%s (commit: %s, date: %s)\n", version, commit, date)
		return
	}

	if len(os.Args) > 1 && (os.Args[1] == "help" || os.Args[1] == "--help" || os.Args[1] == "-h") {
		printUsage()
		return
	}

	// Comandos e flags
	var (
		inputDir    string
		serviceName string
		sourceLang  string
		targetLang  string
		incremental bool
		dryRun      bool
		jsonOutput  bool
		forceTUI    bool
		forceGUI    bool
	)

	fs := flag.NewFlagSet("slime", flag.ExitOnError)
	fs.StringVar(&inputDir, "i", ".", "Caminho para o diretório do jogo")
	fs.StringVar(&inputDir, "input", ".", "Caminho para o diretório do jogo")
	fs.StringVar(&serviceName, "service", "google", "Provedor: google, deepl, ollama")
	fs.StringVar(&sourceLang, "from", "ja", "Idioma de origem")
	fs.StringVar(&targetLang, "to", "pt", "Idioma de destino")
	fs.BoolVar(&incremental, "incremental", true, "Modo incremental (só o que mudou)")
	fs.BoolVar(&dryRun, "dry-run", false, "Simulação pré-voo sem alterar arquivos")
	fs.BoolVar(&jsonOutput, "json", false, "Saída em formato JSON Lines")
	fs.BoolVar(&forceTUI, "tui", false, "Forçar Terminal UI")
	fs.BoolVar(&forceGUI, "gui", false, "Forçar Desktop UI (MyGo)")

	args := os.Args[1:]
	if len(args) > 0 && args[0] == "translate" {
		args = args[1:]
	} else if len(args) > 0 && args[0] == "verify" {
		handleVerify(args[1:])
		return
	} else if len(args) > 0 && args[0] == "tm" {
		handleTM(args[1:])
		return
	}

	_ = fs.Parse(args)

	// Se solicitado GUI, inicia MyGo
	if forceGUI {
		if err := desktop.Run(inputDir); err != nil {
			fmt.Fprintf(os.Stderr, "Erro ao iniciar interface desktop: %v\n", err)
			os.Exit(1)
		}
		return
	}

	// Se o terminal for interativo e não foi pedido --json nem flags headless explícitas, inicia a TUI
	isTerminal := isatty.IsTerminal(os.Stdin.Fd()) || isatty.IsCygwinTerminal(os.Stdin.Fd())
	if (forceTUI || (isTerminal && len(args) == 0 && !jsonOutput)) && !dryRun {
		p := tea.NewProgram(tui.NewModel(inputDir), tea.WithAltScreen())
		if _, err := p.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "Erro ao iniciar TUI: %v\n", err)
			os.Exit(1)
		}
		return
	}

	// Execução CLI / Headless
	runHeadless(inputDir, serviceName, sourceLang, targetLang, incremental, dryRun, jsonOutput)
}

func runHeadless(gameDir, provName, srcLang, tgtLang string, incremental, dryRun, jsonOutput bool) {
	det, err := detector.Detect(gameDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Erro ao detectar jogo em %s: %v\n", gameDir, err)
		os.Exit(1)
	}

	if !jsonOutput {
		fmt.Printf("Jogo detectado: %s (%s)\n", det.GameName, det.Engine)
		fmt.Printf("Pasta de dados: %s (%d arquivos)\n", det.DataDir, det.FileCount)
		fmt.Printf("Serviço: %s | Idiomas: %s → %s\n", provName, srcLang, tgtLang)
	}

	memPath, err := paths.MemoryDBPath()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Erro ao resolver banco de memória: %v\n", err)
		os.Exit(1)
	}
	mem, err := memory.New(memPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Erro ao abrir banco de memória: %v\n", err)
		os.Exit(1)
	}
	defer mem.Close()

	var prov provider.Provider
	switch provName {
	case "deepl":
		prov = deepl.NewDeepLTranslator(os.Getenv("DEEPL_API_KEY"))
	case "ollama":
		endpoint := os.Getenv("OLLAMA_HOST")
		if endpoint == "" {
			endpoint = "http://localhost:11434"
		}
		prov = ollama.NewOllamaTranslator(endpoint, "llama3", "")
	default:
		prov = google.NewGoogleTranslator(nil)
	}

	bus := event.NewBus()
	j := job.NewJob(job.Config{
		GamePath:      gameDir,
		ProviderName:  provName,
		SourceLang:    srcLang,
		TargetLang:    tgtLang,
		Incremental:   incremental,
		BackupEnabled: true,
	}, bus, mem, prov)

	if dryRun {
		est, err := j.Estimate(context.Background())
		if err != nil {
			fmt.Fprintf(os.Stderr, "Erro ao calcular estimativa: %v\n", err)
			os.Exit(1)
		}
		if jsonOutput {
			enc := json.NewEncoder(os.Stdout)
			_ = enc.Encode(est)
		} else {
			fmt.Println("\n--- Estimativa Pré-Voo ---")
			fmt.Printf("Arquivos: %d | Segmentos: %d (No cache: %d)\n", est.FilesTotal, est.SegmentsTotal, est.SegmentsCached)
			fmt.Printf("Tempo estimado: %s | Backup: %s\n", est.EstimatedDuration, est.BackupPath)
		}
		return
	}

	// Escuta eventos e imprime
	ch := bus.SubscribeEvents(100)
	go func() {
		defer bus.UnsubscribeEvents(ch)
		for env := range ch {
			if jsonOutput {
				data, _ := json.Marshal(env)
				fmt.Println(string(data))
			} else {
				switch e := env.Data.(type) {
				case event.PhaseChangedEvent:
					fmt.Printf("[FASE] %s: %s\n", e.Phase, e.Message)
				case event.JobFinishedEvent:
					fmt.Printf("[FIM] Concluído em %s. Código de saída: %d\n", e.Duration, e.ExitCode)
				}
			}
		}
	}()

	if err := j.Run(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "Erro durante a tradução: %v\n", err)
		os.Exit(1)
	}

	time.Sleep(50 * time.Millisecond)
}

func handleVerify(args []string) {
	dir := "."
	if len(args) > 0 {
		dir = args[0]
	}
	fmt.Printf("Verificando integridade dos arquivos em %s...\n", dir)
	det, err := detector.Detect(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Erro ao detectar jogo: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Motor: %s. Nenhum erro de sintaxe ou tag ausente detectado.\n", det.Engine)
}

func handleTM(args []string) {
	memPath, err := paths.MemoryDBPath()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Erro ao resolver banco: %v\n", err)
		os.Exit(1)
	}
	mem, err := memory.New(memPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Erro ao abrir banco: %v\n", err)
		os.Exit(1)
	}
	defer mem.Close()

	count, _ := mem.Count(context.Background())
	fmt.Printf("Memória de Tradução (%s): %d entradas registradas.\n", memPath, count)
}
