import pytest

from src.job import JobConfig, TranslationJob
from src.workspace import Workspace
from src.factory import ExtractorFactory, TranslatorFactory


class MockDummyTranslator:
    agent = "MockDummyTranslate"
    display_name = "Mock Dummy Translator"

    def __init__(self, *args, **kwargs):
        self.lang_source = "en"
        self.lang_target = "pt"
        self.applied_config = {}
        self.synopsis = None

    def change_language(self, s, t):
        self.lang_source = s
        self.lang_target = t

    def set_game_synopsis(self, syn):
        self.synopsis = syn

    def apply_configuration(self, config):
        self.applied_config.update(config)

    def save_cache(self):
        pass


class MockDummyExtractor:
    name = "MockDummyExtractor"
    files_types = [".json"]

    def __init__(self, translate, workspace=None):
        self.translate = translate
        self.workspace = workspace
        self.observers = []
        self.applied_config = {}
        self.folderProcess = workspace.process_dir if workspace else "process"
        self.folderOutput = workspace.output_dir if workspace else "output"
        self.processed = False
        self.imported = False

    def apply_configuration(self, config):
        self.applied_config.update(config)

    def add_observer(self, callback):
        self.observers.append(callback)

    def init_folder(self=None):
        pass

    def process_files(self):
        self.processed = True
        for obs in self.observers:
            obs("status_update", {"file": "test.json", "status": "success", "msg": "Done"})

    def import_files(self):
        self.imported = True


@pytest.fixture(autouse=True)
def register_dummy_plugins(monkeypatch):
    monkeypatch.setattr(TranslatorFactory, "find_match", lambda name: "MockDummyTranslate" if "dummy" in name.lower() or name == "Google" else name)
    monkeypatch.setattr(TranslatorFactory, "create", lambda name, *args, **kwargs: MockDummyTranslator())
    monkeypatch.setattr(ExtractorFactory, "find_match", lambda name: "MockDummyExtractor" if "dummy" in name.lower() or name == "RPGMaker" else name)
    monkeypatch.setattr(ExtractorFactory, "create", lambda name, trans, *args, **kwargs: MockDummyExtractor(trans, workspace=kwargs.get("workspace")))


def test_job_config_validation():
    # Válido modo content
    cfg = JobConfig(extractor_name="MockDummyExtractor", translator_name="MockDummyTranslate", lang_source="en", lang_target="pt")
    assert cfg.validate() is True

    # Inválido: modo desconhecido
    with pytest.raises(ValueError, match="Modo inválido"):
        JobConfig(mode="audio").validate()

    # Inválido: content sem extractor
    with pytest.raises(ValueError, match="extractor_name é obrigatório"):
        JobConfig(mode="content", extractor_name=None).validate()

    # Inválido: mesma língua origem e destino
    with pytest.raises(ValueError, match="Idioma de origem e destino não podem ser iguais"):
        JobConfig(extractor_name="MockDummyExtractor", lang_source="pt", lang_target="pt").validate()

    # Válido: auto para pt
    assert JobConfig(extractor_name="MockDummyExtractor", lang_source="auto", lang_target="pt").validate() is True


def test_translation_job_initialization(tmp_path):
    input_dir = tmp_path / "game"
    input_dir.mkdir()

    cfg = JobConfig(
        extractor_name="MockDummyExtractor",
        translator_name="MockDummyTranslate",
        lang_source="ja",
        lang_target="pt",
        synopsis="Uma história mágica",
        extractor_config={"custom_opt": 123},
        translator_config={"api_key": "secret"}
    )
    with Workspace(str(input_dir)) as ws:
        job = TranslationJob(cfg, workspace=ws)
        assert job.workspace is ws
        assert job.translator.lang_source == "ja"
        assert job.translator.lang_target == "pt"
        assert job.translator.synopsis == "Uma história mágica"
        assert job.translator.applied_config == {"api_key": "secret"}
        assert job.extractor.applied_config == {"custom_opt": 123}
        assert job.extractor.workspace is ws


def test_translation_job_run_content_mode(tmp_path):
    input_dir = tmp_path / "game"
    input_dir.mkdir()
    (input_dir / "data.json").write_text("{}", encoding="utf-8")

    cfg = JobConfig(
        extractor_name="MockDummyExtractor",
        translator_name="MockDummyTranslate",
        lang_source="en",
        lang_target="pt",
        backup=False,
        verify=False
    )
    events = []
    with Workspace(str(input_dir)) as ws:
        job = TranslationJob(cfg, workspace=ws)
        job.add_observer(lambda evt, data: events.append((evt, data)))

        success = job.run(is_interactive=False)
        assert success is True
        assert job.extractor.processed is True
        assert job.extractor.imported is True
        assert any(evt == "status_update" for evt, _ in events)


