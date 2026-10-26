import pytest

from src.translate.TranslationMemory import TranslationMemory


@pytest.fixture
def tm(tmp_path):
    return TranslationMemory(str(tmp_path / 'memory.db'), 'en', 'pt', engine='googleTraslator')


def test_dict_like_interface(tm):
    assert 'hello' not in tm
    tm['hello'] = 'ola'
    assert 'hello' in tm
    assert tm['hello'] == 'ola'


def test_cross_engine_reuse(tmp_path, tm):
    tm['hello'] = 'ola'
    other_engine = TranslationMemory(str(tmp_path / 'memory.db'), 'en', 'pt', engine='ollamaTranslator')
    assert other_engine['hello'] == 'ola'


def test_machine_write_does_not_overwrite_reviewed_translation(tm):
    """
    Bug real encontrado ao testar TMX import/export: o UPSERT original só protegia
    status='locked'. Uma retradução de máquina (status='machine') para o mesmo texto
    sobrescrevia silenciosamente o target de uma linha já revisada, mantendo o rótulo
    'reviewed' só que agora mentindo sobre o conteúdo.
    """
    tm.store('X', 'traducao_revisada', status='reviewed')
    tm.store('X', 'traducao_de_maquina', status='machine')

    assert tm.lookup('X') == 'traducao_revisada'


def test_reviewed_write_upgrades_a_machine_translation(tm):
    tm.store('X', 'traducao_de_maquina', status='machine')
    tm.store('X', 'traducao_revisada', status='reviewed')

    assert tm.lookup('X') == 'traducao_revisada'


def test_locked_entry_is_never_overwritten(tm):
    tm.lock_term('X', 'termo_fixo')
    tm.store('X', 'tentativa_machine', status='machine')
    tm.store('X', 'tentativa_reviewed', status='reviewed')

    assert tm.lookup('X') == 'termo_fixo'


def test_fuzzy_candidates_stay_in_sync_via_fts_triggers(tm):
    tm['hello world'] = 'ola mundo'
    candidates = tm.fuzzy_candidates('hello')
    assert any(c['source'] == 'hello world' for c in candidates)


# --- lookup_many / store_many -------------------------------------------------

def test_lookup_many_returns_only_hits(tm):
    tm['a'] = 'A'
    tm['b'] = 'B'
    result = tm.lookup_many(['a', 'b', 'c'])
    assert result == {'a': 'A', 'b': 'B'}
    assert 'c' not in result


def test_lookup_many_matches_lookup_for_whitespace(tm):
    tm['a  b\tc'] = 'ABC'
    # normalize_source colapsa espaços: a variante "a b c" casa no mesmo hash
    assert tm.lookup_many(['a b c']) == {'a b c': 'ABC'}


def test_lookup_many_respects_status_rank(tm):
    tm.store('X', 'maquina', status='machine')
    tm.store('X', 'revisada', status='reviewed')
    assert tm.lookup_many(['X']) == {'X': 'revisada'}
    tm.lock_term('X', 'fixa')
    assert tm.lookup_many(['X']) == {'X': 'fixa'}


def test_lookup_many_chunks_beyond_param_limit(tm):
    pairs = {f'src-{i}': f'tgt-{i}' for i in range(1000)}
    tm.store_many(pairs.items())
    result = tm.lookup_many(list(pairs))
    assert result == pairs


def test_lookup_many_ignores_blank_and_non_str(tm):
    assert tm.lookup_many(['', '   ', None, 123]) == {}


def test_store_many_single_transaction_upsert(tm):
    tm.store('X', 'revisada', status='reviewed')
    tm.store_many([('X', 'rebaixada'), ('Y', 'y'), ('Z', 'z')])
    assert tm.lookup('X') == 'revisada'  # status maior não é rebaixado
    assert tm.lookup('Y') == 'y'
    assert tm.lookup('Z') == 'z'


def test_store_many_skips_none_and_blank(tm):
    tm.store_many([('ok', 'OK'), ('', 'x'), ('  ', 'y'), ('none', None), (123, 'z')])
    assert tm.lookup_many(['ok', '', '  ', 'none']) == {'ok': 'OK'}


def test_store_many_then_lookup_many_roundtrip(tm):
    pairs = [('um', '1'), ('dois', '2'), ('tres', '3')]
    tm.store_many(pairs)
    assert tm.lookup_many([k for k, _ in pairs]) == dict(pairs)


