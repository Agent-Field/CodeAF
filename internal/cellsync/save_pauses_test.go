package cellsync

import (
	"context"
	"testing"
)

func TestSavePausesDisplacementSettlesBeforeChoosingItsHead(t *testing.T) {
	r := newRig(t)
	b := r.batcher()
	r.publishFirst(map[string]string{"a": "shared"})
	r.takeOver()
	var kept string
	b.BeforeResolve = func(context.Context) error {
		kept = r.seal(map[string]string{"a": "finished but held"})
		note(b, kept)
		return nil
	}
	b.Displaced(t.Context())
	id := r.branch.Resolve(cellID)
	if kept == "" || id == cellID || r.head(id).Head != kept || r.head(id).OrphanTurns != 1 {
		t.Fatalf("displacement chose the head before settling: kept=%q branch=%q", kept, id)
	}
}
