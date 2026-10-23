import os
import pytest
from unittest.mock import MagicMock

from src.extractor.wolfrpg.WolfRPGExtractor import WolfRPGExtractor
from src.extractor.wolfrpg.WolfBinaryAdapter import WolfProEncryptedError, WolfCommonEvents, WolfDatabase, WolfMap


class DummyTranslator:
    def __init__(self):
        self.cache = {}

    def translate_batch(self, batch, progress_callback=None):
        return [f"[PT] {item}" if isinstance(item, str) else item for item in batch]


def test_evtext_extract_and_insert(tmp_path):
    translator = DummyTranslator()
    extractor = WolfRPGExtractor(translator)

    # Cria script simulando Evtext da engine Wolf RPG / DazedTL
    evtext_content = (
        "//■パン屋イベント\r\n"
        "/evcgset,A007\r\n"
        "/evcg,b20,c0,d0,j0,n0,o0,t0,\r\n"
        "/fin\r\n"
        "Seris：\r\n"
        "Hello, Manager!\r\n"
        "\r\n"
        "Shopkeeper：\r\n"
        "Welcome to the bakery, Seris!\r\n"
        "/胸+1\r\n"
        "The manager handed me freshly baked bread.\r\n"
    )

    evtext_file = tmp_path / "a01_01h.txt"
    evtext_file.write_bytes(evtext_content.encode('cp932'))

    # 1. extract_files
    fname, loaded = extractor.extract_files(str(evtext_file))
    assert fname == "a01_01h.txt"
    assert loaded['encoding'] == 'cp932'
    assert loaded['lineterminator'] == '\r\n'
    assert len(loaded['lines']) == 11

    # 2. extract_text
    extracted = extractor.extract_text(fname, loaded)
    assert extracted is not None
    assert "lines" in extracted
    lines_dict = extracted["lines"]

    # Comandos / e comentários // NÃO devem estar na extração
    assert "0" not in lines_dict  # //■パン屋イベント
    assert "1" not in lines_dict  # /evcgset,A007
    assert "2" not in lines_dict  # /evcg...
    assert "3" not in lines_dict  # /fin
    assert "9" not in lines_dict  # /胸+1

    # Textos de diálogo devem estar presentes
    assert lines_dict["4"] == "Seris："
    assert lines_dict["5"] == "Hello, Manager!"
    assert lines_dict["7"] == "Shopkeeper："
    assert lines_dict["8"] == "Welcome to the bakery, Seris!"
    assert lines_dict["10"] == "The manager handed me freshly baked bread."

    # 3. update_json
    new_data = {
        "lines": {
            "4": "Seris：",
            "5": "Olá, Gerente!",
            "7": "Balconista：",
            "8": "Bem-vinda à padaria, Seris!",
            "10": "O gerente me entregou o pão quentinho."
        }
    }
    updated = extractor.update_json(fname, loaded, new_data)

    # 4. import_file
    output_dir = tmp_path / "output"
    extractor.import_file(fname, updated, str(output_dir))

    saved_file = output_dir / "a01_01h.txt"
    assert saved_file.exists()

    saved_bytes = saved_file.read_bytes()
    saved_text = saved_bytes.decode('cp932')

    # Verifica que comandos permaneceram intactos
    assert "/evcgset,A007" in saved_text
    assert "/fin" in saved_text
    assert "/胸+1" in saved_text
    assert "//■パン屋イベント" in saved_text

    # Verifica que os diálogos foram traduzidos e normalizados para CP932
    assert "Ola, Gerente!" in saved_text
    assert "Bem-vinda a padaria, Seris!" in saved_text
    assert "O gerente me entregou o pao quentinho." in saved_text


def test_is_translatable_file_evtext():
    translator = DummyTranslator()
    extractor = WolfRPGExtractor(translator)

    assert extractor.is_translatable_file("a01_01h.txt", "/game/Data/Evtext/a01_01h.txt") is True
    assert extractor.is_translatable_file("cZ5_01h.txt", "/game/Data/Evtext/d/cZ5_01h.txt") is True

    # Arquivos que não devem ser traduzidos
    assert extractor.is_translatable_file("readme.txt") is False
    assert extractor.is_translatable_file("patch-config.txt") is False
    assert extractor.is_translatable_file("license.txt") is False


def test_game_dat_title_preservation():
    translator = DummyTranslator()
    extractor = WolfRPGExtractor(translator)

    # Por padrão em instâncias sem configuração, preserve_title é False
    assert extractor.preserve_title is False
    assert extractor.extract_text_gamedat({"title": "Original Game Title"}) == {"title": "Original Game Title"}

    # Quando configurado interativamente para manter título original (opção recomendada)
    extractor.apply_configuration({"preserve_title": "(Recomendado) Sim, manter título original (evita invalidar saves existentes)"})
    assert extractor.preserve_title is True
    assert extractor.extract_text_gamedat({"title": "Original Game Title"}) is None

    # Se configurado para traduzir título
    extractor.apply_configuration({"preserve_title": "Não, traduzir o título do jogo"})
    assert extractor.preserve_title is False
    assert extractor.extract_text_gamedat({"title": "Original Game Title"}) == {"title": "Original Game Title"}


