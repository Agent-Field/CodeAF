package directory

import (
	"errors"
	"reflect"
	"testing"
)

const (
	now   = int64(1_000_000)
	ttl   = int64(90_000)
	devA  = "dev_a"
	devB  = "dev_b"
	head1 = "h1"
	head2 = "h2"
)

// held is a cell devA holds at fence 4 until now+5s.
func held() Cell {
	return Cell{Head: head1, Title: "old", Lease: Lease{Device: devA, Fence: 4, Expires: now + 5000, Pending: 2}}
}

func expired() Cell {
	c := held()
	c.Lease.Expires = now - 1
	return c
}

func TestAcquire(t *testing.T) {
	tests := []struct {
		name string
		cell Cell
		by   string
		want Lease
		err  error
	}{
		{"refused while another holds", held(), devB, held().Lease, ErrLeaseHeld},
		{"refused at the last instant", withExpiry(held(), now+1), devB, Lease{}, ErrLeaseHeld},
		{"allowed at the exact expiry", withExpiry(held(), now), devB, Lease{Device: devB, Fence: 5, Expires: now + ttl}, nil},
		{"allowed after expiry", expired(), devB, Lease{Device: devB, Fence: 5, Expires: now + ttl}, nil},
		{"same device retakes its live lease", held(), devA, Lease{Device: devA, Fence: 5, Expires: now + ttl}, nil},
		{"pending resets", expired(), devA, Lease{Device: devA, Fence: 5, Expires: now + ttl}, nil},
		{"first ever acquire", Cell{}, devA, Lease{Device: devA, Fence: 1, Expires: now + ttl}, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Acquire(tc.cell, tc.by, now, false)
			if !errors.Is(err, tc.err) {
				t.Fatalf("err = %v, want %v", err, tc.err)
			}
			if tc.err == nil && got.Lease != tc.want {
				t.Fatalf("lease = %+v, want %+v", got.Lease, tc.want)
			}
			if tc.err != nil && got.Lease != tc.cell.Lease {
				t.Fatalf("a refusal changed the lease: %+v", got.Lease)
			}
		})
	}
}

// TestAcquireForce: a forced acquire displaces a live holder and raises the
// fence, so the displaced holder's next write is refused as stale.
func TestAcquireForce(t *testing.T) {
	taken, err := Acquire(held(), devB, now, true)
	if err != nil {
		t.Fatalf("forced acquire refused: %v", err)
	}
	if want := (Lease{Device: devB, Fence: 5, Expires: now + ttl}); taken.Lease != want {
		t.Fatalf("lease = %+v, want %+v", taken.Lease, want)
	}
	if _, err := Heartbeat(taken, devA, Beat{Fence: 4}, now); !errors.Is(err, ErrFenceStale) {
		t.Fatalf("displaced holder's heartbeat = %v, want ErrFenceStale", err)
	}
	free, err := Acquire(expired(), devB, now, true)
	plain, _ := Acquire(expired(), devB, now, false)
	if err != nil || free.Lease != plain.Lease {
		t.Fatalf("force on a free cell = %+v, %v; want the plain result %+v", free.Lease, err, plain.Lease)
	}
}

func withExpiry(c Cell, at int64) Cell { c.Lease.Expires = at; return c }

func TestHeartbeat(t *testing.T) {
	tests := []struct {
		name   string
		cell   Cell
		device string
		beat   Beat
		want   Lease
		err    error
	}{
		{"renews a live lease", held(), devA, Beat{Fence: 4, Pending: 7}, Lease{Device: devA, Fence: 4, Expires: now + ttl, Pending: 7}, nil},
		{"renews an expired lease nobody took", expired(), devA, Beat{Fence: 4}, Lease{Device: devA, Fence: 4, Expires: now + ttl}, nil},
		{"stale fence refused", held(), devA, Beat{Fence: 3}, Lease{}, ErrFenceStale},
		{"future fence refused", held(), devA, Beat{Fence: 5}, Lease{}, ErrFenceStale},
		{"other device refused", held(), devB, Beat{Fence: 4}, Lease{}, ErrFenceStale},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Heartbeat(tc.cell, tc.device, tc.beat, now)
			if !errors.Is(err, tc.err) {
				t.Fatalf("err = %v, want %v", err, tc.err)
			}
			if tc.err == nil && got.Lease != tc.want {
				t.Fatalf("lease = %+v, want %+v", got.Lease, tc.want)
			}
		})
	}
}

