import os
import io
import re
import struct
import logging
from typing import Any, Dict, List, Optional, Tuple, Union

from .WolfEventCodes import WolfEventCode
from src.utils.TextsUtils import normalize_western_chars

# MSVC LCG constants for WOLF RPG decryption (matches msvcrt.dll)
RAND_MULTIPLIER = 0x343FD
RAND_INCREMENT = 0x269EC3
RAND_MAX = 0x7FFF
MAX_INT32 = 0x7FFFFFFF


class MSVCRandom:
    """Implementação pura do gerador de números pseudo-aleatórios da MSVCRT (Windows)."""
    def __init__(self, seed: int = 0):
        self.seed = seed & MAX_INT32

    def srand(self, seed: int):
        self.seed = seed & MAX_INT32

    def rand(self, mask: int = RAND_MAX) -> int:
        self.seed = (self.seed * RAND_MULTIPLIER + RAND_INCREMENT) & MAX_INT32
        return (self.seed >> 16) & mask


def decrypt_dat_v1(data: bytearray, seeds: List[int], intervals: List[int]) -> bytearray:
    """Descriptografa arquivo .dat do Wolf RPG v1/v2 utilizando os seeds do cabeçalho."""
    rng = MSVCRandom()
    for i, seed in enumerate(seeds):
        rng.srand(seed)
        step = intervals[i] if i < len(intervals) else 1
        for j in range(0, len(data), step):
            data[j] ^= (rng.rand(0xFFFF) >> 12) & 0xFF
    return data


class WolfDataList(list):
    """Subclasse de list para anexar os dados binários originais do Wolf RPG"""
    def __init__(self, *args, **kwargs):
        super().__init__(*args, **kwargs)
        self._raw_wolf = None


class WolfDataDict(dict):
    """Subclasse de dict para anexar os dados binários originais do Wolf RPG"""
    def __init__(self, *args, **kwargs):
        super().__init__(*args, **kwargs)
        self._raw_wolf = None


class WolfFileCoder:
    """
    Stream de leitura e gravação binária para arquivos do Wolf RPG.
    Lida com endianness, inteiros compactados e codificação de strings (CP932 / UTF-8).
    """
    _p_u1 = struct.Struct('B')
    _p_u2_le = struct.Struct('<H')
    _p_u4_le = struct.Struct('<I')

    CRYPT_HEADER_SIZE = 10
    DECRYPT_INTERVALS = [1, 2, 5]

    def __init__(self, stream: io.BytesIO, is_utf8: bool = False, filename: str = ""):
        self.stream = stream
        self.is_utf8 = is_utf8
        self.filename = filename

    @classmethod
    def open_read(cls, file_path_or_bytes: Union[str, bytes, bytearray], is_utf8: bool = False) -> 'WolfFileCoder':
        if isinstance(file_path_or_bytes, (bytes, bytearray)):
            stream = io.BytesIO(file_path_or_bytes)
            name = ""
        else:
            with open(file_path_or_bytes, 'rb') as f:
                data = f.read()
            stream = io.BytesIO(data)
            name = os.path.basename(file_path_or_bytes)
        return cls(stream, is_utf8=is_utf8, filename=name)

    @classmethod
    def open_write(cls, is_utf8: bool = False, filename: str = "") -> 'WolfFileCoder':
        return cls(io.BytesIO(), is_utf8=is_utf8, filename=filename)

    def getvalue(self) -> bytes:
        return self.stream.getvalue()

    def tell(self) -> int:
        return self.stream.tell()

    def seek(self, offset: int, whence: int = os.SEEK_SET):
        self.stream.seek(offset, whence)

    def skip(self, size: int):
        self.stream.seek(size, os.SEEK_CUR)

    @property
    def eof(self) -> bool:
        pos = self.stream.tell()
        self.stream.seek(0, os.SEEK_END)
        end_pos = self.stream.tell()
        self.stream.seek(pos, os.SEEK_SET)
        return pos >= end_pos

    def filesize(self) -> int:
        pos = self.stream.tell()
        self.stream.seek(0, os.SEEK_END)
        size = self.stream.tell()
        self.stream.seek(pos, os.SEEK_SET)
        return size

    # Leitura
    def read(self, size: Optional[int] = None) -> bytes:
        if size is None:
            return self.stream.read()
        data = self.stream.read(size)
        if len(data) != size:
            raise EOFError(f"Esperado {size} bytes, obtido {len(data)}")
        return data

    def read_u1(self) -> int:
        return self._p_u1.unpack(self.read(1))[0]

    def read_u2(self) -> int:
        return self._p_u2_le.unpack(self.read(2))[0]

    def read_u4(self) -> int:
        return self._p_u4_le.unpack(self.read(4))[0]

    def read_string(self) -> str:
        size = self.read_u4()
        if size == 0:
            return ""
        if size > 1_000_000:
            raise ValueError(f"Tamanho improvável para string no Wolf RPG: {size}")

        bstr = b""
        if size > 1:
            bstr = self.read(size - 1)
        last = self.read_u1()
        if last != 0:
            raise ValueError(f"String não finalizada em zero (último byte: {last})")

        encoding = 'utf-8' if self.is_utf8 else 'cp932'
        try:
            return bstr.decode(encoding)
        except UnicodeDecodeError:
            alt = 'cp932' if self.is_utf8 else 'utf-8'
            try:
                return bstr.decode(alt)
            except UnicodeDecodeError:
                return bstr.decode(encoding, errors='replace')

    def read_byte_array(self, count: Optional[int] = None) -> List[int]:
        if count is None:
            count = self.read_u4()
        return [self.read_u1() for _ in range(count)]

    def read_word_array(self, count: Optional[int] = None) -> List[int]:
        if count is None:
            count = self.read_u4()
        return [self.read_u2() for _ in range(count)]

    def read_int_array(self, count: Optional[int] = None) -> List[int]:
        if count is None:
            count = self.read_u4()
        return [self.read_u4() for _ in range(count)]

    def read_string_array(self, count: Optional[int] = None) -> List[str]:
        if count is None:
            count = self.read_u4()
        return [self.read_string() for _ in range(count)]

    def verify(self, expected: bytes):
        actual = self.read(len(expected))
        if actual != expected:
            self.stream.seek(-len(expected), os.SEEK_CUR)
            raise ValueError(f"Verificação de cabeçalho falhou: esperado {expected!r}, obtido {actual!r}")

    # Escrita
    def write(self, data: bytes):
        if data:
            self.stream.write(data)

    def write_u1(self, val: int):
        self.stream.write(self._p_u1.pack(val & 0xFF))

    def write_u2(self, val: int):
        self.stream.write(self._p_u2_le.pack(val & 0xFFFF))

    def write_u4(self, val: int):
        self.stream.write(self._p_u4_le.pack(val & 0xFFFFFFFF))

    def write_terminator(self, val: int = 0):
        self.write_u1(val)

    def write_string(self, text: Optional[str]):
        if text is None:
            text = ""
        if not self.is_utf8:
            text = normalize_western_chars(text)
            try:
                bstr = text.encode('cp932')
            except UnicodeEncodeError:
                bstr = text.encode('cp932', errors='replace')
        else:
            bstr = text.encode('utf-8', errors='replace')

        self.write_u4(len(bstr) + 1)
        self.write(bstr)
        self.write_terminator(0)

    def write_byte_array(self, arr: List[int], with_length: bool = True):
        if with_length:
            self.write_u4(len(arr))
        for b in arr:
            self.write_u1(b)

    def write_word_array(self, arr: List[int], with_length: bool = True):
        if with_length:
            self.write_u4(len(arr))
        for w in arr:
            self.write_u2(w)

    def write_int_array(self, arr: List[int], with_length: bool = True):
        if with_length:
            self.write_u4(len(arr))
        for i in arr:
            self.write_u4(i)

    def write_string_array(self, arr: List[str], with_length: bool = True):
        if with_length:
            self.write_u4(len(arr))
        for s in arr:
            self.write_string(s)


