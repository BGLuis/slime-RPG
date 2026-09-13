import re
from .RPGEventCodes import RPGEventCode
from .RPGTextFilters import RPGTextFilters

class EventStrategy:
    def extract(self, item, context=None):
        return None

    def insert(self, item, translated_text, context=None):
        pass

class ShowTextStrategy(EventStrategy):
    _text_keys = ["icon:", "secret:", "title:", "name:", "text:", "message:", "text1:", "text2:", "secretText:", "id:"]
    _title_pattern = re.compile(r'<([^<>]*)>|<<([^<>]*)>>')

    def extract(self, item, context=None):
        params = item.get('parameters', [])
        code = item.get('code')
        extracted = []
        for i, param in enumerate(params):
            if code == 101 and i == 0:
                continue
            if not RPGTextFilters.is_technical_or_code(param):
                title_match = self._title_pattern.search(param)
                if title_match:
                    groups = [group for group in title_match.groups() if group]
                    if groups:
                        title = groups[0]
                        text = param[title_match.end():].strip()
                        if text and not RPGTextFilters.is_technical_or_code(title):
                            extracted.append([title, text])
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
        # Para simplificar, vamos ver se temos o mesmo número de parâmetros que extraímos
        
        params = item.get('parameters', [])
        code = item.get('code')
        if len(params) == 1:
            # Lógica legada para Show Text / Titles
            iten_text = params[0]
            new_text = text[1] if isinstance(text, list) and len(text) == 2 else text
            
            match = self._title_pattern.search(iten_text)
            if match:
                item['parameters'][0] = iten_text.replace(iten_text[match.end():].strip(), str(new_text).strip())
            elif any(iten_text.startswith(key) for key in self._text_keys):
                iten_text_parts = iten_text.split(":", 1)
                if len(iten_text_parts) > 1:
                    item['parameters'][0] = iten_text_parts[0] + ": " + str(new_text)
            else:
                item['parameters'][0] = str(new_text)
        else:
            # Para comandos multi-parâmetros (122, 101, etc.)
            # Precisamos encontrar QUAL parâmetro é texto.
            # Como a extração pegou o primeiro que não era técnico, vamos fazer o mesmo na inserção.
            for i in range(len(params)):
                if code == 101 and i == 0:
                    continue
                if not RPGTextFilters.is_technical_or_code(params[i]):
                    item['parameters'][i] = str(text)
                    break

class ChoiceStrategy(EventStrategy):
    _choice_condition_pattern = re.compile(r'^(?:if|show_if|hide_if|en|pt|ja|es|fr)\s*\(.*\)\s*', re.IGNORECASE)

    def extract(self, item, context=None):
        if not item.get('parameters'): return None
        choices = item['parameters'][0]
        if not choices: return None
        
        has_translatable = False
        extracted = []
        for choice in choices:
            choice = choice.strip()
            match = self._choice_condition_pattern.match(choice)
            choice_text = choice[match.end():] if match else choice
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
            orig_clean = orig.strip()
            match = self._choice_condition_pattern.match(orig_clean)
            choice_text = orig_clean[match.end():] if match else orig_clean
            if RPGTextFilters.is_technical_or_code(choice_text):
                final_list.append(orig)
            elif match:
                prefix = match.group(0).rstrip()
                final_list.append(prefix + str(trans).strip())
            else:
                final_list.append(str(trans).strip())
        item['parameters'][0] = final_list

class ChoiceBranchStrategy(EventStrategy):
    def extract(self, item, context=None):
        params = item.get('parameters', [])
        if len(params) > 1:
            text = params[1]
            if not RPGTextFilters.is_technical_or_code(text):
                return text
        return None

    def insert(self, item, text, context=None):
        if not text: return
        params = item.get('parameters', [])
        if len(params) > 1:
            if not RPGTextFilters.is_technical_or_code(params[1]):
                item['parameters'][1] = str(text)