def test_reads_see_committed_writes_from_another_thread(tm):
    import threading

    tm.store('K', 'v')
    seen = {}

    def reader():
        seen['lookup'] = tm.lookup('K')
        seen['bulk'] = tm.lookup_many(['K'])

    t = threading.Thread(target=reader)
    t.start()
    t.join()

    assert seen['lookup'] == 'v'
    assert seen['bulk'] == {'K': 'v'}


def test_store_rejects_identical_machine_translation_when_languages_differ(tm):
    # 'en' != 'pt', status='machine' -> não deve salvar
    tm.store('Apple', 'Apple', status='machine')
    assert tm.lookup('Apple') is None

    # status='reviewed' -> deve permitir salvar
    tm.store('Apple', 'Apple', status='reviewed')
    assert tm.lookup('Apple') == 'Apple'


def test_store_many_rejects_identical_machine_translation_when_languages_differ(tm):
    tm.store_many([
        ('Apple', 'Apple'),
        ('Banana', 'Banana'),
        ('Orange', 'Laranja')
    ], status='machine')

    assert tm.lookup('Apple') is None
    assert tm.lookup('Banana') is None
    assert tm.lookup('Orange') == 'Laranja'


def test_legacy_engine_migration_google_traslator_to_google(tmp_path):
    import sqlite3
    from src.translate.TranslationMemory import SCHEMA, source_hash

    db_file = str(tmp_path / 'legacy_migrate.db')
    conn = sqlite3.connect(db_file)
    conn.executescript(SCHEMA)
    h = source_hash('Hello World')
    conn.execute(
        """INSERT INTO segment (src_lang, tgt_lang, source, target, source_hash, engine, status, created_at, updated_at)
           VALUES ('en', 'pt', 'Hello World', 'Ola Mundo', ?, 'googleTraslator', 'machine', '2026-01-01', '2026-01-01')""",
        (h,)
    )
    conn.commit()
    conn.close()

    TranslationMemory._reset_pools()
    tm = TranslationMemory(db_file, 'en', 'pt', engine='google')

    assert tm.lookup('Hello World') == 'Ola Mundo'

    read_conn = sqlite3.connect(db_file)
    row = read_conn.execute("SELECT engine FROM segment WHERE source_hash=?", (h,)).fetchone()
    read_conn.close()
    assert row[0] == 'google'


def test_engine_migration_resolves_conflict_retaining_higher_status(tmp_path):
    import sqlite3
    from src.translate.TranslationMemory import SCHEMA, source_hash

    db_file = str(tmp_path / 'conflict_migrate.db')
    conn = sqlite3.connect(db_file)
    conn.executescript(SCHEMA)
    h = source_hash('Conflict Text')
    # Row 1: legacy engine with 'reviewed'
    conn.execute(
        """INSERT INTO segment (src_lang, tgt_lang, source, target, source_hash, engine, status, created_at, updated_at)
           VALUES ('en', 'pt', 'Conflict Text', 'Texto Revisado', ?, 'googleTraslator', 'reviewed', '2026-01-01', '2026-01-01')""",
        (h,)
    )
    # Row 2: new engine with 'machine'
    conn.execute(
        """INSERT INTO segment (src_lang, tgt_lang, source, target, source_hash, engine, status, created_at, updated_at)
           VALUES ('en', 'pt', 'Conflict Text', 'Texto Maquina', ?, 'google', 'machine', '2026-01-02', '2026-01-02')""",
        (h,)
    )
    conn.commit()
    conn.close()

    TranslationMemory._reset_pools()
    # Deve migrar sem disparar sqlite3.IntegrityError (UNIQUE constraint failed)
    tm = TranslationMemory(db_file, 'en', 'pt', engine='google')

    assert tm.lookup('Conflict Text') == 'Texto Revisado'

    read_conn = sqlite3.connect(db_file)
    rows = read_conn.execute("SELECT engine, status, target FROM segment WHERE source_hash=?", (h,)).fetchall()
    read_conn.close()
    assert len(rows) == 1
    assert rows[0] == ('google', 'reviewed', 'Texto Revisado')


