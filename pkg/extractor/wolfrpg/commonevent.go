package wolfrpg

import (
	"bytes"
	"errors"
	"fmt"
)

// ErrProProtected indica um arquivo com a proteção comercial do WOLF RPG Pro 3.x,
// que precisa ser removida (por exemplo com o UberWolf) antes da tradução.
var ErrProProtected = errors.New("file uses WOLF RPG Pro 3.x protection; remove it first (e.g. UberWolf 'Remove Pro Protection')")

var (
	commonMagicSJIS = []byte("\x00W\x00\x00OL\x00FC\x00")
	commonMagicUTF8 = []byte("\x00W\x00\x00OLUFC\x00")
	eventMagic      = []byte{0x0A, 0x00, 0x00, 0x00}
	eventMagic3     = []byte{0x0B, 0x00, 0x00, 0x00}
)

// WolfCommonEvents representa o arquivo CommonEvent.dat. Todos os blocos são
// guardados para que a regravação reproduza o arquivo byte a byte.
type WolfCommonEvents struct {
	Magic          []byte
	IsUTF8         bool
	Events         []*WolfCommonEvent
	LastTerminator byte
}

// WolfCommonEvent é um evento comum.
type WolfCommonEvent struct {
	ID          uint32
	Unknown1    uint32
	Blank1      []byte
	Name        string
	Commands    []*WolfCommand
	Unknown11   string
	Description string
	Unknown3    []string
	Unknown31   *uint32
	Unknown32   byte
	Unknown4    []byte
	Unknown5    [][]string
	Unknown6    [][]uint32
	Unknown7    []byte
	Unknown8    []string
	Unknown9    string
	Is0x92      bool
	Unknown10   string
	Unknown12   uint32
}

// LoadWolfCommonEvents lê o CommonEvent.dat do WOLF RPG 2.x (Shift-JIS) e 3.x (UTF-8).
func LoadWolfCommonEvents(data []byte) (*WolfCommonEvents, error) {
	if len(data) < 11 {
		return nil, ErrInvalidCommonMagic
	}
	ce := &WolfCommonEvents{Magic: append([]byte(nil), data[:11]...)}
	switch {
	case bytes.HasPrefix(data, commonMagicUTF8):
		if data[10] == 0xCC {
			return nil, fmt.Errorf("CommonEvent.dat: %w", ErrProProtected)
		}
		ce.IsUTF8 = true
	case bytes.HasPrefix(data, commonMagicSJIS):
	default:
		return nil, ErrInvalidCommonMagic
	}

	coder := NewReaderCoder(data, ce.IsUTF8)
	if _, err := coder.ReadBytes(11); err != nil {
		return nil, err
	}
	count, err := coder.ReadU4()
	if err != nil {
		return nil, err
	}
	for range count {
		ev, err := readCommonEvent(coder)
		if err != nil {
			return nil, fmt.Errorf("common event %d: %w", len(ce.Events), err)
		}
		ce.Events = append(ce.Events, ev)
	}
	if ce.LastTerminator, err = coder.ReadU1(); err != nil {
		return nil, err
	}
	return ce, nil
}

