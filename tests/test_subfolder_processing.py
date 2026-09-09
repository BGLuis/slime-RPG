import os
import shutil
import pytest
from unittest.mock import MagicMock

from main import copy_matching_files, sync_folder_tree, ensure_backup
from src.extractor.wolfrpg.WolfRPGExtractor import WolfRPGExtractor
from src.extractor.wolfrpg.WolfBinaryAdapter import (
    WolfBinaryAdapter,
    WolfFileCoder,
    WolfMap,
    WolfMapEvent,
    WolfMapPage,
    WolfCommand,
    WolfGameDat,
    WolfDatabase
)
from src.extractor.wolfrpg.WolfEventCodes import WolfEventCode


class MockTranslator:
    def __init__(self, dictionary=None):
        self.dict = dictionary or {}
        self.cache = {}
        import threading
        self.cache_lock = threading.Lock()

    def translate_batch(self, batch, progress_callback=None):
        res = []
        for text in batch:
            if isinstance(text, str):
                res.append(self.dict.get(text, f"[TR]{text}"))
            else:
                res.append(text)
        return res

    def translator(self, text, progress_callback=None):
        if isinstance(text, list):
            return self.translate_batch(text, progress_callback)
        if isinstance(text, str):
            return self.dict.get(text, f"[TR]{text}")
        return text

    def reduce_limite(self):
        pass

    def __deepcopy__(self, memo):
        return self


def test_copy_matching_files_filters_and_preserves_subfolders(tmp_path):
    src = tmp_path / "game_data"
    dst = tmp_path / "extracted_input"

    # Cria estrutura simulando Wolf RPG
    (src / "BasicData").mkdir(parents=True)
    (src / "MapData").mkdir(parents=True)
    (src / "Picture").mkdir(parents=True)
    (src / "BGM").mkdir(parents=True)

    (src / "BasicData" / "Game.dat").write_bytes(b"gamedat")
    (src / "BasicData" / "DataBase.project").write_bytes(b"project")
    (src / "MapData" / "Map001.mps").write_bytes(b"map001")
    (src / "Picture" / "hero.png").write_bytes(b"hero image")
    (src / "BGM" / "battle.ogg").write_bytes(b"audio track")
    (src / "Onryou.ttf").write_bytes(b"font data")

    copied = copy_matching_files(str(src), str(dst), extensions=['mps', 'dat', 'project', 'wolf'])

    assert set(copied) == {
        os.path.join("BasicData", "Game.dat"),
        os.path.join("BasicData", "DataBase.project"),
        os.path.join("MapData", "Map001.mps")
    }

    assert (dst / "BasicData" / "Game.dat").exists()
    assert (dst / "BasicData" / "DataBase.project").exists()
    assert (dst / "MapData" / "Map001.mps").exists()
    # Multimídia e fontes não devem ser copiadas
    assert not (dst / "Picture").exists()
    assert not (dst / "BGM").exists()
    assert not (dst / "Onryou.ttf").exists()


def test_ensure_backup_incremental(tmp_path):
    input_dir = tmp_path / "Data"
    backup_dir = tmp_path / "Data-en"

    (input_dir / "BasicData").mkdir(parents=True)
    (input_dir / "MapData").mkdir(parents=True)
    (input_dir / "Picture").mkdir(parents=True)

    (input_dir / "BasicData" / "Game.dat").write_bytes(b"original game.dat")
    (input_dir / "MapData" / "Map001.mps").write_bytes(b"original map001")
    (input_dir / "Picture" / "hero.png").write_bytes(b"hero image")
    (input_dir / "Onryou.ttf").write_bytes(b"font")

    # Simula backup preexistente incompleto (só continha a fonte)
    backup_dir.mkdir(parents=True)
    (backup_dir / "Onryou.ttf").write_bytes(b"font")

    newly = ensure_backup(str(input_dir), str(backup_dir), extensions=['mps', 'dat', 'project', 'wolf'])

    # Deve ter adicionado os arquivos em subpastas e os assets de mídia
    assert os.path.join("BasicData", "Game.dat") in newly
    assert os.path.join("MapData", "Map001.mps") in newly
    assert os.path.join("Picture", "hero.png") in newly
    assert (backup_dir / "BasicData" / "Game.dat").read_bytes() == b"original game.dat"
    assert (backup_dir / "MapData" / "Map001.mps").read_bytes() == b"original map001"
    assert (backup_dir / "Picture" / "hero.png").read_bytes() == b"hero image"

    # Modificar Game.dat em input_dir não deve alterar backup_dir (cópia independente)
    (input_dir / "BasicData" / "Game.dat").write_bytes(b"modified game.dat")
    assert (backup_dir / "BasicData" / "Game.dat").read_bytes() == b"original game.dat"

    # Segunda chamada não sobrescreve nada
    second_run = ensure_backup(str(input_dir), str(backup_dir), extensions=['mps', 'dat', 'project', 'wolf'])
    assert len(second_run) == 0


def test_sync_folder_tree_preserves_unrelated_files(tmp_path):
    output_dir = tmp_path / "output"
    game_dir = tmp_path / "game_data"

    (output_dir / "BasicData").mkdir(parents=True)
    (output_dir / "MapData").mkdir(parents=True)
    (output_dir / "BasicData" / "Game.dat").write_bytes(b"translated game.dat")
    (output_dir / "MapData" / "Map001.mps").write_bytes(b"translated map001")

    (game_dir / "BasicData").mkdir(parents=True)
    (game_dir / "MapData").mkdir(parents=True)
    (game_dir / "Picture").mkdir(parents=True)
    (game_dir / "BasicData" / "Game.dat").write_bytes(b"old game.dat")
    (game_dir / "Picture" / "hero.png").write_bytes(b"hero image")

    sync_folder_tree(str(output_dir), str(game_dir))

    # Arquivos traduzidos foram atualizados
    assert (game_dir / "BasicData" / "Game.dat").read_bytes() == b"translated game.dat"
    assert (game_dir / "MapData" / "Map001.mps").read_bytes() == b"translated map001"
    # Imagens e arquivos não modificados continuam intactos
    assert (game_dir / "Picture" / "hero.png").read_bytes() == b"hero image"


