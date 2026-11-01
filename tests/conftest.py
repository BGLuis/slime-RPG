import os
import sys
import pytest

# Garante que "src" seja importável independente do diretório de onde o pytest for chamado.
sys.path.insert(0, os.path.abspath(os.path.join(os.path.dirname(__file__), '..')))

LIVE_MODULES = {
    "test_parallel_translation",
}

E2E_MODULES = {
    "test_cli_aliases",
    "test_cli_textual",
    "test_gui_dynamic_form",
    "test_m1_wolf_and_providers",
    "test_no_text_binary_integrity",
    "test_translation_job",
    "test_verify_map002_commonevents_pipeline",
}

INTEGRATION_MODULES = {
    "test_cache_fast_lane",
    "test_cross_file_batching",
    "test_csv_json_m1",
    "test_hostile_corpus",
    "test_integration_complete",
    "test_migrate_cache_to_db",
    "test_prescan_redundancy_elimination",
    "test_rvdata2_extractor",
    "test_segments_roundtrip",
    "test_single_retry_concurrency",
    "test_subfolder_processing",
    "test_translate_cache",
    "test_translate_concurrency",
    "test_translation_memory",
    "test_translation_memory_policy",
    "test_wolf_archive",
    "test_wolf_binary_adapter",
    "test_wolf_insert_and_proxy_cooldown",
    "test_wolfrpg_evtext",
    "test_wolfrpg_extractor",
    "test_workspace",
}


def pytest_collection_modifyitems(config, items):
    level_markers = {"unit", "integration", "e2e", "live"}
    for item in items:
        existing = {m.name for m in item.iter_markers()} & level_markers
        if existing:
            continue
        mod_name = item.module.__name__.rsplit(".", 1)[-1]
        if mod_name in LIVE_MODULES:
            item.add_marker(pytest.mark.live)
        elif mod_name in E2E_MODULES:
            item.add_marker(pytest.mark.e2e)
        elif mod_name in INTEGRATION_MODULES:
            item.add_marker(pytest.mark.integration)
        else:
            item.add_marker(pytest.mark.unit)


@pytest.fixture(autouse=True)
def _isolate_runtime_environment(tmp_path_factory, monkeypatch):
    """
    Isola diretórios de dados/config/cache/log e singletons por teste,
    permitindo execução paralela segura via pytest-xdist (-n auto).
    """
    base_dir = tmp_path_factory.mktemp("slime_isolated")
    data_dir = base_dir / "data"
    cache_dir = base_dir / "cache"
    config_dir = base_dir / "config"
    log_dir = base_dir / "log"
    for d in (data_dir, cache_dir, config_dir, log_dir):
        d.mkdir(parents=True, exist_ok=True)

    monkeypatch.setenv("SLIME_DATA_DIR", str(data_dir))
    monkeypatch.setenv("SLIME_CACHE_DIR", str(cache_dir))
    monkeypatch.setenv("SLIME_CONFIG_DIR", str(config_dir))
    monkeypatch.setenv("SLIME_LOG_DIR", str(log_dir))

    from src.translate.BaseTranslate import BaseTranslate
    from src.translate.TranslationMemory import TranslationMemory
    from src.services.ProxyManager import ProxyManager

    monkeypatch.setattr(BaseTranslate, "cache_path_base", str(data_dir))
    TranslationMemory._connections.clear()

    yield

    TranslationMemory._connections.clear()
    pm = ProxyManager.get_instance()
    pm.set_direct_blocked(False)
    pm.set_enabled(True)
    with pm._pool_lock:
        pm._working_proxies.clear()
