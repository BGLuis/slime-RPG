package memory

import (
	"bytes"
	"context"
	"path/filepath"
	"slices"
	"testing"
)

// Exportar para um CAT tool e reimportar a revisão precisa devolver os mesmos
// pares, agora com status reviewed vencendo a tradução de máquina.
func TestTMXRoundTripPromotesReviewedTranslations(t *testing.T) {
	ctx := context.Background()
	tm, err := New(filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer tm.Close()
	if err := tm.StoreBatch(ctx, []Entry{
		{SrcLang: "ja", TgtLang: "pt", SourceText: "勇者", TargetText: "Herói", Engine: "google", Status: StatusMachine},
		{SrcLang: "ja", TgtLang: "pt", SourceText: "<村> & 森", TargetText: "<Vila> & Floresta", Engine: "google", Status: StatusMachine},
		{SrcLang: "ja", TgtLang: "en", SourceText: "勇者", TargetText: "Hero", Engine: "google", Status: StatusMachine},
	}); err != nil {
		t.Fatal(err)
	}

	pairs, err := tm.Pairs(ctx, "ja", "pt")
	if err != nil || len(pairs) != 2 {
		t.Fatalf("pairs = %v, %v", pairs, err)
	}
	var buf bytes.Buffer
	if err := WriteTMX(&buf, "ja", "pt", pairs); err != nil {
		t.Fatal(err)
	}

	reviewed := bytes.Replace(buf.Bytes(), []byte("Herói"), []byte("Heroína"), 1)
	units, skipped, err := ReadTMX(bytes.NewReader(reviewed))
	if err != nil || skipped != 0 || len(units) != 2 {
		t.Fatalf("units = %v skipped=%d err=%v", units, skipped, err)
	}
	if units[1] != (TMXUnit{SrcLang: "ja", TgtLang: "pt", Source: "<村> & 森", Target: "<Vila> & Floresta"}) {
		t.Errorf("escape XML perdido: %+v", units[1])
	}
	for _, u := range units {
		if err := tm.Store(ctx, Entry{SrcLang: u.SrcLang, TgtLang: u.TgtLang, SourceText: u.Source, TargetText: u.Target, Engine: "human", Status: StatusReviewed}); err != nil {
			t.Fatal(err)
		}
	}
	got, _, _ := tm.Lookup(ctx, "ja", "pt", "google", "勇者")
	if got != "Heroína" {
		t.Errorf("a revisão importada deveria vencer a tradução de máquina, obteve %q", got)
	}
	locked, _ := tm.Pairs(ctx, "ja", "pt", StatusReviewed)
	if !slices.ContainsFunc(locked, func(p Pair) bool { return p.Target == "Heroína" }) {
		t.Errorf("filtro por status: %v", locked)
	}
}

func TestReadTMXSkipsIncompleteUnits(t *testing.T) {
	src := `<?xml version="1.0"?><tmx version="1.4"><header srclang="ja"/><body>
<tu><tuv xml:lang="ja"><seg>一</seg></tuv></tu>
<tu><tuv lang="ja"><seg>二</seg></tuv><tuv lang="pt"><seg>dois</seg></tuv></tu>
</body></tmx>`
	units, skipped, err := ReadTMX(bytes.NewReader([]byte(src)))
	if err != nil || skipped != 1 || len(units) != 1 || units[0].Target != "dois" {
		t.Errorf("units=%v skipped=%d err=%v", units, skipped, err)
	}
}
