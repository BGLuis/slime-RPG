#!/usr/bin/env python3
"""
Limpa entradas corrompidas do cache/memory.db geradas por versões anteriores
que gravaram placeholders temporários (__XTOK_...).
"""
import os
import sqlite3
import sys

DB_PATH = os.path.join('cache', 'memory.db')


def clean_corrupted_cache(db_path=DB_PATH):
    if not os.path.exists(db_path):
        print(f"Banco não encontrado em {db_path}")
        return 0

    conn = sqlite3.connect(db_path)
    cur = conn.cursor()

    cur.execute("SELECT count(*) FROM segment WHERE source LIKE '%__XTOK_%' OR target LIKE '%__XTOK_%'")
    count = cur.fetchone()[0]

    if count > 0:
        print(f"Encontrados {count} registros corrompidos com __XTOK_.")
        cur.execute("DELETE FROM segment WHERE source LIKE '%__XTOK_%' OR target LIKE '%__XTOK_%'")
        conn.commit()
        print("Executando VACUUM para otimizar o banco...")
        conn.execute("VACUUM")
        print(f"✓ {count} registros corrompidos removidos com sucesso.")
    else:
        print("Nenhum registro corrompido encontrado.")

    conn.close()
    return count


if __name__ == '__main__':
    db_file = sys.argv[1] if len(sys.argv) > 1 else DB_PATH
    clean_corrupted_cache(db_file)
