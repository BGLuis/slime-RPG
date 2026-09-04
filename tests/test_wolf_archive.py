import os
import shutil
import pytest
from pathlib import Path
from src.extractor.wolfrpg.WolfArchive import WolfArchive, create_wolf_archive, extract_wolf_archive


@pytest.fixture
def temp_archive_dirs(tmp_path):
    in_dir = tmp_path / "archive_input"
    in_dir.mkdir()
    out_dir = tmp_path / "archive_output"
    out_dir.mkdir()
    wolf_file = tmp_path / "Test.wolf"

    # Criar estrutura de teste com arquivos em múltiplas pastas
    (in_dir / "root_file.txt").write_text("Hello from Wolf RPG archive root!", encoding="utf-8")
    sub1 = in_dir / "MapData"
    sub1.mkdir()
    (sub1 / "Map001.txt").write_text("Map data content 12345", encoding="utf-8")
    (sub1 / "Map002.txt").write_bytes(b"\x00\x01\x02\x03\x04" * 50)

    sub2 = in_dir / "BasicData"
    sub2.mkdir()
    (sub2 / "CommonEvent.txt").write_text("Common events mock data", encoding="utf-8")

    return in_dir, out_dir, wolf_file


def test_wolf_archive_pack_unpack(temp_archive_dirs):
    in_dir, out_dir, wolf_file = temp_archive_dirs

    # Empacotar
    res = WolfArchive.create_archive(str(in_dir), str(wolf_file), use_compression=True, use_huffman=True)
    assert res is True
    assert wolf_file.exists()
    assert wolf_file.stat().st_size > 0

    # Desempacotar
    res_ext = WolfArchive.extract_archive(str(wolf_file), str(out_dir))
    assert res_ext is True

    # Validar integridade dos arquivos
    assert (out_dir / "root_file.txt").read_text(encoding="utf-8") == "Hello from Wolf RPG archive root!"
    assert (out_dir / "MapData" / "Map001.txt").read_text(encoding="utf-8") == "Map data content 12345"
    assert (out_dir / "MapData" / "Map002.txt").read_bytes() == b"\x00\x01\x02\x03\x04" * 50
    assert (out_dir / "BasicData" / "CommonEvent.txt").read_text(encoding="utf-8") == "Common events mock data"


def test_wolf_archive_uncompressed(temp_archive_dirs):
    in_dir, out_dir, wolf_file = temp_archive_dirs
    wolf_file_uncomp = wolf_file.with_name("Uncompressed.wolf")

    # Empacotar sem compressão
    res = WolfArchive.create_archive(str(in_dir), str(wolf_file_uncomp), use_compression=False, use_huffman=False)
    assert res is True

    out_uncomp = out_dir / "uncompressed"
    out_uncomp.mkdir()
    res_ext = WolfArchive.extract_archive(str(wolf_file_uncomp), str(out_uncomp))
    assert res_ext is True

    assert (out_uncomp / "root_file.txt").read_text(encoding="utf-8") == "Hello from Wolf RPG archive root!"
    assert (out_uncomp / "MapData" / "Map001.txt").read_text(encoding="utf-8") == "Map data content 12345"
