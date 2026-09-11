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

// ── THE LAW: A BELIEF IS FILED AGAINST THE MACHINE THAT ANSWERED ────────────
//
// THE MEASURED FAILURE (docs/design/recovery/census-20260910.md §8, finding 4).
// Over ten days, 3,728 of 10,107 finished attempts came back from a machine that
// was not the lane this process had chosen, and on 358 of the errors that named
// a provider the `(via X)` contradicted the chosen lane outright: `lane:
// Fireworks` / `via DeepInfra`, 104 times. Every per-lane belief this build
// keeps — the strike ledger, the pacing, the quality axis, the wall — is a claim
// about a MACHINE, and a claim filed against the machine we asked for rather
// than the one that answered is a claim about a machine that may never have seen
// the request. It paces the innocent one and leaves the saturated one in the
// order, which is a ledger that makes routing worse the more it learns.
//
// THE LAW: every ledger write takes `served`. The honest order of answers to
// "who served" is the stream's own `provider` field, else the one machine this
// request permitted when it permitted exactly one, else NOTHING — and never the
// lane we chose, the lane we ordered first, or the lane a rescue demanded
// (velocity.go's [Client.refuseLane], refusalobject.go's [Client.laneRefusalFor]).
//
// IT IS A STRUCTURAL LAW BECAUSE THE WRONG ARGUMENT COMPILES. `lane`, `served`,
// `chosen` and `demanded` are all strings; nothing but this fails the build the
// day one of them is passed where another belongs.
func TestNoLedgerWriteIsKeyedOnTheLaneWeChose(t *testing.T) {
	// servedAt names, for each ledger write, WHICH argument must be the machine
	// that answered. They are the whole set of writes: the strike ledger's two,
	// the belief's two, and the pacing note the refusal door makes.
	servedAt := map[string]int{
		"observe":           1,
		"pace":              1,
		"noteVelocity":      1,
		"notePacedProvider": 1,
		"noteCutProvider":   2,
		"noteLane":          1,
		"noteLaneOutcome":   1,
		"noteRun":           1,
	}
	// chosen is every spelling in this package of a machine we ASKED for. None
	// of them is evidence about who answered.
	chosen := map[string]bool{
		"lane":       true,
		"chosen":     true,
		"demanded":   true,
		"pinned":     true,
		"hedgeLane":  true,
		"onlyLane":   true,
		"asked":      true,
		"preferred":  true,
		"orderFirst": true,
	}

	fileSet := token.NewFileSet()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fileSet, filepath.Join(".", name), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
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
			at, watched := servedAt[selector.Sel.Name]
			if !watched || at >= len(call.Args) {
				return true
			}
			checked++
			if spelled(call.Args[at], chosen) {
				t.Errorf("%s: %s is filed against a machine we CHOSE, not the one that served — "+
					"see THE LAW above", fileSet.Position(call.Pos()), selector.Sel.Name)
			}
			return true
		})
	}
	// A law nobody's code reaches passes for the wrong reason.
	if checked == 0 {
		t.Fatal("no ledger write was found at all, so this law proved nothing")
	}
}

// spelled reports whether an argument is one of the named identifiers, reading
// through a selector so that `knobs.hedgeLane` and `choice.Order[0]` are as
// visible as a bare name would be.
func spelled(arg ast.Expr, named map[string]bool) bool {
	switch expr := arg.(type) {
	case *ast.Ident:
		return named[expr.Name]
	case *ast.SelectorExpr:
		return named[expr.Sel.Name]
	case *ast.IndexExpr:
		return spelled(expr.X, named)
	case *ast.CallExpr:
		// A call is a derivation and is allowed to be about anything; what is
		// banned is handing a chosen name straight over.
		return false
	}
	return false
}
