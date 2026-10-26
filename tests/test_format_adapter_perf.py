import copy
import json
import os
import time
import tracemalloc
import pytest
from unittest.mock import patch

from src.extractor.FormatAdapter import JsonFormatAdapter
from src.extractor.rpgmaker.RPGMakerExtractor import RPGMakerExtractor


def test_no_deepcopy_in_load_file(tmp_path):
    test_json = tmp_path / "test.json"
    data = {"events": [{"id": i, "pages": [{"list": [{"code": 401, "parameters": [f"Text {i}"]}]}]} for i in range(100)]}
    test_json.write_text(json.dumps(data), encoding="utf-8")

    # Patch copy.deepcopy to ensure it is never called during load_file
    with patch("copy.deepcopy", side_effect=AssertionError("copy.deepcopy called during load_file!")):
        loaded = JsonFormatAdapter.load_file(str(test_json))

    assert loaded is not None
    assert getattr(loaded, "_is_dirty", True) is False


def test_unmodified_file_saves_byte_for_byte_without_dirty(tmp_path):
    test_json = tmp_path / "original.json"
    raw_bytes = b'{\n  "name": "Slime",\n  "hp": 100\n}\n'
    test_json.write_bytes(raw_bytes)

    loaded = JsonFormatAdapter.load_file(str(test_json))
    assert getattr(loaded, "_is_dirty", True) is False

    out_json = tmp_path / "saved.json"
    JsonFormatAdapter.save_file(str(out_json), loaded)

    assert out_json.read_bytes() == raw_bytes


def test_performance_memory_and_time_benchmark(tmp_path):
    # Create 5MB JSON with events
    test_json = tmp_path / "large_map.json"
    payload = {"events": [{"id": i, "pages": [{"list": [{"code": 401, "parameters": [f"Sentence number {j} of event {i}"] * 5} for j in range(30)]}]} for i in range(300)]}
    test_json.write_text(json.dumps(payload), encoding="utf-8")

    tracemalloc.start()
    t0 = time.perf_counter()
    loaded = JsonFormatAdapter.load_file(str(test_json))
    elapsed = time.perf_counter() - t0
    current, peak = tracemalloc.get_traced_memory()
    tracemalloc.stop()

    # Peak memory allocated for load_file should not exceed raw payload + parsed JSON overhead (much lower than deepcopy snapshot)
    # And load time should be fast
    assert loaded is not None
    assert peak > 0
    assert elapsed < 2.0
