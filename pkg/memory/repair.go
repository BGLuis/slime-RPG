package memory

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// Categorias de RepairReport.
const (
	RepairCase   = "case"
	RepairName   = "name"
	RepairXTOK   = "xtok"
	RepairEngine = "engine"
)

// RepairCategories é a ordem de exibição das categorias.
var RepairCategories = []string{RepairCase, RepairName, RepairXTOK, RepairEngine}

// testEngines são engines de dublês de teste que vazaram para o banco real.
var testEngines = []string{"prefix_translate", "basetranslate", "dummyprescantranslator"}

// upperNormalized são os códigos que o Python maiusculiza de propósito (_FORMAT_UPPER);
// o RPG Maker os lê sem distinguir caixa, então não são o alvo do reparo.
var upperNormalized = map[string]bool{"c": true, "v": true, "i": true, "ce": true, "m": true, "n": true, "p": true, "g": true}

var (
	// codeWithArg casa códigos de plugin com argumento (\F[ahr]); o bug do Python só
	// alterava a caixa de códigos com argumento.
	codeWithArg = regexp.MustCompile(`\\([A-Za-z]+)\s*\[`)
	nameTag     = regexp.MustCompile(`\\(?:nam[eE]|nom[eE]|name|nome)\s*\[`)
)

// RepairExample é uma amostra de linha alterada ou removida.
type RepairExample struct {
	ID     int64
	Before string
	After  string
}

// RepairCategory resume o que uma categoria alteraria (ou alterou).
type RepairCategory struct {
	Rows     int
	Examples []RepairExample
}

// RepairReport agrega as categorias; ProtectedRows conta linhas reviewed/locked
// afetadas (alteradas ou removidas).
type RepairReport struct {
	Categories    map[string]*RepairCategory
	ProtectedRows int
	BackupPath    string
	Applied       bool
}

const repairExampleLimit = 3

// controlCodes devolve, por nome minúsculo, as grafias de códigos com argumento presentes em text.
func controlCodes(text string) map[string]map[string]bool {
	out := map[string]map[string]bool{}
	for _, m := range codeWithArg.FindAllStringSubmatchIndex(text, -1) {
		if m[0] > 0 && text[m[0]-1] == '\\' {
			continue
		}
		name := text[m[2]:m[3]]
		low := strings.ToLower(name)
		if out[low] == nil {
			out[low] = map[string]bool{}
		}
		out[low][name] = true
	}
	return out
}

// RestoreControlCodeCase devolve target com a caixa dos códigos restaurada a partir
// de source. Só troca um código quando ele é variante de caixa de um único código
// presente no source; prosa e códigos ausentes do source ficam intactos.
func RestoreControlCodeCase(source, target string) string {
	known := controlCodes(source)
	if len(known) == 0 {
		return target
	}
	var b strings.Builder
	last := 0
	for _, m := range codeWithArg.FindAllStringSubmatchIndex(target, -1) {
		if m[0] > 0 && target[m[0]-1] == '\\' {
			continue
		}
		name := target[m[2]:m[3]]
		low := strings.ToLower(name)
		spellings := known[low]
		if upperNormalized[low] || len(spellings) != 1 || spellings[name] {
			continue
		}
		for fixed := range spellings {
			b.WriteString(target[last:m[2]])
			b.WriteString(fixed)
			last = m[3]
		}
	}
	b.WriteString(target[last:])
	return b.String()
}

// NormalizeNameTags converte \nome[, \namE[, \name[... em \NAME[.
func NormalizeNameTags(target string) string {
	return nameTag.ReplaceAllLiteralString(target, `\NAME[`)
}

