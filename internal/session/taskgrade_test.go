package session

// THE GRADING LOOP, AS TESTS: a node settles, the ratings store learns, and the
// next division reads what it learned.
//
// Everything here drives the real seams — the graph's own settle, the belt's own
// `divide_work`, and the ledger internal/router writes and `aforge models`
// reads — because the whole of issue #147 was two ends that both existed and
// never met. A test that asserted against a fixture of its own would be a third
// end.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/roles"
	"github.com/Agent-Field/aforge-v2/internal/router"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// ── harness ─────────────────────────────────────────────────────────────────

// gradeNest is a conversation with a profile directory behind it — which is the
// one thing that turns the grading loop on — and its graph.
type gradeNest struct {
	agent   *Agent
	graph   *TaskGraph
	profile string
}

func newGradeNest(t *testing.T, client Completer) *gradeNest {
	t.Helper()
	profile := t.TempDir()
	agent, _ := newTestAgent(t, client, func(config *Config) {
		config.ProfileDir = profile
		config.SessionFile = filepath.Join(t.TempDir(), "session.jsonl")
	})
	graph := stubbedGraph(agent, func(*TaskNode) {})
	return &gradeNest{agent: agent, graph: graph, profile: profile}
}

// settle admits one node, tells it what the check said, and lands it — which is
// the whole of a node's life as far as the store is concerned.
func (n *gradeNest) settle(t *testing.T, title, model string, verdict provider.Verdict, repairs int, state TaskState) *TaskNode {
	t.Helper()
	return n.settleWith(t, title, model, verdict, repairs, state, nil)
}

// settleWith is the same with one hand on the node's own bill and clock, which
// a test that reads the record itself needs and no other test cares about.
func (n *gradeNest) settleWith(t *testing.T, title, model string, verdict provider.Verdict, repairs int, state TaskState, prepare func(*TaskNode)) *TaskNode {
	t.Helper()
	id := n.graph.reserve()
	n.graph.admit(id, taskSpec{title: title, named: true, brief: "b", acceptance: "a", model: model})
	node := n.graph.node(id)
	if verdict != "" {
		node.checkSaid(verdict, repairs)
	}
	if prepare != nil {
		n.graph.mu.Lock()
		prepare(node)
		n.graph.mu.Unlock()
	}
	node.finish("what it did", nil, "", "")
	n.graph.complete(node, state)
	return node
}

// entries is the ratings store as `aforge models` reads it: the very same call
// cmd/aforge's runModels makes, out of the very same directory.
func (n *gradeNest) entries(t *testing.T) []router.Entry {
	t.Helper()
	ledger, err := router.LoadLedger(n.profile)
	if err != nil {
		t.Fatalf("LoadLedger: %v", err)
	}
	return ledger.Entries()
}

// rows is the diary beside the ratings — one object per settled node.
func (n *gradeNest) rows(t *testing.T) []router.Event {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(n.profile, "router-events.jsonl"))
	if err != nil {
		t.Fatalf("reading the diary: %v", err)
	}
	var rows []router.Event
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var row router.Event
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			t.Fatalf("a diary row is not an object: %v\n%s", err, line)
		}
		rows = append(rows, row)
	}
	return rows
}

// findEntry answers the one row about a model at a kind of work.
func findEntry(entries []router.Entry, model, kind string) (router.Entry, bool) {
	want := string(router.Shaped(provider.ClassTaskNode, kind))
	for _, entry := range entries {
		if entry.Model == model && string(entry.Class) == want {
			return entry, true
		}
	}
	return router.Entry{}, false
}

// ── ACCEPTANCE: after any task settles, aforge models shows a graded row ─────

