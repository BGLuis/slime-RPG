package wolfrpg

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
)

var (
	ErrInvalidMapMagic    = errors.New("invalid wolf map magic header")
	ErrInvalidCommonMagic = errors.New("invalid common events magic header")
)

var (
	mapMagic = []byte("\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00WOLFM\x00")
)

// WolfRouteCommand representa um passo de rota de movimento de evento.
type WolfRouteCommand struct {
	ID   byte
	Args []uint32
}

func readWolfRouteCommand(coder *WolfFileCoder) (*WolfRouteCommand, error) {
	cmdID, err := coder.ReadU1()
	if err != nil {
		return nil, err
	}
	argsLen, err := coder.ReadU1()
	if err != nil {
		return nil, err
	}
	args := make([]uint32, argsLen)
	for i := range args {
		a, err := coder.ReadU4()
		if err != nil {
			return nil, err
		}
		args[i] = a
	}
	if err := coder.Verify([]byte{0x01, 0x00}); err != nil {
		return nil, err
	}
	return &WolfRouteCommand{ID: cmdID, Args: args}, nil
}

func writeWolfRouteCommand(coder *WolfFileCoder, cmd *WolfRouteCommand) {
	coder.WriteU1(cmd.ID)
	coder.WriteU1(byte(len(cmd.Args)))
	for _, a := range cmd.Args {
		coder.WriteU4(a)
	}
	coder.WriteBytes([]byte{0x01, 0x00})
}

// WolfMoveData guarda parâmetros extras quando o comando possui rota personalizada (terminator 1).
type WolfMoveData struct {
	Unknown []byte
	Flags   byte
	Route   []*WolfRouteCommand
}

// WolfCommand representa uma instrução executável dentro de um evento ou evento comum.
type WolfCommand struct {
	CID        WolfEventCode
	Args       []uint32
	StringArgs []string
	Indent     byte
	MoveData   *WolfMoveData
}

func readWolfCommand(coder *WolfFileCoder) (*WolfCommand, error) {
	rawArgsLen, err := coder.ReadU1()
	if err != nil {
		return nil, err
	}
	argsLen := int(rawArgsLen) - 1
	if argsLen < 0 {
		argsLen = 0
	}

	cidRaw, err := coder.ReadU4()
	if err != nil {
		return nil, err
	}

	args := make([]uint32, argsLen)
	for i := range args {
		a, err := coder.ReadU4()
		if err != nil {
			return nil, err
		}
		args[i] = a
	}

	indent, err := coder.ReadU1()
	if err != nil {
		return nil, err
	}

	stringArgsLen, err := coder.ReadU1()
	if err != nil {
		return nil, err
	}
	stringArgs := make([]string, stringArgsLen)
	for i := range stringArgs {
		s, err := coder.ReadString()
		if err != nil {
			return nil, err
		}
		stringArgs[i] = s
	}

	terminator, err := coder.ReadU1()
	if err != nil {
		return nil, err
	}

	var moveData *WolfMoveData
	if terminator == 1 {
		unknown, err := coder.ReadBytes(5)
		if err != nil {
			return nil, err
		}
		flags, err := coder.ReadU1()
		if err != nil {
			return nil, err
		}
		routeLen, err := coder.ReadU4()
		if err != nil {
			return nil, err
		}
		route := make([]*WolfRouteCommand, routeLen)
		for i := range route {
			rc, err := readWolfRouteCommand(coder)
			if err != nil {
				return nil, err
			}
			route[i] = rc
		}
		moveData = &WolfMoveData{
			Unknown: unknown,
			Flags:   flags,
			Route:   route,
		}
	} else if terminator != 0 {
		return nil, fmt.Errorf("unexpected command terminator: %d", terminator)
	}

	return &WolfCommand{
		CID:        WolfEventCode(cidRaw),
		Args:       args,
		StringArgs: stringArgs,
		Indent:     indent,
		MoveData:   moveData,
	}, nil
}

func writeWolfCommand(coder *WolfFileCoder, cmd *WolfCommand) {
	coder.WriteU1(byte(len(cmd.Args) + 1))
	coder.WriteU4(uint32(cmd.CID))
	for _, a := range cmd.Args {
		coder.WriteU4(a)
	}
	coder.WriteU1(cmd.Indent)
	coder.WriteU1(byte(len(cmd.StringArgs)))
	for _, s := range cmd.StringArgs {
		coder.WriteString(s)
	}

	if cmd.MoveData != nil {
		coder.WriteU1(1)
		if len(cmd.MoveData.Unknown) == 5 {
			coder.WriteBytes(cmd.MoveData.Unknown)
		} else {
			coder.WriteBytes(make([]byte, 5))
		}
		coder.WriteU1(cmd.MoveData.Flags)
		coder.WriteU4(uint32(len(cmd.MoveData.Route)))
		for _, rc := range cmd.MoveData.Route {
			writeWolfRouteCommand(coder, rc)
		}
	} else {
		coder.WriteTerminator(0)
	}
}

