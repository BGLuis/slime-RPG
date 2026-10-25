import logging
import os
from typing import Optional, Any
from src.factory import ExtractorFactory
from src.extractor.DetectionResult import DetectionResult


def is_trash_path(path: str) -> bool:
    """Verifica se um caminho pertence a uma lixeira do sistema operacional."""
    if not path:
        return False
    normalized = os.path.abspath(path)
    trash_markers = ['.trash', '/.trash-', '/.local/share/trash', '/trash/']
    return any(marker in normalized.lower() for marker in trash_markers)


def is_project_repo_root(path: str) -> bool:
    """Verifica se o caminho é a raiz do repositório desta ferramenta."""
    if not path or not os.path.isdir(path):
        return False
    has_src = os.path.isdir(os.path.join(path, 'src'))
    has_main = os.path.isfile(os.path.join(path, 'main.py'))
    has_scripts = os.path.isdir(os.path.join(path, 'scripts'))
    return has_src and has_main and has_scripts


def _normalize_detection_result(raw_result: Any, extractor_cls: Any) -> Optional[DetectionResult]:
    if raw_result is None:
        return None
    default_name = getattr(extractor_cls, "display_name", None) or getattr(
        extractor_cls, "name", extractor_cls.__name__
    )
    if isinstance(raw_result, DetectionResult):
        if not raw_result.extractor:
            raw_result.extractor = default_name
        return raw_result
    if isinstance(raw_result, dict):
        detected_path = raw_result.get("detected_path")
        if not detected_path:
            return None
        score = raw_result.get("score", raw_result.get("confidence", 50))
        extractor_name = raw_result.get("extractor") or default_name
        description = raw_result.get("description", "")
        file_count = raw_result.get("file_count", 0)
        extra = {
            k: v
            for k, v in raw_result.items()
            if k not in ("detected_path", "score", "confidence", "extractor", "description", "file_count")
        }
        return DetectionResult(
            detected_path=detected_path,
            score=score,
            extractor=extractor_name,
            description=description,
            file_count=file_count,
            **extra,
        )
    return None


def detect_game_environment(candidate_path: Optional[str] = None) -> Optional[DetectionResult]:
    """
    Detecta automaticamente se uma pasta (ou suas subpastas imediatas)
    é elegível para tradução consultando cada extrator registrado em ExtractorFactory.
    """
    target = candidate_path or os.environ.get('CALLER_WORKING_DIR') or os.getcwd()
    if not target or not os.path.isdir(target):
        return None

    target = os.path.abspath(target)

    if is_trash_path(target) or is_project_repo_root(target):
        return None

    available = ExtractorFactory.get_available()
    best_result: Optional[DetectionResult] = None

    for extractor_cls in available.values():
        detect_fn = getattr(extractor_cls, "detect", None)
        if not callable(detect_fn):
            continue
        try:
            raw = detect_fn(target)
        except Exception as e:
            logging.warning(f"Erro ao detectar ambiente no extrator {extractor_cls}: {e}")
            continue
        result = _normalize_detection_result(raw, extractor_cls)
        if result is None:
            continue
        if best_result is None or result.score > best_result.score:
            best_result = result

    return best_result
