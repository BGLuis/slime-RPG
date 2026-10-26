import pytest
from unittest.mock import MagicMock, patch
from src.extractor.Segment import Segment
from src.translate.Provider import Capabilities, TranslationProvider
from src.translate.GoogleTranslate import GoogleTranslate
from src.translate.DeeplTranslate import DeeplTranslate
from src.translate.OllamaTranslate import OllamaTranslate


class TinyFakeProvider:
    id = "fake"
    capabilities = Capabilities(max_request_chars=2000, max_items=10, native_batch=True)

    def translate(self, segments, ctx=None):
        return [f"TR:{s.source if hasattr(s, 'source') else s}" for s in segments]


@pytest.fixture
def mock_google_provider():
    gt = GoogleTranslate(lang_source="en", lang_target="pt")
    gt._translate_raw = MagicMock(side_effect=lambda text: "\n<span> </span>\n".join(f"TR:{t}" for t in text.split("\n<span> </span>\n")))
    return gt


@pytest.fixture
def mock_deepl_provider():
    dt = DeeplTranslate(lang_source="en", lang_target="pt")
    dt.api_key = "dummy:fx"
    mock_resp = MagicMock()
    mock_resp.status_code = 200
    mock_resp.json.return_value = {
        "translations": [{"text": "TR:Hello"}, {"text": "TR:World"}]
    }
    with patch("requests.post", return_value=mock_resp):
        yield dt


@pytest.fixture
def mock_ollama_provider(monkeypatch):
    monkeypatch.setenv("OLLAMA_MODEL", "dummy-model")
    monkeypatch.setenv("OLLAMA_MAX_REQUESTS", "5")
    ot = OllamaTranslate(lang_source="en", lang_target="pt")
    ot.chat = MagicMock()
    with patch("ollama.chat", side_effect=lambda model, messages: {"message": {"content": f"TR:{messages[-1]['content']}"}}):
        yield ot


def test_tiny_fake_provider_satisfies_contract():
    fake = TinyFakeProvider()
    assert isinstance(fake, TranslationProvider)
    assert fake.id == "fake"
    assert isinstance(fake.capabilities, Capabilities)
    assert fake.capabilities.max_request_chars == 2000
    assert fake.capabilities.native_batch is True

    segs = [
        Segment(id="1", source="Hello", context={'kind': 'dialogue'}),
        Segment(id="2", source="World", context={'kind': 'dialogue'}),
    ]
    res = fake.translate(segs)
    assert res == ["TR:Hello", "TR:World"]
    assert fake.translate([]) == []


@pytest.mark.parametrize("provider_factory", [
    lambda: TinyFakeProvider(),
    lambda: GoogleTranslate.from_languages("en", "pt"),
    lambda: DeeplTranslate.from_languages("en", "pt"),
    lambda: OllamaTranslate.from_languages("en", "pt"),
])
def test_all_providers_implement_protocol_and_capabilities(provider_factory, monkeypatch):
    monkeypatch.setenv("OLLAMA_MODEL", "dummy")
    monkeypatch.setenv("OLLAMA_MAX_REQUESTS", "4")
    provider = provider_factory()
    assert isinstance(provider, TranslationProvider)
    assert isinstance(provider.id, str) and len(provider.id) > 0
    assert isinstance(provider.capabilities, Capabilities)
    assert provider.capabilities.max_request_chars > 0


def test_google_provider_contract(mock_google_provider):
    p = mock_google_provider
    assert p.id == "google"
    assert p.capabilities.native_batch is False

    segs = [
        Segment(id="1", source="Hello", context={'kind': 'dialogue'}),
        Segment(id="2", source="World", context={'kind': 'dialogue'}),
    ]
    res = p.translate(segs)
    assert res == ["TR:Hello", "TR:World"]
    assert p.translate([]) == []


def test_deepl_provider_contract(mock_deepl_provider):
    p = mock_deepl_provider
    assert p.id == "deepl"
    assert p.capabilities.native_batch is True
    assert p.capabilities.tag_handling == "xml"

    segs = [
        Segment(id="1", source="Hello", context={'kind': 'dialogue'}),
        Segment(id="2", source="World", context={'kind': 'dialogue'}),
    ]
    with patch("requests.post") as mock_post:
        mock_resp = MagicMock()
        mock_resp.status_code = 200
        mock_resp.json.return_value = {
            "translations": [{"text": "TR:Hello"}, {"text": "TR:World"}]
        }
        mock_post.return_value = mock_resp
        res = p.translate(segs)
        assert res == ["TR:Hello", "TR:World"]
        assert p.translate([]) == []


def test_ollama_provider_contract(mock_ollama_provider):
    p = mock_ollama_provider
    assert p.id == "ollama"
    assert p.capabilities.context is True

    segs = [
        Segment(id="1", source="Hello", context={'kind': 'dialogue'}),
        Segment(id="2", source="World", context={'kind': 'dialogue'}),
    ]
    with patch("ollama.chat", side_effect=lambda model, messages: {"message": {"content": f"TR:{messages[-1]['content']}"}}):
        res = p.translate(segs)
        assert res == ["TR:Hello", "TR:World"]
        assert p.translate([]) == []
