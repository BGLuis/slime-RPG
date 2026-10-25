import re
import logging
from src.extractor.BaseExtractor import BaseExtractor, _ClassOrInstanceMethod
import src.utils.TextsUtils as TextsUtils
from .RPGEventCodes import RPGEventCode
from .RPGTextFilters import RPGTextFilters
from .RPGEventStrategy import (
    ShowTextStrategy, ChoiceStrategy, ChoiceBranchStrategy, ScriptStrategy,
    PluginStrategyMZ, PluginStrategyMV, ControlVariablesStrategy,
    MovementRouteStrategy, RouteStepStrategy
)
from src.factory import register_extractor

class _MatchSpan:
    """Objeto de match leve compatível com a interface esperada por callers de re.Pattern."""
    def __init__(self, text: str, start: int, end: int):
        self._text = text
        self._start = start
        self._end = end

    def group(self, idx: int = 0) -> str:
        if idx == 0:
            return self._text[self._start:self._end]
        raise IndexError("No such group")

    def start(self) -> int:
        return self._start

    def end(self) -> int:
        return self._end

    def span(self):
        return (self._start, self._end)


class _CodeOnlyPattern:
    """Wrapper de padrão regex que só casa quando o segmento inteiro é código técnico."""
    def __init__(self, regex: re.Pattern):
        self._regex = regex
        self.pattern = regex.pattern

    def _active(self, s: str) -> bool:
        return isinstance(s, str) and RPGTextFilters.is_technical_or_code(s)

    def finditer(self, s: str):
        if not self._active(s):
            return iter(())
        return self._regex.finditer(s)

    def search(self, s: str):
        if not self._active(s):
            return None
        return self._regex.search(s)

    def findall(self, s: str):
        if not self._active(s):
            return []
        return self._regex.findall(s)

    def sub(self, repl, s: str) -> str:
        if not self._active(s):
            return s
        return self._regex.sub(repl, s)


class _BalancedConditionPattern:
    """
    Reconhece condições com parênteses balanceados (incluindo aninhamento como `if (a(b) && c)`).
    - Em segmentos de código: aceita `if|en|pt|ja|es|fr|show_if|hide_if` seguidos de `(...)`.
    - Em segmentos de diálogo/prosa: ignora palavras naturais (`es`, `fr`, `pt`, `ja`, `If (you)`)
      e só mascara condições inline de escolhas do RPG Maker (`show_if(...)`, `hide_if(...)` ou
      `if(...)`/`en(...)` sem espaço e contendo sintaxe de código `[...]`, `$`, `!`, `=`, `<`, `>`, `&`, `|`).
    """
    _CODE_HEAD = re.compile(r'(?<![a-zA-Z0-9_])(if|en|pt|ja|es|fr|show_if|hide_if)\s*\(', re.IGNORECASE)
    _PROSE_HEAD = re.compile(r'(?<![a-zA-Z0-9_])(show_if|hide_if|if|en)(\s*)\(', re.IGNORECASE)
    _INLINE_CODE_CHARS = re.compile(r'[\[\]\$=<>!&|]')
    pattern = r'(?<![a-zA-Z0-9_])(?:if|show_if|hide_if)\s*\((?:[^()]|\([^()]*\))+\)'

    @staticmethod
    def _find_balanced_end(s: str, open_paren_idx: int) -> int:
        depth = 0
        in_quote = None
        escaped = False
        for idx in range(open_paren_idx, len(s)):
            ch = s[idx]
            if escaped:
                escaped = False
                continue
            if ch == '\\':
                escaped = True
                continue
            if in_quote is not None:
                if ch == in_quote:
                    in_quote = None
                continue
            if ch in ('"', "'"):
                in_quote = ch
                continue
            if ch == '(':
                depth += 1
            elif ch == ')':
                depth -= 1
                if depth == 0:
                    return idx + 1
        return -1

    def finditer(self, s: str):
        if not isinstance(s, str):
            return
        is_code = RPGTextFilters.is_technical_or_code(s)
        head_re = self._CODE_HEAD if is_code else self._PROSE_HEAD
        pos = 0
        n = len(s)
        while pos < n:
            m = head_re.search(s, pos)
            if not m:
                break
            open_idx = m.end() - 1
            end_idx = self._find_balanced_end(s, open_idx)
            if end_idx == -1:
                pos = m.end()
                continue

            if not is_code:
                kw = m.group(1).lower()
                ws = m.group(2)
                inner = s[open_idx + 1:end_idx - 1]
                if kw in ('if', 'en'):
                    if ws or not self._INLINE_CODE_CHARS.search(inner):
                        pos = m.end()
                        continue

            yield _MatchSpan(s, m.start(), end_idx)
            pos = end_idx

    def search(self, s: str):
        return next(self.finditer(s), None)

    def findall(self, s: str):
        return [m.group(0) for m in self.finditer(s)]

    def sub(self, repl, s: str) -> str:
        matches = list(self.finditer(s))
        if not matches:
            return s
        out = []
        last = 0
        for m in matches:
            out.append(s[last:m.start()])
            out.append(repl(m) if callable(repl) else repl)
            last = m.end()
        out.append(s[last:])
        return ''.join(out)


