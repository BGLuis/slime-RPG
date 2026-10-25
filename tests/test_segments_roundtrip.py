import codecs
import json
import os
import struct
import pytest
import rubymarshal.writer as r_writer
from rubymarshal.classes import RubyObject, UserDef

from src.extractor.BaseExtractor import Segment
from src.extractor.rpgmaker.RPGMakerExtractor import RPGMakerExtractor
from src.extractor.wolfrpg.WolfRPGExtractor import WolfRPGExtractor
from src.extractor.wolfrpg.WolfBinaryAdapter import (
    WolfMap,
    WolfMapEvent,
    WolfMapPage,
    WolfCommand,
    WolfCommonEvents,
    WolfCommonEvent,
    WolfDatabase,
    WolfDatabaseType,
    WolfDatabaseField,
    WolfDatabaseData,
    WolfGameDat,
)
from src.extractor.wolfrpg.WolfEventCodes import WolfEventCode
from src.extractor.CSVExtractor import CSVExtractor
from src.extractor.JsonExtractor import JsonExtractor


VALID_KINDS = {"dialogue", "choice", "script", "menu", "db"}


def _assert_segment_contracts(extractor, file_path, out_dir):
    file_name, data1 = extractor.extract_files(str(file_path))
    _, data2 = extractor.extract_files(str(file_path))

    segs1 = extractor.extract_segments(file_name, data1)
    segs2 = extractor.extract_segments(file_name, data2)

    assert len(segs1) > 0, f"Expected at least 1 segment from {file_name}"
    assert [s.id for s in segs1] == [s.id for s in segs2]
    assert len({s.id for s in segs1}) == len(segs1), f"Duplicate segment IDs in {file_name}"

    for seg in segs1:
        assert isinstance(seg, Segment)
        assert isinstance(seg.id, str) and seg.id
        assert isinstance(seg.source, str)
        assert seg.context.get("kind") in VALID_KINDS
        assert seg.context.kind in VALID_KINDS

    reconstructed = extractor.apply_segments(extractor.extract_segments(file_name, data1), lambda x: x)
    extractor.import_file(file_name, reconstructed, str(out_dir))

    out_path = os.path.join(str(out_dir), file_name)
    with open(file_path, "rb") as f_in, open(out_path, "rb") as f_out:
        assert f_out.read() == f_in.read(), f"Byte-for-byte mismatch on {file_name}"

    return segs1, data1