def test_wolf_pro_detection(tmp_path):
    # 1. CommonEvent.dat com cabeçalho Pro (0xCC)
    ce_pro = b'\x00W\x00\x00OLUFC\x00\xcc\x99\xd92\x00\xa6\xf4\x12\x00'
    ce_path = tmp_path / "CommonEvent.dat"
    ce_path.write_bytes(ce_pro)

    with pytest.raises(WolfProEncryptedError) as exc_info:
        WolfCommonEvents.load(str(ce_path))
    assert "WOLF RPG Pro" in str(exc_info.value)

    # 2. DataBase.dat com cabeçalho Pro (0xC4)
    db_pro = b'\x00W\x00\x00OLUFM\x00\xc4#\xd8\x11\x008s\x07\x00'
    db_path = tmp_path / "DataBase.dat"
    db_path.write_bytes(db_pro)

    with pytest.raises(WolfProEncryptedError) as exc_info:
        WolfDatabase.load(str(db_path))
    assert "WOLF RPG Pro" in str(exc_info.value)

    # 3. Map*.mps com cabeçalho Pro compactado
    # 16 bytes magic + 4 bytes enc (85) + 4 bytes attr (103) + 1 byte ver (105) + ...
    map_pro = b'\0\0\0\0\0\0\0\0\0\0WOLFM\0\x55\x00\x00\x00\x67\x00\x00\x00\x69\x22\x00\x00\x00\x1b\x00\x00\x00'
    map_path = tmp_path / "Map001.mps"
    map_path.write_bytes(map_pro)

    with pytest.raises(WolfProEncryptedError) as exc_info:
        WolfMap.load(str(map_path))
    assert "WOLF RPG Pro" in str(exc_info.value)


def test_game_detector_evtext(tmp_path):
    from src.services.GameDetector import detect_game_environment

    game_dir = tmp_path / "Game"
    evtext_dir = game_dir / "Data" / "Evtext"
    evtext_dir.mkdir(parents=True)
    (evtext_dir / "scene01.txt").write_text("Hello", encoding="utf-8")

    detected = detect_game_environment(str(game_dir))
    assert detected is not None
    assert detected['extractor'] == 'Wolf RPG'
    assert "Data" in detected['detected_path']


def test_evtext_speaker_name_colon_preservation():
    extractor = WolfRPGExtractor(DummyTranslator())
    data = {
        'encoding': 'cp932',
        'lineterminator': '\r\n',
        'lines': [
            'Seris：\r\n',
            'Hello!\r\n',
            'Shopkeeper：\r\n',
            'Welcome!\r\n'
        ]
    }
    # Simulando tradução onde os dois pontos viraram : comum ou foram omitidos
    new_data = {
        'lines': {
            '0': 'Serie:',
            '1': 'Olá!',
            '2': 'Lojista',
            '3': 'Bem-vindo!'
        }
    }
    extractor.insert_text_evtext(data, new_data)

    # Verifica que o dois pontos japonês full-width foi preservado nos nomes
    assert data['lines'][0] == 'Serie：\r\n'
    assert data['lines'][1] == 'Olá!\r\n'
    assert data['lines'][2] == 'Lojista：\r\n'
    assert data['lines'][3] == 'Bem-vindo!\r\n'


def test_google_translate_circuit_breaker_on_429():
    from src.translate.GoogleTranslate import GoogleTranslate
    from src.services.ProxyManager import ProxyManager
    from src.exceptions import ProviderRateLimited

    proxy_mgr = ProxyManager.get_instance()
    orig_blocked = proxy_mgr.direct_blocked
    orig_enabled = proxy_mgr.enabled

    try:
        proxy_mgr.set_enabled(False)
        proxy_mgr.set_direct_blocked(True)

        gt = GoogleTranslate(lang_source="en", lang_target="pt")
        with pytest.raises(ProviderRateLimited) as exc_info:
            gt._translate_raw("Hello world")

        assert "HTTP 429" in str(exc_info.value)
    finally:
        proxy_mgr.set_direct_blocked(orig_blocked)
        proxy_mgr.set_enabled(orig_enabled)


def test_status_app_message_truncation_and_summary():
    from src.cli import _StatusApp
    from tests.test_cli_textual import run_async

    class MockExt:
        def __init__(self):
            self.threads_status = [
                {
                    'file': 'BasicData/CDataBase.dat',
                    'status': 'erro',
                    'msg': 'O banco de dados CDataBase.dat utiliza protecao compactada/cifrada do WOLF RPG Pro 3.x. Para traduzir este arquivo, desproteja-o previamente utilizando o UberWolf (opcao Remove Pro Protection).'
                },
                {
                    'file': 'Evtext/a01_01h.txt',
                    'status': 'success',
                    'msg': 'Processed successfully',
                    'current': 215,
                    'total': 215
                }
            ]
        def add_observer(self, obs):
            pass

    async def body():
        app = _StatusApp(MockExt())
        async with app.run_test() as pilot:
            await pilot.pause()
            # Verifica que a mensagem foi truncada para <= 60 caracteres na tabela
            table = app.query_one("DataTable")
            row = table.get_row("BasicData/CDataBase.dat")
            # col_msg é o 3º elemento (índice 2)
            msg_cell = row[2]
            assert len(msg_cell) <= 60
            assert msg_cell.endswith("...")

            # Verifica o resumo superior
            summary = app.query_one("#status-summary").render()
            assert "Total: 2" in str(summary)
            assert "Concluídos: 1" in str(summary)
            assert "Erros: 1" in str(summary)
        return True

    assert run_async(body) is True


