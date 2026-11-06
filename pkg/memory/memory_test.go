package memory_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"sync"
	"testing"

	"github.com/BGLuis/slime-rpg/pkg/memory"
)

var ctx = context.Background()

func openTM(t *testing.T) (*memory.TranslationMemory, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "memory.db")
	tm, err := memory.New(path)
	if err != nil {
		t.Fatalf("falha ao abrir TM: %v", err)
	}
	t.Cleanup(func() { _ = tm.Close() })
	return tm, path
}

func store(t *testing.T, tm *memory.TranslationMemory, engine, source, target string, status memory.Status) {
	t.Helper()
	err := tm.Store(ctx, memory.Entry{SrcLang: "en", TgtLang: "pt", Engine: engine, SourceText: source, TargetText: target, Status: status})
	if err != nil {
		t.Fatalf("Store(%q): %v", source, err)
	}
}

func lookup(t *testing.T, tm *memory.TranslationMemory, engine, source string) (string, bool) {
	t.Helper()
	target, ok, err := tm.Lookup(ctx, "en", "pt", engine, source)
	if err != nil {
		t.Fatalf("Lookup(%q): %v", source, err)
	}
	return target, ok
}

func lookupBatch(t *testing.T, tm *memory.TranslationMemory, engine string, texts ...string) map[string]string {
	t.Helper()
	res, err := tm.LookupBatch(ctx, "en", "pt", engine, texts)
	if err != nil {
		t.Fatalf("LookupBatch: %v", err)
	}
	return res
}

func wantLookup(t *testing.T, tm *memory.TranslationMemory, engine, source, want string) {
	t.Helper()
	got, ok := lookup(t, tm, engine, source)
	if !ok || got != want {
		t.Errorf("Lookup(%q) = %q, %v; esperava %q", source, got, ok, want)
	}
}

func wantMiss(t *testing.T, tm *memory.TranslationMemory, engine, source string) {
	t.Helper()
	if got, ok := lookup(t, tm, engine, source); ok {
		t.Errorf("Lookup(%q) = %q; esperava ausência", source, got)
	}
}

// pythonSchema é o schema gravado pela versão Python; os testes de migração
// partem de um banco criado por ela.
const pythonSchema = `
CREATE TABLE segment (
    id INTEGER PRIMARY KEY, src_lang TEXT NOT NULL, tgt_lang TEXT NOT NULL,
    source TEXT NOT NULL, target TEXT NOT NULL, source_hash TEXT NOT NULL,
    engine TEXT NOT NULL, model TEXT, game TEXT, context TEXT,
    status TEXT NOT NULL DEFAULT 'machine', created_at TEXT NOT NULL, updated_at TEXT NOT NULL);
CREATE UNIQUE INDEX idx_seg_lookup ON segment(src_lang, tgt_lang, source_hash, engine);
CREATE VIRTUAL TABLE segment_fts USING fts5(source, content='segment', content_rowid='id');
CREATE TRIGGER segment_ai AFTER INSERT ON segment BEGIN
    INSERT INTO segment_fts(rowid, source) VALUES (new.id, new.source);
END;
CREATE TABLE game_segment (game TEXT NOT NULL, source_hash TEXT NOT NULL, created_at TEXT NOT NULL, PRIMARY KEY(game, source_hash));
`

type legacyRow struct {
	source, target, engine, status, updated string
}

func legacyDB(t *testing.T, rows ...legacyRow) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(pythonSchema); err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		_, err := db.Exec(`INSERT INTO segment (src_lang, tgt_lang, source, target, source_hash, engine, status, created_at, updated_at)
			VALUES ('en', 'pt', ?, ?, ?, ?, ?, ?, ?)`, r.source, r.target, memory.SourceHash(r.source), r.engine, r.status, r.updated, r.updated)
		if err != nil {
			t.Fatal(err)
		}
	}
	return path
}

type dbRow struct{ engine, status, target string }

