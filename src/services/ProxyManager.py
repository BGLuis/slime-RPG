import os
import time
import logging
import threading
from collections import deque
import concurrent.futures
import requests

class ProxyManager:
    """
    Gerenciador thread-safe de pool de proxies rotativos para o Google Translate.
    Quando o IP residencial/local sofre rate-limit (HTTP 429), o ProxyManager
    fornece proxies HTTPS validados que distribuem a carga e contornam o bloqueio.
    """
    _instance = None
    _lock = threading.Lock()

    SOURCES = [
        "https://raw.githubusercontent.com/monosans/proxy-list/main/proxies/http.txt",
        "https://api.proxyscrape.com/v2/?request=displayproxies&protocol=http&timeout=4000&country=all&ssl=all&anonymity=all",
        "https://raw.githubusercontent.com/TheSpeedX/SOCKS-List/master/http.txt"
    ]

    DIRECT_COOLDOWN_BASE = 30
    DIRECT_COOLDOWN_MAX = 300
    MAX_WAIT_FOR_PROXY = 15.0

    TEST_URL = "https://translate.googleapis.com/translate_a/single"
    TEST_PARAMS = {"client": "gtx", "sl": "ja", "tl": "pt", "dt": "t", "q": "テスト"}

    def __init__(self):
        self._working_proxies = deque()
        self._pool_lock = threading.Lock()
        self._is_fetching = False
        self._direct_blocked_until = 0.0
        self._block_streak = 0
        self._custom_proxy = os.getenv("HTTP_PROXY") or os.getenv("HTTPS_PROXY")
        self._last_fetch_time = 0
        disable_env = os.getenv("DISABLE_PROXY_POOL", "false").lower() in ("true", "1", "yes")
        use_env = os.getenv("USE_PROXY_POOL", "true").lower() in ("true", "1", "yes")
        self._enabled = use_env and not disable_env

    @classmethod
    def get_instance(cls):
        with cls._lock:
            if cls._instance is None:
                cls._instance = ProxyManager()
            return cls._instance

    @property
    def enabled(self):
        return self._enabled

    def set_enabled(self, enabled: bool):
        self._enabled = bool(enabled)
        if not self._enabled:
            with self._pool_lock:
                self._working_proxies.clear()
            self._direct_blocked_until = 0.0
            self._block_streak = 0

    def get_pool_size(self):
        with self._pool_lock:
            return len(self._working_proxies)

    @property
    def direct_blocked(self):
        return time.monotonic() < self._direct_blocked_until

    def set_direct_blocked(self, blocked=True):
        if not blocked:
            self._direct_blocked_until = 0.0
            self._block_streak = 0
            return

        if not self._enabled:
            self._direct_blocked_until = float("inf")
            logging.warning("⚠️ IP direto bloqueado pela Google (429) e o pool de proxies está DESATIVADO. Utilize VPN ou troque seu IP para continuar.")
            return

        # O 429 da Google é temporário: bloqueio com cooldown crescente, depois volta a tentar o IP direto.
        with self._pool_lock:
            cooldown = min(self.DIRECT_COOLDOWN_BASE * (2 ** self._block_streak), self.DIRECT_COOLDOWN_MAX)
            self._block_streak += 1
            already_blocked = self.direct_blocked
            self._direct_blocked_until = max(self._direct_blocked_until, time.monotonic() + cooldown)
        if not already_blocked:
            logging.warning(f"⚠️ IP direto bloqueado pela Google (429). Usando proxies por {int(cooldown)}s antes de tentar o IP direto de novo...")
        self.ensure_proxies(min_count=5)

    def report_direct_success(self):
        self._block_streak = 0

    def _load_local_proxies(self):
        proxies = []
        if os.path.exists("proxies.txt"):
            try:
                with open("proxies.txt", "r", encoding="utf-8") as f:
                    for line in f:
                        line = line.strip()
                        if line and not line.startswith("#"):
                            proxies.append(line)
            except Exception:
                pass
        return proxies

    def _validate_proxy(self, proxy_str):
        p_dict = {"http": f"http://{proxy_str}", "https": f"http://{proxy_str}"}
        try:
            r = requests.get(
                self.TEST_URL,
                params=self.TEST_PARAMS,
                proxies=p_dict,
                headers={"User-Agent": "Mozilla/5.0"},
                timeout=(2.0, 3.0)
            )
            if r.status_code == 200 and r.text.startswith("["):
                return proxy_str
        except Exception:
            pass
        return None

    def _fetch_from_sources(self):
        candidates = []
        
        # 1. Priorizar os proxies locais validados de proxies.txt
        local_proxies = self._load_local_proxies()
        candidates.extend(local_proxies)

        # 2. Buscar das fontes públicas se necessário
        if len(candidates) < 30:
            for src in self.SOURCES:
                try:
                    r = requests.get(src, timeout=4)
                    if r.status_code == 200:
                        for line in r.text.splitlines():
                            line = line.strip()
                            if ":" in line and not line.startswith("<") and line not in candidates:
                                candidates.append(line)
                        if len(candidates) >= 60:
                            break
                except Exception:
                    continue

        if not candidates:
            return []

        # Validar em paralelo
        candidate_list = candidates[:max(50, len(local_proxies))]
        working = []
        with concurrent.futures.ThreadPoolExecutor(max_workers=25) as executor:
            results = executor.map(self._validate_proxy, candidate_list)
            for res in results:
                if res:
                    working.append(res)
        return working

    def ensure_proxies(self, min_count=3):
        if not self._enabled:
            return
        with self._pool_lock:
            if len(self._working_proxies) >= min_count:
                return
            if self._is_fetching:
                return
            self._is_fetching = True

        def _worker():
            try:
                new_proxies = self._fetch_from_sources()
                with self._pool_lock:
                    for p in new_proxies:
                        if p not in self._working_proxies:
                            self._working_proxies.append(p)
                    self._last_fetch_time = time.time()
                logging.info(f"✓ Pool de proxies atualizado ({len(self._working_proxies)} disponíveis)")
            except Exception as e:
                logging.error(f"Falha ao buscar proxies: {e}")
            finally:
                with self._pool_lock:
                    self._is_fetching = False

        threading.Thread(target=_worker, daemon=True).start()

    def get_proxy_dict(self):
        if self._custom_proxy:
            return {"http": self._custom_proxy, "https": self._custom_proxy}

        if not self._enabled or not self.direct_blocked:
            return None

        # ensure_proxies() adquire _pool_lock, que não é reentrante: chamar fora do lock.
        if self.get_pool_size() < 3:
            self.ensure_proxies(min_count=5)

        # Sem proxy validado, espera o pool encher ou o cooldown acabar em vez de
        # martelar o IP direto que a Google acabou de bloquear.
        deadline = time.monotonic() + self.MAX_WAIT_FOR_PROXY
        while self.direct_blocked and time.monotonic() < deadline:
            with self._pool_lock:
                if self._working_proxies:
                    break
            time.sleep(0.25)

        with self._pool_lock:
            if not self._working_proxies or not self.direct_blocked:
                return None
            proxy_str = self._working_proxies[0]
            self._working_proxies.rotate(-1)
            return {"http": f"http://{proxy_str}", "https": f"http://{proxy_str}"}

    def report_proxy_failure(self, proxy_dict):
        if not proxy_dict or self._custom_proxy:
            return
        http_val = proxy_dict.get("http", "")
        clean_proxy = http_val.replace("http://", "").replace("https://", "")
        with self._pool_lock:
            if clean_proxy in self._working_proxies:
                self._working_proxies.remove(clean_proxy)
            remaining = len(self._working_proxies)
        if remaining < 3:
            self.ensure_proxies(min_count=5)

    def report_proxy_success(self, proxy_dict):
        # Sucesso: mantém na rotação
        pass
