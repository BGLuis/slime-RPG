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