// THE FIRST HALF OF THE LOOP. `aforge models` reads router.LoadLedger out of the
// profile directory and prints one row per entry; before this wave the chat
// engine wrote nothing there at all, so a machine that had run tasks for weeks
// printed "nothing measured yet".
func TestASettledTaskShowsUpInTheRatingsAforgeModelsReads(t *testing.T) {
	nest := newGradeNest(t, &scriptedCompleter{})

	nest.settle(t, "tests for the rail", "cheap/model", provider.VerdictVerifiedSuccess, 1, TaskDone)

	entries := nest.entries(t)
	if len(entries) == 0 {
		t.Fatal("a task settled and the ratings store is still empty — the panel would say nothing measured yet")
	}
	entry, found := findEntry(entries, "cheap/model", "tests for the rail")
	if !found {
		t.Fatalf("no row for the model at this kind of work; the store holds %v", entries)
	}
	if entry.Count != 1 {
		t.Fatalf("the row rests on %d graded outcomes, want 1", entry.Count)
	}
	if entry.Rating <= 0 {
		t.Fatalf("work the check accepted rated the model at %.2f, want a rating above the middle", entry.Rating)
	}
}

// AND THE RECORD IS THE ONE THE ISSUE ASKED FOR: model, the kind in the
// divider's own words, the outcome in plain words, the retries, the cost and the
// duration — all of it in the diary internal/router already keeps beside the
// ratings, and none of it in a store of its own.
func TestTheGradedRecordCarriesTheModelKindOutcomeRetriesCostAndDuration(t *testing.T) {
	nest := newGradeNest(t, &scriptedCompleter{})

	// A settled node's bill and its age are the graph's own facts, and the record
	// must carry the graph's rather than take a second measurement of its own.
	nest.settleWith(t, "the eleven adapters", "cheap/model", provider.VerdictSemanticFailure, 2, TaskFailed,
		func(node *TaskNode) {
			node.ending = TaskEndingRefused
			node.cost = 0.42
			node.started = time.Now().Add(-2 * time.Second)
		})

	rows := nest.rows(t)
	if len(rows) != 1 {
		t.Fatalf("a settled node wrote %d diary rows, want 1", len(rows))
	}
	row := rows[0]
	for name, got := range map[string]string{
		"the class":   string(row.Class),
		"the kind":    row.Shape,
		"the model":   row.Model,
		"the outcome": row.Outcome,
	} {
		if strings.TrimSpace(got) == "" {
			t.Fatalf("%s is empty in the record: %+v", name, row)
		}
	}
	if row.Class != string(provider.ClassTaskNode) {
		t.Fatalf("the record is filed under %q, want %q", row.Class, provider.ClassTaskNode)
	}
	if row.Shape != "the eleven adapters" {
		t.Fatalf("the kind reads %q, want the divider's own words for the part", row.Shape)
	}
	if row.Outcome != "not accepted" {
		t.Fatalf("the outcome reads %q, want the settle's own plain word", row.Outcome)
	}
	if row.Retries != 2 {
		t.Fatalf("the record says %d retries, want the 2 repair rounds the check spent", row.Retries)
	}
	if row.Verdict != provider.VerdictSemanticFailure {
		t.Fatalf("the record carries the verdict %q, want the check's own answer", row.Verdict)
	}
	if row.Cost != 0.42 {
		t.Fatalf("the record says the work cost %v, want the node's own bill", row.Cost)
	}
	if row.LatencyMS < 2000 {
		t.Fatalf("the record says the work took %dms, want the node's own age", row.LatencyMS)
	}
	if !row.Final {
		t.Fatal("the record is not marked final, so a reader cannot tell it closes the account for this node")
	}
}

// A NODE THAT SETTLES TWICE CORRECTS ITS OWN ROW RATHER THAN BECOMING TWO
// PIECES OF WORK. It lands needing a look, somebody accepts it, and the diary is
// append-only — so both rows are there and they share one call id.
func TestANodeResolvedLaterCorrectsTheRowItAlreadyWrote(t *testing.T) {
	nest := newGradeNest(t, &scriptedCompleter{})

	node := nest.settle(t, "the tricky merge", "cheap/model", "", 0, TaskUnverified)
	// Nobody could say, so nothing was learned: the row exists and the rating does not.
	if entries := nest.entries(t); len(entries) != 0 {
		t.Fatalf("work nobody could check moved a rating: %v", entries)
	}
	node.checkSaid(provider.VerdictVerifiedSuccess, 0)
	nest.graph.resettle(node, TaskDone)

	rows := nest.rows(t)
	if len(rows) != 2 {
		t.Fatalf("the node wrote %d rows across two settles, want 2", len(rows))
	}
	if rows[0].Call != rows[1].Call {
		t.Fatalf("the two rows carry different call ids (%q, %q), so they read as two pieces of work",
			rows[0].Call, rows[1].Call)
	}
	if _, found := findEntry(nest.entries(t), "cheap/model", "the tricky merge"); !found {
		t.Fatal("the answer that arrived second taught the store nothing")
	}
}

