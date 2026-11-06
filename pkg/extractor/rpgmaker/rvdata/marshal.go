package rvdata

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strconv"
)

const (
	TypeNil        = '0'
	TypeTrue       = 'T'
	TypeFalse      = 'F'
	TypeFixnum     = 'i'
	TypeFloat      = 'f'
	TypeString     = '"'
	TypeSymbol     = ':'
	TypeSymlink    = ';'
	TypeArray      = '['
	TypeHash       = '{'
	TypeObject     = 'o'
	TypeUserDef    = 'u'
	TypeUsrMarshal = 'U'
	TypeIVAR       = 'I'
	TypeLink       = '@'
)

var (
	ErrInvalidMarshalMagic = errors.New("invalid ruby marshal magic version (expected 0x04, 0x08)")
	ErrUnsupportedToken    = errors.New("unsupported ruby marshal token")
	ErrInvalidLink         = errors.New("invalid object link index")
	ErrInvalidSymlink      = errors.New("invalid symlink index")
)

// RubySymbol representa um identificador Ruby :symbol.
type RubySymbol struct {
	Name string
}

func (s RubySymbol) String() string {
	return ":" + s.Name
}

// RubyString representa uma string Ruby com seus atributos IVAR (ex: encoding :E => true).
type RubyString struct {
	Value      string
	RawBytes   []byte
	Attributes map[string]any
}

// RubyObject representa uma instância de classe Ruby (ex: RPG::Map, RPG::EventCommand).
type RubyObject struct {
	ClassName  string
	Attributes map[string]any
}

// RubyUserDef representa objetos com serialização binária customizada (ex: Table do RGSS).
type RubyUserDef struct {
	ClassName string
	Data      []byte
}

// Reader decodifica fluxos no formato Ruby Marshal 4.8.
type Reader struct {
	r       *bytes.Reader
	objects []any
	symbols []string
}

// NewReader inicializa um reader validando o cabeçalho mágico \x04\x08.
func NewReader(data []byte) (*Reader, error) {
	if len(data) < 2 || data[0] != 0x04 || data[1] != 0x08 {
		return nil, ErrInvalidMarshalMagic
	}
	return &Reader{
		r:       bytes.NewReader(data[2:]),
		objects: make([]any, 0),
		symbols: make([]string, 0),
	}, nil
}

