import os
import re
import unicodedata
from typing import Optional, Tuple, Dict

# Mapeamento canônico: código ISO -> (Nome em Inglês, Nome em Português, aliases comuns)
LANGUAGE_MAPPINGS: Dict[str, Tuple[str, str, Tuple[str, ...]]] = {
    'en': ('English', 'Inglês', ('en', 'eng', 'en_us', 'en-us', 'en_gb', 'en-gb', 'english', 'ingles')),
    'pt': ('Portuguese', 'Português', ('pt', 'por', 'pt_br', 'pt-br', 'pt_pt', 'pt-pt', 'portuguese', 'portugues', 'brazilian')),
    'es': ('Spanish', 'Espanhol', ('es', 'spa', 'es_es', 'es-es', 'es_419', 'es-419', 'spanish', 'espanol', 'castellano')),
    'fr': ('French', 'Francês', ('fr', 'fra', 'fre', 'french', 'francais')),
    'de': ('German', 'Alemão', ('de', 'deu', 'ger', 'german', 'deutsch')),
    'it': ('Italian', 'Italiano', ('it', 'ita', 'italian', 'italiano')),
    'ja': ('Japanese', 'Japonês', ('ja', 'jp', 'jpn', 'japanese', 'japones', 'nihongo')),
    'ko': ('Korean', 'Coreano', ('ko', 'kor', 'korean', 'coreano', 'hangul')),
    'ru': ('Russian', 'Russo', ('ru', 'rus', 'russian', 'russo')),
    'zh': ('Chinese', 'Chinês', ('zh', 'chi', 'zho', 'zh_cn', 'zh-cn', 'zh_tw', 'zh-tw', 'chinese', 'chines', 'schinese', 'tchinese')),
    'pl': ('Polish', 'Polonês', ('pl', 'pol', 'polish', 'polones')),
    'tr': ('Turkish', 'Turco', ('tr', 'tur', 'turkish', 'turco')),
    'uk': ('Ukrainian', 'Ucraniano', ('uk', 'ukr', 'ukrainian', 'ucraniano')),
    'vi': ('Vietnamese', 'Vietnamita', ('vi', 'vie', 'vietnamese', 'vietnamita')),
}

def strip_accents(text: str) -> str:
    """Remove acentuações de uma string (ex: 'Português' -> 'Portugues')."""
    return "".join(c for c in unicodedata.normalize('NFKD', text) if not unicodedata.combining(c))

def normalize_language_code(text: str) -> Optional[str]:
    """
    Normaliza uma string (código ou nome de idioma) para seu código canônico (ex: 'pt', 'en', 'es').
    Retorna None se não for reconhecido.
    """
    if not text:
        return None
    cleaned = strip_accents(text.strip().lower()).replace('_', '-').replace(' ', '-')
    
    # 1. Checagem direta de código
    if cleaned in LANGUAGE_MAPPINGS:
        return cleaned

    # 2. Checagem por aliases
    for code, (_, _, aliases) in LANGUAGE_MAPPINGS.items():
        if cleaned in aliases:
            return code
        # Checa sem hífens (ex: ptbr -> pt)
        cleaned_no_dash = cleaned.replace('-', '')
        aliases_no_dash = [a.replace('-', '').replace('_', '') for a in aliases]
        if cleaned_no_dash in aliases_no_dash:
            return code

    return None

def get_english_name(lang_code: str) -> str:
    """Retorna o nome em inglês correspondente ao código (ex: 'pt' -> 'Portuguese')."""
    norm = normalize_language_code(lang_code)
    if norm and norm in LANGUAGE_MAPPINGS:
        return LANGUAGE_MAPPINGS[norm][0]
    return lang_code.capitalize()

