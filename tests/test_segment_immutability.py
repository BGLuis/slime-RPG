import pytest
from src.extractor.Segment import Segment, SegmentContext


def test_segment_context_is_immutable():
    seg = Segment(id="map#1", source="Hello", context={"kind": "dialogue"})

    with pytest.raises(TypeError):
        seg.context.kind = "script"

    with pytest.raises(TypeError):
        seg.context["kind"] = "script"

    with pytest.raises(TypeError):
        seg.constraints["max_len"] = 50


def test_segment_is_hashable_and_usable_in_set_and_dict():
    seg1 = Segment(id="map#1", source="Hello", context={"kind": "dialogue", "file": "Map001.json"})
    seg2 = Segment(id="map#1", source="Hello", context={"kind": "dialogue", "file": "Map001.json"})
    seg3 = Segment(id="map#2", source="World", context={"kind": "dialogue", "file": "Map001.json"})

    # hash() must not raise TypeError
    h1 = hash(seg1)
    h2 = hash(seg2)
    assert isinstance(h1, int)
    assert h1 == h2
    assert seg1 == seg2

    # Must be usable in a set
    s = {seg1, seg2, seg3}
    assert len(s) == 2
    assert seg1 in s
    assert seg3 in s

    # Must be usable as dict keys
    d = {seg1: "first", seg3: "third"}
    assert d[seg2] == "first"
