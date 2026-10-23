package session

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/plandb"
)

func completedRecoveryFixture(t *testing.T) (*Agent, string, *TaskCopyRecord, *beltRunDouble, uint64, time.Time, func() *Agent) {
	return completedRecoveryFixtureWithSpend(t, 0.42, 0)
}

func completedRecoveryFixtureWithSpend(t *testing.T, checkpointCost, ledgerCost float64, checkerCost ...float64) (*Agent, string, *TaskCopyRecord, *beltRunDouble, uint64, time.Time, func() *Agent) {
	t.Helper()
	t.Setenv("CODEAF_TASK_BELT", "bash")
	engine := newBeltRunDouble("must not run")
	registerBeltRunEngine(t, engine)
	place, repo := t.TempDir(), newTestRepo(t)
	open := func() *Agent {
		a, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
			c.Place, c.Workspace, c.SessionFile = Place{Dir: place}, repo, filepath.Join(place, placeTranscript)
			c.AskConsent = false
		})
		return a
	}
	first := open()
	g := first.graph()
	id := g.reserve()
	tree, err := prepareTaskTree(first.config.Place, repo, "1111cccc1111cccc", id, "mock completed counter")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tree.dir, "counter.txt"), []byte("mock completed work\n"), 0600); err != nil {
		t.Fatal(err)
	}
	store, err := plandb.Open(g.planPath(), "mock", strconv.FormatUint(id, 10), "mock completed counter", "write counter", g.planChat())
	if err != nil {
		t.Fatal(err)
	}
	if ledgerCost > 0 {
		if err := store.AddSpend(store.RootID(), "mock/model", "work", ledgerCost, 100, 25); err != nil {
			t.Fatal(err)
		}
	}
	if len(checkerCost) > 0 {
		if _, err := store.AddMany([]plandb.TaskSpec{{ID: "mock-checker", Title: "Mock checker", Description: "check completed counter", ParentID: store.RootID()}}); err != nil {
			t.Fatal(err)
		}
		if _, err := store.Claim("mock-checker", "mock-checker"); err != nil {
			t.Fatal(err)
		}
		if _, err := store.Done("mock-checker", "mock-checker", "mock check passed", nil, nil); err != nil {
			t.Fatal(err)
		}
		if err := store.AddSpend("mock-checker", "mock/model", "check", checkerCost[0], 50, 10); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.CompleteRoot("mock test passed; counter ready"); err != nil {
		t.Fatal(err)
	}
	root := store.Task(store.RootID())
	_ = store.Close()
	copy := runCopyOf(tree)
	first.publishRunRow(g, TaskNotice{ID: id, Title: "mock completed counter", State: TaskRunning, Copy: copy, CrewState: first.unroutedCrewRecord(), CostUSD: checkpointCost, StartedAt: time.Now().Add(-time.Minute), PlanTask: planStoreID(strconv.FormatUint(id, 10))})
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	return open(), repo, copy, engine, id, root.CompletedAt, open
}

