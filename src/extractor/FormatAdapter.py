import copy
import json
import logging
import os
from typing import Any, Dict, Iterable, Optional, Protocol, Tuple, runtime_checkable


class JsonDataDict(dict):
    pass


class JsonDataList(list):
    pass


@runtime_checkable
class FormatAdapter(Protocol):
    extensions: Tuple[str, ...]

    @staticmethod
    def load_file(file_path: str) -> Any:
        ...

    @staticmethod
    def save_file(file_path: str, data: Any, **ctx: Any) -> None:
        ...


_FORMAT_ADAPTER_REGISTRY: Dict[str, Any] = {}


def _normalize_ext(ext: str) -> str:
    ext = str(ext).strip().lower()
    if not ext.startswith("."):
        ext = "." + ext
    return ext


def register_format_adapter(
    adapter_or_exts: Any = None,
    extensions: Optional[Iterable[str]] = None,
):
    """Registra um FormatAdapter globalmente por extensão (pode ser usado como decorator ou função)."""

    def _register(adapter: Any, exts: Optional[Iterable[str]] = None):
        target_exts = exts if exts is not None else getattr(adapter, "extensions", ())
        norm_exts = tuple(_normalize_ext(e) for e in target_exts)
        if not getattr(adapter, "extensions", None) and norm_exts:
            try:
                adapter.extensions = norm_exts
            except Exception:
                pass
        for ext in norm_exts:
            _FORMAT_ADAPTER_REGISTRY[ext] = adapter
        return adapter

    if adapter_or_exts is not None and (
        hasattr(adapter_or_exts, "load_file") and hasattr(adapter_or_exts, "save_file")
    ):
        return _register(adapter_or_exts, extensions)

    exts_arg = adapter_or_exts if extensions is None else extensions

    def decorator(adapter_cls):
        return _register(adapter_cls, exts_arg)

    return decorator


def unregister_format_adapter(extension: str) -> None:
    _FORMAT_ADAPTER_REGISTRY.pop(_normalize_ext(extension), None)


def get_registered_format_adapter(file_name_or_path: str) -> Optional[Any]:
    lower = str(file_name_or_path).lower()
    for ext, adapter in sorted(_FORMAT_ADAPTER_REGISTRY.items(), key=lambda kv: len(kv[0]), reverse=True):
        if lower.endswith(ext):
            return adapter
    return None


@register_format_adapter
class JsonFormatAdapter:
    extensions = (".json",)

    @staticmethod
    def load_file(file_path: str) -> Any:
        with open(file_path, "rb") as f:
            raw_bytes = f.read()
        text_content = raw_bytes.decode("utf-8-sig")
        data = json.loads(text_content)
        if isinstance(data, dict):
            wrapped = JsonDataDict(data)
            wrapped._raw_json_bytes = raw_bytes
            wrapped._is_dirty = False
            wrapped._file_name = os.path.basename(file_path)
            return wrapped
        if isinstance(data, list):
            wrapped = JsonDataList(data)
            wrapped._raw_json_bytes = raw_bytes
            wrapped._is_dirty = False
            wrapped._file_name = os.path.basename(file_path)
            return wrapped
        return data

    @staticmethod
    def save_file(file_path: str, data: Any, **ctx: Any) -> None:
        file_name = ctx.get("file_name") or os.path.basename(file_path)
        extractor_cls = ctx.get("extractor_cls")
        os.makedirs(os.path.dirname(file_path) or ".", exist_ok=True)
        try:
            raw_json_bytes = getattr(data, "_raw_json_bytes", None)
            is_dirty = getattr(data, "_is_dirty", True)
            if raw_json_bytes is not None and not is_dirty:
                with open(file_path, "wb") as f:
                    f.write(raw_json_bytes)
                logging.info(f"✓ Validated and saved: {file_name}")
                return

            json_data = data
            if isinstance(json_data, (dict, list)) and extractor_cls is not None and hasattr(extractor_cls, "sanitize_octal_data"):
                json_data = extractor_cls.sanitize_octal_data(json_data)

            json_string = json.dumps(json_data, ensure_ascii=False, separators=(",", ":"))
            with open(file_path, "w", encoding="utf-8") as f:
                f.write(json_string)
            logging.info(f"✓ Validated and saved: {file_name}")
        except (TypeError, ValueError) as e:
            logging.error(f"✗ JSON validation failed for {file_name}: {e}")
            logging.error(f"  Data type: {type(data)}")
            raise Exception(f"Failed to create valid JSON for {file_name}: {e}")
        except Exception as e:
            logging.error(f"✗ Failed to write file {file_name}: {e}")
            raise


@register_format_adapter
class EvtextFormatAdapter:
    extensions = (".txt",)

    @staticmethod
    def load_file(file_path: str) -> Any:
        with open(file_path, "rb") as f:
            raw_bytes = f.read()
        encoding = "cp932"
        try:
            content = raw_bytes.decode("cp932")
        except UnicodeDecodeError:
            content = raw_bytes.decode("utf-8", errors="replace")
            encoding = "utf-8"
        lineterminator = "\r\n" if b"\r\n" in raw_bytes else "\n"
        txt_data = JsonDataDict({
            "encoding": encoding,
            "lineterminator": lineterminator,
            "lines": content.splitlines(keepends=True),
        })
        txt_data._file_name = os.path.basename(file_path)
        return txt_data

    @staticmethod
    def save_file(file_path: str, data: Any, **ctx: Any) -> None:
        file_name = ctx.get("file_name") or os.path.basename(file_path)
        if not isinstance(data, dict) or "lines" not in data:
            JsonFormatAdapter.save_file(file_path, data, **ctx)
            return
        os.makedirs(os.path.dirname(file_path) or ".", exist_ok=True)
        encoding = data.get("encoding", "cp932")
        content = "".join(data.get("lines", []))
        if encoding == "cp932":
            try:
                raw_bytes = content.encode("cp932")
            except UnicodeEncodeError:
                from src.utils.TextsUtils import normalize_western_chars
                raw_bytes = normalize_western_chars(content).encode("cp932", errors="replace")
        else:
            raw_bytes = content.encode(encoding, errors="replace")
        with open(file_path, "wb") as f:
            f.write(raw_bytes)
        logging.info(f"✓ Validated and saved text: {file_name}")