// ── ACCEPTANCE: no additional model call is spent to grade ──────────────────

// countingClient answers nothing and counts being asked. Anything the grading
// loop did that needed a model would show up here as a call nobody made.
type countingClient struct{ calls atomic.Int64 }

func (c *countingClient) CompleteWithMessages(_ context.Context, _ []ai.Message, _ ...ai.Option) (*ai.Response, error) {
	c.calls.Add(1)
	return nil, fmt.Errorf("the grading loop must not make a model call")
}

// THE CHECK'S ANSWER IS THE GRADE, AND IT WAS ALREADY PAID FOR. Nothing in this
// file's road opens a request: the verdict is read off the node, the kind is cut
// from a title, and the store is a small JSON file.
func TestGradingASettledNodeSpendsNoModelCall(t *testing.T) {
	client := &countingClient{}
	nest := newGradeNest(t, client)
	// A LANDING'S NOTE WAKES THE CONVERSATION, and that turn is a model call
	// which has nothing to do with grading. The report hook that carries it is
	// taken off so that what is left under the counter is the settle, the store
	// and nothing else.
	nest.graph.mu.Lock()
	nest.graph.report = nil
	nest.graph.mu.Unlock()

	nest.settle(t, "tests for the rail", "cheap/model", provider.VerdictSemanticFailure, 1, TaskFailed)
	nest.settle(t, "tests for the composer", "cheap/model", provider.VerdictVerifiedSuccess, 0, TaskDone)
	// The read side too: what the divider asks the store before it assigns a tier.
	nest.graph.grades.saysCareful("cheap/model", taskKindOf("tests for the rail"))

	if calls := client.calls.Load(); calls != 0 {
		t.Fatalf("grading two settled nodes and reading the store made %d model calls, want none", calls)
	}
	if len(nest.entries(t)) == 0 {
		t.Fatal("no calls were made and nothing was learned either — the grade is not coming off the check")
	}
}

// AND WORK NOBODY COULD JUDGE TEACHES NOTHING. A stopped node, a node whose
// worktree could not be made, a node that ran with the check turned off: each
// writes its record and moves no rating, which is the ledger's own law about
// outcomes that are not evidence.
func TestWorkNobodyCheckedWritesItsRecordAndMovesNoRating(t *testing.T) {
	nest := newGradeNest(t, &scriptedCompleter{})

	nest.settleWith(t, "the halted sweep", "cheap/model", "", 0, TaskFailed,
		func(node *TaskNode) { node.ending = TaskEndingError })

	if entries := nest.entries(t); len(entries) != 0 {
		t.Fatalf("work nobody checked rated a model: %v", entries)
	}
	rows := nest.rows(t)
	if len(rows) != 1 || rows[0].Verdict != "" {
		t.Fatalf("the record for unjudged work is %+v, want one row carrying no verdict", rows)
	}
	// AND IT DOES NOT READ AS A JUDGEMENT. A node that fell over is not a node
	// the check turned down, and the record must not say it was.
	if rows[0].Outcome != "did not finish" {
		t.Fatalf("the record's outcome reads %q, want the settle's own word for work nobody judged", rows[0].Outcome)
	}
}

// ── ACCEPTANCE: repeated rejections earn the capable tier ───────────────────

// divideTitledArgs is one well-formed division whose parts carry the names the
// test cares about, all of them graded as ordinary work — because the whole
// question is whether the STORE lifts a part the worker called mechanical.
func divideTitledArgs(evidence string, titles ...string) json.RawMessage {
	parts := make([]string, 0, len(titles))
	for _, title := range titles {
		parts = append(parts, fmt.Sprintf(
			`{"title":%q,"summary":"s","brief":"b","acceptance":"a","grade":%q}`, title, gradeMechanical))
	}
	return json.RawMessage(fmt.Sprintf(`{"evidence":%q,"parts":[%s]}`,
		evidence, strings.Join(parts, ",")))
}

