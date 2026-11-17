package clusterstate

import "testing"

// A beat says whether a turn is running on the holder; a publish keeps it,
// because a turn publishes as it goes, and a release or an acquire clears it.
func TestLeaseKeepsWorkingUntilTheNextBeatReleaseOrAcquire(t *testing.T) {
	c := Cell{Head: "h", Lease: Lease{Device: "dev_a", Fence: 2, Expires: 10}}
	c, err := Heartbeat(c, "dev_a", Beat{Fence: 2, Working: true}, 5)
	if err != nil || !c.Lease.Working {
		t.Fatalf("lease after a working beat = %+v, %v", c.Lease, err)
	}
	published, err := PublishTo(c, "dev_a", Publish{Fence: 2, OldHead: "h", Head: "h2"}, 6)
	if err != nil || !published.Lease.Working {
		t.Fatalf("a publish mid-turn dropped working: %+v, %v", published.Lease, err)
	}
	ended, err := Heartbeat(published, "dev_a", Beat{Fence: 2}, 7)
	if err != nil || ended.Lease.Working {
		t.Fatalf("the turn ended, yet the lease says working: %+v, %v", ended.Lease, err)
	}
	released, err := ReleaseOf(c, "dev_a", 2)
	if err != nil || released.Lease.Working {
		t.Fatalf("a release kept working: %+v, %v", released.Lease, err)
	}
	taken, err := Acquire(c, "dev_b", 20, true)
	if err != nil || taken.Lease.Working {
		t.Fatalf("an acquire kept working: %+v, %v", taken.Lease, err)
	}
}
