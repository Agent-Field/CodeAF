package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/crewroute"
	"github.com/Agent-Field/codeaf/internal/delegate"
	"github.com/Agent-Field/codeaf/internal/plandb"
)

// L-LOOP2 C1a: a countdown proposal carries only its own assignment.
func TestProposedRunWorkersReadTheirOwnWorkBeforeTheConversationAsk(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	double := newBeltRunDouble("red was created")
	registerBeltRunEngine(t, double)
	completer := &routedCompleter{parent: []step{
		proposeCall("Create red.txt", "Create red.txt with the word red"),
		finalText("handed off"),
	}}
	agent, _ := newTestAgent(t, completer, func(c *Config) {
		c.Workspace = newTestRepo(t)
		c.Place = Place{Dir: t.TempDir()}
		c.ProfileDir = t.TempDir()
		c.SessionFile = filepath.Join(t.TempDir(), "conversation.jsonl")
		c.AskConsent = false
		c.TaskAutoApproveSeconds = 0
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	const ask = "call propose_task three times: Create red.txt, Create green.txt, Create blue.txt — propose them, don't do them"
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
	if root == nil || root.Title != "Create red.txt" {
		t.Fatalf("the proposed work was not the run's root: %+v", root)
	}
	if spec.CostUSD != config.CrewTaskCapDefault {
		t.Fatalf("proposal's RunSpec has no per-task ceiling: %v", spec.CostUSD)
	}
	assertWorkerDocument := func(doc, work string) {
		t.Helper()
		for _, forbidden := range []string{"propose_task", "Create green.txt", "Create blue.txt", "three times", agent.config.SessionFile} {
			if forbidden != "" && strings.Contains(doc, forbidden) {
				t.Errorf("worker document contains conversation context %q:\n%s", forbidden, doc)
			}
		}
		for _, want := range []string{"Create red.txt", "Create red.txt with the word red", work} {
			if !strings.Contains(doc, want) {
				t.Errorf("worker document is missing its assignment %q:\n%s", want, doc)
			}
		}
	}
	for _, doc := range []string{spec.Brief, root.Description, BeltWorkerBrief(spec.Store, root, true, false, "", "")} {
		assertWorkerDocument(doc, "Create red.txt with the word red")
	}
	children, err := spec.Store.AddMany([]plandb.TaskSpec{{ID: "part", Title: "Write red bytes", Description: "Write red into red.txt"}})
	if err != nil {
		t.Fatal(err)
	}
	childDoc := BeltWorkerBrief(spec.Store, children[0], false, false, "", "")
	assertWorkerDocument(childDoc, "Write red into red.txt")
	if !strings.Contains(strings.SplitN(childDoc, "THE WORK", 2)[0], "Write red bytes") {
		t.Fatalf("worker opening omits its task title:\n%s", childDoc)
	}
}

// L-LOOP2 C1b, C6: a person-authored whole-job brief keeps its exact earlier bytes.
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

// A RAISE THE PERSON GAVE IS THE TREE'S CEILING. A task whose cap was raised
// before it started — by a proposal's raise_cap_usd, routed into its crew's
// guard — runs under the raised figure, and its ending names that figure,
// with and without a routed crew.
func TestARaisedProposalRunsUnderItsRaisedTreeCeiling(t *testing.T) {
	for _, routed := range []bool{true, false} {
		t.Run(fmt.Sprint("routed=", routed), func(t *testing.T) {
			useTaskUsageHome(t)
			agent, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
				c.ProfileDir = t.TempDir()
				c.Interactive = true
				if routed {
					c.RouteCrew = func(config.CrewAsk) (crewroute.Decision, error) { return liveDecision(), nil }
				}
			})
			const id = 3
			if err := agent.actOnProposal(id, TaskRoutingAction{Kind: TaskRoutingAuto, RaiseCapUSD: 8}); err != nil {
				t.Fatal(err)
			}
			ctx := withCrewWish(t.Context(), agent.proposalCrewWish(id, crewWish{}))
			run := &beltRun{row: id}
			if routed {
				crew, err := agent.routeTaskCrew(ctx, id, "Update fixtures", "update every fixture")
				if err != nil {
					t.Fatal(err)
				}
				if crew.guard.TaskCap != 8 {
					t.Fatalf("the crew's guard did not take the raise: %v", crew.guard.TaskCap)
				}
				run.crew = crew
			}
			run.wishedFrom(ctx)
			spec := agent.beltRunSpec(run, "the work")
			if spec.CostUSD != 8 {
				t.Fatalf("a task raised to $8 runs under a $%v tree ceiling", spec.CostUSD)
			}
			summary := RunSummary{Outcome: runLimitSentence, Limit: RunLimitCost}
			if got, want := run.taskLimitWords(summary), config.CrewTaskCapAction(8); got != want {
				t.Fatalf("the raised task's ending says %q, want %q", got, want)
			}
		})
	}
}

