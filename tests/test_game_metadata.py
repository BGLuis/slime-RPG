import os

from src.services import GameMetadata as gm


def _write(base, rel, content):
    path = os.path.join(base, rel)
    os.makedirs(os.path.dirname(path), exist_ok=True)
    with open(path, 'w', encoding='utf-8') as f:
        f.write(content)


def _legacy_game(tmp_path):
    original = str(tmp_path / 'Data-en')
    translated = str(tmp_path / 'Data-pt')
    _write(original, 'a.txt', 'hello')
    _write(translated, 'a.txt', 'ola')
    _write(original, 'b.txt', 'same')
    _write(translated, 'b.txt', 'same')
    _write(original, 'img.png', 'binary')
    _write(translated, 'img.png', 'binary')
    return original, translated


def _adopt(original, translated):
    meta = gm.build_meta(
        translated, original, ['txt'], origin=gm.ORIGIN_LEGACY, extractor='Wolf RPG',
        translator=None, lang_source='en', lang_target='pt',
    )
    gm.save_meta(translated, meta)
    return meta


def _states(translated):
    return {r['file']: r['state'] for r in gm.verify(translated)}


def test_adopt_registra_so_extensoes_traduziveis_e_distingue_traduzido_de_inalterado(tmp_path):
    original, translated = _legacy_game(tmp_path)
    meta = _adopt(original, translated)

    assert set(meta['files']) == {'a.txt', 'b.txt'}
    assert meta['files']['a.txt']['status'] == 'success'
    assert meta['files']['b.txt']['status'] == 'unchanged'
    assert meta['origin'] == gm.ORIGIN_LEGACY


def test_verify_sem_metadado_devolve_none(tmp_path):
    assert gm.verify(str(tmp_path)) is None


def test_verify_detecta_edicao_manual_atualizacao_do_jogo_e_arquivo_novo(tmp_path):
    original, translated = _legacy_game(tmp_path)
    _adopt(original, translated)
    assert _states(translated) == {'a.txt': gm.STATE_INTACT, 'b.txt': gm.STATE_UNCHANGED}

    _write(translated, 'a.txt', 'olá revisado')
    assert _states(translated)['a.txt'] == gm.STATE_EDITED

    _write(original, 'b.txt', 'game update')
    _write(original, 'c.txt', 'new file')
    states = _states(translated)
    assert states['b.txt'] == gm.STATE_ORIGINAL_CHANGED
    assert states['c.txt'] == gm.STATE_NEW


def test_atualizacao_do_original_tem_precedencia_sobre_edicao_manual(tmp_path):
    original, translated = _legacy_game(tmp_path)
    _adopt(original, translated)
    _write(original, 'a.txt', 'hello v2')
    _write(translated, 'a.txt', 'editado')
    assert _states(translated)['a.txt'] == gm.STATE_ORIGINAL_CHANGED


def test_verify_detecta_traducao_removida_e_original_ausente(tmp_path):
    original, translated = _legacy_game(tmp_path)
    _adopt(original, translated)
    os.remove(os.path.join(translated, 'a.txt'))
    os.remove(os.path.join(original, 'b.txt'))
    states = _states(translated)
    assert states['a.txt'] == gm.STATE_TRANSLATION_MISSING
    assert states['b.txt'] == gm.STATE_ORIGINAL_MISSING


def test_metadado_sobrevive_a_mover_o_jogo(tmp_path):
    original, translated = _legacy_game(tmp_path)
    _adopt(original, translated)
    moved = tmp_path / 'moved'
    moved.mkdir()
    os.rename(original, str(moved / 'Data-en'))
    os.rename(translated, str(moved / 'Data-pt'))
    assert _states(str(moved / 'Data-pt'))['a.txt'] == gm.STATE_INTACT


def test_pasta_de_metadado_nao_entra_no_hash(tmp_path):
    original, translated = _legacy_game(tmp_path)
    _adopt(original, translated)
    assert all(not rel.startswith(gm.META_DIRNAME) for rel in gm.scan_hashes(translated))


def test_metadado_de_schema_futuro_e_ignorado(tmp_path):
    _write(str(tmp_path), '.slime/meta.json', '{"schema_version": 999, "files": {}}')
    assert gm.load_meta(str(tmp_path)) is None


def _wolf_game(tmp_path):
    from src.extractor.wolfrpg.WolfArchive import WolfArchive

    raw_dir = tmp_path / 'raw_basic_data'
    _write(str(raw_dir), 'CommonEvent.dat', 'original common events')
    _write(str(raw_dir), 'Game.dat', 'original game title')

    original_dir = tmp_path / 'Data-en'
    original_dir.mkdir(parents=True, exist_ok=True)
    wolf_file = original_dir / 'BasicData.wolf'
    WolfArchive.create_archive(str(raw_dir), str(wolf_file), use_compression=True, use_huffman=True)

    translated_dir = tmp_path / 'Data-pt'
    _write(str(translated_dir), 'BasicData/CommonEvent.dat', 'eventos comuns traduzidos')
    _write(str(translated_dir), 'BasicData/Game.dat', 'original game title')

    return str(original_dir), str(translated_dir)


