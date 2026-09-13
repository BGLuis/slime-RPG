"""Fast lane / desacoplamento das leituras de cache do gargalo de tradução.

Com o código anterior à correção, um arquivo 100% coberto pelo cache ficava preso
atrás do loop de `CacheLookupStep` do arquivo mais lento (que segurava o
`cache_lock` global durante milhares de queries) e/ou atrás dos slots do pool de
arquivos ocupados por traduções de rede. Estes testes provam que:

1. Um arquivo 100% cacheado é resolvido com UMA query em lote (`lookup_many`).
2. Um arquivo cacheado termina bem antes de um arquivo que chama o provedor,
   mesmo rodando concorrentes.
3. `process_files` particiona corretamente entre o fast lane e o fluxo normal.
"""
import json
import re
import threading
import time

import pytest

from src.translate.BaseTranslate import BaseTranslate
from src.translate.TranslationMemory import TranslationMemory
from src.extractor.BaseExtractor import BaseExtractor


class _SlowTranslate(BaseTranslate):
    agent = 'fakeFastLaneTranslator'
    MAX_REQUESTS_SIMULTANEOUSLY = 4
    BATCH_SLEEP = 0.4

    def _translate_single_batch(self, texts):
        time.sleep(self.BATCH_SLEEP)
        return [t.upper() for t in texts]

    def translator(self, texts, progress_callback=None):
        return texts


class _FakeExtractor(BaseExtractor):
    name = 'FakeFastLane'
    files_types = ['json']

    @staticmethod
    def extract_text(file_name, data):
        # data == {"text": [...]}; devolve a lista de strings
        return list(data.get('text', []))

    @staticmethod
    def update_json(file_name, data, new_data):
        return {'text': list(new_data)}

    @staticmethod
    def fix_text_translate(text, original_text=None):
        return text


@pytest.fixture
def env(tmp_path, monkeypatch):
    for folder in ('folderInput', 'folderProcess', 'folderOutput'):
        monkeypatch.setattr(_FakeExtractor, folder, str(tmp_path / folder))
    monkeypatch.setattr(_SlowTranslate, 'cache_path_base', str(tmp_path / 'cache'))
    TranslationMemory._reset_pools()
    _SlowTranslate._request_semaphores.clear()
    yield tmp_path
    TranslationMemory._reset_pools()


def _write_file(folder, name, strings):
    import os
    os.makedirs(folder, exist_ok=True)
    path = os.path.join(folder, name)
    with open(path, 'w', encoding='utf-8') as f:
        json.dump({'text': strings}, f)
    return path


def test_pipeline_100_percent_cache_hit_uses_single_bulk_lookup(env):
    translator = _SlowTranslate(lang_source='en', lang_target='pt')
    for i in range(50):
        translator.cache[f'src {i}'] = f'tgt {i}'

    calls = {'lookup': 0, 'lookup_many': 0}
    real_lookup = translator.cache.lookup
    real_bulk = translator.cache.lookup_many
    translator.cache.lookup = lambda t: (calls.__setitem__('lookup', calls['lookup'] + 1), real_lookup(t))[1]
    translator.cache.lookup_many = lambda ts: (calls.__setitem__('lookup_many', calls['lookup_many'] + 1), real_bulk(ts))[1]

    extractor = _FakeExtractor(translator)
    raw = [f'src {i}' for i in range(50)]
    out = extractor._pipeline_translate('f.json', {}, raw, translator, 'f.json')

    assert out == [f'tgt {i}' for i in range(50)]
    assert calls['lookup_many'] == 1
    assert calls['lookup'] == 0


def test_cached_file_finishes_far_ahead_of_slow_provider_file(env):
    translator = _SlowTranslate(lang_source='en', lang_target='pt')
    cached_strings = [f'cached {i}' for i in range(20)]
    for s in cached_strings:
        translator.cache[s] = s.upper()
    new_strings = [f'brand new {i}' for i in range(6)]

    extractor = _FakeExtractor(translator)
    done = {}
    start = time.perf_counter()

    def run(tag, strings):
        extractor._pipeline_translate(f'{tag}.json', {}, list(strings), translator, f'{tag}.json')
        done[tag] = time.perf_counter() - start

    t_slow = threading.Thread(target=run, args=('slow', new_strings))
    t_fast = threading.Thread(target=run, args=('fast', cached_strings))
    t_slow.start()
    t_fast.start()
    t_slow.join()
    t_fast.join()

    assert done['fast'] < 0.2, f"arquivo cacheado demorou {done['fast']:.3f}s"
    assert done['slow'] >= _SlowTranslate.BATCH_SLEEP
    assert done['fast'] < done['slow']


