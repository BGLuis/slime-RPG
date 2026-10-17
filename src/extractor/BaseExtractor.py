import re
import copy
import glob
import json
import os
import shutil
import threading
import time
import logging
import requests
from abc import ABC, abstractmethod


class FolderProperty(property):
    """
    Descritor compatível com property para folderInput, folderProcess, folderOutput.
    Suporta resolução dinâmica a partir do Workspace (se configurado), além de acesso
    estático na classe e compatibilidade com monkeypatch e atribuição em testes legados.
    """
    def __init__(self, name, ws_attr, default_val):
        self.name = name
        self.ws_attr = ws_attr
        self.default_val = default_val
        super().__init__(self._fget, self._fset)

    def _fget(self, instance):
        if instance is None:
            return self.default_val
        if hasattr(instance, f"_{self.name}"):
            val = getattr(instance, f"_{self.name}")
            if val is not None:
                return val
        ws = getattr(instance, '_workspace', None)
        if ws is not None:
            return getattr(ws, self.ws_attr)
        return getattr(instance.__class__, f"_{self.name}_cls_val", self.default_val)

    def _fset(self, instance, value):
        if instance is None:
            return
        setattr(instance, f"_{self.name}", value)
        ws = getattr(instance, '_workspace', None)
        if ws is not None:
            setattr(ws, self.ws_attr, value)

    def __get__(self, instance, owner=None):
        if instance is None:
            return getattr(owner, f"_{self.name}_cls_val", self.default_val)
        return self._fget(instance)

    def __set__(self, instance, value):
        self._fset(instance, value)


