package store

import (
	"path/filepath"
	"testing"
)

// The journal is the round counter, not a description of one: an admission that
// is lost is a round nobody spent, and a refusal that is lost is a governor
// nobody can see having spoken.
func TestJobGrowthJournalsAdmissionsAndRefusals(t *testing.T) {
	graph, err := Open(filepath.Join(t.TempDir(), "growth.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer graph.Close()

	if err := graph.RecordJobGrowth("job", JobGrowth{
		Reason: "overrun", Lineage: "job-n1", Adding: 3, Round: 1, Allowed: true,
	}); err != nil {
		t.Fatal(err)
	}
	if err := graph.RecordJobGrowth("job", JobGrowth{
		Reason: "revision", Lineage: "job", Adding: 2, Round: 1, Allowed: false,
		Refused: "handing over what's done", Cause: "goal-already-covered",
	}); err != nil {
		t.Fatal(err)
	}

	growths, err := graph.JobGrowths("job")
	if err != nil {
		t.Fatal(err)
	}
	if len(growths) != 2 {
		t.Fatalf("growths = %+v", growths)
	}
	if growths[0].Reason != "overrun" || growths[0].Lineage != "job-n1" || !growths[0].Allowed || growths[0].Adding != 3 {
		t.Fatalf("the admission came back wrong: %+v", growths[0])
	}
	if growths[1].Cause != "goal-already-covered" || growths[1].Allowed ||
		growths[1].Refused != "handing over what's done" {
		t.Fatalf("the refusal came back wrong: %+v", growths[1])
	}

	// A different job's journal is its own, and an unknown one is empty rather
	// than an error — a job that has never grown has nothing to say.
	other, err := graph.JobGrowths("other-job")
	if err != nil || len(other) != 0 {
		t.Fatalf("other job's growths = %+v err=%v", other, err)
	}
	if err := graph.RecordJobGrowth("", JobGrowth{Reason: "overrun"}); err == nil {
		t.Fatal("a growth with no job root was journaled")
	}
	if err := graph.RecordJobGrowth("job", JobGrowth{}); err == nil {
		t.Fatal("a growth with no reason was journaled")
	}
}