// gradedDivideNest is a division fixture whose graph has a ratings store behind
// it — the field the production graph fills from the person's profile directory
// ([Agent.graph]) and every scripted graph leaves nil.
func gradedDivideNest(t *testing.T, profile string) *divideNest {
	t.Helper()
	nest := newDivideNestOn(t, wideBrief, 0, &scriptedCompleter{},
		tierSettings(map[string]string{roles.TierKey(roles.TierHigh): carefulModelID}))
	nest.graph.grades = newTaskGrades(profile)
	return nest
}

// seedRejections is what the store would hold after N settled nodes of one kind
// that the check would not accept. It goes in through the same door a settle
// uses, so nothing here knows the file format.
func seedRejections(profile, model, title string, times int) {
	grades := newTaskGrades(profile)
	for i := 0; i < times; i++ {
		grades.keep(taskGradeRecord{
			Model: model, Kind: taskKindOf(title), Outcome: "not accepted",
			Verdict: provider.VerdictSemanticFailure, Session: "seed", Node: uint64(i + 1),
		})
	}
}

// THE SECOND HALF OF THE LOOP, AND THE WHOLE POINT OF IT. The dividing worker
// called this part ordinary work — which is what it called the four test-writing
// lanes on the run this issue came from — and the store says work of this shape
// keeps being turned down on that model. The part is minted on the careful tier.
func TestAKindThatKeepsBeingTurnedDownEarnsTheCarefulTier(t *testing.T) {
	profile := t.TempDir()
	seedRejections(profile, "test/model", "tests for the rail", 2)
	nest := gradedDivideNest(t, profile)

	nest.divide(t, divideTitledArgs(wideEvidence, "tests for the composer", "the eleven adapters"))

	kids := nest.graph.children(nest.parent.id)
	if len(kids) != 2 {
		t.Fatalf("the division bore %d parts, want 2", len(kids))
	}
	if got := kids[0].spec.model; got != carefulModelID {
		t.Fatalf("a part of a kind the store keeps turning down runs on %q, want the careful tier's model", got)
	}
	// AND ONLY THAT KIND. A part of work the store has never seen is untouched,
	// so the lift is evidence about a kind and not a mood about a division.
	if got := kids[1].spec.model; got != "test/model" {
		t.Fatalf("a part of a kind nothing was measured about runs on %q, want its parent task's model", got)
	}
}

// ONE REJECTION IS NOT "KEEPS GETTING REJECTED". The gate is two checked
// settles, which is the fewest that can tell a pattern from an accident.
func TestOneRejectionDoesNotMoveATier(t *testing.T) {
	profile := t.TempDir()
	seedRejections(profile, "test/model", "tests for the rail", 1)
	nest := gradedDivideNest(t, profile)

	nest.divide(t, divideTitledArgs(wideEvidence, "tests for the composer", "tests for the rail"))

	for _, kid := range nest.graph.children(nest.parent.id) {
		if got := kid.spec.model; got != "test/model" {
			t.Fatalf("one rejection lifted a part onto %q; the gate is %d", got, TaskGradeEvidence)
		}
	}
}

// AND A KIND THAT HOLDS IS LEFT WHERE IT IS. Evidence that points the other way
// is still evidence, and the ordinary road must stay ordinary — otherwise the
// loop would be a ratchet that only ever spends more.
func TestAKindThatKeepsHoldingStaysOnTheCheapTier(t *testing.T) {
	profile := t.TempDir()
	grades := newTaskGrades(profile)
	for i := 0; i < 4; i++ {
		grades.keep(taskGradeRecord{
			Model: "test/model", Kind: taskKindOf("tests for the rail"), Outcome: "landed",
			Verdict: provider.VerdictVerifiedSuccess, Session: "seed", Node: uint64(i + 1),
		})
	}
	nest := gradedDivideNest(t, profile)

	nest.divide(t, divideTitledArgs(wideEvidence, "tests for the composer", "tests for the rail"))

	for _, kid := range nest.graph.children(nest.parent.id) {
		if got := kid.spec.model; got != "test/model" {
			t.Fatalf("a kind the check keeps accepting was lifted onto %q", got)
		}
	}
}

