package pipeline_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/BGLuis/slime-rpg/pkg/memory"
	"github.com/BGLuis/slime-rpg/pkg/model"
	"github.com/BGLuis/slime-rpg/pkg/pipeline"
)

type mockTranslator struct {
	translateFn func(texts []string, src, tgt string) ([]string, error)
}

func (m *mockTranslator) TranslateBatch(ctx context.Context, texts []string, srcLang, tgtLang string) ([]string, error) {
	if m.translateFn != nil {
		return m.translateFn(texts, srcLang, tgtLang)
	}
	res := make([]string, len(texts))
	for i, t := range texts {
		res[i] = fmt.Sprintf("TRADUZIDO: %s", t)
	}
	return res, nil
}

func TestPipeline_AllCached(t *testing.T) {
	ctx := context.Background()
	mem, err := memory.New(":memory:")
	if err != nil {
		t.Fatalf("falha ao inicializar TM: %v", err)
	}
	defer mem.Close()

	seg, _ := model.NewSegment("s1", "Hello", model.KindDialogue, nil, nil)

	// Preenche o cache previamente
	_ = mem.Store(ctx, memory.Entry{
		SrcLang:    "en",
		TgtLang:    "pt",
		SourceText: "Hello",
		TargetText: "Olá",
		Engine:     "google",
	})

	trCalled := false
	tr := &mockTranslator{
		translateFn: func(texts []string, src, tgt string) ([]string, error) {
			trCalled = true
			return texts, nil
		},
	}

	pctx := pipeline.NewContext([]*model.Segment{seg}, "en", "pt", "google", mem, tr)
	pipe := pipeline.DefaultPipeline()

	if err := pipe.Execute(ctx, pctx); err != nil {
		t.Fatalf("pipeline falhou: %v", err)
	}

	if !pctx.IsFullyResolved {
		t.Errorf("esperava IsFullyResolved = true")
	}
	if trCalled {
		t.Errorf("o tradutor não deveria ter sido acionado quando tudo está em cache")
	}
	if pctx.Segments[0].Target() != "Olá" {
		t.Errorf("esperava 'Olá', obteve %q", pctx.Segments[0].Target())
	}
}

func newMemory(t *testing.T) *memory.TranslationMemory {
	t.Helper()
	mem, err := memory.New(":memory:")
	if err != nil {
		t.Fatalf("falha ao inicializar TM: %v", err)
	}
	t.Cleanup(func() { _ = mem.Close() })
	return mem
}

func newSegment(t *testing.T, id, source string, kind model.SegmentKind) *model.Segment {
	t.Helper()
	seg, err := model.NewSegment(id, source, kind, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	return seg
}

func TestPipeline_RestoresTokensAndCachesTranslation(t *testing.T) {
	ctx := context.Background()
	mem := newMemory(t)
	seg := newSegment(t, "msg:1", `Hero: “Look \N[1], \V[10] gold!”`, model.KindDialogue)

	tr := &mockTranslator{translateFn: func(texts []string, src, tgt string) ([]string, error) {
		out := make([]string, len(texts))
		for i, txt := range texts {
			out[i] = strings.NewReplacer("Hero:", "Herói:", "Look", "Olhe", "gold", "ouro").Replace(txt)
		}
		return out, nil
	}}

	pctx := pipeline.NewContext([]*model.Segment{seg}, "en", "pt", "google", mem, tr)
	if err := pipeline.DefaultPipeline().Execute(ctx, pctx); err != nil {
		t.Fatalf("pipeline falhou: %v", err)
	}

	want := `Herói: “Olhe \N[1], \V[10] ouro!”`
	if got := pctx.Segments[0].Target(); got != want {
		t.Errorf("obteve %q, esperava %q (tokens restaurados e aspas tipográficas preservadas no diálogo)", got, want)
	}
	cached, found, err := mem.Lookup(ctx, "en", "pt", "google", seg.Source())
	if err != nil || !found || cached != want {
		t.Errorf("tradução deveria estar na memória: %q, %v, %v", cached, found, err)
	}
}

func TestPipeline_FlattensTypographicQuotesOnlyInScripts(t *testing.T) {
	dialogue := newSegment(t, "d", "say hi", model.KindDialogue)
	script := newSegment(t, "s", "say hi", model.KindScript)
	tr := &mockTranslator{translateFn: func(texts []string, src, tgt string) ([]string, error) {
		return []string{"diga “oi” e ‘tchau’", "diga “oi” e ‘tchau’"}, nil
	}}

	pctx := pipeline.NewContext([]*model.Segment{dialogue, script}, "en", "pt", "google", nil, tr)
	if err := pipeline.DefaultPipeline().Execute(context.Background(), pctx); err != nil {
		t.Fatal(err)
	}
	if got := pctx.Segments[0].Target(); got != "diga “oi” e ‘tchau’" {
		t.Errorf("diálogo deve manter aspas tipográficas: %q", got)
	}
	if got := pctx.Segments[1].Target(); got != `diga "oi" e 'tchau'` {
		t.Errorf("script reinserido em JS precisa de aspas ASCII: %q", got)
	}
}

func TestPipeline_TokenLossFallsBackOnlyThatSegment(t *testing.T) {
	ctx := context.Background()
	mem := newMemory(t)
	broken := newSegment(t, "s1", `Welcome \N[1]!`, model.KindDialogue)
	healthy := newSegment(t, "s2", "Goodbye", model.KindDialogue)

	tr := &mockTranslator{translateFn: func(texts []string, src, tgt string) ([]string, error) {
		return []string{"Bem-vindo ao jogo!", "Adeus"}, nil
	}}

	pctx := pipeline.NewContext([]*model.Segment{broken, healthy}, "en", "pt", "google", mem, tr)
	if err := pipeline.DefaultPipeline().Execute(ctx, pctx); err != nil {
		t.Fatalf("perda de placeholder em um segmento não pode derrubar o arquivo: %v", err)
	}
	if got := pctx.Segments[0].Target(); got != broken.Source() {
		t.Errorf("segmento corrompido deve voltar ao original, obteve %q", got)
	}
	if got := pctx.Segments[1].Target(); got != "Adeus" {
		t.Errorf("segmento saudável deve ser traduzido, obteve %q", got)
	}
	if pctx.Stats.Failed != 1 {
		t.Errorf("esperava 1 falha contabilizada, obteve %d", pctx.Stats.Failed)
	}
	if _, found, _ := mem.Lookup(ctx, "en", "pt", "google", broken.Source()); found {
		t.Error("fallback para o original não pode ser gravado como tradução")
	}
}

func TestPipeline_DoesNotCacheIdenticalFallbackFromProvider(t *testing.T) {
	ctx := context.Background()
	mem := newMemory(t)
	seg := newSegment(t, "s1", "Untranslatable", model.KindDialogue)
	tr := &mockTranslator{translateFn: func(texts []string, src, tgt string) ([]string, error) {
		return texts, nil
	}}

	pctx := pipeline.NewContext([]*model.Segment{seg}, "en", "pt", "google", mem, tr)
	if err := pipeline.DefaultPipeline().Execute(ctx, pctx); err != nil {
		t.Fatal(err)
	}
	if count, _ := mem.Count(ctx); count != 0 {
		t.Errorf("tradução idêntica ao original não deve ir para o cache, registros: %d", count)
	}
}
