package cellindex

import (
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/session"
)

// Index is one derived file the harness keeps about a session.
type Index interface {
	// Name is the index's word in a report.
	Name() string
	// Present reports whether the index already holds this session's rows.
	Present(c cell.Cell) bool
	// Build makes the index again from the transcript's digest.
	Build(c cell.Cell, d session.Digest) error
}

// Indexes is the registry, in build order.
var Indexes = []Index{metaIndex{}, usageIndex{}, taskIndex{}, memoryIndex{}}

// RebuildAt opens the cell at dir and rebuilds its missing indexes, for a
// session whose workspace is where workspace says on this machine.
func RebuildAt(dir, workspace string) ([]string, error) {
	c, err := cell.OpenAt(dir, baseName(dir))
	if err != nil {
		return nil, err
	}
	return Rebuild(c, workspace)
}

// Rebuild builds every missing index of the cell from its transcript and
// returns the names it built. An index already present is left alone: what is
// there was written by the session itself and may hold more than the
// transcript can say. The transcript is read only when something is missing.
//
// A sealed transcript names no folder of any machine, so the opener says where
// the workspace is HERE; when it does, that answer is the digest's workspace.
func Rebuild(c cell.Cell, workspace string) ([]string, error) {
	missing := missingOf(c)
	if len(missing) == 0 {
		return nil, nil
	}
	d, err := session.ReadDigest(session.Place{Dir: c.Root}.Transcript())
	if err != nil {
		return nil, err
	}
	if workspace != "" {
		d.Workspace = workspace
	}
	var built []string
	for _, ix := range missing {
		if err := ix.Build(c, d); err != nil {
			return built, err
		}
		built = append(built, ix.Name())
	}
	return built, nil
}

func missingOf(c cell.Cell) []Index {
	var out []Index
	for _, ix := range Indexes {
		if !ix.Present(c) {
			out = append(out, ix)
		}
	}
	return out
}
