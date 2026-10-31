import pytest
from src.factory import (
    ExtractorFactory,
    TranslatorFactory,
    AmbiguousPluginError,
)
import src.extractor  # noqa: F401
import src.translate  # noqa: F401


def test_standardized_plugin_ids_in_available():
    extractors = ExtractorFactory.get_available()
    for eid in ['rpgmaker', 'wolfrpg', 'csv', 'json']:
        assert eid in extractors

    translators = TranslatorFactory.get_available()
    for tid in ['google', 'deepl', 'ollama']:
        assert tid in translators


def test_legacy_aliases_accessible_via_available():
    extractors = ExtractorFactory.get_available()
    assert 'RPG Maker' in extractors
    assert 'Wolf RPG' in extractors
    assert 'CSV' in extractors
    assert 'Json' in extractors

    translators = TranslatorFactory.get_available()
    assert 'googleTraslator' in translators
    assert 'deeplTranslator' in translators
    assert 'ollamaTranslator' in translators


def test_find_match_exact_and_clean():
    assert ExtractorFactory.find_match('rpgmaker') == 'rpgmaker'
    assert ExtractorFactory.find_match('RPG Maker') == 'rpgmaker'
    assert ExtractorFactory.find_match('rpg_maker') == 'rpgmaker'
    assert ExtractorFactory.find_match('wolfrpg') == 'wolfrpg'
    assert ExtractorFactory.find_match('Wolf RPG') == 'wolfrpg'
    assert ExtractorFactory.find_match('csv') == 'csv'
    assert ExtractorFactory.find_match('json') == 'json'

    assert TranslatorFactory.find_match('google') == 'google'
    assert TranslatorFactory.find_match('googleTraslator') == 'google'
    assert TranslatorFactory.find_match('googleTranslator') == 'google'
    assert TranslatorFactory.find_match('deepl') == 'deepl'
    assert TranslatorFactory.find_match('deeplTranslator') == 'deepl'
    assert TranslatorFactory.find_match('ollama') == 'ollama'


def test_find_match_ambiguous_translator_raises_error():
    with pytest.raises(AmbiguousPluginError) as exc_info:
        TranslatorFactory.find_match('translator')
    err_msg = str(exc_info.value)
    assert 'google' in err_msg
    assert 'deepl' in err_msg


def test_find_match_ambiguous_extractor_raises_error():
    with pytest.raises(AmbiguousPluginError) as exc_info:
        ExtractorFactory.find_match('rpg')
    err_msg = str(exc_info.value)
    assert 'rpgmaker' in err_msg
    assert 'wolfrpg' in err_msg


def test_find_match_substring_matching_removed():
    # Substrings that only match a single plugin must NOT resolve silently anymore
    assert TranslatorFactory.find_match('g') is None
    assert ExtractorFactory.find_match('make') is None
    assert ExtractorFactory.find_match('unknown_plugin') is None


def test_find_match_whitespace_and_punctuation_returns_none():
    assert TranslatorFactory.find_match('   ') is None
    assert TranslatorFactory.find_match(' - ') is None
    assert TranslatorFactory.find_match('___') is None
    assert ExtractorFactory.find_match('   ') is None
    assert ExtractorFactory.find_match(' - ') is None


def test_available_plugins_dict_case_and_punctuation_insensitive():
    extractors = ExtractorFactory.get_available()
    assert 'RPG maker' in extractors
    assert 'rpg_maker' in extractors
    assert 'rpgmaker' in extractors
    assert extractors['RPG maker'].name == 'rpgmaker'
    assert extractors.get('wolf rpg').name == 'wolfrpg'

    translators = TranslatorFactory.get_available()
    assert 'GoogleTranslator' in translators
    assert 'google_translator' in translators
    assert translators['GoogleTranslator'].agent == 'google'
    assert translators.get('deepl_translator').agent == 'deepl'


def test_entry_points_discovery_custom_plugin(monkeypatch):
    from unittest.mock import MagicMock

    class CustomExtractor:
        name = "custom_ext"

    class CustomTranslator:
        agent = "custom_trans"

    ep_ext = MagicMock()
    ep_ext.name = "custom_ext"
    ep_ext.load.return_value = CustomExtractor

    ep_trans = MagicMock()
    ep_trans.name = "custom_trans"
    ep_trans.load.return_value = CustomTranslator

    def fake_entry_points(group=None):
        if group == 'slime.extractors':
            return [ep_ext]
        elif group == 'slime.providers':
            return [ep_trans]
        return []

    import importlib.metadata as im
    monkeypatch.setattr(im, "entry_points", fake_entry_points)

    ExtractorFactory.discover(force=True)
    TranslatorFactory.discover(force=True)

    extractors = ExtractorFactory.get_available()
    assert 'custom_ext' in extractors
    assert extractors['custom_ext'] == CustomExtractor

    translators = TranslatorFactory.get_available()
    assert 'custom_trans' in translators
    assert translators['custom_trans'] == CustomTranslator


def test_pyproject_toml_configuration():
    import os
    import tomllib
    import main

    pyproject_path = os.path.join(
        os.path.dirname(os.path.dirname(os.path.abspath(__file__))),
        "pyproject.toml"
    )
    assert os.path.exists(pyproject_path)

    with open(pyproject_path, "rb") as f:
        data = tomllib.load(f)

    # PEP 517/621 checks
    assert data["build-system"]["build-backend"] == "setuptools.build_meta"
    assert data["project"]["name"] == "slime-rpg"
    assert "slime" in data["project"]["scripts"]
    assert data["project"]["scripts"]["slime"] == "main:main"

    # Entry points
    eps = data["project"]["entry-points"]
    assert "slime.extractors" in eps
    assert "slime.providers" in eps

    for expected_ext in ["rpgmaker", "wolfrpg", "csv", "json"]:
        assert expected_ext in eps["slime.extractors"]

    for expected_prov in ["google", "deepl", "ollama"]:
        assert expected_prov in eps["slime.providers"]

    # Callable console script
    assert callable(main.main)


def test_requirements_txt_covers_pyproject_dependencies():
    import os
    import re
    import tomllib

    root_dir = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
    with open(os.path.join(root_dir, "pyproject.toml"), "rb") as f:
        pyproject = tomllib.load(f)

    with open(os.path.join(root_dir, "requirements.txt"), encoding="utf-8") as f:
        req_lines = [
            line.strip()
            for line in f
            if line.strip() and not line.strip().startswith("#")
        ]

    def pkg_name(spec):
        return re.split(r"[<>=!~\[\s]", spec, maxsplit=1)[0].strip().lower().replace("_", "-")

    req_pkgs = {pkg_name(line) for line in req_lines}
    pyproject_deps = list(pyproject["project"].get("dependencies", []))
    for extra_deps in pyproject["project"].get("optional-dependencies", {}).values():
        pyproject_deps.extend(extra_deps)

    missing = {pkg_name(dep) for dep in pyproject_deps} - req_pkgs
    assert not missing, f"Pacotes em pyproject.toml ausentes de requirements.txt: {sorted(missing)}"