// Repair corrige e limpa o memory.db em dbPath. Sem apply é um dry-run que não
// escreve nada; com apply cria antes um backup (VACUUM INTO, seguro com WAL) e
// aplica tudo numa única transação.
func Repair(ctx context.Context, dbPath string, apply bool) (*RepairReport, error) {
	if _, err := os.Stat(dbPath); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", "file:"+dbPath+"?_pragma=busy_timeout(10000)")
	if err != nil {
		return nil, err
	}
	defer db.Close()

	rep := &RepairReport{Categories: map[string]*RepairCategory{}, Applied: apply}
	for _, c := range RepairCategories {
		rep.Categories[c] = &RepairCategory{}
	}
	if apply {
		rep.BackupPath = filepath.Join(filepath.Dir(dbPath), filepath.Base(dbPath)+".bak-"+time.Now().Format("20060102-150405"))
		if _, err := db.ExecContext(ctx, "VACUUM INTO ?", rep.BackupPath); err != nil {
			return nil, fmt.Errorf("falha ao criar backup: %w", err)
		}
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	type row struct {
		id                             int64
		source, target, engine, status string
	}
	rows, err := tx.QueryContext(ctx, "SELECT id, source, target, engine, status FROM segment")
	if err != nil {
		return nil, err
	}
	var deletes []int64
	updates := map[int64]string{}
	isTestEngine := map[string]bool{}
	for _, e := range testEngines {
		isTestEngine[e] = true
	}
	add := func(cat string, r row, after string) {
		c := rep.Categories[cat]
		c.Rows++
		if len(c.Examples) < repairExampleLimit {
			c.Examples = append(c.Examples, RepairExample{ID: r.id, Before: r.target, After: after})
		}
	}
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.source, &r.target, &r.engine, &r.status); err != nil {
			rows.Close()
			return nil, err
		}
		switch {
		case strings.Contains(r.source, "__XTOK_") || strings.Contains(r.target, "__XTOK_"):
			add(RepairXTOK, r, "")
			deletes = append(deletes, r.id)
		case isTestEngine[strings.ToLower(r.engine)]:
			add(RepairEngine, r, "")
			deletes = append(deletes, r.id)
		default:
			fixed := RestoreControlCodeCase(r.source, r.target)
			if fixed != r.target {
				add(RepairCase, r, fixed)
			}
			final := NormalizeNameTags(fixed)
			if final != fixed {
				add(RepairName, r, final)
			}
			if final == r.target {
				continue
			}
			updates[r.id] = final
		}
		if r.status == string(StatusReviewed) || r.status == string(StatusLocked) {
			rep.ProtectedRows++
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	if !apply {
		return rep, nil
	}
	now := time.Now().Format("2006-01-02T15:04:05")
	for id, target := range updates {
		if _, err := tx.ExecContext(ctx, "UPDATE segment SET target = ?, updated_at = ? WHERE id = ?", target, now, id); err != nil {
			return nil, err
		}
	}
	for _, id := range deletes {
		if _, err := tx.ExecContext(ctx, "DELETE FROM segment WHERE id = ?", id); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return rep, nil
}

// MigrateReport resume uma migração de caches JSON legados.
type MigrateReport struct {
	Files, Imported, SkippedNull, Identity int
}

var legacyFile = regexp.MustCompile(`^cache_([a-z]{2,})_([a-z]{2,})\.json$`)

// MigrateLegacy importa dir/<engine>/cache_<src>_<tgt>.json para a memória.
// É idempotente (chave única src+tgt+hash+engine) e não altera os JSONs.
func MigrateLegacy(ctx context.Context, tm *TranslationMemory, dir string) (*MigrateReport, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "*", "cache_*.json"))
	if err != nil {
		return nil, err
	}
	rep := &MigrateReport{}
	for _, path := range paths {
		m := legacyFile.FindStringSubmatch(filepath.Base(path))
		if m == nil {
			continue
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		var data map[string]any
		if err := json.Unmarshal(raw, &data); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		engine := filepath.Base(filepath.Dir(path))
		var entries []Entry
		for source, v := range data {
			if source == "" {
				continue
			}
			target, ok := v.(string)
			if !ok {
				rep.SkippedNull++
				continue
			}
			if source == target {
				rep.Identity++
			}
			entries = append(entries, Entry{SrcLang: m[1], TgtLang: m[2], SourceText: source, TargetText: target,
				Engine: engine, Status: StatusMachine, AllowIdentity: true})
		}
		if err := tm.StoreBatch(ctx, entries); err != nil {
			return nil, err
		}
		rep.Files++
		rep.Imported += len(entries)
	}
	return rep, nil
}
