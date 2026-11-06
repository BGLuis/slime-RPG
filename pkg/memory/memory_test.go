package memory_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/BGLuis/slime-rpg/pkg/memory"
)

func TestTranslationMemory_StoreAndLookup(t *testing.T) {
	ctx := context.Background()
	tm, err := memory.New(":memory:")
	if err != nil {
		t.Fatalf("falha ao inicializar TM em memória: %v", err)
	}
	defer tm.Close()

	entry := memory.Entry{
		SrcLang:    "en",
		TgtLang:    "pt",
		SourceHash: "hash123",
		SourceText: "Good morning",
		TargetText: "Bom dia",
		Engine:     "google",
		Status:     memory.StatusMachine,
	}

	if err := tm.Store(ctx, entry); err != nil {
		t.Fatalf("falha ao gravar entrada: %v", err)
	}

	target, found, err := tm.Lookup(ctx, "en", "pt", "google", "hash123")
	if err != nil {
		t.Fatalf("erro ao consultar: %v", err)
	}
	if !found {
		t.Fatal("esperava encontrar o segmento gravado")
	}
	if target != "Bom dia" {
		t.Errorf("esperava 'Bom dia', obteve %q", target)
	}

	// Consulta por hash inexistente
	_, foundMissing, err := tm.Lookup(ctx, "en", "pt", "google", "hash_inexistente")
	if err != nil {
		t.Fatalf("erro ao consultar inexistente: %v", err)
	}
	if foundMissing {
		t.Error("não deveria encontrar hash inexistente")
	}
}

func TestTranslationMemory_LookupBatch(t *testing.T) {
	ctx := context.Background()
	tm, err := memory.New(":memory:")
	if err != nil {
		t.Fatalf("falha ao inicializar TM: %v", err)
	}
	defer tm.Close()

	entries := []memory.Entry{
		{
			SrcLang:    "en",
			TgtLang:    "pt",
			SourceHash: "h1",
			SourceText: "Apple",
			TargetText: "Maçã",
			Engine:     "google",
		},
		{
			SrcLang:    "en",
			TgtLang:    "pt",
			SourceHash: "h2",
			SourceText: "Sword",
			TargetText: "Espada",
			Engine:     "google",
		},
		{
			SrcLang:    "en",
			TgtLang:    "pt",
			SourceHash: "h3",
			SourceText: "Shield",
			TargetText: "Escudo",
			Engine:     "google",
		},
	}

	if err := tm.StoreBatch(ctx, entries); err != nil {
		t.Fatalf("falha no StoreBatch: %v", err)
	}

	hashes := []string{"h1", "h3", "h_missing", "h2"}
	results, err := tm.LookupBatch(ctx, "en", "pt", "google", hashes)
	if err != nil {
		t.Fatalf("falha no LookupBatch: %v", err)
	}

	if len(results) != 3 {
		t.Fatalf("esperava 3 resultados encontrados, obteve %d", len(results))
	}
	if results["h1"] != "Maçã" || results["h2"] != "Espada" || results["h3"] != "Escudo" {
		t.Errorf("traduções retornadas incorretas: %+v", results)
	}
	if _, exists := results["h_missing"]; exists {
		t.Error("hash ausente não deveria constar no mapa retornado")
	}
}

func TestTranslationMemory_StatusPrecedence(t *testing.T) {
	ctx := context.Background()
	tm, err := memory.New(":memory:")
	if err != nil {
		t.Fatalf("falha ao inicializar TM: %v", err)
	}
	defer tm.Close()

	entryMachine := memory.Entry{
		SrcLang:    "ja",
		TgtLang:    "pt",
		SourceHash: "hash_status",
		SourceText: "ポーション",
		TargetText: "Poção (máquina)",
		Engine:     "google",
		Status:     memory.StatusMachine,
	}
	if err := tm.Store(ctx, entryMachine); err != nil {
		t.Fatalf("falha no Store: %v", err)
	}

	// 1. Revisado deve sobrescrever máquina
	entryReviewed := memory.Entry{
		SrcLang:    "ja",
		TgtLang:    "pt",
		SourceHash: "hash_status",
		SourceText: "ポーション",
		TargetText: "Poção (humano)",
		Engine:     "google",
		Status:     memory.StatusReviewed,
	}
	if err := tm.Store(ctx, entryReviewed); err != nil {
		t.Fatalf("falha no Store (reviewed): %v", err)
	}

	target, _, _ := tm.Lookup(ctx, "ja", "pt", "google", "hash_status")
	if target != "Poção (humano)" {
		t.Errorf("esperava que reviewed sobrescrevesse machine, obteve %q", target)
	}

	// 2. Máquina NÃO deve sobrescrever revisado
	entryMachine2 := memory.Entry{
		SrcLang:    "ja",
		TgtLang:    "pt",
		SourceHash: "hash_status",
		SourceText: "ポーション",
		TargetText: "Poção (máquina nova)",
		Engine:     "google",
		Status:     memory.StatusMachine,
	}
	if err := tm.Store(ctx, entryMachine2); err != nil {
		t.Fatalf("falha no Store (machine2): %v", err)
	}

	target, _, _ = tm.Lookup(ctx, "ja", "pt", "google", "hash_status")
	if target != "Poção (humano)" {
		t.Errorf("machine não deveria sobrescrever reviewed, obteve %q", target)
	}

	// 3. Locked deve sobrescrever reviewed
	entryLocked := memory.Entry{
		SrcLang:    "ja",
		TgtLang:    "pt",
		SourceHash: "hash_status",
		SourceText: "ポーション",
		TargetText: "Poção Sagrada (locked)",
		Engine:     "google",
		Status:     memory.StatusLocked,
	}
	if err := tm.Store(ctx, entryLocked); err != nil {
		t.Fatalf("falha no Store (locked): %v", err)
	}

	target, _, _ = tm.Lookup(ctx, "ja", "pt", "google", "hash_status")
	if target != "Poção Sagrada (locked)" {
		t.Errorf("locked deveria sobrescrever reviewed, obteve %q", target)
	}

	// 4. Reviewed e Machine NÃO devem sobrescrever locked
	if err := tm.Store(ctx, entryReviewed); err != nil {
		t.Fatalf("falha ao tentar sobrescrever locked com reviewed: %v", err)
	}
	target, _, _ = tm.Lookup(ctx, "ja", "pt", "google", "hash_status")
	if target != "Poção Sagrada (locked)" {
		t.Errorf("locked não deve ser alterado por reviewed, obteve %q", target)
	}
}

