# src/factory.py

class AmbiguousPluginError(ValueError):
    """Lançada quando múltiplos plugins correspondem a um nome ou termo pesquisado."""
    def __init__(self, name, candidates):
        self.name = name
        self.candidates = list(candidates)
        candidates_str = ", ".join(sorted(self.candidates))
        super().__init__(f"Nome de plugin ambíguo '{name}': múltiplos plugins encontrados ({candidates_str})")


_EXTRACTORS = {}
_EXTRACTOR_ALIASES = {}

_TRANSLATORS = {}
_TRANSLATOR_ALIASES = {}


class AvailablePluginsDict(dict):
    """
    Dicionário de plugins que expõe chaves canônicas na iteração,
    mas permite acesso e checagem de pertinência (in, []) também por aliases.
    """
    def __init__(self, canonical_dict, aliases_dict=None):
        super().__init__(canonical_dict)
        self._aliases = aliases_dict or {}

    def __contains__(self, key):
        if super().__contains__(key):
            return True
        if key in self._aliases:
            return True
        k_clean = _clean_str(str(key))
        if any(k_clean == _clean_str(k) for k in self.keys()):
            return True
        return any(k_clean == _clean_str(a) for a in self._aliases.keys())

    def __getitem__(self, key):
        if super().__contains__(key):
            return super().__getitem__(key)
        if key in self._aliases:
            return self._aliases[key]
        k_clean = _clean_str(str(key))
        for k, v in self.items():
            if k_clean == _clean_str(k):
                return v
        for a, v in self._aliases.items():
            if k_clean == _clean_str(a):
                return v
        raise KeyError(key)

    def get(self, key, default=None):
        try:
            return self[key]
        except KeyError:
            return default


def register_extractor(name, hidden=False, aliases=None, display_name=None, **kwargs):
    """
    Decorator para registrar um Extrator.
    :param name: ID canônico do extrator.
    :param hidden: Se True, não aparecerá na lista de opções.
    :param aliases: Lista de nomes alternativos ou legados.
    :param display_name: Nome de exibição amigável.
    :param kwargs: Quaisquer propriedades extras a serem injetadas na classe.
    """
    def decorator(cls):
        if not hidden:
            _EXTRACTORS[name] = cls
            cls.name = name
            cls.plugin_id = name
            disp = display_name or kwargs.get('display_name', name)
            cls.display_name = disp
            for key, value in kwargs.items():
                setattr(cls, key, value)
            all_aliases = list(aliases) if aliases else []
            if disp and disp != name and disp not in all_aliases:
                all_aliases.append(disp)
            if cls.__name__ not in all_aliases and cls.__name__ != name:
                all_aliases.append(cls.__name__)
            for alias in all_aliases:
                _EXTRACTOR_ALIASES[alias] = cls
        return cls
    return decorator


def register_translator(name, hidden=False, aliases=None, display_name=None, **kwargs):
    """
    Decorator para registrar um Tradutor.
    :param name: ID canônico do tradutor.
    :param hidden: Se True, não aparecerá na lista de opções.
    :param aliases: Lista de nomes alternativos ou legados.
    :param display_name: Nome de exibição amigável.
    :param kwargs: Quaisquer propriedades extras a serem injetadas na classe.
    """
    def decorator(cls):
        if not hidden:
            _TRANSLATORS[name] = cls
            cls.agent = name
            cls.id = name
            cls.plugin_id = name
            disp = display_name or kwargs.get('display_name', name)
            cls.display_name = disp
            for key, value in kwargs.items():
                setattr(cls, key, value)
            all_aliases = list(aliases) if aliases else []
            if disp and disp != name and disp not in all_aliases:
                all_aliases.append(disp)
            if cls.__name__ not in all_aliases and cls.__name__ != name:
                all_aliases.append(cls.__name__)
            for alias in all_aliases:
                _TRANSLATOR_ALIASES[alias] = cls
        return cls
    return decorator


def _clean_str(s):
    return s.lower().replace(" ", "").replace("_", "").replace("-", "")


def _find_plugin_match(name, canonical_dict, aliases_dict):
    if not name:
        return None

    # 1. Casamento exato por chave canônica
    if name in canonical_dict:
        return name

    n_clean = _clean_str(name)
    if not n_clean:
        return None

    # 2. Casamento exato normalizado contra chaves canônicas
    exact_canonical_matches = [
        k for k in canonical_dict.keys()
        if n_clean == _clean_str(k)
    ]
    if len(exact_canonical_matches) == 1:
        return exact_canonical_matches[0]
    elif len(exact_canonical_matches) > 1:
        raise AmbiguousPluginError(name, exact_canonical_matches)

    # 3. Casamento exato normalizado contra aliases
    exact_alias_cands = set()
    for alias_name, cls in aliases_dict.items():
        if n_clean == _clean_str(alias_name):
            for canon_name, canon_cls in canonical_dict.items():
                if canon_cls == cls:
                    exact_alias_cands.add(canon_name)
                    break
    if len(exact_alias_cands) == 1:
        return next(iter(exact_alias_cands))
    elif len(exact_alias_cands) > 1:
        raise AmbiguousPluginError(name, sorted(exact_alias_cands))

    # 4. Detecção de ambiguidade por substring (sem substring matching silencioso!)
    sub_cands = set()
    for canon_name, canon_cls in canonical_dict.items():
        all_variants = [canon_name] + [
            a for a, c in aliases_dict.items() if c == canon_cls
        ]
        if any(n_clean in _clean_str(v) for v in all_variants):
            sub_cands.add(canon_name)

    if len(sub_cands) > 1:
        raise AmbiguousPluginError(name, sorted(sub_cands))

    return None


