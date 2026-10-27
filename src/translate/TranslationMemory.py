import hashlib
import os
import sqlite3
import threading
import time

SCHEMA = """
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
"""


def _status_rank_expr(column_ref):
    return f"CASE {column_ref} WHEN 'locked' THEN 0 WHEN 'reviewed' THEN 1 ELSE 2 END"


_STATUS_RANK = _status_rank_expr('status')

# Upsert compartilhado por store()/store_many(): não rebaixa uma tradução cujo
# status atual seja superior ao da nova gravação.
_UPSERT_SQL = f"""INSERT INTO segment
       (src_lang, tgt_lang, source, target, source_hash, engine, game, context, status, created_at, updated_at)
   VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
   ON CONFLICT(src_lang, tgt_lang, source_hash, engine) DO UPDATE SET
       target=excluded.target, status=excluded.status, updated_at=excluded.updated_at,
       game=COALESCE(excluded.game, segment.game),
       context=COALESCE(excluded.context, segment.context)
       WHERE {_status_rank_expr('excluded.status')} <= {_status_rank_expr('segment.status')}"""


def normalize_source(text):
    if not isinstance(text, str):
        return text
    return '\n'.join(' '.join(line.split()) for line in text.split('\n'))


def source_hash(text):
    if not isinstance(text, str):
        text = str(text)
    return hashlib.sha1(text.encode('utf-8')).hexdigest()


