package tui3

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ── THE STRUCTURAL LAW BEHIND offloop.go ────────────────────────────────────

// engineDoors is which calls this law is about: the doors a person's keystroke
// waits on VISIBLY. Every one of them settles a question somebody is looking at
// — an answer of any lane, the rule that answers the next one, the record a
// conversation is rebuilt from — and every one of them is a call into the
// engine's own process, which is a separate process in every window
// (cmd/aforge's chatv3_local.go), not only over `--host`.
//
// THE NAMES ARE A CONVENTION THE ENGINE ALREADY KEEPS: a door that settles
// something is spelled Resolve<what> ([session.Agent]), so the test reads the
// prefix rather than a list that would go stale the day a lane is added. The two
// spelled out are the two that settle something without answering a question:
// the dial a `D` writes, and the reading a switch rebuilds the screen from.
var engineDoors = []string{"Resolve", "SetAutonomy", "AttachReplay"}

// NO DOOR IS ASKED FROM THE UPDATE LOOP.
//
// The freeze this closes is measured in doorbell.go, and the shape is in
// offloop.go: an answer sent from Update held the loop for the round trip, and
// while it held the loop it also held the wire's reader — which was trying to
// hand this same loop the news that the answer had woken the turn. Ten seconds
// per keystroke, and the engine had applied the answer in one millisecond.
//
// A call inside an [app.offLoop] literal cannot do that: it runs on the
// command's own goroutine, and what it found is folded in on the next pass.
func TestNoEngineDoorIsAskedFromTheUpdateLoop(t *testing.T) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(f os.FileInfo) bool {
		return !strings.HasSuffix(f.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("reading the surface's source: %v", err)
	}
	doors := 0
	for _, pkg := range pkgs {
		for path, file := range pkg.Files {
			packages := importedNames(file)
			offLoop := offLoopBodies(file)
			ast.Inspect(file, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || !engineDoor(sel.Sel.Name) {
					return true
				}
				if id, ok := sel.X.(*ast.Ident); ok && packages[id.Name] {
					// A package-level function that happens to be spelled
					// Resolve… (config.ResolveSources, roles.ResolveCall) is not
					// a door on an agent and crosses nothing.
					return true
				}
				doors++
				for _, body := range offLoop {
					if call.Pos() > body.Pos() && call.End() < body.End() {
						return true
					}
				}
				t.Errorf("%s:%d asks %s from the update loop — wrap it in a.offLoop(func() func(bool) tea.Cmd {…}) so the window can draw while the engine answers (offloop.go)",
					filepath.Base(path), fset.Position(call.Pos()).Line, sel.Sel.Name)
				return true
			})
		}
	}
	if doors == 0 {
		t.Error("no engine door is called anywhere in the surface, so this law is guarding nothing — delete it or fix the reading")
	}
}

// engineDoor reports whether a method name is one of the doors above.
func engineDoor(name string) bool {
	for _, door := range engineDoors {
		if door == "Resolve" {
			if rest, cut := strings.CutPrefix(name, door); cut && rest != "" && rest[0] >= 'A' && rest[0] <= 'Z' {
				return true
			}
			continue
		}
		if name == door {
			return true
		}
	}
	return false
}

// offLoopBodies is every function literal handed to [app.offLoop] in one file —
// the only place a door may be asked from.
func offLoopBodies(file *ast.File) []*ast.FuncLit {
	var out []*ast.FuncLit
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "offLoop" || len(call.Args) != 1 {
			return true
		}
		if lit, ok := call.Args[0].(*ast.FuncLit); ok {
			out = append(out, lit)
		}
		return true
	})
	return out
}

// importedNames is every package name this file can spell.
func importedNames(file *ast.File) map[string]bool {
	names := map[string]bool{}
	for _, spec := range file.Imports {
		name := strings.Trim(spec.Path.Value, `"`)
		if at := strings.LastIndex(name, "/"); at >= 0 {
			name = name[at+1:]
		}
		if spec.Name != nil {
			name = spec.Name.Name
		}
		names[name] = true
	}
	return names
}
