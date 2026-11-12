package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/crewroute"
	"github.com/Agent-Field/codeaf/internal/delegate"
	"github.com/Agent-Field/codeaf/internal/plandb"
)

// L-LOOP C1a, C1b: the real conversation proposal door hands the run one job.
func TestProposedRunWorkersReadTheirOwnWorkBeforeTheConversationAsk(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	double := newBeltRunDouble("green was created")
	registerBeltRunEngine(t, double)
	completer := &routedCompleter{parent: []step{
		proposeCall("Create green.txt", "Create green.txt with the word green"),
		finalText("handed off"),
	}}
	agent, _ := newTestAgent(t, completer, func(c *Config) {
		c.Workspace = newTestRepo(t)
		c.Place = Place{Dir: t.TempDir()}
		c.ProfileDir = t.TempDir()
		c.AskConsent = false
		c.TaskAutoApproveSeconds = 0
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	const ask = "call propose_task three times: Create red.txt, Create green.txt, Create blue.txt"
	events, err := agent.Submit(ctx, ask)
	if err != nil {
		t.Fatal(err)
	}
	collect(t, events)
	select {
	case <-double.entered:
	case <-ctx.Done():
		t.Fatal("proposal never reached the run engine")
	}
	defer endBeltRun(t, agent, double)
	double.mu.Lock()
	spec := double.spec
	double.mu.Unlock()
	root := spec.Store.Task(spec.Store.RootID())
	if root == nil || root.Title != "Create green.txt" {
		t.Fatalf("the proposed work was not the run's root: %+v", root)
	}
	if spec.CostUSD != config.CrewTaskCapDefault {
		t.Fatalf("proposal's RunSpec has no per-task ceiling: %v", spec.CostUSD)
	}
	assertWorkerDocument := func(doc, work string) {
		t.Helper()
		workAt, askAt := strings.Index(doc, work), strings.Index(doc, ask)
		if workAt < 0 || askAt < 0 || askAt <= workAt || strings.Contains(doc, briefAskRule) {
			t.Fatalf("worker read the conversation's handoff as its own job:\n%s", doc)
		}
		for _, want := range []string{
			"the message the conversation was answering when it proposed this task",
			"It was addressed to the conversation",
			"anything it asks about proposing, starting or handing out tasks is the conversation's and already done, not yours to do again",
			"This task is the work above",
			"The person's words still govern THIS task",
			"where the work above reads differently from them about this task, theirs win",
			"Say any conflict in your report rather than widening the work",
		} {
			if !strings.Contains(doc, want) {
				t.Errorf("worker document no longer says %q:\n%s", want, doc)
			}
		}
	}
	for _, doc := range []string{spec.Brief, root.Description, BeltWorkerBrief(spec.Store, root, true, false, "", "")} {
		assertWorkerDocument(doc, "Create green.txt with the word green")
	}
	if !strings.HasPrefix(root.Description, briefWorkHeading+"\n\n") {
		t.Fatalf("proposal does not open on its work:\n%s", root.Description)
	}
	children, err := spec.Store.AddMany([]plandb.TaskSpec{{ID: "part", Title: "Write green bytes", Description: "Write green into green.txt"}})
	if err != nil {
		t.Fatal(err)
	}
	childDoc := BeltWorkerBrief(spec.Store, children[0], false, false, "", "")
	assertWorkerDocument(childDoc, "Write green into green.txt")
}

// L-LOOP C1c: a person-authored whole-job brief keeps its exact earlier bytes.
func TestPersonAuthoredWholeBriefKeepsItsExactDocument(t *testing.T) {
	const work = "Create green.txt"
	const want = "WHAT THE PERSON ASKED FOR, IN THEIR OWN WORDS\nThis is the message this work came out of. Where anything below reads differently from it, their words are what was asked for.\n\nCreate green.txt"
	if got := composeBrief(briefWhole, work, work, "", "", "", AdmissionContext{}, taskOrigin{}, taskCopy{}); got != want {
		t.Fatalf("person-authored task brief changed:\n%q", got)
	}
}

// L-LOOP C4a: the shared per-task setting caps an ordinary RunSpec, while a
// delegated program keeps its existing independent ceiling.
func TestConversationRunSpecsHaveATreeSpendCeiling(t *testing.T) {
	for _, tc := range []struct {
		name    string
		limit   float64
		spent   float64
		taskCap string
		program *delegate.Delegate
		want    float64
	}{
		{name: "default", want: config.CrewTaskCapDefault},
		{name: "configured", taskCap: "2.5", want: 2.5},
		{name: "conversation", limit: 3, spent: 1, want: 2},
		{name: "task", limit: 12, spent: 1, want: config.CrewTaskCapDefault},
		{name: "program", program: &delegate.Delegate{Unattended: delegate.Ceilings{CostUSD: 9}}, want: 9},
		{name: "program without ceiling", program: &delegate.Delegate{}, want: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			agent, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
				c.ProfileDir = t.TempDir()
				c.Interactive = true
				c.Budget = Budget{USD: tc.limit}
			})
			if tc.taskCap != "" {
				if err := config.SetCrewTaskCap(agent.config.ProfileDir, tc.taskCap); err != nil {
					t.Fatal(err)
				}
			}
			agent.usage.CostUSD = tc.spent
			run := &beltRun{delegate: tc.program}
			if got := agent.beltRunSpec(run, "the work").CostUSD; got != tc.want {
				t.Fatalf("run cost ceiling = %v, want %v", got, tc.want)
			}
		})
	}
}

