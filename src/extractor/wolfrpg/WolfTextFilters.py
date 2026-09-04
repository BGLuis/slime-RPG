import re

class WolfTextFilters:
    # Extensões de mídia comuns em WOLF RPG
    _MEDIA_EXTENSIONS = (
        '.png', '.jpg', '.jpeg', '.bmp', '.gif',
        '.ogg', '.wav', '.mp3', '.mid', '.midi', '.flac', '.aac',
        '.txt', '.csv', '.dat', '.mps', '.wolf', '.project'
    )

    # Padrões técnicos
    _TECHNICAL_PATTERNS = [
        re.compile(r'^(?:Yes|No|OK|Cancel|True|False|None|null|nil)$', re.IGNORECASE),
        re.compile(r'^(?:start|stop|play|pause|end|loop|exit|return)$', re.IGNORECASE),
        re.compile(r'^[A-Za-z0-9_\-\.\/\\]+$'), # Identificadores simples/caminhos sem espaços
        re.compile(r'^(?:System|Graphic|Picture|Sound|BGM|BGS|SE|MapData|BasicData|Chara|ChipSet)[/\\]', re.IGNORECASE),
    ]

    # Expressões com operadores lógicos/aritméticos ou chamadas de código
    _CODE_PATTERNS = [
        re.compile(r'^(?:[0-9+\-*/=<>!&|~^%()\[\]\s]+)$'), # Apenas operações matemáticas/símbolos
        re.compile(r'^(?:[0-9]+)$'),                       # Puramente numérico
    ]

    @classmethod
    def is_media_path(cls, text: str) -> bool:
        """Verifica se o texto representa o caminho ou nome de um arquivo de mídia."""
        if not isinstance(text, str):
            return False
        clean = text.strip().lower()
        return clean.endswith(cls._MEDIA_EXTENSIONS)

    @classmethod
    def is_technical_or_code(cls, text: str) -> bool:
        """Verifica se o texto é identificador interno, fórmula matemática ou caminho técnico."""
        if not isinstance(text, str):
            return True
        
        cleaned = text.strip()
        if not cleaned or cleaned == '■' or len(cleaned) == 0:
            return True

        if cls.is_media_path(cleaned):
            return True

        # Se for somente números ou símbolos
        for pattern in cls._CODE_PATTERNS:
            if pattern.match(cleaned):
                return True

        # Se for identificador simples curto (ex: "SE_01", "bgm02", "ev_flag")
        if len(cleaned) <= 24 and any(p.match(cleaned) for p in cls._TECHNICAL_PATTERNS):
            # Mas se contiver caracteres CJK ou espaços ou pontuação latina, não é puramente técnico
            has_cjk = bool(re.search(r'[\u3040-\u30ff\u3400-\u4dbf\u4e00-\u9fff\uf900-\ufaff]', cleaned))
            if not has_cjk and (' ' not in cleaned):
                return True

        return False

    @classmethod
    def is_translatable(cls, text: str) -> bool:
        """Determina se o texto deve ser enviado para tradução."""
        if not isinstance(text, str):
            return False
        if cls.is_technical_or_code(text):
            return False
        # Remove quebras de linha e checa se sobra conteúdo legível
        stripped = text.replace('\r', '').replace('\n', '').strip()
        return len(stripped) > 0