func TestPublishTo(t *testing.T) {
	ok := Publish{Fence: 4, OldHead: head1, Head: head2, Size: 9, Class: "chat", Title: "new", Pending: 1}
	tests := []struct {
		name   string
		device string
		p      Publish
		err    error
		check  func(*testing.T, Cell)
	}{
		{"moves the head", devA, ok, nil, func(t *testing.T, c Cell) {
			if c.Head != head2 || c.Size != 9 || c.Class != "chat" || c.Lease.Pending != 1 || c.DurableAt != now || c.Title != "new" || c.Lease.Expires != now+ttl {
				t.Fatalf("cell = %+v", c)
			}
		}},
		{"empty title keeps the old one", devA, with(ok, func(p *Publish) { p.Title = "" }), nil, func(t *testing.T, c Cell) {
			if c.Title != "old" {
				t.Fatalf("title = %q", c.Title)
			}
		}},
		{"stale fence refused", devA, with(ok, func(p *Publish) { p.Fence = 3 }), ErrFenceStale, nil},
		{"other device refused", devB, ok, ErrFenceStale, nil},
		{"moved head refused", devA, with(ok, func(p *Publish) { p.OldHead = "elsewhere" }), ErrHeadMoved, nil},
		{"fence is checked before the head", devB, with(ok, func(p *Publish) { p.OldHead = "elsewhere" }), ErrFenceStale, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := PublishTo(held(), tc.device, tc.p, now)
			if !errors.Is(err, tc.err) {
				t.Fatalf("err = %v, want %v", err, tc.err)
			}
			if tc.err != nil && got.Head != head1 {
				t.Fatalf("a refusal moved the head to %s", got.Head)
			}
			if tc.check != nil {
				tc.check(t, got)
			}
		})
	}
}

func with(p Publish, edit func(*Publish)) Publish { edit(&p); return p }

func TestReleaseOf(t *testing.T) {
	tests := []struct {
		name   string
		device string
		fence  uint64
		want   Lease
		err    error
	}{
		{"frees the lease and keeps the fence", devA, 4, Lease{Device: devA, Fence: 4}, nil},
		{"stale fence refused", devA, 3, Lease{}, ErrFenceStale},
		{"other device refused", devB, 4, Lease{}, ErrFenceStale},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ReleaseOf(held(), tc.device, tc.fence)
			if !errors.Is(err, tc.err) {
				t.Fatalf("err = %v, want %v", err, tc.err)
			}
			if tc.err == nil && got.Lease != tc.want {
				t.Fatalf("lease = %+v, want %+v", got.Lease, tc.want)
			}
		})
	}
}

func TestCreated(t *testing.T) {
	in := CellInit{Head: head1, Class: "chat", Title: "t", ParentCell: "p", Size: 3, OrphanTurns: 2}
	got := Created(in, devA, now)
	want := Lease{Device: devA, Fence: 1, Expires: now + ttl}
	if got.Lease != want || got.DurableAt != now || got.Head != head1 || got.V != 1 ||
		got.ParentCell != "p" || got.Title != "t" || got.OrphanTurns != 2 || got.Size != 3 {
		t.Fatalf("cell = %+v", got)
	}
}

// Lifted raises a live lease's expiry to the vouched time and does nothing
// else: a released lease stays released, an earlier vouch changes nothing, and
// the rest of the cell is untouched.
func TestLiftedRaisesOnlyALiveLease(t *testing.T) {
	held := Cell{Head: "h", Lease: Lease{Device: "d", Fence: 3, Expires: 1000, Pending: 2}}
	for _, tc := range []struct {
		name    string
		in      Cell
		vouched int64
		want    int64
	}{
		{"later vouch lifts", held, 5000, 5000},
		{"no evidence", held, 0, 1000},
		{"earlier vouch", held, 400, 1000},
		{"equal vouch", held, 1000, 1000},
		{"released stays released", Cell{Lease: Lease{Device: "d", Fence: 3}}, 5000, 0},
	} {
		got := Lifted(tc.in, tc.vouched)
		if got.Lease.Expires != tc.want {
			t.Errorf("%s: expires %d, want %d", tc.name, got.Lease.Expires, tc.want)
		}
		want := tc.in
		want.Lease.Expires = tc.want
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s: lifted changed more than the expiry: %+v", tc.name, got)
		}
	}
}

// The lifted cell is what Acquire reads: a vouched lease refuses another device
// after its stored expiry, and an unvouched one does not.
func TestAcquireReadsTheLiftedLease(t *testing.T) {
	c := Cell{Lease: Lease{Device: "a", Fence: 1, Expires: 1000}}
	if _, err := Acquire(Lifted(c, 5000), "b", 2000, false); err != ErrLeaseHeld {
		t.Fatalf("acquire over a vouched lease: %v, want lease_held", err)
	}
	if _, err := Acquire(Lifted(c, 0), "b", 2000, false); err != nil {
		t.Fatalf("acquire over a lapsed lease: %v", err)
	}
}
