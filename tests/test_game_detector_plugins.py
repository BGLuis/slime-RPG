import os
import pytest

import src.factory as factory_mod
import src.extractor.FormatAdapter as adapter_mod
from src.factory import ExtractorFactory, register_extractor
from src.extractor.BaseExtractor import BaseExtractor
from src.extractor.FormatAdapter import register_format_adapter
from src.extractor.Segment import Segment, SegmentContext, SegmentList, resolve_translation_map
from src.services.GameDetector import DetectionResult, detect_game_environment


@pytest.fixture
def isolated_plugin_registries(monkeypatch):
    """Isola registros globais de extratores e adaptadores para testes de plugins."""
    ExtractorFactory.discover()
    monkeypatch.setattr(factory_mod, "_EXTRACTORS", dict(factory_mod._EXTRACTORS))
    monkeypatch.setattr(factory_mod, "_EXTRACTOR_ALIASES", dict(factory_mod._EXTRACTOR_ALIASES))
    monkeypatch.setattr(
        adapter_mod, "_FORMAT_ADAPTER_REGISTRY", dict(adapter_mod._FORMAT_ADAPTER_REGISTRY)
    )


def test_game_detector_has_no_hardcoded_engine_names():
    """Garante que GameDetector.py não contém strings mágicas de motores."""
    detector_path = os.path.join(
        os.path.dirname(os.path.dirname(__file__)),
        "src",
        "services",
        "GameDetector.py",
    )
    with open(detector_path, "r", encoding="utf-8") as f:
        source = f.read()

    assert "'RPG Maker'" not in source
    assert '"RPG Maker"' not in source
    assert "'Wolf RPG'" not in source
    assert '"Wolf RPG"' not in source


def test_detection_result_dict_and_attribute_contract():
    res = DetectionResult(
        detected_path="/tmp/game/data",
        score=88,
        extractor="CustomEngine",
        description="Custom Game Engine",
        file_count=4,
    )
    assert res.detected_path == "/tmp/game/data"
    assert res["detected_path"] == "/tmp/game/data"
    assert res.score == 88
    assert res.confidence == 88
    assert res["score"] == 88
    assert res.extractor == "CustomEngine"
    assert res["extractor"] == "CustomEngine"
    assert res.description == "Custom Game Engine"
    assert res.file_count == 4

    res.score = 92
    assert res.confidence == 92
    assert res["score"] == 92


def test_fake_extractor_detected_without_editing_game_detector(tmp_path, isolated_plugin_registries):
    """Prova que um extrator falso registrado via @register_extractor é detectado por GameDetector."""

    @register_extractor("kirikiri_fake", display_name="KiriKiri Fake")
    class FakeKiriKiriExtractor(BaseExtractor):
        files_types = ["ks"]

        @classmethod
        def detect(cls, path: str):
            scenario_dir = os.path.join(path, "scenario")
            if os.path.isdir(scenario_dir):
                ks_files = [f for f in os.listdir(scenario_dir) if f.endswith(".ks")]
                if ks_files:
                    return DetectionResult(
                        detected_path=scenario_dir,
                        score=90,
                        description="KiriKiri Visual Novel (subpasta scenario)",
                        file_count=len(ks_files),
                    )
            return None

        def extract_text(self, file_name, data):
            return data

        def update_json(self, file, data, new_data):
            return new_data

        @staticmethod
        def fix_text_translate(text, original_text=None):
            return text

    game_dir = tmp_path / "MyVN"
    scenario_dir = game_dir / "scenario"
    scenario_dir.mkdir(parents=True)
    (scenario_dir / "prologue.ks").write_text("*start|Prologue\nHello world!", encoding="utf-8")

    # Mesmo com um JSON genérico na raiz (score=30), o score=90 do FakeKiriKiriExtractor vence
    (game_dir / "config.json").write_text("{}", encoding="utf-8")

    detected = detect_game_environment(str(game_dir))
    assert detected is not None
    assert detected["extractor"] == "KiriKiri Fake"
    assert detected.detected_path == str(scenario_dir)
    assert detected.score == 90
    assert detected.file_count == 1


