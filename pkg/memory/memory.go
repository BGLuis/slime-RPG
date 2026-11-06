package memory

import (
	"context"
	"crypto/sha1"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	_ "modernc.org/sqlite"
)

var (
	ErrEmptyLanguages     = errors.New("source and target languages cannot be empty")
	ErrEmptyEngine        = errors.New("engine cannot be empty")
	ErrDBClosed           = errors.New("translation memory database is closed")
	ErrIncompatibleSchema = errors.New("memory.db has an incompatible segment schema")
)

const fuzzyCandidateLimit = 20

// Status representa o grau de revisão de uma tradução na memória.
type Status string

const (
	StatusMachine  Status = "machine"  // Tradução gerada automaticamente por máquina/API
	StatusReviewed Status = "reviewed" // Tradução revisada por um humano
	StatusLocked   Status = "locked"   // Tradução travada/protegida contra alterações
)

// schema é idêntico ao da TranslationMemory em Python: as duas implementações
// abrem o mesmo memory.db (paths.MemoryDBPath), então qualquer divergência de
// colunas quebraria a memória já acumulada pelos usuários.
const schema = `
CREATE TABLE IF NOT EXISTS segment (
    id INTEGER PRIMARY KEY,
    src_lang TEXT NOT NULL,
    tgt_lang TEXT NOT NULL,
    source TEXT NOT NULL,
    target TEXT NOT NULL,
    source_hash TEXT NOT NULL,
    engine TEXT NOT NULL,
    model TEXT,
    game TEXT,
    context TEXT,
    status TEXT NOT NULL DEFAULT 'machine',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_seg_lookup
    ON segment(src_lang, tgt_lang, source_hash, engine);
CREATE INDEX IF NOT EXISTS idx_seg_status ON segment(status);
CREATE VIRTUAL TABLE IF NOT EXISTS segment_fts USING fts5(source, content='segment', content_rowid='id');

CREATE TRIGGER IF NOT EXISTS segment_ai AFTER INSERT ON segment BEGIN
    INSERT INTO segment_fts(rowid, source) VALUES (new.id, new.source);
END;
CREATE TRIGGER IF NOT EXISTS segment_au AFTER UPDATE ON segment BEGIN
    INSERT INTO segment_fts(segment_fts, rowid, source) VALUES('delete', old.id, old.source);
    INSERT INTO segment_fts(rowid, source) VALUES (new.id, new.source);
END;
CREATE TRIGGER IF NOT EXISTS segment_ad AFTER DELETE ON segment BEGIN
    INSERT INTO segment_fts(segment_fts, rowid, source) VALUES('delete', old.id, old.source);
END;

CREATE TABLE IF NOT EXISTS game_segment (
    game TEXT NOT NULL,
    source_hash TEXT NOT NULL,
    created_at TEXT NOT NULL,
    PRIMARY KEY(game, source_hash)
);
CREATE INDEX IF NOT EXISTS idx_game_segment_game ON game_segment(game);
CREATE INDEX IF NOT EXISTS idx_game_segment_hash ON game_segment(source_hash);
`

func statusRankExpr(column string) string {
	return fmt.Sprintf("CASE %s WHEN 'locked' THEN 0 WHEN 'reviewed' THEN 1 ELSE 2 END", column)
}

// upsertSQL não rebaixa uma tradução cujo status atual seja superior ao da nova gravação.
var upsertSQL = fmt.Sprintf(`INSERT INTO segment
       (src_lang, tgt_lang, source, target, source_hash, engine, game, context, status, created_at, updated_at)
   VALUES (?, ?, ?, ?, ?, ?, ?, NULL, ?, ?, ?)
   ON CONFLICT(src_lang, tgt_lang, source_hash, engine) DO UPDATE SET
       target=excluded.target, status=excluded.status, updated_at=excluded.updated_at,
       game=COALESCE(excluded.game, segment.game),
       context=COALESCE(excluded.context, segment.context)
       WHERE %s <= %s`, statusRankExpr("excluded.status"), statusRankExpr("segment.status"))

var statusRank = map[string]int{"locked": 0, "reviewed": 1, "machine": 2}

// engineAliases normaliza nomes legados de provedores gravados pela versão Python.
var engineAliases = map[string]string{
	"googletraslator":  "google",
	"googletranslator": "google",
	"googletranslate":  "google",
	"deepltranslator":  "deepl",
	"deepltranslate":   "deepl",
	"ollamatranslator": "ollama",
	"ollamatranslate":  "ollama",
}

