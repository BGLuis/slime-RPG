class TranslationError(Exception):
    """Base exception for translation provider and pipeline errors."""


class ProviderRateLimited(TranslationError):
    """Raised when a translation provider hits a rate limit (e.g. HTTP 429)."""


class ProviderAuthError(TranslationError):
    """Raised when authentication fails or quota is permanently exceeded (e.g. HTTP 401, 403, 456)."""


class ProviderUnavailable(TranslationError):
    """Raised when a translation provider is temporarily unavailable (e.g. HTTP 5xx, network timeout)."""


class MaskIntegrityError(TranslationError, ValueError):
    """Raised when placeholder unmasking fails or leaves residual tokens."""
