package tui3

// THE FRAME NEVER READS THE DISK, AND THIS IS THE LAW THAT SAYS SO.
//
// ARCHITECTURE.md's fourth law is one sentence — "`open` and `tick` may read the
// disk and `body` may not" — and it was true by habit rather than by anything a
// build could check. Habit lost three times, and every one of them cost a
// syscall per visible thing per frame at thirty frames a second: a stat of every
// visible picture taken from inside [app.View] because the stat WAS the cache
// key, the model cache read off disk on every frame of the first-run screen, and
// `/workspace` running two `git` processes on the update loop with a four-
// hundred-millisecond ceiling over them.
//
// So the law is a test. It reads this package with go/ast, walks what the frame
// can reach, and fails on a call that opens a file, runs a process, globs a
// directory or touches the network.
//
// WHAT IT WALKS. The roots are the frame's own doors — [app.View] and everything
// it composes. The edges are three:
//
//   - a call on the SURFACE ITSELF — `a.legend(width)` inside another [app]
//     method — resolved to that method on that receiver type;
//   - a call on no receiver at all — `renderPicture(…)`, `fit(…)` — resolved to
//     the package function of that name;
//   - a func-typed FIELD, through whatever function this package assigns to it.
//     `a.gitProbe = gitHead` is what made `a.gitProbe(dir)` two `git` processes,
//     and without this edge the one call that started this lane would not have
//     been on the graph at all.
//
// WHAT IT DOES NOT WALK, SAID OUT LOUD: a method on some OTHER value the frame
// happens to hold — `p.rows()`, `e.word()`, `r.ref()`. Following those by name
// alone drags in every same-named method in an eleven-thousand-line package and
// turns this law into a forty-line allowlist that nobody reads, which is worse
// than no law. The cover is that a value this package hands around — an entry, a
// picker, a palette, a row — holds no disk of its own: the [app] method that
// hands it the value is on the graph, and a value method that grew a syscall
// would have to be handed a path by one of those.
//
// THE ALLOWLIST IS THE INTERESTING PART OF THIS FILE. Every name on it is a
// MEMOISED DOOR: it reads the disk once per file per epoch and the frame reads
// its memo on every frame after, with `open`, the pulse beat or the write that
// changed the bytes refreshing it. Adding a name here is a claim that the read
// happens once and not per frame, and the claim is written down beside the name.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// frameDiskDoors is the allowlist: a reachable function that may still touch the
// disk, and the one line saying why it is not a per-frame read.
var frameDiskDoors = map[string]string{
	".renderPicture": "the decode itself — the WORK the frame is drawing, not a fact it is fetching, and [app.previews] pays it once per file per shape",
}

// updateProcessDoors is the same allowlist for the update loop: a name the loop
// may reach that starts a process. Only the off-loop helpers belong here — a
// command's own closure is not walked at all, which is the escape hatch.
var updateProcessDoors = map[string]string{}

// The syscalls a frame may not make, spelled as this package spells them.
var frameForbidden = map[string]map[string]bool{
	"os": {
		"Stat": true, "Lstat": true, "ReadFile": true, "Open": true,
		"OpenFile": true, "ReadDir": true, "Create": true, "WriteFile": true,
		"Remove": true, "RemoveAll": true, "MkdirAll": true, "Rename": true,
	},
	"exec":     {"Command": true, "CommandContext": true, "LookPath": true},
	"filepath": {"Glob": true, "Walk": true, "WalkDir": true},
	"net":      {"*": true},
	"http":     {"*": true},
}

// processForbidden is the narrower question the update loop is asked: not
// whether it reads a file — it may, it is `open` and `tick` — but whether it
// waits on a PROCESS or the network while a person is typing.
var processForbidden = map[string]map[string]bool{
	"exec": {"Command": true, "CommandContext": true, "LookPath": true},
	"http": {"*": true},
}

// surfaceGraph is this package as the law reads it.
//
// A node's name is its receiver type and its own, joined — `app.frameBody`, or
// `.renderPicture` for a package function — so that a method on the surface and
// a helper that happens to share its spelling are two nodes and not one.
type surfaceGraph struct {
	fset  *token.FileSet
	decls map[string]*ast.FuncDecl // by receiver type and name
	seams map[string][]string      // func-typed field name → functions assigned to it
}