class ScriptStrategy(EventStrategy):
    _quote_pattern = re.compile(r"'.*?'|\".*?\"")
    _subject_pattern = re.compile(r'\d_t=\d{2,}_subject=')
    _om_pattern = re.compile(
        r"\$gameScreen\.OriginalMessage\s*\(\s*(['\"])((?:\\.|(?!\1).)*)\1\s*,\s*(['\"])((?:\\.|(?!\3).)*)\3",
        re.IGNORECASE
    )

    # SAFELIST: Só extrair de funções conhecidas
    SAFE_FUNCTIONS = [
        re.compile(r"(?:\$gameMessage\.add|BattleManager\._logWindow\.push\('addText',|this\.addCommand|this\.setHelpWindowText|TickerManager\.show)\s*\(\s*(['\"])((?:\\.|(?!\1).)*)\1", re.IGNORECASE),
        re.compile(r"(?:mes|text)\s*=\s*(['\"])((?:\\.|(?!\1).)*)\1", re.IGNORECASE)
    ]

    @classmethod
    def extract_script_string(cls, script_content):
        if not isinstance(script_content, str):
            return []

        extracted = []
        # 1. $gameScreen.OriginalMessage("nome", "mensagem", tipo, pos)
        for match in cls._om_pattern.finditer(script_content):
            name = match.group(2)
            msg = match.group(4)
            if not RPGTextFilters.is_technical_or_code(name):
                extracted.append(name)
            if not RPGTextFilters.is_technical_or_code(msg):
                extracted.append(msg)

        # 2. Funções seguras de parâmetro único
        for pattern in cls.SAFE_FUNCTIONS:
            for match in pattern.finditer(script_content):
                text = match.group(2)
                if not RPGTextFilters.is_technical_or_code(text):
                    extracted.append(text)

        # 3. Padrão de subject
        if not extracted and cls._subject_pattern.search(script_content):
            extract_text = script_content.split('_subject=')[1].strip('"')
            if not RPGTextFilters.is_technical_or_code(extract_text):
                extracted.append(extract_text)

        return extracted

    @classmethod
    def _sanitize_script_param(cls, text, quote_char):
        if not isinstance(text, str):
            return text
        s = text.strip()
        # Normalizar aspas tipográficas curvas
        s = s.replace('“', '"').replace('”', '"').replace('‘', "'").replace('’', "'")
        # Remover aspas externas redundantes inseridas pelo tradutor
        while len(s) >= 2 and ((s[0] == '"' and s[-1] == '"') or (s[0] == "'" and s[-1] == "'")):
            s = s[1:-1].strip()
        # Escapar aspas internas para não quebrar a sintaxe do JavaScript
        if quote_char == '"':
            s = re.sub(r'(?<!\\)"', r'\"', s)
        elif quote_char == "'":
            s = re.sub(r"(?<!\\)'", r"\'", s)
        return s

    @classmethod
    def insert_script_string(cls, script_content, text_iter):
        if not isinstance(script_content, str):
            return script_content

        # 1. $gameScreen.OriginalMessage
        if cls._om_pattern.search(script_content):
            def replacer_om(m):
                q1, name = m.group(1), m.group(2)
                q2, msg = m.group(3), m.group(4)
                new_name = name
                new_msg = msg
                if not RPGTextFilters.is_technical_or_code(name):
                    try:
                        new_name = str(next(text_iter))
                    except StopIteration:
                        new_name = name
                if not RPGTextFilters.is_technical_or_code(msg):
                    try:
                        new_msg = str(next(text_iter))
                    except StopIteration:
                        new_msg = msg
                new_name = cls._sanitize_script_param(new_name, q1)
                new_msg = cls._sanitize_script_param(new_msg, q2)
                prefix_call = m.group(0)[:m.start(1) - m.start(0)]
                return f"{prefix_call}{q1}{new_name}{q1}, {q2}{new_msg}{q2}"

            script_content = cls._om_pattern.sub(replacer_om, script_content)

        # 2. Funções seguras de parâmetro único
        for pattern in cls.SAFE_FUNCTIONS:
            if pattern.search(script_content):
                def replacer(m):
                    target_text = m.group(2)
                    quote_char = m.group(1)
                    if RPGTextFilters.is_technical_or_code(target_text):
                        return m.group(0)
                    try:
                        next_text = str(next(text_iter))
                    except StopIteration:
                        next_text = target_text
                    next_text = cls._sanitize_script_param(next_text, quote_char)
                    return m.group(0)[:m.start(2) - m.start(0)] + next_text + m.group(0)[m.end(2) - m.start(0):]

                script_content = pattern.sub(replacer, script_content)

        # 3. Padrão de subject
        if cls._subject_pattern.search(script_content):
            parts = script_content.split('_subject=')
            if len(parts) > 1:
                try:
                    next_text = str(next(text_iter))
                except StopIteration:
                    next_text = parts[1].strip('"')
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

    def extract(self, item, context=None):
        if len(item['parameters']) <= 3 or not isinstance(item['parameters'][3], dict):
            return None
        
        params = item['parameters'][3]
        extracted = {}
        for key, val in params.items():
            if key in self.TECHNICAL_KEYS: continue

            # Chaves fora de TEXT_KEYS são valores de configuração (align, easing...), não
            # diálogo: aqui "Start"/"Auto"/"Center" são técnicos e não devem ser extraídos.
            context = 'dialogue' if key in self.TEXT_KEYS else 'param'
            if isinstance(val, str) and not RPGTextFilters.is_technical_or_code(val, context=context):
                if key in self.TEXT_KEYS or ' ' in val or any(c in val for c in '.,;:!?　、。！？'):
                    extracted[key] = val
        
        if not extracted: return None
        if len(extracted) == 1 and 'text' in extracted: return extracted['text']
        return extracted

    def insert(self, item, text, context=None):
        if not text: return
        params = item['parameters'][3]
        if isinstance(text, dict):
            for key, val in text.items():
                if key in params:
                    params[key] = val
        elif isinstance(text, str) and 'text' in params:
            params['text'] = text

class PluginStrategyMV(EventStrategy):
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
        text_str = str(text)
        # Sanitizar aspas tipográficas (smart quotes) imediatamente
        text_str = text_str.replace('“', '"').replace('”', '"').replace('‘', "'").replace('’', "'")

        trimmed = orig_val.strip().rstrip(';')
        has_semicolon = orig_val.strip().endswith(';')

        if trimmed.startswith('"') and trimmed.endswith('"') and len(trimmed) >= 2:
            quote = '"'
            inner = text_str.strip()
            if inner.startswith(quote) and inner.endswith(quote) and len(inner) >= 2:
                inner = inner[1:-1]
            params[4] = f'{quote}{inner}{quote}' + (';' if has_semicolon else '')
        elif trimmed.startswith("'") and trimmed.endswith("'") and len(trimmed) >= 2:
            quote = "'"
            inner = text_str.strip()
            if inner.startswith(quote) and inner.endswith(quote) and len(inner) >= 2:
                inner = inner[1:-1]
            params[4] = f'{quote}{inner}{quote}' + (';' if has_semicolon else '')
        elif trimmed.startswith('`') and trimmed.endswith('`') and len(trimmed) >= 2:
            quote = '`'
            inner = text_str.strip()
            if inner.startswith(quote) and inner.endswith(quote) and len(inner) >= 2:
                inner = inner[1:-1]
            params[4] = f'{quote}{inner}{quote}' + (';' if has_semicolon else '')
        else:
            params[4] = text_str
