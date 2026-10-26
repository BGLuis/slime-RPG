import csv
import codecs
import copy
import os
import re
import logging
from src.extractor.BaseExtractor import BaseExtractor, _ClassOrInstanceMethod
from src.extractor.Segment import Segment, copy_preserving_metadata
from src.factory import register_extractor
from src.utils.LanguageCodes import (
    identify_language_from_filename,
    generate_target_filename,
    normalize_language_code,
    get_english_name,
    is_language_column
)


class CSVDataList(list):
    pass


def detect_delimiter_from_file(file_path: str) -> str:
    """Detecta automaticamente o delimitador do arquivo CSV (, ; \t |)."""
    try:
        with open(file_path, 'r', encoding='utf-8-sig', errors='replace') as f:
            lines = [f.readline() for _ in range(5)]
            sample = "".join(lines)
            if sample.strip():
                try:
                    sniffer = csv.Sniffer()
                    dialect = sniffer.sniff(sample, delimiters=[',', ';', '\t', '|'])
                    return dialect.delimiter
                except Exception:
                    for line in lines:
                        if line.strip():
                            counts = {
                                ',': line.count(','),
                                ';': line.count(';'),
                                '\t': line.count('\t'),
                                '|': line.count('|')
                            }
                            best_delim = max(counts, key=counts.get)
                            if counts[best_delim] > 0:
                                return best_delim
                            break
    except Exception:
        pass
    return ','

