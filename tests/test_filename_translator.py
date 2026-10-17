import inspect
from src.services.FileNameTranslator import (
    FileNameTranslator,
    FileNameTranslationResult,
    sanitize_filename,
)


class DummyTranslator:
    def __init__(self, mapping=None, default=None):
        self.mapping = mapping or {}
        self.default = default

    def translate_batch(self, batch, progress_callback=None):
        result = []
        for text in batch:
            if text in self.mapping:
                result.append(self.mapping[text])
            elif self.default is not None:
                result.append(self.default)
            else:
                result.append(text)
        return result


def test_filename_translator_does_not_import_cli():
    import src.services.FileNameTranslator as module
    source = inspect.getsource(module)
    assert "from src import cli" not in source
    assert "import cli" not in source


def test_filename_translator_init_does_not_create_output_folder(tmp_path):
    output_dir = tmp_path / "nonexistent_output"
    assert not output_dir.exists()
    translator = FileNameTranslator(DummyTranslator(), folder_output=str(output_dir))
    assert not output_dir.exists(), "Init must not create output directory"
    assert translator.folderOutput == str(output_dir)


def test_sanitize_filename_basic_and_edge_cases():
    assert sanitize_filename("simple", ".txt") == "simple.txt"
    assert sanitize_filename("A/B", ".txt") == "A_B.txt"
    assert sanitize_filename("A\\B:C*D?E<F>G|H", ".dat") == "A_B_C_D_E_F_G_H.dat"
    assert sanitize_filename("   ...   ", ".json") == "unnamed.json"
    assert sanitize_filename("CON", ".txt") == "_CON.txt"
    assert sanitize_filename("con", ".txt") == "_con.txt"
    assert sanitize_filename("AUX", ".png") == "_AUX.png"
    assert sanitize_filename("NUL") == "_NUL"
    assert sanitize_filename("COM1.tar", ".gz") == "_COM1.tar.gz"


def test_collision_resolution_produces_distinct_files(tmp_path):
    input_dir = tmp_path / "input"
    output_dir = tmp_path / "output"
    input_dir.mkdir()

    # Create two files that will translate to the exact same name
    file_a = input_dir / "first.txt"
    file_b = input_dir / "second.txt"
    file_a.write_text("Content of first file", encoding="utf-8")
    file_b.write_text("Content of second file", encoding="utf-8")

    dummy_translator = DummyTranslator(mapping={"first": "item", "second": "item"})
    fn_translator = FileNameTranslator(dummy_translator, folder_output=str(output_dir))

    result = fn_translator.process_directory(str(input_dir))

    assert isinstance(result, FileNameTranslationResult)
    assert result.total_files == 2
    assert result.translated_count == 2
    assert result.collisions_resolved == 1

    dst_1 = output_dir / "item.txt"
    dst_2 = output_dir / "item_1.txt"

    assert dst_1.exists()
    assert dst_2.exists()
    assert dst_1.read_text(encoding="utf-8") == "Content of first file"
    assert dst_2.read_text(encoding="utf-8") == "Content of second file"


def test_slash_backslash_colon_and_reserved_names_sanitized_without_subfolders(tmp_path):
    input_dir = tmp_path / "input"
    output_dir = tmp_path / "output"
    input_dir.mkdir()

    # Create files
    file_slash = input_dir / "slash.txt"
    file_slash.write_text("Slash content", encoding="utf-8")
    file_con = input_dir / "con_test.txt"
    file_con.write_text("CON content", encoding="utf-8")
    file_colon = input_dir / "colon_test.txt"
    file_colon.write_text("Colon content", encoding="utf-8")

    dummy_translator = DummyTranslator(mapping={
        "slash": "A/B",
        "con_test": "CON",
        "colon_test": "foo:bar",
    })
    fn_translator = FileNameTranslator(dummy_translator, folder_output=str(output_dir))

    result = fn_translator.process_directory(str(input_dir))

    assert isinstance(result, FileNameTranslationResult)
    assert result.translated_count == 3

    # Ensure no subfolder 'A' was created
    assert not (output_dir / "A").exists()
    assert (output_dir / "A_B.txt").exists()
    assert (output_dir / "_CON.txt").exists()
    assert (output_dir / "foo_bar.txt").exists()


def test_process_directory_with_keep_original(tmp_path):
    input_dir = tmp_path / "input"
    output_dir = tmp_path / "output"
    input_dir.mkdir()

    f1 = input_dir / "orig.txt"
    f1.write_text("Original content", encoding="utf-8")

    dummy_translator = DummyTranslator(mapping={"orig": "traduzido"})
    fn_translator = FileNameTranslator(dummy_translator, folder_output=str(output_dir))

    result = fn_translator.process_directory(str(input_dir), keep_original=True)

    assert result.translated_count == 1
    assert result.copied_originals == 1
    assert (output_dir / "traduzido.txt").exists()
    assert (output_dir / "orig.txt").exists()


def test_process_directory_log_callback(tmp_path):
    input_dir = tmp_path / "input"
    output_dir = tmp_path / "output"
    input_dir.mkdir()

    f1 = input_dir / "alpha.txt"
    f1.write_text("Alpha", encoding="utf-8")

    dummy_translator = DummyTranslator(mapping={"alpha": "beta"})
    fn_translator = FileNameTranslator(dummy_translator, folder_output=str(output_dir))

    logs = []

    def log_cb(msg, color):
        logs.append((msg, color))

    result = fn_translator.process_directory(str(input_dir), log_callback=log_cb)
    assert result.translated_count == 1
    assert len(logs) > 0
    assert any("Traduzindo" in msg for msg, _ in logs)


def test_process_directory_nonexistent_and_empty(tmp_path):
    dummy_translator = DummyTranslator()
    fn_translator = FileNameTranslator(dummy_translator, folder_output=str(tmp_path / "output"))

    # Nonexistent
    res_nonexistent = fn_translator.process_directory(str(tmp_path / "does_not_exist"))
    assert res_nonexistent.total_files == 0
    assert res_nonexistent.translated_count == 0

    # Empty
    empty_dir = tmp_path / "empty"
    empty_dir.mkdir()
    res_empty = fn_translator.process_directory(str(empty_dir))
    assert res_empty.total_files == 0
    assert res_empty.translated_count == 0
