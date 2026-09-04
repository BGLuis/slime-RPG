import os
import shutil
import pytest
from unittest.mock import MagicMock

from src.factory import ExtractorFactory
from src.extractor.wolfrpg.WolfRPGExtractor import WolfRPGExtractor
from src.extractor.wolfrpg.WolfBinaryAdapter import (
    WolfBinaryAdapter,
    WolfFileCoder,
    WolfMap,
    WolfMapEvent,
    WolfMapPage,
    WolfCommand,
    WolfCommonEvents,
    WolfCommonEvent,
    WolfGameDat
)
from src.extractor.wolfrpg.WolfEventCodes import WolfEventCode
from src.services.GameDetector import detect_game_environment


class MockTranslator:
    def __init__(self, dictionary=None):
        self.dict = dictionary or {}
        self.cache = {}
        import threading
        self.cache_lock = threading.Lock()
        self.called_with = []

    def translate_batch(self, batch, progress_callback=None):
        res = []
        for text in batch:
            if isinstance(text, str):
                self.called_with.append(text)
                res.append(self.dict.get(text, f"[TR]{text}"))
            else:
                res.append(text)
        return res

    def translator(self, text, progress_callback=None):
        if isinstance(text, list):
            return self.translate_batch(text, progress_callback)
        if isinstance(text, str):
            self.called_with.append(text)
            return self.dict.get(text, f"[TR]{text}")
        return text

    def reduce_limite(self):
        pass

    def __deepcopy__(self, memo):
        return self



def test_wolf_rpg_extractor_registration():
    extractors = ExtractorFactory.get_available()
    assert "Wolf RPG" in extractors
    cls = extractors["Wolf RPG"]
    assert cls == WolfRPGExtractor
    assert set(['mps', 'dat', 'project', 'wolf']).issubset(set(cls.files_types))


def test_wolf_rpg_extractor_mask_patterns():
    translator = MockTranslator()
    extractor = WolfRPGExtractor(translator)

    patterns = extractor.get_mask_patterns()
    assert len(patterns) > 0

    sample_text = r"\c[2]Hero:\c[0] \v[10] gold, \self[1] status, \font[Arial]!"
    matched_tokens = []
    for p in patterns:
        for m in p.finditer(sample_text):
            matched_tokens.append(m.group(0))

    assert r"\c[2]" in matched_tokens
    assert r"\c[0]" in matched_tokens
    assert r"\v[10]" in matched_tokens
    assert r"\self[1]" in matched_tokens
    assert r"\font[Arial]" in matched_tokens


def test_wolf_rpg_extractor_fix_text_translate():
    corrupted = r"\ c [ 1 ] Olá \ v [ 2 ] mundo \ self [ 0 ] \ font [ Arial ] \r[漢字,かんじ]"
    fixed = WolfRPGExtractor.fix_text_translate(corrupted)

    assert r"\c[1]" in fixed
    assert r"\v[2]" in fixed
    assert r"\self[0]" in fixed
    assert r"\font[Arial]" in fixed
    assert "漢字" in fixed
    assert r"\r[" not in fixed

    # Teste com estrutura em dicionário
    nested = {
        "dialogue": r"\ c [ 2 ] Teste",
        "sub": [r"\ self [ 5 ] Ok"]
    }
    fixed_nested = WolfRPGExtractor.fix_text_translate(nested)
    assert fixed_nested["dialogue"] == r"\c[2] Teste"
    assert fixed_nested["sub"][0] == r"\self[5] Ok"


def test_wolf_rpg_game_detection(tmp_path):
    # Caso 1: Subpasta Data/MapData com .mps
    game_dir = tmp_path / "Game1"
    map_dir = game_dir / "Data" / "MapData"
    map_dir.mkdir(parents=True)
    (map_dir / "Map001.mps").write_bytes(b"\x00" * 10)

    detected = detect_game_environment(str(game_dir))
    assert detected is not None
    assert detected["extractor"] == "Wolf RPG"

    # Caso 2: Data.wolf presente
    game_dir2 = tmp_path / "Game2"
    game_dir2.mkdir()
    (game_dir2 / "Data.wolf").write_bytes(b"\x00" * 10)

    detected2 = detect_game_environment(str(game_dir2))
    assert detected2 is not None
    assert detected2["extractor"] == "Wolf RPG"