class WolfRouteCommand:
    """Comando individual de rota de movimento no Wolf RPG."""
    TERMINATOR = b'\x01\x00'

    def __init__(self, cmd_id: int = 0, args: Optional[List[int]] = None):
        self.id = cmd_id
        self.args = args or []

    @classmethod
    def read(cls, coder: WolfFileCoder) -> 'WolfRouteCommand':
        cmd_id = coder.read_u1()
        args_len = coder.read_u1()
        args = [coder.read_u4() for _ in range(args_len)]
        coder.verify(cls.TERMINATOR)
        return cls(cmd_id, args)

    def write(self, coder: WolfFileCoder):
        coder.write_u1(self.id)
        coder.write_u1(len(self.args))
        for arg in self.args:
            coder.write_u4(arg)
        coder.write(self.TERMINATOR)


class WolfCommand:
    """Comando de evento do Wolf RPG (usado tanto em Mapas quanto em CommonEvents)."""
    def __init__(self, cid: int = 0, args: Optional[List[int]] = None,
                 string_args: Optional[List[str]] = None, indent: int = 0,
                 move_data: Optional[Dict[str, Any]] = None):
        self.cid = cid
        self.args = args or []
        self.string_args = string_args or []
        self.indent = indent
        self.move_data = move_data  # Dados extras para comando de rota 201 (Move)

    @classmethod
    def read(cls, coder: WolfFileCoder) -> 'WolfCommand':
        args_len = coder.read_u1() - 1
        cid = coder.read_u4()
        args = [coder.read_u4() for _ in range(max(0, args_len))]
        indent = coder.read_u1()
        string_args_len = coder.read_u1()
        string_args = [coder.read_string() for _ in range(string_args_len)]

        terminator = coder.read_u1()
        move_data = None
        if terminator == 1:
            unknown = [coder.read_u1() for _ in range(5)]
            flags = coder.read_u1()
            route_len = coder.read_u4()
            route = [WolfRouteCommand.read(coder) for _ in range(route_len)]
            move_data = {'unknown': unknown, 'flags': flags, 'route': route}
        elif terminator != 0:
            raise ValueError(f"Terminador inesperado no comando: {terminator}")

        return cls(cid, args, string_args, indent, move_data)

    def write(self, coder: WolfFileCoder):
        coder.write_u1(len(self.args) + 1)
        coder.write_u4(self.cid)
        for a in self.args:
            coder.write_u4(a)
        coder.write_u1(self.indent)
        coder.write_u1(len(self.string_args))
        for s in self.string_args:
            coder.write_string(s)

        if self.move_data:
            coder.write_u1(1)
            for b in self.move_data.get('unknown', [0]*5):
                coder.write_u1(b)
            coder.write_u1(self.move_data.get('flags', 0))
            route = self.move_data.get('route', [])
            coder.write_u4(len(route))
            for pt in route:
                pt.write(coder)
        else:
            coder.write_terminator(0)


