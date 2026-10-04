package cellindex

import (
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/session"
)

// artifactIndex is this session's share of the machine's deliverables index:
// one row per file the cell's own ledger (.cell/artifacts.jsonl) cites. The
// ledger and the files beside it are the truth and travel with the chat; this
// index is what the picker on THIS machine reads, with the paths made absolute
// here.
type artifactIndex struct{}

func (artifactIndex) Name() string { return "artifacts.jsonl" }

// Present reports whether the machine's index already names every file the
// ledger cites. A session that cites nothing owes the index nothing.
func (artifactIndex) Present(c cell.Cell) bool {
	return len(unindexed(c)) == 0
}

func (artifactIndex) Build(c cell.Cell, _ session.Digest) error {
	rows := unindexed(c)
	for i := len(rows) - 1; i >= 0; i-- { // the ledger reads newest first; the index grows oldest first
		session.RecordArtifact(artifactsIndexPath(), rows[i])
	}
	return nil
}

// unindexed is the cited deliverables the machine's index has no row for, newest first.
func unindexed(c cell.Cell) []session.Artifact {
	held := map[string]bool{}
	for _, row := range session.ReadArtifacts(artifactsIndexPath()) {
		held[row.Path] = true
	}
	var missing []session.Artifact
	for _, row := range session.SealedArtifacts(c.Root) {
		if !held[row.Path] {
			row.Session = c.ID
			missing = append(missing, row)
		}
	}
	return missing
}

func artifactsIndexPath() string { return home.Join("v3", session.ArtifactsIndexName) }
