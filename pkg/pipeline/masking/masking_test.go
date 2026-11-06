package masking_test

import (
	"errors"
	"testing"

	"github.com/BGLuis/slime-rpg/pkg/pipeline/masking"
)

func TestMasker_MaskAndUnmask(t *testing.T) {
	masker := masking.NewMasker(nil)

	original := `Olá \N[1], você encontrou \V[42] moedas de ouro! <center>Parabéns!</center>`
	masked, tokenMap := masker.Mask(original)

	if len(tokenMap) != 4 {
		t.Fatalf("esperava 4 tokens mascarados, obteve %d (%+v)", len(tokenMap), tokenMap)
	}

	// Simula a tradução preservando os placeholders
	// Exemplo: "__XTOK_0__" -> "\N[1]", "__XTOK_1__" -> "\V[42]", "__XTOK_2__" -> "<center>", "__XTOK_3__" -> "</center>"
	translated := masked // simula resposta onde as palavras ao redor mudam, mas os tokens ficam

	unmasked, err := masker.Unmask(translated, tokenMap)
	if err != nil {
		t.Fatalf("falha ao desmascarar: %v", err)
	}

	if unmasked != original {
		t.Errorf("texto desmascarado diferente do original:\nEsperado: %s\nObtido:   %s", original, unmasked)
	}
}

func TestMasker_Unmask_MissingPlaceholder(t *testing.T) {
	masker := masking.NewMasker(nil)

	original := `Bem-vindo \N[1] ao reino!`
	masked, tokenMap := masker.Mask(original)

	// Simula API de tradução que "comeu" o placeholder
	corrupted := "Welcome to the kingdom!"

	_, err := masker.Unmask(corrupted, tokenMap)
	if err == nil {
		t.Fatal("esperava erro de integridade de máscara por placeholder ausente")
	}
	if !errors.Is(err, masking.ErrMaskIntegrity) {
		t.Errorf("esperava ErrMaskIntegrity, obteve: %v", err)
	}
	_ = masked
}

func TestMasker_Unmask_ResidualPlaceholder(t *testing.T) {
	masker := masking.NewMasker(nil)

	tokenMap := map[string]string{
		"__XTOK_0__": `\N[1]`,
	}
	// Texto com um placeholder fantasma adicional não registrado
	textWithResidual := `Olá \N[1] e __XTOK_99__!`

	_, err := masker.Unmask(textWithResidual, tokenMap)
	if err == nil {
		t.Fatal("esperava erro de integridade por placeholder residual")
	}
	if !errors.Is(err, masking.ErrMaskIntegrity) {
		t.Errorf("esperava ErrMaskIntegrity, obteve: %v", err)
	}
}
