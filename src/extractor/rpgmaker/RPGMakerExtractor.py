import re
import logging
from src.extractor.BaseExtractor import BaseExtractor
import src.utils.TextsUtils as TextsUtils
from .RPGEventCodes import RPGEventCode
from .RPGTextFilters import RPGTextFilters
from .RPGEventStrategy import (
    ShowTextStrategy, ChoiceStrategy, ChoiceBranchStrategy, ScriptStrategy,
    PluginStrategyMZ, PluginStrategyMV
)
from src.factory import register_extractor

@register_extractor("RPG Maker")
class RPGMakerExtractor(BaseExtractor):
    files_types = ['json', 'rvdata2']

    def __init__(self, translate):
        super().__init__(translate)
        self._strategies = {
            RPGEventCode.SHOW_TEXT: ShowTextStrategy(),
            RPGEventCode.SHOW_TEXT_MORE: ShowTextStrategy(),
            RPGEventCode.SCROLL_TEXT: ShowTextStrategy(),
            RPGEventCode.COMMENT: ShowTextStrategy(),
            RPGEventCode.SHOW_CHOICES: ChoiceStrategy(),
            RPGEventCode.SCRIPT: ScriptStrategy(),
            RPGEventCode.SCRIPT_MORE: ScriptStrategy(),
            RPGEventCode.SHOW_NAME: ShowTextStrategy(),
            RPGEventCode.CHANGE_NAME: ShowTextStrategy(),
            # Mesmo formato de parâmetros que CHANGE_NAME ([actorId, texto]): apelido e perfil
            # do ator são texto visível ao jogador, não dado interno.
            RPGEventCode.CHANGE_NICKNAME: ShowTextStrategy(),
            RPGEventCode.CHANGE_PROFILE: ShowTextStrategy(),
            RPGEventCode.CONTROL_VARIABLES: ShowTextStrategy(),
            RPGEventCode.PLUGIN_COMMAND_MV: PluginStrategyMV(),
            RPGEventCode.PLUGIN_COMMAND_MZ: PluginStrategyMZ(),
            RPGEventCode.CHOICE_BRANCH: ChoiceBranchStrategy(),
            # LABEL (118) fica de propósito sem strategy: é um identificador interno de
            # salto (ex: "loop_start", "end", "nochannel"), não texto de jogador.
        }

    def apply_configuration(self, config):
        pass

    def get_mask_patterns(self, file_name=None, data=None):
        p_format_codes = re.compile(r'\\[A-Za-z]{1,3}\s*\[[^\]]*\]', re.IGNORECASE)
        game_keys = ['variables', 'switches', 'party', 'actors', 'player', 'map', 'system', 'screen', 'timer', 'message', 'temp', 'troop', 'interpreter']
        p_game = re.compile(r'\$game(' + '|'.join(game_keys) + r')\b', re.IGNORECASE)
        p_bracket_vars = re.compile(r'!?(?<!\\)\b[A-Za-z]{1,2}\s*\[\s*\d+\s*\]', re.IGNORECASE)
        p_if_condition = re.compile(r'if\s*\(\s*!?\s*[A-Za-z]{1,2}\s*\[\s*\d+\s*\]\s*\)', re.IGNORECASE)
        p_lang_wrapper = re.compile(r'\b(?:en|pt|ja|es|fr)\s*\([^)]+\)', re.IGNORECASE)
        p_boolean_literals = re.compile(r'\b(?:true|false|null|undefined|NaN)\b', re.IGNORECASE)
        p_operators = re.compile(r'(?:&&|\|\||>=|<=|!==|===|!=|==)')
        p_dollar_camel = re.compile(r'\$\s*[a-z]+[A-Z][a-zA-Z]*')
        p_html_tags = re.compile(r'</?[a-zA-Z][a-zA-Z0-9]*(?:\s+[^>]*)?/?>', re.IGNORECASE)
        p_technical_values = re.compile(r'^(?:Yes|No|OK|Cancel|end|start|stop|play|pause|nochannel|channel)$', re.IGNORECASE)

        return [p_format_codes, p_game, p_bracket_vars, p_if_condition, p_lang_wrapper, p_boolean_literals, p_operators, p_dollar_camel, p_html_tags, p_technical_values]

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
        if file_type == 'Map':
            for event in data.get('events', []):
                if not event: continue
                for page_idx, page in enumerate(event.get('pages', [])):
                    for cmd_idx, cmd in enumerate(page.get('list', [])):
                        yield (event['id'], page_idx, cmd_idx, cmd)
        elif file_type == 'CommonEvents':
            for event in data:
                if not event: continue
                for cmd_idx, cmd in enumerate(event.get('list', [])):
                    yield (event['id'], None, cmd_idx, cmd)
        elif file_type == 'Troops':
            for troop in data:
                if not troop: continue
                for page_idx, page in enumerate(troop.get('pages', [])):
                    for cmd_idx, cmd in enumerate(page.get('list', [])):
                        yield (troop['id'], page_idx, cmd_idx, cmd)

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

    def insert_text_map(self, data, translated_data):
        for event_data in translated_data:
            event_id = event_data['id']
            for page_data in event_data['pages']:
                page_id = page_data['id']
                for list_item in page_data['list']:
                    cmd_id = list_item['id']
                    cmd = data['events'][event_id]['pages'][page_id]['list'][cmd_id]
                    self.insert_text_map_item(cmd, list_item)

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
        for event_data in translated_data:
            event_id = event_data['id']
            for list_item in event_data['list']:
                cmd_id = list_item['id']
                cmd = data[event_id]['list'][cmd_id]
                self.insert_text_map_item(cmd, list_item)

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
        for troop_data in translated_data:
            troop_id = troop_data['id']
            for page_data in troop_data['pages']:
                page_id = page_data['id']
                for list_item in page_data['list']:
                    cmd_id = list_item['id']
                    cmd = data[troop_id]['pages'][page_id]['list'][cmd_id]
                    self.insert_text_map_item(cmd, list_item)

    _attributes_find = [r'name', r'note', r'profile', r'description', r'nickname', r'message\d{1}']
    _attributes_system_find = ['armorTypes', 'equipTypes', 'gameTitle', 'currencyUnit', 'skillTypes', 'terms', 'switches', 'elements', 'weaponTypes']
    # Tags de nota do tipo <Desc: ...>, <Help: ...>, <InfoAlgumaCoisa: ...>. O grupo 1
    # precisa capturar só o nome da tag (sem o "<"), pois insert_text_object o reusa
    # para reconstruir a tag original.
    _note_tag_pattern = re.compile(r'<(Desc|Help|Text|Profile|Message|Info[^:>]*):\s*([^>]+)>', re.IGNORECASE)

    def extract_text_object(self, data):
        text = []
        for item in data:
            if item and item.get('name', '') != '':
                obj = {'id': item['id']}
                for key, val in item.items():
                    if any(re.match(pattern, key) for pattern in self._attributes_find):
                        if key == 'note' and val:
                            matches = self._note_tag_pattern.findall(val)
                            if matches:
                                note_data = {tag: content for tag, content in matches}
                                if note_data: obj[key] = note_data
                        elif not RPGTextFilters.is_technical_or_code(val):
                            obj[key] = val
                if len(obj) > 1:
                    text.append(obj)
        return text

    def insert_text_object(self, data, translated_data):
        for texts in translated_data:
            item_id = texts['id']
            if 'note' in texts and isinstance(texts['note'], dict):
                original_note = data[item_id].get('note', '')
                for tag, new_content in texts['note'].items():
                    pattern = re.compile(fr'(<{re.escape(tag)}\s*:\s*)([^>]+)(>)', re.IGNORECASE)
                    original_note = pattern.sub(lambda m: f"{m.group(1)}{new_content}{m.group(3)}", original_note)
                data[item_id]['note'] = original_note
                texts['note'] = original_note
            data[item_id].update(texts)

    def extract_text_System(self, data):
        text = {}
        for key, value in data.items():
            if key in self._attributes_system_find:
                text[key] = value
        return text

    def insert_text_System(self, data, translated_data):
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

    def extract_text(self, file_name, new_json):
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

    @staticmethod
    def fix_text_translate(texts, original_texts=None):
        texts_list = TextsUtils.dictToList(texts)
        original_list = TextsUtils.dictToList(original_texts) if original_texts else None

        _format_codes_pattern = re.compile(r'\\([A-Za-z]{1,3})(?:\s*(\[[^\]]*\]))?', re.IGNORECASE)
        _format_upper = {'c', 'v', 'i', 'ce', 'm', 'n', 'p', 'g'}

        _compiled_patterns = [
            (re.compile(r'\\\s+'), r'\\'), (re.compile(r'(?i)\bif\s*\('), 'if('),
            (re.compile(r'"\s+"'), '""'), (re.compile(r'\\n\s+'), r'\\n'),
            (re.compile(r'\s*_\s*'), '_'), (re.compile(r'<\s*>'), '<>'),
            (re.compile(r'\$\s+'), '$'), (re.compile(r'>\s*='), '>='),
            (re.compile(r'<\s*='), '<='), (re.compile(r'!\s*='), '!='),
            (re.compile(r'=\s*='), '=='),
        ]

        _game_objects = {k: k.capitalize() for k in ['variables', 'switches', 'party', 'actors', 'player', 'map', 'system', 'screen', 'timer', 'message', 'temp', 'troop', 'interpreter']}
        _game_pattern = re.compile(r'\$game(' + '|'.join(_game_objects.keys()) + r')\b', re.IGNORECASE)
        _property_pattern = re.compile(r'\.([_a-zA-Z][_a-zA-Z0-9]*)(?=[\[\(\.]|\s|$)')
        _code_pattern = re.compile(r'(\$game\s*[a-zA-Z]+(?:\s*\.\s*[_a-zA-Z][_a-zA-Z0-9]*(?:\s*\[\s*[^\\\]]+\s*\])?)*|\$\s*[a-z]+[A-Z][a-zA-Z]*|!(?<!\\)\b[A-Za-z]{1,2}\s*\[\s*\d+\s*\]|(?<!\\)(?<!!)\b[A-Za-z]{1,2}\s*\[\s*\d+\s*\])', re.IGNORECASE)

        for i, text in enumerate(texts_list):
            if not isinstance(text, str): continue
            original_text = original_list[i] if original_list and i < len(original_list) else None

            text = _format_codes_pattern.sub(lambda m: f'\\{m.group(1).upper() if m.group(1).lower() in _format_upper else m.group(1).lower()}{m.group(2) or ""}', text)
            for pattern, repl in _compiled_patterns: text = pattern.sub(repl, text)

            boolean_corrections = {
                r'\b(?:verdadeiro|verdadeira|verdadero|verdadera|vrai|vraie)\b': 'true',
                r'\b(?:falso|falsa|faux|fausse)\b': 'false',
                r'\b(?:nulo|nula|nul)\b': 'null',
                r'\b(?:indefinido|indéfini)\b': 'undefined',
            }
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

            texts_list[i] = text
            if any(t in text for t in ['if(', 'var ', 'return', '=>', 'function', 'mes =', 'text =']):
                try:
                    parts = re.split(r'(\'.*?\'|".*?")', text)
                    for j in range(0, len(parts), 2):
                        parts[j] = re.sub(r'\b0+([1-9]\d*)\b', r'\1', parts[j])
                        parts[j] = re.sub(r'\b00+\b', '0', parts[j])
                    texts_list[i] = ''.join(parts)
                except Exception as e: logging.warning(f'Erro ao sanitizar literais octais: {e}')

        TextsUtils.interactive_item(texts, texts_list)
        return texts
