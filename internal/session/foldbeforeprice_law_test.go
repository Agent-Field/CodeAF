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

// theFold and thePrice are the two calls whose order is the law, named for what
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
	foldAt, priceAt := foldAndPriceIn(file, "runTurn")
	if foldAt == token.NoPos || priceAt == token.NoPos {
		t.Fatalf("runTurn no longer calls both %s and %s — the law is reading the wrong tree",
			theFold, thePrice)
	}
	if foldAt > priceAt {
		t.Errorf("%s stands at %s, AFTER %s at %s. The runaway net reads a weight this loop is "+
			"about to reduce, so a turn whose last round pushed it over the line is moved out to a "+
			"cold worker one statement before this build would have folded it back under — which is "+
			"the 2026-09-11 defect this order exists to close.",
			theFold, set.Position(foldAt), thePrice, set.Position(priceAt))
	}
}

// AND THE LAW BITES. The shape it forbids is one statement away from the shape it
// demands, so it is held to NAMING that shape rather than to being green on the
// tree as it happens to stand today.
func TestTheLawNamesAPriceReadBeforeTheFold(t *testing.T) {
	for _, plant := range []struct {
		what    string
		source  string
		ordered bool
	}{{
		what: "the order this loop had when the defect was measured",
		source: `package session
func (a *Agent) runTurn(ctx context.Context) bool {
	for {
		if a.checkpointRound(ctx) {
			return true
		}
		a.foldTurnOutputs(ctx)
		a.maybeCompact(ctx)
	}
}`,
		ordered: false,
	}, {
		what: "a fold that was left behind in a branch the boundary does not take",
		source: `package session
func (a *Agent) runTurn(ctx context.Context) bool {
	for {
		if done {
			a.maybeCompact(ctx)
			return false
		}
		if a.checkpointRound(ctx) {
			return true
		}
	}
}`,
		ordered: false,
	}, {
		what: "the order the loop has now",
		source: `package session
func (a *Agent) runTurn(ctx context.Context) bool {
	for {
		a.maybeCompact(ctx)
		if a.checkpointRound(ctx) {
			return true
		}
	}
}`,
		ordered: true,
	}} {
		set := token.NewFileSet()
		file, err := parser.ParseFile(set, "plant.go", plant.source, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parsing the plant for %s: %v", plant.what, err)
		}
		foldAt, priceAt := foldAndPriceIn(file, "runTurn")
		if foldAt == token.NoPos || priceAt == token.NoPos {
			t.Fatalf("the plant for %s names neither call", plant.what)
		}
		if ordered := foldAt < priceAt; ordered != plant.ordered {
			t.Errorf("%s: the law reads the fold as %s the price, want %s",
				plant.what, before(ordered), before(plant.ordered))
		}
	}
}

func before(ordered bool) string {
	if ordered {
		return "before"
	}
	return "after"
}

// foldAndPriceIn reports where the fold and the price stand in one function.
//
// THE LAST FOLD IN THE FUNCTION IS THE STEP BOUNDARY'S. An earlier call is the
// one the turn's own ending makes, and a law that took the FIRST would be
// satisfied by a loop with the boundary pair still the wrong way round.
func foldAndPriceIn(file *ast.File, name string) (foldAt, priceAt token.Pos) {
	ast.Inspect(file, func(node ast.Node) bool {
		function, ok := node.(*ast.FuncDecl)
		if !ok || function.Name.Name != name {
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
				foldAt = call.Pos()
			case thePrice:
				priceAt = call.Pos()
			}
			return true
		})
		return false
	})
	return foldAt, priceAt
}
