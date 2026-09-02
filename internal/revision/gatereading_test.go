package revision

// The gate's own reading, and the record of it.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/plan"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/verify"
)

// igelWorkspace is the tree igel s9 delivered into: a python package with a
// declared pytest suite beside it. The suite is deliberately runnable, so a
// reading of it is a real reading rather than a refusal.
func igelWorkspace(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for path, body := range map[string]string{
		"Makefile":                     "test:\n\tpython3 -m pytest -rA tests/\n",
		"setup.py":                     "from setuptools import setup\nsetup(name=\"igel\")\n",
		"igel/__init__.py":             "",
		"igel/igel.py":                 "class Igel:\n    pass\n",
		"tests/test_igel/test_igel.py": "def test_fit():\n    assert True\n",
	} {
		full := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// gateStore is a store holding one node for a gate's verdict to be journaled
// against.
func gateStore(t *testing.T) *store.Store {
	t.Helper()
	graph, err := store.Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { graph.Close() })
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{{
		ID: "task-2", Brief: "persist the feature schema", Stage: 1,
	}}}, store.Provenance{
		Origin: store.OriginUser, SessionID: "s1", Intent: "persist the feature schema",
	}); err != nil {
		t.Fatal(err)
	}
	return graph
}

// THE GATE IS THE ONLY READER ON THE GENERALIST'S PATH, AND IT WAS SILENT.
//
// igel s9 and ink s9 finished with ZERO verification events between them — not a
// reading, not a refusal, not a row. Neither run put a single node on the worker
// that photographs, so nothing in either store said whether the project had been
// read, could not be read, or was never asked. ofetch s9, on the identical
// binary, journaled four readings, because one of its nodes happened to run
// under `bare`: the record of a run's own verification was a fact about which
// worker the ruler picked.
//
// The gate takes a reading of the tree it is judging when nobody else did, and
// it always did. What it never did was say so.
func TestTheGateSaysWhatItReadOfTheTreeItJudges(t *testing.T) {
	ForgetChecklists()
	graph := gateStore(t)
	root := igelWorkspace(t)
	points := []plan.Point{{
		Behaviour: "After fit, write feature_schema.joblib in the results directory",
		Quote:     "After fit, write feature_schema.joblib in the results directory",
	}}
	grounds := Grounds{Intent: "When fit runs with dataset.features configured, the selected raw " +
		"feature schema is not persisted. After fit, write feature_schema.joblib in the results " +
		"directory and record feature_schema_path in description.json."}
	evidence := Evidence{
		Accept:    points,
		Workspace: root,
		Artifacts: []string{filepath.Join(root, "igel/igel.py")},
	}
	// A gate with a wall, exactly as the leaf path gives it one.
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	settleAcceptance(ctx, config.Config{}, nil, graph, store.Node{ID: "task-2"},
		evidence, grounds, "worker/model", Judgment{Pass: true, Checked: true})

	readings, err := graph.VerificationsFor("task-2")
	if err != nil {
		t.Fatal(err)
	}
	if len(readings) == 0 {
		t.Fatal("the gate judged a tree and journaled nothing about reading it")
	}
	if len(readings) > 1 {
		t.Errorf("the gate wrote %d rows for one reading: %#v", len(readings), readings)
	}
	row := readings[0]
	if !strings.Contains(row.When, "gate") {
		t.Errorf("the row does not say which reader took it: %q", row.When)
	}
	// Read or refused, it says which and it says why when it did not.
	if !row.Read && strings.TrimSpace(row.Why) == "" {
		t.Error("a reading that was not taken journaled no reason")
	}
	if row.Read && strings.TrimSpace(row.Command) == "" {
		t.Error("a reading that was taken journaled no command")
	}
}