class WolfMapPage:
    """Página de evento de mapa no Wolf RPG."""
    PAGE_TERMINATOR = 0x7A

    def __init__(self, page_id: int):
        self.id = page_id
        self.unknown1 = 0
        self.graphic_name = ""
        self.graphic_direction = 0
        self.graphic_frame = 0
        self.graphic_opacity = 0
        self.graphic_render_mode = 0
        self.conditions = b'\x00' * 37
        self.movement = b'\x00' * 4
        self.flags = 0
        self.route_flags = 0
        self.route: List[WolfRouteCommand] = []
        self.commands: List[WolfCommand] = []
        self.features = 0
        self.shadow_graphic_num = 0
        self.collision_width = 0
        self.collision_height = 0
        self.page_transfer: Optional[int] = None

    @classmethod
    def read(cls, coder: WolfFileCoder, page_id: int) -> 'WolfMapPage':
        page = cls(page_id)
        page.unknown1 = coder.read_u4()
        page.graphic_name = coder.read_string()
        page.graphic_direction = coder.read_u1()
        page.graphic_frame = coder.read_u1()
        page.graphic_opacity = coder.read_u1()
        page.graphic_render_mode = coder.read_u1()

        page.conditions = coder.read(37)
        page.movement = coder.read(4)
        page.flags = coder.read_u1()
        page.route_flags = coder.read_u1()

        route_count = coder.read_u4()
        page.route = [WolfRouteCommand.read(coder) for _ in range(route_count)]

        command_count = coder.read_u4()
        page.commands = [WolfCommand.read(coder) for _ in range(command_count)]

        page.features = coder.read_u4()
        page.shadow_graphic_num = coder.read_u1()
        page.collision_width = coder.read_u1()
        page.collision_height = coder.read_u1()
        if page.features > 3:
            page.page_transfer = coder.read_u1()

        p_term = coder.read_u1()
        if p_term != cls.PAGE_TERMINATOR:
            raise ValueError(f"Terminador de página inválido: {hex(p_term)}")
        return page

    def write(self, coder: WolfFileCoder):
        coder.write_u4(self.unknown1)
        coder.write_string(self.graphic_name)
        coder.write_u1(self.graphic_direction)
        coder.write_u1(self.graphic_frame)
        coder.write_u1(self.graphic_opacity)
        coder.write_u1(self.graphic_render_mode)

        coder.write(self.conditions)
        coder.write(self.movement)
        coder.write_u1(self.flags)
        coder.write_u1(self.route_flags)

        coder.write_u4(len(self.route))
        for pt in self.route:
            pt.write(coder)

        coder.write_u4(len(self.commands))
        for cmd in self.commands:
            cmd.write(coder)

        coder.write_u4(self.features)
        coder.write_u1(self.shadow_graphic_num)
        coder.write_u1(self.collision_width)
        coder.write_u1(self.collision_height)
        if self.features > 3 and self.page_transfer is not None:
            coder.write_u1(self.page_transfer)

        coder.write_terminator(self.PAGE_TERMINATOR)


class WolfMapEvent:
    """Evento em um mapa do Wolf RPG."""
    EVENT_MAGIC1 = bytes([0x39, 0x30, 0x00, 0x00])
    EVENT_MAGIC2 = bytes([0x00, 0x00, 0x00, 0x00])
    EVENT_MARKER = 0x79
    EVENT_TERMINATOR = 0x70

    def __init__(self, event_id: int = 0):
        self.id = event_id
        self.name = ""
        self.x = 0
        self.y = 0
        self.pages: List[WolfMapPage] = []

    @classmethod
    def read(cls, coder: WolfFileCoder) -> 'WolfMapEvent':
        coder.verify(cls.EVENT_MAGIC1)
        ev = cls(coder.read_u4())
        ev.name = coder.read_string()
        ev.x = coder.read_u4()
        ev.y = coder.read_u4()
        page_count = coder.read_u4()
        coder.verify(cls.EVENT_MAGIC2)

        page_id = 0
        indicator = coder.read_u1()
        while indicator == cls.EVENT_MARKER:
            page = WolfMapPage.read(coder, page_id)
            ev.pages.append(page)
            page_id += 1
            indicator = coder.read_u1()

        if len(ev.pages) != page_count:
            raise ValueError(f"Esperado {page_count} páginas, mas lidas {len(ev.pages)}")
        if indicator != cls.EVENT_TERMINATOR:
            raise ValueError(f"Terminador de evento inválido: {hex(indicator)}")
        return ev

    def write(self, coder: WolfFileCoder):
        coder.write(self.EVENT_MAGIC1)
        coder.write_u4(self.id)
        coder.write_string(self.name)
        coder.write_u4(self.x)
        coder.write_u4(self.y)
        coder.write_u4(len(self.pages))
        coder.write(self.EVENT_MAGIC2)

        for page in self.pages:
            coder.write_u1(self.EVENT_MARKER)
            page.write(coder)

        coder.write_terminator(self.EVENT_TERMINATOR)


class WolfMap:
    """Representação de um arquivo de mapa (.mps) do Wolf RPG."""
    MAP_MAGIC = b'\0\0\0\0\0\0\0\0\0\0WOLFM\0'
    MAP_EVENT_MARKER = 0x6F
    MAP_TERMINATOR = 0x66

    def __init__(self):
        self.encoding_type = 0
        self.is_utf8 = False
        self.attributes = 0
        self.version = 0
        self.unknown_str = ""
        self.tileset_id = 0
        self.width = 0
        self.height = 0
        self.no_tiles = False
        self.tiles = b""
        self.events: List[WolfMapEvent] = []

    @classmethod
    def load(cls, file_path_or_bytes: Union[str, bytes]) -> 'WolfMap':
        coder = WolfFileCoder.open_read(file_path_or_bytes)
        coder.verify(cls.MAP_MAGIC)

        m = cls()
        m.encoding_type = coder.read_u4()
        m.is_utf8 = (m.encoding_type == 85)
        coder.is_utf8 = m.is_utf8

        m.attributes = coder.read_u4()
        m.version = coder.read_u1()
        m.unknown_str = coder.read_string()
        m.tileset_id = coder.read_u4()

        m.width = coder.read_u4()
        m.height = coder.read_u4()
        event_count = coder.read_u4()

        m.no_tiles = False
        if m.encoding_type != 0:
            val = coder.read_u4()
            if val == 0xFFFFFFFF:
                m.no_tiles = True
            else:
                coder.skip(-4)

        if not m.no_tiles:
            tiles_len = m.width * m.height * 3 * 4
            m.tiles = coder.read(tiles_len)

        if coder.eof:
            return m

        indicator = coder.read_u1()
        while indicator == cls.MAP_EVENT_MARKER:
            ev = WolfMapEvent.read(coder)
            m.events.append(ev)
            indicator = coder.read_u1()

        if indicator != cls.MAP_TERMINATOR:
            raise ValueError(f"Terminador de mapa inválido: {hex(indicator)}")

        return m

    def save(self, file_path_or_coder: Optional[Union[str, WolfFileCoder]] = None) -> bytes:
        if isinstance(file_path_or_coder, WolfFileCoder):
            coder = file_path_or_coder
        else:
            coder = WolfFileCoder.open_write(is_utf8=self.is_utf8)

        coder.write(self.MAP_MAGIC)
        coder.write_u4(self.encoding_type)
        coder.write_u4(self.attributes)
        coder.write_u1(self.version)
        coder.write_string(self.unknown_str)
        coder.write_u4(self.tileset_id)
        coder.write_u4(self.width)
        coder.write_u4(self.height)
        coder.write_u4(len(self.events))

        if self.encoding_type != 0:
            if self.no_tiles:
                coder.write_u4(0xFFFFFFFF)

        if not self.no_tiles:
            coder.write(self.tiles)

        for ev in self.events:
            coder.write_u1(self.MAP_EVENT_MARKER)
            ev.write(coder)

        coder.write_terminator(self.MAP_TERMINATOR)

        out_bytes = coder.getvalue()
        if isinstance(file_path_or_coder, str):
            with open(file_path_or_coder, 'wb') as f:
                f.write(out_bytes)
        return out_bytes


