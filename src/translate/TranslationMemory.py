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
       target=excluded.target, status=excluded.status, updated_at=excluded.updated_at
       WHERE {_status_rank_expr('excluded.status')} <= {_status_rank_expr('segment.status')}"""


def normalize_source(text):
    return ' '.join(text.split())


def source_hash(text):
    return hashlib.sha1(normalize_source(text).encode('utf-8')).hexdigest()


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

    _connections = {}
    _connections_lock = threading.Lock()

    # Conexões de leitura, uma por (thread, db_path). Nunca usadas para escrita.
    _read_local = threading.local()

    def __init__(self, db_path, src_lang, tgt_lang, engine, game=None):
        self.db_path = db_path
        self.src_lang = src_lang
        self.tgt_lang = tgt_lang
        self.engine = engine
        self.game = game
        self._conn, self._lock = self._get_shared_connection(db_path)

    @classmethod
    def _get_shared_connection(cls, db_path):
        with cls._connections_lock:
            entry = cls._connections.get(db_path)
            if entry is None:
                os.makedirs(os.path.dirname(db_path) or '.', exist_ok=True)
                conn = sqlite3.connect(db_path, check_same_thread=False)
                conn.execute('PRAGMA journal_mode=WAL')
                conn.executescript(SCHEMA)
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

    def lookup(self, text):
        h = source_hash(text)
        row = self._get_read_connection().execute(
            f"""SELECT target FROM segment
                WHERE src_lang=? AND tgt_lang=? AND source_hash=?
                ORDER BY {_STATUS_RANK} LIMIT 1""",
            (self.src_lang, self.tgt_lang, h),
        ).fetchone()
        return row[0] if row else None

    def lookup_many(self, texts):
        """Equivalente em lote de `lookup()`.

        Retorna {texto_original: target} apenas para os textos encontrados. Usa o
        mesmo casamento engine-agnóstico por `source_hash` (whitespace colapsado)
        e a mesma preferência de status (`locked` > `reviewed` > `machine`).
        Ignora entradas não-string ou em branco.
        """
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
        for i in range(0, len(hashes), chunk):
            part = hashes[i:i + chunk]
            placeholders = ','.join('?' * len(part))
            rows = conn.execute(
                f"""SELECT source_hash, target, {_STATUS_RANK} AS rank FROM segment
                    WHERE src_lang=? AND tgt_lang=? AND source_hash IN ({placeholders})
                    ORDER BY source_hash, rank""",
                (self.src_lang, self.tgt_lang, *part),
            ).fetchall()
            for h, target, _rank in rows:
                if h not in hash_to_target:  # 1ª linha por hash == melhor status
                    hash_to_target[h] = target

        result = {}
        for t in texts:
            if isinstance(t, str) and t.strip():
                h = source_hash(t)
                if h in hash_to_target:
                    result[t] = hash_to_target[h]
        return result

    def _segment_row(self, source, translated, status, context, now):
        return (
            self.src_lang, self.tgt_lang, source, translated, source_hash(source),
            self.engine, self.game, context, status, now, now,
        )

    def store(self, text, translated, status='machine', context=None):
        now = time.strftime('%Y-%m-%dT%H:%M:%S')
        with self._lock:
            self._conn.execute(
                _UPSERT_SQL,
                self._segment_row(text, translated, status, context, now),
            )
            self._conn.commit()

    def store_many(self, pairs, status='machine', context=None):
        """Grava vários (source, translated) numa única transação / um commit.

        Mesma semântica de upsert do `store()` (não rebaixa uma tradução de status
        superior). Pula entradas cujo source não é string não-vazia ou cujo
        translated é None.
        """
        now = time.strftime('%Y-%m-%dT%H:%M:%S')
        rows = [
            self._segment_row(s, t, status, context, now)
            for s, t in pairs
            if isinstance(s, str) and s.strip() and t is not None
        ]
        if not rows:
            return
        with self._lock:
            self._conn.executemany(_UPSERT_SQL, rows)
            self._conn.commit()

    def lock_term(self, source_text, translated_text):
        self.store(source_text, translated_text, status='locked')

    def fuzzy_candidates(self, text, limit=5):
        """Sugestões por similaridade textual (FTS5), nunca aplicadas automaticamente."""
        rows = self._get_read_connection().execute(
            """SELECT segment.source, segment.target, segment.status FROM segment_fts
               JOIN segment ON segment.id = segment_fts.rowid
               WHERE segment_fts.source MATCH ? AND segment.src_lang=? AND segment.tgt_lang=?
               LIMIT ?""",
            (normalize_source(text), self.src_lang, self.tgt_lang, limit),
        ).fetchall()
        return [{'source': r[0], 'target': r[1], 'status': r[2]} for r in rows]
