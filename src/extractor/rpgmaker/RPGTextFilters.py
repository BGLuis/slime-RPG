import re

class RPGTextFilters:
    # Prefixos inequívocos de código/config: nenhuma prosa real do jogo começa assim.
    IGNORE_PREFIXES = [
        "D_TEXT", "\"+", "\'+", "voice_", "ChoiceVariableId", "PLM", "\"00",
        "let result", "const ", "this.", "super(", "extends ",
        "async ", "await ", "function", "=>",
    ]

    IGNORE_PARAMS = [
        "&&", "||", "==", "!=", "===", "!==", "$gamevariables.value", "Math."
    ]

    _JS_DOLLAR_PATTERN = re.compile(r'^\$[A-Za-z_]')
    _JS_NEW_PATTERN = re.compile(r'^new\s+[A-Z_][A-Za-z0-9_]*\b')
    _JS_CLASS_PATTERN = re.compile(r'^class\s+[A-Za-z_$][\w$]*\s*(?:extends\s+[\w$]+\s*)?\{')
    _JS_IMPORT_PATTERN = re.compile(r'^import\s+(?:[\w${}\*,\s]+from\s+[\'"]|[\'"])')
    _JS_EXPORT_PATTERN = re.compile(r'^export\s+(?:default\s+|const\s+|let\s+|var\s+|function\s+|class\s+|{[\w\s,]+})')

    _INLINE_CONDITION_HEAD = re.compile(r'^(show_if|hide_if|if|en|pt|ja|es|fr)(\s*)\(', re.IGNORECASE)
    _INLINE_CODE_CHARS = re.compile(r'[\[\]\$=<>!&|]')

    # "if", "for", "try" etc. também são palavras comuns em diálogo real
    # ("If you go there...", "Try again!", "For the king!"), então só contam como
    # código quando têm a forma de código.
    _CONTROL_STMT_PATTERN = re.compile(r'^(?:else\s*if|else|try|catch|finally)\s*[\(\{]', re.IGNORECASE)
    _CONTROL_PAREN_PATTERN = re.compile(r'^(if|while|for|switch)\s*\(', re.IGNORECASE)
    _VAR_DECLARATION_PATTERN = re.compile(r'^var\s+[A-Za-z_$][\w$]*\s*=', re.IGNORECASE)
    _PURE_ESCAPE_OR_TAG_PATTERN = re.compile(r'^\s*(?:\\[A-Za-z]+(?:\s*\[[^\]]*\])?\s*)+$')

    # Literais de código inequívocos (JS/engine): nunca são texto de jogador, em nenhum idioma.
    TECHNICAL_LITERALS = {'TRUE', 'FALSE', 'NULL', 'UNDEFINED', 'NAN'}

    # Valores que são técnicos quando aparecem como parâmetro de configuração de plugin/script
    # (ex: align: "center", easing: "auto"), mas que também são palavras comuns de UI real -
    # um botão de menu pode literalmente se chamar "Start", "Yes" ou "Cancel". Só devem ser
    # tratados como técnicos em contexto de parâmetro (context='param'), nunca em diálogo.
    TECHNICAL_PARAM_VALUES = {
        'YES', 'NO', 'OK', 'CANCEL',
        'END', 'START', 'STOP', 'PLAY', 'PAUSE', 'NOCHANNEL', 'CHANNEL',
        'LEFT', 'RIGHT', 'CENTER', 'TOP', 'BOTTOM', 'MIDDLE', 'ON', 'OFF',
        'NO CHANGE', 'NONE', 'AUTO', 'DEFAULT', 'ALWAYS', 'NEVER',
        'ENABLE', 'DISABLE', 'ENABLED', 'DISABLED',
    }

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

    @classmethod
    def parse_choice_condition(cls, text: str):
        if hasattr(text, 'text'):
            text = text.text
        if not isinstance(text, str):
            return "", text
        stripped = text.strip()
        m = cls._INLINE_CONDITION_HEAD.match(stripped)
        if not m:
            return "", text
        kw = m.group(1).lower()
        ws = m.group(2)
        open_idx = m.end() - 1
        end_idx = cls._find_balanced_end(stripped, open_idx)
        if end_idx == -1:
            return "", text
        inner = stripped[open_idx + 1:end_idx - 1]
        if kw not in ('show_if', 'hide_if'):
            if ws or not cls._INLINE_CODE_CHARS.search(inner):
                return "", text

        rest = stripped[end_idx:]
        # Se contiver '{', terminar com ';' ou for statement/atribuição, é JS, não escolha
        if rest.lstrip().startswith('{') or '{' in rest or rest.rstrip().endswith(';') or re.search(r'[\w$]\s*=[^=]', rest):
            return "", text

        ltrimmed = rest.lstrip()
        space_after = rest[:len(rest) - len(ltrimmed)]
        leading_ws = text[:len(text) - len(stripped)]
        prefix = leading_ws + stripped[:end_idx] + space_after
        return prefix, ltrimmed

    @staticmethod
    def is_numeric(text):
        if hasattr(text, 'text'):
            text = text.text
        if not isinstance(text, str): return False
        try:
            float(text.replace(',', '.'))
            return True
        except ValueError:
            return False

    @staticmethod
    def is_boolean(text):
        if hasattr(text, 'text'):
            text = text.text
        if not isinstance(text, str): return False
        return text.lower() in ['true', 'false']

    @staticmethod
    def is_technical_or_code(text, context='dialogue'):
        """
        context='dialogue' (padrão): texto potencialmente visível ao jogador (fala, escolha,
            label, nome, categoria de sistema). Só barra literais de código inequívocos.
        context='param': valor de parâmetro de plugin/script/config, onde palavras como
            "Start"/"Auto"/"Center" são valores técnicos de configuração, não texto de UI.
        """
        if hasattr(text, 'text'):
            text = text.text
        if not isinstance(text, str):
            if isinstance(text, list):
                return all(RPGTextFilters.is_technical_or_code(item, context) for item in text)
            return True # Retornamos True (ignorar) se não for string nem lista (ex: int, float, bool)

        if text == '':
            return True

        upper_text = text.upper()
        if upper_text in RPGTextFilters.TECHNICAL_LITERALS:
            return True
        if context == 'param' and upper_text in RPGTextFilters.TECHNICAL_PARAM_VALUES:
            return True

        # Ignorar apenas números
        clean_text = text.replace('.', '').replace(',', '').replace('-', '').replace('+', '').replace(' ', '')
        if clean_text.isdigit():
            return True

        # Se começar com condição inline de escolha (ex: show_if(v[1] == 1) Opção),
        # avalia apenas o corpo se existir.
        cond_prefix, choice_body = RPGTextFilters.parse_choice_condition(text)
        if cond_prefix and any(cond_prefix.lower().startswith(p) for p in ('show_if', 'hide_if')):
            if not choice_body:
                return True
            return RPGTextFilters.is_technical_or_code(choice_body, context=context)

        if any(text.startswith(prefix) for prefix in RPGTextFilters.IGNORE_PREFIXES):
            return True

        if RPGTextFilters._JS_DOLLAR_PATTERN.match(text) or \
           RPGTextFilters._JS_NEW_PATTERN.match(text) or \
           RPGTextFilters._JS_CLASS_PATTERN.match(text) or \
           RPGTextFilters._JS_IMPORT_PATTERN.match(text) or \
           RPGTextFilters._JS_EXPORT_PATTERN.match(text):
            return True

        if RPGTextFilters._CONTROL_STMT_PATTERN.match(text) or RPGTextFilters._VAR_DECLARATION_PATTERN.match(text):
            return True

        paren_m = RPGTextFilters._CONTROL_PAREN_PATTERN.match(text)
        if paren_m:
            end_idx = RPGTextFilters._find_balanced_end(text, paren_m.end() - 1)
            if end_idx != -1:
                inner = text[paren_m.end():end_idx - 1]
                after = text[end_idx:].strip()
                if RPGTextFilters._INLINE_CODE_CHARS.search(inner) or after.startswith('{') or after.startswith('return') or ';' in after:
                    return True
            else:
                return True

        if any(param in text for param in RPGTextFilters.IGNORE_PARAMS):
            return True

        if RPGTextFilters._PURE_ESCAPE_OR_TAG_PATTERN.match(text):
            return True

        return False
