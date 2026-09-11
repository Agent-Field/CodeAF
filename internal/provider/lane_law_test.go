package provider

import (
	"go/ast"
	"os"
	"strings"
	"testing"
)

// ── NO FETCH ON THE SEND PATH ───────────────────────────────────────────────
//
// This whole package is the send path. Everything in it runs either while a
// request is being shaped — the encoder reads the routing preference at the
// last possible moment, deliberately, so that a demotion earned thirty seconds
// ago applies to the request being written now — or while a stream is being
// read, one delta at a time, against the connection's own idle watchdog.
//
// The lane sheet (`internal/lane`) has two halves for exactly this reason:
// Rows reads memory and may be called from anywhere, and Refresh goes to the
// network and belongs to a background beat. A Refresh reached from here would
// put an HTTP round trip in front of a person's first token, and it would do it
// invisibly — the call would succeed, the answer would be right, and the
// surface would simply have felt slower since some Tuesday.
//
// The home-lag investigation of 2026-08-30 is the same lesson in another
// package: one reading per beat, never a reading per keystroke. This test is
// the version of it the build can enforce, and it is a text scan rather than an
// AST walk on purpose — a method value, an interface embedded in a struct, and
// a wrapper named RefreshLanes are all things a reader would call a fetch, and
// all of them would slip past a check for one particular call expression.
func TestNothingOnTheSendPathRefreshesASheet(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read the package directory: %v", err)
	}
	scanned := 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		source, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		scanned++
		for number, line := range strings.Split(string(source), "\n") {
			code, _, _ := strings.Cut(line, "//")
			if strings.Contains(code, "Refresh(") {
				t.Errorf("%s:%d fetches a sheet on the send path: %s",
					name, number+1, strings.TrimSpace(line))
			}
		}
	}
	if scanned == 0 {
		t.Fatal("no sources were scanned, so this law passed vacuously")
	}
}

// ── ONE CALL, ONE DRAW ──────────────────────────────────────────────────────
//
// A lane choice is a SAMPLED decision: `internal/lane`'s seed mixes in the
// moment it is asked at, so asking twice about one request gives two different
// answers. One call encodes its request many times — once per attempt, once for
// every rung of the relaxation ladder, once more for the object the refusal door
// re-derives — and while the encoder was free to draw one of its own, each of
// those encodes could be a different request to a different set of machines. The
// plan above them reads the call's choice ([requestSet]), so a choice the
// encoder kept to itself left the move generator walking a set the wire had
// never carried, and the waiting line could name nothing.
//
// The law is therefore about WHO MAY ASK, and it is a short list with a reason
// on every line rather than a rule about where the call sits.

// asksTheChooser are the functions that may put a question to
// [lane.Chooser.Choose], and what each one's question is ABOUT. Nothing else in
// this package may ask, because everything else in it is composing a request
// that has already been decided.
var asksTheChooser = map[string]string{
	// ONE CALL'S OWN DECISION, drawn before the first byte leaves and read by the
	// watch, the plan and every encode under it (lanes.go's withLaneChoice).
	"drawLaneChoice": "the one draw a call makes",
	// AND THE PROBE'S QUESTION IS ABOUT A MODEL AND NOT ABOUT A CALL. It buys a
	// measurement of the two lanes at the head of a model's frontier seconds
	// before anybody asks for anything, so there is no call to be consistent
	// with — it IS the thing that gives the next call something to choose
	// between (probe.go).
	"ProbeLanes": "which two lanes are worth measuring, asked of a model",
}

// drawsTheChoice is who may call [Client.drawLaneChoice], and there is one: the
// function that stamps the answer on the call's context so that everything under
// it reads the same set of machines.
var drawsTheChoice = map[string]string{
	"withLaneChoice": "stamps the one answer on the call",
}

// TestOnlyOneFunctionDrawsACallsLane holds one call to one set of machines.
func TestOnlyOneFunctionDrawsACallsLane(t *testing.T) {
	permitted := map[string]map[string]string{
		"Choose":         asksTheChooser,
		"drawLaneChoice": drawsTheChoice,
	}
	asked := map[string]bool{}
	for name, file := range providerSources(t) {
		for _, decl := range file.Decls {
			function, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			ast.Inspect(function, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				selector, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				callers, watched := permitted[selector.Sel.Name]
				if !watched {
					return true
				}
				if _, allowed := callers[function.Name.Name]; !allowed {
					t.Errorf("%s: %s calls %s — a call draws its lane ONCE, in withLaneChoice, and "+
						"everything below it reads knobs.laneChoice. If this question is about a model "+
						"rather than about a call, add it to the list with the sentence that says so",
						name, function.Name.Name, selector.Sel.Name)
					return true
				}
				asked[selector.Sel.Name+" from "+function.Name.Name] = true
				return true
			})
		}
	}
	for callee, callers := range permitted {
		for name := range callers {
			if !asked[callee+" from "+name] {
				t.Errorf("%s is permitted to call %s and no longer does — delete the line rather than "+
					"leaving a permission nobody uses", name, callee)
			}
		}
	}
}

