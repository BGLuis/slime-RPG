#!/usr/bin/env python3
"""
Varredura e normalização de tags de moldura de nome de personagens
(\\nomE, \\namE, \\nome, \\name) para \\NAME[...] nos arquivos .rvdata2
do RPG Maker VX Ace e no banco de dados de memória de tradução cache/memory.db.
"""
import argparse
import glob
import os
import re
import sqlite3
import sys

# Adiciona a raiz do projeto ao sys.path para importar módulos internos
sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

import src.utils.RubyMarshal as r_marshal
from rubymarshal.classes import RubyObject, RubyString

TAG_PATTERN = re.compile(r"\\(?:nam[eE]|nom[eE]|name|nome)\s*\[")
TARGET_REPLACEMENT = lambda m: r"\NAME["

SKIP_FILES = {"Scripts.rvdata2", "Animations.rvdata2", "Tilesets.rvdata2"}


def normalize_tags_in_obj(obj, pattern=TAG_PATTERN, replacement=TARGET_REPLACEMENT, visited=None):
    """
    Percorre recursivamente a árvore de objetos do Ruby Marshal e
    substitui ocorrências do padrão em RubyString e str.
    """
    if visited is None:
        visited = set()
    obj_id = id(obj)
    if obj_id in visited:
        return 0
    visited.add(obj_id)

    mod_count = 0

    if isinstance(obj, RubyString):
        new_text, n = pattern.subn(replacement, obj.text)
        if n > 0:
            obj.text = new_text
            mod_count += n
        return mod_count

    if isinstance(obj, list):
        for idx, item in enumerate(obj):
            if isinstance(item, RubyString):
                new_text, n = pattern.subn(replacement, item.text)
                if n > 0:
                    item.text = new_text
                    mod_count += n
            elif isinstance(item, str):
                new_text, n = pattern.subn(replacement, item)
                if n > 0:
                    obj[idx] = new_text
                    mod_count += n
            else:
                mod_count += normalize_tags_in_obj(item, pattern, replacement, visited)
        return mod_count

    if isinstance(obj, dict):
        for k, v in list(obj.items()):
            if isinstance(v, RubyString):
                new_text, n = pattern.subn(replacement, v.text)
                if n > 0:
                    v.text = new_text
                    mod_count += n
            elif isinstance(v, str):
                new_text, n = pattern.subn(replacement, v)
                if n > 0:
                    obj[k] = new_text
                    mod_count += n
            else:
                mod_count += normalize_tags_in_obj(v, pattern, replacement, visited)
        return mod_count

    if hasattr(obj, "attributes") and isinstance(obj.attributes, dict):
        for k, v in list(obj.attributes.items()):
            if isinstance(v, RubyString):
                new_text, n = pattern.subn(replacement, v.text)
                if n > 0:
                    v.text = new_text
                    mod_count += n
            elif isinstance(v, str):
                new_text, n = pattern.subn(replacement, v)
                if n > 0:
                    obj.attributes[k] = new_text
                    mod_count += n
            else:
                mod_count += normalize_tags_in_obj(v, pattern, replacement, visited)
        return mod_count

    return 0


def sweep_rvdata2_dir(target_dir, dry_run=False):
    """
    Percorre todos os arquivos .rvdata2 em target_dir e normaliza as tags.
    Retorna (total_files_scanned, total_files_modified, total_tags_normalized).
    """
    if not os.path.exists(target_dir):
        print(f"Diretório não encontrado: {target_dir}")
        return 0, 0, 0

    rvdata_files = sorted(glob.glob(os.path.join(target_dir, "*.rvdata2")))
    total_files = len(rvdata_files)
    modified_files = 0
    total_tags = 0

    print(f"Varrendo diretório: {target_dir} ({total_files} arquivos)...")

    for file_path in rvdata_files:
        fname = os.path.basename(file_path)
        if fname in SKIP_FILES:
            continue

        try:
            with open(file_path, "rb") as f:
                raw_data = r_marshal.load(f)

            n_mod = normalize_tags_in_obj(raw_data)
            if n_mod > 0:
                modified_files += 1
                total_tags += n_mod
                if not dry_run:
                    with open(file_path, "wb") as f:
                        r_marshal.write(f, raw_data)
                print(f"  ✓ {fname}: {n_mod} tags normalizadas")
        except Exception as e:
            print(f"  ✗ Erro em {fname}: {e}")

    return total_files, modified_files, total_tags


