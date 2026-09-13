#!/usr/bin/env python3
"""
Migra os caches antigos em cache/<engine>/cache_<src>_<tgt>.json para a Translation
Memory em cache/memory.db. Idempotente: pode ser rodado de novo sem duplicar linhas
(a chave única é src_lang+tgt_lang+source_hash+engine). Os JSONs originais não são
apagados nem modificados.
"""
import glob
import json
import os
import re
import sys

sys.path.insert(0, os.path.abspath(os.path.join(os.path.dirname(__file__), '..')))

from src.translate.TranslationMemory import TranslationMemory

CACHE_DIR = 'cache'
DB_PATH = os.path.join(CACHE_DIR, 'memory.db')
FILE_PATTERN = re.compile(r'cache_([a-z]{2,})_([a-z]{2,})\.json$')


def find_legacy_cache_files():
    for path in glob.glob(os.path.join(CACHE_DIR, '*', 'cache_*.json')):
        match = FILE_PATTERN.search(os.path.basename(path))
        if not match:
            continue
        engine = os.path.basename(os.path.dirname(path))
        src_lang, tgt_lang = match.groups()
        yield path, engine, src_lang, tgt_lang


def migrate():
    imported = 0
    identity_entries = 0
    skipped_null = 0

    for path, engine, src_lang, tgt_lang in find_legacy_cache_files():
        with open(path, 'r', encoding='utf-8') as f:
            data = json.load(f)

        tm = TranslationMemory(DB_PATH, src_lang, tgt_lang, engine=engine)
        file_imported = 0
        for source, target in data.items():
            if source == '':
                continue
            if target is None:
                skipped_null += 1
                continue
            tm.store(source, target, status='machine', allow_identity=True)
            imported += 1
            file_imported += 1
            if source == target:
                identity_entries += 1

        print(f"{path}: {file_imported}/{len(data)} entradas migradas ({engine}, {src_lang}->{tgt_lang})")

    print(f"\nTotal importado: {imported}")
    print(f"Entradas puladas (tradução nula no JSON de origem): {skipped_null}")
    print(f"Entradas com source == target (candidatas a limpeza manual): {identity_entries}")
    if identity_entries:
        print(
            "Para revisar: SELECT * FROM segment WHERE source = target AND status = 'machine'; "
            f"(arquivo: {DB_PATH})"
        )


if __name__ == '__main__':
    migrate()