func TestTranslationMemory_FTS5Search(t *testing.T) {
	ctx := context.Background()
	tm, err := memory.New(":memory:")
	if err != nil {
		t.Fatalf("falha ao inicializar TM: %v", err)
	}
	defer tm.Close()

	entries := []memory.Entry{
		{
			SrcLang:    "en",
			TgtLang:    "pt",
			SourceHash: "h10",
			SourceText: "The brave knight entered the dark dungeon",
			TargetText: "O bravo cavaleiro entrou na masmorra escura",
			Engine:     "google",
		},
		{
			SrcLang:    "en",
			TgtLang:    "pt",
			SourceHash: "h11",
			SourceText: "A dark shadow appeared in the forest",
			TargetText: "Uma sombra escura apareceu na floresta",
			Engine:     "google",
		},
	}
	if err := tm.StoreBatch(ctx, entries); err != nil {
		t.Fatalf("falha ao gravar batch para FTS: %v", err)
	}

	results, err := tm.SearchFTS(ctx, "dungeon", 10)
	if err != nil {
		t.Fatalf("falha na busca FTS5: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("esperava 1 resultado para 'dungeon', obteve %d", len(results))
	}
	if results[0].SourceHash != "h10" {
		t.Errorf("esperava h10, obteve %s", results[0].SourceHash)
	}
}

func TestTranslationMemory_FilePersistenceAndConcurrency(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "test_tm_db_*")
	if err != nil {
		t.Fatalf("falha ao criar pasta temp: %v", err)
	}
	defer os.RemoveAll(tempDir)

	dbPath := filepath.Join(tempDir, "memory.db")
	tm, err := memory.New(dbPath)
	if err != nil {
		t.Fatalf("falha ao criar TM em arquivo: %v", err)
	}
	defer tm.Close()

	ctx := context.Background()
	const numWriters = 5
	const numReaders = 5
	const itemsPerWorker = 20

	var wg sync.WaitGroup
	wg.Add(numWriters + numReaders)

	// Escritores concorrentes
	for w := 0; w < numWriters; w++ {
		go func(workerID int) {
			defer wg.Done()
			for i := 0; i < itemsPerWorker; i++ {
				h := fmt.Sprintf("hash_w%d_i%d", workerID, i)
				_ = tm.Store(ctx, memory.Entry{
					SrcLang:    "en",
					TgtLang:    "pt",
					SourceHash: h,
					SourceText: fmt.Sprintf("Source %s", h),
					TargetText: fmt.Sprintf("Target %s", h),
					Engine:     "google",
				})
			}
		}(w)
	}

	// Leitores concorrentes
	for r := 0; r < numReaders; r++ {
		go func(workerID int) {
			defer wg.Done()
			for i := 0; i < itemsPerWorker; i++ {
				h := fmt.Sprintf("hash_w%d_i%d", workerID%numWriters, i)
				_, _, _ = tm.Lookup(ctx, "en", "pt", "google", h)
			}
		}(r)
	}

	wg.Wait()

	count, err := tm.Count(ctx)
	if err != nil {
		t.Fatalf("falha ao contar registros: %v", err)
	}
	expectedCount := int64(numWriters * itemsPerWorker)
	if count != expectedCount {
		t.Errorf("esperava %d registros, obteve %d", expectedCount, count)
	}
}

func TestTranslationMemory_LinkGameSegments(t *testing.T) {
	ctx := context.Background()
	tm, err := memory.New(":memory:")
	if err != nil {
		t.Fatalf("falha ao inicializar TM: %v", err)
	}
	defer tm.Close()

	links := []memory.GameSegmentLink{
		{
			GameHash:    "game123",
			SourceHash:  "hashA",
			FilePath:    "Map001.json",
			ContextKind: "dialogue",
		},
		{
			GameHash:    "game123",
			SourceHash:  "hashB",
			FilePath:    "Map002.json",
			ContextKind: "choice",
		},
	}

	if err := tm.LinkGameSegmentsBatch(ctx, links); err != nil {
		t.Fatalf("falha ao vincular segmentos a jogos: %v", err)
	}
}
