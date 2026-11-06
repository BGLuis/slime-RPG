package memory

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

var (
	ErrEmptyLanguages  = errors.New("source and target languages cannot be empty")
	ErrEmptySourceHash = errors.New("source hash cannot be empty")
	ErrEmptyEngine     = errors.New("engine cannot be empty")
	ErrEmptySourceText = errors.New("source text cannot be empty")
	ErrEmptyTargetText = errors.New("target text cannot be empty")
	ErrDBClosed        = errors.New("translation memory database is closed")
)

// Status representa o grau de revisão de uma tradução na memória.
type Status string

const (
	StatusMachine  Status = "machine"  // Tradução gerada automaticamente por máquina/API
	StatusReviewed Status = "reviewed" // Tradução revisada por um humano
	StatusLocked   Status = "locked"   // Tradução travada/protegida contra alterações
)

// Entry representa um registro a ser consultado ou gravado na Memória de Tradução.
type Entry struct {
	SrcLang    string
	TgtLang    string
	SourceHash string
	SourceText string
	TargetText string
	Engine     string
	Status     Status
}

// SearchResult representa o retorno de uma busca textual na tabela FTS5.
type SearchResult struct {
	ID         int64
	SrcLang    string
	TgtLang    string
	SourceHash string
	SourceText string
	TargetText string
	Engine     string
	Status     Status
	Rank       float64
}

// GameSegmentLink vincula a ocorrência de um segmento a um jogo e arquivo específico.
type GameSegmentLink struct {
	GameHash    string
	SourceHash  string
	FilePath    string
	ContextKind string
}

// TranslationMemory gerencia a persistência e busca de traduções em SQLite com WAL mode e FTS5.
type TranslationMemory struct {
	db       *sql.DB
	writeMu  sync.Mutex
	isClosed bool
	dbPath   string
}

// New abre ou cria uma instância de TranslationMemory.
// Se dbPath for ":memory:", um banco em memória temporário será criado (ideal para testes).
func New(dbPath string) (*TranslationMemory, error) {
	if dbPath != ":memory:" {
		absPath, err := filepath.Abs(dbPath)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve database path: %w", err)
		}
		if err := os.MkdirAll(filepath.Dir(absPath), 0755); err != nil {
			return nil, fmt.Errorf("failed to create database directory: %w", err)
		}
		dbPath = absPath
	}

	dsn := fmt.Sprintf("%s?_journal_mode=WAL&_busy_timeout=10000&_synchronous=NORMAL&_foreign_keys=1", dbPath)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite database: %w", err)
	}

	// Limita conexões para respeitar o modelo de concorrência do SQLite
	db.SetMaxOpenConns(64)
	db.SetMaxIdleConns(16)
	db.SetConnMaxLifetime(time.Hour)

	tm := &TranslationMemory{
		db:     db,
		dbPath: dbPath,
	}

	if err := tm.initSchema(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	return tm, nil
}

func (tm *TranslationMemory) initSchema() error {
	schema := `
	CREATE TABLE IF NOT EXISTS segment (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		src_lang TEXT NOT NULL,
		tgt_lang TEXT NOT NULL,
		source_hash TEXT NOT NULL,
		source_text TEXT NOT NULL,
		target_text TEXT NOT NULL,
		engine TEXT NOT NULL,
		status TEXT NOT NULL DEFAULT 'machine',
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		UNIQUE (src_lang, tgt_lang, source_hash, engine)
	);

	CREATE INDEX IF NOT EXISTS idx_segment_lookup ON segment(src_lang, tgt_lang, engine, source_hash);
	CREATE INDEX IF NOT EXISTS idx_segment_source_hash ON segment(source_hash);

	CREATE TABLE IF NOT EXISTS game_segment (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		game_hash TEXT NOT NULL,
		source_hash TEXT NOT NULL,
		file_path TEXT,
		context_kind TEXT,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		UNIQUE (game_hash, source_hash, file_path)
	);

	CREATE VIRTUAL TABLE IF NOT EXISTS segment_fts USING fts5(
		source_text,
		target_text,
		content='segment',
		content_rowid='id'
	);

	CREATE TRIGGER IF NOT EXISTS segment_ai AFTER INSERT ON segment BEGIN
		INSERT INTO segment_fts(rowid, source_text, target_text) VALUES (new.id, new.source_text, new.target_text);
	END;

	CREATE TRIGGER IF NOT EXISTS segment_ad AFTER DELETE ON segment BEGIN
		INSERT INTO segment_fts(segment_fts, rowid, source_text, target_text) VALUES ('delete', old.id, old.source_text, old.target_text);
	END;

	CREATE TRIGGER IF NOT EXISTS segment_au AFTER UPDATE ON segment BEGIN
		INSERT INTO segment_fts(segment_fts, rowid, source_text, target_text) VALUES ('delete', old.id, old.source_text, old.target_text);
		INSERT INTO segment_fts(rowid, source_text, target_text) VALUES (new.id, new.source_text, new.target_text);
	END;
	`

	_, err := tm.db.Exec(schema)
	return err
}

