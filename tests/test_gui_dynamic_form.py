import os
import pytest
from PyQt5.QtWidgets import QApplication, QLineEdit, QComboBox
from src.gui import TranslationGUI
from src.factory import ExtractorFactory, TranslatorFactory


@pytest.fixture(scope="session")
def qapp():
    os.environ.setdefault("QT_QPA_PLATFORM", "offscreen")
    app = QApplication.instance()
    if app is None:
        app = QApplication(["-platform", "offscreen"])
    return app


class MockPluginExtractor:
    name = "MockDynamicExtractor"
    display_name = "Mock Dynamic Extractor"

    @classmethod
    def get_interactive_questions(cls):
        return [
            {
                "key": "target_font",
                "question": "Selecione a fonte:",
                "options": ["Arial", "Tahoma", "MS Gothic"],
                "default": "Tahoma",
                "required": True
            },
            {
                "key": "extra_notes",
                "question": "Notas adicionais:",
                "required": False,
                "default": "Nota padrão"
            }
        ]


class MockPluginTranslator:
    agent = "MockDynamicTranslator"
    display_name = "Mock Dynamic Translator"

    @classmethod
    def get_interactive_questions(cls):
        return [
            {
                "key": "api_key",
                "question": "Chave de API:",
                "required": True,
                "default": ""
            },
            {
                "key": "use_proxy",
                "question": "Usar proxy?",
                "options": {
                    "Não": False,
                    "Sim": True
                },
                "default": "Sim"
            }
        ]


@pytest.fixture
def gui_window(qapp, monkeypatch):
    monkeypatch.setattr(ExtractorFactory, "get_available", lambda: {"MockDynamicExtractor": MockPluginExtractor})
    monkeypatch.setattr(TranslatorFactory, "get_available", lambda: {"MockDynamicTranslator": MockPluginTranslator})

    window = TranslationGUI(lang_options={"Português": "pt", "Inglês": "en"})
    yield window
    window.close()


def test_dynamic_form_widget_generation(gui_window):
    assert "target_font" in gui_window.dynamic_widgets
    assert "extra_notes" in gui_window.dynamic_widgets
    assert "api_key" in gui_window.dynamic_widgets
    assert "use_proxy" in gui_window.dynamic_widgets

    # target_font é um QComboBox
    font_widget = gui_window.dynamic_widgets["target_font"]["widget"]
    assert isinstance(font_widget, QComboBox)
    assert font_widget.currentText() == "Tahoma"
    assert font_widget.count() == 3

    # extra_notes é um QLineEdit comum
    notes_widget = gui_window.dynamic_widgets["extra_notes"]["widget"]
    assert isinstance(notes_widget, QLineEdit)
    assert notes_widget.echoMode() == QLineEdit.Normal
    assert notes_widget.text() == "Nota padrão"

    # api_key é um campo de senha / chave
    api_widget = gui_window.dynamic_widgets["api_key"]["widget"]
    assert isinstance(api_widget, QLineEdit)
    assert api_widget.echoMode() == QLineEdit.Password

    # use_proxy é um QComboBox com dict options
    proxy_widget = gui_window.dynamic_widgets["use_proxy"]["widget"]
    assert isinstance(proxy_widget, QComboBox)
    assert proxy_widget.currentText() == "Sim"
    assert proxy_widget.currentData() is True


def test_dynamic_form_validation(gui_window):
    # Inicialmente, api_key é vazia e required=True
    valid, err = gui_window.validate_dynamic_form()
    assert valid is False
    assert "api_key" in err or "Chave de API" in err

    # Preenchendo api_key
    gui_window.dynamic_widgets["api_key"]["widget"].setText("secret_token_123")
    valid, err = gui_window.validate_dynamic_form()
    assert valid is True
    assert err is None


def test_dynamic_config_extraction(gui_window):
    gui_window.dynamic_widgets["api_key"]["widget"].setText("minha_chave")
    gui_window.dynamic_widgets["target_font"]["widget"].setCurrentText("Arial")
    gui_window.dynamic_widgets["extra_notes"]["widget"].setText("Custom Note")

    cfg = gui_window.get_dynamic_config()
    assert cfg["extractor_config"]["target_font"] == "Arial"
    assert cfg["extractor_config"]["extra_notes"] == "Custom Note"
    assert cfg["translator_config"]["api_key"] == "minha_chave"
    assert cfg["translator_config"]["use_proxy"] is True


def test_metadata_indicator(gui_window, tmp_path):
    from src.services import GameMetadata
    game_dir = tmp_path / "game_with_meta"
    game_dir.mkdir()
    (game_dir / "data.json").write_text("{}", encoding="utf-8")

    meta = GameMetadata.build_meta(
        str(game_dir), None, ["json"],
        origin=GameMetadata.ORIGIN_JOB,
        extractor="json",
        translator="Google",
        lang_source="en",
        lang_target="pt",
    )
    GameMetadata.save_meta(str(game_dir), meta)

    gui_window.auto_detect_game_type(str(game_dir))
    assert gui_window.metadata_indicator.text() != ""
    assert "Google" in gui_window.metadata_indicator.text()
    assert "en" in gui_window.metadata_indicator.text()
    assert "pt" in gui_window.metadata_indicator.text()


def test_no_metadata_indicator(gui_window, tmp_path):
    clean_dir = tmp_path / "clean_game"
    clean_dir.mkdir()

    gui_window.metadata_indicator.setText("Previous Status")
    gui_window.auto_detect_game_type(str(clean_dir))
    assert gui_window.metadata_indicator.text() == ""