def _get_entry_points(group):
    try:
        import importlib.metadata as im
        eps = im.entry_points()
        if hasattr(eps, 'select'):
            return list(eps.select(group=group))
        elif isinstance(eps, dict):
            return list(eps.get(group, []))
        return list(im.entry_points(group=group))
    except Exception:
        try:
            import importlib.metadata as im
            return list(im.entry_points(group=group))
        except Exception:
            return []


_EXTRACTORS_LOADED = False
_TRANSLATORS_LOADED = False


def discover_extractors(force=False):
    """
    Descobre e registra dinamicamente extratores via entry points 'slime.extractors',
    com fallback local para os módulos em src.extractor.
    """
    global _EXTRACTORS_LOADED
    if _EXTRACTORS_LOADED and not force:
        return

    # 1. Fallback local para garantir os extratores padrão da biblioteca
    try:
        import importlib
        importlib.import_module("src.extractor")
    except Exception as e:
        import logging
        logging.debug(f"Fallback local para src.extractor falhou: {e}")

    # 2. Descoberta dinâmica via entry points 'slime.extractors'
    try:
        eps = _get_entry_points('slime.extractors')
        for ep in eps:
            try:
                cls = ep.load()
                if ep.name not in _EXTRACTORS:
                    _EXTRACTORS[ep.name] = cls
                    cls.name = getattr(cls, 'name', ep.name)
                    cls.plugin_id = ep.name
            except Exception as e:
                import logging
                logging.debug(f"Falha ao carregar entry point de extrator {ep.name}: {e}")
    except Exception as e:
        import logging
        logging.debug(f"Falha na descoberta de entry points 'slime.extractors': {e}")

    _EXTRACTORS_LOADED = True


def discover_translators(force=False):
    """
    Descobre e registra dinamicamente provedores via entry points 'slime.providers',
    com fallback local para os módulos em src.translate.
    """
    global _TRANSLATORS_LOADED
    if _TRANSLATORS_LOADED and not force:
        return

    # 1. Fallback local para garantir os tradutores padrão da biblioteca
    try:
        import importlib
        importlib.import_module("src.translate")
    except Exception as e:
        import logging
        logging.debug(f"Fallback local para src.translate falhou: {e}")

    # 2. Descoberta dinâmica via entry points 'slime.providers'
    try:
        eps = _get_entry_points('slime.providers')
        for ep in eps:
            try:
                cls = ep.load()
                if ep.name not in _TRANSLATORS:
                    _TRANSLATORS[ep.name] = cls
                    cls.agent = getattr(cls, 'agent', ep.name)
                    cls.plugin_id = ep.name
            except Exception as e:
                import logging
                logging.debug(f"Falha ao carregar entry point de tradutor {ep.name}: {e}")
    except Exception as e:
        import logging
        logging.debug(f"Falha na descoberta de entry points 'slime.providers': {e}")

    _TRANSLATORS_LOADED = True


class ExtractorFactory:
    @staticmethod
    def discover(force=False):
        discover_extractors(force=force)

    @staticmethod
    def get_available():
        discover_extractors()
        return AvailablePluginsDict(_EXTRACTORS, _EXTRACTOR_ALIASES)

    @staticmethod
    def find_match(name):
        discover_extractors()
        return _find_plugin_match(name, _EXTRACTORS, _EXTRACTOR_ALIASES)

    @staticmethod
    def create(extractor_name, translate_instance, *args, **kwargs):
        discover_extractors()
        match = ExtractorFactory.find_match(extractor_name)
        if match:
            cls = _EXTRACTORS[match]
            try:
                return cls(translate_instance, *args, **kwargs)
            except TypeError:
                inst = cls(translate_instance)
                if 'workspace' in kwargs and hasattr(inst, 'workspace'):
                    inst.workspace = kwargs['workspace']
                return inst
        raise ValueError(f"Extrator '{extractor_name}' não suportado.")


class TranslatorFactory:
    @staticmethod
    def discover(force=False):
        discover_translators(force=force)

    @staticmethod
    def get_available():
        discover_translators()
        return AvailablePluginsDict(_TRANSLATORS, _TRANSLATOR_ALIASES)

    @staticmethod
    def find_match(name):
        discover_translators()
        return _find_plugin_match(name, _TRANSLATORS, _TRANSLATOR_ALIASES)

    @staticmethod
    def create(translator_name, *args, **kwargs):
        discover_translators()
        match = TranslatorFactory.find_match(translator_name)
        if match:
            return _TRANSLATORS[match](*args, **kwargs)
        raise ValueError(f"Tradutor '{translator_name}' não suportado.")
