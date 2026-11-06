package wolfrpg

import (
	"bytes"
	"encoding/binary"
	"errors"
)

var (
	ErrLZ4Corrupt = errors.New("lz4 data corrupt")
)

// DecompressLZ4 descompacta um bloco LZ4 utilizado pelo Wolf RPG Pro / 3.x.
func DecompressLZ4(src []byte, uncompressedSize int) ([]byte, error) {
	srcLen := len(src)
	srcPos := 0
	dst := make([]byte, 0, uncompressedSize)

	for srcPos < srcLen {
		token := src[srcPos]
		srcPos++

		litLen := int(token >> 4)
		if litLen == 15 {
			for srcPos < srcLen {
				s := int(src[srcPos])
				srcPos++
				litLen += s
				if s != 255 {
					break
				}
			}
		}

		if litLen > 0 {
			if srcPos+litLen > srcLen {
				return nil, ErrLZ4Corrupt
			}
			dst = append(dst, src[srcPos:srcPos+litLen]...)
			srcPos += litLen
		}

		if srcPos >= srcLen {
			break
		}

		if srcPos+2 > srcLen {
			return nil, ErrLZ4Corrupt
		}
		offset := int(binary.LittleEndian.Uint16(src[srcPos : srcPos+2]))
		srcPos += 2
		if offset == 0 || offset > len(dst) {
			break
		}

		matchLen := int(token&0x0F) + 4
		if matchLen == 19 {
			for srcPos < srcLen {
				s := int(src[srcPos])
				srcPos++
				matchLen += s
				if s != 255 {
					break
				}
			}
		}

		for i := 0; i < matchLen; i++ {
			dst = append(dst, dst[len(dst)-offset])
		}
	}

	if uncompressedSize > 0 && len(dst) > uncompressedSize {
		return dst[:uncompressedSize], nil
	}
	return dst, nil
}

// CompressLZ4 compacta um buffer em formato de bloco LZ4 para Wolf RPG Pro / 3.x.
func CompressLZ4(src []byte) []byte {
	srcLen := len(src)
	if srcLen == 0 {
		return nil
	}

	hashTable := make(map[uint32]int)
	pos := 0
	anchor := 0
	var dst bytes.Buffer

	hashFn := func(p int) uint32 {
		val := binary.LittleEndian.Uint32(src[p : p+4])
		return ((val * 2654435761) & 0xFFFFFFFF) >> 18
	}

	for pos <= srcLen-12 {
		h := hashFn(pos)
		ref, found := hashTable[h]
		hashTable[h] = pos

		if found && (pos-ref) < 65536 && bytes.Equal(src[ref:ref+4], src[pos:pos+4]) {
			matchLen := 4
			for pos+matchLen < srcLen-5 && src[ref+matchLen] == src[pos+matchLen] {
				matchLen++
			}

			litLen := pos - anchor
			offset := pos - ref

			tokenLit := litLen
			if tokenLit > 15 {
				tokenLit = 15
			}
			tokenMatch := matchLen - 4
			if tokenMatch > 15 {
				tokenMatch = 15
			}

			dst.WriteByte(byte((tokenLit << 4) | tokenMatch))

			if litLen >= 15 {
				rem := litLen - 15
				for rem >= 255 {
					dst.WriteByte(255)
					rem -= 255
				}
				dst.WriteByte(byte(rem))
			}

			if litLen > 0 {
				dst.Write(src[anchor : anchor+litLen])
			}

			offsetBuf := make([]byte, 2)
			binary.LittleEndian.PutUint16(offsetBuf, uint16(offset))
			dst.Write(offsetBuf)

			if matchLen-4 >= 15 {
				rem := (matchLen - 4) - 15
				for rem >= 255 {
					dst.WriteByte(255)
					rem -= 255
				}
				dst.WriteByte(byte(rem))
			}

			pos += matchLen
			anchor = pos
			continue
		}
		pos++
	}

	litLen := srcLen - anchor
	tokenLit := litLen
	if tokenLit > 15 {
		tokenLit = 15
	}
	dst.WriteByte(byte(tokenLit << 4))

	if litLen >= 15 {
		rem := litLen - 15
		for rem >= 255 {
			dst.WriteByte(255)
			rem -= 255
		}
		dst.WriteByte(byte(rem))
	}

	if litLen > 0 {
		dst.Write(src[anchor:srcLen])
	}

	return dst.Bytes()
}
