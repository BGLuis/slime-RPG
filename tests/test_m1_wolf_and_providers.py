import os
import shutil
import subprocess
import pytest
from unittest.mock import MagicMock, patch

from src.exceptions import (
    TranslationError,
    ProviderRateLimited,
    ProviderAuthError,
    ProviderUnavailable,
    MaskIntegrityError,
)
from src.translate.DeeplTranslate import DeeplTranslate
from src.translate.BaseTranslate import BaseTranslate
from src.extractor.BaseExtractor import BaseExtractor
from src.extractor.wolfrpg.WolfRPGExtractor import WolfRPGExtractor
from src.extractor.wolfrpg.WolfArchive import WolfArchive
from src.extractor.wolfrpg.WolfBinaryAdapter import (
    WolfBinaryAdapter,
    WolfMap,
    WolfMapEvent,
    WolfMapPage,
    WolfCommand,
)
from src.extractor.wolfrpg.WolfEventCodes import WolfEventCode
from src.job import JobConfig, TranslationJob


class MockSimpleTranslator(BaseTranslate):
    def __init__(self, dictionary=None, lang_source="ja", lang_target="pt"):
        super().__init__(lang_source=lang_source, lang_target=lang_target)
        self.dict = dictionary or {}
        self.cache = {}
        self.call_count = 0

    def _translate_single_batch(self, batch):
        self.call_count += 1
        res = []
        for text in batch:
            res.append(self.dict.get(text, f"[TR]{text}"))
        return res

    def translator(self, texts, progress_callback=None):
        return super().translator(texts, progress_callback)


class DummyJsonExtractor(BaseExtractor):
    files_types = ['json']

    def extract_files(self, file_path):
        import json
        with open(file_path, 'r', encoding='utf-8') as f:
            return os.path.basename(file_path), json.load(f)

    def extract_text(self, file_name, data):
        return data

    def update_json(self, file_name, data, new_data):
        return new_data

    @staticmethod
    def fix_text_translate(text, original_text=None):
        return text


# ============================================================================
# Testes da Issue #7: WOLF RPG Data.wolf container & prepare()
# ============================================================================

def test_wolf_rpg_extracts_data_wolf_without_ignore_status(tmp_path, monkeypatch):
    input_dir = tmp_path / "input"
    process_dir = tmp_path / "process"
    output_dir = tmp_path / "output"
    staging_dir = tmp_path / "staging"
    input_dir.mkdir()
    process_dir.mkdir()
    output_dir.mkdir()
    staging_dir.mkdir()

    # Cria arquivo Map001.mps sintético com diálogo
    map_staging = staging_dir / "MapData" / "Map001.mps"
    map_staging.parent.mkdir(parents=True)

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
    page.commands = [WolfCommand(cid=WolfEventCode.SHOW_MESSAGE, string_args=["Olá do contêiner!"])]
    ev.pages = [page]
    wolf_map.events = [ev]
    wolf_map.save(str(map_staging))

    # Cria Data/Data.wolf na pasta de entrada
    wolf_data_dir = input_dir / "Data"
    wolf_data_dir.mkdir()
    archive_file = wolf_data_dir / "Data.wolf"
    WolfArchive.create_archive(str(staging_dir), str(archive_file))
    shutil.rmtree(str(staging_dir))

    # O input_dir contém exclusivamente o contêiner Data/Data.wolf
    assert archive_file.exists()
    assert not (input_dir / "MapData" / "Map001.mps").exists()

    monkeypatch.setattr(WolfRPGExtractor, "folderInput", str(input_dir))
    monkeypatch.setattr(WolfRPGExtractor, "folderProcess", str(process_dir))
    monkeypatch.setattr(WolfRPGExtractor, "folderOutput", str(output_dir))

    mock_trans = MockSimpleTranslator({"Olá do contêiner!": "Hello from container!"})
    extractor = WolfRPGExtractor(mock_trans)

    # Executa o fluxo de processamento e importação
    extractor.process_files()
    extractor.import_files()

    statuses = extractor.threads_status
    status_files = [s['file'] for s in statuses]
    # Data.wolf NÃO deve ter sido listado como arquivo traduzível ou com status 'ignore'
    assert not any("Data.wolf" in f and s['status'] == 'ignore' for s, f in zip(statuses, status_files))

    # O mapa descompactado foi processado e traduzido com sucesso
    output_candidates = list(output_dir.rglob("Map001.mps"))
    assert len(output_candidates) == 1
    translated_map = WolfBinaryAdapter.load_file(str(output_candidates[0]))
    cmd_text = translated_map["events"][1]["pages"][0]["list"][0]["string_args"][0]
    assert cmd_text == "Hello from container!"


