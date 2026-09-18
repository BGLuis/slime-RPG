import csv
import os
import pytest
from unittest.mock import MagicMock

from src.utils.LanguageCodes import (
    normalize_language_code,
    get_english_name,
    identify_language_from_filename,
    generate_target_filename,
    is_language_column
)
from src.services.GameDetector import detect_game_environment
from src.extractor.CSVExtractor import CSVExtractor, detect_delimiter_from_file

# ==============================================================================
# 1. TESTES DE UTILS: LanguageCodes
# ==============================================================================

def test_normalize_language_code():
    assert normalize_language_code('en') == 'en'
    assert normalize_language_code('EN') == 'en'
    assert normalize_language_code('english') == 'en'
    assert normalize_language_code('Inglês') == 'en'
    assert normalize_language_code('pt') == 'pt'
    assert normalize_language_code('pt-br') == 'pt'
    assert normalize_language_code('pt_BR') == 'pt'
    assert normalize_language_code('Portuguese') == 'pt'
    assert normalize_language_code('Português') == 'pt'
    assert normalize_language_code('es') == 'es'
    assert normalize_language_code('Spanish') == 'es'
    assert normalize_language_code('Español') == 'es'
    assert normalize_language_code('ja') == 'ja'
    assert normalize_language_code('Japanese') == 'ja'
    assert normalize_language_code('desconhecido_xyz') is None

def test_identify_language_from_filename():
    assert identify_language_from_filename('en.csv') == ('en', 'code_exact')
    assert identify_language_from_filename('EN.csv') == ('en', 'code_exact')
    assert identify_language_from_filename('English.csv') == ('en', 'name_exact')
    assert identify_language_from_filename('portuguese.csv') == ('pt', 'name_exact')
    assert identify_language_from_filename('strings_en.csv') == ('en', 'suffix')
    assert identify_language_from_filename('dialogue_pt_br.csv') == ('pt', 'suffix_composite')
    assert identify_language_from_filename('es_subtitles.csv') == ('es', 'prefix')
    assert identify_language_from_filename('items.csv') is None
    assert identify_language_from_filename('data_01.csv') is None

def test_generate_target_filename():
    assert generate_target_filename('en.csv', 'pt') == 'pt.csv'
    assert generate_target_filename('EN.csv', 'pt') == 'PT.csv'
    assert generate_target_filename('English.csv', 'pt') == 'Portuguese.csv'
    assert generate_target_filename('english.csv', 'pt') == 'portuguese.csv'
    assert generate_target_filename('strings_en.csv', 'pt') == 'strings_pt.csv'
    assert generate_target_filename('loc_en.csv', 'es') == 'loc_es.csv'
    assert generate_target_filename('items.csv', 'pt') == 'items_pt.csv'

def test_is_language_column():
    assert is_language_column('en') == 'en'
    assert is_language_column('EN') == 'en'
    assert is_language_column('English') == 'en'
    assert is_language_column('pt') == 'pt'
    assert is_language_column('pt-BR') == 'pt'
    assert is_language_column('Português') == 'pt'
    assert is_language_column('id') is None
    assert is_language_column('key') is None
    assert is_language_column('value') is None

# ==============================================================================
# 2. TESTES DE DETECÇÃO: GameDetector com Pastas de Localização
# ==============================================================================

def test_detect_game_with_languages_csv_folder(tmp_path):
    """Testa detecção quando o jogo possui subpasta languages/ com CSVs nomeados por idioma."""
    lang_dir = tmp_path / "languages"
    lang_dir.mkdir()
    (lang_dir / "en.csv").write_text("id,text\n1,Hello", encoding="utf-8")
    (lang_dir / "es.csv").write_text("id,text\n1,Hola", encoding="utf-8")

    # Mesmo que exista uma pasta data/ de RPG Maker no mesmo jogo, deve priorizar languages/
    data_dir = tmp_path / "data"
    data_dir.mkdir()
    (data_dir / "System.json").write_text("{}", encoding="utf-8")

    det = detect_game_environment(str(tmp_path))
    assert det is not None
    assert det['detected_path'] == str(lang_dir)
    assert det['extractor'] == 'CSV'
    assert 'Sistema de Localização por CSV' in det['description']
    assert det['file_count'] == 2

