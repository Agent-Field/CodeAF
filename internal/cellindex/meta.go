package cellindex

import (
	"path/filepath"
	"strings"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/session"
)

func baseName(dir string) string { return filepath.Base(filepath.Clean(dir)) }

// metaIndex is the session folder's meta.json: the row a picker reads without
// opening the journal.
type metaIndex struct{}

func (metaIndex) Name() string { return "meta.json" }

func (metaIndex) Present(c cell.Cell) bool {
	m, _ := session.LoadMeta(c.Root)
	return strings.TrimSpace(m.ID) != ""
}

func (metaIndex) Build(c cell.Cell, d session.Digest) error {
	if d.ID == "" {
		return nil // no header yet: nothing has been said, so there is no row
	}
	return session.SaveMeta(c.Root, d.Derive(session.Meta{}))
}
