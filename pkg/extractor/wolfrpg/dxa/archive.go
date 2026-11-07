// Package dxa lê e grava contêineres DXA (DxLib), o formato dos arquivos .wolf
// do Wolf RPG Editor.
package dxa

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"strings"

	"golang.org/x/text/encoding/japanese"
)

const (
	headerSize     = 64
	versionDXA     = 8
	flagNoKey      = 1
	flagNoHeadComp = 2
	none           = ^uint64(0)
	attrDirectory  = 0x10
	maxKeyString   = 63
)

var (
	ErrNotArchive  = errors.New("dxa: not a DX archive")
	ErrUnsupported = errors.New("dxa: unsupported archive")
	errCorrupt     = errors.New("dxa: corrupt archive tables")

	defaultKey = []byte("DXBDXARC\x00")
)

type header struct {
	headSize        uint32
	dataStart       uint64
	nameTableStart  uint64
	fileTableStart  uint64
	dirTableStart   uint64
	flags           uint32
	huffmanEncodeKB uint8
}

func parseHeader(b []byte) (header, error) {
	if binary.LittleEndian.Uint16(b[0:]) != 0x5844 {
		return header{}, ErrNotArchive
	}
	if v := binary.LittleEndian.Uint16(b[2:]); v != versionDXA {
		return header{}, fmt.Errorf("%w: version %d", ErrUnsupported, v)
	}
	return header{
		headSize:        binary.LittleEndian.Uint32(b[4:]),
		dataStart:       binary.LittleEndian.Uint64(b[8:]),
		nameTableStart:  binary.LittleEndian.Uint64(b[16:]),
		fileTableStart:  binary.LittleEndian.Uint64(b[24:]),
		dirTableStart:   binary.LittleEndian.Uint64(b[32:]),
		flags:           binary.LittleEndian.Uint32(b[44:]),
		huffmanEncodeKB: b[48],
	}, nil
}

// Entry é um arquivo dentro do contêiner.
type Entry struct {
	// Path é o caminho relativo ao contêiner, com '/'.
	Path string
	// Size é o tamanho do arquivo descomprimido.
	Size int64

	key        []byte
	dataStart  uint64
	dataSize   uint64
	pressSize  uint64
	huffSize   uint64
	compressed bool
	huffman    bool
}

// Archive é um contêiner aberto. As entradas são lidas sob demanda: abrir um
// Data.wolf de vários GB só carrega as tabelas.
type Archive struct {
	f       *os.File
	size    int64
	head    header
	Entries []*Entry

	newCrypt bool
	noKey    bool
	pass1    *posKey // XOR por posição do arquivo inteiro
	pass2    *posKey // XOR por posição dos dados de cada entrada
	patches  []patch // trechos já decifrados pelo AES (início e tabelas)

	nameTable, fileTable, dirTable []byte
}

type patch struct {
	off  int64
	data []byte
}

// Open abre um contêiner. keyString é a chave textual do jogo; nil usa a padrão
// do DxLib e, nas versões novas de criptografia, a conhecida da versão.
func Open(path string, keyString []byte) (*Archive, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	a, err := openFile(f, keyString)
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	return a, nil
}

// Close libera o arquivo.
func (a *Archive) Close() error { return a.f.Close() }

