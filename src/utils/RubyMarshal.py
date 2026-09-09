import io
import math
import re
import rubymarshal.classes as r_classes
import rubymarshal.constants as r_constants
import rubymarshal.reader as r_reader
import rubymarshal.writer as r_writer
from rubymarshal.classes import (
    Module,
    RubyObject,
    RubyString,
    Symbol,
    UserDef,
    UsrMarshal,
)
from rubymarshal.constants import (
    TYPE_ARRAY,
    TYPE_BIGNUM,
    TYPE_CLASS,
    TYPE_DATA,
    TYPE_FALSE,
    TYPE_FIXNUM,
    TYPE_FLOAT,
    TYPE_HASH,
    TYPE_IVAR,
    TYPE_LINK,
    TYPE_MODULE,
    TYPE_NIL,
    TYPE_OBJECT,
    TYPE_REGEXP,
    TYPE_STRING,
    TYPE_STRUCT,
    TYPE_SYMBOL,
    TYPE_SYMLINK,
    TYPE_TRUE,
    TYPE_USERDEF,
    TYPE_USRMARSHAL,
)
from rubymarshal.utils import (
    read_sbyte,
    read_ushort,
    write_sbyte,
    write_ubyte,
    write_ushort,
)


class RubyWriter(r_writer.Writer):
    """
    Writer de Ruby Marshal corrigido para RGSS3 / RPG Maker.

    Garante alinhamento 1:1 entre a contagem de objetos do Reader e Writer:
    - Rastreia instâncias de str e bytes na tabela de objetos (@ / TYPE_LINK)
    - Rastreia floats, bignums, regexps, modules e classes
    - Mantém referências fortes aos objetos serializados para impedir que o GC
      do Python recicle endereços id() durante a gravação
    - Grava o payload de bytes de strings sem duplicar o índice de objeto
    """

    def __init__(self, fd):
        super().__init__(fd)
        self._held_objects = []

    def must_write(self, obj):
        res = super().must_write(obj)
        if res:
            self._held_objects.append(obj)
        return res

    def _write_raw_bytes(self, b: bytes):
        """Grava a sequência de bytes primitiva TYPE_STRING sem registrar no catálogo de objetos."""
        self.fd.write(TYPE_STRING)
        self.write_long(len(b))
        self.fd.write(b)

    def write_bytes(self, obj: bytes):
        """Grava um objeto bytes do Python como TYPE_STRING registrado no catálogo de objetos."""
        if self.must_write(obj):
            self._write_raw_bytes(obj)

    def write_string(self, obj: str):
        """
        Grava uma string Python nativa como TYPE_IVAR + TYPE_STRING com atributo de encoding UTF-8 (E: True),
        garantindo registro prévio no catálogo de objetos.
        """
        if self.must_write(obj):
            encoded = obj.encode("utf-8")
            self.fd.write(TYPE_IVAR)
            self._write_raw_bytes(encoded)
            self.write_long(1)
            self.write(Symbol("E"))
            self.write(True)

    def write_ruby_string(self, obj: RubyString):
        """
        Grava um RubyString preservando seus atributos de instância,
        usando _write_raw_bytes para evitar contagem duplicada no catálogo.
        """
        if self.must_write(obj):
            encoding = "utf-8"
            attributes = obj.attributes
            if "E" in attributes and not attributes["E"]:
                encoding = "latin-1"
            elif "encoding" in attributes:
                encoding = attributes["encoding"].decode()
            else:
                attributes["E"] = True
            encoded = obj.encode(encoding)
            self.fd.write(TYPE_IVAR)
            self._write_raw_bytes(encoded)
            self.write_attributes(attributes)

    def write_float(self, obj: float):
        if self.must_write(obj):
            super().write_float(obj)

    def write_regexp(self, obj):
        if self.must_write(obj):
            super().write_regexp(obj)

    def write_module(self, obj: Module):
        if self.must_write(obj):
            super().write_module(obj)

    def write_class(self, obj):
        if self.must_write(obj):
            super().write_class(obj)

    def write_int(self, obj: int):
        if obj.bit_length() <= 5 * 8:
            self.fd.write(TYPE_FIXNUM)
            self.write_long(obj)
        else:
            if self.must_write(obj):
                self.fd.write(TYPE_BIGNUM)
                if obj < 0:
                    self.fd.write(b"-")
                else:
                    self.fd.write(b"+")
                abs_obj = abs(obj)
                size = int(math.ceil(abs_obj.bit_length() / 16.0))
                self.write_long(size)
                for _ in range(size):
                    self.write_short(abs_obj % 65536)
                    abs_obj //= 65536


