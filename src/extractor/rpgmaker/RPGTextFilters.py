import re

class RPGTextFilters:
    # Prefixos inequívocos de código/config: nenhuma prosa real do jogo começa assim.
    IGNORE_PREFIXES = [
        "$", "D_TEXT", "\"+", "\'+", "voice_", "ChoiceVariableId", "PLM", "\"00",
        "let result", "const ", "this.", "super(", "new ", "class ", "extends ",
        "import ", "export ", "async ", "await ", "function", "=>",
    ]

    IGNORE_PARAMS = [
        "&&", "||", "==", "!=", "===", "!==", "$gamevariables.value", "Math."
    ]

    # "if", "for", "try" etc. também são palavras comuns em diálogo real
    # ("If you go there...", "Try again!", "For the king!"), então só contam como
    # código quando têm a forma de código: seguidas de "(" ou "{".
    _CONTROL_KEYWORD_PATTERN = re.compile(r'^(?:if|else\s*if|else|while|for|try|catch|finally|switch)\s*[\(\{]', re.IGNORECASE)
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
    def is_numeric(text):
        if not isinstance(text, str): return False
        try:
            float(text.replace(',', '.'))
            return True
        except ValueError:
            return False

    @staticmethod
    def is_boolean(text):
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

        if any(text.startswith(prefix) for prefix in RPGTextFilters.IGNORE_PREFIXES):
            return True

        if RPGTextFilters._CONTROL_KEYWORD_PATTERN.match(text) or RPGTextFilters._VAR_DECLARATION_PATTERN.match(text):
            return True

        if any(param in text for param in RPGTextFilters.IGNORE_PARAMS):
            return True

        if RPGTextFilters._PURE_ESCAPE_OR_TAG_PATTERN.match(text):
            return True

        return False
