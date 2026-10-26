from abc import ABC, abstractmethod
import copy
import os
import logging
import threading
import requests
from concurrent.futures import ThreadPoolExecutor, as_completed

from src.translate.TranslationMemory import TranslationMemory
from src.translate.Provider import Capabilities, TranslationProvider
from src.extractor.Segment import Segment
from src.utils.paths import get_app_dir, get_memory_db_path
from src.exceptions import (
    TranslationError,
    ProviderRateLimited,
    ProviderAuthError,
    ProviderUnavailable,
    MaskIntegrityError,
)


class BaseTranslate(ABC):
    agent = 'BaseTranslate'
    id = 'base'
    capabilities = Capabilities(max_request_chars=5000, max_items=None, native_batch=False)
    delimiter = '\n<span> </span>\n'
    char_limit = 5000
    CHAR_LIMIT_MIN = 1000
    CHAR_LIMIT_DECREMENT = 1000
    lang_source = 'en'
    lang_target = 'pt'
    cache_path_base = get_app_dir("data")
    MAX_REQUESTS_SIMULTANEOUSLY = 99

    # Teto de requisições simultâneas por classe de tradutor, compartilhado entre
    # todos os arquivos/threads de uma mesma execução (evita o produto
    # arquivos-em-paralelo x lotes-em-paralelo virar dezenas de requisições de uma vez).
    _request_semaphores = {}
    _request_semaphore_lock = threading.Lock()

    @classmethod
    def _get_concurrency_limit(cls):
        return cls.MAX_REQUESTS_SIMULTANEOUSLY

    @classmethod
    def _get_request_semaphore(cls):
        with BaseTranslate._request_semaphore_lock:
            key = cls.__name__
            sem = BaseTranslate._request_semaphores.get(key)
            if sem is None:
                base_limit = cls._get_concurrency_limit()
                limit = int(os.environ.get('TRANSLATE_MAX_CONCURRENT_REQUESTS', base_limit))
                sem = threading.Semaphore(max(1, limit))
                BaseTranslate._request_semaphores[key] = sem
            return sem

    def __init__(self, delimiter=None, char_limit=None, lang_source=None, lang_target=None):
        self.delimiter = delimiter if delimiter else self.__class__.delimiter
        self.char_limit = char_limit if char_limit else self.__class__.char_limit
        self.lang_source = lang_source if lang_source else self.__class__.lang_source
        self.lang_target = lang_target if lang_target else self.__class__.lang_target
        self.translate_client = None
        self.game_synopsis = None
        self._session_translations = {}
        self._failed_keys = set()
        self._load_cache_for_current_languages()

    def __deepcopy__(self, memo):
        new_obj = self.__class__.__new__(self.__class__)
        memo[id(self)] = new_obj
        for key, value in self.__dict__.items():
            if key in ('cache', 'cache_lock', '_session_translations', '_failed_keys', '_engine'):
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
        env_mem = os.environ.get("SLIME_MEMORY_DB")
        if env_mem:
            self.cache_path = get_memory_db_path()
        else:
            base = getattr(self, 'cache_path_base', getattr(self.__class__, 'cache_path_base', None))
            default_data_dir = get_app_dir("data")
            if base and os.path.abspath(base) != os.path.abspath(default_data_dir):
                self.cache_path = os.path.join(base, 'memory.db')
            else:
                self.cache_path = get_memory_db_path()
        self.cache = TranslationMemory(
            self.cache_path, self.lang_source, self.lang_target,
            engine=self.__class__.agent, game=getattr(self, 'game_name', None),
        )
        # `cache_lock` é mantido só por compatibilidade (e pelo teste que verifica
        # que o deepcopy por-arquivo o compartilha por identidade). O
        # TranslationMemory agora é dono de todo o seu locking: escritas serializam
        # no seu `_lock` interno e leituras usam conexões thread-local sem lock.
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
        Chunks texts based on capabilities (max_request_chars, max_items) or char_limit.
        Can be overridden by subclasses if different batching logic is needed.
        """
        batches = []
        current_batch = []
        current_length = 0
        caps = getattr(self, 'capabilities', None)
        max_chars = getattr(caps, 'max_request_chars', self.char_limit) if caps else self.char_limit
        max_items = getattr(caps, 'max_items', None) if caps else None

        for text in texts:
            text_len = len(text) + len(self.delimiter)
            item_limit_reached = max_items is not None and len(current_batch) >= max_items
            char_limit_reached = current_length + text_len >= max_chars and bool(current_batch)

            if item_limit_reached or char_limit_reached:
                batches.append(current_batch)
                current_batch = []
                current_length = 0

            current_batch.append(text)
            current_length += text_len

        if current_batch:
            batches.append(current_batch)

        return batches

    @staticmethod
    def _report_progress(progress_callback, current, total, eta_seconds=None, msg=None):
        if not progress_callback:
            return
        import inspect
        try:
            sig = inspect.signature(progress_callback)
            params = sig.parameters
            has_msg = 'msg' in params or any(p.kind == inspect.Parameter.VAR_KEYWORD for p in params.values())
            if has_msg and msg is not None:
                progress_callback(current, total, eta_seconds, msg=msg)
            elif len(params) >= 3 or any(p.kind == inspect.Parameter.VAR_POSITIONAL for p in params.values()):
                progress_callback(current, total, eta_seconds)
            else:
                progress_callback(current, total)
        except Exception:
            try:
                progress_callback(current, total, eta_seconds)
            except Exception:
                pass

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
            fatal_error = None
            for future in as_completed(future_to_index):
                batch_idx = future_to_index[future]
                try:
                    translated_batches[batch_idx] = future.result()
                except Exception as e:
                    translated_batches[batch_idx] = None
                    is_fatal = isinstance(e, (ProviderRateLimited, ProviderAuthError, RecursionError))
                    if is_fatal and fatal_error is None:
                        fatal_error = e
                    logging.warning(f"Erro ao traduzir lote {batch_idx + 1}/{len(batches)}: {e}")

                completed += 1
                if progress_callback:
                    elapsed = time.time() - start_time
                    avg_time = elapsed / completed if completed > 0 else 0
                    remaining = len(batches) - completed
                    eta = avg_time * remaining
                    self._report_progress(progress_callback, completed, len(batches), eta)

        # Se todos os lotes falharam e tivemos erro fatal de conexão/rate-limit/recursão, propaga diretamente
        if fatal_error is not None and all(b is None for b in translated_batches):
            raise fatal_error

        return translated_batches

    def translate_batch(self, texts, progress_callback=None):
        if texts is None:
            return None

        # 1. Identify what needs to be translated vs what is cached / non-translatable
        translated_texts = [None] * len(texts)
        cache_indices = []
        non_cached_indices = []
        none_indices = []  # Indices where input text is None

        # Um único lookup em lote em vez de 2 queries por texto sob lock global.
        has_bulk = hasattr(self.cache, 'lookup_many')
        cache_hits = {}
        if has_bulk:
            candidates = [t for t in texts if isinstance(t, str) and t.strip()]
            if candidates:
                cache_hits = self.cache.lookup_many(candidates)

        for i, text in enumerate(texts):
            if text is None:
                none_indices.append(i)
            elif not isinstance(text, str):
                translated_texts[i] = text
            elif not text.strip():
                translated_texts[i] = text
            elif text in cache_hits:
                translated_texts[i] = cache_hits[text]
                cache_indices.append(i)
            elif not has_bulk and text in self.cache:
                translated_texts[i] = self.cache[text]
                cache_indices.append(i)
            else:
                non_cached_indices.append(i)

        # Se todos os textos já estavam no cache ou são vazios/None, encerra sem processar lotes
        if not non_cached_indices:
            if progress_callback:
                self._report_progress(progress_callback, 1, 1, 0)
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
        failed_items_count = 0
        for batch_idx, batch_result in enumerate(translated_batches_results):
            original_batch = batches[batch_idx]
            if batch_result is None or len(batch_result) != len(original_batch):
                safe_results = []
                total_items = len(original_batch)
                for item_idx, text in enumerate(original_batch):
                    if progress_callback:
                        fallback_msg = f"Recuperando lote {batch_idx + 1}/{len(batches)} (item {item_idx + 1}/{total_items})"
                        self._report_progress(progress_callback, batch_idx + 1, len(batches), None, msg=fallback_msg)

                    if len(text) > self.char_limit:
                        # Chunk oversized strings to avoid persistent >5000 character errors
                        pieces = [text[j:j + self.char_limit] for j in range(0, len(text), self.char_limit)]
                        translated_pieces = []
                        for p in pieces:
                            try:
                                with semaphore:
                                    t = self._translate_single_batch([p])
                                translated_pieces.append(t[0] if t else p)
                            except Exception as e:
                                if isinstance(e, (ProviderRateLimited, ProviderAuthError, RecursionError)):
                                    raise
                                failed_items_count += 1
                                translated_pieces.append(p)
                        safe_results.append("".join(translated_pieces))
                    else:
                        try:
                            with semaphore:
                                single = self._translate_single_batch([text])
                            safe_results.append(single[0] if single else text)
                        except Exception as e:
                            if isinstance(e, (ProviderRateLimited, ProviderAuthError, RecursionError)):
                                raise
                            failed_items_count += 1
                            safe_results.append(text)
                translated_results.extend(safe_results)
            else:
                translated_results.extend(batch_result)

        self.last_failed_items = failed_items_count

        if progress_callback:
            self._report_progress(progress_callback, len(batches), len(batches), 0, msg="Gravando cache e finalizando...")

        # 4. Mapear resultados únicos e salvar no cache
        unique_to_translated = {}
        for idx, orig_text in enumerate(unique_non_cached):
            if idx < len(translated_results) and translated_results[idx] is not None:
                trans_text = translated_results[idx]
            else:
                trans_text = orig_text
            unique_to_translated[orig_text] = trans_text

        storable = [
            (k, v) for k, v in unique_to_translated.items()
            if isinstance(k, str) and k.strip()
            and "__xtok_" not in k.lower() and "__xtok_" not in str(v).lower()
            and not (self.lang_source != self.lang_target and k.strip() == v.strip())
        ]
        if hasattr(self.cache, 'store_many'):
            self.cache.store_many(storable)
        else:
            for k, v in storable:
                self.cache[k] = v

        # 5. Preencher translated_texts para todas as posições originais
        for idx in non_cached_indices:
            orig_text = texts[idx]
            translated_texts[idx] = unique_to_translated.get(orig_text, orig_text)

        # 6. Restore None values
        for index in none_indices:
            translated_texts[index] = None

        return translated_texts

    def translate(self, segments: list[Segment], ctx=None) -> list[str]:
        if not segments:
            return []
        texts = [s.source if hasattr(s, 'source') else str(s) for s in segments]
        res = self._translate_single_batch(texts)
        return res if res is not None else texts

    def translator(self, texts, progress_callback=None):
        import src.utils.TextsUtils as TextsUtils
        treated_text = TextsUtils.dictToList(texts)
        if hasattr(self, 'preprocess_text') and callable(getattr(self, 'preprocess_text')):
            self.preprocess_text(treated_text)
        translate_text = self.translate_batch(treated_text, progress_callback)
        if hasattr(self, 'postprocess_text') and callable(getattr(self, 'postprocess_text')):
            self.postprocess_text(translate_text)
        TextsUtils.interactive_item(texts, translate_text)
        return texts