// Lookup busca a tradução de uma única string por idioma, engine e hash do texto original.
func (tm *TranslationMemory) Lookup(ctx context.Context, srcLang, tgtLang, engine, sourceHash string) (string, bool, error) {
	if tm.isClosed {
		return "", false, ErrDBClosed
	}

	query := `
		SELECT target_text FROM segment 
		WHERE src_lang = ? AND tgt_lang = ? AND engine = ? AND source_hash = ?
		LIMIT 1;
	`

	var target string
	err := tm.db.QueryRowContext(ctx, query, srcLang, tgtLang, engine, sourceHash).Scan(&target)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}

	return target, true, nil
}

// LookupBatch busca traduções em massa para uma lista de hashes.
// Retorna um mapa [source_hash] -> target_text contendo apenas os hashes encontrados.
func (tm *TranslationMemory) LookupBatch(ctx context.Context, srcLang, tgtLang, engine string, hashes []string) (map[string]string, error) {
	if tm.isClosed {
		return nil, ErrDBClosed
	}
	if len(hashes) == 0 {
		return make(map[string]string), nil
	}

	// Filtra e deduplica os hashes de entrada
	uniqueHashes := make([]string, 0, len(hashes))
	seen := make(map[string]struct{}, len(hashes))
	for _, h := range hashes {
		if h == "" {
			continue
		}
		if _, exists := seen[h]; !exists {
			seen[h] = struct{}{}
			uniqueHashes = append(uniqueHashes, h)
		}
	}

	result := make(map[string]string, len(uniqueHashes))
	if len(uniqueHashes) == 0 {
		return result, nil
	}

	// O SQLite possui limite padrão de parâmetros (tipicamente 999 em versões mais antigas).
	// Processamos em lotes de até 500 por segurança.
	const chunkSize = 500
	for i := 0; i < len(uniqueHashes); i += chunkSize {
		end := i + chunkSize
		if end > len(uniqueHashes) {
			end = len(uniqueHashes)
		}
		chunk := uniqueHashes[i:end]

		placeholders := make([]string, len(chunk))
		args := make([]any, 0, len(chunk)+3)
		args = append(args, srcLang, tgtLang, engine)

		for j, h := range chunk {
			placeholders[j] = "?"
			args = append(args, h)
		}

		query := fmt.Sprintf(`
			SELECT source_hash, target_text FROM segment
			WHERE src_lang = ? AND tgt_lang = ? AND engine = ?
			  AND source_hash IN (%s);
		`, strings.Join(placeholders, ","))

		rows, err := tm.db.QueryContext(ctx, query, args...)
		if err != nil {
			return nil, err
		}

		for rows.Next() {
			var h, t string
			if err := rows.Scan(&h, &t); err != nil {
				_ = rows.Close()
				return nil, err
			}
			result[h] = t
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
		_ = rows.Close()
	}

	return result, nil
}

// Store salva ou atualiza um registro único de tradução respeitando as regras de precedência de status.
func (tm *TranslationMemory) Store(ctx context.Context, entry Entry) error {
	return tm.StoreBatch(ctx, []Entry{entry})
}

// StoreBatch grava múltiplos registros de tradução em uma única transação ACID.
// Precedência de status: 'locked' > 'reviewed' > 'machine'.
func (tm *TranslationMemory) StoreBatch(ctx context.Context, entries []Entry) error {
	if tm.isClosed {
		return ErrDBClosed
	}
	if len(entries) == 0 {
		return nil
	}

	tm.writeMu.Lock()
	defer tm.writeMu.Unlock()

	tx, err := tm.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO segment (src_lang, tgt_lang, source_hash, source_text, target_text, engine, status, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(src_lang, tgt_lang, source_hash, engine) DO UPDATE SET
			target_text = CASE 
				WHEN excluded.status = 'locked' THEN excluded.target_text
				WHEN segment.status = 'locked' THEN segment.target_text
				WHEN excluded.status = 'reviewed' THEN excluded.target_text
				WHEN segment.status = 'reviewed' THEN segment.target_text
				ELSE excluded.target_text
			END,
			status = CASE
				WHEN excluded.status = 'locked' THEN 'locked'
				WHEN segment.status = 'locked' THEN 'locked'
				WHEN excluded.status = 'reviewed' THEN 'reviewed'
				WHEN segment.status = 'reviewed' THEN 'reviewed'
				ELSE excluded.status
			END,
			updated_at = CURRENT_TIMESTAMP;
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer stmt.Close()

	for _, e := range entries {
		if e.SrcLang == "" || e.TgtLang == "" {
			return ErrEmptyLanguages
		}
		if e.SourceHash == "" {
			return ErrEmptySourceHash
		}
		if e.Engine == "" {
			return ErrEmptyEngine
		}
		if e.SourceText == "" {
			return ErrEmptySourceText
		}
		if e.TargetText == "" {
			return ErrEmptyTargetText
		}
		status := e.Status
		if status == "" {
			status = StatusMachine
		}

		if _, err := stmt.ExecContext(ctx, e.SrcLang, e.TgtLang, e.SourceHash, e.SourceText, e.TargetText, e.Engine, string(status)); err != nil {
			return fmt.Errorf("failed to insert segment %s: %w", e.SourceHash, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// LinkGameSegmentsBatch associa em massa segmentos a jogos e arquivos específicos.
func (tm *TranslationMemory) LinkGameSegmentsBatch(ctx context.Context, links []GameSegmentLink) error {
	if tm.isClosed {
		return ErrDBClosed
	}
	if len(links) == 0 {
		return nil
	}

	tm.writeMu.Lock()
	defer tm.writeMu.Unlock()

	tx, err := tm.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT OR IGNORE INTO game_segment (game_hash, source_hash, file_path, context_kind)
		VALUES (?, ?, ?, ?);
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer stmt.Close()

	for _, l := range links {
		if l.GameHash == "" || l.SourceHash == "" {
			continue
		}
		if _, err := stmt.ExecContext(ctx, l.GameHash, l.SourceHash, l.FilePath, l.ContextKind); err != nil {
			return fmt.Errorf("failed to insert game_segment link: %w", err)
		}
	}

	return tx.Commit()
}

// SearchFTS realiza busca textual de alta performance via FTS5 no texto original e traduzido.
func (tm *TranslationMemory) SearchFTS(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	if tm.isClosed {
		return nil, ErrDBClosed
	}
	if strings.TrimSpace(query) == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 20
	}

	// Escapa aspas para evitar quebra de sintaxe do operador MATCH do FTS5
	cleanQuery := strings.ReplaceAll(query, `"`, `""`)
	ftsQuery := fmt.Sprintf(`"%s"`, cleanQuery)

	sqlQuery := `
		SELECT s.id, s.src_lang, s.tgt_lang, s.source_hash, s.source_text, s.target_text, s.engine, s.status, fts.rank
		FROM segment_fts fts
		JOIN segment s ON s.id = fts.rowid
		WHERE segment_fts MATCH ?
		ORDER BY fts.rank
		LIMIT ?;
	`

	rows, err := tm.db.QueryContext(ctx, sqlQuery, ftsQuery, limit)
	if err != nil {
		// Se a busca por frase exata falhar, tenta como prefixo/tokens
		return nil, fmt.Errorf("fts search failed: %w", err)
	}
	defer rows.Close()

	var results []SearchResult
	for rows.Next() {
		var r SearchResult
		var statusStr string
		if err := rows.Scan(&r.ID, &r.SrcLang, &r.TgtLang, &r.SourceHash, &r.SourceText, &r.TargetText, &r.Engine, &statusStr, &r.Rank); err != nil {
			return nil, err
		}
		r.Status = Status(statusStr)
		results = append(results, r)
	}

	return results, rows.Err()
}

// Count retorna o número total de segmentos armazenados na memória.
func (tm *TranslationMemory) Count(ctx context.Context) (int64, error) {
	if tm.isClosed {
		return 0, ErrDBClosed
	}
	var count int64
	err := tm.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM segment;").Scan(&count)
	return count, err
}

// Close encerra a conexão com o banco de dados SQLite.
func (tm *TranslationMemory) Close() error {
	tm.writeMu.Lock()
	defer tm.writeMu.Unlock()

	if tm.isClosed {
		return nil
	}
	tm.isClosed = true
	return tm.db.Close()
}