// NormalizeEngine devolve o nome canônico (minúsculo) de um provedor.
func NormalizeEngine(engine string) string {
	lower := strings.ToLower(engine)
	if canonical, ok := engineAliases[lower]; ok {
		return canonical
	}
	return lower
}

// SourceHash é a chave de deduplicação da memória (SHA-1 do texto original),
// a mesma usada pela versão Python.
func SourceHash(text string) string {
	sum := sha1.Sum([]byte(text))
	return hex.EncodeToString(sum[:])
}

// NormalizeSource colapsa espaços em cada linha preservando as quebras de linha.
func NormalizeSource(text string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = strings.Join(strings.Fields(line), " ")
	}
	return strings.Join(lines, "\n")
}

// Entry representa um registro a ser gravado na Memória de Tradução.
type Entry struct {
	SrcLang    string
	TgtLang    string
	SourceText string
	TargetText string
	Engine     string
	Status     Status
	Game       string
}

// Policy controla quais traduções de outros provedores uma consulta pode reaproveitar.
type Policy struct {
	// StrictEngine restringe a consulta ao provedor pedido (sem reuso entre engines).
	StrictEngine bool
	// SwitchEngine descarta traduções de máquina de outros provedores, mantendo
	// apenas as revisadas/travadas: usado ao retraduzir um jogo com outro motor.
	SwitchEngine bool
}

// SearchResult representa uma sugestão retornada pela busca textual FTS5.
type SearchResult struct {
	ID         int64
	SrcLang    string
	TgtLang    string
	SourceHash string
	SourceText string
	TargetText string
	Engine     string
	Status     Status
}

// TranslationMemory gerencia a persistência e busca de traduções em SQLite com WAL mode e FTS5.
type TranslationMemory struct {
	db       *sql.DB
	writeMu  sync.Mutex
	isClosed atomic.Bool
	policyMu sync.RWMutex
	policy   Policy
}

// New abre ou cria uma instância de TranslationMemory.
// Se dbPath for ":memory:", um banco em memória temporário será criado (ideal para testes).
func New(dbPath string) (*TranslationMemory, error) {
	inMemory := dbPath == ":memory:"
	if !inMemory {
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

	if inMemory {
		// Cada conexão a ":memory:" abre um banco vazio e independente: o pool
		// precisa de uma única conexão para que todas as goroutines vejam o schema.
		db.SetMaxOpenConns(1)
	} else {
		db.SetMaxOpenConns(64)
		db.SetMaxIdleConns(16)
		db.SetConnMaxLifetime(time.Hour)
	}

	tm := &TranslationMemory{db: db}

	if err := tm.initSchema(); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := tm.migrateEngineNames(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to migrate legacy engine names: %w", err)
	}

	return tm, nil
}

func (tm *TranslationMemory) initSchema() error {
	// CREATE TABLE IF NOT EXISTS não altera uma tabela pré-existente: um banco
	// gravado por outro schema falharia de forma obscura no meio do processamento.
	rows, err := tm.db.Query("SELECT name FROM pragma_table_info('segment')")
	if err != nil {
		return fmt.Errorf("failed to inspect segment schema: %w", err)
	}
	columns := make(map[string]bool)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			_ = rows.Close()
			return err
		}
		columns[name] = true
	}
	_ = rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	if len(columns) > 0 {
		for _, required := range []string{"src_lang", "tgt_lang", "source", "target", "source_hash", "engine", "status", "game", "updated_at"} {
			if !columns[required] {
				return fmt.Errorf("%w: missing column %q", ErrIncompatibleSchema, required)
			}
		}
	}

	if _, err := tm.db.Exec(schema); err != nil {
		return fmt.Errorf("failed to initialize schema: %w", err)
	}
	return nil
}

