package store

import (
	"math"
	"path/filepath"
	"testing"
	"time"
)

// The bounded reader exists only to stop reading the whole journal. It is the
// same verdict or it is a bug, so the two implementations are held against each
// other directly: identical access counts, identical activation, identical
// quarantine decisions on both sides of the retention threshold.
func TestBoundedAgingMatchesTheUnboundedReader(t *testing.T) {
	graph := activationFixture(t, "accesses")
	facts, err := graph.ActiveFacts("", 10000)
	if err != nil {
		t.Fatal(err)
	}
	aging := make([]Fact, 0, len(facts))
	for _, fact := range facts {
		if fact.Kind == FactTrait || fact.Kind == FactQuestion {
			continue
		}
		aging = append(aging, fact)
	}
	accesses, err := graph.factAccesses(aging)
	if err != nil {
		t.Fatal(err)
	}
	recent := time.Now().Add(72 * time.Hour)
	for _, fact := range aging {
		want, decay, count, err := graph.FactActivation(fact.Seq, recent)
		if err != nil {
			t.Fatal(err)
		}
		batched := append([]time.Time{fact.Time}, accesses[fact.Seq]...)
		got := BaseLevelActivation(batched, recent, decay)
		if len(batched) != count || math.Abs(got-want) > 1e-12 {
			t.Fatalf("fact %d: %v over %d accesses, want %v over %d",
				fact.Seq, got, len(batched), want, count)
		}
	}

	// Once above the retention threshold, once well below it.
	for _, at := range []time.Time{recent, time.Now().Add(5000 * 24 * time.Hour)} {
		unbounded, bounded := activationFixture(t, "unbounded"), activationFixture(t, "bounded")
		originalAged, err := unbounded.AgeFacts(at)
		if err != nil {
			t.Fatal(err)
		}
		boundedAged, err := bounded.AgeFactsBounded(at)
		if err != nil {
			t.Fatal(err)
		}
		if originalAged != boundedAged {
			t.Fatalf("at %v: unbounded aged %d, bounded aged %d", at, originalAged, boundedAged)
		}
		left, err := unbounded.Facts(0)
		if err != nil {
			t.Fatal(err)
		}
		right, err := bounded.Facts(0)
		if err != nil {
			t.Fatal(err)
		}
		if len(left) != len(right) {
			t.Fatalf("fact counts diverged: %d vs %d", len(left), len(right))
		}
		for i := range left {
			if left[i].Seq != right[i].Seq || left[i].Status != right[i].Status {
				t.Fatalf("fact %d status diverged: %s vs %s", left[i].Seq, left[i].Status, right[i].Status)
			}
		}
	}
}

// activationFixture builds a notebook whose injections repeat a belief inside
// one list and across events — the two places the readers could disagree.
func activationFixture(t *testing.T, name string) *Store {
	t.Helper()
	graph := openTestStore(t, filepath.Join(t.TempDir(), name+".db"))
	if err := graph.Splice(RootID, Subtree{Nodes: []NodeSpec{
		{ID: "job", Brief: "exercise activation", Stage: 0},
		{ID: "a", Parent: "job", Brief: "one", Stage: 1},
		{ID: "b", Parent: "job", Brief: "two", Stage: 1},
	}}, Provenance{Origin: OriginUser, Intent: "age beliefs"}); err != nil {
		t.Fatal(err)
	}
	seqs := make([]int64, 0, 3)
	for _, body := range []string{"first lesson", "second lesson", "third lesson"} {
		fact, err := graph.RecordFact("", "repo:aging", FactLesson, body)
		if err != nil {
			t.Fatal(err)
		}
		seqs = append(seqs, fact.Seq)
	}
	playbook, err := graph.RecordFact("", "repo:aging", FactPlaybook, "a playbook")
	if err != nil {
		t.Fatal(err)
	}
	if err := graph.RecordFactInjection("a", []int64{seqs[0], seqs[1], seqs[0]}); err != nil {
		t.Fatal(err)
	}
	if err := graph.RecordFactInjection("a", []int64{seqs[0]}); err != nil {
		t.Fatal(err)
	}
	if err := graph.RecordFactInjection("b", []int64{seqs[2], playbook.Seq}); err != nil {
		t.Fatal(err)
	}
	return graph
}
