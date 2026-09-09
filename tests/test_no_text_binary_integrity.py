import hashlib
import os
import shutil
import tempfile
import unittest

from src.extractor.rpgmaker.RPGMakerExtractor import RPGMakerExtractor
from tests.test_rvdata2_extractor import MockTranslator


class TestNoTextBinaryIntegrity(unittest.TestCase):
    def setUp(self):
        self.temp_dir = tempfile.mkdtemp()
        self.extractor = RPGMakerExtractor(MockTranslator())
        self.extractor.folderInput = os.path.join(self.temp_dir, "input")
        self.extractor.folderProcess = os.path.join(self.temp_dir, "process")
        self.extractor.folderOutput = os.path.join(self.temp_dir, "output")
        os.makedirs(self.extractor.folderInput, exist_ok=True)
        os.makedirs(self.extractor.folderProcess, exist_ok=True)
        os.makedirs(self.extractor.folderOutput, exist_ok=True)

    def tearDown(self):
        shutil.rmtree(self.temp_dir, ignore_errors=True)

    def _sha256(self, file_path):
        h = hashlib.sha256()
        with open(file_path, "rb") as f:
            h.update(f.read())
        return h.hexdigest()

    def test_is_translatable_file_filters(self):
        """Valida que scripts, animations e tilesets não são considerados translatáveis."""
        self.assertFalse(self.extractor.is_translatable_file("Scripts.rvdata2"))
        self.assertFalse(self.extractor.is_translatable_file("Animations.rvdata2"))
        self.assertFalse(self.extractor.is_translatable_file("Tilesets.rvdata2"))
        self.assertFalse(self.extractor.is_translatable_file("UnknownFile.xyz"))

        self.assertTrue(self.extractor.is_translatable_file("Map001.rvdata2"))
        self.assertTrue(self.extractor.is_translatable_file("CommonEvents.rvdata2"))
        self.assertTrue(self.extractor.is_translatable_file("System.rvdata2"))
        self.assertTrue(self.extractor.is_translatable_file("Items.rvdata2"))

    def test_handle_no_text_preserves_exact_binary_hash(self):
        """Valida que _handle_no_text copia arquivos com 100% de integridade binária."""
        # Cria arquivo binário com bytes aleatórios e sequências zlib-like
        source_file = os.path.join(self.extractor.folderInput, "BinaryTest.bin")
        binary_data = b"\x04\x08x\x9c\x03\x00\x00\x00\x00\x01\x06\xff\xfe\x00\x12" * 100
        with open(source_file, "wb") as f:
            f.write(binary_data)

        orig_hash = self._sha256(source_file)

        self.extractor._handle_no_text(source_file, "BinaryTest.bin")

        dest_file = os.path.join(self.extractor.folderOutput, "BinaryTest.bin")
        self.assertTrue(os.path.exists(dest_file))
        dest_hash = self._sha256(dest_file)

        self.assertEqual(orig_hash, dest_hash)
        self.assertEqual(os.path.getsize(source_file), os.path.getsize(dest_file))

    def test_process_file_copies_scripts_rvdata2_intact(self):
        """Valida que process_file em Scripts.rvdata2 copia diretamente sem re-serializar."""
        real_scripts = "/mnt/hdd/game/Dress Quest EN/Data-en/Scripts.rvdata2"
        if not os.path.exists(real_scripts):
            self.skipTest("Arquivo real do jogo não encontrado para este teste")

        test_scripts = os.path.join(self.extractor.folderInput, "Scripts.rvdata2")
        shutil.copy2(real_scripts, test_scripts)

        orig_hash = self._sha256(test_scripts)
        orig_size = os.path.getsize(test_scripts)

        self.extractor.process_file(test_scripts)

        output_file = os.path.join(self.extractor.folderOutput, "Scripts.rvdata2")
        self.assertTrue(os.path.exists(output_file))

        output_hash = self._sha256(output_file)
        output_size = os.path.getsize(output_file)

        self.assertEqual(orig_hash, output_hash)
        self.assertEqual(orig_size, output_size)


if __name__ == "__main__":
    unittest.main()
