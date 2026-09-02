package lane

import (
	"go/ast"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/home"
	"github.com/Agent-Field/aforge-v2/internal/lane/control"
)

// ── THE WAITING LAWS ────────────────────────────────────────────────────────
//
// `docs/design/waiting/DESIGN.md` is the design and this file is the half of it
// the build holds. Each law is named as a sentence and each of them fails with
// a file name and a line number, in the convention `structure_test.go` set.
//
// SEVERAL OF THESE ARE RED ON PURPOSE. They were written before the work they
// describe, so that the work has somewhere to land and so that "done" is a
// thing the build says rather than a thing somebody claims. The design's "laws
// currently red" section lists which, and a lane of the build plan turns each
// of them green.

// installed is the controller factory, or a skipped-forward failure saying why
// there is not one. Every behavioural law here needs one and none of them may
// invent one, because a law that passes against a test double is a law about
// the double.
func installed(t *testing.T) control.Factory {
	t.Helper()
	build := Controller()
	if build == nil {
		t.Fatal("no waiting controller is installed — SetController is never called in a shipped file, " +
			"so every token-generating call falls through to the bare stream loop " +
			"(docs/design/waiting/DESIGN.md, lane W2)")
	}
	return build
}

// coldPlan is a request about a lane nothing whatever is believed about, on the
// role a person is reading. It is the state a cold store leaves every request
// in, and it is the state the whole design is measured against.
func coldPlan(now time.Time) control.Plan {
	return control.Plan{
		Lane:    "somebody",
		Ceiling: RoleTalk.Ceiling(),
		Floor:   ActionFloor,
		Lambda:  RoleTalk.Lambda(),
		Alts:    []control.Alternative{{Lane: "somebody-else"}},
		Began:   now,
	}
}

// TestEveryRoleWaitsForABoundedTime is the invariant, said about the table that
// sets it.
//
// A role that returned no ceiling would be a role outside the design: a call on
// it could wait forever on a belief that says "keep waiting", which is the
// defect this whole wave exists to end. The role decides HOW LONG and never
// WHETHER, so the assertion is about every row of the table at once.
func TestEveryRoleWaitsForABoundedTime(t *testing.T) {
	for _, role := range Roles() {
		ceiling := role.Ceiling()
		if ceiling <= 0 {
			t.Errorf("role %q has no ceiling", role)
			continue
		}
		if ceiling < ActionFloor {
			t.Errorf("role %q waits %s, which is under the floor a second request needs", role, ceiling)
		}
		if ceiling > time.Minute {
			t.Errorf("role %q waits %s before anything is done about it", role, ceiling)
		}
	}
	if got := RoleTalk.Ceiling(); got != VisiblePatience {
		t.Errorf("a person waits %s; the design says %s", got, VisiblePatience)
	}
}

// TestRoutingAndWaitingNeverShareANil is the root cause of the reported wait,
// stated as a law about a struct.
//
// [Choice] answers WHICH LANE. It carried the deadline and the alternative too,
// so a ledger with nothing to rank returned a zero Choice — no order, and, by
// the same value, no clock. A request with no routing opinion is ordinary; a
// request with no clock is the defect. The two questions are two types now, and
// this fails while they are one.
func TestRoutingAndWaitingNeverShareANil(t *testing.T) {
	fset, files := sources(t)
	ast.Inspect(files["contract.go"], func(node ast.Node) bool {
		spec, ok := node.(*ast.TypeSpec)
		if !ok || spec.Name.Name != "Choice" {
			return true
		}
		structure, ok := spec.Type.(*ast.StructType)
		if !ok {
			return false
		}
		for _, field := range structure.Fields.List {
			for _, name := range field.Names {
				if name.Name == "Deadline" || name.Name == "Alt" {
					t.Errorf("%s: Choice carries %s — waiting rides in the routing answer, so a request with no preference gets no clock",
						fset.Position(name.Pos()), name.Name)
				}
			}
		}
		return false
	})
}

// TestUnknownBeliefStillYieldsADeadline is the invariant from zero history.
//
// A cold store is the STEADY STATE for a model somebody picked after launch —
// no sheet has been fetched for it and no lane behind it has ever answered — so
// "we know nothing" may never mean "we wait forever". The ceiling is what says
// so, and it is reached without any belief at all.
func TestUnknownBeliefStillYieldsADeadline(t *testing.T) {
	build := installed(t)
	began := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	watch := build(coldPlan(began))
	for step := time.Duration(0); step <= 2*RoleTalk.Ceiling(); step += 100 * time.Millisecond {
		act := watch.Quiet(began.Add(step))
		if act.Kind == control.None {
			continue
		}
		if step < ActionFloor {
			t.Fatalf("acted after %s, under the floor", step)
		}
		if step > RoleTalk.Ceiling() {
			t.Fatalf("acted after %s, past the ceiling of %s", step, RoleTalk.Ceiling())
		}
		return
	}
	t.Fatalf("nothing was done about a silence of %s on a cold belief", 2*RoleTalk.Ceiling())
}

