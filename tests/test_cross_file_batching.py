import json
import os
import pytest
from unittest.mock import MagicMock
from src.extractor.JsonExtractor import JsonExtractor
from src.translate.BaseTranslate import BaseTranslate
from src.translate.Provider import Capabilities


class MockCountingProvider(BaseTranslate):
    agent = 'mockCounting'
    id = 'mockCounting'
    capabilities = Capabilities(max_request_chars=5000, max_items=None, native_batch=False)
    MAX_REQUESTS_SIMULTANEOUSLY = 4

    def __init__(self, **kw):
        super().__init__(**kw)
        self.request_count = 0
        self.received_strings = []

    def _translate_single_batch(self, texts):
        self.request_count += 1
        self.received_strings.extend(texts)
        return [f"PT:{t}" for t in texts]

    def translate(self, segments, ctx=None):
        self.request_count += 1
        texts = [s.source for s in segments]
        self.received_strings.extend(texts)
        return [f"PT:{t}" for t in texts]


def test_20_files_cross_file_batching_and_dedup(tmp_path):
    input_dir = tmp_path / "input"
    output_dir = tmp_path / "output"
    process_dir = tmp_path / "process"
    input_dir.mkdir()
    output_dir.mkdir()
    process_dir.mkdir()

    provider = MockCountingProvider(lang_source="en", lang_target="pt")
    # Ensure memory db is isolated
    provider.cache_path_base = str(tmp_path)
    provider._load_cache_for_current_languages()

    extractor = JsonExtractor(provider)
    extractor.folderInput = str(input_dir)
    extractor.folderOutput = str(output_dir)
    extractor.folderProcess = str(process_dir)

    # 20 files with 30 lines each (~20k chars in total)
    # Common strings repeated across all files:
    common_strings = ["...", "Sim", "Não", "Hero", "Villain", "Gold", "Sword", "Shield"]
    all_unique_sources = set()

    for file_idx in range(20):
        file_path = input_dir / f"Map{file_idx:03d}.json"
        # 8 common strings + 22 unique per-file strings = 30 strings per file
        file_strings = list(common_strings) + [
            f"Line {line_idx} in Map {file_idx} text dialogue message"
            for line_idx in range(22)
        ]
        all_unique_sources.update(file_strings)
        data = {"events": [{"text": file_strings}]}
        file_path.write_text(json.dumps(data, ensure_ascii=False))

    # Total characters across all 20 files:
    total_chars = sum(len(s) for s in all_unique_sources)
    assert total_chars >= 15000  # ~15k-20k chars

    extractor.process_files()
    extractor.import_files()

    # 1. Requests made to provider must be <= 5 (batches of ~5,000 chars)
    assert provider.request_count <= 5, f"Expected <= 5 requests, got {provider.request_count}"

    # 2. Number of unique strings sent equals number of unique uncached strings in the input set
    assert set(provider.received_strings) == all_unique_sources
    assert len(provider.received_strings) == len(all_unique_sources)

    # 3. Per-file progress is reported for all 20 files
    status_map = {s['file']: s['status'] for s in extractor.threads_status}
    for file_idx in range(20):
        expected_file = f"Map{file_idx:03d}.json"
        assert expected_file in status_map
        assert status_map[expected_file] in ('success', 'process')

    # Verify translated outputs were written to output folder
    for file_idx in range(20):
        out_file = output_dir / f"Map{file_idx:03d}.json"
        assert out_file.exists()
        out_data = json.loads(out_file.read_text())
        assert out_data["events"][0]["text"][0] == "PT:..."


def test_cross_file_batching_preserves_masked_tokens(tmp_path):
    r"""
    Verify that RPG Maker control codes and variables (\V[1], \C[2], etc.)
    are properly masked before cross-file batching and restored in final files.
    """
    from src.extractor.rpgmaker.RPGMakerExtractor import RPGMakerExtractor

    class MaskCheckingProvider(BaseTranslate):
        agent = "mask_checker"
        id = "mask_checker"
        capabilities = Capabilities(max_request_chars=5000, max_items=10, native_batch=True)

        def __init__(self, **kw):
            super().__init__(**kw)
            self.received_texts = []

        def _translate_single_batch(self, texts):
            self.received_texts.extend(texts)
            # Ensure no raw RPG control codes leak into provider
            for t in texts:
                assert "\\V[" not in t, f"Raw control code leaked to provider: {t}"
                assert "\\C[" not in t, f"Raw color code leaked to provider: {t}"
                assert "__XTOK_" in t or "Simple" in t
            return [t.replace("Hero", "Herói").replace("Danger", "Perigo") for t in texts]

        def translate(self, segments, ctx=None):
            texts = [s.source for s in segments]
            return self._translate_single_batch(texts)

    input_dir = tmp_path / "game_input"
    output_dir = tmp_path / "game_output"
    process_dir = tmp_path / "game_process"
    input_dir.mkdir()
    output_dir.mkdir()
    process_dir.mkdir()

    provider = MaskCheckingProvider(lang_source="en", lang_target="pt")
    provider.cache_path_base = str(tmp_path)
    provider._load_cache_for_current_languages()

    extractor = RPGMakerExtractor(provider)
    extractor.folderInput = str(input_dir)
    extractor.folderOutput = str(output_dir)
    extractor.folderProcess = str(process_dir)

    # File 1 with \V[1]
    f1_data = {
        "events": [
            None,
            {
                "id": 1,
                "pages": [
                    {
                        "id": 0,
                        "list": [
                            {"code": 401, "parameters": ["Hero \\V[1]! Danger!"], "indent": 0},
                            {"code": 0, "parameters": [], "indent": 0}
                        ]
                    }
                ]
            }
        ]
    }
    (input_dir / "Map001.json").write_text(json.dumps(f1_data))

    # File 2 with \C[2]
    f2_data = {
        "events": [
            None,
            {
                "id": 1,
                "pages": [
                    {
                        "id": 0,
                        "list": [
                            {"code": 401, "parameters": ["Danger \\C[2]! Hero!"], "indent": 0},
                            {"code": 0, "parameters": [], "indent": 0}
                        ]
                    }
                ]
            }
        ]
    }
    (input_dir / "Map002.json").write_text(json.dumps(f2_data))

    extractor.process_files()
    extractor.import_files()

    # Verify provider received masked strings with __XTOK_
    assert any("__XTOK_" in s for s in provider.received_texts)

    # Verify output Map001 has restored \V[1] and translated words
    out1 = json.loads((output_dir / "Map001.json").read_text())
    text1 = out1["events"][1]["pages"][0]["list"][0]["parameters"][0]
    assert "\\V[1]" in text1
    assert "Herói" in text1
    assert "Perigo" in text1
    assert "__XTOK_" not in text1

    # Verify output Map002 has restored \C[2] and translated words
    out2 = json.loads((output_dir / "Map002.json").read_text())
    text2 = out2["events"][1]["pages"][0]["list"][0]["parameters"][0]
    assert "\\C[2]" in text2
    assert "Herói" in text2
    assert "Perigo" in text2
    assert "__XTOK_" not in text2