def test_translation_job_run_filenames_mode(tmp_path, monkeypatch):
    input_dir = tmp_path / "game"
    input_dir.mkdir()

    called = {}

    def mock_process_directory(self, path, keep_original=False):
        called["input"] = path
        called["keep_original"] = keep_original

    from src.services.FileNameTranslator import FileNameTranslator
    monkeypatch.setattr(FileNameTranslator, "process_directory", mock_process_directory)

    cfg = JobConfig(
        mode="filenames",
        translator_name="MockDummyTranslate",
        lang_source="ja",
        lang_target="pt",
        keep_original=True,
        backup=False
    )
    with Workspace(str(input_dir)) as ws:
        job = TranslationJob(cfg, workspace=ws)
        success = job.run(is_interactive=False)
        assert success is True
        assert called["input"] == str(input_dir.resolve())
        assert called["keep_original"] is True


def test_translation_job_run_with_gui_signals(tmp_path):
    input_dir = tmp_path / "game"
    input_dir.mkdir()
    (input_dir / "data.json").write_text("{}", encoding="utf-8")

    class FakeSignals:
        class FakeEmitter:
            def __init__(self):
                self.messages = []

            def emit(self, *args):
                self.messages.append(args)

        def __init__(self):
            self.log = self.FakeEmitter()
            self.status_update = self.FakeEmitter()
            self.finished = self.FakeEmitter()

    cfg = JobConfig(
        extractor_name="MockDummyExtractor",
        translator_name="MockDummyTranslate",
        lang_source="en",
        lang_target="pt",
        backup=False,
        verify=False
    )
    with Workspace(str(input_dir)) as ws:
        job = TranslationJob(cfg, workspace=ws)
        sigs = FakeSignals()
        success = job.run(gui_signals=sigs)
        assert success is True
        # Garante que process_files() foi de fato chamado sob gui_signals
        assert job.extractor.processed is True
        assert job.extractor.imported is True


def test_translation_job_run_separate_output_dir(tmp_path):
    input_dir = tmp_path / "game_in"
    input_dir.mkdir()
    output_dir = tmp_path / "game_out"
    output_dir.mkdir()
    (input_dir / "data.json").write_text("{}", encoding="utf-8")

    cfg = JobConfig(
        extractor_name="MockDummyExtractor",
        translator_name="MockDummyTranslate",
        lang_source="en",
        lang_target="pt",
        input_dir=str(input_dir),
        output_dir=str(output_dir),
        backup=False,
        verify=False
    )
    with Workspace(str(input_dir), output_dir=str(output_dir)) as ws:
        job = TranslationJob(cfg, workspace=ws)
        success = job.run(is_interactive=False)
        assert success is True
        assert job.extractor.processed is True
        assert job.extractor.imported is True


def test_translation_job_custom_components(tmp_path):
    input_dir = tmp_path / "game"
    input_dir.mkdir()

    ext = MockDummyExtractor(None)
    trans = MockDummyTranslator()

    cfg = JobConfig(
        extractor_name="MockDummyExtractor",
        translator_name="MockDummyTranslate",
        lang_source="en",
        lang_target="pt",
        backup=False,
        verify=False
    )
    with Workspace(str(input_dir)) as ws:
        job = TranslationJob(cfg, workspace=ws, extractor=ext, translator=trans)
        assert job.extractor is ext
        assert job.translator is trans
        assert job.extractor.workspace is ws


def test_translation_worker_legacy_signature():
    from src.gui import TranslationWorker

    called = []

    def mock_run_func(extractor, translate, input_dir, lang_source, backup, verify,
                      gui_signals=None, gui_verify_callback=None):
        called.append((input_dir, lang_source, backup, verify))
        return True

    worker = TranslationWorker(
        mock_run_func,
        None,
        None,
        "/path/to/game",
        "ja",
        True,
        False
    )
    assert worker.run_process_func is mock_run_func
    assert worker.input_dir == "/path/to/game"
    assert worker.lang_source == "ja"
    assert worker.backup is True
    assert worker.verify is False


def test_factory_matches_class_names():
    from src.factory import ExtractorFactory, TranslatorFactory
    import src.extractor.JsonExtractor  # noqa: F401
    import src.translate.GoogleTranslate  # noqa: F401

    assert ExtractorFactory.find_match("JsonExtractor") in ("json", "JsonExtractor")
    assert TranslatorFactory.find_match("GoogleTranslate") in ("Google", "GoogleTranslate")


