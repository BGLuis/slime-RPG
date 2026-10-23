import os
import re
import copy
import logging
import threading
from typing import Any, Dict, List, Optional, Union

from src.extractor.BaseExtractor import BaseExtractor
from src.factory import register_extractor
import src.utils.TextsUtils as TextsUtils

from .WolfEventCodes import WolfEventCode
from .WolfTextFilters import WolfTextFilters
from .WolfEventStrategy import (
    WolfEventStrategy,
    ShowMessageStrategy,
    ChoicesStrategy,
    SetStringStrategy,
    PictureStrategy,
    CommonEventParamStrategy
)
from .WolfBinaryAdapter import WolfBinaryAdapter, WolfDatabase, WolfProEncryptedError
from .WolfArchive import WolfArchive


@register_extractor("wolfrpg", display_name="Wolf RPG", aliases=["Wolf RPG", "wolf_rpg", "wolf-rpg", "wolf"])
class WolfRPGExtractor(BaseExtractor):
    name = "wolfrpg"
    files_types = ['mps', 'dat', 'project', 'txt']

    def __init__(self, translate):
        super().__init__(translate)
        self._strategies = {
            WolfEventCode.SHOW_MESSAGE: ShowMessageStrategy(),
            WolfEventCode.SHOW_CHOICES: ChoicesStrategy(),
            WolfEventCode.SET_STRING: SetStringStrategy(),
            WolfEventCode.PICTURE: PictureStrategy(),
            WolfEventCode.CALL_COMMON_EVENT: CommonEventParamStrategy(),
            WolfEventCode.CALL_COMMON_BY_NAME: CommonEventParamStrategy(skip_first=True),
        }
        self.target_font = None
        self.repack_wolf = False
        self.preserve_title = False
        self._lookup_types = None
        self._lookup_types_lock = threading.Lock()

    def prepare(self):
        """Procura recursivamente por arquivos .wolf em folderInput e descompacta cada um no seu diretório."""
        folder_input = getattr(self, 'folderInput', self.__class__.folderInput)
        if not os.path.exists(folder_input):
            return

        for root, _, filenames in os.walk(folder_input):
            for filename in filenames:
                if filename.lower().endswith('.wolf'):
                    archive_path = os.path.join(root, filename)
                    dest_dir = os.path.dirname(archive_path)
                    try:
                        WolfArchive.extract_archive(archive_path, dest_dir)
                        logging.info(f"✓ Extraído contêiner .wolf {filename} para {dest_dir}")
                    except Exception as e:
                        logging.error(f"✗ Falha ao descompactar contêiner .wolf {archive_path}: {e}")
                        raise

    @classmethod
    def get_interactive_questions(cls):
        return [
            {
                "key": "preserve_title",
                "id": "preserve_title",
                "question": "Deseja preservar o título original do jogo no Game.dat?",
                "options": [
                    "(Recomendado) Sim, manter título original (evita invalidar saves existentes)",
                    "Não, traduzir o título do jogo"
                ],
                "default": "(Recomendado) Sim, manter título original (evita invalidar saves existentes)"
            },
            {
                "key": "target_font",
                "id": "target_font",
                "question": "Deseja substituir a fonte padrão no Game.dat para melhor exibição de caracteres ocidentais?",
                "options": [
                    "(Recomendado) Manter padrão do jogo",
                    "MS UI Gothic",
                    "Tahoma",
                    "Arial"
                ],
                "default": "(Recomendado) Manter padrão do jogo"
            },
            {
                "key": "repack_mode",
                "id": "repack_mode",
                "question": "Como deseja gerar a saída final dos arquivos traduzidos?",
                "options": [
                    "(Recomendado) Arquivos descompactados na pasta Data/ (Carregamento nativo prioritário)",
                    "Empacotar arquivo Data.wolf (.wolf repack)"
                ],
                "default": "(Recomendado) Arquivos descompactados na pasta Data/ (Carregamento nativo prioritário)"
            }
        ]

    def apply_configuration(self, config):
        if not config:
            return
        if "preserve_title" in config:
            title_opt = config.get("preserve_title")
            if isinstance(title_opt, bool):
                self.preserve_title = title_opt
            elif isinstance(title_opt, str):
                if "Sim" in title_opt or title_opt.lower() in ("true", "1", "yes", "sim"):
                    self.preserve_title = True
                elif "Não" in title_opt or title_opt.lower() in ("false", "0", "no", "nao", "não"):
                    self.preserve_title = False

        font_opt = config.get("target_font", "")
        if font_opt and not font_opt.startswith("(Recomendado)"):
            self.target_font = font_opt.strip()

        repack_opt = config.get("repack_mode", "")
        if "Empacotar" in repack_opt:
            self.repack_wolf = True

    def get_mask_patterns(self, file_name=None, data=None):
        """Padrões regex de escape e códigos de controle do WOLF RPG."""
        p_control_codes = re.compile(r'\\[cCvVsS]\s*\[[^\]]*\]', re.IGNORECASE)
        p_self_vars = re.compile(r'\\(?:cself|self)\s*\[[^\]]*\]', re.IGNORECASE)
        p_font = re.compile(r'\\font\s*\[[^\]]*\]', re.IGNORECASE)
        p_wait_speed = re.compile(r'\\[wt]\s*\[[^\]]*\]', re.IGNORECASE)
        p_ruby = re.compile(r'\\r\s*\[[^\]]*\]', re.IGNORECASE)
        p_generic_tags = re.compile(r'\\[A-Za-z]{1,5}\s*\[[^\]]*\]', re.IGNORECASE)
        p_html = re.compile(r'</?[a-zA-Z][a-zA-Z0-9]*(?:\s+[^>]*)?/?>', re.IGNORECASE)
        p_format_vars = re.compile(r'%[0-9]*[sdif]', re.IGNORECASE)

        return [
            p_control_codes,
            p_self_vars,
            p_font,
            p_wait_speed,
            p_ruby,
            p_generic_tags,
            p_html,
            p_format_vars
        ]

    @classmethod
    def extract_files(cls, file_path):
        if not file_path.endswith(tuple(cls.files_types)):
            return [os.path.basename(file_path), None]

        file_name = os.path.basename(file_path)

        # Arquivos de script de texto (Evtext)
        if file_path.endswith('.txt'):
            try:
                with open(file_path, 'rb') as f:
                    raw_bytes = f.read()

                encoding = 'cp932'
                try:
                    content = raw_bytes.decode('cp932')
                except UnicodeDecodeError:
                    content = raw_bytes.decode('utf-8', errors='replace')
                    encoding = 'utf-8'

                lineterminator = '\r\n' if b'\r\n' in raw_bytes else '\n'
                data = {
                    'encoding': encoding,
                    'lineterminator': lineterminator,
                    'lines': content.splitlines(keepends=True)
                }
                return [file_name, data]
            except Exception as e:
                logging.error(f"Erro ao ler arquivo de texto {file_path}: {e}")
                raise

        # Arquivos binários do Wolf RPG
        if file_path.endswith(('.mps', '.dat', '.project')):
            data = WolfBinaryAdapter.load_file(file_path)
            return [file_name, data]

        return super().extract_files(file_path)

    @classmethod
    def import_file(cls, file_name, json_data, folder):
        dest_path = os.path.join(folder, file_name)
        if file_name.endswith('.txt') and isinstance(json_data, dict) and 'lines' in json_data:
            os.makedirs(os.path.dirname(dest_path), exist_ok=True)
            encoding = json_data.get('encoding', 'cp932')
            content = "".join(json_data.get('lines', []))
            if encoding == 'cp932':
                try:
                    raw_bytes = content.encode('cp932')
                except UnicodeEncodeError:
                    from src.utils.TextsUtils import normalize_western_chars
                    norm_content = normalize_western_chars(content)
                    raw_bytes = norm_content.encode('cp932', errors='replace')
            else:
                raw_bytes = content.encode(encoding, errors='replace')

            with open(dest_path, 'wb') as f:
                f.write(raw_bytes)
            logging.info(f"✓ Validado e salvo script WOLF RPG Evtext: {file_name}")
            return

        if file_name.endswith(('.mps', '.dat')):
            try:
                original_raw = getattr(json_data, '_raw_wolf', None)
                has_dummy_types = (
                    isinstance(original_raw, WolfDatabase)
                    and any(t.name.startswith("Type_") for t in original_raw.types)
                )
                if original_raw is None or has_dummy_types:
                    for candidate_folder in [cls.folderInput, cls.folderProcess]:
                        candidate = os.path.join(candidate_folder, file_name)
                        if os.path.exists(candidate):
                            loaded = WolfBinaryAdapter.load_file(candidate)
                            cand_raw = getattr(loaded, '_raw_wolf', None)
                            if cand_raw is not None:
                                original_raw = cand_raw
                                break

                os.makedirs(os.path.dirname(dest_path), exist_ok=True)
                WolfBinaryAdapter.save_file(dest_path, json_data, original_raw=original_raw)
                logging.info(f"✓ Validado e salvo binário WOLF RPG: {file_name}")
                return
            except Exception as e:
                logging.error(f"✗ Falha ao salvar binário WOLF RPG {file_name}: {e}")
                raise

        super().import_file(file_name, json_data, folder)

    def is_translatable_file(self, file_name, file_path=None):
        base_name = os.path.basename(file_name)
        if base_name.endswith('.mps'):
            return True
        if base_name in ('Game.dat', 'CommonEvent.dat', 'DataBase.dat', 'CDataBase.dat', 'SysDatabase.dat'):
            return True
        if base_name.endswith('.txt'):
            lower_name = base_name.lower()
            if lower_name in ('readme.txt', 'license.txt', 'patch-config.txt', 'patch-config.example.txt', 'game_errorlog.txt'):
                return False
            return True
        return False

    def extract_text(self, file_name, data):
        if data is None:
            return None

        base_name = os.path.basename(file_name)
        if base_name.endswith('.mps'):
            return self.extract_text_map(data)
        elif base_name == 'CommonEvent.dat':
            return self.extract_text_common_events(data)
        elif base_name.endswith('.dat') and base_name != 'Game.dat':
            return self.extract_text_database(data)
        elif base_name == 'Game.dat':
            return self.extract_text_gamedat(data)
        elif base_name.endswith('.txt'):
            return self.extract_text_evtext(data)

        return None

    def update_json(self, file_name, data, new_data):
        if not new_data:
            return data

        updated = copy.deepcopy(data)
        if hasattr(data, '_raw_wolf'):
            updated._raw_wolf = data._raw_wolf

        base_name = os.path.basename(file_name)
        if base_name.endswith('.mps'):
            self.insert_text_map(updated, new_data)
        elif base_name == 'CommonEvent.dat':
            self.insert_text_common_events(updated, new_data)
        elif base_name.endswith('.dat') and base_name != 'Game.dat':
            self.insert_text_database(updated, new_data)
        elif base_name == 'Game.dat':
            self.insert_text_gamedat(updated, new_data)
        elif base_name.endswith('.txt'):
            self.insert_text_evtext(updated, new_data)

        return updated

    # =========================================================================
    # Mapas (.mps)
    # =========================================================================

    def extract_text_map(self, data):
        results = []
        events_map = {}

        for event in data.get('events', []):
            if not event:
                continue
            event_id = event.get('id')

            for page in event.get('pages', []):
                page_id = page.get('id')

                for cmd_idx, cmd in enumerate(page.get('list', [])):
                    cid = cmd.get('code')
                    strategy = self._strategies.get(cid)
                    if strategy:
                        text = strategy.extract(cmd)
                        if text:
                            if event_id not in events_map:
                                events_map[event_id] = {"id": event_id, "pages": []}
                                results.append(events_map[event_id])

                            page_obj = next(
                                (p for p in events_map[event_id]['pages'] if p['id'] == page_id),
                                None
                            )
                            if not page_obj:
                                page_obj = {"id": page_id, "list": []}
                                events_map[event_id]['pages'].append(page_obj)

                            page_obj['list'].append({"id": cmd_idx, "text": text})

        return results

    def insert_text_map(self, data, new_data):
        events_dict = {ev['id']: ev for ev in data.get('events', []) if ev}

        for event_data in new_data:
            event_id = event_data.get('id')
            raw_event = events_dict.get(event_id)
            if not raw_event:
                continue

            pages_dict = {p['id']: p for p in raw_event.get('pages', [])}
            for page_data in event_data.get('pages', []):
                page_id = page_data.get('id')
                raw_page = pages_dict.get(page_id)
                if not raw_page:
                    continue

                cmd_list = raw_page.get('list', [])
                for item in page_data.get('list', []):
                    cmd_idx = item.get('id')
                    if cmd_idx < len(cmd_list):
                        cmd = cmd_list[cmd_idx]
                        cid = cmd.get('code')
                        strategy = self._strategies.get(cid)
                        if strategy:
                            strategy.insert(cmd, item.get('text'))

    # =========================================================================
    # Eventos Comuns (CommonEvent.dat)
    # =========================================================================

    def extract_text_common_events(self, data):
        results = []
        events_map = {}

        for event in data:
            if not event:
                continue
            event_id = event.get('id')

            for cmd_idx, cmd in enumerate(event.get('list', [])):
                cid = cmd.get('code')
                strategy = self._strategies.get(cid)
                if strategy:
                    text = strategy.extract(cmd)
                    if text:
                        if event_id not in events_map:
                            events_map[event_id] = {"id": event_id, "list": []}
                            results.append(events_map[event_id])

                        events_map[event_id]['list'].append({"id": cmd_idx, "text": text})

        return results

    def insert_text_common_events(self, data, new_data):
        events_dict = {ev['id']: ev for ev in data if ev}

        for event_data in new_data:
            event_id = event_data.get('id')
            raw_event = events_dict.get(event_id)
            if not raw_event:
                continue

            cmd_list = raw_event.get('list', [])
            for item in event_data.get('list', []):
                cmd_idx = item.get('id')
                if cmd_idx < len(cmd_list):
                    cmd = cmd_list[cmd_idx]
                    cid = cmd.get('code')
                    strategy = self._strategies.get(cid)
                    if strategy:
                        strategy.insert(cmd, item.get('text'))

    # =========================================================================
    # Banco de Dados (*.dat / *.project)
    # =========================================================================

    def _name_lookup_types(self):
        """Nomes dos tipos de banco que os eventos consultam por nome literal do dado.

        Nesses tipos, o texto igual ao nome do dado é chave de busca e não pode ser traduzido.
        Retorna None se algum arquivo não pôde ser lido: sem certeza, o chamador é conservador.
        """
        with self._lookup_types_lock:
            if self._lookup_types is None:
                self._lookup_types = self._scan_name_lookup_types()
            return self._lookup_types

    def _scan_name_lookup_types(self):
        folder_input = getattr(self, 'folderInput', self.__class__.folderInput)
        found = set()

        def scan(commands):
            for cmd in commands:
                if cmd.get('code') != WolfEventCode.DATABASE:
                    continue
                strings = cmd.get('string_args') or []
                if len(strings) >= 3 and strings[1] and strings[2]:
                    found.add(strings[1])

        try:
            for root, _, filenames in os.walk(folder_input):
                for filename in filenames:
                    path = os.path.join(root, filename)
                    if filename == 'CommonEvent.dat':
                        for event in WolfBinaryAdapter.load_file(path):
                            if event:
                                scan(event.get('list', []))
                    elif filename.endswith('.mps'):
                        for event in WolfBinaryAdapter.load_file(path).get('events', []):
                            for page in (event or {}).get('pages', []):
                                scan(page.get('list', []))
        except Exception as e:
            logging.warning(f"Não foi possível mapear buscas por nome no banco ({e}); mantendo nomes de dado intactos.")
            return None
        return found

    def extract_text_database(self, data):
        results = []
        lookup_types = self._name_lookup_types()
        for type_idx, type_item in enumerate(data.get('types', [])):
            type_obj = {"id": type_item.get('id', type_idx), "data": []}
            keeps_names = lookup_types is None or type_item.get('name') in lookup_types

            for datum_idx, datum_item in enumerate(type_item.get('data', [])):
                translatable = {}
                for s_idx, val in enumerate(datum_item.get('string_values', [])):
                    # O jogo busca dados pelo nome (ex.: tipo Costume 'Hair 1'); o nome do
                    # dado não é traduzido, então o valor igual a ele também não pode ser.
                    if keeps_names and val == datum_item.get('name'):
                        continue
                    if WolfTextFilters.is_translatable(val):
                        translatable[str(s_idx)] = val

                if translatable:
                    type_obj["data"].append({
                        "id": datum_item.get('id', datum_idx),
                        "strings": translatable
                    })

            if type_obj["data"]:
                results.append(type_obj)

        return results

    def insert_text_database(self, data, new_data):
        type_dict = {t['id']: t for t in data.get('types', [])}

        for type_data in new_data:
            t_id = type_data.get('id')
            raw_type = type_dict.get(t_id)
            if not raw_type:
                continue

            datum_dict = {d['id']: d for d in raw_type.get('data', [])}
            for datum_data in type_data.get('data', []):
                d_id = datum_data.get('id')
                raw_datum = datum_dict.get(d_id)
                if not raw_datum:
                    continue

                strings_map = datum_data.get('strings', {})
                for s_idx_str, trans_text in strings_map.items():
                    s_idx = int(s_idx_str)
                    if s_idx < len(raw_datum.get('string_values', [])):
                        raw_datum['string_values'][s_idx] = str(trans_text)

    # =========================================================================
    # Configurações do Jogo (Game.dat)
    # =========================================================================

    def extract_text_gamedat(self, data):
        if getattr(self, 'preserve_title', False):
            return None
        title = data.get('title', '')
        if WolfTextFilters.is_translatable(title):
            return {"title": title}
        return None

    def insert_text_gamedat(self, data, new_data):
        if isinstance(new_data, dict) and 'title' in new_data:
            new_title = str(new_data['title'])
            data['title'] = new_title
            if hasattr(data, '_raw_wolf') and hasattr(data._raw_wolf, 'title'):
                data._raw_wolf.title = new_title

        if self.target_font:
            data['font'] = self.target_font
            if hasattr(data, '_raw_wolf') and hasattr(data._raw_wolf, 'font'):
                data._raw_wolf.font = self.target_font

    # =========================================================================
    # Scripts de Texto (Evtext/*.txt)
    # =========================================================================

    def extract_text_evtext(self, data):
        if not isinstance(data, dict) or 'lines' not in data:
            return None

        lines = data.get('lines', [])
        results = {}
        for idx, line in enumerate(lines):
            stripped = line.strip()
            # Ignora linhas vazias ou comandos/comentários que iniciam com / ou //
            if not stripped or stripped.startswith('/') or stripped.startswith('//'):
                continue
            if WolfTextFilters.is_translatable(stripped):
                results[str(idx)] = stripped

        return {"lines": results} if results else None

    def insert_text_evtext(self, data, new_data):
        if not isinstance(data, dict) or 'lines' not in data:
            return
        if not isinstance(new_data, dict) or 'lines' not in new_data:
            return

        lines_map = new_data.get('lines', {})
        original_lines = data['lines']
        for idx_str, trans_text in lines_map.items():
            try:
                idx = int(idx_str)
                if idx < len(original_lines):
                    orig_line = original_lines[idx]
                    if orig_line.endswith('\r\n'):
                        terminator = '\r\n'
                    elif orig_line.endswith('\n'):
                        terminator = '\n'
                    else:
                        terminator = data.get('lineterminator', '\r\n')

                    leading_spaces = len(orig_line) - len(orig_line.lstrip(' \t'))
                    prefix = orig_line[:leading_spaces]
                    raw_trans = str(trans_text).strip()
                    raw_trans = " ".join(raw_trans.splitlines())

                    orig_stripped = orig_line.rstrip('\r\n \t')
                    if orig_stripped.endswith('：'):
                        if raw_trans.endswith(':'):
                            raw_trans = raw_trans[:-1] + '：'
                        elif not raw_trans.endswith('：'):
                            raw_trans = raw_trans + '：'

                    data['lines'][idx] = f"{prefix}{raw_trans}{terminator}"
            except (ValueError, IndexError):
                continue

    # =========================================================================
    # Pós-processamento e Correção de Textos
    # =========================================================================

    @staticmethod
    def fix_text_translate(texts, original_texts=None):
        """
        Corrige formatações corrompidas por tradutores automáticos
        (espaçamentos indevidos em tags de controle \\c[...], \\v[...], \\self[...], ruby text).
        """
        is_single_str = isinstance(texts, str)
        if is_single_str:
            target_list = [texts]
            orig_list = [original_texts] if isinstance(original_texts, str) else None
        else:
            target_list = TextsUtils.dictToList(texts)
            orig_list = TextsUtils.dictToList(original_texts) if original_texts else None

        # Regex para normalizar códigos de controle
        p_c_v_s = re.compile(r'\\\s*([cCsSvV])\s*\[\s*([^\]]*?)\s*\]')
        p_self = re.compile(r'\\\s*(cself|self)\s*\[\s*([^\]]*?)\s*\]', re.IGNORECASE)
        p_ruby = re.compile(r'\\\s*r\s*\[\s*([^,\]]+)\s*,\s*([^\]]+)\s*\]', re.IGNORECASE)
        p_font = re.compile(r'\\\s*font\s*\[\s*([^\]]*?)\s*\]', re.IGNORECASE)
        p_escapes = re.compile(r'\\\s+([\\nrt])')

        for idx, text in enumerate(target_list):
            if not isinstance(text, str):
                continue

            # Normaliza \c[1], \v[10], \s[5]
            text = p_c_v_s.sub(lambda m: f'\\{m.group(1).lower()}[{m.group(2).strip()}]', text)
            # Normaliza \self[0], \cself[10]
            text = p_self.sub(lambda m: f'\\{m.group(1).lower()}[{m.group(2).strip()}]', text)
            # Normaliza \font[...]
            text = p_font.sub(lambda m: f'\\font[{m.group(1).strip()}]', text)
            # Normaliza quebras de linha e escapes \n \r \t
            text = p_escapes.sub(lambda m: f'\\{m.group(1)}', text)

            # Para Ruby text: em traduções para línguas ocidentais, kanji ruby \r[kanji,ruby]
            # é substituído pelo termo traduzido
            text = p_ruby.sub(lambda m: m.group(1).strip(), text)

            target_list[idx] = text

        if is_single_str:
            return target_list[0]

        result_copy = copy.deepcopy(texts)
        TextsUtils.interactive_item(result_copy, target_list)
        return result_copy

    def import_files(self):
        super().import_files()

        # Opcional: Reempacotamento .wolf caso configurado
        if self.repack_wolf:
            try:
                wolf_output = os.path.join(self.folderOutput, "Data.wolf")
                WolfArchive.create_archive(self.folderOutput, wolf_output)
                logging.info(f"✓ Gerado arquivo empacotado WOLF RPG: {wolf_output}")
            except Exception as e:
                logging.error(f"✗ Falha ao empacotar arquivo Data.wolf: {e}")
