package session

// THE LAW: THE MODEL THE WORK IS TALKING TO IS WRITTEN THROUGH ONE DOOR, AND
// THAT DOOR IS ALSO WHERE THE PICTURES ARE MADE SAFE.
//
// `a.riding` is what every sentence about work in flight is drawn from — the
// phase news a surface's model cell reads ([Agent.newsModel]), the door that
// decides whether a person's pick is news at all (steer.go), and, published as
// [Agent.TurnModel], the whole of what the chrome names. A second place that
// writes it is a second answer to "what is this turn on", and the defect this
// change closes was exactly two answers to that question drawn side by side on
// one line.
//
// So it is assigned by [Agent.ridesOnLocked] and by nothing else. The three
// places a move happens reach it through that one function:
//
//   - [Agent.startTurnLocked] and [Agent.latchTheModel], where a turn is born —
//     the first under the lock that binds its client, the second as the loop
//     takes the person's word with it;
//   - [Agent.startVisionTurnLocked], where the other kind of turn is born and
//     the model it talks to is the looking model;
//   - [Agent.rideModel], the move itself — a rescue hopping a spent model to the
//     next in the chain, or a person's word taken at the request boundary.
//
// ONE DOOR BECAUSE THE SCRUB HANGS OFF IT. A transcript's pictures are made safe
// for the model the work is talking to, so a writer that set the field without
// going through [Agent.ridesOnLocked] would move the work onto a blind model
// with the base64 still in the messages — which is the picture leak, arriving by
// the one road nobody would think to look down.
//
// AND IT IS NEVER CLEARED, which is why no fourth writer is needed:
// [Agent.TurnModel] answers "" unless a turn is running, so the flag that says
// there is work in flight is the flag that says this latch means anything. A
// clear would be a second thing to keep in step with `running`, and the first
// time the two disagreed the chrome would name a model nothing was talking to —
// which is the defect, back again by another road.

import (
	"go/ast"
	"go/token"
	"testing"
)

// turnModelWriters is the one function that may assign the latch.
var turnModelWriters = map[string]bool{"ridesOnLocked": true}

// turnModelDoors are the functions that must go on reaching it — the two births
// and the move. A door that stopped calling it is a kind of turn whose model
// nothing publishes, which draws the LAST turn's model on the chrome for the
// length of this one, and a move that stopped calling it is the picture leak.
var turnModelDoors = map[string]bool{
	"startTurnLocked":       true,
	"startVisionTurnLocked": true,
	"latchTheModel":         true,
	"rideModel":             true,
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
					if !ok || selector.Sel.Name != "riding" {
						continue
					}
					written[fn.Name.Name] = true
					if !turnModelWriters[fn.Name.Name] {
						t.Errorf("%s writes the model the work is talking to at %s — "+
							"a.riding is assigned by ridesOnLocked alone, because that is where "+
							"the transcript's pictures are made safe for the model being moved to",
							fn.Name.Name, fset.Position(selector.Pos()))
					}
				}
				return true
			})
		}
	})
	for writer := range turnModelWriters {
		if !written[writer] {
			t.Errorf("%s no longer assigns a.riding, so this law is guarding nothing", writer)
		}
	}
	// AND EVERY DOOR IS STILL A DOOR.
	called := map[string]bool{}
	forEachPackageFile(t, func(path string, file *ast.File, fset *token.FileSet) {
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil || !turnModelDoors[fn.Name.Name] {
				continue
			}
			ast.Inspect(fn.Body, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				if selector, ok := call.Fun.(*ast.SelectorExpr); ok && turnModelWriters[selector.Sel.Name] {
					called[fn.Name.Name] = true
				}
				return true
			})
		}
	})
	for door := range turnModelDoors {
		if !called[door] {
			t.Errorf("%s no longer goes through ridesOnLocked, so the work it moves keeps the model "+
				"the turn before it was on and carries pictures the new model may not be able to see", door)
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
