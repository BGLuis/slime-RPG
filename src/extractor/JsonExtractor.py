import copy
import logging
import os
from src.extractor.BaseExtractor import BaseExtractor, _ClassOrInstanceMethod
from src.extractor.Segment import Segment, copy_preserving_metadata
from src.extractor.FormatAdapter import JsonFormatAdapter
from src.extractor.rpgmaker.RPGTextFilters import RPGTextFilters
from src.factory import register_extractor

@register_extractor("json", display_name="Json", aliases=["Json", "JSON"])
class JsonExtractor(BaseExtractor):
    name = "json"
    files_types = ['json']
    format_adapters = (JsonFormatAdapter,)
    LOC_FOLDER_NAMES = ('languages', 'language', 'lang', 'locales', 'localization', 'translations', 'i18n')

    @classmethod
    def detect(cls, path: str):
        import glob
        from src.services.GameDetector import DetectionResult
        from src.utils.LanguageCodes import normalize_language_code

        for loc_name in cls.LOC_FOLDER_NAMES:
            loc_candidate = os.path.join(path, loc_name)
            if os.path.isdir(loc_candidate):
                try:
                    loc_entries = os.listdir(loc_candidate)
                except Exception:
                    loc_entries = []
                subdirs = [d for d in loc_entries if os.path.isdir(os.path.join(loc_candidate, d))]
                lang_subdirs = [d for d in subdirs if normalize_language_code(d) is not None]
                has_lang_json = 'Languages.json' in loc_entries or 'languages.json' in loc_entries
                if len(lang_subdirs) > 0 or has_lang_json:
                    total_json_files = glob.glob(os.path.join(loc_candidate, '**', '*.json'), recursive=True)
                    return DetectionResult(
                        detected_path=loc_candidate,
                        score=95,
                        extractor=cls.display_name,
                        description=f'Sistema de Localização Modular / CTE (subpasta {loc_name})',
                        file_count=len(total_json_files),
                    )

        try:
            entries = os.listdir(path)
        except Exception:
            return None

        subdirs = [d for d in entries if os.path.isdir(os.path.join(path, d))]
        lang_subdirs = [d for d in subdirs if normalize_language_code(d) is not None]
        has_lang_json = 'Languages.json' in entries or 'languages.json' in entries
        if len(lang_subdirs) > 0 or has_lang_json:
            total_json_files = glob.glob(os.path.join(path, '**', '*.json'), recursive=True)
            return DetectionResult(
                detected_path=path,
                score=60,
                extractor=cls.display_name,
                description='Sistema de Localização Modular / CTE (pasta atual)',
                file_count=len(total_json_files),
            )

        json_files = [f for f in entries if f.lower().endswith('.json')]
        if len(json_files) > 0:
            return DetectionResult(
                detected_path=path,
                score=30,
                extractor=cls.display_name,
                description='Arquivos JSON',
                file_count=len(json_files),
            )

        return None

    DEFAULT_TRANSLATABLE_KEYS = (
        'text', 'message', 'name', 'description', 'title',
        'dialogue', 'content', 'summary', 'caption', 'label',
        'desc', 'bio', 'note', 'hint', 'help'
    )

    FILE_EXTENSIONS = (
        '.png', '.jpg', '.jpeg', '.gif', '.webp', '.bmp', '.svg',
        '.ogg', '.mp3', '.wav', '.m4a', '.flac', '.mid',
        '.json', '.js', '.csv', '.dat', '.mps', '.project',
        '.ttf', '.otf', '.woff', '.woff2'
    )

    def __init__(self, translate, workspace=None):
        super().__init__(translate, workspace=workspace)
        self.translatable_keys = set(k.lower() for k in self.DEFAULT_TRANSLATABLE_KEYS)

    @classmethod
    def get_interactive_questions(cls):
        return [
            {
                'key': 'keys',
                'question': 'Digite os nomes das chaves JSON a traduzir (separadas por vírgula, ex: text,name,description ou * para todas):',
                'title': '\n=== CONFIGURAÇÃO JSON ===',
                'description': 'Especifique quais chaves do JSON devem ser traduzidas (use * para todas as chaves não-técnicas).',
                'color': 'cyan',
                'required': False
            }
        ]

    def apply_configuration(self, config):
        if not config:
            return
        raw_keys = config.get('keys') or config.get('columns')
        if raw_keys:
            if isinstance(raw_keys, str):
                stripped = raw_keys.strip()
                if stripped in ('*', 'all'):
                    self.translatable_keys = {'*'}
                    logging.info("✓ JSON configurado para traduzir todas as chaves de texto não-técnicas (*)")
                else:
                    keys = {k.strip().lower() for k in stripped.split(',') if k.strip()}
                    self.translatable_keys = keys
                    logging.info(f"✓ Chaves JSON configuradas para tradução: {', '.join(sorted(keys))}")
            elif isinstance(raw_keys, (list, set, tuple)):
                self.translatable_keys = {str(k).strip().lower() for k in raw_keys}
                logging.info(f"✓ Chaves JSON configuradas para tradução: {', '.join(sorted(self.translatable_keys))}")

    def get_mask_patterns(self, file_name=None, data=None):
        import re
        p_format_codes = re.compile(r'\\[A-Za-z]+\s*\[[^\]]*\]', re.IGNORECASE)
        p_angle_codes = re.compile(r'\\[A-Za-z]+\s*<.*?>', re.IGNORECASE)
        p_conditions = re.compile(r'@(?:if|else)[^\n]*', re.IGNORECASE)
        p_template_expr = re.compile(r'\$\{[^}]*\}')
        p_game_expr = re.compile(r'\$game(?:Variables|Switches|Party|Actors|Player|Map|System|Screen)(?:\.[a-zA-Z0-9_]+(?:\([^)]*\))?)*', re.IGNORECASE)
        p_arrow_func = re.compile(r'^\s*\(?[a-zA-Z0-9_,\s]*\)?\s*=>.*$', re.DOTALL)
        p_bracket_vars = re.compile(r'!?(?<!\\)(?<![A-Za-z0-9_])(?:[vVsS]|ss|sv)\[\s*\d+\s*\]')
        p_printf = re.compile(r'%[0-9]*\.?[0-9]*[sdfoxX]')
        p_curly_vars = re.compile(r'\{[a-zA-Z0-9_]+\}')
        p_unity_tags = re.compile(r'</?[a-zA-Z][a-zA-Z0-9=_\-#]*(?:\s+[^>]*)?/?>', re.IGNORECASE)
        return [p_format_codes, p_angle_codes, p_conditions, p_template_expr, p_game_expr, p_arrow_func, p_bracket_vars, p_printf, p_curly_vars, p_unity_tags]

    @staticmethod
    def _is_translatable_string(text: str) -> bool:
        if not isinstance(text, str):
            return False
        s = text.strip()
        if not s:
            return False
        if RPGTextFilters.is_technical_or_code(s):
            return False
        lower = s.lower()
        if any(lower.endswith(ext) for ext in JsonExtractor.FILE_EXTENSIONS):
            return False
        if ' ' not in s and ('/' in s or '\\' in s):
            return False
        return True

    def extract_text(self_or_file, file_name_or_data, maybe_data=None):
        if maybe_data is not None:
            self_inst = self_or_file if isinstance(self_or_file, JsonExtractor) else None
            data = maybe_data
        else:
            self_inst = self_or_file if isinstance(self_or_file, JsonExtractor) else None
            data = file_name_or_data

        if not isinstance(data, (dict, list)):
            return None

        active_keys = (
            self_inst.translatable_keys if self_inst and hasattr(self_inst, 'translatable_keys')
            else set(k.lower() for k in JsonExtractor.DEFAULT_TRANSLATABLE_KEYS)
        )
        all_keys = '*' in active_keys

        def is_key_active(k: str) -> bool:
            if all_keys:
                return True
            return str(k).lower() in active_keys

        translatable_count = 0

        def walk(node, parent_is_active=False):
            nonlocal translatable_count
            if isinstance(node, dict):
                res = {}
                for k, v in node.items():
                    k_active = is_key_active(k)
                    if isinstance(v, dict):
                        child = walk(v, parent_is_active=k_active)
                        if child:
                            res[k] = child
                    elif isinstance(v, list):
                        child = walk(v, parent_is_active=k_active)
                        if child and any(item is not None and item != {} for item in child):
                            res[k] = child
                    elif isinstance(v, str):
                        if k_active and JsonExtractor._is_translatable_string(v):
                            res[k] = v
                            translatable_count += 1
                return res

            elif isinstance(node, list):
                res = []
                has_content = False
                for item in node:
                    if isinstance(item, (dict, list)):
                        child = walk(item, parent_is_active=parent_is_active)
                        res.append(child)
                        if child and child != {} and child != []:
                            has_content = True
                    elif isinstance(item, str):
                        if parent_is_active and JsonExtractor._is_translatable_string(item):
                            res.append(item)
                            translatable_count += 1
                            has_content = True
                        else:
                            res.append(None)
                    else:
                        res.append(None)
                return res if has_content else []

            return None

        result = walk(data, parent_is_active=all_keys)
        if translatable_count == 0:
            return None
        return result

    @staticmethod
    def _deep_merge(target, updates):
        if isinstance(target, dict) and isinstance(updates, dict):
            for k, v in updates.items():
                if k in target:
                    target[k] = JsonExtractor._deep_merge(target[k], v)
                else:
                    target[k] = copy.deepcopy(v)
            return target
        elif isinstance(target, list) and isinstance(updates, list):
            for i, v in enumerate(updates):
                if i < len(target):
                    if v is not None and v != {}:
                        target[i] = JsonExtractor._deep_merge(target[i], v)
                else:
                    target.append(copy.deepcopy(v))
            return target
        else:
            return updates if updates is not None else target

    def update_json(self_or_file, file_name_or_data, data_or_new=None, maybe_new_data=None):
        if maybe_new_data is not None:
            data = data_or_new
            new_data = maybe_new_data
        elif data_or_new is not None:
            data = file_name_or_data
            new_data = data_or_new
        else:
            return None

        if not isinstance(data, (dict, list)) or not isinstance(new_data, (dict, list)):
            return None

        cleaned_data = BaseExtractor.remove_old_keys(new_data)
        target = copy_preserving_metadata(data)
        res = JsonExtractor._deep_merge(target, cleaned_data)
        if hasattr(res, '__dict__'):
            res._is_dirty = True
        return res

    @_ClassOrInstanceMethod
    def _walk_or_apply_segments(target, file_name, data, mode='extract', translations=None):
        cls = target if isinstance(target, type) else target.__class__
        inst = target._create_segment_instance() if isinstance(target, type) else target
        base_name = os.path.basename(file_name) if file_name else cls._infer_file_name(data)
        extracted = inst.extract_text(base_name, data)
        if not extracted:
            return [] if mode == 'extract' else data

        segments = []
        translations = translations or {}
        changed_any = False

        def _escape_ptr(token: str) -> str:
            return str(token).replace('~', '~0').replace('/', '~1')

        def _walk(node, ptr=""):
            nonlocal changed_any
            if isinstance(node, dict):
                out = {}
                for k, v in node.items():
                    child_ptr = f"{ptr}/{_escape_ptr(k)}"
                    if isinstance(v, (dict, list)):
                        out[k] = _walk(v, child_ptr)
                    elif isinstance(v, str):
                        seg_id = f"{base_name}#{child_ptr}"
                        k_lower = str(k).lower()
                        if k_lower in ('name', 'title', 'label', 'caption', 'header', 'id', 'category'):
                            item_kind = 'menu'
                        else:
                            item_kind = 'dialogue'
                        if mode == 'extract':
                            segments.append(Segment(
                                id=seg_id,
                                source=v,
                                context={'file': base_name, 'pointer': child_ptr, 'key': k, 'kind': item_kind},
                            ))
                        else:
                            new_v = translations.get(seg_id, v)
                            if new_v != v:
                                changed_any = True
                            out[k] = new_v
                    else:
                        out[k] = v
                return out
            elif isinstance(node, list):
                out = []
                for idx, item in enumerate(node):
                    child_ptr = f"{ptr}/{idx}"
                    if isinstance(item, (dict, list)):
                        out.append(_walk(item, child_ptr))
                    elif isinstance(item, str):
                        seg_id = f"{base_name}#{child_ptr}"
                        if mode == 'extract':
                            segments.append(Segment(
                                id=seg_id,
                                source=item,
                                context={'file': base_name, 'pointer': child_ptr, 'index': idx, 'kind': 'dialogue'},
                            ))
                        else:
                            new_v = translations.get(seg_id, item)
                            if new_v != item:
                                changed_any = True
                            out.append(new_v)
                    else:
                        out.append(item)
                return out
            return node

        updated_tree = _walk(extracted, "")
        if mode == 'extract':
            return segments
        if not changed_any:
            return data
        return inst.update_json(base_name, data, updated_tree)

    @staticmethod
    def fix_text_translate(text, original_text=None):
        return text
