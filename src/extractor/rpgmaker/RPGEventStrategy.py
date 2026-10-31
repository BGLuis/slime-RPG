import re
from src.extractor.EventCommandStrategy import EventCommandStrategy
from .RPGEventCodes import RPGEventCode
from .RPGTextFilters import RPGTextFilters

EventStrategy = EventCommandStrategy

class ShowTextStrategy(EventStrategy):
    kind = "dialogue"
    _text_keys = ["icon:", "secret:", "title:", "name:", "text:", "message:", "text1:", "text2:", "secretText:", "id:"]
    _title_pattern = re.compile(r'^(?:<<([^<>]*)>>|<([^<>]*)>)(\s*)')

    def extract(self, item, context=None):
        params = item.get('parameters', [])
        code = item.get('code')
        extracted = []
        for i, param in enumerate(params):
            if code == 101 and i == 0:
                continue
            if not RPGTextFilters.is_technical_or_code(param):
                title_match = self._title_pattern.match(param)
                if title_match:
                    title = title_match.group(1) or title_match.group(2)
                    text = param[title_match.end():].strip()
                    title_is_code = RPGTextFilters.is_technical_or_code(title)
                    if text:
                        if not title_is_code and not RPGTextFilters.is_technical_or_code(text):
                            extracted.append([title, text])
                        elif not RPGTextFilters.is_technical_or_code(text):
                            extracted.append([text])
                    else:
                        if not title_is_code:
                            extracted.append([title])
                elif any(param.startswith(key) for key in self._text_keys):
                    remaining_text = param.split(":", 1)[-1].strip()
                    if not RPGTextFilters.is_technical_or_code(remaining_text) and \
                       not RPGTextFilters.is_numeric(remaining_text) and \
                       not RPGTextFilters.is_boolean(remaining_text):
                        extracted.append(remaining_text)
                else:
                    extracted.append(param)
        
        if not extracted: return None
        return extracted[0] if len(extracted) == 1 else extracted

    def insert(self, item, text, context=None):
        if not text: return
        # Se 'text' for lista, pode ser [título, texto] ou vários parâmetros
        params = item.get('parameters', [])
        code = item.get('code')
        if len(params) == 1:
            iten_text = params[0]
            match = self._title_pattern.match(iten_text)
            if match:
                space = match.group(3)
                full_tag = iten_text[:match.end() - len(space)]
                orig_title = match.group(1) or match.group(2)
                orig_body = iten_text[match.end():].strip()

                if isinstance(text, list) and len(text) == 2:
                    new_title = str(text[0]).strip()
                    new_body = str(text[1]).strip()
                    new_tag = full_tag.replace(orig_title, new_title, 1)
                    item['parameters'][0] = f"{new_tag}{space}{new_body}"
                elif isinstance(text, list) and len(text) == 1:
                    new_val = str(text[0]).strip()
                    if orig_body:
                        item['parameters'][0] = f"{full_tag}{space}{new_val}"
                    else:
                        new_tag = full_tag.replace(orig_title, new_val, 1)
                        item['parameters'][0] = f"{new_tag}{space}"
                elif not isinstance(text, list):
                    new_val = str(text).strip()
                    if orig_body:
                        item['parameters'][0] = f"{full_tag}{space}{new_val}"
                    else:
                        new_tag = full_tag.replace(orig_title, new_val, 1)
                        item['parameters'][0] = f"{new_tag}{space}"
            elif any(iten_text.startswith(key) for key in self._text_keys):
                iten_text_parts = iten_text.split(":", 1)
                if len(iten_text_parts) > 1:
                    orig_rem = iten_text_parts[1].strip()
                    if isinstance(text, (list, tuple)):
                        new_val = text[1] if len(text) == 2 else (text[0] if len(text) == 1 else text)
                    else:
                        new_val = text
                    if str(new_val) == orig_rem:
                        return
                    item['parameters'][0] = iten_text_parts[0] + ": " + str(new_val)
            else:
                if isinstance(text, (list, tuple)):
                    new_val = text[1] if len(text) == 2 else (text[0] if len(text) == 1 else text)
                else:
                    new_val = text
                item['parameters'][0] = str(new_val)
        else:
            # Para comandos multi-parâmetros (122, 101, etc.)
            for i in range(len(params)):
                if code == 101 and i == 0:
                    continue
                if not RPGTextFilters.is_technical_or_code(params[i]):
                    item['parameters'][i] = str(text)
                    break

