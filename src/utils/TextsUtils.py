from .IntWrapper import IntWrapper


def dictToList(dictionary, extract=None):
    if extract is None:
        extract = []
    if isinstance(dictionary, (dict, list, tuple, set)):
        if isinstance(dictionary, dict):
            haystack = dictionary.items()
        elif isinstance(dictionary, (list, tuple)):
            haystack = enumerate(dictionary)
        else:  # set
            haystack = enumerate(list(dictionary))
        for key, value in haystack:
            dictToList(value, extract)
    elif isinstance(dictionary, str):
        extract.append(dictionary)
    return extract


def interactive_item(obj, texts, occurrences=None):
    if occurrences is None:
        occurrences = IntWrapper(0)
    if isinstance(obj, dict) or isinstance(obj, list):
        haystack = obj.items() if isinstance(obj, dict) else enumerate(obj)
        for key, value in haystack:
            if isinstance(value, str):
                current_index = occurrences.get()
                if current_index >= len(texts):
                    raise IndexError(
                        f"Não há textos suficientes. Índice {current_index} fora do range (tamanho: {len(texts)})"
                    )
                obj[key] = texts[current_index]
                occurrences.add(1)
            else:
                interactive_item(value, texts, occurrences)


def convert_special_chars_to_unicode(text):
    try:
        return text.encode('unicode_escape').decode('utf-8')
    except (AttributeError, UnicodeDecodeError, UnicodeEncodeError) as e:
        raise ValueError(f"Erro ao converter caracteres especiais para unicode: {e}")


def decode_unicode_escape(text):
    try:
        return text.encode('utf-8').decode('unicode_escape')
    except (AttributeError, UnicodeDecodeError, UnicodeEncodeError) as e:
        raise ValueError(f"Erro ao decodificar unicode escape: {e}")


import uuid
import re


def _generate_placeholder(prefix="__XTOK_"):
    """Gera um placeholder curto e (praticamente) único."""
    return f"{prefix}{uuid.uuid4().hex[:8]}__"


def mask_tokens_in_structure(obj, patterns, prefix="__XTOK_"):
    """
    Percorre uma estrutura (dict/list/tuple/str) e substitui trechos que casam com
    qualquer regex em `patterns` por placeholders únicos. Retorna (new_obj, mapping)

    - obj: estrutura que contém strings (pode ser dict/list/tuple/str)
    - patterns: lista de objetos compilados re.Pattern (a ordem importa)
    - prefix: prefixo do placeholder gerado

    mapping: dict {placeholder: original_text}
    """
    mapping = {}

    def mask_string(s):
        # Aplica cada padrão em ordem; cada match vira um placeholder
        for pat in patterns:
            # Substituição via função para guardar o original
            def _repl(m):
                orig = m.group(0)
                # não mascarar algo que já pareça um placeholder
                if isinstance(orig, str) and orig.startswith(prefix):
                    return orig
                ph = _generate_placeholder(prefix)
                # garantir unicidade
                while ph in mapping or ph in s:
                    ph = _generate_placeholder(prefix)
                mapping[ph] = orig
                return ph

            try:
                s = pat.sub(_repl, s)
            except Exception:
                # Em caso de algum problema com a regex, apenas pule
                continue
        return s

    def walk(o):
        if isinstance(o, str):
            return mask_string(o)
        elif isinstance(o, dict):
            return {k: walk(v) for k, v in o.items()}
        elif isinstance(o, list):
            return [walk(v) for v in o]
        elif isinstance(o, tuple):
            return tuple(walk(v) for v in o)
        else:
            return o

    return walk(obj), mapping


def _contains_any(obj, needles):
    if isinstance(obj, str):
        return any(needle in obj for needle in needles)
    elif isinstance(obj, dict):
        return any(_contains_any(v, needles) for v in obj.values())
    elif isinstance(obj, (list, tuple)):
        return any(_contains_any(v, needles) for v in obj)
    return False