class WolfCommonEvent:
    """Evento comum individual dentro de CommonEvent.dat."""
    EVENT_MAGIC = b'\x0A\x00\x00\x00'
    EVENT_MAGIC3 = b'\x0B\x00\x00\x00'

    def __init__(self, event_id: int = 0):
        self.id = event_id
        self.unknown1 = 0
        self.blank1 = b'\x00' * 7
        self.name = ""
        self.commands: List[WolfCommand] = []
        self.unknown11 = ""
        self.description = ""
        self.unknown3: List[str] = []
        self.unknown31: Optional[int] = None
        self.unknown32: Optional[int] = None
        self.unknown4: List[int] = []
        self.unknown5: List[List[str]] = []
        self.unknown6: List[List[int]] = []
        self.unknown7 = b'\x00' * 29
        self.unknown8: List[str] = []
        self.unknown9 = ""
        self.is_0x92 = False
        self.unknown10 = ""
        self.unknown12 = 0

    @classmethod
    def read(cls, coder: WolfFileCoder) -> 'WolfCommonEvent':
        ind = coder.read_u1()
        if ind != 0x8E:
            raise ValueError(f"Indicador de CommonEvent inválido: {hex(ind)}")

        ev = cls(coder.read_u4())
        ev.unknown1 = coder.read_u4()
        ev.blank1 = coder.read(7)
        ev.name = coder.read_string()

        cmds_len = coder.read_u4()
        ev.commands = [WolfCommand.read(coder) for _ in range(cmds_len)]

        ev.unknown11 = coder.read_string()
        ev.description = coder.read_string()

        ind = coder.read_u1()
        if ind != 0x8F:
            raise ValueError(f"Indicador pós-descrição de CommonEvent inválido: {hex(ind)}")

        try:
            coder.verify(cls.EVENT_MAGIC)
            ev.unknown3 = coder.read_string_array(10)
        except Exception:
            coder.verify(cls.EVENT_MAGIC3)
            ev.unknown3 = coder.read_string_array(10)
            ev.unknown31 = coder.read_u4()
            ev.unknown32 = coder.read_u1()

        coder.verify(cls.EVENT_MAGIC)
        ev.unknown4 = coder.read_byte_array(10)

        coder.verify(cls.EVENT_MAGIC)
        ev.unknown5 = [coder.read_string_array() for _ in range(10)]

        coder.verify(cls.EVENT_MAGIC)
        ev.unknown6 = [coder.read_int_array() for _ in range(10)]

        ev.unknown7 = coder.read(0x1D)
        ev.unknown8 = [coder.read_string() for _ in range(100)]

        ind = coder.read_u1()
        if ind != 0x91:
            raise ValueError(f"Esperado 0x91 em CommonEvent, obtido {hex(ind)}")

        ev.unknown9 = coder.read_string()
        ind = coder.read_u1()
        if ind == 0x91:
            return ev

        if ind == 0x92:
            ev.is_0x92 = True
            ev.unknown10 = coder.read_string()
            ev.unknown12 = coder.read_u4()
            coder.verify(b'\x92')
        else:
            raise ValueError(f"Esperado 0x91 ou 0x92 em CommonEvent, obtido {hex(ind)}")

        return ev

    def write(self, coder: WolfFileCoder):
        coder.write_u1(0x8E)
        coder.write_u4(self.id)
        coder.write_u4(self.unknown1)
        coder.write(self.blank1)
        coder.write_string(self.name)
        coder.write_u4(len(self.commands))
        for cmd in self.commands:
            cmd.write(coder)

        coder.write_string(self.unknown11)
        coder.write_string(self.description)

        coder.write_u1(0x8F)
        if self.unknown31 is None:
            coder.write(self.EVENT_MAGIC)
        else:
            coder.write(self.EVENT_MAGIC3)
        for s in self.unknown3:
            coder.write_string(s)

        if self.unknown31 is not None:
            coder.write_u4(self.unknown31)
            coder.write_u1(self.unknown32 or 0)

        coder.write(self.EVENT_MAGIC)
        for b in self.unknown4:
            coder.write_u1(b)

        coder.write(self.EVENT_MAGIC)
        for sa in self.unknown5:
            coder.write_u4(len(sa))
            for s in sa:
                coder.write_string(s)

        coder.write(self.EVENT_MAGIC)
        for ia in self.unknown6:
            coder.write_u4(len(ia))
            for i in ia:
                coder.write_u4(i)

        coder.write(self.unknown7)
        for s in self.unknown8:
            coder.write_string(s)

        coder.write_u1(0x91)
        coder.write_string(self.unknown9)

        if self.is_0x92:
            coder.write_u1(0x92)
            coder.write_string(self.unknown10)
            coder.write_u4(self.unknown12)
            coder.write_u1(0x92)
        else:
            coder.write_u1(0x91)


