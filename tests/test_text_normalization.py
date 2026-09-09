import os
import io
import pytest
from src.utils.TextsUtils import normalize_western_chars, fix_mojibake
from src.extractor.wolfrpg.WolfBinaryAdapter import WolfFileCoder, WolfBinaryAdapter, WolfCommonEvents


def test_normalize_western_chars_portuguese():
    # Teste de acentos e cedilha
    original = "O que você gostaria de perguntar? Ações, poções e maçãs no café."
    expected = "O que voce gostaria de perguntar? Acoes, pocoes e macas no cafe."
    assert normalize_western_chars(original) == expected

    # Teste de pontuações curvas e especiais
    punct = "“Diálogo”… ‘Citação’ — traço e 1º lugar"
    expected_punct = '"Dialogo"... \'Citacao\' - traco e 1o lugar'
    assert normalize_western_chars(punct) == expected_punct


def test_fix_mojibake_recovery():
    # Teste de recuperação de texto corrompido por decodificação incorreta em CP932
    corrupted_1 = "O que vocﾃｪ gostaria de perguntar?"
    assert normalize_western_chars(corrupted_1) == "O que voce gostaria de perguntar?"

    corrupted_2 = "Mas isso ﾃｩ informaﾃｧﾃ｣o pessoal窶ｦ"
    assert normalize_western_chars(corrupted_2) == "Mas isso e informacao pessoal..."

    corrupted_3 = "Encerrar a data ﾃ\uf8f0 forﾃｧa"
    assert normalize_western_chars(corrupted_3) == "Encerrar a data a forca"


def test_preserve_genuine_japanese():
    # Teste de preservação de texto japonês genuíno
    jp_text_1 = "○◆メッセージウィンドウ[ｲﾝﾀﾌｪｰｽ]"
    assert normalize_western_chars(jp_text_1) == jp_text_1

    jp_text_2 = "選択肢（座標指定）"
    assert normalize_western_chars(jp_text_2) == jp_text_2

    jp_text_3 = "会話開始"
    assert normalize_western_chars(jp_text_3) == jp_text_3


def test_wolf_file_coder_cp932_normalization():
    # Testa escrita em stream CP932 com normalização automática
    stream = io.BytesIO()
    coder = WolfFileCoder(stream, is_utf8=False)

    text_with_accents = "Você deseja salvar a poção?"
    coder.write_string(text_with_accents)

    # Recarrega a partir da stream em modo CP932
    stream.seek(0)
    reader = WolfFileCoder(stream, is_utf8=False)
    decoded = reader.read_string()

    # O texto decodificado deve ser puro ASCII sem mojibake
    assert decoded == "Voce deseja salvar a pocao?"
    assert "ﾃ" not in decoded
    assert "ç" not in decoded
    assert "ã" not in decoded


def test_choice_coordinates_adjustment():
    # Carrega o CommonEvent.dat atual do jogo e valida que X é mantido em 80
    src_ce = "/mnt/hdd/game/MY v2.10 DLC patched(English)/Data/BasicData/CommonEvent.dat"
    if os.path.exists(src_ce):
        ce_data = WolfBinaryAdapter.load_file(src_ce)
        ce_814 = next((ev for ev in ce_data if ev and ev.get("id") == 814), None)
        ce_815 = next((ev for ev in ce_data if ev and ev.get("id") == 815), None)

        assert ce_814 is not None
        assert ce_814["list"][0]["parameters"][2] == 80  # X coordenate = 80

        assert ce_815 is not None
        assert ce_815["list"][0]["parameters"][1] == 80  # SysVar 9000003 = 80
