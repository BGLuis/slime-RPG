package wolfrpg

import (
	"bytes"
	"errors"
	"fmt"
)

var (
	databaseMagic     = []byte("W\x00\x00OL")
	databaseMagicNext = []byte("FM\x00")
	typeSeparator     = []byte{0xFE, 0xFF, 0xFF, 0xFF}
	datSeedIndices    = []int{0, 3, 9}

	// ErrInvalidDatabase indica um .dat que não é banco de dados do WOLF RPG.
	ErrInvalidDatabase = errors.New("invalid wolf database")
)

const (
	fieldIntStart    = 0x03E8
	fieldStringStart = 0x07D0
)

// WolfDatabase é um banco de dados do WOLF RPG (DataBase.dat, CDataBase.dat,
// SysDatabase.dat). Os nomes de tipos, campos e dados ficam no .project; sem ele
// recebem nomes genéricos. Só o .dat é regravado: o .project guarda o esquema do
// editor e nunca é alterado.
type WolfDatabase struct {
	HasLeadingZero bool
	EncodingType   byte
	IsUTF8         bool
	EngineVersion  byte
	Types          []*WolfDatabaseType
	LastTerminator byte
}

// WolfDatabaseType é uma tabela do banco.
type WolfDatabaseType struct {
	Name        string
	Description string
	Unknown1    uint32
	Fields      []*WolfDatabaseField
	Data        []*WolfDatabaseData
}

// WolfDatabaseField é uma coluna; IndexInfo diz se é inteiro ou texto.
type WolfDatabaseField struct {
	Name      string
	IndexInfo uint32
}

func (f *WolfDatabaseField) isString() bool { return f.IndexInfo >= fieldStringStart }
func (f *WolfDatabaseField) isInt() bool {
	return f.IndexInfo >= fieldIntStart && f.IndexInfo < fieldStringStart
}

// WolfDatabaseData é um registro.
type WolfDatabaseData struct {
	Name         string
	IntValues    []uint32
	StringValues []string
}

// LoadWolfDatabase lê o .dat (descriptografando o formato v1 se preciso) e, se
// houver, o .project com os nomes do esquema.
func LoadWolfDatabase(dat, project []byte) (*WolfDatabase, error) {
	if bytes.HasPrefix(dat, []byte("\x00W\x00\x00OLUFM\x00")) && len(dat) > 10 && (dat[10] == 0xC4 || dat[10] == 0xCC) {
		return nil, fmt.Errorf("database: %w", ErrProProtected)
	}
	db := &WolfDatabase{}
	db.HasLeadingZero = bytes.HasPrefix(dat, append([]byte{0}, databaseMagic...))
	if !bytes.HasPrefix(dat, databaseMagic) && !db.HasLeadingZero {
		if len(dat) < 10 {
			return nil, ErrInvalidDatabase
		}
		seeds := make([]uint32, len(datSeedIndices))
		for i, idx := range datSeedIndices {
			seeds[i] = uint32(dat[idx])
		}
		dat = DecryptDatV1(dat, seeds, []int{1, 2, 5})
		db.HasLeadingZero = bytes.HasPrefix(dat, append([]byte{0}, databaseMagic...))
	}

	c := NewReaderCoder(dat, false)
	if db.HasLeadingZero {
		if _, err := c.ReadU1(); err != nil {
			return nil, err
		}
	}
	if err := c.Verify(databaseMagic); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidDatabase, err)
	}
	var err error
	if db.EncodingType, err = c.ReadU1(); err != nil {
		return nil, err
	}
	db.IsUTF8 = db.EncodingType == 85
	c.IsUTF8 = db.IsUTF8
	if err := c.Verify(databaseMagicNext); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidDatabase, err)
	}
	if db.EngineVersion, err = c.ReadU1(); err != nil {
		return nil, err
	}
	numTypes, err := c.ReadU4()
	if err != nil {
		return nil, err
	}

	if len(project) > 0 {
		if db.Types, err = readProject(project, db.IsUTF8); err != nil {
			return nil, fmt.Errorf("project: %w", err)
		}
	}
	if len(db.Types) == 0 {
		for i := range numTypes {
			db.Types = append(db.Types, &WolfDatabaseType{Name: fmt.Sprintf("Type_%d", i)})
		}
	}

	for _, t := range db.Types {
		if err := c.Verify(typeSeparator); err != nil {
			return nil, err
		}
		if t.Unknown1, err = c.ReadU4(); err != nil {
			return nil, err
		}
		fieldsSize, err := c.ReadU4()
		if err != nil {
			return nil, err
		}
		for i := len(t.Fields); i < int(fieldsSize); i++ {
			t.Fields = append(t.Fields, &WolfDatabaseField{Name: fmt.Sprintf("Field_%d", i)})
		}
		t.Fields = t.Fields[:fieldsSize]
		for _, f := range t.Fields {
			if f.IndexInfo, err = c.ReadU4(); err != nil {
				return nil, err
			}
		}
		dataSize, err := c.ReadU4()
		if err != nil {
			return nil, err
		}
		for i := len(t.Data); i < int(dataSize); i++ {
			t.Data = append(t.Data, &WolfDatabaseData{Name: fmt.Sprintf("Data_%d", i)})
		}
		t.Data = t.Data[:dataSize]

		ints, strs := 0, 0
		for _, f := range t.Fields {
			switch {
			case f.isInt():
				ints++
			case f.isString():
				strs++
			}
		}
		for _, d := range t.Data {
			if d.IntValues, err = c.ReadIntArray(ints); err != nil {
				return nil, err
			}
			if d.StringValues, err = c.ReadStringArray(strs); err != nil {
				return nil, err
			}
		}
	}
	if db.LastTerminator, err = c.ReadU1(); err != nil {
		return nil, err
	}
	return db, nil
}

