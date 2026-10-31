"""
Testes de round-trip do corpus de textos hostis contra todos os extratores
(RPG Maker, Wolf RPG, CSV, JSON) e contra a inserção de strings em JavaScript
(Epic #32: #33, #4, #34, #35, #36, #37, #38).
"""
import pytest

import src.utils.TextsUtils as TextsUtils
from src.extractor.CSVExtractor import CSVExtractor
from src.extractor.JsonExtractor import JsonExtractor
from src.extractor.rpgmaker.RPGEventStrategy import ScriptStrategy
from src.extractor.rpgmaker.RPGMakerExtractor import RPGMakerExtractor
from src.extractor.wolfrpg.WolfRPGExtractor import WolfRPGExtractor
from tests.corpus.hostile_texts import HOSTILE_PROSE_TEXTS, HOSTILE_SCRIPT_TEXTS


EXTRACTOR_CLASSES = [
    pytest.param(RPGMakerExtractor, id="rpgmaker"),
    pytest.param(WolfRPGExtractor, id="wolfrpg"),
    pytest.param(CSVExtractor, id="csv"),
    pytest.param(JsonExtractor, id="json"),
]


def _parse_js_string_literal(literal: str) -> str:
    """
    Parser estrito de literal de string JavaScript ('...' ou "...").
    Levanta SyntaxError se:
    - não começar e terminar com a mesma aspa (' ou ");
    - houver quebra de linha crua não escapada;
    - a aspa delimitadora aparecer não escapada antes do fim;
    - uma barra invertida final escapar a aspa de fechamento.
    Retorna o valor exato da string decodificada.
    """
    if len(literal) < 2 or literal[0] not in ("'", '"') or literal[-1] != literal[0]:
        raise SyntaxError(f"Literal JS malformado (delimitadores): {literal!r}")

    quote = literal[0]
    body = literal[1:-1]
    out = []
    i = 0
    n = len(body)
    escape_map = {
        'n': '\n',
        'r': '\r',
        't': '\t',
        'b': '\b',
        'f': '\f',
        'v': '\v',
        '0': '\0',
        '\\': '\\',
        "'": "'",
        '"': '"',
    }

    while i < n:
        ch = body[i]
        if ch in ('\n', '\r', '\u2028', '\u2029'):
            raise SyntaxError(f"Quebra de linha crua dentro de literal JS: {literal!r}")
        if ch == quote:
            raise SyntaxError(f"Aspa '{quote}' não escapada dentro de literal JS: {literal!r}")
        if ch == '\\':
            if i + 1 >= n:
                raise SyntaxError(f"Barra invertida final escapa o delimitador de fechamento: {literal!r}")
            nxt = body[i + 1]
            if nxt == 'u' and i + 5 < n:
                hex_part = body[i + 2:i + 6]
                out.append(chr(int(hex_part, 16)))
                i += 6
                continue
            out.append(escape_map.get(nxt, nxt))
            i += 2
        else:
            out.append(ch)
            i += 1

    return ''.join(out)


@pytest.mark.parametrize("extractor_cls", EXTRACTOR_CLASSES)
@pytest.mark.parametrize("prose", HOSTILE_PROSE_TEXTS)
def test_hostile_prose_round_trip_across_all_extractors(extractor_cls, prose):
    """
    Para qualquer extrator, o fluxo:
    mask -> fake translate (identidade) -> unmask -> fix_text_translate
    deve devolver o texto de prosa exatamente igual à entrada.
    """
    extractor = extractor_cls(None)
    patterns = extractor.get_mask_patterns()

    masked, mask_map = TextsUtils.mask_tokens_in_structure([prose], patterns)
    # Nenhum dos textos puramente de prosa do corpus deve ter palavras comuns mascaradas
    assert masked == [prose], f"{extractor_cls.name} mascarou prosa indevidamente: {masked!r} (map={mask_map!r})"

    unmasked = TextsUtils.unmask_tokens_in_structure(masked, mask_map)

    if extractor_cls is CSVExtractor:
        fixed_dict = extractor.fix_text_translate({"0": unmasked[0]}, {"0": prose})
        fixed = fixed_dict["0"]
    else:
        fixed_list = extractor.fix_text_translate([unmasked[0]], [prose])
        fixed = fixed_list[0]

    assert fixed == prose


@pytest.mark.parametrize("quote_char", ["'", '"'])
@pytest.mark.parametrize("script_text", HOSTILE_SCRIPT_TEXTS)
def test_hostile_script_string_insertion_produces_valid_js(quote_char, script_text):
    """
    Para toda string do corpus F-06 inserida em chamada JS com aspas simples ou duplas,
    um parser JS lê de volta exatamente o texto inserido (#33, #36).
    """
    template = f"$gameMessage.add({quote_char}x{quote_char})"
    result = ScriptStrategy.insert_script_string(template, iter([script_text]))

    prefix = "$gameMessage.add("
    suffix = ")"
    assert result.startswith(prefix) and result.endswith(suffix)
    js_literal = result[len(prefix):-len(suffix)]

    parsed = _parse_js_string_literal(js_literal)
    assert parsed == script_text


@pytest.mark.parametrize("script_text", HOSTILE_SCRIPT_TEXTS)
def test_hostile_script_subject_branch_produces_valid_js(script_text):
    """O ramo _subject= de ScriptStrategy também deve escapar strings JS corretamente (#36)."""
    template = '"0_t=10_subject=x"'
    result = ScriptStrategy.insert_script_string(template, iter([script_text]))
    parsed = _parse_js_string_literal(result)
    assert parsed == f"0_t=10_subject={script_text}"