class WolfCommonEvents:
    """Representação de CommonEvent.dat do Wolf RPG."""
    COMMON_MAGIC2 = b'\x00W\x00\x00OL\x00FC\x00\x8f'
    COMMON_MAGIC3 = b'\x00W\x00\x00OLUFC\x00\x90'

    def __init__(self):
        self.magic = self.COMMON_MAGIC2
        self.wolfversion = 2
        self.is_utf8 = False
        self.events: List[Optional[WolfCommonEvent]] = []
        self.last_terminator = 0x8F

    @classmethod
    def load(cls, file_path_or_bytes: Union[str, bytes]) -> 'WolfCommonEvents':
        coder = WolfFileCoder.open_read(file_path_or_bytes)
        ce = cls()
        try:
            coder.verify(cls.COMMON_MAGIC2)
            ce.magic = cls.COMMON_MAGIC2
            ce.wolfversion = 2
            coder.is_utf8 = False
        except Exception:
            coder.verify(cls.COMMON_MAGIC3)
            ce.magic = cls.COMMON_MAGIC3
            ce.wolfversion = 3
            ce.is_utf8 = True
            coder.is_utf8 = True

        events_len = coder.read_u4()
        ce.events = [None] * events_len
        for _ in range(events_len):
            event = WolfCommonEvent.read(coder)
            if event.id < events_len:
                ce.events[event.id] = event
            else:
                ce.events.append(event)

        ce.last_terminator = coder.read_u1()
        return ce

    def save(self, file_path_or_coder: Optional[Union[str, WolfFileCoder]] = None) -> bytes:
        if isinstance(file_path_or_coder, WolfFileCoder):
            coder = file_path_or_coder
        else:
            coder = WolfFileCoder.open_write(is_utf8=self.is_utf8)

        coder.write(self.magic)
        valid_events = [ev for ev in self.events if ev is not None]
        coder.write_u4(len(valid_events))
        for ev in valid_events:
            ev.write(coder)
        coder.write_u1(self.last_terminator)

        out_bytes = coder.getvalue()
        if isinstance(file_path_or_coder, str):
            with open(file_path_or_coder, 'wb') as f:
                f.write(out_bytes)
        return out_bytes


class WolfDatabaseField:
    """Campo de esquema de um banco de dados Wolf RPG."""
    STRING_START = 0x07D0
    INT_START = 0x03E8

    def __init__(self, name: str = ""):
        self.name = name
        self.indexinfo = 0
        self.ftype = 0
        self.unknown1 = ""
        self.string_args: List[str] = []
        self.args: List[int] = []
        self.default_value = 0

    @property
    def is_string(self) -> bool:
        return self.indexinfo >= self.STRING_START

    @property
    def is_int(self) -> bool:
        return not self.is_string

    @property
    def index(self) -> int:
        return self.indexinfo - self.STRING_START if self.is_string else self.indexinfo - self.INT_START


class WolfDatabaseData:
    """Registro individual de dados em um banco de dados Wolf RPG."""
    def __init__(self, name: str = ""):
        self.name = name
        self.int_values: List[int] = []
        self.string_values: List[str] = []


class WolfDatabaseType:
    """Tipo / Tabela em um banco de dados Wolf RPG."""
    D_TYPE_SEPARATOR = b'\xFE\xFF\xFF\xFF'

    def __init__(self, name: str = ""):
        self.name = name
        self.description = ""
        self.fields: List[WolfDatabaseField] = []
        self.data: List[WolfDatabaseData] = []
        self.field_type_list_size = 100
        self.unknown1 = 0


