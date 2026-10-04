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

// Refreshable is an index that can also say it is BEHIND: it holds rows, but
// fewer than the transcript and the receipts now say. A copy of a chat this
// machine already had is taken forward by a handoff, and the indexes it kept
// from the older copy are then present and wrong. Behind is asked only with the
// digest already read, never on an ordinary open.
type Refreshable interface {
	Index
	Behind(c cell.Cell, d session.Digest) bool
}

// Indexes is the registry, in build order.
var Indexes = []Index{metaIndex{}, usageIndex{}, taskIndex{}, memoryIndex{}, artifactIndex{}}

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
	d, err := digestOf(c, workspace)
	if err != nil {
		return nil, err
	}
	return build(c, d, missing)
}

// Refresh is [Rebuild] for a chat a handoff has just moved forward: it reads
// the transcript once and builds every index that is missing or behind it. A
// behind index brings in only the rows it lacks, so the ledgers never double.
func Refresh(c cell.Cell, workspace string) ([]string, error) {
	d, err := digestOf(c, workspace)
	if err != nil {
		return nil, err
	}
	var due []Index
	for _, ix := range Indexes {
		if !ix.Present(c) || behind(ix, c, d) {
			due = append(due, ix)
		}
	}
	return build(c, d, due)
}

func behind(ix Index, c cell.Cell, d session.Digest) bool {
	r, ok := ix.(Refreshable)
	return ok && r.Behind(c, d)
}

// build runs the due indexes over the digest.
func build(c cell.Cell, d session.Digest, due []Index) ([]string, error) {
	var built []string
	for _, ix := range due {
		if err := ix.Build(c, d); err != nil {
			return built, err
		}
		built = append(built, ix.Name())
	}
	return built, nil
}

func digestOf(c cell.Cell, workspace string) (session.Digest, error) {
	d, err := session.ReadDigest(session.Place{Dir: c.Root}.Transcript())
	if err == nil && workspace != "" {
		d.Workspace = workspace
	}
	return d, err
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
