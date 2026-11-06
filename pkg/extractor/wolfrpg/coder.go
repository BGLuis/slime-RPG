package wolfrpg

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"strings"
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

// WriteString grava uma string com comprimento + 1 e terminador nulo final. Em
// arquivos Shift-JIS, caracteres sem representação viram o equivalente ASCII
// (acentos) ou "?"; o resto é gravado sem alteração, para que strings não
// traduzidas voltem byte a byte.
func (c *WolfFileCoder) WriteString(s string) {
	var encoded []byte
	if c.IsUTF8 {
		encoded = []byte(strings.ToValidUTF8(s, "?"))
	} else {
		encoded = encodeShiftJIS(s)
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

func encodeShiftJIS(s string) []byte {
	if out, _, err := transform.String(japanese.ShiftJIS.NewEncoder(), s); err == nil {
		return []byte(out)
	}
	var b bytes.Buffer
	for _, r := range s {
		if out, _, err := transform.String(japanese.ShiftJIS.NewEncoder(), string(r)); err == nil {
			b.WriteString(out)
			continue
		}
		if ascii := latinAccents.Replace(string(r)); ascii != string(r) {
			b.WriteString(ascii)
			continue
		}
		b.WriteByte('?')
	}
	return b.Bytes()
}

// ReadByteArray lê count bytes; com count < 0 o tamanho vem num prefixo u4.
func (c *WolfFileCoder) ReadByteArray(count int) ([]byte, error) {
	if count < 0 {
		n, err := c.ReadU4()
		if err != nil {
			return nil, err
		}
		count = int(n)
	}
	return c.ReadBytes(count)
}

// ReadIntArray lê count inteiros u4; com count < 0 o tamanho vem num prefixo u4.
func (c *WolfFileCoder) ReadIntArray(count int) ([]uint32, error) {
	if count < 0 {
		n, err := c.ReadU4()
		if err != nil {
			return nil, err
		}
		count = int(n)
	}
	if count > c.r.Len()/4 {
		return nil, ErrUnexpectedEOF
	}
	out := make([]uint32, count)
	for i := range out {
		v, err := c.ReadU4()
		if err != nil {
			return nil, err
		}
		out[i] = v
	}
	return out, nil
}

// ReadStringArray lê count strings; com count < 0 o tamanho vem num prefixo u4.
func (c *WolfFileCoder) ReadStringArray(count int) ([]string, error) {
	if count < 0 {
		n, err := c.ReadU4()
		if err != nil {
			return nil, err
		}
		count = int(n)
	}
	if count > c.r.Len()/4 {
		return nil, ErrUnexpectedEOF
	}
	out := make([]string, count)
	for i := range out {
		v, err := c.ReadString()
		if err != nil {
			return nil, err
		}
		out[i] = v
	}
	return out, nil
}

// TryVerify consome os bytes esperados se coincidirem; senão não avança.
func (c *WolfFileCoder) TryVerify(expected []byte) bool {
	pos := c.Pos()
	if c.Verify(expected) == nil {
		return true
	}
	_, _ = c.r.Seek(int64(pos), io.SeekStart)
	return false
}

// Pos devolve a posição de leitura.
func (c *WolfFileCoder) Pos() int {
	return int(c.r.Size()) - c.r.Len()
}

// Rest lê todo o restante da stream.
func (c *WolfFileCoder) Rest() []byte {
	b, _ := io.ReadAll(c.r)
	return b
}

// Len devolve o tamanho já escrito.
func (c *WolfFileCoder) Len() int {
	return c.buf.Len()
}

// WriteStringArray grava o tamanho e as strings.
func (c *WolfFileCoder) WriteStringArray(arr []string) {
	c.WriteU4(uint32(len(arr)))
	for _, s := range arr {
		c.WriteString(s)
	}
}

// WriteIntArray grava o tamanho e os inteiros.
func (c *WolfFileCoder) WriteIntArray(arr []uint32) {
	c.WriteU4(uint32(len(arr)))
	for _, v := range arr {
		c.WriteU4(v)
	}
}

var latinAccents = strings.NewReplacer(
	"á", "a", "à", "a", "ã", "a", "â", "a", "ä", "a",
	"Á", "A", "À", "A", "Ã", "A", "Â", "A", "Ä", "A",
	"é", "e", "è", "e", "ê", "e", "ë", "e",
	"É", "E", "È", "E", "Ê", "E", "Ë", "E",
	"í", "i", "ì", "i", "î", "i", "ï", "i",
	"Í", "I", "Ì", "I", "Î", "I", "Ï", "I",
	"ó", "o", "ò", "o", "õ", "o", "ô", "o", "ö", "o",
	"Ó", "O", "Ò", "O", "Õ", "O", "Ô", "O", "Ö", "O",
	"ú", "u", "ù", "u", "û", "u", "ü", "u",
	"Ú", "U", "Ù", "U", "Û", "U", "Ü", "U",
	"ç", "c", "Ç", "C", "ñ", "n", "Ñ", "N",
	"…", "...", "–", "-", "—", "-",
	"“", `"`, "”", `"`, "‘", "'", "’", "'", "«", `"`, "»", `"`,
	"º", "o", "ª", "a",
)

// mojibakeHints são caracteres típicos de UTF-8 lido como CP932 ("vocﾃｪ").
const mojibakeHints = "ﾃ窶縲ｱｲｳｴｵｶｷｸｹｺｻｼｽｾｿ"

// NormalizeWesternChars troca acentos e pontuação tipográfica por ASCII, mantendo
// japonês intacto, e recupera texto que já chegou como mojibake. É aplicada às
// traduções gravadas em arquivos Shift-JIS, cujas fontes legadas não exibem acentos.
func NormalizeWesternChars(s string) string {
	if s == "" {
		return s
	}
	if strings.ContainsAny(s, mojibakeHints) {
		if raw, _, err := transform.String(japanese.ShiftJIS.NewEncoder(), s); err == nil && utf8.ValidString(raw) {
			s = raw
		}
	}
	return latinAccents.Replace(s)
}