func rowsFor(t *testing.T, path, source string) []dbRow {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rows, err := db.Query("SELECT engine, status, target FROM segment WHERE source_hash = ?", memory.SourceHash(source))
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []dbRow
	for rows.Next() {
		var r dbRow
		if err := rows.Scan(&r.engine, &r.status, &r.target); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	return out
}

func TestStoreThenLookup(t *testing.T) {
	tm, _ := openTM(t)
	wantMiss(t, tm, "google", "hello")
	store(t, tm, "google", "hello", "ola", memory.StatusMachine)
	wantLookup(t, tm, "google", "hello", "ola")
}

func TestCrossEngineReuse(t *testing.T) {
	tm, _ := openTM(t)
	store(t, tm, "googleTraslator", "hello", "ola", memory.StatusMachine)
	wantLookup(t, tm, "ollamaTranslator", "hello", "ola")
}

func TestStrictEngineDisablesCrossEngineReuse(t *testing.T) {
	tm, _ := openTM(t)
	store(t, tm, "google", "hello", "ola", memory.StatusMachine)
	tm.SetPolicy(memory.Policy{StrictEngine: true})
	wantMiss(t, tm, "ollama", "hello")
	wantLookup(t, tm, "google", "hello", "ola")
}

func TestStatusPrecedence(t *testing.T) {
	t.Run("machine não sobrescreve reviewed", func(t *testing.T) {
		tm, _ := openTM(t)
		store(t, tm, "google", "X", "traducao_revisada", memory.StatusReviewed)
		store(t, tm, "google", "X", "traducao_de_maquina", memory.StatusMachine)
		wantLookup(t, tm, "google", "X", "traducao_revisada")
	})
	t.Run("reviewed promove machine", func(t *testing.T) {
		tm, _ := openTM(t)
		store(t, tm, "google", "X", "traducao_de_maquina", memory.StatusMachine)
		store(t, tm, "google", "X", "traducao_revisada", memory.StatusReviewed)
		wantLookup(t, tm, "google", "X", "traducao_revisada")
	})
	t.Run("locked nunca é sobrescrito", func(t *testing.T) {
		tm, _ := openTM(t)
		if err := tm.LockTerm(ctx, "en", "pt", "google", "X", "termo_fixo"); err != nil {
			t.Fatal(err)
		}
		store(t, tm, "google", "X", "tentativa_machine", memory.StatusMachine)
		store(t, tm, "google", "X", "tentativa_reviewed", memory.StatusReviewed)
		wantLookup(t, tm, "google", "X", "termo_fixo")
	})
	t.Run("lookup em lote segue o status", func(t *testing.T) {
		tm, _ := openTM(t)
		store(t, tm, "google", "X", "maquina", memory.StatusMachine)
		store(t, tm, "google", "X", "revisada", memory.StatusReviewed)
		if got := lookupBatch(t, tm, "google", "X"); got["X"] != "revisada" {
			t.Errorf("esperava revisada, obteve %v", got)
		}
		if err := tm.LockTerm(ctx, "en", "pt", "google", "X", "fixa"); err != nil {
			t.Fatal(err)
		}
		if got := lookupBatch(t, tm, "google", "X"); got["X"] != "fixa" {
			t.Errorf("esperava fixa, obteve %v", got)
		}
	})
}

func TestPreferredEngineWinsAmongEqualStatus(t *testing.T) {
	tm, _ := openTM(t)
	store(t, tm, "google", "Sword", "Espada (Google)", memory.StatusMachine)
	store(t, tm, "ollama", "Sword", "Lâmina (Ollama)", memory.StatusMachine)

	wantLookup(t, tm, "ollama", "Sword", "Lâmina (Ollama)")
	wantLookup(t, tm, "google", "Sword", "Espada (Google)")
	if got := lookupBatch(t, tm, "ollama", "Sword"); got["Sword"] != "Lâmina (Ollama)" {
		t.Errorf("lote com preferência ollama: %v", got)
	}
}

func TestFTSCandidatesStayInSyncViaTriggers(t *testing.T) {
	tm, _ := openTM(t)
	store(t, tm, "google", "hello world", "ola mundo", memory.StatusMachine)
	store(t, tm, "google", "hello world", "ola, mundo", memory.StatusMachine)

	results, err := tm.SearchFTS(ctx, "en", "pt", "hello", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].SourceText != "hello world" || results[0].TargetText != "ola, mundo" {
		t.Errorf("FTS deveria refletir o upsert, obteve %+v", results)
	}
}

func TestSearchFTSTreatsQueryAsLiteralText(t *testing.T) {
	tm, _ := openTM(t)
	store(t, tm, "google", `He said "go" (now): AND NOT`, "x", memory.StatusMachine)
	if _, err := tm.SearchFTS(ctx, "en", "pt", `"go" (now): AND NOT`, 5); err != nil {
		t.Errorf("sintaxe FTS5 no texto do jogo não pode quebrar a busca: %v", err)
	}
}

func TestLookupBatchReturnsOnlyHits(t *testing.T) {
	tm, _ := openTM(t)
	store(t, tm, "google", "a", "A", memory.StatusMachine)
	store(t, tm, "google", "b", "B", memory.StatusMachine)
	want := map[string]string{"a": "A", "b": "B"}
	if got := lookupBatch(t, tm, "google", "a", "b", "c"); !maps.Equal(got, want) {
		t.Errorf("obteve %v, esperava %v", got, want)
	}
}

func TestWhitespaceVariantsMatchButLineBreaksDoNotCollide(t *testing.T) {
	tm, _ := openTM(t)
	store(t, tm, "google", "a  b\tc", "ABC", memory.StatusMachine)
	if got := lookupBatch(t, tm, "google", "a b c"); got["a b c"] != "ABC" {
		t.Errorf("espaços colapsados deveriam casar, obteve %v", got)
	}

	store(t, tm, "google", "A\nB", "Trad A\nB", memory.StatusMachine)
	store(t, tm, "google", "A B", "Trad A B", memory.StatusMachine)
	if memory.SourceHash("A\nB") == memory.SourceHash("A B") {
		t.Fatal("quebra de linha e espaço devem ter hashes distintos")
	}
	wantLookup(t, tm, "google", "A\nB", "Trad A\nB")
	wantLookup(t, tm, "google", "A B", "Trad A B")
	got := lookupBatch(t, tm, "google", "A\nB", "A B")
	if got["A\nB"] != "Trad A\nB" || got["A B"] != "Trad A B" {
		t.Errorf("lote deveria distinguir as duas formas, obteve %v", got)
	}
}

func TestLookupBatchChunksBeyondParamLimit(t *testing.T) {
	tm, _ := openTM(t)
	want := make(map[string]string, 1000)
	var entries []memory.Entry
	var sources []string
	for i := range 1000 {
		src, tgt := fmt.Sprintf("src-%d", i), fmt.Sprintf("tgt-%d", i)
		want[src] = tgt
		sources = append(sources, src)
		entries = append(entries, memory.Entry{SrcLang: "en", TgtLang: "pt", Engine: "google", SourceText: src, TargetText: tgt})
	}
	if err := tm.StoreBatch(ctx, entries); err != nil {
		t.Fatal(err)
	}
	if got := lookupBatch(t, tm, "google", sources...); !maps.Equal(got, want) {
		t.Errorf("esperava %d acertos, obteve %d", len(want), len(got))
	}
}

func TestLookupBatchIgnoresBlank(t *testing.T) {
	tm, _ := openTM(t)
	if got := lookupBatch(t, tm, "google", "", "   "); len(got) != 0 {
		t.Errorf("textos em branco não devem gerar acertos: %v", got)
	}
}

func TestStoreBatchDoesNotDowngradeAndSkipsBlank(t *testing.T) {
	tm, _ := openTM(t)
	store(t, tm, "google", "X", "revisada", memory.StatusReviewed)
	err := tm.StoreBatch(ctx, []memory.Entry{
		{SrcLang: "en", TgtLang: "pt", Engine: "google", SourceText: "X", TargetText: "rebaixada"},
		{SrcLang: "en", TgtLang: "pt", Engine: "google", SourceText: "Y", TargetText: "y"},
		{SrcLang: "en", TgtLang: "pt", Engine: "google", SourceText: "", TargetText: "x"},
		{SrcLang: "en", TgtLang: "pt", Engine: "google", SourceText: "  ", TargetText: "y"},
		{SrcLang: "en", TgtLang: "pt", Engine: "google", SourceText: "vazio", TargetText: ""},
	})
	if err != nil {
		t.Fatalf("entradas em branco devem ser puladas, não falhar o lote: %v", err)
	}
	wantLookup(t, tm, "google", "X", "revisada")
	wantLookup(t, tm, "google", "Y", "y")
	wantMiss(t, tm, "google", "vazio")
}

func TestRejectsIdenticalMachineTranslationWhenLanguagesDiffer(t *testing.T) {
	tm, _ := openTM(t)
	err := tm.StoreBatch(ctx, []memory.Entry{
		{SrcLang: "en", TgtLang: "pt", Engine: "google", SourceText: "Apple", TargetText: "Apple"},
		{SrcLang: "en", TgtLang: "pt", Engine: "google", SourceText: "Banana", TargetText: " Banana "},
		{SrcLang: "en", TgtLang: "pt", Engine: "google", SourceText: "Orange", TargetText: "Laranja"},
	})
	if err != nil {
		t.Fatal(err)
	}
	wantMiss(t, tm, "google", "Apple")
	wantMiss(t, tm, "google", "Banana")
	wantLookup(t, tm, "google", "Orange", "Laranja")

	store(t, tm, "google", "Apple", "Apple", memory.StatusReviewed)
	wantLookup(t, tm, "google", "Apple", "Apple")
}

func TestReadsSeeCommittedWritesFromAnotherGoroutine(t *testing.T) {
	tm, _ := openTM(t)
	store(t, tm, "google", "K", "v", memory.StatusMachine)

	var got string
	var ok bool
	done := make(chan struct{})
	go func() {
		defer close(done)
		got, ok, _ = tm.Lookup(ctx, "en", "pt", "google", "K")
	}()
	<-done
	if !ok || got != "v" {
		t.Errorf("outra goroutine deveria ler o valor gravado, obteve %q", got)
	}
}

func TestInMemoryDatabaseSupportsConcurrentAccess(t *testing.T) {
	tm, err := memory.New(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer tm.Close()

	var wg sync.WaitGroup
	errs := make(chan error, 100)
	for i := range 50 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			if _, err := tm.LookupBatch(ctx, "en", "pt", "google", []string{"x"}); err != nil {
				errs <- err
			}
		}()
		go func() {
			defer wg.Done()
			if err := tm.Store(ctx, memory.Entry{SrcLang: "en", TgtLang: "pt", Engine: "google", SourceText: fmt.Sprint("s", i), TargetText: "t"}); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("acesso concorrente a :memory: falhou: %v", err)
	}
}

func TestFilePersistenceAndConcurrency(t *testing.T) {
	tm, path := openTM(t)

	const workers, items = 5, 20
	var wg sync.WaitGroup
	errs := make(chan error, workers*items*2)
	for w := range workers {
		wg.Add(2)
		go func() {
			defer wg.Done()
			for i := range items {
				err := tm.Store(ctx, memory.Entry{SrcLang: "en", TgtLang: "pt", Engine: "google",
					SourceText: fmt.Sprintf("w%d-%d", w, i), TargetText: fmt.Sprintf("t%d-%d", w, i)})
				if err != nil {
					errs <- err
				}
			}
		}()
		go func() {
			defer wg.Done()
			for i := range items {
				if _, err := tm.LookupBatch(ctx, "en", "pt", "google", []string{fmt.Sprintf("w%d-%d", w, i)}); err != nil {
					errs <- err
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("erro concorrente: %v", err)
	}
	_ = tm.Close()

	reopened, err := memory.New(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	count, err := reopened.Count(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if count != workers*items {
		t.Errorf("esperava %d registros persistidos, obteve %d", workers*items, count)
	}
}

func TestOpensDatabaseWrittenByPythonVersion(t *testing.T) {
	path := legacyDB(t, legacyRow{"Hello", "Ola", "google", "machine", "2026-01-01T00:00:00"})
	tm, err := memory.New(path)
	if err != nil {
		t.Fatalf("Go precisa abrir o memory.db existente da versão Python: %v", err)
	}
	defer tm.Close()
	wantLookup(t, tm, "google", "Hello", "Ola")
	store(t, tm, "google", "Bye", "Tchau", memory.StatusMachine)
	wantLookup(t, tm, "google", "Bye", "Tchau")
}

func TestRejectsIncompatibleSchemaUpFront(t *testing.T) {
	path := filepath.Join(t.TempDir(), "other.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("CREATE TABLE segment (id INTEGER PRIMARY KEY, source_text TEXT, target_text TEXT)"); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()

	if _, err := memory.New(path); !errors.Is(err, memory.ErrIncompatibleSchema) {
		t.Errorf("esperava ErrIncompatibleSchema, obteve %v", err)
	}
}

func TestEngineNameMigration(t *testing.T) {
	tests := []struct {
		name    string
		rows    []legacyRow
		wantRow dbRow
	}{
		{
			name:    "googleTraslator vira google",
			rows:    []legacyRow{{"T", "Ola Mundo", "googleTraslator", "machine", "2026-01-01"}},
			wantRow: dbRow{"google", "machine", "Ola Mundo"},
		},
		{
			name: "conflito mantém o status mais alto (legado revisado)",
			rows: []legacyRow{
				{"T", "Texto Revisado", "googleTraslator", "reviewed", "2026-01-01"},
				{"T", "Texto Maquina", "google", "machine", "2026-01-02"},
			},
			wantRow: dbRow{"google", "reviewed", "Texto Revisado"},
		},
		{
			name: "conflito mantém locked canônico",
			rows: []legacyRow{
				{"T", "Texto Maquina", "googleTraslator", "machine", "2026-01-01"},
				{"T", "Texto Travado", "google", "locked", "2026-01-02"},
			},
			wantRow: dbRow{"google", "locked", "Texto Travado"},
		},
		{
			name: "conflito tripartite",
			rows: []legacyRow{
				{"T", "Versao Maquina", "google", "machine", "2026-01-01"},
				{"T", "Versao Revisada", "googleTraslator", "reviewed", "2026-01-02"},
				{"T", "Versao Travada", "googleTranslator", "locked", "2026-01-03"},
			},
			wantRow: dbRow{"google", "locked", "Versao Travada"},
		},
		{
			name: "status empatado mantém o mais recente",
			rows: []legacyRow{
				{"T", "Traducao Antiga", "google", "machine", "2026-01-01T00:00:00"},
				{"T", "Traducao Nova", "googleTraslator", "machine", "2026-02-01T00:00:00"},
			},
			wantRow: dbRow{"google", "machine", "Traducao Nova"},
		},
		{
			name: "dois legados sem canônico",
			rows: []legacyRow{
				{"T", "De Maquina", "googleTraslator", "machine", "2026-01-01"},
				{"T", "Revisada", "googleTranslator", "reviewed", "2026-01-02"},
			},
			wantRow: dbRow{"google", "reviewed", "Revisada"},
		},
		{
			name:    "maiúsculas são normalizadas",
			rows:    []legacyRow{{"T", "Cap Target", "Google", "machine", "2026-01-01"}},
			wantRow: dbRow{"google", "machine", "Cap Target"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := legacyDB(t, tt.rows...)
			tm, err := memory.New(path)
			if err != nil {
				t.Fatalf("migração não pode violar a UNIQUE: %v", err)
			}
			defer tm.Close()

			wantLookup(t, tm, "Google", "T", tt.wantRow.target)
			got := rowsFor(t, path, "T")
			if len(got) != 1 || got[0] != tt.wantRow {
				t.Errorf("linhas após migração = %+v, esperava [%+v]", got, tt.wantRow)
			}
		})
	}

	t.Run("deepl e ollama", func(t *testing.T) {
		path := legacyDB(t,
			legacyRow{"DeepL Test", "Teste DeepL", "deeplTranslator", "machine", "2026-01-01"},
			legacyRow{"Ollama Test", "Teste Ollama", "ollamaTranslator", "machine", "2026-01-01"},
		)
		tm, err := memory.New(path)
		if err != nil {
			t.Fatal(err)
		}
		_ = tm.Close()
		if got := rowsFor(t, path, "DeepL Test"); len(got) != 1 || got[0].engine != "deepl" {
			t.Errorf("deeplTranslator deveria virar deepl: %+v", got)
		}
		if got := rowsFor(t, path, "Ollama Test"); len(got) != 1 || got[0].engine != "ollama" {
			t.Errorf("ollamaTranslator deveria virar ollama: %+v", got)
		}
	})
}

func gamesFor(t *testing.T, path, source string) map[string]bool {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rows, err := db.Query("SELECT game FROM game_segment WHERE source_hash = ?", memory.SourceHash(source))
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	games := map[string]bool{}
	for rows.Next() {
		var g string
		if err := rows.Scan(&g); err != nil {
			t.Fatal(err)
		}
		games[g] = true
	}
	return games
}

func TestGameSegmentLinks(t *testing.T) {
	tm, path := openTM(t)
	for _, game := range []string{"GameA", "GameB"} {
		if err := tm.Store(ctx, memory.Entry{SrcLang: "en", TgtLang: "pt", Engine: "google", SourceText: "Attack", TargetText: "Ataque", Game: game}); err != nil {
			t.Fatal(err)
		}
	}
	if got := gamesFor(t, path, "Attack"); !maps.Equal(got, map[string]bool{"GameA": true, "GameB": true}) {
		t.Errorf("cada jogo que usou o texto deve ser vinculado: %v", got)
	}

	if err := tm.LinkGameSegments(ctx, "GameC", []string{memory.SourceHash("Attack")}); err != nil {
		t.Fatal(err)
	}
	if got := gamesFor(t, path, "Attack"); len(got) != 3 || !got["GameC"] {
		t.Errorf("vínculo retroativo deveria adicionar GameC: %v", got)
	}
}

func TestSwitchEnginePolicy(t *testing.T) {
	t.Run("ignora machine de outro provedor", func(t *testing.T) {
		tm, _ := openTM(t)
		store(t, tm, "google", "Hello", "Ola Google", memory.StatusMachine)
		tm.SetPolicy(memory.Policy{SwitchEngine: true})
		wantMiss(t, tm, "deepl", "Hello")
		if got := lookupBatch(t, tm, "deepl", "Hello"); len(got) != 0 {
			t.Errorf("lote deveria ignorar machine de outro provedor: %v", got)
		}
	})
	t.Run("preserva reviewed e locked", func(t *testing.T) {
		tm, _ := openTM(t)
		store(t, tm, "google", "Save", "Salvar Revisado", memory.StatusReviewed)
		store(t, tm, "google", "Quit", "Sair Bloqueado", memory.StatusLocked)
		tm.SetPolicy(memory.Policy{SwitchEngine: true})
		want := map[string]string{"Save": "Salvar Revisado", "Quit": "Sair Bloqueado"}
		if got := lookupBatch(t, tm, "deepl", "Save", "Quit"); !maps.Equal(got, want) {
			t.Errorf("obteve %v, esperava %v", got, want)
		}
	})
	t.Run("sem provedor aceita só revisadas", func(t *testing.T) {
		tm, _ := openTM(t)
		store(t, tm, "google", "Hello", "Ola Google", memory.StatusMachine)
		store(t, tm, "google", "Save", "Salvar Revisado", memory.StatusReviewed)
		tm.SetPolicy(memory.Policy{SwitchEngine: true})
		wantMiss(t, tm, "", "Hello")
		wantLookup(t, tm, "", "Save", "Salvar Revisado")
	})
	t.Run("fallback de whitespace respeita a política", func(t *testing.T) {
		tm, _ := openTM(t)
		store(t, tm, "google", "Old   engine   text", "Old Engine Machine", memory.StatusMachine)
		store(t, tm, "deepl", "Hello   world", "Ola mundo Deepl", memory.StatusMachine)
		tm.SetPolicy(memory.Policy{SwitchEngine: true})
		wantLookup(t, tm, "deepl", "Hello world", "Ola mundo Deepl")
		wantMiss(t, tm, "deepl", "Old engine text")
	})
}

func TestClosedMemoryRejectsCalls(t *testing.T) {
	tm, _ := openTM(t)
	if err := tm.Close(); err != nil {
		t.Fatal(err)
	}
	if err := tm.Close(); err != nil {
		t.Errorf("Close deve ser idempotente: %v", err)
	}
	if _, err := tm.LookupBatch(ctx, "en", "pt", "google", []string{"x"}); !errors.Is(err, memory.ErrDBClosed) {
		t.Errorf("esperava ErrDBClosed, obteve %v", err)
	}
}