// nodeName is how one declaration is keyed, and [surfaceGraph.callsIn] spells
// every edge the same way.
func nodeName(recv, name string) string { return recv + "." + name }

// receiverType is the type a method hangs off, with the pointer taken off, or
// "" for a package function.
func receiverType(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return ""
	}
	expr := fn.Recv.List[0].Type
	if star, ok := expr.(*ast.StarExpr); ok {
		expr = star.X
	}
	if ident, ok := expr.(*ast.Ident); ok {
		return ident.Name
	}
	return ""
}

// receiverName is what the method calls itself inside its own body — the `a` of
// `func (a *app)` — which is how a call on the surface is told from a call on
// some other value it is holding.
func receiverName(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 || len(fn.Recv.List[0].Names) == 0 {
		return ""
	}
	return fn.Recv.List[0].Names[0].Name
}

func readSurfaceGraph(t *testing.T) *surfaceGraph {
	t.Helper()
	fset := token.NewFileSet()
	g := &surfaceGraph{fset: fset, decls: map[string]*ast.FuncDecl{}, seams: map[string][]string{}}
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range entries {
		name := item.Name()
		if item.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, filepath.Join(".", name), nil, 0)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			g.decls[nodeName(receiverType(fn), fn.Name.Name)] = fn
		}
		// A FUNC-TYPED FIELD IS AN EDGE TOO. `a.gitProbe = gitHead` is what makes
		// `a.gitProbe(dir)` two `git` processes, and without this the one call
		// this file exists to have caught would not have been on the graph at all.
		ast.Inspect(file, func(n ast.Node) bool {
			assign, ok := n.(*ast.AssignStmt)
			if !ok {
				return true
			}
			for i, lhs := range assign.Lhs {
				sel, ok := lhs.(*ast.SelectorExpr)
				if !ok || i >= len(assign.Rhs) {
					continue
				}
				if rhs, ok := assign.Rhs[i].(*ast.Ident); ok {
					g.seams[sel.Sel.Name] = append(g.seams[sel.Sel.Name], rhs.Name)
				}
			}
			return true
		})
	}
	if len(g.decls) == 0 {
		t.Fatal("no sources read: the law walked an empty package")
	}
	return g
}

// handedOff is every function literal in one body that this package HANDS TO
// SOMEBODY ELSE TO RUN rather than running itself: the `func() tea.Msg` a method
// returns, one passed to tea.Batch, tea.Sequence or a tick, and one started with
// `go`. Those are the off-loop escape hatch — [app.probeGit] is the worked
// example — and what they do is not what the loop does.
//
// Every OTHER literal in a body runs inside it. `run := func(args …string)` in
// [gitHead] is the case that made this distinction necessary: skipping every
// literal meant the two `git` processes the whole lane is about were invisible to
// the law that exists to find them.
func handedOff(body ast.Node) map[*ast.FuncLit]bool {
	off := map[*ast.FuncLit]bool{}
	mark := func(expr ast.Expr) {
		if lit, ok := expr.(*ast.FuncLit); ok {
			off[lit] = true
		}
	}
	ast.Inspect(body, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.ReturnStmt:
			for _, result := range node.Results {
				mark(result)
			}
		case *ast.GoStmt:
			mark(node.Call.Fun)
		case *ast.CallExpr:
			if sel, ok := node.Fun.(*ast.SelectorExpr); ok {
				if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == "tea" {
					for _, arg := range node.Args {
						mark(arg)
					}
				}
			}
		}
		return true
	})
	return off
}

