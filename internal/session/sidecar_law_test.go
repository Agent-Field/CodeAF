package session

// THE STRUCTURAL HALF OF loop.go's LAW.
//
// loop_speed_test.go proves the two figures on one turn; this proves the SHAPE
// on the whole package, which is the half that survives somebody writing a fixture
// the runtime test happens not to cover. Every reading beside the work goes
// through [readBeside] (sidecar.go) and nowhere else — three of them used to be
// three goroutines with three ad-hoc channels, and the fourth was a straight-line
// call in the turn loop.
//
// IT READS THE TREE ITSELF, so it runs on the laws gate of every pull request
// (scripts/laws.sh finds it by this import).

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// readingsBesideTheWork is every reading this package makes on its own behalf
// during a turn that used to be awaited in front of the person's work.
//
// A NAME LANDS HERE WHEN ITS WAIT WAS MEASURED AND MOVED. Adding one is how the
// next lane keeps this law honest about a call it takes off the path.
var readingsBesideTheWork = map[string]string{
	"refreshMemory": "which remembered lines this message needs (memory.go)",
	"readMark":      "what is left of a long answer (checkpoint.go)",
	"routeJudge":    "whether the answer should have been work (route_judge.go)",
}

// endingDoors are the two calls that MOVE A TURN SOMEWHERE ELSE. They are named
// for what they are rather than for where they are called from, and they are the
// whole of the exception below.
//
// THE EXCEPTION IS A PROPERTY AND NOT A LIST OF PLACES. A reading beside the work
// exists to be APPLIED — to this step if it lands in time, to the next one if it
// does not — so the one condition under which awaiting it is honest is that there
// is no next step to apply it to: the turn is ending. The tree can see that
// without being told, because a function that ends a turn calls one of these
// doors in the same body. Adding a call site to one of those roads therefore
// needs no edit here, and awaiting a reading anywhere a turn CARRIES ON fails the
// build no matter what it is called.
var endingDoors = map[string]bool{
	"handOverRunningTurn": true,
	"checkpointCeiling":   true,
}

func TestEveryReadingBesideTheWorkGoesThroughTheOneDoor(t *testing.T) {
	set := token.NewFileSet()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("reading the package: %v", err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(set, filepath.Join(".", name), nil, parser.ParseComments)
		if err != nil {
			t.Fatalf("parsing %s: %v", name, err)
		}
		besides := besideRanges(file)
		ast.Inspect(file, func(node ast.Node) bool {
			decl, ok := node.(*ast.FuncDecl)
			if !ok || decl.Body == nil {
				return true
			}
			if endsTheTurn(decl.Body) {
				// The turn is being moved; there is nothing left to read beside.
				return false
			}
			// The door itself, and the handles that own it, are where these names
			// are supposed to appear.
			ast.Inspect(decl.Body, func(inner ast.Node) bool {
				call, ok := inner.(*ast.CallExpr)
				if !ok {
					return true
				}
				selector, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				what, watched := readingsBesideTheWork[selector.Sel.Name]
				if !watched || insideSpan(besides, call.Pos()) {
					return true
				}
				t.Errorf("%s: %s.%s is awaited in %s — %s must run beside the work through readBeside "+
					"(sidecar.go), never in front of it",
					set.Position(call.Pos()), exprText(selector.X), selector.Sel.Name, decl.Name.Name, what)
				return true
			})
			return false
		})
	}
}

// endsTheTurn reports whether this function body hands the turn somewhere else,
// which is the one state in which a reading has nothing to run beside.
func endsTheTurn(body *ast.BlockStmt) bool {
	ends := false
	ast.Inspect(body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		if selector, ok := call.Fun.(*ast.SelectorExpr); ok && endingDoors[selector.Sel.Name] {
			ends = true
		}
		return !ends
	})
	return ends
}

// besideRanges is every span of source that is an argument to [readBeside], which
// is the one place a reading beside the work is allowed to be named.
func besideRanges(file *ast.File) [][2]token.Pos {
	var spans [][2]token.Pos
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		name, ok := call.Fun.(*ast.Ident)
		if !ok || name.Name != "readBeside" {
			// A generic call is spelled `readBeside[T](…)` when the type cannot be
			// inferred; the index expression is the same door.
			index, indexed := call.Fun.(*ast.IndexExpr)
			if !indexed {
				return true
			}
			inner, named := index.X.(*ast.Ident)
			if !named || inner.Name != "readBeside" {
				return true
			}
		}
		spans = append(spans, [2]token.Pos{call.Pos(), call.End()})
		return true
	})
	return spans
}

func insideSpan(spans [][2]token.Pos, at token.Pos) bool {
	for _, span := range spans {
		if at >= span[0] && at <= span[1] {
			return true
		}
	}
	return false
}

// exprText is a receiver in the shortest honest form, for the failure sentence.
func exprText(expr ast.Expr) string {
	if name, ok := expr.(*ast.Ident); ok {
		return name.Name
	}
	return "…"
}

// AND THE DOOR ITSELF IS ONE DOOR. A second copy of [readBeside] — a `go func()`
// with its own channel, written because somebody wanted one field more — is the
// spaghetti this file exists to prevent, so the package is allowed exactly one
// declaration of it.
func TestThereIsOneDoorForAReadingBesideTheWork(t *testing.T) {
	set := token.NewFileSet()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("reading the package: %v", err)
	}
	doors := 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(set, filepath.Join(".", name), nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parsing %s: %v", name, err)
		}
		for _, decl := range file.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok && fn.Recv == nil && fn.Name.Name == "readBeside" {
				doors++
			}
		}
	}
	if doors != 1 {
		t.Fatalf("%d declarations of readBeside, want exactly one (sidecar.go)", doors)
	}
}
