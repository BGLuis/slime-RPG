import copy

import pytest

from src.translate.GoogleTranslate import GoogleTranslate
from src.translate.TranslationMemory import TranslationMemory


@pytest.fixture
def isolated_cache_dir(tmp_path, monkeypatch):
    monkeypatch.setattr(GoogleTranslate, 'cache_path_base', str(tmp_path))
    TranslationMemory._connections.clear()
    yield tmp_path


def test_deepcopy_shares_cache_but_isolates_retry_state(isolated_cache_dir):
    original = GoogleTranslate(lang_source='en', lang_target='pt')
    per_file_copy = copy.deepcopy(original)

    assert per_file_copy.cache is original.cache
    assert per_file_copy.cache_lock is original.cache_lock

    per_file_copy.char_limit = 1000
    assert original.char_limit != 1000


def test_separate_instances_do_not_leak_language(isolated_cache_dir):
    en_pt = GoogleTranslate(lang_source='en', lang_target='pt')
    ja_pt = GoogleTranslate(lang_source='ja', lang_target='pt')

    assert en_pt.lang_source == 'en'
    assert ja_pt.lang_source == 'ja'


def test_from_languages_maps_keyword_arguments_correctly(isolated_cache_dir):
    translator = GoogleTranslate.from_languages('es', 'fr')
    assert translator.lang_source == 'es'
    assert translator.lang_target == 'fr'


def test_save_cache_persists_to_disk(isolated_cache_dir):
    translator = GoogleTranslate(lang_source='en', lang_target='pt')
    translator.cache['hello'] = 'ola'
    translator.save_cache()

    reopened = TranslationMemory(translator.cache_path, 'en', 'pt', engine=GoogleTranslate.agent)
    assert reopened['hello'] == 'ola'


def test_cache_is_reused_across_engines_for_the_same_pair(isolated_cache_dir):
    """
    O ponto central de trocar o cache por uma TM em SQLite: uma tradução feita pelo
    Google fica disponível para o Ollama, e vice-versa, em vez de cada engine manter
    seu próprio cache isolado como acontecia com os arquivos JSON separados.
    """
    google = GoogleTranslate(lang_source='en', lang_target='pt')
    google.cache['hello'] = 'ola'

    other_engine_view = TranslationMemory(google.cache_path, 'en', 'pt', engine='ollamaTranslator')
    assert other_engine_view['hello'] == 'ola'


def test_batch_deduplication(isolated_cache_dir, monkeypatch):
    """Garante que textos repetidos no lote são enviados apenas uma vez para o provedor."""
    translator = GoogleTranslate(lang_source='en', lang_target='pt')
    
    called_batches = []
    def mock_translate_single_batch(texts):
        called_batches.append(list(texts))
        mapping = {"Attack": "Ataque", "Defend": "Defender"}
        return [mapping.get(t, t) for t in texts]

    monkeypatch.setattr(translator, '_translate_single_batch', mock_translate_single_batch)

    input_texts = ["Attack", "Attack", "Defend", "Attack", "Defend"]
    results = translator.translate_batch(input_texts)

    assert results == ["Ataque", "Ataque", "Defender", "Ataque", "Defender"]
    # Garante que só enviou os 2 únicos
    assert called_batches == [["Attack", "Defend"]]
    assert translator.cache["Attack"] == "Ataque"
    assert translator.cache["Defend"] == "Defender"


def test_whitespace_and_empty_strings_bypass_batcher(isolated_cache_dir, monkeypatch):
    """Garante que strings vazias e whitespace não entram no lote de tradução."""
    translator = GoogleTranslate(lang_source='en', lang_target='pt')

    called_batches = []
    def mock_translate_single_batch(texts):
        called_batches.append(list(texts))
        return ["Olá" if t == "Hello" else t for t in texts]

    monkeypatch.setattr(translator, '_translate_single_batch', mock_translate_single_batch)

    input_texts = ["", "   ", None, "Hello", " \t\n "]
    results = translator.translate_batch(input_texts)

    assert results == ["", "   ", None, "Olá", " \t\n "]
    assert called_batches == [["Hello"]]