// A RAISE GIVEN WHILE THE TASK RUNS LIFTS THE RUNNING TREE TOO. The live
// change raises the crew's guard and the ceiling the engine was handed when
// the run started, so the run is not stopped at the old figure, and the
// ending names the figure the person raised it to.
func TestALiveRaiseLiftsTheRunningTreeCeiling(t *testing.T) {
	useTaskUsageHome(t)
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
		c.ProfileDir = t.TempDir()
		c.Interactive = true
		c.RouteCrew = func(config.CrewAsk) (crewroute.Decision, error) {
			d := liveDecision()
			for i := range d.Crew {
				d.Crew[i].Model, d.Crew[i].Send, d.Crew[i].EstUSD = "opus-"+string(d.Crew[i].Seat), "opus-"+string(d.Crew[i].Seat), 3
			}
			return d, nil
		}
	})
	crew := liveCrew(config.CrewTaskCapDefault)
	crew.guard.TaskAction = config.CrewTaskCapAction(config.CrewTaskCapDefault)
	run := &beltRun{row: 1, crew: crew}
	spec := agent.beltRunSpec(run, "the work")
	if spec.CostUSD != config.CrewTaskCapDefault {
		t.Fatalf("the run started under $%v, want the $5 cap", spec.CostUSD)
	}
	agent.beltMu.Lock()
	agent.beltRun = run
	agent.beltMu.Unlock()
	defer func() {
		agent.beltMu.Lock()
		agent.beltRun = nil
		agent.beltMu.Unlock()
	}()
	if _, err := agent.actOnLiveCrew(1, crew, TaskRoutingAction{Kind: TaskRoutingBest, RaiseCapUSD: 9}); err != nil {
		t.Fatal(err)
	}
	want := config.CrewTaskCapAction(9)
	if crew.guard.TaskCap != 9 || crew.guard.TaskAction != want {
		t.Fatalf("the guard holds $%v and says %q after a raise to $9", crew.guard.TaskCap, crew.guard.TaskAction)
	}
	summary := RunSummary{Outcome: runLimitSentence, Limit: RunLimitCost}
	if got := run.taskLimitWords(summary); got != want {
		t.Fatalf("a run raised to $9 ends saying %q, want %q", got, want)
	}
	if spec.CostCeiling == nil || spec.CostCeiling() != 9 {
		t.Fatal("the engine's ceiling did not rise with the raise")
	}
}

