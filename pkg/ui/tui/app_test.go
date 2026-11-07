package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BGLuis/slime-rpg/pkg/detector"
	"github.com/BGLuis/slime-rpg/pkg/formats"
	"github.com/BGLuis/slime-rpg/pkg/i18n"
	"github.com/BGLuis/slime-rpg/pkg/job"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestModel_StepTransitions(t *testing.T) {
	game, _ := writeGame(t)
	m := NewModel(game)
	if m.view != ViewOpenGame {
		t.Fatalf("expected initial view to be ViewOpenGame, got %v", m.view)
	}

	// Step 1 -> Step 2
	newM, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = newM.(*Model)
	if m.view != ViewConfigure {
		t.Fatalf("expected view ViewConfigure after Enter, got %v", m.view)
	}

	// Esc returns to Step 1
	newM, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = newM.(*Model)
	if m.view != ViewOpenGame {
		t.Fatalf("expected view ViewOpenGame after Esc, got %v", m.view)
	}

	// Sem jogo detectado na pasta, Enter em Configurar não inicia nada.
	m.setGame(t.TempDir(), nil)
	m.view = ViewConfigure
	newM, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = newM.(*Model)
	if m.view != ViewConfigure || m.statusMsg == "" || m.job != nil {
		t.Fatalf("esperava permanecer em Configurar com aviso, view=%v status=%q", m.view, m.statusMsg)
	}

	// Revisar volta ao Resultado com Esc
	m.view = ViewReviewing
	newM, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = newM.(*Model)
	if m.view != ViewResult {
		t.Fatalf("expected view ViewResult, got %v", m.view)
	}

	// From Step 4, 'n' starts a new game (returns to Step 1)
	newM, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	m = newM.(*Model)
	if m.view != ViewOpenGame {
		t.Fatalf("expected view ViewOpenGame, got %v", m.view)
	}
}

func TestModel_KeyInteractions(t *testing.T) {
	game, _ := writeGame(t)
	m := NewModel(game)

	// Navigation in Step 1
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	if m.selectedFolderIndex != 1 {
		t.Errorf("expected selectedFolderIndex 1, got %d", m.selectedFolderIndex)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyUp})
	if m.selectedFolderIndex != 0 {
		t.Errorf("expected selectedFolderIndex 0, got %d", m.selectedFolderIndex)
	}

	// Move to Step 2
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	// Toggle incremental with Space
	origInc := m.incremental
	m.Update(tea.KeyMsg{Type: tea.KeySpace})
	if m.incremental == origInc {
		t.Errorf("expected incremental to toggle on space")
	}

	// Change provider with Left/Right
	m.Update(tea.KeyMsg{Type: tea.KeyRight})
	if m.selectedProvider != 1 {
		t.Errorf("expected selectedProvider 1, got %d", m.selectedProvider)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyLeft})
	if m.selectedProvider != 0 {
		t.Errorf("expected selectedProvider 0, got %d", m.selectedProvider)
	}

	// Move to Reviewing
	m.view = ViewReviewing
	m.Update(tea.KeyMsg{Type: tea.KeyTab})
	if m.selectedFilter != 1 {
		t.Errorf("expected selectedFilter 1 after Tab, got %d", m.selectedFilter)
	}
}

func TestModel_Rendering(t *testing.T) {
	m := NewModel(".")

	views := []ViewState{ViewOpenGame, ViewConfigure, ViewTranslating, ViewReviewing, ViewResult}
	for _, v := range views {
		m.view = v
		out := m.View()
		if len(out) == 0 {
			t.Errorf("view %v rendered empty string", v)
		}
		if !strings.Contains(out, "Slime") {
			t.Errorf("view %v output missing 'Slime'", v)
		}
	}

	// Test quit rendering
	m.quitting = true
	quitOut := m.View()
	if !strings.Contains(quitOut, "finalizado") {
		t.Errorf("expected quit message in view output, got: %s", quitOut)
	}
}

func TestModel_QuitKeys(t *testing.T) {
	m := NewModel(".")

	// 'q' in ViewOpenGame quits
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if !m.quitting || cmd == nil {
		t.Fatalf("expected quitting true and quit cmd on 'q'")
	}

	// reset
	m.quitting = false
	// ctrl+c always quits
	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if !m.quitting || cmd == nil {
		t.Fatalf("expected quitting true and quit cmd on ctrl+c")
	}
}

func TestConfigureShowsExtractorOptionsAndAcceptsInput(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.csv"), []byte("id,text\n1,hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := NewModel(dir)
	m.view = ViewConfigure
	if !strings.Contains(m.View(), "Delimitador") {
		t.Fatal("a tela Configurar devia listar as opções do CSV detectado")
	}
	for !m.editingText() {
		m.Update(tea.KeyMsg{Type: tea.KeyTab})
	}
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n', 'o', 't', 'e'}})
	if got := m.fieldValues["csv.columns"]; got != "note" {
		t.Errorf("csv.columns = %q, esperava note", got)
	}
}

