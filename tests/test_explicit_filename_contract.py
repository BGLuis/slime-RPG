import pytest
from src.extractor.rpgmaker.RPGMakerExtractor import RPGMakerExtractor
from src.extractor.wolfrpg.WolfRPGExtractor import WolfRPGExtractor
from src.extractor.JsonExtractor import JsonExtractor


def test_extract_segments_without_filename_and_without_attr_raises_value_error():
    rpg = RPGMakerExtractor(None)
    data = {"events": [{"id": 1, "pages": [{"list": [{"code": 401, "parameters": ["Hello"]}]}]}]}

    # Calling extract_segments without file_name and without _file_name must raise ValueError
    with pytest.raises(ValueError, match="extract_segments requires an explicit file_name"):
        rpg.extract_segments(None, data)

    with pytest.raises(ValueError, match="extract_segments requires an explicit file_name"):
        rpg.extract_segments("", data)

    wolf = WolfRPGExtractor(None)
    wolf_data = {"events": [{"id": 1, "pages": [{"list": [{"code": 101, "string_args": ["Hello"]}]}]}]}
    with pytest.raises(ValueError, match="extract_segments requires an explicit file_name"):
        wolf.extract_segments(None, wolf_data)

    with pytest.raises(ValueError, match="extract_segments requires an explicit file_name"):
        wolf.extract_segments("", wolf_data)


def test_extract_segments_uses_data_file_name_attr_if_not_passed():
    class CustomData(dict):
        pass

    rpg = RPGMakerExtractor(None)
    data = CustomData({"events": [{"id": 1, "pages": [{"list": [{"code": 401, "parameters": ["Hello"]}]}]}]})
    data._file_name = "Map002.json"

    segs = rpg.extract_segments(None, data)
    assert len(segs) > 0
    assert all(s.id.startswith("Map002.json#") for s in segs)