class WolfDatabase:
    """Representação de banco de dados (.dat e opcional .project) do Wolf RPG."""
    DATABASE_MAGIC = b'W\0\0OL'
    DATABASE_MAGIC_NEXT = b'FM\0'
    DAT_SEED_INDICES = [0, 3, 9]

    def __init__(self):
        self.has_leading_zero = False
        self.encoding_type = 0
        self.is_utf8 = False
        self.engine_version = 0
        self.types: List[WolfDatabaseType] = []
        self.last_terminator = 195  # VersionFooter.V3_0
        self.project_data: Optional[bytes] = None

    @classmethod
    def load(cls, dat_path: str, project_path: Optional[str] = None) -> 'WolfDatabase':
        db = cls()
        with open(dat_path, 'rb') as f:
            dat_bytes = bytearray(f.read())

        db.has_leading_zero = dat_bytes.startswith(b'\x00' + cls.DATABASE_MAGIC)
        # Verifica se o arquivo .dat é criptografado
        if not (dat_bytes.startswith(cls.DATABASE_MAGIC) or db.has_leading_zero):
            seeds = [dat_bytes[i] for i in cls.DAT_SEED_INDICES]
            dat_bytes = decrypt_dat_v1(dat_bytes, seeds, [1, 2, 5])
            db.has_leading_zero = dat_bytes.startswith(b'\x00' + cls.DATABASE_MAGIC)

        coder = WolfFileCoder(io.BytesIO(dat_bytes))
        if db.has_leading_zero:
            coder.read_u1()

        coder.verify(cls.DATABASE_MAGIC)
        db.encoding_type = coder.read_u1()
        db.is_utf8 = (db.encoding_type == 85)
        coder.is_utf8 = db.is_utf8
        coder.verify(cls.DATABASE_MAGIC_NEXT)
        db.engine_version = coder.read_u1()

        num_types = coder.read_u4()

        # Lê .project se existir
        if project_path and os.path.exists(project_path):
            with open(project_path, 'rb') as f:
                db.project_data = f.read()
            p_coder = WolfFileCoder(io.BytesIO(db.project_data), is_utf8=db.is_utf8)
            p_types_count = p_coder.read_u4()
            for _ in range(p_types_count):
                t = WolfDatabaseType(p_coder.read_string())
                f_count = p_coder.read_u4()
                t.fields = [WolfDatabaseField(p_coder.read_string()) for _ in range(f_count)]
                d_count = p_coder.read_u4()
                t.data = [WolfDatabaseData(p_coder.read_string()) for _ in range(d_count)]
                t.description = p_coder.read_string()

                t.field_type_list_size = p_coder.read_u4()
                for idx in range(min(len(t.fields), t.field_type_list_size)):
                    t.fields[idx].ftype = p_coder.read_u1()
                p_coder.skip(max(0, t.field_type_list_size - len(t.fields)))

                u1_len = p_coder.read_u4()
                for idx in range(min(len(t.fields), u1_len)):
                    t.fields[idx].unknown1 = p_coder.read_string()

                str_args_len = p_coder.read_u4()
                for idx in range(min(len(t.fields), str_args_len)):
                    t.fields[idx].string_args = p_coder.read_string_array()

                args_len = p_coder.read_u4()
                for idx in range(min(len(t.fields), args_len)):
                    t.fields[idx].args = p_coder.read_int_array()

                defs_len = p_coder.read_u4()
                for idx in range(min(len(t.fields), defs_len)):
                    t.fields[idx].default_value = p_coder.read_u4()

                db.types.append(t)

        # Se não há .project, cria tipos placeholder
        if not db.types:
            db.types = [WolfDatabaseType(f"Type_{i}") for i in range(num_types)]

        # Lê dados de cada tabela do .dat
        for t in db.types:
            coder.verify(WolfDatabaseType.D_TYPE_SEPARATOR)
            t.unknown1 = coder.read_u4()
            fields_size = coder.read_u4()

            if len(t.fields) < fields_size:
                for idx in range(len(t.fields), fields_size):
                    t.fields.append(WolfDatabaseField(f"Field_{idx}"))

            for idx in range(fields_size):
                t.fields[idx].indexinfo = coder.read_u4()

            data_size = coder.read_u4()
            if len(t.data) < data_size:
                for idx in range(len(t.data), data_size):
                    t.data.append(WolfDatabaseData(f"Data_{idx}"))

            int_fields = [f for f in t.fields[:fields_size] if f.is_int]
            str_fields = [f for f in t.fields[:fields_size] if f.is_string]

            for datum in t.data:
                datum.int_values = [coder.read_u4() for _ in int_fields]
                datum.string_values = [coder.read_string() for _ in str_fields]

        db.last_terminator = coder.read_u1()
        return db

    def save(self, dat_path: str, project_path: Optional[str] = None):
        # Salva .project se houver
        if project_path:
            p_coder = WolfFileCoder.open_write(is_utf8=self.is_utf8)
            p_coder.write_u4(len(self.types))
            for t in self.types:
                p_coder.write_string(t.name)
                p_coder.write_u4(len(t.fields))
                for fld in t.fields:
                    p_coder.write_string(fld.name)
                p_coder.write_u4(len(t.data))
                for datum in t.data:
                    p_coder.write_string(datum.name)
                p_coder.write_string(t.description)

                p_coder.write_u4(t.field_type_list_size)
                for idx in range(min(len(t.fields), t.field_type_list_size)):
                    p_coder.write_u1(t.fields[idx].ftype)
                for _ in range(max(0, t.field_type_list_size - len(t.fields))):
                    p_coder.write_u1(0)

                p_coder.write_u4(len(t.fields))
                for fld in t.fields:
                    p_coder.write_string(fld.unknown1)

                p_coder.write_u4(len(t.fields))
                for fld in t.fields:
                    p_coder.write_string_array(fld.string_args)

                p_coder.write_u4(len(t.fields))
                for fld in t.fields:
                    p_coder.write_int_array(fld.args)

                p_coder.write_u4(len(t.fields))
                for fld in t.fields:
                    p_coder.write_u4(fld.default_value)

            with open(project_path, 'wb') as f:
                f.write(p_coder.getvalue())

        # Salva .dat
        coder = WolfFileCoder.open_write(is_utf8=self.is_utf8)
        if self.has_leading_zero:
            coder.write_u1(0)
        coder.write(self.DATABASE_MAGIC)
        coder.write_u1(self.encoding_type)
        coder.write(self.DATABASE_MAGIC_NEXT)
        coder.write_u1(self.engine_version)
        coder.write_u4(len(self.types))

        for t in self.types:
            coder.write(WolfDatabaseType.D_TYPE_SEPARATOR)
            coder.write_u4(t.unknown1)
            coder.write_u4(len(t.fields))
            for fld in t.fields:
                coder.write_u4(fld.indexinfo)
            coder.write_u4(len(t.data))
            for datum in t.data:
                for val in datum.int_values:
                    coder.write_u4(val)
                for s in datum.string_values:
                    coder.write_string(s)

        coder.write_u1(self.last_terminator)
        with open(dat_path, 'wb') as f:
            f.write(coder.getvalue())


