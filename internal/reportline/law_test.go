package reportline

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// EVERY PATH THAT QUOTES A REPORT'S FIRST LINE ROUTES THROUGH THIS PACKAGE.
//
// The bug (#931) was two readers: the surface skipped the fence through
// firstProseLine while the engine read the literal first line through taskOutcome,
// so the same report drew a sentence on the landed card and three backticks on the
// tasks table. The fix is one reader, and this law is what keeps it one — a future
// hand that reaches for firstLine instead of FirstProse to open a report fails here
// before it can put the fence back on a surface.
//
// The sanctioned readers are reportline.FirstProse itself and tui3's firstProseLine,
// the thin wrapper the surface already calls (and which this law also pins to
// FirstProse). Each named function below must call one of them.
func TestEverySurfaceThatQuotesAReportRoutesThroughFirstProse(t *testing.T) {
	_, here, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate repository")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(here), "..", ".."))

	// name → whether the function's body calls a sanctioned report reader.
	mustRoute := map[string]bool{
		"taskOutcome":       false, // internal/session/task_index.go
		"taskFirstSentence": false, // internal/tui3/taskstable.go
		"switcherFirstLine": false, // internal/tui3/switcher.go
		"landedCard":        false, // internal/tui3/taskdone.go
	}
	seen := map[string]bool{}
	// firstProseLine is the surface's wrapper; it must itself reach FirstProse.
	wrapperReachesFirstProse := false
	wrapperSeen := false

	set := token.NewFileSet()
	walkErr := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", "bin", "third_party", "testdata":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(set, path, nil, 0)
		if err != nil {
			return err
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			if _, watched := mustRoute[fn.Name.Name]; watched {
				seen[fn.Name.Name] = true
				if bodyCallsAny(fn.Body, "FirstProse", "firstProseLine") {
					mustRoute[fn.Name.Name] = true
				}
			}
			if fn.Name.Name == "firstProseLine" {
				wrapperSeen = true
				if bodyCallsAny(fn.Body, "FirstProse") {
					wrapperReachesFirstProse = true
				}
			}
		}
		return nil
	})
	if walkErr != nil {
		t.Fatal(walkErr)
	}

	for name, routed := range mustRoute {
		if !seen[name] {
			t.Errorf("%s was not found in the tree — if it was renamed, teach this law its new name", name)
			continue
		}
		if !routed {
			t.Errorf("%s does not route through reportline.FirstProse (or tui3's firstProseLine); a report read with firstLine puts the fence back on the surface (#931)", name)
		}
	}
	if !wrapperSeen {
		t.Error("tui3's firstProseLine was not found; it is the surface's sanctioned wrapper over reportline.FirstProse")
	} else if !wrapperReachesFirstProse {
		t.Error("tui3's firstProseLine must delegate to reportline.FirstProse so there is one reading, not a copied loop (#931)")
	}
}

// bodyCallsAny reports whether body contains a call whose function name — a bare
// identifier or the selector's final name — is one of names.
func bodyCallsAny(body *ast.BlockStmt, names ...string) bool {
	allow := make(map[string]bool, len(names))
	for _, name := range names {
		allow[name] = true
	}
	found := false
	ast.Inspect(body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		switch fun := call.Fun.(type) {
		case *ast.SelectorExpr:
			found = found || allow[fun.Sel.Name]
		case *ast.Ident:
			found = found || allow[fun.Name]
		}
		return true
	})
	return found
}
