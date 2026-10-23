import os
import sys
import json
import logging
from collections import OrderedDict

# Garante acesso aos módulos de src/
sys.path.insert(0, os.path.abspath(os.path.join(os.path.dirname(__file__), '..')))

from src.services.ProxyManager import ProxyManager
from src.translate.GoogleTranslate import GoogleTranslate
from src.extractor.JsonExtractor import JsonExtractor
from src.utils.TextsUtils import dictToList

def is_empty_or_placeholder(val):
    if not isinstance(val, str):
        return False
    s = val.strip()
    return not s or s in ('x', '--')

def has_translatable_text(obj):
    if isinstance(obj, str):
        return bool(obj.strip())
    elif isinstance(obj, dict):
        return any(has_translatable_text(v) for v in obj.values())
    elif isinstance(obj, list):
        return any(has_translatable_text(v) for v in obj)
    return False

def translate_file(file_name, src_path, dst_path, extractor, translator):
    if not os.path.exists(src_path):
        return False

    with open(src_path, 'r', encoding='utf-8') as f:
        src_data = json.load(f, object_pairs_hook=OrderedDict)

    if not has_translatable_text(src_data):
        with open(dst_path, 'w', encoding='utf-8') as f:
            json.dump(src_data, f, ensure_ascii=False, indent=4)
        return True

    # Se destino já possui tradução preenchida e não vazia
    if os.path.exists(dst_path) and os.path.getsize(dst_path) > 10:
        try:
            with open(dst_path, 'r', encoding='utf-8') as f:
                dst_data = json.load(f)
            if has_translatable_text(dst_data):
                print(f"[{file_name}] Já traduzido previamente.")
                return True
        except Exception:
            pass

    raw_text = extractor.extract_text(file_name, src_data)
    if not raw_text:
        with open(dst_path, 'w', encoding='utf-8') as f:
            json.dump(src_data, f, ensure_ascii=False, indent=4)
        return True

    try:
        translated_text = extractor._pipeline_translate(file_name, src_data, raw_text, translator, file_name)
        merged_data = extractor.merge_dicts_texts(translated_text, raw_text)
        updated_data = extractor.update_json(file_name, src_data, merged_data)

        # Preserva encryptionKey em System.json
        if file_name == 'System.json' and isinstance(updated_data, dict):
            updated_data['encryptionKey'] = src_data.get('encryptionKey', 'a4ce5a98a8950c04a3d34a2e2cb8c89f')

        os.makedirs(os.path.dirname(dst_path), exist_ok=True)
        with open(dst_path, 'w', encoding='utf-8') as f:
            json.dump(updated_data, f, ensure_ascii=False, indent=4)

        return True
    except Exception as e:
        print(f"Erro ao traduzir {file_name}: {e}")
        return False

def main():
    game_dir = "/home/luis/Downloads/star-knightess-aura-linux"
    src_folder = os.path.join(game_dir, "languages", "EN")
    dst_folder = os.path.join(game_dir, "languages", "PT")

    print("Inicializando ProxyManager e GoogleTranslate...")
    pm = ProxyManager.get_instance()
    pm.set_enabled(True)
    pm.set_direct_blocked(True)

    g = GoogleTranslate(lang_source='en', lang_target='pt')
    extractor = JsonExtractor(g)

    # Prioridade inicial: mapas do início do jogo e cidades
    early_maps = [f"Map{i:03d}.json" for i in range(7, 31)]
    
    # Todos os arquivos JSON da pasta EN
    all_json_files = sorted([f for f in os.listdir(src_folder) if f.endswith('.json')])
    
    # Ordem: prioritários da base já feitos, depois mapas iniciais, depois o restante
    priority_files = [
        "System.json", "TemplateMessages.json", "Actors.json", "Classes.json",
        "Costumes.json", "Armors.json", "Weapons.json", "States.json",
        "Enemies.json", "Speakers.json", "Variables.json", "Quests.json",
        "Items.json", "Skills.json", "Compendium.json", "AllPlugins.json",
        "Map001.json", "Map004.json", "Map005.json", "Map006.json"
    ] + early_maps
    
    queue = []
    seen = set()
    for f in priority_files + all_json_files:
        if f not in seen and os.path.exists(os.path.join(src_folder, f)):
            seen.add(f)
            queue.append(f)

    total_files = len(queue)
    print(f"Iniciando tradução da fila de arquivos ({total_files} arquivos)...")

    for i, file_name in enumerate(queue, 1):
        src_path = os.path.join(src_folder, file_name)
        dst_path = os.path.join(dst_folder, file_name)

        print(f"[{i}/{total_files}] Processando {file_name}...")
        success = translate_file(file_name, src_path, dst_path, extractor, g)
        if success:
            print(f"  ✓ {file_name} traduzido com sucesso!")
        else:
            print(f"  ✗ {file_name} falhou.")

    print("\nTradução dos arquivos concluída!")

if __name__ == '__main__':
    main()