// L-LOOP C4b: the existing report, outcome note and ending carry the stop.
func TestPerTaskTreeLimitNamesTheLimitAndHowToRaiseIt(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
		c.ProfileDir = t.TempDir()
		c.Interactive = true
	})
	run := &beltRun{}
	agent.holdProgramCeilings(run, agent.beltRunSpec(run, "the work"))
	summary := RunSummary{Outcome: "a limit you set stopped it", Limit: RunLimitCost}
	want := config.CrewTaskCapAction(config.CrewTaskCapDefault)
	if notice := agent.beltRunNotice(run, summary, RunLanding{}); notice.Ending != TaskEndingCostLimit || !strings.Contains(notice.Report, want) {
		t.Fatalf("task-limit row lost its ending or action: %+v", notice)
	}
	if note := run.outcomeNote(summary, RunLanding{}, 0); !strings.Contains(note, want) {
		t.Fatalf("task-limit outcome note lost the action: %s", note)
	}
	agent.config.Budget = Budget{USD: 1}
	agent.holdProgramCeilings(run, agent.beltRunSpec(run, "the work"))
	if got := run.taskLimitWords(summary); got != "" {
		t.Fatalf("conversation's smaller limit was called the per-task limit: %s", got)
	}
}

// L-LOOP C4c: both guard branches carry the typed task stop through stopCrew;
// a checker ceiling remains local to that seat.
func TestTaskSpendStopKeepsItsScopeThroughTheCrewError(t *testing.T) {
	for _, priced := range []bool{false, true} {
		t.Run(fmt.Sprint(priced), func(t *testing.T) {
			guard := &SpendGuard{TaskCap: 1, TaskAction: "task limit", Task: &SpendTask{}}
			if priced {
				guard.Price = func(string) (float64, float64, float64, bool) { return 0, 1, 0, true }
			} else {
				guard.Task.settle(0, 1)
			}
			_, err := guard.before(t.Context(), "test/model", []ai.Message{textMessage("user", "work")}, nil)
			agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
			run := &beltRun{crew: &taskCrew{}}
			var stopped ErrSpendStopped
			if !errors.As(agent.stopCrew(run, err), &stopped) || !stopped.TaskLimit {
				t.Fatalf("task stop lost its scope: %v", err)
			}
		})
	}
	guard := &SpendGuard{
		Price:   func(string) (float64, float64, float64, bool) { return 0, 1, 0, true },
		TaskCap: 5, SeatCeilings: map[crewroute.Seat]float64{crewroute.Checker: .01}, CeilingAction: "checker $%.2f",
	}
	_, err := guard.before(context.WithValue(t.Context(), crewSeatContextKey{}, crewroute.Checker), "test/model", nil, nil)
	var stopped ErrSpendStopped
	if !errors.As(err, &stopped) || stopped.TaskLimit {
		t.Fatalf("a seat ceiling became a tree stop: %v", err)
	}
}