// Read lê o próximo valor da stream.
func (r *Reader) Read() (any, error) {
	b, err := r.r.ReadByte()
	if err != nil {
		return nil, err
	}

	switch b {
	case TypeNil:
		return nil, nil
	case TypeTrue:
		return true, nil
	case TypeFalse:
		return false, nil
	case TypeFixnum:
		return r.readFixnum()
	case TypeFloat:
		raw, err := r.readBlob()
		if err != nil {
			return nil, err
		}
		f, err := strconv.ParseFloat(string(raw), 64)
		if err != nil {
			return 0.0, nil
		}
		r.objects = append(r.objects, f)
		return f, nil
	case TypeSymbol:
		sym, err := r.readSymbol()
		if err != nil {
			return nil, err
		}
		return sym, nil
	case TypeSymlink:
		idx, err := r.readFixnum()
		if err != nil {
			return nil, err
		}
		if idx < 0 || int(idx) >= len(r.symbols) {
			return nil, fmt.Errorf("%w: %d", ErrInvalidSymlink, idx)
		}
		return RubySymbol{Name: r.symbols[idx]}, nil
	case TypeString:
		raw, err := r.readBlob()
		if err != nil {
			return nil, err
		}
		rs := &RubyString{Value: string(raw), RawBytes: raw}
		r.objects = append(r.objects, rs)
		return rs, nil
	case TypeIVAR:
		// Wrapper para objetos com atributos de instância (especialmente strings com encoding :E)
		val, err := r.Read()
		if err != nil {
			return nil, err
		}
		attrCount, err := r.readFixnum()
		if err != nil {
			return nil, err
		}
		attrs := make(map[string]any, attrCount)
		for i := int64(0); i < attrCount; i++ {
			k, err := r.Read()
			if err != nil {
				return nil, err
			}
			v, err := r.Read()
			if err != nil {
				return nil, err
			}
			var keyName string
			if sym, ok := k.(RubySymbol); ok {
				keyName = sym.Name
			} else {
				keyName = fmt.Sprint(k)
			}
			attrs[keyName] = v
		}

		if rs, ok := val.(*RubyString); ok {
			rs.Attributes = attrs
			if len(attrs) == 1 && attrs["E"] == true {
				if len(r.objects) > 0 {
					r.objects[len(r.objects)-1] = rs.Value
				}
				return rs.Value, nil
			}
			return rs, nil
		}
		if ro, ok := val.(*RubyObject); ok {
			if ro.Attributes == nil {
				ro.Attributes = make(map[string]any)
			}
			for k, v := range attrs {
				ro.Attributes[k] = v
			}
			return ro, nil
		}
		return val, nil
	case TypeArray:
		length, err := r.readFixnum()
		if err != nil {
			return nil, err
		}
		arr := make([]any, length)
		objIdx := len(r.objects)
		r.objects = append(r.objects, arr) // Reserva espaço para referências circulares
		for i := int64(0); i < length; i++ {
			elem, err := r.Read()
			if err != nil {
				return nil, err
			}
			arr[i] = elem
		}
		r.objects[objIdx] = arr
		return arr, nil
	case TypeHash:
		pairCount, err := r.readFixnum()
		if err != nil {
			return nil, err
		}
		h := make(map[any]any, pairCount)
		objIdx := len(r.objects)
		r.objects = append(r.objects, h)
		for i := int64(0); i < pairCount; i++ {
			k, err := r.Read()
			if err != nil {
				return nil, err
			}
			v, err := r.Read()
			if err != nil {
				return nil, err
			}
			h[k] = v
		}
		r.objects[objIdx] = h
		return h, nil
	case TypeObject:
		classVal, err := r.Read()
		if err != nil {
			return nil, err
		}
		className := ""
		if sym, ok := classVal.(RubySymbol); ok {
			className = sym.Name
		} else {
			className = fmt.Sprint(classVal)
		}

		ro := &RubyObject{
			ClassName:  className,
			Attributes: make(map[string]any),
		}
		objIdx := len(r.objects)
		r.objects = append(r.objects, ro)

		attrCount, err := r.readFixnum()
		if err != nil {
			return nil, err
		}
		for i := int64(0); i < attrCount; i++ {
			k, err := r.Read()
			if err != nil {
				return nil, err
			}
			v, err := r.Read()
			if err != nil {
				return nil, err
			}
			var keyName string
			if sym, ok := k.(RubySymbol); ok {
				keyName = sym.Name
			} else {
				keyName = fmt.Sprint(k)
			}
			ro.Attributes[keyName] = v
		}
		r.objects[objIdx] = ro
		return ro, nil
	case TypeUserDef:
		classVal, err := r.Read()
		if err != nil {
			return nil, err
		}
		className := ""
		if sym, ok := classVal.(RubySymbol); ok {
			className = sym.Name
		} else {
			className = fmt.Sprint(classVal)
		}
		blob, err := r.readBlob()
		if err != nil {
			return nil, err
		}
		ud := &RubyUserDef{
			ClassName: className,
			Data:      blob,
		}
		r.objects = append(r.objects, ud)
		return ud, nil
	case TypeLink:
		linkIdx, err := r.readFixnum()
		if err != nil {
			return nil, err
		}
		if linkIdx < 0 || int(linkIdx) >= len(r.objects) {
			return nil, fmt.Errorf("%w: %d (pool size %d)", ErrInvalidLink, linkIdx, len(r.objects))
		}
		return r.objects[linkIdx], nil
	default:
		return nil, fmt.Errorf("%w: %q (0x%02x)", ErrUnsupportedToken, b, b)
	}
}

func (r *Reader) readBlob() ([]byte, error) {
	lenFixnum, err := r.readFixnum()
	if err != nil {
		return nil, err
	}
	buf := make([]byte, lenFixnum)
	if _, err := io.ReadFull(r.r, buf); err != nil {
		return nil, err
	}
	return buf, nil
}

func (r *Reader) readSymbol() (RubySymbol, error) {
	raw, err := r.readBlob()
	if err != nil {
		return RubySymbol{}, err
	}
	sym := string(raw)
	r.symbols = append(r.symbols, sym)
	return RubySymbol{Name: sym}, nil
}