def test_engine_migration_resolves_conflict_retaining_locked_over_machine(tmp_path):
    import sqlite3
    from src.translate.TranslationMemory import SCHEMA, source_hash

    db_file = str(tmp_path / 'conflict_locked.db')
    conn = sqlite3.connect(db_file)
    conn.executescript(SCHEMA)
    h = source_hash('Locked Text')
    # Row 1: legacy engine with 'machine'
    conn.execute(
        """INSERT INTO segment (src_lang, tgt_lang, source, target, source_hash, engine, status, created_at, updated_at)
           VALUES ('en', 'pt', 'Locked Text', 'Texto Maquina', ?, 'googleTraslator', 'machine', '2026-01-01', '2026-01-01')""",
        (h,)
    )
    # Row 2: new engine with 'locked'
    conn.execute(
        """INSERT INTO segment (src_lang, tgt_lang, source, target, source_hash, engine, status, created_at, updated_at)
           VALUES ('en', 'pt', 'Locked Text', 'Texto Travado', ?, 'google', 'locked', '2026-01-02', '2026-01-02')""",
        (h,)
    )
    conn.commit()
    conn.close()

    TranslationMemory._reset_pools()
    tm = TranslationMemory(db_file, 'en', 'pt', engine='google')

    assert tm.lookup('Locked Text') == 'Texto Travado'

    read_conn = sqlite3.connect(db_file)
    rows = read_conn.execute("SELECT engine, status, target FROM segment WHERE source_hash=?", (h,)).fetchall()
    read_conn.close()
    assert len(rows) == 1
    assert rows[0] == ('google', 'locked', 'Texto Travado')


def test_engine_migration_deepl_and_ollama(tmp_path):
    import sqlite3
    from src.translate.TranslationMemory import SCHEMA, source_hash

    db_file = str(tmp_path / 'multi_migrate.db')
    conn = sqlite3.connect(db_file)
    conn.executescript(SCHEMA)
    h1 = source_hash('DeepL Test')
    h2 = source_hash('Ollama Test')
    conn.execute(
        """INSERT INTO segment (src_lang, tgt_lang, source, target, source_hash, engine, status, created_at, updated_at)
           VALUES ('en', 'pt', 'DeepL Test', 'Teste DeepL', ?, 'deeplTranslator', 'machine', '2026-01-01', '2026-01-01')""",
        (h1,)
    )
    conn.execute(
        """INSERT INTO segment (src_lang, tgt_lang, source, target, source_hash, engine, status, created_at, updated_at)
           VALUES ('en', 'pt', 'Ollama Test', 'Teste Ollama', ?, 'ollamaTranslator', 'machine', '2026-01-01', '2026-01-01')""",
        (h2,)
    )
    conn.commit()
    conn.close()

    TranslationMemory._reset_pools()
    _ = TranslationMemory(db_file, 'en', 'pt', engine='deepl')

    read_conn = sqlite3.connect(db_file)
    r1 = read_conn.execute("SELECT engine FROM segment WHERE source_hash=?", (h1,)).fetchone()
    r2 = read_conn.execute("SELECT engine FROM segment WHERE source_hash=?", (h2,)).fetchone()
    read_conn.close()
    assert r1[0] == 'deepl'
    assert r2[0] == 'ollama'


def test_engine_migration_tripartite_conflict(tmp_path):
    import sqlite3
    from src.translate.TranslationMemory import SCHEMA, source_hash

    db_file = str(tmp_path / 'tripartite.db')
    conn = sqlite3.connect(db_file)
    conn.executescript(SCHEMA)
    h = source_hash('Tripartite Text')

    conn.execute(
        """INSERT INTO segment (src_lang, tgt_lang, source, target, source_hash, engine, status, created_at, updated_at)
           VALUES ('en', 'pt', 'Tripartite Text', 'Versao Maquina', ?, 'google', 'machine', '2026-01-01', '2026-01-01')""",
        (h,)
    )
    conn.execute(
        """INSERT INTO segment (src_lang, tgt_lang, source, target, source_hash, engine, status, created_at, updated_at)
           VALUES ('en', 'pt', 'Tripartite Text', 'Versao Revisada', ?, 'googleTraslator', 'reviewed', '2026-01-02', '2026-01-02')""",
        (h,)
    )
    conn.execute(
        """INSERT INTO segment (src_lang, tgt_lang, source, target, source_hash, engine, status, created_at, updated_at)
           VALUES ('en', 'pt', 'Tripartite Text', 'Versao Travada', ?, 'googleTranslator', 'locked', '2026-01-03', '2026-01-03')""",
        (h,)
    )
    conn.commit()
    conn.close()

    TranslationMemory._reset_pools()
    tm = TranslationMemory(db_file, 'en', 'pt', engine='google')
    assert tm.lookup('Tripartite Text') == 'Versao Travada'

    read_conn = sqlite3.connect(db_file)
    rows = read_conn.execute("SELECT engine, status, target FROM segment WHERE source_hash=?", (h,)).fetchall()
    read_conn.close()
    assert len(rows) == 1
    assert rows[0] == ('google', 'locked', 'Versao Travada')


