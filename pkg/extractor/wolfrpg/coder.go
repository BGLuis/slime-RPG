package wolfrpg

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"unicode/utf8"

	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/transform"
)

var (
	ErrUnexpectedEOF = errors.New("unexpected eof in wolf file coder")
	ErrHeaderFailed  = errors.New("header verification failed")
	ErrStringTooLong = errors.New("unlikely large string in wolf data")
)

// WolfFileCoder manipula streams de leitura e gravação para os formatos binários do WOLF RPG.
type WolfFileCoder struct {
	buf    *bytes.Buffer
	r      *bytes.Reader
	IsUTF8 bool
}

// NewReaderCoder cria um coder para leitura a partir de bytes.
func NewReaderCoder(data []byte, isUTF8 bool) *WolfFileCoder {
	return &WolfFileCoder{
		r:      bytes.NewReader(data),
		IsUTF8: isUTF8,
	}
}

// NewWriterCoder cria um coder para serialização em memória.
func NewWriterCoder(isUTF8 bool) *WolfFileCoder {
	return &WolfFileCoder{
		buf:    new(bytes.Buffer),
		IsUTF8: isUTF8,
	}
}

// Bytes retorna o buffer acumulado pelo writer.
func (c *WolfFileCoder) Bytes() []byte {
	if c.buf != nil {
		return c.buf.Bytes()
	}
	return nil
}

// ReadU1 lê 1 byte não-sinalizado.
func (c *WolfFileCoder) ReadU1() (byte, error) {
	if c.r == nil {
		return 0, io.ErrUnexpectedEOF
	}
	return c.r.ReadByte()
}

// ReadU2 lê 2 bytes em LittleEndian.
func (c *WolfFileCoder) ReadU2() (uint16, error) {
	b := make([]byte, 2)
	if _, err := io.ReadFull(c.r, b); err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint16(b), nil
}

// ReadU4 lê 4 bytes em LittleEndian.
func (c *WolfFileCoder) ReadU4() (uint32, error) {
	b := make([]byte, 4)
	if _, err := io.ReadFull(c.r, b); err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint32(b), nil
}

// ReadBytes lê n bytes da stream.
func (c *WolfFileCoder) ReadBytes(n int) ([]byte, error) {
	b := make([]byte, n)
	if _, err := io.ReadFull(c.r, b); err != nil {
		return nil, err
	}
	return b, nil
}

// ReadString lê uma string do Wolf RPG com prefixo de tamanho de 4 bytes e terminador nulo.
func (c *WolfFileCoder) ReadString() (string, error) {
	size, err := c.ReadU4()
	if err != nil {
		return "", err
	}
	if size == 0 {
		return "", nil
	}
	if size > 1_000_000 {
		return "", fmt.Errorf("%w: %d", ErrStringTooLong, size)
	}

	var raw []byte
	if size > 1 {
		raw, err = c.ReadBytes(int(size - 1))
		if err != nil {
			return "", err
		}
	}

	last, err := c.ReadU1()
	if err != nil {
		return "", err
	}
	if last != 0 {
		return "", fmt.Errorf("string without zero terminator: %x", last)
	}

	if c.IsUTF8 {
		if utf8.Valid(raw) {
			return string(raw), nil
		}
	}

	// Tenta decodificar como ShiftJIS / CP932
	decoded, err := decodeShiftJIS(raw)
	if err == nil {
		return decoded, nil
	}

	// Fallback para UTF-8 lossy
	return string(raw), nil
}

// Verify confere se os próximos bytes coincidem com o padrão esperado.
func (c *WolfFileCoder) Verify(expected []byte) error {
	actual, err := c.ReadBytes(len(expected))
	if err != nil {
		return err
	}
	if !bytes.Equal(actual, expected) {
		return fmt.Errorf("%w: expected %v, got %v", ErrHeaderFailed, expected, actual)
	}
	return nil
}

// EOF informa se o leitor atingiu o fim dos dados.
func (c *WolfFileCoder) EOF() bool {
	if c.r == nil {
		return true
	}
	return c.r.Len() == 0
}

// WriteU1 escreve 1 byte na stream.
func (c *WolfFileCoder) WriteU1(val byte) {
	c.buf.WriteByte(val)
}

// WriteU2 escreve uint16 em LittleEndian.
func (c *WolfFileCoder) WriteU2(val uint16) {
	b := make([]byte, 2)
	binary.LittleEndian.PutUint16(b, val)
	c.buf.Write(b)
}

// WriteU4 escreve uint32 em LittleEndian.
func (c *WolfFileCoder) WriteU4(val uint32) {
	b := make([]byte, 4)
	binary.LittleEndian.PutUint32(b, val)
	c.buf.Write(b)
}

// WriteBytes grava um slice de bytes.
func (c *WolfFileCoder) WriteBytes(b []byte) {
	c.buf.Write(b)
}

// WriteTerminator escreve um byte terminador.
func (c *WolfFileCoder) WriteTerminator(val byte) {
	c.buf.WriteByte(val)
}

// WriteString grava uma string com comprimento + 1 e terminador nulo final.
func (c *WolfFileCoder) WriteString(s string) {
	var encoded []byte
	if c.IsUTF8 {
		encoded = []byte(s)
	} else {
		var err error
		encoded, err = encodeShiftJIS(s)
		if err != nil {
			encoded = []byte(s)
		}
	}

	c.WriteU4(uint32(len(encoded) + 1))
	c.WriteBytes(encoded)
	c.WriteTerminator(0)
}

func decodeShiftJIS(b []byte) (string, error) {
	r := transform.NewReader(bytes.NewReader(b), japanese.ShiftJIS.NewDecoder())
	res, err := io.ReadAll(r)
	if err != nil {
		return "", err
	}
	return string(res), nil
}

func encodeShiftJIS(s string) ([]byte, error) {
	w := bytes.NewBuffer(nil)
	t := transform.NewWriter(w, japanese.ShiftJIS.NewEncoder())
	_, err := t.Write([]byte(s))
	if err != nil {
		return nil, err
	}
	if err := t.Close(); err != nil {
		return nil, err
	}
	return w.Bytes(), nil
}
