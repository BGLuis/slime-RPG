package pipeline

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/BGLuis/slime-rpg/pkg/memory"
	"github.com/BGLuis/slime-rpg/pkg/model"
	"github.com/BGLuis/slime-rpg/pkg/pipeline/masking"
)

var (
	ErrNoSegments          = errors.New("pipeline has no segments to process")
	ErrNilTranslator       = errors.New("translator is required when translations are pending")
	ErrTranslationMismatch = errors.New("translator returned different count of translations")
)

// Translator define o contrato para tradução de lotes de strings.
type Translator interface {
	TranslateBatch(ctx context.Context, texts []string, srcLang, tgtLang string) ([]string, error)
}

// Memory define o contrato da Memória de Tradução consumida pelo pipeline.
type Memory interface {
	LookupBatch(ctx context.Context, srcLang, tgtLang, engine string, hashes []string) (map[string]string, error)
	StoreBatch(ctx context.Context, entries []memory.Entry) error
}

// Stats armazena métricas da execução do pipeline sobre um conjunto de segmentos.
type Stats struct {
	Total      int
	Cached     int
	Translated int
	Failed     int
}

// Context carrega o estado de um arquivo/lote através das etapas do pipeline.
type Context struct {
	Segments        []*model.Segment
	SrcLang         string
	TgtLang         string
	Engine          string
	Translator      Translator
	Memory          Memory
	Masker          *masking.Masker
	TokenMaps       map[string]map[string]string // ID do segmento -> [placeholder]original
	MaskedTexts     map[string]string            // ID do segmento -> texto mascarado
	IsFullyResolved bool
	Stats           Stats
}

// NewContext cria um novo contexto de pipeline.
func NewContext(segments []*model.Segment, srcLang, tgtLang, engine string, mem Memory, tr Translator) *Context {
	return &Context{
		Segments:    segments,
		SrcLang:     srcLang,
		TgtLang:     tgtLang,
		Engine:      engine,
		Memory:      mem,
		Translator:  tr,
		Masker:      masking.NewMasker(nil),
		TokenMaps:   make(map[string]map[string]string),
		MaskedTexts: make(map[string]string),
		Stats: Stats{
			Total: len(segments),
		},
	}
}

// Step é a interface que cada estágio do pipeline deve implementar.
type Step interface {
	Name() string
	Execute(ctx context.Context, pctx *Context) error
}

// Pipeline coordena a execução sequencial dos passos (Chain of Responsibility).
type Pipeline struct {
	steps []Step
}

// NewPipeline cria um pipeline com os passos customizados.
func NewPipeline(steps ...Step) *Pipeline {
	return &Pipeline{steps: steps}
}

// DefaultPipeline monta a cadeia canônica de 7 passos do Slime RPG.
func DefaultPipeline() *Pipeline {
	return NewPipeline(
		&CacheLookupStep{},
		&PatternDetectionStep{},
		&MaskingStep{},
		&TranslationStep{},
		&UnmaskingStep{},
		&FixingStep{},
		&CacheStoreStep{},
	)
}

// Execute executa sequencialmente cada passo do pipeline.
func (p *Pipeline) Execute(ctx context.Context, pctx *Context) error {
	if len(pctx.Segments) == 0 {
		return nil
	}

	for _, step := range p.steps {
		if err := step.Execute(ctx, pctx); err != nil {
			return fmt.Errorf("step %q failed: %w", step.Name(), err)
		}
	}
	return nil
}

// 1. CacheLookupStep: Consulta o SQLite. Se todos os segmentos forem cacheados, pula os demais.
type CacheLookupStep struct{}

func (s *CacheLookupStep) Name() string { return "CacheLookup" }

func (s *CacheLookupStep) Execute(ctx context.Context, pctx *Context) error {
	if pctx.Memory == nil {
		return nil
	}

	hashes := make([]string, len(pctx.Segments))
	for i, seg := range pctx.Segments {
		hashes[i] = seg.SourceHash()
	}

	cachedPairs, err := pctx.Memory.LookupBatch(ctx, pctx.SrcLang, pctx.TgtLang, pctx.Engine, hashes)
	if err != nil {
		return err
	}

	cachedCount := 0
	for i, seg := range pctx.Segments {
		if target, found := cachedPairs[seg.SourceHash()]; found && target != "" {
			pctx.Segments[i] = seg.WithTarget(target)
			cachedCount++
		}
	}

	pctx.Stats.Cached = cachedCount
	if cachedCount == len(pctx.Segments) {
		pctx.IsFullyResolved = true
	}

	return nil
}

// 2. PatternDetectionStep: Garante inicialização dos padrões de regex.
type PatternDetectionStep struct{}

func (s *PatternDetectionStep) Name() string { return "PatternDetection" }

func (s *PatternDetectionStep) Execute(ctx context.Context, pctx *Context) error {
	if pctx.IsFullyResolved {
		return nil
	}
	if pctx.Masker == nil {
		pctx.Masker = masking.NewMasker(nil)
	}
	return nil
}