// THE PER-TASK LIMIT IS SAID ONCE, WHERE THE PERSON LOOKS. A run its tree's
// dollar ceiling ended leads the row's report and the outcome note with the
// limit's sentence, and carries it as the crew's stop — the line under the row
// and the task's place in the Tasks column. Whether the engine's ceiling cut
// the run with no call refused (the hand test: the root had written its result
// and the outcome word was the generic one) or the crew's guard refused the
// root's own call (which made the same sentence the worker's failure), the
// sentence is said exactly once in each.
func TestAPerTaskLimitEndingSaysItsSentenceOnceWhereThePersonLooks(t *testing.T) {
	want := config.CrewTaskCapAction(config.CrewTaskCapDefault)
	for _, tc := range []struct {
		name    string
		summary RunSummary
	}{
		{"the engine's ceiling cut it", RunSummary{Outcome: "ran and did not finish", Result: "Wrote a.txt, b.txt and c.txt", Limit: RunLimitCost}},
		{"the guard refused the root", RunSummary{Outcome: runLimitSentence, Failure: want, Limit: RunLimitCost}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("CODEAF_TASK_BELT", "bash")
			useTaskUsageHome(t)
			double := newBeltRunDouble("")
			double.summary = tc.summary
			double.leaveOpen = true
			registerBeltRunEngine(t, double)
			dir := t.TempDir()
			agent, _ := newTestAgent(t, beltRunCompleter{text: "Wrote a.txt, b.txt and c.txt"}, func(c *Config) {
				c.Workspace = newTestRepo(t)
				c.Place = Place{Dir: dir}
				c.SessionFile = filepath.Join(dir, placeTranscript)
				c.ProfileDir = t.TempDir()
				c.AskConsent = false
				c.RouteCrew = func(config.CrewAsk) (crewroute.Decision, error) { return liveDecision(), nil }
			})
			id, _, _, err := agent.StartTask(context.Background(), "Write three files a.txt, b.txt and c.txt", false)
			if err != nil {
				t.Fatal(err)
			}
			<-double.entered
			endBeltRun(t, agent, double)
			row, found := runRowOf(agent.graph(), id)
			if !found || row.Ending != TaskEndingCostLimit || strings.Count(row.Report, want) != 1 || !strings.HasPrefix(row.Report, want) {
				t.Fatalf("the row's report does not lead with the limit exactly once:\n%s\n%+v", row.Report, row)
			}
			if row.Crew == nil || row.Crew.Stopped != want {
				t.Fatalf("the row's crew does not stop on the limit, so nothing draws it under the row: %+v", row.Crew)
			}
			store := beltRunStoreAt(t, dir)
			defer store.Close()
			said, led := 0, false
			for _, note := range store.Notes(strconv.FormatUint(id, 10), 0) {
				said += strings.Count(note.Body, want)
				led = led || strings.HasPrefix(note.Body, want+" · ")
			}
			if said != 1 || !led {
				t.Fatalf("the outcome note says the limit %d times, leading=%v: %+v", said, led, store.Notes(strconv.FormatUint(id, 10), 0))
			}
		})
	}
}

// A run that FINISHED whole is not a limit ending, even when its final receipt
// crossed the tree's ceiling (the engine ends such a run done with Limit cost;
// internal/run TestFinishedChildlessRunAtCostLimitStillEndsDone). Its crew
// keeps no limit stop, so its completion is announced, "run again with a
// stronger crew" stays offered, and neither its report nor its outcome note
// says the limit — as on dev.
func TestAFinishedRunAtItsCeilingIsNoPerTaskLimitStop(t *testing.T) {
	want := config.CrewTaskCapAction(config.CrewTaskCapDefault)
	t.Setenv("CODEAF_TASK_BELT", "bash")
	useTaskUsageHome(t)
	double := newBeltRunDouble("")
	double.summary = RunSummary{Outcome: beltRunOutcomeDone, Result: "Wrote a.txt, b.txt and c.txt", Limit: RunLimitCost}
	double.leaveOpen = true
	registerBeltRunEngine(t, double)
	dir := t.TempDir()
	agent, _ := newTestAgent(t, beltRunCompleter{text: "Wrote a.txt, b.txt and c.txt"}, func(c *Config) {
		c.Workspace = newTestRepo(t)
		c.Place = Place{Dir: dir}
		c.SessionFile = filepath.Join(dir, placeTranscript)
		c.ProfileDir = t.TempDir()
		c.AskConsent = false
		c.RouteCrew = func(config.CrewAsk) (crewroute.Decision, error) { return liveDecision(), nil }
	})
	id, _, _, err := agent.StartTask(context.Background(), "Write three files a.txt, b.txt and c.txt", false)
	if err != nil {
		t.Fatal(err)
	}
	<-double.entered
	endBeltRun(t, agent, double)
	row, found := runRowOf(agent.graph(), id)
	if !found {
		t.Fatal("the run's row is gone")
	}
	if row.Crew != nil && row.Crew.Stopped != "" {
		t.Fatalf("a finished run's crew was stopped on %q, so its completion is never announced", row.Crew.Stopped)
	}
	if strings.Contains(row.Report, want) {
		t.Fatalf("a finished run's report says the per-task limit:\n%s", row.Report)
	}
	store := beltRunStoreAt(t, dir)
	defer store.Close()
	for _, note := range store.Notes(strconv.FormatUint(id, 10), 0) {
		if strings.Contains(note.Body, want) {
			t.Fatalf("a finished run's outcome note says the per-task limit: %s", note.Body)
		}
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
