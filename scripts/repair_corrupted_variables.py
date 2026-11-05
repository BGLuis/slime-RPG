#!/usr/bin/env python3
"""
Script de reparo para restaurar variáveis técnicas (código 122) indevidamente traduzidas
em jogos RPG Maker MV/MZ, preservando diálogos e textos legítimos traduzidos.
"""

import os
import sys
import glob
import json
import shutil

sys.path.insert(0, os.path.abspath(os.path.join(os.path.dirname(__file__), '..')))
from src.extractor.rpgmaker.RPGEventStrategy import ControlVariablesStrategy


def repair_file(orig_path, trans_path, strategy):
    if not os.path.exists(orig_path) or not os.path.exists(trans_path):
        return 0, 0

    with open(orig_path, 'r', encoding='utf-8') as f1, open(trans_path, 'r', encoding='utf-8') as f2:
        try:
            d_orig = json.load(f1)
            d_trans = json.load(f2)
        except Exception as e:
            print(f"Erro ao ler JSON: {e}")
            return 0, 0

    restored = 0
    kept = 0

    def process_command_list(orig_list, trans_list):
        nonlocal restored, kept
        for c1, c2 in zip(orig_list, trans_list):
            if not isinstance(c1, dict) or not isinstance(c2, dict):
                continue
            if c1.get('code') == 122 and c2.get('code') == 122:
                p1 = c1.get('parameters', [])
                p2 = c2.get('parameters', [])
                if len(p1) >= 5 and len(p2) >= 5 and p1[3] == 4 and p2[3] == 4:
                    # Se o original não deveria ter sido extraído (é código/asset/identificador)
                    if strategy.extract(c1) is None:
                        if p1[4] != p2[4]:
                            p2[4] = p1[4]
                            restored += 1
                    else:
                        if p1[4] != p2[4]:
                            kept += 1

    # Formato CommonEvents.json: lista de eventos
    if isinstance(d_orig, list) and isinstance(d_trans, list):
        for e1, e2 in zip(d_orig, d_trans):
            if isinstance(e1, dict) and isinstance(e2, dict) and 'list' in e1 and 'list' in e2:
                process_command_list(e1['list'], e2['list'])

    # Formato MapXXX.json ou Troop.json: dict com chave 'events'
    elif isinstance(d_orig, dict) and isinstance(d_trans, dict):
        evs1 = d_orig.get('events', [])
        evs2 = d_trans.get('events', [])
        if isinstance(evs1, list) and isinstance(evs2, list):
            for e1, e2 in zip(evs1, evs2):
                if isinstance(e1, dict) and isinstance(e2, dict):
                    pages1 = e1.get('pages', [])
                    pages2 = e2.get('pages', [])
                    if isinstance(pages1, list) and isinstance(pages2, list):
                        for p1, p2 in zip(pages1, pages2):
                            if isinstance(p1, dict) and isinstance(p2, dict) and 'list' in p1 and 'list' in p2:
                                process_command_list(p1['list'], p2['list'])

    if restored > 0:
        # Fazer backup antes de sobrescrever
        bak_path = trans_path + '.bak'
        if not os.path.exists(bak_path):
            shutil.copy2(trans_path, bak_path)
        with open(trans_path, 'w', encoding='utf-8') as f:
            json.dump(d_trans, f, ensure_ascii=False, separators=(',', ':'))

    return restored, kept


def main():
    if len(sys.argv) < 3:
        print("Uso: python repair_corrupted_variables.py <dir_original_data> <dir_traduzido_data>")
        sys.exit(1)

    orig_dir = sys.argv[1]
    trans_dir = sys.argv[2]

    strategy = ControlVariablesStrategy()

    total_restored = 0
    total_kept = 0

    for json_file in glob.glob(os.path.join(orig_dir, '*.json')):
        fname = os.path.basename(json_file)
        target = os.path.join(trans_dir, fname)
        if os.path.exists(target):
            rest, kept = repair_file(json_file, target, strategy)
            if rest > 0:
                print(f"[{fname}] Restauradas {rest} variáveis técnicas (mantidos {kept} diálogos traduzidos)")
                total_restored += rest
                total_kept += kept

    print(f"\nConcluído! Total restaurado: {total_restored} variáveis técnicas corrigidas. Diálogos preservados: {total_kept}.")


if __name__ == '__main__':
    main()