def test_paths_memory_db_and_settings_db(monkeypatch, tmp_path):
    from src.utils.paths import get_memory_db_path, get_settings_db_path

    custom_mem = tmp_path / "custom_mem.db"
    custom_set = tmp_path / "custom_set.db"

    monkeypatch.setenv("SLIME_MEMORY_DB", str(custom_mem))
    monkeypatch.setenv("SLIME_SETTINGS_DB", str(custom_set))

    assert get_memory_db_path() == str(custom_mem)
    assert get_settings_db_path() == str(custom_set)


def test_memory_only_no_network(tmp_path):
    import json
    from src.extractor.JsonExtractor import JsonExtractor

    input_dir = tmp_path / "game_offline"
    input_dir.mkdir()
    (input_dir / "test.json").write_text(json.dumps({"dialogue": "Uncached string"}), encoding="utf-8")

    class StrictNoNetworkTranslator:
        agent = "StrictNoNet"
        name = "StrictNoNet"
        lang_source = "en"
        lang_target = "pt"

        def __init__(self):
            self.network_called = False
            self.cache = {}

        def translate_batch(self, batch, callback=None):
            self.network_called = True
            raise AssertionError("Network call attempted in memory-only mode!")

        def translator(self, text, callback=None):
            self.network_called = True
            raise AssertionError("Network call attempted in memory-only mode!")

        def change_language(self, s, t):
            self.lang_source = s
            self.lang_target = t

        def save_cache(self):
            pass

    translator = StrictNoNetworkTranslator()
    cfg = JobConfig(
        extractor_name="json",
        translator_name="Google",
        lang_source="en",
        lang_target="pt",
        input_dir=str(input_dir),
        memory_only=True,
        backup=False,
        verify=False,
    )
    with Workspace(str(input_dir)) as ws:
        extractor = JsonExtractor(translator, workspace=ws)
        job = TranslationJob(cfg, workspace=ws, extractor=extractor, translator=translator)
        success = job.run(is_interactive=False)
        assert success is True
        assert translator.network_called is False


def test_memory_only_application(tmp_path):
    import json
    from src.extractor.JsonExtractor import JsonExtractor
    from src.translate.TranslationMemory import TranslationMemory

    input_dir = tmp_path / "game_offline_app"
    input_dir.mkdir()
    (input_dir / "test.json").write_text(
        json.dumps({"dialogue": "Hello", "message": "Farewell"}),
        encoding="utf-8"
    )

    db_path = str(tmp_path / "offline_memory.db")
    TranslationMemory._reset_pools()
    tm = TranslationMemory(db_path, "en", "pt", engine="StrictNet")
    tm.store("Hello", "Olá")

    class StrictNetTranslator:
        agent = "StrictNet"
        name = "StrictNet"
        lang_source = "en"
        lang_target = "pt"

        def __init__(self, cache):
            self.cache = cache
            self.network_called = False

        def translate_batch(self, batch, callback=None):
            self.network_called = True
            raise AssertionError("Network call attempted in memory-only mode!")

        def translator(self, text, callback=None):
            self.network_called = True
            raise AssertionError("Network call attempted in memory-only mode!")

        def change_language(self, s, t):
            self.lang_source = s
            self.lang_target = t

        def save_cache(self):
            pass

    translator = StrictNetTranslator(tm)
    cfg = JobConfig(
        extractor_name="json",
        translator_name="Google",
        lang_source="en",
        lang_target="pt",
        input_dir=str(input_dir),
        memory_only=True,
        backup=False,
        verify=False,
    )
    with Workspace(str(input_dir)) as ws:
        extractor = JsonExtractor(translator, workspace=ws)
        job = TranslationJob(cfg, workspace=ws, extractor=extractor, translator=translator)
        success = job.run(is_interactive=False)
        assert success is True
        assert translator.network_called is False

        result_data = json.loads((input_dir / "test.json").read_text(encoding="utf-8"))
        assert result_data["dialogue"] == "Olá"
        assert result_data["message"] == "Farewell"