def test_wolf_rpg_extractor_end_to_end(tmp_path, monkeypatch):
    # Diretórios de trabalho isolados
    input_dir = tmp_path / "input"
    process_dir = tmp_path / "process"
    output_dir = tmp_path / "output"
    input_dir.mkdir()
    process_dir.mkdir()
    output_dir.mkdir()

    # Redireciona pastas de trabalho do BaseExtractor
    monkeypatch.setattr(WolfRPGExtractor, "folderInput", str(input_dir))
    monkeypatch.setattr(WolfRPGExtractor, "folderProcess", str(process_dir))
    monkeypatch.setattr(WolfRPGExtractor, "folderOutput", str(output_dir))

    # Cria mapa de entrada com diálogo transladável
    map_file = input_dir / "Map001.mps"

    wolf_map = WolfMap()
    wolf_map.encoding_type = 85  # UTF-8
    wolf_map.is_utf8 = True
    wolf_map.attributes = 100
    wolf_map.version = 102
    wolf_map.unknown_str = "None"
    wolf_map.tileset_id = 1
    wolf_map.width = 5
    wolf_map.height = 5
    wolf_map.no_tiles = True

    ev = WolfMapEvent(event_id=1)
    ev.name = "Villager"
    ev.x = 2
    ev.y = 2

    page = WolfMapPage(page_id=0)
    cmd = WolfCommand(cid=WolfEventCode.SHOW_MESSAGE, string_args=["Olá aventureiro!"])
    page.commands = [cmd]
    ev.pages = [page]
    wolf_map.events = [ev]
    wolf_map.save(str(map_file))

    # Configura tradutor mock com mapeamento exato
    mock_trans = MockTranslator({"Olá aventureiro!": "Hello adventurer!"})
    extractor = WolfRPGExtractor(mock_trans)

    # Processa o arquivo
    extractor.process_file(str(map_file))

    # Verifica status e arquivo intermediário em process/
    processed_map_path = process_dir / "Map001.mps"
    assert processed_map_path.exists()

    # Finaliza importação
    extractor.import_files()

    output_map_path = output_dir / "Map001.mps"
    assert output_map_path.exists()

    # Recarrega o mapa gerado e valida o diálogo traduzido
    reloaded_map = WolfBinaryAdapter.load_file(str(output_map_path))
    reloaded_cmd = reloaded_map["events"][1]["pages"][0]["list"][0]
    assert reloaded_cmd["string_args"][0] == "Hello adventurer!"


def test_wolf_rpg_extractor_font_and_repack_config(tmp_path, monkeypatch):
    input_dir = tmp_path / "input"
    process_dir = tmp_path / "process"
    output_dir = tmp_path / "output"
    input_dir.mkdir()
    process_dir.mkdir()
    output_dir.mkdir()

    monkeypatch.setattr(WolfRPGExtractor, "folderInput", str(input_dir))
    monkeypatch.setattr(WolfRPGExtractor, "folderProcess", str(process_dir))
    monkeypatch.setattr(WolfRPGExtractor, "folderOutput", str(output_dir))

    # Cria Game.dat sintético
    gamedat_path = input_dir / "Game.dat"
    coder = WolfFileCoder.open_write(is_utf8=True)
    coder.write(WolfGameDat.GAMEDAT_MAGIC)
    coder.write_u1(85)
    coder.write_u4(21)
    coder.write(b'\x00' * 21)
    coder.write_u4(7)
    coder.write_string("Título Original")
    coder.write_string("0000-0000")
    coder.write_u4(0)
    coder.write_string("MS Gothic")
    coder.write_string("MS UI Gothic")
    coder.write_string("Arial")
    coder.write_string("Tahoma")
    gamedat_path.write_bytes(coder.getvalue())

    mock_trans = MockTranslator({"Título Original": "Translated Title"})
    extractor = WolfRPGExtractor(mock_trans)
    extractor.apply_configuration({
        "target_font": "Tahoma",
        "repack_mode": "Empacotar arquivo Data.wolf (.wolf repack)"
    })

    assert extractor.target_font == "Tahoma"
    assert extractor.repack_wolf is True

    # Processa Game.dat
    extractor.process_file(str(gamedat_path))
    extractor.import_files()

    output_gamedat = output_dir / "Game.dat"
    assert output_gamedat.exists()

    reloaded_game = WolfBinaryAdapter.load_file(str(output_gamedat))
    assert reloaded_game["title"] == "Translated Title"
    assert reloaded_game["font"] == "Tahoma"

    # Verifica se Data.wolf foi empacotado
    output_wolf = output_dir / "Data.wolf"
    assert output_wolf.exists()
    assert output_wolf.stat().st_size > 0
