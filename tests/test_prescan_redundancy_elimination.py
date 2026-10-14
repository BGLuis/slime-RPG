import json
import os
from unittest.mock import MagicMock
from src.extractor.BaseExtractor import BaseExtractor
from src.translate.BaseTranslate import BaseTranslate


class _DummyTranslate(BaseTranslate):
    agent = 'dummyPrescanTranslator'

    def _translate_single_batch(self, texts):
        return [t.upper() for t in texts]

    def translator(self, texts, progress_callback=None):
        return texts


class _DummyExtractor(BaseExtractor):
    name = 'DummyPrescan'
    files_types = ['json']

    @staticmethod
    def extract_text(file_name, data):
        return list(data.get('text', []))

    @staticmethod
    def update_json(file_name, data, new_data):
        return {'text': list(new_data)}

    @staticmethod
    def fix_text_translate(text, original_text=None):
        return text


def test_prescan_returns_tuple_for_pending_files(tmp_path):
    trans = _DummyTranslate(lang_source='en', lang_target='pt')
    extractor = _DummyExtractor(trans)

    inp_dir = tmp_path / "input"
    inp_dir.mkdir()
    file_path = inp_dir / "test.json"
    file_path.write_text(json.dumps({"text": ["hello", "world"]}), encoding="utf-8")

    lane, entry = extractor._prescan_file(str(file_path), str(inp_dir))
    assert lane == 'pending'
    assert entry is not None
    rel_path, data, raw_text, file_name = entry
    assert rel_path == "test.json"
    assert data == {"text": ["hello", "world"]}
    assert raw_text == ["hello", "world"]
    assert file_name == "test.json"


def test_process_file_skips_disk_extraction_when_preextracted_data_provided(tmp_path):
    trans = _DummyTranslate(lang_source='en', lang_target='pt')
    extractor = _DummyExtractor(trans)
    extractor.folderInput = str(tmp_path / "input")
    extractor.folderProcess = str(tmp_path / "process")
    extractor.folderOutput = str(tmp_path / "output")
    os.makedirs(extractor.folderInput, exist_ok=True)
    os.makedirs(extractor.folderProcess, exist_ok=True)
    os.makedirs(extractor.folderOutput, exist_ok=True)

    file_path = os.path.join(extractor.folderInput, "game.json")
    with open(file_path, "w", encoding="utf-8") as f:
        json.dump({"text": ["disk text"]}, f)

    # Monkeypatch extract_files to fail if called
    extractor.extract_files = MagicMock(side_effect=AssertionError("extract_files should not be called!"))

    pre_data = {"text": ["memory text"]}
    pre_raw = ["memory text"]

    # Passing pre_data and pre_raw should bypass extract_files
    extractor.process_file(file_path, data=pre_data, raw_text=pre_raw)

    extractor.extract_files.assert_not_called()

    # Verify that file was saved to process folder with memory text
    proc_file = os.path.join(extractor.folderProcess, "game.json")
    assert os.path.exists(proc_file)
    with open(proc_file, "r", encoding="utf-8") as f:
        saved = json.load(f)
    assert saved["text"] == ["MEMORY TEXT"]


def test_import_file_sanitizes_octal_in_memory_before_dump(tmp_path):
    folder = str(tmp_path / "out")
    os.makedirs(folder, exist_ok=True)

    data = {
        "script": "if(x == 012) { return 05; }",
        "quoted": "var msg = '012';",
        "regular": "hello 012 world"
    }

    BaseExtractor.import_file("test.json", data, folder)

    saved_path = os.path.join(folder, "test.json")
    assert os.path.exists(saved_path)
    with open(saved_path, "r", encoding="utf-8") as f:
        content = json.load(f)

    # 012 and 05 should be stripped in code triggers, but not in quotes or non-code
    assert content["script"] == "if(x == 12) { return 5; }"
    assert content["quoted"] == "var msg = '012';"
    assert content["regular"] == "hello 012 world"


def test_import_files_copies_without_reextracting(tmp_path):
    trans = _DummyTranslate(lang_source='en', lang_target='pt')
    extractor = _DummyExtractor(trans)
    extractor.folderProcess = str(tmp_path / "process")
    extractor.folderOutput = str(tmp_path / "output")
    os.makedirs(extractor.folderProcess, exist_ok=True)
    os.makedirs(extractor.folderOutput, exist_ok=True)

    proc_file = os.path.join(extractor.folderProcess, "dialogue.json")
    with open(proc_file, "w", encoding="utf-8") as f:
        json.dump({"text": ["processed"]}, f)

    extractor.extract_files = MagicMock(side_effect=AssertionError("extract_files should not be called!"))

    extractor.import_files()

    extractor.extract_files.assert_not_called()

    out_file = os.path.join(extractor.folderOutput, "dialogue.json")
    assert os.path.exists(out_file)
    with open(out_file, "r", encoding="utf-8") as f:
        assert json.load(f)["text"] == ["processed"]


def test_prescan_returns_tuple_even_when_cache_is_none(tmp_path):
    trans = _DummyTranslate(lang_source='en', lang_target='pt')
    trans.cache = None
    extractor = _DummyExtractor(trans)

    inp_dir = tmp_path / "input"
    inp_dir.mkdir()
    file_path = inp_dir / "test.json"
    file_path.write_text(json.dumps({"text": ["hello", "world"]}), encoding="utf-8")

    lane, entry = extractor._prescan_file(str(file_path), str(inp_dir))
    assert lane == 'pending'
    assert entry is not None
    rel_path, data, raw_text, file_name = entry
    assert rel_path == "test.json"
    assert data == {"text": ["hello", "world"]}
    assert raw_text == ["hello", "world"]
    assert file_name == "test.json"


def test_process_file_with_preprovided_empty_raw_text(tmp_path):
    trans = _DummyTranslate(lang_source='en', lang_target='pt')
    extractor = _DummyExtractor(trans)
    extractor.folderInput = str(tmp_path / "input")
    extractor.folderProcess = str(tmp_path / "process")
    extractor.folderOutput = str(tmp_path / "output")
    os.makedirs(extractor.folderInput, exist_ok=True)
    os.makedirs(extractor.folderProcess, exist_ok=True)
    os.makedirs(extractor.folderOutput, exist_ok=True)

    file_path = os.path.join(extractor.folderInput, "empty.json")
    with open(file_path, "w", encoding="utf-8") as f:
        json.dump({"text": []}, f)

    extractor._pipeline_translate = MagicMock(side_effect=AssertionError("pipeline should not be called"))

    extractor.process_file(file_path, data={"text": []}, raw_text=[])

    extractor._pipeline_translate.assert_not_called()
    out_file = os.path.join(extractor.folderOutput, "empty.json")
    assert os.path.exists(out_file)