def unmask_tokens_in_structure(obj, mapping):
    """
    Restaura placeholders em `obj` usando o dicionário `mapping` (placeholder -> original).
    Funciona recursivamente sobre dict/list/tuple/str.

    Levanta exceção se algum placeholder não puder ser restaurado: deixar um
    "__XTOK_xxxxxxxx__" vazar para o arquivo final do jogo é pior do que falhar o
    processamento desse arquivo e deixar o retry de process_file tentar de novo.
    """
    if not mapping:
        return obj

    # Ordenar por comprimento decrescente evita substituições parciais
    placeholders = sorted(mapping.keys(), key=len, reverse=True)
    pattern = re.compile("|".join(re.escape(p) for p in placeholders))

    def unmask_string(s):
        return pattern.sub(lambda m: mapping[m.group(0)], s)

    def walk(o):
        if isinstance(o, str):
            return unmask_string(o)
        elif isinstance(o, dict):
            return {k: walk(v) for k, v in o.items()}
        elif isinstance(o, list):
            return [walk(v) for v in o]
        elif isinstance(o, tuple):
            return tuple(walk(v) for v in o)
        else:
            return o

    restored = walk(obj)

    if _contains_any(restored, placeholders):
        raise ValueError("Desmascaramento incompleto: placeholder de texto sobrou no resultado final.")

    return restored


import unicodedata


# Mapeamento explícito de caracteres acentuados ocidentais e pontuações para ASCII simples
# Garante que textos em japonês (incluindo katakana com dakuten e meio-largura) fiquem 100% preservados
LATIN_ACCENT_MAP = {
    ord("á"): "a", ord("à"): "a", ord("ã"): "a", ord("â"): "a", ord("ä"): "a",
    ord("Á"): "A", ord("À"): "A", ord("Ã"): "A", ord("Â"): "A", ord("Ä"): "A",
    ord("é"): "e", ord("è"): "e", ord("ê"): "e", ord("ë"): "e",
    ord("É"): "E", ord("È"): "E", ord("Ê"): "E", ord("Ë"): "E",
    ord("í"): "i", ord("ì"): "i", ord("î"): "i", ord("ï"): "i",
    ord("Í"): "I", ord("Ì"): "I", ord("Î"): "I", ord("Ï"): "I",
    ord("ó"): "o", ord("ò"): "o", ord("õ"): "o", ord("ô"): "o", ord("ö"): "o",
    ord("Ó"): "O", ord("Ò"): "O", ord("Õ"): "O", ord("Ô"): "O", ord("Ö"): "O",
    ord("ú"): "u", ord("ù"): "u", ord("û"): "u", ord("ü"): "u",
    ord("Ú"): "U", ord("Ù"): "U", ord("Û"): "U", ord("Ü"): "U",
    ord("ç"): "c", ord("Ç"): "C",
    ord("ñ"): "n", ord("Ñ"): "N",
    ord("…"): "...",
    ord("–"): "-", ord("—"): "-",
    ord("“"): '"', ord("”"): '"',
    ord("‘"): "'", ord("’"): "'",
    ord("«"): '"', ord("»"): '"',
    ord("º"): "o", ord("ª"): "a",
}


def fix_mojibake(text: str) -> str:
    """
    Tenta recuperar textos em que bytes UTF-8 foram interpretados/decodificados como CP932 (Shift_JIS).
    Exemplo: 'vocﾃｪ' -> 'você', 'informaﾃｧﾃ｣o' -> 'informação'.
    Caso não seja mojibake, retorna o texto original inalterado.
    """
    if not text:
        return text

    # Verificação rápida: se não contiver caracteres típicos de mojibake CP932 (halfwidth katakana ﾃ, ｧ, etc.)
    # ou se for puramente ASCII, retorna diretamente
    if not any(c in text for c in ('ﾃ', '窶', '縲', 'ｱ', 'ｲ', 'ｳ', 'ｴ', 'ｵ', 'ｶ', 'ｷ', 'ｸ', 'ｹ', 'ｺ', 'ｻ', 'ｼ', 'ｽ', 'ｾ', 'ｿ')):
        return text

    try:
        return text.encode('cp932').decode('utf-8')
    except (UnicodeEncodeError, UnicodeDecodeError):
        pass

    # Tratamento para mojibakes parciais ou que contêm caracteres da Área de Uso Privado (PUA)
    try:
        clean = "".join(c for c in text if ord(c) < 0xF000)
        return clean.encode('cp932').decode('utf-8')
    except Exception:
        return text


def normalize_western_chars(text: str) -> str:
    """
    Normaliza caracteres ocidentais com acentos ou diacríticos para seus equivalentes ASCII simples,
    e pontuações especiais para caracteres padrão, prevenindo a exibição de ideogramas
    japoneses (mojibake) em engines que utilizam fontes legadas sem suporte a acentuação (como Wolf RPG).
    Preserva caracteres japoneses (Hiragana, Katakana e Kanji) sem alterações.
    """
    if not text:
        return text

    # 1. Recupera possível mojibake prévio
    text = fix_mojibake(text)

    # 2. Tradução direta de acentos ocidentais e pontuações para ASCII simples
    return text.translate(LATIN_ACCENT_MAP)

