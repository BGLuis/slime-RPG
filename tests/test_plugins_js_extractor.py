import unittest
import json
from src.extractor.rpgmaker.RPGMakerExtractor import RPGMakerExtractor, PluginParametersProcessor
from src.extractor.FormatAdapter import PluginJsFormatAdapter
import src.utils.TextsUtils as TextsUtils


class TestPluginsJsExtractor(unittest.TestCase):
    def setUp(self):
        self.sample_plugins = [
            {
                "name": "Community_Basic",
                "status": True,
                "description": "Basic params",
                "parameters": {"screenHeight": "1080", "renderingMode": "auto"}
            },
            {
                "name": "CustomizeConfigItem",
                "status": True,
                "description": "Menu de opções customizadas",
                "parameters": {
                    "文字項目": json.dumps([
                        json.dumps({
                            "Name": "主人公名変更",
                            "DefaultValue": "0",
                            "VariableID": "0",
                            "Script": "SceneManager.push(Scene_Name);"
                        }),
                        json.dumps({
                            "Name": "主人公の見た目",
                            "StringItems": json.dumps(["ノーマル", "種付けマスター"])
                        })
                    ]),
                    "スイッチ項目": json.dumps([
                        json.dumps({
                            "Name": "エロシーンピストンオート",
                            "SwitchID": "20"
                        }),
                        json.dumps({
                            "Name": "断面図表示",
                            "SwitchID": "37"
                        })
                    ]),
                    "音量項目": json.dumps([
                        json.dumps({
                            "Name": "喘ぎ声 音量",
                            "VariableID": "81"
                        })
                    ])
                }
            },
            {
                "name": "Vitsuno_Difficulty",
                "status": True,
                "description": "Dificuldade",
                "parameters": {
                    "Option Name": "難易度",
                    "1 : Name": "イージー",
                    "2 : Name": "ノーマル",
                    "3 : Name": "ハード"
                }
            },
            {
                "name": "InactivePlugin",
                "status": False,
                "description": "Desativado",
                "parameters": {"Option": "無視されるべきテキスト"}
            }
        ]

    def test_is_translatable_file_plugins_js(self):
        extractor = RPGMakerExtractor(None)
        self.assertTrue(extractor.is_translatable_file("plugins.js"))
        self.assertTrue(extractor.is_translatable_file("www/js/plugins.js"))
        self.assertFalse(extractor.is_translatable_file("other_script.js"))

    def test_extract_and_insert_text_plugins_js(self):
        extractor = RPGMakerExtractor(None)
        extracted = extractor.extract_text("plugins.js", self.sample_plugins)

        self.assertIn("CustomizeConfigItem", extracted)
        self.assertIn("Vitsuno_Difficulty", extracted)
        self.assertNotIn("InactivePlugin", extracted)

        strings = TextsUtils.dictToList(extracted)
        self.assertIn("主人公名変更", strings)
        self.assertIn("主人公の見た目", strings)
        self.assertIn("ノーマル", strings)
        self.assertIn("種付けマスター", strings)
        self.assertIn("エロシーンピストンオート", strings)
        self.assertIn("断面図表示", strings)
        self.assertIn("喘ぎ声 音量", strings)
        self.assertIn("難易度", strings)
        self.assertIn("イージー", strings)
        self.assertIn("ノーマル", strings)
        self.assertIn("ハード", strings)

        # Simular tradução
        translated_list = [f"TRADUZIDO_{s}" for s in strings]
        TextsUtils.interactive_item(extracted, translated_list)

        # Reinserir
        extractor.update_json("plugins.js", self.sample_plugins, extracted)

        # Validar conteúdo nos plugins atualizados
        config_plugin = next(p for p in self.sample_plugins if p["name"] == "CustomizeConfigItem")
        text_items = json.loads(config_plugin["parameters"]["文字項目"])
        item0 = json.loads(text_items[0])
        self.assertEqual(item0["Name"], "TRADUZIDO_主人公名変更")
        self.assertEqual(item0["Script"], "SceneManager.push(Scene_Name);")

        item1 = json.loads(text_items[1])
        self.assertEqual(item1["Name"], "TRADUZIDO_主人公の見た目")
        str_items = json.loads(item1["StringItems"])
        self.assertEqual(str_items[0], "TRADUZIDO_ノーマル")
        self.assertEqual(str_items[1], "TRADUZIDO_種付けマスター")

        diff_plugin = next(p for p in self.sample_plugins if p["name"] == "Vitsuno_Difficulty")
        self.assertEqual(diff_plugin["parameters"]["Option Name"], "TRADUZIDO_難易度")
        self.assertEqual(diff_plugin["parameters"]["1 : Name"], "TRADUZIDO_イージー")

    def test_plugins_js_segments_roundtrip(self):
        extractor = RPGMakerExtractor(None)
        segs = extractor.extract_segments("plugins.js", self.sample_plugins)
        self.assertTrue(len(segs) >= 11)

        sources = {s.source for s in segs}
        self.assertIn("難易度", sources)
        self.assertIn("エロシーンピストンオート", sources)

        # Aplicar traduções via segmentos
        translations = {s.id: f"PT_{s.source}" for s in segs}
        updated = extractor.apply_segments("plugins.js", self.sample_plugins, translations)

        diff_plugin = next(p for p in updated if p["name"] == "Vitsuno_Difficulty")
        self.assertEqual(diff_plugin["parameters"]["Option Name"], "PT_難易度")

    def test_format_adapter_plugins_js_load_save(self, tmp_path=None):
        import tempfile
        import os
        js_code = (
            "// Generated by RPG Maker.\n"
            "// Do not edit this file directly.\n"
            "var $plugins =\n"
            "[\n"
            '{"name":"Test","status":true,"description":"","parameters":{"title":"テスト"}}\n'
            "];\n"
        )
        with tempfile.NamedTemporaryFile("w+", suffix=".js", delete=False) as f:
            f.write(js_code)
            temp_path = f.name

        try:
            loaded = PluginJsFormatAdapter.load_file(temp_path)
            self.assertEqual(loaded[0]["name"], "Test")
            self.assertEqual(loaded[0]["parameters"]["title"], "テスト")

            loaded[0]["parameters"]["title"] = "Teste Traduzido"
            PluginJsFormatAdapter.save_file(temp_path, loaded)

            with open(temp_path, "r", encoding="utf-8") as f:
                saved_content = f.read()

            self.assertIn("var $plugins =", saved_content)
            self.assertIn("Teste Traduzido", saved_content)
        finally:
            if os.path.exists(temp_path):
                os.remove(temp_path)


if __name__ == "__main__":
    unittest.main()
