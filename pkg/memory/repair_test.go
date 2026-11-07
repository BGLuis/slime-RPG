package memory_test

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/BGLuis/slime-rpg/pkg/memory"
)

func TestRestoreControlCodeCase(t *testing.T) {
	tests := []struct{ name, source, target, want string }{
		{"restaura código do plugin", `\F[ahr]こんにちは`, `\f[ahr]Olá`, `\F[ahr]Olá`},
		{"restaura vários", `\FH[ON]\SP[20]x`, `\fh[ON]\sp[20]y`, `\FH[ON]\SP[20]y`},
		{"código ausente no source fica intacto", `\F[ahr]x`, `\q[1]y`, `\q[1]y`},
		{"prosa nunca muda", `\F[ahr]Hello`, `\F[ahr]hello \f world`, `\F[ahr]hello \f world`},
		{"grafia já correta fica intacta", `\F[a]`, `\F[a]`, `\F[a]`},
		{"grafias ambíguas no source não são tocadas", `\FH[1]\fH[2]`, `\fh[3]`, `\fh[3]`},
		{"códigos que o Python maiusculiza de propósito ficam como estão", `\c[1]x`, `\C[1]y`, `\C[1]y`},
		{"sem argumento não é código", `\F x`, `\f x`, `\f x`},
	}
	for _, tc := range tests {
		if got := memory.RestoreControlCodeCase(tc.source, tc.target); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestNormalizeNameTags(t *testing.T) {
	got := memory.NormalizeNameTags(`\nome[1] \namE[2] \nomE[3] \name[4] \NAME[5] \n[6] nome[7]`)
	want := `\NAME[1] \NAME[2] \NAME[3] \NAME[4] \NAME[5] \n[6] nome[7]`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

type seg struct{ source, target, engine, status string }

func repairDB(t *testing.T, segs ...seg) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "memory.db")
	tm, err := memory.New(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := tm.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for i, s := range segs {
		_, err := db.Exec(`INSERT INTO segment (src_lang, tgt_lang, source, target, source_hash, engine, status, created_at, updated_at)
			VALUES ('ja','pt',?,?,?,?,?,'t','t')`, s.source, s.target, memory.SourceHash(s.source+string(rune('a'+i))), s.engine, s.status)
		if err != nil {
			t.Fatal(err)
		}
	}
	return path
}

func targets(t *testing.T, path string) map[string]string {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rows, err := db.Query("SELECT source, target FROM segment")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var s, tg string
		if err := rows.Scan(&s, &tg); err != nil {
			t.Fatal(err)
		}
		out[s] = tg
	}
	return out
}

func fixtureDB(t *testing.T) string {
	return repairDB(t,
		seg{`\F[ahr]a`, `\f[ahr]a`, "google", "machine"},
		seg{`\F[ahr]b`, `\f[ahr]b`, "google", "locked"},
		seg{`prosa`, `texto \f[x] livre`, "google", "machine"},
		seg{`n`, `\nome[1] oi`, "google", "reviewed"},
		seg{`x__XTOK_1__`, `ok`, "google", "machine"},
		seg{`y`, `a __XTOK_2__`, "google", "machine"},
		seg{`z`, `lixo`, "BaseTranslate", "machine"},
		seg{`w`, `lixo`, "prefix_translate", "machine"},
		seg{`k`, `bom`, "google", "machine"},
	)
}

func TestRepairDryRunChangesNothing(t *testing.T) {
	path := fixtureDB(t)
	before := targets(t, path)

	rep, err := memory.Repair(ctx, path, false)
	if err != nil {
		t.Fatal(err)
	}
	wantRows := map[string]int{memory.RepairCase: 2, memory.RepairName: 1, memory.RepairXTOK: 2, memory.RepairEngine: 2}
	for c, n := range wantRows {
		if rep.Categories[c].Rows != n {
			t.Errorf("categoria %s: %d linhas, esperava %d", c, rep.Categories[c].Rows, n)
		}
	}
	if rep.ProtectedRows != 2 {
		t.Errorf("ProtectedRows = %d, esperava 2 (locked + reviewed)", rep.ProtectedRows)
	}
	after := targets(t, path)
	if len(after) != len(before) {
		t.Fatalf("dry-run alterou o número de linhas: %d -> %d", len(before), len(after))
	}
	for s, tg := range before {
		if after[s] != tg {
			t.Errorf("dry-run alterou %q: %q -> %q", s, tg, after[s])
		}
	}
	if matches, _ := filepath.Glob(path + ".bak-*"); len(matches) != 0 {
		t.Errorf("dry-run criou backup: %v", matches)
	}
}

func TestRepairApply(t *testing.T) {
	path := fixtureDB(t)
	rep, err := memory.Repair(ctx, path, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(rep.BackupPath); err != nil {
		t.Fatalf("backup ausente: %v", err)
	}
	got := targets(t, path)
	want := map[string]string{
		`\F[ahr]a`: `\F[ahr]a`,
		`\F[ahr]b`: `\F[ahr]b`,
		`prosa`:    `texto \f[x] livre`,
		`n`:        `\NAME[1] oi`,
		`k`:        `bom`,
	}
	if len(got) != len(want) {
		t.Errorf("restaram %d linhas, esperava %d: %v", len(got), len(want), got)
	}
	for s, tg := range want {
		if got[s] != tg {
			t.Errorf("%q: target %q, esperava %q", s, got[s], tg)
		}
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var status string
	if err := db.QueryRow("SELECT status FROM segment WHERE source = ?", `\F[ahr]b`).Scan(&status); err != nil || status != "locked" {
		t.Errorf("status da linha locked = %q (%v), deveria ser preservado", status, err)
	}
	if _, err := db.Exec("INSERT INTO segment_fts(segment_fts, rank) VALUES('integrity-check', 1)"); err != nil {
		t.Errorf("índice FTS inconsistente após o repair: %v", err)
	}

	again, err := memory.Repair(ctx, path, false)
	if err != nil {
		t.Fatal(err)
	}
	for c, cat := range again.Categories {
		if cat.Rows != 0 {
			t.Errorf("repair não é idempotente: %s ainda tem %d linhas", c, cat.Rows)
		}
	}
}

func TestMigrateLegacyIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	engineDir := filepath.Join(dir, "googleTraslator")
	if err := os.MkdirAll(engineDir, 0o755); err != nil {
		t.Fatal(err)
	}
	json := `{"こんにちは": "Olá", "same": "same", "nulo": null, "": "x"}`
	if err := os.WriteFile(filepath.Join(engineDir, "cache_ja_pt.json"), []byte(json), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(engineDir, "outro.json"), []byte(`{"a":"b"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	tm, path := openTM(t)
	for i := 0; i < 2; i++ {
		rep, err := memory.MigrateLegacy(ctx, tm, dir)
		if err != nil {
			t.Fatal(err)
		}
		if rep.Files != 1 || rep.Imported != 2 || rep.SkippedNull != 1 || rep.Identity != 1 {
			t.Fatalf("relatório inesperado: %+v", rep)
		}
	}
	if n, _ := tm.Count(ctx); n != 2 {
		t.Errorf("Count = %d após duas migrações, esperava 2", n)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var engine, status string
	if err := db.QueryRow("SELECT engine, status FROM segment WHERE source = 'same'").Scan(&engine, &status); err != nil {
		t.Fatalf("entrada idêntica deveria ser mantida: %v", err)
	}
	if engine != "google" || status != "machine" {
		t.Errorf("engine=%q status=%q; esperava google/machine", engine, status)
	}
}
