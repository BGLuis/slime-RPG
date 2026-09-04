import os
import io
import shutil
import tempfile
import unittest
from rubymarshal.classes import RubyObject, RubyString, UserDef
import rubymarshal.writer as r_writer
import rubymarshal.reader as r_reader

from src.extractor.rpgmaker.RPGMakerExtractor import RPGMakerExtractor
from src.extractor.rpgmaker.RVDataAdapter import RVDataAdapter, RVDataDict, RVDataList
from src.extractor.BaseExtractor import BaseExtractor

class MockTranslator:
    def __init__(self):
        self.cache = {}
        import threading
        self.cache_lock = threading.Lock()

    def translate_batch(self, batch, progress_callback=None):
        return [f"[TRAD] {text}" if isinstance(text, str) else text for text in batch]

    def translator(self, text, progress_callback=None):
        if isinstance(text, list):
            return [f"[TRAD] {t}" if isinstance(t, str) else t for t in text]
        if isinstance(text, str):
            return f"[TRAD] {text}"
        return text

    def reduce_limite(self):
        pass

    def __deepcopy__(self, memo):
        return self

class TestRVData2Extractor(unittest.TestCase):
    def setUp(self):
        self.temp_dir = tempfile.mkdtemp()
        self.extractor = RPGMakerExtractor(MockTranslator())

    def tearDown(self):
        shutil.rmtree(self.temp_dir, ignore_errors=True)

    def _create_synthetic_table(self, x=2, y=2, z=1):
        """Cria um objeto UserDef Table simulando o binário do RGSS3"""
        table = UserDef("Table")
        dim = 2 if y > 0 and z <= 1 else (3 if z > 1 else 1)
        size = x * y * z
        import struct
        header = struct.pack("lllll", dim, x, y, z, size)
        data = struct.pack(f"{size}h", *([0] * size))
        table._load(header + data)
        return table

    def _create_event_command(self, code, indent, parameters):
        cmd = RubyObject("RPG::EventCommand")
        cmd.attributes = {
            "@code": code,
            "@indent": indent,
            "@parameters": parameters
        }
        return cmd

    def test_map_rvdata2_extraction_and_insertion(self):
        """Valida extração e re-injeção de MapXXX.rvdata2 com integridade de Table"""
        # Monta Map001.rvdata2 sintético
        cmd1 = self._create_event_command(401, 0, ["Olá mundo do RPG Maker!"])
        cmd2 = self._create_event_command(102, 0, [["Sim", "Não"], 1])
        cmd_end = self._create_event_command(0, 0, [])

        page = RubyObject("RPG::Event::Page")
        page.attributes = {
            "@list": [cmd1, cmd2, cmd_end]
        }

        event = RubyObject("RPG::Event")
        event.attributes = {
            "@id": 1,
            "@name": "EV001",
            "@pages": [page]
        }

        table = self._create_synthetic_table(2, 2, 1)

        rv_map = RubyObject("RPG::Map")
        rv_map.attributes = {
            "@display_name": "Vila Inicial",
            "@data": table,
            "@events": {1: event}
        }

        map_path = os.path.join(self.temp_dir, "Map001.rvdata2")
        with open(map_path, "wb") as f:
            r_writer.write(f, rv_map)

        # 1. Carrega através do BaseExtractor.extract_files
        file_name, data = self.extractor.extract_files(map_path)
        self.assertEqual(file_name, "Map001.rvdata2")
        self.assertIsNotNone(data)
        self.assertIn("events", data)
        self.assertEqual(data["displayName"], "Vila Inicial")

        # 2. Extrai texto através do RPGMakerExtractor
        extracted = self.extractor.extract_text(file_name, data)
        self.assertEqual(len(extracted), 1)
        self.assertEqual(extracted[0]["id"], 1)
        page_items = extracted[0]["pages"][0]["list"]
        # Deve ter extraído o ShowText e as escolhas do ShowChoices
        self.assertEqual(page_items[0]["text"], "Olá mundo do RPG Maker!")
        self.assertEqual(page_items[1]["text"], ["Sim", "Não"])

        # 3. Simula tradução e atualiza os dados
        page_items[0]["text"] = "Hello RPG Maker world!"
        page_items[1]["text"] = ["Yes", "No"]

        updated_data = self.extractor.update_json(file_name, data, extracted)

        # 4. Salva de volta via BaseExtractor.import_file
        out_dir = os.path.join(self.temp_dir, "output")
        os.makedirs(out_dir, exist_ok=True)
        self.extractor.import_file(file_name, updated_data, out_dir)

        # 5. Valida o arquivo salvo
        saved_path = os.path.join(out_dir, "Map001.rvdata2")
        self.assertTrue(os.path.exists(saved_path))

        with open(saved_path, "rb") as f:
            reloaded_rv = r_reader.load(f)

        self.assertEqual(reloaded_rv.ruby_class_name, "RPG::Map")
        reloaded_cmds = reloaded_rv.attributes["@events"][1].attributes["@pages"][0].attributes["@list"]
        self.assertEqual(reloaded_cmds[0].attributes["@parameters"][0], "Hello RPG Maker world!")
        self.assertEqual(reloaded_cmds[1].attributes["@parameters"][0], ["Yes", "No"])
        # Valida que o Table permaneceu intacto com a mesma quantidade de bytes
        self.assertEqual(reloaded_rv.attributes["@data"].ruby_class_name, "Table")
        self.assertEqual(len(reloaded_rv.attributes["@data"]._private_data), len(table._private_data))

    def test_actors_rvdata2_extraction_and_insertion(self):
        """Valida extração e re-injeção de Actors.rvdata2"""
        actor1 = RubyObject("RPG::Actor")
        actor1.attributes = {
            "@id": 1,
            "@name": "Herói",
            "@nickname": "O Corajoso",
            "@profile": "Um jovem guerreiro em busca de aventura.",
            "@description": "Guerreiro da luz",
            "@note": "<Desc: Nota especial>"
        }

        actors_list = [None, actor1]
        file_path = os.path.join(self.temp_dir, "Actors.rvdata2")
        with open(file_path, "wb") as f:
            r_writer.write(f, actors_list)

        file_name, data = self.extractor.extract_files(file_path)
        extracted = self.extractor.extract_text(file_name, data)
        self.assertEqual(len(extracted), 1)
        self.assertEqual(extracted[0]["name"], "Herói")
        self.assertEqual(extracted[0]["nickname"], "O Corajoso")
        self.assertEqual(extracted[0]["profile"], "Um jovem guerreiro em busca de aventura.")

        # Modifica dados
        extracted[0]["name"] = "Hero"
        extracted[0]["nickname"] = "The Brave"
        extracted[0]["profile"] = "A young warrior seeking adventure."

        updated_data = self.extractor.update_json(file_name, data, extracted)
        out_dir = os.path.join(self.temp_dir, "output")
        self.extractor.import_file(file_name, updated_data, out_dir)

        saved_path = os.path.join(out_dir, "Actors.rvdata2")
        with open(saved_path, "rb") as f:
            reloaded = r_reader.load(f)

        saved_actor = reloaded[1]
        self.assertEqual(saved_actor.attributes["@name"], "Hero")
        self.assertEqual(saved_actor.attributes["@nickname"], "The Brave")
        self.assertEqual(saved_actor.attributes["@profile"], "A young warrior seeking adventure.")

    def test_system_rvdata2_extraction_and_insertion(self):
        """Valida extração e re-injeção de System.rvdata2 com termos do RGSS3"""
        terms = RubyObject("RPG::System::Terms")
        terms.attributes = {
            "@basic": ["Nível", "HP", "MP", "TP"],
            "@params": ["Ataque", "Defesa"],
            "@etypes": ["Arma", "Escudo"],
            "@commands": ["Lutar", "Fugir"]
        }

        system = RubyObject("RPG::System")
        system.attributes = {
            "@game_title": "Minha Aventura",
            "@currency_unit": "Ouro",
            "@elements": ["", "Fogo", "Gelo"],
            "@skill_types": ["", "Magia", "Especial"],
            "@weapon_types": ["", "Espada", "Arco"],
            "@armor_types": ["", "Geral", "Pesada"],
            "@switches": ["", "IntroConcluida"],
            "@terms": terms
        }

        file_path = os.path.join(self.temp_dir, "System.rvdata2")
        with open(file_path, "wb") as f:
            r_writer.write(f, system)

        file_name, data = self.extractor.extract_files(file_path)
        extracted = self.extractor.extract_text(file_name, data)
        self.assertEqual(extracted["gameTitle"], "Minha Aventura")
        self.assertEqual(extracted["currencyUnit"], "Ouro")
        self.assertIn("terms", extracted)
        self.assertEqual(extracted["terms"]["basic"][0], "Nível")

        # Modifica dados
        extracted["gameTitle"] = "My Adventure"
        extracted["currencyUnit"] = "Gold"
        extracted["terms"]["basic"][0] = "Level"

        updated_data = self.extractor.update_json(file_name, data, extracted)
        out_dir = os.path.join(self.temp_dir, "output")
        self.extractor.import_file(file_name, updated_data, out_dir)

        saved_path = os.path.join(out_dir, "System.rvdata2")
        with open(saved_path, "rb") as f:
            reloaded = r_reader.load(f)

        self.assertEqual(reloaded.attributes["@game_title"], "My Adventure")
        self.assertEqual(reloaded.attributes["@currency_unit"], "Gold")
        self.assertEqual(reloaded.attributes["@terms"].attributes["@basic"][0], "Level")

    def test_commonevents_rvdata2_extraction_and_insertion(self):
        """Valida CommonEvents.rvdata2"""
        cmd = self._create_event_command(401, 0, ["Texto do evento comum"])
        ev = RubyObject("RPG::CommonEvent")
        ev.attributes = {
            "@id": 1,
            "@name": "Evento01",
            "@list": [cmd]
        }

        file_path = os.path.join(self.temp_dir, "CommonEvents.rvdata2")
        with open(file_path, "wb") as f:
            r_writer.write(f, [None, ev])

        file_name, data = self.extractor.extract_files(file_path)
        extracted = self.extractor.extract_text(file_name, data)
        self.assertEqual(len(extracted), 1)
        self.assertEqual(extracted[0]["list"][0]["text"], "Texto do evento comum")

        extracted[0]["list"][0]["text"] = "Common event text"
        updated_data = self.extractor.update_json(file_name, data, extracted)
        out_dir = os.path.join(self.temp_dir, "output")
        self.extractor.import_file(file_name, updated_data, out_dir)

        saved_path = os.path.join(out_dir, "CommonEvents.rvdata2")
        with open(saved_path, "rb") as f:
            reloaded = r_reader.load(f)

        self.assertEqual(reloaded[1].attributes["@list"][0].attributes["@parameters"][0], "Common event text")

    def test_troops_rvdata2_extraction_and_insertion(self):
        """Valida Troops.rvdata2"""
        cmd = self._create_event_command(401, 0, ["Texto da batalha"])
        page = RubyObject("RPG::Troop::Page")
        page.attributes = {"@list": [cmd]}
        troop = RubyObject("RPG::Troop")
        troop.attributes = {
            "@id": 1,
            "@name": "Bando de Goblins",
            "@pages": [page]
        }

        file_path = os.path.join(self.temp_dir, "Troops.rvdata2")
        with open(file_path, "wb") as f:
            r_writer.write(f, [None, troop])

        file_name, data = self.extractor.extract_files(file_path)
        extracted = self.extractor.extract_text(file_name, data)
        self.assertEqual(len(extracted), 1)
        self.assertEqual(extracted[0]["pages"][0]["list"][0]["text"], "Texto da batalha")

        extracted[0]["pages"][0]["list"][0]["text"] = "Battle text"
        updated_data = self.extractor.update_json(file_name, data, extracted)
        out_dir = os.path.join(self.temp_dir, "output")
        self.extractor.import_file(file_name, updated_data, out_dir)

        saved_path = os.path.join(out_dir, "Troops.rvdata2")
        with open(saved_path, "rb") as f:
            reloaded = r_reader.load(f)

        self.assertEqual(reloaded[1].attributes["@pages"][0].attributes["@list"][0].attributes["@parameters"][0], "Battle text")

    def test_mapinfos_rvdata2_extraction_and_insertion(self):
        """Valida MapInfos.rvdata2 (Hash { id => MapInfo } no RGSS3)"""
        info1 = RubyObject("RPG::MapInfo")
        info1.attributes = {"@name": "Mapa do Castelo"}
        mapinfos = {1: info1}

        file_path = os.path.join(self.temp_dir, "MapInfos.rvdata2")
        with open(file_path, "wb") as f:
            r_writer.write(f, mapinfos)

        file_name, data = self.extractor.extract_files(file_path)
        extracted = self.extractor.extract_text(file_name, data)
        self.assertEqual(len(extracted), 1)
        self.assertEqual(extracted[0]["name"], "Mapa do Castelo")

        extracted[0]["name"] = "Castle Map"
        updated_data = self.extractor.update_json(file_name, data, extracted)
        out_dir = os.path.join(self.temp_dir, "output")
        self.extractor.import_file(file_name, updated_data, out_dir)

        saved_path = os.path.join(out_dir, "MapInfos.rvdata2")
        with open(saved_path, "rb") as f:
            reloaded = r_reader.load(f)

        self.assertEqual(reloaded[1].attributes["@name"], "Castle Map")

    def test_end_to_end_pipeline_rvdata2(self):
        """Executa o fluxo completo de process_file com tradução simulada em .rvdata2"""
        input_dir = os.path.join(self.temp_dir, "input")
        process_dir = os.path.join(self.temp_dir, "process")
        output_dir = os.path.join(self.temp_dir, "output")
        os.makedirs(input_dir, exist_ok=True)
        os.makedirs(process_dir, exist_ok=True)
        os.makedirs(output_dir, exist_ok=True)

        # Configura pastas no extractor
        self.extractor.folderInput = input_dir
        self.extractor.folderProcess = process_dir
        self.extractor.folderOutput = output_dir

        cmd = self._create_event_command(401, 0, ["Olá mundo!"])
        page = RubyObject("RPG::Event::Page")
        page.attributes = {"@list": [cmd]}
        event = RubyObject("RPG::Event")
        event.attributes = {"@id": 1, "@name": "EV001", "@pages": [page]}
        table = self._create_synthetic_table(2, 2, 1)

        rv_map = RubyObject("RPG::Map")
        rv_map.attributes = {
            "@display_name": "Vila",
            "@data": table,
            "@events": {1: event}
        }

        file_path = os.path.join(input_dir, "Map001.rvdata2")
        with open(file_path, "wb") as f:
            r_writer.write(f, rv_map)

        # Processa o arquivo
        self.extractor.process_file(file_path)

        # Verifica se foi gerado em process/
        process_file = os.path.join(process_dir, "Map001.rvdata2")
        self.assertTrue(os.path.exists(process_file))

        # Importa para output/
        self.extractor.import_files()
        output_file = os.path.join(output_dir, "Map001.rvdata2")
        self.assertTrue(os.path.exists(output_file))

        # Verifica conteúdo traduzido
        with open(output_file, "rb") as f:
            result = r_reader.load(f)

        final_cmd = result.attributes["@events"][1].attributes["@pages"][0].attributes["@list"][0]
        self.assertEqual(final_cmd.attributes["@parameters"][0], "[TRAD] Olá mundo!")
        self.assertEqual(result.attributes["@data"].ruby_class_name, "Table")
