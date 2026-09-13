import unittest
import json
import re
from src.extractor.rpgmaker.RPGMakerExtractor import RPGMakerExtractor
from src.extractor.rpgmaker.RPGEventCodes import RPGEventCode
from src.extractor.rpgmaker.RPGEventStrategy import (
    ScriptStrategy, MovementRouteStrategy, RouteStepStrategy
)
import src.utils.TextsUtils as TextsUtils


class DummyTranslator:
    def __init__(self):
        self.cache = {}
        self.lang_source = 'ja'
        self.lang_target = 'pt'


class TestMap002AndCommonEventsExtraction(unittest.TestCase):
    def setUp(self):
        self.translator = DummyTranslator()
        self.extractor = RPGMakerExtractor(self.translator)

    def test_choice_condition_masking_and_unmasking(self):
        """Verifica que condições de escolha após caracteres japoneses ou símbolos
        são mascaradas de forma atômica e desmascaradas sem gerar tokens aninhados."""
        patterns = self.extractor.get_mask_patterns()
        strings = [
            '地下水路-最奥if(s[183])en(!s[4982])',
            '\\C[27]・・・？♡en(s[1265])',
            '夜まで休むif(s[2])',
            'お風呂につかるif(s[1])',
            '昼if(s[16])'
        ]

        masked, mapping = TextsUtils.mask_tokens_in_structure(strings, patterns, prefix='__XTOK_')

        # Nenhum valor no mapping pode conter outro placeholder
        for ph, orig in mapping.items():
            self.assertNotIn('__XTOK_', orig, f"Placeholder aninhado encontrado no mapping: {ph} -> {orig}")

        # Desmascaramento deve ser 100% fiel
        unmasked = TextsUtils.unmask_tokens_in_structure(masked, mapping)
        self.assertEqual(unmasked, strings)

    def test_mask_tokens_unrolls_swallowed_placeholders(self):
        """Testa o caso em que um padrão engole um token já existente no mapping."""
        # Se pattern 1 casa 'bar' e pattern 2 casa 'foobarbaz'
        p1 = re.compile(r'bar')
        p2 = re.compile(r'foo__XTOK_\d+__baz')

        s = ["foobarbaz"]
        masked, mapping = TextsUtils.mask_tokens_in_structure(s, [p1, p2], prefix='__XTOK_')

        # O mapping deve conter apenas foobarbaz, sem tokens aninhados
        for ph, val in mapping.items():
            self.assertNotIn('__XTOK_', val)

        unmasked = TextsUtils.unmask_tokens_in_structure(masked, mapping)
        self.assertEqual(unmasked, ["foobarbaz"])

    def test_original_message_extraction_and_insertion(self):
        """Testa extração e inserção de $gameScreen.OriginalMessage."""
        strategy = ScriptStrategy()

        # Com nome de personagem
        cmd_with_name = {
            'code': 355,
            'parameters': ['$gameScreen.OriginalMessage("ルミナ", "「（……）」", 0, 2);']
        }
        extracted1 = strategy.extract(cmd_with_name)
        self.assertEqual(extracted1, ["ルミナ", "「（……）」"])

        strategy.insert(cmd_with_name, ["Lumina", "「(...)」"])
        self.assertEqual(
            cmd_with_name['parameters'][0],
            '$gameScreen.OriginalMessage("Lumina", "「(...)」", 0, 2);'
        )

        # Sem nome de personagem (nome vazio não deve ser extraído nem consumir tokens)
        cmd_empty_name = {
            'code': 355,
            'parameters': ['$gameScreen.OriginalMessage("", "少女について教えてください。", 2, 2);']
        }
        extracted2 = strategy.extract(cmd_empty_name)
        self.assertEqual(extracted2, "少女について教えてください。")

        strategy.insert(cmd_empty_name, "Por favor, fale sobre a garota.")
        self.assertEqual(
            cmd_empty_name['parameters'][0],
            '$gameScreen.OriginalMessage("", "Por favor, fale sobre a garota.", 2, 2);'
        )

    def test_ticker_manager_extraction_and_insertion(self):
        """Testa extração e inserção de TickerManager.show."""
        strategy = ScriptStrategy()

        cmd = {
            'code': 355,
            'parameters': ["TickerManager.show('\\\\c[16]身体を休めました……');"]
        }
        extracted = strategy.extract(cmd)
        self.assertEqual(extracted, r"\\c[16]身体を休めました……")

        strategy.insert(cmd, r"\\c[16]O corpo descansou...")
        self.assertEqual(
            cmd['parameters'][0],
            "TickerManager.show('\\\\c[16]O corpo descansou...');"
        )

    def test_movement_route_strategy_codes_205_and_505(self):
        """Testa extração e reinserção de textos em rotas de movimento (códigos 205 e 505)."""
        route_strat = MovementRouteStrategy()
        step_strat = RouteStepStrategy()

        cmd_205 = {
            'code': 205,
            'parameters': [
                8,
                {
                    'list': [
                        {'code': 45, 'parameters': ["TickerManager.show('\\\\c[16]☆メインクエストクリア');"]},
                        {'code': 15, 'parameters': [30]},
                        {'code': 45, 'parameters': ["TickerManager.show('\\\\c[16]☆街でイベントが発生しました');"]},
                        {'code': 0}
                    ]
                }
            ]
        }

        cmd_505_1 = {
            'code': 505,
            'parameters': [
                {'code': 45, 'parameters': ["TickerManager.show('\\\\c[16]☆メインクエストクリア');"]}
            ]
        }

        # Extração de 205 (múltiplos passos)
        ext_205 = route_strat.extract(cmd_205)
        self.assertEqual(ext_205, [
            r"\\c[16]☆メインクエストクリア",
            r"\\c[16]☆街でイベントが発生しました"
        ])

        # Extração de 505 (passo individual)
        ext_505 = step_strat.extract(cmd_505_1)
        self.assertEqual(ext_505, r"\\c[16]☆メインクエストクリア")

        # Inserção de 205
        route_strat.insert(cmd_205, [
            r"\\c[16]☆Missão principal concluída",
            r"\\c[16]☆Novo evento na cidade"
        ])
        steps = cmd_205['parameters'][1]['list']
        self.assertEqual(steps[0]['parameters'][0], "TickerManager.show('\\\\c[16]☆Missão principal concluída');")
        self.assertEqual(steps[2]['parameters'][0], "TickerManager.show('\\\\c[16]☆Novo evento na cidade');")

        # Inserção de 505
        step_strat.insert(cmd_505_1, r"\\c[16]☆Missão principal concluída")
        self.assertEqual(
            cmd_505_1['parameters'][0]['parameters'][0],
            "TickerManager.show('\\\\c[16]☆Missão principal concluída');"
        )


if __name__ == '__main__':
    unittest.main()
