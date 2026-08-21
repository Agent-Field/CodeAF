package resident

import (
	"fmt"
	"math"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/plan"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

func TestShrinkOverrunRate(t *testing.T) {
	tests := []struct {
		name          string
		local, global float64
		samples       int
		want          float64
	}{
		{name: "no local history", local: 1, global: .25, samples: 0, want: .25},
		{name: "eight observations split the weight", local: .75, global: .25, samples: 8, want: .50},
		{name: "larger samples dominate", local: .75, global: .25, samples: 24, want: .625},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := ShrinkOverrunRate(test.local, test.global, test.samples); math.Abs(got-test.want) > 1e-9 {
				t.Fatalf("ShrinkOverrunRate() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestCapacityOptionsReadsSettledLeavesByModelAndHonorsSwarm(t *testing.T) {
	journal, err := store.Open(filepath.Join(t.TempDir(), "capacity.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { journal.Close() })
	specs := []store.NodeSpec{{ID: "job", Brief: "job"}}
	for index := 0; index < 8; index++ {
		specs = append(specs, store.NodeSpec{
			ID: fmt.Sprintf("leaf-%d", index), Parent: "job", Brief: "leaf",
		})
	}
	if err := journal.Splice(store.RootID, store.Subtree{Nodes: specs},
		store.Provenance{Origin: store.OriginUser, Intent: "measure capacity"}); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 8; index++ {
		id := fmt.Sprintf("leaf-%d", index)
		claim, ok, claimErr := journal.Claim(id, "worker")
		if claimErr != nil || !ok {
			t.Fatalf("claim %s: ok=%t err=%v", id, ok, claimErr)
		}
		if err := journal.Start(claim); err != nil {
			t.Fatal(err)
		}
		if err := journal.RecordUsage(store.NodeUsage{NodeID: id, Model: "model-a"}); err != nil {
			t.Fatal(err)
		}
		if index < 4 {
			if err := journal.RecordJobGrowth("job", store.JobGrowth{
				Reason: GrowOverrun, Lineage: id, Round: 1, Allowed: true,
			}); err != nil {
				t.Fatal(err)
			}
		}
		if err := journal.Complete(claim, "done"); err != nil {
			t.Fatal(err)
		}
	}

	base := plan.Options{Invoice: "prices"}
	if got := CapacityOptions(journal, false, "model-a", base); got.Invoice != base.Invoice || got.CapacitySamples != 0 {
		t.Fatalf("disabled swarm changed options: %+v", got)
	}
	got := CapacityOptions(journal, true, "model-a", base)
	if got.CapacitySamples != 8 || math.Abs(got.CapacityOverrunRate-.5) > 1e-9 {
		t.Fatalf("capacity options = %+v, want 8 samples at .5", got)
	}
}
