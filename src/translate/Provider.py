from dataclasses import dataclass
from typing import Any, Optional, Protocol, runtime_checkable
from src.extractor.Segment import Segment


@dataclass
class Capabilities:
    max_request_chars: int
    max_items: Optional[int] = None
    native_batch: bool = False         # DeepL: list of texts; Google gtx: delimiter join
    tag_handling: Optional[str] = None # "xml" / "html" / None
    glossary: bool = False
    context: bool = False              # LLM providers


@runtime_checkable
class TranslationProvider(Protocol):
    id: str                            # stable: "google", "deepl", "ollama"
    capabilities: Capabilities

    def translate(self, segments: list[Segment], ctx: Optional[Any] = None) -> list[str]:
        ...