class WolfGameDat:
    """Leitor e modificador de Game.dat do Wolf RPG."""
    GAMEDAT_MAGIC = b'W\0\0OL\0FM'

    def __init__(self):
        self.raw_data = b""
        self.has_leading_zero = False
        self.encoding_type = 0
        self.is_utf8 = False
        self.byte_settings = b""
        self.str_count = 0
        self.title = ""
        self.serial = "0000-0000"
        self.encryption_key = b""
        self.font = ""
        self.subfonts: List[str] = ["", "", ""]
        self.starting_hero_graphic = ""
        self.extra_strings: List[str] = []
        self.rest_of_file = b""

    @classmethod
    def load(cls, file_path_or_bytes: Union[str, bytes]) -> 'WolfGameDat':
        gd = cls()
        if isinstance(file_path_or_bytes, str):
            with open(file_path_or_bytes, 'rb') as f:
                gd.raw_data = f.read()
        else:
            gd.raw_data = bytes(file_path_or_bytes)

        gd.has_leading_zero = gd.raw_data.startswith(b'\x00' + cls.GAMEDAT_MAGIC)
        coder = WolfFileCoder.open_read(gd.raw_data)
        if gd.has_leading_zero:
            coder.read_u1()

        if coder.stream.read(len(cls.GAMEDAT_MAGIC)) != cls.GAMEDAT_MAGIC:
            return gd

        gd.encoding_type = coder.read_u1()
        gd.is_utf8 = (gd.encoding_type == 85)
        coder.is_utf8 = gd.is_utf8

        # Lê ByteSettings
        u1_count = coder.read_u4()
        gd.byte_settings = coder.read(u1_count)

        # Lê StringSettings
        gd.str_count = coder.read_u4()
        if gd.str_count >= 1:
            gd.title = coder.read_string()
        if gd.str_count >= 2:
            gd.serial = coder.read_string()
        if gd.str_count >= 3:
            key_len = coder.read_u4()
            gd.encryption_key = coder.read(key_len)
        if gd.str_count >= 4:
            gd.font = coder.read_string()
        if gd.str_count >= 7:
            gd.subfonts = [coder.read_string() for _ in range(3)]
        if gd.str_count >= 8:
            gd.starting_hero_graphic = coder.read_string()
        if gd.str_count > 8:
            for _ in range(gd.str_count - 8):
                gd.extra_strings.append(coder.read_string())

        if not coder.eof:
            gd.rest_of_file = coder.read()

        return gd

    def save(self, file_path: str):
        coder = WolfFileCoder.open_write(is_utf8=self.is_utf8)
        if self.has_leading_zero:
            coder.write_u1(0)
        coder.write(self.GAMEDAT_MAGIC)
        coder.write_u1(self.encoding_type)

        coder.write_u4(len(self.byte_settings))
        coder.write(self.byte_settings)

        str_count = max(self.str_count, 7)
        coder.write_u4(str_count)
        coder.write_string(self.title)
        coder.write_string(self.serial)
        coder.write_u4(len(self.encryption_key))
        coder.write(self.encryption_key)
        coder.write_string(self.font)
        for sf in self.subfonts:
            coder.write_string(sf)
        if str_count >= 8:
            coder.write_string(self.starting_hero_graphic)
        for es in self.extra_strings:
            coder.write_string(es)

        if self.rest_of_file:
            if len(self.rest_of_file) >= 4:
                rest_after = self.rest_of_file[4:]
                new_size = coder.tell() + 4 + len(rest_after)
                coder.write_u4(new_size)
                coder.write(rest_after)
            else:
                coder.write(self.rest_of_file)

        self.raw_data = coder.getvalue()
        with open(file_path, 'wb') as f:
            f.write(self.raw_data)



