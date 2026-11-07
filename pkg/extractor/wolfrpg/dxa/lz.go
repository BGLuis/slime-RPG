package dxa

import (
	"encoding/binary"
	"errors"
)

const (
	lzMinMatch    = 4
	lzMaxMatch    = 0x1FFF + lzMinMatch
	lzMaxDistance = 1 << 24
	lzMaxChain    = 64
)

var errLZ = errors.New("dxa: lz stream is corrupt")

// lzDecode descomprime o LZ do DxLib: cabeçalho de 9 bytes (tamanho original,
// tamanho comprimido, byte de escape) seguido de literais e referências
// introduzidas pelo byte de escape.
func lzDecode(src []byte) ([]byte, error) {
	if len(src) < 9 {
		return nil, errLZ
	}
	destSize := int(binary.LittleEndian.Uint32(src[0:4]))
	packed := int(binary.LittleEndian.Uint32(src[4:8]))
	keycode := src[8]
	if packed < 9 || packed > len(src) || destSize > maxHuffmanSize {
		return nil, errLZ
	}
	in := src[9:packed]
	out := make([]byte, 0, destSize)

	for p := 0; p < len(in); {
		if in[p] != keycode {
			out = append(out, in[p])
			p++
			continue
		}
		if p+1 >= len(in) {
			return nil, errLZ
		}
		if in[p+1] == keycode {
			out = append(out, keycode)
			p += 2
			continue
		}
		code := int(in[p+1])
		if code > int(keycode) {
			code--
		}
		p += 2

		length := code >> 3
		if code&4 != 0 {
			if p >= len(in) {
				return nil, errLZ
			}
			length |= int(in[p]) << 5
			p++
		}
		length += lzMinMatch

		var index int
		switch code & 3 {
		case 0:
			if p+1 > len(in) {
				return nil, errLZ
			}
			index = int(in[p])
			p++
		case 1:
			if p+2 > len(in) {
				return nil, errLZ
			}
			index = int(binary.LittleEndian.Uint16(in[p:]))
			p += 2
		case 2:
			if p+3 > len(in) {
				return nil, errLZ
			}
			index = int(binary.LittleEndian.Uint16(in[p:])) | int(in[p+2])<<16
			p += 3
		default:
			return nil, errLZ
		}
		index++
		if index > len(out) {
			return nil, errLZ
		}
		// A cópia pode se sobrepor ao próprio resultado (repetição de padrão).
		for i := 0; i < length; i++ {
			out = append(out, out[len(out)-index])
		}
	}
	if len(out) < destSize {
		return nil, errLZ
	}
	return out[:destSize], nil
}

// lzEncode comprime src no formato lido por lzDecode.
func lzEncode(src []byte) []byte {
	var count [256]int
	for _, b := range src {
		count[b]++
	}
	keycode := 0
	for i := 1; i < 256; i++ {
		if count[i] < count[keycode] {
			keycode = i
		}
	}
	key := byte(keycode)

	out := make([]byte, 9, len(src)+len(src)/8+9)
	binary.LittleEndian.PutUint32(out[0:4], uint32(len(src)))
	out[8] = key

	const hashBits = 16
	head := make([]int32, 1<<hashBits)
	for i := range head {
		head[i] = -1
	}
	prev := make([]int32, len(src))
	hash := func(p int) int {
		v := uint32(src[p]) | uint32(src[p+1])<<8 | uint32(src[p+2])<<16 | uint32(src[p+3])<<24
		return int(v * 2654435761 >> (32 - hashBits))
	}
	insert := func(p int) {
		if p+lzMinMatch <= len(src) {
			h := hash(p)
			prev[p] = head[h]
			head[h] = int32(p)
		}
	}

	for sp := 0; sp < len(src); {
		if src[sp] == key {
			out = append(out, key, key)
			insert(sp)
			sp++
			continue
		}

		bestLen, bestDist := 0, 0
		if sp+lzMinMatch <= len(src) {
			cand := head[hash(sp)]
			for chain := 0; cand >= 0 && chain < lzMaxChain; chain++ {
				dist := sp - int(cand)
				if dist > lzMaxDistance {
					break
				}
				n := 0
				for sp+n < len(src) && n < lzMaxMatch && src[int(cand)+n] == src[sp+n] {
					n++
				}
				if n > bestLen {
					bestLen, bestDist = n, dist
				}
				cand = prev[cand]
			}
		}

		if bestLen < lzMinMatch {
			out = append(out, src[sp])
			insert(sp)
			sp++
			continue
		}

		index := bestDist - 1
		indexSize := 0
		switch {
		case index > 0xFFFF:
			indexSize = 2
		case index > 0xFF:
			indexSize = 1
		}
		length := bestLen - lzMinMatch
		var code int
		extra := -1
		if length >= 0x20 {
			code = (length&0x1F)<<3 | 4 | indexSize
			extra = length >> 5
		} else {
			code = length<<3 | indexSize
		}
		if code >= keycode {
			code++
		}
		out = append(out, key, byte(code))
		if extra >= 0 {
			out = append(out, byte(extra))
		}
		switch indexSize {
		case 0:
			out = append(out, byte(index))
		case 1:
			out = append(out, byte(index), byte(index>>8))
		default:
			out = append(out, byte(index), byte(index>>8), byte(index>>16))
		}
		for i := 0; i < bestLen; i++ {
			insert(sp + i)
		}
		sp += bestLen
	}
	binary.LittleEndian.PutUint32(out[4:8], uint32(len(out)))
	return out
}
