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
// WHAT IT DOES NOT WALK, SAID OUT LOUD, because a law whose reach a reader
// cannot predict is a law that lies by omission:
//
//   - A METHOD ON SOME OTHER VALUE the frame happens to hold — `p.rows()`,
//     `e.word()`, `r.ref()`. Following those by name alone drags in every
//     same-named method in an eleven-thousand-line package and turns this law
//     into a forty-line allowlist that nobody reads, which is worse than no law.
//     The cover is that a value this package hands around — an entry, a picker, a
//     palette, a row — holds no disk of its own: the [app] method that hands it
//     the value is on the graph, and a value method that grew a syscall would
//     have to be handed a path by one of those.
//   - ANOTHER PACKAGE'S BODY. The walk parses THIS directory, so a call into
//     internal/config, internal/home or cmd/aforge is a leaf. That is how a real
//     per-frame read hid for a while: the door installs [Options.ModelsForService]
//     from cmd/aforge, whose own last line read the model cache off disk, and
//     nothing here could see it. The seam is followed to the FIELD (below); what
//     is on the other side of it is the other package's law to keep.
//   - A METHOD VALUE PASSED AS AN ARGUMENT — `watch(a.load)` — because only a
//     call's own Fun is read as an edge.
//   - `go a.method()` is walked as though it were on the loop, while
//     `go func(){ a.method() }()` is not. That is strict rather than loose, so it
//     costs a false positive and never a miss.
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
	// os.UserHomeDir is DELIBERATELY ABSENT: on unix it reads $HOME and touches
	// nothing, so forbidding it would be the law making a claim about a function
	// that costs what a map lookup costs. Everything else here is a syscall.
	"os": {
		"Stat": true, "Lstat": true, "ReadFile": true, "Open": true,
		"OpenFile": true, "ReadDir": true, "Create": true, "WriteFile": true,
		"Remove": true, "RemoveAll": true, "MkdirAll": true, "Rename": true,
		"Getwd": true, "Chdir": true, "Readlink": true, "Symlink": true,
		"Truncate": true, "DirFS": true,
	},
	"exec": {"Command": true, "CommandContext": true, "LookPath": true},
	// EvalSymlinks is an lstat for every segment of the path, which is the most
	// expensive thing on this list on a machine with an automounted home
	// (PERF.md's hosted-home row).
	"filepath": {"Glob": true, "Walk": true, "WalkDir": true, "EvalSymlinks": true},
	"net":      {"*": true},
	"http":     {"*": true},
}

// processForbidden is the narrower question the update loop is asked: not
// whether it reads a file — it may, it is `open` and `tick` — but whether it
// STARTS A PROCESS or waits on the network while a person is typing.
//
// BUILDING A COMMAND IS NOT STARTING ONE. exec.Command resolves a name on PATH
// and records a miss without forking, and a door that must say "no way to open"
// on the frame that needs it may do exactly that on the loop (opener.go). What
// the loop may not do is START the process — [processStarts] on a command, which
// the walk finds by the name the command was bound to — and that is the rule
// that caught `/workspace`'s two `git` processes.
var processForbidden = map[string]map[string]bool{
	"http": {"*": true},
}

// processStarts is every method that forks or waits on an *exec.Cmd. The walk has
// no types, so it applies these only to a name the same function bound to
// exec.Command or exec.CommandContext, or to such a call directly.
var processStarts = map[string]bool{
	"Start": true, "Run": true, "Output": true, "CombinedOutput": true, "Wait": true,
}

// commandNames is every local in one body bound to a command: `cmd :=
// exec.Command(…)`. Closures are included, because `run := func(…)` in
// [gitHead] is where the binding and the start both live.
func commandNames(body ast.Node) map[string]bool {
	names := map[string]bool{}
	ast.Inspect(body, func(n ast.Node) bool {
		assign, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		for i, lhs := range assign.Lhs {
			ident, ok := lhs.(*ast.Ident)
			if ok && i < len(assign.Rhs) && buildsCommand(assign.Rhs[i]) {
				names[ident.Name] = true
			}
		}
		return true
	})
	return names
}

