import json
import sqlite3
import pytest
from src.translate.TranslationMemory import TranslationMemory, source_hash
from src.extractor.JsonExtractor import JsonExtractor
from src.translate.BaseTranslate import BaseTranslate
from src.translate.Provider import Capabilities


class DummyProvider(BaseTranslate):
    agent = 'dummy'
    id = 'dummy'
    capabilities = Capabilities(max_request_chars=2000, max_items=10, native_batch=True)

    def _translate_single_batch(self, texts):
        return [f"TR:{t}" for t in texts]


def test_storing_same_source_two_engines_prefer_engine(tmp_path):
    """
    Acceptance criterion: Test storing the same source with two engines:
    lookup with prefer_engine='ollama' returns the Ollama translation.
    """
    db_path = str(tmp_path / "memory_engines.db")

    tm_google = TranslationMemory(db_path, 'en', 'pt', engine='google')
    tm_google.store("Sword", "Espada (Google)", status='machine')

    tm_ollama = TranslationMemory(db_path, 'en', 'pt', engine='ollama')
    tm_ollama.store("Sword", "Lâmina (Ollama)", status='machine')

    # Lookup using prefer_engine='ollama' returns Ollama translation
    assert tm_google.lookup("Sword", prefer_engine="ollama") == "Lâmina (Ollama)"
    assert tm_ollama.lookup("Sword", prefer_engine="ollama") == "Lâmina (Ollama)"

    # Lookup using prefer_engine='google' returns Google translation
    assert tm_google.lookup("Sword", prefer_engine="google") == "Espada (Google)"
    assert tm_ollama.lookup("Sword", prefer_engine="google") == "Espada (Google)"

    # Bulk lookup_many respects prefer_engine
    res_ollama = tm_google.lookup_many(["Sword"], prefer_engine="ollama")
    assert res_ollama["Sword"] == "Lâmina (Ollama)"
    res_google = tm_google.lookup_many(["Sword"], prefer_engine="google")
    assert res_google["Sword"] == "Espada (Google)"


def test_line_breaks_and_spaces_do_not_collide(tmp_path):
    """
    Acceptance criterion: "A\\nB" and "A B" do not collide.
    """
    db_path = str(tmp_path / "memory_linebreaks.db")
    tm = TranslationMemory(db_path, 'en', 'pt', engine='google')

    tm.store("A\nB", "Trad A\nB")
    tm.store("A B", "Trad A B")

    # Hashes must be different
    assert source_hash("A\nB") != source_hash("A B")

    # In database, both rows must exist
    conn = sqlite3.connect(db_path)
    rows = conn.execute("SELECT source, target FROM segment WHERE src_lang='en' AND tgt_lang='pt'").fetchall()
    conn.close()

    assert len(rows) == 2
    sources = {r[0]: r[1] for r in rows}
    assert sources["A\nB"] == "Trad A\nB"
    assert sources["A B"] == "Trad A B"

    # Lookups return their respective exact match
    assert tm.lookup("A\nB") == "Trad A\nB"
    assert tm.lookup("A B") == "Trad A B"

    # Bulk lookups also distinguish both
    res = tm.lookup_many(["A\nB", "A B"])
    assert res["A\nB"] == "Trad A\nB"
    assert res["A B"] == "Trad A B"


def test_rows_written_in_a_run_have_game_set(tmp_path):
    """
    Acceptance criterion: Rows written in a run have game set.
    """
    db_path = str(tmp_path / "run_memory.db")
    input_dir = tmp_path / "SuperFantasyGame" / "data"
    output_dir = tmp_path / "SuperFantasyGame" / "output"
    process_dir = tmp_path / "SuperFantasyGame" / "process"
    input_dir.mkdir(parents=True)
    output_dir.mkdir(parents=True)
    process_dir.mkdir(parents=True)

    provider = DummyProvider(lang_source="en", lang_target="pt")
    provider.cache_path = db_path
    provider.cache = TranslationMemory(db_path, 'en', 'pt', engine=provider.id)

    # Set up extractor in game folder
    extractor = JsonExtractor(provider)
    extractor.folderInput = str(input_dir)
    extractor.folderOutput = str(output_dir)
    extractor.folderProcess = str(process_dir)

    test_file = input_dir / "Map001.json"
    test_file.write_text(json.dumps({"events": [{"text": ["Welcome to the kingdom", "Choose your path"]}]}))

    extractor.process_files()
    extractor.import_files()

    conn = sqlite3.connect(db_path)
    rows = conn.execute("SELECT source, target, game FROM segment").fetchall()
    conn.close()

    assert len(rows) >= 2
    for s, t, game in rows:
        assert game is not None
        assert len(game) > 0
        assert game == "SuperFantasyGame"
