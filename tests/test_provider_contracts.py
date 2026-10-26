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
def fake_provider():
    return TinyFakeProvider()


@pytest.fixture
def google_provider():
    gt = GoogleTranslate(lang_source="en", lang_target="pt")
    def mock_post(url, *args, **kwargs):
        data = kwargs.get('data', {})
        text = data.get('q', '')
        resp = MagicMock()
        resp.status_code = 200
        parts = text.split(gt.delimiter)
        resp.json.return_value = [[[f"TR:{p}", p] for p in parts], None, "en"]
        resp.raise_for_status = MagicMock()
        return resp

    with patch("requests.Session.post", side_effect=mock_post):
        yield gt


@pytest.fixture
def deepl_provider():
    dt = DeeplTranslate(lang_source="en", lang_target="pt")
    dt.api_key = "dummy:fx"
    def mock_post(url, *args, **kwargs):
        data = kwargs.get('data', {})
        texts = data.get('text', [])
        resp = MagicMock()
        resp.status_code = 200
        resp.json.return_value = {
            "translations": [{"text": f"TR:{t}"} for t in texts]
        }
        resp.raise_for_status = MagicMock()
        return resp

    with patch("requests.post", side_effect=mock_post):
        yield dt


@pytest.fixture
def ollama_provider(monkeypatch):
    monkeypatch.setenv("OLLAMA_MODEL", "dummy-model")
    monkeypatch.setenv("OLLAMA_MAX_REQUESTS", "5")
    ot = OllamaTranslate(lang_source="en", lang_target="pt")
    with patch("ollama.chat", side_effect=lambda model, messages: {"message": {"content": f"TR:{messages[-1]['content']}"}}):
        yield ot


@pytest.mark.parametrize("provider_name", [
    "fake_provider",
    "google_provider",
    "deepl_provider",
    "ollama_provider",
])
def test_shared_provider_contract_suite(provider_name, request):
    """
    Acceptance criteria (#17, #18):
    - Shared contract tests run against every provider with mocked HTTP.
    - A test provider written in under 30 lines passes the SAME contract test suite as Google, DeepL and Ollama.
    """
    provider = request.getfixturevalue(provider_name)

    # 1. Implements TranslationProvider Protocol
    assert isinstance(provider, TranslationProvider)
    assert isinstance(provider.id, str) and len(provider.id) > 0
    assert isinstance(provider.capabilities, Capabilities)
    assert provider.capabilities.max_request_chars > 0

    # 2. Empty segments returns []
    assert provider.translate([]) == []

    # 3. List of segments returns translated strings matching length, type and order
    segs = [
        Segment(id="1", source="Hello", context={'kind': 'dialogue'}),
        Segment(id="2", source="World", context={'kind': 'dialogue'}),
    ]
    res = provider.translate(segs)
    assert isinstance(res, list)
    assert len(res) == len(segs)
    assert all(isinstance(t, str) for t in res)
    assert res == ["TR:Hello", "TR:World"]


def test_provider_specific_capabilities(fake_provider, google_provider, deepl_provider, ollama_provider):
    assert fake_provider.id == "fake"
    assert fake_provider.capabilities.native_batch is True

    assert google_provider.id == "google"
    assert google_provider.capabilities.native_batch is False

    assert deepl_provider.id == "deepl"
    assert deepl_provider.capabilities.native_batch is True
    assert deepl_provider.capabilities.tag_handling == "xml"

    assert ollama_provider.id == "ollama"
    assert ollama_provider.capabilities.context is True
