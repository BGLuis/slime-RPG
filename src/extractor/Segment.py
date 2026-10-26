import copy
from collections.abc import Mapping
from dataclasses import dataclass, field
from types import MappingProxyType
from typing import Any, Dict, Iterable, List, Optional


def _make_hashable(val: Any) -> Any:
    if isinstance(val, (dict, Mapping)):
        return tuple(sorted((k, _make_hashable(v)) for k, v in val.items()))
    if isinstance(val, (list, tuple)):
        return tuple(_make_hashable(v) for v in val)
    if isinstance(val, set):
        return tuple(sorted(_make_hashable(v) for v in val))
    return val


class SegmentContext(Mapping):
    """Mapeamento imutável de contexto/restrições com suporte a acesso por atributo (ex: context.kind)."""

    def __init__(self, data: Any = None, **kwargs: Any):
        self._data: Dict[str, Any] = {}
        if isinstance(data, (dict, Mapping)):
            self._data.update(data)
        elif data is not None:
            try:
                self._data.update(dict(data))
            except Exception:
                pass
        if kwargs:
            self._data.update(kwargs)
        self._proxy = MappingProxyType(self._data)
        self._cached_hash: Optional[int] = None

    def __getitem__(self, key: str) -> Any:
        return self._proxy[key]

    def __iter__(self):
        return iter(self._proxy)

    def __len__(self) -> int:
        return len(self._proxy)

    def __getattr__(self, name: str) -> Any:
        try:
            return self._proxy[name]
        except KeyError:
            raise AttributeError(f"'{type(self).__name__}' object has no attribute '{name}'")

    def __setattr__(self, name: str, value: Any) -> None:
        if name in ("_data", "_proxy", "_cached_hash"):
            object.__setattr__(self, name, value)
        else:
            raise TypeError(f"'{type(self).__name__}' object is immutable")

    def __setitem__(self, key: str, value: Any) -> None:
        raise TypeError(f"'{type(self).__name__}' object is immutable")

    def __delitem__(self, key: str) -> None:
        raise TypeError(f"'{type(self).__name__}' object is immutable")

    def __hash__(self) -> int:
        if self._cached_hash is None:
            self._cached_hash = hash(_make_hashable(self._data))
        return self._cached_hash

    def __eq__(self, other: Any) -> bool:
        if isinstance(other, (dict, Mapping)):
            return dict(self._data) == dict(other)
        return False

    def __repr__(self) -> str:
        return f"SegmentContext({dict(self._data)})"


@dataclass(frozen=True)
class Segment:
    id: str
    source: str
    context: Any = field(default_factory=dict)
    constraints: Any = field(default_factory=dict)

    def __post_init__(self) -> None:
        ctx_data = dict(self.context) if isinstance(self.context, (dict, Mapping)) else {}
        if not ctx_data.get("kind"):
            raise ValueError("Segment requires an explicit 'kind' in context")
        cons_data = dict(self.constraints) if isinstance(self.constraints, (dict, Mapping)) else {}
        object.__setattr__(self, "context", SegmentContext(ctx_data))
        object.__setattr__(self, "constraints", SegmentContext(cons_data))

    def __hash__(self) -> int:
        return hash((self.id, self.source, hash(self.context), hash(self.constraints)))

    def __eq__(self, other: Any) -> bool:
        if not isinstance(other, Segment):
            return False
        return (
            self.id == other.id
            and self.source == other.source
            and self.context == other.context
            and self.constraints == other.constraints
        )


class SegmentList(list):
    """Lista de Segment que preserva referência opcional aos dados de origem para apply_segments."""

    def __init__(self, iterable: Iterable[Segment] = (), *, source_data: Any = None, file_name: Optional[str] = None):
        super().__init__(iterable)
        self._source_data = source_data
        self._file_name = file_name


_METADATA_ATTRS = (
    "_raw_rvdata",
    "_raw_rvdata_bytes",
    "_is_dirty",
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
            val = translations(seg.source)
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
