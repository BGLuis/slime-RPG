import os
import pytest
from src.extractor.wolfrpg.WolfBinaryAdapter import (
    WolfBinaryAdapter,
    WolfFileCoder,
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
    WolfGameDat
)
from src.extractor.wolfrpg.WolfEventCodes import WolfEventCode


def test_wolf_map_roundtrip(tmp_path):
    map_path = tmp_path / "Map001.mps"

    # 1. Construir mapa sintético
    wolf_map = WolfMap()
    wolf_map.encoding_type = 85  # UTF-8
    wolf_map.is_utf8 = True
    wolf_map.attributes = 100
    wolf_map.version = 102
    wolf_map.unknown_str = "None"
    wolf_map.tileset_id = 1
    wolf_map.width = 10
    wolf_map.height = 10
    wolf_map.no_tiles = True

    ev = WolfMapEvent(event_id=1)
    ev.name = "NPC_Guide"
    ev.x = 5
    ev.y = 5

    page = WolfMapPage(page_id=0)
    cmd1 = WolfCommand(cid=WolfEventCode.SHOW_MESSAGE, string_args=["Bem-vindo ao vilarejo!"])
    cmd2 = WolfCommand(cid=WolfEventCode.SHOW_CHOICES, string_args=["Comprar", "Sair"])
    cmd3 = WolfCommand(cid=WolfEventCode.SET_STRING, args=[1], string_args=["Missão Ativa"])

    page.commands = [cmd1, cmd2, cmd3]
    ev.pages = [page]
    wolf_map.events = [ev]

    wolf_map.save(str(map_path))
    assert map_path.exists()

    # 2. Carregar via WolfBinaryAdapter
    normalized = WolfBinaryAdapter.load_file(str(map_path))
    assert "events" in normalized
    assert len(normalized["events"]) > 1
    npc_ev = normalized["events"][1]
    assert npc_ev["name"] == "NPC_Guide"

    cmds = npc_ev["pages"][0]["list"]
    assert len(cmds) == 3
    assert cmds[0]["string_args"] == ["Bem-vindo ao vilarejo!"]
    assert cmds[1]["string_args"] == ["Comprar", "Sair"]

    # 3. Modificar textos (simulando tradução)
    cmds[0]["string_args"] = ["Welcome to the village!"]
    cmds[1]["string_args"] = ["Buy", "Exit"]
    cmds[2]["string_args"] = ["Active Quest"]

    # 4. Salvar via WolfBinaryAdapter
    WolfBinaryAdapter.save_file(str(map_path), normalized)

    # 5. Recarregar e verificar integridade
    reloaded = WolfBinaryAdapter.load_file(str(map_path))
    reloaded_cmds = reloaded["events"][1]["pages"][0]["list"]
    assert reloaded_cmds[0]["string_args"] == ["Welcome to the village!"]
    assert reloaded_cmds[1]["string_args"] == ["Buy", "Exit"]
    assert reloaded_cmds[2]["string_args"] == ["Active Quest"]


def test_wolf_common_events_roundtrip(tmp_path):
    ce_path = tmp_path / "CommonEvent.dat"

    # 1. Construir CommonEvent.dat sintético
    ce_obj = WolfCommonEvents()
    ce_obj.magic = WolfCommonEvents.COMMON_MAGIC3
    ce_obj.is_utf8 = True
    ce_obj.wolfversion = 3

    ev = WolfCommonEvent(event_id=0)
    ev.name = "Menu_Principal"
    ev.description = "Gerencia a exibição do menu"

    cmd1 = WolfCommand(cid=WolfEventCode.SHOW_MESSAGE, string_args=["Selecione uma ação:"])
    cmd2 = WolfCommand(cid=WolfEventCode.CALL_COMMON_EVENT, string_args=["Abrir Inventário"])
    ev.commands = [cmd1, cmd2]

    # Preencher metadados padrão do CommonEvent
    ev.unknown3 = ["param1", "param2", "param3", "param4", "param5", "param6", "param7", "param8", "param9", "param10"]
    ev.unknown4 = [0] * 10
    ev.unknown5 = [["str_sub"] for _ in range(10)]
    ev.unknown6 = [[1] for _ in range(10)]
    ev.unknown7 = b'\x00' * 29
    ev.unknown8 = [""] * 100
    ev.unknown9 = "terminator_str"

    ce_obj.events = [ev]
    ce_obj.save(str(ce_path))
    assert ce_path.exists()

    # 2. Carregar via WolfBinaryAdapter
    normalized = WolfBinaryAdapter.load_file(str(ce_path))
    assert len(normalized) == 1
    assert normalized[0]["name"] == "Menu_Principal"
    assert normalized[0]["list"][0]["string_args"] == ["Selecione uma ação:"]

    # 3. Modificar textos
    normalized[0]["list"][0]["string_args"] = ["Select an action:"]
    normalized[0]["list"][1]["string_args"] = ["Open Inventory"]

    # 4. Salvar
    WolfBinaryAdapter.save_file(str(ce_path), normalized)

    # 5. Recarregar e verificar
    reloaded = WolfBinaryAdapter.load_file(str(ce_path))
    assert reloaded[0]["list"][0]["string_args"] == ["Select an action:"]
    assert reloaded[0]["list"][1]["string_args"] == ["Open Inventory"]