def test_issue_4_and_34_code_segment_fixes_still_work():
    """Segmentos que são código real continuam recebendo as correções JS (#4, #34)."""
    res = RPGMakerExtractor.fix_text_translate(
        ['if(v[1] == verdadeiro)'],
        ['if(v[1] == true)']
    )
    assert res == ['if(v[1] == true)']

    res_no_orig = RPGMakerExtractor.fix_text_translate(['if(v[1] == verdadeiro)'])
    assert res_no_orig == ['if(v[1] == true)']


def test_issue_35_nested_parentheses_in_script_masked_as_single_token():
    """'if (a(b) && c)' deve ser mascarado como um único token quando é script (#35)."""
    extractor = RPGMakerExtractor(None)
    patterns = extractor.get_mask_patterns()
    masked, mask_map = TextsUtils.mask_tokens_in_structure('if (a(b) && c)', patterns)
    assert len(mask_map) == 1
    token = next(iter(mask_map.keys()))
    assert masked == token
    assert mask_map[token] == 'if (a(b) && c)'


def test_issue_36_specific_reproductions():
    """Três casos explícitos de reprodução da issue #36."""
    r1 = ScriptStrategy.insert_script_string("$gameMessage.add('x')", iter(['It\'s "fine"\\']))
    assert _parse_js_string_literal(r1[len("$gameMessage.add("):-1]) == 'It\'s "fine"\\'

    s2 = ScriptStrategy._sanitize_script_param('a\\\\"b', '"')
    assert _parse_js_string_literal(f'"{s2}"') == 'a\\\\"b'

    s3 = ScriptStrategy._sanitize_script_param('"Don\'t"', "'")
    assert _parse_js_string_literal(f"'{s3}'") == '"Don\'t"'


def test_issue_38_mutated_token_never_restores_wrong_code():
    """Dois tokens diferindo por um caractere, com um mutado, nunca restauram o código errado (#38)."""
    originals = ["Herói \\V[1] e \\C[2]ouro"]
    patterns = RPGMakerExtractor(None).get_mask_patterns()
    masked, mask_map = TextsUtils.mask_tokens_in_structure(originals, patterns)

    tokens = list(mask_map.keys())
    assert tokens == ["__XTOK_0__", "__XTOK_1__"]

    # Mutar __XTOK_1__ para __XTOK_9__ (1 caractere de diferença para __XTOK_0__ e __XTOK_1__)
    corrupted = [masked[0].replace("__XTOK_1__", "__XTOK_9__")]
    restored = TextsUtils.unmask_tokens_in_structure(corrupted, mask_map)

    # Nunca deve duplicar \V[1] no lugar de \C[2]; deve fazer fallback seguro para o original do segmento
    assert restored == originals
    assert restored[0].count("\\V[1]") == 1
    assert "\\C[2]" in restored[0]


def test_issue_38_lost_token_degrades_only_its_own_segment():
    """Um token perdido degrada apenas o seu próprio segmento; os demais do lote são restaurados (#38)."""
    originals = [
        "Segmento 1 com \\V[10] perdido",
        "Segmento 2 com \\C[2]preservado\\C[0]",
    ]
    patterns = RPGMakerExtractor(None).get_mask_patterns()
    masked, mask_map = TextsUtils.mask_tokens_in_structure(originals, patterns)

    # Simula tradutor que perdeu o token do segmento 0 mas manteve os tokens do segmento 1
    translated = [
        "Segment 1 with lost token",
        masked[1].replace("Segmento 2 com", "Segment 2 with").replace("preservado", "preserved"),
    ]

    restored = TextsUtils.unmask_tokens_in_structure(translated, mask_map)
    assert restored[0] == originals[0]
    assert restored[1] == "Segment 2 with \\C[2]preserved\\C[0]"


def test_battle_manager_log_window_push_extraction_and_insertion():
    from src.extractor.rpgmaker.RPGEventStrategy import ScriptStrategy
    s = "BattleManager._logWindow.push('addText', 'Critical hit!')"
    extracted = ScriptStrategy.extract_script_string(s)
    assert extracted == ["Critical hit!"]
    res = ScriptStrategy.insert_script_string(s, iter(["Golpe crítico!"]))
    assert res == "BattleManager._logWindow.push('addText', 'Golpe crítico!')"


def test_choice_condition_extraction_and_natural_language_parentheses():
    from src.extractor.rpgmaker.RPGEventStrategy import ChoiceStrategy, ChoiceBranchStrategy
    choice_strat = ChoiceStrategy()
    branch_strat = ChoiceBranchStrategy()

    cmd_choice = {"code": 102, "parameters": [["if(s[1]) Attack (Strong)"]]}
    assert choice_strat.extract(cmd_choice) == ["Attack (Strong)"]

    cmd_branch = {"code": 402, "parameters": [0, "if(s[1]) Attack (Strong)"]}
    assert branch_strat.extract(cmd_branch) == "Attack (Strong)"

    cmd_prose = {"code": 102, "parameters": [["If (you dare) fight", "Es (opcional) hablar"]]}
    assert choice_strat.extract(cmd_prose) == ["If (you dare) fight", "Es (opcional) hablar"]


def test_game_switches_masks_boolean_literals():
    pats = RPGMakerExtractor(None).get_mask_patterns()
    s = "$gameSwitches.setValue(1, true)"
    masked, mp = TextsUtils.mask_tokens_in_structure(s, pats)
    assert "$gameSwitches" in mp.values()
    assert "true" in mp.values()


def test_bracket_vars_leaves_prepositions_unmasked():
    pats = RPGMakerExtractor(None).get_mask_patterns()
    s = "Choose a [1] or v[2] in [3]"
    masked, mp = TextsUtils.mask_tokens_in_structure(s, pats)
    assert mp == {"__XTOK_0__": "v[2]"}
    assert masked == "Choose a [1] or __XTOK_0__ in [3]"



