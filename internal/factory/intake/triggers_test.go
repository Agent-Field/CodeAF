package intake_test

import (
	"context"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/route"
)

// The audit of factory triggers (2026-11-06) found these on the wire; each
// test below is one of its findings, in the words a person reads.

// routedRows is every Routed activity row of job id, oldest first.
func routedRows(t *testing.T, r *routed, id int) []string {
	t.Helper()
	events, err := r.history.Since(context.Background(), time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range events {
		if e.Job == id && e.Kind == record.Routed {
			out = append(out, e.Detail)
		}
	}
	return out
}

// A RULE NAMING A PIPELINE ITS REPOSITORY DOES NOT HAVE NEVER FAILS THE PASS.
// The pass error is what made every source's poll back off for ten minutes
// while the rule stood. Now the job stays unrouted, ONE activity row says
// why in the person's words, a second pass writes no second row, and the
// other jobs of the pass are worked on as before.
func TestARefusedRuleWritesOneRowAndNeverFailsThePass(t *testing.T) {
	rule := jiraRule(route.StartAuto)
	rule.Then.Pipeline = "nope"
	r, job := routedFloor(t, []route.Rule{rule})
	ready, err := r.st.Create(factory.Item{Repo: "acme/demo", Num: 4, Kind: factory.KindIssue, Tier: factory.TierOwner, State: factory.StateNew,
		Title: "Ready", Labels: []string{"agent-ready"}, Created: time.Unix(300, 0)})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for pass := 0; pass < 2; pass++ {
		if err := r.in.Once(ctx); err != nil {
			t.Fatalf("pass %d failed on a refused rule: %v", pass, err)
		}
	}
	got, _ := r.st.Get(job.ID)
	if got.Route != "" || got.Repo != "" || got.State != factory.StateNew {
		t.Fatalf("a refused rule changed the job: %+v", got)
	}
	rows := routedRows(t, r, job.ID)
	if len(rows) != 1 || rows[0] != "routing rule jira FL: acme/demo has no pipeline nope" {
		t.Fatalf("activity rows %q, want exactly one naming the rule, the repository and the pipeline", rows)
	}
	// The fake launch leaves the job new, so each pass starts it again.
	if len(r.started) != 2 || r.started[0] != ready.ID || r.started[1] != ready.ID {
		t.Fatalf("started %v, want the labelled job %d the refusal must not stop", r.started, ready.ID)
	}
}

// A RULE THAT FILES A JOB IN BACKLOG SAYS SO, once: nothing ran, but the job's
// activity names the rule that moved it.
func TestABacklogRuleWritesItsActivityRow(t *testing.T) {
	r, job := routedFloor(t, []route.Rule{jiraRule(route.StartBacklog)})
	ctx := context.Background()
	for pass := 0; pass < 2; pass++ {
		if err := r.in.Once(ctx); err != nil {
			t.Fatal(err)
		}
	}
	rows := routedRows(t, r, job.ID)
	if len(rows) != 1 || rows[0] != "routing rule jira FL · Backlog" {
		t.Fatalf("activity rows %q, want one `routing rule jira FL · Backlog`", rows)
	}
}

// editedFloor is a Jira ticket labelled with the repository's start label,
// filed in Backlog by rule A (its title says AUDC, quick pipeline), with
// rest after it. The ticket is then retitled so rule A no longer matches.
func editedFloor(t *testing.T, rest ...route.Rule) (*routed, factory.Item) {
	t.Helper()
	a := route.Rule{Name: "A", When: route.Match{Text: []string{"AUDC"}}, Then: route.Action{Repo: "acme/demo", Pipeline: "quick", Start: route.StartBacklog}}
	r, job := routedFloor(t, append([]route.Rule{a}, rest...))
	if err := r.st.Update(job.ID, func(cur *factory.Item) error {
		cur.Title, cur.Labels = "AUDC: the checkout button is grey", []string{"agent-ready"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := r.in.Once(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, _ := r.st.Get(job.ID)
	if got.Route != "A" || stageNames(got.Stages) != "write proof" {
		t.Fatalf("rule A did not file the job: %+v", got)
	}
	if err := r.st.Update(job.ID, func(cur *factory.Item) error { cur.Title = "The checkout button is grey"; return nil }); err != nil {
		t.Fatal(err)
	}
	return r, got
}

// AN EDIT KEEPS A ROUTED JOB'S ROUTE AND STEPS when no rule matches it now.
// It used to fall through to the label rule, which reset its steps to the
// repository's default and started it.
func TestAnEditedTicketKeepsItsRouteWhenNoRuleMatches(t *testing.T) {
	r, job := editedFloor(t)
	if err := r.in.Once(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, _ := r.st.Get(job.ID)
	if len(r.started) != 0 || got.Route != "A" || stageNames(got.Stages) != "write proof" || got.State != factory.StateNew {
		t.Fatalf("started %v job %+v stages %s; want it left in Backlog on rule A's steps", r.started, got, stageNames(got.Stages))
	}
}

// A DIFFERENT RULE THAT NOW MATCHES MOVES THE JOB, PROPERLY AND OUT LOUD: its
// pipeline (none named: the repository's own) replaces rule A's, and the
// activity says which rule took the place of which, with the steps it has
// now, so a pipeline is never changed silently.
func TestADifferentRuleMovesARoutedJobAndSaysSo(t *testing.T) {
	b := route.Rule{Name: "B", When: route.Match{Sources: []string{"jira"}}, Then: route.Action{Repo: "acme/demo", Start: route.StartBacklog}}
	r, job := editedFloor(t, b)
	if err := r.in.Once(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, _ := r.st.Get(job.ID)
	if got.Route != "B" || stageNames(got.Stages) != "plan write proof" {
		t.Fatalf("job %+v stages %s; want rule B's", got, stageNames(got.Stages))
	}
	rows := routedRows(t, r, job.ID)
	want := "routing rule B · in place of routing rule A · steps plan, write, proof · Backlog"
	if len(rows) != 2 || rows[1] != want {
		t.Fatalf("activity rows %q, want the last to be %q", rows, want)
	}
}

// PICK UP ISSUES OFF GOVERNS THE LABEL RULE ONLY. A routing rule still sends
// the Jira ticket to its repository and starts it, while the labelled issue no
// rule matched waits for a person.
func TestLabelsOffStillRoutes(t *testing.T) {
	r, job := routedFloor(t, []route.Rule{jiraRule(route.StartAuto)})
	labelled, err := r.st.Create(factory.Item{Repo: "acme/demo", Num: 4, Kind: factory.KindIssue, Tier: factory.TierOwner, State: factory.StateNew,
		Title: "Ready", Labels: []string{"agent-ready"}, Created: time.Unix(300, 0)})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.in.LabelsGated(func() bool { return false }).Once(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(r.started) != 1 || r.started[0] != job.ID {
		t.Fatalf("started %v, want only the routed job %d", r.started, job.ID)
	}
	if got, _ := r.st.Get(labelled.ID); got.State != factory.StateNew || got.Route != "" {
		t.Fatalf("the labelled job moved with Pick up issues off: %+v", got)
	}
	if got, _ := r.st.Get(job.ID); got.Route != "jira FL" || got.Repo != "acme/demo" {
		t.Fatalf("routed job %+v", got)
	}
}

// A PERSON'S REPOSITORY WINS over a rule's: the rule still decides the job's
// pipeline and start, on the repository the person gave it.
func TestAPersonsRepositoryWinsOverARules(t *testing.T) {
	rule := jiraRule(route.StartBacklog)
	rule.Then.Repo = "acme/other"
	r, job := routedFloor(t, []route.Rule{rule})
	if err := r.st.Update(job.ID, func(cur *factory.Item) error {
		cur.Repo, cur.Places, cur.RepoSetBy = "acme/demo", []string{"acme/demo"}, "ann"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := r.in.Once(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, _ := r.st.Get(job.ID)
	if got.Repo != "acme/demo" || got.Route != "jira FL" || stageNames(got.Stages) != "write proof" {
		t.Fatalf("job %+v stages %s; want ann's repository with the rule's pipeline", got, stageNames(got.Stages))
	}
}