def test_wolf_database_roundtrip(tmp_path):
    dat_path = tmp_path / "UserDatabase.dat"
    project_path = tmp_path / "UserDatabase.project"

    # 1. Construir banco de dados sintético
    db = WolfDatabase()
    db.encoding_type = 85
    db.is_utf8 = True
    db.engine_version = 1

    t = WolfDatabaseType("Itens")
    t.description = "Itens consumíveis"

    fld_name = WolfDatabaseField("Nome")
    fld_name.indexinfo = 0x07D0  # String index 0
    fld_desc = WolfDatabaseField("Descricao")
    fld_desc.indexinfo = 0x07D1  # String index 1
    fld_price = WolfDatabaseField("Preco")
    fld_price.indexinfo = 0x03E8  # Int index 0

    t.fields = [fld_name, fld_desc, fld_price]

    datum = WolfDatabaseData("Item001")
    datum.string_values = ["Poção de Cura", "Restaura 50 pontos de HP"]
    datum.int_values = [100]

    t.data = [datum]
    db.types = [t]

    db.save(str(dat_path), str(project_path))
    assert dat_path.exists()
    assert project_path.exists()

    # 2. Carregar via WolfBinaryAdapter
    normalized = WolfBinaryAdapter.load_file(str(dat_path))
    assert "types" in normalized
    assert normalized["types"][0]["name"] == "Itens"
    assert normalized["types"][0]["data"][0]["string_values"] == ["Poção de Cura", "Restaura 50 pontos de HP"]

    # 3. Modificar
    normalized["types"][0]["data"][0]["string_values"] = ["Health Potion", "Restores 50 HP"]
    WolfBinaryAdapter.save_file(str(dat_path), normalized)

    # 4. Recarregar e verificar
    reloaded = WolfBinaryAdapter.load_file(str(dat_path))
    assert reloaded["types"][0]["data"][0]["string_values"] == ["Health Potion", "Restores 50 HP"]


def test_wolf_gamedat_roundtrip(tmp_path):
    gamedat_path = tmp_path / "Game.dat"

    # Construir Game.dat sintético mínimo
    coder = WolfFileCoder.open_write(is_utf8=True)
    coder.write(WolfGameDat.GAMEDAT_MAGIC)
    coder.write_u1(85)  # Unicode

    # ByteSettings (u1_count = 21)
    coder.write_u4(21)
    coder.write(b'\x00' * 21)

    # StringSettings
    coder.write_u4(7)
    coder.write_string("O Castelo Sombrio")  # title
    coder.write_string("0000-0000")          # serial
    coder.write_u4(0)                        # encryption key len
    coder.write_string("MS Gothic")          # font
    coder.write_string("MS UI Gothic")       # subfont 1
    coder.write_string("Arial")              # subfont 2
    coder.write_string("Tahoma")             # subfont 3

    # Salvar
    with open(str(gamedat_path), 'wb') as f:
        f.write(coder.getvalue())

    # Carregar via WolfBinaryAdapter
    normalized = WolfBinaryAdapter.load_file(str(gamedat_path))
    assert normalized["title"] == "O Castelo Sombrio"
    assert normalized["font"] == "MS Gothic"

    # Modificar título
    normalized["title"] = "The Dark Castle"
    normalized._raw_wolf.title = "The Dark Castle"
    WolfBinaryAdapter.save_file(str(gamedat_path), normalized)

    # Recarregar
    reloaded = WolfBinaryAdapter.load_file(str(gamedat_path))
    assert reloaded["title"] == "The Dark Castle"


