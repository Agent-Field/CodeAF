package session

// THE LAW: THE TURN'S MODEL IS LATCHED IN ONE PLACE PER KIND OF TURN, AND MOVED
// BY ONE FUNCTION.
//
// `a.turnModel` is what every sentence about work in flight is drawn from — the
// phase news a surface's model cell reads ([Agent.newsModel]), the loop's own
// per-step model (loop.go), the sentence `/model` says about what a swap did not
// touch. A second place that writes it is a second answer to "what is this turn
// on", and the defect this whole change closes was exactly two answers to that
// question drawn side by side on one line.
//
// So it may be written by three functions and no fourth:
//
//   - [Agent.startTurnLocked], where an ordinary turn is born, under the lock
//     that binds its client;
//   - [Agent.startVisionTurnLocked], where the one other kind of turn is born
//     and the model it talks to is the looking model;
//   - [Agent.turnMovedTo], the one move — a rescue hopping a spent model to the
//     next in the chain, announced in the same breath.
//
// AND IT IS NEVER CLEARED, which is the other half of the law and is why no
// fourth writer is needed: [Agent.turnModelLocked] answers "" unless a turn is
// running, so the flag that says there is work in flight is the flag that says
// this latch means anything. A clear would be a second thing to keep in step
// with `running`, and the first time the two disagreed the chrome would name a
// model nothing was talking to — which is the defect, back again by another
// road.

import (
	"go/ast"
	"go/token"
	"testing"
)

// turnModelWriters are the only functions that may assign the latch.
var turnModelWriters = map[string]bool{
	"startTurnLocked":       true,
	"startVisionTurnLocked": true,
	"turnMovedTo":           true,
}

func TestOnlyTheTurnsOwnDoorsLatchItsModel(t *testing.T) {
	written := map[string]bool{}
	forEachPackageFile(t, func(path string, file *ast.File, fset *token.FileSet) {
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			ast.Inspect(fn.Body, func(node ast.Node) bool {
				assign, ok := node.(*ast.AssignStmt)
				if !ok {
					return true
				}
				for _, target := range assign.Lhs {
					selector, ok := target.(*ast.SelectorExpr)
					if !ok || selector.Sel.Name != "turnModel" {
						continue
					}
					written[fn.Name.Name] = true
					if !turnModelWriters[fn.Name.Name] {
						t.Errorf("%s writes the turn's latched model at %s — "+
							"the latch is stamped where a turn is born and moved only by turnMovedTo",
							fn.Name.Name, fset.Position(selector.Pos()))
					}
				}
				return true
			})
		}
	})
	// AND EVERY DOOR IS STILL A DOOR. A writer that lost its assignment is a kind
	// of turn whose model nothing latches, which draws the LAST turn's model on
	// the chrome for the length of this one.
	for door := range turnModelWriters {
		if !written[door] {
			t.Errorf("%s no longer latches the turn's model, so the work it starts names whatever the turn before it was on", door)
		}
	}
}

// turnModelReaders are the places inside a turn that must name the model THE
// TURN is on rather than the dial — the sentence a person reads about work in
// flight, and the row the work is written down on.
//
// The vision turn is the one that got this wrong for a whole release: it ran on
// the looking model and sealed itself with [Agent.Model], so the usage row, the
// journal and every reader downstream recorded a turn on a model that had not
// been asked anything. The seal takes `seer` now, which is the same fact the
// reply's own `[vision: <model>]` prefix carries.
var turnModelReaders = map[string]bool{"runVision": true}

func TestTheWorkInFlightIsSealedUnderItsOwnModel(t *testing.T) {
	seen := map[string]bool{}
	forEachPackageFile(t, func(path string, file *ast.File, fset *token.FileSet) {
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil || !turnModelReaders[fn.Name.Name] {
				continue
			}
			seen[fn.Name.Name] = true
			ast.Inspect(fn.Body, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				selector, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || selector.Sel.Name != "Model" {
					return true
				}
				if ident, ok := selector.X.(*ast.Ident); !ok || ident.Name != "a" {
					return true
				}
				t.Errorf("%s reads the dial at %s — a turn in flight is named, drawn and "+
					"sealed under the model it is actually running on",
					fn.Name.Name, fset.Position(selector.Pos()))
				return true
			})
		}
	})
	for name := range turnModelReaders {
		if !seen[name] {
			t.Errorf("%s is gone, so this law is guarding nothing", name)
		}
	}
}
