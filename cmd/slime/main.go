package main

import (
	"database/sql"
	"flag"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	_ "github.com/egoist/mygo"
	_ "modernc.org/sqlite"
)

var (
	version = "0.1.0-dev"
	commit  = "none"
	date    = "unknown"
)

type model struct {
	quitting bool
}

func (m model) Init() tea.Cmd {
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c", "esc", "enter":
			m.quitting = true
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m model) View() string {
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#04B575")).
		Padding(1, 2).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#04B575"))

	subStyle := lipgloss.NewStyle().
		Faint(true).
		MarginTop(1)

	content := fmt.Sprintf("Slime RPG (Go Edition) v%s\nFramework Desktop: MyGo | TUI: Bubble Tea\n\nPressione [Enter] ou [Q] para sair.", version)
	return titleStyle.Render(content) + "\n" + subStyle.Render("Portabilidade iniciada com sucesso.")
}

func main() {
	versionFlag := flag.Bool("version", false, "Exibe a versão do Slime RPG")
	tuiFlag := flag.Bool("tui", false, "Executa o modo Terminal User Interface (TUI)")
	flag.Parse()

	if *versionFlag {
		fmt.Printf("Slime RPG (Go) v%s (commit: %s, date: %s)\n", version, commit, date)
		return
	}

	// Verifica se o driver sqlite pure-go está funcionando
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Erro ao inicializar SQLite: %v\n", err)
		os.Exit(1)
	}
	_ = db.Close()

	if *tuiFlag || len(os.Args) == 1 {
		p := tea.NewProgram(model{})
		if _, err := p.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "Erro ao iniciar TUI: %v\n", err)
			os.Exit(1)
		}
		return
	}

	fmt.Println("Slime RPG CLI inicializado. Use --help para opções.")
}
