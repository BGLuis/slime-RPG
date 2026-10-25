import os
import re
from src.extractor.EventCommandStrategy import (
    EventCommandStrategy,
    walk_event_commands,
    extract_commands_tree,
    insert_commands_tree,
)
from src.extractor.rpgmaker.RPGEventStrategy import (
    EventStrategy,
    ShowTextStrategy,
    ChoiceStrategy,
    ChoiceBranchStrategy,
    ScriptStrategy,
    PluginStrategyMZ,
    PluginStrategyMV,
    ControlVariablesStrategy,
    MovementRouteStrategy,
    RouteStepStrategy,
)
from src.extractor.wolfrpg.WolfEventStrategy import (
    WolfEventStrategy,
    ShowMessageStrategy,
    ChoicesStrategy,
    SetStringStrategy,
    CommonEventParamStrategy,
    PictureStrategy,
    CommentStrategy,
)


def test_only_single_strategy_base_class_exists_under_src_extractor():
    extractor_root = os.path.join(os.path.dirname(os.path.dirname(__file__)), "src", "extractor")
    matches = []
    pattern = re.compile(r"class .*Strategy:")
    for root, _, files in os.walk(extractor_root):
        for fname in files:
            if not fname.endswith(".py"):
                continue
            fpath = os.path.join(root, fname)
            with open(fpath, "r", encoding="utf-8") as f:
                for line_no, line in enumerate(f, start=1):
                    if pattern.search(line):
                        matches.append((os.path.relpath(fpath, extractor_root), line_no, line.strip()))

    assert len(matches) == 1
    assert matches[0][0] == "EventCommandStrategy.py"
    assert matches[0][2] == "class EventCommandStrategy:"


def test_all_rpgmaker_and_wolf_strategies_inherit_unified_base():
    assert EventStrategy is EventCommandStrategy
    assert WolfEventStrategy is EventCommandStrategy

    rpg_strategies = (
        ShowTextStrategy,
        ChoiceStrategy,
        ChoiceBranchStrategy,
        ScriptStrategy,
        PluginStrategyMZ,
        PluginStrategyMV,
        ControlVariablesStrategy,
        MovementRouteStrategy,
        RouteStepStrategy,
    )
    wolf_strategies = (
        ShowMessageStrategy,
        ChoicesStrategy,
        SetStringStrategy,
        CommonEventParamStrategy,
        PictureStrategy,
        CommentStrategy,
    )
    for strat_cls in rpg_strategies + wolf_strategies:
        assert issubclass(strat_cls, EventCommandStrategy)


def test_shared_walker_handles_rpgmaker_and_wolf_maps():
    map_data = {
        "events": [
            None,
            {
                "id": 5,
                "pages": [
                    {
                        "id": 2,
                        "list": [
                            {"code": 101, "string_args": ["Hello WOLF!"]},
                        ],
                    }
                ],
            },
        ]
    }
    walked = list(walk_event_commands(map_data, "Map"))
    assert len(walked) == 1
    assert walked[0][:3] == (5, 2, 0)

    strat = ShowMessageStrategy()
    segs = strat.extract_segments(walked[0][3], prefix="Map001.mps#event:5/page:2/cmd:0", file_name="Map001.mps")
    assert len(segs) == 1
    assert segs[0].source == "Hello WOLF!"
    assert segs[0].context.kind == "dialogue"

    choice_segs = ChoiceStrategy().extract_segments(
        {"code": 102, "parameters": [["Yes", "No"], 1]},
        prefix="Map001.json#event:1/page:0/cmd:1",
        file_name="Map001.json",
    )
    assert [s.context.kind for s in choice_segs] == ["choice", "choice"]

    script_segs = ScriptStrategy().extract_segments(
        {"code": 355, "parameters": ["$gameMessage.add('Alert!');"]},
        prefix="Map001.json#event:1/page:0/cmd:2",
        file_name="Map001.json",
    )
    assert [s.context.kind for s in script_segs] == ["script"]

    wolf_choice_segs = ChoicesStrategy().extract_segments(
        {"code": 102, "string_args": ["Sim", "Não"]},
        prefix="Map001.mps#event:1/page:0/cmd:1",
        file_name="Map001.mps",
    )
    assert [s.context.kind for s in wolf_choice_segs] == ["choice", "choice"]

    wolf_setstr_segs = SetStringStrategy().extract_segments(
        {"code": 122, "string_args": ["Valor de texto"]},
        prefix="Map001.mps#event:1/page:0/cmd:2",
        file_name="Map001.mps",
    )
    assert [s.context.kind for s in wolf_setstr_segs] == ["script"]