// Na tela Configurar, ↑↓ escolhem a linha e ←→ mudam só o valor dela; num campo de
// texto as letras são digitadas e as setas não podem trocar o serviço por baixo do usuário.
func TestConfigureNavigatesByFocusedRow(t *testing.T) {
	game, _ := writeGame(t)
	m := NewModel(game)
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	key := func(k tea.KeyType) { m.Update(tea.KeyMsg{Type: k}) }

	key(tea.KeyDown) // origem
	key(tea.KeyLeft)
	if m.sourceLang != "it" {
		t.Errorf("← na origem devia voltar ja→it, deu %q", m.sourceLang)
	}
	key(tea.KeyDown) // destino
	key(tea.KeyRight)
	if m.targetLang != "en" || m.sourceLang != "it" {
		t.Errorf("→ no destino devia mudar só o destino: origem=%q destino=%q", m.sourceLang, m.targetLang)
	}

	key(tea.KeyUp)
	key(tea.KeyUp) // serviço
	key(tea.KeyLeft)
	if m.providers[m.selectedProvider] != i18n.T("tui_provider_memory") {
		t.Errorf("← no primeiro serviço devia dar a volta até o último, deu %q", m.providers[m.selectedProvider])
	}
	key(tea.KeyLeft) // Ollama: ganha os campos de modelo e endereço
	for !m.editingText() {
		key(tea.KeyDown)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("sdx")})
	key(tea.KeyRight)
	if got := m.fieldValues["ollama_model"]; got != "sdx" || providerIDs[m.selectedProvider] != "ollama" {
		t.Errorf("campo em foco devia receber as letras e manter o Ollama: valor=%q serviço=%d", got, m.selectedProvider)
	}
	if m.targetLang != "en" || m.enableProxies {
		t.Error("atalhos s/d/x não podem agir enquanto se digita num campo")
	}

	key(tea.KeyUp) // incremental: proxies não aparece fora do Google
	if m.focusedRow().kind != rowIncremental {
		t.Fatalf("esperava foco em incremental, está em %v", m.focusedRow().kind)
	}
}

func TestConfigureBoolFieldTogglesInsteadOfTyping(t *testing.T) {
	m := NewModel(t.TempDir())
	m.setGame(t.TempDir(), &detector.DetectionResult{Engine: "wolfrpg"})
	m.view = ViewConfigure
	for m.focusedRow().field.Key != formats.OptWolfPreserveTit {
		m.Update(tea.KeyMsg{Type: tea.KeyDown})
	}
	m.Update(tea.KeyMsg{Type: tea.KeySpace})
	if got := m.extractorOptions()[formats.OptWolfPreserveTit]; got != "false" {
		t.Errorf("espaço devia desligar o padrão true, opção = %q", got)
	}
}

// Nenhuma linha pode passar da largura do terminal, nem com texto japonês (2 colunas por letra).
func TestViewsFitTerminalWidth(t *testing.T) {
	game, _ := writeGame(t)
	for _, width := range []int{60, 80, 120} {
		m := NewModel(game)
		m.Update(tea.WindowSizeMsg{Width: width, Height: 40})
		m.selectedProvider = 2
		m.detectedGame.GameName = strings.Repeat("勇者の旅", 10)
		m.snapshot.Files = []job.FileProgress{{Path: strings.Repeat("マップ/", 20), Status: "error", Message: strings.Repeat("失敗", 30)}}
		m.reviewItems = []job.ReviewItem{{Source: strings.Repeat("こんにちは", 30), Target: "olá", File: "Map001.json"}}
		for _, v := range []ViewState{ViewOpenGame, ViewConfigure, ViewTranslating, ViewReviewing, ViewResult} {
			m.view = v
			lines := strings.Split(m.View(), "\n")
			for i, l := range lines {
				if w := ansi.StringWidth(l); w > width {
					t.Errorf("largura %d, view %v, linha %d com %d colunas: %q", width, v, i, w, ansi.Strip(l))
				}
			}
			if v != ViewConfigure {
				continue
			}
			// Bordas das caixas alinhadas: toda linha da caixa tem a mesma largura que o topo.
			var boxW int
			for _, l := range lines {
				l = strings.TrimSpace(ansi.Strip(l))
				switch {
				case strings.HasPrefix(l, "╭"):
					boxW = ansi.StringWidth(strings.SplitAfter(l, "╮")[0])
				case boxW > 0 && strings.HasPrefix(l, "│"):
					if got := ansi.StringWidth(l[:strings.Index(l[3:], "│")+3+len("│")]); got != boxW {
						t.Errorf("largura %d: linha da caixa com %d colunas, topo com %d: %q", width, got, boxW, l)
					}
				}
			}
		}
	}
}

func TestViewKeepsHeaderWhenTerminalIsShort(t *testing.T) {
	game, _ := writeGame(t)
	m := NewModel(game)
	m.Update(tea.WindowSizeMsg{Width: 70, Height: 20})
	m.view = ViewConfigure
	m.selectedProvider = 2
	out := m.View()
	if n := len(strings.Split(out, "\n")); n > 20 {
		t.Errorf("view com %d linhas num terminal de 20", n)
	}
	if !strings.Contains(out, "Slime") || !strings.Contains(out, "enter") {
		t.Error("cabeçalho e rodapé precisam continuar visíveis")
	}
}
