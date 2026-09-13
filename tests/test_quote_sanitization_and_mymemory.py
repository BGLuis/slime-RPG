import unittest
from unittest.mock import MagicMock, patch
from src.extractor.rpgmaker.RPGEventStrategy import ScriptStrategy
from src.translate.GoogleTranslate import GoogleTranslate


class TestQuoteSanitizationAndMyMemory(unittest.TestCase):

    def test_script_strategy_strips_redundant_quotes(self):
        code_str = '$gameScreen.OriginalMessage("ルミナ", "「（……）」", 0, 2);'
        # Translator returns quotes around the text
        translated_texts = ["Lumina", '"(...)"']
        res = ScriptStrategy.insert_script_string(code_str, iter(translated_texts))
        self.assertEqual(res, '$gameScreen.OriginalMessage("Lumina", "(...)", 0, 2);')

    def test_script_strategy_escapes_inner_quotes(self):
        code_str = '$gameScreen.OriginalMessage("ルミナ", "「はい」", 0, 2);'
        translated_texts = ["Lumina", 'Ela disse "olá"']
        res = ScriptStrategy.insert_script_string(code_str, iter(translated_texts))
        self.assertEqual(res, '$gameScreen.OriginalMessage("Lumina", "Ela disse \\"olá\\"", 0, 2);')

    def test_script_strategy_normalizes_curly_quotes(self):
        code_str = '$gameScreen.OriginalMessage("ルミナ", "「はい」", 0, 2);'
        translated_texts = ["Lumina", '“Texto entre aspas curvas”']
        res = ScriptStrategy.insert_script_string(code_str, iter(translated_texts))
        self.assertEqual(res, '$gameScreen.OriginalMessage("Lumina", "Texto entre aspas curvas", 0, 2);')

    def test_single_quote_delimiters(self):
        code_str = "$gameScreen.OriginalMessage('ルミナ', '「はい」', 0, 2);"
        translated_texts = ["Lumina", "'Texto'"]
        res = ScriptStrategy.insert_script_string(code_str, iter(translated_texts))
        self.assertEqual(res, "$gameScreen.OriginalMessage('Lumina', 'Texto', 0, 2);")

    @patch('deep_translator.MyMemoryTranslator')
    def test_mymemory_delimiter_batch_handling(self, mock_mm_class):
        mock_instance = MagicMock()
        mock_instance.translate.side_effect = lambda t: f"TRADUZIDO_{t}"
        mock_mm_class.return_value = mock_instance

        gt = GoogleTranslate(lang_source="ja", lang_target="pt")
        delimiter = gt.delimiter
        batch_text = f"texto1{delimiter}texto2{delimiter}texto3"

        res = gt._translate_with_mymemory(batch_text)
        expected = f"TRADUZIDO_texto1{delimiter}TRADUZIDO_texto2{delimiter}TRADUZIDO_texto3"
        self.assertEqual(res, expected)


if __name__ == '__main__':
    unittest.main()
