import codecs
import json
import os
import pytest
from src.extractor.CSVExtractor import CSVExtractor
from src.extractor.JsonExtractor import JsonExtractor
from src.translate.BaseTranslate import BaseTranslate


class IdentityTranslate(BaseTranslate):
    agent = 'identity'

    def __init__(self, *args, **kwargs):
        super().__init__(*args, **kwargs)
        self.cache = {}

    def _translate_single_batch(self, texts):
        return [t for t in texts]

    def translator(self, texts, progress_callback=None):
        return texts


class PrefixTranslate(BaseTranslate):
    agent = 'prefix_translate'

    def _translate_single_batch(self, texts):
        return [f"[PT] {t}" for t in texts]

    def translator(self, texts, progress_callback=None):
        if isinstance(texts, list):
            return [f"[PT] {t}" if isinstance(t, str) else t for t in texts]
        elif isinstance(texts, dict):
            return {k: f"[PT] {v}" if isinstance(v, str) else v for k, v in texts.items()}
        elif isinstance(texts, str):
            return f"[PT] {texts}"
        return texts


# ==============================================================================
# TESTES DA ISSUE #6 — CSVExtractor
# ==============================================================================

def test_csv_roundtrip_no_bom_byte_for_byte_identical(tmp_path):
    """
    Testa que um CSV sem BOM processado com tradutor identidade resulta em
    um arquivo de saída byte-a-byte idêntico ao de entrada (sem BOM e sem colunas _old).
    """
    inp_dir = tmp_path / "input"
    proc_dir = tmp_path / "process"
    out_dir = tmp_path / "output"
    inp_dir.mkdir()
    proc_dir.mkdir()
    out_dir.mkdir()

    raw_csv_content = b"id,text\n1,Hello there\n2,Good bye\n"
    csv_file = inp_dir / "sample.csv"
    csv_file.write_bytes(raw_csv_content)

    trans = IdentityTranslate(lang_source='en', lang_target='pt')
    extractor = CSVExtractor(trans)
    extractor.folderInput = str(inp_dir)
    extractor.folderProcess = str(proc_dir)
    extractor.folderOutput = str(out_dir)

    extractor.process_file(str(csv_file))
    extractor.import_files()

    output_file = out_dir / "sample.csv"
    assert output_file.exists()

    output_bytes = output_file.read_bytes()
    assert output_bytes == raw_csv_content
    assert not output_bytes.startswith(codecs.BOM_UTF8)
    assert b"text_old" not in output_bytes


def test_csv_roundtrip_with_utf8_bom_and_crlf_byte_for_byte_identical(tmp_path):
    """
    Testa que um CSV com BOM UTF-8 e CRLF processado com tradutor identidade resulta em
    um arquivo de saída byte-a-byte idêntico ao de entrada (preservando BOM UTF-8 e CRLF).
    """
    inp_dir = tmp_path / "input"
    proc_dir = tmp_path / "process"
    out_dir = tmp_path / "output"
    inp_dir.mkdir()
    proc_dir.mkdir()
    out_dir.mkdir()

    raw_csv_content = b"\xef\xbb\xbfid,text\r\n1,Hello there\r\n2,Good bye\r\n"
    csv_file = inp_dir / "bom_sample.csv"
    csv_file.write_bytes(raw_csv_content)

    trans = IdentityTranslate(lang_source='en', lang_target='pt')
    extractor = CSVExtractor(trans)
    extractor.folderInput = str(inp_dir)
    extractor.folderProcess = str(proc_dir)
    extractor.folderOutput = str(out_dir)

    extractor.process_file(str(csv_file))
    extractor.import_files()

    output_file = out_dir / "bom_sample.csv"
    assert output_file.exists()

    output_bytes = output_file.read_bytes()
    assert output_bytes == raw_csv_content
    assert output_bytes.startswith(codecs.BOM_UTF8)
    assert b"\r\n" in output_bytes
    assert b"text_old" not in output_bytes


