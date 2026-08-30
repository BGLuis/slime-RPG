# src/factory.py

_EXTRACTORS = {}
_TRANSLATORS = {}

def register_extractor(name, hidden=False, **kwargs):
    """
    Decorator para registrar um Extrator.
    :param name: Nome que aparecerá na CLI/GUI.
    :param hidden: Se True, não aparecerá na lista de opções.
    :param kwargs: Quaisquer propriedades extras a serem injetadas na classe.
    """
    def decorator(cls):
        if not hidden:
            _EXTRACTORS[name] = cls
            cls.name = name  # Força o nome na classe para manter compatibilidade
            for key, value in kwargs.items():
                setattr(cls, key, value)
        return cls
    return decorator

def register_translator(name, hidden=False, **kwargs):
    """
    Decorator para registrar um Tradutor.
    :param name: Nome que aparecerá na CLI/GUI.
    :param hidden: Se True, não aparecerá na lista de opções.
    :param kwargs: Quaisquer propriedades extras a serem injetadas na classe.
    """
    def decorator(cls):
        if not hidden:
            _TRANSLATORS[name] = cls
            cls.agent = name # Força o agent na classe para manter compatibilidade
            for key, value in kwargs.items():
                setattr(cls, key, value)
        return cls
    return decorator

class ExtractorFactory:
    @staticmethod
    def get_available():
        return _EXTRACTORS.copy()

    @staticmethod
    def find_match(name):
        """Busca flexível (case-insensitive, sem espaços/hífens) para nomes de extratores"""
        if not name:
            return None
        available = ExtractorFactory.get_available()
        if name in available:
            return name
        n_clean = name.lower().replace(" ", "").replace("_", "").replace("-", "")
        for key in available.keys():
            k_clean = key.lower().replace(" ", "").replace("_", "").replace("-", "")
            if n_clean == k_clean:
                return key
        # Busca por substring caso não seja exato (ex: 'rpg' -> 'RPG Maker')
        for key in available.keys():
            k_clean = key.lower().replace(" ", "").replace("_", "").replace("-", "")
            if n_clean in k_clean or k_clean in n_clean:
                return key
        return None

    @staticmethod
    def create(extractor_name, translate_instance):
        match = ExtractorFactory.find_match(extractor_name)
        if match:
            return _EXTRACTORS[match](translate_instance)
        raise ValueError(f"Extrator '{extractor_name}' não suportado.")

class TranslatorFactory:
    @staticmethod
    def get_available():
        return _TRANSLATORS.copy()

    @staticmethod
    def find_match(name):
        """Busca flexível (case-insensitive, sem espaços/hífens) para nomes de tradutores"""
        if not name:
            return None
        available = TranslatorFactory.get_available()
        if name in available:
            return name
        n_clean = name.lower().replace(" ", "").replace("_", "").replace("-", "")
        for key in available.keys():
            k_clean = key.lower().replace(" ", "").replace("_", "").replace("-", "")
            if n_clean == k_clean:
                return key
        # Busca por substring caso não seja exato (ex: 'google' -> 'googleTraslator')
        for key in available.keys():
            k_clean = key.lower().replace(" ", "").replace("_", "").replace("-", "")
            if n_clean in k_clean or k_clean in n_clean:
                return key
        return None

    @staticmethod
    def create(translator_name):
        match = TranslatorFactory.find_match(translator_name)
        if match:
            return _TRANSLATORS[match]()
        raise ValueError(f"Tradutor '{translator_name}' não suportado.")