func TestCompletedPlanRecoveryOffersItsSavedWorkWithoutReplayingOrMerging(t *testing.T) {
	second, repo, copy, engine, id, ended, open := completedRecoveryFixture(t)
	nothingStarted(t, second, engine)
	kept, found := runRowOf(second.graph(), id)
	if !found || kept.State != TaskUnverified || kept.Copy == nil || kept.Copy.Branch != copy.Branch || kept.CostUSD != 0.42 || kept.CrewState == nil || !kept.EndedAt.Equal(ended) {
		t.Fatalf("completed plan stranded or changed on restart: %+v", kept)
	}
	if _, err := os.Stat(filepath.Join(repo, "counter.txt")); !os.IsNotExist(err) {
		t.Fatal("restart merged work without acceptance")
	}
	if len(second.PendingDecisions()) != 1 {
		t.Fatal("recovered task has no replayable landing question")
	}
	page, ok := second.PlanTaskPage(planStoreID(strconv.FormatUint(id, 10)))
	if !ok || page.Row.StateWord() != taskWordYourCall || page.Row.USD != 0.42 || !strings.Contains(page.Row.Note, completedRunReview) {
		t.Fatalf("recovered page hides its review or spend: %+v", page.Row)
	}
	if err := second.Close(); err != nil {
		t.Fatal(err)
	}
	second = open()
	questions := second.OpenQuestions()
	if len(questions) != 1 || questions[0].ID != id || questions[0].Kind != QuestionLanding || len(questions[0].Options) != 2 {
		t.Fatalf("second restart lost actionable explicit review: %+v", questions)
	}
	for _, option := range questions[0].Options {
		if option.Key != LandingYesKey && option.Key != LandingNoKey {
			t.Fatalf("dead recovered control: %+v", option)
		}
	}
	args, _ := json.Marshal(map[string]any{"id": strconv.FormatUint(id, 10), "resolve": "accept", "say": "I inspected the mock counter"})
	reply, bad, err := second.tasksTool().Execute(context.Background(), args)
	if err != nil || bad || strings.Contains(reply, "No task") {
		t.Fatalf("completed run has no working accept door: %s bad=%v err=%v", reply, bad, err)
	}
	data, err := os.ReadFile(filepath.Join(repo, "counter.txt"))
	if err != nil || string(data) != "mock completed work\n" {
		t.Fatalf("accepted work did not land: %q %v (%s)", data, err, reply)
	}
	landed, _ := runRowOf(second.graph(), id)
	if landed.State != TaskDone || landed.CostUSD != 0.42 || landed.Copy == nil || landed.Copy.Branch != copy.Branch {
		t.Fatalf("accept lost completed run identity/spend: %+v", landed)
	}
	nothingStarted(t, second, engine)
}

func TestCompletedPlanRecoveryRefutesWithoutTouchingItsSavedBranch(t *testing.T) {
	agent, repo, copy, engine, id, _, _ := completedRecoveryFixture(t)
	before := runTreeHead(repo)
	if err := agent.ResolveUnverified(id, TaskRefute, "mock work is not right"); err != nil {
		t.Fatal(err)
	}
	kept, _ := runRowOf(agent.graph(), id)
	if kept.State != TaskFailed || kept.Ending != TaskEndingRefused || kept.CostUSD != 0.42 || !kept.RecoveredCompletion || runTreeHead(repo) != before {
		t.Fatalf("refute lost actual state or changed main: %+v", kept)
	}
	if data, err := os.ReadFile(filepath.Join(copy.Dir, "counter.txt")); err != nil || string(data) != "mock completed work\n" {
		t.Fatalf("refute discarded work: %q %v", data, err)
	}
	if len(agent.PendingDecisions()) != 0 {
		t.Fatal("refuted run kept asking")
	}
	nothingStarted(t, agent, engine)
}

func TestCompletedPlanRecoveryRejectsChangedBranchAndUnavailableChecker(t *testing.T) {
	agent, repo, copy, engine, id, _, _ := completedRecoveryFixture(t)
	before := runTreeHead(repo)
	if err := agent.ResolveUnverified(id, TaskReaudit, ""); err == nil || !strings.Contains(err.Error(), "without a final check receipt") {
		t.Fatalf("recovery claimed a checker it cannot run: %v", err)
	}
	mustGit(t, copy.Dir, "checkout", "-b", "mock-unrelated-branch")
	if err := agent.ResolveUnverified(id, TaskAccept, ""); err == nil || !strings.Contains(err.Error(), "different branch") {
		t.Fatalf("accepted different saved branch: %v", err)
	}
	if runTreeHead(repo) != before || len(agent.PendingDecisions()) != 1 {
		t.Fatal("refused accept changed work or dismissed review")
	}
	nothingStarted(t, agent, engine)
}

func TestCompletedPlanRecoveryRejectsWorkerResolutionOfTheSharedParent(t *testing.T) {
	owner, repo, _, engine, id, _, _ := completedRecoveryFixture(t)
	before := runTreeHead(repo)
	worker, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) { c.InTask = true; c.taskID = 999; c.tasker = owner.graph(); c.Workspace = repo })
	args, _ := json.Marshal(map[string]any{"id": strconv.FormatUint(id, 10), "resolve": "accept"})
	reply, bad, err := worker.tasksTool().Execute(context.Background(), args)
	if err != nil || !bad {
		t.Fatalf("worker accepted parent's review: %s bad=%v err=%v", reply, bad, err)
	}
	kept, _ := runRowOf(owner.graph(), id)
	if kept.State != TaskUnverified || runTreeHead(repo) != before {
		t.Fatal("scoped worker changed parent's work")
	}
	nothingStarted(t, owner, engine)
}