class ChoiceStrategy(EventStrategy):
    kind = "choice"

    def extract(self, item, context=None):
        if not item.get('parameters'): return None
        choices = item['parameters'][0]
        if not choices: return None
        
        has_translatable = False
        extracted = []
        for choice in choices:
            choice_str = choice.text if hasattr(choice, 'text') else str(choice)
            prefix, choice_text = RPGTextFilters.parse_choice_condition(choice_str)
            extracted.append(choice_text)
            if not RPGTextFilters.is_technical_or_code(choice_text):
                has_translatable = True
        
        return extracted if has_translatable else None

    def insert(self, item, text, context=None):
        if not text or not isinstance(text, list): return
        original_list = item['parameters'][0]
        if len(original_list) != len(text): return
        
        final_list = []
        for orig, trans in zip(original_list, text):
            orig_str = orig.text if hasattr(orig, 'text') else str(orig)
            prefix, choice_text = RPGTextFilters.parse_choice_condition(orig_str)
            if RPGTextFilters.is_technical_or_code(choice_text):
                final_list.append(orig)
            elif str(trans).strip() == choice_text:
                final_list.append(orig)
            else:
                final_list.append(prefix + str(trans).strip())
        item['parameters'][0] = final_list

class ChoiceBranchStrategy(EventStrategy):
    kind = "choice"

    def extract(self, item, context=None):
        params = item.get('parameters', [])
        if len(params) > 1:
            raw_text = params[1]
            raw_str = raw_text.text if hasattr(raw_text, 'text') else str(raw_text)
            prefix, choice_text = RPGTextFilters.parse_choice_condition(raw_str)
            if not RPGTextFilters.is_technical_or_code(choice_text):
                return choice_text
        return None

    def insert(self, item, text, context=None):
        if not text: return
        params = item.get('parameters', [])
        if len(params) > 1:
            raw_text = params[1]
            raw_str = raw_text.text if hasattr(raw_text, 'text') else str(raw_text)
            prefix, choice_text = RPGTextFilters.parse_choice_condition(raw_str)
            if not RPGTextFilters.is_technical_or_code(choice_text):
                item['parameters'][1] = prefix + str(text).strip()