@register_extractor("rpgmaker", display_name="RPG Maker", aliases=["RPG Maker", "rpg_maker", "rpg-maker"])
class RPGMakerExtractor(BaseExtractor):
    name = "rpgmaker"
    files_types = ['json', 'rvdata2']

    def __init__(self, translate):
        super().__init__(translate)
        self._strategies = {
            RPGEventCode.SHOW_TEXT: ShowTextStrategy(),
            RPGEventCode.SHOW_TEXT_MORE: ShowTextStrategy(),
            RPGEventCode.SHOW_CHOICES: ChoiceStrategy(),
            RPGEventCode.SCRIPT: ScriptStrategy(),
            RPGEventCode.SCRIPT_MORE: ScriptStrategy(),
            RPGEventCode.SHOW_NAME: ShowTextStrategy(),
            RPGEventCode.CHANGE_NAME: ShowTextStrategy(),
            # Mesmo formato de parâmetros que CHANGE_NAME ([actorId, texto]): apelido e perfil
            # do ator são texto visível ao jogador, não dado interno.
            RPGEventCode.CHANGE_NICKNAME: ShowTextStrategy(),
            RPGEventCode.CHANGE_PROFILE: ShowTextStrategy(),
            RPGEventCode.CONTROL_VARIABLES: ControlVariablesStrategy(),
            RPGEventCode.PLUGIN_COMMAND_MV: PluginStrategyMV(),
            RPGEventCode.PLUGIN_COMMAND_MZ: PluginStrategyMZ(),
            RPGEventCode.CHOICE_BRANCH: ChoiceBranchStrategy(),
            RPGEventCode.SET_MOVEMENT_ROUTE: MovementRouteStrategy(),
            RPGEventCode.ROUTE_STEP: RouteStepStrategy(),
            # LABEL (118), COMMENT (108) e COMMENT_MORE (408) ficam de propósito sem strategy:
            # são identificadores internos de salto, anotações de dev e diretivas de plugins
            # (ex: MOG Chrono Engine, Yanfly, tags de colisão, IDs de ferramentas/ações),
            # nunca texto visível ao jogador. Traduzi-los corrompe mecânicas e sistemas de batalha.
        }

    def apply_configuration(self, config):
        pass

    def get_mask_patterns(self, file_name=None, data=None):
        p_format_codes = re.compile(r'\\[A-Za-z]+\s*\[[^\]]*\]', re.IGNORECASE)
        p_condition = _BalancedConditionPattern()
        game_keys = ['variables', 'switches', 'party', 'actors', 'player', 'map', 'system', 'screen', 'timer', 'message', 'temp', 'troop', 'interpreter']
        p_game = re.compile(r'\$game(?:' + '|'.join(game_keys) + r')\b', re.IGNORECASE)
        p_bracket_vars = re.compile(r'!?(?<!\\)(?<![a-zA-Z0-9_])[A-Za-z]{1,2}\s*\[\s*\d+\s*\]', re.IGNORECASE)
        p_boolean_literals = _CodeOnlyPattern(re.compile(r'\b(?:true|false|null|undefined|NaN)\b', re.IGNORECASE))
        p_operators = _CodeOnlyPattern(re.compile(r'(?:&&|\|\||>=|<=|!==|===|!=|==)'))
        p_dollar_camel = re.compile(r'\$\s*[a-z]+[A-Z][a-zA-Z]*')
        p_html_tags = re.compile(r'</?[a-zA-Z][a-zA-Z0-9]*(?:\s+[^>]*)?/?>', re.IGNORECASE)
        p_technical_values = re.compile(r'^(?:Yes|No|OK|Cancel|end|start|stop|play|pause|nochannel|channel)$', re.IGNORECASE)

        return [p_format_codes, p_condition, p_game, p_bracket_vars, p_boolean_literals, p_operators, p_dollar_camel, p_html_tags, p_technical_values]

    @staticmethod
    def ignore_text(text):
        return RPGTextFilters.is_technical_or_code(text)

    def extract_text_codes_item(self, item, index):
        code = RPGEventCode.from_code(item.get('code'))
        strategy = self._strategies.get(code)
        if strategy:
            text = strategy.extract(item)
            if text:
                return {"id": index, "text": text}
        return {"id": index, "text": []}

    def insert_text_map_item(self, item, list_item):
        code = RPGEventCode.from_code(item.get('code'))
        strategy = self._strategies.get(code)
        if strategy:
            strategy.insert(item, list_item['text'])

    def _yield_commands(self, data, file_type):
        if not isinstance(data, (dict, list)):
            return

        if file_type == 'Map':
            if not isinstance(data, dict):
                return
            events = data.get('events')
            if isinstance(events, list):
                for event in events:
                    if not event or not isinstance(event, dict): continue
                    pages = event.get('pages')
                    if not isinstance(pages, list): continue
                    for page_idx, page in enumerate(pages):
                        if not isinstance(page, dict): continue
                        cmd_list = page.get('list')
                        if not isinstance(cmd_list, list): continue
                        for cmd_idx, cmd in enumerate(cmd_list):
                            if isinstance(cmd, dict):
                                yield (event.get('id'), page_idx, cmd_idx, cmd)
            elif isinstance(events, dict):
                for event_id, event in events.items():
                    if not event or not isinstance(event, dict): continue
                    pages = event.get('pages')
                    if not isinstance(pages, list): continue
                    for page_idx, page in enumerate(pages):
                        if not isinstance(page, dict): continue
                        cmd_list = page.get('list')
                        if not isinstance(cmd_list, list): continue
                        for cmd_idx, cmd in enumerate(cmd_list):
                            if isinstance(cmd, dict):
                                yield (event.get('id', event_id), page_idx, cmd_idx, cmd)
        elif file_type == 'CommonEvents':
            if not isinstance(data, list):
                return
            for event in data:
                if not event or not isinstance(event, dict): continue
                cmd_list = event.get('list')
                if not isinstance(cmd_list, list): continue
                for cmd_idx, cmd in enumerate(cmd_list):
                    if isinstance(cmd, dict):
                        yield (event.get('id'), None, cmd_idx, cmd)
        elif file_type == 'Troops':
            if not isinstance(data, list):
                return
            for troop in data:
                if not troop or not isinstance(troop, dict): continue
                pages = troop.get('pages')
                if not isinstance(pages, list): continue
                for page_idx, page in enumerate(pages):
                    if not isinstance(page, dict): continue
                    cmd_list = page.get('list')
                    if not isinstance(cmd_list, list): continue
                    for cmd_idx, cmd in enumerate(cmd_list):
                        if isinstance(cmd, dict):
                            yield (troop.get('id'), page_idx, cmd_idx, cmd)

    def extract_text_map(self, data):
        results = []
        events_map = {}
        for event_id, page_idx, cmd_idx, cmd in self._yield_commands(data, 'Map'):
            text_obj = self.extract_text_codes_item(cmd, cmd_idx)
            if text_obj['text']:
                if event_id not in events_map:
                    events_map[event_id] = {"id": event_id, "pages": []}
                    results.append(events_map[event_id])
                
                # Find or create page
                page_obj = next((p for p in events_map[event_id]['pages'] if p['id'] == page_idx), None)
                if not page_obj:
                    page_obj = {"id": page_idx, "list": []}
                    events_map[event_id]['pages'].append(page_obj)
                
                page_obj['list'].append(text_obj)
        return results

    @staticmethod
    def _resolve_by_id(collection, target_id):
        if isinstance(collection, dict):
            if target_id in collection and isinstance(collection[target_id], dict):
                return collection[target_id]
            for val in collection.values():
                if isinstance(val, dict) and val.get('id') == target_id:
                    return val
            return None
        if isinstance(collection, list):
            if any(isinstance(el, dict) and 'id' in el for el in collection):
                for el in collection:
                    if isinstance(el, dict) and el.get('id') == target_id:
                        return el
            if isinstance(target_id, int) and 0 <= target_id < len(collection):
                candidate = collection[target_id]
                if isinstance(candidate, dict):
                    return candidate
        return None

    def insert_text_map(self, data, translated_data):
        if not isinstance(data, dict):
            return
        events = data.get('events')
        if not isinstance(events, (list, dict)):
            return
        for event_data in translated_data:
            raw_ev = self._resolve_by_id(events, event_data['id'])
            if not isinstance(raw_ev, dict):
                continue
            for page_data in event_data['pages']:
                raw_page = self._resolve_by_id(raw_ev.get('pages', []), page_data['id'])
                if not isinstance(raw_page, dict):
                    continue
                for list_item in page_data['list']:
                    cmd_id = list_item['id']
                    try:
                        cmd = raw_page['list'][cmd_id]
                        self.insert_text_map_item(cmd, list_item)
                    except (IndexError, KeyError, TypeError):
                        continue

    def extract_text_common_events(self, data):
        results = []
        events_map = {}
        for event_id, _, cmd_idx, cmd in self._yield_commands(data, 'CommonEvents'):
            text_obj = self.extract_text_codes_item(cmd, cmd_idx)
            if text_obj['text']:
                if event_id not in events_map:
                    events_map[event_id] = {"id": event_id, "list": []}
                    results.append(events_map[event_id])
                events_map[event_id]['list'].append(text_obj)
        return results

    def insert_text_common_events(self, data, translated_data):
        if not isinstance(data, (list, dict)):
            return
        for event_data in translated_data:
            raw_ev = self._resolve_by_id(data, event_data['id'])
            if not isinstance(raw_ev, dict):
                continue
            for list_item in event_data['list']:
                cmd_id = list_item['id']
                try:
                    cmd = raw_ev['list'][cmd_id]
                    self.insert_text_map_item(cmd, list_item)
                except (IndexError, KeyError, TypeError):
                    continue

    def extract_text_troops(self, data):
        results = []
        troops_map = {}
        for troop_id, page_idx, cmd_idx, cmd in self._yield_commands(data, 'Troops'):
            text_obj = self.extract_text_codes_item(cmd, cmd_idx)
            if text_obj['text']:
                if troop_id not in troops_map:
                    troops_map[troop_id] = {"id": troop_id, "pages": []}
                    results.append(troops_map[troop_id])
                
                page_obj = next((p for p in troops_map[troop_id]['pages'] if p['id'] == page_idx), None)
                if not page_obj:
                    page_obj = {"id": page_idx, "list": []}
                    troops_map[troop_id]['pages'].append(page_obj)
                
                page_obj['list'].append(text_obj)
        return results

    def insert_text_troops(self, data, translated_data):
        if not isinstance(data, (list, dict)):
            return
        for troop_data in translated_data:
            raw_tr = self._resolve_by_id(data, troop_data['id'])
            if not isinstance(raw_tr, dict):
                continue
            for page_data in troop_data['pages']:
                raw_page = self._resolve_by_id(raw_tr.get('pages', []), page_data['id'])
                if not isinstance(raw_page, dict):
                    continue
                for list_item in page_data['list']:
                    cmd_id = list_item['id']
                    try:
                        cmd = raw_page['list'][cmd_id]
                        self.insert_text_map_item(cmd, list_item)
                    except (IndexError, KeyError, TypeError):
                        continue

    _attributes_find = [r'name', r'note', r'profile', r'description', r'nickname', r'message\d{1}']
    _attributes_system_find = ['armorTypes', 'equipTypes', 'gameTitle', 'currencyUnit', 'skillTypes', 'terms', 'elements', 'weaponTypes']
    # Tags de nota do tipo <Desc: ...>, <Help: ...>, <InfoAlgumaCoisa: ...>. O grupo 1
    # precisa capturar só o nome da tag (sem o "<"), pois insert_text_object o reusa
    # para reconstruir a tag original.
    _note_tag_pattern = re.compile(r'<(Desc|Help|Text|Profile|Message|Info[^:>]*):\s*([^>]+)>', re.IGNORECASE)

    def extract_text_object(self, data):
        text = []
        if not isinstance(data, list):
            return text
        for item in data:
            if item and isinstance(item, dict) and item.get('name', '') != '':
                obj = {'id': item.get('id')}
                for key, val in item.items():
                    if any(re.match(pattern, key) for pattern in self._attributes_find):
                        if key == 'note' and val and isinstance(val, str):
                            matches = self._note_tag_pattern.findall(val)
                            if matches:
                                note_data = {tag: content for tag, content in matches}
                                if note_data: obj[key] = note_data
                        elif isinstance(val, str) and not RPGTextFilters.is_technical_or_code(val):
                            obj[key] = val
                if len(obj) > 1:
                    text.append(obj)
        return text

    def insert_text_object(self, data, translated_data):
        if not isinstance(data, (list, dict)):
            return
        for texts in translated_data:
            item_id = texts['id']
            target_item = self._resolve_by_id(data, item_id)
            if not isinstance(target_item, dict):
                continue
            if 'note' in texts and isinstance(texts['note'], dict):
                original_note = target_item.get('note', '')
                for tag, new_content in texts['note'].items():
                    pattern = re.compile(fr'(<{re.escape(tag)}\s*:\s*)([^>]+)(>)', re.IGNORECASE)
                    original_note = pattern.sub(lambda m: f"{m.group(1)}{new_content}{m.group(3)}", original_note)
                target_item['note'] = original_note
                texts['note'] = original_note
            target_item.update(texts)

    def extract_text_System(self, data):
        import copy
        text = {}
        if not isinstance(data, dict):
            return text
        for key, value in data.items():
            if key in self._attributes_system_find:
                text[key] = copy.deepcopy(value)
        return text

    def insert_text_System(self, data, translated_data):
        if not isinstance(data, dict) or not isinstance(translated_data, dict):
            return
        for key, value in translated_data.items():
            if key in self._attributes_system_find:
                if isinstance(value, list) and key in data and isinstance(data[key], list):
                    for i, item in enumerate(value):
                        if i < len(data[key]) and RPGTextFilters.is_technical_or_code(data[key][i]):
                            value[i] = data[key][i]
                elif isinstance(value, dict) and key in data and isinstance(data[key], dict):
                    for sub_k, sub_v in value.items():
                        if isinstance(sub_v, list) and sub_k in data[key] and isinstance(data[key][sub_k], list):
                            for i, item in enumerate(sub_v):
                                if i < len(data[key][sub_k]) and RPGTextFilters.is_technical_or_code(data[key][sub_k][i]):
                                    sub_v[i] = data[key][sub_k][i]
                data[key] = value

    @property
    def extract_map(self):
        return [
            {"file_patterns": [r'Map\d{3,}'], "extractor": self.extract_text_map, "insert": self.insert_text_map},
            {"file_patterns": [r'CommonEvents'], "extractor": self.extract_text_common_events, "insert": self.insert_text_common_events},
            {"file_patterns": [r'Troops'], "extractor": self.extract_text_troops, "insert": self.insert_text_troops},
            {"file_patterns": [r'MapInfos', r'Weapons', r'Items', r'Skills', r'States', r'Enemies', r'Actors', r'Armors', r'Classes'], "extractor": self.extract_text_object, "insert": self.insert_text_object},
            {"file_patterns": [r'System'], "extractor": self.extract_text_System, "insert": self.insert_text_System}
        ]

    NON_TRANSLATABLE_FILES = {
        'Scripts.rvdata2', 'Animations.rvdata2', 'Tilesets.rvdata2',
        'Scripts.rvdata', 'Animations.rvdata', 'Tilesets.rvdata',
        'Scripts.rxdata', 'Animations.rxdata', 'Tilesets.rxdata',
    }

    def is_translatable_file(self, file_name):
        import os
        base_name = os.path.basename(file_name)
        if base_name in self.NON_TRANSLATABLE_FILES:
            return False
        return any(re.search(p, base_name) for item in self.extract_map for p in item['file_patterns'])

    def extract_text(self, file_name, new_json):
        if not self.is_translatable_file(file_name):
            return None
        for item in self.extract_map:
            if any(re.search(p, file_name) for p in item['file_patterns']):
                return item['extractor'](new_json)
        return None

    def update_json(self, file_name, new_json, new_data):
        for item in self.extract_map:
            if any(re.search(p, file_name) for p in item['file_patterns']):
                item['insert'](new_json, new_data)
                return new_json
        return new_json

    @classmethod
    def _infer_file_name(cls, data):
        if hasattr(data, '_file_name') and getattr(data, '_file_name'):
            return getattr(data, '_file_name')
        if isinstance(data, dict):
            if 'events' in data:
                return 'Map001.json'
            if any(k in data for k in ('gameTitle', 'terms', 'armorTypes', 'equipTypes')):
                return 'System.json'
        elif isinstance(data, list):
            for item in data:
                if isinstance(item, dict):
                    if 'pages' in item:
                        return 'Troops.json'
                    if 'list' in item:
                        return 'CommonEvents.json'
                    return 'Actors.json'
        return 'Map001.json'

    @staticmethod
    def _kind_for_code(raw_code):
        code = RPGEventCode.from_code(raw_code)
        if code in (RPGEventCode.SHOW_CHOICES, RPGEventCode.CHOICE_BRANCH):
            return 'choice'
        if code in (
            RPGEventCode.SCRIPT,
            RPGEventCode.SCRIPT_MORE,
            RPGEventCode.CONTROL_VARIABLES,
            RPGEventCode.PLUGIN_COMMAND_MV,
            RPGEventCode.PLUGIN_COMMAND_MZ,
            RPGEventCode.SET_MOVEMENT_ROUTE,
            RPGEventCode.ROUTE_STEP,
        ):
            return 'script'
        return 'dialogue'

    @_ClassOrInstanceMethod
    def _walk_or_apply_segments(target, file_name, data, mode='extract', translations=None):
        import copy
        import os
        from src.extractor.Segment import Segment

        cls = target if isinstance(target, type) else target.__class__
        inst = target(None) if isinstance(target, type) else target
        base_name = os.path.basename(file_name) if file_name else cls._infer_file_name(data)
        if not inst.is_translatable_file(base_name):
            return [] if mode == 'extract' else data

        segments = []
        translations = translations or {}

        def _lookup_cmd_in_owner_page(collection, owner_id, page_id, cmd_id):
            raw_owner = cls._resolve_by_id(collection, owner_id)
            if not isinstance(raw_owner, dict):
                return {}
            raw_page = cls._resolve_by_id(raw_owner.get('pages', []), page_id)
            if not isinstance(raw_page, dict):
                return {}
            cmd_list = raw_page.get('list', [])
            if isinstance(cmd_list, list) and isinstance(cmd_id, int) and 0 <= cmd_id < len(cmd_list):
                return cmd_list[cmd_id]
            return {}

        def _lookup_cmd_in_owner(collection, owner_id, cmd_id):
            raw_owner = cls._resolve_by_id(collection, owner_id)
            if not isinstance(raw_owner, dict):
                return {}
            cmd_list = raw_owner.get('list', [])
            if isinstance(cmd_list, list) and isinstance(cmd_id, int) and 0 <= cmd_id < len(cmd_list):
                return cmd_list[cmd_id]
            return {}

        def _handle_cmd_List(cmd_items, prefix_base, cmd_lookup_fn):
            modified_items = []
            for list_item in cmd_items:
                cmd_id = list_item['id']
                val = list_item['text']
                raw_cmd = cmd_lookup_fn(cmd_id)
                raw_code = raw_cmd.get('code') if isinstance(raw_cmd, dict) else None
                kind = cls._kind_for_code(raw_code)
                cmd_prefix = f"{base_name}#{prefix_base}/cmd:{cmd_id}"
                changed = False

                if isinstance(val, str):
                    seg_id = cmd_prefix
                    if mode == 'extract':
                        segments.append(Segment(
                            id=seg_id,
                            source=val,
                            context={'file': base_name, 'code': raw_code, 'kind': kind},
                        ))
                    else:
                        new_val = translations.get(seg_id, val)
                        if new_val != val:
                            changed = True
                        list_item['text'] = new_val
                elif isinstance(val, list):
                    new_list = []
                    for sub_idx, sub_val in enumerate(val):
                        seg_id = f"{cmd_prefix}/{sub_idx}"
                        if isinstance(sub_val, str):
                            if mode == 'extract':
                                segments.append(Segment(
                                    id=seg_id,
                                    source=sub_val,
                                    context={'file': base_name, 'code': raw_code, 'index': sub_idx, 'kind': kind},
                                ))
                            else:
                                new_sub = translations.get(seg_id, sub_val)
                                if new_sub != sub_val:
                                    changed = True
                                new_list.append(new_sub)
                        else:
                            new_list.append(sub_val)
                    if mode == 'apply':
                        list_item['text'] = new_list
                elif isinstance(val, dict):
                    new_dict = {}
                    for k, sub_val in val.items():
                        seg_id = f"{cmd_prefix}/{k}"
                        if isinstance(sub_val, str):
                            if mode == 'extract':
                                segments.append(Segment(
                                    id=seg_id,
                                    source=sub_val,
                                    context={'file': base_name, 'code': raw_code, 'key': k, 'kind': kind},
                                ))
                            else:
                                new_sub = translations.get(seg_id, sub_val)
                                if new_sub != sub_val:
                                    changed = True
                                new_dict[k] = new_sub
                        else:
                            new_dict[k] = sub_val
                    if mode == 'apply':
                        list_item['text'] = new_dict

                if mode == 'apply' and changed:
                    modified_items.append(list_item)
            return modified_items

        if re.search(r'Map\d{3,}', base_name):
            extracted = inst.extract_text_map(copy.deepcopy(data))
            events_col = data.get('events') if isinstance(data, dict) else []
            if mode == 'extract':
                for ev in extracted:
                    ev_id = ev['id']
                    for page in ev['pages']:
                        p_id = page['id']
                        _handle_cmd_List(
                            page['list'],
                            f"event:{ev_id}/page:{p_id}",
                            lambda cid, eid=ev_id, pid=p_id: _lookup_cmd_in_owner_page(events_col, eid, pid, cid),
                        )
                return segments
            else:
                filtered_events = []
                for ev in extracted:
                    ev_id = ev['id']
                    filtered_pages = []
                    for page in ev['pages']:
                        p_id = page['id']
                        mod_list = _handle_cmd_List(
                            page['list'],
                            f"event:{ev_id}/page:{p_id}",
                            lambda cid, eid=ev_id, pid=p_id: _lookup_cmd_in_owner_page(events_col, eid, pid, cid),
                        )
                        if mod_list:
                            filtered_pages.append({'id': p_id, 'list': mod_list})
                    if filtered_pages:
                        filtered_events.append({'id': ev_id, 'pages': filtered_pages})
                if filtered_events:
                    inst.insert_text_map(data, filtered_events)
                return data

        if re.search(r'CommonEvents', base_name):
            extracted = inst.extract_text_common_events(copy.deepcopy(data))
            if mode == 'extract':
                for ev in extracted:
                    ev_id = ev['id']
                    _handle_cmd_List(
                        ev['list'],
                        f"common_event:{ev_id}",
                        lambda cid, eid=ev_id: _lookup_cmd_in_owner(data, eid, cid),
                    )
                return segments
            else:
                filtered_events = []
                for ev in extracted:
                    ev_id = ev['id']
                    mod_list = _handle_cmd_List(
                        ev['list'],
                        f"common_event:{ev_id}",
                        lambda cid, eid=ev_id: _lookup_cmd_in_owner(data, eid, cid),
                    )
                    if mod_list:
                        filtered_events.append({'id': ev_id, 'list': mod_list})
                if filtered_events:
                    inst.insert_text_common_events(data, filtered_events)
                return data

        if re.search(r'Troops', base_name):
            extracted = inst.extract_text_troops(copy.deepcopy(data))
            if mode == 'extract':
                for tr in extracted:
                    tr_id = tr['id']
                    for page in tr['pages']:
                        p_id = page['id']
                        _handle_cmd_List(
                            page['list'],
                            f"troop:{tr_id}/page:{p_id}",
                            lambda cid, tid=tr_id, pid=p_id: _lookup_cmd_in_owner_page(data, tid, pid, cid),
                        )
                return segments
            else:
                filtered_troops = []
                for tr in extracted:
                    tr_id = tr['id']
                    filtered_pages = []
                    for page in tr['pages']:
                        p_id = page['id']
                        mod_list = _handle_cmd_List(
                            page['list'],
                            f"troop:{tr_id}/page:{p_id}",
                            lambda cid, tid=tr_id, pid=p_id: _lookup_cmd_in_owner_page(data, tid, pid, cid),
                        )
                        if mod_list:
                            filtered_pages.append({'id': p_id, 'list': mod_list})
                    if filtered_pages:
                        filtered_troops.append({'id': tr_id, 'pages': filtered_pages})
                if filtered_troops:
                    inst.insert_text_troops(data, filtered_troops)
                return data

        if re.search(r'System', base_name):
            extracted = inst.extract_text_System(data)
            if mode == 'extract':
                for key, value in extracted.items():
                    if isinstance(value, str) and value and not RPGTextFilters.is_technical_or_code(value):
                        segments.append(Segment(
                            id=f"{base_name}#system/{key}",
                            source=value,
                            context={'file': base_name, 'field': key, 'kind': 'menu'},
                        ))
                    elif isinstance(value, list):
                        for i, item in enumerate(value):
                            if isinstance(item, str) and item and not RPGTextFilters.is_technical_or_code(item):
                                segments.append(Segment(
                                    id=f"{base_name}#system/{key}/{i}",
                                    source=item,
                                    context={'file': base_name, 'field': key, 'index': i, 'kind': 'menu'},
                                ))
                    elif isinstance(value, dict):
                        for sub_k, sub_v in value.items():
                            if isinstance(sub_v, list):
                                for i, item in enumerate(sub_v):
                                    if isinstance(item, str) and item and not RPGTextFilters.is_technical_or_code(item):
                                        segments.append(Segment(
                                            id=f"{base_name}#system/{key}/{sub_k}/{i}",
                                            source=item,
                                            context={'file': base_name, 'field': f"{key}.{sub_k}", 'index': i, 'kind': 'menu'},
                                        ))
                            elif isinstance(sub_v, dict):
                                for msg_k, msg_v in sub_v.items():
                                    if isinstance(msg_v, str) and msg_v and not RPGTextFilters.is_technical_or_code(msg_v):
                                        segments.append(Segment(
                                            id=f"{base_name}#system/{key}/{sub_k}/{msg_k}",
                                            source=msg_v,
                                            context={'file': base_name, 'field': f"{key}.{sub_k}.{msg_k}", 'kind': 'menu'},
                                        ))
                            elif isinstance(sub_v, str) and sub_v and not RPGTextFilters.is_technical_or_code(sub_v):
                                segments.append(Segment(
                                    id=f"{base_name}#system/{key}/{sub_k}",
                                    source=sub_v,
                                    context={'file': base_name, 'field': f"{key}.{sub_k}", 'kind': 'menu'},
                                ))
                return segments
            else:
                changed_any = False
                for key, value in extracted.items():
                    if isinstance(value, str) and value and not RPGTextFilters.is_technical_or_code(value):
                        seg_id = f"{base_name}#system/{key}"
                        new_v = translations.get(seg_id, value)
                        if new_v != value:
                            extracted[key] = new_v
                            changed_any = True
                    elif isinstance(value, list):
                        for i, item in enumerate(value):
                            if isinstance(item, str) and item and not RPGTextFilters.is_technical_or_code(item):
                                seg_id = f"{base_name}#system/{key}/{i}"
                                new_v = translations.get(seg_id, item)
                                if new_v != item:
                                    value[i] = new_v
                                    changed_any = True
                    elif isinstance(value, dict):
                        for sub_k, sub_v in value.items():
                            if isinstance(sub_v, list):
                                for i, item in enumerate(sub_v):
                                    if isinstance(item, str) and item and not RPGTextFilters.is_technical_or_code(item):
                                        seg_id = f"{base_name}#system/{key}/{sub_k}/{i}"
                                        new_v = translations.get(seg_id, item)
                                        if new_v != item:
                                            sub_v[i] = new_v
                                            changed_any = True
                            elif isinstance(sub_v, dict):
                                for msg_k, msg_v in sub_v.items():
                                    if isinstance(msg_v, str) and msg_v and not RPGTextFilters.is_technical_or_code(msg_v):
                                        seg_id = f"{base_name}#system/{key}/{sub_k}/{msg_k}"
                                        new_v = translations.get(seg_id, msg_v)
                                        if new_v != msg_v:
                                            sub_v[msg_k] = new_v
                                            changed_any = True
                            elif isinstance(sub_v, str) and sub_v and not RPGTextFilters.is_technical_or_code(sub_v):
                                seg_id = f"{base_name}#system/{key}/{sub_k}"
                                new_v = translations.get(seg_id, sub_v)
                                if new_v != sub_v:
                                     value[sub_k] = new_v
                                     changed_any = True
                if changed_any:
                    inst.insert_text_System(data, extracted)
                return data

        # Database object files (Actors, Items, Weapons, Armors, Skills, States, Enemies, Classes, MapInfos)
        extracted = inst.extract_text_object(copy.deepcopy(data))
        if mode == 'extract':
            for obj in extracted:
                item_id = obj.get('id')
                for k, v in obj.items():
                    if k == 'id':
                        continue
                    if k == 'note' and isinstance(v, dict):
                        for tag, content in v.items():
                            segments.append(Segment(
                                id=f"{base_name}#item:{item_id}/note:{tag}",
                                source=content,
                                context={'file': base_name, 'item_id': item_id, 'note_tag': tag, 'kind': 'db'},
                            ))
                    elif isinstance(v, str):
                        segments.append(Segment(
                            id=f"{base_name}#item:{item_id}/field:{k}",
                            source=v,
                            context={'file': base_name, 'item_id': item_id, 'field': k, 'kind': 'db'},
                        ))
            return segments
        else:
            modified_objs = []
            for obj in extracted:
                item_id = obj.get('id')
                mod_obj = {'id': item_id}
                for k, v in obj.items():
                    if k == 'id':
                        continue
                    if k == 'note' and isinstance(v, dict):
                        mod_notes = {}
                        for tag, content in v.items():
                            seg_id = f"{base_name}#item:{item_id}/note:{tag}"
                            new_c = translations.get(seg_id, content)
                            if new_c != content:
                                mod_notes[tag] = new_c
                        if mod_notes:
                            mod_obj['note'] = mod_notes
                    elif isinstance(v, str):
                        seg_id = f"{base_name}#item:{item_id}/field:{k}"
                        new_v = translations.get(seg_id, v)
                        if new_v != v:
                            mod_obj[k] = new_v
                if len(mod_obj) > 1:
                    modified_objs.append(mod_obj)
            if modified_objs:
                inst.insert_text_object(data, modified_objs)
            return data

    @staticmethod
    def fix_text_translate(texts, original_texts=None):
        texts_list = TextsUtils.dictToList(texts)
        original_list = TextsUtils.dictToList(original_texts) if original_texts else None

        _format_codes_pattern = re.compile(r'(?<!\\)\\([A-Za-z]+)(?:\s*(\[[^\]]*\]))?', re.IGNORECASE)
        _format_upper = {'c', 'v', 'i', 'ce', 'm', 'n', 'p', 'g'}

        def _fix_format_code(m):
            code = m.group(1)
            code_lower = code.lower()
            arg = m.group(2) or ""
            if not arg and code_lower != 'g':
                return m.group(0)
            if code_lower in ("name", "nome"):
                return f"\\NAME{arg}"
            if code_lower in _format_upper:
                return f"\\{code.upper()}{arg}"
            return f"\\{code.lower()}{arg}"

        _compiled_patterns = [
            (re.compile(r'\\\s+'), r'\\'), (re.compile(r'(?i)\bif\s*\('), 'if('),
            (re.compile(r'"\s+"'), '""'), (re.compile(r'\\n\s+'), r'\\n'),
            (re.compile(r'\s*_\s*'), '_'), (re.compile(r'<\s*>'), '<>'),
            (re.compile(r'\$\s+'), '$'), (re.compile(r'>\s*='), '>='),
            (re.compile(r'<\s*='), '<='), (re.compile(r'!\s*='), '!='),
            (re.compile(r'=\s*='), '=='),
        ]

        boolean_corrections = {
            r'\b(?:verdadeiro|verdadeira|verdadero|verdadera|vrai|vraie)\b': 'true',
            r'\b(?:falso|falsa|faux|fausse)\b': 'false',
            r'\b(?:nulo|nula|nul)\b': 'null',
            r'\b(?:indefinido|indéfini)\b': 'undefined',
        }

        _game_objects = {k: k.capitalize() for k in ['variables', 'switches', 'party', 'actors', 'player', 'map', 'system', 'screen', 'timer', 'message', 'temp', 'troop', 'interpreter']}
        _game_pattern = re.compile(r'\$game(' + '|'.join(_game_objects.keys()) + r')\b', re.IGNORECASE)
        _property_pattern = re.compile(r'\.([_a-zA-Z][_a-zA-Z0-9]*)(?=[\[\(\.]|\s|$)')
        _code_pattern = re.compile(r'(\$game\s*[a-zA-Z]+(?:\s*\.\s*[_a-zA-Z][_a-zA-Z0-9]*(?:\s*\[\s*[^\\\]]+\s*\])?)*|\$\s*[a-z]+[A-Z][a-zA-Z]*|!(?<!\\)\b[A-Za-z]{1,2}\s*\[\s*\d+\s*\]|(?<!\\)(?<!!)\b[A-Za-z]{1,2}\s*\[\s*\d+\s*\])', re.IGNORECASE)

        for i, text in enumerate(texts_list):
            if not isinstance(text, str):
                continue
            original_text = original_list[i] if original_list and i < len(original_list) else None
            ref_text = original_text if original_text is not None else text
            is_code = RPGTextFilters.is_technical_or_code(ref_text)

            text = _format_codes_pattern.sub(_fix_format_code, text)

            if is_code:
                # Sanitizar aspas tipográficas (smart quotes) para aspas ASCII padrão apenas em código (#37)
                text = text.replace('“', '"').replace('”', '"').replace('‘', "'").replace('’', "'")
                for pattern, repl in _compiled_patterns:
                    text = pattern.sub(repl, text)

                for pattern, replacement in boolean_corrections.items():
                    text = re.sub(pattern, replacement, text, flags=re.IGNORECASE)

                if original_text:
                    original_codes = {re.sub(r'\s+', '', m.group(0)).lower(): m.group(0) for m in _code_pattern.finditer(original_text)}
                    def restore_code(match):
                        code = match.group(0)
                        key = re.sub(r'\s+', '', code).lower()
                        return original_codes.get(key, re.sub(r'\s+', '', code))
                    text = _code_pattern.sub(restore_code, text)

                text = _game_pattern.sub(lambda m: f'$game{_game_objects.get(m.group(1).lower(), m.group(1))}', text)
                if not original_text:
                    text = _property_pattern.sub(lambda m: '.' + m.group(1).lower() if m.group(1).isupper() or (len(m.group(1)) > 1 and m.group(1)[0].isupper() and '_' in m.group(1)) else m.group(0), text)

                try:
                    parts = re.split(r'(\'.*?\'|".*?")', text)
                    for j in range(0, len(parts), 2):
                        parts[j] = re.sub(r'\b0+([1-9]\d*)\b', r'\1', parts[j])
                        parts[j] = re.sub(r'\b00+\b', '0', parts[j])
                    text = ''.join(parts)
                except Exception as e:
                    logging.warning(f'Erro ao sanitizar literais octais: {e}')

            texts_list[i] = text

        TextsUtils.interactive_item(texts, texts_list)
        return texts
