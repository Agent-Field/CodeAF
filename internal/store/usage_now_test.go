package store

import (
	"path/filepath"
	"testing"
	"testing/synctest"
	"time"
)

// Contract 5: a completed charge is visible even before the clock advances.
// Explicit end times retain their half-open boundary.
func TestPublishContract5SpendIncludesCurrentClockTick(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.db")
	synctest.Test(t, func(t *testing.T) {
		graph, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		defer graph.Close()
		if err := graph.Splice(RootID, Subtree{Nodes: []NodeSpec{{ID: "job", Title: "Work", Brief: "Work"}}}, Provenance{Origin: OriginUser, Intent: "Work"}); err != nil {
			t.Fatal(err)
		}
		at := time.Now()
		if err := graph.RecordUsage(NodeUsage{NodeID: "job", Cost: 1.5, Model: "test/model"}); err != nil {
			t.Fatal(err)
		}
		if !time.Now().Equal(at) {
			t.Fatal("the test clock advanced before the charge was read")
		}
		window, err := graph.SpendBetween(at.Add(-time.Hour), time.Time{})
		if err != nil || window.Runs != 1 || window.Cost != 1.5 {
			t.Fatalf("current spending = %+v, %v; want one call costing $1.50", window, err)
		}
		jobs, err := graph.SpendByJob(at.Add(-time.Hour), time.Time{}, 0)
		if err != nil || len(jobs) != 1 || jobs[0].Cost != 1.5 {
			t.Fatalf("current jobs = %+v, %v; want one job costing $1.50", jobs, err)
		}
		window, err = graph.SpendBetween(at.Add(-time.Hour), at)
		if err != nil || window.Runs != 0 || window.Cost != 0 {
			t.Fatalf("explicit upper boundary = %+v, %v; want no calls", window, err)
		}
		jobs, err = graph.SpendByJob(at.Add(-time.Hour), at, 0)
		if err != nil || len(jobs) != 0 {
			t.Fatalf("explicit job upper boundary = %+v, %v; want no jobs", jobs, err)
		}
	})
}
