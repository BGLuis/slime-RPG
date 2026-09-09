import os
import re
import src.utils.RubyMarshal as r_marshal
import rubymarshal.reader as r_reader
import rubymarshal.writer as r_writer
from src.utils.RubyMarshal import RubyObject, RubyString

class RVDataList(list):
    """Subclasse de list para anexar os dados binários originais do Ruby"""
    def __init__(self, *args, **kwargs):
        super().__init__(*args, **kwargs)
        self._raw_rvdata = None

class RVDataDict(dict):
    """Subclasse de dict para anexar os dados binários originais do Ruby"""
    def __init__(self, *args, **kwargs):
        super().__init__(*args, **kwargs)
        self._raw_rvdata = None

class RVDataAdapter:
    """
    Adaptador bidirecional entre arquivos .rvdata2 (Ruby Marshal RGSS3)
    e dicionários/listas normalizados compatíveis com a arquitetura RPG Maker (MV/MZ).
    """

    @staticmethod
    def _str_val(val):
        if val is None:
            return None
        if isinstance(val, RubyString):
            return val.text
        return str(val)

    @staticmethod
    def load_file(file_path):
        """Lê um arquivo .rvdata2 e retorna a estrutura normalizada com _raw_rvdata anexado."""
        with open(file_path, 'rb') as f:
            raw_data = r_marshal.load(f)

        file_name = os.path.basename(file_path)
        normalized = RVDataAdapter.to_normalized(file_name, raw_data)
        normalized._raw_rvdata = raw_data
        return normalized

    @staticmethod
    def save_file(file_path, data, original_raw=None):
        """
        Re-injeta os dados traduzidos na árvore de objetos Ruby original
        e serializa de volta para .rvdata2.
        """
        raw_to_save = original_raw
        if raw_to_save is None and hasattr(data, '_raw_rvdata') and data._raw_rvdata is not None:
            raw_to_save = data._raw_rvdata

        if raw_to_save is None:
            raise ValueError(f'Não foi possível encontrar os dados Ruby originais para salvar {file_path}')

        file_name = os.path.basename(file_path)
        RVDataAdapter.apply_normalized(file_name, raw_to_save, data)

        dir_name = os.path.dirname(file_path)
        if dir_name and not os.path.exists(dir_name):
            os.makedirs(dir_name, exist_ok=True)

        with open(file_path, 'wb') as f:
            r_marshal.write(f, raw_to_save)

    # =========================================================================
    # Conversão RVData2 -> Estrutura Normalizada (Compatível com MV/MZ)
    # =========================================================================

    @staticmethod
    def to_normalized(file_name, raw_data):
        if re.search(r'Map\d{3,}', file_name):
            return RVDataAdapter._map_to_dict(raw_data)
        elif re.search(r'CommonEvents', file_name):
            return RVDataAdapter._commonevents_to_list(raw_data)
        elif re.search(r'Troops', file_name):
            return RVDataAdapter._troops_to_list(raw_data)
        elif re.search(r'MapInfos', file_name):
            return RVDataAdapter._mapinfos_to_list(raw_data)
        elif re.search(r'System', file_name):
            return RVDataAdapter._system_to_dict(raw_data)
        elif re.search(r'(?:Weapons|Items|Skills|States|Enemies|Actors|Armors|Classes)', file_name):
            return RVDataAdapter._objects_to_list(raw_data)
        else:
            # Arquivo desconhecido/genérico
            if isinstance(raw_data, list):
                res = RVDataList(raw_data)
            elif isinstance(raw_data, dict):
                res = RVDataDict(raw_data)
            else:
                res = RVDataDict({'raw': raw_data})
            return res

    @staticmethod
    def _map_to_dict(rv_map):
        events_dict = rv_map.attributes.get('@events', {}) if hasattr(rv_map, 'attributes') else {}
        max_id = max(events_dict.keys()) if events_dict else 0
        events_list = [None] * (max_id + 1)

        for ev_id, ev_obj in events_dict.items():
            if not ev_obj or not hasattr(ev_obj, 'attributes'):
                continue
            pages = []
            for page_idx, page_obj in enumerate(ev_obj.attributes.get('@pages', [])):
                if not page_obj or not hasattr(page_obj, 'attributes'):
                    continue
                cmd_list = []
                for cmd_obj in page_obj.attributes.get('@list', []):
                    if not cmd_obj or not hasattr(cmd_obj, 'attributes'):
                        continue
                    params = [p.text if isinstance(p, RubyString) else p for p in cmd_obj.attributes.get('@parameters', [])]
                    cmd_list.append({
                        'code': cmd_obj.attributes.get('@code', 0),
                        'indent': cmd_obj.attributes.get('@indent', 0),
                        'parameters': params
                    })
                pages.append({'id': page_idx, 'list': cmd_list})

            events_list[ev_id] = {
                'id': ev_id,
                'name': RVDataAdapter._str_val(ev_obj.attributes.get('@name', '')),
                'pages': pages
            }

        res = RVDataDict({
            'displayName': RVDataAdapter._str_val(rv_map.attributes.get('@display_name', '') if hasattr(rv_map, 'attributes') else ''),
            'events': events_list
        })
        return res

    @staticmethod
    def _commonevents_to_list(rv_events):
        res_list = RVDataList()
        if not isinstance(rv_events, list):
            return res_list

        for ev_obj in rv_events:
            if not ev_obj or not hasattr(ev_obj, 'attributes'):
                res_list.append(None)
                continue
            cmd_list = []
            for cmd_obj in ev_obj.attributes.get('@list', []):
                if not cmd_obj or not hasattr(cmd_obj, 'attributes'):
                    continue
                params = [p.text if isinstance(p, RubyString) else p for p in cmd_obj.attributes.get('@parameters', [])]
                cmd_list.append({
                    'code': cmd_obj.attributes.get('@code', 0),
                    'indent': cmd_obj.attributes.get('@indent', 0),
                    'parameters': params
                })
            res_list.append({
                'id': ev_obj.attributes.get('@id', 0),
                'name': RVDataAdapter._str_val(ev_obj.attributes.get('@name', '')),
                'list': cmd_list
            })
        return res_list

    @staticmethod
    def _troops_to_list(rv_troops):
        res_list = RVDataList()
        if not isinstance(rv_troops, list):
            return res_list

        for troop_obj in rv_troops:
            if not troop_obj or not hasattr(troop_obj, 'attributes'):
                res_list.append(None)
                continue
            pages = []
            for page_idx, page_obj in enumerate(troop_obj.attributes.get('@pages', [])):
                if not page_obj or not hasattr(page_obj, 'attributes'):
                    continue
                cmd_list = []
                for cmd_obj in page_obj.attributes.get('@list', []):
                    if not cmd_obj or not hasattr(cmd_obj, 'attributes'):
                        continue
                    params = [p.text if isinstance(p, RubyString) else p for p in cmd_obj.attributes.get('@parameters', [])]
                    cmd_list.append({
                        'code': cmd_obj.attributes.get('@code', 0),
                        'indent': cmd_obj.attributes.get('@indent', 0),
                        'parameters': params
                    })
                pages.append({'id': page_idx, 'list': cmd_list})

            res_list.append({
                'id': troop_obj.attributes.get('@id', 0),
                'name': RVDataAdapter._str_val(troop_obj.attributes.get('@name', '')),
                'pages': pages
            })
        return res_list

    @staticmethod
    def _objects_to_list(rv_objects):
        res_list = RVDataList()
        if not isinstance(rv_objects, list):
            return res_list

        attrs_to_extract = ['name', 'description', 'note', 'nickname', 'profile',
                            'message1', 'message2', 'message3', 'message4']

        for item_obj in rv_objects:
            if not item_obj or not hasattr(item_obj, 'attributes'):
                res_list.append(None)
                continue
            item_dict = {'id': item_obj.attributes.get('@id', 0)}
            for attr in attrs_to_extract:
                ivar = f'@{attr}'
                if ivar in item_obj.attributes:
                    val = item_obj.attributes[ivar]
                    item_dict[attr] = RVDataAdapter._str_val(val)
            res_list.append(item_dict)
        return res_list

    @staticmethod
    def _mapinfos_to_list(rv_mapinfos):
        # MapInfos no VX Ace é um Hash { map_id => RPG::MapInfo }
        if not isinstance(rv_mapinfos, dict):
            return RVDataList()

        max_id = max(rv_mapinfos.keys()) if rv_mapinfos else 0
        res_list = RVDataList([None] * (max_id + 1))

        for map_id, info_obj in rv_mapinfos.items():
            if not info_obj or not hasattr(info_obj, 'attributes'):
                continue
            res_list[map_id] = {
                'id': map_id,
                'name': RVDataAdapter._str_val(info_obj.attributes.get('@name', ''))
            }
        return res_list

    @staticmethod
    def _system_to_dict(rv_system):
        res = RVDataDict()
        if not hasattr(rv_system, 'attributes'):
            return res

        res['gameTitle'] = RVDataAdapter._str_val(rv_system.attributes.get('@game_title', ''))
        res['currencyUnit'] = RVDataAdapter._str_val(rv_system.attributes.get('@currency_unit', ''))

        list_mappings = [
            ('elements', '@elements'),
            ('skillTypes', '@skill_types'),
            ('weaponTypes', '@weapon_types'),
            ('armorTypes', '@armor_types'),
            ('switches', '@switches')
        ]
        for dict_key, ivar in list_mappings:
            raw_list = rv_system.attributes.get(ivar, [])
            res[dict_key] = [RVDataAdapter._str_val(x) for x in raw_list]

        terms_obj = rv_system.attributes.get('@terms')
        if terms_obj and hasattr(terms_obj, 'attributes'):
            res['terms'] = {
                'basic': [RVDataAdapter._str_val(x) for x in terms_obj.attributes.get('@basic', [])],
                'params': [RVDataAdapter._str_val(x) for x in terms_obj.attributes.get('@params', [])],
                'etypes': [RVDataAdapter._str_val(x) for x in terms_obj.attributes.get('@etypes', [])],
                'commands': [RVDataAdapter._str_val(x) for x in terms_obj.attributes.get('@commands', [])],
            }

        return res

    # =========================================================================
    # Re-injeção: Estrutura Normalizada Traduzida -> RVData2 Original
    # =========================================================================

    @staticmethod
    def apply_normalized(file_name, raw_data, translated_data):
        if re.search(r'Map\d{3,}', file_name):
            RVDataAdapter._apply_map(raw_data, translated_data)
        elif re.search(r'CommonEvents', file_name):
            RVDataAdapter._apply_commonevents(raw_data, translated_data)
        elif re.search(r'Troops', file_name):
            RVDataAdapter._apply_troops(raw_data, translated_data)
        elif re.search(r'MapInfos', file_name):
            RVDataAdapter._apply_mapinfos(raw_data, translated_data)
        elif re.search(r'System', file_name):
            RVDataAdapter._apply_system(raw_data, translated_data)
        elif re.search(r'(?:Weapons|Items|Skills|States|Enemies|Actors|Armors|Classes)', file_name):
            RVDataAdapter._apply_objects(raw_data, translated_data)

    @staticmethod
    def _apply_map(rv_map, data_dict):
        if not hasattr(rv_map, 'attributes'):
            return
        if 'displayName' in data_dict and data_dict['displayName'] is not None:
            rv_map.attributes['@display_name'] = data_dict['displayName']

        events_dict = rv_map.attributes.get('@events', {})
        for ev_dict in data_dict.get('events', []):
            if not ev_dict or 'id' not in ev_dict:
                continue
            ev_id = ev_dict['id']
            if ev_id not in events_dict:
                continue
            ev_obj = events_dict[ev_id]
            if not hasattr(ev_obj, 'attributes'):
                continue
            rv_pages = ev_obj.attributes.get('@pages', [])
            for page_dict in ev_dict.get('pages', []):
                page_id = page_dict.get('id', 0)
                if page_id >= len(rv_pages) or not hasattr(rv_pages[page_id], 'attributes'):
                    continue
                page_obj = rv_pages[page_id]
                rv_cmds = page_obj.attributes.get('@list', [])
                for cmd_idx, cmd_dict in enumerate(page_dict.get('list', [])):
                    if cmd_idx < len(rv_cmds) and hasattr(rv_cmds[cmd_idx], 'attributes'):
                        rv_cmds[cmd_idx].attributes['@parameters'] = cmd_dict['parameters']

    @staticmethod
    def _apply_commonevents(rv_events, data_list):
        if not isinstance(rv_events, list):
            return
        for ev_dict in data_list:
            if not ev_dict or 'id' not in ev_dict:
                continue
            ev_id = ev_dict['id']
            if ev_id >= len(rv_events) or not rv_events[ev_id] or not hasattr(rv_events[ev_id], 'attributes'):
                continue
            ev_obj = rv_events[ev_id]
            if 'name' in ev_dict and ev_dict['name'] is not None:
                ev_obj.attributes['@name'] = ev_dict['name']

            rv_cmds = ev_obj.attributes.get('@list', [])
            for cmd_idx, cmd_dict in enumerate(ev_dict.get('list', [])):
                if cmd_idx < len(rv_cmds) and hasattr(rv_cmds[cmd_idx], 'attributes'):
                    rv_cmds[cmd_idx].attributes['@parameters'] = cmd_dict['parameters']

    @staticmethod
    def _apply_troops(rv_troops, data_list):
        if not isinstance(rv_troops, list):
            return
        for troop_dict in data_list:
            if not troop_dict or 'id' not in troop_dict:
                continue
            troop_id = troop_dict['id']
            if troop_id >= len(rv_troops) or not rv_troops[troop_id] or not hasattr(rv_troops[troop_id], 'attributes'):
                continue
            troop_obj = rv_troops[troop_id]
            if 'name' in troop_dict and troop_dict['name'] is not None:
                troop_obj.attributes['@name'] = troop_dict['name']

            rv_pages = troop_obj.attributes.get('@pages', [])
            for page_dict in troop_dict.get('pages', []):
                page_id = page_dict.get('id', 0)
                if page_id >= len(rv_pages) or not hasattr(rv_pages[page_id], 'attributes'):
                    continue
                page_obj = rv_pages[page_id]
                rv_cmds = page_obj.attributes.get('@list', [])
                for cmd_idx, cmd_dict in enumerate(page_dict.get('list', [])):
                    if cmd_idx < len(rv_cmds) and hasattr(rv_cmds[cmd_idx], 'attributes'):
                        rv_cmds[cmd_idx].attributes['@parameters'] = cmd_dict['parameters']

    @staticmethod
    def _apply_objects(rv_objects, data_list):
        if not isinstance(rv_objects, list):
            return
        attrs = ['name', 'description', 'note', 'nickname', 'profile',
                 'message1', 'message2', 'message3', 'message4']
        for item_dict in data_list:
            if not item_dict or 'id' not in item_dict:
                continue
            item_id = item_dict['id']
            if item_id >= len(rv_objects) or not rv_objects[item_id] or not hasattr(rv_objects[item_id], 'attributes'):
                continue
            obj = rv_objects[item_id]
            for attr in attrs:
                if attr in item_dict and item_dict[attr] is not None:
                    obj.attributes[f'@{attr}'] = item_dict[attr]

    @staticmethod
    def _apply_mapinfos(rv_mapinfos, data_list):
        if not isinstance(rv_mapinfos, dict):
            return
        for item_dict in data_list:
            if not item_dict or 'id' not in item_dict:
                continue
            map_id = item_dict['id']
            if map_id in rv_mapinfos and hasattr(rv_mapinfos[map_id], 'attributes'):
                if 'name' in item_dict and item_dict['name'] is not None:
                    rv_mapinfos[map_id].attributes['@name'] = item_dict['name']

    @staticmethod
    def _apply_system(rv_system, data_dict):
        if not hasattr(rv_system, 'attributes'):
            return

        if 'gameTitle' in data_dict and data_dict['gameTitle'] is not None:
            rv_system.attributes['@game_title'] = data_dict['gameTitle']
        if 'currencyUnit' in data_dict and data_dict['currencyUnit'] is not None:
            rv_system.attributes['@currency_unit'] = data_dict['currencyUnit']

        list_mappings = [
            ('elements', '@elements'),
            ('skillTypes', '@skill_types'),
            ('weaponTypes', '@weapon_types'),
            ('armorTypes', '@armor_types'),
            ('switches', '@switches')
        ]
        for dict_key, ivar in list_mappings:
            if dict_key in data_dict and isinstance(data_dict[dict_key], list):
                rv_system.attributes[ivar] = data_dict[dict_key]

        terms_dict = data_dict.get('terms')
        terms_obj = rv_system.attributes.get('@terms')
        if terms_dict and isinstance(terms_dict, dict) and terms_obj and hasattr(terms_obj, 'attributes'):
            for sub_key in ['basic', 'params', 'etypes', 'commands']:
                if sub_key in terms_dict and isinstance(terms_dict[sub_key], list):
                    terms_obj.attributes[f'@{sub_key}'] = terms_dict[sub_key]
