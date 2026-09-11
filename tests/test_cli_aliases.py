import os
import subprocess
import sys
import pytest

PROJECT_ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
SLIME_SCRIPT = os.path.join(PROJECT_ROOT, "scripts", "slime")
EXTRACTOR_SCRIPT = os.path.join(PROJECT_ROOT, "scripts", "extractor-translation")


def test_slime_script_exists_and_executable():
    assert os.path.exists(SLIME_SCRIPT), "scripts/slime must exist"
    assert os.access(SLIME_SCRIPT, os.X_OK), "scripts/slime must be executable"


def test_extractor_translation_is_symlink_to_slime():
    assert os.path.exists(EXTRACTOR_SCRIPT), "scripts/extractor-translation must exist"
    assert os.path.islink(EXTRACTOR_SCRIPT), "scripts/extractor-translation must be a symlink"
    target = os.readlink(EXTRACTOR_SCRIPT)
    assert "slime" in target, "symlink should target slime"


def test_slime_script_list_extractors():
    result = subprocess.run(
        [SLIME_SCRIPT, "--list-extractors"],
        capture_output=True,
        text=True,
        cwd=PROJECT_ROOT
    )
    assert result.returncode == 0
    assert "EXTRATORES DISPONÍVEIS" in result.stdout
    assert "RPG Maker" in result.stdout


def test_parse_arguments_dynamic_prog(monkeypatch):
    import main

    # Test default fallback when invoked without env
    monkeypatch.delenv("SLIME_INVOKED_AS", raising=False)
    monkeypatch.setattr(sys, "argv", ["main.py", "--list-extractors"])
    args = main.parse_arguments()
    assert args.list_extractors is True

    # Test when invoked as alias 'slm'
    monkeypatch.setenv("SLIME_INVOKED_AS", "slm")
    monkeypatch.setattr(sys, "argv", ["/path/to/main.py", "--list-translators"])
    args = main.parse_arguments()
    assert args.list_translators is True


def test_slime_alias_management_flags(tmp_path):
    # Use temporary directory as LOCAL_BIN
    env = os.environ.copy()
    env["LOCAL_BIN"] = str(tmp_path)

    # 1. Add alias
    res_add = subprocess.run(
        [SLIME_SCRIPT, "--add-alias", "test_sl_alias"],
        capture_output=True,
        text=True,
        env=env,
        cwd=PROJECT_ROOT
    )
    assert res_add.returncode == 0
    alias_path = tmp_path / "test_sl_alias"
    assert alias_path.exists()
    assert alias_path.is_symlink()

    # 2. List aliases
    res_list = subprocess.run(
        [SLIME_SCRIPT, "--list-aliases"],
        capture_output=True,
        text=True,
        env=env,
        cwd=PROJECT_ROOT
    )
    assert res_list.returncode == 0
    assert "test_sl_alias" in res_list.stdout

    # 3. Remove alias
    res_rm = subprocess.run(
        [SLIME_SCRIPT, "--remove-alias", "test_sl_alias"],
        capture_output=True,
        text=True,
        env=env,
        cwd=PROJECT_ROOT
    )
    assert res_rm.returncode == 0
    assert not alias_path.exists()
