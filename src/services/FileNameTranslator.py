from dataclasses import dataclass, field
import logging
import os
from pathlib import Path
import re
import shutil

WINDOWS_RESERVED = {
    "CON", "PRN", "AUX", "NUL",
    "COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9",
    "LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9",
}


@dataclass
class FileNameTranslationResult:
    total_files: int = 0
    translatable_files: int = 0
    translated_count: int = 0
    copied_originals: int = 0
    collisions_resolved: int = 0
    output_dir: str = ""
    files_map: dict = field(default_factory=dict)


def sanitize_filename(name: str, ext: str = "") -> str:
    """Sanitiza o nome de arquivo removendo caracteres inválidos, separadores e nomes reservados."""
    stem = re.sub(r'[/\\<>:"|?*\x00-\x1f]', '_', name)
    stem = stem.strip(" .")
    if not stem:
        stem = "unnamed"

    stem_check = stem.upper().split('.')[0]
    if stem.upper() in WINDOWS_RESERVED or stem_check in WINDOWS_RESERVED:
        stem = f"_{stem}"

    if ext:
        ext = re.sub(r'[/\\<>:"|?*\x00-\x1f]', '_', ext)

    return f"{stem}{ext}"


class FileNameTranslator:
    def __init__(self, translate, folder_output: str = 'output'):
        self.translate = translate
        self.folderOutput = folder_output

    @property
    def folder_output(self) -> str:
        return self.folderOutput

    @folder_output.setter
    def folder_output(self, value: str):
        self.folderOutput = value

    @staticmethod
    def sanitize_filename(name: str, ext: str = "") -> str:
        return sanitize_filename(name, ext)

    def process_directory(self, input_dir: str, keep_original: bool = False, log_callback=None) -> FileNameTranslationResult:
        result = FileNameTranslationResult(output_dir=self.folderOutput)

        def _log(msg: str, level: str = 'info', color: str = 'white'):
            if level == 'warning':
                logging.warning(msg)
            elif level == 'error':
                logging.error(msg)
            else:
                logging.info(msg)
            if log_callback:
                try:
                    log_callback(msg, color)
                except Exception:
                    pass

        if not os.path.exists(input_dir):
            _log(f"Erro: Pasta '{input_dir}' não encontrada.", level='error', color='red')
            return result

        all_files_info = []
        for root, dirs, files in os.walk(input_dir):
            dirs.sort()
            for f in sorted(files):
                rel_dir = os.path.relpath(root, input_dir)
                if rel_dir == ".":
                    rel_dir = ""

                name, ext = os.path.splitext(f)
                all_files_info.append({
                    'rel_dir': rel_dir,
                    'original_name': f,
                    'name_no_ext': name,
                    'ext': ext,
                    'full_src_path': os.path.join(root, f),
                })

        result.total_files = len(all_files_info)
        if not all_files_info:
            _log("Nenhum arquivo encontrado na pasta ou subpastas.", level='warning', color='yellow')
            return result

        _log(f"\nEncontrados {len(all_files_info)} arquivos em '{input_dir}' e suas subpastas.", level='info', color='cyan')

        file_names_to_translate = [info['name_no_ext'] for info in all_files_info]
        result.translatable_files = len(file_names_to_translate)

        _log(f"Traduzindo {len(file_names_to_translate)} nomes de arquivos...", level='info', color='cyan')

        translated_names = self.translate.translate_batch(file_names_to_translate)

        if not translated_names or len(translated_names) != len(file_names_to_translate):
            _log("Erro ao traduzir nomes de arquivos. Verifique a conexão ou as chaves de API.", level='error', color='red')
            return result

        output_base = self.folderOutput
        _log(f"Salvando resultados em '{output_base}'...", level='info', color='cyan')

        allocated_by_dir: dict[str, set[str]] = {}

        for i, info in enumerate(all_files_info):
            src_path = info['full_src_path']
            rel_dir = info['rel_dir']
            target_dir = os.path.join(output_base, rel_dir)

            if target_dir not in allocated_by_dir:
                allocated_by_dir[target_dir] = set()
            allocated = allocated_by_dir[target_dir]

            t_val = translated_names[i]
            trans_stem_raw = str(t_val) if t_val is not None else info['name_no_ext']
            sanitized_stem = sanitize_filename(trans_stem_raw, "")
            ext = re.sub(r'[/\\<>:"|?*\x00-\x1f]', '_', info['ext'])

            candidate_filename = f"{sanitized_stem}{ext}"

            def is_in_use(fname: str) -> bool:
                if fname.lower() in allocated:
                    return True
                fpath = os.path.join(target_dir, fname)
                if os.path.lexists(fpath):
                    try:
                        if Path(fpath).resolve() != Path(src_path).resolve():
                            return True
                    except Exception:
                        return True
                return False

            if is_in_use(candidate_filename):
                result.collisions_resolved += 1
                suffix = 1
                while True:
                    candidate_filename = f"{sanitized_stem}_{suffix}{ext}"
                    if not is_in_use(candidate_filename):
                        break
                    suffix += 1

            dst_path = os.path.join(target_dir, candidate_filename)

            target_path_obj = Path(target_dir).resolve()
            dst_path_obj = Path(dst_path).resolve()
            if not dst_path_obj.is_relative_to(target_path_obj):
                _log(f"Caminho inseguro detectado para o arquivo '{dst_path}'. Operação cancelada para este arquivo.", level='error', color='red')
                continue

            allocated.add(candidate_filename.lower())

            try:
                os.makedirs(target_dir, exist_ok=True)
                if Path(src_path).resolve() != dst_path_obj:
                    shutil.copy2(src_path, dst_path)
                result.translated_count += 1
                result.files_map[src_path] = dst_path

                if keep_original:
                    orig_name = info['original_name']
                    orig_dst_path = os.path.join(target_dir, orig_name)
                    if candidate_filename.lower() != orig_name.lower() and Path(src_path).resolve() != Path(orig_dst_path).resolve():
                        if is_in_use(orig_name):
                            orig_stem, orig_ext = os.path.splitext(orig_name)
                            s_idx = 1
                            while True:
                                cand_orig = f"{orig_stem}_{s_idx}{orig_ext}"
                                if not is_in_use(cand_orig):
                                    orig_name = cand_orig
                                    orig_dst_path = os.path.join(target_dir, orig_name)
                                    result.collisions_resolved += 1
                                    break
                                s_idx += 1
                        allocated.add(orig_name.lower())
                        shutil.copy2(src_path, orig_dst_path)
                        result.copied_originals += 1
            except Exception as e:
                _log(f"Erro ao processar arquivo '{info['original_name']}': {e}", level='error', color='red')

        _log("\n✓ Sucesso!", level='info', color='green')
        _log(f"  - Arquivos traduzidos: {result.translated_count}", level='info', color='white')
        if keep_original:
            _log(f"  - Originais mantidos: {result.copied_originals}", level='info', color='white')
        if result.collisions_resolved > 0:
            _log(f"  - Colisões resolvidas: {result.collisions_resolved}", level='info', color='white')
        _log(f"  - Localização: {os.path.abspath(output_base)}", level='info', color='white')

        return result
