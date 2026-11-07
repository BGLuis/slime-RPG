package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
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
	m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n', 'o', 't', 'e'}})
	if got := m.fieldValues["csv.columns"]; got != "note" {
		t.Errorf("csv.columns = %q, esperava note", got)
	}
}
