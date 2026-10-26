import hashlib
import json
import logging
import os
import tempfile
from datetime import datetime, timezone
from typing import Dict, Iterable, List, Optional

SCHEMA_VERSION = 1
META_DIRNAME = '.slime'
META_FILENAME = 'meta.json'

ORIGIN_JOB = 'job'
ORIGIN_LEGACY = 'legacy-adopted'

STATE_INTACT = 'intact'
STATE_UNCHANGED = 'unchanged'
STATE_EDITED = 'edited'
STATE_ORIGINAL_CHANGED = 'original_changed'
STATE_ORIGINAL_MISSING = 'original_missing'
STATE_TRANSLATION_MISSING = 'translation_missing'
STATE_PARTIAL = 'partial'
STATE_NEW = 'new'

WOLF_MULTIMEDIA_PREFIXES = (
    'bgm', 'se', 'seh', 'sound', 'voice', 'graphic', 'picture', 'cg',
    'hcg', 'samune', 'hsamune', 'face', 'charachip', 'enemychip', 'enemygraphic',
    'eventgraphic', 'systemgraphic', 'battlegraphic', 'battleeffect',
    'mapchip', 'parallax', 'fog', 'font', 'hfont', 'icon', 'movie', 'video'
)


def meta_path(translated_dir: str) -> str:
    return os.path.join(translated_dir, META_DIRNAME, META_FILENAME)


def _now() -> str:
    return datetime.now(timezone.utc).isoformat(timespec='seconds')


def sha256_file(path: str) -> Optional[str]:
    h = hashlib.sha256()
    try:
        with open(path, 'rb') as f:
            for chunk in iter(lambda: f.read(1 << 20), b''):
                h.update(chunk)
    except OSError:
        return None
    return h.hexdigest()


def _normalize_exts(extensions: Optional[Iterable[str]]):
    if not extensions:
        return None
    return tuple(f".{e.lower().lstrip('.')}" for e in extensions)


def scan_entries(root: str, extensions: Optional[Iterable[str]] = None) -> Dict[str, dict]:
    """Escaneia arquivos soltos e contêineres (.wolf) sob `root`.

    Devolve mapa:
    rel_path -> {'sha256': str, 'container': Optional[str], 'container_sha256': Optional[str]}
    """
    exts = _normalize_exts(extensions)
    entries: Dict[str, dict] = {}
    wolf_archives = []

    for dirpath, dirnames, filenames in os.walk(root):
        dirnames[:] = [d for d in dirnames if d != META_DIRNAME]
        for name in filenames:
            full = os.path.join(dirpath, name)
            if name.lower().endswith('.wolf'):
                stem = os.path.splitext(name)[0]
                if not any(stem.lower().startswith(p) for p in WOLF_MULTIMEDIA_PREFIXES):
                    wolf_archives.append((full, dirpath, name, stem))
                continue

            if exts and not name.lower().endswith(exts):
                continue

            digest = sha256_file(full)
            if digest:
                rel = os.path.relpath(full, root).replace(os.sep, '/')
                entries[rel] = {
                    'sha256': digest,
                    'container': None,
                    'container_sha256': None,
                }

    # Se houver contêineres .wolf, inspeciona entradas que não existam como arquivo solto
    if wolf_archives:
        try:
            from src.extractor.wolfrpg.WolfArchive import WolfArchive
            for full_arch, dirpath, filename, stem in wolf_archives:
                rel_dir = os.path.relpath(dirpath, root).replace(os.sep, '/')
                if rel_dir == '.':
                    rel_dir = ''

                if stem.lower() != 'data':
                    virtual_base = f"{rel_dir}/{stem}".strip('/')
                else:
                    virtual_base = rel_dir

                arch_entries = WolfArchive.list_entries_with_hashes(
                    full_arch, virtual_base_dir=virtual_base, extensions=extensions
                )
                for rel_entry, info in arch_entries.items():
                    if rel_entry not in entries:
                        entries[rel_entry] = {
                            'sha256': info['sha256'],
                            'container': os.path.relpath(full_arch, root).replace(os.sep, '/'),
                            'container_sha256': info['container_sha256'],
                        }
        except Exception as e:
            logging.warning(f"Erro ao inspecionar contêineres .wolf em {root}: {e}")

    return entries


def scan_hashes(root: str, extensions: Optional[Iterable[str]] = None) -> Dict[str, str]:
    """Hash SHA-256 de cada arquivo sob `root`, indexado pelo caminho relativo (separador '/')."""
    return {rel: info['sha256'] for rel, info in scan_entries(root, extensions).items()}


def load_meta(translated_dir: str) -> Optional[dict]:
    try:
        with open(meta_path(translated_dir), 'r', encoding='utf-8') as f:
            data = json.load(f)
    except (OSError, ValueError):
        return None
    if not isinstance(data, dict) or data.get('schema_version', 0) > SCHEMA_VERSION:
        return None
    return data


def save_meta(translated_dir: str, meta: dict) -> str:
    """Grava o metadado de forma atômica para não deixar um JSON truncado se o processo cair."""
    path = meta_path(translated_dir)
    os.makedirs(os.path.dirname(path), exist_ok=True)
    fd, tmp = tempfile.mkstemp(dir=os.path.dirname(path), prefix='.meta_', suffix='.tmp')
    try:
        with os.fdopen(fd, 'w', encoding='utf-8') as f:
            json.dump(meta, f, ensure_ascii=False, indent=2, sort_keys=True)
        os.replace(tmp, path)
    except Exception:
        if os.path.exists(tmp):
            os.remove(tmp)
        raise
    return path


def resolve_original_dir(translated_dir: str, meta: dict) -> Optional[str]:
    rel = meta.get('original_dir')
    if not rel:
        return None
    return os.path.normpath(os.path.join(translated_dir, rel))


