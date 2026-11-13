package dirwatch

import (
	"testing"
	"time"
)

// R22 starts with a live legacy-vouching socket and no receipt. Neither its
// version frame nor pongs authorize suppressing an ordinary durable heartbeat.
func TestR22PongsCannotReplaceCommittedReceipt(t *testing.T) {
	r := newHolderRig(t, 4)
	w := r.holder.Take(hold{"chat", 7})
	defer w.Release()
	s := r.socket()
	s.send(Frame{Version: 1}, nil)
	for deadline := time.Now().Add(5 * time.Second); !w.feed.snapshot().Up; {
		if time.Now().After(deadline) {
			t.Fatal("socket did not open")
		}
		time.Sleep(time.Millisecond)
	}
	if w.Healthy() {
		t.Fatal("R22 socket-only renewal")
	}
	now := r.clock.Now().UnixMilli()
	w.feed.hear(Frame{Holds: []HoldReceipt{{Cell: "chat", Fence: 6, Index: 1, At: now, Until: now + 90000}}}, true)
	if w.Healthy() {
		t.Fatal("R22 wrong fence covered")
	}
	w.feed.hear(Frame{Holds: []HoldReceipt{{Cell: "chat", Fence: 7, Index: 1, At: now, Until: now + 90000}}}, true)
	if !w.Healthy() {
		t.Fatal("R22 committed receipt missing")
	}
	r.clock.mu.Lock()
	r.clock.now = r.clock.now.Add(61 * time.Second)
	r.clock.mu.Unlock()
	w.feed.hear(Frame{Pong: true}, true)
	if w.Healthy() {
		t.Fatal("R22 pongs renewed expiring receipt")
	}
	// A delayed old receipt cannot reset its absolute expiry.
	w.feed.hear(Frame{Holds: []HoldReceipt{{Cell: "chat", Fence: 7, Index: 1, At: now, Until: now + 90000}}}, true)
	if w.Healthy() {
		t.Fatal("R22 delayed receipt renewed expiry")
	}
}
