import pytest
from src.extractor.rpgmaker.RPGMakerExtractor import RPGMakerExtractor


@pytest.mark.parametrize(
    "input_text,expected_text",
    [
        (r"\NAME[General Cliff]", r"\NAME[General Cliff]"),
        (r"\nomE[General Cliff]", r"\NAME[General Cliff]"),
        (r"\namE[General Cliff]", r"\NAME[General Cliff]"),
        (r"\nome[General Cliff]", r"\NAME[General Cliff]"),
        (r"\name[General Cliff]", r"\NAME[General Cliff]"),
        (r"\nomE [General Cliff]", r"\NAME[General Cliff]"),
        (r"\NAME [General Cliff]", r"\NAME[General Cliff]"),
        (
            r"\nomE[General Cliff]Olá, \namE[Eris]!",
            r"\NAME[General Cliff]Olá, \NAME[Eris]!",
        ),
        (
            r"\c[1]\nomE[Morador da cidade]\c[0]Bom dia!",
            r"\C[1]\NAME[Morador da cidade]\C[0]Bom dia!",
        ),
        (
            r"\fs[20]\nomE[Velho]\g",
            r"\fs[20]\NAME[Velho]\G",
        ),
    ],
)
def test_name_tag_normalization(input_text, expected_text):
    texts = [input_text]
    RPGMakerExtractor.fix_text_translate(texts)
    assert texts[0] == expected_text


def test_name_tag_dict_input():
    texts = {"0": r"\nomE[General Cliff]Mensagem de teste"}
    RPGMakerExtractor.fix_text_translate(texts)
    assert texts["0"] == r"\NAME[General Cliff]Mensagem de teste"