class TranslationMemory:
    """
    Backend SQLite para o cache de traduções, com interface parecida com dict
    (`in`, `[]`, `[]=`) para não exigir mudanças em BaseTranslate.translate_batch.

    Modelo de concorrência:
    - ESCRITA: uma conexão (com seu próprio lock) compartilhada por db_path entre
      todas as instâncias/threads/deepcopies que apontam para o mesmo arquivo .db.
      `store`/`store_many` serializam nesse lock e sempre commitam antes de soltá-lo.
    - LEITURA: cada thread abre a sua própria conexão (thread-local), em modo
      WAL + query_only, e lê SEM lock. Como o WAL permite N leitores concorrentes
      com 1 escritor e não há transação de escrita aberta (o writer commita sob o
      lock), cada `SELECT` autocommit enxerga o último estado commitado.
    """

    ENGINE_MIGRATION_MAP = {
        'googletraslator': 'google',
        'googletranslator': 'google',
        'googletranslate': 'google',
        'google': 'google',
        'deepltranslator': 'deepl',
        'deepltranslate': 'deepl',
        'deepl': 'deepl',
        'ollamatranslator': 'ollama',
        'ollamatranslate': 'ollama',
        'ollama': 'ollama',
    }

    _connections = {}
    _connections_lock = threading.Lock()

    # Conexões de leitura, uma por (thread, db_path). Nunca usadas para escrita.
    _read_local = threading.local()

    def __init__(
        self,
        db_path,
        src_lang,
        tgt_lang,
        engine,
        game=None,
        prefer_engine=None,
        scope='global',
        reuse_other_engines=True,
        switch_engine=False,
    ):
        self.db_path = db_path
        self.src_lang = src_lang
        self.tgt_lang = tgt_lang
        self.engine = self.ENGINE_MIGRATION_MAP.get(engine.lower(), engine.lower() if engine else engine) if engine else engine
        self.game = game
        self.prefer_engine = (
            self.ENGINE_MIGRATION_MAP.get(prefer_engine.lower(), prefer_engine.lower())
            if prefer_engine else self.engine
        )
        self.scope = scope
        self.reuse_other_engines = reuse_other_engines
        self.switch_engine = switch_engine
        self._conn, self._lock = self._get_shared_connection(db_path)

    def __deepcopy__(self, memo):
        memo[id(self)] = self
        return self

    @classmethod
    def migrate_engine_names(cls, conn):
        """
        Migra nomes legados de provedores (ex: googleTraslator -> google),
        resolvendo conflitos antes de atualizar para evitar violação da constraint UNIQUE.
        Se já existir uma entrada com o novo nome de engine para o mesmo (src_lang, tgt_lang, source_hash),
        a entrada com status superior (locked > reviewed > machine) é preservada, e a duplicata é excluída
        antes do UPDATE final.
        """
        cursor = conn.cursor()
        try:
            cursor.execute("SELECT DISTINCT engine FROM segment")
            existing_engines = [row[0] for row in cursor.fetchall() if row[0]]
        except Exception:
            return

        migrations = {}
        for eng in existing_engines:
            target = cls.ENGINE_MIGRATION_MAP.get(eng.lower(), eng.lower())
            if target != eng:
                migrations[eng] = target

        if not migrations:
            return

        rank_map = {'locked': 0, 'reviewed': 1, 'machine': 2}
        for old_eng, new_eng in migrations.items():
            cursor.execute("""
                SELECT old.id, old.status, old.target, old.updated_at, old.context, old.model, old.game,
                       new.id, new.status, new.updated_at
                FROM segment old
                JOIN segment new ON old.src_lang = new.src_lang
                                AND old.tgt_lang = new.tgt_lang
                                AND old.source_hash = new.source_hash
                WHERE old.engine = ? AND new.engine = ?
            """, (old_eng, new_eng))
            conflicts = cursor.fetchall()

            for (old_id, old_status, old_target, old_updated, old_context, old_model, old_game,
                 new_id, new_status, new_updated) in conflicts:
                old_rank = rank_map.get(old_status, 2)
                new_rank = rank_map.get(new_status, 2)
                if old_rank < new_rank or (old_rank == new_rank and (old_updated or '') > (new_updated or '')):
                    cursor.execute("""
                        UPDATE segment SET target = ?, status = ?, updated_at = ?,
                                           context = COALESCE(?, context),
                                           model = COALESCE(?, model),
                                           game = COALESCE(?, game)
                        WHERE id = ?
                    """, (old_target, old_status, old_updated, old_context, old_model, old_game, new_id))
                cursor.execute("DELETE FROM segment WHERE id = ?", (old_id,))

            cursor.execute("UPDATE segment SET engine = ? WHERE engine = ?", (new_eng, old_eng))

        conn.commit()

    @classmethod
    def _get_shared_connection(cls, db_path):
        with cls._connections_lock:
            entry = cls._connections.get(db_path)
            if entry is None:
                os.makedirs(os.path.dirname(db_path) or '.', exist_ok=True)
                conn = sqlite3.connect(db_path, check_same_thread=False)
                conn.execute('PRAGMA journal_mode=WAL')
                conn.executescript(SCHEMA)
                cls.migrate_engine_names(conn)
                conn.commit()
                entry = {'conn': conn, 'lock': threading.Lock()}
                cls._connections[db_path] = entry
        return entry['conn'], entry['lock']

    def _get_read_connection(self):
        """Conexão de leitura da thread atual para este db_path (lazy, sem lock)."""
        store = TranslationMemory._read_local
        conns = getattr(store, 'conns', None)
        if conns is None:
            conns = {}
            store.conns = conns
        conn = conns.get(self.db_path)
        if conn is None:
            os.makedirs(os.path.dirname(self.db_path) or '.', exist_ok=True)
            conn = sqlite3.connect(self.db_path, check_same_thread=False)
            conn.execute('PRAGMA journal_mode=WAL')
            conn.execute('PRAGMA query_only=1')
            conns[self.db_path] = conn
        return conn

    @classmethod
    def _reset_pools(cls):
        """Fecha e limpa as conexões de escrita e as de leitura da thread atual.

        Útil em testes/teardown. Threads que não chamarem isto continuam com suas
        conexões de leitura próprias, inofensivas (só SELECT).
        """
        with cls._connections_lock:
            for entry in cls._connections.values():
                try:
                    entry['conn'].close()
                except Exception:
                    pass
            cls._connections.clear()
        conns = getattr(cls._read_local, 'conns', None)
        if conns:
            for conn in conns.values():
                try:
                    conn.close()
                except Exception:
                    pass
            conns.clear()

    def __contains__(self, text):
        return self.lookup(text) is not None

    def __getitem__(self, text):
        result = self.lookup(text)
        if result is None:
            raise KeyError(text)
        return result

    def __setitem__(self, text, translated):
        self.store(text, translated)

    # Limite conservador de parâmetros por statement (o padrão histórico do SQLite
    # é 999; versões novas usam 32766). 900 deixa folga para os 2 params fixos.
    _LOOKUP_MANY_CHUNK = 900

    def lookup(self, text, prefer_engine=None, scope=None, reuse_other_engines=None, switch_engine=None):
        eff_prefer = (
            self.ENGINE_MIGRATION_MAP.get(prefer_engine.lower(), prefer_engine.lower())
            if prefer_engine is not None else self.prefer_engine
        )
        eff_scope = scope if scope is not None else self.scope
        eff_reuse = reuse_other_engines if reuse_other_engines is not None else self.reuse_other_engines
        eff_switch = switch_engine if switch_engine is not None else getattr(self, 'switch_engine', False)

        h = source_hash(text)
        params = [self.src_lang, self.tgt_lang, h]
        clauses = ["src_lang=?", "tgt_lang=?", "source_hash=?"]

        if eff_switch:
            if eff_prefer:
                clauses.append("(status IN ('locked', 'reviewed') OR engine=?)")
                params.append(eff_prefer)
            else:
                clauses.append("status IN ('locked', 'reviewed')")
        elif not eff_reuse and eff_prefer:
            clauses.append("engine=?")
            params.append(eff_prefer)

        if eff_scope == 'game' and self.game:
            clauses.append("(game=? OR game IS NULL OR source_hash IN (SELECT source_hash FROM game_segment WHERE game=?))")
            params.extend([self.game, self.game])

        order_clauses = [_STATUS_RANK]
        if eff_prefer:
            order_clauses.append("CASE WHEN engine=? THEN 0 ELSE 1 END")
            params.append(eff_prefer)
        if self.game:
            order_clauses.append("CASE WHEN (game=? OR source_hash IN (SELECT source_hash FROM game_segment WHERE game=?)) THEN 0 ELSE 1 END")
            params.extend([self.game, self.game])
        order_clauses.append("updated_at DESC")

        sql = f"""SELECT target FROM segment
                  WHERE {' AND '.join(clauses)}
                  ORDER BY {', '.join(order_clauses)} LIMIT 1"""

        row = self._get_read_connection().execute(sql, tuple(params)).fetchone()
        if row:
            return row[0]

        norm = normalize_source(text)
        try:
            candidates = self.fuzzy_candidates(text, limit=5)
            for cand in candidates:
                if eff_switch:
                    if cand.get('status') not in ('locked', 'reviewed') and (not eff_prefer or cand.get('engine') != eff_prefer):
                        continue
                elif not eff_reuse and eff_prefer and cand.get('engine') != eff_prefer:
                    continue
                if normalize_source(cand['source']) == norm:
                    return cand['target']
        except Exception:
            pass

        return None

    def lookup_many(self, texts, prefer_engine=None, scope=None, reuse_other_engines=None, switch_engine=None):
        """Equivalente em lote de `lookup()`.

        Retorna {texto_original: target} apenas para os textos encontrados. Respeita
        a política de engine (`prefer_engine`), escopo de jogo (`scope`), status (`locked` >
        `reviewed` > `machine`) e fallback secundário para whitespace normalizado.
        Ignora entradas não-string ou em branco.
        """
        eff_prefer = (
            self.ENGINE_MIGRATION_MAP.get(prefer_engine.lower(), prefer_engine.lower())
            if prefer_engine is not None else self.prefer_engine
        )
        eff_scope = scope if scope is not None else self.scope
        eff_reuse = reuse_other_engines if reuse_other_engines is not None else self.reuse_other_engines
        eff_switch = switch_engine if switch_engine is not None else getattr(self, 'switch_engine', False)

        wanted = {}  # source_hash -> primeiro texto original visto
        for t in texts:
            if isinstance(t, str) and t.strip():
                wanted.setdefault(source_hash(t), t)
        if not wanted:
            return {}

        conn = self._get_read_connection()
        hashes = list(wanted)
        hash_to_target = {}
        chunk = self._LOOKUP_MANY_CHUNK

        extra_where = []
        extra_where_params = []
        if eff_switch:
            if eff_prefer:
                extra_where.append("(status IN ('locked', 'reviewed') OR engine=?)")
                extra_where_params.append(eff_prefer)
            else:
                extra_where.append("status IN ('locked', 'reviewed')")
        elif not eff_reuse and eff_prefer:
            extra_where.append("engine=?")
            extra_where_params.append(eff_prefer)
        if eff_scope == 'game' and self.game:
            extra_where.append("(game=? OR game IS NULL OR source_hash IN (SELECT source_hash FROM game_segment WHERE game=?))")
            extra_where_params.extend([self.game, self.game])

        order_clauses = ["source_hash", _STATUS_RANK]
        order_params = []
        if eff_prefer:
            order_clauses.append("CASE WHEN engine=? THEN 0 ELSE 1 END")
            order_params.append(eff_prefer)
        if self.game:
            order_clauses.append("CASE WHEN (game=? OR source_hash IN (SELECT source_hash FROM game_segment WHERE game=?)) THEN 0 ELSE 1 END")
            order_params.extend([self.game, self.game])
        order_clauses.append("updated_at DESC")

        for i in range(0, len(hashes), chunk):
            part = hashes[i:i + chunk]
            placeholders = ','.join('?' * len(part))
            where_sql = f"src_lang=? AND tgt_lang=? AND source_hash IN ({placeholders})"
            if extra_where:
                where_sql += " AND " + " AND ".join(extra_where)
            query_params = [self.src_lang, self.tgt_lang, *part, *extra_where_params, *order_params]

            rows = conn.execute(
                f"""SELECT source_hash, target FROM segment
                    WHERE {where_sql}
                    ORDER BY {', '.join(order_clauses)}""",
                tuple(query_params),
            ).fetchall()
            for h, target in rows:
                if h not in hash_to_target:
                    hash_to_target[h] = target

        result = {}
        missing_texts = []
        for t in texts:
            if isinstance(t, str) and t.strip():
                h = source_hash(t)
                if h in hash_to_target:
                    result[t] = hash_to_target[h]
                else:
                    missing_texts.append(t)

        for t in missing_texts:
            norm = normalize_source(t)
            try:
                candidates = self.fuzzy_candidates(t, limit=5)
                for cand in candidates:
                    if eff_switch:
                        if cand.get('status') not in ('locked', 'reviewed') and (not eff_prefer or cand.get('engine') != eff_prefer):
                            continue
                    elif not eff_reuse and eff_prefer and cand.get('engine') != eff_prefer:
                        continue
                    if normalize_source(cand['source']) == norm:
                        result[t] = cand['target']
                        break
            except Exception:
                pass

        return result

    def _segment_row(self, source, translated, status, context, now, game=None):
        eff_game = game if game is not None else self.game
        return (
            self.src_lang, self.tgt_lang, source, translated, source_hash(source),
            self.engine, eff_game, context, status, now, now,
        )

    def store(self, text, translated, status='machine', context=None, allow_identity=False, game=None):
        if not allow_identity and status == 'machine' and self.src_lang != self.tgt_lang:
            if isinstance(text, str) and isinstance(translated, str) and text.strip() == translated.strip():
                return
        now = time.strftime('%Y-%m-%dT%H:%M:%S')
        eff_game = game if game is not None else self.game
        with self._lock:
            self._conn.execute(
                _UPSERT_SQL,
                self._segment_row(text, translated, status, context, now, game=game),
            )
            if eff_game and isinstance(text, str) and text.strip():
                self._conn.execute(
                    "INSERT OR IGNORE INTO game_segment (game, source_hash, created_at) VALUES (?, ?, ?)",
                    (eff_game, source_hash(text), now),
                )
            self._conn.commit()

    def store_many(self, pairs, status='machine', context=None, allow_identity=False, game=None):
        """Grava vários (source, translated) numa única transação / um commit.

        Mesma semântica de upsert do `store()` (não rebaixa uma tradução de status
        superior). Pula entradas cujo source não é string não-vazia ou cujo
        translated é None.
        Descarta traduções de máquina que sejam idênticas ao original quando os
        idiomas de origem e destino forem distintos (evita envenenar o cache com fallbacks).
        """
        now = time.strftime('%Y-%m-%dT%H:%M:%S')
        eff_game = game if game is not None else self.game
        valid_pairs = [
            (s, t) for s, t in pairs
            if isinstance(s, str) and s.strip() and t is not None
            and (allow_identity or not (status == 'machine' and self.src_lang != self.tgt_lang and s.strip() == t.strip()))
        ]
        if not valid_pairs:
            return
        rows = [
            self._segment_row(s, t, status, context, now, game=game)
            for s, t in valid_pairs
        ]
        with self._lock:
            self._conn.executemany(_UPSERT_SQL, rows)
            if eff_game:
                unique_hashes = {source_hash(s) for s, _ in valid_pairs}
                game_rows = [(eff_game, h, now) for h in unique_hashes]
                self._conn.executemany(
                    "INSERT OR IGNORE INTO game_segment (game, source_hash, created_at) VALUES (?, ?, ?)",
                    game_rows,
                )
            self._conn.commit()

    def link_game_segments(self, game_name, source_hashes):
        """Associa retroativamente uma lista de source_hashes a um jogo na tabela game_segment."""
        if not game_name or not source_hashes:
            return
        now = time.strftime('%Y-%m-%dT%H:%M:%S')
        rows = [(game_name, h, now) for h in set(source_hashes) if h]
        if not rows:
            return
        with self._lock:
            self._conn.executemany(
                "INSERT OR IGNORE INTO game_segment (game, source_hash, created_at) VALUES (?, ?, ?)",
                rows,
            )
            self._conn.commit()

    def lock_term(self, source_text, translated_text):
        self.store(source_text, translated_text, status='locked')

    def flush(self):
        """Força o commit de quaisquer transações/escritas na conexão compartilhada."""
        with self._lock:
            self._conn.commit()

    def fuzzy_candidates(self, text, limit=5):
        """Sugestões por similaridade textual (FTS5), nunca aplicadas automaticamente."""
        rows = self._get_read_connection().execute(
            """SELECT segment.source, segment.target, segment.status, segment.engine FROM segment_fts
               JOIN segment ON segment.id = segment_fts.rowid
               WHERE segment_fts.source MATCH ? AND segment.src_lang=? AND segment.tgt_lang=?
               LIMIT ?""",
            (normalize_source(text), self.src_lang, self.tgt_lang, limit),
        ).fetchall()
        return [{'source': r[0], 'target': r[1], 'status': r[2], 'engine': r[3]} for r in rows]