def test_incremental_skips_intact(tmp_path):
    import os
    import json
    from src.extractor.JsonExtractor import JsonExtractor
    from src.services import GameMetadata

    orig_dir = tmp_path / "original"
    orig_dir.mkdir()
    trans_dir = tmp_path / "translated"
    trans_dir.mkdir()

    (orig_dir / "intact.json").write_text(json.dumps({"dialogue": "Original Intact"}), encoding="utf-8")
    (trans_dir / "intact.json").write_text(json.dumps({"dialogue": "Traduzido Intacto"}), encoding="utf-8")

    (orig_dir / "changed.json").write_text(json.dumps({"dialogue": "Original Initial"}), encoding="utf-8")
    (trans_dir / "changed.json").write_text(json.dumps({"dialogue": "Traduzido Initial"}), encoding="utf-8")

    meta = GameMetadata.build_meta(
        str(trans_dir), str(orig_dir), ["json"],
        origin=GameMetadata.ORIGIN_JOB,
        extractor="json",
        translator="MockTranslator",
        lang_source="en",
        lang_target="pt",
    )
    GameMetadata.save_meta(str(trans_dir), meta)

    (orig_dir / "changed.json").write_text(json.dumps({"dialogue": "Original Updated"}), encoding="utf-8")

    processed_files = []

    class TrackingTranslator:
        agent = "TrackingTrans"
        name = "TrackingTrans"
        lang_source = "en"
        lang_target = "pt"
        cache = {}

        def translate_batch(self, batch, callback=None):
            return [f"Tr-{s}" for s in batch]

        def translator(self, text, callback=None):
            return f"Tr-{text}"

        def change_language(self, s, t):
            self.lang_source = s
            self.lang_target = t

        def save_cache(self):
            pass

    translator = TrackingTranslator()
    cfg = JobConfig(
        extractor_name="json",
        translator_name="Google",
        lang_source="en",
        lang_target="pt",
        input_dir=str(orig_dir),
        output_dir=str(trans_dir),
        incremental=True,
        backup=False,
        verify=False,
    )
    with Workspace(str(orig_dir), output_dir=str(trans_dir)) as ws:
        extractor = JsonExtractor(translator, workspace=ws)
        orig_process_file = extractor.process_file

        def tracking_process_file(file, *args, **kwargs):
            processed_files.append(os.path.basename(str(file)))
            return orig_process_file(file, *args, **kwargs)

        extractor.process_file = tracking_process_file

        job = TranslationJob(cfg, workspace=ws, extractor=extractor, translator=translator)
        success = job.run(is_interactive=False)
        assert success is True

    assert "intact.json" not in processed_files
    assert "changed.json" in processed_files

    intact_content = json.loads((trans_dir / "intact.json").read_text(encoding="utf-8"))
    assert intact_content["dialogue"] == "Traduzido Intacto"


def test_incremental_updates_metadata(tmp_path):
    import json
    from src.extractor.JsonExtractor import JsonExtractor
    from src.services import GameMetadata

    orig_dir = tmp_path / "original_meta"
    orig_dir.mkdir()
    trans_dir = tmp_path / "translated_meta"
    trans_dir.mkdir()

    (orig_dir / "intact.json").write_text(json.dumps({"dialogue": "Original Intact"}), encoding="utf-8")
    (trans_dir / "intact.json").write_text(json.dumps({"dialogue": "Traduzido Intacto"}), encoding="utf-8")

    (orig_dir / "changed.json").write_text(json.dumps({"dialogue": "Original Initial"}), encoding="utf-8")
    (trans_dir / "changed.json").write_text(json.dumps({"dialogue": "Traduzido Initial"}), encoding="utf-8")

    past_ts = "2020-01-01T00:00:00"
    meta = GameMetadata.build_meta(
        str(trans_dir), str(orig_dir), ["json"],
        origin=GameMetadata.ORIGIN_JOB,
        extractor="json",
        translator="MockTranslator",
        lang_source="en",
        lang_target="pt",
    )
    meta["created_at"] = past_ts
    meta["updated_at"] = past_ts
    GameMetadata.save_meta(str(trans_dir), meta)

    (orig_dir / "changed.json").write_text(json.dumps({"dialogue": "Brand New Content"}), encoding="utf-8")

    class SimpleTranslator:
        agent = "SimpleTrans"
        name = "SimpleTrans"
        lang_source = "en"
        lang_target = "pt"
        cache = {}

        def translate_batch(self, batch, callback=None):
            return [f"Tr-{s}" for s in batch]

        def translator(self, text, callback=None):
            return f"Tr-{text}"

        def change_language(self, s, t):
            self.lang_source = s
            self.lang_target = t

        def save_cache(self):
            pass

    translator = SimpleTranslator()
    cfg = JobConfig(
        extractor_name="json",
        translator_name="Google",
        lang_source="en",
        lang_target="pt",
        input_dir=str(orig_dir),
        output_dir=str(trans_dir),
        incremental=True,
        backup=False,
        verify=False,
    )
    with Workspace(str(orig_dir), output_dir=str(trans_dir)) as ws:
        extractor = JsonExtractor(translator, workspace=ws)
        job = TranslationJob(cfg, workspace=ws, extractor=extractor, translator=translator)
        success = job.run(is_interactive=False)
        assert success is True

    updated_meta = GameMetadata.load_meta(str(trans_dir))
    assert updated_meta is not None
    assert updated_meta["created_at"] == past_ts
    assert updated_meta["updated_at"] > past_ts

    expected_new_hash = GameMetadata.sha256_file(str(trans_dir / "changed.json"))
    assert updated_meta["files"]["changed.json"]["translated_sha256"] == expected_new_hash
