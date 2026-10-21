package cell

import (
	"encoding/hex"
	"fmt"
)

// Class is declared per cell, never detected (SCHEMAS.md §4).
type Class string

const (
	FilesOnly Class = "files-only"
	Sandboxed Class = "sandboxed"
	HostBound Class = "host-bound"
)

var knownClasses = map[Class]bool{FilesOnly: true, Sandboxed: true, HostBound: true}

// Base is where a fetch optimization may come from. Remote never carries
// credentials.
type Base struct {
	Remote string `json:"remote,omitempty"`
	SHA    string `json:"sha,omitempty"`
}

// Meta is .cell/meta.json. V is first so readers can branch on it.
type Meta struct {
	V         uint16 `json:"V"`
	Class     Class  `json:"class"`
	CellKeyID string `json:"cell_key_id"`
	Base      *Base  `json:"base,omitempty"`
}

func (o Options) meta() (Meta, error) {
	if o.KeyID == "" {
		id, err := newKeyID()
		if err != nil {
			return Meta{}, err
		}
		o.KeyID = id
	}
	m := Meta{V: schemaV, Class: o.Class, CellKeyID: o.KeyID, Base: o.Base}
	return m, m.validate()
}

func (m Meta) validate() error {
	if m.V != schemaV {
		return fmt.Errorf("%w: version %d", errBadMeta, m.V)
	}
	if !knownClasses[m.Class] {
		return fmt.Errorf("%w: class %q", errBadMeta, m.Class)
	}
	if raw, err := hex.DecodeString(m.CellKeyID); err != nil || len(raw) != 16 {
		return fmt.Errorf("%w: cell_key_id must be 32 hex chars", errBadMeta)
	}
	return nil
}