def identify_language_from_filename(filename: str) -> Optional[Tuple[str, str]]:
    """
    Verifica se o nome de um arquivo (com ou sem extensão) representa um idioma.
    Retorna uma tupla (código_canônico, padrão_detectado) ou None.
    
    Exemplos de padrões detectados:
    - 'en.csv' -> ('en', 'code_exact')
    - 'English.csv' -> ('en', 'name_exact')
    - 'strings_pt-br.csv' -> ('pt', 'suffix')
    - 'localization_es.csv' -> ('es', 'suffix')
    - 'FR.json' -> ('fr', 'code_exact')
    """
    base_name, _ = os.path.splitext(os.path.basename(filename))
    
    # 1. Nome exato é o código ou alias
    exact_code = normalize_language_code(base_name)
    if exact_code:
        # Verifica se era nome completo ou código curto
        is_name = base_name.lower() in [val[0].lower() for val in LANGUAGE_MAPPINGS.values()]
        return (exact_code, 'name_exact' if is_name else 'code_exact')

    # 2. Padrão com prefixo ou sufixo: 'strings_en', 'localization_pt_br', 'text-es'
    # Extrai o último segmento delimitado por '_' ou '-'
    parts = re.split(r'[-_]', base_name)
    if len(parts) >= 2:
        # Tenta o último segmento
        last_seg = parts[-1]
        code = normalize_language_code(last_seg)
        if code:
            return (code, 'suffix')
        
        # Tenta os dois últimos segmentos (ex: strings_pt_br)
        if len(parts) >= 3:
            combined = f"{parts[-2]}-{parts[-1]}"
            code = normalize_language_code(combined)
            if code:
                return (code, 'suffix_composite')

        # Tenta o primeiro segmento (ex: en_subtitles)
        first_seg = parts[0]
        code = normalize_language_code(first_seg)
        if code:
            return (code, 'prefix')

    return None

def generate_target_filename(source_filename: str, target_lang_code: str) -> str:
    """
    Gera o nome do arquivo de destino com base no padrão do arquivo de origem.
    
    Exemplos:
    - ('en.csv', 'pt') -> 'pt.csv'
    - ('EN.csv', 'pt') -> 'PT.csv'
    - ('English.csv', 'pt') -> 'Portuguese.csv'
    - ('english.csv', 'pt') -> 'portuguese.csv'
    - ('strings_en.csv', 'pt') -> 'strings_pt.csv'
    - ('text_en_US.csv', 'pt') -> 'text_pt.csv'
    """
    dirname = os.path.dirname(source_filename)
    basename = os.path.basename(source_filename)
    name, ext = os.path.splitext(basename)

    id_info = identify_language_from_filename(basename)
    if not id_info:
        # Se não detectou padrão claro, usa o target_lang_code com a extensão
        new_basename = f"{name}_{target_lang_code}{ext}"
        return os.path.join(dirname, new_basename) if dirname else new_basename

    source_code, pattern = id_info
    norm_target = normalize_language_code(target_lang_code) or target_lang_code.lower()

    if pattern == 'name_exact':
        # Mantém a capitalização do nome
        target_name = get_english_name(norm_target)
        if name.islower():
            target_name = target_name.lower()
        elif name.isupper():
            target_name = target_name.upper()
        new_basename = f"{target_name}{ext}"
    elif pattern == 'code_exact':
        target_code = norm_target
        if name.isupper():
            target_code = target_code.upper()
        new_basename = f"{target_code}{ext}"
    elif pattern in ('suffix', 'suffix_composite'):
        # Substitui o segmento identificado no final
        parts = re.split(r'([-_])', name) # Preserva os separadores
        if pattern == 'suffix_composite' and len(parts) >= 5:
            new_name = "".join(parts[:-3]) + parts[-3] + norm_target
        else:
            new_name = "".join(parts[:-1]) + norm_target
        new_basename = f"{new_name}{ext}"
    elif pattern == 'prefix':
        parts = re.split(r'([-_])', name)
        new_name = norm_target + "".join(parts[1:])
        new_basename = f"{new_name}{ext}"
    else:
        new_basename = f"{name}_{norm_target}{ext}"

    return os.path.join(dirname, new_basename) if dirname else new_basename

def is_language_column(column_name: str) -> Optional[str]:
    """
    Verifica se o nome de uma coluna em uma tabela CSV é um identificador de idioma.
    Exemplos: 'en', 'English', 'pt-BR', 'Português' -> retorna 'en', 'en', 'pt', 'pt'.
    """
    if not column_name:
        return None
    return normalize_language_code(column_name)
