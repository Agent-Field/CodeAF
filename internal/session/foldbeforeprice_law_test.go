package session

// THE ORDER IS THE MECHANISM.
//
// A turn leaves this conversation only when it cannot carry on where it is, and
// the one reading that decides it is a QUANTITY: what the conversation weighs
// against the line a fold fires at (checkpoint.go's [Agent.checkpointRound]
// reading inherit.go's [Agent.turnHasRunAway]). That quantity is one this loop
// reduces on its own, every step, for free and with no model call — so WHERE the
// reduction stands relative to the reading is the whole of whether the reading is
// honest.
//
// It stood in the wrong place and it was measured. On 2026-09-11 a one-paragraph
// request for a hover effect ran forty rounds with most of its window still free
// and spilled into a fresh worker task that re-read everything the conversation
// had already found out: the price was read one statement before the fold that
// would have brought the weight back under the line.
//
// THERE IS NOTHING TO TEST BUT THE ORDER, which is exactly why this law exists. A
// rung of its own that folded and asked again would have been a second mechanism
// for a shape this loop already had, and a comment saying "keep these in this
// order" is a comment. So the rule is held structurally: inside the step loop of
// [Agent.runTurn], the fold runs before the price is read.
//
// IT READS THE TREE ITSELF, so it runs on the laws gate of every pull request
// (scripts/laws.sh finds it by this import).

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

// theFoldAndThePrice are the two calls whose order is the law, named for what
// they do rather than for where they are: a fold that stops being called
// `maybeCompact` still has to move this line.
const (
	theFold  = "maybeCompact"
	thePrice = "checkpointRound"
)

func TestTheTranscriptIsFoldedBeforeItsWeightIsPriced(t *testing.T) {
	set := token.NewFileSet()
	file, err := parser.ParseFile(set, "loop.go", nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parsing loop.go: %v", err)
	}

	var foldAt, priceAt token.Pos
	ast.Inspect(file, func(node ast.Node) bool {
		function, ok := node.(*ast.FuncDecl)
		if !ok || function.Name.Name != "runTurn" {
			return true
		}
		ast.Inspect(function, func(inner ast.Node) bool {
			call, ok := inner.(*ast.CallExpr)
			if !ok {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			switch selector.Sel.Name {
			case theFold:
				// THE LAST ONE IN THE FUNCTION IS THE STEP BOUNDARY'S. An earlier
				// call is the one the turn's own opening makes, and a law that
				// took the first would be satisfied by a loop with the boundary
				// pair still the wrong way round.
				foldAt = call.Pos()
			case thePrice:
				priceAt = call.Pos()
			}
			return true
		})
		return false
	})

	if foldAt == token.NoPos || priceAt == token.NoPos {
		t.Fatalf("runTurn no longer calls both %s and %s — the law is reading the wrong tree", theFold, thePrice)
	}
	if foldAt > priceAt {
		t.Errorf("%s stands at %s, AFTER %s at %s. The runaway net reads a weight this loop is "+
			"about to reduce, so a turn whose last round pushed it over the line is moved out to a "+
			"cold worker one statement before this build would have folded it back under — which is "+
			"the 2026-09-11 defect this order exists to close.",
			theFold, set.Position(foldAt), thePrice, set.Position(priceAt))
	}
}
