package model_test

import (
	"errors"
	"testing"

	"github.com/BGLuis/slime-rpg/pkg/model"
)

func TestNewSegment_Validation(t *testing.T) {
	tests := []struct {
		name        string
		id          string
		source      string
		kind        model.SegmentKind
		context     map[string]any
		constraints map[string]string
		wantErr     error
	}{
		{
			name:        "válido com diálogo",
			id:          "Map001.json#event:1/cmd:1",
			source:      "Hello world",
			kind:        model.KindDialogue,
			context:     map[string]any{"file": "Map001.json"},
			constraints: map[string]string{"max_length": "100"},
			wantErr:     nil,
		},
		{
			name:    "erro ID vazio",
			id:      "",
			source:  "Hello",
			kind:    model.KindDialogue,
			wantErr: model.ErrEmptyID,
		},
		{
			name:    "erro Source vazio",
			id:      "id_1",
			source:  "",
			kind:    model.KindDialogue,
			wantErr: model.ErrEmptySource,
		},
		{
			name:    "erro Kind vazio",
			id:      "id_1",
			source:  "Hello",
			kind:    "",
			wantErr: model.ErrMissingKind,
		},
		{
			name:    "erro Kind inválido",
			id:      "id_1",
			source:  "Hello",
			kind:    "invalid_kind",
			wantErr: model.ErrInvalidKind,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			seg, err := model.NewSegment(tt.id, tt.source, tt.kind, tt.context, tt.constraints)
			if tt.wantErr != nil {
				if err == nil {
					t.Fatalf("esperava erro %v, obteve nil", tt.wantErr)
				}
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("esperava erro derivado de %v, obteve %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("erro inesperado: %v", err)
			}
			if seg.ID() != tt.id {
				t.Errorf("esperava id %q, obteve %q", tt.id, seg.ID())
			}
			if seg.Source() != tt.source {
				t.Errorf("esperava source %q, obteve %q", tt.source, seg.Source())
			}
			if seg.Kind() != tt.kind {
				t.Errorf("esperava kind %q, obteve %q", tt.kind, seg.Kind())
			}
			if seg.SourceHash() == "" {
				t.Errorf("sourceHash não deve ser vazio")
			}
		})
	}
}

func TestSegment_Immutability(t *testing.T) {
	ctx := map[string]any{"actor": "Hero"}
	constraints := map[string]string{"limit": "50"}

	seg, err := model.NewSegment("item:1", "Sword", model.KindDB, ctx, constraints)
	if err != nil {
		t.Fatalf("falha ao criar segmento: %v", err)
	}

	// Mutação externa não deve alterar o segmento
	ctx["actor"] = "Villain"
	constraints["limit"] = "99"

	actor, ok := seg.ContextValue("actor")
	if !ok || actor != "Hero" {
		t.Errorf("esperava contexto intacto 'Hero', obteve %v", actor)
	}

	limit, ok := seg.ConstraintValue("limit")
	if !ok || limit != "50" {
		t.Errorf("esperava restrição intacta '50', obteve %v", limit)
	}

	// Mutação de cópia defensiva retornada não deve alterar o segmento
	retrievedCtx := seg.Context()
	retrievedCtx["actor"] = "Another"
	actorAgain, _ := seg.ContextValue("actor")
	if actorAgain != "Hero" {
		t.Errorf("esperava contexto interno intacto após mutação na cópia, obteve %v", actorAgain)
	}
}

func TestSegment_WithTarget(t *testing.T) {
	seg, err := model.NewSegment("msg:1", "Good morning", model.KindDialogue, nil, nil)
	if err != nil {
		t.Fatalf("falha ao criar segmento: %v", err)
	}

	if seg.IsResolved() {
		t.Errorf("segmento recém criado não deve estar resolvido")
	}

	translated := seg.WithTarget("Bom dia")
	if !translated.IsResolved() {
		t.Errorf("segmento traduzido deve estar resolvido")
	}
	if translated.Target() != "Bom dia" {
		t.Errorf("esperava target 'Bom dia', obteve %q", translated.Target())
	}
	// Segmento original permanece inalterado
	if seg.IsResolved() || seg.Target() != "" {
		t.Errorf("segmento original não deve ter sido modificado por WithTarget")
	}
}

func TestComputeHash(t *testing.T) {
	h1 := model.ComputeHash("Teste")
	h2 := model.ComputeHash("Teste")
	h3 := model.ComputeHash("Outro")

	if h1 != h2 {
		t.Errorf("hashes para o mesmo texto devem ser idênticos: %s != %s", h1, h2)
	}
	if h1 == h3 {
		t.Errorf("hashes para textos diferentes devem ser distintos")
	}
}
