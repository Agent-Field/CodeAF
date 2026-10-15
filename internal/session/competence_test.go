package session

import (
	"bufio"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/competence"
	"github.com/Agent-Field/codeaf/internal/plandb"
)

// ledgerRowsFor is every ledger row one conversation wrote. The hermetic home
// is the package's, so other tests' rows are filtered out by transcript.
func ledgerRowsFor(t *testing.T, transcript string) []competence.Record {
	t.Helper()
	file, err := os.Open(competence.LedgerPath())
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	var rows []competence.Record
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var row competence.Record
		if json.Unmarshal(scanner.Bytes(), &row) == nil && row.Source.Transcript == transcript {
			rows = append(rows, row)
		}
	}
	return rows
}

// journalledAgent is a test conversation WITH a journal on disk, which is what
// makes its work attributable: a conversation with no journal writes nothing.
func journalledAgent(t *testing.T) (*Agent, string) {
	t.Helper()
	transcript := filepath.Join(t.TempDir(), "conv", "transcript.jsonl")
	agent, dir := newTestAgent(t, &scriptedCompleter{}, func(c *Config) { c.SessionFile = transcript })
	if agent.journalID() == "" {
		t.Fatal("the fixture's conversation has no journal")
	}
	return agent, dir
}

// runFixture is a settled run's plan store: a root, one work task spent on the
// given models, and one check task whose basis is the given exits. A model is
// an ANSWERED charge; "built:<id>" is a charge on the model a seat was built
// on, the only kind written before answered charges existed; "free:<id>" is an
// answer that cost nothing; "" is a charge whose answer named no model.
func runFixture(t *testing.T, agent *Agent, ground string, models []string, exits map[string]int) *beltRun {
	t.Helper()
	store, err := plandb.Open(filepath.Join(t.TempDir(), "plandb.db"), "p", "7", "Fix the parser", "fix it", agent.journalID())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	if _, err := store.AddMany([]plandb.TaskSpec{{ID: "t-work", Title: "the lexer", ParentID: "7"}}); err != nil {
		t.Fatal(err)
	}
	check, err := store.AddReviewCheck(plandb.TaskSpec{ID: "t-check", Title: "check: the lexer", ParentID: "7", Role: plandb.RoleCheck})
	if err != nil {
		t.Fatal(err)
	}
	basis := plandb.VerdictBasis{Kind: "run"}
	for command, exit := range exits {
		basis.Runs = append(basis.Runs, plandb.VerdictRun{Command: command, ExitCode: exit})
	}
	if _, err := store.SetVerdictBasis(check.ID, basis); err != nil {
		t.Fatal(err)
	}
	for _, model := range models {
		var err error
		if built, ok := strings.CutPrefix(model, "built:"); ok || model == "" {
			err = store.AddSpend("t-work", built, plandb.RoleWork, 0.01, 10, 10)
		} else if free, ok := strings.CutPrefix(model, "free:"); ok {
			err = store.AddAnsweredSpend("t-work", free, plandb.RoleWork, 0, 0, 0)
		} else {
			err = store.AddAnsweredSpend("t-work", model, plandb.RoleWork, 0.01, 10, 10)
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	// The checker's own spend is never the work's model.
	if err := store.AddSpend(check.ID, "careful/checker", plandb.RoleCheck, 0.01, 10, 10); err != nil {
		t.Fatal(err)
	}
	return &beltRun{store: store, root: "7", row: 7, title: "Fix the parser", ground: ground,
		ended: time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)}
}

// THE DEFAULT ROAD: A SETTLED RUN'S DECLARED-CHECK EXITS BECOME ONE RESULT, on
// the one model its work was spent through and under the task row the
// conversation knows it by. A real non-zero exit beside a pass is a failure.
func TestASettledRunWritesItsCheckExitsOnItsWorkModel(t *testing.T) {
	agent, dir := journalledAgent(t)
	run := runFixture(t, agent, dir, []string{"m/flash"}, map[string]int{"go test ./...": 0, "go vet ./...": 1})
	agent.recordRunProven(run)
	rows := ledgerRowsFor(t, agent.config.SessionFile)
	if len(rows) != 1 {
		t.Fatalf("rows %+v", rows)
	}
	row := rows[0]
	if row.Model != "m/flash" || row.ID != competence.TaskID(agent.journalID(), "7") || row.Source.Task != "7" ||
		row.Place != dir || row.Title != "Fix the parser" || row.Outcome != competence.Failed || row.Ran != 2 {
		t.Fatalf("row %+v", row)
	}
}

// A DECLARED CHECK THAT COULD NOT RUN LEAVES THE WHOLE RUN UNWRITTEN: one
// physical pass beside a 126, a 127 or a negative code is not a pass.
func TestARunWithACheckThatCouldNotRunWritesNothing(t *testing.T) {
	for _, code := range []int{126, 127, -1} {
		agent, dir := journalledAgent(t)
		agent.recordRunProven(runFixture(t, agent, dir, []string{"m/flash"}, map[string]int{"go test ./...": 0, "golint ./...": code}))
		if rows := ledgerRowsFor(t, agent.config.SessionFile); len(rows) != 0 {
			t.Errorf("a pass beside exit %d wrote %+v", code, rows)
		}
	}
}

// AN ANSWER THAT COST NOTHING IS STILL AN ANSWER: a free model alone is the
// run's model, and a free model beside a paid one is a second model.
func TestAFreeAnswerCountsAsAModel(t *testing.T) {
	agent, dir := journalledAgent(t)
	agent.recordRunProven(runFixture(t, agent, dir, []string{"free:m/free"}, map[string]int{"go test ./...": 0}))
	if rows := ledgerRowsFor(t, agent.config.SessionFile); len(rows) != 1 || rows[0].Model != "m/free" || rows[0].Outcome != competence.Passed {
		t.Fatalf("a free model's run wrote %+v", rows)
	}
	other, otherDir := journalledAgent(t)
	other.recordRunProven(runFixture(t, other, otherDir, []string{"m/a", "free:m/b"}, map[string]int{"go test ./...": 0}))
	if rows := ledgerRowsFor(t, other.config.SessionFile); len(rows) != 0 {
		t.Fatalf("a paid and a free model wrote %+v", rows)
	}
}

// AN ANSWER IS COUNTED WHERE IT ARRIVES, with or without usage on it, under the
// model it was asked of; a missing response is no answer, and a late receipt
// banks money without making an answer of it.
func TestAnswersAreCountedWithoutUsageAndNeverFromReceipts(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	var turn Usage
	agent.addUsage(&turn, &ai.Response{}, "m/b", "", laneFacts{})
	agent.addUsage(&turn, nil, "m/c", "", laneFacts{})
	agent.bank(bankedCall{used: Usage{Input: 3, CostUSD: 0.01, Calls: 1}, model: "m/late"})
	if answers := agent.Answers(); len(answers) != 1 || answers["m/b"] != 1 {
		t.Fatalf("answers %+v", answers)
	}
	if shares := agent.AnsweredUsage(); shares["m/b"] != (Usage{}) {
		t.Fatalf("an answer with no usage was billed: %+v", shares)
	}
}

// A RUN NOBODY CAN ATTRIBUTE IS NO EVIDENCE: two answering models, none, an
// unknown one, a built-on name, or no check that ran.
func TestARunWithNoSingleWorkModelOrNoRunCheckWritesNothing(t *testing.T) {
	for name, fixture := range map[string]struct {
		models []string
		exits  map[string]int
	}{
		"two models": {[]string{"m/a", "m/b"}, map[string]int{"go test ./...": 0}},
		"no spend":   {nil, map[string]int{"go test ./...": 0}},
		"no run":     {[]string{"m/a"}, nil},
		// An answer that named no model is unknown, never the one beside it.
		"unknown answer": {[]string{"m/a", ""}, map[string]int{"go test ./...": 0}},
		"unknown alone":  {[]string{""}, map[string]int{"go test ./...": 0}},
		// THE MODEL A SEAT WAS BUILT ON IS NEVER THE CHECKED MODEL: an old
		// ledger, or a resumed run's earlier seat, with real exits beside it.
		"built model only": {[]string{"built:m/flash"}, map[string]int{"go test ./...": 0}},
		// And one such row spoils a run even when its name matches the answer.
		"built beside answered": {[]string{"m/flash", "built:m/flash"}, map[string]int{"go test ./...": 0}},
	} {
		agent, dir := journalledAgent(t)
		agent.recordRunProven(runFixture(t, agent, dir, fixture.models, fixture.exits))
		if rows := ledgerRowsFor(t, agent.config.SessionFile); len(rows) != 0 {
			t.Errorf("%s wrote %+v", name, rows)
		}
	}
}

// A DELETED SOURCE IS SAID, and a source nobody recorded is not called deleted.
func TestASourcesDeletionIsReadOffTheRecords(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "conv")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	transcript := filepath.Join(dir, "transcript.jsonl")
	source := competence.Source{Kind: competence.SourceTask, Agent: "conv", Task: "3", Transcript: transcript}
	if got := CheckedSourceGone(source); got != "" {
		t.Fatalf("a present source read %q", got)
	}
	if err := os.WriteFile(filepath.Join(dir, taskDeletedFile), []byte(`{"3":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := CheckedSourceGone(source); got != competence.SourceTask {
		t.Fatalf("a deleted task read %q", got)
	}
	if err := os.WriteFile(filepath.Join(dir, conversationDeletedFile), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := CheckedSourceGone(source); got != competence.SourceConversation {
		t.Fatalf("a deleted conversation read %q", got)
	}
	source.Transcript = filepath.Join(t.TempDir(), "gone", "transcript.jsonl")
	if got := CheckedSourceGone(source); got != competence.SourceConversation {
		t.Fatalf("a missing conversation folder read %q", got)
	}
	if got := CheckedSourceGone(competence.Source{Kind: competence.SourceTask, Task: "3"}); got != "" {
		t.Fatalf("an unrecorded source read %q", got)
	}
}

// A RUN OTHER HAND-OFFS JOINED IS NOBODY'S ONE GOAL, and its checks are not
// credited to the first; a PLANNER'S spend on the root is never the work's.
func TestAJoinedRunIsNoEvidenceAndAPlannerIsNotTheWorker(t *testing.T) {
	agent, dir := journalledAgent(t)
	run := runFixture(t, agent, dir, []string{"m/flash"}, map[string]int{"go test ./...": 0})
	if err := run.store.AddSpend("7", "m/planner", plandb.RolePlan, 0.02, 10, 10); err != nil {
		t.Fatal(err)
	}
	run.joined = []uint64{8}
	agent.recordRunProven(run)
	if rows := ledgerRowsFor(t, agent.config.SessionFile); len(rows) != 0 {
		t.Fatalf("a joined run wrote %+v", rows)
	}
	run.joined = nil
	agent.recordRunProven(run)
	rows := ledgerRowsFor(t, agent.config.SessionFile)
	if len(rows) != 1 || rows[0].Model != "m/flash" {
		t.Fatalf("the planner's spend moved the work's model: %+v", rows)
	}
}

// THE ANSWERED TALLY IS KEPT UNDER THE MODEL EACH CALL NAMED, and sums to the
// session's own figures.
func TestTheAnsweredTallyFollowsTheAnsweringModel(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	if agent.AnsweredUsage() != nil {
		t.Fatal("an agent that spent nothing has a tally")
	}
	agent.bank(bankedCall{used: Usage{Input: 10, Output: 5, CostUSD: 0.01, Calls: 1}, model: "m/primary", turn: true})
	agent.bank(bankedCall{used: Usage{Input: 20, Output: 7, CostUSD: 0.03, Calls: 1}, model: "m/fallback", turn: true})
	agent.bank(bankedCall{used: Usage{Input: 1, Output: 1, CostUSD: 0.001, Calls: 1}, model: "m/fallback"})
	shares := agent.AnsweredUsage()
	if len(shares) != 2 || shares["m/fallback"].Input != 21 || shares["m/primary"].Output != 5 {
		t.Fatalf("shares %+v", shares)
	}
	total := agent.Usage()
	if shares["m/primary"].Input+shares["m/fallback"].Input != total.Input ||
		math.Abs(shares["m/primary"].CostUSD+shares["m/fallback"].CostUSD-total.CostUSD) > 1e-12 {
		t.Fatalf("shares %+v do not sum to %+v", shares, total)
	}
}

// A response's explicit model is stronger evidence than its request target,
// even when the provider supplies no usage at all.
func TestAnAnswerNamesItsActualModelWithoutUsage(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	var turn Usage
	agent.addUsage(&turn, &ai.Response{Model: "m/served"}, "m/requested", "", laneFacts{})
	if answers := agent.Answers(); len(answers) != 1 || answers["m/served"] != 1 {
		t.Fatalf("answered model was replaced by request target: %+v", answers)
	}
}
