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
	priced, folded := foldBeforePrice(file, "runTurn")
	if !priced {
		t.Fatalf("runTurn no longer reads the price through %s — the law is reading the wrong tree",
			thePrice)
	}
	if !folded {
		t.Errorf("%s does not stand above %s in the block that reads it. The runaway net reads a "+
			"weight this loop is about to reduce, so a turn whose last round pushed it over the "+
			"line is moved out to a cold worker one statement before this build would have folded "+
			"it back under — which is the 2026-09-11 defect this order exists to close.",
			theFold, thePrice)
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
		what: "a fold that is only reached on a road the boundary does not take",
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
		a.foldTurnOutputs(ctx)
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
		priced, folded := foldBeforePrice(file, "runTurn")
		if !priced {
			t.Fatalf("the plant for %s never reads the price", plant.what)
		}
		if folded != plant.ordered {
			t.Errorf("%s: the law reads the fold as %s the price, want %s",
				plant.what, before(folded), before(plant.ordered))
		}
	}
}

func before(ordered bool) string {
	if ordered {
		return "above"
	}
	return "not above"
}

// foldBeforePrice reports whether the named function reads the price at all, and
// whether the fold stands above it IN THE SAME RUN OF STATEMENTS.
//
// THE BLOCK IS THE UNIT AND NOT THE FILE POSITION, which is the difference
// between "the boundary folds first" and "there is a fold somewhere earlier in
// this function". A pass reached only on a road the boundary does not take — the
// one the turn's own ending makes, on its way out — is not a pass the reading
// below it benefits from, and a law that counted it would be satisfied by a
// boundary with no fold in front of the price at all.
func foldBeforePrice(file *ast.File, name string) (priced, folded bool) {
	ast.Inspect(file, func(node ast.Node) bool {
		function, ok := node.(*ast.FuncDecl)
		if !ok || function.Name.Name != name {
			return true
		}
		ast.Inspect(function, func(inner ast.Node) bool {
			block, ok := inner.(*ast.BlockStmt)
			if !ok {
				return true
			}
			foldAt, priceAt := -1, -1
			for index, statement := range block.List {
				if calls(statement, theFold) && foldAt < 0 {
					foldAt = index
				}
				if calls(statement, thePrice) {
					priceAt = index
				}
			}
			if priceAt < 0 {
				return true
			}
			priced = true
			if foldAt >= 0 && foldAt < priceAt {
				folded = true
			}
			return true
		})
		return false
	})
	return priced, folded
}

// calls reports whether a statement ITSELF makes the named method call — the
// condition of an `if` included, since the price is read there, and the BODY of
// one excluded, since a call only some branch makes is not a call this run of
// statements makes. That exclusion is the whole of the second plant.
func calls(statement ast.Stmt, name string) bool {
	found, top := false, true
	ast.Inspect(statement, func(node ast.Node) bool {
		if _, ok := node.(*ast.BlockStmt); ok && !top {
			return false
		}
		top = false
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		if selector, ok := call.Fun.(*ast.SelectorExpr); ok && selector.Sel.Name == name {
			found = true
		}
		return true
	})
	return found
}
