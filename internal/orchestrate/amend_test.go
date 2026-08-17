package orchestrate

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// bare is an orchestrator with no planner and no executor: everything in this
// file is about what an amendment is ALLOWED to say, which is decided before
// either of them is reached.
func bare() *Orchestrator {
	return New("the goal", plannerFunc(func(context.Context, View) (Amendment, error) {
		return Amendment{}, nil
	}), execFunc(func(context.Context, Node, []NodeStatus) (string, float64, error) {
		return "", 0, nil
	}), Options{})
}

func TestParseAmendment(t *testing.T) {
	cases := []struct {
		name  string
		raw   string
		check func(*testing.T, Amendment, error)
	}{
		{"silence is a noop", "", func(t *testing.T, a Amendment, err error) {
			if err != nil || !a.Empty() {
				t.Fatalf("got %+v, %v", a, err)
			}
		}},
		{"the word is a noop", "  NOOP\n", func(t *testing.T, a Amendment, err error) {
			if err != nil || !a.Empty() {
				t.Fatalf("got %+v, %v", a, err)
			}
		}},
		{"an empty object is a noop", "{}", func(t *testing.T, a Amendment, err error) {
			if err != nil || !a.Empty() {
				t.Fatalf("got %+v, %v", a, err)
			}
		}},
		{"a fenced reply is salvaged", "Here you go:\n```json\n{\"note\": \"two more\"}\n```\n", func(t *testing.T, a Amendment, err error) {
			if err != nil || a.Note != "two more" {
				t.Fatalf("got %+v, %v", a, err)
			}
		}},
		{"typographic quotes are salvaged", "{“note”: “fine”}", func(t *testing.T, a Amendment, err error) {
			if err != nil || a.Note != "fine" {
				t.Fatalf("got %+v, %v", a, err)
			}
		}},
		{"nodes decode", `{"add":[{"id":"n1","goal":"read the file","needs":["n0"],"write_scope":["docs"]}]}`,
			func(t *testing.T, a Amendment, err error) {
				if err != nil {
					t.Fatal(err)
				}
				if len(a.Add) != 1 || a.Add[0].ID != "n1" || a.Add[0].Needs[0] != "n0" || a.Add[0].WriteScope[0] != "docs" {
					t.Fatalf("got %+v", a)
				}
			}},
		{"a key nobody asked for is refused", `{"add":[],"plan":"whatever"}`, func(t *testing.T, _ Amendment, err error) {
			if err == nil {
				t.Fatalf("an unknown key is a model answering a different question")
			}
		}},
		{"prose is not an amendment", "I think we should probably add two nodes here.", func(t *testing.T, _ Amendment, err error) {
			if err == nil {
				t.Fatalf("that is not JSON and no rung makes it JSON")
			}
		}},
	}
	for _, each := range cases {
		t.Run(each.name, func(t *testing.T) {
			amendment, err := ParseAmendment(each.raw)
			each.check(t, amendment, err)
		})
	}
}

// TestCancelNeverTouchesRunning is the commitment law.
func TestCancelNeverTouchesRunning(t *testing.T) {
	run := bare()
	run.apply(Amendment{Add: []Node{{ID: "a", Goal: "work"}, {ID: "b", Goal: "more"}}})
	run.nodes[0].State = Running

	err := run.validate(Amendment{Cancel: []Cancel{{ID: "a", Reason: "changed my mind"}}})
	if err == nil || !strings.Contains(err.Error(), "RUNNING") {
		t.Fatalf("cancelling a running node must be refused, got %v", err)
	}
	if err := run.validate(Amendment{Cancel: []Cancel{{ID: "b"}}}); err != nil {
		t.Fatalf("cancelling a pending node is legal: %v", err)
	}

	run.nodes[0].State = Done
	if err := run.validate(Amendment{Cancel: []Cancel{{ID: "a"}}}); err == nil {
		t.Fatalf("a completed node is a fact and cannot be cancelled")
	}
}

func TestValidateRefusesNonsense(t *testing.T) {
	run := bare()
	run.apply(Amendment{Add: []Node{{ID: "a", Goal: "work"}}})

	cases := []struct {
		name string
		am   Amendment
	}{
		{"a duplicate id", Amendment{Add: []Node{{ID: "a", Goal: "again"}}}},
		{"two of the same id at once", Amendment{Add: []Node{{ID: "n", Goal: "one"}, {ID: "n", Goal: "two"}}}},
		{"no id", Amendment{Add: []Node{{Goal: "nameless"}}}},
		{"no goal", Amendment{Add: []Node{{ID: "n"}}}},
		{"a need nobody has", Amendment{Add: []Node{{ID: "n", Goal: "work", Needs: []string{"ghost"}}}}},
		{"a need on itself", Amendment{Add: []Node{{ID: "n", Goal: "work", Needs: []string{"n"}}}}},
		{"a circle", Amendment{Add: []Node{
			{ID: "x", Goal: "work", Needs: []string{"y"}},
			{ID: "y", Goal: "work", Needs: []string{"x"}},
		}}},
		{"a cancel of nothing", Amendment{Cancel: []Cancel{{ID: "ghost"}}}},
		{"a need on what the same amendment cancels", Amendment{
			Cancel: []Cancel{{ID: "a"}},
			Add:    []Node{{ID: "n", Goal: "work", Needs: []string{"a"}}},
		}},
	}
	for _, each := range cases {
		t.Run(each.name, func(t *testing.T) {
			if err := run.validate(each.am); err == nil {
				t.Fatalf("%s was accepted", each.name)
			}
		})
	}

	// And the legal one, so the table above is not just refusing everything.
	if err := run.validate(Amendment{Add: []Node{{ID: "n", Goal: "work", Needs: []string{"a"}}}}); err != nil {
		t.Fatalf("a legal amendment was refused: %v", err)
	}
}