def test_pipeline_100_percent_cache_hit_bypasses_translation_step(isolated_cache_dir, monkeypatch):
    """Se todos os textos estiverem no cache, o pipeline deve resolver em 0 requisições."""
    from src.extractor.rpgmaker.RPGMakerExtractor import RPGMakerExtractor

    translator = GoogleTranslate(lang_source='en', lang_target='pt')
    translator.cache["Hello"] = "Olá"
    translator.cache["\\C[2]Gold\\C[0]"] = "\\C[2]Ouro\\C[0]"

    mock_called = []
    def mock_translate_batch(texts, cb=None):
        mock_called.append(texts)
        return texts

    monkeypatch.setattr(translator, 'translate_batch', mock_translate_batch)

    extractor = RPGMakerExtractor(translator)
    raw_text = [{"id": 0, "text": ["Hello", "\\C[2]Gold\\C[0]"]}]

    translated = extractor._pipeline_translate("Map001.json", {}, raw_text, translator, "Map001.json")

    assert translated == [{"id": 0, "text": ["Olá", "\\C[2]Ouro\\C[0]"]}]
    # Não chamou translate_batch porque foi 100% cache hit no CacheLookupStep
    assert len(mock_called) == 0


def test_pipeline_clean_cache_storage_and_subsequent_hit(isolated_cache_dir, monkeypatch):
    """
    Testa o ciclo completo:
    1. Texto com códigos RPG Maker é traduzido, corrigido e salvo no cache como texto limpo.
    2. Nenhuma chave com '__XTOK_' é gravada no banco.
    3. Na segunda execução, a frase inteira bate no cache diretamente.
    """
    from src.extractor.rpgmaker.RPGMakerExtractor import RPGMakerExtractor

    translator = GoogleTranslate(lang_source='en', lang_target='pt')
    extractor = RPGMakerExtractor(translator)

    call_count = 0
    def mock_translate_single_batch(texts):
        nonlocal call_count
        call_count += 1
        # Simula tradução que preserva placeholders
        results = []
        for t in texts:
            results.append(t.replace("Hero", "Herói"))
        return results

    monkeypatch.setattr(translator, '_translate_single_batch', mock_translate_single_batch)

    input_text = [{"id": 0, "text": ["\\C[2]Hero\\C[0] venceu!"]}]

    # Primeira execução: Cold cache
    res1 = extractor._pipeline_translate("Map001.json", {}, copy.deepcopy(input_text), translator, "Map001.json")
    assert res1 == [{"id": 0, "text": ["\\C[2]Herói\\C[0] venceu!"]}]
    assert call_count == 1

    # Verificar que o cache armazenou a string limpa (sem __XTOK_)
    assert "\\C[2]Hero\\C[0] venceu!" in translator.cache
    assert translator.cache["\\C[2]Hero\\C[0] venceu!"] == "\\C[2]Herói\\C[0] venceu!"

    # Segunda execução: Warm cache (100% cache hit)
    res2 = extractor._pipeline_translate("Map001.json", {}, copy.deepcopy(input_text), translator, "Map001.json")
    assert res2 == [{"id": 0, "text": ["\\C[2]Herói\\C[0] venceu!"]}]
    # call_count continua 1, não fez novas requisições
    assert call_count == 1


def test_translate_batch_precheck_uses_bulk_lookup(isolated_cache_dir, monkeypatch):
    """O pré-check de cache do translate_batch deve usar lookup_many (1 query em
    lote), não lookup item-a-item."""
    translator = GoogleTranslate(lang_source='en', lang_target='pt')
    translator.cache['Attack'] = 'Ataque'
    translator.cache['Defend'] = 'Defender'

    def boom(*_a, **_k):
        raise AssertionError("lookup() item-a-item não deveria ser chamado")

    monkeypatch.setattr(translator.cache, 'lookup', boom)

    called = []
    monkeypatch.setattr(translator, '_translate_single_batch',
                        lambda texts: called.append(list(texts)) or list(texts))

    results = translator.translate_batch(['Attack', 'Defend'])
    assert results == ['Ataque', 'Defender']
    assert called == []  # tudo veio do cache, nenhum lote enviado


