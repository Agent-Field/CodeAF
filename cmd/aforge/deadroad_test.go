package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// THE STANDING GATE ON COMMAND ENTRY POINTS THAT NOTHING DISPATCHES.
//
// `runChat` sat at the top of `cmd/aforge/chat.go` for weeks with no caller:
// `main` sent `aforge chat` to `runChatV3` in every arm, and the six thousand
// lines under the dead entry still read as though a chat window ran the
// resident scheduler. Nothing in the compiler notices that — an unexported
// function with no caller is not an error in Go — and a reader has no way to
// tell a live road from a dead one except by tracing the dispatch by hand.
// That trace cost a whole recon pass on #323 before it was filed as #329.
//
// So the shape is held here: EVERY `run<Command>` IN THIS PACKAGE IS REACHABLE
// FROM `main`. A road that loses its dispatch fails this test in the same
// change that orphans it, while the author still knows whether the road was
// meant to die or the switch arm was meant to stay.
//
// The reachability is deliberately GENEROUS — a name mentioned anywhere in a
// reachable body counts as called, receivers are not resolved, shadowing is
// ignored, and one name reaches EVERY declaration that bears it, methods on
// unrelated types included. Every one of those choices errs the same way: a
// generous walk can call a dead road live, and it can never call a live road
// dead. That is the property a gate needs — it fires on nothing but a genuine
// orphan, so it needs no exception ledger beside it. Package-level variable
// initialisers are roots alongside `main`, because a function handed to a table
// is dispatched just as truly as one named in the switch.
//
// The generosity has one edge worth naming: `var _ = runSomething` would count,
// so a road could in principle be kept alive by a reference that dispatches
// nothing. That is a line a reviewer reads in a diff, which is the same bargain
// `SIZE-BUDGET` and `.github/known-red.txt` make.
func TestEveryCommandEntryIsReachableFromMain(t *testing.T) {
	files, decls := parsePackageMain(t)

	entry := regexp.MustCompile(`^run[A-Z]`)
	var entries []string
	for name := range decls {
		if entry.MatchString(name) {
			entries = append(entries, name)
		}
	}
	sort.Strings(entries)
	if len(entries) == 0 {
		t.Fatal("no run<Command> entry points found — the walk is looking at the wrong files")
	}

	reached := reachableFromMain(files, decls)
	var orphans []string
	for _, name := range entries {
		if !reached[name] {
			orphans = append(orphans, name)
		}
	}
	if len(orphans) > 0 {
		t.Fatalf("these command entry points are not reachable from main, so nothing can ever run them: %s\n"+
			"Either dispatch them from main.go or delete them along with everything only they reach.",
			strings.Join(orphans, ", "))
	}
}

// parsePackageMain reads every non-test source file of cmd/aforge and returns
// the parsed files beside an index of the top-level functions in them.
//
// A name maps to EVERY declaration that bears it. Methods are indexed by their
// own name without their receiver — the walk below never needs to know which
// type a call landed on, only whether the name was spoken — and this package
// has several names on several types (`start`, `check`, `prime`, `close`). One
// entry per name would keep whichever the parse saw last and lose the bodies of
// the rest, which would leave the things only those bodies reach looking dead.
func parsePackageMain(t *testing.T) ([]*ast.File, map[string][]*ast.FuncDecl) {
	t.Helper()
	names, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("list the package sources: %v", err)
	}
	fset := token.NewFileSet()
	var files []*ast.File
	decls := map[string][]*ast.FuncDecl{}
	for _, name := range names {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		source, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		file, err := parser.ParseFile(fset, name, source, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		files = append(files, file)
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			decls[fn.Name.Name] = append(decls[fn.Name.Name], fn)
		}
	}
	if len(files) == 0 {
		t.Fatal("no sources parsed — run this test from the package directory")
	}
	return files, decls
}

// reachableFromMain walks out from `main` and from every package-level variable
// initialiser, marking each declared name spoken along the way.
func reachableFromMain(files []*ast.File, decls map[string][]*ast.FuncDecl) map[string]bool {
	reached := map[string]bool{}
	var queue []string

	visit := func(node ast.Node) {
		for name := range namesSpokenIn(node) {
			if _, declared := decls[name]; declared && !reached[name] {
				reached[name] = true
				queue = append(queue, name)
			}
		}
	}
	for _, file := range files {
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.VAR {
				continue
			}
			visit(gen)
		}
	}
	if mains, ok := decls["main"]; ok {
		reached["main"] = true
		for _, main := range mains {
			visit(main.Body)
		}
	}
	for len(queue) > 0 {
		name := queue[len(queue)-1]
		queue = queue[:len(queue)-1]
		for _, fn := range decls[name] {
			visit(fn.Body)
		}
	}
	return reached
}

// namesSpokenIn collects every bare identifier and every selector's field name
// under a node. Both matter: a package-level function is called bare, and a
// method is called through a selector whose receiver this walk does not type.
func namesSpokenIn(node ast.Node) map[string]bool {
	spoken := map[string]bool{}
	ast.Inspect(node, func(n ast.Node) bool {
		switch typed := n.(type) {
		case *ast.Ident:
			spoken[typed.Name] = true
		case *ast.SelectorExpr:
			spoken[typed.Sel.Name] = true
		}
		return true
	})
	return spoken
}