def test_rpgmaker_json_roundtrip_and_stable_ids(tmp_path):
    in_dir = tmp_path / "in_rpg_json"
    out_dir = tmp_path / "out_rpg_json"
    in_dir.mkdir()
    out_dir.mkdir()

    extractor = RPGMakerExtractor(None)

    # 1. Map001.json (dialogue, choice, script, plugin, control variable)
    map_data = {
        "displayName": "Starting Town",
        "events": [
            None,
            {
                "id": 1,
                "name": "EV001",
                "pages": [
                    {
                        "list": [
                            {"code": 101, "indent": 0, "parameters": ["Actor1", 0, 0, 2, "Hero"]},
                            {"code": 401, "indent": 0, "parameters": ["<Alice>  Hello \\C[1]world\\C[0]!"]},
                            {"code": 102, "indent": 0, "parameters": [["  Yes  ", "if(s[1]) No", "Cancel"], 1, 0, 2, 0]},
                            {"code": 402, "indent": 0, "parameters": [0, "Yes"]},
                            {"code": 355, "indent": 0, "parameters": ["$gameMessage.add(\"Secret \\\\C[2]message!\");"]},
                            {"code": 356, "indent": 0, "parameters": ["D_TEXT Welcome to the town!"]},
                            {"code": 357, "indent": 0, "parameters": ["MyPlugin", "ShowPopup", "Show", {"text": "Quest started!", "align": "Center"}]},
                            {"code": 122, "indent": 0, "parameters": [1, 1, 0, 4, "'Village Square';"]},
                            {"code": 0, "indent": 0, "parameters": []},
                        ]
                    }
                ],
            },
        ],
    }
    map_path = in_dir / "Map001.json"
    map_path.write_bytes(json.dumps(map_data, ensure_ascii=False, indent=2).encode("utf-8"))

    segs_map, loaded_map = _assert_segment_contracts(extractor, map_path, out_dir)
    kinds_map = {s.context.kind for s in segs_map}
    assert "dialogue" in kinds_map
    assert "choice" in kinds_map
    assert "script" in kinds_map

    # Verify targeted translation via apply_segments
    target_seg = segs_map[0]
    modified_map = extractor.apply_segments(
        "Map001.json",
        loaded_map,
        segs_map,
        {target_seg.id: "Hero Translated"},
    )
    segs_after = extractor.extract_segments("Map001.json", modified_map)
    by_id = {s.id: s.source for s in segs_after}
    assert by_id[target_seg.id] == "Hero Translated"

    # 2. CommonEvents.json
    ce_data = [
        None,
        {
            "id": 1,
            "name": "CE1",
            "list": [
                {"code": 401, "indent": 0, "parameters": ["Common event dialogue line."]},
                {"code": 0, "indent": 0, "parameters": []},
            ],
        },
    ]
    ce_path = in_dir / "CommonEvents.json"
    ce_path.write_bytes(json.dumps(ce_data, ensure_ascii=False).encode("utf-8"))
    _assert_segment_contracts(extractor, ce_path, out_dir)

    # 3. Troops.json
    troops_data = [
        None,
        {
            "id": 1,
            "name": "Slime*2",
            "pages": [
                {
                    "list": [
                        {"code": 401, "indent": 0, "parameters": ["The slime attacks!"]},
                        {"code": 0, "indent": 0, "parameters": []},
                    ]
                }
            ],
        },
    ]
    troops_path = in_dir / "Troops.json"
    troops_path.write_bytes(json.dumps(troops_data, ensure_ascii=False).encode("utf-8"))
    _assert_segment_contracts(extractor, troops_path, out_dir)

    # 4. Actors.json
    actors_data = [
        None,
        {
            "id": 1,
            "name": "Harold",
            "nickname": "The Brave",
            "profile": "A young hero on a journey.",
            "note": "<Desc: A legendary warrior>\n<Unrelated: 123>",
        },
    ]
    actors_path = in_dir / "Actors.json"
    actors_path.write_bytes(json.dumps(actors_data, ensure_ascii=False, separators=(",", ":")).encode("utf-8"))
    segs_actors, _ = _assert_segment_contracts(extractor, actors_path, out_dir)
    assert all(s.context.kind == "db" for s in segs_actors)

    # 5. System.json
    system_data = {
        "gameTitle": "Slime Odyssey",
        "currencyUnit": "Gold",
        "armorTypes": ["", "General Armor", "Magic Armor"],
        "terms": {
            "basic": ["Level", "Lv", "HP", "HP"],
            "commands": ["Fight", "Escape", "Attack", "Guard"],
            "params": ["Max HP", "Max MP"],
            "messages": {"alwaysDash": "Always Dash", "bgmVolume": "BGM Volume"},
        },
    }
    sys_path = in_dir / "System.json"
    sys_path.write_bytes(json.dumps(system_data, ensure_ascii=False, indent=4).encode("utf-8"))
    segs_sys, _ = _assert_segment_contracts(extractor, sys_path, out_dir)
    assert all(s.context.kind == "menu" for s in segs_sys)


def test_rpgmaker_rvdata2_roundtrip_and_stable_ids(tmp_path):
    in_dir = tmp_path / "in_rvdata2"
    out_dir = tmp_path / "out_rvdata2"
    in_dir.mkdir()
    out_dir.mkdir()

    extractor = RPGMakerExtractor(None)

    # Build Map001.rvdata2 with RGSS3 Table + EventCommand
    table = UserDef("Table")
    header = struct.pack("lllll", 2, 2, 2, 1, 4)
    body = struct.pack("4h", 0, 1, 2, 3)
    table._load(header + body)

    cmd1 = RubyObject("RPG::EventCommand")
    cmd1.attributes = {"@code": 401, "@indent": 0, "@parameters": ["Olá aventureiro do VX Ace!"]}
    cmd2 = RubyObject("RPG::EventCommand")
    cmd2.attributes = {"@code": 102, "@indent": 0, "@parameters": [["Aceitar", "Recusar"], 1]}
    cmd_end = RubyObject("RPG::EventCommand")
    cmd_end.attributes = {"@code": 0, "@indent": 0, "@parameters": []}

    page = RubyObject("RPG::Event::Page")
    page.attributes = {"@list": [cmd1, cmd2, cmd_end]}

    event = RubyObject("RPG::Event")
    event.attributes = {"@id": 1, "@name": "EV001", "@pages": [page]}

    rv_map = RubyObject("RPG::Map")
    rv_map.attributes = {"@display_name": "Vila Inicial", "@data": table, "@events": {1: event}}

    map_path = in_dir / "Map001.rvdata2"
    with open(map_path, "wb") as f:
        r_writer.write(f, rv_map)

    segs_rv, _ = _assert_segment_contracts(extractor, map_path, out_dir)
    assert {s.context.kind for s in segs_rv} == {"dialogue", "choice"}

    # Build Actors.rvdata2
    actor = RubyObject("RPG::Actor")
    actor.attributes = {
        "@id": 1,
        "@name": "Eric",
        "@nickname": "Paladino",
        "@description": "Defensor do reino.",
        "@note": "<Help: Cura aliados próximos>",
    }
    actors_path = in_dir / "Actors.rvdata2"
    with open(actors_path, "wb") as f:
        r_writer.write(f, [None, actor])

    segs_actors_rv, _ = _assert_segment_contracts(extractor, actors_path, out_dir)
    assert all(s.context.kind == "db" for s in segs_actors_rv)


