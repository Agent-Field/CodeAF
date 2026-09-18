package tui3

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"sort"
	"strings"
	"testing"
)

// A TEST MAY NOT READ A PROFILE IT DID NOT CREATE, AND A HELPER IS WHERE A TEST
// GETS ONE.
//
// [runTests] moves CODEAF_HOME to ONE temporary root for the whole package run,
// so an app built without a profile of its own reads and writes that one
// directory — and an earlier test's setup_seen_at decides what a later one sees.
// That is contamination by construction, and it hid a red for a week:
// TestTheOpeningHintNamesBothDoors passed in the package run because a test
// before it had written a marker, and failed alone, from #680 until #1153.
//
// The fix is at the helpers — the one place every test's surface comes from —
// so the rule is held here rather than in a reviewer's head: every helper in
// this package that builds a surface with [newApp] must either name a profile
// directory in the call or move the state root itself, which is the same fact
// stated the other way — `t.Setenv(home.EnvVar, t.TempDir())` is how
// [ordinaryLaunch] opens a surface the way a bare `codeaf` does, with no
// profile directory named and a root of its own. The few helpers a *testing.T
// never reaches mint one with [mintProfileDir]; the rest take a t.TempDir(). A
// helper that means two surfaces to share a profile says so by naming the
// directory, which this test reads for.
//
// IT IS A LAW ABOUT HELPERS AND NOT ABOUT TEST BODIES. A test that calls
// [newApp] directly is not a helper, and this test does not read those sites —
// the brief that asked for this law narrowed the work to the helpers on purpose,
// because a sweep of every direct call site would collide with a branch
// rebasing onto trunk. The direct sites that name no profile are therefore
// still exposed, and the change note lists them rather than hiding them.
func TestEveryHelperThatBuildsASurfaceNamesAProfile(t *testing.T) {
	fset := token.NewFileSet()
	for _, name := range testSources(t) {
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil || isTestEntry(fn.Name.Name) {
				continue
			}
			if movesStateRoot(fn) {
				// The whole function is isolated: every app it builds resolves its
				// unnamed profile under a root this test moved.
				continue
			}
			for _, call := range surfaceCalls(fn) {
				if callNamesProfile(call, fn) {
					continue
				}
				t.Errorf("%s: %s builds a surface with newApp but names no profile "+
					"(line %d).\nA helper must give each app a profile of its own: t.TempDir() "+
					"where a *testing.T is in hand, mintProfileDir() where none is. A helper that "+
					"means two surfaces to share one profile passes the same directory to both on "+
					"purpose.", name, fn.Name.Name, fset.Position(call.Pos()).Line)
			}
		}
	}
}

// testSources is every _test.go file in this package, sorted so a failure reads
// the same way twice.
func testSources(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, "_test.go") {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// isTestEntry reports whether a function is one the go test runner names — a
// test, a benchmark, a fuzz target or an example — rather than a helper.
func isTestEntry(name string) bool {
	for _, prefix := range []string{"Test", "Benchmark", "Fuzz", "Example"} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

// surfaceCalls is every direct [newApp] call in one function's body.
func surfaceCalls(fn *ast.FuncDecl) []*ast.CallExpr {
	var out []*ast.CallExpr
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if id, ok := call.Fun.(*ast.Ident); ok && id.Name == "newApp" {
			out = append(out, call)
		}
		return true
	})
	return out
}

// movesStateRoot reports whether a function pins the state root itself —
// `t.Setenv("CODEAF_HOME", ...)` or `t.Setenv(home.EnvVar, ...)` — which makes
// an unnamed profile a directory of this test's own rather than the run's.
func movesStateRoot(fn *ast.FuncDecl) bool {
	found := false
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || len(call.Args) == 0 {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Setenv" {
			return true
		}
		if namesStateRoot(call.Args[0]) {
			found = true
		}
		return true
	})
	return found
}

// namesStateRoot is the variable a root move names: the literal CODEAF_HOME or
// home.EnvVar, which carries the same bytes.
func namesStateRoot(arg ast.Expr) bool {
	switch node := arg.(type) {
	case *ast.BasicLit:
		return node.Kind == token.STRING && node.Value == `"CODEAF_HOME"`
	case *ast.SelectorExpr:
		return node.Sel.Name == "EnvVar"
	}
	return false
}

// callNamesProfile reports whether a [newApp] call passes an [Options] literal
// carrying a ProfileDir key. It follows the one shape a helper actually uses —
// `opts := Options{...; ProfileDir: dir}` and then `newApp(ctx, opts)` — as well
// as the inline literal, so a helper that names the profile in a local is read
// correctly rather than failed.
func callNamesProfile(call *ast.CallExpr, fn *ast.FuncDecl) bool {
	if len(call.Args) < 2 {
		return false
	}
	arg := call.Args[len(call.Args)-1]
	if id, ok := arg.(*ast.Ident); ok {
		arg = localLiteral(fn, id.Name)
	}
	lit, ok := arg.(*ast.CompositeLit)
	if !ok {
		return false
	}
	for _, elt := range lit.Elts {
		if kv, ok := elt.(*ast.KeyValueExpr); ok {
			if key, ok := kv.Key.(*ast.Ident); ok && key.Name == "ProfileDir" {
				return true
			}
		}
	}
	return false
}

// localLiteral is the composite literal a local name was assigned in this
// function, or nil when the name is not a literal the function built.
func localLiteral(fn *ast.FuncDecl, name string) ast.Expr {
	var found ast.Expr
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		assign, ok := n.(*ast.AssignStmt)
		if !ok || len(assign.Lhs) != len(assign.Rhs) {
			return true
		}
		for i, lhs := range assign.Lhs {
			id, ok := lhs.(*ast.Ident)
			if !ok || id.Name != name {
				continue
			}
			if lit, ok := assign.Rhs[i].(*ast.CompositeLit); ok {
				found = lit
			}
		}
		return true
	})
	return found
}
