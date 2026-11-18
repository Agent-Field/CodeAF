package serve_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
)

// THE PERSON'S RESUME CARRIES AUTO PAST THE FAILING CHECK IT PAUSED ON: the
// notice clears, Auto's own approval publishes, and the record and the job's
// history name who overrode which finding.
func TestResumeAutoCarriesAPausedJobPastItsFailingCheck(t *testing.T) {
	f := newV2Jobs(t)
	in := autoProof()
	in.Policy = []factory.Claim{{Text: "FINDING: a vendored secret", Medium: "policy"}}
	it := f.create(t, in)
	if j := f.read(t, it); !strings.HasPrefix(j.AutonomyWhy, "Repository policy needs attention: FINDING: a vendored secret") {
		t.Fatalf("Auto did not pause on the policy finding: %q", j.AutonomyWhy)
	}
	if saved, _ := f.st.Get(it.ID); saved.Publishing {
		t.Fatal("a job paused on a failing check was published")
	}

	f.post(t, it, serve.ResumeAutoVerb, `{"words":"resume, that secret is a test fixture"}`, 200)

	if j := f.read(t, it); j.AutonomyWhy != "" {
		t.Fatalf("the pause notice is still up after the person's resume: %q", j.AutonomyWhy)
	}
	saved, err := f.st.Get(it.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !saved.Publishing || saved.Approval == nil || !saved.Approval.Automatic {
		t.Fatalf("Auto did not carry on: publishing=%v approval=%+v", saved.Publishing, saved.Approval)
	}
	if len(saved.Overrides) != 1 || saved.Overrides[0].By != f.person || saved.Overrides[0].Words != "resume, that secret is a test fixture" {
		t.Fatalf("the override reads %+v", saved.Overrides)
	}
	noted := false
	for _, ev := range saved.Activity {
		noted = noted || ev.What == "Auto pause overridden by @"+f.person+" · repository policy: FINDING: a vendored secret"
	}
	if !noted {
		t.Fatalf("the job's history does not name the override: %+v", saved.Activity)
	}
	events, err := f.rec.Since(context.Background(), time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	recorded := false
	for _, e := range events {
		recorded = recorded || e.Kind == record.Resumed && e.Handle == f.person && strings.Contains(e.Detail, "FINDING: a vendored secret")
	}
	if !recorded {
		t.Fatalf("the record does not name the override: %+v", events)
	}
}

// A RESUME WITH NOTHING TO OVERRIDE CHANGES NOTHING.
func TestResumeAutoIsRefusedWithoutAPauseOnAFinding(t *testing.T) {
	f := newV2Jobs(t)
	in := autoProof()
	in.Autonomy = factory.AutonomyManual
	in.Policy = []factory.Claim{{Text: "FINDING: a vendored secret", Medium: "policy"}}
	it := f.create(t, in)
	f.post(t, it, serve.ResumeAutoVerb, `{"words":"resume"}`, 409)
	if saved, _ := f.st.Get(it.ID); len(saved.Overrides) != 0 || saved.Publishing {
		t.Fatalf("a refused resume changed the job: overrides=%+v publishing=%v", saved.Overrides, saved.Publishing)
	}
}
