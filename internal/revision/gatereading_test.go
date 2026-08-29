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
			// read at all.
			name:     "no checklist, and the world read anyway",
			evidence: Evidence{Workspace: t.TempDir()},
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
