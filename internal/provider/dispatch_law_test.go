package provider

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ── ONE DISPATCHER ──────────────────────────────────────────────────────────
//
// docs/design/recovery/DESIGN.md §4 is one sentence about structure: there is
// ONE loop that decides what a failed call does next, and everything else is a
// move generator under it or a watcher feeding it. Both laws below are that
// sentence said in a way the build can check, because the eleven controllers of
// §2 did not arrive in one commit — each was a reasonable answer to a real
// defect, added next to the last one, and the shape they made together is what
// took four waves to see.
//
// They ride the `laws` gate (scripts/laws.sh finds them by the go/ast import),
// so a second attempt loop fails the pull request that adds it.

// dispatcherFile is where the one loop lives. It is named once, here.
const dispatcherFile = "dispatch.go"

// sendsForACompletion are the files that may put a COMPLETION on the wire, and
// there is one. The three beside it are on the list with their reasons, and none
// of them is a completion: a law that pretended they did not exist would be a
// law nobody could keep.
//
//   - media.go posts an image, a transcription or a document parse. It has no
//     token stream, no lane belief and no relaxation ladder — [lane.RoleMedia]
//     is the role that says so — and its retries are the media API's own.
//   - receipt.go reconciles what a finished call cost, minutes later, against
//     the router's generation endpoint. It is an accounting read about a call
//     that has already ended; there is nothing left to recover.
//   - connectivity.go, probe.go and lanes.go ask the ORIGIN a question — is it
//     there, how fast is this endpoint, what does the sheet say. None of them
//     carries a person's request, so none of them has a move to make when it
//     fails: it answers "unknown" and the call above it decides.
var sendsForACompletion = map[string]string{
	dispatcherFile:    "the one loop",
	"media.go":        "an image, a transcription or a document parse — no stream, no ladder",
	"receipt.go":      "an accounting read about a call that has already ended",
	"connectivity.go": "is the origin there",
	"probe.go":        "how fast is this endpoint",
	"lanes.go":        "what does the sheet say",
}

// TestOneDispatcherSendsForACompletion holds the module to one place that puts
// a person's request on the wire.
//
// A SECOND ONE IS A SECOND RECOVERY POLICY, whatever its author intended. It
// gets its own idea of how many attempts, its own backoff, its own view of who
// has already refused — and the census of 2026-09-10 is what that costs: 53 % of
// multi-attempt chains never left the lane they started on, because two of the
// three places that could send did not know what the third had tried.
func TestOneDispatcherSendsForACompletion(t *testing.T) {
	for name, file := range providerSources(t) {
		reason, allowed := sendsForACompletion[name]
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || selector.Sel.Name != "Do" || len(call.Args) != 1 {
				return true
			}
			// `sync.Once.Do` takes a function and an HTTP client takes a
			// request, and both spell it `.Do(x)`. The once is told by its
			// receiver, which is the only half of the expression that differs
			// reliably: a literal function body, or something named for what it
			// is (`transportOnce`, `w.once`).
			if _, isFunc := call.Args[0].(*ast.FuncLit); isFunc {
				return true
			}
			if strings.Contains(strings.ToLower(receiverOf(selector.X)), "once") {
				return true
			}
			if !allowed {
				t.Errorf("%s sends a request of its own — every completion goes through %s's send "+
					"(docs/design/recovery/DESIGN.md §4). If this is not a completion, add it to "+
					"sendsForACompletion with the sentence that says why", name, dispatcherFile)
			}
			_ = reason
			return true
		})
	}
}

// countsAttempts are the files that may hold a loop counting its own attempts,
// and the dispatcher is not on it twice: receipt.go's schedule is about a row in
// somebody's billing ledger arriving late, which is not a recovery and has no
// move to make.
var countsAttempts = map[string]string{
	dispatcherFile: "the one budget",
	"receipt.go":   "a billing row that has not been written yet — not a recovery",
}

