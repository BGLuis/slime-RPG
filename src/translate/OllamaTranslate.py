from src.translate.BaseTranslate import BaseTranslate
from src.translate.Provider import Capabilities
from dotenv import load_dotenv
import os
import ollama
import src.utils.TextsUtils as TextsUtils
from src.factory import register_translator
from src.exceptions import (
    ProviderRateLimited,
    ProviderAuthError,
    ProviderUnavailable,
)
load_dotenv()

@register_translator("ollama", aliases=["ollamaTranslator", "ollama_translator"])
class OllamaTranslate(BaseTranslate):
    agent = 'ollama'
    id = 'ollama'
    capabilities = Capabilities(
        max_request_chars=10000,
        max_items=1,
        native_batch=False,
        tag_handling=None,
        glossary=False,
        context=True,
    )
    MAX_REQUESTS_SIMULTANEOUSLY = 10
    char_limit = 10000
    context_function = "Reply with only the translated text, without any additional comments, explanations, or notes."
    context_additional = "Act as an expert game localizer. Your mission is to translate text for a video game, ensuring the translation is engaging and immersive for the player. The text could be character dialogue, an item description, a quest objective, or a UI menu element. Adapt the translation to fit the gaming context, using appropriate and common gaming jargon. Maintain the original tone, whether it's serious, humorous, or epic. Do not provide a literal, word-for-word translation if a more natural, context-aware alternative exists."

    @classmethod
    def requires_synopsis(cls):
        return True

    @classmethod
    def get_interactive_questions(cls):
        return [
            {
                'key': 'synopsis',
                'question': 'Digite a sinopse do jogo (ou pressione Enter para pular):',
                'title': '\n=== SINOPSE DO JOGO (OPCIONAL) ===',
                'description': 'A sinopse ajuda a melhorar significativamente a qualidade da tradução fornecendo contexto ao modelo Ollama.',
                'color': 'yellow',
                'required': False
            }
        ]

    def apply_configuration(self, config):
        if 'synopsis' in config and config['synopsis']:
            self.set_game_synopsis(config['synopsis'])

    def __init__(self, delimiter=None, char_limit=None, lang_source=None, lang_target=None):
        super().__init__(delimiter, char_limit, lang_source, lang_target)
        self.context_function = os.getenv('OLLAMA_CONTEXT_FUNCTION') or self.context_function
        self.context_additional = os.getenv('OLLAMA_CONTEXT_ADDITIONAL') or self.context_additional
        self.model = os.getenv('OLLAMA_MODEL')
        self.max_requests = os.getenv('OLLAMA_MAX_REQUESTS')
        if not self.model or not self.max_requests:
            raise ValueError("Environment variables OLLAMA_MODEL and OLLAMA_MAX_REQUESTS must be defined.")
        self.context_language = f"Translate the user's message from {self.lang_source} to {self.lang_target}."

    def change_language(self, lang_source, lang_target):
        self.lang_source = lang_source
        self.lang_target = lang_target
        self._load_cache_for_current_languages()
        self.context_language = f"Translate the user's message from {self.lang_source} to {self.lang_target}."

    def _create_batches(self, texts):
        """Ollama prefers single-item batches to manage context window and prompt size."""
        return [[text] for text in texts]

    def _translate_single_batch(self, texts):
        if not texts:
            return []

        results = []
        for text in texts:
            messages = [
                {"role": "system", "content": self.context_function},
                {"role": "system", "content": self.context_additional},
                {"role": "system", "content": "Tokens like __XTOK_12345678__ are placeholders for game control codes: copy each one into the translation exactly as written, without translating, reformatting, or removing it."},
            ]

            if self.game_synopsis:
                synopsis_context = f"Game Synopsis/Context: {self.game_synopsis}\n\nUse this synopsis to better understand the game's context and provide more accurate, context-aware translations."
                messages.append({"role": "system", "content": synopsis_context})

            messages.append({"role": "system", "content": self.context_language})
            messages.append({"role": "user", "content": text})

            try:
                response = ollama.chat(
                    model=self.model,
                    messages=messages
                )
                result = response['message']['content'].strip()
                results.append(result)
            except Exception as e:
                status_code = getattr(e, 'status_code', None) or getattr(e, 'code', None)
                if hasattr(e, 'response') and getattr(e.response, 'status_code', None):
                    status_code = e.response.status_code

                if status_code in (401, 403):
                    raise ProviderAuthError(f"Ollama auth error (HTTP {status_code}): {e}") from e
                if status_code == 429:
                    raise ProviderRateLimited(f"Ollama rate limit exceeded (HTTP 429): {e}") from e
                if (status_code and 500 <= status_code < 600) or isinstance(e, (ConnectionError, TimeoutError, OSError)):
                    raise ProviderUnavailable(f"Ollama server unavailable: {e}") from e

                err_cls = type(e).__name__.lower()
                if "connection" in err_cls or "timeout" in err_cls or "request" in err_cls:
                    raise ProviderUnavailable(f"Ollama network error: {e}") from e
                raise

        return results