// buildsCommand is whether one expression is a call to exec.Command or
// exec.CommandContext.
func buildsCommand(expr ast.Expr) bool {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Name == "exec" && (sel.Sel.Name == "Command" || sel.Sel.Name == "CommandContext")
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

func newSurfaceGraph() *surfaceGraph {
	return &surfaceGraph{fset: token.NewFileSet(), decls: map[string]*ast.FuncDecl{}, seams: map[string][]string{}}
}

// readSurfaceGraph is this package's non-test sources, as the law reads them.
func readSurfaceGraph(t *testing.T) *surfaceGraph {
	t.Helper()
	g := newSurfaceGraph()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range entries {
		name := item.Name()
		if item.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(g.fset, filepath.Join(".", name), nil, 0)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		g.add(file)
	}
	if len(g.decls) == 0 {
		t.Fatal("no sources read: the law walked an empty package")
	}
	return g
}

// add files one source file's declarations and seams.
func (g *surfaceGraph) add(file *ast.File) {
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		g.decls[nodeName(receiverType(fn), fn.Name.Name)] = fn
	}
	// A NAME THAT HOLDS A FUNCTION IS AN EDGE TOO, and this package installs
	// one four ways: `a.gitProbe = gitHead`, `var processOpener = startOpener`,
	// `modelsForService: opts.ModelsForService` in a composite literal, and a
	// literal written straight into any of those. All four are collected, by
	// the NAME the call site will spell — the field's or the variable's — and
	// [surfaceGraph.reach] follows them.
	//
	// THE FIRST SPELLING WAS THE ONLY ONE THIS LAW KNEW, and the gap was not
	// academic: `processOpener` is a package-level var holding [startOpener],
	// which ran a process ON the update loop on a keystroke. A law that follows
	// one spelling of indirection and not the others is a law whose name is true
	// only of the code somebody happened to check.
	g.readSeams(file)
}

// handedOff is every function literal in one body that this package HANDS TO
// SOMEBODY ELSE TO RUN rather than running itself: the `func() tea.Msg` a method
// returns, one passed to a [handOffDoors] call, and one started with `go`. Those are the off-loop escape hatch — [app.probeGit] is the worked
// example — and what they do is not what the loop does.
//
// Every OTHER literal in a body runs inside it. `run := func(args …string)` in
// [gitHead] is the case that made this distinction necessary: skipping every
// literal meant the two `git` processes the whole lane is about were invisible to
// the law that exists to find them.
// handOffDoors is every call in this repository that takes a closure TO RUN
// ELSEWHERE, as data: anything in bubbletea's package (a command, a batch, a
// tick), and internal/guard's Go, which is this repository's one door for "start
// this on a goroutine and recover it if it panics". A literal handed to one of
// them is not run by the body that wrote it.
var handOffDoors = map[string]map[string]bool{
	"tea":   {"*": true},
	"guard": {"Go": true},
}

