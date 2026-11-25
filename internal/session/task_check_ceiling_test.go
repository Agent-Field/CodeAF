package session

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/plandb"
)

// run19Ceiling is the sentence run19's check stopped on, spelled by the guard's
// own format so the test cannot drift from what the guard says.
var run19Ceiling = strings.Replace(config.CrewCheckCeilingAction, "%.2f", "0.37", 1)

// uncheckedRunFixture is a run whose worker finished and whose check met its
// own spend ceiling, driven through the real belt door: the plan store holds
// the completed root (internal/run completes it once the check ended as a
// non-answer), and the engine answers done with the stop carried typed.
func uncheckedRunFixture(t *testing.T) (*Agent, uint64) {
	t.Helper()
	t.Setenv("CODEAF_TASK_BELT", "bash")
	place := t.TempDir()
	client := &scriptedCompleter{steps: []step{finalText("what: vector brief\nsince: it was built\nnow: The brief is on the canvas.\nnext: Nothing.")}}
	agent, _ := newTestAgent(t, client, func(c *Config) {
		c.Place = Place{Dir: place}
		c.AskConsent = false
	})
	g := agent.graph()
	id := g.reserve()
	store, err := plandb.Open(g.planPath(), "run19", strconv.FormatUint(id, 10), "Vector database research brief", "deep research brief on vector dbs", g.planChat())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	result := "Built the vector-DB research brief on canvas cv_xuxcbjkotc5c"
	if err := store.CompleteRoot(result); err != nil {
		t.Fatal(err)
	}
	agent.publishRunRow(g, TaskNotice{ID: id, Title: "Vector database research brief", State: TaskRunning, StartedAt: time.Now().Add(-10 * time.Minute), PlanTask: planStoreID(strconv.FormatUint(id, 10))})
	run := &beltRun{store: store, root: strconv.FormatUint(id, 10), row: id, title: "Vector database research brief"}
	agent.driveBeltRun(context.Background(), landingRunDouble{
		summary: RunSummary{Outcome: beltRunOutcomeDone, Result: result, Unchecked: run19Ceiling, USD: 0.58},
	}, run, RunSpec{})
	return agent, id
}

// THE RUN19 ROW: the worker built the brief, the check stopped at its spend
// ceiling, and the person read "Vector database research brief · failed". The
// row now asks the check's question — your call — led by the stop itself, and
// it is never drawn failed and never drawn done.
func TestFinishedWorkWhoseCheckMetItsCeilingIsYourCallNotFailed(t *testing.T) {
	agent, id := uncheckedRunFixture(t)
	kept, found := runRowOf(agent.graph(), id)
	if !found {
		t.Fatal("the run's row is gone")
	}
	if kept.State != TaskUnverified || !kept.Unchecked {
		t.Fatalf("row = %s unchecked=%v (%q), want your call over landed work", kept.State, kept.Unchecked, kept.Report)
	}
	if !strings.HasPrefix(kept.Report, run19Ceiling) || !strings.Contains(kept.Report, "Built the vector-DB research brief") {
		t.Fatalf("report = %q, want the stop as its lead and the work's own account under it", kept.Report)
	}
	project := ProjectTask(kept.StatusFacts())
	if project.Word != taskWordYourCall {
		t.Fatalf("person reads %q, want %q", project.Word, taskWordYourCall)
	}
	if project.Ask.Kind != TaskAskCheck || project.Ask.Reason != taskAskCeilingReason || project.Ask.Yes != taskAskCheckYes || project.Ask.No != taskAskCheckNo {
		t.Fatalf("ask = %+v, want the check's question for a check that met its ceiling", project.Ask)
	}
	if strings.Contains(strings.ToLower(project.Word+" "+project.Ask.Reason), "fail") {
		t.Fatalf("finished work still reads as failed: %+v", project)
	}
	if got := len(agent.PendingDecisions()); got != 1 {
		t.Fatalf("pending decisions = %d, want the one question on this landing", got)
	}
}

// Accept takes the landed work as done, and nothing lands a second time.
func TestAcceptingWorkWhoseCheckMetItsCeilingSettlesItDone(t *testing.T) {
	agent, id := uncheckedRunFixture(t)
	if err := agent.ResolveUnverified(id, TaskAccept, "I read the brief"); err != nil {
		t.Fatal(err)
	}
	kept, _ := runRowOf(agent.graph(), id)
	if kept.State != TaskDone || kept.Unchecked || !strings.Contains(kept.Report, "I read the brief") || !strings.Contains(kept.Report, "Built the vector-DB research brief") {
		t.Fatalf("accepted row = %s unchecked=%v %q, want done with the person's word and the work's account", kept.State, kept.Unchecked, kept.Report)
	}
	if got := len(agent.PendingDecisions()); got != 0 {
		t.Fatalf("pending decisions after accept = %d, want none", got)
	}
}

// Not right is the person's word on the work, and the check is never asked
// again on its own: that would spend its ceiling a second time.
func TestRefusingOrRecheckingWorkWhoseCheckMetItsCeiling(t *testing.T) {
	agent, id := uncheckedRunFixture(t)
	if err := agent.ResolveUnverified(id, TaskReaudit, ""); err == nil || !strings.Contains(err.Error(), "spend ceiling") {
		t.Fatalf("check again = %v, want a refusal naming the ceiling", err)
	}
	if err := agent.ResolveUnverified(id, TaskRefute, "the cost chart is empty"); err != nil {
		t.Fatal(err)
	}
	kept, _ := runRowOf(agent.graph(), id)
	if kept.State != TaskFailed || kept.Ending != TaskEndingRefused || kept.Unchecked || !strings.Contains(kept.Report, "the cost chart is empty") {
		t.Fatalf("refused row = %s %s unchecked=%v %q", kept.State, kept.Ending, kept.Unchecked, kept.Report)
	}
}

// A CHECK THAT DID ANSWER is not this road: a done run with no stop on it is
// done, exactly as before.
func TestADoneRunWithACheckThatAnsweredStaysDone(t *testing.T) {
	run := &beltRun{title: "brief"}
	state, report := run.noticeResultWords(RunSummary{Outcome: beltRunOutcomeDone, Result: "built"}, RunLanding{})
	if state != TaskDone || report != "built" {
		t.Fatalf("answered run = %s %q, want done", state, report)
	}
}