// callsIn is every name one body reaches, and every forbidden call it makes
// itself. intoClosures says whether EVERY function literal counts as part of
// this body — it does for the frame, which runs everything it builds — and when
// it is false the only literals skipped are the ones handed off to be run
// elsewhere ([handedOff]).
func (g *surfaceGraph) callsIn(fn *ast.FuncDecl, forbidden map[string]map[string]bool,
	intoClosures bool) (names []string, bad []string) {
	self, recv := receiverName(fn), receiverType(fn)
	off := handedOff(fn.Body)
	var walk func(ast.Node)
	walk = func(node ast.Node) {
		ast.Inspect(node, func(n ast.Node) bool {
			if lit, ok := n.(*ast.FuncLit); ok {
				if intoClosures || !off[lit] {
					walk(lit.Body)
				}
				return false
			}
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			switch fun := call.Fun.(type) {
			case *ast.Ident:
				names = append(names, nodeName("", fun.Name))
			case *ast.SelectorExpr:
				ident, ok := fun.X.(*ast.Ident)
				if !ok {
					return true
				}
				if calls, watched := forbidden[ident.Name]; watched {
					if calls["*"] || calls[fun.Sel.Name] {
						bad = append(bad, ident.Name+"."+fun.Sel.Name+" at "+
							g.fset.Position(call.Pos()).String())
					}
					return true
				}
				// A CALL ON THE SURFACE ITSELF IS AN EDGE; a call on some other
				// value it is holding is not (this file's head says why).
				if self != "" && ident.Name == self {
					names = append(names, nodeName(recv, fun.Sel.Name))
					// AND IT MAY BE A FUNC-TYPED FIELD RATHER THAN A METHOD, which
					// is the same call to read and a different thing to resolve.
					names = append(names, fun.Sel.Name)
				}
			}
			return true
		})
	}
	walk(fn.Body)
	return names, bad
}

// reach walks out from the roots and reports what each reachable name does that
// it may not, skipping the allowlist and everything behind it.
func (g *surfaceGraph) reach(roots []string, forbidden map[string]map[string]bool,
	allowed map[string]string, intoClosures bool) map[string][]string {
	seen := map[string]bool{}
	sins := map[string][]string{}
	var visit func(name string)
	visit = func(name string) {
		if seen[name] || allowed[name] != "" {
			return
		}
		seen[name] = true
		// A func-typed field reaches whatever this package assigns to it. Field
		// names arrive here bare — no receiver type in front of them — which is
		// exactly how [surfaceGraph.readSeams] filed them.
		for _, next := range g.seams[name] {
			visit(nodeName("", next))
		}
		fn, known := g.decls[name]
		if !known {
			return
		}
		names, bad := g.callsIn(fn, forbidden, intoClosures)
		if len(bad) > 0 {
			sins[name] = append(sins[name], bad...)
		}
		for _, next := range names {
			visit(next)
		}
	}
	for _, root := range roots {
		visit(root)
	}
	return sins
}

func reportSins(t *testing.T, what string, sins map[string][]string) {
	t.Helper()
	if len(sins) == 0 {
		return
	}
	names := make([]string, 0, len(sins))
	for name := range sins {
		names = append(names, name)
	}
	sort.Strings(names)
	var out strings.Builder
	for _, name := range names {
		out.WriteString("\n  " + name + ":")
		for _, sin := range sins[name] {
			out.WriteString("\n      " + sin)
		}
	}
	t.Fatalf("%s%s\n\nMove the reading to open or to a tick, or name the memoised door "+
		"in framedisk_law_test.go with the line that says why it is not a per-frame read.",
		what, out.String())
}

// THE FRAME. Everything [app.View] composes, walked into the closures it builds,
// may not open a file, run a process, glob a directory or reach the network.
func TestTheFrameNeverReadsTheDisk(t *testing.T) {
	g := readSurfaceGraph(t)
	roots := []string{"app.View"}
	reportSins(t, "the frame read the disk:", g.reach(roots, frameForbidden, frameDiskDoors, true))
}

// THE UPDATE LOOP may read a file — it is `open` and it is `tick` — and it may
// not WAIT ON A PROCESS while somebody is typing. A closure is not walked,
// because a closure handed back as a tea.Cmd is exactly how this surface gets
// off the loop ([app.probeGit] is the worked example).
func TestTheUpdateLoopStartsNoProcessOfItsOwn(t *testing.T) {
	g := readSurfaceGraph(t)
	roots := []string{"app.Update"}
	reportSins(t, "the update loop waited on a process:",
		g.reach(roots, processForbidden, updateProcessDoors, false))
}
