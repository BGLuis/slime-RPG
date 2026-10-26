import pytest
from src.extractor.Segment import Segment, resolve_translation_map
from src.extractor.rpgmaker.RPGMakerExtractor import RPGMakerExtractor


def test_resolve_translation_map_propagates_internal_type_error():
    segs = [Segment(id="1", source="Hello", context={"kind": "dialogue"})]

    def buggy_translator(arg):
        if isinstance(arg, Segment):
            return "masked_bug!"
        return arg + 12345

    with pytest.raises(TypeError):
        resolve_translation_map(segs, buggy_translator)


def test_apply_segments_requires_canonical_arguments():
    rpg = RPGMakerExtractor(None)
    data = {"events": [{"id": 1, "pages": [{"list": [{"code": 401, "parameters": ["Hello"]}]}]}]}

    # Fails when missing required arguments
    with pytest.raises(TypeError):
        rpg.apply_segments()

    with pytest.raises(TypeError):
        rpg.apply_segments("Map001.json")

    with pytest.raises(TypeError):
        rpg.apply_segments("Map001.json", data)
