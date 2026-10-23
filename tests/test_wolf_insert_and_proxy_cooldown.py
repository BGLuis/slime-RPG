import time

from src.extractor.wolfrpg.WolfEventStrategy import ShowMessageStrategy, SetStringStrategy
from src.services.ProxyManager import ProxyManager


def test_insert_does_not_write_text_into_numeric_parameters():
    # parameters são os args u4 do comando; texto ali quebra WolfCommand.write.
    for strategy in (ShowMessageStrategy(), SetStringStrategy()):
        item = {'parameters': [7, 1], 'string_args': ['Hello']}
        strategy.insert(item, 'Olá')
        assert item['string_args'] == ['Olá']
        assert item['parameters'] == [7, 1]


def test_direct_block_expires_after_cooldown():
    pm = ProxyManager()
    pm.set_enabled(True)
    pm.ensure_proxies = lambda min_count=3: None

    pm.set_direct_blocked(True)
    assert pm.direct_blocked

    pm._direct_blocked_until = time.monotonic() - 1
    assert not pm.direct_blocked
    assert pm.get_proxy_dict() is None


def test_cooldown_grows_on_repeated_blocks_and_resets_on_success():
    pm = ProxyManager()
    pm.set_enabled(True)
    pm.ensure_proxies = lambda min_count=3: None

    pm.set_direct_blocked(True)
    first = pm._direct_blocked_until - time.monotonic()
    pm._direct_blocked_until = 0.0
    pm.set_direct_blocked(True)
    second = pm._direct_blocked_until - time.monotonic()
    assert second > first

    pm.report_direct_success()
    pm._direct_blocked_until = 0.0
    pm.set_direct_blocked(True)
    assert pm._direct_blocked_until - time.monotonic() <= first + 1


def test_unvalidated_local_proxies_are_not_loaded_into_pool(tmp_path, monkeypatch):
    (tmp_path / "proxies.txt").write_text("1.1.1.1:80\n2.2.2.2:80\n")
    monkeypatch.chdir(tmp_path)
    monkeypatch.setenv("USE_PROXY_POOL", "true")
    pm = ProxyManager()
    assert pm.get_pool_size() == 0
    assert not pm.direct_blocked