class ScriptStrategy(EventStrategy):
    kind = "script"
    _quote_pattern = re.compile(r"'.*?'|\".*?\"")
    _subject_pattern = re.compile(r'\d_t=\d{2,}_subject=')
    _om_pattern = re.compile(
        r"\$gameScreen\.OriginalMessage\s*\(\s*(['\"])((?:\\.|(?!\1).)*)\1\s*,\s*(['\"])((?:\\.|(?!\3).)*)\3",
        re.IGNORECASE
    )

    # SAFELIST: Só extrair de funções conhecidas
    SAFE_FUNCTIONS = [
        re.compile(r"(?:\$gameMessage\.add|this\.addCommand|this\.setHelpWindowText|TickerManager\.show)\s*\(\s*(['\"])((?:\\.|(?!\1).)*)\1", re.IGNORECASE),
        re.compile(r"BattleManager\._logWindow\.push\s*\(\s*(?:['\"])addText(?:['\"])\s*,\s*(['\"])((?:\\.|(?!\1).)*)\1\s*\)", re.IGNORECASE),
        re.compile(r"(?:mes|text)\s*=\s*(['\"])((?:\\.|(?!\1).)*)\1", re.IGNORECASE)
    ]

    @classmethod
    def _unescape_quotes(cls, text):
        if not isinstance(text, str):
            return text
        return re.sub(r'(?<!\\)\\(["\'])', r'\1', text)

    @classmethod
    def extract_script_string(cls, script_content):
        if not isinstance(script_content, str):
            return []

        extracted = []
        # 1. $gameScreen.OriginalMessage("nome", "mensagem", tipo, pos)
        for match in cls._om_pattern.finditer(script_content):
            name = cls._unescape_quotes(match.group(2))
            msg = cls._unescape_quotes(match.group(4))
            if not RPGTextFilters.is_technical_or_code(name):
                extracted.append(name)
            if not RPGTextFilters.is_technical_or_code(msg):
                extracted.append(msg)

        # 2. Funções seguras de parâmetro único
        for pattern in cls.SAFE_FUNCTIONS:
            for match in pattern.finditer(script_content):
                text = cls._unescape_quotes(match.group(2))
                if not RPGTextFilters.is_technical_or_code(text):
                    extracted.append(text)

        # 3. Padrão de subject
        if not extracted and cls._subject_pattern.search(script_content):
            extract_text = script_content.split('_subject=')[1].strip('"')
            if not RPGTextFilters.is_technical_or_code(extract_text):
                extracted.append(extract_text)

        return extracted

    _PRE_ESCAPED_FORMAT_CODE = re.compile(r'\\\\([A-Za-z]+\[[^\]]*\])')

    @classmethod
    def _sanitize_script_param(cls, text, quote_char, original_text=None):
        if not isinstance(text, str):
            return text
        if isinstance(original_text, str) and text == original_text:
            return original_text
        import json
        s = text.strip()

        # Remover aspas externas redundantes inseridas pelo tradutor apenas quando o original
        # usava delimitadores de fala japoneses (「...」 ou 『...』) e não tinha aspas próprias (#36).
        if isinstance(original_text, str):
            orig_s = original_text.strip()
            if (orig_s.startswith('「') and orig_s.endswith('」')) or (orig_s.startswith('『') and orig_s.endswith('』')):
                while len(s) >= 2 and (
                    (s[0] == '"' and s[-1] == '"') or
                    (s[0] == "'" and s[-1] == "'") or
                    (s[0] == '“' and s[-1] == '”') or
                    (s[0] == '‘' and s[-1] == '’')
                ):
                    s = s[1:-1].strip()
            if cls._PRE_ESCAPED_FORMAT_CODE.search(original_text):
                s = cls._PRE_ESCAPED_FORMAT_CODE.sub(r'\\\1', s)

        escaped = json.dumps(s, ensure_ascii=False)[1:-1]
        escaped = escaped.replace('\u2028', r'\u2028').replace('\u2029', r'\u2029')
        if quote_char == "'":
            escaped = escaped.replace(r'\"', '"').replace("'", r"\'")
        elif quote_char == '`':
            escaped = escaped.replace(r'\"', '"').replace('`', r'\`')
        return escaped

    @classmethod
    def insert_script_string(cls, script_content, text_iter):
        if not isinstance(script_content, str):
            return script_content

        # 1. $gameScreen.OriginalMessage
        if cls._om_pattern.search(script_content):
            def replacer_om(m):
                q1, name = m.group(1), m.group(2)
                q2, msg = m.group(3), m.group(4)
                unescaped_name = cls._unescape_quotes(name)
                unescaped_msg = cls._unescape_quotes(msg)
                new_name = unescaped_name
                new_msg = unescaped_msg
                if not RPGTextFilters.is_technical_or_code(unescaped_name):
                    try:
                        new_name = str(next(text_iter))
                    except StopIteration:
                        new_name = unescaped_name
                if not RPGTextFilters.is_technical_or_code(unescaped_msg):
                    try:
                        new_msg = str(next(text_iter))
                    except StopIteration:
                        new_msg = unescaped_msg
                if new_name == unescaped_name:
                    new_name = name
                else:
                    new_name = cls._sanitize_script_param(new_name, q1, original_text=unescaped_name)
                if new_msg == unescaped_msg:
                    new_msg = msg
                else:
                    new_msg = cls._sanitize_script_param(new_msg, q2, original_text=unescaped_msg)
                prefix_call = m.group(0)[:m.start(1) - m.start(0)]
                return f"{prefix_call}{q1}{new_name}{q1}, {q2}{new_msg}{q2}"

            script_content = cls._om_pattern.sub(replacer_om, script_content)

        # 2. Funções seguras de parâmetro único
        for pattern in cls.SAFE_FUNCTIONS:
            if pattern.search(script_content):
                def replacer(m):
                    raw_target = m.group(2)
                    target_text = cls._unescape_quotes(raw_target)
                    quote_char = m.group(1)
                    if RPGTextFilters.is_technical_or_code(target_text):
                        return m.group(0)
                    try:
                        next_text = str(next(text_iter))
                    except StopIteration:
                        next_text = target_text
                    if next_text == target_text:
                        next_text = raw_target
                    else:
                        next_text = cls._sanitize_script_param(next_text, quote_char, original_text=target_text)
                    return m.group(0)[:m.start(2) - m.start(0)] + next_text + m.group(0)[m.end(2) - m.start(0):]

                script_content = pattern.sub(replacer, script_content)

        # 3. Padrão de subject
        if cls._subject_pattern.search(script_content):
            parts = script_content.split('_subject=')
            if len(parts) > 1:
                orig_subj = parts[1].strip('"')
                try:
                    next_text = str(next(text_iter))
                except StopIteration:
                    next_text = orig_subj
                next_text = cls._sanitize_script_param(next_text, '"', original_text=orig_subj)
                parts[1] = f'{next_text}"'
                script_content = '_subject='.join(parts)

        return script_content

    def extract(self, item, context=None):
        params = item.get('parameters', [])
        script_content = params[0] if params else None
        extracted = self.extract_script_string(script_content)
        if not extracted:
            return None
        return extracted[0] if len(extracted) == 1 else extracted

    def insert(self, item, text, context=None):
        if not text:
            return
        params = item.get('parameters', [])
        if not params:
            return
        text_iter = iter(text) if isinstance(text, list) else iter([str(text)])
        item['parameters'][0] = self.insert_script_string(params[0], text_iter)