// readProject lê do .project os nomes de tipos, campos e dados e a descrição de
// cada tipo; os demais blocos do esquema não interessam à tradução.
func readProject(data []byte, isUTF8 bool) ([]*WolfDatabaseType, error) {
	c := NewReaderCoder(data, isUTF8)
	count, err := c.ReadU4()
	if err != nil {
		return nil, err
	}
	var types []*WolfDatabaseType
	for range count {
		t := &WolfDatabaseType{}
		if t.Name, err = c.ReadString(); err != nil {
			return nil, err
		}
		names, err := c.ReadStringArray(-1)
		if err != nil {
			return nil, err
		}
		for _, n := range names {
			t.Fields = append(t.Fields, &WolfDatabaseField{Name: n})
		}
		dataNames, err := c.ReadStringArray(-1)
		if err != nil {
			return nil, err
		}
		for _, n := range dataNames {
			t.Data = append(t.Data, &WolfDatabaseData{Name: n})
		}
		if t.Description, err = c.ReadString(); err != nil {
			return nil, err
		}

		listSize, err := c.ReadU4()
		if err != nil {
			return nil, err
		}
		if _, err := c.ReadBytes(int(listSize)); err != nil {
			return nil, err
		}
		if _, err := c.ReadStringArray(min(len(t.Fields), int(mustU4(c)))); err != nil {
			return nil, err
		}
		n := min(len(t.Fields), int(mustU4(c)))
		for range n {
			if _, err := c.ReadStringArray(-1); err != nil {
				return nil, err
			}
		}
		n = min(len(t.Fields), int(mustU4(c)))
		for range n {
			if _, err := c.ReadIntArray(-1); err != nil {
				return nil, err
			}
		}
		if _, err := c.ReadIntArray(min(len(t.Fields), int(mustU4(c)))); err != nil {
			return nil, err
		}
		types = append(types, t)
	}
	return types, nil
}

func mustU4(c *WolfFileCoder) uint32 {
	v, _ := c.ReadU4()
	return v
}

// Save serializa o .dat (sempre sem criptografia, como a versão Python).
func (db *WolfDatabase) Save() []byte {
	c := NewWriterCoder(db.IsUTF8)
	if db.HasLeadingZero {
		c.WriteU1(0)
	}
	c.WriteBytes(databaseMagic)
	c.WriteU1(db.EncodingType)
	c.WriteBytes(databaseMagicNext)
	c.WriteU1(db.EngineVersion)
	c.WriteU4(uint32(len(db.Types)))
	for _, t := range db.Types {
		c.WriteBytes(typeSeparator)
		c.WriteU4(t.Unknown1)
		c.WriteU4(uint32(len(t.Fields)))
		for _, f := range t.Fields {
			c.WriteU4(f.IndexInfo)
		}
		c.WriteU4(uint32(len(t.Data)))
		for _, d := range t.Data {
			for _, v := range d.IntValues {
				c.WriteU4(v)
			}
			for _, s := range d.StringValues {
				c.WriteString(s)
			}
		}
	}
	c.WriteU1(db.LastTerminator)
	return c.Bytes()
}