def test_engine_migration_equal_status_keeps_newer_timestamp(tmp_path):
    import sqlite3
    from src.translate.TranslationMemory import SCHEMA, source_hash

    db_file = str(tmp_path / 'newer_ts.db')
    conn = sqlite3.connect(db_file)
    conn.executescript(SCHEMA)
    h = source_hash('Timestamp Text')

    # canonical row older
    conn.execute(
        """INSERT INTO segment (src_lang, tgt_lang, source, target, source_hash, engine, status, created_at, updated_at)
           VALUES ('en', 'pt', 'Timestamp Text', 'Traducao Antiga', ?, 'google', 'machine', '2026-01-01T00:00:00', '2026-01-01T00:00:00')""",
        (h,)
    )
    # legacy row newer
    conn.execute(
        """INSERT INTO segment (src_lang, tgt_lang, source, target, source_hash, engine, status, created_at, updated_at)
           VALUES ('en', 'pt', 'Timestamp Text', 'Traducao Nova', ?, 'googleTraslator', 'machine', '2026-02-01T00:00:00', '2026-02-01T00:00:00')""",
        (h,)
    )
    conn.commit()
    conn.close()

    TranslationMemory._reset_pools()
    tm = TranslationMemory(db_file, 'en', 'pt', engine='google')
    assert tm.lookup('Timestamp Text') == 'Traducao Nova'

    read_conn = sqlite3.connect(db_file)
    rows = read_conn.execute("SELECT engine, status, target FROM segment WHERE source_hash=?", (h,)).fetchall()
    read_conn.close()
    assert len(rows) == 1
    assert rows[0] == ('google', 'machine', 'Traducao Nova')


def test_engine_migration_two_legacies_without_canonical(tmp_path):
    import sqlite3
    from src.translate.TranslationMemory import SCHEMA, source_hash

    db_file = str(tmp_path / 'two_legacies.db')
    conn = sqlite3.connect(db_file)
    conn.executescript(SCHEMA)
    h = source_hash('No Canonical Init')

    conn.execute(
        """INSERT INTO segment (src_lang, tgt_lang, source, target, source_hash, engine, status, created_at, updated_at)
           VALUES ('en', 'pt', 'No Canonical Init', 'De Maquina', ?, 'googleTraslator', 'machine', '2026-01-01', '2026-01-01')""",
        (h,)
    )
    conn.execute(
        """INSERT INTO segment (src_lang, tgt_lang, source, target, source_hash, engine, status, created_at, updated_at)
           VALUES ('en', 'pt', 'No Canonical Init', 'Revisada', ?, 'googleTranslator', 'reviewed', '2026-01-02', '2026-01-02')""",
        (h,)
    )
    conn.commit()
    conn.close()

    TranslationMemory._reset_pools()
    tm = TranslationMemory(db_file, 'en', 'pt', engine='google')
    assert tm.lookup('No Canonical Init') == 'Revisada'

    read_conn = sqlite3.connect(db_file)
    rows = read_conn.execute("SELECT engine, status, target FROM segment WHERE source_hash=?", (h,)).fetchall()
    read_conn.close()
    assert len(rows) == 1
    assert rows[0] == ('google', 'reviewed', 'Revisada')


def test_engine_migration_capitalized_and_variants(tmp_path):
    import sqlite3
    from src.translate.TranslationMemory import SCHEMA, source_hash

    db_file = str(tmp_path / 'capitalized.db')
    conn = sqlite3.connect(db_file)
    conn.executescript(SCHEMA)
    h = source_hash('Cap Text')

    conn.execute(
        """INSERT INTO segment (src_lang, tgt_lang, source, target, source_hash, engine, status, created_at, updated_at)
           VALUES ('en', 'pt', 'Cap Text', 'Cap Target', ?, 'Google', 'machine', '2026-01-01', '2026-01-01')""",
        (h,)
    )
    conn.commit()
    conn.close()

    TranslationMemory._reset_pools()
    tm = TranslationMemory(db_file, 'en', 'pt', engine='Google')
    assert tm.engine == 'google'
    assert tm.lookup('Cap Text') == 'Cap Target'

    read_conn = sqlite3.connect(db_file)
    row = read_conn.execute("SELECT engine FROM segment WHERE source_hash=?", (h,)).fetchone()
    read_conn.close()
    assert row[0] == 'google'


