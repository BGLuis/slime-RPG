from src.extractor.wolfrpg.WolfEventStrategy import CommonEventParamStrategy
from src.extractor.wolfrpg.WolfRPGExtractor import WolfRPGExtractor
from src.extractor.wolfrpg.WolfEventCodes import WolfEventCode
from src.extractor.wolfrpg.WolfTextFilters import WolfTextFilters
from src.utils.TextsUtils import dictToList


def _extractor():
    return WolfRPGExtractor(None)


def test_database_value_equal_to_data_name_is_a_lookup_key():
    db = {"types": [{"id": 62, "name": "Costume", "data": [
        {"id": 0, "name": "Hair 1", "string_values": ["Hair 1", "Pretty hair"]},
    ]}]}
    texts = dictToList(_extractor().extract_text_database(db))
    assert "Hair 1" not in texts
    assert "Pretty hair" in texts


def test_media_path_with_extra_lines_is_not_translatable():
    assert not WolfTextFilters.is_translatable("SystemFile/[SE]Cancel.ogg\r\n80\r\n100")


def test_text_with_only_control_codes_is_not_translatable():
    assert not WolfTextFilters.is_translatable("\\s[1]")
    assert WolfTextFilters.is_translatable("\\c[2]Hello\\c[0]")


def test_call_by_name_never_translates_the_event_name():
    ex = _extractor()
    strategy = ex._strategies[WolfEventCode.CALL_COMMON_BY_NAME]
    item = {"string_args": ["歩行グラフィック：向き変更", "Check it"]}

    assert strategy.extract(item) == ["歩行グラフィック：向き変更", "Check it"]
    strategy.insert(item, ["X", "Verifique"])
    assert item["string_args"] == ["歩行グラフィック：向き変更", "Verifique"]


def test_call_by_name_with_only_the_name_has_nothing_to_translate():
    assert CommonEventParamStrategy(skip_first=True).extract({"string_args": ["初期設定"]}) is None


def test_comments_are_not_extracted():
    assert WolfEventCode.COMMENT not in _extractor()._strategies
