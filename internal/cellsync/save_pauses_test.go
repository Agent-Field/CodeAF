package cellsync

import (
	"context"
	"errors"
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

func TestSavePausesDisplacementDoesNotAcknowledgeAFailedSettle(t *testing.T) {
	r := newRig(t)
	b := r.batcher()
	r.publishFirst(map[string]string{"a": "shared"})
	note(b, r.seal(map[string]string{"a": "older pending head"}))
	r.takeOver()
	fault := errors.New("latest calls could not be saved")
	b.BeforeResolve = func(context.Context) error { return fault }
	announced := false
	b.OnSuperseded = func(Superseded) { announced = true }
	b.Displaced(t.Context())
	if !errors.Is(b.lastFlushErr, fault) || announced || r.branch.Resolve(cellID) != cellID || b.Pending() != 1 {
		t.Fatalf("failed settle acknowledged older work: err=%v announced=%v pending=%d", b.lastFlushErr, announced, b.Pending())
	}
}