func openFile(f *os.File, keyString []byte) (*Archive, error) {
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	a := &Archive{f: f, size: info.Size()}
	raw := make([]byte, headerSize)
	if _, err := io.ReadFull(f, raw); err != nil {
		return nil, ErrNotArchive
	}
	if a.head, err = parseHeader(raw); err != nil {
		return nil, err
	}

	cryptVersion := int(a.head.flags >> 16)
	a.newCrypt = (cryptVersion >= 331 && cryptVersion < 1000) || cryptVersion >= 1010

	var packedTables []byte
	var fileKey []byte
	if a.newCrypt {
		if packedTables, err = a.setupNewCrypt(raw, cryptVersion, keyString); err != nil {
			return nil, err
		}
	} else {
		a.noKey = a.head.flags&flagNoKey != 0
		if keyString == nil {
			keyString = defaultKey
		}
		if len(keyString) > maxKeyString {
			keyString = keyString[:maxKeyString]
		}
		fileKey = keyString
		if a.head.headSize == 0 {
			return nil, errCorrupt
		}
		if a.head.nameTableStart < headerSize || int64(a.head.nameTableStart) >= a.size {
			return nil, errCorrupt
		}
		packedTables = make([]byte, a.size-int64(a.head.nameTableStart))
		if _, err := f.ReadAt(packedTables, int64(a.head.nameTableStart)); err != nil {
			return nil, err
		}
		if !a.noKey {
			xorKey(packedTables, 0, makeKey(keyString))
		}
	}

	tables := packedTables
	if a.newCrypt || a.head.flags&flagNoHeadComp == 0 {
		huff := packedTables
		if a.newCrypt {
			huff = bytes.Clone(packedTables)
			a.pass2.xor(huff, 0)
		}
		lz, err := huffmanDecode(huff)
		if err != nil {
			return nil, err
		}
		if tables, err = lzDecode(lz); err != nil {
			return nil, err
		}
	} else if uint32(len(tables)) > a.head.headSize {
		tables = tables[:a.head.headSize]
	}

	// Nas versões novas os endereços das tabelas já são relativos à tabela de nomes.
	rel := func(addr uint64) uint64 {
		if addr >= a.head.nameTableStart {
			return addr - a.head.nameTableStart
		}
		return addr
	}
	fs, ds := rel(a.head.fileTableStart), rel(a.head.dirTableStart)
	if fs > ds || ds > uint64(len(tables)) {
		return nil, errCorrupt
	}
	a.nameTable, a.fileTable, a.dirTable = tables[:fs], tables[fs:ds], tables[ds:]

	if err := a.walk(0, "", 0, fileKey, nil); err != nil {
		return nil, err
	}
	return a, nil
}

// setupNewCrypt prepara a decifragem das versões novas e devolve a tabela de
// nomes/arquivos/diretórios já passada pelo AES (ainda comprimida).
func (a *Archive) setupNewCrypt(raw []byte, version int, keyString []byte) ([]byte, error) {
	pwd := bytes.Clone(raw[49:64])
	key := knownKeys[version]
	if key == nil && keyString != nil && !bytes.Equal(keyString, defaultKey) {
		key = keyString
	}
	if key == nil {
		return nil, fmt.Errorf("%w: encryption version %#x without a known key", ErrUnsupported, version)
	}
	key, _, _ = bytes.Cut(key, []byte{0})

	scrambled := bytes.Clone(raw)
	scrambleHeader(scrambled, pwd)
	var err error
	if a.head, err = parseHeader(scrambled); err != nil {
		return nil, err
	}
	nts := int64(a.head.nameTableStart)
	if nts < headerSize+0x400 || nts >= a.size {
		return nil, errCorrupt
	}

	a.pass1 = newPosKey(pwd, key, true)
	a.pass2 = newPosKey(pwd, key, false)
	aes := newAESCTR(pwd)

	start := a.readRaw(headerSize, 0x400)
	aes.xor(start)
	tail := a.readRaw(nts, int(a.size-nts))
	aes.xor(tail)
	a.patches = []patch{{headerSize, start}, {nts, tail}}
	return bytes.Clone(tail), nil
}

// readRaw lê e aplica o XOR por posição do arquivo inteiro. Os últimos 64 bytes
// ficam fora dele, como no original.
func (a *Archive) readRaw(off int64, n int) []byte {
	buf := make([]byte, n)
	m, _ := a.f.ReadAt(buf, off)
	buf = buf[:m]
	if a.pass1 != nil {
		lo, hi := max(off, headerSize), min(off+int64(m), a.size-headerSize)
		if hi > lo {
			a.pass1.xor(buf[lo-off:hi-off], int(lo))
		}
	}
	return buf
}

