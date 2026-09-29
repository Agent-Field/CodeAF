package session

// artifactcell.go keeps a chat's deliverables in its cell. The files live in
// .cell/artifacts/ and .cell/artifacts.jsonl is their ledger: one row per
// deliverable whose path is spelled by the path codec, so both travel with the
// sealed folder. The machine-wide artifacts.jsonl is then only an index of
// every cell on this machine and is made again from the ledgers
// (internal/cellindex), never the other way round.

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

const placeArtifactLedger = "artifacts.jsonl"

// ArtifactLedger is the cell's own list of what it produced.
func (p Place) ArtifactLedger() string { return p.truth(placeArtifactLedger) }

// RecordSealedArtifact cites a deliverable in the cell's ledger. A file under
// no folder the codec knows has no spelling that survives a move, so it is not
// cited: the machine-wide index still names it for as long as it exists here.
func RecordSealedArtifact(place Place, artifact Artifact) {
	ref, ok := codecFor(place.Dir, place.Workspace).encode(artifact.Path)
	if !ok {
		return
	}
	artifact.Path = ref
	RecordArtifact(place.ArtifactLedger(), artifact)
}

// SealedArtifacts is what the cell at dir cites, NEWEST FIRST, with every path
// resolved for this machine. A row whose file is gone (renamed, deleted, or
// never carried) is dropped, because a deliverable that cannot be opened is not
// one; a path written twice keeps only its latest row, which is how an
// overwritten file is cited once.
func SealedArtifacts(dir string) []Artifact {
	meta, _ := LoadMeta(dir)
	codec := codecOf(dir, meta)
	seen := map[string]bool{}
	var out []Artifact
	for _, row := range ReadArtifacts(truthPath(dir, placeArtifactLedger)) {
		path, ok := codec.resolve(row.Path)
		if !ok || seen[path] || !isFile(path) {
			continue
		}
		seen[path] = true
		row.Path = path
		out = append(out, row)
	}
	return out
}

// AdoptArtifacts is the one seam that brings a chat made before artifacts
// travelled into the cell. Its files were moved into .cell/artifacts/ with the
// rest of the truth (TruthCarriers); its rows are in the machine-wide index
// with the old absolute paths. Each such row whose file still exists, here or
// at its new home, is cited in the ledger; a row whose file is missing has
// nothing to cite and is left behind. It is safe to run on every open: a path
// the ledger already cites is not cited twice.
func AdoptArtifacts(place Place, index string) {
	held := citedPaths(SealedArtifacts(place.Dir))
	rows := ReadArtifacts(index)
	for i := len(rows) - 1; i >= 0; i-- { // the index is newest first; the ledger grows oldest first
		row := rows[i]
		if row.Session != place.ID() {
			continue
		}
		row.Path = movedArtifact(place, row.Path)
		if isFile(row.Path) && !held[row.Path] {
			RecordSealedArtifact(place, row)
		}
	}
}

// movedArtifact is where the file an old row names is now: a file the old
// artifacts/ folder held is in .cell/artifacts/, and any other path stands.
func movedArtifact(place Place, path string) string {
	old := filepath.Join(place.Dir, placeArtifacts) + string(filepath.Separator)
	if rest, ok := strings.CutPrefix(path, old); ok {
		return filepath.Join(place.Artifacts(), rest)
	}
	return path
}

func citedPaths(rows []Artifact) map[string]bool {
	held := make(map[string]bool, len(rows))
	for _, r := range rows {
		held[r.Path] = true
	}
	return held
}

func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

// recordArtifact is the one door every tool that makes a deliverable goes
// through: the cell's ledger is the truth and travels, and the machine-wide
// index is what this machine's picker reads. Two doors would let the picker
// show a file the ledger forgot, which is the file a move then loses.
func (a *Agent) recordArtifact(path, title, kind string) {
	artifact := Artifact{Path: path, Session: a.journalID(), Title: title, Kind: kind, Created: time.Now()}
	RecordSealedArtifact(a.config.Place, artifact)
	RecordArtifact(a.config.ArtifactsIndex, artifact)
}
