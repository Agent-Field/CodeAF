package lane

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

// ── THE LAWS, HELD BY THE BUILD ─────────────────────────────────────────────
//
// This package is written by five lanes of work at once, and every one of them
// has a reason to break one of these four rules in a hurry. Each is stated in
// the package doc and each fails here with a file name and a line number.
//
// They are structural tests in this repo's convention — the sources are read
// with go/ast and held to a law rather than to a behaviour, as
// `internal/session/taxonomy_law_test.go` does for the response boundary.

// sources parses every .go file in this directory, test files included, and
// says which are which.
func sources(t *testing.T) (*token.FileSet, map[string]*ast.File) {
	t.Helper()
	fset := token.NewFileSet()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read the package directory: %v", err)
	}
	files := map[string]*ast.File{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			continue
		}
		parsed, err := parser.ParseFile(fset, entry.Name(), nil, parser.ParseComments)
		if err != nil {
			t.Fatalf("parse %s: %v", entry.Name(), err)
		}
		files[entry.Name()] = parsed
	}
	if len(files) == 0 {
		t.Fatal("no sources were found, so these laws would pass vacuously")
	}
	return fset, files
}

func isTest(name string) bool { return strings.HasSuffix(name, "_test.go") }

// TestNothingHereOpensAConnectionOrDrawsAnything is the boundary the whole
// design rests on.
//
// This package holds an opinion about lanes. It does not fetch, it does not
// stream, and it does not draw. The transport adapts it and the surface reads
// it, and both of those directions are one-way: an import of `net/http` here
// would mean a belief could go and look something up, which is the exact
// failure the "no fetch on the send path" law exists to prevent, and an import
// of a surface would mean a number could be formatted for a person in the place
// that computes it.
//
// The one legitimate need for a server in this package's tests is met by
// `internal/lane/lanestub`, which is a separate package for that reason.
func TestNothingHereOpensAConnectionOrDrawsAnything(t *testing.T) {
	forbidden := []string{
		"net/http",
		"github.com/Agent-Field/aforge-v2/internal/provider",
		"github.com/Agent-Field/aforge-v2/internal/tui",
		"github.com/Agent-Field/aforge-v2/internal/tui2",
		"github.com/Agent-Field/aforge-v2/internal/tui3",
		"github.com/Agent-Field/aforge-v2/internal/session",
	}
	fset, files := sources(t)
	for name, file := range files {
		if isTest(name) {
			continue
		}
		for _, imported := range file.Imports {
			path := strings.Trim(imported.Path.Value, `"`)
			for _, banned := range forbidden {
				if path == banned {
					t.Errorf("%s imports %s — this package is provider-agnostic and surface-agnostic",
						fset.Position(imported.Pos()), path)
				}
			}
		}
	}
}

// TestTheChoiceReadsNoClock keeps the chooser pure.
//
// [Request.Now] is how the moment gets in, and it is what makes a choice
// reproducible: the same request and the same beliefs give the same answer,
// today and in a test that pins a Tuesday in August. A time.Now anywhere in the
// choosing files would make every test of the router a test of the machine it
// ran on, and the exploration this router does is random enough already.
func TestTheChoiceReadsNoClock(t *testing.T) {
	choosing := func(name string) bool {
		return strings.HasPrefix(name, "choose") ||
			strings.HasPrefix(name, "frontier") ||
			strings.HasPrefix(name, "value")
	}
	fset, files := sources(t)
	seen := 0
	for name, file := range files {
		if isTest(name) || !choosing(name) {
			continue
		}
		seen++
		ast.Inspect(file, func(node ast.Node) bool {
			selector, ok := node.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			ident, ok := selector.X.(*ast.Ident)
			if !ok {
				return true
			}
			if ident.Name == "time" && (selector.Sel.Name == "Now" || selector.Sel.Name == "Since") {
				t.Errorf("%s reads a clock — the moment is Request.Now", fset.Position(selector.Pos()))
			}
			return true
		})
	}
	if seen == 0 {
		t.Fatal("no choosing files were found, so this law passed vacuously")
	}
}

// TestEveryFileOpensWithItsLaw holds the repo's own convention where it matters
// most.
//
// Everything in this package is a judgement — why a lane was dropped, why a
// wait was thought surprising, why a second request was worth its money — and a
// judgement without its reasoning written down is one nobody can safely change.
// So every source file here opens with prose before its first declaration.
func TestEveryFileOpensWithItsLaw(t *testing.T) {
	fset, files := sources(t)
	for name, file := range files {
		if isTest(name) {
			continue
		}
		if len(file.Comments) == 0 {
			t.Errorf("%s opens with no law", name)
			continue
		}
		opening := file.Comments[0]
		// The import block does not count as a declaration here: a law stated
		// above the imports and a law stated below them are the same law, and
		// which side of them it sits on is gofmt's business rather than a
		// reader's.
		if first := firstRealDecl(file); first != nil && opening.Pos() > first.Pos() {
			t.Errorf("%s: the first prose in the file is at %s, after its first declaration",
				name, fset.Position(opening.Pos()))
			continue
		}
		text := strings.TrimSpace(opening.Text())
		if !strings.Contains(text, ".") || len(text) < 80 {
			t.Errorf("%s opens with %q, which is a label rather than a reason", name, text)
		}
	}
}

// TestConcreteImplementationsAreBuiltOnlyInTheRegistry is why there is a
// registry at all.
//
// Five seams and five implementations, and exactly one file that knows which
// struct answers which interface. A caller that reached past it and built a
// concrete ledger for itself would be a caller with its own beliefs, and the
// first time the two disagreed the picker and the router would be steering by
// different numbers.
//
// Test files are exempt: a test double is the whole point of the seam.
func TestConcreteImplementationsAreBuiltOnlyInTheRegistry(t *testing.T) {
	const factory = "registry.go"
	fset, files := sources(t)
	built := 0
	for name, file := range files {
		if isTest(name) || name == factory {
			continue
		}
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			ident, ok := call.Fun.(*ast.Ident)
			if !ok || !strings.HasPrefix(ident.Name, "new") || len(ident.Name) < 4 {
				return true
			}
			if ident.Name[3] < 'A' || ident.Name[3] > 'Z' {
				return true
			}
			t.Errorf("%s builds %s outside %s", fset.Position(call.Pos()), ident.Name, factory)
			return true
		})
	}
	// And the registry really does build all five, so that the law is about
	// something rather than about an empty set.
	ast.Inspect(files[factory], func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		if ident, ok := call.Fun.(*ast.Ident); ok && strings.HasPrefix(ident.Name, "new") && ident.Name != "newRegistry" {
			built++
		}
		return true
	})
	if built < 5 {
		t.Fatalf("the registry builds %d implementations; there are five seams", built)
	}
}

// firstRealDecl is the first declaration that is not the import block.
func firstRealDecl(file *ast.File) ast.Decl {
	for _, decl := range file.Decls {
		if general, ok := decl.(*ast.GenDecl); ok && general.Tok == token.IMPORT {
			continue
		}
		return decl
	}
	return nil
}
