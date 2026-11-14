package syncsetup

import (
	"sync"
	"testing"
	"time"
)

func TestIdleRelease_R6_LocalVaultContentionKeepsQuietCadence(t *testing.T) {
	h := newTwoHomes(t)
	c := idleContractChat(t, h)
	d := c.drive
	d.SetIdleCheck(func() bool { return true })
	settleIdleFixture(t, h, d)
	proof, err := h.engine.SavedState(t.Context(), h.cell, d.profileFiles()...)
	mustNil(t, err)
	if proof == nil || len(proof.Changes) != 0 {
		t.Fatalf("fixture has unsaved changes: %+v", proof)
	}
	held, resume := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(resume) }) }
	defer unblock()
	go func() {
		d.vaultMu.Lock()
		defer d.vaultMu.Unlock()
		close(held)
		<-resume
	}()
	<-held
	d.Idle()
	// A competing ordinary flush spans the first quiet attempt. Once it ends,
	// release should use another quiet tick, rather than an outage backoff.
	time.Sleep(3 * time.Second)
	unblock()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		v, err := h.a.Dir.Cell(t.Context(), h.cell.ID)
		if err == nil && v.Cell.Lease.Expires == 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("a completed local flush delayed idle release beyond the next quiet tick")
}