var gameDatMagic = []byte("W\x00\x00OL\x00FM")

// WolfGameDat é o Game.dat: configurações, título e fontes do jogo.
type WolfGameDat struct {
	HasLeadingZero bool
	EncodingType   byte
	IsUTF8         bool
	ByteSettings   []byte
	StrCount       uint32
	Title          string
	Serial         string
	EncryptionKey  []byte
	Font           string
	Subfonts       []string
	HeroGraphic    string
	ExtraStrings   []string
	Rest           []byte
}

// LoadWolfGameDat lê o Game.dat; devolve ErrInvalidDatabase se a assinatura não bater.
func LoadWolfGameDat(data []byte) (*WolfGameDat, error) {
	g := &WolfGameDat{Serial: "0000-0000", Subfonts: []string{"", "", ""}}
	g.HasLeadingZero = bytes.HasPrefix(data, append([]byte{0}, gameDatMagic...))
	c := NewReaderCoder(data, false)
	if g.HasLeadingZero {
		_, _ = c.ReadU1()
	}
	if err := c.Verify(gameDatMagic); err != nil {
		return nil, fmt.Errorf("%w: Game.dat", ErrInvalidDatabase)
	}
	var err error
	if g.EncodingType, err = c.ReadU1(); err != nil {
		return nil, err
	}
	g.IsUTF8 = g.EncodingType == 85
	c.IsUTF8 = g.IsUTF8
	if g.ByteSettings, err = c.ReadByteArray(-1); err != nil {
		return nil, err
	}
	if g.StrCount, err = c.ReadU4(); err != nil {
		return nil, err
	}
	read := func(dst *string) error {
		v, err := c.ReadString()
		*dst = v
		return err
	}
	if g.StrCount >= 1 {
		if err := read(&g.Title); err != nil {
			return nil, err
		}
	}
	if g.StrCount >= 2 {
		if err := read(&g.Serial); err != nil {
			return nil, err
		}
	}
	if g.StrCount >= 3 {
		if g.EncryptionKey, err = c.ReadByteArray(-1); err != nil {
			return nil, err
		}
	}
	if g.StrCount >= 4 {
		if err := read(&g.Font); err != nil {
			return nil, err
		}
	}
	if g.StrCount >= 7 {
		for i := range g.Subfonts {
			if err := read(&g.Subfonts[i]); err != nil {
				return nil, err
			}
		}
	}
	if g.StrCount >= 8 {
		if err := read(&g.HeroGraphic); err != nil {
			return nil, err
		}
	}
	for i := uint32(8); i < g.StrCount; i++ {
		var s string
		if err := read(&s); err != nil {
			return nil, err
		}
		g.ExtraStrings = append(g.ExtraStrings, s)
	}
	g.Rest = c.Rest()
	return g, nil
}

// Save serializa o Game.dat. O bloco final começa com o tamanho total do
// arquivo, recalculado porque o título traduzido muda o tamanho.
func (g *WolfGameDat) Save() []byte {
	c := NewWriterCoder(g.IsUTF8)
	if g.HasLeadingZero {
		c.WriteU1(0)
	}
	c.WriteBytes(gameDatMagic)
	c.WriteU1(g.EncodingType)
	c.WriteU4(uint32(len(g.ByteSettings)))
	c.WriteBytes(g.ByteSettings)

	strCount := max(g.StrCount, 7)
	c.WriteU4(strCount)
	c.WriteString(g.Title)
	c.WriteString(g.Serial)
	c.WriteU4(uint32(len(g.EncryptionKey)))
	c.WriteBytes(g.EncryptionKey)
	c.WriteString(g.Font)
	for _, sf := range g.Subfonts {
		c.WriteString(sf)
	}
	if strCount >= 8 {
		c.WriteString(g.HeroGraphic)
	}
	for _, s := range g.ExtraStrings {
		c.WriteString(s)
	}

	if len(g.Rest) >= 4 {
		after := g.Rest[4:]
		c.WriteU4(uint32(c.Len() + 4 + len(after)))
		c.WriteBytes(after)
	} else {
		c.WriteBytes(g.Rest)
	}
	return c.Bytes()
}