// readAt lê dados já decifrados da primeira camada (XOR por posição + AES).
func (a *Archive) readAt(off int64, n int) ([]byte, error) {
	if n < 0 || off < 0 || off+int64(n) > a.size {
		return nil, errCorrupt
	}
	buf := a.readRaw(off, n)
	if len(buf) != n {
		return nil, io.ErrUnexpectedEOF
	}
	for _, p := range a.patches {
		lo, hi := max(off, p.off), min(off+int64(n), p.off+int64(len(p.data)))
		if hi > lo {
			copy(buf[lo-off:hi-off], p.data[lo-p.off:hi-p.off])
		}
	}
	return buf, nil
}

type key7 [7]byte

func makeKey(source []byte) key7 {
	if len(source) < 4 {
		source = append(bytes.Clone(source), defaultKey...)
	}
	var even, odd []byte
	for i, b := range source {
		if i%2 == 0 {
			even = append(even, b)
		} else {
			odd = append(odd, b)
		}
	}
	c0, c1 := crc32.ChecksumIEEE(even), crc32.ChecksumIEEE(odd)
	return key7{byte(c0), byte(c0 >> 8), byte(c0 >> 16), byte(c0 >> 24), byte(c1), byte(c1 >> 8), byte(c1 >> 16)}
}

func xorKey(data []byte, position int, k key7) {
	j := position % 7
	for i := range data {
		data[i] ^= k[j]
		if j++; j == 7 {
			j = 0
		}
	}
}

func u64s(b []byte, off uint64, n int) ([]uint64, bool) {
	if off > uint64(len(b)) || uint64(len(b))-off < uint64(n)*8 {
		return nil, false
	}
	v := make([]uint64, n)
	for i := range v {
		v[i] = binary.LittleEndian.Uint64(b[off+uint64(i)*8:])
	}
	return v, true
}

// name devolve o nome original de uma entrada da tabela de nomes.
func (a *Archive) name(addr uint64) (string, error) {
	if addr+4 > uint64(len(a.nameTable)) {
		return "", errCorrupt
	}
	start := addr + uint64(binary.LittleEndian.Uint16(a.nameTable[addr:]))*4 + 4
	if start >= uint64(len(a.nameTable)) {
		return "", errCorrupt
	}
	raw, _, _ := bytes.Cut(a.nameTable[start:], []byte{0})
	s := string(raw)
	if !isUTF8(raw) {
		dec, err := japanese.ShiftJIS.NewDecoder().Bytes(raw)
		if err != nil {
			return "", err
		}
		s = string(dec)
	}
	if s == "" || s == "." || s == ".." || strings.ContainsAny(s, "/\\\x00") {
		return "", fmt.Errorf("%w: unsafe entry name %q", errCorrupt, s)
	}
	return s, nil
}

func isUTF8(b []byte) bool { return strings.ToValidUTF8(string(b), "�") == string(b) }

// upperName é o nome em maiúsculas (parity) usado para derivar a chave do arquivo.
func (a *Archive) upperName(addr uint64) []byte {
	if addr+4 > uint64(len(a.nameTable)) {
		return nil
	}
	s, _, _ := bytes.Cut(a.nameTable[addr+4:], []byte{0})
	return s
}

