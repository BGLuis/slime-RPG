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

