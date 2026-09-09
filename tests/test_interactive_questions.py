from unittest.mock import patch, MagicMock
import pytest

from main import prompt_interactive_questions
from src.extractor.wolfrpg.WolfRPGExtractor import WolfRPGExtractor


class MockTranslator:
    def __init__(self):
        self.cache = {}
        import threading
        self.cache_lock = threading.Lock()

    def reduce_limite(self):
        pass

    def __deepcopy__(self, memo):
        return self


def test_prompt_interactive_questions_empty():
    assert prompt_interactive_questions([]) == {}
    assert prompt_interactive_questions(None) == {}


def test_prompt_interactive_questions_with_options(monkeypatch):
    questions = [
        {
            "key": "target_font",
            "question": "Selecione a fonte:",
            "options": ["Padrão", "Arial", "Tahoma"],
            "default": "Tahoma"
        }
    ]

    called = {}
    def mock_select_option(title, options, index=0):
        called["title"] = title
        called["options"] = options
        called["index"] = index
        return options[index]

    import src.cli as cli
    monkeypatch.setattr(cli, "select_option", mock_select_option)

    config = prompt_interactive_questions(questions)
    assert config == {"target_font": "Tahoma"}
    assert called["title"] == "Selecione a fonte:"
    assert called["options"] == ["Padrão", "Arial", "Tahoma"]
    assert called["index"] == 2  # Tahoma index


def test_prompt_interactive_questions_with_id_fallback(monkeypatch):
    """Garante retrocompatibilidade caso a pergunta possua apenas 'id'."""
    questions = [
        {
            "id": "target_font",
            "question": "Selecione a fonte:",
            "options": ["Padrão", "MS UI Gothic"],
            "default": "MS UI Gothic"
        }
    ]

    import src.cli as cli
    monkeypatch.setattr(cli, "select_option", lambda title, options, index=0: options[index])

    config = prompt_interactive_questions(questions)
    assert config == {"target_font": "MS UI Gothic"}


def test_prompt_interactive_questions_options_exit_returns_none(monkeypatch):
    questions = [
        {
            "key": "mode",
            "question": "Escolha:",
            "options": ["A", "B"]
        }
    ]

    import src.cli as cli
    monkeypatch.setattr(cli, "select_option", lambda title, options, index=0: "Exit")

    result = prompt_interactive_questions(questions)
    assert result is None


def test_prompt_interactive_questions_text_input(monkeypatch):
    questions = [
        {
            "key": "synopsis",
            "question": "Digite a sinopse:",
            "required": False
        }
    ]

    monkeypatch.setattr("builtins.input", lambda: "Jogo de RPG épico")
    config = prompt_interactive_questions(questions)
    assert config == {"synopsis": "Jogo de RPG épico"}


def test_prompt_interactive_questions_text_default_fallback(monkeypatch):
    questions = [
        {
            "key": "delimiter",
            "question": "Delimitador:",
            "default": ",",
            "required": False
        }
    ]

    monkeypatch.setattr("builtins.input", lambda: "")
    config = prompt_interactive_questions(questions)
    assert config == {"delimiter": ","}


def test_prompt_interactive_questions_required_validation_fails(monkeypatch):
    questions = [
        {
            "key": "api_key",
            "question": "Chave de API:",
            "required": True
        }
    ]

    monkeypatch.setattr("builtins.input", lambda: "")
    result = prompt_interactive_questions(questions)
    assert result is None


def test_wolfrpg_questions_integration_with_apply_configuration(monkeypatch):
    """Testa o fluxo completo: get_interactive_questions -> prompt_interactive_questions -> apply_configuration."""
    questions = WolfRPGExtractor.get_interactive_questions()

    # Simula o usuário escolhendo Tahoma e repack .wolf
    responses = {
        "Deseja substituir a fonte padrão no Game.dat para melhor exibição de caracteres ocidentais?": "Tahoma",
        "Como deseja gerar a saída final dos arquivos traduzidos?": "Empacotar arquivo Data.wolf (.wolf repack)"
    }

    import src.cli as cli
    monkeypatch.setattr(cli, "select_option", lambda title, options, index=0: responses.get(title, options[index]))

    config = prompt_interactive_questions(questions)
    assert config is not None
    assert config["target_font"] == "Tahoma"
    assert config["repack_mode"] == "Empacotar arquivo Data.wolf (.wolf repack)"

    extractor = WolfRPGExtractor(MockTranslator())
    extractor.apply_configuration(config)

    assert extractor.target_font == "Tahoma"
    assert extractor.repack_wolf is True