def test_wolf_rpg_roundtrip_and_stable_ids(tmp_path):
    in_dir = tmp_path / "in_wolf"
    out_dir = tmp_path / "out_wolf"
    in_dir.mkdir()
    out_dir.mkdir()

    extractor = WolfRPGExtractor(None)
    extractor.folderInput = str(in_dir)
    extractor.folderProcess = str(tmp_path / "proc_wolf")
    extractor.folderOutput = str(out_dir)

    # 1. Map001.mps
    wolf_map = WolfMap()
    wolf_map.encoding_type = 85
    wolf_map.is_utf8 = True
    wolf_map.attributes = 100
    wolf_map.version = 102
    wolf_map.unknown_str = "None"
    wolf_map.tileset_id = 1
    wolf_map.width = 4
    wolf_map.height = 4
    wolf_map.no_tiles = True

    ev = WolfMapEvent(event_id=1)
    ev.name = "Guide"
    page = WolfMapPage(page_id=0)
    page.commands = [
        WolfCommand(cid=WolfEventCode.SHOW_MESSAGE, string_args=["Bem-vindo à masmorra!"]),
        WolfCommand(cid=WolfEventCode.SHOW_CHOICES, string_args=["Entrar na masmorra", "Fugir agora"]),
        WolfCommand(cid=WolfEventCode.SET_STRING, args=[1], string_args=["Missão Iniciada"]),
    ]
    ev.pages = [page]
    wolf_map.events = [ev]

    mps_path = in_dir / "Map001.mps"
    wolf_map.save(str(mps_path))

    segs_mps, _ = _assert_segment_contracts(extractor, mps_path, out_dir)
    assert {s.context.kind for s in segs_mps} == {"dialogue", "choice", "script"}

    from src.extractor.wolfrpg.WolfBinaryAdapter import WolfFileCoder

    # 2. CommonEvent.dat
    ce_obj = WolfCommonEvents()
    ce_obj.magic = WolfCommonEvents.COMMON_MAGIC3
    ce_obj.is_utf8 = True
    ce_obj.wolfversion = 3
    ce = WolfCommonEvent(event_id=0)
    ce.name = "SystemMenu"
    ce.description = "Desc"
    ce.commands = [
        WolfCommand(cid=WolfEventCode.SHOW_MESSAGE, string_args=["Escolha um item para usar:"]),
    ]
    ce.unknown3 = ["param1", "param2", "param3", "param4", "param5", "param6", "param7", "param8", "param9", "param10"]
    ce.unknown4 = [0] * 10
    ce.unknown5 = [["str_sub"] for _ in range(10)]
    ce.unknown6 = [[1] for _ in range(10)]
    ce.unknown7 = b"\x00" * 29
    ce.unknown8 = [""] * 100
    ce.unknown9 = "terminator_str"
    ce_obj.events = [ce]

    ce_path = in_dir / "CommonEvent.dat"
    ce_obj.save(str(ce_path))
    _assert_segment_contracts(extractor, ce_path, out_dir)

    # 3. DataBase.dat + DataBase.project
    db_obj = WolfDatabase()
    db_obj.encoding_type = 85
    db_obj.is_utf8 = True
    db_obj.engine_version = 1
    t = WolfDatabaseType("Itens")
    t.description = "Itens consumíveis"
    f1 = WolfDatabaseField("Nome")
    f1.indexinfo = 0x07D0
    f2 = WolfDatabaseField("Descricao")
    f2.indexinfo = 0x07D1
    t.fields = [f1, f2]
    d = WolfDatabaseData("Item001")
    d.string_values = ["Poção de Cura", "Restaura 50 pontos de HP"]
    d.int_values = []
    t.data = [d]
    db_obj.types = [t]

    db_path = in_dir / "DataBase.dat"
    proj_path = in_dir / "DataBase.project"
    db_obj.save(str(db_path), str(proj_path))
    segs_db, _ = _assert_segment_contracts(extractor, db_path, out_dir)
    assert all(s.context.kind == "db" for s in segs_db)

    # 4. Game.dat
    coder = WolfFileCoder.open_write(is_utf8=True)
    coder.write(WolfGameDat.GAMEDAT_MAGIC)
    coder.write_u1(85)
    coder.write_u4(21)
    coder.write(b"\x00" * 21)
    coder.write_u4(7)
    coder.write_string("O Castelo Sombrio")
    coder.write_string("0000-0000")
    coder.write_u4(0)
    coder.write_string("MS Gothic")
    coder.write_string("MS UI Gothic")
    coder.write_string("Arial")
    coder.write_string("Tahoma")
    gdat_path = in_dir / "Game.dat"
    gdat_path.write_bytes(coder.getvalue())
    segs_gdat, _ = _assert_segment_contracts(extractor, gdat_path, out_dir)
    assert all(s.context.kind == "menu" for s in segs_gdat)

    # 5. Evtext script (.txt)
    evtext_path = in_dir / "scene01.txt"
    evtext_path.write_bytes("  Bem-vindo ao castelo!  \r\n// Comentário interno\r\nGuarda：Quem vem lá?\r\n".encode("utf-8"))
    _assert_segment_contracts(extractor, evtext_path, out_dir)


