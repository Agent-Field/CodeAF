package directory

import (
	"context"
	"testing"
	"time"
)

// A heartbeat that only moves the lease's expiry is not a change a person sees;
// losing or gaining the lease, a new fence or a new pending count is.
func TestDiffersIgnoresOnlyTheExpiry(t *testing.T) {
	const now = int64(1000)
	held := Cell{Lease: Lease{Device: "d", Fence: 1, Expires: now + 500}}
	later := held
	later.Lease.Expires = now + 900
	if differs(held, later, now) {
		t.Fatal("an expiry moved later counted as a visible change")
	}
	for name, next := range map[string]Cell{
		"release":  {Lease: Lease{Device: "d", Fence: 1}},
		"acquire":  {Lease: Lease{Device: "d", Fence: 2, Expires: now + 500}},
		"pending":  {Lease: Lease{Device: "d", Fence: 1, Expires: now + 500, Pending: 1}},
		"archived": {Lease: held.Lease, Archived: true},
	} {
		if !differs(held, next, now) {
			t.Errorf("%s was not counted as a visible change", name)
		}
	}
}

// A watcher is told the current version at once, then only newer ones, and a
// slow one skips to the newest instead of queueing.
func TestSubHearsNewestOnly(t *testing.T) {
	f := NewFeed(time.Now)
	sub, err := f.Subscribe("d", nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	want := func(v uint64) {
		t.Helper()
		st, err := sub.Next(ctx)
		if err != nil || st.Version != v {
			t.Fatalf("Next = %d, %v; want %d", st.Version, err, v)
		}
	}
	want(0)
	f.Publish(Status{Version: 1})
	f.Publish(Status{Version: 2})
	f.Publish(Status{Version: 1}) // a commit that finished late must not lower it
	want(2)
}

func TestFeedCapFreesOnClose(t *testing.T) {
	f := NewFeed(time.Now)
	f.SetMaxWatchers(1)
	first, err := f.Subscribe("d", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Subscribe("d", nil); err != ErrTooManyWatchers {
		t.Fatalf("second subscribe = %v, want ErrTooManyWatchers", err)
	}
	first.Close()
	if _, err := f.Subscribe("d", nil); err != nil {
		t.Fatalf("after a close: %v", err)
	}
}

// fakeNow is a clock a test moves by hand.
type fakeNow struct{ ms int64 }

func (f *fakeNow) now() time.Time { return time.UnixMilli(f.ms) }

// A socket vouches from its accept time, a ping moves it on, only the device
// that opened it and only the exact (cell, fence) it named count, and a closed
// socket vouches for nothing.
func TestVouchedUntilIsNewestSignOfLifeOfMatchingSockets(t *testing.T) {
	clk := &fakeNow{ms: 1000}
	f := NewFeed(clk.now)
	if got := f.VouchedUntil("d", "c", 1); got != 0 {
		t.Fatalf("no sockets vouched until %d", got)
	}
	first, _ := f.Subscribe("d", []Hold{{"c", 1}})
	if got, want := f.VouchedUntil("d", "c", 1), 1000+ttlMs; got != want {
		t.Fatalf("accept time: vouched until %d, want %d", got, want)
	}
	clk.ms = 5000
	second, _ := f.Subscribe("d", []Hold{{"c", 1}})
	first.Ping()
	clk.ms = 7000
	second.Ping()
	if got, want := f.VouchedUntil("d", "c", 1), 7000+ttlMs; got != want {
		t.Fatalf("newest of two sockets: %d, want %d", got, want)
	}
	for name, key := range map[string][3]any{
		"other device": {"e", "c", uint64(1)}, "other cell": {"d", "x", uint64(1)}, "other fence": {"d", "c", uint64(2)},
	} {
		if got := f.VouchedUntil(key[0].(string), key[1].(string), key[2].(uint64)); got != 0 {
			t.Errorf("%s vouched until %d", name, got)
		}
	}
	second.Close()
	if got, want := f.VouchedUntil("d", "c", 1), 5000+ttlMs; got != want {
		t.Fatalf("after the newer socket closed: %d, want %d", got, want)
	}
	first.Close()
	if got := f.VouchedUntil("d", "c", 1); got != 0 {
		t.Fatalf("after every socket closed: vouched until %d", got)
	}
}

// Evidence is not a change: a lease lifted by a socket and the same lease with
// a later stored expiry look the same, and only held-ness counts.
func TestEvidenceAloneIsNotAVisibleChange(t *testing.T) {
	clk := &fakeNow{ms: 1000}
	f := NewFeed(clk.now)
	s, _ := f.Subscribe("d", []Hold{{"c", 1}})
	stored := Cell{Lease: Lease{Device: "d", Fence: 1, Expires: 1000 + ttlMs}}
	clk.ms = 50_000
	s.Ping()
	clk.ms = 1000 + ttlMs + 10 // past the stored expiry, inside the socket's vouch
	later := stored
	later.Lease.Expires = clk.ms + ttlMs
	if f.differs("c", stored, later, clk.ms) {
		t.Fatal("moving the stored expiry of a vouched lease counted as a change")
	}
	if !f.differs("other", stored, later, clk.ms) {
		t.Fatal("a lease past its stored expiry that a renewal makes held must count when nothing vouches as a change")
	}
}
