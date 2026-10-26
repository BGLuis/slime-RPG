import pytest
from src.extractor.Segment import Segment
from src.extractor.EventCommandStrategy import EventCommandStrategy
from src.extractor.rpgmaker.RPGMakerExtractor import RPGMakerExtractor
from src.extractor.wolfrpg.WolfRPGExtractor import WolfRPGExtractor
from src.extractor.rpgmaker.RPGEventCodes import RPGEventCode
from src.extractor.wolfrpg.WolfEventCodes import WolfEventCode


def test_omitting_kind_in_segment_raises_value_error():
    # Omit context
    with pytest.raises(ValueError, match="Segment requires an explicit 'kind' in context"):
        Segment(id="seg#1", source="Hello")

    # Empty context
    with pytest.raises(ValueError, match="Segment requires an explicit 'kind' in context"):
        Segment(id="seg#1", source="Hello", context={})

    # None or empty kind
    with pytest.raises(ValueError, match="Segment requires an explicit 'kind' in context"):
        Segment(id="seg#1", source="Hello", context={"kind": None})

    with pytest.raises(ValueError, match="Segment requires an explicit 'kind' in context"):
        Segment(id="seg#1", source="Hello", context={"kind": ""})


def test_event_command_strategy_has_no_default_dialogue_fallback():
    # EventCommandStrategy.kind should not be hardcoded to "dialogue"
    assert getattr(EventCommandStrategy, "kind", None) is None


def test_kind_for_code_exercised_in_rpgmaker_and_wolfrpg():
    # RPGMakerExtractor._kind_for_code
    assert RPGMakerExtractor._kind_for_code(RPGEventCode.SHOW_CHOICES.value) == "choice"
    assert RPGMakerExtractor._kind_for_code(RPGEventCode.CHOICE_BRANCH.value) == "choice"
    assert RPGMakerExtractor._kind_for_code(RPGEventCode.SCRIPT.value) == "script"
    assert RPGMakerExtractor._kind_for_code(RPGEventCode.PLUGIN_COMMAND_MV.value) == "script"
    assert RPGMakerExtractor._kind_for_code(RPGEventCode.SHOW_TEXT.value) == "dialogue"

    # WolfRPGExtractor._kind_for_code
    assert WolfRPGExtractor._kind_for_code(WolfEventCode.SHOW_CHOICES) == "choice"
    assert WolfRPGExtractor._kind_for_code(WolfEventCode.SET_STRING) == "script"
    assert WolfRPGExtractor._kind_for_code(WolfEventCode.SHOW_MESSAGE) == "dialogue"
