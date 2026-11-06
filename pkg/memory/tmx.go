package memory

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"strings"
)

// Pair é um par origem/tradução guardado na memória.
type Pair struct {
	Source string
	Target string
}

// Pairs devolve os pares distintos de um par de idiomas, opcionalmente filtrados
// por status (ex.: reviewed, locked), na ordem de inserção.
func (tm *TranslationMemory) Pairs(ctx context.Context, srcLang, tgtLang string, statuses ...Status) ([]Pair, error) {
	if tm.isClosed.Load() {
		return nil, ErrDBClosed
	}
	query := `SELECT source, target FROM segment WHERE src_lang = ? AND tgt_lang = ?`
	args := []any{srcLang, tgtLang}
	if len(statuses) > 0 {
		query += ` AND status IN (?` + strings.Repeat(",?", len(statuses)-1) + `)`
		for _, s := range statuses {
			args = append(args, string(s))
		}
	}
	query += ` GROUP BY source, target ORDER BY MIN(id)`
	rows, err := tm.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var pairs []Pair
	for rows.Next() {
		var p Pair
		if err := rows.Scan(&p.Source, &p.Target); err != nil {
			return nil, err
		}
		pairs = append(pairs, p)
	}
	return pairs, rows.Err()
}

type tmxDoc struct {
	XMLName xml.Name  `xml:"tmx"`
	Version string    `xml:"version,attr"`
	Header  tmxHeader `xml:"header"`
	Units   []tmxUnit `xml:"body>tu"`
}

type tmxHeader struct {
	CreationTool        string `xml:"creationtool,attr"`
	CreationToolVersion string `xml:"creationtoolversion,attr"`
	DataType            string `xml:"datatype,attr"`
	SegType             string `xml:"segtype,attr"`
	AdminLang           string `xml:"adminlang,attr"`
	SrcLang             string `xml:"srclang,attr"`
	OTMF                string `xml:"o-tmf,attr"`
}

type tmxUnit struct {
	Variants []tmxVariant `xml:"tuv"`
}

type tmxVariant struct {
	Lang    string `xml:"http://www.w3.org/XML/1998/namespace lang,attr"`
	LangOld string `xml:"lang,attr"`
	Seg     string `xml:"seg"`
}

// WriteTMX grava os pares em TMX 1.4b, o formato de intercâmbio de CAT tools
// (OmegaT, Weblate), como o scripts/tm_export.py da versão Python.
func WriteTMX(w io.Writer, srcLang, tgtLang string, pairs []Pair) error {
	if _, err := io.WriteString(w, xml.Header); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, `<tmx version="1.4"><header creationtool="extractor-and-translation" creationtoolversion="1.0" datatype="plaintext" segtype="sentence" adminlang="en" srclang=%q o-tmf="extractor-and-translation" /><body>`, srcLang); err != nil {
		return err
	}
	for _, p := range pairs {
		if _, err := fmt.Fprintf(w, `<tu><tuv xml:lang=%q><seg>%s</seg></tuv><tuv xml:lang=%q><seg>%s</seg></tuv></tu>`,
			srcLang, escapeXML(p.Source), tgtLang, escapeXML(p.Target)); err != nil {
			return err
		}
	}
	_, err := io.WriteString(w, "</body></tmx>")
	return err
}

func escapeXML(s string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

// TMXUnit é uma unidade lida de um TMX.
type TMXUnit struct {
	SrcLang, TgtLang string
	Source, Target   string
}

// ReadTMX lê as unidades com pelo menos duas variantes; devolve também quantas
// unidades incompletas foram ignoradas.
func ReadTMX(r io.Reader) (units []TMXUnit, skipped int, err error) {
	var doc tmxDoc
	if err := xml.NewDecoder(r).Decode(&doc); err != nil {
		return nil, 0, fmt.Errorf("invalid TMX: %w", err)
	}
	for _, tu := range doc.Units {
		var vs []tmxVariant
		for _, v := range tu.Variants {
			if v.Lang == "" {
				v.Lang = v.LangOld
			}
			if v.Lang != "" && v.Seg != "" {
				vs = append(vs, v)
			}
		}
		if len(vs) < 2 {
			skipped++
			continue
		}
		units = append(units, TMXUnit{SrcLang: vs[0].Lang, TgtLang: vs[1].Lang, Source: vs[0].Seg, Target: vs[1].Seg})
	}
	return units, skipped, nil
}