// And every way of NOT reading is a row too, because the absence of the event is
// the one spelling all of them shared.
func TestEveryRefusalToReadReachesTheRecord(t *testing.T) {
	points := []plan.Point{{Behaviour: "write feature_schema.joblib", Quote: "write feature_schema.joblib"}}
	grounds := Grounds{Intent: "After fit, write feature_schema.joblib in the results directory."}

	for _, probe := range []struct {
		name     string
		evidence Evidence
		points   []plan.Point
		timed    bool
		says     string
	}{
		{
			name:     "no workspace to read",
			evidence: Evidence{Accept: points},
			points:   points, timed: true, says: "workspace",
		},
		{
			name:     "no wall to size a reading against",
			evidence: Evidence{Accept: points, Workspace: t.TempDir()},
			points:   points, says: "deadline",
		},
		{
			// A job with no checklist still has its world read. The checklist
			// governs coverage; it has no say in whether the project can be
			// read at all. The work left a file behind, which is what gives
			// this gate something to read at all — see the rule that a job
			// which changed nothing, on a request that names nothing, is told
			// there is nothing to read rather than reading everything.
			name:     "no checklist, and the world read anyway",
			evidence: Evidence{Workspace: t.TempDir(), Artifacts: []string{"notes.md"}},
			timed:    true, says: "declares no way of checking itself",
		},
	} {
		t.Run(probe.name, func(t *testing.T) {
			ForgetChecklists()
			graph := gateStore(t)
			ctx := context.Background()
			if probe.timed {
				timed, cancel := context.WithTimeout(ctx, time.Minute)
				defer cancel()
				ctx = timed
			}
			settleAcceptance(ctx, config.Config{}, nil, graph, store.Node{ID: "task-2"},
				probe.evidence, grounds, "worker/model", Judgment{Pass: true, Checked: true})

			readings, err := graph.VerificationsFor("task-2")
			if err != nil {
				t.Fatal(err)
			}
			if len(readings) != 1 {
				t.Fatalf("want one row saying why nothing was read, got %d: %#v", len(readings), readings)
			}
			if readings[0].Read {
				t.Errorf("a refusal was journaled as a reading: %#v", readings[0])
			}
			if !strings.Contains(readings[0].Why, probe.says) {
				t.Errorf("the row does not say why: %q", readings[0].Why)
			}
		})
	}
}

// AN EMPTY LADDER IS IMPOSSIBLE. A project that declares a way of checking itself
// always has the whole suite as its last rung, whatever the scope in front of it
// chose — so the only way to have no strategy at all is to have no declaration,
// and that answer carries its own sentence rather than a silence.
func TestTheWholeSuiteIsAlwaysTheLastRung(t *testing.T) {
	root := igelWorkspace(t)
	// The igel s9 request names no path at all: a subject, a file basename and
	// a couple of field names.
	focus := verify.Focus(verify.NamedSubjects(
		"When fit runs with dataset.features configured, the selected raw feature schema " +
			"is not persisted. After fit, write feature_schema.joblib in the results directory " +
			"and record feature_schema_path, input_features and dropped_features in description.json."))
	ladder, ok := verify.ReadingStrategies(root, verify.Discover(root), focus)
	if !ok || len(ladder) == 0 {
		t.Fatal("a project with a declared pytest suite produced no strategy at all")
	}
	if last := ladder[len(ladder)-1]; last.Scope != verify.ScopeWhole {
		t.Errorf("the ladder does not end at the whole suite: %#v", last)
	}
	// And a project that declares nothing says so rather than going quiet.
	silent := verify.Photograph(context.Background(), t.TempDir(), time.Hour, nil, verify.Pace{})
	if silent.Taken {
		t.Fatal("an empty directory produced a reading")
	}
	if strings.TrimSpace(silent.Unread) == "" {
		t.Error("a project with no verification produced no sentence saying so")
	}
}

// A PASS OVER A SUITE NOBODY COULD READ IS NOT WHOLE, AND A JOB WITH NO
// CHECKLIST IS NOT AN EXCEPTION TO THAT.
//
// The reading used to sit BELOW the checklist, and the early return for a job
// that stated none took the reading with it: nothing asked whether the project
// could be read, `Unreadable` was unreachable on that path, and the delivery gate
// settled Whole() over a verification nobody had looked at. Exit 0, on a run
// where the one question that could have said otherwise was never put.
func TestAPassOverAnUnreadableSuiteIsPartialWithOrWithoutAChecklist(t *testing.T) {
	// A project that DECLARES a way of checking itself, and a run that could not
	// read it. That is the unanswered question — not the unanswerable one.
	unreadable := Evidence{
		Workspace: t.TempDir(),
		Verification: verify.Reading{
			Plan: verify.Plan{Entrypoints: []verify.Entrypoint{
				{Kind: verify.KindTest, Command: "pytest"}}},
			Unread: "`pytest` was killed at its ceiling without naming a check",
		},
	}
	grounds := Grounds{Intent: "After fit, write feature_schema.joblib in the results directory."}
	points := []plan.Point{{
		Behaviour: "write feature_schema.joblib",
		Quote:     "write feature_schema.joblib in the results directory",
	}}

	for _, probe := range []struct {
		name   string
		accept []plan.Point
	}{
		{name: "with a checklist", accept: points},
		{name: "with no checklist at all"},
	} {
		t.Run(probe.name, func(t *testing.T) {
			ForgetChecklists()
			graph := gateStore(t)
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			evidence := unreadable
			evidence.Accept = probe.accept

			settled := settleAcceptance(ctx, config.Config{}, nil, graph,
				store.Node{ID: "task-2"}, evidence, grounds, "worker/model",
				Judgment{Pass: true, Checked: true})

			if !settled.Unreadable {
				t.Fatalf("a pass over a suite nothing could read settled readable: %#v", settled)
			}
			if !strings.Contains(settled.Unmeasured, "could be read") {
				t.Errorf("the verdict does not say what was unreadable: %q", settled.Unmeasured)
			}
			// And that is what the door reads.
			if (store.DeliveryGate{Pass: true, Unreadable: settled.Unreadable}).Whole() {
				t.Error("the delivery gate called it whole")
			}
		})
	}
}