// migrateEngineNames renomeia provedores legados (ex: googleTraslator -> google).
// Quando o nome canônico já existe para o mesmo texto, preserva a linha de status
// superior (locked > reviewed > machine) ou, empatando, a mais recente.
func (tm *TranslationMemory) migrateEngineNames() error {
	rows, err := tm.db.Query("SELECT DISTINCT engine FROM segment")
	if err != nil {
		return err
	}
	var engines []string
	for rows.Next() {
		var engine sql.NullString
		if err := rows.Scan(&engine); err != nil {
			_ = rows.Close()
			return err
		}
		if engine.Valid && engine.String != "" {
			engines = append(engines, engine.String)
		}
	}
	_ = rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	type rename struct{ from, to string }
	var renames []rename
	for _, engine := range engines {
		if canonical := NormalizeEngine(engine); canonical != engine {
			renames = append(renames, rename{engine, canonical})
		}
	}
	if len(renames) == 0 {
		return nil
	}

	tm.writeMu.Lock()
	defer tm.writeMu.Unlock()

	tx, err := tm.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	type conflict struct {
		oldID                         int64
		oldStatus, oldTarget          string
		oldUpdated                    sql.NullString
		oldContext, oldModel, oldGame sql.NullString
		newID                         int64
		newStatus                     string
		newUpdated                    sql.NullString
	}

	for _, r := range renames {
		conflictRows, err := tx.Query(`
			SELECT old.id, old.status, old.target, old.updated_at, old.context, old.model, old.game,
			       new.id, new.status, new.updated_at
			FROM segment old
			JOIN segment new ON old.src_lang = new.src_lang
			                AND old.tgt_lang = new.tgt_lang
			                AND old.source_hash = new.source_hash
			WHERE old.engine = ? AND new.engine = ?`, r.from, r.to)
		if err != nil {
			return err
		}
		var conflicts []conflict
		for conflictRows.Next() {
			var c conflict
			if err := conflictRows.Scan(&c.oldID, &c.oldStatus, &c.oldTarget, &c.oldUpdated, &c.oldContext, &c.oldModel, &c.oldGame,
				&c.newID, &c.newStatus, &c.newUpdated); err != nil {
				_ = conflictRows.Close()
				return err
			}
			conflicts = append(conflicts, c)
		}
		_ = conflictRows.Close()
		if err := conflictRows.Err(); err != nil {
			return err
		}

		for _, c := range conflicts {
			oldRank, newRank := rankOf(c.oldStatus), rankOf(c.newStatus)
			if oldRank < newRank || (oldRank == newRank && c.oldUpdated.String > c.newUpdated.String) {
				if _, err := tx.Exec(`
					UPDATE segment SET target = ?, status = ?, updated_at = ?,
					                   context = COALESCE(?, context),
					                   model = COALESCE(?, model),
					                   game = COALESCE(?, game)
					WHERE id = ?`, c.oldTarget, c.oldStatus, c.oldUpdated, c.oldContext, c.oldModel, c.oldGame, c.newID); err != nil {
					return err
				}
			}
			if _, err := tx.Exec("DELETE FROM segment WHERE id = ?", c.oldID); err != nil {
				return err
			}
		}

		if _, err := tx.Exec("UPDATE segment SET engine = ? WHERE engine = ?", r.to, r.from); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func rankOf(status string) int {
	if r, ok := statusRank[status]; ok {
		return r
	}
	return 2
}

// SetPolicy define a política de reuso entre provedores aplicada às consultas.
func (tm *TranslationMemory) SetPolicy(p Policy) {
	tm.policyMu.Lock()
	defer tm.policyMu.Unlock()
	tm.policy = p
}

func (tm *TranslationMemory) currentPolicy() Policy {
	tm.policyMu.RLock()
	defer tm.policyMu.RUnlock()
	return tm.policy
}

// engineFilter traduz a política numa cláusula SQL e no predicado equivalente,
// aplicado aos candidatos da busca por whitespace normalizado.
func engineFilter(p Policy, engine string) (clause string, args []any, accept func(status, candEngine string) bool) {
	switch {
	case p.SwitchEngine && engine != "":
		return "(status IN ('locked', 'reviewed') OR engine = ?)", []any{engine},
			func(status, candEngine string) bool {
				return status == "locked" || status == "reviewed" || candEngine == engine
			}
	case p.SwitchEngine:
		return "status IN ('locked', 'reviewed')", nil,
			func(status, _ string) bool { return status == "locked" || status == "reviewed" }
	case p.StrictEngine && engine != "":
		return "engine = ?", []any{engine},
			func(_, candEngine string) bool { return candEngine == engine }
	default:
		return "", nil, func(string, string) bool { return true }
	}
}

// Lookup busca a tradução de um único texto. engine é o provedor preferido quando
// houver traduções de vários provedores com o mesmo status.
func (tm *TranslationMemory) Lookup(ctx context.Context, srcLang, tgtLang, engine, text string) (string, bool, error) {
	hits, err := tm.LookupBatch(ctx, srcLang, tgtLang, engine, []string{text})
	if err != nil {
		return "", false, err
	}
	target, ok := hits[text]
	return target, ok, nil
}

// LookupBatch busca traduções em massa e devolve [texto original] -> tradução
// apenas para os textos encontrados. Entre várias candidatas vence o maior status,
// depois o provedor preferido e por fim a gravação mais recente. Textos sem
// correspondência exata ainda podem casar com uma entrada que só difere em espaços.
func (tm *TranslationMemory) LookupBatch(ctx context.Context, srcLang, tgtLang, engine string, texts []string) (map[string]string, error) {
	if tm.isClosed.Load() {
		return nil, ErrDBClosed
	}
	engine = NormalizeEngine(engine)
	filter, filterArgs, accept := engineFilter(tm.currentPolicy(), engine)

	seen := make(map[string]bool)
	var hashes []string
	for _, t := range texts {
		if strings.TrimSpace(t) == "" {
			continue
		}
		if h := SourceHash(t); !seen[h] {
			seen[h] = true
			hashes = append(hashes, h)
		}
	}
	result := make(map[string]string)
	if len(hashes) == 0 {
		return result, nil
	}

	order := "source_hash, " + statusRankExpr("status")
	var orderArgs []any
	if engine != "" {
		order += ", CASE WHEN engine = ? THEN 0 ELSE 1 END"
		orderArgs = append(orderArgs, engine)
	}
	order += ", updated_at DESC"

	// Limite conservador de parâmetros por statement (o padrão histórico do SQLite é 999).
	const chunkSize = 900
	hashToTarget := make(map[string]string, len(hashes))
	for i := 0; i < len(hashes); i += chunkSize {
		chunk := hashes[i:min(i+chunkSize, len(hashes))]

		args := []any{srcLang, tgtLang}
		for _, h := range chunk {
			args = append(args, h)
		}
		where := fmt.Sprintf("src_lang = ? AND tgt_lang = ? AND source_hash IN (%s)", strings.TrimSuffix(strings.Repeat("?,", len(chunk)), ","))
		if filter != "" {
			where += " AND " + filter
			args = append(args, filterArgs...)
		}
		args = append(args, orderArgs...)

		rows, err := tm.db.QueryContext(ctx, fmt.Sprintf("SELECT source_hash, target FROM segment WHERE %s ORDER BY %s", where, order), args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var h, target string
			if err := rows.Scan(&h, &target); err != nil {
				_ = rows.Close()
				return nil, err
			}
			if _, done := hashToTarget[h]; !done {
				hashToTarget[h] = target
			}
		}
		_ = rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}

	for _, t := range texts {
		if strings.TrimSpace(t) == "" {
			continue
		}
		if _, done := result[t]; done {
			continue
		}
		if target, ok := hashToTarget[SourceHash(t)]; ok {
			result[t] = target
			continue
		}
		target, ok, err := tm.whitespaceFallback(ctx, srcLang, tgtLang, t, accept)
		if err != nil {
			return nil, err
		}
		if ok {
			result[t] = target
		}
	}

	return result, nil
}

func (tm *TranslationMemory) whitespaceFallback(ctx context.Context, srcLang, tgtLang, text string, accept func(status, engine string) bool) (string, bool, error) {
	norm := NormalizeSource(text)
	candidates, err := tm.searchSource(ctx, srcLang, tgtLang, norm, fuzzyCandidateLimit)
	if err != nil {
		return "", false, err
	}
	for _, c := range candidates {
		if !accept(string(c.Status), c.Engine) {
			continue
		}
		if NormalizeSource(c.SourceText) == norm {
			return c.TargetText, true, nil
		}
	}
	return "", false, nil
}

// Store salva ou atualiza um registro único de tradução.
func (tm *TranslationMemory) Store(ctx context.Context, entry Entry) error {
	return tm.StoreBatch(ctx, []Entry{entry})
}

// StoreBatch grava múltiplos registros numa única transação.
// Nunca rebaixa um status superior (locked > reviewed > machine). Pula entradas com
// texto original ou tradução em branco e traduções de máquina idênticas ao original
// quando os idiomas diferem, para não envenenar o cache com fallbacks do provedor.
func (tm *TranslationMemory) StoreBatch(ctx context.Context, entries []Entry) error {
	if tm.isClosed.Load() {
		return ErrDBClosed
	}

	type row struct {
		e      Entry
		hash   string
		engine string
		status Status
	}
	var rows []row
	for _, e := range entries {
		if e.SrcLang == "" || e.TgtLang == "" {
			return ErrEmptyLanguages
		}
		if e.Engine == "" {
			return ErrEmptyEngine
		}
		if strings.TrimSpace(e.SourceText) == "" || e.TargetText == "" {
			continue
		}
		status := e.Status
		if status == "" {
			status = StatusMachine
		}
		if status == StatusMachine && e.SrcLang != e.TgtLang && strings.TrimSpace(e.SourceText) == strings.TrimSpace(e.TargetText) {
			continue
		}
		rows = append(rows, row{e: e, hash: SourceHash(e.SourceText), engine: NormalizeEngine(e.Engine), status: status})
	}
	if len(rows) == 0 {
		return nil
	}

	tm.writeMu.Lock()
	defer tm.writeMu.Unlock()

	tx, err := tm.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.PrepareContext(ctx, upsertSQL)
	if err != nil {
		return fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer stmt.Close()

	now := time.Now().Format("2006-01-02T15:04:05")
	for _, r := range rows {
		game := sql.NullString{String: r.e.Game, Valid: r.e.Game != ""}
		if _, err := stmt.ExecContext(ctx, r.e.SrcLang, r.e.TgtLang, r.e.SourceText, r.e.TargetText, r.hash,
			r.engine, game, string(r.status), now, now); err != nil {
			return fmt.Errorf("failed to store segment %s: %w", r.hash, err)
		}
		if game.Valid {
			if _, err := tx.ExecContext(ctx,
				"INSERT OR IGNORE INTO game_segment (game, source_hash, created_at) VALUES (?, ?, ?)",
				r.e.Game, r.hash, now); err != nil {
				return fmt.Errorf("failed to link segment %s to game: %w", r.hash, err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}
	return nil
}

// LockTerm grava uma tradução travada, que nenhuma gravação posterior sobrescreve.
func (tm *TranslationMemory) LockTerm(ctx context.Context, srcLang, tgtLang, engine, source, target string) error {
	return tm.Store(ctx, Entry{SrcLang: srcLang, TgtLang: tgtLang, Engine: engine, SourceText: source, TargetText: target, Status: StatusLocked})
}

// LinkGameSegments associa retroativamente hashes de texto (SourceHash) a um jogo.
func (tm *TranslationMemory) LinkGameSegments(ctx context.Context, game string, sourceHashes []string) error {
	if tm.isClosed.Load() {
		return ErrDBClosed
	}
	if game == "" || len(sourceHashes) == 0 {
		return nil
	}

	tm.writeMu.Lock()
	defer tm.writeMu.Unlock()

	tx, err := tm.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	now := time.Now().Format("2006-01-02T15:04:05")
	for _, h := range sourceHashes {
		if h == "" {
			continue
		}
		if _, err := tx.ExecContext(ctx,
			"INSERT OR IGNORE INTO game_segment (game, source_hash, created_at) VALUES (?, ?, ?)", game, h, now); err != nil {
			return fmt.Errorf("failed to link game segment: %w", err)
		}
	}
	return tx.Commit()
}

// SearchFTS devolve sugestões por similaridade textual do texto original (FTS5),
// que nunca são aplicadas automaticamente.
func (tm *TranslationMemory) SearchFTS(ctx context.Context, srcLang, tgtLang, query string, limit int) ([]SearchResult, error) {
	if tm.isClosed.Load() {
		return nil, ErrDBClosed
	}
	if strings.TrimSpace(query) == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 20
	}
	return tm.searchSource(ctx, srcLang, tgtLang, query, limit)
}

func (tm *TranslationMemory) searchSource(ctx context.Context, srcLang, tgtLang, query string, limit int) ([]SearchResult, error) {
	// Consulta como frase entre aspas: o texto do jogo não pode ser interpretado
	// como sintaxe do FTS5 (operadores, parênteses, ':' etc.).
	phrase := `"` + strings.ReplaceAll(query, `"`, `""`) + `"`

	rows, err := tm.db.QueryContext(ctx, `
		SELECT s.id, s.src_lang, s.tgt_lang, s.source_hash, s.source, s.target, s.engine, s.status
		FROM segment_fts
		JOIN segment s ON s.id = segment_fts.rowid
		WHERE segment_fts.source MATCH ? AND s.src_lang = ? AND s.tgt_lang = ?
		LIMIT ?`, phrase, srcLang, tgtLang, limit)
	if err != nil {
		return nil, fmt.Errorf("fts search failed: %w", err)
	}
	defer rows.Close()

	var results []SearchResult
	for rows.Next() {
		var r SearchResult
		var status string
		if err := rows.Scan(&r.ID, &r.SrcLang, &r.TgtLang, &r.SourceHash, &r.SourceText, &r.TargetText, &r.Engine, &status); err != nil {
			return nil, err
		}
		r.Status = Status(status)
		results = append(results, r)
	}
	return results, rows.Err()
}

// Count retorna o número total de segmentos armazenados na memória.
func (tm *TranslationMemory) Count(ctx context.Context) (int64, error) {
	if tm.isClosed.Load() {
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

	if tm.isClosed.Swap(true) {
		return nil
	}
	return tm.db.Close()
}
