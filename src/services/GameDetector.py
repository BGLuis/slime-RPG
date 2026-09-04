import os
import glob
from typing import Optional, Dict, Any

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
