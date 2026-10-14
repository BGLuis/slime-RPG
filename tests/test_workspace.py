import os
import pytest
from unittest.mock import MagicMock

from src.workspace import Workspace
from src.extractor.BaseExtractor import BaseExtractor


class DummyTranslator:
    def __init__(self):
        self.cache = {}
        self.cache_lock = MagicMock()
        self.agent = "DummyTranslate"
        self.lang_source = "en"
        self.lang_target = "pt"

    def reduce_limite(self):
        pass

    def __deepcopy__(self, memo):
        return self


class DummyExtractor(BaseExtractor):
    name = "DummyExtractor"
    files_types = [".json"]

    @staticmethod
    def extract_text(file_name, data):
        return data

    @staticmethod
    def update_json(file_name, data, new_data):
        return new_data

    @staticmethod
    def fix_text_translate(text, original_text=None):
        return text


def test_workspace_init_in_place(tmp_path):
    input_dir = tmp_path / "game"
    input_dir.mkdir()

    ws = Workspace(str(input_dir))
    try:
        assert ws.input_dir == str(input_dir.resolve())
        assert ws.output_dir == str(input_dir.resolve())
        assert ws.is_in_place is True
        assert os.path.exists(ws.temp_dir)
        assert os.path.exists(ws.process_dir)
        assert os.path.exists(ws.output_work_dir)
    finally:
        ws.cleanup()
    assert not os.path.exists(ws.temp_dir)


def test_workspace_init_separate_output(tmp_path):
    input_dir = tmp_path / "game"
    input_dir.mkdir()
    output_dir = tmp_path / "translated"

    ws = Workspace(str(input_dir), output_dir=str(output_dir))
    try:
        assert ws.input_dir == str(input_dir.resolve())
        assert ws.output_dir == str(output_dir.resolve())
        assert ws.is_in_place is False
    finally:
        ws.cleanup()


def test_workspace_invalid_input():
    with pytest.raises(ValueError):
        Workspace("")
    with pytest.raises(ValueError):
        Workspace(None)


def test_workspace_context_manager(tmp_path):
    input_dir = tmp_path / "game"
    input_dir.mkdir()

    temp_path = None
    with Workspace(str(input_dir)) as ws:
        temp_path = ws.temp_dir
        assert os.path.exists(temp_path)

    assert not os.path.exists(temp_path)


def test_workspace_cleanup_idempotent(tmp_path):
    input_dir = tmp_path / "game"
    input_dir.mkdir()

    ws = Workspace(str(input_dir))
    ws.cleanup()
    assert not os.path.exists(ws.temp_dir)
    # Segunda chamada não deve lançar exceção
    ws.cleanup()


def test_workspace_clean_process(tmp_path):
    input_dir = tmp_path / "game"
    input_dir.mkdir()

    with Workspace(str(input_dir)) as ws:
        dummy_file = os.path.join(ws.process_dir, "test.txt")
        with open(dummy_file, "w", encoding="utf-8") as f:
            f.write("temporary data")
        assert os.path.exists(dummy_file)

        ws.clean_process()
        assert not os.path.exists(dummy_file)
        assert os.path.exists(ws.process_dir)


def test_workspace_atomic_replace(tmp_path):
    src_file = tmp_path / "new.txt"
    src_file.write_text("novo conteúdo", encoding="utf-8")

    dst_dir = tmp_path / "target_sub"
    dst_file = dst_dir / "file.txt"

    # Substituição em arquivo inexistente com criação de diretório
    Workspace.atomic_replace(str(src_file), str(dst_file))
    assert dst_file.read_text(encoding="utf-8") == "novo conteúdo"

    # Substituição atômica de arquivo existente
    src_file.write_text("conteúdo atualizado", encoding="utf-8")
    Workspace.atomic_replace(str(src_file), str(dst_file))
    assert dst_file.read_text(encoding="utf-8") == "conteúdo atualizado"


def test_workspace_commit(tmp_path):
    input_dir = tmp_path / "game"
    input_dir.mkdir()
    output_dir = tmp_path / "output_game"

    with Workspace(str(input_dir), output_dir=str(output_dir)) as ws:
        # Cria arquivos no process_dir
        sub_dir = os.path.join(ws.process_dir, "sub")
        os.makedirs(sub_dir, exist_ok=True)
        file1 = os.path.join(ws.process_dir, "map.json")
        file2 = os.path.join(sub_dir, "event.json")

        with open(file1, "w", encoding="utf-8") as f:
            f.write('{"id": 1}')
        with open(file2, "w", encoding="utf-8") as f:
            f.write('{"event": "start"}')

        committed = ws.commit()
        assert "map.json" in committed
        assert os.path.join("sub", "event.json") in committed

        out_file1 = os.path.join(str(output_dir), "map.json")
        out_file2 = os.path.join(str(output_dir), "sub", "event.json")
        assert os.path.exists(out_file1)
        assert os.path.exists(out_file2)
        with open(out_file1, encoding="utf-8") as f:
            assert f.read() == '{"id": 1}'


def test_base_extractor_workspace_integration(tmp_path):
    input_dir = tmp_path / "game"
    input_dir.mkdir()
    output_dir = tmp_path / "out"
    output_dir.mkdir()

    # Preenche saída com arquivo que não deve ser apagado
    preserved = output_dir / "keep_me.txt"
    preserved.write_text("keep", encoding="utf-8")

    with Workspace(str(input_dir), output_dir=str(output_dir)) as ws:
        extractor = DummyExtractor(DummyTranslator(), workspace=ws)

        assert extractor.workspace is ws
        assert extractor.folderInput == str(input_dir.resolve())
        assert extractor.folderProcess == ws.process_dir
        assert extractor.folderOutput == str(output_dir.resolve())

        # init_folder não deve apagar folderOutput quando workspace está ativo
        extractor.init_folder()
        assert os.path.exists(str(preserved))

        # Reatribuição deve refletir no workspace
        new_input = tmp_path / "new_input"
        new_input.mkdir()
        extractor.folderInput = str(new_input)
        assert extractor.folderInput == str(new_input)
        assert ws.input_dir == str(new_input)


def test_base_extractor_legacy_mode_without_workspace(tmp_path):
    proc = tmp_path / "process"
    out = tmp_path / "output"
    proc.mkdir()
    out.mkdir()

    extractor = DummyExtractor(DummyTranslator())
    extractor.folderProcess = str(proc)
    extractor.folderOutput = str(out)

    file_to_clean = out / "to_delete.txt"
    file_to_clean.write_text("delete", encoding="utf-8")

    # No modo legado sem workspace, init_folder limpa tanto process quanto output
    extractor.init_folder()
    assert not os.path.exists(str(file_to_clean))