// TestNoAttemptCountingLoopOutsideTheDispatcher refuses the OTHER half of the
// shape: not a second place that sends, but a second place that counts.
//
// Six of the eleven budgets in docs/design/recovery/DESIGN.md §2 were loops with
// a private count in their header — three faults, six paced sends, sixty patient
// ones, four arms, one ladder arm, seven rungs. Every one of them was correct on
// its own and none of them could see the others, so their product bounded the
// call and nobody could state it. There is one budget now and it is a deadline.
func TestNoAttemptCountingLoopOutsideTheDispatcher(t *testing.T) {
	counting := map[string]bool{"attempt": true, "attempts": true, "tries": true, "retry": true, "retries": true}
	for name, file := range providerSources(t) {
		if _, allowed := countsAttempts[name]; allowed {
			continue
		}
		ast.Inspect(file, func(node ast.Node) bool {
			// AND THE LADDER IS A COUNT TOO, spelled as a range rather than as a
			// number: `for _, step := range c.relaxationPlan(...)` was the seventh
			// budget, and it walked the rungs itself while [control.Next] sat
			// beside it already knowing them. A rung is a move now, so the only
			// place that may walk them is the place that asks for moves.
			if loop, ok := node.(*ast.RangeStmt); ok {
				over := strings.ToLower(receiverOf(loop.X))
				if strings.Contains(over, "relaxationplan") || strings.HasSuffix(over, ".shapes") {
					t.Errorf("%s walks the relaxation rungs itself — the rungs are the plan's and "+
						"%s asks control.Next for one (docs/design/recovery/DESIGN.md §4)", name, dispatcherFile)
				}
				return true
			}
			loop, ok := node.(*ast.ForStmt)
			if !ok || loop.Init == nil {
				return true
			}
			assign, ok := loop.Init.(*ast.AssignStmt)
			if !ok {
				return true
			}
			for _, target := range assign.Lhs {
				ident, ok := target.(*ast.Ident)
				if ok && counting[strings.ToLower(ident.Name)] {
					t.Errorf("%s counts %q in a loop header — the one budget is the plan's deadline "+
						"(docs/design/recovery/DESIGN.md §4)", name, ident.Name)
				}
			}
			return true
		})
	}
}

// TestNoConstantInThisPackageBoundsAnAttemptCount is the same law said about the
// numbers rather than the loops, because a count does not have to be in a `for`
// header to be a budget: `outOfPatience` read two of them out of a helper.
func TestNoConstantInThisPackageBoundsAnAttemptCount(t *testing.T) {
	// The one exception, and it is not an attempt count: [maxBackoffShift] caps
	// an EXPONENT so a long-lived call's doubling cannot overflow into a
	// negative duration. It bounds arithmetic, never patience.
	allowed := map[string]bool{"maxBackoffShift": true, "receiptAttempts": true}
	for name, file := range providerSources(t) {
		if name == "receipt.go" {
			continue
		}
		ast.Inspect(file, func(node ast.Node) bool {
			spec, ok := node.(*ast.ValueSpec)
			if !ok {
				return true
			}
			for _, ident := range spec.Names {
				lower := strings.ToLower(ident.Name)
				if allowed[ident.Name] {
					continue
				}
				if strings.HasSuffix(lower, "attempts") || strings.HasSuffix(lower, "pacingbudget") ||
					strings.HasSuffix(lower, "moves") && strings.HasPrefix(lower, "free") {
					t.Errorf("%s declares %s — a budget of its own is what the one deadline replaced "+
						"(docs/design/recovery/DESIGN.md §4)", name, ident.Name)
				}
			}
			return true
		})
	}
}

// receiverOf spells the thing a method was called on, as far as one name goes.
func receiverOf(expr ast.Expr) string {
	switch node := expr.(type) {
	case *ast.Ident:
		return node.Name
	case *ast.SelectorExpr:
		return receiverOf(node.X) + "." + node.Sel.Name
	case *ast.CallExpr:
		return receiverOf(node.Fun)
	}
	return ""
}

// providerSources parses every non-test file of this package, keyed by base name.
func providerSources(t *testing.T) map[string]*ast.File {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	files := map[string]*ast.File{}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(fset, filepath.Join(".", name), nil, parser.ParseComments)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		files[name] = parsed
	}
	if len(files) == 0 {
		t.Fatal("no sources parsed — this law would pass by finding nothing")
	}
	return files
}