// TestThinkingDoesNotStopTheClock is the measured defect, in one assertion.
//
// A run of reasoning is the endpoint writing and none of it is on the screen. It
// keeps the stream ALIVE — the path is plainly not dead — and it moves the
// phase, and it is not progress: a person is still watching an empty line. The
// old watch read a thinking delta as a first token, so the deadline branch was
// left forever and a stream that stalled sixty seconds into its thinking was
// answered by a transport bound two and a half minutes away.
func TestThinkingDoesNotStopTheClock(t *testing.T) {
	build := installed(t)
	began := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	watch := build(coldPlan(began))
	// A thought arrives, and then the lane stops writing altogether.
	watch.Note(control.Reading{At: began.Add(200 * time.Millisecond), Hidden: 1})
	if watch.Phase() != control.PhaseThinking {
		t.Fatalf("a thinking delta left the phase at %d", watch.Phase())
	}
	deadline := began.Add(200*time.Millisecond + RoleTalk.Ceiling())
	for step := 300 * time.Millisecond; step <= 2*RoleTalk.Ceiling(); step += 100 * time.Millisecond {
		if watch.Quiet(began.Add(step)).Kind != control.None {
			return
		}
	}
	t.Fatalf("a stream that stalled inside its thinking was never acted on; the ceiling was %s", deadline.Sub(began))
}

// TestHeartbeatsDoNotResetSilence keeps the two claims apart.
//
// `: OPENROUTER PROCESSING` is proof about the PATH: the connection is open and
// somebody is on the other end. It is not proof that the endpoint is writing,
// and a silence clock it reset would be a clock a router could hold open
// forever by saying nothing in a well-formed way.
func TestHeartbeatsDoNotResetSilence(t *testing.T) {
	build := installed(t)
	began := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	watch := build(coldPlan(began))
	acted := time.Duration(0)
	for step := 100 * time.Millisecond; step <= 2*RoleTalk.Ceiling(); step += 100 * time.Millisecond {
		now := began.Add(step)
		watch.Note(control.Reading{At: now, Beat: true})
		if watch.Quiet(now).Kind != control.None {
			acted = step
			break
		}
	}
	if acted == 0 {
		t.Fatal("a stream that only ever heartbeat was never acted on")
	}
	if acted > RoleTalk.Ceiling() {
		t.Fatalf("heartbeats held the clock open for %s past a ceiling of %s", acted, RoleTalk.Ceiling())
	}
}

// TestAPinnedLaneRaisesAnOfferNotAHedge is the whole of what a pin means.
//
// A person who named a machine is owed that machine, so a slow pin is not
// silently rescued: it is a question, asked once, that a token withdraws. What
// a pinned request may never do is go somewhere else without being told to.
func TestAPinnedLaneRaisesAnOfferNotAHedge(t *testing.T) {
	build := installed(t)
	began := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	plan := coldPlan(began)
	plan.Pinned = true
	watch := build(plan)
	for step := 100 * time.Millisecond; step <= 2*RoleTalk.Ceiling(); step += 100 * time.Millisecond {
		switch act := watch.Quiet(began.Add(step)); act.Kind {
		case control.None:
			continue
		case control.Ask:
			return
		default:
			t.Fatalf("a pinned lane produced %d after %s; a pin is asked, never overridden", act.Kind, step)
		}
	}
	t.Fatal("a pinned lane that said nothing at all never raised an offer")
}

// TestWithNothingToHedgeToTheWaitIsReported is why silence is not an option.
//
// One lane, or every lane believed slow, is a real state and a common one. The
// answer is not to hedge — there is nowhere to go — and it is not to say
// nothing either, because a person watching an empty line with no explanation is
// exactly the experience this design exists to remove.
func TestWithNothingToHedgeToTheWaitIsReported(t *testing.T) {
	build := installed(t)
	began := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	plan := coldPlan(began)
	plan.Alts = nil
	watch := build(plan)
	for step := 100 * time.Millisecond; step <= 2*RoleTalk.Ceiling(); step += 100 * time.Millisecond {
		if act := watch.Quiet(began.Add(step)); act.Kind == control.Report {
			return
		}
	}
	t.Fatal("a request with nowhere to go said nothing about its own wait")
}