// WolfMapPage é a página de condições e comandos de um evento de mapa.
type WolfMapPage struct {
	ID                int
	Unknown1          uint32
	GraphicName       string
	GraphicDirection  byte
	GraphicFrame      byte
	GraphicOpacity    byte
	GraphicRenderMode byte
	Conditions        []byte
	Movement          []byte
	Flags             byte
	RouteFlags        byte
	Route             []*WolfRouteCommand
	Commands          []*WolfCommand
	Features          uint32
	ShadowGraphicNum  byte
	CollisionWidth    byte
	CollisionHeight   byte
	PageTransfer      *byte
}

func readWolfMapPage(coder *WolfFileCoder, pageID int) (*WolfMapPage, error) {
	p := &WolfMapPage{ID: pageID}
	var err error
	if p.Unknown1, err = coder.ReadU4(); err != nil {
		return nil, err
	}
	if p.GraphicName, err = coder.ReadString(); err != nil {
		return nil, err
	}
	if p.GraphicDirection, err = coder.ReadU1(); err != nil {
		return nil, err
	}
	if p.GraphicFrame, err = coder.ReadU1(); err != nil {
		return nil, err
	}
	if p.GraphicOpacity, err = coder.ReadU1(); err != nil {
		return nil, err
	}
	if p.GraphicRenderMode, err = coder.ReadU1(); err != nil {
		return nil, err
	}
	if p.Conditions, err = coder.ReadBytes(37); err != nil {
		return nil, err
	}
	if p.Movement, err = coder.ReadBytes(4); err != nil {
		return nil, err
	}
	if p.Flags, err = coder.ReadU1(); err != nil {
		return nil, err
	}
	if p.RouteFlags, err = coder.ReadU1(); err != nil {
		return nil, err
	}

	routeCount, err := coder.ReadU4()
	if err != nil {
		return nil, err
	}
	p.Route = make([]*WolfRouteCommand, routeCount)
	for i := range p.Route {
		rc, err := readWolfRouteCommand(coder)
		if err != nil {
			return nil, err
		}
		p.Route[i] = rc
	}

	cmdCount, err := coder.ReadU4()
	if err != nil {
		return nil, err
	}
	p.Commands = make([]*WolfCommand, cmdCount)
	for i := range p.Commands {
		cmd, err := readWolfCommand(coder)
		if err != nil {
			return nil, err
		}
		p.Commands[i] = cmd
	}

	if p.Features, err = coder.ReadU4(); err != nil {
		return nil, err
	}
	if p.ShadowGraphicNum, err = coder.ReadU1(); err != nil {
		return nil, err
	}
	if p.CollisionWidth, err = coder.ReadU1(); err != nil {
		return nil, err
	}
	if p.CollisionHeight, err = coder.ReadU1(); err != nil {
		return nil, err
	}

	if p.Features > 3 {
		pt, err := coder.ReadU1()
		if err != nil {
			return nil, err
		}
		p.PageTransfer = &pt
	}

	pTerm, err := coder.ReadU1()
	if err != nil {
		return nil, err
	}
	if pTerm != 0x7A {
		return nil, fmt.Errorf("invalid page terminator: %x", pTerm)
	}

	return p, nil
}

func writeWolfMapPage(coder *WolfFileCoder, p *WolfMapPage) {
	coder.WriteU4(p.Unknown1)
	coder.WriteString(p.GraphicName)
	coder.WriteU1(p.GraphicDirection)
	coder.WriteU1(p.GraphicFrame)
	coder.WriteU1(p.GraphicOpacity)
	coder.WriteU1(p.GraphicRenderMode)

	if len(p.Conditions) == 37 {
		coder.WriteBytes(p.Conditions)
	} else {
		coder.WriteBytes(make([]byte, 37))
	}

	if len(p.Movement) == 4 {
		coder.WriteBytes(p.Movement)
	} else {
		coder.WriteBytes(make([]byte, 4))
	}

	coder.WriteU1(p.Flags)
	coder.WriteU1(p.RouteFlags)

	coder.WriteU4(uint32(len(p.Route)))
	for _, rc := range p.Route {
		writeWolfRouteCommand(coder, rc)
	}

	coder.WriteU4(uint32(len(p.Commands)))
	for _, cmd := range p.Commands {
		writeWolfCommand(coder, cmd)
	}

	coder.WriteU4(p.Features)
	coder.WriteU1(p.ShadowGraphicNum)
	coder.WriteU1(p.CollisionWidth)
	coder.WriteU1(p.CollisionHeight)

	if p.Features > 3 && p.PageTransfer != nil {
		coder.WriteU1(*p.PageTransfer)
	}
	coder.WriteTerminator(0x7A)
}

