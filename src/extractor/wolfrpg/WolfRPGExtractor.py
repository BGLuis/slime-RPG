import os
import re
import copy
import logging
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
    CommentStrategy,
    CommonEventParamStrategy
)
from .WolfBinaryAdapter import WolfBinaryAdapter
from .WolfArchive import WolfArchive


@register_extractor("Wolf RPG")
class WolfRPGExtractor(BaseExtractor):
    name = "Wolf RPG"
    files_types = ['mps', 'dat', 'project', 'wolf']

    def __init__(self, translate):
        super().__init__(translate)
        self._strategies = {
            WolfEventCode.SHOW_MESSAGE: ShowMessageStrategy(),
            WolfEventCode.SHOW_CHOICES: ChoicesStrategy(),
            WolfEventCode.SET_STRING: SetStringStrategy(),
            WolfEventCode.PICTURE: PictureStrategy(),
            WolfEventCode.COMMENT: CommentStrategy(),
            WolfEventCode.CALL_COMMON_EVENT: CommonEventParamStrategy(),
            WolfEventCode.CALL_COMMON_BY_NAME: CommonEventParamStrategy(),
        }
        self.target_font = None
        self.repack_wolf = False

    @classmethod
    def get_interactive_questions(cls):
        return [
            {
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

        # Se for um arquivo .wolf, descompacta para folderInput
        if file_path.endswith('.wolf'):
            try:
                dest_dir = os.path.join(cls.folderInput, os.path.splitext(file_name)[0])
                WolfArchive.extract_archive(file_path, dest_dir)
                logging.info(f"✓ Extraído arquivo .wolf {file_name} para {dest_dir}")
                return [file_name, None]
            except Exception as e:
                logging.error(f"✗ Falha ao extrair .wolf {file_name}: {e}")
                raise

        # Arquivos binários do Wolf RPG
        if file_path.endswith(('.mps', '.dat', '.project')):
            data = WolfBinaryAdapter.load_file(file_path)
            return [file_name, data]

        return super().extract_files(file_path)

    @classmethod
    def import_file(cls, file_name, json_data, folder):
        dest_path = os.path.join(folder, file_name)
        if file_name.endswith(('.mps', '.dat', '.project')):
            try:
                original_raw = getattr(json_data, '_raw_wolf', None)
                if original_raw is None:
                    for candidate_folder in [cls.folderInput, cls.folderProcess]:
                        candidate = os.path.join(candidate_folder, file_name)
                        if os.path.exists(candidate):
                            loaded = WolfBinaryAdapter.load_file(candidate)
                            original_raw = getattr(loaded, '_raw_wolf', None)
                            break

                WolfBinaryAdapter.save_file(dest_path, json_data, original_raw=original_raw)
                logging.info(f"✓ Validado e salvo binário WOLF RPG: {file_name}")
                return
            except Exception as e:
                logging.error(f"✗ Falha ao salvar binário WOLF RPG {file_name}: {e}")
                raise

        super().import_file(file_name, json_data, folder)

    def extract_text(self, file_name, data):
        if data is None:
            return None

        if file_name.endswith('.mps'):
            return self.extract_text_map(data)
        elif file_name == 'CommonEvent.dat':
            return self.extract_text_common_events(data)
        elif file_name.endswith('.dat') and file_name != 'Game.dat':
            return self.extract_text_database(data)
        elif file_name == 'Game.dat':
            return self.extract_text_gamedat(data)

        return None

    def update_json(self, file_name, data, new_data):
        if not new_data:
            return data

        updated = copy.deepcopy(data)
        if hasattr(data, '_raw_wolf'):
            updated._raw_wolf = data._raw_wolf

        if file_name.endswith('.mps'):
            self.insert_text_map(updated, new_data)
        elif file_name == 'CommonEvent.dat':
            self.insert_text_common_events(updated, new_data)
        elif file_name.endswith('.dat') and file_name != 'Game.dat':
            self.insert_text_database(updated, new_data)
        elif file_name == 'Game.dat':
            self.insert_text_gamedat(updated, new_data)

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

    def extract_text_database(self, data):
        results = []
        for type_idx, type_item in enumerate(data.get('types', [])):
            type_obj = {"id": type_item.get('id', type_idx), "data": []}

            for datum_idx, datum_item in enumerate(type_item.get('data', [])):
                translatable = {}
                for s_idx, val in enumerate(datum_item.get('string_values', [])):
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
