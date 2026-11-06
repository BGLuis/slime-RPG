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
		SourceHash: seg.SourceHash(),
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

func TestPipeline_EndToEndWithMaskingAndFixing(t *testing.T) {
	ctx := context.Background()
	mem, err := memory.New(":memory:")
	if err != nil {
		t.Fatalf("falha ao inicializar TM: %v", err)
	}
	defer mem.Close()

	// Segmento com tags do RPG Maker e aspas tipográficas
	seg, _ := model.NewSegment("msg:1", `Hero: “Look \N[1], \V[10] gold!”`, model.KindDialogue, nil, nil)

	tr := &mockTranslator{
		translateFn: func(texts []string, src, tgt string) ([]string, error) {
			// Simula tradução que preserva os tokens __XTOK_0__ e __XTOK_1__,
			// mas insere aspas tipográficas espanholas/inglesas
			res := make([]string, len(texts))
			for i, txt := range texts {
				// Troca Hero por Herói e Look por Olhe, mantendo __XTOK_
				translated := strings.Replace(txt, "Hero:", "Herói:", 1)
				translated = strings.Replace(translated, "Look", "Olhe", 1)
				translated = strings.Replace(translated, "gold!”", "ouro!”", 1)
				res[i] = translated
			}
			return res, nil
		},
	}

	pctx := pipeline.NewContext([]*model.Segment{seg}, "en", "pt", "google", mem, tr)
	pipe := pipeline.DefaultPipeline()

	if err := pipe.Execute(ctx, pctx); err != nil {
		t.Fatalf("pipeline falhou: %v", err)
	}

	res := pctx.Segments[0]
	if !res.IsResolved() {
		t.Fatalf("segmento deveria estar resolvido")
	}

	// 1. Verifica se os tokens foram restaurados corretamente
	if !strings.Contains(res.Target(), `\N[1]`) || !strings.Contains(res.Target(), `\V[10]`) {
		t.Errorf("tokens não foram restaurados no resultado: %s", res.Target())
	}
	if strings.Contains(res.Target(), "__XTOK_") {
		t.Errorf("placeholder vazou no resultado: %s", res.Target())
	}

	// 2. Verifica se a etapa de Fixing normalizou as aspas tipográficas para ASCII
	if strings.Contains(res.Target(), "“") || strings.Contains(res.Target(), "”") {
		t.Errorf("aspas tipográficas deveriam ter sido convertidas para ASCII: %s", res.Target())
	}

	// 3. Verifica se a tradução foi persistida na TranslationMemory
	cachedTarget, found, err := mem.Lookup(ctx, "en", "pt", "google", seg.SourceHash())
	if err != nil || !found {
		t.Fatalf("nova tradução deveria estar salva na memória: %v", err)
	}
	if cachedTarget != res.Target() {
		t.Errorf("alvo em cache diferente do resultado: %q != %q", cachedTarget, res.Target())
	}
}

func TestPipeline_MaskIntegrityError(t *testing.T) {
	ctx := context.Background()

	seg, _ := model.NewSegment("s1", `Welcome \N[1]!`, model.KindDialogue, nil, nil)

	// Tradutor defeituoso que elimina o placeholder
	corruptTranslator := &mockTranslator{
		translateFn: func(texts []string, src, tgt string) ([]string, error) {
			return []string{"Bem-vindo ao jogo!"}, nil
		},
	}

	pctx := pipeline.NewContext([]*model.Segment{seg}, "en", "pt", "google", nil, corruptTranslator)
	pipe := pipeline.DefaultPipeline()

	err := pipe.Execute(ctx, pctx)
	if err == nil {
		t.Fatal("esperava erro de integridade de máscara por placeholder perdido")
	}
	if !strings.Contains(err.Error(), "Unmasking") {
		t.Errorf("erro deveria vir do passo Unmasking: %v", err)
	}
}
