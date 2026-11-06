// Package jsonedit lê e reescreve strings de um documento JSON endereçadas por
// JSON Pointer (RFC 6901) sem re-serializar o documento: ordem das chaves,
// indentação, formato dos números e BOM permanecem byte a byte.
package jsonedit

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
)

var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// StripBOM remove o BOM UTF-8 inicial, que encoding/json rejeita.
func StripBOM(data []byte) []byte {
	return bytes.TrimPrefix(data, utf8BOM)
}

// EscapePointer aplica o escape de um token de JSON Pointer: '~' -> '~0', '/' -> '~1'.
func EscapePointer(token string) string {
	return strings.ReplaceAll(strings.ReplaceAll(token, "~", "~0"), "/", "~1")
}

// StringValue é uma string de valor (não chave) encontrada no documento.
type StringValue struct {
	Pointer string // JSON Pointer do valor
	Key     string // nome do membro de objeto mais próximo (herdado através de arrays)
	Value   string
	start   int // offset da aspa inicial no documento sem BOM
	end     int // offset logo após a aspa final
}

type frame struct {
	pointer   string
	key       string
	isObject  bool
	expectKey bool
	index     int
	member    string
}

// Strings devolve todas as strings de valor do documento na ordem em que aparecem.
func Strings(data []byte) ([]StringValue, error) {
	doc := StripBOM(data)
	dec := json.NewDecoder(bytes.NewReader(doc))
	dec.UseNumber()

	var out []StringValue
	var stack []*frame

	// childPointer devolve o ponteiro e a chave herdada do próximo valor no
	// container do topo, e avança o índice quando o container é um array.
	childPointer := func() (string, string) {
		if len(stack) == 0 {
			return "", ""
		}
		top := stack[len(stack)-1]
		if top.isObject {
			top.expectKey = true
			return top.pointer + "/" + EscapePointer(top.member), top.member
		}
		p := top.pointer + "/" + strconv.Itoa(top.index)
		top.index++
		return p, top.key
	}

	for {
		before := dec.InputOffset()
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		after := int(dec.InputOffset())

		if len(stack) > 0 {
			top := stack[len(stack)-1]
			if top.isObject && top.expectKey {
				if d, ok := tok.(json.Delim); ok && d == '}' {
					stack = stack[:len(stack)-1]
					continue
				}
				key, ok := tok.(string)
				if !ok {
					return nil, fmt.Errorf("jsonedit: expected object key at offset %d", before)
				}
				top.member = key
				top.expectKey = false
				continue
			}
		}

		switch v := tok.(type) {
		case json.Delim:
			switch v {
			case '{', '[':
				ptr, key := childPointer()
				stack = append(stack, &frame{pointer: ptr, key: key, isObject: v == '{', expectKey: v == '{'})
			case ']', '}':
				stack = stack[:len(stack)-1]
			}
		case string:
			ptr, key := childPointer()
			start := bytes.IndexByte(doc[before:after], '"')
			if start < 0 {
				return nil, fmt.Errorf("jsonedit: string literal not found at offset %d", before)
			}
			out = append(out, StringValue{Pointer: ptr, Key: key, Value: v, start: int(before) + start, end: after})
		default:
			childPointer()
		}
	}
	if len(stack) > 0 {
		return nil, fmt.Errorf("jsonedit: %w", io.ErrUnexpectedEOF)
	}
	return out, nil
}

// ReplaceStrings troca as strings de valor nos ponteiros indicados e preserva todos
// os outros bytes do documento. Ponteiros inexistentes ou que não apontam para uma
// string são ignorados.
func ReplaceStrings(data []byte, replacements map[string]string) ([]byte, error) {
	if len(replacements) == 0 {
		return data, nil
	}
	values, err := Strings(data)
	if err != nil {
		return nil, err
	}

	bom := len(data) - len(StripBOM(data))
	doc := data[bom:]

	type edit struct {
		start, end int
		literal    []byte
	}
	var edits []edit
	for _, v := range values {
		target, ok := replacements[v.Pointer]
		if !ok || target == v.Value {
			continue
		}
		lit, err := encodeString(target)
		if err != nil {
			return nil, err
		}
		edits = append(edits, edit{v.start, v.end, lit})
	}
	if len(edits) == 0 {
		return data, nil
	}
	sort.Slice(edits, func(i, j int) bool { return edits[i].start < edits[j].start })

	var buf bytes.Buffer
	buf.Grow(len(data))
	buf.Write(data[:bom])
	last := 0
	for _, e := range edits {
		buf.Write(doc[last:e.start])
		buf.Write(e.literal)
		last = e.end
	}
	buf.Write(doc[last:])
	return buf.Bytes(), nil
}

// encodeString gera o literal JSON em UTF-8 sem escapar <, > e &, como o
// json.dumps(ensure_ascii=False) da versão Python.
func encodeString(s string) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(s); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}