def test_csv_subfolders_same_filename_different_delimiters(tmp_path):
    """
    Testa que dois CSVs de mesmo nome em subpastas diferentes com delimitadores diferentes
    (en/strings.csv com vírgula e menu/strings.csv com ponto e vírgula) mantêm cada um seu delimitador original.
    """
    inp_dir = tmp_path / "input"
    proc_dir = tmp_path / "process"
    out_dir = tmp_path / "output"

    en_dir = inp_dir / "en"
    menu_dir = inp_dir / "menu"
    en_dir.mkdir(parents=True)
    menu_dir.mkdir(parents=True)
    proc_dir.mkdir()
    out_dir.mkdir()

    # en/strings.csv usa delimitador ','
    en_file = en_dir / "strings.csv"
    en_content = "id,text\n10,Attack\n20,Defend\n"
    en_file.write_text(en_content, encoding='utf-8')

    # menu/strings.csv usa delimitador ';'
    menu_file = menu_dir / "strings.csv"
    menu_content = "id;text\n1;Start Game\n2;Options\n"
    menu_file.write_text(menu_content, encoding='utf-8')

    trans = IdentityTranslate(lang_source='en', lang_target='pt')
    extractor = CSVExtractor(trans)
    extractor.folderInput = str(inp_dir)
    extractor.folderProcess = str(proc_dir)
    extractor.folderOutput = str(out_dir)

    extractor.process_files()
    extractor.import_files()

    out_en = out_dir / "en" / "strings.csv"
    out_menu = out_dir / "menu" / "strings.csv"

    assert out_en.exists()
    assert out_menu.exists()

    en_res = out_en.read_text(encoding='utf-8')
    menu_res = out_menu.read_text(encoding='utf-8')

    # Verifica se os delimitadores respectivos foram preservados
    assert "," in en_res and ";" not in en_res
    assert ";" in menu_res and "," not in menu_res
    assert "Attack" in en_res
    assert "Start Game" in menu_res


def test_csv_never_writes_old_columns(tmp_path):
    """
    Garante que colunas *_old nunca sejam gravadas no arquivo CSV de saída,
    mesmo quando o texto traduzido difere do original.
    """
    inp_dir = tmp_path / "input"
    proc_dir = tmp_path / "process"
    out_dir = tmp_path / "output"
    inp_dir.mkdir()
    proc_dir.mkdir()
    out_dir.mkdir()

    csv_file = inp_dir / "dialogues.csv"
    csv_file.write_text("id,text\n1,Good morning\n", encoding='utf-8')

    trans = PrefixTranslate(lang_source='en', lang_target='pt')
    extractor = CSVExtractor(trans)
    extractor.folderInput = str(inp_dir)
    extractor.folderProcess = str(proc_dir)
    extractor.folderOutput = str(out_dir)

    extractor.process_file(str(csv_file))
    extractor.import_files()

    output_file = out_dir / "dialogues.csv"
    assert output_file.exists()
    content = output_file.read_text(encoding='utf-8')

    assert "[PT] Good morning" in content
    assert "text_old" not in content
    assert "_old" not in content


def test_csv_apply_configuration_logging():
    """Testa que apply_configuration configura corretamente e não quebra com logging."""
    trans = IdentityTranslate(lang_source='en', lang_target='pt')
    extractor = CSVExtractor(trans)

    extractor.apply_configuration({
        'columns': 'custom_val,custom_desc',
        'delimiter': ';'
    })

    assert extractor.columns_to_translate == ['custom_val', 'custom_desc']
    assert extractor.csv_delimiter == ';'


# ==============================================================================
# TESTES DA ISSUE #9 — JsonExtractor
# ==============================================================================

def test_json_preserves_non_translatable_keys_and_translates_target(tmp_path):
    """
    Testa que {'face': 'Actor1', 'text': 'Hello'} mantém 'face': 'Actor1' inalterado
    e traduz 'text'.
    """
    inp_dir = tmp_path / "input"
    proc_dir = tmp_path / "process"
    out_dir = tmp_path / "output"
    inp_dir.mkdir()
    proc_dir.mkdir()
    out_dir.mkdir()

    data = {"face": "Actor1", "text": "Hello"}
    json_file = inp_dir / "actor_msg.json"
    json_file.write_text(json.dumps(data), encoding='utf-8')

    trans = PrefixTranslate(lang_source='en', lang_target='pt')
    extractor = JsonExtractor(trans)
    extractor.folderInput = str(inp_dir)
    extractor.folderProcess = str(proc_dir)
    extractor.folderOutput = str(out_dir)

    extractor.process_file(str(json_file))
    extractor.import_files()

    output_file = out_dir / "actor_msg.json"
    assert output_file.exists()

    result = json.loads(output_file.read_text(encoding='utf-8'))
    assert result["face"] == "Actor1"
    assert result["text"] == "[PT] Hello"
    assert "text_old" not in result
    assert "_old" not in str(result)


