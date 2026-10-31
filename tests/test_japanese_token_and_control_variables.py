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
        # Diálogo preserva aspas tipográficas (#37); segmento de código converte para ASCII
        dialogue_texts = ["Ele disse: “Normalmente” e ‘sim’."]
        fixed_dialogue = self.extractor.fix_text_translate(dialogue_texts)
        self.assertEqual(fixed_dialogue[0], "Ele disse: “Normalmente” e ‘sim’.")

        code_texts = ["if(v[1] == “sim” && s[2] != ‘nao’)"]
        fixed_code = self.extractor.fix_text_translate(code_texts)
        self.assertEqual(fixed_code[0], 'if(v[1] == "sim" && s[2] != \'nao\')')

    def test_numeric_placeholder_generation(self):
        ph0 = TextsUtils._generate_placeholder(0)
        ph1 = TextsUtils._generate_placeholder(1)
        self.assertEqual(ph0, "__XTOK_0__")
        self.assertEqual(ph1, "__XTOK_1__")
        digits = ph0[len("__XTOK_"):-len("__")]
        self.assertTrue(digits.isdigit(), f"Placeholder {ph0} should be numeric digits")

    def test_unmask_robust_to_spaces_and_case(self):
        text = "Sensibilidade __ XTOK_12345678 __ e mais __xtok_99887766__"
        mapping = {
            "__XTOK_12345678__": "\\V[59]",
            "__XTOK_99887766__": "\\C[0]"
        }
        restored = TextsUtils.unmask_tokens_in_structure(text, mapping)
        self.assertEqual(restored, "Sensibilidade \\V[59] e mais \\C[0]")

    def test_unmask_fuzzy_match_one_char_mutation(self):
        # Mutação de 1 caractere não deve restaurar outro token por engano (#38)
        text = "Sensibilidade __XTOK_00817FEV__"
        mapping = {
            "__XTOK_00817FEB__": "\\V[59]"
        }
        with self.assertRaises(ValueError):
            TextsUtils.unmask_tokens_in_structure(text, mapping)

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

    def test_show_text_speaker_tag_and_plugin_strategy_mz(self):
        from src.extractor.rpgmaker.RPGEventStrategy import ShowTextStrategy, PluginStrategyMZ
        cmd_tag = {"code": 401, "parameters": ["<\\N[1]> Hello world!"]}
        self.assertEqual(ShowTextStrategy().extract(cmd_tag), ["Hello world!"])

        cmd_chapter = {"code": 401, "parameters": ["<Chapter 1>"]}
        self.assertEqual(ShowTextStrategy().extract(cmd_chapter), ["Chapter 1"])

        cmd_guard = {"code": 401, "parameters": ["<Guard> Halt!"]}
        ShowTextStrategy().insert(cmd_guard, ["Guarda", "Alto!"])
        self.assertEqual(cmd_guard["parameters"][0], "<Guarda> Alto!")

        cmd_mz = {
            "code": 357,
            "parameters": [
                "QuestSystem", "Start", "Start",
                {"Text": "クエスト開始", "questTitle": "スライム討伐", "buttonLabel": "Inventory"}
            ]
        }
        res_mz = PluginStrategyMZ().extract(cmd_mz)
        self.assertEqual(list(res_mz.values()) if isinstance(res_mz, dict) else res_mz, ["クエスト開始", "スライム討伐", "Inventory"])

    def test_wolf_ruby_not_masked_into_xtok(self):
        from src.extractor.wolfrpg.WolfRPGExtractor import WolfRPGExtractor
        raw = r"\r[魔法,まほう]を使う"
        pats = WolfRPGExtractor(None).get_mask_patterns()
        masked, mp = TextsUtils.mask_tokens_in_structure(raw, pats)
        self.assertNotIn("__XTOK_0__", masked)
        self.assertIn("魔法", masked)

        unwrapped = WolfRPGExtractor.unwrap_ruby(raw)
        self.assertEqual(unwrapped, "魔法を使う")
        masked_unwrapped, mp2 = TextsUtils.mask_tokens_in_structure(unwrapped, pats)
        self.assertEqual(masked_unwrapped, "魔法を使う")
        self.assertEqual(len(mp2), 0)

    def test_identical_tokens_reuse_placeholder_indices(self):
        pats = RPGMakerExtractor(None).get_mask_patterns()
        input_list = [r"\C[2]Attack\C[0]", r"\C[2]Attack\C[0]", r"\C[2]Attack\C[0]"]
        masked, mp = TextsUtils.mask_tokens_in_structure(input_list, pats)
        self.assertEqual(len(set(masked)), 1)
        restored = TextsUtils.unmask_tokens_in_structure(masked, mp)
        self.assertEqual(restored, input_list)

    def test_repeated_tokens_in_single_string_preserves_translation(self):
        pats = RPGMakerExtractor(None).get_mask_patterns()
        s1 = r"\C[1]Red\C[0] and \C[1]Blue\C[0]"
        s2 = r"\C[1]Red\C[0] and \C[1]Blue\C[0]"
        input_list = [s1, s2]
        masked, mp = TextsUtils.mask_tokens_in_structure(input_list, pats)
        self.assertEqual(len(set(masked)), 1)
        translated = [m.replace("Red", "Vermelho").replace("Blue", "Azul").replace("and", "e") for m in masked]
        restored = TextsUtils.unmask_tokens_in_structure(translated, mp)
        expected = [r"\C[1]Vermelho\C[0] e \C[1]Azul\C[0]", r"\C[1]Vermelho\C[0] e \C[1]Azul\C[0]"]
        self.assertEqual(restored, expected)

    def test_unrolling_does_not_break_earlier_segment_sharing_placeholder(self):
        pats = RPGMakerExtractor(None).get_mask_patterns()
        src = [r"\V[1] coins", r"show_if(\V[1] > 0) Buy"]
        masked, mp = TextsUtils.mask_tokens_in_structure(src, pats)
        translated = [masked[0].replace("coins", "moedas"), masked[1].replace("Buy", "Comprar")]
        restored = TextsUtils.unmask_tokens_in_structure(translated, mp)
        self.assertEqual(restored, [r"\V[1] moedas", r"show_if(\V[1] > 0) Comprar"])

    def test_wolf_ruby_with_nested_format_tags(self):
        from src.extractor.wolfrpg.WolfRPGExtractor import WolfRPGExtractor
        s1 = r"\r[kanji,\C[1]ruby\C[0]]"
        s2 = r"\r[\C[1]kanji\C[0],ruby]"
        self.assertEqual(WolfRPGExtractor.unwrap_ruby(s1), "kanji")
        self.assertEqual(WolfRPGExtractor.unwrap_ruby(s2), r"\C[1]kanji\C[0]")
        fixed = WolfRPGExtractor.fix_text_translate([s1, s2])
        self.assertEqual(fixed, ["kanji", r"\c[1]kanji\c[0]"])


if __name__ == '__main__':
    unittest.main()