func readCommonEvent(c *WolfFileCoder) (*WolfCommonEvent, error) {
	var err error
	expect := func(want byte) error {
		got, err := c.ReadU1()
		if err != nil {
			return err
		}
		if got != want {
			return fmt.Errorf("expected indicator %#x, got %#x", want, got)
		}
		return nil
	}
	if err := expect(0x8E); err != nil {
		return nil, err
	}

	ev := &WolfCommonEvent{}
	if ev.ID, err = c.ReadU4(); err != nil {
		return nil, err
	}
	if ev.Unknown1, err = c.ReadU4(); err != nil {
		return nil, err
	}
	if ev.Blank1, err = c.ReadBytes(7); err != nil {
		return nil, err
	}
	if ev.Name, err = c.ReadString(); err != nil {
		return nil, err
	}
	n, err := c.ReadU4()
	if err != nil {
		return nil, err
	}
	ev.Commands = make([]*WolfCommand, n)
	for i := range ev.Commands {
		if ev.Commands[i], err = readWolfCommand(c); err != nil {
			return nil, err
		}
	}
	if ev.Unknown11, err = c.ReadString(); err != nil {
		return nil, err
	}
	if ev.Description, err = c.ReadString(); err != nil {
		return nil, err
	}
	if err := expect(0x8F); err != nil {
		return nil, err
	}

	switch {
	case c.TryVerify(eventMagic):
		if ev.Unknown3, err = c.ReadStringArray(10); err != nil {
			return nil, err
		}
	case c.TryVerify(eventMagic3):
		if ev.Unknown3, err = c.ReadStringArray(10); err != nil {
			return nil, err
		}
		v, err := c.ReadU4()
		if err != nil {
			return nil, err
		}
		ev.Unknown31 = &v
		if ev.Unknown32, err = c.ReadU1(); err != nil {
			return nil, err
		}
	default:
		return nil, errors.New("unknown common event block magic")
	}

	if err := c.Verify(eventMagic); err != nil {
		return nil, err
	}
	if ev.Unknown4, err = c.ReadByteArray(10); err != nil {
		return nil, err
	}
	if err := c.Verify(eventMagic); err != nil {
		return nil, err
	}
	ev.Unknown5 = make([][]string, 10)
	for i := range ev.Unknown5 {
		if ev.Unknown5[i], err = c.ReadStringArray(-1); err != nil {
			return nil, err
		}
	}
	if err := c.Verify(eventMagic); err != nil {
		return nil, err
	}
	ev.Unknown6 = make([][]uint32, 10)
	for i := range ev.Unknown6 {
		if ev.Unknown6[i], err = c.ReadIntArray(-1); err != nil {
			return nil, err
		}
	}
	if ev.Unknown7, err = c.ReadBytes(0x1D); err != nil {
		return nil, err
	}
	if ev.Unknown8, err = c.ReadStringArray(100); err != nil {
		return nil, err
	}
	if err := expect(0x91); err != nil {
		return nil, err
	}
	if ev.Unknown9, err = c.ReadString(); err != nil {
		return nil, err
	}

	ind, err := c.ReadU1()
	if err != nil {
		return nil, err
	}
	switch ind {
	case 0x91:
	case 0x92:
		ev.Is0x92 = true
		if ev.Unknown10, err = c.ReadString(); err != nil {
			return nil, err
		}
		if ev.Unknown12, err = c.ReadU4(); err != nil {
			return nil, err
		}
		if err := expect(0x92); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("expected 0x91 or 0x92, got %#x", ind)
	}
	return ev, nil
}

// Save serializa o CommonEvent.dat.
func (ce *WolfCommonEvents) Save() []byte {
	c := NewWriterCoder(ce.IsUTF8)
	c.WriteBytes(ce.Magic)
	c.WriteU4(uint32(len(ce.Events)))
	for _, ev := range ce.Events {
		ev.write(c)
	}
	c.WriteU1(ce.LastTerminator)
	return c.Bytes()
}

func (ev *WolfCommonEvent) write(c *WolfFileCoder) {
	c.WriteU1(0x8E)
	c.WriteU4(ev.ID)
	c.WriteU4(ev.Unknown1)
	c.WriteBytes(ev.Blank1)
	c.WriteString(ev.Name)
	c.WriteU4(uint32(len(ev.Commands)))
	for _, cmd := range ev.Commands {
		writeWolfCommand(c, cmd)
	}
	c.WriteString(ev.Unknown11)
	c.WriteString(ev.Description)
	c.WriteU1(0x8F)

	if ev.Unknown31 == nil {
		c.WriteBytes(eventMagic)
	} else {
		c.WriteBytes(eventMagic3)
	}
	for _, s := range ev.Unknown3 {
		c.WriteString(s)
	}
	if ev.Unknown31 != nil {
		c.WriteU4(*ev.Unknown31)
		c.WriteU1(ev.Unknown32)
	}

	c.WriteBytes(eventMagic)
	c.WriteBytes(ev.Unknown4)
	c.WriteBytes(eventMagic)
	for _, arr := range ev.Unknown5 {
		c.WriteStringArray(arr)
	}
	c.WriteBytes(eventMagic)
	for _, arr := range ev.Unknown6 {
		c.WriteIntArray(arr)
	}
	c.WriteBytes(ev.Unknown7)
	for _, s := range ev.Unknown8 {
		c.WriteString(s)
	}
	c.WriteU1(0x91)
	c.WriteString(ev.Unknown9)
	if ev.Is0x92 {
		c.WriteU1(0x92)
		c.WriteString(ev.Unknown10)
		c.WriteU4(ev.Unknown12)
		c.WriteU1(0x92)
	} else {
		c.WriteU1(0x91)
	}
}
