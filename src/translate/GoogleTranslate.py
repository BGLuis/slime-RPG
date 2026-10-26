import logging
import time
import threading
import requests
from requests.adapters import HTTPAdapter
from src.translate.BaseTranslate import BaseTranslate
from src.translate.Provider import Capabilities
from deep_translator import GoogleTranslator
import src.utils.TextsUtils as TextsUtils
from src.factory import register_translator
from src.exceptions import (
    ProviderRateLimited,
    ProviderAuthError,
    ProviderUnavailable,
)

try:
    from deep_translator.exceptions import TooManyRequests
except ImportError:
    class TooManyRequests(Exception):
        pass


def _is_rate_limit_exception(e) -> bool:
    if isinstance(e, (ProviderRateLimited, TooManyRequests)):
        return True
    if isinstance(e, requests.exceptions.HTTPError):
        if e.response is not None and e.response.status_code == 429:
            return True
    if getattr(e, 'status_code', None) == 429:
        return True
    err_cls = type(e).__name__.lower()
    if "toomanyrequests" in err_cls or "ratelimit" in err_cls:
        return True
    return False

@register_translator("google", aliases=["googleTraslator", "googleTranslator", "google_translator"])
class GoogleTranslate(BaseTranslate):
    agent = 'google'
    id = 'google'
    capabilities = Capabilities(
        max_request_chars=5000,
        max_items=None,
        native_batch=False,
        tag_handling=None,
        glossary=False,
        context=False,
    )
    # Evita inconsistências em lotes com o cliente HTTP compartilhado.
    MAX_REQUESTS_SIMULTANEOUSLY = 4
    USER_AGENT = 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/128.0.0.0 Safari/537.36'
    _session = None
    _session_lock = threading.Lock()
    _last_request_time = 0.0
    _rate_lock = threading.Lock()
    _MIN_REQUEST_INTERVAL = 0.2  # Pacing suave para requisições diretas

    @classmethod
    def _get_concurrency_limit(cls):
        base_limit = cls.MAX_REQUESTS_SIMULTANEOUSLY
        try:
            import os
            from src.services.ProxyManager import ProxyManager
            pm = ProxyManager.get_instance()
            if pm.enabled and pm.direct_blocked:
                pool_size = pm.get_pool_size()
                if pool_size > 0:
                    req_per_proxy = int(os.environ.get('REQUESTS_PER_PROXY', '1'))
                    base_limit = max(cls.MAX_REQUESTS_SIMULTANEOUSLY, min(16, pool_size * req_per_proxy))
        except Exception:
            pass
        return base_limit


    @classmethod
    def _rate_limit(cls):
        with cls._rate_lock:
            now = time.time()
            elapsed = now - cls._last_request_time
            if elapsed < cls._MIN_REQUEST_INTERVAL:
                time.sleep(cls._MIN_REQUEST_INTERVAL - elapsed)
            cls._last_request_time = time.time()

    @classmethod
    def _get_session(cls):
        with cls._session_lock:
            if cls._session is None:
                s = requests.Session()
                adapter = HTTPAdapter(pool_connections=20, pool_maxsize=20)
                s.mount('https://', adapter)
                s.mount('http://', adapter)
                cls._session = s
            return cls._session

    @classmethod
    def requires_synopsis(cls):
        return False

    @classmethod
    def get_interactive_questions(cls):
        from src.services.ProxyManager import ProxyManager
        pm = ProxyManager.get_instance()
        default_label = "Sim (Rotacionar proxies públicos caso o IP seja bloqueado)" if pm.enabled else "Não (Usar apenas conexão direta / VPN própria)"
        return [
            {
                'key': 'use_proxy_pool',
                'question': 'Deseja habilitar pool de proxies rotativos para o GoogleTranslate?',
                'title': '\n=== CONFIGURAÇÃO DO GOOGLE TRANSLATOR ===',
                'description': 'O pool de proxies rotativos contorna automaticamente o erro HTTP 429 da Google. Se você estiver usando VPN própria ou hotspot móvel, pode deixar desativado.',
                'color': 'cyan',
                'options': {
                    'Não (Usar apenas conexão direta / VPN própria)': False,
                    'Sim (Rotacionar proxies públicos caso o IP seja bloqueado)': True
                },
                'default': default_label
            }
        ]

    def apply_configuration(self, config):
        from src.services.ProxyManager import ProxyManager
        if 'use_proxy_pool' in config:
            ProxyManager.get_instance().set_enabled(bool(config['use_proxy_pool']))

    def __init__(self, delimiter=None, char_limit=None, lang_source=None, lang_target=None):
        super().__init__(delimiter, char_limit, lang_source, lang_target)
        self.translate_client = GoogleTranslator(source=self.lang_source, target=self.lang_target)

    def _translate_gtx(self, text):
        from src.services.ProxyManager import ProxyManager
        proxy_mgr = ProxyManager.get_instance()
        proxy_dict = proxy_mgr.get_proxy_dict()

        if not proxy_dict:
            self._rate_limit()

        url = 'https://translate.googleapis.com/translate_a/single'
        data = {
            'client': 'gtx',
            'sl': self.lang_source,
            'tl': self.lang_target,
            'dt': 't',
            'q': text
        }
        headers = {'User-Agent': self.USER_AGENT}

        try:
            if proxy_dict:
                r = requests.post(url, data=data, headers=headers, proxies=proxy_dict, timeout=(3.0, 5.0))
            else:
                session = self._get_session()
                r = session.post(url, data=data, headers=headers, timeout=(4.0, 8.0))

            if r.status_code == 429:
                if not proxy_dict:
                    proxy_mgr.set_direct_blocked(True)
                else:
                    proxy_mgr.report_proxy_failure(proxy_dict)
                raise ProviderRateLimited("Google GTX HTTP 429")

            if r.status_code in (401, 403):
                raise ProviderAuthError(f"Google GTX HTTP {r.status_code}")

            r.raise_for_status()
            data_json = r.json()
            if not data_json or not data_json[0]:
                return None

            if not proxy_dict:
                proxy_mgr.report_direct_success()

            return ''.join(part[0] for part in data_json[0] if part and part[0])
        except Exception as e:
            if proxy_dict:
                proxy_mgr.report_proxy_failure(proxy_dict)
            if _is_rate_limit_exception(e):
                if not proxy_mgr.direct_blocked:
                    proxy_mgr.set_direct_blocked(True)
                if proxy_mgr.get_pool_size() == 0:
                    raise ProviderRateLimited("Google rate limit exceeded") from e
            raise

    def _translate_with_deep_translator(self, text):
        from src.services.ProxyManager import ProxyManager
        proxy_mgr = ProxyManager.get_instance()
        proxy_dict = proxy_mgr.get_proxy_dict()
        if not proxy_dict:
            self._rate_limit()

        try:
            client = GoogleTranslator(source=self.lang_source, target=self.lang_target, proxies=proxy_dict)
            return client.translate(text)
        except Exception:
            if proxy_dict:
                proxy_mgr.report_proxy_failure(proxy_dict)
            raise

    def _translate_with_mymemory(self, text):
        from deep_translator import MyMemoryTranslator
        from src.services.ProxyManager import ProxyManager
        proxy_mgr = ProxyManager.get_instance()
        proxy_dict = proxy_mgr.get_proxy_dict()
        if not proxy_dict:
            self._rate_limit()

        lang_map = {
            'ja': 'ja-JP',
            'pt': 'pt-BR',
            'en': 'en-GB',
            'es': 'es-ES',
            'fr': 'fr-FR',
            'de': 'de-DE',
            'it': 'it-IT',
            'ko': 'ko-KR',
            'zh-cn': 'zh-CN',
            'zh': 'zh-CN'
        }
        src = lang_map.get(self.lang_source.lower(), self.lang_source)
        tgt = lang_map.get(self.lang_target.lower(), self.lang_target)
        try:
            client = MyMemoryTranslator(source=src, target=tgt, proxies=proxy_dict)
        except Exception:
            return text

        def _translate_chunk(chunk):
            if not chunk or not chunk.strip():
                return chunk
            if len(chunk) <= 450:
                self._rate_limit()
                try:
                    return client.translate(chunk) or chunk
                except Exception:
                    return chunk

            sub_chunks = []
            rem = chunk
            while rem:
                if len(rem) <= 450:
                    sub_chunks.append(rem)
                    break
                idx = rem.rfind('\n', 0, 450)
                if idx != -1 and idx > 0:
                    sub_chunks.append(rem[:idx])
                    rem = rem[idx + 1:]
                else:
                    sub_chunks.append(rem[:450])
                    rem = rem[450:]

            res_parts = []
            for sp in sub_chunks:
                if not sp.strip():
                    res_parts.append(sp)
                else:
                    self._rate_limit()
                    try:
                        res_parts.append(client.translate(sp) or sp)
                    except Exception:
                        res_parts.append(sp)
            return "\n".join(res_parts)

        if self.delimiter in text:
            parts = text.split(self.delimiter)
            return self.delimiter.join(_translate_chunk(p) for p in parts)
        return _translate_chunk(text)

    def _translate_raw(self, text):
        if not text or not text.strip():
            return text

        from src.services.ProxyManager import ProxyManager
        proxy_mgr = ProxyManager.get_instance()
        if proxy_mgr.direct_blocked and not proxy_mgr.enabled:
            raise ProviderRateLimited("IP direto bloqueado pela Google (HTTP 429) e pool de proxies desativado. Ative proxies ou use VPN.")

        try:
            res = self._translate_gtx(text)
            if res:
                return res
        except Exception as e:
            if isinstance(e, ProviderRateLimited) and proxy_mgr.direct_blocked and not proxy_mgr.enabled:
                raise
            logging.debug(f"GTX falhou para '{text[:30]}...': {e}")

        if proxy_mgr.direct_blocked and not proxy_mgr.enabled:
            raise ProviderRateLimited("IP direto bloqueado pela Google (HTTP 429) e pool de proxies desativado. Ative proxies ou use VPN.")

        try:
            res = self._translate_with_deep_translator(text)
            if res:
                return res
        except Exception as e:
            if isinstance(e, ProviderRateLimited) and proxy_mgr.direct_blocked and not proxy_mgr.enabled:
                raise
            logging.debug(f"deep_translator falhou para '{text[:30]}...': {e}")

        try:
            return self._translate_with_mymemory(text)
        except Exception as e:
            if isinstance(e, (ProviderRateLimited, ProviderAuthError)):
                raise
            logging.warning(f"Fallback para texto original: {e}")
            return text

    def _translate_single_batch(self, texts):
        if not texts:
            return []

        import re
        try:
            list_join = self.delimiter.join(texts)
            translate_str = self._translate_raw(list_join)
            if not translate_str:
                raise ProviderUnavailable("Empty translation received")
            translate_list = translate_str.split(self.delimiter)

            # Se o delimitador foi ligeiramente alterado por espaços/tags pelo provedor:
            if len(translate_list) != len(texts) and '<span>' in self.delimiter:
                alt_list = re.split(r'\s*<span[^>]*>\s*</span>\s*', translate_str)
                if len(alt_list) == len(texts):
                    translate_list = alt_list

            # Se o delimitador for alterado pelo provedor, o batch pode ficar desalinhado.
            # Fallback sequencial sem abrir pool interno que fura o limite de concorrência:
            if len(translate_list) != len(texts):
                safe_results = [self._translate_raw(t) for t in texts]
                return safe_results

            return translate_list
        except Exception as e:
            is_429 = _is_rate_limit_exception(e)
            from src.services.ProxyManager import ProxyManager
            pm = ProxyManager.get_instance()
            if is_429 and not pm.enabled:
                logging.error("❌ IP direto bloqueado por limite de requisições (HTTP 429) e o pool de proxies está DESATIVADO.")
                raise ProviderRateLimited(
                    "IP bloqueado por limite de requisições (HTTP 429) e o pool de proxies está desativado. "
                    "Conecte sua VPN (ex: ProtonVPN) ou reative o pool de proxies no menu para continuar."
                ) from e
            raise


