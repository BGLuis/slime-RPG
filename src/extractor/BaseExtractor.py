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

    @staticmethod
    def import_file(file_name, json_data, folder):
        try:
            # Validar se json_data é serializável antes de escrever
            json_string = json.dumps(json_data, ensure_ascii=False, separators=(',', ':'))

            # Validar se o JSON gerado pode ser recarregado (teste de integridade)
            json.loads(json_string)

            # Se validação passou, escrever arquivo
            with open(os.path.join(folder, file_name), 'w', encoding='utf-8') as f:
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

    def add_threads_status(self, status):
        status_state = status.get('status', 'info')
        file_path = status.get('file', 'Unknown File')
        msg = status.get('msg', '')
        
        # Loga no arquivo de forma amigável
        if status_state != 'process': # Ignora logs de "process" repetitivos para não poluir o arquivo
            logging.info(f"[{status_state.upper()}] {file_path} - {msg}")

        with self.threads_status_lock:
            self.threads_status = [s for s in self.threads_status if s['file'] != status['file']]
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

    def _handle_no_text(self, file_path_str, file_name, data):
        self.add_threads_status(
            {'file': file_path_str, 'status': 'ignore', 'msg': "No text to process"})
        self.import_file(file_name, data, self.folderOutput)

    def process_file(self, file, retries=6, delay=20, max_delay=300):
        translate = copy.deepcopy(self.translate)
        file_path_str = str(file)

        for attempt in range(retries):
            try:
                file_name, data = self.extract_files(file_path_str)
                if data is None:
                    self.add_threads_status({'file': file_path_str, 'status': 'erro', 'msg': "Formato não suportado ou arquivo vazio"})
                    return
                
                raw_text = self.extract_text(file_name, data)

                if not raw_text:
                    self._handle_no_text(file_path_str, file_name, data)
                    return

                translated_text = self._pipeline_translate(file_name, data, raw_text, translate, file_path_str)

                merged_data = self.merge_dicts_texts(translated_text, raw_text)

                updated_data = self.update_json(file_name, data, merged_data)
                self.import_file(file_name, updated_data, self.folderProcess)
                self.add_threads_status({'file': file_path_str, 'status': 'success', 'msg': "Processed successfully"})
                return

            except Exception as e:
                logging.error(f"Error processing file {file}: {e}")
                self.add_threads_status(
                    {'file': file_path_str, 'status': 'danger', 'msg': f"Error processing file {file}"})

                if attempt < retries - 1:
                    backoff = min(delay * (2 ** attempt), max_delay)
                    self.add_threads_status(
                        {'file': file_path_str, 'status': 'waiting', 'msg': f"Retrying in {backoff} seconds..."})
                    time.sleep(backoff)
                    translate.reduce_limite()
                else:
                    self.add_threads_status(
                        {'file': file_path_str, 'status': 'erro', 'msg': f"Failed to process after {retries}"})

    def process_files(self):
        import concurrent.futures
        # Limitar a concorrência para evitar travamentos (ex: 5 a 10)
        self.executor = concurrent.futures.ThreadPoolExecutor(max_workers=5)
        self.futures = []
        
        files = glob.glob(self.__class__.folderInput + '/*')
        
        # Pré-popula o status para que a UI reconheça arquivos na fila
        for file in files:
            self.add_threads_status({'file': file, 'status': 'waiting', 'msg': 'Na fila de processamento...'})
            
        # Inicia a execução controlada
        for file in files:
            future = self.executor.submit(self.process_file, file)
            self.futures.append(future)

    def import_files(self):
        # Aguarda todas as tarefas do pool terminarem
        if hasattr(self, 'executor') and self.executor:
            import concurrent.futures
            concurrent.futures.wait(self.futures)
            self.executor.shutdown(wait=True)
            self.futures = []

        for file in glob.glob(self.__class__.folderProcess + '/*'):
            file_name = os.path.basename(file)
            input_file = os.path.join(BaseExtractor.folderInput, file_name)
            if os.path.exists(input_file):
                process_data = self.extract_files(file)

                original_data = self.extract_files(input_file)
                original_json = original_data[1] if original_data[1] else None

                if original_json is None:
                    logging.warning(f"⚠️ fix_text_translate called WITHOUT original JSON for {file_name}")

                self.import_file(file_name, process_data[1], BaseExtractor.folderOutput)

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