def sweep_database(db_path, dry_run=False):
    """
    Atualiza todas as tags corrompidas na tabela segment de memory.db.
    Retorna total_segments_updated.
    """
    if not os.path.exists(db_path):
        print(f"Banco de dados não encontrado: {db_path}")
        return 0

    print(f"Varrendo banco de dados: {db_path}...")
    conn = sqlite3.connect(db_path)
    cur = conn.cursor()

    cur.execute(
        "SELECT id, target FROM segment WHERE target LIKE '%\\nomE[%' OR target LIKE '%\\namE[%' OR target LIKE '%\\nome[%' OR target LIKE '%\\name[%'"
    )
    rows = cur.fetchall()
    updated = 0

    for seg_id, target in rows:
        new_target, n = TAG_PATTERN.subn(TARGET_REPLACEMENT, target)
        if n > 0:
            updated += 1
            if not dry_run:
                cur.execute(
                    "UPDATE segment SET target = ?, updated_at = datetime('now') WHERE id = ?",
                    (new_target, seg_id),
                )

    if not dry_run and updated > 0:
        conn.commit()
        print(f"  ✓ {updated} segmentos atualizados no banco de dados.")
    elif dry_run:
        print(f"  [DRY-RUN] {updated} segmentos seriam atualizados no banco de dados.")
    else:
        print("  Nenhum segmento precisou ser atualizado no banco.")

    conn.close()
    return updated


def verify_clean(game_dir):
    """Verifica se restou alguma tag antiga no diretório do jogo."""
    corrupted_pattern = re.compile(rb"\\(?:nam[eE]|nom[eE]|nome)\s*\[")
    name_pattern = re.compile(rb"\\NAME\s*\[")
    corrupted_count = 0
    name_count = 0

    for path in glob.glob(os.path.join(game_dir, "*.rvdata2")):
        fname = os.path.basename(path)
        if fname in SKIP_FILES:
            continue
        with open(path, "rb") as f:
            content = f.read()
        corrupted_count += len(corrupted_pattern.findall(content))
        name_count += len(name_pattern.findall(content))

    return corrupted_count, name_count


def main():
    parser = argparse.ArgumentParser(
        description="Normaliza tags de nome de personagem para \\NAME[...] nos arquivos de jogo e no cache."
    )
    parser.add_argument(
        "--game-dir",
        default="/mnt/hdd/game/Dress Quest EN/Data",
        help="Caminho para o diretório Data do jogo",
    )
    parser.add_argument(
        "--output-dir",
        default="output",
        help="Caminho para o diretório output",
    )
    parser.add_argument(
        "--db-path",
        default="cache/memory.db",
        help="Caminho para o arquivo do banco de memória",
    )
    parser.add_argument(
        "--dry-run",
        action="store_true",
        help="Apenas simula as alterações sem gravar nos arquivos ou banco",
    )

    args = parser.parse_args()

    print("=== INÍCIO DA VARREDURA DE NORMALIZAÇÃO DE TAGS ===")
    if args.dry_run:
        print("[MODO DE SIMULAÇÃO - NENHUM ARQUIVO SERÁ ALTERADO]")

    # 1. Varredura do diretório de dados do jogo
    game_files, game_mod_files, game_tags = sweep_rvdata2_dir(args.game_dir, dry_run=args.dry_run)

    # 2. Varredura do diretório output (staging)
    out_files, out_mod_files, out_tags = sweep_rvdata2_dir(args.output_dir, dry_run=args.dry_run)

    # 3. Varredura do banco de memória de tradução
    db_updated = sweep_database(args.db_path, dry_run=args.dry_run)

    print("\n=== RESUMO DA OPERAÇÃO ===")
    print(f"Diretório do Jogo ({args.game_dir}):")
    print(f"  - Arquivos escaneados: {game_files}")
    print(f"  - Arquivos modificados: {game_mod_files}")
    print(f"  - Tags normalizadas: {game_tags}")
    print(f"Diretório Output ({args.output_dir}):")
    print(f"  - Arquivos escaneados: {out_files}")
    print(f"  - Arquivos modificados: {out_mod_files}")
    print(f"  - Tags normalizadas: {out_tags}")
    print(f"Banco de Memória ({args.db_path}):")
    print(f"  - Segmentos atualizados: {db_updated}")

    # 4. Verificação final de integridade
    if not args.dry_run and os.path.exists(args.game_dir):
        corrupted, normalized = verify_clean(args.game_dir)
        print("\n=== VERIFICAÇÃO FINAL ===")
        print(f"Tags corrompidas restantes em {args.game_dir}: {corrupted}")
        print(f"Tags \\NAME[...] ativas em {args.game_dir}: {normalized}")
        if corrupted == 0:
            print("✓ SUCESSO: Todas as tags foram 100% normalizadas!")
        else:
            print(f"⚠ ALERTA: Ainda restam {corrupted} tags não normalizadas.")


if __name__ == "__main__":
    main()
