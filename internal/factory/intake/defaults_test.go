package intake_test

import (
	"context"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/github"
	"github.com/Agent-Field/codeaf/internal/factory/intake"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// pickUp is one repository's floor: the poll writes what the forge says, and
// intake decides under f. Launch moves a job to Queued, as the real door does.
type pickUp struct {
	st      *store.Store
	in      *intake.Intake
	started int
}

func newPickUp(t *testing.T, f pipeline.File) *pickUp {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetRepos([]string{"owner/repo"}); err != nil {
		t.Fatal(err)
	}
	history, err := record.Open(st, record.Options{})
	if err != nil {
		t.Fatal(err)
	}
	p := &pickUp{st: st}
	p.in = intake.New(st, intake.Options{Record: history, Load: func(string) (pipeline.File, error) { return f, nil }, Launch: func(id int) error {
		p.started++
		return st.Update(id, func(it *factory.Item) error { it.State = factory.StateQueued; return nil })
	}})
	return p
}

// poll reads one issue with these labels from the forge, then runs intake.
func (p *pickUp) poll(t *testing.T, labels ...string) factory.Item {
	t.Helper()
	ctx := context.Background()
	src := source{[]factory.Item{{Origin: "fake", Repo: "owner/repo", Num: 1, Kind: factory.KindIssue, Tier: factory.TierOwner, Title: "Fix the docs", Body: "fix docs/readme.md", Labels: labels}}}
	if _, err := github.PollOnce(ctx, src, p.st, "", func() time.Time { return time.Unix(100, 0) }); err != nil {
		t.Fatal(err)
	}
	if err := p.in.Once(ctx); err != nil {
		t.Fatal(err)
	}
	items, err := p.st.List()
	if err != nil || len(items) != 1 {
		t.Fatalf("items %+v %v", items, err)
	}
	return items[0]
}

// A FACTORY THAT CHANGED NOTHING BUT PICK UP ISSUES STARTS WHAT IT PICKS UP:
// the defaults' budget is within the defaults' cost limit, and an issue nobody
// sized is M, which the defaults let start.
func TestADefaultFactoryStartsAnIssueItPicksUp(t *testing.T) {
	f := pipeline.WithHouseRules(pipeline.Defaults(), factory.HouseRules{StartOnItsOwn: true, StartLabels: []string{"agent-ready"}})
	p := newPickUp(t, f)
	it := p.poll(t, "agent-ready")
	if p.started != 1 || it.State != factory.StateQueued || it.AskedFirstReason != "" {
		t.Fatalf("started %d, job %s held %q", p.started, it.State, it.AskedFirstReason)
	}
	if it.Cap != factory.DefaultJobBudget {
		t.Fatalf("budget %v, want the default %v", it.Cap, factory.DefaultJobBudget)
	}
}

// A START HELD FOR ITS LABELS IS DECIDED AGAIN WHEN THEY CHANGE: a size label
// put on a held issue lets it start, with no person in between.
func TestAHeldStartIsDecidedAgainWhenItsLabelsChange(t *testing.T) {
	f, err := pipeline.Parse("start:\n  labels: [agent-ready]\nsize:\n  issue: label\n")
	if err != nil {
		t.Fatal(err)
	}
	p := newPickUp(t, f)
	it := p.poll(t, "agent-ready")
	if p.started != 0 || it.State != factory.StateNeedsYou || it.AskedFirstReason != "no size label" {
		t.Fatalf("held %s %q", it.State, it.AskedFirstReason)
	}
	// The same labels again ask nothing new.
	if it = p.poll(t, "agent-ready"); p.started != 0 || it.State != factory.StateNeedsYou {
		t.Fatalf("an unchanged poll moved the hold: %s", it.State)
	}
	// Still over the limit: asked again, with the new reason.
	if it = p.poll(t, "agent-ready", "size:XL"); p.started != 0 || it.State != factory.StateNeedsYou || it.AskedFirstReason != "over size M · this one is XL" {
		t.Fatalf("relabelled XL: %s %q", it.State, it.AskedFirstReason)
	}
	if it = p.poll(t, "agent-ready", "size:S"); p.started != 1 || it.State != factory.StateQueued || it.QKind != "" || it.Question != "" {
		t.Fatalf("relabelled S: started %d, %s %q %q", p.started, it.State, it.QKind, it.Question)
	}
}

// A HOLD A PERSON ANSWERED IS THEIRS: a declined start stays declined whatever
// its labels become.
func TestADeclinedStartStaysDeclinedWhenItsLabelsChange(t *testing.T) {
	f, err := pipeline.Parse("start:\n  labels: [agent-ready]\nsize:\n  issue: label\n")
	if err != nil {
		t.Fatal(err)
	}
	p := newPickUp(t, f)
	it := p.poll(t, "agent-ready")
	if err := p.st.Update(it.ID, func(cur *factory.Item) error {
		cur.State, cur.QKind, cur.Question = factory.StateNew, "", ""
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if it = p.poll(t, "agent-ready", "size:S"); p.started != 0 || it.State != factory.StateNew {
		t.Fatalf("a declined start ran: started %d, %s", p.started, it.State)
	}
}
