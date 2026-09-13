import logging
import time
import threading
import requests
from requests.adapters import HTTPAdapter
from src.translate.BaseTranslate import BaseTranslate
from deep_translator import GoogleTranslator
import src.utils.TextsUtils as TextsUtils
from src.factory import register_translator

@register_translator("googleTraslator")
class GoogleTranslate(BaseTranslate):
    # Evita inconsistências em lotes com o cliente HTTP compartilhado.
    MAX_REQUESTS_SIMULTANEOUSLY = 4
    USER_AGENT = 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/128.0.0.0 Safari/537.36'
    _session = None
    _session_lock = threading.Lock()
    _last_request_time = 0.0
    _rate_lock = threading.Lock()
    _MIN_REQUEST_INTERVAL = 0.2  # Pacing suave para requisições diretas

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

    @staticmethod
    def preprocess_text(texts):
        for i, text in enumerate(texts):
            texts[i] = text

    @staticmethod
    def postprocess_text(texts):
        for i, text in enumerate(texts):
            texts[i] = text

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
                r = requests.post(url, data=data, headers=headers, proxies=proxy_dict, timeout=10)
            else:
                session = self._get_session()
                r = session.post(url, data=data, headers=headers, timeout=15)

            if r.status_code == 429:
                if not proxy_dict:
                    proxy_mgr.set_direct_blocked(True)
                else:
                    proxy_mgr.report_proxy_failure(proxy_dict)
                r.raise_for_status()

            r.raise_for_status()
            data_json = r.json()
            if not data_json or not data_json[0]:
                return None

            if proxy_dict:
                proxy_mgr.report_proxy_success(proxy_dict)

            return ''.join(part[0] for part in data_json[0] if part and part[0])
        except Exception as e:
            if proxy_dict:
                proxy_mgr.report_proxy_failure(proxy_dict)
            err_str = str(e).lower()
            if "429" in err_str:
                proxy_mgr.set_direct_blocked(True)
            raise

    def _translate_with_deep_translator(self, text):
        from src.services.ProxyManager import ProxyManager
        proxy_mgr = ProxyManager.get_instance()
        proxy_dict = proxy_mgr.get_proxy_dict()
        if not proxy_dict:
            self._rate_limit()

        client = GoogleTranslator(source=self.lang_source, target=self.lang_target, proxies=proxy_dict)
        return client.translate(text)

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
        client = MyMemoryTranslator(source=src, target=tgt, proxies=proxy_dict)

        def _translate_chunk(chunk):
            if not chunk or not chunk.strip():
                return chunk
            if len(chunk) <= 450:
                self._rate_limit()
                return client.translate(chunk) or chunk

            # Slicing puramente iterativo para evitar qualquer risco de RecursionError
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
                    res_parts.append(client.translate(sp) or sp)
            return "\n".join(res_parts)

        if self.delimiter in text:
            parts = text.split(self.delimiter)
            return self.delimiter.join(_translate_chunk(p) for p in parts)
        return _translate_chunk(text)

    def _translate_raw(self, text):
        if not text or not text.strip():
            return text
        try:
            res = self._translate_gtx(text)
            if res:
                return res
        except Exception as e:
            logging.debug(f"GTX falhou para '{text[:30]}...', tentando deep_translator: {e}")

        try:
            res = self._translate_with_deep_translator(text)
            if res:
                return res
        except Exception as e:
            logging.debug(f"deep_translator falhou para '{text[:30]}...', tentando MyMemory: {e}")

        try:
            return self._translate_with_mymemory(text)
        except Exception as e:
            logging.error(f"Todos os provedores de tradução falharam para '{text[:30]}...': {e}")
            raise

    def _translate_single_batch(self, texts):
        if not texts:
            return []

        import re
        max_retries = 3
        last_error = None

        for attempt in range(max_retries):
            try:
                list_join = self.delimiter.join(texts)
                translate_str = self._translate_raw(list_join)
                if not translate_str:
                    raise ValueError("Empty translation received")
                translate_list = translate_str.split(self.delimiter)

                # Se o delimitador foi ligeiramente alterado por espaços/tags pelo provedor:
                if len(translate_list) != len(texts) and '<span>' in self.delimiter:
                    alt_list = re.split(r'\s*<span[^>]*>\s*</span>\s*', translate_str)
                    if len(alt_list) == len(texts):
                        translate_list = alt_list

                # Se o delimitador for alterado pelo provedor, o batch pode ficar desalinhado.
                # Nessa situação, traduzimos item a item para manter mapeamento correto.
                if len(translate_list) != len(texts):
                    safe_results = []
                    for text in texts:
                        res = self._translate_raw(text)
                        if res is None:
                            raise ValueError(f"Falha ao traduzir item avulso: {text[:30]}")
                        safe_results.append(res)
                    return safe_results

                return translate_list
            except Exception as e:
                last_error = e
                err_str = str(e).lower()
                is_429 = "429" in err_str or "too many requests" in err_str or "toomanyrequests" in type(e).__name__.lower()
                from src.services.ProxyManager import ProxyManager
                pm = ProxyManager.get_instance()

                # Se o IP direto foi bloqueado com 429 e o usuário desativou o pool de proxies:
                if is_429 and not pm.enabled:
                    logging.error("❌ IP direto bloqueado pelo Google (HTTP 429) e o pool de proxies está DESATIVADO.")
                    raise RuntimeError(
                        "IP bloqueado pelo Google (HTTP 429) e o pool de proxies está desativado. "
                        "Conecte sua VPN (ex: ProtonVPN) ou reative o pool de proxies no menu para continuar."
                    ) from e

                if attempt < max_retries - 1:
                    if pm.direct_blocked or is_429:
                        wait_time = 0.5 * (attempt + 1)
                    else:
                        wait_time = (5.0 * (attempt + 1)) if is_429 else (1.5 * (attempt + 1))
                    logging.warning(f"Tentativa {attempt + 1}/{max_retries} falhou ({e}). Aguardando {wait_time}s...")
                    time.sleep(wait_time)
                else:
                    logging.error(f"Esgotadas {max_retries} tentativas para o lote. Propagando erro: {last_error}")
                    raise last_error

    def translator(self, texts, progress_callback=None):
        treated_text = TextsUtils.dictToList(texts)
        self.preprocess_text(treated_text)
        translate_text = self.translate_batch(treated_text, progress_callback)
        self.postprocess_text(translate_text)
        TextsUtils.interactive_item(texts, translate_text)
        return texts