def test_translation_memory_flush_commits_shared_connection(tmp_path):
    db_file = str(tmp_path / 'flush_test.db')
    TranslationMemory._reset_pools()
    tm = TranslationMemory(db_file, 'en', 'pt', engine='google')
    tm.flush()


def test_base_translate_save_cache_calls_flush(tmp_path, monkeypatch):
    from unittest.mock import MagicMock
    from src.translate.GoogleTranslate import GoogleTranslate
    monkeypatch.setattr(GoogleTranslate, 'cache_path_base', str(tmp_path))
    TranslationMemory._reset_pools()
    gt = GoogleTranslate(lang_source='en', lang_target='pt')
    mock_flush = MagicMock()
    gt.cache.flush = mock_flush
    gt.save_cache()
    mock_flush.assert_called_once()


def test_game_segment(tmp_path):
    import sqlite3
    from src.translate.TranslationMemory import source_hash
    db_file = str(tmp_path / 'game_seg.db')
    TranslationMemory._reset_pools()
    tm = TranslationMemory(db_file, 'en', 'pt', engine='google', game='GameA')
    tm.store('Hello', 'Ola')

    conn = sqlite3.connect(db_file)
    rows = conn.execute("SELECT game, source_hash FROM game_segment").fetchall()
    conn.close()

    assert len(rows) == 1
    assert rows[0] == ('GameA', source_hash('Hello'))


def test_game_segment_multi(tmp_path):
    import sqlite3
    from src.translate.TranslationMemory import source_hash
    db_file = str(tmp_path / 'game_seg_multi.db')
    TranslationMemory._reset_pools()

    tm_a = TranslationMemory(db_file, 'en', 'pt', engine='google', game='GameA')
    tm_a.store('Attack', 'Ataque')

    TranslationMemory._reset_pools()
    tm_b = TranslationMemory(db_file, 'en', 'pt', engine='google', game='GameB')
    tm_b.store('Attack', 'Ataque')

    h = source_hash('Attack')
    conn = sqlite3.connect(db_file)
    rows = conn.execute("SELECT game, source_hash FROM game_segment WHERE source_hash=?", (h,)).fetchall()
    conn.close()

    assert len(rows) == 2
    games = {r[0] for r in rows}
    assert games == {'GameA', 'GameB'}

    # Test link_game_segments
    tm_b.link_game_segments('GameC', [h])
    conn = sqlite3.connect(db_file)
    rows = conn.execute("SELECT game, source_hash FROM game_segment WHERE source_hash=?", (h,)).fetchall()
    conn.close()
    assert len(rows) == 3
    assert {r[0] for r in rows} == {'GameA', 'GameB', 'GameC'}


def test_switch_engine_ignores_machine(tmp_path):
    db_file = str(tmp_path / 'switch_eng.db')
    TranslationMemory._reset_pools()
    tm_google = TranslationMemory(db_file, 'en', 'pt', engine='google')
    tm_google.store('Hello', 'Ola Google', status='machine')

    TranslationMemory._reset_pools()
    tm_deepl = TranslationMemory(db_file, 'en', 'pt', engine='deepl', switch_engine=True)
    assert tm_deepl.lookup('Hello') is None
    assert tm_deepl.lookup_many(['Hello']) == {}


def test_switch_engine_preserves_reviewed(tmp_path):
    db_file = str(tmp_path / 'switch_eng_rev.db')
    TranslationMemory._reset_pools()
    tm_google = TranslationMemory(db_file, 'en', 'pt', engine='google')
    tm_google.store('Save', 'Salvar Revisado', status='reviewed')
    tm_google.store('Quit', 'Sair Bloqueado', status='locked')

    TranslationMemory._reset_pools()
    tm_deepl = TranslationMemory(db_file, 'en', 'pt', engine='deepl', switch_engine=True)
    assert tm_deepl.lookup('Save') == 'Salvar Revisado'
    assert tm_deepl.lookup('Quit') == 'Sair Bloqueado'
    res = tm_deepl.lookup_many(['Save', 'Quit'])
    assert res == {'Save': 'Salvar Revisado', 'Quit': 'Sair Bloqueado'}
