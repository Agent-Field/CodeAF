package session

import (
	"errors"
	"io/fs"
	"strings"
	"time"
)

// These hooks let tests control write ordering and bounded lookup progress.
var writtenBeforeRecord = func() {}
var writtenRead = func(n int) {}
var writtenDeadline = func() time.Time { return time.Now().Add(2 * time.Second) }

// recordWrittenArtifact updates the latest deliverable for this session,
// path and source. The ledger lock makes identity selection and append atomic,
// after the mutation queue has serialized writes through directory aliases.
func recordWrittenArtifact(index string, artifact Artifact) (Artifact, bool) {
	writtenBeforeRecord()
	artifactsMu.Lock()
	defer artifactsMu.Unlock()
	previous, err := latestWrittenArtifact(index, artifact)
	if err != nil {
		return Artifact{}, false
	}
	if previous.Deliver != nil {
		artifact.Created = previous.Created
		artifact.Deliver.ID = previous.Deliver.ID
		artifact.Deliver.Update = true
		artifact.Deliver.MediaUpdate = artifact.Media != nil
		artifact.Deliver.State = previous.Deliver.State
		artifact.Deliver.Kept = previous.Deliver.Kept
		if file, prior := artifact.Deliver.File, previous.Deliver.File; artifact.Kind == shareKindFile && file != nil && prior != nil && prior.MIME != "" {
			if file.MIME == "application/octet-stream" || strings.HasPrefix(file.MIME, "text/plain") {
				file.MIME = prior.MIME
			}
		}
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