# ============================================================================
# Testes da Issue #10: DeeplTranslate AttributeError e auto source_lang
# ============================================================================

@patch("requests.post")
def test_deepl_translator_dict_without_attribute_error(mock_post):
    mock_response = MagicMock()
    mock_response.status_code = 200
    mock_response.json.return_value = {
        "translations": [{"text": "Olá mundo"}]
    }
    mock_post.return_value = mock_response

    dt = DeeplTranslate(lang_source="en", lang_target="pt")
    dt.api_key = "test_key:fx"

    # Não deve levantar AttributeError (preprocess_text / postprocess_text)
    result = dt.translator({"msg": "Hello world"})
    assert result == {"msg": "Olá mundo"}


@patch("requests.post")
def test_deepl_omits_source_lang_when_auto(mock_post):
    mock_response = MagicMock()
    mock_response.status_code = 200
    mock_response.json.return_value = {
        "translations": [{"text": "Texto traduzido"}]
    }
    mock_post.return_value = mock_response

    dt = DeeplTranslate(lang_source="auto", lang_target="pt")
    dt.api_key = "test_key:fx"

    dt._translate_single_batch(["Hello"])

    assert mock_post.called
    data_payload = mock_post.call_args.kwargs.get("data")
    assert "source_lang" not in data_payload
    assert data_payload["target_lang"] == "PT-BR"


@patch("requests.post")
def test_deepl_typed_exceptions_on_http_errors(mock_post):
    dt = DeeplTranslate(lang_source="en", lang_target="pt")
    dt.api_key = "test_key:fx"

    # HTTP 403 -> ProviderAuthError
    mock_post.return_value = MagicMock(status_code=403, text="Forbidden")
    with pytest.raises(ProviderAuthError):
        dt._translate_single_batch(["Hello"])

    # HTTP 429 -> ProviderRateLimited
    mock_post.return_value = MagicMock(status_code=429, text="Rate Limited")
    with pytest.raises(ProviderRateLimited):
        dt._translate_single_batch(["Hello"])

    # HTTP 500 -> ProviderUnavailable
    mock_post.return_value = MagicMock(status_code=500, text="Internal Error")
    with pytest.raises(ProviderUnavailable):
        dt._translate_single_batch(["Hello"])


# ============================================================================
# Testes da Issue #8: Exceções Tipadas, Retry e Status Partial
# ============================================================================

def test_provider_failing_1_of_10_yields_partial_status(tmp_path):
    input_file = tmp_path / "test10.json"
    import json
    data_10 = {f"k_{i}": f"phrase_{i}" for i in range(10)}
    input_file.write_text(json.dumps(data_10), encoding='utf-8')

    class PartialFailingTranslator(MockSimpleTranslator):
        def __init__(self):
            super().__init__(lang_source="en", lang_target="pt")

        def _translate_single_batch(self, batch):
            res = []
            for item in batch:
                if "phrase_4" in item:
                    # Falha em 1 dos 10 itens
                    raise ProviderUnavailable("Provider failure on item 4")
                res.append(f"TRANSLATED_{item}")
            return res

    trans = PartialFailingTranslator()
    extractor = DummyJsonExtractor(trans)
    extractor.folderInput = str(tmp_path)
    extractor.folderProcess = str(tmp_path / "proc")
    extractor.folderOutput = str(tmp_path / "out")

    extractor.process_file(str(input_file))

    statuses = extractor.threads_status
    assert len(statuses) == 1
    st = statuses[0]
    assert st['status'] == 'partial'
    assert "9/10" in st['msg']
    assert extractor.file_translation_stats[os.path.basename(str(input_file))] == (9, 10)