// TestBeliefsDecayWhenUnobserved is what "the belief adapts to the current
// time" means in arithmetic.
//
// Certainty is what fades, never the estimate: a lane that was quick an hour ago
// is still believed quick, and believed it far less strongly. Each level of the
// hierarchy fades at its own rate, because one deployment's queue turns over in
// minutes and a model's size never does.
func TestBeliefsDecayWhenUnobserved(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	t.Cleanup(Default().Reset)
	Default().Reset()
	hierarchy, ok := Default().Ledger().(Hierarchy)
	if !ok {
		t.Fatal("the ledger does not answer the hierarchy's door — there is no chain to decay " +
			"(docs/design/waiting/DESIGN.md, lane W1)")
	}
	id := ID{Model: "vendor/model", Lane: "somebody"}
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	early := hierarchy.Wait(id, now)
	late := hierarchy.Wait(id, now.Add(time.Hour))
	_, first := early.Predict()
	_, second := late.Predict()
	if second <= first {
		t.Errorf("an hour of silence left the belief as sure as it was: %g then, %g now", first, second)
	}
}

// TestTheControllerIsBuiltFromTheOneFactory is the registry's argument, said
// about waiting.
//
// A caller that built its own controller would be a caller with its own idea of
// when to act, and the surface's countdown and the transport's alarm would be
// two numbers that agree until the day one of them is fixed.
func TestTheControllerIsBuiltFromTheOneFactory(t *testing.T) {
	fset, files := sources(t)
	for name, file := range files {
		if isTest(name) || name == "waiting.go" {
			continue
		}
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			ident, ok := selector.X.(*ast.Ident)
			if ok && ident.Name == "control" && selector.Sel.Name != "Survival" {
				t.Errorf("%s builds control.%s outside the factory", fset.Position(call.Pos()), selector.Sel.Name)
			}
			return true
		})
	}
}

// TestNoTestWritesTheRealHome is a law about this directory's own tests, and it
// is here because breaking it costs somebody else their belief file.
//
// The default registry's ledger writes every belief through a store rooted at
// AFORGE_HOME, so a test that touches it without moving the state root folds its
// invented lanes into the file a real session reads on its next cold start. It
// happened: `~/.aforge/v3/lanes.json` on the machine this design was written on
// carried lanes from the beat's own test.
//
// A helper that moves the root counts for every test that calls it, which is how
// `e2e_test.go` passes with one setenv for seven scenarios.
func TestNoTestWritesTheRealHome(t *testing.T) {
	_, files := sources(t)
	for name, file := range files {
		if !isTest(name) || strings.Contains(name, "e2e_real") {
			continue
		}
		guarded := map[string]bool{}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if ok && (mentions(fn, "EnvVar") || quotes(fn, home.EnvVar)) {
				guarded[fn.Name.Name] = true
			}
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || !strings.HasPrefix(fn.Name.Name, "Test") {
				continue
			}
			if !mentions(fn, "Default") || guarded[fn.Name.Name] || callsAny(fn, guarded) {
				continue
			}
			t.Errorf("%s: %s uses the default registry without a home of its own", name, fn.Name.Name)
		}
	}
}

// quotes reports whether a function carries a string literal, which is how a
// test that spells the environment variable out rather than naming the constant
// still counts as guarded. Both spellings move the same root.
func quotes(fn *ast.FuncDecl, text string) bool {
	found := false
	ast.Inspect(fn, func(node ast.Node) bool {
		if literal, ok := node.(*ast.BasicLit); ok && strings.Trim(literal.Value, `"`) == text {
			found = true
		}
		return !found
	})
	return found
}

// mentions reports whether a function names an identifier anywhere in its body
// or signature.
func mentions(fn *ast.FuncDecl, name string) bool {
	found := false
	ast.Inspect(fn, func(node ast.Node) bool {
		if ident, ok := node.(*ast.Ident); ok && ident.Name == name {
			found = true
		}
		return !found
	})
	return found
}

// callsAny reports whether a function calls one of the named functions of its
// own package. One level of indirection is enough: a helper that sets the home
// and a helper that calls a helper that does are the same promise, and a
// deeper walk would be a call graph nobody needs to read this law.
func callsAny(fn *ast.FuncDecl, names map[string]bool) bool {
	found := false
	ast.Inspect(fn, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		if ident, ok := call.Fun.(*ast.Ident); ok && names[ident.Name] {
			found = true
		}
		return !found
	})
	return found
}