func runsElsewhere(pkg, name string) bool {
	calls, ok := handOffDoors[pkg]
	return ok && (calls["*"] || calls[name])
}

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
				if pkg, ok := sel.X.(*ast.Ident); ok && runsElsewhere(pkg.Name, sel.Sel.Name) {
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

// readSeams collects every name in one file that holds a function, so that a
// call through it is an edge like any other. A literal is filed under a name of
// its own so the walk can reach its body.
func (g *surfaceGraph) readSeams(file *ast.File) {
	lit := 0
	// note files one right-hand side under the name it was installed as.
	var note func(name string, rhs ast.Expr)
	note = func(name string, rhs ast.Expr) {
		switch value := rhs.(type) {
		case *ast.Ident:
			g.seams[name] = append(g.seams[name], value.Name)
		case *ast.SelectorExpr:
			// A method value or another package's function: the name is all the
			// walk can use, and this package's own declaration of it — if there
			// is one — is what it will find.
			g.seams[name] = append(g.seams[name], value.Sel.Name)
		case *ast.FuncLit:
			// A LITERAL IS GIVEN A NAME so the walk has something to visit. The
			// name cannot collide with a declaration: it is not an identifier.
			lit++
			made := "func literal " + g.fset.Position(value.Pos()).String()
			g.decls[made] = &ast.FuncDecl{Name: ast.NewIdent("literal"), Body: value.Body}
			g.seams[name] = append(g.seams[name], made)
		}
	}
	ast.Inspect(file, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.AssignStmt:
			for i, lhs := range node.Lhs {
				if i >= len(node.Rhs) {
					continue
				}
				// A FIELD, AND DELIBERATELY NOT A LOCAL. Seams are filed by name,
				// so `look := func(…)` inside one function would lend its body to
				// every other function with a local called `look` — which is not an
				// over-approximation a reader can act on, it is a fictional call
				// chain. A field and a package-level name are spelled once in the
				// package and are the two shapes that actually carry indirection
				// out of the function that made them.
				if target, ok := lhs.(*ast.SelectorExpr); ok {
					note(target.Sel.Name, node.Rhs[i])
				}
			}
		case *ast.ValueSpec:
			// `var processOpener = startOpener`, and every other package-level
			// name that holds a function. (A `var` inside a function body reaches
			// here too; that is the same shape spelled in a smaller scope.)
			for i, name := range node.Names {
				if i < len(node.Values) {
					note(name.Name, node.Values[i])
				}
			}
		case *ast.CompositeLit:
			// `modelsForService: opts.ModelsForService` — which is how EVERY seam
			// the door installs on [Options] reaches this package.
			for _, element := range node.Elts {
				if pair, ok := element.(*ast.KeyValueExpr); ok {
					if key, ok := pair.Key.(*ast.Ident); ok {
						note(key.Name, pair.Value)
					}
				}
			}
		}
		return true
	})
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
	commands := commandNames(fn.Body)
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
				// A PROCESS STARTED, on a command this body built — bound to a
				// name, or chained straight off exec.Command.
				if processStarts[fun.Sel.Name] {
					bound, _ := fun.X.(*ast.Ident)
					if buildsCommand(fun.X) || (bound != nil && commands[bound.Name]) {
						bad = append(bad, "exec.Cmd."+fun.Sel.Name+" at "+
							g.fset.Position(call.Pos()).String())
						return true
					}
				}
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
		// A name that holds a function reaches whatever this package installed in
		// it. Call sites arrive here two ways — `a.field(…)` pushes the bare field
		// name, `plainName(…)` pushes `.plainName` — and [surfaceGraph.readSeams]
		// filed both under the bare name.
		for _, next := range g.seams[strings.TrimPrefix(name, ".")] {
			if _, made := g.decls[next]; made {
				visit(next)
				continue
			}
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

// THE LAW'S OWN LAW: every shape of indirection the head of this file claims to
// follow, planted in a package of its own and found. A walk that is green over
// the real package proves only that nothing it can SEE is wrong; this proves
// what it can see. Each planted source is the smallest spelling of one shape the
// real package uses, and the planted sin is named so the assertion can say which
// shape went blind.
func TestTheLawSeesEveryShapeOfIndirectionItClaims(t *testing.T) {
	const planted = `package tui3

import (
	"os"
	"os/exec"

	"github.com/Agent-Field/aforge-v2/internal/guard"
)

type app struct{ probe func(); door func() }

// A field assigned a function: a.gitProbe = gitHead.
func fieldTarget() { os.Stat("field") }

// A package-level var holding a function: var processOpener = startOpener.
var viaVar = varTarget
func varTarget() { exec.Command("var").Run() }

// A composite-literal key: modelsForService: opts.ModelsForService.
func literalKeyTarget() { os.ReadFile("key") }

// A literal written straight into a package var.
var inlineLiteral = func() { os.Open("literal") }

func (a *app) View() {
	a.probe = fieldTarget
	a.probe()
	viaVar()
	a.door()
	inlineLiteral()
	a.other()
}

// The constructor that installs the door, as newApp installs every Options seam.
func build() *app { return &app{door: literalKeyTarget} }

// A LOCAL named like another function's local must NOT lend it a body.
func (a *app) other() { look := func() {}; look() }
func unrelated()      { look := func() { os.Stat("fictional") }; look() }

// The update loop: a closure handed to guard.Go runs elsewhere; the same work
// written inline runs on the loop.
func (a *app) Update() {
	guard.Go("x", func() { exec.Command("elsewhere").Run() })
	run := func() { exec.Command("inline").Run() }
	run()
}
`
	g := newSurfaceGraph()
	file, err := parser.ParseFile(g.fset, "planted.go", planted, 0)
	if err != nil {
		t.Fatal(err)
	}
	g.add(file)

	frame := g.reach([]string{"app.View"}, frameForbidden, nil, true)
	for _, want := range []struct{ shape, node string }{
		{"a field assigned a function", ".fieldTarget"},
		{"a package-level var holding a function", ".varTarget"},
		{"a composite-literal key", ".literalKeyTarget"},
	} {
		if len(frame[want.node]) == 0 {
			t.Errorf("the walk is blind to %s: %s was not reached", want.shape, want.node)
		}
	}
	literal := false
	for node := range frame {
		if strings.HasPrefix(node, "func literal") {
			literal = true
		}
	}
	if !literal {
		t.Error("the walk is blind to a literal written straight into a package var")
	}
	if _, fictional := frame[".unrelated"]; fictional {
		t.Error("a local closure lent its body to another function's local of the same name")
	}
	for node, sins := range frame {
		for _, sin := range sins {
			if strings.Contains(sin, "fictional") || strings.Contains(node, "unrelated") {
				t.Errorf("a fictional call chain reached %s: %s", node, sin)
			}
		}
	}

	// The two planted process starts, found by the line their argument is on so
	// the assertion does not depend on counting lines by hand.
	lineOf := func(needle string) string {
		for i, line := range strings.Split(planted, "\n") {
			if strings.Contains(line, needle) {
				return "planted.go:" + itoa(i+1) + ":"
			}
		}
		t.Fatalf("the planted source lost %q", needle)
		return ""
	}
	loop := g.reach([]string{"app.Update"}, processForbidden, nil, false)
	inline, elsewhere := false, false
	for _, sins := range loop {
		for _, sin := range sins {
			inline = inline || strings.Contains(sin, lineOf(`"inline"`))
			elsewhere = elsewhere || strings.Contains(sin, lineOf(`"elsewhere"`))
		}
	}
	if !inline {
		t.Errorf("the update law missed a process started by a closure the loop runs itself: %v", loop)
	}
	if elsewhere {
		t.Error("the update law walked a closure handed to guard.Go, which runs elsewhere")
	}
}
