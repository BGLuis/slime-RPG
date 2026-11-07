package dxa

import (
	"errors"
	"fmt"
)

var errHuffman = errors.New("dxa: huffman stream is corrupt")

// huffTree é a árvore de Huffman do DxLib: 256 folhas (0-255) seguidas de 255
// nós internos; a raiz é o último nó criado. A construção precisa seguir a ordem
// do original (varredura por índice, desempate pelo primeiro), pois o decodificador
// reconstrói a árvore só a partir dos pesos gravados.
type huffTree struct {
	parent [511]int
	child  [511][2]int
	index  [511]int
	weight [511]int
}

const (
	huffRoot       = 510
	maxHuffmanSize = 1 << 31
)

func newHuffTree(weights *[256]int) *huffTree {
	t := &huffTree{}
	for i := range t.parent {
		t.parent[i] = -1
		t.child[i] = [2]int{-1, -1}
	}
	copy(t.weight[:256], weights[:])

	nodeNum := 256
	for dataNum := 256; dataNum > 1; dataNum-- {
		min1, min2 := -1, -1
		for i, idx := 0, 0; i < dataNum; idx++ {
			if t.parent[idx] != -1 {
				continue
			}
			i++
			if min1 == -1 || t.weight[min1] > t.weight[idx] {
				min2 = min1
				min1 = idx
			} else if min2 == -1 || t.weight[min2] > t.weight[idx] {
				min2 = idx
			}
		}
		t.weight[nodeNum] = t.weight[min1] + t.weight[min2]
		t.child[nodeNum] = [2]int{min1, min2}
		t.index[min1], t.index[min2] = 0, 1
		t.parent[min1], t.parent[min2] = nodeNum, nodeNum
		nodeNum++
	}
	return t
}

// code devolve os bits do caminho da raiz até a folha.
func (t *huffTree) code(leaf int) []byte {
	var bits []byte
	for n := leaf; t.parent[n] != -1; n = t.parent[n] {
		bits = append(bits, byte(t.index[n]))
	}
	for i, j := 0, len(bits)-1; i < j; i, j = i+1, j-1 {
		bits[i], bits[j] = bits[j], bits[i]
	}
	return bits
}

type bitReader struct {
	buf []byte
	pos int // em bits
	err error
}

// msb lê n bits, do mais para o menos significativo. Após o fim dos dados, err
// fica definido e o resultado é 0.
func (r *bitReader) msb(n int) int {
	v := 0
	for i := 0; i < n; i++ {
		byteIdx := r.pos >> 3
		if byteIdx >= len(r.buf) {
			r.err = errHuffman
			return 0
		}
		v = v<<1 | int(r.buf[byteIdx]>>(7-uint(r.pos&7))&1)
		r.pos++
	}
	return v
}

type bitWriter struct {
	buf  []byte
	bits int
}

func (w *bitWriter) msb(n int, v uint64) {
	for i := n - 1; i >= 0; i-- {
		w.put(byte(v>>uint(i)) & 1)
	}
}

func (w *bitWriter) put(bit byte) {
	if w.bits&7 == 0 {
		w.buf = append(w.buf, 0)
	}
	w.buf[len(w.buf)-1] |= bit << (7 - uint(w.bits&7))
	w.bits++
}

// putLSB grava um bit no fluxo de dados, que preenche cada byte do bit menos
// significativo para o mais significativo.
func (w *bitWriter) putLSB(bit byte) {
	if w.bits&7 == 0 {
		w.buf = append(w.buf, 0)
	}
	w.buf[len(w.buf)-1] |= bit << uint(w.bits&7)
	w.bits++
}

func bitLen(v int) int {
	for i := 1; i < 64; i++ {
		if v < 1<<i {
			return i
		}
	}
	return 63
}

// huffmanDecodedSize lê só o tamanho original gravado no cabeçalho.
func huffmanDecodedSize(src []byte) (int, error) {
	r := &bitReader{buf: src}
	n := r.msb(r.msb(6) + 1)
	return n, r.err
}

// huffmanDecode decodifica um fluxo do DxLib (cabeçalho com pesos + dados com
// bits do menos para o mais significativo).
func huffmanDecode(src []byte) ([]byte, error) {
	r := &bitReader{buf: src}
	size := r.msb(r.msb(6) + 1)
	_ = r.msb(r.msb(6) + 1) // tamanho comprimido, redundante

	var weights [256]int
	for i := range weights {
		bits := (r.msb(3) + 1) * 2
		minus := r.msb(1)
		v := r.msb(bits)
		switch {
		case i == 0:
			weights[i] = v
		case minus == 1:
			weights[i] = (weights[i-1] - v) & 0xFFFF
		default:
			weights[i] = (weights[i-1] + v) & 0xFFFF
		}
	}
	if r.err != nil {
		return nil, r.err
	}
	if size > maxHuffmanSize {
		return nil, fmt.Errorf("%w: size %d", errHuffman, size)
	}

	tree := newHuffTree(&weights)
	data := src[(r.pos+7)>>3:]
	out := make([]byte, size)
	bit := 0
	for i := range out {
		n := huffRoot
		for n > 255 {
			byteIdx := bit >> 3
			if byteIdx >= len(data) {
				return nil, errHuffman
			}
			n = tree.child[n][data[byteIdx]>>(uint(bit&7))&1]
			bit++
		}
		out[i] = byte(n)
	}
	return out, nil
}

// huffmanEncode comprime src (não vazio) no formato do DxLib.
func huffmanEncode(src []byte) []byte {
	var weights [256]int
	for _, b := range src {
		weights[b]++
	}
	for i := range weights {
		weights[i] = weights[i] * 0xFFFF / len(src)
	}
	tree := newHuffTree(&weights)

	var codes [256][]byte
	for i := range codes {
		codes[i] = tree.code(i)
	}
	data := &bitWriter{}
	for _, b := range src {
		for _, bit := range codes[b] {
			data.putLSB(bit)
		}
	}

	head := &bitWriter{}
	n := bitLen(len(src))
	if n > 0 {
		n--
	}
	head.msb(6, uint64(n))
	head.msb(n+1, uint64(len(src)))
	n = bitLen(len(data.buf))
	head.msb(6, uint64(n))
	head.msb(n+1, uint64(len(data.buf)))
	for i := 0; i < 256; i++ {
		diff := weights[i]
		if i > 0 {
			diff -= weights[i-1]
		}
		minus := byte(0)
		if diff < 0 {
			diff, minus = -diff, 1
		}
		bits := (bitLen(diff) + 1) / 2
		if bits > 0 {
			bits--
		}
		head.msb(3, uint64(bits))
		head.put(minus)
		head.msb((bits+1)*2, uint64(diff))
	}
	return append(head.buf, data.buf...)
}
