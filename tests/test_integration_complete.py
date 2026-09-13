"""
Fluxo completo do pipeline de tradução (mascarar -> "traduzir" -> desmascarar -> corrigir)
sem depender de rede: garante que tags HTML e códigos de formato RPG Maker sobrevivem
intactos mesmo quando a "tradução" (aqui simulada) altera a capitalização dos códigos,
que é exatamente o que o Google Translate faz na prática.
"""
import pytest

import src.utils.TextsUtils as TextsUtils
from src.extractor.rpgmaker.RPGMakerExtractor import RPGMakerExtractor
from src.pipeline import TranslationContext, UnmaskingStep


def test_full_translation_flow_preserves_format_codes_and_html():
    extractor = RPGMakerExtractor(None)
    original_text = "<center>Welcome \\M[INFO_0001]!</center>"

    patterns = extractor.get_mask_patterns()
    masked_text, mask_map = TextsUtils.mask_tokens_in_structure(original_text, patterns)

    # A máscara precisa ter protegido o código e as tags: nada disso deve sobrar
    # visível no texto que seria enviado a um tradutor real.
    assert "\\M[INFO_0001]" not in masked_text
    assert "<center>" not in masked_text

    # Simula um tradutor real: reescreve o texto livre, sem tocar nos placeholders __XTOK_...
    simulated_translated = masked_text.replace("Welcome", "Bem-vindo")

    unmasked_text = TextsUtils.unmask_tokens_in_structure(simulated_translated, mask_map)

    translated_list = [unmasked_text]
    RPGMakerExtractor.fix_text_translate(translated_list, [original_text])
    final_text = translated_list[0]

    assert "\\M[INFO_0001]" in final_text
    assert "<center>" in final_text and "</center>" in final_text
    assert "Bem-vindo" in final_text


def test_multiple_codes_are_restored_after_case_corruption():
    original = "Olá \\N[1], você tem \\C[2]\\V[10]\\C[0] moedas e \\I[5]itens."
    translated = "Olá \\n[1], você tem \\c[2]\\v[10]\\c[0] moedas e \\i[5]itens."

    translated_list = [translated]
    RPGMakerExtractor.fix_text_translate(translated_list, [original])

    assert translated_list[0] == original


def test_mask_unmask_round_trip_is_lossless():
    extractor = RPGMakerExtractor(None)
    original_text = "<center>Welcome \\V[5]! You have \\C[2]gold\\C[0].</center>"

    patterns = extractor.get_mask_patterns()
    masked_text, mask_map = TextsUtils.mask_tokens_in_structure(original_text, patterns)
    restored_text = TextsUtils.unmask_tokens_in_structure(masked_text, mask_map)

    assert restored_text == original_text


def test_unmask_raises_instead_of_leaking_an_unresolved_placeholder():
    """
    Se restar qualquer placeholder sem resolução (por exemplo, token corrompido ou
    inexistente no mapping), a função deve levantar ValueError para impedir que
    placeholders vazem para os arquivos finais do jogo.
    """
    mask_map = {
        "__XTOK_aaaaaaaa__": "leftover __XTOK_bbbbbbbb__ text",
        # __XTOK_bbbbbbbb__ não existe no mask_map e não pode ser resolvido
    }

    with pytest.raises(ValueError):
        TextsUtils.unmask_tokens_in_structure("start __XTOK_aaaaaaaa__ end", mask_map)


def test_unmasking_step_propagates_failure_instead_of_swallowing_it():
    """UnmaskingStep não pode engolir a exceção: o arquivo precisa ir para o retry de
    process_file em vez de seguir adiante com um placeholder não resolvido."""
    context = TranslationContext(
        file_name="Map001.json", data={}, text="start __XTOK_aaaaaaaa__ end",
        translate_instance=None, file_path_str="Map001.json", extractor=None,
    )
    context.mask_map = {
        "__XTOK_aaaaaaaa__": "leftover __XTOK_bbbbbbbb__ text",
        # __XTOK_bbbbbbbb__ não existe no mask_map e não pode ser resolvido
    }

    with pytest.raises(ValueError):
        UnmaskingStep().process(context)


def test_unmask_resolves_transitive_placeholders():
    """Placeholders aninhados/transitivos no mapping são resolvidos com sucesso."""
    mask_map = {
        "__XTOK_aaaaaaaa__": "leftover __XTOK_bbbbbbbb__ text",
        "__XTOK_bbbbbbbb__": "real content",
    }
    res = TextsUtils.unmask_tokens_in_structure("start __XTOK_aaaaaaaa__ end", mask_map)
    assert res == "start leftover real content text end"

