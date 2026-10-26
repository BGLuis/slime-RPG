import os
import re
import copy
import logging
import threading
from typing import Any, Dict, List, Optional, Union

from src.extractor.BaseExtractor import BaseExtractor, _ClassOrInstanceMethod
from src.extractor.FormatAdapter import JsonDataDict
from src.extractor.FormatAdapter import EvtextFormatAdapter
from src.extractor.Segment import Segment, copy_preserving_metadata
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
    format_adapters = (WolfBinaryAdapter, EvtextFormatAdapter)

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

        multimedia_prefixes = (
            'bgm', 'se', 'seh', 'sound', 'voice', 'graphic', 'picture', 'cg',
            'hcg', 'samune', 'hsamune', 'face', 'charachip', 'enemychip', 'enemygraphic',
            'eventgraphic', 'systemgraphic', 'battlegraphic', 'battleeffect',
            'mapchip', 'parallax', 'fog', 'font', 'hfont', 'icon', 'movie', 'video'
        )

        for root, _, filenames in os.walk(folder_input):
            for filename in filenames:
                if filename.lower().endswith('.wolf'):
                    archive_path = os.path.join(root, filename)
                    stem = os.path.splitext(filename)[0]

                    if any(stem.lower().startswith(p) for p in multimedia_prefixes):
                        logging.info(f"Ignorando contêiner multimídia {filename}")
                        continue

                    # No Wolf RPG, arquivos como BasicData.wolf e MapData.wolf correspondem
                    # às pastas Data/BasicData e Data/MapData.
                    # Se o contêiner for Data.wolf, os arquivos já costumam conter a hierarquia interna.
                    if stem.lower() != 'data':
                        dest_dir = os.path.join(root, stem)
                    else:
                        dest_dir = root

                    if os.path.exists(dest_dir) and any(not f.lower().endswith('.wolf') for f in os.listdir(dest_dir)):
                        logging.info(f"Pasta {dest_dir} já contém arquivos extraídos. Pulando descompactação de {filename}.")
                        bak_path = archive_path + ".bak"
                        if not os.path.exists(bak_path):
                            try:
                                os.rename(archive_path, bak_path)
                                logging.info(f"Renomeado {filename} para {os.path.basename(bak_path)} para priorizar pasta solta.")
                            except Exception as e:
                                logging.warning(f"Não foi possível renomear {filename}: {e}")
                        continue

                    try:
                        WolfArchive.extract_archive(archive_path, dest_dir)
                        logging.info(f"✓ Extraído contêiner .wolf {filename} para {dest_dir}")
                        bak_path = archive_path + ".bak"
                        if not os.path.exists(bak_path):
                            try:
                                os.rename(archive_path, bak_path)
                                logging.info(f"Renomeado {filename} para {os.path.basename(bak_path)} para priorizar pasta solta.")
                            except Exception as e:
                                logging.warning(f"Não foi possível renomear {filename}: {e}")
                    except Exception as e:
                        logging.error(f"✗ Falha ao descompactar contêiner .wolf {archive_path}: {e}")
                        raise

    @classmethod
    def detect(cls, path: str):
        import glob
        from src.services.GameDetector import DetectionResult

        wolf_data_dir = os.path.join(path, 'Data')
        if os.path.isdir(wolf_data_dir):
            common_dat = os.path.join(wolf_data_dir, 'BasicData', 'CommonEvent.dat')
            map_files = glob.glob(os.path.join(wolf_data_dir, 'MapData', '*.mps'))
            wolf_archives = glob.glob(os.path.join(wolf_data_dir, '*.wolf'))
            evtext_files = glob.glob(os.path.join(wolf_data_dir, 'Evtext', '**', '*.txt'), recursive=True)
            if os.path.isfile(common_dat) or len(map_files) > 0 or len(wolf_archives) > 0 or len(evtext_files) > 0:
                total_files = (
                    (1 if os.path.isfile(common_dat) else 0)
                    + len(map_files)
                    + len(wolf_archives)
                    + len(evtext_files)
                )
                return DetectionResult(
                    detected_path=wolf_data_dir,
                    score=82,
                    extractor=cls.display_name,
                    description='WOLF RPG Editor (subpasta Data)',
                    file_count=total_files,
                )

        root_wolf_files = glob.glob(os.path.join(path, '*.wolf'))
        if len(root_wolf_files) > 0:
            return DetectionResult(
                detected_path=path,
                score=80,
                extractor=cls.display_name,
                description='WOLF RPG Editor (arquivos .wolf)',
                file_count=len(root_wolf_files),
            )

        try:
            entries = os.listdir(path)
        except Exception:
            return None

        wolf_entries = [
            f for f in entries
            if f.lower().endswith(('.mps', '.wolf')) or f in ('CommonEvent.dat', 'Game.dat')
        ]
        evtext_files = glob.glob(os.path.join(path, 'Evtext', '**', '*.txt'), recursive=True)
        if len(wolf_entries) > 0 or len(evtext_files) > 0:
            return DetectionResult(
                detected_path=path,
                score=75,
                extractor=cls.display_name,
                description='WOLF RPG Editor (pasta atual)',
                file_count=len(wolf_entries) + len(evtext_files),
            )

        return None

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
    def is_translatable_file(cls, file_name, file_path=None):
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

        updated = copy_preserving_metadata(data)
        if hasattr(updated, '__dict__'):
            updated._is_dirty = True

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
    def _extract_cmd_item(self, cmd, index):
        cid = cmd.get('code')
        strategy = self._strategies.get(cid)
        if strategy:
            text = strategy.extract(cmd)
            if text:
                return {"id": index, "text": text}
        return {"id": index, "text": []}

    def _insert_cmd_item(self, cmd, list_item):
        cid = cmd.get('code')
        strategy = self._strategies.get(cid)
        if strategy:
            strategy.insert(cmd, list_item.get('text'))

    def extract_text_map(self, data):
        from src.extractor.EventCommandStrategy import extract_commands_tree
        return extract_commands_tree(data, 'Map', self._extract_cmd_item)

    def insert_text_map(self, data, new_data):
        from src.extractor.EventCommandStrategy import insert_commands_tree
        insert_commands_tree(data, new_data, 'Map', self._insert_cmd_item)

    # =========================================================================
    # Eventos Comuns (CommonEvent.dat)
    # =========================================================================

    def extract_text_common_events(self, data):
        from src.extractor.EventCommandStrategy import extract_commands_tree
        return extract_commands_tree(data, 'CommonEvents', self._extract_cmd_item)

    def insert_text_common_events(self, data, new_data):
        from src.extractor.EventCommandStrategy import insert_commands_tree
        insert_commands_tree(data, new_data, 'CommonEvents', self._insert_cmd_item)

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
        type_dict = {
            t.get('id', t_idx): t
            for t_idx, t in enumerate(data.get('types', []))
            if isinstance(t, dict)
        }

        for type_data in new_data:
            t_id = type_data.get('id')
            raw_type = type_dict.get(t_id)
            if not raw_type:
                continue

            datum_dict = {
                d.get('id', d_idx): d
                for d_idx, d in enumerate(raw_type.get('data', []))
                if isinstance(d, dict)
            }
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
                    if str(trans_text).strip() == orig_line.strip():
                        continue
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

    @classmethod
    def _infer_file_name(cls, data):
        if hasattr(data, '_file_name') and getattr(data, '_file_name'):
            return getattr(data, '_file_name')
        return None

    @staticmethod
    def _kind_for_code(raw_code):
        if raw_code == WolfEventCode.SHOW_CHOICES:
            return 'choice'
        if raw_code in (
            WolfEventCode.SET_STRING,
            WolfEventCode.CALL_COMMON_EVENT,
            WolfEventCode.CALL_COMMON_BY_NAME,
        ):
            return 'script'
        return 'dialogue'

    @_ClassOrInstanceMethod
    def _walk_or_apply_segments(target, file_name, data, mode='extract', translations=None):
        from src.extractor.EventCommandStrategy import _resolve_item_by_id, process_event_command_items

        cls = target if isinstance(target, type) else target.__class__
        inst = target._create_segment_instance() if isinstance(target, type) else target
        base_name = os.path.basename(file_name) if file_name else cls._infer_file_name(data)
        if not base_name:
            raise ValueError("extract_segments requires an explicit file_name")
        if not inst.is_translatable_file(base_name):
            return [] if mode == 'extract' else data

        segments = []
        translations = translations or {}

        def _handle_cmd_list(cmd_items, prefix_base, raw_cmd_list):
            return process_event_command_items(
                cmd_items=cmd_items,
                prefix_base=prefix_base,
                base_name=base_name,
                cmd_lookup_fn=lambda cid: raw_cmd_list[cid] if isinstance(raw_cmd_list, list) and cid < len(raw_cmd_list) else {},
                strategy_lookup_fn=lambda code: inst._strategies.get(code),
                kind_lookup_fn=cls._kind_for_code,
                mode=mode,
                translations=translations,
                segments=segments,
            )

        if base_name.endswith('.mps'):
            extracted = inst.extract_text_map(copy.deepcopy(data))
            events_col = data.get('events', []) if isinstance(data, dict) else []
            if mode == 'extract':
                for ev in extracted:
                    ev_id = ev['id']
                    raw_ev = _resolve_item_by_id(events_col, ev_id) or {}
                    raw_pages = raw_ev.get('pages', [])
                    for page in ev['pages']:
                        p_id = page['id']
                        raw_page = _resolve_item_by_id(raw_pages, p_id) or {}
                        _handle_cmd_list(page['list'], f"event:{ev_id}/page:{p_id}", raw_page.get('list', []))
                return segments
            else:
                filtered_events = []
                for ev in extracted:
                    ev_id = ev['id']
                    raw_ev = _resolve_item_by_id(events_col, ev_id) or {}
                    raw_pages = raw_ev.get('pages', [])
                    filtered_pages = []
                    for page in ev['pages']:
                        p_id = page['id']
                        raw_page = _resolve_item_by_id(raw_pages, p_id) or {}
                        mod_list = _handle_cmd_list(page['list'], f"event:{ev_id}/page:{p_id}", raw_page.get('list', []))
                        if mod_list:
                            filtered_pages.append({'id': p_id, 'list': mod_list})
                    if filtered_pages:
                        filtered_events.append({'id': ev_id, 'pages': filtered_pages})
                if filtered_events:
                    inst.insert_text_map(data, filtered_events)
                return data

        if base_name == 'CommonEvent.dat':
            extracted = inst.extract_text_common_events(copy.deepcopy(data))
            if mode == 'extract':
                for ev in extracted:
                    ev_id = ev['id']
                    raw_ev = _resolve_item_by_id(data, ev_id) or {}
                    _handle_cmd_list(ev['list'], f"common_event:{ev_id}", raw_ev.get('list', []))
                return segments
            else:
                filtered_events = []
                for ev in extracted:
                    ev_id = ev['id']
                    raw_ev = _resolve_item_by_id(data, ev_id) or {}
                    mod_list = _handle_cmd_list(ev['list'], f"common_event:{ev_id}", raw_ev.get('list', []))
                    if mod_list:
                        filtered_events.append({'id': ev_id, 'list': mod_list})
                if filtered_events:
                    inst.insert_text_common_events(data, filtered_events)
                return data

        if base_name.endswith('.dat') and base_name != 'Game.dat':
            extracted = inst.extract_text_database(copy.deepcopy(data))
            if mode == 'extract':
                for type_obj in extracted:
                    t_id = type_obj['id']
                    for datum_obj in type_obj['data']:
                        d_id = datum_obj['id']
                        for s_idx_str, val in datum_obj['strings'].items():
                            segments.append(Segment(
                                id=f"{base_name}#type:{t_id}/datum:{d_id}/str:{s_idx_str}",
                                source=val,
                                context={
                                    'file': base_name,
                                    'type_id': t_id,
                                    'datum_id': d_id,
                                    'string_index': int(s_idx_str),
                                    'kind': 'db',
                                },
                            ))
                return segments
            else:
                modified_types = []
                for type_obj in extracted:
                    t_id = type_obj['id']
                    mod_data = []
                    for datum_obj in type_obj['data']:
                        d_id = datum_obj['id']
                        mod_strings = {}
                        for s_idx_str, val in datum_obj['strings'].items():
                            seg_id = f"{base_name}#type:{t_id}/datum:{d_id}/str:{s_idx_str}"
                            new_val = translations.get(seg_id, val)
                            if new_val != val:
                                mod_strings[s_idx_str] = new_val
                        if mod_strings:
                            mod_data.append({'id': d_id, 'strings': mod_strings})
                    if mod_data:
                        modified_types.append({'id': t_id, 'data': mod_data})
                if modified_types:
                    inst.insert_text_database(data, modified_types)
                return data

        if base_name == 'Game.dat':
            extracted = inst.extract_text_gamedat(data)
            if mode == 'extract':
                if extracted and 'title' in extracted:
                    segments.append(Segment(
                        id=f"{base_name}#game/title",
                        source=extracted['title'],
                        context={'file': base_name, 'field': 'title', 'kind': 'menu'},
                    ))
                return segments
            else:
                if extracted and 'title' in extracted:
                    seg_id = f"{base_name}#game/title"
                    new_title = translations.get(seg_id, extracted['title'])
                    if new_title != extracted['title']:
                        inst.insert_text_gamedat(data, {'title': new_title})
                return data

        if base_name.endswith('.txt'):
            extracted = inst.extract_text_evtext(data)
            if mode == 'extract':
                if extracted and 'lines' in extracted:
                    for idx_str, val in extracted['lines'].items():
                        segments.append(Segment(
                            id=f"{base_name}#line:{idx_str}",
                            source=val,
                            context={'file': base_name, 'line': int(idx_str), 'kind': 'dialogue'},
                        ))
                return segments
            else:
                if extracted and 'lines' in extracted:
                    mod_lines = {}
                    for idx_str, val in extracted['lines'].items():
                        seg_id = f"{base_name}#line:{idx_str}"
                        new_val = translations.get(seg_id, val)
                        if new_val != val:
                            mod_lines[idx_str] = new_val
                    if mod_lines:
                        inst.insert_text_evtext(data, {'lines': mod_lines})
                return data

        return [] if mode == 'extract' else data

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