// THE CHECKLIST IS REMEMBERED WHERE IT IS READ, NOT WHERE IT IS SETTLED.
//
// ofetch s10: the planner read 47 points onto `task-2`'s spec and journaled
// them; `task-2` was handed over without ever reaching a delivery gate; and
// `task-2-x1` — planned afresh, so carrying no Accept of its own — reached the
// only gate of the run with no checklist at all. Its event holds `pass: true`
// and nothing else: no mapping, no finding, no `unmeasured`. The coverage
// question was not answered wrongly, it was never asked, and the run left at 42
// of 47. The memory was there; only the gate ever wrote to it.
func TestAContinuationInheritsTheChecklistTheRequestWasReadInto(t *testing.T) {
	ForgetChecklists()
	request := "Implement an opt-in per-origin circuit breaker for fetch requests. " +
		"When circuitBreaker: true, defaults are threshold = 5 and halfOpenMaxRequests = 1."
	points := []plan.Point{
		{Behaviour: "Circuit breaker is opt-in per origin for fetch requests",
			Quote: "opt-in per-origin circuit breaker for fetch requests"},
		{Behaviour: "When circuitBreaker: true, defaults are halfOpenMaxRequests = 1",
			Quote: "When circuitBreaker: true, defaults are threshold = 5 and halfOpenMaxRequests = 1"},
	}
	// The planner reads the request onto the first node's spec, and that is the
	// moment the job learns what it is judged against.
	RememberChecklistForRequest(request, points)

	// The continuation's own spec carries none of it.
	grounds := Grounds{Intent: request}
	if held := Held(ChecklistFor(verify.JobKey(request)), grounds); len(held) != len(points) {
		t.Fatalf("the continuation inherited %d of %d points", len(held), len(points))
	}
	graph := gateStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	settled := settleAcceptance(ctx, config.Config{}, nil, graph, store.Node{ID: "task-2"},
		Evidence{Workspace: t.TempDir()}, grounds, "worker/model",
		Judgment{Pass: true, Checked: true})
	// With no reading anywhere the coverage question is unanswerable, and the
	// verdict says so — which is already more than an event holding `pass: true`
	// and nothing else.
	if strings.TrimSpace(settled.Unmeasured) == "" {
		t.Error("a gate that inherited a checklist and could measure nothing said nothing")
	}
}

