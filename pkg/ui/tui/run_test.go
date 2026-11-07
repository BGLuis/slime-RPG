package tui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/BGLuis/slime-rpg/pkg/memory"
	"github.com/BGLuis/slime-rpg/pkg/provider"
	tea "github.com/charmbracelet/bubbletea"
)

const mapJSON = `{"events":[null,{"id":1,"pages":[{"list":[` +
	`{"code":401,"indent":0,"parameters":["こんにちは"]},` +
	`{"code":0,"indent":0,"parameters":[]}]}]}]}`

func writeGame(t *testing.T) (root, mapPath string) {
	t.Helper()
	root = filepath.Join(t.TempDir(), "Hero")
	data := filepath.Join(root, "www", "data")
	if err := os.MkdirAll(data, 0o755); err != nil {
		t.Fatal(err)
	}
	mapPath = filepath.Join(data, "Map001.json")
	for path, content := range map[string]string{
		mapPath:                            mapJSON,
		filepath.Join(data, "System.json"): `{"gameTitle":"勇者の旅"}`,
	} {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root, mapPath
}

type fakeProvider struct {
	block   bool
	entered chan struct{}
	once    sync.Once
}

func (f *fakeProvider) ID() string { return "google" }

func (f *fakeProvider) TranslateBatch(ctx context.Context, texts []string, _, _ string) ([]string, error) {
	if f.block {
		f.once.Do(func() { close(f.entered) })
		<-ctx.Done()
		return nil, ctx.Err()
	}
	out := make([]string, len(texts))
	for i, s := range texts {
		out[i] = "PT[" + s + "]"
	}
	return out, nil
}

func testMemory(t *testing.T) MemoryOpener {
	t.Helper()
	return func() (*memory.TranslationMemory, error) {
		return memory.New(filepath.Join(t.TempDir(), "memory.db"))
	}
}

// pump executa os comandos do Bubble Tea em goroutines e devolve as mensagens ao Update,
// no lugar do loop do tea.Program.
type pump struct {
	t    *testing.T
	m    *Model
	msgs chan tea.Msg
}

func newPump(t *testing.T, m *Model) *pump {
	return &pump{t: t, m: m, msgs: make(chan tea.Msg, 4096)}
}

func (p *pump) run(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	go func() {
		msg := cmd()
		if batch, ok := msg.(tea.BatchMsg); ok {
			for _, c := range batch {
				p.run(c)
			}
			return
		}
		if msg != nil {
			p.msgs <- msg
		}
	}()
}

func (p *pump) key(k tea.KeyMsg) {
	_, cmd := p.m.Update(k)
	p.run(cmd)
}

func (p *pump) until(cond func() bool) {
	p.t.Helper()
	deadline := time.After(20 * time.Second)
	for !cond() {
		select {
		case msg := <-p.msgs:
			_, cmd := p.m.Update(msg)
			p.run(cmd)
		case <-deadline:
			p.t.Fatalf("tempo esgotado; view=%v fase=%q err=%v", p.m.view, p.m.snapshot.Phase, p.m.runErr)
		}
	}
}

var enter = tea.KeyMsg{Type: tea.KeyEnter}

func TestConfigureRunsRealJobAndShowsResult(t *testing.T) {
	game, mapPath := writeGame(t)
	m := NewModel(game,
		WithProviderFactory(func(p ProviderParams) (provider.Provider, error) {
			if p.ID != "google" {
				t.Errorf("provedor = %q, esperava google", p.ID)
			}
			return &fakeProvider{}, nil
		}),
		WithMemoryOpener(testMemory(t)))
	m.view = ViewConfigure
	p := newPump(t, m)

	p.key(enter)
	if m.view != ViewTranslating {
		t.Fatalf("view = %v, esperava ViewTranslating (status=%q)", m.view, m.statusMsg)
	}
	p.until(func() bool { return m.view == ViewResult })

	if m.runErr != nil {
		t.Fatalf("runErr = %v", m.runErr)
	}
	got, _ := os.ReadFile(mapPath)
	if !strings.Contains(string(got), "PT[こんにちは]") {
		t.Errorf("mapa não foi traduzido: %s", got)
	}
	if m.snapshot.SegmentsNew == 0 || m.snapshot.ExitCode != 0 {
		t.Errorf("snapshot final = %+v", m.snapshot)
	}
	if out := m.View(); !strings.Contains(out, "Jogo traduzido") || !strings.Contains(out, "2 completos") {
		t.Errorf("tela de resultado sem o resumo do job:\n%s", out)
	}

	p.key(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	if m.view != ViewReviewing || len(m.reviewItems) == 0 {
		t.Fatalf("revisão deveria listar os segmentos do job: view=%v itens=%d", m.view, len(m.reviewItems))
	}
	p.key(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	for _, r := range "!" {
		p.key(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	p.key(enter)
	var edited bool
	for _, it := range m.job.SegmentsForReviewFiltered("all") {
		edited = edited || strings.HasSuffix(it.Target, "!")
	}
	if !edited {
		t.Error("ApplyCorrection não chegou ao job")
	}
	m.stopJob()
}

func TestCancelKeyStopsRunningJob(t *testing.T) {
	game, mapPath := writeGame(t)
	prov := &fakeProvider{block: true, entered: make(chan struct{})}
	m := NewModel(game,
		WithProviderFactory(func(ProviderParams) (provider.Provider, error) { return prov, nil }),
		WithMemoryOpener(testMemory(t)))
	m.view = ViewConfigure
	p := newPump(t, m)

	p.key(enter)
	select {
	case <-prov.entered:
	case <-time.After(10 * time.Second):
		t.Fatal("o job nunca chamou o provedor")
	}
	p.key(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	p.until(func() bool { return m.view == ViewResult })

	if !errors.Is(m.runErr, context.Canceled) {
		t.Errorf("runErr = %v, esperava context.Canceled", m.runErr)
	}
	if got, _ := os.ReadFile(mapPath); string(got) != mapJSON {
		t.Errorf("cancelar não pode gravar no jogo: %s", got)
	}
	if !strings.Contains(m.View(), "cancelada") {
		t.Error("o resultado devia informar o cancelamento")
	}
	m.stopJob()
}

func TestMemoryOnlyModeNeverCreatesProvider(t *testing.T) {
	game, mapPath := writeGame(t)
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	mem, err := memory.New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := mem.StoreBatch(context.Background(), []memory.Entry{{
		SrcLang: "ja", TgtLang: "pt", SourceText: "こんにちは", TargetText: "Olá", Engine: "google", Status: memory.StatusMachine,
	}}); err != nil {
		t.Fatal(err)
	}
	_ = mem.Close()
	m := NewModel(game,
		WithProviderFactory(func(ProviderParams) (provider.Provider, error) {
			t.Error("o modo só-memória não deve criar provedor")
			return nil, errors.New("não deveria ser chamado")
		}),
		WithMemoryOpener(func() (*memory.TranslationMemory, error) { return memory.New(dbPath) }))
	m.view = ViewConfigure
	m.selectedProvider = len(m.providers) - 1
	p := newPump(t, m)

	p.key(enter)
	p.until(func() bool { return m.view == ViewResult })

	if got, _ := os.ReadFile(mapPath); !strings.Contains(string(got), "Olá") {
		t.Errorf("a memória não foi aplicada: %s", got)
	}
}

func TestStartJobReportsProviderErrorOnConfigure(t *testing.T) {
	game, _ := writeGame(t)
	m := NewModel(game,
		WithProviderFactory(func(ProviderParams) (provider.Provider, error) {
			return nil, errors.New("sem chave")
		}),
		WithMemoryOpener(testMemory(t)))
	m.view = ViewConfigure

	m.Update(enter)
	if m.view != ViewConfigure || !strings.Contains(m.statusMsg, "sem chave") {
		t.Errorf("view=%v status=%q", m.view, m.statusMsg)
	}
}

func TestOpenGameTypedPathDetectsGameAndEstimates(t *testing.T) {
	game, _ := writeGame(t)
	m := NewModel(t.TempDir(), WithMemoryOpener(testMemory(t)))
	p := newPump(t, m)

	p.key(enter)
	if !m.pathInput {
		t.Fatal("sem jogo na pasta, Enter devia abrir o campo de caminho")
	}
	for _, r := range filepath.Join(t.TempDir(), "nada") {
		p.key(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	p.key(enter)
	if m.view != ViewOpenGame || !strings.Contains(m.statusMsg, "Nenhum jogo") {
		t.Fatalf("caminho sem jogo devia dar erro: view=%v status=%q", m.view, m.statusMsg)
	}

	m.pathBuf = game
	p.key(enter)
	if m.view != ViewConfigure || m.gamePath != game {
		t.Fatalf("view=%v gamePath=%q", m.view, m.gamePath)
	}
	p.until(func() bool { return m.estimate != nil })
	if m.estimate.SegmentsTotal == 0 || !strings.Contains(m.View(), "textos para traduzir") {
		t.Errorf("painel de estimativa sem dados reais: %+v", m.estimate)
	}
}

func TestConfigureChangesReestimateAndReachTheJob(t *testing.T) {
	game, mapPath := writeGame(t)
	var got ProviderParams
	m := NewModel(game,
		WithProviderFactory(func(p ProviderParams) (provider.Provider, error) {
			got = p
			return &fakeProvider{}, nil
		}),
		WithMemoryOpener(testMemory(t)))
	p := newPump(t, m)
	p.key(enter)
	p.until(func() bool { return m.estimate != nil })
	first := m.estimateSeq

	p.key(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}}) // pt -> en
	if m.targetLang != "en" || m.estimateSeq == first {
		t.Fatalf("mudar o idioma devia recalcular: target=%q seq=%d", m.targetLang, m.estimateSeq)
	}
	p.until(func() bool { return !m.estimating })

	p.key(tea.KeyMsg{Type: tea.KeyRight})
	p.key(tea.KeyMsg{Type: tea.KeyRight}) // Ollama
	fields := m.configFields()
	if len(fields) == 0 || fields[len(fields)-1].Key != "ollama_host" {
		t.Fatalf("Ollama devia mostrar modelo e endereço: %+v", fields)
	}
	p.key(tea.KeyMsg{Type: tea.KeyTab})
	for _, r := range "llama3" {
		p.key(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	p.key(tea.KeyMsg{Type: tea.KeyTab})
	p.key(tea.KeyMsg{Type: tea.KeyTab})
	if m.fieldFocus != -1 {
		t.Fatalf("fieldFocus = %d", m.fieldFocus)
	}
	p.key(enter)
	p.until(func() bool { return m.view == ViewResult })

	if got.ID != "ollama" || got.Model != "llama3" {
		t.Errorf("params do provedor = %+v", got)
	}
	if out, _ := os.ReadFile(mapPath); !strings.Contains(string(out), "PT[") {
		t.Errorf("mapa não traduzido: %s", out)
	}
	if m.snapshot.TargetLang != "en" {
		t.Errorf("idioma de destino do job = %q, esperava en", m.snapshot.TargetLang)
	}
	m.stopJob()
}
