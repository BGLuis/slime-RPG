import pytest
from src.extractor.wolfrpg.WolfArchive import WolfArchive


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


def test_wolf_archive_crypto_primitives():
    from src.extractor.wolfrpg.WolfArchive import (
        WinRng, init_wolf_crypt, wolf_crypt, init_aes128, aes_ctr_xcrypt, KNOWN_KEYS
    )

    # 1. Test MSVC LCG Rng
    rng = WinRng(12345)
    vals = [rng.rand() for _ in range(5)]
    assert len(vals) == 5
    rng2 = WinRng(12345)
    assert [rng2.rand() for _ in range(5)] == vals

    # 2. Test Wolf Crypt XOR stream symmetry
    p_pwd = b"123456789012345"
    key_str = KNOWN_KEYS[0x14B].split(b'\x00')[0]
    key = init_wolf_crypt(0x14B, p_pwd, key_str, other=True)
    assert len(key) == 768

    plain = bytearray(b"Hello World WOLF RPG Cryptography test data!" * 10)
    cipher = bytearray(plain)
    wolf_crypt(key, cipher, 0, 64, 64 + len(plain))
    assert cipher != plain
    wolf_crypt(key, cipher, 0, 64, 64 + len(plain))
    assert cipher == plain

    # 3. Test AES-128 CTR symmetry
    aes_rk = init_aes128(p_pwd, 0x14B)
    data = bytearray(b"AES CTR test stream block" * 4)
    data_enc = bytearray(data)
    aes_ctr_xcrypt(data_enc, 0, bytearray(aes_rk), len(data))
    assert data_enc != data
    aes_ctr_xcrypt(data_enc, 0, bytearray(aes_rk), len(data))
    assert data_enc == data


def test_wolf_common_events_engine_version_support(tmp_path):
    from src.extractor.wolfrpg.WolfBinaryAdapter import WolfCommonEvents

    # Test Wolf 3 with version 0xCA (Wolf 3.31)
    ce_v331 = WolfCommonEvents()
    ce_v331.magic = b'\x00W\x00\x00OLUFC\x00\xca'
    ce_v331.wolfversion = 3
    ce_v331.is_utf8 = True
    ce_v331.events = []
    ce_v331.last_terminator = 0x91

    p = tmp_path / "CommonEvent_331.dat"
    ce_v331.save(str(p))
    loaded = WolfCommonEvents.load(str(p))
    assert loaded.magic == b'\x00W\x00\x00OLUFC\x00\xca'
    assert loaded.wolfversion == 3
    assert loaded.is_utf8 is True