def test_wolfrpg_subfolder_end_to_end(tmp_path, monkeypatch):
    input_dir = tmp_path / "input"
    process_dir = tmp_path / "process"
    output_dir = tmp_path / "output"

    (input_dir / "BasicData").mkdir(parents=True)
    (input_dir / "MapData").mkdir(parents=True)
    process_dir.mkdir()
    output_dir.mkdir()

    monkeypatch.setattr(WolfRPGExtractor, "folderInput", str(input_dir))
    monkeypatch.setattr(WolfRPGExtractor, "folderProcess", str(process_dir))
    monkeypatch.setattr(WolfRPGExtractor, "folderOutput", str(output_dir))

    # 1. Cria MapData/Map001.mps com diálogo
    map_file = input_dir / "MapData" / "Map001.mps"
    wolf_map = WolfMap()
    wolf_map.encoding_type = 85
    wolf_map.is_utf8 = True
    wolf_map.attributes = 100
    wolf_map.version = 102
    wolf_map.unknown_str = "None"
    wolf_map.tileset_id = 1
    wolf_map.width = 5
    wolf_map.height = 5
    wolf_map.no_tiles = True

    ev = WolfMapEvent(event_id=1)
    page = WolfMapPage(page_id=0)
    page.commands = [WolfCommand(cid=WolfEventCode.SHOW_MESSAGE, string_args=["Bom dia!"])]
    ev.pages = [page]
    wolf_map.events = [ev]
    wolf_map.save(str(map_file))

    # 2. Cria BasicData/Game.dat com título
    gamedat_path = input_dir / "BasicData" / "Game.dat"
    coder = WolfFileCoder.open_write(is_utf8=True)
    coder.write(WolfGameDat.GAMEDAT_MAGIC)
    coder.write_u1(85)
    coder.write_u4(21)
    coder.write(b'\x00' * 21)
    coder.write_u4(7)
    coder.write_string("Jogo de Teste")
    coder.write_string("0000-0000")
    coder.write_u4(0)
    coder.write_string("MS Gothic")
    coder.write_string("MS UI Gothic")
    coder.write_string("Arial")
    coder.write_string("Tahoma")
    gamedat_path.write_bytes(coder.getvalue())

    # 3. Cria BasicData/MapTree.dat (não translatável - editor tree)
    (input_dir / "BasicData" / "MapTree.dat").write_bytes(b"\x00\x01\x04\x00\x07editor tree")

    mock_trans = MockTranslator({
        "Bom dia!": "Good morning!",
        "Jogo de Teste": "Test Game"
    })
    extractor = WolfRPGExtractor(mock_trans)

    # Executa process_files (deve varrer recursivamente)
    extractor.process_files()

    import concurrent.futures
    concurrent.futures.wait(extractor.futures)

    # Verifica que os arquivos intermediários em process/ preservaram a estrutura de subpastas
    assert (process_dir / "MapData" / "Map001.mps").exists()
    assert (process_dir / "BasicData" / "Game.dat").exists()

    # Executa import_files
    extractor.import_files()

    # Verifica que em output/ a estrutura de subpastas foi preservada
    out_map = output_dir / "MapData" / "Map001.mps"
    out_game = output_dir / "BasicData" / "Game.dat"
    out_tree = output_dir / "BasicData" / "MapTree.dat"

    assert out_map.exists()
    assert out_game.exists()
    assert out_tree.exists()

    # Valida conteúdo traduzido
    reloaded_map = WolfBinaryAdapter.load_file(str(out_map))
    assert reloaded_map["events"][1]["pages"][0]["list"][0]["string_args"][0] == "Good morning!"

    reloaded_game = WolfBinaryAdapter.load_file(str(out_game))
    assert reloaded_game["title"] == "Test Game"


def test_wolf_binary_leading_zero_support(tmp_path):
    # Testa que arquivos com cabeçalho contendo byte nulo inicial são lidos e salvos preservando integridade
    coder = WolfFileCoder.open_write(is_utf8=True)
    coder.write_u1(0)  # Leading null byte
    coder.write(WolfGameDat.GAMEDAT_MAGIC)
    coder.write_u1(85)
    coder.write_u4(21)
    coder.write(b'\x00' * 21)
    coder.write_u4(7)
    coder.write_string("Título Com Zero")
    coder.write_string("0000-0000")
    coder.write_u4(0)
    coder.write_string("Tahoma")
    coder.write_string("Sub1")
    coder.write_string("Sub2")
    coder.write_string("Sub3")

    g_file = tmp_path / "Game.dat"
    g_file.write_bytes(coder.getvalue())

    gd = WolfGameDat.load(str(g_file))
    assert gd.has_leading_zero is True
    assert gd.title == "Título Com Zero"
    assert gd.font == "Tahoma"

    # Salva e verifica que o byte inicial 0 continua presente
    save_file = tmp_path / "Game_saved.dat"
    gd.save(str(save_file))
    saved_bytes = save_file.read_bytes()
    assert saved_bytes[0] == 0
    assert saved_bytes[1:1 + len(WolfGameDat.GAMEDAT_MAGIC)] == WolfGameDat.GAMEDAT_MAGIC