// L-LOOP C5a, C5b, C5c: the expiry fact crosses the real wait and commit;
// an explicit approval and a typed task retain their person-approved join.
func TestSilenceCannotGrowALiveRunPastItsBounds(t *testing.T) {
	for _, tc := range []struct {
		name, title string
		full, click bool
	}{
		{name: "duplicate", title: "Open the run"},
		{name: "full", title: "Different work", full: true},
		{name: "approved full", title: "Different work", full: true, click: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("CODEAF_TASK_BELT", "bash")
			double := newBeltRunDouble("done")
			registerBeltRunEngine(t, double)
			agent, _ := newTestAgent(t, beltRunCompleter{text: "done"}, func(c *Config) {
				c.Workspace = newTestRepo(t)
				c.Place = Place{Dir: t.TempDir()}
				c.ProfileDir = t.TempDir()
				c.AskConsent = false
			})
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if _, _, _, err := agent.StartTask(ctx, "Open the run", false); err != nil {
				t.Fatal(err)
			}
			<-double.entered
			defer endBeltRun(t, agent, double)
			double.mu.Lock()
			store := double.spec.Store
			double.mu.Unlock()
			if tc.full {
				for i := 0; i < plandb.TreeTaskLimit; i++ {
					if _, err := store.AddMany([]plandb.TaskSpec{{ID: fmt.Sprintf("part%d", i), Title: fmt.Sprintf("Part %d", i)}}); err != nil {
						t.Fatal(err)
					}
				}
			}
			before := store.Tasks()
			expiry := make(chan time.Time, 1)
			agent.taskTimer = func(time.Duration) (<-chan time.Time, func()) { return expiry, func() {} }
			args, _ := json.Marshal(taskArguments{Title: tc.title, Summary: "the work", Brief: "Do the different work", Deliverable: "the output", Acceptance: "the output exists"})
			proposal, ok := agent.stageTask(ctx, args).(*stagedProposal)
			if !ok {
				t.Fatal("proposal was not staged")
			}
			if tc.click {
				agent.ResolveTask(proposal.id, TaskAnswer{Approved: true})
			} else {
				expiry <- time.Now()
			}
			answer, refused, err := proposal.Commit(ctx)
			if err != nil || refused == tc.click || proposal.wait.silent == tc.click {
				t.Fatalf("join: refused=%v silent=%v err=%v answer=%s", refused, proposal.wait.silent, err, answer)
			}
			if tc.click {
				if len(store.Tasks()) != len(before)+1 {
					t.Fatal("person-approved proposal did not join at the count limit")
				}
				if _, _, _, err := agent.StartTask(ctx, "One more typed task", false); err != nil {
					t.Fatalf("typed task could not join: %v", err)
				}
			} else if !reflect.DeepEqual(before, store.Tasks()) || !strings.Contains(answer, "nothing was added") || agent.graph().node(proposal.id) != nil {
				t.Fatalf("silent refusal changed the store or fell back to a session node: %s", answer)
			}
			if !tc.click {
				row, found := runRowOf(agent.graph(), proposal.id)
				if !found || row.State != TaskFailed || !strings.Contains(row.Report, "did not start") {
					t.Fatalf("refused proposal was left as a starting card: %+v", row)
				}
			}
		})
	}
}
