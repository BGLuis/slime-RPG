import os
import tempfile
import threading
import asyncio
from pathlib import Path
import pytest

from src.extractor.BaseExtractor import BaseExtractor
from src.extractor.wolfrpg.WolfRPGExtractor import WolfRPGExtractor
from src.extractor.wolfrpg.WolfBinaryAdapter import (
    WolfFileCoder, WolfMap, WolfMapEvent, WolfMapPage, WolfCommand, WolfGameDat
)
from src.extractor.wolfrpg.WolfEventCodes import WolfEventCode
from src.cli import _StatusApp
from tests.test_cli_textual import run_async


class DummyExtractor(BaseExtractor):
    name = "Dummy"
    files_types = ["json"]

    def extract_text(file_name, data):
        return data

    def update_json(file_name, data, new_data):
        return new_data

    def fix_text_translate(text, original_text=None):
        return text


class MockTranslator:
    def __init__(self, dictionary=None):
        self.dict = dictionary or {}
        self.cache = {}
        self.cache_lock = threading.Lock()

    def translate_batch(self, batch, progress_callback=None):
        res = []
        for text in batch:
            if isinstance(text, str):
                res.append(self.dict.get(text, f"[TR]{text}"))
            else:
                res.append(text)
        if progress_callback:
            progress_callback(len(batch), len(batch), 0)
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


def test_normalize_status_file_paths(tmp_path):
    extractor = DummyExtractor(MockTranslator())
    extractor.folderInput = str(tmp_path / "input")
    extractor.folderProcess = str(tmp_path / "process")
    extractor.folderOutput = str(tmp_path / "output")

    # 1. Caminho relativo padrão
    assert extractor.normalize_status_file("BasicData/Game.dat") == os.path.normpath("BasicData/Game.dat")

    # 2. Caminho relativo com prefixo da pasta input
    input_prefixed = os.path.join(extractor.folderInput, "BasicData", "Game.dat")
    assert extractor.normalize_status_file(input_prefixed) == os.path.normpath("BasicData/Game.dat")

    # 3. Caminho com ./input/
    relative_prefixed = os.path.join("input", "MapData", "Map001.mps")
    extractor.folderInput = "input"
    assert extractor.normalize_status_file(relative_prefixed) == os.path.normpath("MapData/Map001.mps")

    # 4. Arquivo simples na raiz
    assert extractor.normalize_status_file("Actors.json") == "Actors.json"
    assert extractor.normalize_status_file(os.path.join("input", "Actors.json")) == "Actors.json"


def test_add_threads_status_deduplication():
    extractor = DummyExtractor(MockTranslator())
    extractor.folderInput = "input"

    # Adiciona status inicial com caminho relativo
    extractor.add_threads_status({
        'file': 'BasicData/Game.dat',
        'status': 'waiting',
        'msg': 'Na fila de processamento...'
    })
    assert len(extractor.threads_status) == 1
    assert extractor.threads_status[0]['status'] == 'waiting'

    # Durante a tradução, pipeline reporta progresso com input/BasicData/Game.dat
    extractor.add_threads_status({
        'file': 'input/BasicData/Game.dat',
        'status': 'process',
        'current': 1,
        'total': 2,
        'msg': 'Translating batch 1/2'
    })
    assert len(extractor.threads_status) == 1
    assert extractor.threads_status[0]['status'] == 'process'
    assert extractor.threads_status[0]['file'] == os.path.normpath('BasicData/Game.dat')
    assert extractor.threads_status[0]['current'] == 1

    # Próximo batch reporta conclusão
    extractor.add_threads_status({
        'file': 'input/BasicData/Game.dat',
        'status': 'process',
        'current': 2,
        'total': 2,
        'msg': 'Translating batch 2/2'
    })
    assert len(extractor.threads_status) == 1
    assert extractor.threads_status[0]['status'] == 'process'
    assert extractor.threads_status[0]['current'] == 2

    # Conclusão do process_file reporta sucesso com caminho relativo
    extractor.add_threads_status({
        'file': 'BasicData/Game.dat',
        'status': 'success',
        'msg': 'Processed successfully'
    })
    assert len(extractor.threads_status) == 1
    assert extractor.threads_status[0]['status'] == 'success'
    assert extractor.threads_status[0]['file'] == os.path.normpath('BasicData/Game.dat')


def test_wolfrpg_status_lifecycle_clean_success(tmp_path):
    input_dir = tmp_path / "input"
    process_dir = tmp_path / "process"
    output_dir = tmp_path / "output"
    (input_dir / "BasicData").mkdir(parents=True)
    (input_dir / "MapData").mkdir(parents=True)
    process_dir.mkdir()
    output_dir.mkdir()

    WolfRPGExtractor.folderInput = str(input_dir)
    WolfRPGExtractor.folderProcess = str(process_dir)
    WolfRPGExtractor.folderOutput = str(output_dir)

    # Cria arquivo MapData/Map001.mps
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

    mock_trans = MockTranslator({"Bom dia!": "Good morning!"})
    extractor = WolfRPGExtractor(mock_trans)
    extractor.process_files()

    import concurrent.futures
    concurrent.futures.wait(extractor.futures)

    # Verifica que NÃO há status duplicado e que o status final é estritamente 'success'
    statuses = extractor.threads_status
    assert len(statuses) == 1
    assert statuses[0]['file'] == os.path.normpath("MapData/Map001.mps")
    assert statuses[0]['status'] == 'success'
    assert statuses[0]['msg'] == 'Processed successfully'


def test_status_app_exits_cleanly_on_all_done():
    async def body():
        extractor = DummyExtractor(MockTranslator())
        extractor.threads_status = [
            {'file': 'Map001.mps', 'status': 'process', 'current': 1, 'total': 1, 'msg': 'Translating batch 1/1'}
        ]
        app = _StatusApp(extractor)
        async with app.run_test() as pilot:
            await pilot.pause()

            # Simula transição para sucesso
            def complete_worker():
                extractor.add_threads_status({'file': 'Map001.mps', 'status': 'success', 'msg': 'Processed successfully'})

            t = threading.Thread(target=complete_worker)
            t.start()
            await asyncio.to_thread(t.join)
            await pilot.pause()

        return app.return_value

    run_async(body)
