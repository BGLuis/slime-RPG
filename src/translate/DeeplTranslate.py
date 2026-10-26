import os
import logging
import requests
from src.translate.BaseTranslate import BaseTranslate
from src.translate.Provider import Capabilities
import src.utils.TextsUtils as TextsUtils
from src.factory import register_translator
from src.exceptions import (
    ProviderRateLimited,
    ProviderAuthError,
    ProviderUnavailable
)

@register_translator("deepl", aliases=["deeplTranslator", "deepl_translator"])
class DeeplTranslate(BaseTranslate):
    agent = 'deepl'
    id = 'deepl'
    capabilities = Capabilities(
        max_request_chars=10000,
        max_items=50,
        native_batch=True,
        tag_handling="xml",
        glossary=True,
        context=False,
    )
    MAX_REQUESTS_SIMULTANEOUSLY = 5
    char_limit = 10000

    def __init__(self, delimiter=None, char_limit=None, lang_source=None, lang_target=None):
        super().__init__(delimiter, char_limit, lang_source, lang_target)
        self.api_key = os.getenv("DEEPL_API_KEY", "")
        self.use_free_api = True

    @classmethod
    def requires_synopsis(cls):
        return False

    @classmethod
    def get_interactive_questions(cls):
        env_key = os.getenv("DEEPL_API_KEY", "")
        desc = "Chave de API gratuita do DeepL (500.000 caracteres/mês grátis)."
        if env_key:
            desc += f" (Chave detectada no .env: {env_key[:4]}...{env_key[-4:]})"
        return [
            {
                'key': 'api_key',
                'question': 'Insira sua DeepL API Key (ou pressione Enter para usar a do .env):',
                'title': '\n=== CONFIGURAÇÃO DO DEEPL API ===',
                'description': desc,
                'color': 'cyan',
                'required': not bool(env_key),
                'default': env_key
            }
        ]

    def apply_configuration(self, config):
        if 'api_key' in config and config['api_key']:
            self.api_key = config['api_key'].strip()

    def _get_api_url(self):
        if self.api_key.endswith(":fx"):
            return "https://api-free.deepl.com/v2/translate"
        return "https://api.deepl.com/v2/translate"

    def _map_lang_code(self, code, is_target=False):
        if not code:
            return None if not is_target else "EN-US"
        c = code.lower().strip()
        if not is_target and (c == 'auto' or not c):
            return None
        if c == 'ja':
            return 'JA'
        if c == 'en':
            return 'EN-US' if is_target else 'EN'
        if c == 'pt':
            return 'PT-BR' if is_target else 'PT'
        return c.upper()

    def _translate_single_batch(self, texts):
        if not texts:
            return []
        if not self.api_key:
            raise ProviderAuthError("DEEPL_API_KEY não informada. Defina no .env ou na inicialização.")

        url = self._get_api_url()
        src_lang = self._map_lang_code(self.lang_source, is_target=False)
        tgt_lang = self._map_lang_code(self.lang_target, is_target=True)

        payload = {
            "target_lang": tgt_lang,
            "text": texts,
            "preserve_formatting": True,
            "tag_handling": "xml"
        }
        if src_lang is not None and src_lang.upper() != "AUTO":
            payload["source_lang"] = src_lang

        headers = {
            "Authorization": f"DeepL-Auth-Key {self.api_key}"
        }

        try:
            r = requests.post(url, data=payload, headers=headers, timeout=20)
            status_code = r.status_code
            if status_code in (401, 403, 456):
                raise ProviderAuthError(f"DeepL auth/quota error (HTTP {status_code}): {r.text}")
            if status_code == 429:
                raise ProviderRateLimited("DeepL rate limit exceeded (HTTP 429)")
            if 500 <= status_code < 600:
                raise ProviderUnavailable(f"DeepL server error (HTTP {status_code}): {r.text}")
            r.raise_for_status()
            data = r.json()
        except requests.exceptions.HTTPError as e:
            status_code = e.response.status_code if e.response is not None else None
            if status_code in (401, 403, 456):
                raise ProviderAuthError(f"DeepL auth/quota error (HTTP {status_code})") from e
            if status_code == 429:
                raise ProviderRateLimited("DeepL rate limit exceeded (HTTP 429)") from e
            if status_code and 500 <= status_code < 600:
                raise ProviderUnavailable(f"DeepL server error (HTTP {status_code})") from e
            raise ProviderUnavailable(f"DeepL HTTP error: {e}") from e
        except requests.exceptions.RequestException as e:
            raise ProviderUnavailable(f"DeepL network error: {e}") from e

        translations = [item.get("text", "") for item in data.get("translations", [])]
        if len(translations) != len(texts):
            raise ValueError(f"Divergência no número de itens traduzidos pelo DeepL: esperados {len(texts)}, recebidos {len(translations)}")
        return translations

