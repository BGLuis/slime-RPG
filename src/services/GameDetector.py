import os
import glob
from typing import Optional, Dict, Any
from src.utils.LanguageCodes import (
    identify_language_from_filename,
    normalize_language_code,
    is_language_column
)

def is_trash_path(path: str) -> bool:
    """Verifica se um caminho pertence a uma lixeira do sistema operacional."""
    if not path:
        return False
    normalized = os.path.abspath(path)
    trash_markers = ['.trash', '/.trash-', '/.local/share/trash', '/trash/']
    return any(marker in normalized.lower() for marker in trash_markers)

def is_project_repo_root(path: str) -> bool:
    """Verifica se o caminho é a raiz do repositório desta ferramenta."""
    if not path or not os.path.isdir(path):
        return False
    # Checa se possui arquivos característicos do projeto
    has_src = os.path.isdir(os.path.join(path, 'src'))
    has_main = os.path.isfile(os.path.join(path, 'main.py'))
    has_scripts = os.path.isdir(os.path.join(path, 'scripts'))
    return has_src and has_main and has_scripts

def detect_game_environment(candidate_path: Optional[str] = None) -> Optional[Dict[str, Any]]:
    """
    Detecta automaticamente se uma pasta (ou suas subpastas imediatas como www/data ou data)
    é elegível para tradução e qual Extrator deve ser utilizado.
    """
    target = candidate_path or os.environ.get('CALLER_WORKING_DIR') or os.getcwd()
    if not target or not os.path.isdir(target):
        return None

    target = os.path.abspath(target)

    # Não sugerir se for lixeira ou a própria raiz do código-fonte
    if is_trash_path(target) or is_project_repo_root(target):
        return None

    # 0. Checagem de pastas dedicadas de localização/idiomas (ex: languages, language, lang, localization, locales)
    # PRIORITÁRIO: Jogos com sistema de localização dedicado não devem ter seus dados brutos de motor (ex: data/ ou www/data/) alterados diretamente.
    loc_folder_names = ['languages', 'language', 'lang', 'locales', 'localization', 'translations', 'i18n']
    for loc_name in loc_folder_names:
        loc_candidate = os.path.join(target, loc_name)
        if os.path.isdir(loc_candidate):
            try:
                loc_entries = os.listdir(loc_candidate)
            except Exception:
                loc_entries = []

            # A. Verifica se possui arquivos CSV de idioma (ex: en.csv, pt.csv, English.csv) ou tabelas multilíngues
            csv_files = [f for f in loc_entries if f.lower().endswith('.csv')]
            lang_csv_files = [f for f in csv_files if identify_language_from_filename(f) is not None]
            if len(lang_csv_files) > 0 or len(csv_files) > 0:
                return {
                    'detected_path': loc_candidate,
                    'extractor': 'CSV',
                    'description': f'Sistema de Localização por CSV (subpasta {loc_name})',
                    'file_count': len(csv_files)
                }

            # B. Verifica se possui subpastas de idioma com JSONs (ex: languages/EN/, languages/ES/) ou Languages.json (CustomTranslationEngine)
            subdirs = [d for d in loc_entries if os.path.isdir(os.path.join(loc_candidate, d))]
            lang_subdirs = [d for d in subdirs if normalize_language_code(d) is not None]
            has_lang_json = 'Languages.json' in loc_entries or 'languages.json' in loc_entries
            if len(lang_subdirs) > 0 or has_lang_json:
                total_json_files = glob.glob(os.path.join(loc_candidate, '**', '*.json'), recursive=True)
                return {
                    'detected_path': loc_candidate,
                    'extractor': 'Json',
                    'description': f'Sistema de Localização Modular / CTE (subpasta {loc_name})',
                    'file_count': len(total_json_files)
                }

    # 1. Checagem de subpastas comuns de RPG Maker (ex: www/data ou data)
    www_data = os.path.join(target, 'www', 'data')
    if os.path.isdir(www_data):
        rpg_files = glob.glob(os.path.join(www_data, '*.json'))
        if any(os.path.basename(f) in ('System.json', 'Actors.json') or os.path.basename(f).startswith('Map') for f in rpg_files):
            return {
                'detected_path': www_data,
                'extractor': 'RPG Maker',
                'description': 'RPG Maker MV/MZ (subpasta www/data)',
                'file_count': len(rpg_files)
            }

    data_dir = os.path.join(target, 'data')
    if os.path.isdir(data_dir):
        rpg_files = glob.glob(os.path.join(data_dir, '*.json'))
        if any(os.path.basename(f) in ('System.json', 'Actors.json') or os.path.basename(f).startswith('Map') for f in rpg_files):
            return {
                'detected_path': data_dir,
                'extractor': 'RPG Maker',
                'description': 'RPG Maker (subpasta data)',
                'file_count': len(rpg_files)
            }

    # 2. Checagem de pastas e arquivos WOLF RPG Editor (Data.wolf, Data/BasicData, Data/MapData)
    wolf_data_dir = os.path.join(target, 'Data')
    if os.path.isdir(wolf_data_dir):
        common_dat = os.path.join(wolf_data_dir, 'BasicData', 'CommonEvent.dat')
        map_files = glob.glob(os.path.join(wolf_data_dir, 'MapData', '*.mps'))
        wolf_archives = glob.glob(os.path.join(wolf_data_dir, '*.wolf'))
        if os.path.isfile(common_dat) or len(map_files) > 0 or len(wolf_archives) > 0:
            total_files = (1 if os.path.isfile(common_dat) else 0) + len(map_files) + len(wolf_archives)
            return {
                'detected_path': wolf_data_dir,
                'extractor': 'Wolf RPG',
                'description': 'WOLF RPG Editor (subpasta Data)',
                'file_count': total_files
            }

    # Checagem de arquivos .wolf na raiz
    root_wolf_files = glob.glob(os.path.join(target, '*.wolf'))
    if len(root_wolf_files) > 0:
        return {
            'detected_path': target,
            'extractor': 'Wolf RPG',
            'description': 'WOLF RPG Editor (arquivos .wolf)',
            'file_count': len(root_wolf_files)
        }

    # 3. Checagem no próprio diretório atual
    try:
        entries = os.listdir(target)
    except Exception:
        return None

    json_files = [f for f in entries if f.lower().endswith('.json')]
    csv_files = [f for f in entries if f.lower().endswith('.csv')]
    wolf_entries = [f for f in entries if f.lower().endswith(('.mps', '.wolf')) or f in ('CommonEvent.dat', 'Game.dat')]

    # Wolf RPG no diretório atual
    if len(wolf_entries) > 0:
        return {
            'detected_path': target,
            'extractor': 'Wolf RPG',
            'description': 'WOLF RPG Editor (pasta atual)',
            'file_count': len(wolf_entries)
        }

    # RPG Maker no diretório atual
    if any(f in ('System.json', 'Actors.json', 'CommonEvents.json') or f.startswith('Map') for f in json_files):
        return {
            'detected_path': target,
            'extractor': 'RPG Maker',
            'description': 'RPG Maker (pasta atual)',
            'file_count': len(json_files)
        }

    # CSV de idioma na pasta atual
    lang_csv_files = [f for f in csv_files if identify_language_from_filename(f) is not None]
    if len(lang_csv_files) > 0:
        return {
            'detected_path': target,
            'extractor': 'CSV',
            'description': 'Arquivos CSV de Idioma (pasta atual)',
            'file_count': len(csv_files)
        }

    # CTE / Subpastas de idioma na pasta atual
    subdirs = [d for d in entries if os.path.isdir(os.path.join(target, d))]
    lang_subdirs = [d for d in subdirs if normalize_language_code(d) is not None]
    has_lang_json = 'Languages.json' in entries or 'languages.json' in entries
    if len(lang_subdirs) > 0 or has_lang_json:
        total_json_files = glob.glob(os.path.join(target, '**', '*.json'), recursive=True)
        return {
            'detected_path': target,
            'extractor': 'Json',
            'description': 'Sistema de Localização Modular / CTE (pasta atual)',
            'file_count': len(total_json_files)
        }

    # JSON genérico
    if len(json_files) > 0:
        return {
            'detected_path': target,
            'extractor': 'Json',
            'description': 'Arquivos JSON',
            'file_count': len(json_files)
        }

    # CSV genérico
    if len(csv_files) > 0:
        return {
            'detected_path': target,
            'extractor': 'CSV',
            'description': 'Arquivos CSV',
            'file_count': len(csv_files)
        }

    return None

