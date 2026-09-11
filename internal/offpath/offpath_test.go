package offpath

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// THE LAW OF A READING: a settle that cannot have the fact yet costs the caller
// its bound and no more, and the SAME reading answers when it arrives. A
// reading that was thrown away on a missed settle would turn one slow gathering
// into one slow gathering per batch forever.
func TestAReadingThatIsNotReadyCostsItsBoundAndIsStillThere(t *testing.T) {
	release := make(chan struct{})
	reading := Take(func() string {
		<-release
		return "gathered"
	})

	began := time.Now()
	if _, ok := reading.Settle(20 * time.Millisecond); ok {
		t.Fatal("a gathering that has not finished settled anyway")
	}
	if waited := time.Since(began); waited > 500*time.Millisecond {
		t.Fatalf("a settle bounded at 20ms waited %s", waited)
	}

	close(release)
	value, ok := reading.Settle(2 * time.Second)
	if !ok || value != "gathered" {
		t.Fatalf("the same reading answered %q/%v, want the fact it was still gathering", value, ok)
	}
}

// A reading that is already in is free, and a zero bound is what asks for
// exactly that: take it if it is here, never wait.
func TestAReadingAlreadyInCostsNothing(t *testing.T) {
	reading := Take(func() int { return 7 })
	deadline := time.Now().Add(2 * time.Second)
	for {
		if value, ok := reading.Settle(0); ok {
			if value != 7 {
				t.Fatalf("settled %d, want 7", value)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("a gathering of one integer never arrived")
		}
		time.Sleep(time.Millisecond)
	}
}

// THE LAW OF A DEFERRED WRITE: Owe returns without writing, and Settle is the
// one door that says it landed.
func TestADeferredWriteDoesNotWriteOnThePath(t *testing.T) {
	var writes atomic.Int64
	let := make(chan struct{})
	write := Deferred(func() {
		<-let
		writes.Add(1)
	})

	began := time.Now()
	write.Owe()
	if waited := time.Since(began); waited > 500*time.Millisecond {
		t.Fatalf("Owe took %s — it is not allowed to wait for the write", waited)
	}
	if performed := writes.Load(); performed != 0 {
		t.Fatalf("%d writes happened on the path, want none", performed)
	}

	close(let)
	write.Settle()
	if performed := writes.Load(); performed == 0 {
		t.Fatal("a settled write never landed")
	}
}

// AND MANY OWES COLLAPSE INTO ONE FURTHER WRITE. What is written is the current
// state of the thing behind it, not a log of changes, so a hundred owes during
// one write owe exactly one more.
func TestOwesDuringAWriteCollapseIntoOne(t *testing.T) {
	var writes atomic.Int64
	inside := make(chan struct{})
	let := make(chan struct{})
	var once sync.Once
	var write *Write
	write = Deferred(func() {
		writes.Add(1)
		once.Do(func() {
			close(inside)
			<-let
			for range 100 {
				write.Owe()
			}
		})
	})

	write.Owe()
	<-inside
	close(let)
	write.Settle()
	if performed := writes.Load(); performed != 2 {
		t.Fatalf("a hundred owes during one write produced %d writes, want 2 — one running plus one more", performed)
	}
}

// Settle on a write nobody owed returns at once rather than waiting for a
// goroutine that was never started.
func TestSettleWithNothingOwedReturnsAtOnce(t *testing.T) {
	write := Deferred(func() { t.Fatal("nothing was owed and something was written") })
	began := time.Now()
	write.Settle()
	if waited := time.Since(began); waited > time.Second {
		t.Fatalf("settling an idle write waited %s", waited)
	}
}
