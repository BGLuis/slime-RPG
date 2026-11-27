package json

import (
	"slices"
	"strings"
	"testing"

	"github.com/BGLuis/slime-rpg/pkg/model"
)

func TestMVExtractor_UskCSV(t *testing.T) {
	ext := NewMVExtractor()
	if !ext.CanHandle("data/unique/eventsOpening.csv") {
		t.Fatal("esperava CanHandle aceitar CSV do RPG Maker")
	}

	raw := "オープニングイベント,\"メッセージ内容\n【CSVファイル側で有効なコマンド】\n  :name[名前]\n\"\n\n" +
		"town_op01,\"\n:script\nMIRA.forceFaceSet([1,8,1]);\nMIRA.gainSemen(200, \"vagina\");\n:end\n:voice[mira/op_10]\n:name[\\P[1]]\n本当に悪魔が迷宮を作ったというなら\n一度会ってみたいものだね\n\n:page\n次のセリフだよ\n\"\n"

	segs, err := ext.Extract("data/unique/eventsOpening.csv", []byte(raw))
	if err != nil {
		t.Fatalf("Extract falhou: %v", err)
	}

	var sources []string
	for _, s := range segs {
		sources = append(sources, s.Source())
	}
	want := []string{
		"本当に悪魔が迷宮を作ったというなら\n一度会ってみたいものだね",
		"次のセリフだよ",
	}
	if !slices.Equal(sources, want) {
		t.Fatalf("sources = %#v, esperava %#v", sources, want)
	}

	var resolved []*model.Segment
	for i, s := range segs {
		if i == 0 {
			resolved = append(resolved, s.WithTarget(`Se um demônio "realmente" criou o labirinto`+"\n"+`eu gostaria de encontrá-lo`))
		} else {
			resolved = append(resolved, s.WithTarget("Próxima fala"))
		}
	}

	out, err := ext.Inject("data/unique/eventsOpening.csv", []byte(raw), resolved)
	if err != nil {
		t.Fatalf("Inject falhou: %v", err)
	}
	outStr := string(out)
	if strings.Contains(outStr, `""`) {
		t.Errorf("saída CSV não pode conter aspas dobradas \"\" (quebra ExternMessage.js):\n%s", outStr)
	}
	for _, keep := range []string{
		"town_op01,",
		":script\nMIRA.forceFaceSet([1,8,1]);\nMIRA.gainSemen(200, \"vagina\");\n:end",
		":voice[mira/op_10]",
		":name[\\P[1]]",
		":page",
		"Se um demônio “realmente” criou o labirinto",
		"Próxima fala",
	} {
		if !strings.Contains(outStr, keep) {
			t.Errorf("saída não preservou/injetou %q:\n%s", keep, outStr)
		}
	}
}