// 3. MaskingStep: Substitui variáveis/tags por placeholders nos segmentos pendentes.
type MaskingStep struct{}

func (s *MaskingStep) Name() string { return "Masking" }

func (s *MaskingStep) Execute(ctx context.Context, pctx *Context) error {
	if pctx.IsFullyResolved {
		return nil
	}

	for _, seg := range pctx.Segments {
		if seg.IsResolved() {
			continue
		}
		masked, tokenMap := pctx.Masker.Mask(seg.Source())
		pctx.MaskedTexts[seg.ID()] = masked
		pctx.TokenMaps[seg.ID()] = tokenMap
	}

	return nil
}

// 4. TranslationStep: Despacha strings pendentes para o provedor de tradução.
type TranslationStep struct{}

func (s *TranslationStep) Name() string { return "Translation" }

func (s *TranslationStep) Execute(ctx context.Context, pctx *Context) error {
	if pctx.IsFullyResolved {
		return nil
	}

	var pendingIndices []int
	var textsToTranslate []string

	for i, seg := range pctx.Segments {
		if !seg.IsResolved() {
			pendingIndices = append(pendingIndices, i)
			maskedText, ok := pctx.MaskedTexts[seg.ID()]
			if !ok {
				maskedText = seg.Source()
			}
			textsToTranslate = append(textsToTranslate, maskedText)
		}
	}

	if len(textsToTranslate) == 0 {
		return nil
	}

	if pctx.Translator == nil {
		return ErrNilTranslator
	}

	translations, err := pctx.Translator.TranslateBatch(ctx, textsToTranslate, pctx.SrcLang, pctx.TgtLang)
	if err != nil {
		pctx.Stats.Failed += len(textsToTranslate)
		return err
	}

	if len(translations) != len(textsToTranslate) {
		pctx.Stats.Failed += len(textsToTranslate)
		return fmt.Errorf("%w: expected %d, got %d", ErrTranslationMismatch, len(textsToTranslate), len(translations))
	}

	for idx, translated := range translations {
		segIdx := pendingIndices[idx]
		pctx.Segments[segIdx] = pctx.Segments[segIdx].WithTarget(translated)
	}

	return nil
}

// 5. UnmaskingStep: Restaura códigos de escape e valida a integridade da máscara.
type UnmaskingStep struct{}

func (s *UnmaskingStep) Name() string { return "Unmasking" }

func (s *UnmaskingStep) Execute(ctx context.Context, pctx *Context) error {
	if pctx.IsFullyResolved {
		return nil
	}

	for i, seg := range pctx.Segments {
		tokenMap, hasTokens := pctx.TokenMaps[seg.ID()]
		if !hasTokens || len(tokenMap) == 0 {
			continue
		}

		unmasked, err := pctx.Masker.Unmask(seg.Target(), tokenMap)
		if err != nil {
			return fmt.Errorf("segment %s failed unmasking: %w", seg.ID(), err)
		}
		pctx.Segments[i] = seg.WithTarget(unmasked)
	}

	return nil
}

// 6. FixingStep: Corrige aspas tipográficas e sanitizações de sintaxe.
type FixingStep struct{}

func (s *FixingStep) Name() string { return "Fixing" }

func (s *FixingStep) Execute(ctx context.Context, pctx *Context) error {
	if pctx.IsFullyResolved {
		return nil
	}

	replacer := strings.NewReplacer(
		"“", "\"",
		"”", "\"",
		"‘", "'",
		"’", "'",
	)

	for i, seg := range pctx.Segments {
		if !seg.IsResolved() {
			continue
		}
		fixed := replacer.Replace(seg.Target())
		if fixed != seg.Target() {
			pctx.Segments[i] = seg.WithTarget(fixed)
		}
	}

	return nil
}

// 7. CacheStoreStep: Persiste novos pares válidos no SQLite.
type CacheStoreStep struct{}

func (s *CacheStoreStep) Name() string { return "CacheStore" }

func (s *CacheStoreStep) Execute(ctx context.Context, pctx *Context) error {
	if pctx.Memory == nil || pctx.IsFullyResolved {
		return nil
	}

	var newEntries []memory.Entry
	for _, seg := range pctx.Segments {
		// Só grava se estiver resolvido, diferente do source e sem placeholder residual
		if seg.IsResolved() && seg.Target() != seg.Source() && !strings.Contains(seg.Target(), "__XTOK_") {
			newEntries = append(newEntries, memory.Entry{
				SrcLang:    pctx.SrcLang,
				TgtLang:    pctx.TgtLang,
				SourceHash: seg.SourceHash(),
				SourceText: seg.Source(),
				TargetText: seg.Target(),
				Engine:     pctx.Engine,
				Status:     memory.StatusMachine,
			})
		}
	}

	if len(newEntries) > 0 {
		if err := pctx.Memory.StoreBatch(ctx, newEntries); err != nil {
			return fmt.Errorf("failed to store new pairs: %w", err)
		}
		pctx.Stats.Translated = len(newEntries)
	}

	return nil
}
