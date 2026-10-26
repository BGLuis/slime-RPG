import copy
from dataclasses import dataclass, field
from typing import Any, Dict, Iterable, List, Optional


class SegmentContext(dict):
    """Dicionário de contexto/restrições com suporte a acesso por atributo (ex: context.kind)."""

    def __getattr__(self, name: str) -> Any:
        try:
            return self[name]
        except KeyError:
            raise AttributeError(f"'{type(self).__name__}' object has no attribute '{name}'")

    def __setattr__(self, name: str, value: Any) -> None:
        self[name] = value


@dataclass(frozen=True)
class Segment:
    id: str
    source: str
    context: Dict[str, Any] = field(default_factory=dict)
    constraints: Dict[str, Any] = field(default_factory=dict)

    def __post_init__(self) -> None:
        ctx = SegmentContext(self.context) if self.context is not None else SegmentContext()
        if not ctx.get("kind"):
            ctx["kind"] = "dialogue"
        cons = SegmentContext(self.constraints) if self.constraints is not None else SegmentContext()
        object.__setattr__(self, "context", ctx)
        object.__setattr__(self, "constraints", cons)


class SegmentList(list):
    """Lista de Segment que preserva referência opcional aos dados de origem para apply_segments."""

    def __init__(self, iterable: Iterable[Segment] = (), *, source_data: Any = None, file_name: Optional[str] = None):
        super().__init__(iterable)
        self._source_data = source_data
        self._file_name = file_name


_METADATA_ATTRS = (
    "_raw_rvdata",
    "_raw_rvdata_bytes",
    "_rvdata_snapshot",
    "_raw_wolf",
    "_raw_wolf_bytes",
    "_wolf_snapshot",
    "_csv_meta",
    "_raw_json_bytes",
    "_json_snapshot",
    "_file_name",
)


def copy_preserving_metadata(data: Any) -> Any:
    """Realiza deepcopy de data preservando atributos de metadados dos adaptadores de formato."""
    copied = copy.deepcopy(data)
    if hasattr(data, "__dict__") and hasattr(copied, "__dict__"):
        for attr in _METADATA_ATTRS:
            if hasattr(data, attr):
                val = getattr(data, attr)
                if attr in ("_raw_rvdata", "_raw_wolf"):
                    setattr(copied, attr, copy.deepcopy(val))
                else:
                    setattr(copied, attr, val)
    return copied


def resolve_translation_map(segments: List[Segment], translations: Any) -> Dict[str, str]:
    """Normaliza um dicionário {id: texto}, função ou lista de segmentos em Dict[str, str]."""
    if translations is None:
        return {}
    if isinstance(translations, dict):
        result = {}
        for k, v in translations.items():
            if isinstance(v, Segment):
                result[str(k)] = v.source
            elif v is not None:
                result[str(k)] = str(v)
        return result
    if callable(translations):
        result = {}
        for seg in segments:
            try:
                val = translations(seg.source)
            except (TypeError, AttributeError):
                val = translations(seg)
            if isinstance(val, Segment):
                val = val.source
            result[seg.id] = str(val) if val is not None else seg.source
        return result
    if isinstance(translations, (list, tuple)):
        result = {}
        for idx, item in enumerate(translations):
            if isinstance(item, Segment):
                result[item.id] = item.source
            elif isinstance(item, str) and idx < len(segments):
                result[segments[idx].id] = item
        return result
    return {}
