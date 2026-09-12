package session

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// ── THE STEWARD'S CLOCK MOVES THROUGH ITS LOCK, NEVER PAST IT ───────────────
//
// [Steward.now] and [Steward.started] are read by the product under `s.mu` —
// [Steward.Budget], [Steward.since], and wallIsUp in wallclock.go all take the
// lock before they copy either field. #957 is what happens when something
// writes one of them without it on a live session: the wall-clock goroutine
// (armWallClock → watchTheWall → Budget) is reading the same field under the
// lock, one side of a pair is no happens-before, and `go test -race` went red
// on clean `dev`.
//
// [Steward.setClock] and [Steward.setStarted] take the same lock on the write
// side, so the field has exactly one door and both sides of every access go
// through it. This law is what keeps that true after the fixtures that opened
// the race are fixed, because the next fixture that assigns the field reopens
// it the day it is written — on whichever session already has a wall clock
// running.
//
// IT IS ON THE PULL-REQUEST GATE through scripts/laws.sh, which finds laws by
// their go/ast import.

// stewardClockFields are the two fields of the Steward that ever move after it
// is built. Nothing else on the type is written twice.
var stewardClockFields = map[string]bool{"now": true, "started": true}

// stewardClockDoor is the one file that may assign them: it declares the type,
// builds it, and holds the two setters that take the lock. Everywhere else —
// product and fixture alike — goes through the door.
const stewardClockDoor = "principal.go"

// TestTheStewardsClockIsOnlyMovedThroughItsLockedDoor reads this package for
// anything that assigns the Steward's clock fields directly.
//
// IT FINDS ITS SUBJECTS BY TYPE AND NOT BY NAME. A gate that looked for a
// variable spelled `steward` would pass the first fixture that called its own
// `owner`, which is the same hand-kept list the race came out of. Instead the
// walk reads the package's own declarations for every function that hands back
// a `*Steward` — [NewSteward] and the three helpers that wrap it today — and
// tracks the variables they are assigned to, plus the receiver of any method on
// the type. Add a fourth producer and it is tracked the day it is written.
//
// WHAT IT CANNOT SEE is a Steward that reaches an assignment without passing
// through a call this package declares: out of a struct field, a slice, or an
// interface asserted somewhere else. Those are not shapes this package's
// fixtures use, and the failure below says which door to use rather than
// merely refusing.
func TestTheStewardsClockIsOnlyMovedThroughItsLockedDoor(t *testing.T) {
	paths, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	parsed := map[string]*ast.File{}
	for _, path := range paths {
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		parsed[path] = file
	}
	producers := stewardProducers(parsed)
	if len(producers) == 0 {
		t.Fatal("nothing in this package hands back a *Steward — this law has nothing to hold")
	}

	var reached []string
	for path, file := range parsed {
		if filepath.Base(path) == stewardClockDoor {
			continue
		}
		holders := stewardHolders(file, producers)
		ast.Inspect(file, func(node ast.Node) bool {
			assign, ok := node.(*ast.AssignStmt)
			if !ok {
				return true
			}
			for _, lhs := range assign.Lhs {
				field, ok := lhs.(*ast.SelectorExpr)
				if !ok || !stewardClockFields[field.Sel.Name] {
					continue
				}
				held, ok := field.X.(*ast.Ident)
				if !ok || !holders[held.Name] {
					continue
				}
				reached = append(reached, fmt.Sprintf("%s: %s.%s is assigned directly",
					fset.Position(field.Pos()), held.Name, field.Sel.Name))
			}
			return true
		})
	}
	if len(reached) > 0 {
		sort.Strings(reached)
		t.Fatalf("the Steward's clock is read under s.mu and must be written under it too — "+
			"call setClock or setStarted instead of reaching past the lock (#957):\n%s",
			strings.Join(reached, "\n"))
	}
}

// stewardProducers is every function and method in this package whose one
// result is a `*Steward`, by the name a caller writes at the call site.
func stewardProducers(files map[string]*ast.File) map[string]bool {
	producers := map[string]bool{}
	for _, file := range files {
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Type.Results == nil || len(fn.Type.Results.List) != 1 {
				continue
			}
			if pointsAtSteward(fn.Type.Results.List[0].Type) {
				producers[fn.Name.Name] = true
			}
		}
	}
	return producers
}

// stewardHolders is every variable in one file that holds a `*Steward`: one
// assigned from a producer, and the receiver of a method on the type.
func stewardHolders(file *ast.File, producers map[string]bool) map[string]bool {
	holders := map[string]bool{}
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv == nil {
			continue
		}
		for _, receiver := range fn.Recv.List {
			if !pointsAtSteward(receiver.Type) {
				continue
			}
			for _, name := range receiver.Names {
				holders[name.Name] = true
			}
		}
	}
	ast.Inspect(file, func(node ast.Node) bool {
		var names []ast.Expr
		var values []ast.Expr
		switch bound := node.(type) {
		case *ast.AssignStmt:
			names, values = bound.Lhs, bound.Rhs
		case *ast.ValueSpec:
			for _, name := range bound.Names {
				names = append(names, name)
			}
			values = bound.Values
		default:
			return true
		}
		// ONE NAME AND ONE CALL, which is how every fixture takes a steward.
		// A multiple assignment spreads one call's results over several names
		// and none of them is the whole `*Steward`.
		if len(names) != 1 || len(values) != 1 {
			return true
		}
		call, ok := values[0].(*ast.CallExpr)
		if !ok {
			return true
		}
		if !producers[calleeName(call.Fun)] {
			return true
		}
		if name, ok := names[0].(*ast.Ident); ok {
			holders[name.Name] = true
		}
		return true
	})
	return holders
}

// pointsAtSteward reports whether a type expression is `*Steward`.
func pointsAtSteward(typ ast.Expr) bool {
	pointer, ok := typ.(*ast.StarExpr)
	if !ok {
		return false
	}
	named, ok := pointer.X.(*ast.Ident)
	return ok && named.Name == "Steward"
}

// calleeName is the name a call is written with, either way it can be written:
// bare inside the package that declares it, and through a receiver otherwise.
func calleeName(fun ast.Expr) string {
	switch named := fun.(type) {
	case *ast.Ident:
		return named.Name
	case *ast.SelectorExpr:
		if named.Sel != nil {
			return named.Sel.Name
		}
	}
	return ""
}
