package dxa

import (
	"encoding/binary"
	"io"
	"path"
	"sort"
	"strings"
)

// File é um arquivo a gravar. Read é chamado uma única vez, só na hora de
// gravar, para não manter o conteúdo de todos os arquivos em memória.
type File struct {
	Path string // relativo ao contêiner, com '/'
	Read func() ([]byte, error)
}

type wdir struct {
	name     string
	parent   int
	subdirs  []int
	files    []File
	tableOff uint64 // posição na tabela de diretórios
	headOff  uint64 // posição do FILEHEAD deste diretório na tabela de arquivos
}

// Write grava um contêiner sem chave, com tabelas e arquivos comprimidos (LZ +
// Huffman quando reduzem o tamanho). huffmanEncodeKB fica em 0xFF: o Huffman
// cobre o fluxo inteiro, que é o que Read espera nesse modo.
func Write(w io.WriteSeeker, files []File) error {
	dirs := []*wdir{{parent: -1}}
	index := map[string]int{"": 0}
	sorted := append([]File(nil), files...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Path < sorted[j].Path })
	for _, f := range sorted {
		dir, base := path.Split(strings.Trim(f.Path, "/"))
		cur, built := 0, ""
		if dir != "" {
			for _, part := range strings.Split(strings.TrimSuffix(dir, "/"), "/") {
				built += part + "/"
				next, ok := index[built]
				if !ok {
					next = len(dirs)
					dirs = append(dirs, &wdir{name: part, parent: cur})
					dirs[cur].subdirs = append(dirs[cur].subdirs, next)
					index[built] = next
				}
				cur = next
			}
		}
		dirs[cur].files = append(dirs[cur].files, File{Path: base, Read: f.Read})
	}

	if _, err := w.Seek(headerSize, io.SeekStart); err != nil {
		return err
	}

	var names, fileTable, dirTable []byte
	nameAddr := map[string]uint64{}
	addName := func(n string) uint64 {
		if a, ok := names2(nameAddr, n); ok {
			return a
		}
		a := uint64(len(names))
		names = binary.LittleEndian.AppendUint16(names, 0)
		names = binary.LittleEndian.AppendUint16(names, uint16(len(n)))
		names = append(append(names, n...), 0)
		nameAddr[n] = a
		return a
	}
	for _, d := range dirs {
		d.tableOff = uint64(len(dirTable))
		dirTable = append(dirTable, make([]byte, 32)...)
	}

	head := func(vals ...uint64) {
		for _, v := range vals {
			fileTable = binary.LittleEndian.AppendUint64(fileTable, v)
		}
	}
	var dataPos uint64
	for _, d := range dirs {
		headAddr := uint64(len(fileTable))
		self := dirTable[d.tableOff:]
		parent := none
		if d.parent >= 0 {
			parent = dirs[d.parent].tableOff
		}
		dirAddr := none
		if d.parent >= 0 {
			dirAddr = d.headOff
		}
		binary.LittleEndian.PutUint64(self[0:], dirAddr)
		binary.LittleEndian.PutUint64(self[8:], parent)
		binary.LittleEndian.PutUint64(self[16:], uint64(len(d.subdirs)+len(d.files)))
		binary.LittleEndian.PutUint64(self[24:], headAddr)

		for _, s := range d.subdirs {
			sub := dirs[s]
			sub.headOff = uint64(len(fileTable))
			head(addName(sub.name), attrDirectory, 0, 0, 0, sub.tableOff, none, none, none)
		}
		for _, f := range d.files {
			data, err := f.Read()
			if err != nil {
				return err
			}
			payload, press, huff := compressPayload(data)
			if _, err := w.Write(payload); err != nil {
				return err
			}
			head(addName(f.Path), 0, 0, 0, 0, dataPos, uint64(len(data)), press, huff)
			dataPos += uint64(len(payload))
		}
	}

	tables := append(append(append([]byte(nil), names...), fileTable...), dirTable...)
	lz := lzEncode(tables)
	packed := huffmanEncode(lz)

	h := make([]byte, headerSize)
	binary.LittleEndian.PutUint16(h[0:], 0x5844)
	binary.LittleEndian.PutUint16(h[2:], versionDXA)
	binary.LittleEndian.PutUint32(h[4:], uint32(len(tables)))
	nts := uint64(headerSize) + dataPos
	binary.LittleEndian.PutUint64(h[8:], headerSize)
	binary.LittleEndian.PutUint64(h[16:], nts)
	binary.LittleEndian.PutUint64(h[24:], nts+uint64(len(names)))
	binary.LittleEndian.PutUint64(h[32:], nts+uint64(len(names)+len(fileTable)))
	binary.LittleEndian.PutUint32(h[44:], flagNoKey)
	h[48] = 0xFF

	if _, err := w.Write(packed); err != nil {
		return err
	}
	if _, err := w.Seek(0, io.SeekStart); err != nil {
		return err
	}
	_, err := w.Write(h)
	return err
}

func names2(m map[string]uint64, n string) (uint64, bool) { a, ok := m[n]; return a, ok }

// compressPayload escolhe a menor representação: bruto, LZ ou LZ + Huffman.
func compressPayload(data []byte) (payload []byte, press, huff uint64) {
	if len(data) <= lzMinMatch {
		return data, none, none
	}
	lz := lzEncode(data)
	if len(lz) >= len(data) {
		return data, none, none
	}
	if h := huffmanEncode(lz); len(h) < len(lz) {
		return h, uint64(len(lz)), uint64(len(h))
	}
	return lz, uint64(len(lz)), none
}
