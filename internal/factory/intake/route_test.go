package intake_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/inbox"
	"github.com/Agent-Field/codeaf/internal/factory/intake"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/route"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// routedFile is factory-demo's file: start labels the routed job never
// carries, and a quick pipeline beside the issue one.
const routedFile = `default_budget: 1
start:
  labels: [agent-ready]
pipeline:
  issue: [plan, write, proof]
  quick: [write, proof]
steps:
  plan:
    prompt: plan it
  write:
    prompt: write it
  proof:
    prompt: prove it
`

type routed struct {
	st      *store.Store
	in      *intake.Intake
	history *record.Record
	loaded  []string
	started []int
}

// routedFloor is a factory watching acme/demo, with one job from Jira that has
// no repository and no label, and the routing table rules.
func routedFloor(t *testing.T, rules []route.Rule, judge ...route.Judge) (*routed, factory.Item) {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetRepos([]string{"acme/demo"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SetRoutes(rules, factory.RulesChange{}, nil); err != nil {
		t.Fatal(err)
	}
	job, err := st.Create(factory.Item{Origin: "jira", SourceRef: "jira:FL-7", Kind: factory.KindIssue, Tier: factory.TierOwner, State: factory.StateNew,
		Title: "Checkout button is grey", Body: "fix docs/readme.md", Created: time.Unix(100, 0)})
	if err != nil {
		t.Fatal(err)
	}
	history, err := record.Open(st, record.Options{})
	if err != nil {
		t.Fatal(err)
	}
	f, err := pipeline.Parse(routedFile)
	if err != nil {
		t.Fatal(err)
	}
	r := &routed{st: st, history: history}
	var j route.Judge
	if len(judge) > 0 {
		j = judge[0]
	}
	r.in = intake.New(st, intake.Options{Record: history, Judge: j,
		Load:   func(repo string) (pipeline.File, error) { r.loaded = append(r.loaded, repo); return f, nil },
		Launch: func(id int) error { r.started = append(r.started, id); return nil }})
	return r, job
}

func jiraRule(start route.Start) route.Rule {
	return route.Rule{Name: "jira FL", When: route.Match{Sources: []string{"jira"}},
		Then: route.Action{Repo: "acme/demo", Pipeline: "quick", Priority: "p0", Start: start}}
}

// A ROUTED JOB NEEDS NO LABEL. start: auto sends it to the rule's repository,
// gives it the rule's pipeline and priority, and starts it.
func TestARoutedJobStartsOnTheRulesPipeline(t *testing.T) {
	r, job := routedFloor(t, []route.Rule{jiraRule(route.StartAuto)})
	if err := r.in.Once(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, err := r.st.Get(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.started) != 1 || r.started[0] != job.ID {
		t.Fatalf("started %v, want job %d", r.started, job.ID)
	}
	if len(r.loaded) == 0 || r.loaded[0] != "acme/demo" {
		t.Fatalf("read the file of %v, want acme/demo", r.loaded)
	}
	if got.Route != "jira FL" || got.Repo != "acme/demo" || stageNames(got.Stages) != "write proof" || got.Priority == nil || got.Priority.Value != "urgent" {
		t.Fatalf("routed job %+v stages %s", got, stageNames(got.Stages))
	}
}

// start: ask holds the job with the existing ask-first hold, naming the rule.
func TestARoutedJobAsksFirstNamingTheRule(t *testing.T) {
	r, job := routedFloor(t, []route.Rule{jiraRule(route.StartAsk)})
	if err := r.in.Once(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, err := r.st.Get(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.started) != 0 || got.QKind != factory.QKindAskFirst || got.AskedFirstReason != "routing rule jira FL" ||
		got.Question != "Approval needed: start · routing rule jira FL" || stageNames(got.Stages) != "write proof" {
		t.Fatalf("asked job %+v, started %v", got, r.started)
	}
}

// start: backlog files the job with the rule's decision on it, starts
// nothing, and the next pass leaves it without reading its file again.
func TestARoutedJobWaitsInBacklog(t *testing.T) {
	r, job := routedFloor(t, []route.Rule{jiraRule(route.StartBacklog)})
	ctx := context.Background()
	if err := r.in.Once(ctx); err != nil {
		t.Fatal(err)
	}
	got, err := r.st.Get(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.started) != 0 || got.State != factory.StateNew || got.QKind != "" || got.Route != "jira FL" || got.Repo != "acme/demo" || stageNames(got.Stages) != "write proof" {
		t.Fatalf("backlog job %+v, started %v", got, r.started)
	}
	loads := len(r.loaded)
	if err := r.in.Once(ctx); err != nil {
		t.Fatal(err)
	}
	if len(r.loaded) != loads {
		t.Fatalf("a filed job was read again: %v", r.loaded)
	}
}

// A pipeline the target repository's file does not list refuses the rule
// and changes nothing on the job (triggers_test.go has the activity row and
// the pass that goes on).
func TestARoutedJobRefusesAPipelineTheFileLacks(t *testing.T) {
	rule := jiraRule(route.StartAuto)
	rule.Then.Pipeline = "missing"
	r, job := routedFloor(t, []route.Rule{rule})
	if err := r.in.Once(context.Background()); err != nil {
		t.Fatalf("a refused rule failed the pass: %v", err)
	}
	got, _ := r.st.Get(job.ID)
	if len(r.started) != 0 || got.Route != "" || got.Repo != "" {
		t.Fatalf("a refused rule changed the job: %+v", got)
	}
}

// A rule that matches nothing leaves an unlabelled job alone, as before.
func TestAnUnmatchedUnlabelledJobIsLeftAlone(t *testing.T) {
	r, job := routedFloor(t, []route.Rule{{Name: "github only", When: route.Match{Sources: []string{"github"}}, Then: route.Action{Start: route.StartAuto}}})
	if err := r.in.Once(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, _ := r.st.Get(job.ID)
	if len(r.loaded) != 0 || len(r.started) != 0 || got.Route != "" {
		t.Fatalf("an unmatched job was worked on: loaded %v started %v job %+v", r.loaded, r.started, got)
	}
}

// A routed job sent to a repository that is off is passed by, like a labelled one.
func TestARoutedJobToAnOffRepoIsPassedBy(t *testing.T) {
	rule := jiraRule(route.StartAuto)
	rule.Then.Repo = "acme/elsewhere"
	r, _ := routedFloor(t, []route.Rule{rule})
	if err := r.in.Once(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(r.loaded) != 0 || len(r.started) != 0 {
		t.Fatalf("intake worked on an off repository: loaded %v started %v", r.loaded, r.started)
	}
}

// fakeJudge answers every question the same way and counts the calls.
type fakeJudge struct {
	yes   bool
	why   string
	err   error
	calls int
}

func (j *fakeJudge) Judge(_ context.Context, _ string, _ factory.Item) (route.Answer, error) {
	j.calls++
	return route.Answer{Yes: j.yes, Why: j.why, Model: "fake"}, j.err
}

const smallBug = "Is this a small, well-specified bug fix?"

func askingRules() []route.Rule {
	ask := jiraRule(route.StartAuto)
	ask.When.Ask = smallBug
	return []route.Rule{ask, {Name: "the rest", When: route.Match{Sources: []string{"jira"}}, Then: route.Action{Repo: "acme/demo", Start: route.StartBacklog}}}
}

func lastActivity(t *testing.T, r *routed, id int) record.Event {
	t.Helper()
	events, err := r.history.Since(context.Background(), time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Job == id {
			return events[i]
		}
	}
	t.Fatalf("job %d has no activity", id)
	return record.Event{}
}

// AN AI YES MATCHES THE RULE: the answer is kept on the job, the job starts,
// and the activity line names the rule and the model's reason. The next pass
// asks nothing again.
func TestAJudgedYesStartsAndIsKept(t *testing.T) {
	judge := &fakeJudge{yes: true, why: "a one-line colour fix"}
	r, job := routedFloor(t, askingRules(), judge)
	ctx := context.Background()
	if err := r.in.Once(ctx); err != nil {
		t.Fatal(err)
	}
	got, _ := r.st.Get(job.ID)
	v, ok := got.Judged["jira FL"]
	if judge.calls != 1 || len(r.started) != 1 || !ok || !v.Yes || v.Why != "a one-line colour fix" || v.Key != route.AnswerKey(smallBug, got) || v.At.IsZero() {
		t.Fatalf("calls %d started %v answer %+v", judge.calls, r.started, v)
	}
	if ev := lastActivity(t, r, job.ID); ev.Kind != record.AutoStarted || ev.Detail != "routing rule jira FL · a one-line colour fix" {
		t.Fatalf("activity %+v", ev)
	}
	if err := r.in.Once(ctx); err != nil {
		t.Fatal(err)
	}
	if judge.calls != 1 {
		t.Fatalf("a kept answer was asked again: %d calls", judge.calls)
	}
}

// An AI no reads on to the next rule, once, and keeps the no.
func TestAJudgedNoReadsOnToTheNextRule(t *testing.T) {
	judge := &fakeJudge{why: "touches billing"}
	r, job := routedFloor(t, askingRules(), judge)
	if err := r.in.Once(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, _ := r.st.Get(job.ID)
	if judge.calls != 1 || len(r.started) != 0 || got.Route != "the rest" || got.Judged["jira FL"].Yes || got.Judged["jira FL"].Why != "touches billing" {
		t.Fatalf("calls %d started %v job %+v", judge.calls, r.started, got)
	}
}

// A JUDGE THAT FAILS IS A NO, kept as "couldn't judge: …" with no key, so it
// is asked again on the next pass and never more than once in one.
func TestAJudgeFailureIsANoAndIsAskedAgainNextPass(t *testing.T) {
	judge := &fakeJudge{yes: true, err: errors.New("model timed out")}
	r, job := routedFloor(t, askingRules(), judge)
	ctx := context.Background()
	if err := r.in.Once(ctx); err != nil {
		t.Fatal(err)
	}
	got, _ := r.st.Get(job.ID)
	v := got.Judged["jira FL"]
	if judge.calls != 1 || len(r.started) != 0 || v.Yes || v.Why != "couldn't judge: model timed out" || v.Key != "" || got.Route != "the rest" {
		t.Fatalf("calls %d started %v job %+v", judge.calls, r.started, got)
	}
	// The job is filed in Backlog by the next rule, which a pass leaves be;
	// take that filing away so the next pass reads the table again, and the
	// failed question is asked once more.
	if err := r.st.Update(job.ID, func(it *factory.Item) error { it.Route = ""; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := r.in.Once(ctx); err != nil {
		t.Fatal(err)
	}
	if judge.calls != 2 {
		t.Fatalf("a failed judgement was not asked again: %d calls", judge.calls)
	}
}

// A STRANGER'S TICKET IS NEVER STARTED BY AN AI YES: start: auto asks first.
func TestAnAIYesHoldsAStrangersTicket(t *testing.T) {
	judge := &fakeJudge{yes: true, why: "looks small"}
	r, job := routedFloor(t, askingRules(), judge)
	if err := r.st.Update(job.ID, func(it *factory.Item) error { it.Tier = factory.TierStranger; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := r.in.Once(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, _ := r.st.Get(job.ID)
	want := "routing rule jira FL · looks small · asks first: an AI yes never starts a stranger's ticket"
	if len(r.started) != 0 || got.QKind != factory.QKindAskFirst || got.AskedFirstReason != want {
		t.Fatalf("started %v job %+v", r.started, got)
	}
	if ev := lastActivity(t, r, job.ID); ev.Kind != record.AskedFirst || ev.Detail != want {
		t.Fatalf("activity %+v", ev)
	}
}

// With no judge, a rule that asks a question never matches and nothing is
// kept; the table reads on past it.
func TestNoJudgeMeansAQuestionNeverMatches(t *testing.T) {
	r, job := routedFloor(t, askingRules())
	if err := r.in.Once(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, _ := r.st.Get(job.ID)
	if len(r.started) != 0 || got.Judged != nil || got.Route != "the rest" {
		t.Fatalf("started %v job %+v", r.started, got)
	}
}

// AN INBOX JOB WITH NO LABEL AND NO REPOSITORY IS ROUTED BEFORE EITHER SKIP:
// the rule names its repository, and it starts.
func TestAnInboxJobWithNoLabelOrRepoIsRoutedAndStarts(t *testing.T) {
	rule := route.Rule{Name: "ops", When: route.Match{Sources: []string{"inbox:ops"}}, Then: route.Action{Repo: "acme/demo", Start: route.StartAuto}}
	r, _ := routedFloor(t, []route.Rule{rule})
	job, err := r.st.Create(factory.Item{Origin: "inbox:ops", SourceRef: "inbox:ops:A1", Kind: factory.KindIssue, Tier: factory.TierOwner, State: factory.StateNew,
		Title: "Rotate the staging key", Created: time.Unix(200, 0)})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.in.Once(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, _ := r.st.Get(job.ID)
	if len(r.started) != 1 || r.started[0] != job.ID || got.Repo != "acme/demo" || got.Route != "ops" || stageNames(got.Stages) != "plan write proof" {
		t.Fatalf("started %v job %+v", r.started, got)
	}
}

// With no rule for it, the same job waits in Backlog: no label, no repository.
func TestAnUnroutedInboxJobWaits(t *testing.T) {
	r, job := routedFloor(t, nil)
	if err := r.in.Once(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, _ := r.st.Get(job.ID)
	if len(r.loaded) != 0 || len(r.started) != 0 || got.Route != "" || got.State != factory.StateNew {
		t.Fatalf("loaded %v started %v job %+v", r.loaded, r.started, got)
	}
}

// A RULE'S PRIORITY WINS OVER THE ONE THE SOURCE SENT. Jira's Medium arrives
// on the job as p2 marked set by the source ([inbox.SetBySource]), which is
// not a person's mark; a rule that says p0 is the factory deciding, so the job
// runs at p0. Only a person's own mark keeps its priority (the next test).
// Found end to end: a Jira ticket on "Small Jira bugs" (p1) stayed at Medium.
func TestARulesPriorityWinsOverTheSources(t *testing.T) {
	r, job := routedFloor(t, []route.Rule{jiraRule(route.StartAuto)})
	if err := r.st.Update(job.ID, func(cur *factory.Item) error {
		cur.Priority = &factory.JobPriority{Value: "medium", SetBy: inbox.SetBySource}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := r.in.Once(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, err := r.st.Get(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Priority == nil || got.Priority.Value != "urgent" {
		t.Fatalf("routed job's priority is %+v, want the rule's p0 (urgent) over the source's medium", got.Priority)
	}
}

// A PERSON'S PRIORITY MARK STILL WINS over a rule's.
func TestAPersonsPriorityWinsOverARules(t *testing.T) {
	r, job := routedFloor(t, []route.Rule{jiraRule(route.StartAuto)})
	if err := r.st.Update(job.ID, func(cur *factory.Item) error {
		cur.Priority = &factory.JobPriority{Value: "low", SetBy: "ann"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := r.in.Once(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, err := r.st.Get(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Priority == nil || got.Priority.Value != "low" || got.Priority.SetBy != "ann" {
		t.Fatalf("routed job's priority is %+v, want ann's low kept", got.Priority)
	}
}
