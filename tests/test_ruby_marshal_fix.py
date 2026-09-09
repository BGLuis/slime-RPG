import io
import os
import shutil
import tempfile
import unittest

from src.utils.RubyMarshal import (
    RubyObject,
    RubyString,
    UserDef,
    Symbol,
    RubyWriter,
    RubyReader,
    load,
    loads,
    write,
    writes,
)
from src.extractor.rpgmaker.RPGMakerExtractor import RPGMakerExtractor
from src.extractor.rpgmaker.RVDataAdapter import RVDataAdapter
from tests.test_rvdata2_extractor import MockTranslator


class TestRubyMarshalFix(unittest.TestCase):
    def setUp(self):
        self.temp_dir = tempfile.mkdtemp()
        self.extractor = RPGMakerExtractor(MockTranslator())

    def tearDown(self):
        shutil.rmtree(self.temp_dir, ignore_errors=True)

    def test_writer_and_reader_object_index_sync_with_python_str(self):
        """
        Garante que strings Python nativas (str) e bytes incrementem
        a tabela de objetos no Writer e Reader de forma 1:1,
        evitando corrupção de TYPE_LINK.
        """
        shared_cmd = RubyObject("RPG::MoveCommand", {"@code": 44, "@parameters": []})

        # Estrutura contendo strings Python, seguidas de objeto compartilhado (gera link)
        data = [
            "primeira_string_python",
            "segunda_string_python",
            b"bytes_sem_ivar",
            shared_cmd,
            shared_cmd,  # Deve gerar TYPE_LINK para shared_cmd
        ]

        # Serializa com writes (RubyWriter)
        buf = writes(data)

        # Desserializa com loads (RubyReader)
        unmarshaled = loads(buf)

        self.assertEqual(len(unmarshaled), 5)
        self.assertEqual(unmarshaled[0], "primeira_string_python")
        self.assertEqual(unmarshaled[1], "segunda_string_python")
        self.assertEqual(unmarshaled[2], b"bytes_sem_ivar")
        self.assertIsInstance(unmarshaled[3], RubyObject)
        self.assertEqual(unmarshaled[3].ruby_class_name, "RPG::MoveCommand")
        # O quinto elemento deve ser exatamente o mesmo objeto (referência de link resolvida)
        self.assertIs(unmarshaled[3], unmarshaled[4])

    def test_rpgmaker_move_command_links_after_translation(self):
        """
        Reproduz a estrutura real de eventos do RPG Maker VX Ace:
        - Comando 205 (Set Move Route) com RPG::MoveRoute contendo RPG::MoveCommand
        - Comandos subsequentes 505 reutilizando as mesmas instâncias de RPG::MoveCommand
        - Comandos de texto contendo strings traduzidas
        Valida que salvar e recarregar não lança 'Object id ... is not yet unmarshaled'.
        """
        mv_cmd1 = RubyObject("RPG::MoveCommand", {"@code": 37, "@parameters": []})
        mv_cmd2 = RubyObject("RPG::MoveCommand", {"@code": 39, "@parameters": []})

        route = RubyObject(
            "RPG::MoveRoute",
            {"@repeat": False, "@skippable": False, "@wait": True, "@list": [mv_cmd1, mv_cmd2]},
        )

        cmd_show_text = RubyObject(
            "RPG::EventCommand",
            {"@code": 101, "@indent": 0, "@parameters": ["", 0, 0, 2]},
        )
        cmd_text_body = RubyObject(
            "RPG::EventCommand",
            {"@code": 401, "@indent": 0, "@parameters": ["Olá aventureiro!"]},
        )
        cmd_move_route = RubyObject(
            "RPG::EventCommand",
            {"@code": 205, "@indent": 0, "@parameters": [0, route]},
        )
        cmd_move_step1 = RubyObject(
            "RPG::EventCommand",
            {"@code": 505, "@indent": 0, "@parameters": [mv_cmd1]},
        )
        cmd_move_step2 = RubyObject(
            "RPG::EventCommand",
            {"@code": 505, "@indent": 0, "@parameters": [mv_cmd2]},
        )
        cmd_end = RubyObject(
            "RPG::EventCommand",
            {"@code": 0, "@indent": 0, "@parameters": []},
        )

        page = RubyObject(
            "RPG::Event::Page",
            {"@list": [cmd_show_text, cmd_text_body, cmd_move_route, cmd_move_step1, cmd_move_step2, cmd_end]},
        )

        event = RubyObject(
            "RPG::Event",
            {"@id": 40, "@name": "EV040", "@pages": [page]},
        )

        rv_map = RubyObject(
            "RPG::Map",
            {"@display_name": "Mapa de Teste", "@events": {40: event}},
        )

        # 1. Converte para normalizado e simula tradução do texto
        normalized = RVDataAdapter.to_normalized("Map040.rvdata2", rv_map)
        normalized._raw_rvdata = rv_map

        # Aplica a tradução com string Python
        normalized["events"][40]["pages"][0]["list"][1]["parameters"][0] = "Texto Traduzido Com Sucesso!"

        # 2. Salva para arquivo .rvdata2
        map_path = os.path.join(self.temp_dir, "Map040.rvdata2")
        RVDataAdapter.save_file(map_path, normalized)

        # 3. Recarrega o arquivo - não pode lançar 'invalid link destination'
        reloaded = RVDataAdapter.load_file(map_path)
        self.assertIsNotNone(reloaded)

        # Verifica integridade do texto recarregado
        reloaded_text = reloaded["events"][40]["pages"][0]["list"][1]["parameters"][0]
        self.assertEqual(reloaded_text, "Texto Traduzido Com Sucesso!")

    def test_resilient_binary_string_decoding(self):
        """
        Garante que sequências de bytes compactadas / binárias contendo \\u
        (como scripts compilados em Scripts.rvdata2) não falhem com UnicodeDecodeError.
        """
        # Bytes com prefixo \u seguido de bytes não-hex que falhariam em unicode-escape
        binary_payload = b"compressed_code_\x00\xff\\uZZ_invalid_escape_binary_data"

        data = [
            1,
            "Script1",
            RubyString(binary_payload.decode("latin1"), {"E": True}),
        ]

        buf = writes(data)
        # Deve ler sem lançar UnicodeDecodeError
        unmarshaled = loads(buf)
        self.assertEqual(len(unmarshaled), 3)
        self.assertEqual(unmarshaled[1], "Script1")

    def test_full_pipeline_process_and_import_cycle(self):
        """
        Testa o ciclo completo do pipeline:
        BaseExtractor.process_file -> folderProcess
        BaseExtractor.import_files -> folderOutput
        """
        folder_in = os.path.join(self.temp_dir, "input")
        folder_proc = os.path.join(self.temp_dir, "process")
        folder_out = os.path.join(self.temp_dir, "output")
        os.makedirs(folder_in, exist_ok=True)
        os.makedirs(folder_proc, exist_ok=True)
        os.makedirs(folder_out, exist_ok=True)

        # Monta um mapa sintetizado com rotas e links
        mv = RubyObject("RPG::MoveCommand", {"@code": 10, "@parameters": []})
        route = RubyObject("RPG::MoveRoute", {"@list": [mv]})
        cmds = [
            RubyObject("RPG::EventCommand", {"@code": 101, "@indent": 0, "@parameters": ["Hero", 0, 0, 0]}),
            RubyObject("RPG::EventCommand", {"@code": 401, "@indent": 0, "@parameters": ["Hello World!"]}),
            RubyObject("RPG::EventCommand", {"@code": 205, "@indent": 0, "@parameters": [0, route]}),
            RubyObject("RPG::EventCommand", {"@code": 505, "@indent": 0, "@parameters": [mv]}),
            RubyObject("RPG::EventCommand", {"@code": 0, "@indent": 0, "@parameters": []}),
        ]
        page = RubyObject("RPG::Event::Page", {"@list": cmds})
        event = RubyObject("RPG::Event", {"@id": 1, "@name": "EV001", "@pages": [page]})
        rv_map = RubyObject("RPG::Map", {"@display_name": "Test Map", "@events": {1: event}})

        map_path = os.path.join(folder_in, "Map001.rvdata2")
        with open(map_path, "wb") as f:
            write(f, rv_map)

        self.extractor.folderInput = folder_in
        self.extractor.folderProcess = folder_proc
        self.extractor.folderOutput = folder_out

        # Executa process_file
        self.extractor.process_file(map_path)

        # Executa import_files (a etapa que falhava antes)
        self.extractor.import_files()

        # Verifica arquivo final em output
        output_file = os.path.join(folder_out, "Map001.rvdata2")
        self.assertTrue(os.path.exists(output_file))

        final_data = RVDataAdapter.load_file(output_file)
        extracted = self.extractor.extract_text("Map001.rvdata2", final_data)
        self.assertTrue(len(extracted) > 0)
        translated_text = extracted[0]["pages"][0]["list"][0]["text"]
        translated_line = translated_text[0] if isinstance(translated_text, list) else translated_text
        self.assertTrue("[TRAD]" in translated_line)


if __name__ == "__main__":
    unittest.main()
