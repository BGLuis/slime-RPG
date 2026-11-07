package tui

import (
	"strings"
	"testing"

	"github.com/BGLuis/slime-rpg/pkg/i18n"
)

// A TUI precisa seguir o idioma ativo em todas as telas, sem restos do português.
func TestViewsFollowInterfaceLanguage(t *testing.T) {
	game, _ := writeGame(t)
	t.Cleanup(func() { i18n.Set(i18n.PT) })

	cases := []struct {
		view   ViewState
		en, pt string
		footEN string
		footPT string
	}{
		{ViewOpenGame, "Which game shall we translate?", "Qual jogo vamos traduzir?", "enter choose · q quit", "enter escolher · q sair"},
		{ViewConfigure, "Source language (s)", "Idioma de origem (s)", "enter translate", "enter traduzir"},
		{ViewTranslating, "Phase:", "Fase:", "p pause/resume", "p pausar/retomar"},
		{ViewReviewing, "corrections go to the Translation Memory", "correções vão para a Memória de Tradução", "e edit", "e editar"},
		{ViewResult, "Where everything ended up", "Onde ficou cada coisa", "n another game", "n outro jogo"},
	}
	render := func(lang i18n.Lang, v ViewState) string {
		i18n.Set(lang)
		m := NewModel(game)
		m.view = v
		return m.View()
	}
	for _, c := range cases {
		en := render(i18n.EN, c.view)
		for _, want := range []string{c.en, c.footEN} {
			if !strings.Contains(en, want) {
				t.Errorf("view %v em EN sem %q", c.view, want)
			}
		}
		for _, notWant := range []string{c.pt, c.footPT} {
			if strings.Contains(en, notWant) {
				t.Errorf("view %v em EN ainda tem o texto PT %q", c.view, notWant)
			}
		}
		if pt := render(i18n.PT, c.view); !strings.Contains(pt, c.pt) || !strings.Contains(pt, c.footPT) {
			t.Errorf("view %v em PT perdeu %q ou %q", c.view, c.pt, c.footPT)
		}
	}

	i18n.Set(i18n.EN)
	if got := langLabel("es"); got != "Spanish (es)" {
		t.Errorf("langLabel em EN = %q", got)
	}
}

func TestLanguageCyclePreservesOrder(t *testing.T) {
	want := []string{"pt", "en", "es", "fr", "it", "ja", "auto", "pt"}
	got := []string{"pt"}
	for range want[1:] {
		got = append(got, stepLang(got[len(got)-1], 1))
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("ciclo de idiomas = %v, quer %v", got, want)
	}
	if back := stepLang("pt", -1); back != "auto" {
		t.Errorf("voltar de pt devia dar a volta até auto, deu %q", back)
	}
}
