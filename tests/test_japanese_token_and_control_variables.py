import unittest
from unittest.mock import patch, MagicMock
import re
from src.extractor.rpgmaker.RPGMakerExtractor import RPGMakerExtractor
from src.extractor.rpgmaker.RPGEventCodes import RPGEventCode
from src.extractor.rpgmaker.RPGEventStrategy import ControlVariablesStrategy
import src.utils.TextsUtils as TextsUtils
from src.translate.BaseTranslate import BaseTranslate
from src.translate.GoogleTranslate import GoogleTranslate


class DummyTranslator:
    def __init__(self):
        self.cache = {}
        self.lang_source = 'ja'
        self.lang_target = 'pt'


class TestJapaneseTokenAndControlVariables(unittest.TestCase):
    def setUp(self):
        self.translator = DummyTranslator()
        self.extractor = RPGMakerExtractor(self.translator)

    def test_control_variables_difficulty_enums_ignored(self):
        strategy = ControlVariablesStrategy()
        
        # Difficulties like "Easy", "Normal", "Hard" with or without quotes/semicolons must NOT be extracted
        easy_cmd = {"code": 122, "parameters": [948, 948, 0, 4, '"Easy";']}
        normal_cmd = {"code": 122, "parameters": [948, 948, 0, 4, '"Normal";']}
        hard_cmd = {"code": 122, "parameters": [948, 948, 0, 4, '"Hard";']}
        
        self.assertIsNone(strategy.extract(easy_cmd))
        self.assertIsNone(strategy.extract(normal_cmd))
        self.assertIsNone(strategy.extract(hard_cmd))

    def test_control_variables_text_string_extracted_and_inserted_safely(self):
        strategy = ControlVariablesStrategy()
        
        # User-facing text stored in quotes
        text_cmd = {"code": 122, "parameters": [996, 996, 0, 4, "'戦いに少し経験があります(Normal)。'"]}
        extracted = strategy.extract(text_cmd)
        self.assertEqual(extracted, "戦いに少し経験があります(Normal)。")
        
        # When inserted with smart quotes, it MUST convert to ASCII quotes
        strategy.insert(text_cmd, "Tenho alguma “experiência” em combate (Normal).")
        inserted_val = text_cmd["parameters"][4]
        self.assertEqual(inserted_val, "'Tenho alguma \"experiência\" em combate (Normal).'")
        self.assertNotIn("“", inserted_val)
        self.assertNotIn("”", inserted_val)

    def test_smart_quotes_sanitized_in_fix_text_translate(self):
        dirty_texts = ["Ele disse: “Normalmente” e ‘sim’."]
        fixed = self.extractor.fix_text_translate(dirty_texts)
        self.assertEqual(fixed[0], 'Ele disse: "Normalmente" e \'sim\'.')

    def test_numeric_placeholder_generation(self):
        ph = TextsUtils._generate_placeholder()
        self.assertTrue(ph.startswith("__XTOK_"))
        self.assertTrue(ph.endswith("__"))
        # Must be 8 pure digits
        digits = ph[len("__XTOK_"):-len("__")]
        self.assertTrue(digits.isdigit(), f"Placeholder {ph} should be numeric digits")
        self.assertEqual(len(digits), 8)

    def test_unmask_robust_to_spaces_and_case(self):
        text = "Sensibilidade __ XTOK_12345678 __ e mais __xtok_99887766__"
        mapping = {
            "__XTOK_12345678__": "\\V[59]",
            "__XTOK_99887766__": "\\C[0]"
        }
        restored = TextsUtils.unmask_tokens_in_structure(text, mapping)
        self.assertEqual(restored, "Sensibilidade \\V[59] e mais \\C[0]")

    def test_unmask_fuzzy_match_one_char_mutation(self):
        # February -> fev (Google Translate altering 'b' to 'v')
        text = "Sensibilidade __XTOK_00817FEV__"
        mapping = {
            "__XTOK_00817FEB__": "\\V[59]"
        }
        restored = TextsUtils.unmask_tokens_in_structure(text, mapping)
        self.assertEqual(restored, "Sensibilidade \\V[59]")

    def test_create_batches_does_not_multiply_cjk_by_six(self):
        class ConcreteTranslate(BaseTranslate):
            def _translate_single_batch(self, texts): return texts
            def translator(self, texts, progress_callback=None): return texts

        trans = ConcreteTranslate(char_limit=1000)
        # 10 Japanese strings of 50 chars each = 500 chars (+ delimiter ~160 chars = ~660 chars)
        # In old code: 500 * 6 = 3000 chars, so it would break into 4 batches!
        # With fix: 660 < 1000, so it must fit into 1 batch!
        japanese_texts = ["これはテストです。魔法少女シルフィーナの翻訳。" * 2] * 10
        batches = trans._create_batches(japanese_texts)
        self.assertEqual(len(batches), 1)

    @patch('requests.Session.post')
    def test_google_translate_gtx_uses_post(self, mock_post):
        mock_resp = MagicMock()
        mock_resp.status_code = 200
        mock_resp.json.return_value = [[["Tradução", "翻訳", None, None, 1]]]
        mock_post.return_value = mock_resp

        gt = GoogleTranslate(lang_source='ja', lang_target='pt')
        result = gt._translate_gtx("翻訳")
        
        self.assertEqual(result, "Tradução")
        mock_post.assert_called_once()
        args, kwargs = mock_post.call_args
        self.assertEqual(args[0], 'https://translate.googleapis.com/translate_a/single')
        self.assertIn('data', kwargs)
        self.assertEqual(kwargs['data']['q'], '翻訳')
        self.assertEqual(kwargs['data']['sl'], 'ja')
        self.assertEqual(kwargs['data']['tl'], 'pt')

    def test_translate_batch_never_caches_xtok(self):
        class MockTranslate(BaseTranslate):
            def _translate_single_batch(self, texts):
                return [f"Tradução de {t}" for t in texts]
            def translator(self, texts, progress_callback=None):
                return texts

        trans = MockTranslate(lang_source='ja', lang_target='pt')
        trans.cache = {}
        
        # Batch containing masked text with __XTOK_
        texts = ["Texto limpo", "__XTOK_12345678__ texto mascarado"]
        trans.translate_batch(texts)
        
        # "__XTOK_" must NEVER be in cache keys or values!
        for k, v in trans.cache.items():
            self.assertNotIn("__xtok_", k.lower())
            self.assertNotIn("__xtok_", str(v).lower())
        self.assertIn("Texto limpo", trans.cache)


if __name__ == '__main__':
    unittest.main()
