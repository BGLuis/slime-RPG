import csv
import os
import re
import logging
from src.extractor.BaseExtractor import BaseExtractor
from src.factory import register_extractor
from src.utils.LanguageCodes import (
    identify_language_from_filename,
    generate_target_filename,
    normalize_language_code,
    get_english_name,
    is_language_column
)

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

@register_extractor("CSV")
class CSVExtractor(BaseExtractor):
    files_types = ['csv']
    _detected_delimiters = {}

    def __init__(self, translate):
        super().__init__(translate)
        self.columns_to_translate = ['value', 'text']
        self.csv_delimiter = ','
        self.skip_empty = True
        self.skip_headers = True
        self._file_modes = {}

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
            print(f"✓ Colunas configuradas para tradução: {', '.join(columns)}")
        else:
            print(f"⚠ Usando colunas padrão / detecção automática: {', '.join(self.columns_to_translate)}")

        if 'delimiter' in config and config['delimiter']:
            delimiter = config['delimiter']
            if delimiter == '\\t':
                delimiter = '\t'
            elif delimiter == '\\n':
                delimiter = '\n'
            self.csv_delimiter = delimiter
            print(f"✓ Delimitador configurado: '{delimiter}'")

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

    @classmethod
    def extract_files(cls, file_path):
        if not file_path.endswith(tuple(cls.files_types)):
            return [os.path.basename(file_path), None]

        base_name = os.path.basename(file_path)
        delimiter = detect_delimiter_from_file(file_path)
        cls._detected_delimiters[base_name] = delimiter
        cls._detected_delimiters[file_path] = delimiter

        try:
            with open(file_path, 'r', encoding='utf-8-sig', newline='', errors='replace') as f:
                reader = csv.DictReader(f, delimiter=delimiter)
                data = list(reader)
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

        mode_info = self._file_modes.get(file_name, {})
        mode = mode_info.get('mode', 'standard')
        source_col = mode_info.get('source_col')
        target_col = mode_info.get('target_col')
        text_cols = mode_info.get('text_cols', self.columns_to_translate)

        is_file_per_lang = self.get_target_filename(file_name) != file_name
        updated_data = []

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
                            if not is_file_per_lang:
                                if row[column] and row[column] != new_data[key]:
                                    updated_row[f"{column}_old"] = row[column]
                            updated_row[column] = new_data[key]
                updated_data.append(updated_row)

        return updated_data

    def import_file(self_or_cls, file_name, data, folder):
        if not data:
            return

        if isinstance(self_or_cls, CSVExtractor):
            target_name = self_or_cls.get_target_filename(file_name)
            delimiter = (
                self_or_cls._detected_delimiters.get(file_name) or
                self_or_cls._detected_delimiters.get(os.path.basename(file_name)) or
                self_or_cls.csv_delimiter
            )
        else:
            target_name = file_name
            delims = getattr(self_or_cls, '_detected_delimiters', {})
            delimiter = delims.get(file_name, ',')

        file_path = os.path.join(folder, target_name)
        os.makedirs(os.path.dirname(file_path), exist_ok=True)

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

            with open(file_path, 'w', encoding='utf-8-sig', newline='') as f:
                writer = csv.DictWriter(f, fieldnames=fieldnames, delimiter=delimiter, extrasaction='ignore')
                writer.writeheader()
                writer.writerows(data)
            logging.info(f"✓ Salvo CSV: {target_name} com delimitador '{delimiter}'")
        except Exception as e:
            logging.error(f"Erro ao salvar CSV {file_path}: {e}")

    @staticmethod
    def fix_text_translate(text, original_text=None):
        if isinstance(text, dict):
            for key, value in text.items():
                if isinstance(value, str):
                    text[key] = value.strip()
        return text
