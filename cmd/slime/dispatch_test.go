package main

import (
	"flag"
	"testing"
)

func TestResolveDispatch(t *testing.T) {
	tests := []struct {
		name       string
		argv0      string
		cmd        string
		args       []string
		isTerminal bool
		wantMode   uiMode
		wantInput  string
	}{
		{
			name:       "slime sem args no terminal abre TUI",
			argv0:      "slime",
			cmd:        "",
			args:       nil,
			isTerminal: true,
			wantMode:   modeTUI,
			wantInput:  ".",
		},
		{
			name:       "slm sem args no terminal abre TUI",
			argv0:      "/usr/bin/slm",
			cmd:        "",
			args:       nil,
			isTerminal: true,
			wantMode:   modeTUI,
			wantInput:  ".",
		},
		{
			name:       "slime sem args fora do terminal (atalho desktop) abre GUI",
			argv0:      "slime",
			cmd:        "",
			args:       nil,
			isTerminal: false,
			wantMode:   modeGUI,
			wantInput:  ".",
		},
		{
			name:       "slime --gui sem pasta abre GUI no diretório atual",
			argv0:      "slime",
			cmd:        "",
			args:       []string{"--gui"},
			isTerminal: true,
			wantMode:   modeGUI,
			wantInput:  ".",
		},
		{
			name:       "slime --gui com pasta posicional (.desktop Exec=slime --gui %f) abre GUI na pasta",
			argv0:      "slime",
			cmd:        "",
			args:       []string{"--gui", "/jogos/MeuRPG"},
			isTerminal: false,
			wantMode:   modeGUI,
			wantInput:  "/jogos/MeuRPG",
		},
		{
			name:       "slime --gui -i vazio (.desktop legado sem pasta) abre GUI sem falhar",
			argv0:      "slime",
			cmd:        "",
			args:       []string{"--gui", "-i"},
			isTerminal: false,
			wantMode:   modeGUI,
			wantInput:  ".",
		},
		{
			name:       "atalho slime-gui abre GUI direto",
			argv0:      "/usr/bin/slime-gui",
			cmd:        "",
			args:       []string{"/jogos/OutroRPG"},
			isTerminal: true,
			wantMode:   modeGUI,
			wantInput:  "/jogos/OutroRPG",
		},
		{
			name:       "subcomando gui abre GUI",
			argv0:      "slime",
			cmd:        "gui",
			args:       []string{"/jogos/RPGMaker"},
			isTerminal: true,
			wantMode:   modeGUI,
			wantInput:  "/jogos/RPGMaker",
		},
		{
			name:       "subcomando tui abre TUI",
			argv0:      "slime",
			cmd:        "tui",
			args:       []string{"-i", "/jogos/WolfRPG"},
			isTerminal: true,
			wantMode:   modeTUI,
			wantInput:  "/jogos/WolfRPG",
		},
		{
			name:       "slime com vários parâmetros no terminal abre CLI headless",
			argv0:      "slime",
			cmd:        "",
			args:       []string{"-i", "/jogos/MeuRPG", "-s", "ja", "-d", "pt"},
			isTerminal: true,
			wantMode:   modeCLI,
			wantInput:  "/jogos/MeuRPG",
		},
		{
			name:       "slime translate sem flags adicionais roda CLI headless",
			argv0:      "slime",
			cmd:        "translate",
			args:       nil,
			isTerminal: true,
			wantMode:   modeCLI,
			wantInput:  ".",
		},
		{
			name:       "slime --dry-run no terminal roda CLI headless",
			argv0:      "slime",
			cmd:        "",
			args:       []string{"--dry-run"},
			isTerminal: true,
			wantMode:   modeCLI,
			wantInput:  ".",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mode, opts, err := resolveDispatch(tt.argv0, tt.cmd, tt.args, tt.isTerminal, flag.ContinueOnError)
			if err != nil {
				t.Fatalf("resolveDispatch retornou erro inesperado: %v", err)
			}
			if mode != tt.wantMode {
				t.Errorf("mode = %v, esperado %v", mode, tt.wantMode)
			}
			if opts.input != tt.wantInput {
				t.Errorf("opts.input = %q, esperado %q", opts.input, tt.wantInput)
			}
		})
	}
}