// THE LIFT IS WRITTEN DOWN, so an autopsy can tell a part somebody CALLED
// careful from one that EARNED it. Nothing else in the record says which.
func TestTheRecordSaysWhichPartsTheStoreLifted(t *testing.T) {
	profile := t.TempDir()
	seedRejections(profile, "test/model", "tests for the rail", 2)
	nest := gradedDivideNest(t, profile)

	nest.divide(t, divideTitledArgs(wideEvidence, "tests for the composer", "the eleven adapters"))

	var lifted []string
	for _, division := range journaledDivisions(t, nest.journal) {
		if len(division.Lifted) > 0 {
			lifted = division.Lifted
		}
	}
	if len(lifted) != 1 || lifted[0] != "tests for the composer" {
		t.Fatalf("the division's record says %v was lifted, want the one part the store spoke about", lifted)
	}
}

// AN INSTALL WITH NO TIERS CONFIGURED PAYS NOTHING FOR ANY OF THIS. The careful
// tier floors on the model the task is already on, so there is nothing to lift a
// part to — and the record must not claim a decision nobody made.
func TestWithNoTiersConfiguredTheStoreLiftsNothing(t *testing.T) {
	profile := t.TempDir()
	seedRejections(profile, "test/model", "tests for the rail", 3)
	nest := newDivideNest(t, wideBrief, 0)
	nest.graph.grades = newTaskGrades(profile)

	nest.divide(t, divideTitledArgs(wideEvidence, "tests for the composer", "tests for the rail"))

	for _, kid := range nest.graph.children(nest.parent.id) {
		if got := kid.spec.model; got != "test/model" {
			t.Fatalf("a part was lifted onto %q on an install with no tiers", got)
		}
	}
	for _, division := range journaledDivisions(t, nest.journal) {
		if len(division.Lifted) > 0 {
			t.Fatalf("the record claims %v was lifted where there was nowhere to lift it to", division.Lifted)
		}
	}
}

// ── the kind itself ─────────────────────────────────────────────────────────

// A KIND IS THE DIVIDER'S OWN WORDS AND NOTHING ELSE — no enum, no domain list,
// no rule about what any word means. It is cut to words, lowercased, deduped and
// capped, and it keeps its order so the class reads as a name on `aforge models`.
func TestAKindIsTheWordsSomebodyWroteForTheWork(t *testing.T) {
	for title, want := range map[string]string{
		"The Eleven Adapters":            "the eleven adapters",
		"tests for the rail":             "tests for the rail",
		"  Rail — tests!  ":              "rail tests",
		"tests, tests and more tests":    "tests and more",
		"one two three four five six se": "one two three four five six",
		"":                               "",
	} {
		if got := taskKindOf(title); got != want {
			t.Fatalf("%q became the kind %q, want %q", title, got, want)
		}
	}
}

// TWO NAMES FOR ONE KIND OF WORK ARE ONE POPULATION, and two names for two are
// not. Half the words of the shorter side is the whole rule.
func TestKindsMeetOnHalfTheirWordsAndNoFewer(t *testing.T) {
	for _, test := range []struct {
		one, other string
		meet       bool
	}{
		{"tests for the rail", "tests for the composer", true},
		{"the eleven adapters", "the twelve adapters", true},
		{"tests for the rail", "the eleven adapters", false},
		{"write the adapters", "the release notes", false},
		{"adapters", "the eleven adapters", true},
		{"", "tests for the rail", false},
		// AND THE TRADE, WRITTEN DOWN AS A CASE. Two names that differ by one
		// word are one family whichever word it is, so "write the adapters" and
		// "write the tests" pool as well. No rule over word sets can separate
		// that pair from the one above it without a grammar, and the two ways of
		// being wrong are not the same size: a family drawn too wide costs one
		// part on a dearer model, and one drawn too tight costs a lift that
		// never happens.
		{"write the adapters", "write the tests", true},
	} {
		if got := taskKindsMeet(test.one, test.other); got != test.meet {
			t.Fatalf("%q and %q meet=%v, want %v", test.one, test.other, got, test.meet)
		}
		if got := taskKindsMeet(test.other, test.one); got != test.meet {
			t.Fatalf("the reading is not symmetric: %q against %q", test.other, test.one)
		}
	}
}

