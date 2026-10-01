package directory

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

// backend is what the two stores share for these tests.
type backend struct {
	name  string
	dir   Directory
	feed  *Feed
	join  func(Joined)
	clock *tick
}

// tick is a clock that moves only when told.
type tick struct{ t time.Time }

func (c *tick) Now() time.Time          { return c.t }
func (c *tick) Advance(d time.Duration) { c.t = c.t.Add(d) }

func backends(t *testing.T) []backend {
	t.Helper()
	cm := &tick{time.UnixMilli(1_000_000)}
	mem := NewMemory(cm.Now)
	cs := &tick{time.UnixMilli(1_000_000)}
	db, err := OpenSQLite(filepath.Join(t.TempDir(), "d.db"), cs.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return []backend{
		{"memory", mem, mem.Feed(), mem.pairing.onJoin, cm},
		{"sqlite", db, db.Feed(), db.pairing.onJoin, cs},
	}
}

func TestBackendsAnnounceJoinedAndStampLastSeen(t *testing.T) {
	for _, b := range backends(t) {
		t.Run(b.name, func(t *testing.T) {
			ctx := context.Background()
			if err := b.dir.For("dev_a").PutDevice(ctx, "dev_a", Device{V: 1}); err != nil {
				t.Fatal(err)
			}
			before, _ := b.dir.For("dev_a").List(ctx)
			b.clock.Advance(time.Hour)
			_, events := join(t, b.feed, "dev_a", true)
			drain(events)
			b.join(Joined{Device: "dev_new", Name: "bg==", Platform: "linux", At: 7})
			same(t, drain(events), []string{"joined:dev_new"})
			after, _ := b.dir.For("dev_a").List(ctx)
			if got := after.Devices["dev_a"].LastSeen; got != b.clock.Now().UnixMilli() {
				t.Fatalf("last_seen is %d, want the connect time", got)
			}
			if after.Version != before.Version {
				t.Fatalf("a connect moved the version %d -> %d", before.Version, after.Version)
			}
		})
	}
}

func TestPresenceViewListsOnlyDevicesThatAreNotRevoked(t *testing.T) {
	l := Listing{Now: 9, Devices: map[string]Device{
		"dev_on":  {LastSeen: 1},
		"dev_off": {LastSeen: 4},
		"dev_rev": {Revoked: true},
	}}
	v := presenceOf(l, map[string]bool{"dev_on": true})
	want := map[string]DevicePresence{"dev_on": {true, 9}, "dev_off": {false, 4}}
	if len(v.Devices) != 2 || v.Devices["dev_on"] != want["dev_on"] || v.Devices["dev_off"] != want["dev_off"] {
		t.Fatalf("view is %+v", v)
	}
}