def test_csv_roundtrip_and_stable_ids(tmp_path):
    in_dir = tmp_path / "in_csv"
    out_dir = tmp_path / "out_csv"
    in_dir.mkdir()
    out_dir.mkdir()

    extractor = CSVExtractor(None)

    # 1. Semicolon + UTF-8 BOM + CRLF CSV
    csv1_path = in_dir / "dialogue.csv"
    raw_csv1 = codecs.BOM_UTF8 + "id;text;notes\r\n1;Hello hero!;intro\r\n2;Watch out for slimes!;battle\r\n".encode("utf-8")
    csv1_path.write_bytes(raw_csv1)
    segs_csv1, data_csv1 = _assert_segment_contracts(extractor, csv1_path, out_dir)
    assert [s.id for s in segs_csv1] == ["dialogue.csv#row:0/col:text", "dialogue.csv#row:1/col:text"]

    # Test non-identity translation on CSV
    updated_csv = extractor.apply_segments(
        "dialogue.csv",
        data_csv1,
        segs_csv1,
        {"dialogue.csv#row:0/col:text": "Olá herói!"},
    )
    assert updated_csv[0]["text"] == "Olá herói!"
    assert updated_csv[1]["text"] == "Watch out for slimes!"

    # 2. Multilingual CSV with empty target column
    csv2_path = in_dir / "strings.csv"
    raw_csv2 = "key,en,pt\nmenu_start,Start Game,\nmenu_quit,Quit Game,\n".encode("utf-8")
    csv2_path.write_bytes(raw_csv2)
    _assert_segment_contracts(extractor, csv2_path, out_dir)


def test_json_extractor_roundtrip_and_stable_ids(tmp_path):
    in_dir = tmp_path / "in_json"
    out_dir = tmp_path / "out_json"
    in_dir.mkdir()
    out_dir.mkdir()

    extractor = JsonExtractor(None)

    json_payload = {
        "ui": {
            "title": "Dungeon Explorer",
            "icon": "assets/icon.png",
        },
        "dialogues": [
            {"id": "d1", "text": "Press Start to begin your adventure."},
            {"id": "d2", "message": "Your party has been fully healed!"},
        ],
    }
    json_path = in_dir / "ui_strings.json"
    # Pretty-printed with trailing newline to ensure byte-for-byte preservation on identity
    json_path.write_bytes((json.dumps(json_payload, indent=2, ensure_ascii=False) + "\n").encode("utf-8"))

    segs_json, data_json = _assert_segment_contracts(extractor, json_path, out_dir)
    expected_ids = [
        "ui_strings.json#/ui/title",
        "ui_strings.json#/dialogues/0/text",
        "ui_strings.json#/dialogues/1/message",
    ]
    assert [s.id for s in segs_json] == expected_ids

    # Non-identity translation
    updated_json = extractor.apply_segments(
        "ui_strings.json",
        data_json,
        segs_json,
        {"ui_strings.json#/ui/title": "Explorador de Masmorras"},
    )
    assert updated_json["ui"]["title"] == "Explorador de Masmorras"
    assert updated_json["dialogues"][0]["text"] == "Press Start to begin your adventure."