def test_detect_game_with_cte_modular_folder(tmp_path):
    """Testa detecção para jogos com sistema modular CTE (como Liora's Price of Dignity)."""
    lang_dir = tmp_path / "languages"
    lang_dir.mkdir()
    (lang_dir / "Languages.json").write_text('{"codes": {"EN": 1, "ES": 0.5}}', encoding="utf-8")
    (lang_dir / "EN").mkdir()
    (lang_dir / "EN" / "Map001.json").write_text('{}', encoding="utf-8")

    # Cria também data/ para garantir que não confunda
    data_dir = tmp_path / "data"
    data_dir.mkdir()
    (data_dir / "System.json").write_text("{}", encoding="utf-8")

    det = detect_game_environment(str(tmp_path))
    assert det is not None
    assert det['detected_path'] == str(lang_dir)
    assert det['extractor'] == 'Json'
    assert 'CTE' in det['description'] or 'Modular' in det['description']

def test_detect_game_with_csv_in_current_folder(tmp_path):
    """Testa detecção quando o usuário roda diretamente dentro da pasta contendo CSVs de idioma."""
    (tmp_path / "en.csv").write_text("key,val\nmsg,hi", encoding="utf-8")
    (tmp_path / "pt.csv").write_text("key,val\nmsg,oi", encoding="utf-8")

    det = detect_game_environment(str(tmp_path))
    assert det is not None
    assert det['detected_path'] == str(tmp_path)
    assert det['extractor'] == 'CSV'
    assert 'Arquivos CSV de Idioma' in det['description']

# ==============================================================================
# 3. TESTES DO CSVExtractor: Delimitadores, Arquivo-por-Idioma e Tabela Multilíngue
# ==============================================================================

def test_detect_delimiter(tmp_path):
    comma_file = tmp_path / "comma.csv"
    comma_file.write_text("id,text,note\n1,hello,world", encoding="utf-8")
    assert detect_delimiter_from_file(str(comma_file)) == ','

    semi_file = tmp_path / "semi.csv"
    semi_file.write_text("id;text;note\n1;hello;world", encoding="utf-8")
    assert detect_delimiter_from_file(str(semi_file)) == ';'

    tab_file = tmp_path / "tab.csv"
    tab_file.write_text("id\ttext\tnote\n1\thello\tworld", encoding="utf-8")
    assert detect_delimiter_from_file(str(tab_file)) == '\t'

def test_csv_extractor_file_per_language_workflow(tmp_path):
    """
    Testa o fluxo em que en.csv é a entrada e deve gerar pt.csv sem modificar en.csv.
    """
    mock_translate = MagicMock()
    mock_translate.lang_source = 'en'
    mock_translate.lang_target = 'pt'

    extractor = CSVExtractor(mock_translate)

    # Cria en.csv com ponto e vírgula para testar preservação de delimitador
    en_file = tmp_path / "en.csv"
    en_file.write_text("id;text\n1;Sword of Fire\n2;Shield of Ice\n", encoding="utf-8-sig")

    _, data = extractor.extract_files(str(en_file))
    assert data is not None
    assert len(data) == 2

    extracted = extractor.extract_text("en.csv", data)
    assert extracted == {
        "row_0_col_text": "Sword of Fire",
        "row_1_col_text": "Shield of Ice"
    }

    # Simula a tradução vinda da pipeline
    translated_texts = {
        "row_0_col_text": "Espada de Fogo",
        "row_1_col_text": "Escudo de Gelo"
    }

    updated = extractor.update_json("en.csv", data, translated_texts)
    assert updated[0]["text"] == "Espada de Fogo"
    assert updated[1]["text"] == "Escudo de Gelo"
    # Como é file-per-language, não deve criar 'text_old' poluindo o arquivo
    assert "text_old" not in updated[0]

    # Salva usando import_file
    output_folder = tmp_path / "output"
    extractor.import_file("en.csv", updated, str(output_folder))

    # Verifica que foi salvo como pt.csv!
    assert (output_folder / "pt.csv").exists()
    assert not (output_folder / "en.csv").exists()

    # Verifica conteúdo e delimitador do arquivo gerado
    content = (output_folder / "pt.csv").read_text(encoding="utf-8-sig")
    assert "Espada de Fogo" in content
    assert ";" in content # Preservou o delimitador original!