class MovementRouteStrategy(EventStrategy):
    """Estratégia para comando 205 (SET_MOVEMENT_ROUTE).
    parameters = [targetId, routeObject] onde routeObject['list'] contém passos de rota.
    Passo com código 45 é script (ex: TickerManager.show).
    """
    kind = "script"
    def extract(self, item, context=None):
        params = item.get('parameters', [])
        if len(params) < 2 or not isinstance(params[1], dict):
            return None
        route_list = params[1].get('list', [])
        extracted = []
        for step in route_list:
            if isinstance(step, dict) and step.get('code') == 45:
                step_params = step.get('parameters', [])
                if step_params and isinstance(step_params[0], str):
                    extracted.extend(ScriptStrategy.extract_script_string(step_params[0]))
        if not extracted:
            return None
        return extracted[0] if len(extracted) == 1 else extracted

    def insert(self, item, text, context=None):
        if not text:
            return
        params = item.get('parameters', [])
        if len(params) < 2 or not isinstance(params[1], dict):
            return
        route_list = params[1].get('list', [])
        text_iter = iter(text) if isinstance(text, list) else iter([str(text)])
        for step in route_list:
            if isinstance(step, dict) and step.get('code') == 45:
                step_params = step.get('parameters', [])
                if step_params and isinstance(step_params[0], str):
                    step['parameters'][0] = ScriptStrategy.insert_script_string(step_params[0], text_iter)


class RouteStepStrategy(EventStrategy):
    """Estratégia para comando 505 (ROUTE_STEP).
    parameters = [stepObject] onde stepObject['code'] == 45 é script.
    """
    kind = "script"

    def extract(self, item, context=None):
        params = item.get('parameters', [])
        if not params or not isinstance(params[0], dict):
            return None
        step = params[0]
        if step.get('code') == 45:
            step_params = step.get('parameters', [])
            if step_params and isinstance(step_params[0], str):
                extracted = ScriptStrategy.extract_script_string(step_params[0])
                if extracted:
                    return extracted[0] if len(extracted) == 1 else extracted
        return None

    def insert(self, item, text, context=None):
        if not text:
            return
        params = item.get('parameters', [])
        if not params or not isinstance(params[0], dict):
            return
        step = params[0]
        if step.get('code') == 45:
            step_params = step.get('parameters', [])
            if step_params and isinstance(step_params[0], str):
                text_iter = iter(text) if isinstance(text, list) else iter([str(text)])
                step['parameters'][0] = ScriptStrategy.insert_script_string(step_params[0], text_iter)