def test_wolf_database_never_modifies_project_on_save(tmp_path):
    dat_path = tmp_path / "CDataBase.dat"
    project_path = tmp_path / "CDataBase.project"

    # 1. Cria DB sintético com tipo específico
    db = WolfDatabase()
    db.encoding_type = 85
    db.is_utf8 = True
    db.engine_version = 1

    t = WolfDatabaseType("基本システム用変数")
    t.description = "Variáveis do sistema básico"
    fld = WolfDatabaseField("Var0")
    fld.indexinfo = 0x07D0
    t.fields = [fld]
    datum = WolfDatabaseData("Data0")
    datum.string_values = ["Original Value"]
    datum.int_values = [0]
    t.data = [datum]
    db.types = [t]

    db.save(str(dat_path), str(project_path))
    orig_project_bytes = project_path.read_bytes()

    # 2. Carrega via adapter
    normalized = WolfBinaryAdapter.load_file(str(dat_path))
    assert normalized["types"][0]["name"] == "基本システム用変数"

    # 3. Altera dados e salva via adapter
    normalized["types"][0]["data"][0]["string_values"] = ["Translated Value"]
    WolfBinaryAdapter.save_file(str(dat_path), normalized)

    # 4. Verifica que .project NUNCA foi modificado no save
    assert project_path.read_bytes() == orig_project_bytes

    # 5. Verifica que o .dat foi atualizado com sucesso
    reloaded = WolfBinaryAdapter.load_file(str(dat_path))
    assert reloaded["types"][0]["data"][0]["string_values"] == ["Translated Value"]


def test_wolf_database_resolves_project_from_input_folder(tmp_path):
    input_basic = tmp_path / "input" / "BasicData"
    process_basic = tmp_path / "process" / "BasicData"
    output_basic = tmp_path / "output" / "BasicData"
    input_basic.mkdir(parents=True)
    process_basic.mkdir(parents=True)
    output_basic.mkdir(parents=True)

    input_dat = input_basic / "CDataBase.dat"
    input_project = input_basic / "CDataBase.project"

    # 1. Cria DB sintético em input com tipo fundamental
    db = WolfDatabase()
    db.encoding_type = 85
    db.is_utf8 = True
    db.engine_version = 1

    t = WolfDatabaseType("基本システム用変数")
    t.description = "Variáveis do sistema básico"
    fld = WolfDatabaseField("Var0")
    fld.indexinfo = 0x07D0
    t.fields = [fld]
    datum = WolfDatabaseData("Data0")
    datum.string_values = ["Val"]
    datum.int_values = [0]
    t.data = [datum]
    db.types = [t]

    db.save(str(input_dat), str(input_project))

    # 2. Copia .dat para process/ (sem .project em process/)
    process_dat = process_basic / "CDataBase.dat"
    process_dat.write_bytes(input_dat.read_bytes())

    # 3. Copia .project original para output/ (como _handle_no_text faz)
    output_project = output_basic / "CDataBase.project"
    output_project.write_bytes(input_project.read_bytes())
    original_project_bytes = output_project.read_bytes()

    # 4. Carrega a partir de process/ (onde não há .project adjacente)
    # Deve localizar automaticamente o .project em input/
    loaded_process = WolfBinaryAdapter.load_file(str(process_dat))
    assert loaded_process["types"][0]["name"] == "基本システム用変数"
    assert not loaded_process["types"][0]["name"].startswith("Type_")

    # 5. Salva em output/
    output_dat = output_basic / "CDataBase.dat"
    WolfBinaryAdapter.save_file(str(output_dat), loaded_process)

    # 6. Garante que output/BasicData/CDataBase.project permaneceu 100% idêntico ao original
    assert output_project.read_bytes() == original_project_bytes