def test_csv_extractor_multilanguage_columns(tmp_path):
    """
    Testa tabela multilíngue com colunas key, en, es (e adição automática da coluna pt).
    """
    mock_translate = MagicMock()
    mock_translate.lang_source = 'en'
    mock_translate.lang_target = 'pt'

    extractor = CSVExtractor(mock_translate)

    loc_file = tmp_path / "localization.csv"
    loc_file.write_text("key,en,es\nHELLO,Hello,Hola\nBYE,Goodbye,Adios\n", encoding="utf-8-sig")

    _, data = extractor.extract_files(str(loc_file))
    extracted = extractor.extract_text("localization.csv", data)

    # Deve extrair da coluna 'en'
    assert extracted == {
        "row_0_col_en": "Hello",
        "row_1_col_en": "Goodbye"
    }

    translated = {
        "row_0_col_en": "Olá",
        "row_1_col_en": "Adeus"
    }

    updated = extractor.update_json("localization.csv", data, translated)
    # Coluna original 'en' deve ser preservada intacta!
    assert updated[0]["en"] == "Hello"
    # Nova coluna 'pt' deve ser criada e preenchida com a tradução
    assert updated[0]["pt"] == "Olá"
    assert updated[1]["pt"] == "Adeus"

    output_folder = tmp_path / "output"
    extractor.import_file("localization.csv", updated, str(output_folder))

    out_file = output_folder / "localization.csv"
    assert out_file.exists()
    out_content = out_file.read_text(encoding="utf-8-sig")
    assert "key,en,es,pt" in out_content
    assert "HELLO,Hello,Hola,Olá" in out_content

def test_csv_extractor_is_translatable_filtering():
    """Testa que arquivos de outros idiomas não são traduzidos quando source é en."""
    mock_translate = MagicMock()
    mock_translate.lang_source = 'en'
    mock_translate.lang_target = 'pt'

    extractor = CSVExtractor(mock_translate)
    assert extractor.is_translatable_file("en.csv") is True
    assert extractor.is_translatable_file("English.csv") is True
    assert extractor.is_translatable_file("strings_en.csv") is True
    assert extractor.is_translatable_file("dialogue.csv") is True # Arquivo genérico sem idioma

    # Arquivos de outros idiomas devem ser ignorados
    assert extractor.is_translatable_file("es.csv") is False
    assert extractor.is_translatable_file("fr.csv") is False
    assert extractor.is_translatable_file("Spanish.csv") is False

def test_csv_mask_patterns():
    """Testa que códigos como \\c[2], \\item<123>, {player_name} e %s são capturados pelas máscaras."""
    extractor = CSVExtractor(MagicMock())
    patterns = extractor.get_mask_patterns()

    sample = "Hello \\c[2]hero\\c[0], you found \\item<42>! Name: {hero_name}, score: %d."
    matched_tokens = []
    for p in patterns:
        for m in p.finditer(sample):
            matched_tokens.append(m.group(0))

    assert "\\c[2]" in matched_tokens
    assert "\\c[0]" in matched_tokens
    assert "\\item<42>" in matched_tokens
    assert "{hero_name}" in matched_tokens
    assert "%d" in matched_tokens


def test_json_mask_patterns_js_code():
    """Testa que expressões JS completas, ${...}, arrow functions e $gameVariables.value(...) são mascarados."""
    from src.extractor.JsonExtractor import JsonExtractor
    extractor = JsonExtractor(MagicMock())
    patterns = extractor.get_mask_patterns()

    # 1. $gameVariables.value(207)
    sample_gv = "$gameVariables.value(207)"
    matched_gv = [m.group(0) for p in patterns for m in p.finditer(sample_gv)]
    assert "$gameVariables.value(207)" in matched_gv

    # 2. Template interpolation ${...}
    sample_tpl = "`${actor.name()} Use ${skill.name}. (-${actor.actorSkillCost(skill).value} HP)`"
    matched_tpl = [m.group(0) for p in patterns for m in p.finditer(sample_tpl)]
    assert "${actor.name()}" in matched_tpl
    assert "${skill.name}" in matched_tpl
    assert "${actor.actorSkillCost(skill).value}" in matched_tpl

    # 3. Arrow function
    sample_func = "(word, num) => word + ((num === 1 || !word.slice(-1).match(/[a-z]/i)) ? \"\" : \"s\");"
    matched_func = [m.group(0) for p in patterns for m in p.finditer(sample_func)]
    assert sample_func in matched_func

