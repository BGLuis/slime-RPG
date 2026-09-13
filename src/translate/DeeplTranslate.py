import os
import logging
import requests
from src.translate.BaseTranslate import BaseTranslate
import src.utils.TextsUtils as TextsUtils
from src.factory import register_translator

@register_translator("deeplTranslator")
class DeeplTranslate(BaseTranslate):
    agent = 'deeplTranslator'
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
        c = code.lower().strip()
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
            raise ValueError("DEEPL_API_KEY não informada. Defina no .env ou na inicialização.")

        url = self._get_api_url()
        src_lang = self._map_lang_code(self.lang_source, is_target=False)
        tgt_lang = self._map_lang_code(self.lang_target, is_target=True)

        payload = {
            "source_lang": src_lang,
            "target_lang": tgt_lang,
            "text": texts,
            "tag_handling": "xml",
            "preserve_formatting": "1"
        }
        headers = {
            "Authorization": f"DeepL-Auth-Key {self.api_key}"
        }

        r = requests.post(url, data=payload, headers=headers, timeout=20)
        r.raise_for_status()
        data = r.json()
        translations = [item.get("text", "") for item in data.get("translations", [])]
        if len(translations) != len(texts):
            raise ValueError(f"Divergência no número de itens traduzidos pelo DeepL: esperados {len(texts)}, recebidos {len(translations)}")
        return translations

    def translator(self, texts, progress_callback=None):
        treated_text = TextsUtils.dictToList(texts)
        self.preprocess_text(treated_text)
        translate_text = self.translate_batch(treated_text, progress_callback)
        self.postprocess_text(translate_text)
        TextsUtils.interactive_item(texts, translate_text)
        return texts
