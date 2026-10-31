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
from src.extractor.Segment import (
    Segment,
    SegmentList,
    copy_preserving_metadata,
    resolve_translation_map,
)
from src.extractor.FormatAdapter import (
    JsonDataDict,
    JsonFormatAdapter,
    get_registered_format_adapter,
    register_format_adapter,
)


class _ClassOrInstanceMethod:
    def __init__(self, func):
        self.func = func

    def __get__(self, instance, owner=None):
        target = owner if instance is None else instance

        def wrapper(*args, **kwargs):
            return self.func(target, *args, **kwargs)

        return wrapper


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
    format_adapters = ()
    _suppress_folder_creation = False

    @classmethod
    def _create_segment_instance(cls):
        try:
            BaseExtractor._suppress_folder_creation = True
            return cls(None)
        finally:
            BaseExtractor._suppress_folder_creation = False

    @classmethod
    def detect(cls, path):
        """
        Detecta se `path` (ou subpastas imediatas) pertence ao layout deste extrator.
        Retorna `DetectionResult` (ou dict compatível) ou `None` se não reconhecido.
        """
        return None

    def __init__(self, translate, workspace=None):
        self._workspace = workspace
        self._folderInput = None
        self._folderProcess = None
        self._folderOutput = None

        if not self._suppress_folder_creation:
            if workspace is None:
                self.create_directory_if_not_exists(self.folderProcess)
                self.create_directory_if_not_exists(self.folderInput)
                self.create_directory_if_not_exists(self.folderOutput)
            else:
                self.create_directory_if_not_exists(self.folderProcess)

        self.translate = translate
        self.game_name = None
        if workspace is not None and getattr(workspace, 'input_dir', None):
            base = os.path.basename(os.path.abspath(workspace.input_dir))
            if base.lower() in ('data', 'www', 'www/data') or not base:
                base = os.path.basename(os.path.dirname(os.path.abspath(workspace.input_dir)))
            self.game_name = base or 'default_game'
        if self.translate and self.game_name:
            if not getattr(self.translate, 'game_name', None):
                self.translate.game_name = self.game_name
            if hasattr(self.translate, 'cache') and self.translate.cache:
                self.translate.cache.game = self.game_name

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

    @property
    def translation_memory(self):
        return getattr(self.translate, 'cache', None)

    @property
    def _session_translations(self):
        if hasattr(self.translate, '_session_translations'):
            return self.translate._session_translations
        if not hasattr(self, '_session_trans_store'):
            self._session_trans_store = {}
        return self._session_trans_store

    @_session_translations.setter
    def _session_translations(self, val):
        if hasattr(self.translate, '_session_translations'):
            self.translate._session_translations = val
        self._session_trans_store = val

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
    def register_format_adapter(cls, adapter, extensions=None):
        return register_format_adapter(adapter, extensions=extensions)

    @classmethod
    def get_format_adapter(cls, file_name_or_path):
        lower = str(file_name_or_path).lower()

        declared = getattr(cls, 'format_adapters', None) or ()
        if isinstance(declared, dict):
            for ext, adp in declared.items():
                norm_ext = ext if str(ext).startswith('.') else f".{ext}"
                if lower.endswith(norm_ext.lower()):
                    return adp
        else:
            for adp in declared:
                exts = getattr(adp, 'extensions', ()) or ()
                for ext in exts:
                    norm_ext = ext if str(ext).startswith('.') else f".{ext}"
                    if lower.endswith(norm_ext.lower()):
                        return adp

        global_adapter = get_registered_format_adapter(lower)
        if global_adapter is not None:
            return global_adapter
        return JsonFormatAdapter

    @classmethod
    def extract_files(cls, file_path):
        if not file_path.endswith(tuple(cls.files_types)):
            return [os.path.basename(file_path), None]
        adapter = cls.get_format_adapter(file_path)
        data = adapter.load_file(file_path)
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
        if not getattr(self, 'game_name', None):
            folder_input = getattr(self, 'folderInput', self.__class__.folderInput)
            norm = os.path.abspath(folder_input)
            base = os.path.basename(norm)
            if base.lower() in ('data', 'www', 'www/data') or not base:
                base = os.path.basename(os.path.dirname(norm))
            self.game_name = base or 'default_game'
        if self.translate:
            if not getattr(self.translate, 'game_name', None):
                self.translate.game_name = self.game_name
            if hasattr(self.translate, 'cache') and self.translate.cache:
                self.translate.cache.game = self.game_name


    @staticmethod
    @abstractmethod
    def extract_text(file_name, data):
        pass

    @staticmethod
    @abstractmethod
    def update_json(file_name, data, new_data):
        pass

    @classmethod
    def _infer_file_name(cls, data):
        if hasattr(data, '_file_name') and getattr(data, '_file_name'):
            return getattr(data, '_file_name')
        return None

    @_ClassOrInstanceMethod
    def extract_segments(target, file_name, data):
        if not file_name:
            if hasattr(data, '_file_name') and getattr(data, '_file_name'):
                file_name = getattr(data, '_file_name')
            else:
                raise ValueError("extract_segments requires an explicit file_name")

        if hasattr(target, '_walk_or_apply_segments'):
            segments = target._walk_or_apply_segments(file_name, data, mode='extract')
        else:
            inst = target._create_segment_instance() if isinstance(target, type) else target
            raw_texts = inst.extract_text(file_name, copy_preserving_metadata(data)) or []
            segments = [
                Segment(
                    id=f"{file_name}#{idx}",
                    source=str(text),
                    context={'file': file_name, 'index': idx, 'kind': 'dialogue'},
                )
                for idx, text in enumerate(raw_texts)
            ]
        return SegmentList(segments, source_data=data, file_name=file_name)

    @_ClassOrInstanceMethod
    def apply_segments(target, file_name, data, translations):
        if not file_name:
            if hasattr(data, '_file_name') and getattr(data, '_file_name'):
                file_name = getattr(data, '_file_name')
            else:
                raise ValueError("apply_segments requires an explicit file_name")

        working_data = copy_preserving_metadata(data)
        segments = target.extract_segments(file_name, data)
        trans_map = resolve_translation_map(list(segments), translations)
        has_changes = any(trans_map.get(seg.id, seg.source) != seg.source for seg in segments)

        if hasattr(target, '_walk_or_apply_segments'):
            res = target._walk_or_apply_segments(file_name, working_data, mode='apply', translations=trans_map)
            if hasattr(res, '__dict__'):
                res._is_dirty = getattr(res, '_is_dirty', False) or has_changes
            elif hasattr(res, '_csv_meta') and isinstance(res._csv_meta, dict):
                res._csv_meta['is_dirty'] = res._csv_meta.get('is_dirty', False) or has_changes
            return res

        inst = target._create_segment_instance() if isinstance(target, type) else target
        ordered_texts = [trans_map.get(seg.id, seg.source) for seg in segments]
        res = inst.update_json(file_name, working_data, ordered_texts)
        if hasattr(res, '__dict__'):
            res._is_dirty = getattr(res, '_is_dirty', False) or has_changes
        elif hasattr(res, '_csv_meta') and isinstance(res._csv_meta, dict):
            res._csv_meta['is_dirty'] = res._csv_meta.get('is_dirty', False) or has_changes
        return res

    _QUOTE_SPLIT_PATTERN = re.compile(r"""('(?:\\.|[^'\\])*'|"(?:\\.|[^"\\])*"|`(?:\\.|[^`\\])*`)""")
    _OCTAL_LEADING_ZERO_PATTERN = re.compile(r"\b0+([0-9]+)\b")

    @classmethod
    def sanitize_octal_data(cls, data):
        """Sanitiza números com zeros à esquerda em trechos de código em estruturas de dados na memória."""
        from src.extractor.rpgmaker.RPGTextFilters import RPGTextFilters

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
                if RPGTextFilters.is_technical_or_code(s) and cls._OCTAL_LEADING_ZERO_PATTERN.search(s):
                    parts = cls._QUOTE_SPLIT_PATTERN.split(s)
                    for j in range(0, len(parts), 2):
                        parts[j] = cls._OCTAL_LEADING_ZERO_PATTERN.sub(r'\1', parts[j])
                    return ''.join(parts)
                return s
            return obj

        return walk(data)

    @classmethod
    def import_file(cls, file_name, json_data, folder):
        dest_path = os.path.join(folder, file_name)
        os.makedirs(os.path.dirname(dest_path), exist_ok=True)
        adapter = cls.get_format_adapter(file_name)
        adapter.save_file(
            dest_path,
            json_data,
            file_name=file_name,
            folder=folder,
            extractor_cls=cls,
            candidate_folders=[cls.folderInput, cls.folderProcess],
        )

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
                re.compile(r'!?(?<!\\)(?<![A-Za-z0-9_])(?:[vVsS]|ss|sv)\[\s*\d+\s*\]'),
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

        if getattr(self, '_aborted', False):
            folder_input = getattr(self, 'folderInput', self.__class__.folderInput)
            try:
                rel_path = os.path.relpath(file_path_str, folder_input)
            except Exception:
                rel_path = os.path.basename(file_path_str)
            self.add_threads_status({
                'file': rel_path,
                'status': 'erro',
                'msg': "Interrompido: limite de requisições excedido"
            })
            return

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
            try:
                import inspect
                sig = inspect.signature(self.is_translatable_file)
                can_trans = self.is_translatable_file(file_name, file_path_str) if len(sig.parameters) >= 2 else self.is_translatable_file(file_name)
            except Exception:
                can_trans = self.is_translatable_file(file_name)
            if not can_trans:
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

                from src.services.ProxyManager import ProxyManager
                from src.exceptions import ProviderRateLimited
                proxy_mgr = ProxyManager.get_instance()
                if isinstance(e, ProviderRateLimited) and proxy_mgr.direct_blocked and not proxy_mgr.enabled:
                    self._aborted = True
                    self.add_threads_status({
                        'file': rel_path,
                        'status': 'erro',
                        'msg': "Limite de requisições excedido. Ative proxies ou VPN."
                    })
                    return

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

                last_failed = getattr(translate, 'last_failed_items', 0)
                if last_failed == 0 and translatable_items:
                    translated_count = total_count
                else:
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
                try:
                    import inspect
                    sig = inspect.signature(self.is_translatable_file)
                    can_trans = self.is_translatable_file(file_name, file_path_str) if len(sig.parameters) >= 2 else self.is_translatable_file(file_name)
                except Exception:
                    can_trans = self.is_translatable_file(file_name)
                if not can_trans:
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
        target_files = getattr(self, 'target_files', None)
        for root, _, filenames in os.walk(folder_input):
            for filename in filenames:
                file_path = os.path.join(root, filename)
                if exts is None or filename.lower().endswith(exts):
                    rel = os.path.relpath(file_path, folder_input).replace('\\', '/')
                    if target_files is not None and rel not in target_files and filename not in target_files:
                        continue
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
            pending_files = []
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
                        pending_files.append((file, entry))

                if pending_files:
                    try:
                        from src.translate.TranslationEngine import TranslationEngine
                        from src.extractor.Segment import Segment
                        import src.utils.TextsUtils as TextsUtils

                        engine = TranslationEngine(
                            provider=self.translate,
                            memory=getattr(self.translate, 'cache', None),
                            game=getattr(self, 'game_name', None),
                        )
                        files_segments = {}
                        file_mask_records = {}
                        session_trans = self._session_translations
                        tm = self.translation_memory

                        for f, ent in pending_files:
                            if ent:
                                _r, _d, rtext, fn = ent
                            else:
                                fn = os.path.basename(f)
                                try:
                                    _, _d = self.extract_files(f)
                                    rtext = self.extract_text(fn, _d) if _d is not None else None
                                except Exception:
                                    rtext = None
                                    _d = None
                            if rtext:
                                strings = [s for s in TextsUtils.dictToList(rtext) if isinstance(s, str) and s.strip()]
                                if strings:
                                    if tm and hasattr(tm, 'lookup_many'):
                                        try:
                                            cached_hits = tm.lookup_many(strings)
                                            for orig, trans in cached_hits.items():
                                                if orig.strip() != str(trans).strip():
                                                    session_trans[orig] = trans
                                        except Exception as e:
                                            logging.debug(f"lookup_many error in _dispatch: {e}")

                                    misses = [s for s in strings if s not in session_trans]
                                    if misses:
                                        patterns = self._get_patterns(fn, _d)
                                        masked_strings, mask_map = TextsUtils.mask_tokens_in_structure(misses, patterns)
                                        file_mask_records[f] = (misses, masked_strings, mask_map, fn)
                                        files_segments[f] = [
                                            Segment(id=f"{fn}#{idx}", source=m_s, context={'kind': 'dialogue'})
                                            for idx, m_s in enumerate(masked_strings)
                                        ]

                        to_store_cache = []
                        if files_segments:
                            results_by_file = engine.translate_segments_across_files(files_segments)
                            if getattr(engine, 'circuit_open', False):
                                if self.translate is not None:
                                    setattr(self.translate, 'circuit_open', True)
                                self.circuit_open = True

                            for f, (orig_strings, masked_strings, mask_map, fn) in file_mask_records.items():
                                file_results = results_by_file.get(f, {})
                                translated_masked = [
                                    file_results.get(f"{fn}#{idx}", m_s)
                                    for idx, m_s in enumerate(masked_strings)
                                ]
                                unmasked = TextsUtils.unmask_tokens_in_structure(translated_masked, mask_map)
                                fixed = self.fix_text_translate(unmasked, orig_strings) if hasattr(self, 'fix_text_translate') else unmasked

                                for orig, trans in zip(orig_strings, fixed):
                                    if orig.strip() != str(trans).strip():
                                        session_trans[orig] = trans
                                        if "__xtok_" not in str(trans).lower():
                                            to_store_cache.append((orig, trans))

                        self._session_translations = session_trans

                        is_offline = (
                            getattr(self, 'memory_only', False)
                            or getattr(self, 'offline', False)
                            or getattr(self.translate, 'memory_only', False)
                            or getattr(self.translate, 'offline', False)
                        )
                        cache = getattr(self.translate, 'cache', None)
                        if not is_offline and cache and hasattr(cache, 'store_many') and to_store_cache:
                            cache.store_many(to_store_cache, game=getattr(self, 'game_name', None))
                    except Exception as e:
                        if 'engine' in locals() and getattr(engine, 'circuit_open', False):
                            if self.translate is not None:
                                setattr(self.translate, 'circuit_open', True)
                            self.circuit_open = True
                        logging.debug(f"Cross-file engine translation fallback to per-file: {e}")

                    for f, ent in pending_files:
                        if ent:
                            _rel, data, raw_text, _fname = ent
                            fut = self.executor.submit(self.process_file, f, data=data, raw_text=raw_text)
                        else:
                            fut = self.executor.submit(self.process_file, f)
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
