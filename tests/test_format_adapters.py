import os
import re
import pytest

from src.extractor.BaseExtractor import BaseExtractor
from src.extractor.FormatAdapter import (
    FormatAdapter,
    JsonFormatAdapter,
    EvtextFormatAdapter,
    get_registered_format_adapter,
    register_format_adapter,
)
from src.extractor.rpgmaker.RVDataAdapter import RVDataAdapter
from src.extractor.wolfrpg.WolfBinaryAdapter import WolfBinaryAdapter


def test_base_extractor_has_no_format_specific_knowledge():
    base_extractor_path = os.path.join(
        os.path.dirname(os.path.dirname(__file__)),
        "src",
        "extractor",
        "BaseExtractor.py",
    )
    with open(base_extractor_path, "r", encoding="utf-8") as f:
        content = f.read()

    matches = re.findall(r"rvdata2|mps|wolf", content)
    assert matches == [], f"Found forbidden format references in BaseExtractor.py: {matches}"


def test_builtin_adapters_satisfy_format_adapter_protocol():
    for adapter in (JsonFormatAdapter, EvtextFormatAdapter, RVDataAdapter, WolfBinaryAdapter):
        assert isinstance(adapter, FormatAdapter)
        assert isinstance(adapter.extensions, tuple) and len(adapter.extensions) > 0


def test_register_fake_txt_adapter_without_editing_base_extractor(tmp_path):
    original_txt_adapter = get_registered_format_adapter(".txt")
    load_calls = []
    save_calls = []

    try:
        @register_format_adapter
        class FakeTxtAdapter:
            extensions = (".txt",)

            @staticmethod
            def load_file(file_path: str):
                load_calls.append(file_path)
                with open(file_path, "r", encoding="utf-8") as f:
                    return {"fake_loaded": [line.rstrip("\n") for line in f]}

            @staticmethod
            def save_file(file_path: str, data, **ctx):
                save_calls.append((file_path, data))
                os.makedirs(os.path.dirname(file_path) or ".", exist_ok=True)
                with open(file_path, "w", encoding="utf-8") as f:
                    f.write("\n".join(data.get("fake_loaded", [])) + "\n")

        class DummyTxtExtractor(BaseExtractor):
            files_types = ["txt"]

            @staticmethod
            def extract_text(file_name, data):
                return data.get("fake_loaded", [])

            @staticmethod
            def update_json(file_name, data, new_data):
                return {"fake_loaded": list(new_data)}

            @staticmethod
            def fix_text_translate(text, original_text=None):
                return text

        txt_file = tmp_path / "sample.txt"
        txt_file.write_text("Line 1\nLine 2\n", encoding="utf-8")

        fname, loaded = DummyTxtExtractor.extract_files(str(txt_file))
        assert fname == "sample.txt"
        assert loaded == {"fake_loaded": ["Line 1", "Line 2"]}
        assert len(load_calls) == 1

        out_dir = tmp_path / "out"
        DummyTxtExtractor.import_file(fname, {"fake_loaded": ["Linha 1", "Linha 2"]}, str(out_dir))
        assert len(save_calls) == 1
        assert (out_dir / "sample.txt").read_text(encoding="utf-8") == "Linha 1\nLinha 2\n"
    finally:
        if original_txt_adapter is not None:
            register_format_adapter(original_txt_adapter)


def test_declared_adapter_takes_precedence_over_global_registry():
    class EngineAAdapter:
        extensions = (".json",)

        @staticmethod
        def load_file(p):
            pass

        @staticmethod
        def save_file(p, d, **ctx):
            pass

    class EngineBAdapter:
        extensions = (".json",)

        @staticmethod
        def load_file(p):
            pass

        @staticmethod
        def save_file(p, d, **ctx):
            pass

    class ExtractorA(BaseExtractor):
        format_adapters = (EngineAAdapter,)
        files_types = ["json"]

        @staticmethod
        def extract_text(file_name, data):
            return data

        @staticmethod
        def update_json(file_name, data, new_data):
            return new_data

        @staticmethod
        def fix_text_translate(text, original_text=None):
            return text

    class ExtractorB(BaseExtractor):
        format_adapters = (EngineBAdapter,)
        files_types = ["json"]

        @staticmethod
        def extract_text(file_name, data):
            return data

        @staticmethod
        def update_json(file_name, data, new_data):
            return new_data

        @staticmethod
        def fix_text_translate(text, original_text=None):
            return text

    class FallbackExtractor(BaseExtractor):
        files_types = ["json"]

        @staticmethod
        def extract_text(file_name, data):
            return data

        @staticmethod
        def update_json(file_name, data, new_data):
            return new_data

        @staticmethod
        def fix_text_translate(text, original_text=None):
            return text

    # O declarado no extrator vence sempre
    assert ExtractorA.get_format_adapter("test.json") is EngineAAdapter
    assert ExtractorB.get_format_adapter("test.json") is EngineBAdapter

    # Extrator sem adapter declarado recorre ao global registrado
    assert FallbackExtractor.get_format_adapter("test.json") is JsonFormatAdapter