class BaseExtractor(ABC):
    name = 'BaseExtractor'
    folderProcess = FolderProperty('folderProcess', 'process_dir', 'process')
    folderInput = FolderProperty('folderInput', 'input_dir', 'input')
    folderOutput = FolderProperty('folderOutput', 'output_dir', 'output')
    files_types = []

    def __init__(self, translate, workspace=None):
        self._workspace = workspace
        self._folderInput = None
        self._folderProcess = None
        self._folderOutput = None

        if workspace is None:
            self.create_directory_if_not_exists(self.folderProcess)
            self.create_directory_if_not_exists(self.folderInput)
            self.create_directory_if_not_exists(self.folderOutput)
        else:
            self.create_directory_if_not_exists(self.folderProcess)

        self.translate = translate
        self._threads_status_map = {}
        self.file_translation_stats = {}
        self.threads_status_lock = threading.Lock()
        self.futures = []
        self.executor = None
        self.observers = []

    @property
    def workspace(self):
        return self._workspace

    @workspace.setter
    def workspace(self, ws):
        self._workspace = ws
        if ws is not None:
            self._folderInput = ws.input_dir
            self._folderProcess = ws.process_dir
            self._folderOutput = ws.output_dir

    def add_observer(self, callback):
        """Padrão Observer: Adiciona um ouvinte para receber eventos do Extrator"""
        self.observers.append(callback)

    def notify_observers(self, event_name, data):
        """Notifica todos os ouvintes sobre uma mudança de estado"""
        for obs in self.observers:
            try:
                obs(event_name, data)
            except Exception as e:
                logging.error(f"Erro no observer: {e}")

    @staticmethod
    def create_directory_if_not_exists(directory):
        if not os.path.exists(directory):
            os.makedirs(directory)

    @staticmethod
    def clean_folder(folder):
        for file in glob.glob(folder + '/*'):
            if os.path.isdir(file):
                shutil.rmtree(file)
            else:
                os.remove(file)

    @classmethod
    def extract_files(cls, file_path):
        if not file_path.endswith(tuple(cls.files_types)):
            return [os.path.basename(file_path), None]
        if file_path.endswith('.rvdata2'):
            from src.extractor.rpgmaker.RVDataAdapter import RVDataAdapter
            data = RVDataAdapter.load_file(file_path)
            return [os.path.basename(file_path), data]
        if file_path.endswith(('.mps', '.dat', '.project')):
            from src.extractor.wolfrpg.WolfBinaryAdapter import WolfBinaryAdapter
            data = WolfBinaryAdapter.load_file(file_path)
            return [os.path.basename(file_path), data]
        with open(file_path, 'r', encoding='utf-8-sig') as f:
            data = json.load(f)
            return [os.path.basename(file_path), data]

    def init_folder(self=None):
        if self is not None and not isinstance(self, type):
            self.clean_folder(self.folderProcess)
            if getattr(self, '_workspace', None) is None:
                self.clean_folder(self.folderOutput)
        else:
            BaseExtractor.clean_folder(BaseExtractor.folderProcess)
            BaseExtractor.clean_folder(BaseExtractor.folderOutput)

    @classmethod
    def get_interactive_questions(cls):
        return []

    def apply_configuration(self, config):
        pass

    def prepare(self):
        """Hook executado antes do processamento de arquivos (ex: descompactação de contêineres)."""
        pass

    @staticmethod
    @abstractmethod
    def extract_text(file_name, data):
        pass

    @staticmethod
    @abstractmethod
    def update_json(file_name, data, new_data):
        pass

    @classmethod
    def sanitize_octal_data(cls, data):
        """Sanitiza números com zeros à esquerda em trechos de código em estruturas de dados na memória."""
        from src.extractor.rpgmaker.RPGTextFilters import RPGTextFilters
        num_leading_zero = re.compile(r"\b0+([0-9]+)\b")

        def walk(obj):
            if isinstance(obj, dict):
                for k, v in obj.items():
                    obj[k] = walk(v)
                return obj
            elif isinstance(obj, list):
                for i, v in enumerate(obj):
                    obj[i] = walk(v)
                return obj
            elif isinstance(obj, str):
                s = obj
                if RPGTextFilters.is_technical_or_code(s) and num_leading_zero.search(s):
                    def repl(m):
                        start, end = m.start(), m.end()
                        before = s[:start]
                        after = s[end:]
                        in_double = before.count('"') % 2 == 1 and after.count('"') % 2 == 1
                        in_single = before.count("'") % 2 == 1 and after.count("'") % 2 == 1
                        if in_double or in_single:
                            return m.group(0)
                        return m.group(1)

                    return num_leading_zero.sub(repl, s)
                return s
            return obj

        return walk(data)

    @classmethod
    def import_file(cls, file_name, json_data, folder):
        dest_path = os.path.join(folder, file_name)
        os.makedirs(os.path.dirname(dest_path), exist_ok=True)
        if file_name.endswith('.rvdata2'):
            try:
                from src.extractor.rpgmaker.RVDataAdapter import RVDataAdapter
                original_raw = getattr(json_data, '_raw_rvdata', None)
                if original_raw is None:
                    for candidate_folder in [cls.folderInput, cls.folderProcess]:
                        candidate = os.path.join(candidate_folder, file_name)
                        if os.path.exists(candidate):
                            import src.utils.RubyMarshal as r_marshal
                            with open(candidate, 'rb') as f:
                                original_raw = r_marshal.load(f)
                            break
                RVDataAdapter.save_file(dest_path, json_data, original_raw=original_raw)
                logging.info(f"✓ Validated and saved RVData2: {file_name}")
                return
            except Exception as e:
                logging.error(f"✗ Failed to save RVData2 {file_name}: {e}")
                raise

        if file_name.endswith(('.mps', '.dat', '.project')):
            try:
                from src.extractor.wolfrpg.WolfBinaryAdapter import WolfBinaryAdapter
                original_raw = getattr(json_data, '_raw_wolf', None)
                if original_raw is None:
                    for candidate_folder in [cls.folderInput, cls.folderProcess]:
                        candidate = os.path.join(candidate_folder, file_name)
                        if os.path.exists(candidate):
                            loaded = WolfBinaryAdapter.load_file(candidate)
                            original_raw = getattr(loaded, '_raw_wolf', None)
                            break
                WolfBinaryAdapter.save_file(dest_path, json_data, original_raw=original_raw)
                logging.info(f"✓ Validated and saved WOLF RPG binary: {file_name}")
                return
            except Exception as e:
                logging.error(f"✗ Failed to save WOLF RPG binary {file_name}: {e}")
                raise

        try:
            if isinstance(json_data, (dict, list)):
                json_data = cls.sanitize_octal_data(json_data)

            # Validar se json_data é serializável antes de escrever
            json_string = json.dumps(json_data, ensure_ascii=False, separators=(',', ':'))

            # Se validação passou, escrever arquivo diretamente sem releitura
            with open(dest_path, 'w', encoding='utf-8') as f:
                f.write(json_string)

            logging.info(f"✓ Validated and saved: {file_name}")
        except (TypeError, ValueError) as e:
            logging.error(f"✗ JSON validation failed for {file_name}: {e}")
            logging.error(f"  Data type: {type(json_data)}")
            raise Exception(f"Failed to create valid JSON for {file_name}: {e}")
        except Exception as e:
            logging.error(f"✗ Failed to write file {file_name}: {e}")
            raise

    @staticmethod
    @abstractmethod
    def fix_text_translate(text, original_text=None):
        pass

    @staticmethod
    def remove_old_keys(obj):
        if isinstance(obj, dict):
            return {k: BaseExtractor.remove_old_keys(v) for k, v in obj.items() if not k.endswith('_old')}
        elif isinstance(obj, list):
            return [BaseExtractor.remove_old_keys(i) for i in obj]
        else:
            return obj

    @staticmethod
    def merge_dicts_texts(dict1, dict2):
        if not isinstance(dict2, (dict, list)):
            return dict1

        if isinstance(dict2, list):
            merged_list = dict2.copy()
            for i, value in enumerate(dict1):
                if i >= len(merged_list):
                    merged_list.append(value)
                    continue

                if isinstance(value, str):
                    merged_list[i] = value
                elif isinstance(value, (dict, list)):
                    merged_list[i] = BaseExtractor.merge_dicts_texts(value, merged_list[i])
            return merged_list

        merged_dict = dict2.copy()
        haystack = dict1.items()
        for key, value in haystack:
            if isinstance(value, str):
                if key in merged_dict and merged_dict[key] != value:
                    merged_dict[f"{key}_old"] = merged_dict[key]
                merged_dict[key] = value
            elif isinstance(value, dict):
                merged_dict[key] = BaseExtractor.merge_dicts_texts(value, merged_dict.get(key, {}))
            elif isinstance(value, list):
                if key not in merged_dict:
                    merged_dict[key] = value
                    continue

                for i, item in enumerate(value):
                    if i >= len(merged_dict[key]):
                        merged_dict[key].append(item)
                        continue

                    if isinstance(item, (dict, list)):
                        merged_dict[key][i] = BaseExtractor.merge_dicts_texts(item, merged_dict[key][i])
                    else:
                        if merged_dict[key][i] != item:
                            if f"{key}_old" not in merged_dict:
                                merged_dict[f"{key}_old"] = []
                            merged_dict[f"{key}_old"].append(merged_dict[key][i])
                        merged_dict[key][i] = item
        return merged_dict

    def normalize_status_file(self, file_path):
        if not file_path or file_path == 'Unknown File':
            return file_path
        folders = [
            getattr(self, 'folderInput', getattr(self.__class__, 'folderInput', 'input')),
            getattr(self, 'folderProcess', getattr(self.__class__, 'folderProcess', 'process')),
            getattr(self, 'folderOutput', getattr(self.__class__, 'folderOutput', 'output')),
        ]
        norm = os.path.normpath(str(file_path))
        for folder in folders:
            try:
                if os.path.isabs(norm):
                    abs_folder = os.path.abspath(folder)
                    if os.path.commonpath([os.path.abspath(norm), abs_folder]) == abs_folder:
                        return os.path.normpath(os.path.relpath(norm, abs_folder))
                else:
                    norm_folder = os.path.normpath(folder)
                    if norm == norm_folder:
                        return norm
                    if norm.startswith(norm_folder + os.sep) or norm.startswith(norm_folder + "/"):
                        return os.path.normpath(os.path.relpath(norm, norm_folder))
            except Exception:
                continue
        return norm

    @property
    def threads_status(self):
        with self.threads_status_lock:
            return list(self._threads_status_map.values())

    @threads_status.setter
    def threads_status(self, val):
        with self.threads_status_lock:
            if isinstance(val, dict):
                self._threads_status_map = val.copy()
            elif isinstance(val, list):
                self._threads_status_map = {
                    self.normalize_status_file(s.get('file', '')): s for s in val
                }
            else:
                self._threads_status_map = {}

    def add_threads_status(self, status):
        status_state = status.get('status', 'info')
        raw_file = status.get('file', 'Unknown File')
        norm_file = self.normalize_status_file(raw_file)
        status['file'] = norm_file
        msg = status.get('msg', '')

        # Loga no arquivo de forma amigável
        if status_state != 'process':  # Ignora logs de "process" repetitivos para não poluir o arquivo
            logging.info(f"[{status_state.upper()}] {norm_file} - {msg}")

        with self.threads_status_lock:
            self._threads_status_map[norm_file] = status

        self.notify_observers('status_update', status)

    def _get_patterns(self, file_name, data):
        if hasattr(self, 'get_mask_patterns') and callable(getattr(self, 'get_mask_patterns')):
            patterns = self.get_mask_patterns(file_name, data)
        elif hasattr(self, 'mask_patterns'):
            patterns = self.mask_patterns
        else:
            patterns = [
                re.compile(r'\\[A-Za-z]{1,3}\s*\[[^\]]*\]', re.IGNORECASE),
                re.compile(r'\$game[a-zA-Z_]+\b', re.IGNORECASE),
                re.compile(r'\$\s*[a-z]+[A-Z][a-zA-Z]*'),
                re.compile(r'!?(?<!\\)\b[A-Za-z]{1,2}\s*\[\s*\d+\s*\]', re.IGNORECASE),
            ]

        if isinstance(patterns, (list, tuple)):
            compiled_patterns = []
            for p in patterns:
                if isinstance(p, str):
                    compiled_patterns.append(re.compile(p))
                else:
                    compiled_patterns.append(p)
            patterns = compiled_patterns
        else:
            patterns = [patterns]
        return patterns

    def _pipeline_translate(self, file_name, data, text, translate_instance, file_path_str):
        from src.pipeline import (
            TranslationContext, TranslationPipeline,
            GetPatternsStep, CacheLookupStep, MaskingStep, TranslationStep,
            UnmaskingStep, FixingStep, CacheStoreStep
        )

        context = TranslationContext(
            file_name=file_name,
            data=data,
            text=text,
            translate_instance=translate_instance,
            file_path_str=file_path_str,
            extractor=self
        )

        pipeline = TranslationPipeline()
        pipeline.add_step(CacheLookupStep()) \
                .add_step(GetPatternsStep()) \
                .add_step(MaskingStep()) \
                .add_step(TranslationStep()) \
                .add_step(UnmaskingStep()) \
                .add_step(FixingStep()) \
                .add_step(CacheStoreStep())

        return pipeline.execute(context)

    def _handle_no_text(self, file_path_str, file_name, data=None):
        folder_input = getattr(self, 'folderInput', self.__class__.folderInput)
        try:
            rel_path = os.path.relpath(file_path_str, folder_input)
        except Exception:
            rel_path = file_name
        dest_path = os.path.join(self.folderOutput, rel_path)
        self.add_threads_status(
            {'file': rel_path, 'status': 'ignore', 'msg': "No text to process"})
        if os.path.abspath(file_path_str) != os.path.abspath(dest_path):
            os.makedirs(os.path.dirname(dest_path), exist_ok=True)
            shutil.copy2(file_path_str, dest_path)

    @staticmethod
    def _is_retriable_error(exc):
        """Determina se uma exceção ocorrida durante a tradução é transitória e elegível para retry.

        Erros determinísticos (como erros de sintaxe, tipos, chaves ou problemas de I/O de arquivo)
        NÃO devem ser retentados pois nunca terão sucesso.
        """
        import json
        from src.exceptions import (
            ProviderAuthError,
            ProviderRateLimited,
            ProviderUnavailable,
            MaskIntegrityError,
        )

        if isinstance(exc, ProviderAuthError):
            return False

        if isinstance(exc, (ProviderRateLimited, ProviderUnavailable, MaskIntegrityError)):
            return True

        if isinstance(exc, requests.exceptions.HTTPError) or hasattr(exc, 'response'):
            resp = getattr(exc, 'response', None)
            status = getattr(resp, 'status_code', None) if resp is not None else getattr(exc, 'status_code', None)
            if status is not None:
                if status in (400, 401, 403, 404, 456):
                    return False
                if status in (408, 429, 500, 502, 503, 504):
                    return True

        NON_RETRIABLE_EXCEPTIONS = (
            TypeError,
            KeyError,
            IndexError,
            AttributeError,
            SyntaxError,
            NameError,
            ImportError,
            NotImplementedError,
            json.JSONDecodeError,
            FileNotFoundError,
            PermissionError,
            IsADirectoryError,
            RecursionError,
        )
        if isinstance(exc, NON_RETRIABLE_EXCEPTIONS):
            return False

        if isinstance(exc, (ConnectionError, TimeoutError, OSError, requests.exceptions.RequestException)):
            return True

        if isinstance(exc, ValueError):
            if isinstance(exc, MaskIntegrityError):
                return True
            msg = str(exc).lower()
            if "desmascaramento incompleto" in msg or "placeholder" in msg or "empty translation" in msg:
                return True
            return False

        return False

    def process_file(self, file, retries=5, delay=5, max_delay=60, data=None, raw_text=None):
        translate = copy.deepcopy(self.translate)
        file_path_str = str(file)
        folder_input = getattr(self, 'folderInput', self.__class__.folderInput)

        if not os.path.exists(file_path_str) and os.path.exists(os.path.join(folder_input, file_path_str)):
            file_path_str = os.path.join(folder_input, file_path_str)

        norm_file = os.path.normpath(file_path_str)
        norm_input = os.path.normpath(folder_input)
        if norm_file.startswith(norm_input + os.sep) or norm_file.startswith(norm_input + "/"):
            rel_path = os.path.relpath(norm_file, norm_input)
        elif os.path.isabs(norm_file):
            try:
                abs_input = os.path.abspath(folder_input)
                if os.path.commonpath([os.path.abspath(norm_file), abs_input]) == abs_input:
                    rel_path = os.path.relpath(norm_file, abs_input)
                else:
                    rel_path = os.path.basename(file_path_str)
            except Exception:
                rel_path = os.path.basename(file_path_str)
        else:
            rel_path = norm_file

        file_name = os.path.basename(file_path_str)

        if hasattr(self, 'is_translatable_file') and callable(getattr(self, 'is_translatable_file')):
            if not self.is_translatable_file(file_name):
                self._handle_no_text(file_path_str, rel_path, None)
                return

        # 1. Fase de Extração (determinística, local - sem retry)
        if data is None or raw_text is None:
            try:
                extracted_name, data = self.extract_files(file_path_str)
                if data is None:
                    self.add_threads_status({'file': rel_path, 'status': 'erro', 'msg': "Formato não suportado ou arquivo vazio"})
                    return

                raw_text = self.extract_text(file_name, data)
            except Exception as e:
                logging.error(f"Erro na extração do arquivo {file}: {e}")
                self.add_threads_status(
                    {'file': rel_path, 'status': 'erro', 'msg': f"Erro na extração: {e}"})
                return

        if not raw_text:
            self._handle_no_text(file_path_str, rel_path, data)
            return

        # 2. Fase de Tradução (rede/API - retry apenas para falhas transitórias)
        translated_text = None
        for attempt in range(retries):
            try:
                translated_text = self._pipeline_translate(file_name, data, raw_text, translate, rel_path)
                break
            except Exception as e:
                logging.error(f"Erro ao traduzir {file} (tentativa {attempt + 1}/{retries}): {e}")

                if not self._is_retriable_error(e):
                    self.add_threads_status(
                        {'file': rel_path, 'status': 'erro', 'msg': f"Erro não recuperável: {e}"})
                    return

                self.add_threads_status(
                    {'file': rel_path, 'status': 'danger', 'msg': f"Error processing file {file}"})

                if attempt < retries - 1:
                    backoff = min(delay * (2 ** attempt), max_delay)
                    self.add_threads_status(
                        {'file': rel_path, 'status': 'waiting', 'msg': f"Retrying in {backoff} seconds..."})
                    time.sleep(backoff)
                    translate.reduce_limite()
                else:
                    self.add_threads_status(
                        {'file': rel_path, 'status': 'erro', 'msg': f"Failed to process after {retries}"})
                    return

        # 3. Fase de Reconstrução e Gravação (local - sem retry de tradução)
        try:
            merged_data = self.merge_dicts_texts(translated_text, raw_text)
            updated_data = self.update_json(file_name, data, merged_data)
            self.import_file(rel_path, updated_data, self.folderProcess)

            import src.utils.TextsUtils as TextsUtils
            orig_list = TextsUtils.dictToList(raw_text)
            trans_list = TextsUtils.dictToList(translated_text)
            translatable_items = [
                (o, t) for o, t in zip(orig_list, trans_list)
                if isinstance(o, str) and o.strip()
            ]
            total_count = len(translatable_items)

            pipeline_stats = getattr(self, '_last_stats', None)
            if pipeline_stats and pipeline_stats[1] == total_count:
                translated_count = pipeline_stats[0]
            else:
                lang_src = getattr(translate, 'lang_source', None)
                lang_tgt = getattr(translate, 'lang_target', None)
                is_different_langs = bool(lang_src and lang_tgt and lang_src.lower() != lang_tgt.lower())
                cache = getattr(translate, 'cache', None)

                translated_count = 0
                for orig_str, trans_str in translatable_items:
                    orig_clean = orig_str.strip()
                    trans_clean = trans_str.strip() if isinstance(trans_str, str) else ""
                    if is_different_langs:
                        if orig_clean != trans_clean:
                            translated_count += 1
                        elif cache is not None:
                            hits = cache.lookup_many([orig_str]) if hasattr(cache, 'lookup_many') else {}
                            if orig_str in hits or (not hasattr(cache, 'lookup_many') and orig_str in cache):
                                translated_count += 1
                    else:
                        translated_count += 1

            self.file_translation_stats[rel_path] = (translated_count, total_count)

            if total_count > 0 and translated_count < total_count:
                self.add_threads_status({
                    'file': rel_path,
                    'status': 'partial',
                    'msg': f"Partially translated ({translated_count}/{total_count})"
                })
            else:
                self.add_threads_status({'file': rel_path, 'status': 'success', 'msg': "Processed successfully"})
        except Exception as e:
            logging.error(f"Erro ao salvar arquivo traduzido {file}: {e}")
            self.add_threads_status(
                {'file': rel_path, 'status': 'erro', 'msg': f"Erro ao salvar arquivo: {e}"})
            return

    def _prescan_file(self, file, folder_input):
        """Classifica um arquivo para o fast lane (100% no cache ou sem texto) ou fluxo normal.

        Retorna `(lane, entry)` onde `lane` é `'cached'`, `'empty'` ou `'pending'`.
        """
        import src.utils.TextsUtils as TextsUtils

        file_path_str = str(file)
        file_name = os.path.basename(file_path_str)
        try:
            try:
                rel_path = os.path.relpath(file_path_str, folder_input)
            except Exception:
                rel_path = file_name

            if hasattr(self, 'is_translatable_file') and callable(getattr(self, 'is_translatable_file')):
                if not self.is_translatable_file(file_name):
                    return 'empty', (rel_path, file_path_str, file_name, None)

            _, data = self.extract_files(file_path_str)
            if data is None:
                return 'pending', None

            raw_text = self.extract_text(file_name, data)
            if not raw_text:
                return 'empty', (rel_path, file_path_str, file_name, data)

            strings = [
                s for s in TextsUtils.dictToList(raw_text)
                if isinstance(s, str) and s.strip()
            ]
            if not strings:
                return 'empty', (rel_path, file_path_str, file_name, data)

            cache = getattr(self.translate, 'cache', None)
            if cache is not None and hasattr(cache, 'lookup_many'):
                hits = cache.lookup_many(strings)
                if all(s in hits for s in strings):
                    return 'cached', (rel_path, data, raw_text, file_name)

            return 'pending', (rel_path, data, raw_text, file_name)
        except Exception as e:
            logging.debug(f"Prescan: {file} vai para o fluxo normal ({e})")
            return 'pending', None

    def _finalize_cached_file(self, file, entry):
        """Fast lane: grava um arquivo 100% coberto pelo cache sem provedor.

        Reusa `data`/`raw_text` já extraídos na pré-varredura. O
        `_pipeline_translate` de um arquivo totalmente cacheado resolve tudo no
        `CacheLookupStep` (um `lookup_many`) e curto-circuita os demais passos.
        """
        rel_path, data, raw_text, file_name = entry
        try:
            translated_text = self._pipeline_translate(file_name, data, raw_text, self.translate, rel_path)
            merged_data = self.merge_dicts_texts(translated_text, raw_text)
            updated_data = self.update_json(file_name, data, merged_data)
            self.import_file(rel_path, updated_data, self.folderProcess)

            import src.utils.TextsUtils as TextsUtils
            orig_list = TextsUtils.dictToList(raw_text)
            total_count = sum(1 for s in orig_list if isinstance(s, str) and s.strip())
            self.file_translation_stats[rel_path] = (total_count, total_count)

            self.add_threads_status({'file': rel_path, 'status': 'success', 'msg': "Processed successfully"})
        except Exception as e:
            logging.error(f"Fast-lane falhou para {file}, reprocessando pelo fluxo normal: {e}")
            self.add_threads_status(
                {'file': rel_path, 'status': 'danger', 'msg': f"Fast-lane falhou, reprocessando {file}"})
            self.process_file(file)

    def _finalize_empty_file(self, file, entry):
        """Fast lane: copia instantaneamente arquivos que não possuem textos a traduzir."""
        rel_path, file_path_str, file_name, data = entry
        self._handle_no_text(file_path_str, rel_path, data)

    def process_files(self):
        self.prepare()
        import concurrent.futures

        slow_workers = max(1, int(os.environ.get('EXTRACTOR_MAX_WORKERS', 8)))
        fast_workers = max(1, int(os.environ.get('EXTRACTOR_FASTLANE_WORKERS', 16)))
        scan_workers = max(1, int(os.environ.get('EXTRACTOR_PRESCAN_WORKERS', 8)))

        folder_input = getattr(self, 'folderInput', self.__class__.folderInput)
        exts = tuple(f".{t.lower().lstrip('.')}" for t in self.files_types) if self.files_types else None

        files = []
        for root, _, filenames in os.walk(folder_input):
            for filename in filenames:
                file_path = os.path.join(root, filename)
                if exts is None or filename.lower().endswith(exts):
                    files.append(file_path)

        files.sort()

        # Pré-popula o status para que a UI reconheça arquivos na fila
        for file in files:
            rel_path = os.path.relpath(file, folder_input)
            self.add_threads_status({'file': rel_path, 'status': 'waiting', 'msg': 'Na fila de processamento...'})

        self.executor = concurrent.futures.ThreadPoolExecutor(max_workers=slow_workers)
        self.fast_executor = concurrent.futures.ThreadPoolExecutor(max_workers=fast_workers)
        self.futures = []
        self.fast_futures = []

        cache = getattr(self.translate, 'cache', None)
        if cache is None or not hasattr(cache, 'lookup_many'):
            for file in files:
                self.futures.append(self.executor.submit(self.process_file, file))
            return

        # Pré-varredura em streaming, num thread próprio para não bloquear o
        # retorno de process_files(): assim que um arquivo é classificado ele já é
        # despachado para a fila certa — arquivos 100% no cache vão para o
        # fast_executor e não ficam presos atrás dos que chamam o provedor no
        # executor lento. `_dispatch_lock` protege `futures`/`fast_futures` contra
        # a leitura concorrente feita por import_files().
        self._dispatch_lock = threading.Lock()

        def _dispatch():
            prescan_ex = concurrent.futures.ThreadPoolExecutor(max_workers=scan_workers)
            try:
                scan_futures = {
                    prescan_ex.submit(self._prescan_file, file, folder_input): file
                    for file in files
                }
                for scan_future in concurrent.futures.as_completed(scan_futures):
                    file = scan_futures[scan_future]
                    try:
                        lane, entry = scan_future.result()
                    except Exception as e:
                        logging.debug(f"Prescan falhou para {file}, fluxo normal ({e})")
                        lane, entry = 'pending', None
                    if lane == 'cached':
                        fut = self.fast_executor.submit(self._finalize_cached_file, file, entry)
                        with self._dispatch_lock:
                            self.fast_futures.append(fut)
                    elif lane == 'empty':
                        fut = self.fast_executor.submit(self._finalize_empty_file, file, entry)
                        with self._dispatch_lock:
                            self.fast_futures.append(fut)
                    else:
                        if entry:
                            _rel, data, raw_text, _fname = entry
                            fut = self.executor.submit(self.process_file, file, data=data, raw_text=raw_text)
                        else:
                            fut = self.executor.submit(self.process_file, file)
                        with self._dispatch_lock:
                            self.futures.append(fut)
            finally:
                prescan_ex.shutdown(wait=True)

        self._dispatch_thread = threading.Thread(target=_dispatch, name='prescan-dispatch', daemon=True)
        self._dispatch_thread.start()

    def wait_for_dispatch(self):
        """Bloqueia até a pré-varredura ter despachado todos os arquivos."""
        t = getattr(self, '_dispatch_thread', None)
        if t is not None:
            t.join()

    def import_files(self):
        import concurrent.futures
        # Garante que todos os arquivos já foram enfileirados antes de esperar
        self.wait_for_dispatch()
        # Aguarda as tarefas dos dois pools (fast lane + fluxo normal) terminarem
        lock = getattr(self, '_dispatch_lock', None)
        if lock:
            with lock:
                all_futures = list(getattr(self, 'futures', [])) + list(getattr(self, 'fast_futures', []))
        else:
            all_futures = list(getattr(self, 'futures', [])) + list(getattr(self, 'fast_futures', []))
        if all_futures:
            concurrent.futures.wait(all_futures)
        for ex_attr in ('executor', 'fast_executor'):
            ex = getattr(self, ex_attr, None)
            if ex:
                ex.shutdown(wait=True)
        self.futures = []
        self.fast_futures = []

        folder_process = getattr(self, 'folderProcess', self.__class__.folderProcess)
        folder_output = getattr(self, 'folderOutput', self.__class__.folderOutput)

        for root, _, filenames in os.walk(folder_process):
            for filename in filenames:
                file = os.path.join(root, filename)
                rel_path = os.path.relpath(file, folder_process)
                dest_path = os.path.join(folder_output, rel_path)
                if os.path.abspath(file) != os.path.abspath(dest_path):
                    os.makedirs(os.path.dirname(dest_path), exist_ok=True)
                    ws = getattr(self, '_workspace', None)
                    if ws is not None:
                        ws.atomic_replace(file, dest_path)
                    else:
                        tmp_dest = f"{dest_path}.tmp_{os.getpid()}_{threading.get_ident()}"
                        shutil.copy2(file, tmp_dest)
                        try:
                            os.replace(tmp_dest, dest_path)
                        except Exception:
                            if os.path.exists(tmp_dest):
                                try:
                                    os.remove(tmp_dest)
                                except Exception:
                                    pass
                            shutil.copy2(file, dest_path)

    def sanitize_output_files(self):
        """
        Mantido para compatibilidade retroativa. A sanitização de literais octais
        agora é executada em memória antes da serialização, eliminando re-leituras em disco.
        """
        pass