def build_meta(
    translated_dir: str,
    original_dir: Optional[str] = None,
    extensions: Optional[Iterable[str]] = None,
    *,
    origin: str,
    extractor: Optional[str],
    translator: Optional[str],
    lang_source: Optional[str],
    lang_target: Optional[str],
    file_status: Optional[Dict[str, str]] = None,
    previous: Optional[dict] = None,
) -> dict:
    """Monta o metadado cruzando original e traduzido.

    `file_status` traz o resultado por arquivo de um job; sem ele (adoção de legado)
    todo arquivo que difere do original é tratado como traduzido com sucesso.
    """
    translated_entries = scan_entries(translated_dir, extensions)
    file_status = file_status or {}

    files = {}
    if original_dir and os.path.isdir(original_dir):
        original_entries = scan_entries(original_dir, extensions)
        original_rel_dir = os.path.relpath(original_dir, translated_dir).replace(os.sep, '/')
        for rel, o_info in original_entries.items():
            t_info = translated_entries.get(rel)
            if t_info is None:
                continue
            original_hash = o_info['sha256']
            translated_hash = t_info['sha256']
            prev_info = (previous or {}).get('files', {}).get(rel, {})
            status = file_status.get(rel) or prev_info.get('status') or (
                'success' if translated_hash != original_hash else 'unchanged'
            )
            item = {
                'original_sha256': original_hash,
                'translated_sha256': translated_hash,
                'status': status,
            }
            if o_info.get('container'):
                item['container'] = o_info['container']
                item['container_sha256'] = o_info['container_sha256']
            files[rel] = item
    else:
        original_rel_dir = None
        for rel, t_info in translated_entries.items():
            prev_info = (previous or {}).get('files', {}).get(rel, {})
            status = file_status.get(rel) or prev_info.get('status', 'success')
            files[rel] = {
                'original_sha256': None,
                'translated_sha256': t_info['sha256'],
                'status': status,
            }

    now = _now()
    return {
        'schema_version': SCHEMA_VERSION,
        'origin': origin,
        'created_at': (previous or {}).get('created_at', now),
        'updated_at': now,
        'extractor': extractor,
        'translator': translator,
        'lang_source': lang_source,
        'lang_target': lang_target,
        'original_dir': original_rel_dir,
        'extensions': sorted(_normalize_exts(extensions) or []),
        'files': files,
    }


def verify(translated_dir: str, original_dir: Optional[str] = None) -> Optional[List[dict]]:
    """Classifica cada arquivo registrado. Devolve None se não houver metadado."""
    meta = load_meta(translated_dir)
    if meta is None:
        return None
    original_dir = original_dir or resolve_original_dir(translated_dir, meta)
    extensions = meta.get('extensions')

    container_cache: Dict[str, Dict[str, dict]] = {}

    report = []
    for rel, entry in sorted(meta['files'].items()):
        translated_now = sha256_file(os.path.join(translated_dir, rel))
        original_expected = entry.get('original_sha256')

        if original_expected is None:
            # Arquivo registrado sem pasta de backup (--no-backup)
            if translated_now is None:
                state = STATE_TRANSLATION_MISSING
            elif translated_now != entry.get('translated_sha256'):
                state = STATE_EDITED
            elif entry.get('status') == 'partial':
                state = STATE_PARTIAL
            elif entry.get('status') == 'unchanged':
                state = STATE_UNCHANGED
            else:
                state = STATE_INTACT
            report.append({'file': rel, 'state': state})
            continue

        original_now = None
        if original_dir and os.path.isdir(original_dir):
            loose_path = os.path.join(original_dir, rel)
            if os.path.isfile(loose_path):
                original_now = sha256_file(loose_path)
            elif entry.get('container'):
                c_rel = entry['container']
                c_path = os.path.join(original_dir, c_rel)
                if os.path.isfile(c_path):
                    c_now_hash = sha256_file(c_path)
                    if c_now_hash == entry.get('container_sha256'):
                        original_now = original_expected
                    else:
                        if c_path not in container_cache:
                            from src.extractor.wolfrpg.WolfArchive import WolfArchive
                            stem = os.path.splitext(os.path.basename(c_path))[0]
                            c_dir = os.path.dirname(c_rel).replace(os.sep, '/')
                            if stem.lower() != 'data':
                                v_base = f"{c_dir}/{stem}".strip('/')
                            else:
                                v_base = c_dir
                            container_cache[c_path] = WolfArchive.list_entries_with_hashes(
                                c_path, virtual_base_dir=v_base, extensions=extensions
                            )
                        c_entries = container_cache[c_path]
                        original_now = c_entries.get(rel, {}).get('sha256')

        if original_now is None:
            state = STATE_ORIGINAL_MISSING
        elif original_now != original_expected:
            state = STATE_ORIGINAL_CHANGED
        elif translated_now is None:
            state = STATE_TRANSLATION_MISSING
        elif translated_now != entry.get('translated_sha256'):
            state = STATE_EDITED
        elif entry.get('status') == 'partial':
            state = STATE_PARTIAL
        elif entry.get('status') == 'unchanged':
            state = STATE_UNCHANGED
        else:
            state = STATE_INTACT

        report.append({'file': rel, 'state': state})

    if original_dir and os.path.isdir(original_dir):
        current_original_entries = scan_entries(original_dir, extensions)
        for rel in sorted(set(current_original_entries) - set(meta['files'])):
            report.append({'file': rel, 'state': STATE_NEW})

    return report


def summarize(report: List[dict]) -> Dict[str, int]:
    summary: Dict[str, int] = {}
    for item in report:
        summary[item['state']] = summary.get(item['state'], 0) + 1
    return summary