// WolfMapEvent é um evento presente no grid do mapa.
type WolfMapEvent struct {
	ID    uint32
	Name  string
	X, Y  uint32
	Pages []*WolfMapPage
}

func readWolfMapEvent(coder *WolfFileCoder) (*WolfMapEvent, error) {
	if err := coder.Verify([]byte{0x39, 0x30, 0x00, 0x00}); err != nil {
		return nil, err
	}
	id, err := coder.ReadU4()
	if err != nil {
		return nil, err
	}
	name, err := coder.ReadString()
	if err != nil {
		return nil, err
	}
	x, err := coder.ReadU4()
	if err != nil {
		return nil, err
	}
	y, err := coder.ReadU4()
	if err != nil {
		return nil, err
	}
	pageCount, err := coder.ReadU4()
	if err != nil {
		return nil, err
	}
	if err := coder.Verify([]byte{0x00, 0x00, 0x00, 0x00}); err != nil {
		return nil, err
	}

	ev := &WolfMapEvent{ID: id, Name: name, X: x, Y: y}
	pageID := 0
	indicator, err := coder.ReadU1()
	if err != nil {
		return nil, err
	}
	for indicator == 0x79 {
		page, err := readWolfMapPage(coder, pageID)
		if err != nil {
			return nil, err
		}
		ev.Pages = append(ev.Pages, page)
		pageID++
		indicator, err = coder.ReadU1()
		if err != nil {
			return nil, err
		}
	}

	if uint32(len(ev.Pages)) != pageCount {
		return nil, fmt.Errorf("expected %d pages, read %d", pageCount, len(ev.Pages))
	}
	if indicator != 0x70 {
		return nil, fmt.Errorf("invalid event terminator: %x", indicator)
	}

	return ev, nil
}

func writeWolfMapEvent(coder *WolfFileCoder, ev *WolfMapEvent) {
	coder.WriteBytes([]byte{0x39, 0x30, 0x00, 0x00})
	coder.WriteU4(ev.ID)
	coder.WriteString(ev.Name)
	coder.WriteU4(ev.X)
	coder.WriteU4(ev.Y)
	coder.WriteU4(uint32(len(ev.Pages)))
	coder.WriteBytes([]byte{0x00, 0x00, 0x00, 0x00})

	for _, page := range ev.Pages {
		coder.WriteU1(0x79)
		writeWolfMapPage(coder, page)
	}
	coder.WriteTerminator(0x70)
}

// WolfMap representa o arquivo de mapa .mps.
type WolfMap struct {
	EncodingType uint32
	IsUTF8       bool
	Attributes   uint32
	Version      byte
	UnknownStr   string
	TilesetID    uint32
	Width        uint32
	Height       uint32
	NoTiles      bool
	Tiles        []byte
	Events       []*WolfMapEvent
	IsCompressed bool
}