def test_adopt_and_verify_with_wolf_archives(tmp_path):
    original, translated = _wolf_game(tmp_path)
    meta = gm.build_meta(
        translated, original, ['dat'], origin=gm.ORIGIN_LEGACY, extractor='Wolf RPG',
        translator=None, lang_source='en', lang_target='pt'
    )
    gm.save_meta(translated, meta)

    assert set(meta['files']) == {'BasicData/CommonEvent.dat', 'BasicData/Game.dat'}
    assert meta['files']['BasicData/CommonEvent.dat']['status'] == 'success'
    assert meta['files']['BasicData/Game.dat']['status'] == 'unchanged'
    assert meta['files']['BasicData/CommonEvent.dat']['container'] == 'BasicData.wolf'
    assert meta['files']['BasicData/CommonEvent.dat']['container_sha256'] is not None

    states = _states(translated)
    assert states['BasicData/CommonEvent.dat'] == gm.STATE_INTACT
    assert states['BasicData/Game.dat'] == gm.STATE_UNCHANGED


def test_verify_detects_wolf_container_update(tmp_path):
    from src.extractor.wolfrpg.WolfArchive import WolfArchive

    original, translated = _wolf_game(tmp_path)
    meta = gm.build_meta(
        translated, original, ['dat'], origin=gm.ORIGIN_LEGACY, extractor='Wolf RPG',
        translator=None, lang_source='en', lang_target='pt'
    )
    gm.save_meta(translated, meta)

    # Simula patch/update no jogo original: regera BasicData.wolf com novo conteúdo em CommonEvent.dat
    raw_v2 = tmp_path / 'raw_v2'
    _write(str(raw_v2), 'CommonEvent.dat', 'original common events PATCHED v2')
    _write(str(raw_v2), 'Game.dat', 'original game title')
    wolf_file = os.path.join(original, 'BasicData.wolf')
    WolfArchive.create_archive(str(raw_v2), wolf_file, use_compression=True, use_huffman=True)

    states = _states(translated)
    assert states['BasicData/CommonEvent.dat'] == gm.STATE_ORIGINAL_CHANGED
    assert states['BasicData/Game.dat'] == gm.STATE_UNCHANGED


def test_build_and_save_meta_without_backup(tmp_path):
    translated = str(tmp_path / 'Data-pt')
    _write(translated, 'Map001.mps', 'mapa traduzido')
    _write(translated, 'Game.dat', 'titulo traduzido')

    meta = gm.build_meta(
        translated, original_dir=None, extensions=['mps', 'dat'],
        origin=gm.ORIGIN_JOB, extractor='Wolf RPG', translator='google',
        lang_source='en', lang_target='pt'
    )
    assert meta['original_dir'] is None
    assert set(meta['files']) == {'Map001.mps', 'Game.dat'}
    assert meta['files']['Map001.mps']['original_sha256'] is None
    assert meta['files']['Map001.mps']['translated_sha256'] is not None

    gm.save_meta(translated, meta)
    states = _states(translated)
    assert states['Map001.mps'] == gm.STATE_INTACT
    assert states['Game.dat'] == gm.STATE_INTACT

    # Modificação manual
    _write(translated, 'Map001.mps', 'mapa modificado')
    states2 = _states(translated)
    assert states2['Map001.mps'] == gm.STATE_EDITED


def test_job_records_metadata_without_backup(tmp_path):
    from unittest.mock import MagicMock
    from src.job import TranslationJob

    job = TranslationJob.__new__(TranslationJob)
    job.workspace = MagicMock()
    job.workspace.output_dir = str(tmp_path / 'output')
    job.workspace.is_in_place = True
    job._backup_dir = None
    job.extractor = MagicMock()
    job.extractor.files_types = ['txt']
    job.config = MagicMock()
    job.config.extractor_name = 'Wolf RPG'
    job.config.translator_name = 'google'
    job.config.lang_source = 'en'
    job.config.lang_target = 'pt'

    _write(job.workspace.output_dir, 'sample.txt', 'conteudo')

    logged_messages = []
    job._record_game_metadata(
        [{'file': 'sample.txt', 'status': 'success'}],
        log=lambda msg, color: logged_messages.append((msg, color))
    )

    meta = gm.load_meta(job.workspace.output_dir)
    assert meta is not None
    assert meta['original_dir'] is None
    assert 'sample.txt' in meta['files']
    assert any('sem backup original' in msg for msg, color in logged_messages)