def test_process_files_partitions_cached_and_pending(env):
    translator = _SlowTranslate(lang_source='en', lang_target='pt')
    for i in range(5):
        translator.cache[f'hit {i}'] = f'HIT {i}'

    extractor = _FakeExtractor(translator)
    inp = _FakeExtractor.folderInput
    _write_file(inp, 'all_cached.json', [f'hit {i}' for i in range(5)])
    _write_file(inp, 'needs_work.json', ['hit 0', 'totally new string'])
    _write_file(inp, 'also_cached.json', ['hit 1', 'hit 2'])

    seen = []
    extractor._prescan_file  # sanity: método existe

    orig_finalize = extractor._finalize_cached_file
    orig_process = extractor.process_file
    extractor._finalize_cached_file = lambda f, e: (seen.append(('fast', f)), orig_finalize(f, e))[1]
    extractor.process_file = lambda f, **kw: (seen.append(('slow', f)), orig_process(f, **kw))[1]

    extractor.process_files()
    extractor.import_files()

    lanes = {('fast' if lane == 'fast' else 'slow'): [] for lane, _ in seen}
    for lane, f in seen:
        lanes.setdefault(lane, []).append(f.split('/')[-1])

    assert sorted(lanes.get('fast', [])) == ['all_cached.json', 'also_cached.json']
    assert lanes.get('slow', []) == ['needs_work.json']

    # saída gravada para os arquivos do fast lane
    import os
    proc = _FakeExtractor.folderProcess
    with open(os.path.join(proc, 'all_cached.json'), encoding='utf-8') as f:
        assert json.load(f)['text'] == [f'HIT {i}' for i in range(5)]


def test_finalize_cached_file_never_deepcopies_or_calls_provider(env):
    translator = _SlowTranslate(lang_source='en', lang_target='pt')
    for i in range(4):
        translator.cache[f'k{i}'] = f'V{i}'

    extractor = _FakeExtractor(translator)

    def boom(*_a, **_k):
        raise AssertionError("provedor não deveria ser chamado no fast lane")

    translator._translate_single_batch = boom

    entry = ('done.json', {}, [f'k{i}' for i in range(4)], 'done.json')
    extractor._finalize_cached_file('done.json', entry)

    import os
    with open(os.path.join(_FakeExtractor.folderProcess, 'done.json'), encoding='utf-8') as f:
        assert json.load(f)['text'] == [f'V{i}' for i in range(4)]


def test_finalize_empty_file_copies_immediately(env):
    import os
    translator = _SlowTranslate(lang_source='en', lang_target='pt')
    extractor = _FakeExtractor(translator)

    # Cria arquivo original em folderInput
    os.makedirs(_FakeExtractor.folderInput, exist_ok=True)
    input_file = os.path.join(_FakeExtractor.folderInput, 'empty_map.json')
    with open(input_file, 'w', encoding='utf-8') as f:
        json.dump({"events": []}, f)

    entry = ('empty_map.json', input_file, 'empty_map.json', {"events": []})
    extractor._finalize_empty_file('empty_map.json', entry)

    out_file = os.path.join(_FakeExtractor.folderOutput, 'empty_map.json')
    assert os.path.exists(out_file)
    with open(out_file, encoding='utf-8') as f:
        assert json.load(f) == {"events": []}


def test_process_files_routes_empty_files_to_fast_executor(env):
    import os
    translator = _SlowTranslate(lang_source='en', lang_target='pt')
    extractor = _FakeExtractor(translator)

    os.makedirs(_FakeExtractor.folderInput, exist_ok=True)
    with open(os.path.join(_FakeExtractor.folderInput, 'empty.json'), 'w', encoding='utf-8') as f:
        json.dump({'text': []}, f)
    with open(os.path.join(_FakeExtractor.folderInput, 'cached.json'), 'w', encoding='utf-8') as f:
        json.dump({'text': ['hello']}, f)
    translator.cache['hello'] = 'olá'

    extractor.process_files()
    extractor.import_files()

    assert os.path.exists(os.path.join(_FakeExtractor.folderOutput, 'empty.json'))
    assert os.path.exists(os.path.join(_FakeExtractor.folderOutput, 'cached.json'))


