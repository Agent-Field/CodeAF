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
	f := NewFeed()
	sub, err := f.Subscribe()
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
	f := NewFeed()
	f.SetMaxWatchers(1)
	first, err := f.Subscribe()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Subscribe(); err != ErrTooManyWatchers {
		t.Fatalf("second subscribe = %v, want ErrTooManyWatchers", err)
	}
	first.Close()
	if _, err := f.Subscribe(); err != nil {
		t.Fatalf("after a close: %v", err)
	}
}
