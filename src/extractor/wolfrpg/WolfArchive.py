import io
import os
from io import SEEK_END, SEEK_SET
from pathlib import Path
from stat import FILE_ATTRIBUTE_DIRECTORY

try:
    from .huffman import huffman_Decode, huffman_Encode
except ImportError:
    from huffman import huffman_Decode, huffman_Encode
import struct


DXA_HEAD = struct.unpack("H", b"DX")[0]  # Header
DXA_VER = 0x0008  # Version
DXA_VER_MIN = 0x0008  # The minimum version supported.
DXA_BUFFERSIZE = 0x1000000  # Size of the buffer used when creating the archive
DXA_KEY_BYTES = 7  # Number of bytes in the key
DXA_KEY_STRING_LENGTH = 63  # Length of key string
DXA_KEY_STRING_MAXLENGTH = 2048  # Size of key string buffer

# Default key string
defaultKeyString = bytearray(
    [0x44, 0x58, 0x42, 0x44, 0x58, 0x41, 0x52, 0x43, 0x00]
)  # "DXLIBARC" # It's actually b"DXBDXARC\x00" ¯\_(ツ)_/¯

# Length of the log string
logStringLength = 0

# Flags
DXA_FLAG_NO_KEY = 0x00000001  # No key processing
DXA_FLAG_NO_HEAD_PRESS = 0x00000002  # No header compression


class DARC_HEAD:
    head = None  # Header
    version = None  # Version
    headSize = None  # Total size of the file without the DARC_HEAD header information.
    dataStartAddress = None  # The data address where the data of the first file is stored (the first address of the file is assumed to be address 0)
    fileNameTableStartAddress = None  # The first address of the file name table (the first address of the file is assumed to be address 0)
    fileTableStartAddress = None  # First address of the file table (assumes the address of the member variable FileNameTableStartAddress to be 0)
    directoryTableStartAddress = None  # First address of the directory table (assumes the address of the member variable FileNameTableStartAddress to be 0)
    # The DARC_DIRECTORY structure located at address 0 is the root directory.
    charCodeFormat = None  # Code page number used for the file name
    flags = None  # Flags (DXA_FLAG_NO_KEY, etc.)
    huffmanEncodeKB = None  # Size to be compressed by Huffman before and after the file (unit: kilobytes If 0xff, all files are compressed)
    reserve = None  # Reserved area

    def __init__(self, header_bytes=None):
        if header_bytes is None:
            return
        unpacked = struct.unpack("HHIQQQQIIB14sB", header_bytes)
        self.head = unpacked[0]
        self.version = unpacked[1]
        self.headSize = unpacked[2]
        self.dataStartAddress = unpacked[3]
        self.fileNameTableStartAddress = unpacked[4]
        self.fileTableStartAddress = unpacked[5]
        self.directoryTableStartAddress = unpacked[6]
        self.charCodeFormat = unpacked[7]
        self.flags = unpacked[8]
        self.huffmanEncodeKB = unpacked[9]
        self.reserve = unpacked[10]

    def __len__(self) -> int:
        return struct.calcsize("HHIQQQQIIB14sB")

    def __repr__(self) -> str:
        return f"""
Head->head = {self.head}
Head->self.version = {self.version}
Head->headSize = {self.headSize}
Head->dataStartAddress = {self.dataStartAddress}
Head->fileNameTableStartAddress = {self.fileNameTableStartAddress}
Head->fileTableStartAddress = {self.fileTableStartAddress}
Head->directoryTableStartAddress = {self.directoryTableStartAddress}
Head->charCodeFormat = {self.charCodeFormat}
Head->flags = {self.flags}
Head->huffmanEncodeKB = {self.huffmanEncodeKB}
Head->reserve = {self.reserve}
"""


# Time information of the file
class DARC_FILETIME:
    create = None  # Creation time
    lastAccess = None  # Last access time
    lastWrite = None  # Last update time

    def __init__(self, fileTime_bytes=None):
        if fileTime_bytes is None:
            return
        unpacked = struct.unpack("QQQ", fileTime_bytes[: len(self)])
        self.create = unpacked[0]
        self.lastAccess = unpacked[1]
        self.lastWrite = unpacked[2]

    def __len__(self) -> int:
        return struct.calcsize("QQQ")

    def __repr__(self) -> str:
        return f"""\tTime->create = {self.create}
\tTime->lastAccess = {self.lastAccess}
\tTime->lastWrite = {self.lastWrite}"""


# File storage information
class DARC_FILEHEAD:
    nameAddress = None  # Address where the file name is stored (the address of the member variable FileNameTableStartAddress of the ARCHIVE_HEAD structure is set to address 0)
    attributes = None  # File attributes
    time = None  # Time information
    dataAddress = None  # Address where the file is stored.
    #            In the case of a file, the address indicated by the member variable DataStartAddress of the DARC_HEAD structure shall be address 0.
    #            In the case of a directory: The address indicated by the member variable "DirectoryTableStartAddress" of the DARC_HEAD structure shall be set to address 0.
    dataSize = None  # Data size of the file
    pressDataSize = None  # The size of the data after compression ( 0xffffffffffffffffff: not compressed ) (added in Ver0x0002)
    huffPressDataSize = None  # Size of the data after Huffman compression ( 0xffffffffffffffff: not compressed ) (added in Ver0x0008)

    def __init__(self, fileHead_bytes=None):
        if fileHead_bytes is None:
            return
        unpacked = struct.unpack("QQQQQQQQQ", fileHead_bytes[: len(self)])
        self.nameAddress = unpacked[0]
        self.attributes = unpacked[1]
        self.time = DARC_FILETIME()
        self.time.create = unpacked[2]
        self.time.lastAccess = unpacked[3]
        self.time.lastWrite = unpacked[4]
        self.dataAddress = unpacked[5]
        self.dataSize = unpacked[6]
        self.pressDataSize = unpacked[7]
        self.huffPressDataSize = unpacked[8]

    def __len__(self) -> int:
        return struct.calcsize("QQQQQQQQQ")

    def __repr__(self) -> str:
        return f"""File->nameAddress = {self.nameAddress}
File->attributes = {self.attributes}
File->time.create = {self.time.create}
File->time.lastAccess = {self.time.lastAccess}
File->time.lastWrite = {self.time.lastWrite}
File->dataAddress = {self.dataAddress}
File->dataSize = {self.dataSize}
File->pressDataSize = {self.pressDataSize}
File->huffPressDataSize = {self.huffPressDataSize}"""


# Directory storage information
class DARC_DIRECTORY:
    directoryAddress = None  # Address where my DARC_FILEHEAD is stored (Address 0 is the address indicated by the member variable FileTableStartAddress of the DARC_HEAD structure)
    parentDirectoryAddress = None  # The address where DARC_DIRECTORY of the parent directory is stored ( The address indicated by the member variable DirectoryTableStartAddress of the DARC_HEAD structure is set to address 0.)
    fileHeadNum = None  # Number of files in the directory
    fileHeadAddress = None  # The address where the header column of the file in the directory is stored ( The address indicated by the member variable FileTableStartAddress of the DARC_HEAD structure is set to address 0.)

    def __init__(self, directory_bytes=None):
        if directory_bytes is None:
            return
        unpacked = struct.unpack("QQQQ", directory_bytes[: len(self)])
        self.directoryAddress = unpacked[0]
        self.parentDirectoryAddress = unpacked[1]
        self.fileHeadNum = unpacked[2]
        self.fileHeadAddress = unpacked[3]

    def __len__(self) -> int:
        return struct.calcsize("QQQQ")

    def __repr__(self) -> str:
        return f"""
self.directoryAddress = {self.directoryAddress}
self.parentDirectoryAddress = {self.parentDirectoryAddress}
self.fileHeadNum = {self.fileHeadNum}
self.fileHeadAddress = {self.fileHeadAddress}
"""


# Information for storing the progress of the encoding process
class DARC_ENCODEINFO:
    totalFileNum = None  # Total number of files
    compFileNum = None  # Number of files processed.
    prevDispTime = None  # Time of the last status output
    processFileName = None  # Name of the file currently being processed
    outputStatus = None  # Whether status output is performed or not

    def __repr__(self) -> str:
        return f"""
self.totalFileNum = {self.totalFileNum}
self.compFileNum = {self.compFileNum}
self.prevDispTime = {self.prevDispTime}
self.processFileName = {self.processFileName}
self.outputStatus = {self.outputStatus}
"""


class ArchivedFile:
    filePath: Path
    compressed: bool
    huffmanCompressed: bool
    key: bytearray | None
    dataStart: int
    dataSize: int
    pressDataSize: int
    huffPressDataSize: int

    def __str__(self) -> str:
        return f"""ArchivedFile(
\tfilePath: {self.filePath}
\tcompressed: {self.compressed}
\thuffmanCompressed: {self.huffmanCompressed}
\tkey: {self.key}
\tdataStart: {self.dataStart}
\tdataSize: {self.dataSize}
\tpressDataSize: {self.pressDataSize}
\thuffPressDataSize: {self.huffPressDataSize}
)"""


class WinRng:
    """MSVC LCG Pseudo-Random Number Generator."""
    def __init__(self, seed: int = 1):
        self.state = seed & 0xFFFFFFFF

    def srand(self, seed: int):
        self.state = seed & 0xFFFFFFFF

    def rand(self) -> int:
        self.state = (self.state * 214013 + 2531011) & 0xFFFFFFFF
        return (self.state >> 16) & 0x7FFF


