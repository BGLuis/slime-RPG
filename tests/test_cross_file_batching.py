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
