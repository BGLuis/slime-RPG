import unittest
from unittest.mock import MagicMock, patch
from src.services.ProxyManager import ProxyManager
from src.translate.DeeplTranslate import DeeplTranslate
from src.factory import TranslatorFactory


class TestProxyManagerAndDeepl(unittest.TestCase):

    def test_proxy_manager_singleton(self):
        pm1 = ProxyManager.get_instance()
        pm2 = ProxyManager.get_instance()
        self.assertIs(pm1, pm2)

    def test_proxy_manager_rotation(self):
        pm = ProxyManager.get_instance()
        pm.set_direct_blocked(True)
        with pm._pool_lock:
            pm._working_proxies.clear()
            pm._working_proxies.extend(["1.1.1.1:8080", "2.2.2.2:8080"])

        p1 = pm.get_proxy_dict()
        p2 = pm.get_proxy_dict()
        self.assertIn("1.1.1.1:8080", p1["http"])
        self.assertIn("2.2.2.2:8080", p2["http"])

        # Test failure removal
        pm.report_proxy_failure(p1)
        with pm._pool_lock:
            self.assertNotIn("1.1.1.1:8080", pm._working_proxies)

    def test_deepl_translator_registered_in_factory(self):
        available = TranslatorFactory.get_available()
        self.assertIn("deeplTranslator", available)
        self.assertIs(available["deeplTranslator"], DeeplTranslate)

    def test_deepl_language_mapping(self):
        dt = DeeplTranslate(lang_source="ja", lang_target="pt")
        self.assertEqual(dt._map_lang_code("ja", is_target=False), "JA")
        self.assertEqual(dt._map_lang_code("pt", is_target=True), "PT-BR")
        self.assertEqual(dt._map_lang_code("en", is_target=True), "EN-US")
        self.assertEqual(dt._map_lang_code("en", is_target=False), "EN")

    @patch("requests.post")
    def test_deepl_batch_translation(self, mock_post):
        mock_response = MagicMock()
        mock_response.status_code = 200
        mock_response.json.return_value = {
            "translations": [
                {"text": "Texto traduzido 1"},
                {"text": "Texto traduzido 2"}
            ]
        }
        mock_post.return_value = mock_response

        dt = DeeplTranslate(lang_source="ja", lang_target="pt")
        dt.api_key = "test_key:fx"

        res = dt._translate_single_batch(["texto1", "texto2"])
        self.assertEqual(res, ["Texto traduzido 1", "Texto traduzido 2"])
        self.assertEqual(dt._get_api_url(), "https://api-free.deepl.com/v2/translate")

    def test_proxy_manager_disable(self):
        pm = ProxyManager.get_instance()
        pm.set_enabled(False)
        self.assertFalse(pm.enabled)
        self.assertIsNone(pm.get_proxy_dict())

        # Test enable again
        pm.set_enabled(True)
        self.assertTrue(pm.enabled)

    def test_google_translate_interactive_question(self):
        from src.translate.GoogleTranslate import GoogleTranslate
        questions = GoogleTranslate.get_interactive_questions()
        self.assertEqual(len(questions), 1)
        self.assertEqual(questions[0]['key'], 'use_proxy_pool')
        self.assertIn('options', questions[0])


if __name__ == '__main__':
    unittest.main()
