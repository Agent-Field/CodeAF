// artifacts.go is the global deliverables index (the implementation, Decision
// 26): one append-only JSONL file under the v3 home, one row per thing the
// harness produced FOR the person — a generated image, an export, a finished
// document. It is a citation, not an archive, in exactly the sense
// task_index.go established: the smallest row that lets a person find the work
// again, from any directory, by title and recency.
//
// It is GLOBAL where tasks.jsonl is per-project, because the question it
// answers — "where is that report from Tuesday" — is a cross-project question.
// Droppings (logs, stubs) are never recorded here; a row in this file
// is a claim that a person might want the path back.
package session

import (
	"bufio"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ArtifactsIndexName is the file, under the v3 home directory
// (~/.codeaf/v3/artifacts.jsonl). The caller hands the full path in, for
// SessionFile's reason: where a person's state lives is the surface's
// decision.
const ArtifactsIndexName = "artifacts.jsonl"

// artifactRows caps one read, newest kept, same law as the task index.
const artifactRows = 2000

// Artifact is one deliverable as the index remembers it.
type Artifact struct {
	// Path is where the deliverable landed, absolute. The row does not
	// promise the file still exists — a picker verifies before it offers.
	Path string `json:"path"`
	// Session is the 16-hex id of the conversation that produced it.
	Session string `json:"session,omitempty"`
	// Title is what a picker row says: the export's name, the image's prompt
	// slug, the document's first heading.
	Title string `json:"title"`
	// Kind names what it is — "image", "export", "document" — a word for a
	// glyph, not a taxonomy.
	Kind string `json:"kind,omitempty"`
	// Created is when it landed; every ordering in this file is on it.
	Created time.Time `json:"created"`
	// Media is the structured fact of a media file (mediafact.go). Optional
	// and additive: a row without it is a legacy row and keeps listing.
	Media *MediaFact `json:"media,omitempty"`
	// Deliver is the typed deliverable fact (deliverable.go). Optional and
	// additive, like Media: a row without it is a legacy row, whose turn, task,
	// inputs and state are UNKNOWN and are never inferred.
	Deliver *DeliverableFact `json:"deliver,omitempty"`
}

// artifactsMu serializes this process's appends; two processes are serialized
// by O_APPEND, which is what makes an append-only file the right shape.
var artifactsMu sync.Mutex

// RecordArtifact writes one row. Every failure is silence, for
// appendTaskIndex's reason: the caller has just produced real work, and there
// is nothing it could usefully do with the news that a lookup file could not
// be written. A row with no path or no title is not a citation and is not
// written. It answers whether the row was written, for the caller that must not
// hand out an id for a row that is not there.
func RecordArtifact(path string, artifact Artifact) bool {
	_, ok := recordArtifactRow(path, artifact)
	return ok
}

// recordArtifactRow is [RecordArtifact] that answers the row as it was written.
func recordArtifactRow(path string, artifact Artifact) (Artifact, bool) {
	artifactsMu.Lock()
	defer artifactsMu.Unlock()
	return recordArtifactRowLocked(path, artifact)
}

// recordArtifactRowLocked appends while the caller holds artifactsMu.
func recordArtifactRowLocked(path string, artifact Artifact) (Artifact, bool) {
	if strings.TrimSpace(path) == "" {
		return Artifact{}, false
	}
	if strings.TrimSpace(artifact.Path) == "" || strings.TrimSpace(artifact.Title) == "" {
		return Artifact{}, false
	}
	line, err := json.Marshal(artifact)
	if err != nil {
		return Artifact{}, false
	}
	if artifact.Deliver != nil && (!deliverRowFits(line) || !ValidDeliverableID(artifact.Deliver.ID)) {
		// A deliverable row past the bound, or under an id the readers refuse,
		// is one they must never have to skip. An update cannot be shortened
		// without changing what it says, so it is refused; a creation is
		// written as the plain row it would have been, whose provenance is then
		// honestly unknown.
		if artifact.Deliver.Update {
			return Artifact{}, false
		}
		artifact.Deliver = nil
		if line, err = json.Marshal(artifact); err != nil {
			return Artifact{}, false
		}
	}
	if directory := filepath.Dir(path); directory != "" && directory != "." {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			return Artifact{}, false
		}
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return Artifact{}, false
	}
	defer file.Close()
	// ONE write, so O_APPEND's atomic offset covers the whole row.
	_, err = file.Write(append(line, '\n'))
	return artifact, err == nil
}

// ReadArtifacts reads the rows at path, NEWEST FIRST, and tolerates
// everything: a missing file is a person who has never made anything and
// answers nil; a line that does not parse, or parses into a row with no path
// or title, is skipped.
func ReadArtifacts(path string) []Artifact {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	file, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) || err != nil {
		return nil
	}
	defer file.Close()
	var rows []Artifact
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		var row Artifact
		if json.Unmarshal(scanner.Bytes(), &row) != nil {
			continue
		}
		if strings.TrimSpace(row.Path) == "" || strings.TrimSpace(row.Title) == "" {
			continue
		}
		rows = append(rows, row)
	}
	if len(rows) > artifactRows {
		rows = rows[len(rows)-artifactRows:]
	}
	for i, j := 0, len(rows)-1; i < j; i, j = i+1, j-1 {
		rows[i], rows[j] = rows[j], rows[i]
	}
	return rows
}

// ArtifactsSince is the deliverables at path that landed AFTER since, newest
// first — the files made while a person was away, read against the look stamp
// ([LastLook]). path is the index file itself, exactly as [ReadArtifacts]
// takes it.
//
// A ZERO STAMP ANSWERS NOTHING, never everything. It is a machine with no origin
// yet, and look.go's rule for that is that the first look marks nothing as news
// — a ledger that called every file ever made "since you left" would be the one
// screen on the machine telling a person something false about their absence.
func ArtifactsSince(path string, since time.Time) []Artifact {
	if since.IsZero() {
		return nil
	}
	var out []Artifact
	for _, row := range ReadArtifacts(path) {
		if row.Created.After(since) {
			out = append(out, row)
		}
	}
	return out
}