class WolfBinaryAdapter:
    """
    Adaptador bidirecional entre binários do WOLF RPG (.mps, CommonEvent.dat, *.dat, Game.dat)
    e estruturas normalizadas em dicionários/listas compatíveis com a arquitetura de tradução.
    """

    @staticmethod
    def load_file(file_path: str, project_path: Optional[str] = None) -> Union[WolfDataDict, WolfDataList]:
        file_name = os.path.basename(file_path)

        if file_name.endswith('.mps'):
            map_obj = WolfMap.load(file_path)
            normalized = WolfBinaryAdapter._map_to_dict(map_obj)
            normalized._raw_wolf = map_obj
            return normalized

        elif file_name == 'CommonEvent.dat':
            ce_obj = WolfCommonEvents.load(file_path)
            normalized = WolfBinaryAdapter._commonevents_to_list(ce_obj)
            normalized._raw_wolf = ce_obj
            return normalized

        elif file_name.endswith('.dat') and file_name != 'Game.dat':
            if project_path is None or not os.path.exists(project_path):
                candidate_project = os.path.splitext(file_path)[0] + '.project'
                if os.path.exists(candidate_project):
                    project_path = candidate_project
                else:
                    # Se o arquivo estiver em pasta process/output, procura o .project na pasta input original
                    norm = os.path.normpath(file_path)
                    parts = norm.split(os.sep)
                    for staging in ('process', 'output'):
                        if staging in parts:
                            p_idx = parts.index(staging)
                            parts_input = list(parts)
                            parts_input[p_idx] = 'input'
                            candidate_input = os.path.splitext(os.sep.join(parts_input))[0] + '.project'
                            if os.path.exists(candidate_input):
                                project_path = candidate_input
                                break

            db_obj = WolfDatabase.load(file_path, project_path if (project_path and os.path.exists(project_path)) else None)
            normalized = WolfBinaryAdapter._database_to_dict(db_obj)
            normalized._raw_wolf = db_obj
            return normalized

        elif file_name == 'Game.dat':
            game_obj = WolfGameDat.load(file_path)
            normalized = WolfBinaryAdapter._gamedat_to_dict(game_obj)
            normalized._raw_wolf = game_obj
            return normalized

        raise ValueError(f"Extensão ou arquivo WOLF RPG não suportado: {file_name}")

    @staticmethod
    def save_file(file_path: str, data: Union[WolfDataDict, WolfDataList], original_raw: Optional[Any] = None):
        file_name = os.path.basename(file_path)
        raw_to_save = original_raw
        if raw_to_save is None and hasattr(data, '_raw_wolf') and data._raw_wolf is not None:
            raw_to_save = data._raw_wolf

        dir_name = os.path.dirname(file_path)
        if dir_name and not os.path.exists(dir_name):
            os.makedirs(dir_name, exist_ok=True)

        if file_name.endswith('.mps'):
            if raw_to_save is None or not isinstance(raw_to_save, WolfMap):
                raise ValueError(f"Objeto WolfMap original ausente para {file_name}")
            WolfBinaryAdapter._apply_map_dict(raw_to_save, data)
            raw_to_save.save(file_path)

        elif file_name == 'CommonEvent.dat':
            if raw_to_save is None or not isinstance(raw_to_save, WolfCommonEvents):
                raise ValueError(f"Objeto WolfCommonEvents original ausente para {file_name}")
            WolfBinaryAdapter._apply_commonevents_list(raw_to_save, data)
            raw_to_save.save(file_path)

        elif file_name.endswith('.dat') and file_name != 'Game.dat':
            if raw_to_save is None or not isinstance(raw_to_save, WolfDatabase):
                raise ValueError(f"Objeto WolfDatabase original ausente para {file_name}")
            WolfBinaryAdapter._apply_database_dict(raw_to_save, data)
            # Salva apenas o binário .dat. O arquivo .project contém os esquemas do editor
            # e tipos fundamentais (ex: 基本システム用変数) que não devem ser sobrescritos/regenerados.
            raw_to_save.save(file_path, None)

        elif file_name == 'Game.dat':
            if raw_to_save is None or not isinstance(raw_to_save, WolfGameDat):
                raise ValueError(f"Objeto WolfGameDat original ausente para {file_name}")
            WolfBinaryAdapter._apply_gamedat_dict(raw_to_save, data)
            raw_to_save.save(file_path)
        else:
            raise ValueError(f"Formato não suportado para salvar: {file_name}")

    # =========================================================================
    # Conversões Normalizadas -> Objetos e vice-versa
    # =========================================================================

    @staticmethod
    def _map_to_dict(map_obj: WolfMap) -> WolfDataDict:
        events_list: List[Optional[Dict[str, Any]]] = [None]
        for ev in map_obj.events:
            pages_list = []
            for p in ev.pages:
                cmd_list = []
                for cmd in p.commands:
                    cmd_list.append({
                        "code": cmd.cid,
                        "indent": cmd.indent,
                        "parameters": list(cmd.args),
                        "string_args": list(cmd.string_args)
                    })
                pages_list.append({
                    "id": p.id,
                    "list": cmd_list
                })

            events_list.append({
                "id": ev.id,
                "name": ev.name,
                "pages": pages_list
            })

        res = WolfDataDict({"events": events_list})
        res._raw_wolf = map_obj
        return res

    @staticmethod
    def _apply_map_dict(map_obj: WolfMap, data: Dict[str, Any]):
        events_data = data.get('events', [])
        ev_lookup = {ev.id: ev for ev in map_obj.events}

        for ev_item in events_data:
            if not ev_item:
                continue
            ev_id = ev_item.get('id')
            raw_ev = ev_lookup.get(ev_id)
            if not raw_ev:
                continue

            for page_item in ev_item.get('pages', []):
                p_id = page_item.get('id')
                if p_id < len(raw_ev.pages):
                    raw_page = raw_ev.pages[p_id]
                    cmd_items = page_item.get('list', [])
                    for idx, cmd_item in enumerate(cmd_items):
                        if idx < len(raw_page.commands):
                            raw_cmd = raw_page.commands[idx]
                            if 'parameters' in cmd_item and cmd_item['parameters'] is not None:
                                raw_cmd.args = list(cmd_item['parameters'])
                            if 'string_args' in cmd_item and cmd_item['string_args'] is not None:
                                raw_cmd.string_args = list(cmd_item['string_args'])

    @staticmethod
    def _commonevents_to_list(ce_obj: WolfCommonEvents) -> WolfDataList:
        events_list: List[Optional[Dict[str, Any]]] = []
        for ev in ce_obj.events:
            if ev is None:
                events_list.append(None)
                continue

            cmd_list = []
            for cmd in ev.commands:
                cmd_list.append({
                    "code": cmd.cid,
                    "indent": cmd.indent,
                    "parameters": list(cmd.args),
                    "string_args": list(cmd.string_args)
                })

            events_list.append({
                "id": ev.id,
                "name": ev.name,
                "description": ev.description,
                "list": cmd_list
            })

        res = WolfDataList(events_list)
        res._raw_wolf = ce_obj
        return res

    @staticmethod
    def _apply_commonevents_list(ce_obj: WolfCommonEvents, data: List[Optional[Dict[str, Any]]]):
        ce_lookup = {ev.id: ev for ev in ce_obj.events if ev is not None}
        for ev_item in data:
            if not ev_item:
                continue
            ev_id = ev_item.get('id')
            raw_ev = ce_lookup.get(ev_id)
            if not raw_ev:
                continue

            cmd_items = ev_item.get('list', [])
            for idx, cmd_item in enumerate(cmd_items):
                if idx < len(raw_ev.commands):
                    raw_cmd = raw_ev.commands[idx]
                    if 'parameters' in cmd_item and cmd_item['parameters'] is not None:
                        raw_cmd.args = list(cmd_item['parameters'])
                    if 'string_args' in cmd_item and cmd_item['string_args'] is not None:
                        raw_cmd.string_args = list(cmd_item['string_args'])

    @staticmethod
    def _database_to_dict(db_obj: WolfDatabase) -> WolfDataDict:
        types_list = []
        for t_idx, t in enumerate(db_obj.types):
            data_list = []
            for d_idx, datum in enumerate(t.data):
                data_list.append({
                    "id": d_idx,
                    "name": datum.name,
                    "string_values": list(datum.string_values)
                })
            types_list.append({
                "id": t_idx,
                "name": t.name,
                "description": t.description,
                "data": data_list
            })

        res = WolfDataDict({"types": types_list})
        res._raw_wolf = db_obj
        return res

    @staticmethod
    def _apply_database_dict(db_obj: WolfDatabase, data: Dict[str, Any]):
        types_data = data.get('types', [])
        for t_item in types_data:
            t_id = t_item.get('id')
            if t_id is not None and t_id < len(db_obj.types):
                raw_type = db_obj.types[t_id]
                for d_item in t_item.get('data', []):
                    d_id = d_item.get('id')
                    if d_id is not None and d_id < len(raw_type.data):
                        raw_datum = raw_type.data[d_id]
                        raw_datum.string_values = list(d_item.get('string_values', []))

    @staticmethod
    def _gamedat_to_dict(game_obj: WolfGameDat) -> WolfDataDict:
        res = WolfDataDict({
            "title": game_obj.title,
            "font": game_obj.font,
            "subfonts": list(game_obj.subfonts)
        })
        res._raw_wolf = game_obj
        return res

    @staticmethod
    def _apply_gamedat_dict(game_obj: WolfGameDat, data: Dict[str, Any]):
        if 'title' in data:
            game_obj.title = str(data['title'])
        if 'font' in data:
            game_obj.font = str(data['font'])
        if 'subfonts' in data and isinstance(data['subfonts'], list):
            game_obj.subfonts = [str(s) for s in data['subfonts']]

