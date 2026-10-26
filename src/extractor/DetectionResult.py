from typing import Optional, Any


class DetectionResult(dict):
    """Resultado estruturado da detecção de ambiente de um extrator."""

    def __init__(
        self,
        detected_path: str,
        score: int = 50,
        extractor: Optional[str] = None,
        description: str = "",
        file_count: int = 0,
        **extra: Any,
    ):
        super().__init__(
            detected_path=detected_path,
            score=score,
            confidence=score,
            extractor=extractor,
            description=description,
            file_count=file_count,
            **extra,
        )

    @property
    def detected_path(self) -> str:
        return self["detected_path"]

    @detected_path.setter
    def detected_path(self, value: str) -> None:
        self["detected_path"] = value

    @property
    def score(self) -> int:
        return self["score"]

    @score.setter
    def score(self, value: int) -> None:
        self["score"] = value
        self["confidence"] = value

    @property
    def confidence(self) -> int:
        return self["confidence"]

    @confidence.setter
    def confidence(self, value: int) -> None:
        self["score"] = value
        self["confidence"] = value

    @property
    def extractor(self) -> Optional[str]:
        return self.get("extractor")

    @extractor.setter
    def extractor(self, value: Optional[str]) -> None:
        self["extractor"] = value

    @property
    def description(self) -> str:
        return self.get("description", "")

    @description.setter
    def description(self, value: str) -> None:
        self["description"] = value

    @property
    def file_count(self) -> int:
        return self.get("file_count", 0)

    @file_count.setter
    def file_count(self, value: int) -> None:
        self["file_count"] = value