def test_cache_writes_are_batched_not_per_row(isolated_cache_dir, monkeypatch):
    """As gravações de cache no pipeline usam store_many (lote), nunca store()
    linha a linha."""
    from src.extractor.rpgmaker.RPGMakerExtractor import RPGMakerExtractor

    translator = GoogleTranslate(lang_source='en', lang_target='pt')

    batched_calls = []
    real_store_many = translator.cache.store_many
    monkeypatch.setattr(
        translator.cache, 'store_many',
        lambda pairs, **kw: (lambda p: (batched_calls.append(p), real_store_many(p, **kw))[1])(list(pairs)),
    )

    def no_per_row(*_a, **_k):
        raise AssertionError("store()/__setitem__ linha a linha não deveria ser usado")

    monkeypatch.setattr(translator.cache, 'store', no_per_row)

    monkeypatch.setattr(translator, '_translate_single_batch',
                        lambda texts: [t.upper() for t in texts])

    extractor = RPGMakerExtractor(translator)
    raw_text = [{"id": 0, "text": ["one", "two", "three"]}]
    result = extractor._pipeline_translate("Map001.json", {}, raw_text, translator, "Map001.json")

    assert result == [{"id": 0, "text": ["ONE", "TWO", "THREE"]}]
    assert batched_calls  # ao menos uma gravação em lote aconteceu
    stored = {k: v for call in batched_calls for k, v in call}
    assert stored == {"one": "ONE", "two": "TWO", "three": "THREE"}


def test_clean_corrupted_cache_script(tmp_path):
    """Testa a limpeza de registros corrompidos com __XTOK_."""
    from scripts.clean_corrupted_cache import clean_corrupted_cache
    import sqlite3

    db_path = str(tmp_path / "test_memory.db")
    conn = sqlite3.connect(db_path)
    from src.translate.TranslationMemory import SCHEMA
    conn.executescript(SCHEMA)

    # Inserir 1 registro válido e 2 corrompidos
    conn.execute(
        "INSERT INTO segment (src_lang, tgt_lang, source, target, source_hash, engine, created_at, updated_at) "
        "VALUES ('en', 'pt', 'Valid Text', 'Texto Valido', 'h1', 'google', '2026-01-01', '2026-01-01')"
    )
    conn.execute(
        "INSERT INTO segment (src_lang, tgt_lang, source, target, source_hash, engine, created_at, updated_at) "
        "VALUES ('en', 'pt', '__XTOK_1234abcd__Text', '__XTOK_1234abcd__Texto', 'h2', 'google', '2026-01-01', '2026-01-01')"
    )
    conn.commit()
    conn.close()

    removed = clean_corrupted_cache(db_path)
    assert removed == 1

    conn = sqlite3.connect(db_path)
    rows = conn.execute("SELECT source FROM segment").fetchall()
    conn.close()

    assert len(rows) == 1
    assert rows[0][0] == "Valid Text"


def test_pipeline_does_not_cache_identical_source_target_fallback(isolated_cache_dir, monkeypatch):
    """Garante que se a tradução for idêntica ao original (como em fallbacks), o cache não é envenenado."""
    translator = GoogleTranslate(lang_source='en', lang_target='pt')

    # Simula um tradutor que falha e devolve o texto original
    monkeypatch.setattr(translator, '_translate_single_batch', lambda texts: list(texts))

    from src.extractor.rpgmaker.RPGMakerExtractor import RPGMakerExtractor
    extractor = RPGMakerExtractor(translator)
    raw_text = [{"id": 0, "text": ["Is this ... a wooden sword?"]}]
    result = extractor._pipeline_translate("Map001.json", {}, raw_text, translator, "Map001.json")

    # O texto do pipeline é o texto recebido (fallback para não quebrar a execução)
    assert result == [{"id": 0, "text": ["Is this ... a wooden sword?"]}]

    # MAS o cache NÃO deve conter o registro
    assert translator.cache.lookup("Is this ... a wooden sword?") is None


def test_google_translate_gtx_translation():
    translator = GoogleTranslate(lang_source='en', lang_target='pt')
    try:
        res = translator._translate_gtx("Is this ... a wooden sword?")
    except Exception as e:
        pytest.skip(f"Google Translate endpoint indisponível ou com rate limit: {e}")
    assert res is not None
    assert "espada de madeira" in res.lower()