// ── A PIN IS A PERSON'S WORD, AND IT IS NEVER COUNTED ───────────────────────
//
// `Only` carries two different facts that look identical on the wire: the set
// this process admitted, which is ours to relax the moment it stops working,
// and the one machine somebody typed, which is not. [control.Plan.Pinned] is
// what tells the build which it is holding, and everything it changes is
// person-facing: a stall on a pinned machine becomes `switch to auto? (y)`
// instead of a rescue, and the primary arm stops walking off a transient fault.
//
// It was `len(choice.Only) > 0` until the chooser began demanding its own
// admitted set, at which point that sentence made every ordinary call pinned —
// the question asked about machines nobody chose, and no rescue sent. So the
// law: a pin may be WRITTEN only where a person's own row is read, and may
// never be DERIVED from a list of names.

// pinsACall is who may put a pin on a choice or a plan. Clearing one is not on
// this list and needs no permission: a rescue's own arm and a reconnection are
// both this process's doing, and neither of them is anybody's instruction.
var pinsACall = map[string]string{
	// THE ONE READING OF A PERSON'S ROW (lanepin.go's lanePinFor), which is the
	// only place in this build that knows a machine was named by hand.
	"drawLaneChoice": "reads the person's own row",
	// AND THE ONE PLACE THE PLAN IS BUILT, which copies the choice's own word
	// across rather than inferring one.
	"planFor": "copies the choice's word onto the plan",
}

// TestOnlyAPersonsOwnRowPinsACall is that law.
func TestOnlyAPersonsOwnRowPinsACall(t *testing.T) {
	written := map[string]bool{}
	for name, file := range providerSources(t) {
		for _, decl := range file.Decls {
			function, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			ast.Inspect(function, func(node ast.Node) bool {
				value, where := pinWrite(node)
				if value == nil {
					return true
				}
				// Clearing a pin is always allowed: see above.
				if ident, ok := value.(*ast.Ident); ok && ident.Name == "false" {
					return true
				}
				if _, allowed := pinsACall[function.Name.Name]; !allowed {
					t.Errorf("%s: %s pins a call at %s — a pin is a PERSON's one machine, written where "+
						"their own row is read and nowhere else. A request that merely names machines is "+
						"a demand, and a demand is ours to relax", name, function.Name.Name, where)
					return true
				}
				if namesTheDemand(value) {
					t.Errorf("%s: %s derives a pin from `Only` — that field now carries the chooser's own "+
						"admitted set as well as a person's pin, so counting it pins everybody", name, function.Name.Name)
				}
				written[function.Name.Name] = true
				return true
			})
		}
	}
	for name := range pinsACall {
		if !written[name] {
			t.Errorf("%s is permitted to pin a call and no longer does — delete the line rather than "+
				"leaving a permission nobody uses", name)
		}
	}
}

// pinWrite is the value being written to a `Pinned` field, or nil.
func pinWrite(node ast.Node) (ast.Expr, string) {
	switch written := node.(type) {
	case *ast.AssignStmt:
		for index, target := range written.Lhs {
			selector, ok := target.(*ast.SelectorExpr)
			if !ok || selector.Sel.Name != "Pinned" || index >= len(written.Rhs) {
				continue
			}
			return written.Rhs[index], "an assignment"
		}
	case *ast.KeyValueExpr:
		if key, ok := written.Key.(*ast.Ident); ok && key.Name == "Pinned" {
			return written.Value, "a literal"
		}
	}
	return nil, ""
}

// namesTheDemand reports whether an expression reads the demanded set.
func namesTheDemand(value ast.Expr) bool {
	found := false
	ast.Inspect(value, func(node ast.Node) bool {
		if ident, ok := node.(*ast.Ident); ok && ident.Name == "Only" {
			found = true
		}
		return !found
	})
	return found
}
