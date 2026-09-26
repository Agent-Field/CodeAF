//go:build !windows

package app

import (
	"context"
)

type countingWriter int64

func (writer *countingWriter) Write(value []byte) (int, error) {
	*writer += countingWriter(len(value))
	return len(value), nil
}

// emitPatchSummary records the shape of the run's final diff against the base
// commit -- files, line counts, binaries, patch bytes, untracked files -- on
// the event stream. It is observational only: nothing in the run acts on it.
func (runner *pipeline) emitPatchSummary(baseSHA string) {
	ctx, cancel := context.WithTimeout(context.Background(), summaryTimeout)
	defer cancel()
	data, status := runner.recorder.Summary(ctx, baseSHA)
	runner.events.stage("patch-summary", status, data)
}