// A JUDGE'S SAY-SO IS VOCABULARY, AND THE MAPPING GOES THROUGH THE SAME DOOR A
// CITATION DOES.
//
// The defaults family is the one ofetch keeps failing on: s8's gate named
// `When circuitBreaker: true, defaults are halfOpenMaxRequests = 1` and its
// siblings as exercised by nothing, and the suite meanwhile grew checks about
// the breaker in general. A mapping weighed on vocabulary alone eventually pairs
// those two — the words are about the same subject, and the check is not about
// that behaviour.
func TestAMappingIsGroundedInWhatTheCheckItselfNames(t *testing.T) {
	root := t.TempDir()
	body := "" +
		"import { describe, it, expect } from 'vitest'\n" +
		"describe('circuit breaker', () => {\n" +
		"  it('opens after repeated failures', async () => {\n" +
		"    await $fetch('/x', { circuitBreaker: true })\n" +
		"  })\n" +
		"  it('honours an explicit cooldown', async () => {\n" +
		"    await $fetch('/x', { circuitBreaker: { cooldown: 1000 } })\n" +
		"  })\n" +
		"})\n"
	if err := os.MkdirAll(filepath.Join(root, "test"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "test/circuit-breaker.test.ts"),
		[]byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	record := []string{"test/circuit-breaker.test.ts"}

	points := []plan.Point{
		{Behaviour: "Request option circuitBreaker accepts an object with cooldown"},
		{Behaviour: "When circuitBreaker: true, defaults are halfOpenMaxRequests = 1"},
		{Behaviour: "When circuitBreaker: true, defaults are failureStatusCodes = [408, 409]"},
		{Behaviour: "Normal scrolling must still update the visible viewport"},
	}
	// What a judge asked to be helpful hands back: every point paired with the
	// one check file that exists.
	const check = "test/circuit-breaker.test.ts > circuit breaker > opens after repeated failures"
	generous := make([]store.ExercisedPoint, 0, len(points))
	for _, point := range points {
		generous = append(generous, store.ExercisedPoint{Point: point.Behaviour, Check: check})
	}

	grounded := GroundMapping(root, record, points, generous)
	if len(grounded) != len(points) {
		t.Fatalf("the door dropped rows: %d of %d", len(grounded), len(points))
	}
	// The cooldown option IS in that file, spelled the way the request spells it.
	if grounded[0].Check == "" {
		t.Error("a pairing the file itself supports was refused")
	}
	// These two are not, and no amount of talking about circuit breakers makes
	// them so.
	for _, index := range []int{1, 2} {
		if grounded[index].Check != "" {
			t.Errorf("%q was declared exercised by a check that never names it",
				points[index].Behaviour)
		}
	}
	// And a behaviour that spells no name at all is not judged here: there is no
	// structural question to ask of it, and answering "unexercised" to every such
	// behaviour would fail every prose request this program is given.
	if grounded[3].Check == "" {
		t.Error("a behaviour that names nothing distinctive was refused by a door about names")
	}
}

// The whole-name rule the adjacency is held to, asked of this door: `Log` inside
// `Logger` is not a mention of Log.
func TestGroundingAMappingNeverMatchesInsideAnotherName(t *testing.T) {
	if namesSymbol("class logger:\n    pass\n", "log") {
		t.Error("a name matched inside another name")
	}
	if !namesSymbol("from textual.widgets import log\n", "log") {
		t.Error("a whole name beside punctuation was not matched")
	}
	if !namesSymbol("await $fetch('/x', { circuitbreaker: true })", "circuitbreaker") {
		t.Error("a name beside a brace was not matched")
	}
}

// A PUBLIC NAME THIS WORK DELETED IS A FINDING, AND IT IS THE GATE'S OWN.
//
// igel s11: the check-level photograph read the finished tree as better — named
// 2 → 14, red 2 → 0 — while all twenty-four hidden tests failed at setup on
// `Igel.results_path`. The suite was not lying. A project only owns checks for
// what somebody wrote checks for, and nobody had written one for that attribute.
func TestARemovedPublicNameIsASourcedFinding(t *testing.T) {
	lost, removed := RemovedPublicNames([]string{"Igel.results_path", "Igel.description_file"})
	if !removed {
		t.Fatal("two deleted public attributes raised nothing")
	}
	if lost.Pass {
		t.Error("a delivery that deleted a public name passed")
	}
	if !strings.Contains(lost.Gaps,
		"This work removed a public name that existed before it: Igel.results_path") {
		t.Errorf("the finding does not name what was lost:\n%s", lost.Gaps)
	}
	// SOURCED, so it is admitted with no citation weighed — exactly as a check
	// regression is, and for the same reason: nobody has to ask for the public
	// names their repository already had.
	if !lost.Sourced {
		t.Error("the finding would have to quote a request that never mentioned it")
	}
	if !lost.Checked {
		t.Error("a measured finding was recorded as unchecked")
	}
	if len(lost.Citations) == 0 {
		t.Error("the finding travels with no list for a repair to aim at")
	}
	// Nothing removed is no claim, not an acquittal.
	if _, any := RemovedPublicNames(nil); any {
		t.Error("a run that measured nothing raised a finding anyway")
	}
	if _, any := RemovedPublicNames([]string{"  "}); any {
		t.Error("whitespace was read as a lost name")
	}
}

// A LEAF WHOSE OWN CHECKS ARE RED HAS NOT FINISHED; A LEAF THAT TURNED SOMEBODY
// ELSE'S CHECK RED HAS BROKEN THE REPOSITORY. Two findings, two sentences.
func TestTheChecksThisWorkWroteAreTheirOwnFinding(t *testing.T) {
	red := []string{
		"IntersectionObserver initial observation queuing Queues an entry for each target",
		"IntersectionObserver observation-order preservation",
	}
	unfinished, any := OwnChecksFailing(red)
	if !any {
		t.Fatal("a leaf whose own new checks are red raised nothing")
	}
	if unfinished.Pass {
		t.Error("a leaf with red checks of its own passed")
	}
	if !strings.HasPrefix(unfinished.Gaps, "The checks this work wrote fail:") {
		t.Errorf("the finding does not say whose checks they are:\n%s", unfinished.Gaps)
	}
	if strings.Contains(unfinished.Gaps, "broke checks that were passing") {
		t.Errorf("a leaf that has not finished was accused of breaking things:\n%s",
			unfinished.Gaps)
	}
	// Sourced, so it is admitted with no citation weighed and buys the round a
	// regression buys.
	if !unfinished.Sourced || !unfinished.Checked {
		t.Errorf("the finding would have to quote a request that never mentioned it: %#v",
			unfinished)
	}
	if len(unfinished.OwnFailing) != len(red) {
		t.Errorf("the finding travels with no list of its own: %v", unfinished.OwnFailing)
	}
	if _, any := OwnChecksFailing(nil); any {
		t.Error("a run whose own checks all pass raised a finding anyway")
	}
	// And the two are different findings on the same evidence.
	regression, _ := Regressions(red)
	if regression.Gaps == unfinished.Gaps {
		t.Error("the two findings still say the same thing")
	}
}

// THE GATE DOES NOT PHOTOGRAPH A TREE NOTHING CHANGED.
//
// It is the same law the leaf's own second reading is written to, one seam
// later, because the leaf that did the work and the node that gets judged are
// routinely not the same node. The run measured in #429 read `go test -json
// ./...` over 4,587 tests on four "finished" trees that were byte for byte the
// tree the first reading had already been taken of.
func TestTheGateInheritsTheReadingOfATreeNothingChanged(t *testing.T) {
	verify.ForgetBaselines()
	t.Cleanup(verify.ForgetBaselines)
	graph := gateStore(t)
	root := igelWorkspace(t)
	job := verify.JobKey("run the suite and report the final line; change no files")
	verify.RememberBaseline(root, job, verify.TreeState(nil), verify.Reading{
		Taken: true,
		Before: verify.Result{
			Strategy: verify.Strategy{
				Command: "python3 -m pytest -rA", Runner: "pytest", Scope: verify.ScopeWhole},
			Reported: []string{"test_fit", "test_predict"},
		},
	})
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	// The job changed no file, so the tree in front of the gate is the tree in
	// the reading it is holding.
	reading := jobReading(ctx, graph, "task-2", Evidence{Workspace: root}, job)
	if !reading.AfterTaken {
		t.Fatal("the gate was left with no reading of the tree it is judging")
	}
	if len(reading.After.Reported) != 2 {
		t.Errorf("the roster that stands is not the one that was read: %#v", reading.After.Reported)
	}
	rows, err := graph.VerificationsFor("task-2")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || !rows[0].Inherited || !rows[0].Read {
		t.Fatalf("the gate did not journal one inherited reading: %#v", rows)
	}
}

// AND A JOB THAT CHANGED NOTHING, ON A REQUEST THAT NAMES NOTHING, HAS NOTHING
// TO READ — which is an answer, and a free one.
//
// What it used to buy was a reading of the whole repository, killed at its
// ceiling two minutes later, which answered neither the coverage question nor
// the regression one. NOTHING TO READ IS A FACT, AND A FACT ABOUT THE RUN
// REACHES THE RECORD.
func TestAGateWithNothingToReadSaysSoRatherThanReadingEverything(t *testing.T) {
	verify.ForgetBaselines()
	t.Cleanup(verify.ForgetBaselines)
	graph := gateStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	reading := jobReading(ctx, graph, "task-2", Evidence{Workspace: igelWorkspace(t)},
		verify.JobKey("report the final line the suite prints; change no files"))
	if reading.Taken {
		t.Error("a gate with nothing to read read the whole repository anyway")
	}
	rows, err := graph.VerificationsFor("task-2")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Read {
		t.Fatalf("the gate did not journal one refusal: %#v", rows)
	}
	if !strings.Contains(rows[0].Why, "nothing to read") {
		t.Errorf("the row does not say there was nothing to read: %q", rows[0].Why)
	}
}
