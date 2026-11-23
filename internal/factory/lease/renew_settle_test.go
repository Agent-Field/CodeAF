package lease_test

import (
	"bytes"
	"os"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory/lease"
)

// A RENEWAL REACHES THE DISK ONLY WHEN ITS PROMISE NEEDS IT (live e2e round
// 9, idle CPU): every held job's runner renews its hold every two seconds, and
// every renewal rewrote leases.json, which woke every watcher of the factory's
// folder — on macOS a re-read of the whole folder — for a hold whose deadline
// was still most of a minute away. The renewal answers its full new deadline
// at once; the file records it once the stored one has aged.
func TestARenewalRewritesTheLeasesOnlyOnceTheStoredOneHasAged(t *testing.T) {
	for _, renew := range []string{"heartbeat", "renew"} {
		t.Run(renew, func(t *testing.T) {
			f := setup(t)
			_, held := f.running(t)
			beat := func() lease.Lease {
				t.Helper()
				var got lease.Lease
				var err error
				if renew == "heartbeat" {
					got, err = f.desk.Heartbeat(held.Job, held.Worker)
				} else {
					got, err = f.desk.Renew(held.ID, held.Worker)
				}
				if err != nil {
					t.Fatal(err)
				}
				return got
			}
			written, err := os.ReadFile(f.desk.Path())
			if err != nil {
				t.Fatal(err)
			}
			for range 4 {
				f.now = f.now.Add(lease.HeartbeatEvery)
				got := beat()
				if want := f.now.Add(f.desk.TTL()); !got.ExpiresAt.Equal(want) {
					t.Fatalf("a renewal answered a deadline of %v, want %v", got.ExpiresAt, want)
				}
				if now, _ := os.ReadFile(f.desk.Path()); !bytes.Equal(now, written) {
					t.Fatalf("a renewal %v after the stored one rewrote leases.json", f.now.Sub(held.HeartbeatAt))
				}
			}
			f.now = held.HeartbeatAt.Add(lease.RenewalSettles)
			beat()
			if now, _ := os.ReadFile(f.desk.Path()); bytes.Equal(now, written) {
				t.Fatalf("a renewal %v after the stored one did not reach the disk", lease.RenewalSettles)
			}
			// The hold never lapses between writes: a runner that beats on
			// time is live a whole TTL after its last beat that was written.
			f.now = f.now.Add(f.desk.TTL() - time.Second)
			if live, err := f.desk.Live(); err != nil || len(live) != 1 {
				t.Fatalf("a hold renewed on time lapsed: %+v, %v", live, err)
			}
		})
	}
}
