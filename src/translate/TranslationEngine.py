import logging
import random
import threading
import time
from concurrent.futures import ThreadPoolExecutor, as_completed
from typing import Any, Dict, List, Optional

from src.exceptions import (
    ProviderAuthError,
    ProviderRateLimited,
    ProviderUnavailable,
)
from src.extractor.Segment import Segment
from src.translate.Provider import Capabilities, TranslationProvider
from src.translate.TranslationMemory import TranslationMemory


class TranslationEngine:
    """
    Central translation engine providing:
    - Global segment queue & cross-file deduplication
    - Batching by provider capabilities
    - Translation memory integration with explicit lookup policy
    - Single retry layer with exponential backoff & jitter
    - Concurrency control capping peak concurrency to MAX_REQUESTS_SIMULTANEOUSLY
    - Circuit breaker for failing providers
    """

    def __init__(
        self,
        provider: TranslationProvider,
        memory: Optional[TranslationMemory] = None,
        prefer_engine: Optional[str] = None,
        scope: str = 'global',
        reuse_other_engines: bool = True,
        game: Optional[str] = None,
        max_retries: int = 5,
        base_delay: float = 0.5,
        max_delay: float = 30.0,
        max_concurrent_requests: Optional[int] = None,
    ):
        self.provider = provider
        self.memory = memory
        self.prefer_engine = prefer_engine or getattr(provider, 'id', getattr(provider, 'agent', None))
        self.scope = scope
        self.reuse_other_engines = reuse_other_engines
        self.game = game
        self.max_retries = max_retries
        self.base_delay = base_delay
        self.max_delay = max_delay
        self.memory_only = (
            getattr(provider, 'memory_only', False)
            or getattr(provider, 'offline', False)
        )

        if max_concurrent_requests is not None:
            self.max_concurrent_requests = max_concurrent_requests
        else:
            self.max_concurrent_requests = getattr(
                provider, 'MAX_REQUESTS_SIMULTANEOUSLY', 4
            )
        self.semaphore = threading.Semaphore(max(1, self.max_concurrent_requests))
        self._concurrency_lock = threading.Lock()
        self.active_concurrency = 0
        self.peak_concurrency = 0

        self.call_count = 0
        self.failed_call_count = 0
        self.consecutive_failures = 0
        self.circuit_open = False
        self.failure_threshold = max_retries

    def batch_segments(self, segments: List[Segment]) -> List[List[Segment]]:
        caps = getattr(self.provider, 'capabilities', None)
        max_chars = getattr(caps, 'max_request_chars', 5000) if caps else 5000
        max_items = getattr(caps, 'max_items', None) if caps else None
        delimiter = getattr(self.provider, 'delimiter', '\n<span> </span>\n')
        delim_len = len(delimiter)

        batches: List[List[Segment]] = []
        current_batch: List[Segment] = []
        current_length = 0

        for seg in segments:
            text = seg.source
            text_len = len(text) + delim_len

            item_limit_reached = max_items is not None and len(current_batch) >= max_items
            char_limit_reached = current_length + text_len >= max_chars and bool(current_batch)

            if item_limit_reached or char_limit_reached:
                batches.append(current_batch)
                current_batch = []
                current_length = 0

            current_batch.append(seg)
            current_length += text_len

        if current_batch:
            batches.append(current_batch)

        return batches

    def _execute_single_batch(self, batch: List[Segment], ctx: Optional[Any] = None) -> List[str]:
        if not batch:
            return []

        if self.circuit_open:
            raise ProviderUnavailable("Circuit breaker open: provider currently unavailable")

        last_error = None
        should_fallback = False

        for attempt in range(self.max_retries):
            with self.semaphore:
                with self._concurrency_lock:
                    self.active_concurrency += 1
                    if self.active_concurrency > self.peak_concurrency:
                        self.peak_concurrency = self.active_concurrency
                    self.call_count += 1
                try:
                    if hasattr(self.provider, 'translate'):
                        result = self.provider.translate(batch, ctx)
                    elif hasattr(self.provider, '_translate_single_batch'):
                        texts = [s.source for s in batch]
                        result = self.provider._translate_single_batch(texts)
                    else:
                        raise NotImplementedError("Provider does not implement translate")

                    if result is not None and len(result) == len(batch):
                        self.consecutive_failures = 0
                        return result
                    else:
                        # Divergência de itens: aciona fallback item a item fora deste permit
                        should_fallback = True
                        break
                except Exception as e:
                    last_error = e
                    self.failed_call_count += 1
                    self.consecutive_failures += 1
                    if self.consecutive_failures >= self.failure_threshold:
                        self.circuit_open = True
                        if self.provider is not None:
                            setattr(self.provider, 'circuit_open', True)

                    if isinstance(e, (ProviderAuthError, ProviderRateLimited)):
                        raise

                    if attempt < self.max_retries - 1:
                        backoff = min(self.max_delay, self.base_delay * (2 ** attempt)) + random.uniform(0, 0.05)
                        time.sleep(backoff)
                    else:
                        break
                finally:
                    with self._concurrency_lock:
                        self.active_concurrency -= 1

        if should_fallback:
            return self._fallback_per_item(batch, ctx)

        if last_error is not None:
            logging.warning(f"Lote de tradução falhou após {self.max_retries} tentativas: {last_error}")
        return [s.source for s in batch]

    def _fallback_per_item(self, batch: List[Segment], ctx: Optional[Any] = None) -> List[str]:
        results = []
        for seg in batch:
            if self.circuit_open:
                results.append(seg.source)
                continue
            with self.semaphore:
                with self._concurrency_lock:
                    self.active_concurrency += 1
                    if self.active_concurrency > self.peak_concurrency:
                        self.peak_concurrency = self.active_concurrency
                    self.call_count += 1
                try:
                    if hasattr(self.provider, 'translate'):
                        res = self.provider.translate([seg], ctx)
                    elif hasattr(self.provider, '_translate_single_batch'):
                        res = self.provider._translate_single_batch([seg.source])
                    else:
                        res = [seg.source]
                    results.append(res[0] if res else seg.source)
                except Exception as e:
                    if isinstance(e, (ProviderAuthError, ProviderRateLimited)):
                        raise
                    results.append(seg.source)
                finally:
                    with self._concurrency_lock:
                        self.active_concurrency -= 1
        return results

    def translate_segments_across_files(
        self,
        files_segments: Dict[str, List[Segment]],
        progress_callback: Optional[Any] = None,
        ctx: Optional[Any] = None,
    ) -> Dict[str, Dict[str, str]]:
        """
        Deduplica segmentos de todos os arquivos, consulta o cache em lote,
        agrupa textos únicos não-cacheados pelas capacidades do provedor,
        executa tradução respeitando concorrência e semáforo, armazena no cache
        com o game setado e mapeia os resultados de volta por arquivo.
        """
        all_segments: List[Segment] = []
        for segs in files_segments.values():
            all_segments.extend(segs)

        all_sources = [s.source for s in all_segments if s.source and s.source.strip()]

        cache_hits: Dict[str, str] = {}
        if self.memory and hasattr(self.memory, 'lookup_many'):
            switch_eng = getattr(self.provider, 'switch_engine', False) or getattr(self.memory, 'switch_engine', False)
            cache_hits = self.memory.lookup_many(
                all_sources,
                prefer_engine=self.prefer_engine,
                scope=self.scope,
                reuse_other_engines=self.reuse_other_engines,
                switch_engine=switch_eng,
            )

        unique_uncached = list(dict.fromkeys(s for s in all_sources if s not in cache_hits))

        synthetic_segments = [
            Segment(id=f"uniq_{idx}", source=text, context={'kind': 'dialogue'})
            for idx, text in enumerate(unique_uncached)
        ]
        batches = self.batch_segments(synthetic_segments)

        translated_map = dict(cache_hits)

        if batches:
            if self.memory_only:
                for b in batches:
                    for s in b:
                        translated_map[s.source] = s.source
            else:
                max_workers = min(self.max_concurrent_requests, len(batches))
                with ThreadPoolExecutor(max_workers=max_workers) as executor:
                    future_to_batch = {
                        executor.submit(self._execute_single_batch, b, ctx): b
                        for b in batches
                    }
                    for fut in as_completed(future_to_batch):
                        b = future_to_batch[fut]
                        try:
                            res = fut.result()
                        except Exception:
                            res = [s.source for s in b]
                        for s, trans in zip(b, res):
                            translated_map[s.source] = trans

            if not self.memory_only and self.memory and hasattr(self.memory, 'store_many'):
                storable = [
                    (s, trans) for s, trans in translated_map.items()
                    if s in unique_uncached and trans is not None
                    and "__xtok_" not in s.lower() and "__xtok_" not in str(trans).lower()
                    and not (s.strip() == str(trans).strip())
                ]
                if storable:
                    self.memory.store_many(storable, game=self.game)

        results_by_file: Dict[str, Dict[str, str]] = {}
        for file_key, segs in files_segments.items():
            results_by_file[file_key] = {
                s.id: translated_map.get(s.source, s.source)
                for s in segs
            }
            if progress_callback:
                progress_callback(file_key, len(segs))

        return results_by_file
