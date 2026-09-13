import pytest
from unittest.mock import MagicMock, patch
import requests
from src.translate.GoogleTranslate import GoogleTranslate
from src.translate.BaseTranslate import BaseTranslate
from src.extractor.BaseExtractor import BaseExtractor
from src.services.ProxyManager import ProxyManager


def test_is_retriable_error_rejects_recursion_error():
    assert not BaseExtractor._is_retriable_error(RecursionError("maximum recursion depth exceeded"))


def test_mymemory_large_text_iterative_no_recursion():
    with patch('deep_translator.MyMemoryTranslator') as mock_mm_class:
        mock_instance = MagicMock()
        mock_instance.translate.side_effect = lambda t: f"TRAD_{t[:10]}"
        mock_mm_class.return_value = mock_instance

        gt = GoogleTranslate(lang_source="ja", lang_target="pt")
        # Texto com mais de 2000 caracteres
        large_text = "あ" * 2500

        res = gt._translate_with_mymemory(large_text)
        assert res is not None
        assert "TRAD_" in res
        # Garantir que foi chamado mais de 4 vezes para cobrir os 2500 caracteres em blocos de <= 450
        assert mock_instance.translate.call_count >= 5


def test_translate_with_deep_translator_does_not_modify_global_requests_get():
    orig_requests_get = requests.get

    with patch('src.translate.GoogleTranslate.GoogleTranslator') as mock_gt_class:
        mock_instance = MagicMock()
        mock_instance.translate.return_value = "Texto traduzido"
        mock_gt_class.return_value = mock_instance

        gt = GoogleTranslate(lang_source="ja", lang_target="pt")
        res = gt._translate_with_deep_translator("テスト")
        assert res == "Texto traduzido"

        # requests.get NUNCA pode ter sido alterado globalmente no módulo
        assert requests.get is orig_requests_get


def test_google_translate_fails_fast_when_429_and_proxy_disabled():
    pm = ProxyManager.get_instance()
    pm.set_enabled(False)

    gt = GoogleTranslate(lang_source="ja", lang_target="pt")

    with patch.object(gt, '_translate_raw', side_effect=Exception("HTTP 429 Too Many Requests")):
        with pytest.raises(RuntimeError) as exc_info:
            gt._translate_single_batch(["teste"])
        
        assert "IP bloqueado pelo Google (HTTP 429)" in str(exc_info.value)
        assert "pool de proxies está desativado" in str(exc_info.value)


class DummyTranslator(BaseTranslate):
    def translator(self, texts, progress_callback=None):
        return texts

    def _translate_single_batch(self, batch):
        return [f"TR_{t}" for t in batch]


def test_translate_batch_parallel_raises_fatal_error_when_all_fail():
    trans = DummyTranslator(char_limit=100)
    batches = [["text1"], ["text2"]]

    # Simular erro 429 em todos os lotes
    with patch.object(trans, '_translate_single_batch', side_effect=Exception("429 Too Many Requests")):
        with pytest.raises(Exception) as exc_info:
            trans.translate_batch_parallel(batches)
        assert "429" in str(exc_info.value)


def test_translate_batch_fallback_reports_progress():
    trans = DummyTranslator(char_limit=100)
    # Garante que os textos não estão no cache
    trans.cache = {}
    test_texts = ["unique_phrase_alpha_991", "unique_phrase_beta_992"]

    # Fazer com que o lote paralelo falhe com erro recuperável (tamanho incompatível ou None)
    with patch.object(trans, 'translate_batch_parallel', return_value=[None]):
        progress_calls = []

        def callback(current, total, eta_seconds=None, msg=None):
            progress_calls.append((current, total, msg))

        res = trans.translate_batch(test_texts, progress_callback=callback)
        assert res == ["TR_unique_phrase_alpha_991", "TR_unique_phrase_beta_992"]

        # Verificar se chamadas de progresso foram emitidas com mensagens de recuperação
        fallback_msgs = [call[2] for call in progress_calls if call[2] and "Recuperando lote" in call[2]]
        assert len(fallback_msgs) == 2
        assert "item 1/2" in fallback_msgs[0]
        assert "item 2/2" in fallback_msgs[1]

        final_msgs = [call[2] for call in progress_calls if call[2] and "finalizando" in call[2].lower()]
        assert len(final_msgs) >= 1
