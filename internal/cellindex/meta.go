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
	held, _ := session.LoadMeta(c.Root) // the sealed truth the summary is written beside
	return session.SaveMeta(c.Root, d.Derive(held))
}

// spendEpsilon is the dollars below which two spends are the same: the ledger
// keeps whole micro dollars.
const spendEpsilon = 1e-6

// Behind reports whether the transcript holds spend the row does not: the row
// is from an older copy. A row that says MORE than the transcript is the live
// session's own total, which also counts calls no transcript line holds.
func (metaIndex) Behind(c cell.Cell, d session.Digest) bool {
	m, _ := session.LoadMeta(c.Root)
	usd, tokens := d.Spend()
	return usd > m.SpentUSD+spendEpsilon || tokens > m.Tokens
}