class PluginStrategyMZ(EventStrategy):
    kind = "script"
    TECHNICAL_KEYS = {
        'switchTypeEX', 'switchIdEX', 'targetIdEX', 'variableIdEX',
        'loop', 'smooth', 'volume', 'reload', 'autoplay', 'finishSwitch',
        'fileName', 'pitch', 'pan', 'wait', 'repeat', 'skippable',
        'WordWrap', 'FontFace', 'FontSize', 'TextSpeed', 'AutoColor',
        'MessageRows', 'MessageWidth', 'ChoiceLineHeight', 'ChoiceRows',
        'switchType', 'variableId', 'value', 'operation', 'operand',
        'image', 'picture', 'icon', 'se', 'bgm', 'bgs', 'me',
        'windowPosition', 'direction', 'id', 'animationType', 'customPattern',
        'cellNumber', 'frameNumber', 'fade', 'pictureNumber', 'align',
        'position', 'type', 'mode', 'easing', 'color', 'opacity', 'blendMode'
    }
    
    TEXT_KEYS = {'text', 'message', 'description', 'title', 'name', 'label', 'tooltip', 'help', 'content', 'body'}
    TEXT_SUFFIXES = {'text', 'message', 'description', 'title', 'label', 'tooltip', 'help', 'content', 'body'}
    _CJK_PATTERN = re.compile(r'[\u3040-\u9fff]')

    @classmethod
    def _is_text_key(cls, key: str) -> bool:
        k = key.lower()
        if k in cls.TEXT_KEYS:
            return True
        return any(k.endswith(sfx) for sfx in cls.TEXT_SUFFIXES)

    def _get_translatable_keys(self, params: dict):
        keys = []
        for key, val in params.items():
            if key in self.TECHNICAL_KEYS:
                continue
            context = 'dialogue' if self._is_text_key(key) else 'param'
            if isinstance(val, str) and not RPGTextFilters.is_technical_or_code(val, context=context):
                if self._is_text_key(key) or ' ' in val or any(c in val for c in '.,;:!?　、。！？') or self._CJK_PATTERN.search(val):
                    keys.append(key)
        return keys

    def extract(self, item, context=None):
        params = item.get('parameters', [])
        if len(params) <= 3 or not isinstance(params[3], dict):
            return None
        
        dict_params = params[3]
        keys = self._get_translatable_keys(dict_params)
        if not keys:
            return None
        extracted = {k: dict_params[k] for k in keys}
        if len(extracted) == 1 and next(iter(extracted)).lower() == 'text':
            return next(iter(extracted.values()))
        return extracted

    def insert(self, item, text, context=None):
        if not text:
            return
        params = item.get('parameters', [])
        if len(params) <= 3 or not isinstance(params[3], dict):
            return
        dict_params = params[3]
        if isinstance(text, dict):
            for key, val in text.items():
                if key in dict_params:
                    dict_params[key] = val
        elif isinstance(text, list):
            keys = self._get_translatable_keys(dict_params)
            for k, val in zip(keys, text):
                dict_params[k] = val
        elif isinstance(text, str):
            keys = self._get_translatable_keys(dict_params)
            target_key = next((k for k in keys if k.lower() == 'text'), keys[0] if keys else None)
            if target_key:
                dict_params[target_key] = text

class PluginStrategyMV(EventStrategy):
    kind = "script"
    _prefixes = ["D_TEXT", "addLog", r"InformationWindow \d+ Text:", "mes ="]
    _compiled_prefixes = [re.compile(prefix) for prefix in _prefixes]

    def extract(self, item, context=None):
        content = item['parameters'][0]
        for compiled_prefix in self._compiled_prefixes:
            match = compiled_prefix.match(content)
            if match:
                prefix = match.group(0)
                split_result = content.split(prefix + " ", 1)
                if len(split_result) > 1:
                    text = split_result[1]
                    if not RPGTextFilters.is_technical_or_code(text):
                        return text
        return None

    def insert(self, item, text, context=None):
        if not text: return
        content = item['parameters'][0]
        for compiled_prefix in self._compiled_prefixes:
            match = compiled_prefix.match(content)
            if match:
                prefix = match.group(0)
                item['parameters'][0] = f'{prefix} {text}'
                break