func (r *Reader) readFixnum() (int64, error) {
	b, err := r.r.ReadByte()
	if err != nil {
		return 0, err
	}

	sb := int8(b)
	if sb == 0 {
		return 0, nil
	}
	if sb > 4 {
		return int64(sb) - 5, nil
	}
	if sb < -4 {
		return int64(sb) + 5, nil
	}

	lenBytes := int(sb)
	if lenBytes > 0 {
		var res int64
		for i := 0; i < lenBytes; i++ {
			by, err := r.r.ReadByte()
			if err != nil {
				return 0, err
			}
			res |= int64(by) << (i * 8)
		}
		return res, nil
	}

	// lenBytes < 0
	count := -lenBytes
	var res int64 = -1
	for i := 0; i < count; i++ {
		by, err := r.r.ReadByte()
		if err != nil {
			return 0, err
		}
		mask := ^(int64(0xFF) << (i * 8))
		res = (res & mask) | (int64(by) << (i * 8))
	}
	return res, nil
}

// Writer serializa valores em formato Ruby Marshal 4.8.
type Writer struct {
	buf       bytes.Buffer
	objects   []any
	ptrMap    map[uintptr]int
	symbols   []string
	symbolMap map[string]int
}

// NewWriter inicializa um escritor com cabeçalho mágico \x04\x08.
func NewWriter() *Writer {
	w := &Writer{
		objects:   make([]any, 0),
		ptrMap:    make(map[uintptr]int),
		symbols:   make([]string, 0),
		symbolMap: make(map[string]int),
	}
	w.buf.Write([]byte{0x04, 0x08})
	return w
}

// Bytes retorna o buffer completo serializado.
func (w *Writer) Bytes() []byte {
	return w.buf.Bytes()
}

// Write serializa qualquer valor compatível com Ruby.
func (w *Writer) Write(v any) error {
	if v == nil {
		w.buf.WriteByte(TypeNil)
		return nil
	}

	switch val := v.(type) {
	case bool:
		if val {
			w.buf.WriteByte(TypeTrue)
		} else {
			w.buf.WriteByte(TypeFalse)
		}
		return nil
	case int:
		w.buf.WriteByte(TypeFixnum)
		w.writeFixnum(int64(val))
		return nil
	case int32:
		w.buf.WriteByte(TypeFixnum)
		w.writeFixnum(int64(val))
		return nil
	case int64:
		w.buf.WriteByte(TypeFixnum)
		w.writeFixnum(val)
		return nil
	case float64:
		// Float ocupa um índice na tabela de objetos do Ruby (o Reader também o
		// conta); sem isso, todo TYPE_LINK gravado depois aponta para o objeto errado.
		w.objects = append(w.objects, val)
		w.buf.WriteByte(TypeFloat)
		s := strconv.FormatFloat(val, 'g', -1, 64)
		w.writeBlob([]byte(s))
		return nil
	case RubySymbol:
		return w.writeSymbol(val.Name)
	case string:
		// String Go nativa é gravada como TYPE_IVAR + TYPE_STRING com :E => true (UTF-8).
		// Strings iguais continuam objetos distintos: o Reader converte toda string
		// UTF-8 em string Go, e ligá-las por valor faria o Ruby compartilhar (e
		// mutar em conjunto) strings que eram independentes no arquivo original.
		w.objects = append(w.objects, val)

		w.buf.WriteByte(TypeIVAR)
		w.buf.WriteByte(TypeString)
		w.writeBlob([]byte(val))
		w.writeFixnum(1) // 1 atributo
		if err := w.writeSymbol("E"); err != nil {
			return err
		}
		w.buf.WriteByte(TypeTrue)
		return nil
	case *RubyString:
		ptr := reflect.ValueOf(val).Pointer()
		if idx, ok := w.ptrMap[ptr]; ok {
			w.buf.WriteByte(TypeLink)
			w.writeFixnum(int64(idx))
			return nil
		}
		w.ptrMap[ptr] = len(w.objects)
		w.objects = append(w.objects, val)

		payload := val.RawBytes
		if len(payload) == 0 && len(val.Value) > 0 {
			payload = []byte(val.Value)
		}

		if len(val.Attributes) > 0 {
			w.buf.WriteByte(TypeIVAR)
			w.buf.WriteByte(TypeString)
			w.writeBlob(payload)
			w.writeFixnum(int64(len(val.Attributes)))
			for k, attrV := range val.Attributes {
				if err := w.writeSymbol(k); err != nil {
					return err
				}
				if err := w.Write(attrV); err != nil {
					return err
				}
			}
		} else {
			w.buf.WriteByte(TypeString)
			w.writeBlob(payload)
		}
		return nil
	case []any:
		// Slices vazios compartilham o mesmo endereço em Go (zerobase): só um slice
		// não vazio identifica um Array Ruby compartilhado.
		ptr := reflect.ValueOf(val).Pointer()
		if len(val) > 0 {
			if idx, ok := w.ptrMap[ptr]; ok {
				w.buf.WriteByte(TypeLink)
				w.writeFixnum(int64(idx))
				return nil
			}
			w.ptrMap[ptr] = len(w.objects)
		}
		w.objects = append(w.objects, val)

		w.buf.WriteByte(TypeArray)
		w.writeFixnum(int64(len(val)))
		for _, elem := range val {
			if err := w.Write(elem); err != nil {
				return err
			}
		}
		return nil
	case map[any]any:
		ptr := reflect.ValueOf(val).Pointer()
		if ptr != 0 {
			if idx, ok := w.ptrMap[ptr]; ok {
				w.buf.WriteByte(TypeLink)
				w.writeFixnum(int64(idx))
				return nil
			}
			w.ptrMap[ptr] = len(w.objects)
		}
		w.objects = append(w.objects, val)

		w.buf.WriteByte(TypeHash)
		w.writeFixnum(int64(len(val)))
		for k, itemV := range val {
			if err := w.Write(k); err != nil {
				return err
			}
			if err := w.Write(itemV); err != nil {
				return err
			}
		}
		return nil
	case *RubyObject:
		ptr := reflect.ValueOf(val).Pointer()
		if idx, ok := w.ptrMap[ptr]; ok {
			w.buf.WriteByte(TypeLink)
			w.writeFixnum(int64(idx))
			return nil
		}
		w.ptrMap[ptr] = len(w.objects)
		w.objects = append(w.objects, val)

		w.buf.WriteByte(TypeObject)
		if err := w.writeSymbol(val.ClassName); err != nil {
			return err
		}
		w.writeFixnum(int64(len(val.Attributes)))
		for k, attrV := range val.Attributes {
			if err := w.writeSymbol(k); err != nil {
				return err
			}
			if err := w.Write(attrV); err != nil {
				return err
			}
		}
		return nil
	case *RubyUserDef:
		ptr := reflect.ValueOf(val).Pointer()
		if idx, ok := w.ptrMap[ptr]; ok {
			w.buf.WriteByte(TypeLink)
			w.writeFixnum(int64(idx))
			return nil
		}
		w.ptrMap[ptr] = len(w.objects)
		w.objects = append(w.objects, val)

		w.buf.WriteByte(TypeUserDef)
		if err := w.writeSymbol(val.ClassName); err != nil {
			return err
		}
		w.writeBlob(val.Data)
		return nil
	default:
		return fmt.Errorf("unsupported type to marshal: %T", v)
	}
}

