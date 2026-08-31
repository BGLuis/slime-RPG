from abc import ABC, abstractmethod
import copy
import os
import threading
from concurrent.futures import ThreadPoolExecutor, as_completed

from src.translate.TranslationMemory import TranslationMemory


class BaseTranslate(ABC):
    agent = 'BaseTranslate'
    delimiter = '\n<span> </span>\n'
    char_limit = 5000
    CHAR_LIMIT_MIN = 1000
    CHAR_LIMIT_DECREMENT = 1000
    lang_source = 'en'
    lang_target = 'pt'
    cache_path_base = 'cache'
    MAX_REQUESTS_SIMULTANEOUSLY = 99

    # Teto de requisições simultâneas por classe de tradutor, compartilhado entre
    # todos os arquivos/threads de uma mesma execução (evita o produto
    # arquivos-em-paralelo x lotes-em-paralelo virar dezenas de requisições de uma vez).
    _request_semaphores = {}
    _request_semaphore_lock = threading.Lock()

    @classmethod
    def _get_request_semaphore(cls):
        with BaseTranslate._request_semaphore_lock:
            sem = BaseTranslate._request_semaphores.get(cls.__name__)
            if sem is None:
                limit = int(os.environ.get('TRANSLATE_MAX_CONCURRENT_REQUESTS', cls.MAX_REQUESTS_SIMULTANEOUSLY))
                sem = threading.Semaphore(max(1, limit))
                BaseTranslate._request_semaphores[cls.__name__] = sem
            return sem

    def __init__(self, delimiter=None, char_limit=None, lang_source=None, lang_target=None):
        self.delimiter = delimiter if delimiter else self.__class__.delimiter
        self.char_limit = char_limit if char_limit else self.__class__.char_limit
        self.lang_source = lang_source if lang_source else self.__class__.lang_source
        self.lang_target = lang_target if lang_target else self.__class__.lang_target
        self.translate_client = None
        self.game_synopsis = None
        self._load_cache_for_current_languages()

    def __deepcopy__(self, memo):
        new_obj = self.__class__.__new__(self.__class__)
        memo[id(self)] = new_obj
        for key, value in self.__dict__.items():
            if key in ('cache', 'cache_lock'):
                setattr(new_obj, key, value)
            else:
                setattr(new_obj, key, copy.deepcopy(value, memo))
        return new_obj

    @classmethod
    def from_default(cls):
        return cls()

    @classmethod
    def from_languages(cls, lang_source, lang_target):
        return cls(lang_source=lang_source, lang_target=lang_target)

    @classmethod
    def from_all(cls):
        return cls(BaseTranslate.delimiter, BaseTranslate.char_limit, BaseTranslate.lang_source, BaseTranslate.lang_target)

    def reduce_limite(self):
        if self.char_limit > self.__class__.CHAR_LIMIT_MIN:
            self.char_limit -= self.__class__.CHAR_LIMIT_DECREMENT
            return True

    def list_lang(self):
        return self.translate_client.get_supported_languages()

    def set_game_synopsis(self, synopsis):
        self.game_synopsis = synopsis.strip() if synopsis else None

    def get_game_synopsis(self):
        return self.game_synopsis

    def change_language(self, lang_source, lang_target):
        self.lang_source = lang_source
        self.lang_target = lang_target

        self.translate_client.target = self.lang_target
        self.translate_client.source = self.lang_source
        self._load_cache_for_current_languages()

    def _load_cache_for_current_languages(self):
        self.cache_path = f'{self.__class__.cache_path_base}/memory.db'
        self.cache = TranslationMemory(
            self.cache_path, self.lang_source, self.lang_target,
            engine=self.__class__.agent, game=getattr(self, 'game_name', None),
        )
        self.cache_lock = threading.Lock()

    def save_cache(self):
        # TranslationMemory já commita a cada store(); mantido só para não quebrar
        # quem chama translate.save_cache() esperando um flush explícito no final.
        with self.cache._lock:
            self.cache._conn.commit()


    @classmethod
    def requires_synopsis(cls):
        return False

    @classmethod
    def get_interactive_questions(cls):
        return []

    def apply_configuration(self, config):
        pass

    @abstractmethod
    def _translate_single_batch(self, texts):
        pass

    def _create_batches(self, texts):
        """
        Default implementation: chunks texts based on char_limit.
        Can be overridden by subclasses if different batching logic is needed.
        """
        import src.utils.TextsUtils as TextsUtils
        batches = []
        current_batch = []
        current_length = 0

        for text in texts:
            text_unicode = TextsUtils.convert_special_chars_to_unicode(text)
            text_limiter = text_unicode + self.delimiter
            
            # If adding this text exceeds the limit and we have a non-empty batch, start a new one
            if current_length + len(text_limiter) >= self.char_limit and current_batch:
                batches.append(current_batch)
                current_batch = []
                current_length = 0
            
            current_batch.append(text)
            current_length += len(text_limiter)

        if current_batch:
            batches.append(current_batch)
            
        return batches

    def translate_batch_parallel(self, batches, progress_callback=None):
        import time
        if not batches:
            return []

        max_workers = min(self.MAX_REQUESTS_SIMULTANEOUSLY, len(batches))

        translated_batches = [None] * len(batches)
        start_time = time.time()

        semaphore = self.__class__._get_request_semaphore()

        def _translate_with_limit(batch):
            with semaphore:
                return self._translate_single_batch(batch)

        with ThreadPoolExecutor(max_workers=max_workers) as executor:
            future_to_index = {
                executor.submit(_translate_with_limit, batch): idx
                for idx, batch in enumerate(batches)
            }

            completed = 0
            for future in as_completed(future_to_index):
                batch_idx = future_to_index[future]
                try:
                    translated_batches[batch_idx] = future.result()
                except Exception as e:
                    translated_batches[batch_idx] = None
                    print(f"Error translating batch {batch_idx}: {e}")
                
                completed += 1
                if progress_callback:
                    elapsed = time.time() - start_time
                    avg_time = elapsed / completed
                    remaining = len(batches) - completed
                    eta = avg_time * remaining
                    progress_callback(completed, len(batches), eta)

        return translated_batches

    def translate_batch(self, texts, progress_callback=None):
        if texts is None:
            return None

        # 1. Identify what needs to be translated vs what is cached / non-translatable
        translated_texts = [None] * len(texts)
        cache_indices = []
        non_cached_indices = []
        none_indices = [] # Indices where input text is None

        with self.cache_lock:
            for i, text in enumerate(texts):
                if text is None:
                    none_indices.append(i)
                elif not isinstance(text, str):
                    translated_texts[i] = text
                elif not text.strip():
                    translated_texts[i] = text
                elif text in self.cache:
                    translated_texts[i] = self.cache[text]
                    cache_indices.append(i)
                else:
                    non_cached_indices.append(i)

        # Se todos os textos já estavam no cache ou são vazios/None, encerra sem processar lotes
        if not non_cached_indices:
            if progress_callback:
                progress_callback(1, 1, 0)
            for index in none_indices:
                translated_texts[index] = None
            return translated_texts

        # 2. Deduplicar textos não-cacheados para evitar enviar repetidos ao provedor
        unique_non_cached = list(dict.fromkeys(texts[i] for i in non_cached_indices))

        # 3. Processar textos únicos em lotes paralelos
        batches = self._create_batches(unique_non_cached)
        translated_batches_results = self.translate_batch_parallel(batches, progress_callback)

        # Process batches and apply fallback ONLY to failed batches
        semaphore = self.__class__._get_request_semaphore()

        translated_results = []
        for batch_idx, batch_result in enumerate(translated_batches_results):
            original_batch = batches[batch_idx]
            if batch_result is None or len(batch_result) != len(original_batch):
                safe_results = []
                for text in original_batch:
                    if len(text) > self.char_limit:
                        # Chunk oversized strings to avoid persistent >5000 character errors
                        pieces = [text[j:j+self.char_limit] for j in range(0, len(text), self.char_limit)]
                        translated_pieces = []
                        for p in pieces:
                            try:
                                with semaphore:
                                    t = self._translate_single_batch([p])
                                translated_pieces.append(t[0] if t else p)
                            except Exception:
                                translated_pieces.append(p)
                        safe_results.append("".join(translated_pieces))
                    else:
                        try:
                            with semaphore:
                                single = self._translate_single_batch([text])
                            safe_results.append(single[0] if single else text)
                        except Exception:
                            safe_results.append(text)
                translated_results.extend(safe_results)
            else:
                translated_results.extend(batch_result)

        # 4. Mapear resultados únicos e salvar no cache
        unique_to_translated = {}
        with self.cache_lock:
            for idx, orig_text in enumerate(unique_non_cached):
                if idx < len(translated_results) and translated_results[idx] is not None:
                    trans_text = translated_results[idx]
                else:
                    trans_text = orig_text
                unique_to_translated[orig_text] = trans_text
                if isinstance(orig_text, str) and orig_text.strip():
                    self.cache[orig_text] = trans_text

        # 5. Preencher translated_texts para todas as posições originais
        for idx in non_cached_indices:
            orig_text = texts[idx]
            translated_texts[idx] = unique_to_translated.get(orig_text, orig_text)

        # 6. Restore None values
        for index in none_indices:
            translated_texts[index] = None

        return translated_texts


    @abstractmethod
    def translator(self, texts, progress_callback=None):
        pass