def rotr8(val: int, r: int) -> int:
    val &= 0xFF
    r %= 8
    return ((val >> r) | (val << (8 - r))) & 0xFF


SBOX = [
    0x63, 0x7C, 0x77, 0x7B, 0xF2, 0x6B, 0x6F, 0xC5, 0x30, 0x01, 0x67, 0x2B, 0xFE, 0xD7, 0xAB, 0x76,
    0xCA, 0x82, 0xC9, 0x7D, 0xFA, 0x59, 0x47, 0xF0, 0xAD, 0xD4, 0xA2, 0xAF, 0x9C, 0xA4, 0x72, 0xC0,
    0xB7, 0xFD, 0x93, 0x26, 0x36, 0x3F, 0xF7, 0xCC, 0x34, 0xA5, 0xE5, 0xF1, 0x71, 0xD8, 0x31, 0x15,
    0x04, 0xC7, 0x23, 0xC3, 0x18, 0x96, 0x05, 0x9A, 0x07, 0x12, 0x80, 0xE2, 0xEB, 0x27, 0xB2, 0x75,
    0x09, 0x83, 0x2C, 0x1A, 0x1B, 0x6E, 0x5A, 0xA0, 0x52, 0x3B, 0xD6, 0xB3, 0x29, 0xE3, 0x2F, 0x84,
    0x53, 0xD1, 0x00, 0xED, 0x20, 0xFC, 0xB1, 0x5B, 0x6A, 0xCB, 0xBE, 0x39, 0x4A, 0x4C, 0x58, 0xCF,
    0xD0, 0xEF, 0xAA, 0xFB, 0x43, 0x4D, 0x33, 0x85, 0x45, 0xF9, 0x02, 0x7F, 0x50, 0x3C, 0x9F, 0xA8,
    0x51, 0xA3, 0x40, 0x8F, 0x92, 0x9D, 0x38, 0xF5, 0xBC, 0xB6, 0xDA, 0x21, 0x10, 0xFF, 0xF3, 0xD2,
    0xCD, 0x0C, 0x13, 0xEC, 0x5F, 0x97, 0x44, 0x17, 0xC4, 0xA7, 0x7E, 0x3D, 0x64, 0x5D, 0x19, 0x73,
    0x60, 0x81, 0x4F, 0xDC, 0x22, 0x2A, 0x90, 0x88, 0x46, 0xEE, 0xB8, 0x14, 0xDE, 0x5E, 0x0B, 0xDB,
    0xE0, 0x32, 0x3A, 0x0A, 0x49, 0x06, 0x24, 0x5C, 0xC2, 0xD3, 0xAC, 0x62, 0x91, 0x95, 0xE4, 0x79,
    0xE7, 0xC8, 0x37, 0x6D, 0x8D, 0xD5, 0x4E, 0xA9, 0x6C, 0x56, 0xF4, 0xEA, 0x65, 0x7A, 0xAE, 0x08,
    0xBA, 0x78, 0x25, 0x2E, 0x1C, 0xA6, 0xB4, 0xC6, 0xE8, 0xDD, 0x74, 0x1F, 0x4B, 0xBD, 0x8B, 0x8A,
    0x70, 0x3E, 0xB5, 0x66, 0x48, 0x03, 0xF6, 0x0E, 0x61, 0x35, 0x57, 0xB9, 0x86, 0xC1, 0x1D, 0x9E,
    0xE1, 0xF8, 0x98, 0x11, 0x69, 0xD9, 0x8E, 0x94, 0x9B, 0x1E, 0x87, 0xE9, 0xCE, 0x55, 0x28, 0xDF,
    0x8C, 0xA1, 0x89, 0x0D, 0xBF, 0xE6, 0x42, 0x68, 0x41, 0x99, 0x2D, 0x0F, 0xB0, 0x54, 0xBB, 0x16,
]
RCON = [0x8D, 0x01, 0x02, 0x04, 0x08, 0x10, 0x20, 0x40, 0x80, 0x1B, 0x36]


def xtime(x: int) -> int:
    return ((x << 1) ^ (((x >> 7) & 1) * 0x1B)) & 0xFF