// AND THE POOLING IS ACROSS THE KINDS THAT MEET, so evidence gathered under one
// name reaches the part named the other way. A store that only ever answered
// about an exact string would learn nothing anybody could use: a division names
// its parts once and never again.
func TestEvidenceUnderOneNameReachesTheKindItMeets(t *testing.T) {
	profile := t.TempDir()
	seedRejections(profile, "cheap/model", "tests for the rail", 2)
	grades := newTaskGrades(profile)

	near := grades.reading("cheap/model", taskKindOf("tests for the composer"))
	if near.Count != 2 || near.Rating >= 0 {
		t.Fatalf("the near kind reads %+v, want the two rejections behind it", near)
	}
	far := grades.reading("cheap/model", taskKindOf("the eleven adapters"))
	if far.Count != 0 {
		t.Fatalf("unrelated work read %+v, want nothing", far)
	}
	other := grades.reading("other/model", taskKindOf("tests for the composer"))
	if other.Count != 0 {
		t.Fatalf("another model read %+v, want nothing — a rating is about one model", other)
	}
}

// A SESSION WITH NO PROFILE BEHIND IT GRADES NOTHING AND READS NOTHING, which is
// the absence law: a headless run, a node's own agent and every scripted graph
// in this package are byte-identical to what they were before this file existed.
func TestWithNoProfileTheLoopIsAbsentRatherThanBroken(t *testing.T) {
	if newTaskGrades("  ") != nil {
		t.Fatal("a blank profile directory built a store")
	}
	var absent *taskGrades
	absent.keep(taskGradeRecord{Model: "m", Kind: "k", Verdict: provider.VerdictVerifiedSuccess})
	if absent.saysCareful("m", "k") {
		t.Fatal("a store nobody has says work needs the careful tier")
	}
	if reading := absent.reading("m", "k"); reading.Count != 0 {
		t.Fatalf("a store nobody has read %+v", reading)
	}
}

// A HISTORICAL NOTES ENDING stays `stopped` in the record rather than changing
// meaning when reopened by a build that no longer emits that ending.
func TestAHistoricalNotesEndingRemainsGradedStopped(t *testing.T) {
	nest := newGradeNest(t, &scriptedCompleter{})

	nest.settleWith(t, "tests for the rail", "cheap/model", "", 0, TaskFailed,
		func(node *TaskNode) { node.ending = TaskEndingNotes })

	rows := nest.rows(t)
	if len(rows) != 1 {
		t.Fatalf("a settled node wrote %d diary rows, want 1", len(rows))
	}
	if rows[0].Outcome != "stopped" {
		t.Fatalf("the outcome reads %q, want %q", rows[0].Outcome, "stopped")
	}
	// AND THE OTHER ENDINGS ARE UNMOVED. A halted run that genuinely ran out is
	// still the run that ran out.
	for ending, want := range map[TaskEnding]string{
		TaskEndingNotes:    "stopped",
		TaskEndingSteps:    "did not finish",
		TaskEndingCircling: "did not finish",
		TaskEndingRefused:  "not accepted",
	} {
		if got := taskGradeOutcome(TaskFailed, ending, false); got != want {
			t.Errorf("%q graded %q, want %q", ending, got, want)
		}
	}
	// AND A HISTORICAL NODE THAT LANDED IS NEVER STOPPED, whatever older ending
	// value remained beside its successful state.
	if got := taskGradeOutcome(TaskDone, TaskEndingNotes, false); got != "landed" {
		t.Errorf("a finished node graded %q, want landed", got)
	}
}