@patch("time.sleep")
def test_simulated_http_403_is_not_retried(mock_sleep, tmp_path):
    input_file = tmp_path / "test.json"
    import json
    input_file.write_text(json.dumps({"a": "hello"}), encoding='utf-8')

    call_counter = [0]

    class AuthFailingTranslator(MockSimpleTranslator):
        def __init__(self):
            super().__init__(lang_source="en", lang_target="pt")

        def _translate_single_batch(self, batch):
            call_counter[0] += 1
            raise ProviderAuthError("HTTP 403 Forbidden")

    trans = AuthFailingTranslator()
    extractor = DummyJsonExtractor(trans)
    extractor.folderInput = str(tmp_path)
    extractor.folderProcess = str(tmp_path / "proc")
    extractor.folderOutput = str(tmp_path / "out")

    extractor.process_file(str(input_file), retries=5)

    # Não deve tentar 5 vezes; aborta na primeira tentativa sem dormir
    assert call_counter[0] == 1
    mock_sleep.assert_not_called()
    assert extractor.threads_status[0]['status'] == 'erro'
    assert "Erro não recuperável" in extractor.threads_status[0]['msg']


def test_no_magic_429_strings_in_src():
    """Garante que '429' in e 'bloqueado pelo google' foram totalmente erradicados de src/."""
    result = subprocess.run(
        ["grep", "-rn", '"429" in\\|bloqueado pelo google', "src/"],
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True
    )
    # exit code 1 significa nenhuma correspondência encontrada
    assert result.returncode == 1, f"Found forbidden strings in src:\n{result.stdout}"


def test_mask_integrity_error_inheritance():
    """Verifica herança múltipla de MaskIntegrityError para retrocompatibilidade."""
    err = MaskIntegrityError("Desmascaramento incompleto")
    assert isinstance(err, TranslationError)
    assert isinstance(err, ValueError)


def test_job_summary_reports_partial_files(tmp_path):
    """Verifica que o resumo final do TranslationJob lista arquivos parciais."""
    logs = []

    def mock_log(msg, color='white'):
        logs.append(msg)

    mock_trans = MagicMock()
    extractor = DummyJsonExtractor(mock_trans)
    extractor.folderInput = str(tmp_path / "input")
    extractor.folderProcess = str(tmp_path / "proc")
    extractor.folderOutput = str(tmp_path / "out")
    os.makedirs(extractor.folderInput, exist_ok=True)
    os.makedirs(extractor.folderProcess, exist_ok=True)
    os.makedirs(extractor.folderOutput, exist_ok=True)

    extractor.add_threads_status({
        'file': 'partial_file.json',
        'status': 'partial',
        'msg': 'Partially translated (7/10)'
    })

    ws_in = tmp_path / "ws_in"
    ws_out = tmp_path / "ws_out"
    ws_proc = tmp_path / "ws_proc"
    ws_in.mkdir()
    ws_out.mkdir()
    ws_proc.mkdir()

    from src.workspace import Workspace
    ws = Workspace(ws_in, ws_out, ws_proc)
    extractor.workspace = ws

    mock_signals = MagicMock()
    mock_signals.log.emit = MagicMock(side_effect=lambda msg, color='white': logs.append(msg))

    job = TranslationJob(
        JobConfig(extractor_name="json", backup=False, verify=False),
        extractor=extractor,
        translator=mock_trans,
        workspace=ws
    )

    # Mocka métodos pesados para testar o fluxo do job.run com resumo final
    extractor.init_folder = MagicMock()
    extractor.process_files = MagicMock()
    extractor.wait_for_dispatch = MagicMock()
    extractor.import_files = MagicMock()

    job.run(mock_signals, is_interactive=False)

    assert any("Resumo de Arquivos com Tradução Parcial" in line for line in logs)
    assert any("partial_file.json: Partially translated (7/10)" in line for line in logs)
