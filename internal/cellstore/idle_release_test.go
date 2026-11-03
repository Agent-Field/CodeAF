package cellstore

import (
	"testing"
	"time"
)

func TestIdleRelease_C2_TailWritesAndSessionActivityRetainHold(t *testing.T) {
	entered, finish := make(chan struct{}), make(chan struct{})
	w := &SealWatch{Quiet: time.Millisecond, OnTail: func() { close(entered); <-finish }}
	w.Waiting()
	if w.IdleReady() {
		t.Fatal("a watch without its session certified idle")
	}
	w.SetIdleCheck(func() bool { return true })
	if !w.IdleReady() {
		t.Fatal("a settled session was not idle")
	}
	w.TurnStarted()
	if w.IdleReady() {
		t.Fatal("a running turn certified idle")
	}
	w.TurnEnded()
	w.Wrote()
	if w.IdleReady() {
		t.Fatal("an unsaved tail certified idle")
	}
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("tail did not start")
	}
	if w.IdleReady() {
		t.Fatal("an in-flight tail seal certified idle")
	}
	close(finish)
	w.SetIdleCheck(func() bool { return false })
	if w.IdleReady() {
		t.Fatal("a running task or job certified idle")
	}
}