def test_epic_12_full_fake_engine_plugin_end_to_end(tmp_path, isolated_plugin_registries):
    """
    Critério do Épico #12: registra um motor falso completo (detector + FormatAdapter + BaseExtractor
    com Segment round-trip) sem tocar em BaseExtractor.py nem em GameDetector.py.
    """

    @register_format_adapter
    class FakeVNAdapter:
        extensions = (".fvn",)

        @staticmethod
        def load_file(file_path: str):
            lines = []
            with open(file_path, "r", encoding="utf-8") as f:
                for raw in f:
                    raw = raw.rstrip("\r\n")
                    if ":" in raw:
                        speaker, text = raw.split(":", 1)
                        lines.append({"speaker": speaker.strip(), "text": text.strip()})
            return {"dialogues": lines}

        @staticmethod
        def save_file(file_path: str, data, **ctx):
            os.makedirs(os.path.dirname(file_path), exist_ok=True)
            with open(file_path, "w", encoding="utf-8") as f:
                for entry in data.get("dialogues", []):
                    f.write(f"{entry['speaker']}: {entry['text']}\n")
            return True

    @register_extractor("fakevn", display_name="FakeVN Engine", aliases=["FakeVN"])
    class FakeVNExtractor(BaseExtractor):
        files_types = ["fvn"]
        format_adapters = (FakeVNAdapter,)

        @classmethod
        def detect(cls, path: str):
            scenes_dir = os.path.join(path, "scenes")
            target_dir = scenes_dir if os.path.isdir(scenes_dir) else path
            try:
                fvn_files = [f for f in os.listdir(target_dir) if f.lower().endswith(".fvn")]
            except OSError:
                return None
            if fvn_files:
                return DetectionResult(
                    detected_path=target_dir,
                    score=92,
                    description="FakeVN Engine (scenes)",
                    file_count=len(fvn_files),
                )
            return None

        def extract_segments(self, file_name, data) -> SegmentList:
            segments = SegmentList()
            for idx, entry in enumerate(data.get("dialogues", [])):
                segments.append(
                    Segment(
                        id=f"{file_name}:line:{idx}",
                        source=entry["text"],
                        context=SegmentContext(
                            file=str(file_name),
                            event=f"line:{idx}",
                            speaker=entry.get("speaker"),
                            kind="dialogue",
                            index=idx,
                        ),
                    )
                )
            return segments

        def apply_segments(self, file_name, data, translations):
            import copy
            out = copy.deepcopy(data)
            segments = self.extract_segments(file_name, out)
            resolved = resolve_translation_map(segments, translations)
            for seg in segments:
                if seg.id in resolved:
                    idx = seg.context.index
                    out["dialogues"][idx]["text"] = resolved[seg.id]
            return out

        def extract_text(self, file_name, data):
            return {seg.id: seg.source for seg in self.extract_segments(file_name, data)}

        def update_json(self, file, data, new_data):
            return self.apply_segments(file, data, new_data)

        @staticmethod
        def fix_text_translate(text, original_text=None):
            return text

    # 1. Cria estrutura de jogo do motor falso
    game_root = tmp_path / "FakeGame"
    scenes_dir = game_root / "scenes"
    scenes_dir.mkdir(parents=True)
    scene_file = scenes_dir / "chapter1.fvn"
    scene_file.write_text("Alice: Good morning!\nBob: Ready for adventure?\n", encoding="utf-8")

    # 2. Detecta automaticamente via GameDetector
    detected = detect_game_environment(str(game_root))
    assert detected is not None
    assert detected.extractor == "FakeVN Engine"
    assert detected.detected_path == str(scenes_dir)

    # 3. Instancia via ExtractorFactory usando o nome retornado pelo detector
    extractor = ExtractorFactory.create(detected["extractor"], translate_instance=None)
    assert isinstance(extractor, FakeVNExtractor)

    # 4. Carrega via BaseExtractor.extract_files (despachado para FakeVNAdapter)
    rel_name, loaded_data = extractor.extract_files(str(scene_file))
    assert rel_name == "chapter1.fvn"
    assert loaded_data == {
        "dialogues": [
            {"speaker": "Alice", "text": "Good morning!"},
            {"speaker": "Bob", "text": "Ready for adventure?"},
        ]
    }

    # 5. Extrai segmentos tipados e verifica round-trip identitário e traduzido
    segments = extractor.extract_segments(rel_name, loaded_data)
    assert len(segments) == 2
    assert segments[0].id == "chapter1.fvn:line:0"
    assert segments[0].context.speaker == "Alice"

    identity_data = extractor.apply_segments(
        rel_name, loaded_data, {seg.id: seg.source for seg in segments}
    )
    assert identity_data == loaded_data

    translated_data = extractor.apply_segments(
        rel_name,
        loaded_data,
        {
            "chapter1.fvn:line:0": "Bom dia!",
            "chapter1.fvn:line:1": "Pronto para a aventura?",
        },
    )

    # 6. Salva via BaseExtractor.import_file (despachado para FakeVNAdapter)
    output_dir = tmp_path / "output"
    extractor.import_file(rel_name, translated_data, str(output_dir))
    saved_file = output_dir / "chapter1.fvn"
    assert saved_file.exists()
    assert saved_file.read_text(encoding="utf-8") == (
        "Alice: Bom dia!\nBob: Pronto para a aventura?\n"
    )


def test_detector_edge_cases_and_rvdata2(tmp_path, isolated_plugin_registries):
    """Testa casos de borda: caminhos inexistentes, vazios, lixeira, .rvdata2 e retorno dict puro."""
    assert detect_game_environment(str(tmp_path / "nonexistent")) is None
    assert detect_game_environment(str(tmp_path)) is None

    trash_dir = tmp_path / ".Trash-1000" / "Game"
    trash_dir.mkdir(parents=True)
    (trash_dir / "System.json").write_text("{}", encoding="utf-8")
    assert detect_game_environment(str(trash_dir)) is None

    # RPG Maker VX Ace (.rvdata2 em Data/)
    vx_game = tmp_path / "VXAceGame"
    vx_data = vx_game / "Data"
    vx_data.mkdir(parents=True)
    (vx_data / "System.rvdata2").write_bytes(b"\x04\x080")
    det_vx = detect_game_environment(str(vx_game))
    assert det_vx is not None
    assert det_vx["extractor"] == "RPG Maker"
    assert det_vx.detected_path == str(vx_data)

    # Extrator que retorna dict simples em vez de DetectionResult
    @register_extractor("dict_engine", display_name="Dict Engine")
    class DictDetectorExtractor(BaseExtractor):
        files_types = ["dct"]

        @classmethod
        def detect(cls, path: str):
            if os.path.isfile(os.path.join(path, "game.dct")):
                return {
                    "detected_path": path,
                    "score": 99,
                    "description": "Dict-based detection",
                    "file_count": 1,
                }
            return None

        def extract_text(self, file_name, data):
            return data

        def update_json(self, file, data, new_data):
            return new_data

        @staticmethod
        def fix_text_translate(text, original_text=None):
            return text

    dct_game = tmp_path / "DctGame"
    dct_game.mkdir()
    (dct_game / "game.dct").write_text("hello", encoding="utf-8")
    det_dct = detect_game_environment(str(dct_game))
    assert isinstance(det_dct, DetectionResult)
    assert det_dct.extractor == "Dict Engine"
    assert det_dct.score == 99