@register_extractor("csv", display_name="CSV", aliases=["CSV"])
class CSVExtractor(BaseExtractor):
    name = "csv"
    files_types = ['csv']
    LOC_FOLDER_NAMES = ('languages', 'language', 'lang', 'locales', 'localization', 'translations', 'i18n')

    @classmethod
    def detect(cls, path: str):
        from src.services.GameDetector import DetectionResult

        for loc_name in cls.LOC_FOLDER_NAMES:
            loc_candidate = os.path.join(path, loc_name)
            if os.path.isdir(loc_candidate):
                try:
                    loc_entries = os.listdir(loc_candidate)
                except Exception:
                    loc_entries = []
                csv_files = [f for f in loc_entries if f.lower().endswith('.csv')]
                lang_csv_files = [f for f in csv_files if identify_language_from_filename(f) is not None]
                if len(lang_csv_files) > 0 or len(csv_files) > 0:
                    return DetectionResult(
                        detected_path=loc_candidate,
                        score=100,
                        extractor=cls.display_name,
                        description=f'Sistema de Localização por CSV (subpasta {loc_name})',
                        file_count=len(csv_files),
                    )

        try:
            entries = os.listdir(path)
        except Exception:
            return None

        csv_files = [f for f in entries if f.lower().endswith('.csv')]
        if not csv_files:
            return None

        lang_csv_files = [f for f in csv_files if identify_language_from_filename(f) is not None]
        if len(lang_csv_files) > 0:
            return DetectionResult(
                detected_path=path,
                score=65,
                extractor=cls.display_name,
                description='Arquivos CSV de Idioma (pasta atual)',
                file_count=len(csv_files),
            )

        return DetectionResult(
            detected_path=path,
            score=20,
            extractor=cls.display_name,
            description='Arquivos CSV',
            file_count=len(csv_files),
        )

    def __init__(self, translate, workspace=None):
        super().__init__(translate, workspace=workspace)
        self.columns_to_translate = ['value', 'text']
        self.csv_delimiter = ','
        self.skip_empty = True
        self.skip_headers = True
        self._file_modes = {}
        self._detected_delimiters = {}
        self._file_boms = {}
        self._file_lineterminators = {}

    def _normalize_file_key(self, file_path_or_name: str) -> str:
        if not file_path_or_name:
            return ""
        folder_input = getattr(self, 'folderInput', getattr(self.__class__, 'folderInput', None))
        norm_path = os.path.normpath(str(file_path_or_name))
        if folder_input:
            try:
                norm_input = os.path.normpath(str(folder_input))
                if os.path.isabs(norm_path) and os.path.isabs(norm_input):
                    if os.path.commonpath([norm_path, norm_input]) == norm_input:
                        return os.path.normpath(os.path.relpath(norm_path, norm_input))
                elif not os.path.isabs(norm_path) and (norm_path.startswith(norm_input + os.sep) or norm_path.startswith(norm_input + "/")):
                    return os.path.normpath(os.path.relpath(norm_path, norm_input))
                elif os.path.isabs(norm_path):
                    abs_input = os.path.abspath(norm_input)
                    if os.path.commonpath([norm_path, abs_input]) == abs_input:
                        return os.path.normpath(os.path.relpath(norm_path, abs_input))
            except Exception:
                pass
        return norm_path

    @classmethod
    def get_interactive_questions(cls):
        return [
            {
                'key': 'columns',
                'question': 'Digite os nomes das colunas a traduzir (separadas por vírgula, ex: value,text):',
                'title': '\n=== CONFIGURAÇÃO CSV ===',
                'description': 'Especifique quais colunas do CSV devem ser traduzidas (deixe em branco para detecção automática).',
                'color': 'cyan',
                'required': False
            },
            {
                'key': 'delimiter',
                'question': 'Digite o delimitador do CSV (deixe em branco para detecção automática):',
                'description': 'Exemplo: , (vírgula), ; (ponto e vírgula), \\t (tab)',
                'color': 'yellow',
                'required': False
            }
        ]

    def apply_configuration(self, config):
        if 'columns' in config and config['columns']:
            columns = [col.strip() for col in config['columns'].split(',')]
            self.columns_to_translate = columns
            logging.info(f"✓ Colunas configuradas para tradução: {', '.join(columns)}")
        else:
            logging.info(f"⚠ Usando colunas padrão / detecção automática: {', '.join(self.columns_to_translate)}")

        if 'delimiter' in config and config['delimiter']:
            delimiter = config['delimiter']
            if delimiter == '\\t':
                delimiter = '\t'
            elif delimiter == '\\n':
                delimiter = '\n'
            self.csv_delimiter = delimiter
            logging.info(f"✓ Delimitador configurado: '{delimiter}'")

    def get_mask_patterns(self, file_name=None, data=None):
        p_format_codes = re.compile(r'\\[A-Za-z]+\s*\[[^\]]*\]', re.IGNORECASE)
        p_angle_codes = re.compile(r'\\[A-Za-z]+\s*<[^>]*>', re.IGNORECASE)
        p_printf = re.compile(r'%[0-9]*\.?[0-9]*[sdfoxX]')
        p_curly_vars = re.compile(r'\{[a-zA-Z0-9_]+\}')
        p_unity_tags = re.compile(r'</?[a-zA-Z][a-zA-Z0-9=_\-#]*(?:\s+[^>]*)?/?>', re.IGNORECASE)
        p_escaped_newline = re.compile(r'\\n|\\r|\\t')
        return [p_format_codes, p_angle_codes, p_printf, p_curly_vars, p_unity_tags, p_escaped_newline]

    def is_translatable_file(self, file_name):
        """
        Em pastas com múltiplos CSVs nomeados por idioma (ex: en.csv, es.csv, fr.csv),
        apenas o arquivo correspondente ao idioma de origem (source) deve ser traduzido.
        """
        id_info = identify_language_from_filename(file_name)
        if id_info:
            file_lang, _ = id_info
            source_code = normalize_language_code(getattr(self.translate, 'lang_source', 'en'))
            if source_code and file_lang != source_code:
                logging.info(f"Ignorando arquivo de outro idioma: {file_name} (origem esperada: {source_code})")
                return False
        return True

    def get_target_filename(self, file_name):
        """
        Gera o nome do arquivo de destino se for um arquivo nomeado por idioma (ex: en.csv -> pt.csv).
        Caso contrário, mantém o mesmo nome.
        """
        target_code = getattr(self.translate, 'lang_target', 'pt')
        id_info = identify_language_from_filename(file_name)
        if id_info:
            return generate_target_filename(file_name, target_code)
        return file_name

    def extract_files(self_or_cls, file_path=None):
        if file_path is None:
            file_path = self_or_cls
            self_or_cls = None

        files_types = self_or_cls.files_types if isinstance(self_or_cls, CSVExtractor) else CSVExtractor.files_types
        if not file_path.endswith(tuple(files_types)):
            return [os.path.basename(file_path), None]

        base_name = os.path.basename(file_path)
        delimiter = detect_delimiter_from_file(file_path)

        has_bom = False
        lineterminator = '\n'
        raw_bytes = None
        try:
            with open(file_path, 'rb') as f_bin:
                raw_bytes = f_bin.read()
            has_bom = raw_bytes.startswith(codecs.BOM_UTF8)
            lineterminator = '\r\n' if b'\r\n' in raw_bytes else ('\r' if b'\r' in raw_bytes and b'\n' not in raw_bytes else '\n')
        except Exception as e:
            logging.error(f"Erro ao analisar cabeçalho binário do CSV {file_path}: {e}")

        if isinstance(self_or_cls, CSVExtractor):
            rel_key = self_or_cls._normalize_file_key(file_path)
            self_or_cls._detected_delimiters[rel_key] = delimiter
            self_or_cls._detected_delimiters[file_path] = delimiter
            self_or_cls._detected_delimiters[base_name] = delimiter

            self_or_cls._file_boms[rel_key] = has_bom
            self_or_cls._file_boms[file_path] = has_bom
            self_or_cls._file_boms[base_name] = has_bom

            self_or_cls._file_lineterminators[rel_key] = lineterminator
            self_or_cls._file_lineterminators[file_path] = lineterminator
            self_or_cls._file_lineterminators[base_name] = lineterminator

        try:
            with open(file_path, 'r', encoding='utf-8-sig', newline='', errors='replace') as f:
                reader = csv.DictReader(f, delimiter=delimiter)
                rows = list(reader)
                data = CSVDataList(rows)
                data._file_name = base_name
                data._csv_meta = {
                    'delimiter': delimiter,
                    'has_bom': has_bom,
                    'lineterminator': lineterminator,
                    'raw_bytes': raw_bytes,
                    'snapshot': copy.deepcopy(rows),
                }
                return [base_name, data]
        except Exception as e:
            logging.error(f"Erro ao ler CSV {file_path}: {e}")
            return [base_name, None]

    def _resolve_columns_for_data(self, file_name, data):
        """
        Determina a estratégia e as colunas a serem traduzidas para os dados do CSV.
        Retorna (mode, source_col, target_col, text_columns)
        """
        if not data or not isinstance(data, list) or not isinstance(data[0], dict):
            return ('standard', None, None, self.columns_to_translate)

        fieldnames = list(data[0].keys())

        # 1. Checagem de Tabela Multilíngue (colunas com nomes/códigos de idiomas)
        source_lang = normalize_language_code(getattr(self.translate, 'lang_source', 'en'))
        target_lang = normalize_language_code(getattr(self.translate, 'lang_target', 'pt'))

        source_col = None
        target_col = None

        for col in fieldnames:
            norm_col = is_language_column(col)
            if norm_col:
                if norm_col == source_lang and source_col is None:
                    source_col = col
                elif norm_col == target_lang and target_col is None:
                    target_col = col

        if source_col is not None:
            # Encontrou coluna para o idioma de origem!
            if target_col is None:
                if source_col.islower():
                    target_col = target_lang
                elif source_col.isupper():
                    target_col = target_lang.upper()
                else:
                    target_col = get_english_name(target_lang)
            return ('columns', source_col, target_col, [source_col])

        # 2. Checagem de colunas explicitamente configuradas presentes no CSV
        configured_present = [col for col in self.columns_to_translate if col in fieldnames]
        if configured_present:
            return ('standard', None, None, configured_present)

        # 3. Heurística para colunas comuns de texto
        COMMON_TEXT_COLUMNS = ['text', 'value', 'translation', 'content', 'message', 'msg', 'dialogue', 'string', 'desc', 'description', 'target']
        text_cols = [col for col in fieldnames if col.lower() in COMMON_TEXT_COLUMNS]
        if text_cols:
            return ('standard', None, None, text_cols)

        # 4. Caso de 2 colunas: se uma for id/key e a outra texto
        if len(fieldnames) == 2:
            COMMON_ID_COLUMNS = ['id', 'key', 'code', 'name', 'identifier', 'tag', 'label', 'index']
            if fieldnames[0].lower() in COMMON_ID_COLUMNS and fieldnames[1].lower() not in COMMON_ID_COLUMNS:
                return ('standard', None, None, [fieldnames[1]])
            if fieldnames[1].lower() in COMMON_ID_COLUMNS and fieldnames[0].lower() not in COMMON_ID_COLUMNS:
                return ('standard', None, None, [fieldnames[0]])
            return ('standard', None, None, [fieldnames[1]])

        return ('standard', None, None, self.columns_to_translate)

    def extract_text(self, file_name, data):
        if not data:
            return None

        mode, source_col, target_col, text_cols = self._resolve_columns_for_data(file_name, data)
        self._file_modes[file_name] = {
            'mode': mode,
            'source_col': source_col,
            'target_col': target_col,
            'text_cols': text_cols
        }

        texts_to_translate = {}

        if mode == 'columns':
            for row_idx, row in enumerate(data):
                if target_col in row and row[target_col] and row[target_col].strip():
                    continue
                text = row.get(source_col, '')
                if self.skip_empty and not text:
                    continue
                if self.skip_headers and text.startswith('■'):
                    continue
                key = f"row_{row_idx}_col_{source_col}"
                texts_to_translate[key] = text
        else:
            # Modo padrão ou file-per-language
            target_name = self.get_target_filename(file_name)
            existing_target_data = None
            if target_name != file_name:
                folder_input = getattr(self, 'folderInput', self.__class__.folderInput)
                candidate_target = os.path.join(folder_input, target_name)
                if os.path.exists(candidate_target):
                    _, existing_target_data = self.extract_files(candidate_target)

            for row_idx, row in enumerate(data):
                for column in text_cols:
                    if column in row:
                        if existing_target_data and row_idx < len(existing_target_data):
                            existing_val = existing_target_data[row_idx].get(column, '')
                            if existing_val and existing_val.strip():
                                continue

                        text = row[column]
                        if self.skip_empty and not text:
                            continue
                        if self.skip_headers and text.startswith('■'):
                            continue
                        key = f"row_{row_idx}_col_{column}"
                        texts_to_translate[key] = text

        return texts_to_translate if texts_to_translate else None

    def update_json(self, file_name, data, new_data):
        if not data:
            return data

        mode_info = self._file_modes.get(file_name)
        if not mode_info:
            mode, source_col, target_col, text_cols = self._resolve_columns_for_data(file_name, data)
        else:
            mode = mode_info.get('mode', 'standard')
            source_col = mode_info.get('source_col')
            target_col = mode_info.get('target_col')
            text_cols = mode_info.get('text_cols', self.columns_to_translate)

        updated_data = CSVDataList()
        if hasattr(data, '_csv_meta'):
            updated_data._csv_meta = getattr(data, '_csv_meta')
        if hasattr(data, '_file_name'):
            updated_data._file_name = getattr(data, '_file_name')

        if mode == 'columns':
            for row_idx, row in enumerate(data):
                updated_row = row.copy()
                key = f"row_{row_idx}_col_{source_col}"
                if new_data and key in new_data:
                    updated_row[target_col] = new_data[key]
                elif target_col not in updated_row:
                    updated_row[target_col] = ""
                updated_data.append(updated_row)
        else:
            for row_idx, row in enumerate(data):
                updated_row = row.copy()
                for column in text_cols:
                    if column in row:
                        key = f"row_{row_idx}_col_{column}"
                        if new_data and key in new_data:
                            updated_row[column] = new_data[key]
                updated_data.append(updated_row)

        return updated_data

    @classmethod
    def _infer_file_name(cls, data):
        if hasattr(data, '_file_name') and getattr(data, '_file_name'):
            return getattr(data, '_file_name')
        return 'file.csv'

    @_ClassOrInstanceMethod
    def _walk_or_apply_segments(target, file_name, data, mode='extract', translations=None):
        cls = target if isinstance(target, type) else target.__class__
        inst = target._create_segment_instance() if isinstance(target, type) else target
        base_name = os.path.basename(file_name) if file_name else cls._infer_file_name(data)
        extracted = inst.extract_text(base_name, data) or {}
        key_re = re.compile(r'^row_(\d+)_col_(.+)$')

        if mode == 'extract':
            segments = []
            for key, val in extracted.items():
                m = key_re.match(key)
                if m:
                    r_idx = int(m.group(1))
                    col = m.group(2)
                    seg_id = f"{base_name}#row:{r_idx}/col:{col}"
                    ctx = {'file': base_name, 'row': r_idx, 'column': col, 'kind': 'dialogue'}
                else:
                    seg_id = f"{base_name}#{key}"
                    ctx = {'file': base_name, 'key': key, 'kind': 'dialogue'}
                segments.append(Segment(id=seg_id, source=val, context=ctx))
            return segments

        translations = translations or {}
        new_data = {}
        changed = False
        for key, val in extracted.items():
            m = key_re.match(key)
            if m:
                r_idx = int(m.group(1))
                col = m.group(2)
                seg_id = f"{base_name}#row:{r_idx}/col:{col}"
            else:
                seg_id = f"{base_name}#{key}"
            new_val = translations.get(seg_id, val)
            if new_val != val:
                changed = True
                new_data[key] = new_val
            else:
                new_data[key] = val

        if not changed:
            return copy_preserving_metadata(data)
        return inst.update_json(base_name, data, new_data)

    def import_file(self_or_cls, file_name, data, folder):
        if not data:
            return

        csv_meta = getattr(data, '_csv_meta', None) or {}

        if isinstance(self_or_cls, CSVExtractor):
            target_name = self_or_cls.get_target_filename(file_name)
            rel_key = self_or_cls._normalize_file_key(file_name)
            base_key = os.path.basename(file_name)
            delimiter = (
                self_or_cls._detected_delimiters.get(rel_key) or
                self_or_cls._detected_delimiters.get(file_name) or
                self_or_cls._detected_delimiters.get(base_key) or
                csv_meta.get('delimiter') or
                self_or_cls.csv_delimiter
            )
            has_bom = (
                self_or_cls._file_boms.get(rel_key)
                if rel_key in self_or_cls._file_boms
                else (
                    self_or_cls._file_boms.get(file_name)
                    if file_name in self_or_cls._file_boms
                    else (
                        self_or_cls._file_boms.get(base_key)
                        if base_key in self_or_cls._file_boms
                        else csv_meta.get('has_bom', False)
                    )
                )
            )
            lineterminator = (
                self_or_cls._file_lineterminators.get(rel_key) or
                self_or_cls._file_lineterminators.get(file_name) or
                self_or_cls._file_lineterminators.get(base_key) or
                csv_meta.get('lineterminator') or
                '\n'
            )
        else:
            target_name = file_name
            delims = getattr(self_or_cls, '_detected_delimiters', {})
            delimiter = (delims.get(file_name) if isinstance(delims, dict) else None) or csv_meta.get('delimiter', ',')
            boms = getattr(self_or_cls, '_file_boms', {})
            has_bom = (boms.get(file_name) if isinstance(boms, dict) and file_name in boms else None)
            if has_bom is None:
                has_bom = csv_meta.get('has_bom', False)
            lineterms = getattr(self_or_cls, '_file_lineterminators', {})
            lineterminator = (lineterms.get(file_name) if isinstance(lineterms, dict) else None) or csv_meta.get('lineterminator', '\n')

        file_path = os.path.join(folder, target_name)
        os.makedirs(os.path.dirname(file_path), exist_ok=True)

        raw_bytes = csv_meta.get('raw_bytes')
        snapshot = csv_meta.get('snapshot')
        if raw_bytes is not None and snapshot is not None and target_name == file_name and list(data) == snapshot:
            with open(file_path, 'wb') as f_bin:
                f_bin.write(raw_bytes)
            logging.info(f"✓ Salvo CSV: {target_name} com delimitador '{delimiter}' (BOM={has_bom})")
            return

        try:
            if isinstance(data, list) and len(data) > 0:
                fieldnames = list(data[0].keys())
                seen = set(fieldnames)
                for row in data:
                    for key in row.keys():
                        if key not in seen:
                            fieldnames.append(key)
                            seen.add(key)
            else:
                return

            encoding = 'utf-8-sig' if has_bom else 'utf-8'
            with open(file_path, 'w', encoding=encoding, newline='') as f:
                writer = csv.DictWriter(
                    f,
                    fieldnames=fieldnames,
                    delimiter=delimiter,
                    lineterminator=lineterminator,
                    extrasaction='ignore'
                )
                writer.writeheader()
                writer.writerows(data)
            logging.info(f"✓ Salvo CSV: {target_name} com delimitador '{delimiter}' (BOM={has_bom})")
        except Exception as e:
            logging.error(f"Erro ao salvar CSV {file_path}: {e}")

    @staticmethod
    def fix_text_translate(text, original_text=None):
        if isinstance(text, dict):
            for key, value in text.items():
                if isinstance(value, str):
                    text[key] = value.strip()
        return text
