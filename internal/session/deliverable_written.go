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

// recordWrittenArtifact refreshes only authored identities. A shared-only path
// gets a new authored identity so an unsettled share can still reconcile its plan.
func recordWrittenArtifact(index string, artifact Artifact) (Artifact, bool) {
	writtenBeforeRecord()
	return mutateDeliverable(index, DeliverableRow{Artifact: artifact}, func(latest *Artifact) {
		if latest.Deliver == nil || !latest.Deliver.Authored {
			*latest = artifact
			return
		}
		fact := *latest.Deliver
		written := artifact.Deliver
		fact.Update, fact.MediaUpdate = true, artifact.Media != nil
		fact.At, fact.Turn, fact.Run, fact.Call = written.At, written.Turn, written.Run, written.Call
		fact.From, fact.Origin, fact.Authored = written.From, written.Origin, written.Authored
		fact.Version = written.Version
		file := written.File
		if prior := fact.File; artifact.Kind == shareKindFile && file != nil && prior != nil && prior.MIME != "" {
			if file.MIME == "application/octet-stream" || strings.HasPrefix(file.MIME, "text/plain") {
				file.MIME = prior.MIME
			}
		}
		fact.File = file
		latest.Title, latest.Kind, latest.Media = artifact.Title, artifact.Kind, artifact.Media
		latest.Deliver = &fact
	})
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
