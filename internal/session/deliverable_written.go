package session

import (
	"errors"
	"io/fs"
	"time"
)

// These hooks let tests control lookup and lock contention without sleeps.
var writtenAfterLookup = func() {}
var writtenWaiting = func() {}
var writtenRead = func(n int) {}
var writtenDeadline = func() time.Time { return time.Now().Add(2 * time.Second) }

// recordWrittenArtifact updates the latest deliverable for this session,
// path and source. The ledger lock makes identity selection and append atomic,
// including writers that entered different file mutation queues through aliases.
func recordWrittenArtifact(index string, artifact Artifact) (Artifact, bool) {
	if !artifactsMu.TryLock() {
		writtenWaiting()
		artifactsMu.Lock()
	}
	defer artifactsMu.Unlock()
	previous, err := latestWrittenArtifact(index, artifact)
	if err != nil {
		return Artifact{}, false
	}
	writtenAfterLookup()
	if previous.Deliver != nil {
		artifact.Created = previous.Created
		artifact.Deliver.ID = previous.Deliver.ID
		artifact.Deliver.Update = true
		artifact.Deliver.MediaUpdate = artifact.Media != nil
		artifact.Deliver.State = previous.Deliver.State
		artifact.Deliver.Kept = previous.Deliver.Kept
	}
	return recordArtifactRowLocked(index, artifact)
}

func latestWrittenArtifact(index string, artifact Artifact) (Artifact, error) {
	file, now, err := openIndex(index)
	if errors.Is(err, fs.ErrNotExist) {
		return Artifact{}, nil
	}
	if err != nil {
		return Artifact{}, err
	}
	defer file.Close()
	paths, release, err := writtenIndex(index, file, now)
	if err != nil {
		return Artifact{}, err
	}
	defer release()
	return paths.latest(file, artifact)
}

// sameWrittenSource distinguishes checkout identities from unmerged branches.
// A settled row retains its branch provenance, but its canonical file identifies
// the checkout source that later owner writes refresh.
func sameWrittenSource(previous, next DeliverableFact) bool {
	if next.File != nil && next.File.Canonical != "" {
		return previous.Branch == "" || (previous.File != nil && previous.File.Canonical == next.File.Canonical)
	}
	return previous.Branch == next.Branch && (previous.File == nil || previous.File.Canonical == "")
}
