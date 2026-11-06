package masking_test

import (
	"errors"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/BGLuis/slime-rpg/pkg/pipeline/masking"
)

func TestMaskProtectsFormatCodesAndHTML(t *testing.T) {
	m := masking.NewMasker(nil)
	original := `<center>Welcome \M[INFO_0001]!</center>`

	masked, tokens := m.Mask(original)
	for _, protected := range []string{`\M[INFO_0001]`, "<center>", "</center>"} {
		if strings.Contains(masked, protected) {
			t.Errorf("%q não pode chegar ao tradutor: %q", protected, masked)
		}
	}

	translated := strings.Replace(masked, "Welcome", "Bem-vindo", 1)
	got, err := m.Unmask(translated, tokens)
	if err != nil {
		t.Fatal(err)
	}
	if want := `<center>Bem-vindo \M[INFO_0001]!</center>`; got != want {
		t.Errorf("obteve %q, esperava %q", got, want)
	}
}

func TestMaskUnmaskRoundTripIsLossless(t *testing.T) {
	m := masking.NewMasker(nil)
	for _, original := range []string{
		`<center>Welcome \V[5]! You have \C[2]gold\C[0].</center>`,
		`Olá \N[1], você encontrou \V[42] moedas de ouro! <center>Parabéns!</center>`,
		`\C[2]a\C[2]b\C[2]`,
		`Sem códigos`,
	} {
		masked, tokens := m.Mask(original)
		got, err := m.Unmask(masked, tokens)
		if err != nil || got != original {
			t.Errorf("round-trip de %q: obteve %q, %v", original, got, err)
		}
	}
}

// Uma tag que contém um código (ex.: <font color=\C[2]>) era mascarada como
// placeholder dentro de placeholder e o Unmask falhava conforme a ordem de
// iteração do map — na maioria das execuções, mesmo sem tradução nenhuma.
func TestNestedMatchesBecomeSinglePlaceholderDeterministically(t *testing.T) {
	m := masking.NewMasker(nil)
	original := `<font color=\C[2]>Hi</font>`

	masked, tokens := m.Mask(original)
	for ph, value := range tokens {
		if strings.Contains(value, "__XTOK_") {
			t.Fatalf("valor do mapa não pode conter outro placeholder: %s -> %q", ph, value)
		}
	}
	for range 100 {
		got, err := m.Unmask(masked, tokens)
		if err != nil || got != original {
			t.Fatalf("Unmask não determinístico: %q, %v", got, err)
		}
	}
}

func TestUnmaskToleratesPlaceholdersMangledByTranslator(t *testing.T) {
	m := masking.NewMasker(nil)
	masked, tokens := m.Mask(`Hello \N[1], take \V[2] coins`)
	if !strings.Contains(masked, "__XTOK_0__") || !strings.Contains(masked, "__XTOK_1__") {
		t.Fatalf("placeholders inesperados: %q", masked)
	}

	got, err := m.Unmask("Olá __xtok_0__, pegue __ XTOK_1 __ moedas", tokens)
	if err != nil {
		t.Fatal(err)
	}
	if want := `Olá \N[1], pegue \V[2] moedas`; got != want {
		t.Errorf("obteve %q, esperava %q", got, want)
	}
}

func TestUnmaskRejectsCorruptedPlaceholders(t *testing.T) {
	m := masking.NewMasker(nil)
	_, tokens := m.Mask(`Bem-vindo \N[1] ao reino!`)

	cases := map[string]string{
		"perdido":      "Welcome to the kingdom!",
		"duplicado":    "Welcome __XTOK_0__ __XTOK_0__!",
		"desconhecido": "Welcome __XTOK_0__ and __XTOK_99__!",
	}
	for name, translated := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := m.Unmask(translated, tokens); !errors.Is(err, masking.ErrMaskIntegrity) {
				t.Errorf("esperava ErrMaskIntegrity, obteve %v", err)
			}
		})
	}
}

func TestUnmaskRejectsPlaceholderInventedForTextWithoutTokens(t *testing.T) {
	m := masking.NewMasker(nil)
	_, tokens := m.Mask("Plain text")
	if _, err := m.Unmask("Texto __XTOK_0__", tokens); !errors.Is(err, masking.ErrMaskIntegrity) {
		t.Errorf("placeholder sem origem deve falhar, obteve %v", err)
	}
}

func TestMaskAvoidsPlaceholderAlreadyInSource(t *testing.T) {
	m := masking.NewMasker(nil)
	masked, tokens := m.Mask(`literal __XTOK_0__ and \N[1]`)
	if _, clash := tokens["__XTOK_0__"]; clash {
		t.Errorf("placeholder gerado colide com texto literal: %q %v", masked, tokens)
	}
}

func TestNotPrecededByEmulatesLookbehind(t *testing.T) {
	m := masking.NotPrecededBy(regexp.MustCompile(`!?(?:[vVsS]|ss|sv)\[\s*\d+\s*\]`), "!", masking.IsWordOrBackslash)
	cases := map[string][]string{
		"Você tem v[1] moedas":   {"v[1]"},
		"!s[2] ativo":            {"!s[2]"},
		`\v[3] já é código`:      nil,
		"prev[4] não é variável": nil,
		"x!v[5]":                 {"!v[5]"},
	}
	for text, want := range cases {
		var got []string
		for _, loc := range m.FindAllStringIndex(text, -1) {
			got = append(got, text[loc[0]:loc[1]])
		}
		if !slices.Equal(got, want) {
			t.Errorf("%q: got %q, want %q", text, got, want)
		}
	}
}
