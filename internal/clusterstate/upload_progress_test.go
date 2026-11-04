package clusterstate

import "testing"

// A beat's upload progress lives on the lease while turns are pending, and a
// publish, a release or an acquire clears it.
func TestLeaseKeepsUploadProgressOnlyWhilePending(t *testing.T) {
	c := Cell{Head: "h", Lease: Lease{Device: "dev_a", Fence: 2, Expires: 10}}
	c, err := Heartbeat(c, "dev_a", Beat{Fence: 2, Pending: 1, UploadSent: 412, UploadTotal: 1433}, 5)
	if err != nil {
		t.Fatal(err)
	}
	if c.Lease.UploadSent != 412 || c.Lease.UploadTotal != 1433 {
		t.Fatalf("lease after a beat = %+v, want 412 of 1433", c.Lease)
	}
	over, err := Heartbeat(c, "dev_a", Beat{Fence: 2, Pending: 1, UploadSent: 2000, UploadTotal: 1433}, 6)
	if err != nil || over.Lease.UploadSent != 1433 {
		t.Fatalf("sent beyond the total = %+v, %v; want it capped", over.Lease, err)
	}
	none, err := Heartbeat(c, "dev_a", Beat{Fence: 2, Pending: 0, UploadSent: 412, UploadTotal: 1433}, 6)
	if err != nil || none.Lease.UploadSent != 0 || none.Lease.UploadTotal != 0 {
		t.Fatalf("nothing pending kept progress: %+v, %v", none.Lease, err)
	}
	published, err := PublishTo(c, "dev_a", Publish{Fence: 2, OldHead: "h", Head: "h2"}, 7)
	if err != nil || published.Lease.UploadTotal != 0 {
		t.Fatalf("a publish kept progress: %+v, %v", published.Lease, err)
	}
	released, err := ReleaseOf(c, "dev_a", 2)
	if err != nil || released.Lease.UploadTotal != 0 {
		t.Fatalf("a release kept progress: %+v, %v", released.Lease, err)
	}
	taken, err := Acquire(c, "dev_b", 20, true)
	if err != nil || taken.Lease.UploadTotal != 0 {
		t.Fatalf("an acquire kept progress: %+v, %v", taken.Lease, err)
	}
}
