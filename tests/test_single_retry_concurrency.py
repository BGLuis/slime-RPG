import threading
import time
import pytest
from src.extractor.Segment import Segment
from src.translate.Provider import Capabilities, TranslationProvider
from src.translate.TranslationEngine import TranslationEngine


class AlwaysFailingProvider:
    id = "always_fail"
    capabilities = Capabilities(max_request_chars=5000, max_items=10, native_batch=True)
    MAX_REQUESTS_SIMULTANEOUSLY = 4

    def __init__(self):
        self.call_count = 0
        self._lock = threading.Lock()

    def translate(self, segments, ctx=None):
        with self._lock:
            self.call_count += 1
        raise ConnectionError("Simulated persistent network outage")


class ConcurrencyTrackingProvider:
    id = "concurrency_tracker"
    capabilities = Capabilities(max_request_chars=1000, max_items=5, native_batch=True)
    MAX_REQUESTS_SIMULTANEOUSLY = 4

    def __init__(self, delay=0.03, mismatch=False):
        self.delay = delay
        self.mismatch = mismatch
        self._lock = threading.Lock()
        self.active_calls = 0
        self.peak_calls = 0
        self.total_calls = 0

    def translate(self, segments, ctx=None):
        with self._lock:
            self.active_calls += 1
            self.total_calls += 1
            if self.active_calls > self.peak_calls:
                self.peak_calls = self.active_calls

        time.sleep(self.delay)

        with self._lock:
            self.active_calls -= 1

        if self.mismatch and len(segments) > 1:
            # Returns fewer items to trigger per-item fallback
            return ["TR:single"]
        return [f"TR:{s.source}" for s in segments]


def test_always_failing_provider_calls_per_batch_le_5():
    """Acceptance criterion: With the always-failing fake provider, calls per batch are <= 5."""
    provider = AlwaysFailingProvider()
    engine = TranslationEngine(provider=provider, max_retries=5, base_delay=0.01, max_delay=0.05)

    batch = [
        Segment(id=f"id_{i}", source=f"Text {i}", context={'kind': 'dialogue'})
        for i in range(5)
    ]

    result = engine._execute_single_batch(batch)

    # Must give up and return fallback after <= 5 calls
    assert provider.call_count <= 5, f"Expected <= 5 calls, but got {provider.call_count}"
    assert result == [s.source for s in batch]


def test_peak_concurrency_never_exceeds_max_requests_simultaneously():
    """Acceptance criterion: A peak-concurrency counter never exceeds MAX_REQUESTS_SIMULTANEOUSLY."""
    provider = ConcurrencyTrackingProvider(delay=0.04)
    engine = TranslationEngine(provider=provider, max_concurrent_requests=4)

    # 10 separate batches of segments
    all_batches = [
        [Segment(id=f"id_{b}_{i}", source=f"Batch {b} Text {i}", context={'kind': 'dialogue'}) for i in range(3)]
        for b in range(10)
    ]

    threads = []
    for b in all_batches:
        t = threading.Thread(target=engine._execute_single_batch, args=(b,))
        threads.append(t)
        t.start()

    for t in threads:
        t.join()

    assert provider.peak_calls <= provider.MAX_REQUESTS_SIMULTANEOUSLY, (
        f"Peak calls ({provider.peak_calls}) exceeded ceiling ({provider.MAX_REQUESTS_SIMULTANEOUSLY})"
    )
    assert engine.peak_concurrency <= provider.MAX_REQUESTS_SIMULTANEOUSLY


def test_per_item_fallback_never_exceeds_concurrency_ceiling():
    """Acceptance criterion: Per-item fallback goes through the same semaphore and never bypasses concurrency."""
    # When mismatch is True, batch translation returns mismatched length, triggering per-item fallback
    provider = ConcurrencyTrackingProvider(delay=0.03, mismatch=True)
    engine = TranslationEngine(provider=provider, max_concurrent_requests=4)

    # Run multiple batches concurrently, each triggering per-item fallback
    all_batches = [
        [Segment(id=f"id_{b}_{i}", source=f"Text {b}_{i}", context={'kind': 'dialogue'}) for i in range(4)]
        for b in range(6)
    ]

    threads = []
    for b in all_batches:
        t = threading.Thread(target=engine._execute_single_batch, args=(b,))
        threads.append(t)
        t.start()

    for t in threads:
        t.join()

    assert provider.peak_calls <= provider.MAX_REQUESTS_SIMULTANEOUSLY, (
        f"Peak calls during per-item fallback ({provider.peak_calls}) exceeded ceiling ({provider.MAX_REQUESTS_SIMULTANEOUSLY})"
    )
    assert engine.peak_concurrency <= provider.MAX_REQUESTS_SIMULTANEOUSLY
