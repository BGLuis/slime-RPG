package model

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
)

// Erros de domínio para validação de Segmentos
var (
	ErrMissingKind = errors.New("segment kind is mandatory")
	ErrInvalidKind = errors.New("invalid segment kind")
	ErrEmptySource = errors.New("segment source cannot be empty")
	ErrEmptyID     = errors.New("segment id cannot be empty")
)

// SegmentKind define a taxonomia semântica de um texto extraído.
type SegmentKind string

const (
	KindDialogue SegmentKind = "dialogue" // Falas e diálogos na tela
	KindChoice   SegmentKind = "choice"   // Opções interativas de menus/escolhas
	KindScript   SegmentKind = "script"   // Strings extraídas de scripts avaliados
	KindMenu     SegmentKind = "menu"     // Nomes de comandos, títulos e UI de sistema
	KindDB       SegmentKind = "db"       // Registros de banco de dados (itens, habilidades, etc.)
)

// IsValid verifica se o SegmentKind pertence à safelist permitida.
func (k SegmentKind) IsValid() bool {
	switch k {
	case KindDialogue, KindChoice, KindScript, KindMenu, KindDB:
		return true
	default:
		return false
	}
}

// ComputeHash calcula o hash SHA-256 canônico de um texto fonte.
func ComputeHash(source string) string {
	sum := sha256.Sum256([]byte(source))
	return hex.EncodeToString(sum[:])
}

// Segment representa uma unidade atômica e imutável de texto extraído.
type Segment struct {
	id          string
	source      string
	target      string
	kind        SegmentKind
	sourceHash  string
	context     map[string]any
	constraints map[string]string
}

// NewSegment cria um novo Segment validando os campos obrigatórios e clonando os metadados.
func NewSegment(id, source string, kind SegmentKind, context map[string]any, constraints map[string]string) (*Segment, error) {
	if id == "" {
		return nil, ErrEmptyID
	}
	if source == "" {
		return nil, ErrEmptySource
	}
	if kind == "" {
		return nil, ErrMissingKind
	}
	if !kind.IsValid() {
		return nil, fmt.Errorf("%w: %q", ErrInvalidKind, kind)
	}

	// Cópia defensiva para garantir imutabilidade do contexto
	clonedCtx := make(map[string]any, len(context))
	maps.Copy(clonedCtx, context)

	clonedConstraints := make(map[string]string, len(constraints))
	maps.Copy(clonedConstraints, constraints)

	return &Segment{
		id:          id,
		source:      source,
		target:      "",
		kind:        kind,
		sourceHash:  ComputeHash(source),
		context:     clonedCtx,
		constraints: clonedConstraints,
	}, nil
}

// ID retorna o identificador canônico no formato URI.
func (s *Segment) ID() string {
	return s.id
}

// Source retorna o texto original.
func (s *Segment) Source() string {
	return s.source
}

// Target retorna a tradução do texto.
func (s *Segment) Target() string {
	return s.target
}

// Kind retorna a classificação semântica do segmento.
func (s *Segment) Kind() SegmentKind {
	return s.kind
}

// SourceHash retorna o hash SHA-256 do texto fonte.
func (s *Segment) SourceHash() string {
	return s.sourceHash
}

// ContextValue busca um valor no contexto de forma segura.
func (s *Segment) ContextValue(key string) (any, bool) {
	val, ok := s.context[key]
	return val, ok
}

// Context retorna uma cópia defensiva do mapa de contexto.
func (s *Segment) Context() map[string]any {
	clone := make(map[string]any, len(s.context))
	maps.Copy(clone, s.context)
	return clone
}

// ConstraintValue busca uma restrição configurada.
func (s *Segment) ConstraintValue(key string) (string, bool) {
	val, ok := s.constraints[key]
	return val, ok
}

// Constraints retorna uma cópia defensiva do mapa de restrições.
func (s *Segment) Constraints() map[string]string {
	clone := make(map[string]string, len(s.constraints))
	maps.Copy(clone, s.constraints)
	return clone
}

// WithTarget retorna uma cópia do segmento com a tradução preenchida, mantendo o original intacto.
func (s *Segment) WithTarget(target string) *Segment {
	return &Segment{
		id:          s.id,
		source:      s.source,
		target:      target,
		kind:        s.kind,
		sourceHash:  s.sourceHash,
		context:     s.context, // Mapas internos permanecem imutáveis
		constraints: s.constraints,
	}
}

// IsResolved indica se o segmento já possui uma tradução atribuída diferente de vazia.
func (s *Segment) IsResolved() bool {
	return s.target != ""
}
