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

// THE STRUCTURAL GUARD ON ONE LAW: an empty profile directory is the normal
// case, not the absent case, and absence is a hosted window.
//
// [config.ProfileDir] carries AFORGE_PROFILE_DIR, which almost nobody exports,
// so the empty string is what very nearly every launch hands this surface — and
// internal/config has always resolved it to this process's own profile in the
// state root ([config.ProfilePath]). A guard that reads emptiness as "there is
// no profile" and goes quiet is therefore silent on the ordinary launch and
// loud only on the rare one, which is the exact inversion that has now been
// found four separate times here: the model picker's lane pin (lanes.go), the
// settings pair on the belt (internal/session), the status line's crew segment
// and the notices' own memory (#315).
//
// It is not a rule a reviewer can be expected to hold in their head, so it is a
// test. Every comparison of a profile directory against the empty string in this
// package's own sources is listed below WITH THE REASON IT IS THERE. A new one
// fails this test by name, and the fix is nearly always [app.hosted]: a
// connection is the one window whose profile is somebody else's machine.
func TestNoNewSiteReadsAnEmptyProfileDirectoryAsNoProfile(t *testing.T) {
	// known is every site that compares a profile directory against "" today,
	// as `file.go:function`, and what it decides.
	known := map[string]string{
		// The setup screen has nowhere to write an answer — the reading it was
		// given when the guard was written. It is on the wrong side of this law
		// like the four above it, and it is left alone here on purpose: it
		// decides whether a whole screen opens, so moving it is its own change
		// with its own end-to-end run behind it, not a line carried along by the
		// status line's.
		"firstrun.go:openSetup": "whether the first-run and provider screens open at all",
		// The effort rung draws nothing without a profile to write into — the
		// same shape, left for the same reason.
		"effortscope.go:effortProfile": "whether the install's own effort rung is drawn",
		// The gate's posture is deliberately NOT the default here: the YOLO
		// segment's law is that it appears only when somebody said so, and this
		// answer feeds a safety claim. Same shape, same standing question.
		"app.go:readApproval": "whether the status line may claim the gate is open",
	}

	found := map[string]string{}
	for _, name := range packageSources(t) {
		fileSet := token.NewFileSet()
		parsed, err := parser.ParseFile(fileSet, name, nil, 0)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		for _, decl := range parsed.Decls {
			function, ok := decl.(*ast.FuncDecl)
			if !ok || function.Body == nil {
				continue
			}
			for _, line := range emptyProfileComparisons(function) {
				found[name+":"+function.Name.Name] = line
			}
		}
	}

	for site, line := range found {
		if _, ok := known[site]; !ok {
			t.Errorf("%s decides on an empty profile directory (%s), which is the ORDINARY launch.\n"+
				"If the absence you mean is a connection, guard on a.hosted(); if you mean a path,\n"+
				"resolve it with config.ProfilePath. If it truly belongs, add it to this test with its reason.", site, line)
		}
	}
	for site := range known {
		if _, ok := found[site]; !ok {
			t.Errorf("%s no longer compares a profile directory against \"\" — delete its line from this test", site)
		}
	}
}

// packageSources is every non-test Go file in this package, sorted so a failure
// reads the same way twice.
func packageSources(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// emptyProfileComparisons is every `<profile directory> == ""` and `!= ""` in
// one function, rendered as source.
//
// It follows a profile directory through ONE local, because that is how these
// guards are actually written — `dir := strings.TrimSpace(a.profileDir)` and
// then `if dir == ""` — and a check that only saw the direct form would have
// missed two of the three sites it is here to remember.
func emptyProfileComparisons(function *ast.FuncDecl) []string {
	locals := map[string]bool{}
	ast.Inspect(function.Body, func(node ast.Node) bool {
		assign, ok := node.(*ast.AssignStmt)
		if !ok {
			return true
		}
		for i, right := range assign.Rhs {
			if i >= len(assign.Lhs) || !isProfileDir(right, locals) {
				continue
			}
			if name, ok := assign.Lhs[i].(*ast.Ident); ok {
				locals[name.Name] = true
			}
		}
		return true
	})

	var out []string
	ast.Inspect(function.Body, func(node ast.Node) bool {
		compare, ok := node.(*ast.BinaryExpr)
		if !ok || (compare.Op != token.EQL && compare.Op != token.NEQ) {
			return true
		}
		left, right := compare.X, compare.Y
		if isEmptyString(left) {
			left, right = right, left
		}
		if isEmptyString(right) && isProfileDir(left, locals) {
			out = append(out, render(left)+" "+compare.Op.String()+" \"\"")
		}
		return true
	})
	return out
}

// isProfileDir reports whether the expression IS a profile directory —
// `profileDir`, `a.profileDir`, a local that was assigned one, or any of those
// through [strings.TrimSpace]. A call that merely READS the profile
// (`config.TaskModelAt(a.profileDir)`) is a different fact and is not one.
func isProfileDir(expr ast.Expr, locals map[string]bool) bool {
	switch node := expr.(type) {
	case *ast.ParenExpr:
		return isProfileDir(node.X, locals)
	case *ast.Ident:
		return node.Name == "profileDir" || locals[node.Name]
	case *ast.SelectorExpr:
		return node.Sel.Name == "profileDir"
	case *ast.CallExpr:
		if !isTrimSpace(node.Fun) || len(node.Args) != 1 {
			return false
		}
		return isProfileDir(node.Args[0], locals)
	}
	return false
}

func isTrimSpace(fun ast.Expr) bool {
	selector, ok := fun.(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != "TrimSpace" {
		return false
	}
	pkg, ok := selector.X.(*ast.Ident)
	return ok && pkg.Name == "strings"
}

func isEmptyString(expr ast.Expr) bool {
	literal, ok := expr.(*ast.BasicLit)
	return ok && literal.Kind == token.STRING && literal.Value == `""`
}

// render is the expression as a person reads it, which is all a failure needs.
func render(expr ast.Expr) string {
	switch node := expr.(type) {
	case *ast.ParenExpr:
		return "(" + render(node.X) + ")"
	case *ast.Ident:
		return node.Name
	case *ast.SelectorExpr:
		return render(node.X) + "." + node.Sel.Name
	case *ast.CallExpr:
		args := make([]string, 0, len(node.Args))
		for _, arg := range node.Args {
			args = append(args, render(arg))
		}
		return render(node.Fun) + "(" + strings.Join(args, ", ") + ")"
	}
	return "an expression"
}