// LoadWolfMap carrega e analisa um mapa .mps, suportando compressão LZ4 transparente.
func LoadWolfMap(data []byte) (*WolfMap, error) {
	raw := data
	isCompressed := false

	if len(raw) >= 25 && bytes.HasPrefix(raw, mapMagic) {
		attr := binary.LittleEndian.Uint32(raw[20:24])
		if attr >= 0x65 && len(raw) >= 33 {
			decSize := binary.LittleEndian.Uint32(raw[25:29])
			encSize := binary.LittleEndian.Uint32(raw[29:33])
			if int(33+encSize) <= len(raw) {
				decomp, err := DecompressLZ4(raw[33:33+encSize], int(decSize))
				if err == nil {
					raw = append(append([]byte{}, raw[:25]...), decomp...)
					isCompressed = true
				}
			}
		}
	}

	coder := NewReaderCoder(raw, false)
	if err := coder.Verify(mapMagic); err != nil {
		return nil, ErrInvalidMapMagic
	}

	m := &WolfMap{IsCompressed: isCompressed}
	var err error
	if m.EncodingType, err = coder.ReadU4(); err != nil {
		return nil, err
	}
	m.IsUTF8 = (m.EncodingType == 85)
	coder.IsUTF8 = m.IsUTF8

	if m.Attributes, err = coder.ReadU4(); err != nil {
		return nil, err
	}
	if m.Version, err = coder.ReadU1(); err != nil {
		return nil, err
	}
	if m.UnknownStr, err = coder.ReadString(); err != nil {
		return nil, err
	}
	if m.TilesetID, err = coder.ReadU4(); err != nil {
		return nil, err
	}
	if m.Width, err = coder.ReadU4(); err != nil {
		return nil, err
	}
	if m.Height, err = coder.ReadU4(); err != nil {
		return nil, err
	}
	if _, err = coder.ReadU4(); err != nil { // event_count
		return nil, err
	}

	if m.EncodingType != 0 {
		val, err := coder.ReadU4()
		if err == nil {
			if val == 0xFFFFFFFF {
				m.NoTiles = true
			} else {
				// Rebobina 4 bytes
				coder.r.Seek(-4, 1)
			}
		}
	}

	if !m.NoTiles {
		tilesLen := int(m.Width * m.Height * 3 * 4)
		if m.Tiles, err = coder.ReadBytes(tilesLen); err != nil {
			return nil, err
		}
	}

	if coder.EOF() {
		return m, nil
	}

	indicator, err := coder.ReadU1()
	if err != nil {
		return nil, err
	}
	for indicator == 0x6F {
		ev, err := readWolfMapEvent(coder)
		if err != nil {
			return nil, err
		}
		m.Events = append(m.Events, ev)
		indicator, err = coder.ReadU1()
		if err != nil {
			return nil, err
		}
	}

	if indicator != 0x66 {
		return nil, fmt.Errorf("invalid map terminator: %x", indicator)
	}

	return m, nil
}

// Save serializa o mapa para bytes com preservação do cabeçalho e compactação LZ4 se necessário.
func (m *WolfMap) Save() ([]byte, error) {
	if m.IsCompressed || m.Attributes >= 0x65 {
		headerCoder := NewWriterCoder(m.IsUTF8)
		headerCoder.WriteBytes(mapMagic)
		headerCoder.WriteU4(m.EncodingType)
		headerCoder.WriteU4(m.Attributes)
		headerCoder.WriteU1(m.Version)
		headerBytes := headerCoder.Bytes()

		bodyCoder := NewWriterCoder(m.IsUTF8)
		bodyCoder.WriteString(m.UnknownStr)
		bodyCoder.WriteU4(m.TilesetID)
		bodyCoder.WriteU4(m.Width)
		bodyCoder.WriteU4(m.Height)
		bodyCoder.WriteU4(uint32(len(m.Events)))

		if m.EncodingType != 0 && m.NoTiles {
			bodyCoder.WriteU4(0xFFFFFFFF)
		}
		if !m.NoTiles {
			bodyCoder.WriteBytes(m.Tiles)
		}

		for _, ev := range m.Events {
			bodyCoder.WriteU1(0x6F)
			writeWolfMapEvent(bodyCoder, ev)
		}
		bodyCoder.WriteTerminator(0x66)
		bodyBytes := bodyCoder.Bytes()

		compBytes := CompressLZ4(bodyBytes)
		var buf bytes.Buffer
		buf.Write(headerBytes)
		lenBuf := make([]byte, 8)
		binary.LittleEndian.PutUint32(lenBuf[0:4], uint32(len(bodyBytes)))
		binary.LittleEndian.PutUint32(lenBuf[4:8], uint32(len(compBytes)))
		buf.Write(lenBuf)
		buf.Write(compBytes)
		return buf.Bytes(), nil
	}

	coder := NewWriterCoder(m.IsUTF8)
	coder.WriteBytes(mapMagic)
	coder.WriteU4(m.EncodingType)
	coder.WriteU4(m.Attributes)
	coder.WriteU1(m.Version)
	coder.WriteString(m.UnknownStr)
	coder.WriteU4(m.TilesetID)
	coder.WriteU4(m.Width)
	coder.WriteU4(m.Height)
	coder.WriteU4(uint32(len(m.Events)))

	if m.EncodingType != 0 && m.NoTiles {
		coder.WriteU4(0xFFFFFFFF)
	}
	if !m.NoTiles {
		coder.WriteBytes(m.Tiles)
	}

	for _, ev := range m.Events {
		coder.WriteU1(0x6F)
		writeWolfMapEvent(coder, ev)
	}
	coder.WriteTerminator(0x66)
	return coder.Bytes(), nil
}