func (w *Writer) writeSymbol(name string) error {
	if idx, ok := w.symbolMap[name]; ok {
		w.buf.WriteByte(TypeSymlink)
		w.writeFixnum(int64(idx))
		return nil
	}

	w.symbolMap[name] = len(w.symbols)
	w.symbols = append(w.symbols, name)

	w.buf.WriteByte(TypeSymbol)
	w.writeBlob([]byte(name))
	return nil
}

func (w *Writer) writeBlob(b []byte) {
	w.writeFixnum(int64(len(b)))
	w.buf.Write(b)
}

func (w *Writer) writeFixnum(val int64) {
	if val == 0 {
		w.buf.WriteByte(0)
		return
	}
	if val > 0 && val < 123 {
		w.buf.WriteByte(byte(val + 5))
		return
	}
	if val < 0 && val > -124 {
		w.buf.WriteByte(byte(int8(val - 5)))
		return
	}

	var bytesBuf [4]byte
	binary.LittleEndian.PutUint32(bytesBuf[:], uint32(val))

	if val > 0 {
		lenBytes := 4
		for lenBytes > 1 && bytesBuf[lenBytes-1] == 0 {
			lenBytes--
		}
		w.buf.WriteByte(byte(lenBytes))
		w.buf.Write(bytesBuf[:lenBytes])
	} else {
		lenBytes := 4
		for lenBytes > 1 && bytesBuf[lenBytes-1] == 0xFF {
			lenBytes--
		}
		w.buf.WriteByte(byte(-lenBytes))
		w.buf.Write(bytesBuf[:lenBytes])
	}
}