// TestCancelCascadesToWhatWasWaiting: dropping a node drops the pending work
// that could never become ready without it.
func TestCancelCascadesToWhatWasWaiting(t *testing.T) {
	run := bare()
	run.apply(Amendment{Add: []Node{
		{ID: "a", Goal: "first"},
		{ID: "b", Goal: "needs a", Needs: []string{"a"}},
		{ID: "c", Goal: "needs b", Needs: []string{"b"}},
		{ID: "d", Goal: "independent"},
	}})
	run.apply(Amendment{Cancel: []Cancel{{ID: "a", Reason: "it turned out to be unnecessary"}}})

	snap := run.Snapshot()
	if len(snap.Nodes) != 1 || snap.Nodes[0].ID != "d" {
		t.Fatalf("the cascade kept the wrong nodes: %+v", snap.Nodes)
	}
}

// TestWriteScopeSerializes is law 10: two nodes that write the same path are
// not independent, whatever the planner believed.
func TestWriteScopeSerializes(t *testing.T) {
	run := bare()
	run.apply(Amendment{Add: []Node{
		{ID: "a", Goal: "edit the package", WriteScope: []string{"internal/session"}},
		{ID: "b", Goal: "edit one file", WriteScope: []string{"internal/session/orchestrate.go"}},
		{ID: "c", Goal: "somewhere else", WriteScope: []string{"docs"}},
		{ID: "d", Goal: "read only"},
	}})
	snap := run.Snapshot()
	needs := map[string][]string{}
	for _, node := range snap.Nodes {
		needs[node.ID] = node.Needs
	}
	if len(needs["a"]) != 0 {
		t.Fatalf("the first writer waits on nobody: %+v", needs["a"])
	}
	if len(needs["b"]) != 1 || needs["b"][0] != "a" {
		t.Fatalf("b writes under a's scope and must be serialized behind it: %+v", needs["b"])
	}
	if len(needs["c"]) != 0 || len(needs["d"]) != 0 {
		t.Fatalf("disjoint scopes are independent: c=%v d=%v", needs["c"], needs["d"])
	}
}

// TestInvalidAmendmentIsRepairedOnce, then dropped with a note.
func TestInvalidAmendmentIsRepairedOnce(t *testing.T) {
	run := bare()
	run.apply(Amendment{Add: []Node{{ID: "a", Goal: "work"}}})
	run.nodes[0].State = Running

	fixer := &repairPlanner{fixed: Amendment{Note: "left it alone"}}
	run.planner = fixer
	run.absorb(context.Background(), thought{amendment: Amendment{Cancel: []Cancel{{ID: "a"}}}})
	if fixer.asked != 1 {
		t.Fatalf("the planner was handed its refusal %d times, want once", fixer.asked)
	}
	if snap := run.Snapshot(); len(snap.Notes) != 1 || snap.Notes[0] != "left it alone" {
		t.Fatalf("the repair did not land: %+v", run.Snapshot().Notes)
	}

	// A repair that is refused too is dropped, and says so.
	fixer.fixed = Amendment{Cancel: []Cancel{{ID: "a"}}}
	run.absorb(context.Background(), thought{amendment: Amendment{Cancel: []Cancel{{ID: "a"}}}})
	notes := strings.Join(run.Snapshot().Notes, " | ")
	if !strings.Contains(notes, "dropped") {
		t.Fatalf("a twice-refused amendment is dropped out loud: %s", notes)
	}
	if stateOf(run.Snapshot(), "a") != Running {
		t.Fatalf("the running node was touched")
	}
}

// TestPlannerErrorIsANotAnAmendment: the planner's own failure never reaches
// the frontier.
func TestPlannerErrorIsNotAnAmendment(t *testing.T) {
	run := bare()
	run.absorb(context.Background(), thought{err: errors.New("the model timed out")})
	if snap := run.Snapshot(); len(snap.Nodes) != 0 || len(snap.Notes) != 1 {
		t.Fatalf("got %+v", snap)
	}
}

type repairPlanner struct {
	fixed Amendment
	asked int
}

func (r *repairPlanner) Plan(context.Context, View) (Amendment, error) { return Amendment{}, nil }

func (r *repairPlanner) Repair(_ context.Context, _ View, why string) (Amendment, error) {
	r.asked++
	if why == "" {
		return Amendment{}, errors.New("a repair with no complaint is not a repair")
	}
	return r.fixed, nil
}
