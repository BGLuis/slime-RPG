import os
import json
from src.extractor.rpgmaker.RPGMakerExtractor import RPGMakerExtractor
from src.pipeline import (
    TranslationContext, TranslationPipeline,
    GetPatternsStep, MaskingStep, UnmaskingStep, FixingStep
)
import src.utils.TextsUtils as TextsUtils


def test_map002_and_commonevents_pipeline():
    base_ja = "/mnt/hdd/game/Mahou Shoujo Shirufiina otomi-games/otomi-games.com_GN6UASU1O/魔法少女シルフィーナ/www/data-ja"
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
        merged = extractor.merge_dicts_texts(result_text, raw_text)
        updated_data = extractor.update_json(fname, data, merged)

        # Validar serialização JSON
        dumped = json.dumps(updated_data, ensure_ascii=False)
        reloaded = json.loads(dumped)
        assert reloaded is not None
        print(f"Pipeline verification PASSED for {fname} ({len(orig_list)} strings)")


if __name__ == "__main__":
    test_map002_and_commonevents_pipeline()
