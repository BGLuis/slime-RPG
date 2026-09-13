import json
import pytest
from unittest.mock import MagicMock, patch
from src.extractor.BaseExtractor import BaseExtractor


class DummyExtractor(BaseExtractor):
    files_types = ['json']

    def extract_files(self, file_path):
        return "file", {"dummy": 1}

    def extract_text(self, file_name, data):
        return {"1": "text to translate"}

    def update_json(self, file_name, data, new_data):
        return new_data

    @staticmethod
    def fix_text_translate(text, original_text=None):
        return text

    @property
    def extract_map(self):
        return []


def test_is_retriable_error_classification():
    # Erros não recuperáveis (devem retornar False)
    assert not BaseExtractor._is_retriable_error(TypeError("bool is not iterable"))
    assert not BaseExtractor._is_retriable_error(KeyError("missing_key"))
    assert not BaseExtractor._is_retriable_error(IndexError("out of bounds"))
    assert not BaseExtractor._is_retriable_error(AttributeError("no attribute"))
    assert not BaseExtractor._is_retriable_error(SyntaxError("bad syntax"))
    assert not BaseExtractor._is_retriable_error(FileNotFoundError("file not found"))
    assert not BaseExtractor._is_retriable_error(PermissionError("denied"))
    assert not BaseExtractor._is_retriable_error(json.JSONDecodeError("msg", "doc", 0))
    assert not BaseExtractor._is_retriable_error(ValueError("invalid literal for int()"))

    # Erros recuperáveis de tradução (devem retornar True)
    assert BaseExtractor._is_retriable_error(ConnectionError("Connection reset"))
    assert BaseExtractor._is_retriable_error(TimeoutError("Request timed out"))
    assert BaseExtractor._is_retriable_error(ValueError("Desmascaramento incompleto: placeholder sobrou"))
    assert BaseExtractor._is_retriable_error(ValueError("Empty translation received"))
    assert BaseExtractor._is_retriable_error(Exception("HTTP 429 Too Many Requests"))


@patch("time.sleep")
def test_extraction_error_aborts_immediately_without_retry(mock_sleep, tmp_path):
    input_file = tmp_path / "test.json"
    input_file.write_text('{"bad": 1}')

    mock_translate = MagicMock()
    extractor = DummyExtractor(mock_translate)
    extractor.folderInput = str(tmp_path)
    extractor.folderOutput = str(tmp_path / "out")
    extractor.folderProcess = str(tmp_path / "proc")

    # Força erro na extração
    extractor.extract_text = MagicMock(side_effect=TypeError("'bool' object is not iterable"))

    extractor.process_file(str(input_file), retries=6, delay=20)

    # Verifica que NÃO dormiu (sem retries)
    mock_sleep.assert_not_called()
    # Verifica que reduce_limite NÃO foi chamado
    mock_translate.reduce_limite.assert_not_called()
    # Verifica status final de erro
    assert len(extractor.threads_status) == 1
    assert extractor.threads_status[0]['status'] == 'erro'
    assert "Erro na extração" in extractor.threads_status[0]['msg']


@patch("time.sleep")
def test_non_retriable_translation_error_aborts_without_sleeping(mock_sleep, tmp_path):
    input_file = tmp_path / "test.json"
    input_file.write_text('{"ok": 1}')

    mock_translate = MagicMock()
    extractor = DummyExtractor(mock_translate)
    extractor.folderInput = str(tmp_path)
    extractor.folderOutput = str(tmp_path / "out")
    extractor.folderProcess = str(tmp_path / "proc")

    # Simula um TypeError dentro do pipeline de tradução
    extractor._pipeline_translate = MagicMock(side_effect=TypeError("Unexpected type in pipeline"))

    extractor.process_file(str(input_file), retries=6, delay=20)

    # Não deve tentar de novo nem dormir
    mock_sleep.assert_not_called()
    mock_translate.reduce_limite.assert_not_called()
    assert extractor.threads_status[-1]['status'] == 'erro'
    assert "Erro não recuperável" in extractor.threads_status[-1]['msg']


@patch("time.sleep")
def test_retriable_translation_error_performs_retries(mock_sleep, tmp_path):
    input_file = tmp_path / "test.json"
    input_file.write_text('{"ok": 1}')

    mock_translate = MagicMock()
    extractor = DummyExtractor(mock_translate)
    extractor.folderInput = str(tmp_path)
    extractor.folderOutput = str(tmp_path / "out")
    extractor.folderProcess = str(tmp_path / "proc")

    # Simula erro de conexão transitório
    extractor._pipeline_translate = MagicMock(side_effect=ConnectionError("Temporary network failure"))

    extractor.process_file(str(input_file), retries=3, delay=1, max_delay=10)

    # Deve ter dormido 2 vezes (retries - 1)
    assert mock_sleep.call_count == 2
    # Deve ter chamado reduce_limite 2 vezes
    # Note que translate foi clonado com deepcopy, mas mock_translate pode ser inspecionado
    # ou verificamos se mock_sleep foi chamado com backoffs exponenciais
    mock_sleep.assert_any_call(1)  # 1 * (2**0)
    mock_sleep.assert_any_call(2)  # 1 * (2**1)
    assert extractor.threads_status[-1]['status'] == 'erro'
    assert "Failed to process after 3" in extractor.threads_status[-1]['msg']