def aes_key_expansion(key: bytes) -> bytearray:
    p_round_key = bytearray(176)
    for i in range(4):
        p_round_key[i * 4 + 0] = key[i * 4 + 0]
        p_round_key[i * 4 + 1] = key[i * 4 + 1]
        p_round_key[i * 4 + 2] = key[i * 4 + 2]
        p_round_key[i * 4 + 3] = key[i * 4 + 3]

    for i in range(4, 44):
        k = (i - 1) * 4
        tempa = [p_round_key[k + 0], p_round_key[k + 1], p_round_key[k + 2], p_round_key[k + 3]]
        if (i % 4) == 0:
            u8tmp = tempa[0]
            tempa[0] = tempa[1]
            tempa[1] = tempa[2]
            tempa[2] = tempa[3]
            tempa[3] = u8tmp
            tempa[0] = SBOX[tempa[0]] ^ RCON[i // 4]
            tempa[1] = SBOX[tempa[1]] >> 4
            tempa[2] = (~SBOX[tempa[2]]) & 0xFF
            tempa[3] = rotr8(SBOX[tempa[3]], 7)

        j = i * 4
        k = (i - 4) * 4
        p_round_key[j + 0] = p_round_key[k + 0] ^ tempa[0]
        p_round_key[j + 1] = p_round_key[k + 1] ^ tempa[1]
        p_round_key[j + 2] = p_round_key[k + 2] ^ tempa[2]
        p_round_key[j + 3] = p_round_key[k + 3] ^ tempa[3]

    return p_round_key


def aes_cipher(p_state: bytearray, p_round_key: bytearray):
    for i in range(16):
        p_state[i] ^= p_round_key[i]

    for r in range(1, 10):
        for i in range(16):
            p_state[i] = SBOX[p_state[i]]
        p_state[1], p_state[5], p_state[9], p_state[13] = p_state[5], p_state[9], p_state[13], p_state[1]
        p_state[2], p_state[10] = p_state[10], p_state[2]
        p_state[6], p_state[14] = p_state[14], p_state[6]
        p_state[3], p_state[7], p_state[11], p_state[15] = p_state[15], p_state[3], p_state[7], p_state[11]

        for c in range(4):
            idx = c * 4
            s0 = p_state[idx + 0]
            s1 = p_state[idx + 1]
            s2 = p_state[idx + 2]
            s3 = p_state[idx + 3]
            tmp = s0 ^ s1 ^ s2 ^ s3
            p_state[idx + 0] ^= tmp ^ xtime(s1 ^ s0)
            p_state[idx + 1] ^= tmp ^ xtime(s2 ^ s1)
            p_state[idx + 2] ^= tmp ^ xtime(s2 ^ s3)
            p_state[idx + 3] ^= tmp ^ xtime(s3 ^ s0)

        rk_idx = r * 16
        for i in range(16):
            p_state[i] ^= p_round_key[rk_idx + i]

    for i in range(16):
        p_state[i] = SBOX[p_state[i]]
    p_state[1], p_state[5], p_state[9], p_state[13] = p_state[5], p_state[9], p_state[13], p_state[1]
    p_state[2], p_state[10] = p_state[10], p_state[2]
    p_state[6], p_state[14] = p_state[14], p_state[6]
    p_state[3], p_state[7], p_state[11], p_state[15] = p_state[15], p_state[3], p_state[7], p_state[11]

    for i in range(16):
        p_state[i] ^= p_round_key[160 + i]


def init_aes128(p_pwd: bytes, crypt_version: int) -> bytearray:
    key = bytearray(16)
    iv = bytearray(16)
    p_pro_key = bytearray(4)

    for i in range(15):
        key[i] ^= (p_pwd[(i * 7) % 15] + i * i) & 0xFF
        iv[i] ^= (p_pwd[(i * 11) % 15] - i * i) & 0xFF
        key[15] ^= (p_pwd[i] + i * 3) & 0xFF
        iv[15] ^= (p_pwd[i] + i * 5) & 0xFF

    key[0] ^= p_pro_key[0]
    iv[10] ^= p_pro_key[0]
    key[4] ^= p_pro_key[1]
    iv[1] ^= p_pro_key[1]
    key[8] ^= p_pro_key[2]
    iv[4] ^= p_pro_key[2]
    key[12] ^= p_pro_key[3]
    iv[7] ^= p_pro_key[3]

    round_key = aes_key_expansion(key)
    return round_key + iv


def aes_ctr_xcrypt(data: bytearray, data_offset: int, key_with_iv: bytearray, size: int):
    p_iv = memoryview(key_with_iv)[176:192]
    p_key = key_with_iv[:176]
    state = bytearray(16)
    bi = 16

    for i in range(size):
        if bi == 16:
            state[:] = p_iv
            aes_cipher(state, p_key)
            for j in range(15, -1, -1):
                if p_iv[j] == 0xFF:
                    p_iv[j] = 0
                    continue
                p_iv[j] += 1
                break
            bi = 0
        data[data_offset + i] ^= state[bi]
        bi += 1


def calc_salt(key_string: bytes) -> bytearray:
    salt = bytearray(128)
    str_len = len(key_string)
    if str_len == 0:
        return salt
    for i in range(128):
        salt[i] = ((i // str_len) + key_string[i % str_len]) & 0xFF
    return salt


def init_wolf_crypt(crypt_version: int, p_pwd: bytes, key_string: bytes = None, other: bool = False) -> bytearray:
    fac = bytearray(3)
    s0 = p_pwd[2] & 0xFF
    s1 = p_pwd[5] & 0xFF
    s2 = p_pwd[12] & 0xFF
    s3 = 0

    if not other:
        length = (p_pwd[11] & 0xFF) // 3
        for i in range(length):
            s3 = i ^ rotr8(s3 ^ (p_pwd[i % 15] & 0xFF), 3)
    else:
        length = (p_pwd[8] & 0xFF) // 4
        for i in range(length):
            s3 = i ^ rotr8(s3 ^ (p_pwd[i % 15] & 0xFF), 2)

    seed = (s0 * s1 + s2 + s3) & 0xFFFFFFFF
    rng = WinRng(seed)

    fac[s3 % 3] = rng.rand() % 256
    p_key = bytearray(768)

    for i in range(256):
        rn = rng.rand() & 0xFFFF
        p_key[i] = (fac[0] ^ (rng.rand() & 0xFF)) & 0xFF
        p_key[i + 256] = (fac[1] ^ (rn >> 8)) & 0xFF
        p_key[i + 512] = (fac[2] ^ (rn & 0xFF)) & 0xFF

    if other and key_string:
        salt = calc_salt(key_string)
        mod_factor = 7

        for i in range(3):
            t = s3
            for j in range(256):
                skip = False
                cur_s = salt[j & 0x7F]
                cur_s2 = salt[(j + i) % 0x80]
                cur_k = p_key[i * 256 + j]
                s_x_k = cur_s ^ cur_k
                round_val = (cur_s2 | (cur_s << 8)) % mod_factor
                new_k = s_x_k

                if round_val == 1:
                    if (cur_s2 % 0xB) == 0:
                        new_k = cur_k
                elif round_val == 2:
                    if (cur_s % 0x1D) == 0:
                        new_k = (~s_x_k) & 0xFF
                elif round_val == 3:
                    if ((round_val + j) % 0x25) == 0:
                        new_k = cur_s2 ^ s_x_k
                elif round_val == 4:
                    if ((cur_s + cur_s2) % 97) == 0:
                        new_k = (cur_s + s_x_k) & 0xFF
                elif round_val == 5:
                    if ((j * round_val) % 0x7B) == 0:
                        new_k = s_x_k ^ t
                elif round_val == 6:
                    if cur_s == 0xFF and cur_s2 == 0:
                        new_k = 0
                        skip = True

                if ((j + i) % (cur_s % 5 + 1)) == 0:
                    new_k ^= (t & 0xFF)
                elif skip:
                    new_k = (~s_x_k) & 0xFF

                p_key[i * 256 + j] = new_k & 0xFF
                t += i

    return p_key


def wolf_crypt(p_key: bytearray, data: bytearray, data_offset: int, start: int, end: int):
    length = end - start
    v1 = start % 256
    v2 = (start // 256) % 256
    v3 = (start // 0x10000) % 256

    pos = 0
    while pos < length:
        chunk_len = min(256 - v1, length - pos)
        k23 = p_key[v2 + 256] ^ p_key[v3 + 512]
        if v1 == 0 and chunk_len == 256:
            mask = bytes(p_key[i] ^ k23 for i in range(256))
            mask_int = int.from_bytes(mask, 'little')
            c_int = int.from_bytes(data[data_offset + pos : data_offset + pos + 256], 'little')
            data[data_offset + pos : data_offset + pos + 256] = (c_int ^ mask_int).to_bytes(256, 'little')
        else:
            for i in range(chunk_len):
                data[data_offset + pos + i] ^= p_key[v1 + i] ^ k23

        pos += chunk_len
        v1 = (v1 + chunk_len) % 256
        if v1 == 0:
            v2 += 1
            if v2 == 256:
                v2 = 0
                v3 = (v3 + 1) % 256


def crypt_addresses(data: bytearray, p_pwd: bytes):
    seed = (p_pwd[0] & 0xFF) + (p_pwd[7] & 0xFF) * (p_pwd[12] & 0xFF)
    rng = WinRng(seed)
    u16_list = list(struct.unpack('<32H', data[:64]))
    for i in range(4):
        base_idx = 4 + i * 4
        for j in range(3, -1, -1):
            u16_list[base_idx + j] ^= rng.rand() & 0xFFFF
    data[:64] = struct.pack('<32H', *u16_list)


KNOWN_KEYS = {
    0x14B: bytes([
        0xCA, 0x08, 0x4C, 0x5D, 0x17, 0x0D, 0xDA, 0xA1, 0xD7, 0x27, 0xC8, 0x41, 0x54, 0x38, 0x82, 0x32,
        0x54, 0xB7, 0xF9, 0x46, 0x8E, 0x13, 0x6B, 0xCA, 0xD0, 0x5C, 0x95, 0x95, 0xE2, 0xDC, 0x03, 0x53,
        0x60, 0x9B, 0x4A, 0x38, 0x17, 0xF3, 0x69, 0x59, 0xA4, 0xC7, 0x9A, 0x43, 0x63, 0xE6, 0x54, 0xAF,
        0xDB, 0xBB, 0x43, 0x58, 0x00
    ]),
    0x13A: bytes([
        0x31, 0xF9, 0x01, 0x36, 0xA3, 0xE3, 0x8D, 0x3C, 0x7B, 0xC3, 0x7D, 0x25, 0xAD, 0x63, 0x28, 0x19,
        0x1B, 0xF7, 0x8E, 0x6C, 0xC4, 0xE5, 0xE2, 0x76, 0x82, 0xEA, 0x4F, 0xED, 0x61, 0xDA, 0xE0, 0x44,
        0x5B, 0xB6, 0x46, 0x3B, 0x06, 0xD5, 0xCE, 0xB6, 0x78, 0x58, 0xD0, 0x7C, 0x82, 0x00
    ]),
    0x12C: bytes([
        0x0F, 0x53, 0xE1, 0x3E, 0x8E, 0xB5, 0x41, 0x91, 0x52, 0x16, 0x55, 0xAE, 0x34, 0xC9, 0x8F, 0x79,
        0x59, 0x2F, 0x59, 0x6B, 0x95, 0x19, 0x9B, 0x1B, 0x35, 0x9A, 0x2F, 0xDE, 0xC9, 0x7C, 0x12, 0x96,
        0xC3, 0x14, 0xB5, 0x0F, 0x53, 0xE1, 0x3E, 0x8E, 0x00
    ]),
}


class DXArchive:
    MIN_COMPRESS = 4  # Minimum number of compressed bytes
    MAX_SEARCHLISTNUM = (
        64  # Maximum number of lists to traverse to find the maximum match length
    )
    MAX_SUBLISTNUM = 65536  # Maximum number of sublists to reduce compression time
    MAX_COPYSIZE = (
        0x1FFF + MIN_COMPRESS
    )  # Maximum size to copy from a reference address ( Maximum copy size that a compression code can represent + Minimum number of compressed bytes )
    MAX_ADDRESSLISTNUM = 1024 * 1024 * 1  # Maximum size of slide dictionary
    MAX_POSITION = 1 << 24  # Maximum relative address that can be referenced ( 16MB )

    def __init__(self) -> None:
        self.archivedFiles = []
        self.fp = None
        self.huffman_Encode = huffman_Encode
        self.huffman_Decode = huffman_Decode
        self.newCrypt = False
        self.specialKey = None
        self.cryptVersion = 0

    def error(self) -> bool:
        if self.fp is not None:
            self.fp.close()

        return False

    def loadArchive(
        self,
        archivePath: Path,
        outputPath: Path = Path("."),
        keyString_: bytearray = None,
    ):
        self.fp = open(archivePath, mode="rb")
        self.outputPath = outputPath
        self.directory = self.outputPath

        key = bytearray([0] * DXA_KEY_BYTES)
        keyString = bytearray([0] * (DXA_KEY_STRING_LENGTH + 1))

        if keyString_ is None:
            keyString_ = defaultKeyString

        keyStringBytes = len(keyString_)
        if keyStringBytes > DXA_KEY_STRING_LENGTH:
            keyStringBytes = DXA_KEY_STRING_LENGTH

        keyString = keyString_[:keyStringBytes]

        # Creating a key
        key = self.keyCreate(keyString, keyStringBytes, key)

        header_bytes = self.fp.read(len(DARC_HEAD()))  # 64
        self.archiveHead = DARC_HEAD(header_bytes)

        if self.archiveHead.head != DXA_HEAD:
            return self.error()

        if self.archiveHead.version > DXA_VER or self.archiveHead.version < DXA_VER_MIN:
            return self.error()

        crypt_version = (self.archiveHead.flags >> 16) if self.archiveHead.flags is not None else 0
        is_new_crypt = (331 <= crypt_version < 1000) or (crypt_version >= 1010)

        if is_new_crypt:
            self.fp.seek(0, SEEK_SET)
            file_data = bytearray(self.fp.read())
            size = len(file_data)
            p_pwd = bytes(file_data[49:64])

            key_bytes = KNOWN_KEYS.get(crypt_version)
            if not key_bytes and keyString_ is not None and keyString_ != defaultKeyString:
                key_bytes = bytes(keyString_)
            if not key_bytes:
                self.error()
                raise ValueError(f"Versão de criptografia Wolf {hex(crypt_version)} não suportada ou chave ausente")

            key_str = key_bytes.split(b'\x00')[0]

            crypt_addresses(file_data, p_pwd)
            self.archiveHead = DARC_HEAD(file_data[:64])

            special_key_pass1 = init_wolf_crypt(crypt_version, p_pwd, key_str, other=True)
            wolf_crypt(special_key_pass1, file_data, 64, 64, size - 64)

            aes_rk = init_aes128(p_pwd, crypt_version)
            aes_ctr_xcrypt(file_data, 64, aes_rk, 0x400)
            table_size = size - self.archiveHead.fileNameTableStartAddress
            aes_ctr_xcrypt(file_data, self.archiveHead.fileNameTableStartAddress, aes_rk, table_size)

            pass2_key = init_wolf_crypt(crypt_version, p_pwd, key_str, other=False)
            self.specialKey = pass2_key
            self.newCrypt = True
            self.cryptVersion = crypt_version
            self.noKey = False

            self.fp.close()
            self.fp = io.BytesIO(file_data)

            huff_buf = bytearray(file_data[self.archiveHead.fileNameTableStartAddress:])
            wolf_crypt(self.specialKey, huff_buf, 0, 0, len(huff_buf))

            lz_size = huffman_Decode(bytes(huff_buf), None)
            if lz_size is None or lz_size <= 0:
                return self.error()

            lz_buf = bytearray(lz_size)
            huffman_Decode(bytes(huff_buf), lz_buf)

            head_buf = bytearray(self.archiveHead.headSize)
            head_buf, _ = self.decode(lz_buf, head_buf)

            base = self.archiveHead.fileNameTableStartAddress or 0
            f_start = self.archiveHead.fileTableStartAddress - base if self.archiveHead.fileTableStartAddress >= base else self.archiveHead.fileTableStartAddress
            d_start = self.archiveHead.directoryTableStartAddress - base if self.archiveHead.directoryTableStartAddress >= base else self.archiveHead.directoryTableStartAddress

            self.nameTable = head_buf[:f_start]
            self.fileTable = head_buf[f_start:d_start]
            self.directoryTable = head_buf[d_start:]

            self.directoryDecode(
                DARC_DIRECTORY(self.directoryTable), key, keyString, keyStringBytes
            )

            return True

        headBuffer = [0] * self.archiveHead.headSize

        self.noKey = (self.archiveHead.flags & DXA_FLAG_NO_KEY) != 0

        if self.archiveHead.headSize is None or self.archiveHead.headSize == 0:
            return self.error()

        if (self.archiveHead.flags & DXA_FLAG_NO_HEAD_PRESS) != 0:
            # If not compressed, read normally
            self.fp.seek(self.archiveHead.fileNameTableStartAddress, SEEK_SET)
            headBuffer = self.keyConvFileRead(self.archiveHead.headSize, 0)
        else:
            # Get compressed header capacity
            self.fp.seek(0, SEEK_END)
            fileSize = self.fp.tell()
            self.fp.seek(self.archiveHead.fileNameTableStartAddress, SEEK_SET)
            huffHeadSize = fileSize - self.fp.tell()

            if huffHeadSize is None or huffHeadSize <= 0:
                return self.error()

            huffHeadBuffer = bytearray([0] * huffHeadSize)

            # Read Huffman compressed headers into memory
            huffHeadBuffer = self.keyConvFileRead(
                huffHeadBuffer, huffHeadSize, None if self.noKey else key, 0
            )

            # Obtain the decompressed capacity of the Huffman compressed header
            lzHeadSize = huffman_Decode(huffHeadBuffer, None)

            if lzHeadSize is None or lzHeadSize <= 0:
                return self.error()

            lzHeadBuffer = bytearray([0] * lzHeadSize)

            # Decompress Huffman compressed headers
            (lzHeadBuffer, originalSize) = huffman_Decode(huffHeadBuffer, lzHeadBuffer)

            # Decompress LZ compressed headers
            (headBuffer, size) = self.decode(lzHeadBuffer, bytearray(headBuffer))

            base = self.archiveHead.fileNameTableStartAddress or 0
            f_start = self.archiveHead.fileTableStartAddress - base if self.archiveHead.fileTableStartAddress >= base else self.archiveHead.fileTableStartAddress
            d_start = self.archiveHead.directoryTableStartAddress - base if self.archiveHead.directoryTableStartAddress >= base else self.archiveHead.directoryTableStartAddress

            self.nameTable = headBuffer[: f_start]
            self.fileTable = headBuffer[f_start : d_start]
            self.directoryTable = headBuffer[d_start :]

            self.directoryDecode(
                DARC_DIRECTORY(self.directoryTable), key, keyString, keyStringBytes
            )

            return True

    def keyCreate(self, source: bytearray, sourceBytes: int, key: bytearray):
        workBuffer = bytearray([0] * 1024)

        if sourceBytes == 0:
            sourceBytes = len(source)

        # If it's too short, add defaultKeyString
        if sourceBytes < 4:
            sourceTempBuffer = source
            sourceTempBuffer += defaultKeyString
            source = sourceTempBuffer
            sourceBytes = len(source)

        if sourceBytes > len(workBuffer):
            useWorkBuffer = bytearray([0] * sourceBytes)
        else:
            useWorkBuffer = workBuffer

        j = 0
        for i in range(0, sourceBytes, 2):
            useWorkBuffer[j] = source[i]
            j += 1

        CRC32_0 = self.CRC32(useWorkBuffer, j)

        j = 0
        for i in range(1, sourceBytes, 2):
            useWorkBuffer[j] = source[i]
            j += 1

        CRC32_1 = self.CRC32(useWorkBuffer, j)

        key[0] = (CRC32_0 >> 0) % 256
        key[1] = (CRC32_0 >> 8) % 256
        key[2] = (CRC32_0 >> 16) % 256
        key[3] = (CRC32_0 >> 24) % 256
        key[4] = (CRC32_1 >> 0) % 256
        key[5] = (CRC32_1 >> 8) % 256
        key[6] = (CRC32_1 >> 16) % 256

        return key

    def CRC32(self, SrcData: bytearray, SrcDataSize: bytearray) -> int:
        CRC32TableInit = 0
        CRC32Table = []
        CRC = 0xFFFFFFFF

        # Initialize the table if it is not already initialized
        if CRC32TableInit == 0:
            Magic = 0xEDB88320  # The bit-wise reverse order of 0x4c11db7 is 0xedb88320

            for i in range(256):
                Data = i
                for j in range(8):
                    b = Data & 1
                    Data = Data >> 1
                    if b != 0:
                        Data = Data ^ Magic

                CRC32Table.append(Data)

            # Sets the table initialization flag
            CRC32TableInit = 1

        for i in range(SrcDataSize):
            tmp = (CRC ^ SrcData[i]) % 256
            CRC = CRC32Table[tmp] ^ (CRC >> 8)

        return CRC ^ 0xFFFFFFFF

    def keyConvFileRead(
        self,
        data: bytearray,
        size: int,
        key: bytearray,
        position: int
    ) -> bytearray:
        pos = 0

        if key is not None:
            # Retrieve the location of the file.
            if position == -1:
                pos = self.fp.tell()
            else:
                pos = position

        # fetch
        data = bytearray(self.fp.read(size))  # For assignment in keyConv data[i] ^= key[j]

        if key is not None:
            # Xor operation with data using key string
            data = self.keyConv(data, size, pos, key)

        return data

    def keyConv(
        self, data: bytearray, size: int, position: int, key: bytearray
    ) -> bytes:
        if self.newCrypt:
            wolf_crypt(self.specialKey, data, 0, position, position + size)
            return data

        if key is None:
            return data

        if not isinstance(data, bytearray):
            data = bytearray(data)

        position %= DXA_KEY_BYTES

        if size < 0x100000000:
            j = position % 0xFFFFFFFF
            for i in range(size):
                data[i] ^= key[j]
                j += 1
                if j == DXA_KEY_BYTES:
                    j = 0
        else:
            j = position
            for i in range(size):
                data[i] ^= key[j]
                j += 1
                if j == DXA_KEY_BYTES:
                    j = 0

        return data

    def decode(self, src, dest) -> tuple:
        srcp = src

        destsize = struct.unpack("I", srcp[0:4])[0]
        srcsize = struct.unpack("I", srcp[4:8])[0] - 9

        keycode = srcp[8]

        if dest is None:
            return destsize

        sp = srcp[9:]

        tda = bytearray([0] * destsize)
        tdac = 0

        while srcsize > 0:
            if sp[0] != keycode:
                tda[tdac] = sp[0]
                tdac += 1
                sp = sp[1:]
                srcsize -= 1
                continue

            if sp[1] == keycode:
                tda[tdac] = keycode % 256
                tdac += 1
                sp = sp[2:]
                srcsize -= 2
                continue

            code = sp[1]

            if code > keycode:
                code -= 1

            sp = sp[2:]
            srcsize -= 2

            conbo = code >> 3
            if code & (0x1 << 2):
                conbo |= sp[0] << 5
                sp = sp[1:]
                srcsize -= 1

            conbo += self.MIN_COMPRESS

            indexsize = code & 0x3
            if indexsize == 0:
                index = sp[0]
                sp = sp[1:]
                srcsize -= 1
            elif indexsize == 1:
                index = struct.unpack("H", sp[0:2])[0]
                sp = sp[2:]
                srcsize -= 2
            elif indexsize == 2:
                index = struct.unpack("H", sp[0:2])[0] | (sp[2] << 16)
                sp = sp[3:]
                srcsize -= 3

            index += 1

            if index < conbo:
                num = index
                while conbo > num:
                    copied_bytes = tda[tdac - num : tdac - num + num]
                    tda[tdac : tdac + num] = copied_bytes
                    tdac += num
                    conbo -= num
                    num += num
                if conbo != 0:
                    copied_bytes = tda[tdac - num : tdac - num + conbo]
                    tda[tdac : tdac + conbo] = copied_bytes
                    tdac += conbo
            else:
                copied_bytes = tda[tdac - index : tdac - index + conbo]
                tda[tdac : tdac + conbo] = copied_bytes
                tdac += conbo

        return (tda, destsize)

    def directoryDecode(
        self,
        directoryInfo: DARC_DIRECTORY,
        key: bytearray,
        keyString: str,
        keyStringBytes: int,
    ) -> None:
        """
        Recursively get all directory information from directoryTable:
            Directory Name
            Information about files inside directory (actual files and other directories)
        """

        # Save current directory
        old_directory = self.directory

        if (
            directoryInfo.directoryAddress != 0xFFFFFFFFFFFFFFFF
            and directoryInfo.parentDirectoryAddress != 0xFFFFFFFFFFFFFFFF
        ):
            dirFile = DARC_FILEHEAD(self.fileTable[directoryInfo.directoryAddress :])
            pName = self.getOriginalFileName(self.nameTable[dirFile.nameAddress :])
            self.directory = self.directory / pName

        # Get info about file sinside this directory
        for i in range(directoryInfo.fileHeadNum):
            offset = len(DARC_FILEHEAD()) * i
            fileInfo = DARC_FILEHEAD(
                self.fileTable[directoryInfo.fileHeadAddress + offset :]
            )

            # Is the file another directory?
            if fileInfo.attributes & FILE_ATTRIBUTE_DIRECTORY:
                # Get that info too
                self.directoryDecode(
                    DARC_DIRECTORY(self.directoryTable[fileInfo.dataAddress :]),
                    key,
                    keyString,
                    keyStringBytes,
                )
            else:
                # It's an actual file
                pName = self.getOriginalFileName(self.nameTable[fileInfo.nameAddress :])
                filePath = self.directory / pName

                archivedFile = ArchivedFile()
                archivedFile.filePath = filePath
                archivedFile.compressed = fileInfo.pressDataSize != 0xFFFFFFFFFFFFFFFF
                archivedFile.huffmanCompressed = (
                    fileInfo.huffPressDataSize != 0xFFFFFFFFFFFFFFFF
                )
                archivedFile.key = None
                archivedFile.dataStart = (
                    self.archiveHead.dataStartAddress + fileInfo.dataAddress
                )
                archivedFile.dataSize = fileInfo.dataSize
                archivedFile.pressDataSize = fileInfo.pressDataSize
                archivedFile.huffPressDataSize = fileInfo.huffPressDataSize

                # Create individual file keys
                if not self.noKey:
                    keyStringBuffer = self.createKeyFileString(
                        keyString, keyStringBytes, directoryInfo, fileInfo
                    )
                    keyStringBufferBytes = len(keyStringBuffer)
                    lKey = self.keyCreate(
                        keyStringBuffer,
                        keyStringBufferBytes,
                        bytearray([0] * DXA_KEY_BYTES),
                    )
                    archivedFile.key = lKey

                self.archivedFiles.append(archivedFile)

            if i == directoryInfo.fileHeadNum - 1:
                break

        # Like going one directory up ../
        self.directory = old_directory

    def getOriginalFileName(self, fileNameTable) -> Path:
        filename_start_pos = fileNameTable[0] * 4 + 4
        null_pos = fileNameTable[filename_start_pos:].find(0x0)
        pName = fileNameTable[filename_start_pos : filename_start_pos + null_pos]
        try:
            return Path(pName.decode("utf8"))
        except UnicodeDecodeError:
            return Path(pName.decode("cp932"))  # For Japanese characters

    def createKeyFileString(
        self,
        keyString,
        keyStringBytes,
        directory: DARC_DIRECTORY,
        fileHead: DARC_FILEHEAD,
    ) -> bytearray:
        # At the end of the day this create a key that is comprised of
        # keyString + FILENAME + PARENT DIRECTORY [ + PARENT PARENT DIRECTORY ]
        # So the key for ./test1/test2/test3/file.txt
        # would be keyStringFILE.TXTTEST3TEST2TEST1
        fileString = bytearray([0] * DXA_KEY_STRING_MAXLENGTH)
        # First, set the password string
        if keyString is not None and keyStringBytes != 0:
            fileString[:keyStringBytes] = keyString[:keyStringBytes]
            fileString[keyStringBytes] = 0
            startAddr = keyStringBytes
        else:
            fileString[0] = 0
            startAddr = 0

        og_startAddr = startAddr

        fileString[DXA_KEY_STRING_MAXLENGTH - 8 : DXA_KEY_STRING_MAXLENGTH] = bytearray(
            b"00000000"
        )

        src = self.nameTable[fileHead.nameAddress + 4 :]
        amount = (DXA_KEY_STRING_MAXLENGTH - 8) - og_startAddr
        end_string = min(src.find(0x0), amount - 1)
        copied = src[:end_string]
        fileString[startAddr : startAddr + len(copied)] = copied
        startAddr = startAddr + len(copied)

        curr_dir = directory
        while (
            curr_dir is not None
            and curr_dir.directoryAddress != 0xFFFFFFFFFFFFFFFF
            and curr_dir.parentDirectoryAddress != 0xFFFFFFFFFFFFFFFF
        ):
            if curr_dir.directoryAddress + len(DARC_FILEHEAD()) > len(self.fileTable):
                break
            fileHead = DARC_FILEHEAD(self.fileTable[curr_dir.directoryAddress :])
            src = self.nameTable[fileHead.nameAddress + 4 :]
            amount = (DXA_KEY_STRING_MAXLENGTH - 8) - og_startAddr
            end_string = min(src.find(0x0), amount - 1)
            copied = src[:end_string]
            fileString[startAddr : startAddr + len(copied)] = copied
            startAddr = startAddr + len(copied)
            if (
                curr_dir.parentDirectoryAddress == 0xFFFFFFFFFFFFFFFF
                or curr_dir.parentDirectoryAddress + len(DARC_DIRECTORY()) > len(self.directoryTable)
            ):
                break
            curr_dir = DARC_DIRECTORY(
                self.directoryTable[curr_dir.parentDirectoryAddress :]
            )

        new_key = fileString[:startAddr]
        return new_key

    def extractAll(self) -> None:
        for archivedFile in self.archivedFiles:
            self.extractFile(archivedFile)

    def extractFile(self, archivedFile: ArchivedFile, dest=None) -> None:
        if dest is None:
            if not archivedFile.filePath.parent.exists():
                archivedFile.filePath.parent.mkdir(parents=True)
            destP = open(archivedFile.filePath, mode="wb")
            should_close = True
        else:
            destP = dest
            should_close = False

        if archivedFile.dataSize != 0:
            outputSize = archivedFile.dataSize

            if self.fp.tell() != archivedFile.dataStart:
                self.fp.seek(archivedFile.dataStart, SEEK_SET)

            if archivedFile.compressed:
                outputSize += archivedFile.pressDataSize

            if archivedFile.huffmanCompressed:
                outputSize += archivedFile.huffPressDataSize

            output = bytearray([0] * (outputSize))

            # If there's huffman compression
            if archivedFile.huffmanCompressed:
                keyConvFileReadSize = (
                    archivedFile.pressDataSize
                    if archivedFile.compressed
                    else archivedFile.dataSize
                )

                read = self.keyConvFileRead(
                    output,
                    archivedFile.huffPressDataSize,
                    archivedFile.key,
                    archivedFile.dataSize if self.newCrypt else 0,
                )
                output[: len(read)] = read

                (decoded, _) = huffman_Decode(
                    output, output[archivedFile.huffPressDataSize :]
                )
                output[archivedFile.huffPressDataSize :] = decoded

                if (
                    self.archiveHead.huffmanEncodeKB != 0xFF
                    and keyConvFileReadSize
                    > self.archiveHead.huffmanEncodeKB * 1024 * 2
                ):
                    amount_to_move = self.archiveHead.huffmanEncodeKB * 1024
                    start_dest = (
                        archivedFile.huffPressDataSize
                        + keyConvFileReadSize
                        - self.archiveHead.huffmanEncodeKB * 1024
                    )
                    start_src = (
                        archivedFile.huffPressDataSize
                        + self.archiveHead.huffmanEncodeKB * 1024
                    )
                    moved_bytes = output[start_src : start_src + amount_to_move]
                    output[start_dest : start_dest + amount_to_move] = moved_bytes

                    data = self.keyConvFileRead(
                        output[
                            archivedFile.huffPressDataSize
                            + self.archiveHead.huffmanEncodeKB * 1024 :
                        ],
                        keyConvFileReadSize
                        - self.archiveHead.huffmanEncodeKB * 1024 * 2,
                        archivedFile.key,
                        archivedFile.dataSize + archivedFile.huffPressDataSize,
                    )
                    output[
                        archivedFile.huffPressDataSize
                        + self.archiveHead.huffmanEncodeKB * 1024 :
                        archivedFile.huffPressDataSize
                        + self.archiveHead.huffmanEncodeKB * 1024
                        + len(data)
                    ] = data

                if archivedFile.compressed:
                    (decoded, _) = self.decode(
                        output[archivedFile.huffPressDataSize :],
                        output[
                            archivedFile.huffPressDataSize
                            + archivedFile.pressDataSize :
                        ],
                    )

                    output[
                        archivedFile.huffPressDataSize + archivedFile.pressDataSize :
                    ] = decoded
                    written_bytes = destP.write(
                        output[
                            archivedFile.huffPressDataSize
                            + archivedFile.pressDataSize : archivedFile.huffPressDataSize
                            + archivedFile.pressDataSize
                            + archivedFile.dataSize
                        ]
                    )
                else:
                    written_bytes = destP.write(
                        output[
                            archivedFile.huffPressDataSize : archivedFile.huffPressDataSize
                            + archivedFile.dataSize
                        ]
                    )

            else:
                # There's no huffman compression, check for regular compression
                if archivedFile.compressed:
                    read = self.keyConvFileRead(
                        output,
                        archivedFile.pressDataSize,
                        archivedFile.key,
                        archivedFile.dataSize if self.newCrypt else 0,
                    )
                    output[: len(read)] = read

                    (decoded, _) = self.decode(
                        output, output[archivedFile.pressDataSize :]
                    )
                    output[archivedFile.pressDataSize :] = decoded

                    destP.write(
                        output[
                            archivedFile.pressDataSize : archivedFile.pressDataSize
                            + archivedFile.dataSize
                        ]
                    )
                else:
                    writeSize = 0
                    while writeSize < archivedFile.dataSize:
                        if archivedFile.dataSize - writeSize > DXA_BUFFERSIZE:
                            moveSize = DXA_BUFFERSIZE
                        else:
                            moveSize = archivedFile.dataSize - writeSize

                        read = self.keyConvFileRead(
                            output,
                            moveSize,
                            archivedFile.key,
                            writeSize,
                        )
                        output[: len(read)] = read

                        destP.write(output[:moveSize])

                        writeSize += moveSize

        if should_close:
            destP.close()

    def extract_file_bytes(self, archivedFile: ArchivedFile) -> bytes:
        bio = io.BytesIO()
        self.extractFile(archivedFile, dest=bio)
        return bio.getvalue()


    def encode(self, src, dest=None):
        """
        LZ compression function for DXArchive
        """
        srcsize = len(src)

        if dest is None:
            # Just calculate the size
            return srcsize + (srcsize // 8) + 9

        # Find a character that appears least in the source data for use as an escape character
        keycode = 0
        keycodecount = 0xffffffff

        # Count occurrences of each byte value
        count = [0] * 256
        for i in range(srcsize):
            count[src[i]] += 1

        # Find the least common byte value
        for i in range(256):
            if count[i] < keycodecount:
                keycode = i
                keycodecount = count[i]

        # Write header
        dest[0:4] = struct.pack("I", srcsize)
        dest[4:8] = struct.pack("I", 0)  # Will be filled in later
        dest[8] = keycode

        dp = 9  # Destination pointer
        sp = 0  # Source pointer

        # Process the source data
        while sp < srcsize:
            # If the current byte is the keycode, we need to escape it
            if src[sp] == keycode:
                dest[dp] = keycode
                dp += 1
                dest[dp] = keycode
                dp += 1
                sp += 1
                continue

            # Try to find a match
            maxlen = 0
            maxpos = 0

            # Look back for matches up to MAX_POSITION
            searchlen = min(sp, self.MAX_POSITION)
            for i in range(1, searchlen + 1):
                pos = sp - i
                # Check how many bytes match
                matchlen = 0
                while sp + matchlen < srcsize and src[pos + matchlen] == src[sp + matchlen]:
                    matchlen += 1
                    if matchlen >= self.MAX_COPYSIZE:
                        break
                    if pos + matchlen >= sp:
                        # Don't go beyond current position
                        break

                if matchlen > maxlen:
                    maxlen = matchlen
                    maxpos = i

            # If we found a good match, encode it
            if maxlen >= self.MIN_COMPRESS:
                # Calculate the code value
                code = 0
                index = maxpos - 1

                # Determine index size
                if index <= 0xff:
                    indexsize = 0
                elif index <= 0xffff:
                    indexsize = 1
                else:
                    indexsize = 2

                # Set the combo size
                conbo = maxlen - self.MIN_COMPRESS

                # Create the code
                extra_byte = None
                if conbo >= 0x20:
                    code = ((conbo & 0x1f) << 3) | 0x04 | indexsize
                    extra_byte = (conbo >> 5) & 0xff
                else:
                    code = (conbo << 3) | indexsize

                # If the code value is the keycode or larger, increment it
                if code >= keycode:
                    code += 1

                # Write the escape code and the LZ code
                dest[dp] = keycode
                dp += 1
                dest[dp] = code
                dp += 1
                if extra_byte is not None:
                    dest[dp] = extra_byte
                    dp += 1

                # Write the index value based on size
                if indexsize == 0:
                    dest[dp] = index & 0xff
                    dp += 1
                elif indexsize == 1:
                    dest[dp:dp+2] = struct.pack("H", index)
                    dp += 2
                elif indexsize == 2:
                    dest[dp:dp+2] = struct.pack("H", index & 0xffff)
                    dp += 2
                    dest[dp] = (index >> 16) & 0xff
                    dp += 1

                sp += maxlen
            else:
                # Just copy the byte as is
                dest[dp] = src[sp]
                dp += 1
                sp += 1

        # Update the compressed size in the header
        dest[4:8] = struct.pack("I", dp)

        return dp

    def create_archive(self, output_path, input_files, key_string=None, use_compression=True, use_huffman=True, base_dir=None):
        """
        Create a DXArchive from a list of files
        """
        if key_string is None:
            key_string = defaultKeyString
            self.noKey = True
        else:
            self.noKey = False

        key = self.keyCreate(key_string, len(key_string), bytearray([0] * DXA_KEY_BYTES))

        # Open output file
        self.output_fp = open(output_path, "wb")

        # Initialize archive header
        self.archiveHead = DARC_HEAD()
        self.archiveHead.head = DXA_HEAD
        self.archiveHead.version = DXA_VER
        self.archiveHead.headSize = 0  # To be filled in later
        self.archiveHead.dataStartAddress = 0  # To be filled in later
        self.archiveHead.fileNameTableStartAddress = len(DARC_HEAD())
        self.archiveHead.fileTableStartAddress = 0  # To be filled in later
        self.archiveHead.directoryTableStartAddress = 0  # To be filled in later
        self.archiveHead.charCodeFormat = 0  # UTF-8
        self.archiveHead.flags = DXA_FLAG_NO_KEY if self.noKey else 0
        self.archiveHead.huffmanEncodeKB = 0 if not use_huffman else 0x10  # 16KB

        # Write placeholder header
        self.output_fp.write(bytearray(len(DARC_HEAD())))

        # Build file structure
        file_list = []
        dir_structure = {}

        # Add root directory
        root_dir = {
            'name': '',
            'files': [],
            'dirs': {},
            'parent': None,
            'address': 0
        }
        dir_structure['/'] = root_dir

        base_path = Path(base_dir) if base_dir else None

        # Process input files
        for file_path in input_files:
            # Convert to Path object if it's a string
            if isinstance(file_path, str):
                file_path = Path(file_path)

            # Skip if file doesn't exist
            if not file_path.exists():
                print(f"Warning: {file_path} does not exist, skipping")
                continue

            # Get file stats
            stats = file_path.stat()

            # Create file entry
            file_entry = {
                'path': file_path,
                'name': file_path.name,
                'size': stats.st_size,
                'create_time': int(stats.st_ctime),
                'access_time': int(stats.st_atime),
                'modify_time': int(stats.st_mtime),
                'is_dir': file_path.is_dir()
            }

            # Determine directory path
            if base_path:
                try:
                    rel_parent = file_path.relative_to(base_path).parent
                except ValueError:
                    rel_parent = file_path.parent
            else:
                rel_parent = Path("")

            rel_str = str(rel_parent).replace('\\', '/')
            if rel_str in ('.', '', '/'):
                dir_path = '/'
            else:
                dir_path = '/' + rel_str.strip('/') + '/'

            # Create directory structure if needed
            current_dir = '/'
            for part in dir_path.split('/'):
                if not part:
                    continue

                next_dir = current_dir + part + '/'
                if next_dir not in dir_structure:
                    dir_structure[next_dir] = {
                        'name': part,
                        'files': [],
                        'dirs': {},
                        'parent': current_dir,
                        'address': 0
                    }
                    dir_structure[current_dir]['dirs'][part] = next_dir

                current_dir = next_dir

            # Add file to directory
            dir_structure[current_dir]['files'].append(file_entry)
            file_list.append(file_entry)

        # Build name table, file table, and directory table
        name_table = bytearray()
        file_table = bytearray()
        directory_table = bytearray()
        self.nameTable = name_table
        self.fileTable = file_table
        self.directoryTable = directory_table

        # Add names to name table and track their addresses
        name_addresses = {}

        def add_name(name):
            if name in name_addresses:
                return name_addresses[name]

            # Store the name in UTF-8
            encoded_name = name.encode('utf-8')
            # Format: 4-byte header (byte 0 is 0 for 4-byte offset) + string + null terminator
            entry = struct.pack("BBH", 0, 0, len(encoded_name)) + encoded_name + b'\x00'
            addr = len(name_table)
            name_table.extend(entry)
            name_addresses[name] = addr
            return addr

        # Process directories
        dir_addresses = {}
        file_head_addresses = {}

        # First pass: assign addresses to directories
        for dir_path, dir_info in dir_structure.items():
            dir_addresses[dir_path] = len(directory_table)
            # Placeholder for directory structure initialized with 0xFF (unset)
            directory_table.extend(b'\xff' * len(DARC_DIRECTORY()))

        # Second pass: fill in directory structures and contiguous file table entries
        data_pos = 0
        file_payloads = []

        for dir_path, dir_info in dir_structure.items():
            dir_addr = dir_addresses[dir_path]

            # Create directory entry
            directory_entry = DARC_DIRECTORY()

            # Set parent directory address
            if dir_info['parent'] is not None:
                directory_entry.parentDirectoryAddress = dir_addresses[dir_info['parent']]
            else:
                directory_entry.parentDirectoryAddress = 0xFFFFFFFFFFFFFFFF

            # Set file head info
            file_head_addr = len(file_table)
            file_head_addresses[dir_path] = file_head_addr
            directory_entry.fileHeadNum = len(dir_info['dirs']) + len(dir_info['files'])
            directory_entry.fileHeadAddress = file_head_addr

            # Update directory table entry (preserve directoryAddress if already set by parent)
            existing_dir_address = struct.unpack_from("Q", directory_table, dir_addr)[0]
            directory_entry.directoryAddress = existing_dir_address

            directory_bytes = struct.pack(
                "QQQQ",
                directory_entry.directoryAddress,
                directory_entry.parentDirectoryAddress,
                directory_entry.fileHeadNum,
                directory_entry.fileHeadAddress
            )
            directory_table[dir_addr:dir_addr+len(directory_bytes)] = directory_bytes

            # 1. Add subdirectory entries to file_table for this directory
            for subdir_name, subdir_path in dir_info['dirs'].items():
                subdir_filehead_offset = len(file_table)
                # Link the child directory's directoryAddress to this FILEHEAD offset
                struct.pack_into("Q", directory_table, dir_addresses[subdir_path], subdir_filehead_offset)

                file_head = DARC_FILEHEAD()
                file_head.nameAddress = add_name(subdir_name)
                file_head.attributes = FILE_ATTRIBUTE_DIRECTORY
                file_head.time = DARC_FILETIME()
                file_head.dataAddress = dir_addresses[subdir_path]
                file_head.dataSize = 0xFFFFFFFFFFFFFFFF
                file_head.pressDataSize = 0xFFFFFFFFFFFFFFFF
                file_head.huffPressDataSize = 0xFFFFFFFFFFFFFFFF

                file_table.extend(struct.pack(
                    "QQQQQQQQQ",
                    file_head.nameAddress,
                    file_head.attributes,
                    0, 0, 0,
                    file_head.dataAddress,
                    file_head.dataSize,
                    file_head.pressDataSize,
                    file_head.huffPressDataSize
                ))

            # 2. Add regular files to file_table for this directory
            for file_entry in dir_info['files']:
                if file_entry['is_dir']:
                    continue

                file_head = DARC_FILEHEAD()
                file_head.nameAddress = add_name(file_entry['name'])
                file_head.attributes = 0
                file_head.time = DARC_FILETIME()
                file_head.time.create = file_entry['create_time']
                file_head.time.lastAccess = file_entry['access_time']
                file_head.time.lastWrite = file_entry['modify_time']

                with open(file_entry['path'], 'rb') as f:
                    file_data = f.read()

                file_head.dataSize = len(file_data)
                file_head.dataAddress = data_pos

                if use_compression and len(file_data) > self.MIN_COMPRESS:
                    lz_buffer_size = len(file_data) + (len(file_data) // 8) + 9
                    lz_buffer = bytearray(lz_buffer_size)
                    compressed_size = self.encode(file_data, lz_buffer)

                    if compressed_size < len(file_data):
                        file_head.pressDataSize = compressed_size
                        compressed_data = lz_buffer[:compressed_size]

                        if use_huffman and hasattr(self, 'huffman_Encode'):
                            huffman_buffer = bytearray()
                            huffman_buffer, huffman_size = huffman_Encode(compressed_data, len(compressed_data), huffman_buffer)

                            if huffman_size < compressed_size:
                                file_head.huffPressDataSize = huffman_size
                                final_data = huffman_buffer[:huffman_size]
                            else:
                                file_head.huffPressDataSize = 0xFFFFFFFFFFFFFFFF
                                final_data = compressed_data
                        else:
                            file_head.huffPressDataSize = 0xFFFFFFFFFFFFFFFF
                            final_data = compressed_data
                    else:
                        file_head.pressDataSize = 0xFFFFFFFFFFFFFFFF
                        file_head.huffPressDataSize = 0xFFFFFFFFFFFFFFFF
                        final_data = file_data
                else:
                    file_head.pressDataSize = 0xFFFFFFFFFFFFFFFF
                    file_head.huffPressDataSize = 0xFFFFFFFFFFFFFFFF
                    final_data = file_data

                if not self.noKey:
                    key_string_buffer = self.createKeyFileString(
                        key_string,
                        len(key_string),
                        DARC_DIRECTORY(directory_table[dir_addresses[dir_path]:]),
                        file_head
                    )
                    file_key = self.keyCreate(
                        key_string_buffer,
                        len(key_string_buffer),
                        bytearray([0] * DXA_KEY_BYTES)
                    )
                    final_data = self.keyConv(final_data, len(final_data), 0, file_key)

                data_pos += len(final_data)
                file_payloads.append(final_data)

                file_table.extend(struct.pack(
                    "QQQQQQQQQ",
                    file_head.nameAddress,
                    file_head.attributes,
                    file_head.time.create,
                    file_head.time.lastAccess,
                    file_head.time.lastWrite,
                    file_head.dataAddress,
                    file_head.dataSize,
                    file_head.pressDataSize,
                    file_head.huffPressDataSize
                ))

        # Update header with table addresses
        self.archiveHead.dataStartAddress = len(DARC_HEAD())
        self.archiveHead.fileNameTableStartAddress = self.archiveHead.dataStartAddress + data_pos
        self.archiveHead.fileTableStartAddress = self.archiveHead.fileNameTableStartAddress + len(name_table)
        self.archiveHead.directoryTableStartAddress = self.archiveHead.fileTableStartAddress + len(file_table)

        # Combine all tables
        header_data = name_table + file_table + directory_table
        self.archiveHead.headSize = len(header_data)

        # Compress header if needed
        if not (self.archiveHead.flags & DXA_FLAG_NO_HEAD_PRESS):
            # LZ compression
            lz_buffer_size = len(header_data) + (len(header_data) // 8) + 9
            lz_buffer = bytearray(lz_buffer_size)
            lz_size = self.encode(header_data, lz_buffer)

            # Huffman compression
            huff_buffer = bytearray()
            huff_buffer, huff_size = huffman_Encode(lz_buffer[:lz_size], lz_size, huff_buffer)

            # Use the compressed header
            compressed_header = huff_buffer[:huff_size]

            # Encrypt if needed
            if not self.noKey:
                compressed_header = self.keyConv(compressed_header, len(compressed_header), 0, key)
        else:
            compressed_header = header_data
            if not self.noKey:
                compressed_header = self.keyConv(compressed_header, len(compressed_header), 0, key)

        # Write header at start of file
        self.output_fp.seek(0)
        header_bytes = struct.pack(
            "HHIQQQQIIB14sB",
            self.archiveHead.head,
            self.archiveHead.version,
            self.archiveHead.headSize,
            self.archiveHead.dataStartAddress,
            self.archiveHead.fileNameTableStartAddress,
            self.archiveHead.fileTableStartAddress,
            self.archiveHead.directoryTableStartAddress,
            self.archiveHead.charCodeFormat,
            self.archiveHead.flags,
            self.archiveHead.huffmanEncodeKB,
            bytearray(14),  # reserve
            0  # padding
        )
        self.output_fp.write(header_bytes)

        # Write all file data payloads
        for payload in file_payloads:
            self.output_fp.write(payload)

        # Write the compressed header tables at the end
        self.output_fp.write(compressed_header)

        # Close the file
        self.output_fp.close()
        return True

    def add_to_archive(self, archive_path, files_to_add, key_string=None, use_compression=True, use_huffman=True):
        """
        Add files to an existing archive
        """
        # First, extract the existing archive to a temporary location
        temp_dir = Path("temp_extract")
        if temp_dir.exists():
            import shutil
            shutil.rmtree(temp_dir)
        temp_dir.mkdir()

        # Load the archive
        if not self.loadArchive(archive_path, temp_dir, key_string):
            print(f"Failed to load archive {archive_path}")
            return False

        # Extract all files
        self.extractAll()

        # Get list of all extracted files
        extracted_files = []
        for root, dirs, files in os.walk(temp_dir):
            for file in files:
                file_path = Path(root) / file
                relative_path = file_path.relative_to(temp_dir)
                extracted_files.append(relative_path)

        # Add new files to the extracted files
        for file_to_add in files_to_add:
            file_path = Path(file_to_add)
            if file_path.exists():
                # Copy the file to the temp directory
                dest_path = temp_dir / file_path.name
                with open(file_path, 'rb') as src, open(dest_path, 'wb') as dst:
                    dst.write(src.read())
                extracted_files.append(Path(file_path.name))

        # Create a new archive with all files
        all_files = [temp_dir / file for file in extracted_files]
        return self.create_archive(archive_path, all_files, key_string, use_compression, use_huffman)

    def __enter__(self):
        return self

    def __exit__(self, exc_type, exc_value, exc_traceback):
        if self.fp and not self.fp.closed:
            self.fp.close()

def main():
    parser = argparse.ArgumentParser(description='DXArchive tool for extracting and creating DX archives')

    # Create subparsers for different commands
    subparsers = parser.add_subparsers(dest='command', help='Command to execute')

    # Extract command
    extract_parser = subparsers.add_parser('extract', help='Extract files from archive')
    extract_parser.add_argument('archive', help='Path to the archive file')
    extract_parser.add_argument('-o', '--output', default='output', help='Output directory')
    extract_parser.add_argument('-k', '--key', default="WLFRPrO!p(;s5((8P@((UFWlu$#5(=", help='Key string for encrypted archives')
    extract_parser.add_argument('-f', '--file', help='Extract only this specific file')

    # Create command
    create_parser = subparsers.add_parser('create', help='Create a new archive')
    create_parser.add_argument('archive', help='Path to the new archive file')
    create_parser.add_argument('-i', '--input', nargs='+', required=True, help='Files or directories to add to the archive')
    create_parser.add_argument('-k', '--key', help='Key string for encryption')
    create_parser.add_argument('--no-compression', action='store_true', help='Disable compression')
    create_parser.add_argument('--no-huffman', action='store_true', help='Disable Huffman compression')

    # Add command
    add_parser = subparsers.add_parser('add', help='Add files to an existing archive')
    add_parser.add_argument('archive', help='Path to the existing archive file')
    add_parser.add_argument('-i', '--input', nargs='+', required=True, help='Files or directories to add to the archive')
    add_parser.add_argument('-k', '--key', help='Key string for encryption')
    add_parser.add_argument('--no-compression', action='store_true', help='Disable compression')
    add_parser.add_argument('--no-huffman', action='store_true', help='Disable Huffman compression')

    # List command
    list_parser = subparsers.add_parser('list', help='List files in the archive')
    list_parser.add_argument('archive', help='Path to the archive file')
    list_parser.add_argument('-k', '--key', help='Key string for encrypted archives')

    # Parse arguments
    args = parser.parse_args()

    # Handle different commands
    if args.command == 'extract':
        # Convert key string to bytearray if provided
        key_string = bytearray(args.key.encode('utf-8')) if args.key else None

        with DXArchive() as archive:
            if archive.loadArchive(Path(args.archive), Path(args.output), key_string):
                if args.file:
                    # Extract specific file
                    for file in archive.archivedFiles:
                        if str(file.filePath) == args.file or file.filePath.name == args.file:
                            print(f"Extracting {file.filePath}...")
                            archive.extractFile(file)
                            break
                    else:
                        print(f"File {args.file} not found in archive")
                else:
                    # Extract all files
                    for file in archive.archivedFiles:
                        print(f"Extracting {file.filePath}...")
                        archive.extractFile(file)

                print(f"Extraction complete to {args.output}")
            else:
                print(f"Failed to load archive {args.archive}")

    elif args.command == 'create':
        # Convert key string to bytearray if provided
        key_string = bytearray(args.key.encode('utf-8')) if args.key else None

        # Expand directories to file lists
        input_files = []
        for input_path in args.input:
            path = Path(input_path)
            if path.is_dir():
                for root, dirs, files in os.walk(path):
                    for file in files:
                        file_path = Path(root) / file
                        input_files.append(file_path)
            else:
                input_files.append(path)

        with DXArchive() as archive:
            if archive.create_archive(
                Path(args.archive),
                input_files,
                key_string,
                not args.no_compression,
                not args.no_huffman
            ):
                print(f"Archive {args.archive} created successfully with {len(input_files)} files")
            else:
                print(f"Failed to create archive {args.archive}")

    elif args.command == 'add':
        # Convert key string to bytearray if provided
        key_string = bytearray(args.key.encode('utf-8')) if args.key else None

        # Expand directories to file lists
        input_files = []
        for input_path in args.input:
            path = Path(input_path)
            if path.is_dir():
                for root, dirs, files in os.walk(path):
                    for file in files:
                        file_path = Path(root) / file
                        input_files.append(file_path)
            else:
                input_files.append(path)

        with DXArchive() as archive:
            if archive.add_to_archive(
                Path(args.archive),
                input_files,
                key_string,
                not args.no_compression,
                not args.no_huffman
            ):
                print(f"Added {len(input_files)} files to archive {args.archive}")
            else:
                print(f"Failed to add files to archive {args.archive}")

    elif args.command == 'list':
        # Convert key string to bytearray if provided
        key_string = bytearray(args.key.encode('utf-8')) if args.key else None

        with DXArchive() as archive:
            if archive.loadArchive(Path(args.archive), Path("."), key_string):
                print(f"Archive: {args.archive}")
                print(f"Number of files: {len(archive.archivedFiles)}")
                print("\nFiles:")
                for file in archive.archivedFiles:
                    compressed = "Compressed" if file.compressed else "Uncompressed"
                    huffman = ", Huffman" if file.huffmanCompressed else ""
                    size_text = f"{file.dataSize} bytes"
                    if file.compressed:
                        compression_ratio = (1 - (file.pressDataSize / file.dataSize)) * 100
                        size_text += f" ({compression_ratio:.1f}% compressed)"
                    print(f"{file.filePath} - {size_text} - {compressed}{huffman}")
            else:
                print(f"Failed to load archive {args.archive}")

    else:
        parser.print_help()

def extract_wolf_archive(archive_path, output_dir, key_string=None):
    archive = DXArchive()
    p_arch = Path(archive_path)
    p_out = Path(output_dir)
    p_out.mkdir(parents=True, exist_ok=True)
    k_str = key_string if key_string is not None else defaultKeyString
    if isinstance(k_str, str):
        k_str = bytearray(k_str.encode('utf-8') + b'\x00')
    elif isinstance(k_str, bytes):
        k_str = bytearray(k_str)
    
    success = archive.loadArchive(p_arch, p_out, k_str)
    if success is False:
        raise ValueError(f"Falha ao carregar arquivo Wolf/DXA: {archive_path}")
    archive.extractAll()
    if archive.fp:
        archive.fp.close()
    return True


def create_wolf_archive(output_path, input_dir, key_string=None, use_compression=True, use_huffman=True):
    archive = DXArchive()
    p_out = Path(output_path)
    p_in = Path(input_dir)
    input_files = [f for f in p_in.rglob('*') if f.is_file()]
    k_str = key_string if key_string is not None else defaultKeyString
    if isinstance(k_str, str):
        k_str = bytearray(k_str.encode('utf-8') + b'\x00')
    elif isinstance(k_str, bytes):
        k_str = bytearray(k_str)
    
    return archive.create_archive(p_out, input_files, k_str, use_compression, use_huffman, base_dir=p_in)


class WolfArchive:
    @staticmethod
    def extract_archive(archive_path, output_dir, key_string=None):
        return extract_wolf_archive(archive_path, output_dir, key_string=key_string)

    @staticmethod
    def create_archive(input_dir, output_path, key_string=None, use_compression=True, use_huffman=True):
        return create_wolf_archive(output_path, input_dir, key_string=key_string, use_compression=use_compression, use_huffman=use_huffman)

    @classmethod
    def list_entries_with_hashes(cls, archive_path, virtual_base_dir="", extensions=None, key_string=None):
        import hashlib
        archive = DXArchive()
        p_arch = Path(archive_path)
        if not p_arch.exists():
            return {}

        h = hashlib.sha256()
        with open(p_arch, 'rb') as f:
            for chunk in iter(lambda: f.read(1 << 20), b''):
                h.update(chunk)
        container_hash = h.hexdigest()

        k_str = key_string if key_string is not None else defaultKeyString
        if isinstance(k_str, str):
            k_str = bytearray(k_str.encode('utf-8') + b'\x00')
        elif isinstance(k_str, bytes):
            k_str = bytearray(k_str)

        try:
            success = archive.loadArchive(p_arch, Path("."), k_str)
            if not success:
                if archive.fp:
                    archive.fp.close()
                return {}

            exts = tuple(f".{e.lower().lstrip('.')}" for e in extensions) if extensions else None
            results = {}
            for af in archive.archivedFiles:
                rel_path = af.filePath.as_posix().lstrip('./')
                if virtual_base_dir:
                    full_rel = f"{virtual_base_dir}/{rel_path}".strip('/')
                else:
                    full_rel = rel_path

                if exts and not full_rel.lower().endswith(exts):
                    continue

                data = archive.extract_file_bytes(af)
                digest = hashlib.sha256(data).hexdigest()
                results[full_rel] = {
                    'sha256': digest,
                    'container': os.path.basename(archive_path),
                    'container_sha256': container_hash,
                }
            return results
        finally:
            if archive.fp:
                archive.fp.close()


if __name__ == "__main__":
    import argparse
    main()