"""
Duas coisas precisam ser verdade ao mesmo tempo:
1. Um valor de CONFIGURAÇÃO de plugin/script (align: "center", easing: "auto") não pode
   ser traduzido - "Center" virar "Centro" quebra o plugin.
2. A MESMA palavra como diálogo/escolha real do jogador (um menu "Start", uma escolha
   "Yes"/"No") tem que ser traduzida - senão o jogador vê a interface em inglês, que é
   exatamente a falha de usabilidade que este filtro causava antes desta correção.
O conteúdo sozinho não distingue os dois casos, por isso is_technical_or_code recebe um
`context` explícito de quem chama.
"""
import pytest

from src.extractor.rpgmaker.RPGEventStrategy import ChoiceStrategy
from src.extractor.rpgmaker.RPGTextFilters import RPGTextFilters

CONTEXT_DEPENDENT_VALUES = ["end", "nochannel", "channel", "start", "stop", "play", "pause", "YES", "NO", "OK", "Cancel"]
UNCONDITIONAL_CODE_LITERALS = ["true", "false", "null", "undefined", "NaN"]


@pytest.mark.parametrize("value", CONTEXT_DEPENDENT_VALUES)
def test_context_dependent_values_are_technical_only_as_plugin_params(value):
    assert RPGTextFilters.is_technical_or_code(value, context='param') is True
    assert RPGTextFilters.is_technical_or_code(value, context='dialogue') is False


@pytest.mark.parametrize("value", UNCONDITIONAL_CODE_LITERALS)
def test_code_literals_are_never_translatable_in_any_context(value):
    assert RPGTextFilters.is_technical_or_code(value, context='param') is True
    assert RPGTextFilters.is_technical_or_code(value, context='dialogue') is True


def test_choice_yes_no_are_now_translated_as_real_dialogue():
    item = {"code": 102, "parameters": [["Yes", "No"]]}
    assert ChoiceStrategy().extract(item) == ["Yes", "No"]


def test_choice_mixed_list_keeps_everything_that_is_not_code():
    item = {"code": 102, "parameters": [["Attack", "Yes", "Run away"]]}
    extracted = ChoiceStrategy().extract(item)
    assert extracted == ["Attack", "Yes", "Run away"]


def test_rpg_text_filters_narrowed_patterns():
    translatable_prose = [
        "$100 for this sword!",
        "new game plus unlocked!",
        "class change available",
        "if (you want) rest here",
        "show_if(v[1] == 1) Secret option",
    ]
    for s in translatable_prose:
        assert RPGTextFilters.is_technical_or_code(s) is False, f"Expected False for {s}"

    code_strings = [
        "$gameSwitches.setValue(1, true)",
        "if (v[1] == 1) { return; }",
    ]
    for s in code_strings:
        assert RPGTextFilters.is_technical_or_code(s) is True, f"Expected True for {s}"


def test_mask_patterns_preserve_ui_words():
    from src.extractor.rpgmaker.RPGMakerExtractor import RPGMakerExtractor
    import src.utils.TextsUtils as TextsUtils

    labels = ["Yes", "No", "Start", "Cancel"]
    pats = RPGMakerExtractor(None).get_mask_patterns()
    masked, mp = TextsUtils.mask_tokens_in_structure(labels, pats)
    assert masked == ["Yes", "No", "Start", "Cancel"]
    assert len(mp) == 0


def test_wolf_text_filters_single_words_and_technical_identifiers():
    from src.extractor.wolfrpg.WolfTextFilters import WolfTextFilters

    for w in ["Attack", "Potion", "Yes", "No", "Wait."]:
        assert WolfTextFilters.is_translatable(w) is True, f"Expected True for {w!r}"

    for w in ["true", "false", "null", "se_attack_01", "bgm/battle.ogg"]:
        assert WolfTextFilters.is_translatable(w) is False, f"Expected False for {w!r}"


def test_json_extractor_cjk_escapes_and_ui_slash_pairs():
    from src.extractor.JsonExtractor import JsonExtractor

    for s in [r"\C[2]魔法の剣\C[0]", r"こんにちは\n世界", "Yes/No", "はい/いいえ", "Press Attack/Defend to choose your action.", "Choose sword and/or shield"]:
        assert JsonExtractor._is_translatable_string(s) is True, f"Expected True for {s!r}"

    for s in ["img/pictures/hero.png", "audio/bgm/theme", "custom/folder/file", "some_folder/asset"]:
        assert JsonExtractor._is_translatable_string(s) is False, f"Expected False for {s!r}"




