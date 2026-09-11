import re
import copy
import glob
import json
import os
import shutil
import threading
import time
import logging
from abc import ABC, abstractmethod

class BaseExtractor(ABC):
    name = 'BaseExtractor'
    folderProcess = 'process'
    folderInput = 'input'
    folderOutput = 'output'
    files_types = []

    def __init__(self, translate):
        self.create_directory_if_not_exists(self.__class__.folderProcess)
        self.create_directory_if_not_exists(self.__class__.folderInput)
        self.create_directory_if_not_exists(self.__class__.folderOutput)
        self.translate = translate
        self.threads_status = []
        self.threads_status_lock = threading.Lock()
        self.futures = []
        self.executor = None
        self.observers = []

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

    @staticmethod
    def init_folder():
        BaseExtractor.clean_folder(BaseExtractor.folderProcess)
        BaseExtractor.clean_folder(BaseExtractor.folderOutput)

    @classmethod
    def get_interactive_questions(cls):
        return []

    def apply_configuration(self, config):
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
            # Validar se json_data é serializável antes de escrever
            json_string = json.dumps(json_data, ensure_ascii=False, separators=(',', ':'))

            # Validar se o JSON gerado pode ser recarregado (teste de integridade)
            json.loads(json_string)

            # Se validação passou, escrever arquivo
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

    def add_threads_status(self, status):
        status_state = status.get('status', 'info')
        raw_file = status.get('file', 'Unknown File')
        norm_file = self.normalize_status_file(raw_file)
        status['file'] = norm_file
        msg = status.get('msg', '')
        
        # Loga no arquivo de forma amigável
        if status_state != 'process': # Ignora logs de "process" repetitivos para não poluir o arquivo
            logging.info(f"[{status_state.upper()}] {norm_file} - {msg}")

        with self.threads_status_lock:
            self.threads_status = [
                s for s in self.threads_status 
                if self.normalize_status_file(s.get('file', '')) != norm_file
            ]
            self.threads_status.append(status)
            snapshot = list(self.threads_status)

        self.notify_observers('status_update', snapshot)

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

    def process_file(self, file, retries=6, delay=20, max_delay=300):
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

        for attempt in range(retries):
            try:
                extracted_name, data = self.extract_files(file_path_str)
                if data is None:
                    self.add_threads_status({'file': rel_path, 'status': 'erro', 'msg': "Formato não suportado ou arquivo vazio"})
                    return
                
                raw_text = self.extract_text(file_name, data)

                if not raw_text:
                    self._handle_no_text(file_path_str, rel_path, data)
                    return

                translated_text = self._pipeline_translate(file_name, data, raw_text, translate, rel_path)

                merged_data = self.merge_dicts_texts(translated_text, raw_text)

                updated_data = self.update_json(file_name, data, merged_data)
                self.import_file(rel_path, updated_data, self.folderProcess)
                self.add_threads_status({'file': rel_path, 'status': 'success', 'msg': "Processed successfully"})
                return

            except Exception as e:
                logging.error(f"Error processing file {file}: {e}")
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

    def _prescan_file(self, file, folder_input):
        """Classifica um arquivo para o fast lane (100% no cache) ou fluxo normal.

        Retorna `(lane, entry)` onde `lane` é `'cached'` ou `'pending'` e `entry`
        é `(rel_path, data, raw_text, file_name)` quando `cached` (para o fast
        lane não reextrair), senão `None`.
        """
        import src.utils.TextsUtils as TextsUtils

        cache = getattr(self.translate, 'cache', None)
        if cache is None or not hasattr(cache, 'lookup_many'):
            return 'pending', None

        file_path_str = str(file)
        file_name = os.path.basename(file_path_str)
        try:
            if hasattr(self, 'is_translatable_file') and callable(getattr(self, 'is_translatable_file')):
                if not self.is_translatable_file(file_name):
                    return 'pending', None
            _, data = self.extract_files(file_path_str)
            if data is None:
                return 'pending', None
            raw_text = self.extract_text(file_name, data)
            if not raw_text:
                return 'pending', None
            strings = [
                s for s in TextsUtils.dictToList(raw_text)
                if isinstance(s, str) and s.strip()
            ]
            if not strings:
                return 'pending', None
            hits = cache.lookup_many(strings)
            if all(s in hits for s in strings):
                try:
                    rel_path = os.path.relpath(file_path_str, folder_input)
                except Exception:
                    rel_path = file_name
                return 'cached', (rel_path, data, raw_text, file_name)
            return 'pending', None
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
            self.add_threads_status({'file': rel_path, 'status': 'success', 'msg': "Processed successfully"})
        except Exception as e:
            logging.error(f"Fast-lane falhou para {file}, reprocessando pelo fluxo normal: {e}")
            self.add_threads_status(
                {'file': rel_path, 'status': 'danger', 'msg': f"Fast-lane falhou, reprocessando {file}"})
            self.process_file(file)

    def process_files(self):
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
        folder_input = getattr(self, 'folderInput', self.__class__.folderInput)
        folder_output = getattr(self, 'folderOutput', self.__class__.folderOutput)

        for root, _, filenames in os.walk(folder_process):
            for filename in filenames:
                file = os.path.join(root, filename)
                rel_path = os.path.relpath(file, folder_process)
                input_file = os.path.join(folder_input, rel_path)
                if os.path.exists(input_file):
                    process_data = self.extract_files(file)

                    original_data = self.extract_files(input_file)
                    original_json = original_data[1] if original_data[1] else None

                    if original_json is None:
                        logging.warning(f"⚠️ fix_text_translate called WITHOUT original JSON for {rel_path}")

                    self.import_file(rel_path, process_data[1], folder_output)

    def sanitize_output_files(self):
        """
        Varre os arquivos JSON em `folderOutput` e remove números com zeros à esquerda
        que aparecem em trechos de código (podem se tornar literais octais em JS strict).

        Heurística:
        - Percorre todos os valores string dentro do JSON.
        - Para cada string que contém padrões parecidos com código (if(, var , function, =>, mes =, text =)
          substitui tokens numéricos com zeros à esquerda por sua forma sem zeros.
        - Não altera números que estão dentro de literais de string (detectado por contagem de aspas simples/duplas).
        """
        import json
        import re
        from pathlib import Path

        code_triggers = ['if(', 'var ', 'return', '=>', 'function', 'mes =', 'text =']
        num_leading_zero = re.compile(r"\b0+([0-9]+)\b")

        for p in Path(self.folderOutput).glob('*.json'):
            try:
                data = json.load(p.open('r', encoding='utf-8'))
            except Exception:
                continue

            changed = False

            def walk(obj):
                nonlocal changed
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
                    if any(t in s for t in code_triggers) and num_leading_zero.search(s):
                        # substituir apenas ocorrências que não estejam dentro de aspas
                        def repl(m):
                            start, end = m.start(), m.end()
                            # heurística: se número estiver dentro de aspas na string, não substituir
                            before = s[:start]
                            after = s[end:]
                            in_double = before.count('"') % 2 == 1 and after.count('"') % 2 == 1
                            in_single = before.count("'") % 2 == 1 and after.count("'") % 2 == 1
                            if in_double or in_single:
                                return m.group(0)
                            else:
                                changed_local = True
                                return m.group(1)

                        new_s = num_leading_zero.sub(repl, s)
                        if new_s != s:
                            changed = True
                            return new_s
                    return s
                else:
                    return obj

            new_data = walk(data)
            if changed:
                try:
                    with p.open('w', encoding='utf-8') as fh:
                        json.dump(new_data, fh, ensure_ascii=False, separators=(',', ':'))
                except Exception as e:
                    logging.error(f"Failed to write sanitized file {p}: {e}")