func TestCompletedPlanRecoverySerializesStopWithMainMergeAdmission(t *testing.T) {
	for _, afterAdmission := range []bool{false, true} {
		name := "stop-before-admission"
		if afterAdmission {
			name = "stop-after-admission"
		}
		t.Run(name, func(t *testing.T) {
			agent, repo, _, engine, id, _, _ := completedRecoveryFixture(t)
			entered, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			prior := completedRunHomecomingAt
			completedRunHomecomingAt = func(admitted bool) {
				if admitted == afterAdmission {
					close(entered)
					<-release
				}
			}
			t.Cleanup(func() { unblock(); completedRunHomecomingAt = prior })
			result := make(chan error, 1)
			go func() { result <- agent.ResolveUnverified(id, TaskAccept, "read mock work") }()
			select {
			case <-entered:
			case <-time.After(3 * time.Second):
				t.Fatal("acceptance did not reach merge admission")
			}
			if err := agent.StopWork(); err != nil {
				t.Fatal(err)
			}
			agent.mu.Lock()
			done := agent.workStopDone
			agent.mu.Unlock()
			select {
			case <-done:
				t.Fatal("Stop completed before its admitted acceptance joined")
			default:
			}
			unblock()
			select {
			case err := <-result:
				if afterAdmission && err != nil {
					t.Fatalf("later stop lost actual merge: %v", err)
				}
				if !afterAdmission && !errors.Is(err, errWorkStopping) {
					t.Fatalf("earlier stop did not refuse merge: %v", err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("acceptance did not settle")
			}
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("Stop did not join completed acceptance")
			}
			_, err := os.Stat(filepath.Join(repo, "counter.txt"))
			kept, _ := runRowOf(agent.graph(), id)
			if afterAdmission {
				if err != nil || kept.State != TaskDone || !strings.Contains(kept.Report, "work commit ") {
					t.Fatalf("actual landing receipt lost: %+v %v", kept, err)
				}
			} else if !os.IsNotExist(err) || kept.State != TaskUnverified {
				t.Fatalf("Stop winning before admission still merged: %+v %v", kept, err)
			}
			nothingStarted(t, agent, engine)
		})
	}
}

func TestCompletedPlanRecoveryKeepsActualMergeWhenPageReceiptCannotBeSaved(t *testing.T) {
	agent, repo, _, engine, id, _, open := completedRecoveryFixture(t)
	prior := completedRunReceiptWriter
	completedRunReceiptWriter = func(string, string, string) error { return errors.New("mock page write unavailable") }
	t.Cleanup(func() { completedRunReceiptWriter = prior })
	err := agent.ResolveUnverified(id, TaskAccept, "mock work inspected")
	if err == nil || !strings.Contains(err.Error(), "outcome was saved") || !strings.Contains(err.Error(), "work commit ") {
		t.Fatalf("page write failure concealed actual merge: %v", err)
	}
	head := runTreeHead(repo)
	if _, err := os.Stat(filepath.Join(repo, "counter.txt")); err != nil {
		t.Fatal("accepted work did not arrive")
	}
	kept, _ := runRowOf(agent.graph(), id)
	if kept.State != TaskDone || kept.CostUSD != 0.42 {
		t.Fatalf("failed receipt discarded actual outcome: %+v", kept)
	}
	if err := agent.Close(); err != nil {
		t.Fatal(err)
	}
	agent = open()
	if err := agent.ResolveUnverified(id, TaskAccept, "retry"); !errors.Is(err, ErrTaskDecided) {
		t.Fatalf("retry repeated a merge or lost task identity: %v", err)
	}
	if runTreeHead(repo) != head {
		t.Fatal("receipt retry changed already-landed work")
	}
	nothingStarted(t, agent, engine)
}

func TestCompletedPlanRecoveryReportsFailedCheckpointAfterActualMerge(t *testing.T) {
	agent, repo, _, engine, id, _, _ := completedRecoveryFixture(t)
	g := agent.graph()
	g.mu.Lock()
	store := g.store
	g.mu.Unlock()
	store.mu.Lock()
	original := store.path
	store.path = t.TempDir()
	store.mu.Unlock()
	t.Cleanup(func() { store.mu.Lock(); store.path = original; store.mu.Unlock() })
	err := agent.ResolveUnverified(id, TaskAccept, "mock inspected")
	if err == nil || !strings.Contains(err.Error(), "task checkpoint could not be saved") || strings.Contains(err.Error(), "Its outcome was saved") || !strings.Contains(err.Error(), "work commit ") {
		t.Fatalf("unchecked checkpoint claimed durable success: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repo, "counter.txt")); err != nil {
		t.Fatal("actual accepted work was rolled back")
	}
	kept, _ := runRowOf(g, id)
	if kept.State != TaskDone {
		t.Fatalf("failed checkpoint falsified actual merge: %+v", kept)
	}
	if err := agent.ResolveUnverified(id, TaskAccept, "retry"); !errors.Is(err, ErrTaskDecided) {
		t.Fatalf("retry repeated accepted merge: %v", err)
	}
	nothingStarted(t, agent, engine)
}

func TestCompletedPlanRecoveryNeverTakesAnUnattendedDefault(t *testing.T) {
	agent, _, _, _, id, _, _ := completedRecoveryFixture(t)
	agent.config.Unattended, agent.config.AskConsent = true, true
	kept, _ := runRowOf(agent.graph(), id)
	if agent.landingTakesItsDefault(kept) {
		t.Fatal("restart silently accepted completed work")
	}
	ordinary := kept
	ordinary.RecoveredCompletion = false
	if !agent.landingTakesItsDefault(ordinary) {
		t.Fatal("recovered guard changed ordinary unattended landing policy")
	}
}

func TestCompletedPlanRecoveryKeepsConsequencesAndOrdinaryQuestionControls(t *testing.T) {
	q := Question{Kind: QuestionConflict, Options: []AnswerOption{{Key: LandingYesKey, Label: "resolve it", Consequence: "mock existing files will be set aside"}, {Key: LandingNoKey, Label: "drop it"}, {Key: LandingDecideKey, Label: "tell it"}}}
	recovered := completedLandingQuestion(q, TaskNotice{RecoveredCompletion: true, Merge: mergeConflicted})
	if recovered.Kind != QuestionLanding || len(recovered.Options) != 2 || recovered.Options[0].Label != taskAskCheckYes || recovered.Options[0].Consequence != q.Options[0].Consequence {
		t.Fatalf("recovered controls hid a consequence or offered a dead action: %+v", recovered)
	}
	ordinary := completedLandingQuestion(q, TaskNotice{})
	if ordinary.Kind != QuestionConflict || len(ordinary.Options) != 3 || ordinary.Options[2].Key != LandingDecideKey {
		t.Fatal("recovered repair changed ordinary question controls")
	}
}

func TestCompletedPlanRecoveryReportsUnsavedRefusalWithoutChangingItsCopy(t *testing.T) {
	agent, repo, copy, engine, id, _, _ := completedRecoveryFixture(t)
	before := runTreeHead(repo)
	g := agent.graph()
	g.mu.Lock()
	store := g.store
	g.mu.Unlock()
	store.mu.Lock()
	original := store.path
	store.path = t.TempDir()
	store.mu.Unlock()
	t.Cleanup(func() { store.mu.Lock(); store.path = original; store.mu.Unlock() })
	err := agent.ResolveUnverified(id, TaskRefute, "mock rejected")
	if err == nil || !strings.Contains(err.Error(), "task checkpoint could not be saved") || strings.Contains(err.Error(), "Its outcome was saved") {
		t.Fatalf("unchecked refusal claimed durable success: %v", err)
	}
	kept, _ := runRowOf(g, id)
	if kept.State != TaskFailed || kept.Ending != TaskEndingRefused || runTreeHead(repo) != before {
		t.Fatalf("unsaved refusal changed work or lost actual answer: %+v", kept)
	}
	if _, err := os.Stat(filepath.Join(copy.Dir, "counter.txt")); err != nil {
		t.Fatal("refusal removed saved work")
	}
	nothingStarted(t, agent, engine)
}

func TestCompletedPlanRecoveryKeepsLedgerSpendWhenCheckpointIsUnpriced(t *testing.T) {
	agent, _, _, engine, id, _, open := completedRecoveryFixtureWithSpend(t, 0, 0.37)
	check := func(a *Agent) {
		t.Helper()
		kept, _ := runRowOf(a.graph(), id)
		if kept.CostUSD != 0.37 {
			t.Fatalf("run receipt lost authoritative ledger price: %+v", kept)
		}
		if a.Usage().CostUSD != 0 {
			t.Fatal("restoring a price label charged the conversation again")
		}
		row := planRowFor(a.PlanTasks(), planStoreID(strconv.FormatUint(id, 10)))
		if row == nil || row.USD != 0.37 {
			t.Fatalf("recovery erased task's authoritative spend: %+v", row)
		}
		page, ok := a.PlanTaskPage(planStoreID(strconv.FormatUint(id, 10)))
		if !ok || page.Row.USD != 0.37 {
			t.Fatalf("task page erased its ledger spend: %+v", page.Row)
		}
		text, ok := a.planTaskText(a.runPlanTasks(), strconv.FormatUint(id, 10))
		if !ok || !strings.Contains(text, "$0.37") {
			t.Fatalf("task reader lost ledger cost: %q", text)
		}
	}
	check(agent)
	if err := agent.Close(); err != nil {
		t.Fatal(err)
	}
	agent = open()
	check(agent)
	if err := agent.ResolveUnverified(id, TaskAccept, "mock work inspected"); err != nil {
		t.Fatal(err)
	}
	kept, _ := runRowOf(agent.graph(), id)
	if kept.CostUSD != 0.37 {
		t.Fatalf("accepted run receipt lost ledger spend: %+v", kept)
	}
	var indexCost float64
	for _, entry := range ReadTaskIndex(TaskIndexPath(agent.config.SessionFile)) {
		if entry.ID == strconv.FormatUint(id, 10) {
			indexCost = entry.Cost
		}
	}
	if indexCost != 0.37 {
		t.Fatalf("accepted task index lost ledger spend: %g", indexCost)
	}
	check(agent)
	if err := agent.Close(); err != nil {
		t.Fatal(err)
	}
	agent = open()
	check(agent)
	nothingStarted(t, agent, engine)
}

func TestCompletedPlanRecoveryAcceptsItsArchiveWithoutAdoptingTheNewerPlan(t *testing.T) {
	agent, repo, _, engine, id, _, _ := completedRecoveryFixture(t)
	path := agent.graph().planPath()
	if err := setAsideRunStore(path); err != nil {
		t.Fatal(err)
	}
	nextID := strconv.FormatUint(id+1, 10)
	newer, err := plandb.Open(path, "mock", nextID, "newer mock work", "leave this new request alone", agent.graph().planChat())
	if err != nil {
		t.Fatal(err)
	}
	defer newer.Close()
	if err := newer.AddSpend(nextID, "mock/newer-model", "work", 0.91, 10, 2); err != nil {
		t.Fatal(err)
	}
	before := newer.Task(nextID)
	if err := agent.ResolveUnverified(id, TaskAccept, "read the older saved work"); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(filepath.Join(repo, "counter.txt")); err != nil || string(data) != "mock completed work\n" {
		t.Fatalf("archived work did not land: %q %v", data, err)
	}
	after := newer.Task(nextID)
	if after.Status != before.Status || after.Description != before.Description || after.Result != "" || len(newer.Notes(nextID, 0)) != 0 {
		t.Fatalf("old acceptance adopted or changed the newer plan: %+v", after)
	}
	archived, err := plandb.Open(path+".1", "", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	defer archived.Close()
	notes := archived.Notes(strconv.FormatUint(id, 10), 0)
	if len(notes) == 0 || !strings.Contains(notes[len(notes)-1].Body, "work commit ") {
		t.Fatalf("receipt went to the wrong plan: %+v", notes)
	}
	kept, _ := runRowOf(agent.graph(), id)
	if kept.State != TaskDone || kept.CostUSD != 0.42 {
		t.Fatalf("archived acceptance mixed newer task's state or spend: %+v", kept)
	}
	nothingStarted(t, agent, engine)
}

func TestCompletedPlanRecoveryRefusesAPlanFromAnotherChat(t *testing.T) {
	agent, repo, copy, engine, id, _, _ := completedRecoveryFixture(t)
	before := runTreeHead(repo)
	path := agent.graph().planPath()
	armPlanStore(t, agent, path, "mock-foreign-chat")
	err := agent.ResolveUnverified(id, TaskAccept, "must not cross chats")
	if err == nil || !strings.Contains(err.Error(), "no completed plan in this conversation") {
		t.Fatalf("foreign plan was accepted or refused without explanation: %v", err)
	}
	kept, _ := runRowOf(agent.graph(), id)
	if kept.State != TaskUnverified || runTreeHead(repo) != before {
		t.Fatal("foreign-plan refusal changed existing work")
	}
	if _, err := os.Stat(filepath.Join(copy.Dir, "counter.txt")); err != nil {
		t.Fatal("foreign-plan refusal removed saved work")
	}
	nothingStarted(t, agent, engine)
}

func TestCompletedPlanRecoveryNamesTheMissingCopyThroughItsActualToolDoor(t *testing.T) {
	agent, repo, _, engine, id, _, open := completedRecoveryFixture(t)
	before := runTreeHead(repo)
	kept, _ := runRowOf(agent.graph(), id)
	kept.Copy = nil
	kept.State = TaskInterrupted
	agent.graph().keepRunRows(id, []TaskNotice{kept})
	if err := agent.Close(); err != nil {
		t.Fatal(err)
	}
	agent = open()
	args, _ := json.Marshal(map[string]any{"id": strconv.FormatUint(id, 10), "resolve": "accept"})
	reply, bad, err := agent.tasksTool().Execute(context.Background(), args)
	if err != nil || !bad || !strings.Contains(reply, "working copy was not written down") || strings.Contains(reply, "No task") {
		t.Fatalf("missing saved copy became a false task miss or success: %q bad=%v err=%v", reply, bad, err)
	}
	kept, _ = runRowOf(agent.graph(), id)
	if kept.State != TaskInterrupted || kept.Copy != nil || runTreeHead(repo) != before {
		t.Fatalf("missing-copy refusal invented or changed work: %+v", kept)
	}
	nothingStarted(t, agent, engine)
}

func TestCompletedPlanRecoveryKeepsWholeRunSpendWithoutChargingAgain(t *testing.T) {
	agent, _, _, engine, id, _, open := completedRecoveryFixtureWithSpend(t, 0, 0.02, 0.02)
	check := func(a *Agent) {
		t.Helper()
		kept, _ := runRowOf(a.graph(), id)
		page, ok := a.PlanTaskPage(planStoreID(strconv.FormatUint(id, 10)))
		if kept.CostUSD != 0.04 || !ok || page.Row.USD != 0.04 {
			t.Fatalf("root and checker must appear once in whole-run receipt and page: receipt=%g page=%g", kept.CostUSD, page.Row.USD)
		}
		store, err := plandb.Open(a.graph().planPath(), "mock", strconv.FormatUint(id, 10), "mock completed counter", "write counter", a.graph().planChat())
		if err != nil {
			t.Fatal(err)
		}
		spent := storeSpent(store)
		_ = store.Close()
		if spent != 0.04 || a.Usage().CostUSD != 0 {
			t.Fatalf("recovery changed actual ledger or charged again: ledger=%g usage=%g", spent, a.Usage().CostUSD)
		}
	}
	check(agent)
	if err := agent.Close(); err != nil {
		t.Fatal(err)
	}
	agent = open()
	check(agent)
	if err := agent.ResolveUnverified(id, TaskAccept, "mock root and checker inspected"); err != nil {
		t.Fatal(err)
	}
	check(agent)
	if err := agent.Close(); err != nil {
		t.Fatal(err)
	}
	agent = open()
	check(agent)
	nothingStarted(t, agent, engine)
}
