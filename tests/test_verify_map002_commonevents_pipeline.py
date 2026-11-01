import os
import json
import pytest
from src.extractor.rpgmaker.RPGMakerExtractor import RPGMakerExtractor
from src.pipeline import (
    TranslationContext, TranslationPipeline,
    GetPatternsStep, MaskingStep, UnmaskingStep
)
import src.utils.TextsUtils as TextsUtils


def _build_synthetic_rpgmaker_fixture_dir(tmp_path):
    map002 = {
        "displayName": "地下水路-最奥",
        "events": [
            None,
            {
                "id": 1,
                "name": "EV001",
                "pages": [
                    {
                        "list": [
                            {"code": 101, "indent": 0, "parameters": ["Actor1", 0, 0, 2, "ルミナ"]},
                            {"code": 401, "indent": 0, "parameters": ["\\C[27]・・・？♡\\C[0] ここはどこ？"]},
                            {
                                "code": 102,
                                "indent": 0,
                                "parameters": [
                                    [
                                        "地下水路-最奥if(s[183])en(!s[4982])",
                                        "夜まで休むif(s[2])",
                                    ],
                                    1,
                                ],
                            },
                            {
                                "code": 355,
                                "indent": 0,
                                "parameters": ['$gameScreen.OriginalMessage("ルミナ", "「（……）」", 0, 2);'],
                            },
                            {"code": 0, "indent": 0, "parameters": []},
                        ]
                    }
                ],
            },
        ],
    }
    common_events = [
        None,
        {
            "id": 1,
            "name": "休憩イベント",
            "list": [
                {
                    "code": 355,
                    "indent": 0,
                    "parameters": ["TickerManager.show('\\\\c[16]身体を休めました……');"],
                },
                {
                    "code": 205,
                    "indent": 0,
                    "parameters": [
                        8,
                        {
                            "list": [
                                {"code": 45, "parameters": ["TickerManager.show('\\\\c[16]☆メインクエストクリア');"]},
                                {"code": 0},
                            ]
                        },
                    ],
                },
                {"code": 0, "indent": 0, "parameters": []},
            ],
        },
    ]
    (tmp_path / "Map002.json").write_text(json.dumps(map002, ensure_ascii=False), encoding="utf-8")
    (tmp_path / "CommonEvents.json").write_text(
        json.dumps(common_events, ensure_ascii=False), encoding="utf-8"
    )
    return str(tmp_path)


def test_map002_and_commonevents_pipeline(tmp_path):
    base_ja = "/mnt/hdd/game/Mahou Shoujo Shirufiina otomi-games/otomi-games.com_GN6UASU1O/魔法少女シルフィーナ/www/data-ja"
    if not os.path.exists(base_ja):
        base_ja = _build_synthetic_rpgmaker_fixture_dir(tmp_path)
    extractor = RPGMakerExtractor(None)

    for fname in ["Map002.json", "CommonEvents.json"]:
        fpath = os.path.join(base_ja, fname)
        with open(fpath, "r", encoding="utf-8") as f:
            data = json.load(f)

        raw_text = extractor.extract_text(fname, data)
        assert raw_text, f"{fname} should have extracted texts"

        # Simular pipeline local (sem rede)
        context = TranslationContext(
            file_name=fname,
            data=data,
            text=raw_text,
            translate_instance=None,
            file_path_str=fpath,
            extractor=extractor
        )

        pipeline = TranslationPipeline()
        pipeline.add_step(GetPatternsStep()) \
                .add_step(MaskingStep()) \
                .add_step(UnmaskingStep())

        result_text = pipeline.execute(context)

        # Validar que todos os textos desmascarados batem com os originais
        orig_list = TextsUtils.dictToList(raw_text)
        res_list = TextsUtils.dictToList(result_text)
        assert len(orig_list) == len(res_list), f"Contagem divergente em {fname}"
        assert orig_list == res_list, f"Textos divergentes após unmask em {fname}"

        # Executar FixingStep (normalização de format codes e aspas)
        fixed_text = extractor.fix_text_translate(result_text, raw_text)

        # Validar update_json
        merged = extractor.merge_dicts_texts(fixed_text, raw_text)
        updated_data = extractor.update_json(fname, data, merged)

        # Validar serialização JSON
        dumped = json.dumps(updated_data, ensure_ascii=False)
        reloaded = json.loads(dumped)
        assert reloaded is not None
        print(f"Pipeline verification PASSED for {fname} ({len(orig_list)} strings)")


if __name__ == "__main__":
    test_map002_and_commonevents_pipeline()