class RubyReader(r_reader.Reader):
    """
    Reader de Ruby Marshal com tratamento resiliente de decodificação de strings.
    Fallback para latin1 em sequências binárias arbitrárias (ex: Scripts.rvdata2 compactados).
    """

    def read(self, in_ivar=False):
        result = None
        object_index = None
        re_flags = None

        token = self.fd.read(1)

        if token in (
            TYPE_CLASS,
            TYPE_MODULE,
            TYPE_FLOAT,
            TYPE_BIGNUM,
            TYPE_STRING,
            TYPE_REGEXP,
            TYPE_ARRAY,
            TYPE_HASH,
            TYPE_STRUCT,
            TYPE_OBJECT,
            TYPE_DATA,
            TYPE_USRMARSHAL,
            TYPE_USERDEF,
        ):
            object_index = len(self.objects)
            self.objects.append(None)

        if token == TYPE_NIL:
            pass
        elif token == TYPE_TRUE:
            result = True
        elif token == TYPE_FALSE:
            result = False
        elif token == TYPE_IVAR:
            result = self.read(in_ivar=True)
        elif token == TYPE_STRING:
            result = self.read_blob()
        elif token == TYPE_SYMBOL:
            result = self.read_symreal()
        elif token == TYPE_FIXNUM:
            result = self.read_long()
        elif token == TYPE_ARRAY:
            num_elements = self.read_long()
            result = [self.read() for _ in range(num_elements)]
        elif token == TYPE_HASH:
            num_elements = self.read_long()
            result = {}
            for _ in range(num_elements):
                key = self.ensure_hashable(self.read())
                value = self.read()
                result[key] = value
        elif token == TYPE_FLOAT:
            floatn = self.read_blob()
            floatn = floatn.split(b"\0")
            result = float(floatn[0].decode("utf-8"))
        elif token == TYPE_BIGNUM:
            sign = 1 if self.fd.read(1) == b"+" else -1
            num_elements = self.read_long()
            result = 0
            factor = 1
            for _ in range(num_elements):
                result += self.read_short() * factor
                factor *= 2**16
            result *= sign
        elif token == TYPE_REGEXP:
            result = self.read_blob()
            options = ord(self.fd.read(1))
            re_flags = 0
            if options & 1:
                re_flags |= re.IGNORECASE
            if options & 4:
                re_flags |= re.MULTILINE
        elif token == TYPE_USRMARSHAL:
            class_symbol = self.read()
            if not isinstance(class_symbol, Symbol):
                raise ValueError("invalid class name: %r" % class_symbol)
            class_name = class_symbol.name
            attr_list = self.read()
            python_class = self.registry.get(class_name, UsrMarshal)
            if not issubclass(python_class, UsrMarshal):
                raise ValueError(
                    "invalid class mapping for %r: %r should be a subclass of %r."
                    % (class_name, python_class, UsrMarshal)
                )
            result = python_class(class_name)
            result.marshal_load(attr_list)
        elif token == TYPE_SYMLINK:
            result = self.read_symlink()
        elif token == TYPE_LINK:
            link_id = self.read_long()
            if link_id > len(self.objects):
                raise ValueError(
                    "invalid link destination: %d should be lower than %d or equal."
                    % (link_id, len(self.objects))
                )
            result = self.objects[link_id]
            if result is None:
                raise ValueError(
                    "invalid link destination: Object id %d is not yet unmarshaled."
                    % link_id
                )
        elif token == TYPE_USERDEF:
            class_symbol = self.read()
            private_data = self.read_blob()
            if not isinstance(class_symbol, Symbol):
                raise ValueError("invalid class name: %r" % class_symbol)
            class_name = class_symbol.name
            python_class = self.registry.get(class_name, UserDef)
            if not issubclass(python_class, UserDef):
                raise ValueError(
                    "invalid class mapping for %r: %r should be a subclass of %r."
                    % (class_name, python_class, UserDef)
                )
            result = python_class(class_name)
            result._load(private_data)
        elif token == TYPE_MODULE:
            data = self.read_blob()
            module_name = data.decode()
            result = Module(module_name, None)
        elif token == TYPE_OBJECT:
            class_symbol = self.read()
            assert isinstance(class_symbol, Symbol)
            class_name = class_symbol.name
            python_class = self.registry.get(class_name, RubyObject)
            if not issubclass(python_class, RubyObject):
                raise ValueError(
                    "invalid class mapping for %r: %r should be a subclass of %r."
                    % (class_name, python_class, RubyObject)
                )
            attributes = self.read_attributes()
            result = python_class(class_name, attributes)
        elif token == r_constants.TYPE_EXTENDED:
            class_name = self.read_blob()
            result = r_classes.Extended(class_name, None)
        elif token == TYPE_CLASS:
            data = self.read_blob()
            class_name = data.decode()
            if class_name in self.registry:
                result = self.registry[class_name]
            else:
                result = type(
                    class_name.rpartition(":")[2],
                    (RubyObject,),
                    {"ruby_class_name": class_name},
                )
        else:
            raise ValueError("token %s is not recognized" % token)

        if in_ivar:
            attributes = self.read_attributes()
            if token in (TYPE_STRING, TYPE_REGEXP):
                encoding = self._get_encoding(attributes)
                try:
                    result = result.decode(encoding)
                except UnicodeDecodeError:
                    try:
                        result = result.decode("unicode-escape")
                    except UnicodeDecodeError:
                        result = result.decode("latin1", errors="replace")
                if attributes and token == TYPE_STRING:
                    result = RubyString(result, attributes)
            elif attributes:
                result.set_attributes(attributes)

        if token == TYPE_REGEXP:
            result = re.compile(str(result), re_flags)

        if object_index is not None:
            self.objects[object_index] = result
        return result


def write(fd, obj, cls=RubyWriter):
    """Escreve um objeto Python para um file descriptor no formato Ruby Marshal."""
    fd.write(b"\x04\x08")
    writer = cls(fd)
    writer.write(obj)


def writes(obj, cls=RubyWriter) -> bytes:
    """Escreve um objeto Python para bytes no formato Ruby Marshal."""
    fd = io.BytesIO()
    write(fd, obj, cls=cls)
    return fd.getvalue()


def load(fd, registry=None):
    """Lê um objeto Python a partir de um file descriptor no formato Ruby Marshal."""
    magic = fd.read(2)
    if magic != b"\x04\x08":
        raise ValueError(f"Invalid Ruby Marshal magic version: {magic!r}")
    reader = RubyReader(fd, registry=registry)
    return reader.read()


def loads(byte_text: bytes, registry=None):
    """Lê um objeto Python a partir de bytes no formato Ruby Marshal."""
    return load(io.BytesIO(byte_text), registry=registry)


# Monkeypatch transparente no rubymarshal para assegurar compatibilidade global
r_writer.Writer = RubyWriter
r_writer.write = write
r_writer.writes = writes

r_reader.Reader = RubyReader
r_reader.load = load
r_reader.loads = loads