class ControlVariablesStrategy(EventStrategy):
    """
    Estratégia para o código 122 (Control Variables).
    No RPG Maker, o comando 122 opera variáveis. Quando params[3] == 4, o valor (params[4])
    é uma expressão JavaScript avaliada com eval().

    Regras de Segurança:
    1. Expressões técnicas, enums de dificuldade ("Easy", "Normal", "Hard") ou código puro não são extraídos.
    2. Literais de texto (entre aspas simples, duplas ou backticks) têm apenas o conteúdo interno extraído.
    3. Na inserção, aspas tipográficas (smart quotes “ ” ‘ ’) são estritamente convertidas para aspas ASCII
       para nunca quebrar o eval() do JavaScript com 'SyntaxError: Invalid or unexpected token'.
    """
    kind = "script"
    _DIFFICULTY_ENUM_PATTERN = re.compile(
        r'^[\'"]?\s*(?:easy|normal|hard|very\s*hard|expert|nightmare|hell|beginner|insane|lunatic)\s*[\'"]?;?$',
        re.IGNORECASE
    )

    def extract(self, item, context=None):
        params = item.get('parameters', [])
        if len(params) < 5 or params[3] != 4:
            return None
        val = params[4]
        if not isinstance(val, str) or not val.strip():
            return None

        # Não extrair se for enum de dificuldade ou código técnico
        if self._DIFFICULTY_ENUM_PATTERN.match(val.strip()):
            return None
        if RPGTextFilters.is_technical_or_code(val) or RPGTextFilters.is_numeric(val) or RPGTextFilters.is_boolean(val):
            return None

        # Se for string entre aspas: '"texto"' ou "'texto'" ou '`texto`'
        trimmed = val.strip().rstrip(';')
        if (trimmed.startswith('"') and trimmed.endswith('"') and len(trimmed) >= 2) or \
           (trimmed.startswith("'") and trimmed.endswith("'") and len(trimmed) >= 2) or \
           (trimmed.startswith('`') and trimmed.endswith('`') and len(trimmed) >= 2):
            inner = trimmed[1:-1]
            inner = ScriptStrategy._unescape_quotes(inner)
            if not RPGTextFilters.is_technical_or_code(inner):
                return inner
            return None

        return val

    def insert(self, item, text, context=None):
        if text is None:
            return
        params = item.get('parameters', [])
        if len(params) < 5 or params[3] != 4:
            return

        orig_val = params[4] if isinstance(params[4], str) else ""
        if isinstance(text, list):
            text_str = str(text[0]) if text else ""
        else:
            text_str = str(text)

        trimmed = orig_val.strip().rstrip(';')
        has_semicolon = orig_val.strip().endswith(';')

        if (trimmed.startswith('"') and trimmed.endswith('"') and len(trimmed) >= 2) or \
           (trimmed.startswith("'") and trimmed.endswith("'") and len(trimmed) >= 2) or \
           (trimmed.startswith('`') and trimmed.endswith('`') and len(trimmed) >= 2):
            quote = trimmed[0]
            # Sanitizar aspas tipográficas (smart quotes) imediatamente
            text_str = text_str.replace('“', '"').replace('”', '"').replace('‘', "'").replace('’', "'")
            inner = text_str.strip()
            if inner.startswith(quote) and inner.endswith(quote) and len(inner) >= 2:
                inner = inner[1:-1]
            orig_inner = trimmed[1:-1]
            orig_unescaped = ScriptStrategy._unescape_quotes(orig_inner)
            if inner == orig_unescaped:
                sanitized = orig_inner
            else:
                sanitized = ScriptStrategy._sanitize_script_param(inner, quote, original_text=orig_unescaped)
            params[4] = f'{quote}{sanitized}{quote}' + (';' if has_semicolon else '')
        else:
            params[4] = text_str
