package directory

import (
	"fmt"
	"testing"
	"time"

	cs "github.com/Agent-Field/codeaf/internal/clusterstate"
	"github.com/Agent-Field/codeaf/internal/dirwatch"
)

func TestR7_3CommittedWatchesShareBoundedCycle(t *testing.T) {
	clock := &fakeNow{ms: 1_000_000}
	f := NewFeed(clock.now)
	defer f.Close()
	var batches [][]Hold
	var samples []int64
	renew := func(_ string, holds []Hold, at int64) ([]dirwatch.HoldReceipt, error) {
		batches = append(batches, append([]Hold(nil), holds...))
		samples = append(samples, at)
		if len(holds) > cs.MaxRenewHolds {
			t.Fatal("r7.3 renewal exceeded batch bound", len(holds))
		}
		var receipts []dirwatch.HoldReceipt
		for _, h := range holds {
			receipts = append(receipts, dirwatch.HoldReceipt{Cell: h.Cell, Fence: h.Fence, Index: 1, At: at, Until: at + cs.LeaseTTLMillis})
		}
		return receipts, nil
	}
	var subs []*Sub
	for socket := range cs.MaxRenewHolds/MaxHolds + 1 {
		holds := make([]Hold, MaxHolds)
		for i := range holds {
			holds[i] = Hold{Cell: fmt.Sprintf("chat-%d-%d", socket, i), Fence: 1}
		}
		s, err := f.SubscribeCommitted("device", holds, DefaultBeat, renew)
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()
		s.takeReceipts()
		subs = append(subs, s)
	}
	for period := range 6 {
		batches, samples = nil, nil
		for beat := 1; beat <= int(cs.RenewEveryMillis/cs.PresenceEveryMillis); beat++ {
			clock.ms += cs.PresenceEveryMillis
			before := len(batches)
			if err := subs[0].PingCommitted(); err != nil {
				t.Fatal(err)
			}
			items := 0
			for _, batch := range batches[before:] {
				items += len(batch)
			}
			if items > cs.MaxRenewHolds {
				t.Fatalf("r7.3 one turn renewed %d holds, cap %d", items, cs.MaxRenewHolds)
			}
			for _, s := range subs[1:] {
				if err := s.PingCommitted(); err != nil {
					t.Fatal(err)
				}
			}
			if beat*int(cs.PresenceEveryMillis) < int(cs.RenewEveryMillis) && len(batches) != 0 {
				t.Fatalf("r7.3 period %d: %d renewals before the shared deadline", period, len(batches))
			}
		}
		seen := map[string]bool{}
		for i, batch := range batches {
			if clock.ms-samples[i] > cs.EvidenceWindowMillis {
				t.Fatal("r7.3 stale sample", samples[i])
			}
			for _, h := range batch {
				if seen[h.Cell] {
					t.Fatal("r7.3 duplicate renewal in a period", h)
				}
				seen[h.Cell] = true
			}
		}
		if len(seen) != len(subs)*MaxHolds {
			t.Fatalf("r7.3 later sockets starved: renewed=%d, want %d", len(seen), len(subs)*MaxHolds)
		}
		for _, s := range subs {
			if receipts := s.takeReceipts(); len(receipts) != MaxHolds {
				t.Fatalf("r7.3 pending receipts=%d, want %d", len(receipts), MaxHolds)
			}
		}
	}
	for _, s := range subs[1:] {
		s.Close()
	}
	batches = nil
	clock.ms += cs.RenewEveryMillis
	if err := subs[0].PingCommitted(); err != nil {
		t.Fatal(err)
	}
	if len(batches) != 1 || len(batches[0]) != MaxHolds {
		t.Fatal("r7.3 closed subscriptions retained evidence", batches)
	}
	clock.ms += cs.EvidenceWindowMillis + 1
	other, err := f.SubscribeCommitted("other", []Hold{{Cell: "other-chat", Fence: 2}}, time.Second, renew)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	batches = nil
	clock.ms += cs.RenewEveryMillis
	if err = other.PingCommitted(); err != nil {
		t.Fatal(err)
	}
	for _, batch := range batches {
		for _, h := range batch {
			if h.Cell != "other-chat" {
				t.Fatal("r7.3 silent socket retained fresh evidence", h)
			}
		}
	}
}