def test_json_nested_structure_selective_translation(tmp_path):
    """
    Testa estrutura aninhada com dicts e listas contendo IDs, assets, flags técnicas,
    e campos de texto permitidos. Garante que 100% dos campos técnicos e posições
    sejam preservados e apenas as chaves da allow-list sejam traduzidas.
    """
    inp_dir = tmp_path / "input"
    proc_dir = tmp_path / "process"
    out_dir = tmp_path / "output"
    inp_dir.mkdir()
    proc_dir.mkdir()
    out_dir.mkdir()

    data = {
        "id": 101,
        "face": "Actor1",
        "image": "img/characters/hero.png",
        "audio": "se/attack.ogg",
        "stats": [10, 20, 30],
        "actors": [
            {
                "id": 1,
                "faceName": "Actor1_2",
                "name": "Harold",
                "description": "A brave warrior",
                "sprite": "hero.png"
            },
            {
                "id": 2,
                "faceName": "Actor2_1",
                "name": "Therese",
                "description": "A wise mage",
                "sprite": "mage.png"
            }
        ],
        "dialogue": [
            "Welcome to the kingdom!",
            "Beware of monsters."
        ],
        "technical_flag": "$gameVariables.value(1)",
        "version": "1.0.0"
    }

    json_file = inp_dir / "complex.json"
    json_file.write_text(json.dumps(data, indent=2), encoding='utf-8')

    trans = PrefixTranslate(lang_source='en', lang_target='pt')
    extractor = JsonExtractor(trans)
    extractor.folderInput = str(inp_dir)
    extractor.folderProcess = str(proc_dir)
    extractor.folderOutput = str(out_dir)

    extractor.process_file(str(json_file))
    extractor.import_files()

    output_file = out_dir / "complex.json"
    assert output_file.exists()

    result = json.loads(output_file.read_text(encoding='utf-8'))

    # Campos técnicos mantidos 100% intactos
    assert result["id"] == 101
    assert result["face"] == "Actor1"
    assert result["image"] == "img/characters/hero.png"
    assert result["audio"] == "se/attack.ogg"
    assert result["stats"] == [10, 20, 30]
    assert result["technical_flag"] == "$gameVariables.value(1)"
    assert result["version"] == "1.0.0"

    # Atores dentro da lista
    assert result["actors"][0]["id"] == 1
    assert result["actors"][0]["faceName"] == "Actor1_2"
    assert result["actors"][0]["sprite"] == "hero.png"
    assert result["actors"][0]["name"] == "[PT] Harold"
    assert result["actors"][0]["description"] == "[PT] A brave warrior"

    assert result["actors"][1]["id"] == 2
    assert result["actors"][1]["faceName"] == "Actor2_1"
    assert result["actors"][1]["sprite"] == "mage.png"
    assert result["actors"][1]["name"] == "[PT] Therese"
    assert result["actors"][1]["description"] == "[PT] A wise mage"

    # Lista de diálogos traduzida
    assert result["dialogue"] == [
        "[PT] Welcome to the kingdom!",
        "[PT] Beware of monsters."
    ]

    # Nenhuma chave _old residual
    assert "_old" not in json.dumps(result)


def test_json_extractor_configurable_allowlist():
    """Testa a configuração interativa de chaves em JsonExtractor."""
    trans = IdentityTranslate(lang_source='en', lang_target='pt')
    extractor = JsonExtractor(trans)

    # 1. Configuração de chaves específicas
    extractor.apply_configuration({'keys': 'custom_tag,title'})
    assert extractor.translatable_keys == {'custom_tag', 'title'}

    sample_data = {
        "custom_tag": "Special Event",
        "text": "Ignored text",
        "title": "King of Kings",
        "desc": "Ignored desc"
    }
    extracted = extractor.extract_text("test.json", sample_data)
    assert extracted == {
        "custom_tag": "Special Event",
        "title": "King of Kings"
    }

    # 2. Configuração com wildcard '*'
    extractor.apply_configuration({'keys': '*'})
    assert extractor.translatable_keys == {'*'}
    extracted_all = extractor.extract_text("test.json", sample_data)
    assert extracted_all == sample_data


def test_json_no_translatable_text_bypasses_translation(tmp_path):
    """
    Testa que um JSON sem nenhuma chave ou string traduzível é copiado diretamente
    sem acionar tradução e mantendo o conteúdo original idêntico.
    """
    inp_dir = tmp_path / "input"
    proc_dir = tmp_path / "process"
    out_dir = tmp_path / "output"
    inp_dir.mkdir()
    proc_dir.mkdir()
    out_dir.mkdir()

    data = {
        "id": 999,
        "texture": "system/window.png",
        "bgm": "theme.ogg",
        "code": 404,
        "flag": True
    }
    raw_json = json.dumps(data)
    json_file = inp_dir / "system.json"
    json_file.write_text(raw_json, encoding='utf-8')

    trans = PrefixTranslate(lang_source='en', lang_target='pt')
    extractor = JsonExtractor(trans)
    extractor.folderInput = str(inp_dir)
    extractor.folderProcess = str(proc_dir)
    extractor.folderOutput = str(out_dir)

    # extract_text deve retornar None
    assert extractor.extract_text("system.json", data) is None

    extractor.process_file(str(json_file))
    extractor.import_files()

    output_file = out_dir / "system.json"
    assert output_file.exists()
    assert json.loads(output_file.read_text(encoding='utf-8')) == data