def test_sparse_and_non_contiguous_ids_roundtrip():
    """
    Garante que extract_segments e apply_segments funcionam quando:
    - O índice da lista de eventos/atores/tropas não coincide com o campo 'id' (ex: arrays compactados ou dicts por chave string).
    - Páginas do WOLF RPG ou tipos/dados do banco não possuem chave 'id' explícita (usam índice posicional).
    """
    rpg = RPGMakerExtractor(None)

    # 1. RPG Maker Map com evento de id=5 na posição 1 e dict de eventos com chave diferente de id
    sparse_map = {
        "events": [
            None,
            {
                "id": 5,
                "pages": [
                    {
                        "list": [
                            {"code": 102, "indent": 0, "parameters": [["Yes", "No"], 1, 0, 2, 0]},
                            {"code": 356, "indent": 0, "parameters": ["D_TEXT Sparse event!"]},
                        ]
                    }
                ],
            },
        ]
    }
    segs_map = rpg.extract_segments("Map001.json", sparse_map)
    assert [s.context.kind for s in segs_map] == ["choice", "choice", "script"]
    updated_map = rpg.apply_segments(
        "Map001.json",
        sparse_map,
        segs_map,
        {"Map001.json#event:5/page:0/cmd:0/0": "Sim", "Map001.json#event:5/page:0/cmd:1": "Evento esparso!"},
    )
    assert updated_map["events"][1]["pages"][0]["list"][0]["parameters"][0][0] == "Sim"
    assert updated_map["events"][1]["pages"][0]["list"][1]["parameters"][0] == "D_TEXT Evento esparso!"

    # 2. RPG Maker CommonEvents e Troops com id != índice
    sparse_ce = [None, {"id": 7, "list": [{"code": 102, "indent": 0, "parameters": [["Go", "Stay"], 1]}]}]
    segs_ce = rpg.extract_segments("CommonEvents.json", sparse_ce)
    assert segs_ce[0].context.kind == "choice"
    updated_ce = rpg.apply_segments("CommonEvents.json", sparse_ce, {segs_ce[0].id: "Ir"})
    assert updated_ce[1]["list"][0]["parameters"][0][0] == "Ir"

    sparse_troops = [
        None,
        {"id": 9, "pages": [{"list": [{"code": 355, "indent": 0, "parameters": ["$gameMessage.add('Boss!');"]}]}]},
    ]
    segs_tr = rpg.extract_segments("Troops.json", sparse_troops)
    assert segs_tr[0].context.kind == "script"
    updated_tr = rpg.apply_segments("Troops.json", sparse_troops, {segs_tr[0].id: "Chefe!"})
    assert "Chefe!" in updated_tr[1]["pages"][0]["list"][0]["parameters"][0]

    # 3. RPG Maker Actors com id != índice
    sparse_actors = [None, {"id": 5, "name": "Harold", "nickname": "The Brave"}]
    segs_act = rpg.extract_segments("Actors.json", sparse_actors)
    updated_act = rpg.apply_segments("Actors.json", sparse_actors, {segs_act[0].id: "Haroldo"})
    assert updated_act[1]["name"] == "Haroldo"

    # 4. WOLF RPG Map com página sem chave 'id' explícita e DataBase sem 'id' explícito
    wolf = WolfRPGExtractor(None)
    wolf_map = {
        "events": [
            None,
            {
                "id": 10,
                "pages": [
                    {
                        "list": [
                            {"code": WolfEventCode.SHOW_CHOICES, "string_args": ["Attack now", "Defend now"]},
                        ]
                    }
                ],
            },
        ]
    }
    segs_wmap = wolf.extract_segments("Map001.mps", wolf_map)
    assert [s.context.kind for s in segs_wmap] == ["choice", "choice"]
    updated_wmap = wolf.apply_segments("Map001.mps", wolf_map, {segs_wmap[0].id: "Atacar agora"})
    assert updated_wmap["events"][1]["pages"][0]["list"][0]["string_args"][0] == "Atacar agora"

    wolf_db = {
        "types": [
            {
                "name": "Items",
                "data": [
                    {
                        "name": "Potion",
                        "string_values": ["Potion", "Heals 50 HP"],
                    }
                ],
            }
        ]
    }
    segs_wdb = wolf.extract_segments("DataBase.dat", wolf_db)
    assert len(segs_wdb) == 1
    assert segs_wdb[0].context.kind == "db"
    updated_wdb = wolf.apply_segments("DataBase.dat", wolf_db, {segs_wdb[0].id: "Cura 50 HP"})
    assert updated_wdb["types"][0]["data"][0]["string_values"][1] == "Cura 50 HP"