// walk percorre o diretório em dirAddr. chain acumula os nomes dos diretórios
// ancestrais para a chave de cada arquivo (chave + NOME + PAI + AVÔ...).
func (a *Archive) walk(dirAddr uint64, prefix string, depth int, keyString []byte, chain []byte) error {
	if depth > 64 {
		return errCorrupt
	}
	d, ok := u64s(a.dirTable, dirAddr, 4)
	if !ok {
		return errCorrupt
	}
	dirName, parent, count, headAddr := d[0], d[1], d[2], d[3]

	if dirName != none && parent != none {
		fh, ok := u64s(a.fileTable, dirName, 9)
		if !ok {
			return errCorrupt
		}
		nm, err := a.name(fh[0])
		if err != nil {
			return err
		}
		prefix += nm + "/"
		chain = append(bytes.Clone(a.upperName(fh[0])), chain...)
	}

	for i := uint64(0); i < count; i++ {
		fh, ok := u64s(a.fileTable, headAddr+i*72, 9)
		if !ok {
			return errCorrupt
		}
		if fh[1]&attrDirectory != 0 {
			if err := a.walk(fh[5], prefix, depth+1, keyString, chain); err != nil {
				return err
			}
			continue
		}
		nm, err := a.name(fh[0])
		if err != nil {
			return err
		}
		e := &Entry{
			Path: prefix + nm, Size: int64(fh[6]),
			dataStart: a.head.dataStart + fh[5], dataSize: fh[6], pressSize: fh[7], huffSize: fh[8],
			compressed: fh[7] != none, huffman: fh[8] != none,
		}
		if !a.noKey && !a.newCrypt {
			ks := append(bytes.Clone(keyString), a.upperName(fh[0])...)
			ks = append(ks, chain...)
			k := makeKey(ks)
			e.key = k[:]
		}
		a.Entries = append(a.Entries, e)
	}
	return nil
}

// conv decifra os dados de uma entrada, que começam na posição position do fluxo.
func (a *Archive) conv(e *Entry, data []byte, position uint64) {
	switch {
	case a.newCrypt:
		a.pass2.xor(data, int(position))
	case e.key != nil:
		xorKey(data, int(position), key7(e.key))
	}
}

// Read devolve o conteúdo descomprimido da entrada.
func (a *Archive) Read(e *Entry) ([]byte, error) {
	if e.dataSize == 0 {
		return []byte{}, nil
	}
	const limit = 1 << 32
	if e.dataSize > limit || (e.compressed && e.pressSize > limit) || (e.huffman && e.huffSize > limit) {
		return nil, errCorrupt
	}
	// Nas versões novas, o fluxo das entradas comprimidas começa em dataSize.
	pos0 := uint64(0)
	if a.newCrypt {
		pos0 = e.dataSize
	}
	off := int64(e.dataStart)
	read := func(n uint64, position uint64) ([]byte, error) {
		b, err := a.readAt(off, int(n))
		if err != nil {
			return nil, err
		}
		off += int64(n)
		a.conv(e, b, position)
		return b, nil
	}

	var packed []byte
	switch {
	case e.huffman:
		stored := e.dataSize
		if e.compressed {
			stored = e.pressSize
		}
		head, err := read(e.huffSize, pos0)
		if err != nil {
			return nil, err
		}
		decoded, err := huffmanDecode(head)
		if err != nil {
			return nil, err
		}
		kb := uint64(a.head.huffmanEncodeKB) * 1024
		if a.head.huffmanEncodeKB != 0xFF && stored > kb*2 {
			if uint64(len(decoded)) < kb*2 {
				return nil, errCorrupt
			}
			middle, err := read(stored-kb*2, e.dataSize+e.huffSize)
			if err != nil {
				return nil, err
			}
			packed = make([]byte, 0, stored)
			packed = append(packed, decoded[:kb]...)
			packed = append(packed, middle...)
			packed = append(packed, decoded[kb:2*kb]...)
		} else {
			packed = decoded
		}
	case e.compressed:
		var err error
		if packed, err = read(e.pressSize, pos0); err != nil {
			return nil, err
		}
	default:
		return read(e.dataSize, 0)
	}

	if !e.compressed {
		if uint64(len(packed)) < e.dataSize {
			return nil, errCorrupt
		}
		return packed[:e.dataSize], nil
	}
	out, err := lzDecode(packed)
	if err != nil {
		return nil, err
	}
	if uint64(len(out)) < e.dataSize {
		return nil, errCorrupt
	}
	return out[:e.dataSize], nil
}
