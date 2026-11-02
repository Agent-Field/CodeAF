package intake_test

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/intake"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// THE READ NEVER GRANTS A START, whatever size it names and from wherever.
func TestTheShownSizeNeverGrantsAStart(t *testing.T) {
	// An issue with no size label reads as M ([pipeline.File.DecideJob]), so
	// the limit is S: a small read must not pass what the label alone would hold.
	f, err := pipeline.Parse("default_budget: 1\nstart:\n  labels: [agent-ready]\n  max_size: S\n")
	if err != nil {
		t.Fatal(err)
	}
	read := factory.Triage{Size: "S", SizeFrom: factory.SizeFromRead, TriagedAt: time.Unix(100, 0), StartJudged: true, StartMatched: []string{}}
	for _, tc := range []struct {
		name   string
		labels []string
		tr     factory.Triage
		start  bool
		reason string
	}{
		{"a small read and no label asks", []string{"agent-ready"}, read, false, "this one is M"},
		{"a small diff and no label asks", []string{"agent-ready"}, factory.Triage{Size: "S", SizeFrom: factory.SizeFromDiff, StartJudged: true, StartMatched: []string{}}, false, "this one is M"},
		{"a legacy size and no label asks", []string{"agent-ready"}, factory.Triage{Size: "S", TriagedAt: time.Unix(100, 0), StartJudged: true, StartMatched: []string{}}, false, "this one is M"},
		{"a large read does not hold a small label", []string{"agent-ready", "size:s"}, factory.Triage{Size: "L", SizeFrom: factory.SizeFromRead, TriagedAt: time.Unix(100, 0), StartJudged: true, StartMatched: []string{}}, true, ""},
		{"a small read does not rescue a large label", []string{"agent-ready", "size:l"}, read, false, "this one is L"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			it := factory.Item{ID: 1, Repo: "owner/repo", Num: 1, Kind: factory.KindIssue, Tier: factory.TierOwner, Labels: tc.labels, Triage: tc.tr}
			if got := intake.Job(it).Size; got != factory.JobSize(it) {
				t.Fatalf("intake sized the job %q, the label says %q", got, factory.JobSize(it))
			}
			d := f.DecideJob(it, intake.Job(it).Paths)
			if d.Start != tc.start || d.Ask == tc.start || (tc.reason != "" && !strings.Contains(strings.Join(d.Reasons, "; "), tc.reason)) {
				t.Fatalf("decision %+v", d)
			}
		})
	}
}

// Attaching a read's size to a floor starts nothing and moves nothing in the
// order intake works the queue in.
func TestAReadSizeStartsNothingAndReordersNothing(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var ids []int
	for n := 1; n <= 3; n++ {
		it, err := st.Create(factory.Item{Repo: "owner/repo", Num: n, Kind: factory.KindIssue, State: factory.StateNew,
			Labels: []string{"agent-ready"}, Triage: factory.Triage{StartJudged: true, StartMatched: []string{}, Priority: 4 - n}})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, it.ID)
	}
	order := func() []int {
		items, _ := st.List()
		got := make([]int, len(items))
		for i, it := range items {
			got[i] = it.ID
		}
		return got
	}
	before := order()
	for i, id := range ids {
		size := []string{"L", "S", "M"}[i]
		_ = st.Update(id, func(it *factory.Item) error {
			it.Triage.Size, it.Triage.SizeFrom, it.Triage.TriagedAt = size, factory.SizeFromRead, time.Unix(100, 0)
			return nil
		})
	}
	if after := order(); !reflect.DeepEqual(before, after) {
		t.Fatalf("a read size reordered the queue: %v then %v", before, after)
	}
	// Sizes are labels here, so only a label could start one: a read cannot.
	f, err := pipeline.Parse("default_budget: 1\nstart:\n  labels: [agent-ready]\nsize:\n  issue: label\n")
	if err != nil {
		t.Fatal(err)
	}
	history, err := record.Open(st, record.Options{})
	if err != nil {
		t.Fatal(err)
	}
	started := 0
	in := intake.New(st, intake.Options{Record: history, Load: func(string) (pipeline.File, error) { return f, nil }, Launch: func(int) error { started++; return nil }})
	if err := in.Once(ctx); err != nil {
		t.Fatal(err)
	}
	if started != 0 {
		t.Fatalf("a read size started %d jobs", started)
	}
	items, _ := st.List()
	for _, it := range items {
		if it.State == factory.StateQueued || it.State == factory.StateRunning {
			t.Fatalf("%s was started by a read size", it.Ref())
		}
	}
}
