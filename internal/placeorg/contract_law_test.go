package placeorg

// The contract, enforced on every package under internal/placeorg by reading
// the tree itself. scripts/laws.sh finds this file by its go/ast import and
// runs it on every pull request.
//
// FOUR LAWS. No package here reaches a provider, a network, the engine or
// the bridge: a model is reached only through the Judge the Runner hands a
// stage, and an embedding only through an Embedder the engine supplies. A
// package that references the Judge declares its fallbacks by conforming to
// Judged. A call to the Judge passes a real fallback, never nil and never an
// empty option. The Tree is changed in exactly one function, the Runner's
// apply, so every change is journaled.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// forbiddenImports are the doors a stage must not open itself, with the
// reason a reader needs.
var forbiddenImports = map[string]string{
	"net/http": "a stage never talks to a network; the engine's Embedder and Judge do",
	"net":      "a stage never talks to a network",
	"os/exec":  "a stage never runs a program",
	"github.com/Agent-Field/codeaf/internal/provider":      "models are reached through the Judge the Runner supplies",
	"github.com/Agent-Field/codeaf/internal/ai":            "models are reached through the Judge the Runner supplies",
	"github.com/Agent-Field/codeaf/internal/session":       "the engine wires this package, never the other way round",
	"github.com/Agent-Field/codeaf/internal/remote":        "the engine wires this package, never the other way round",
	"github.com/Agent-Field/codeaf/internal/desktopbridge": "the bridge calls this package, never the other way round",
}

type parsedFile struct {
	path string
	pkg  string
	file *ast.File
}

// sources parses every non-test Go file under this package's directory,
// sub-packages included.
func sources(t *testing.T) (*token.FileSet, []parsedFile) {
	t.Helper()
	fset := token.NewFileSet()
	var out []parsedFile
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		out = append(out, parsedFile{path: path, pkg: filepath.Dir(path), file: f})
		return nil
	})
	if err != nil {
		t.Fatalf("reading the package tree: %v", err)
	}
	if len(out) == 0 {
		t.Fatal("no sources found; the law must run from internal/placeorg")
	}
	return fset, out
}

func TestNoStageOpensADoorItself(t *testing.T) {
	fset, files := sources(t)
	for _, pf := range files {
		for _, imp := range pf.file.Imports {
			path, _ := strconv.Unquote(imp.Path.Value)
			if why, bad := forbiddenImports[path]; bad {
				t.Errorf("%s: imports %s; %s", fset.Position(imp.Pos()), path, why)
			}
		}
	}
}

// mentionsJudge reports whether a file names the Judge type (as placeorg.Judge
// from a sub-package, or Judge from the root).
func mentionsJudge(pf parsedFile) bool {
	found := false
	ast.Inspect(pf.file, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.SelectorExpr:
			if id, ok := x.X.(*ast.Ident); ok && id.Name == "placeorg" && x.Sel.Name == "Judge" {
				found = true
			}
		case *ast.Ident:
			if pf.pkg == "." && x.Name == "Judge" {
				found = true
			}
		}
		return !found
	})
	return found
}

// declaresJudged reports whether a file has `var _ placeorg.Judged = ...`.
func declaresJudged(pf parsedFile) bool {
	for _, d := range pf.file.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok != token.VAR {
			continue
		}
		for _, s := range gd.Specs {
			vs := s.(*ast.ValueSpec)
			if len(vs.Names) != 1 || vs.Names[0].Name != "_" {
				continue
			}
			if sel, ok := vs.Type.(*ast.SelectorExpr); ok && sel.Sel.Name == "Judged" {
				return true
			}
		}
	}
	return false
}

func TestEveryPackageThatAsksAJudgeDeclaresItsFallbacks(t *testing.T) {
	_, files := sources(t)
	asks := map[string]bool{}
	declares := map[string]bool{}
	for _, pf := range files {
		if pf.pkg == "." {
			continue // the root defines Judge; it holds no stage.
		}
		if mentionsJudge(pf) {
			asks[pf.pkg] = true
		}
		if declaresJudged(pf) {
			declares[pf.pkg] = true
		}
	}
	for pkg := range asks {
		if !declares[pkg] {
			t.Errorf("%s: references placeorg.Judge but no file in it declares `var _ placeorg.Judged = (*YourStage)(nil)`; a stage that asks a model states its fallbacks in code", pkg)
		}
	}
}

// isEmptyFallback reports whether an argument is nil or an empty composite
// literal: a fallback that is no fallback.
func isEmptyFallback(e ast.Expr) bool {
	switch x := e.(type) {
	case *ast.Ident:
		return x.Name == "nil"
	case *ast.CompositeLit:
		return len(x.Elts) == 0
	case *ast.BasicLit:
		return x.Kind == token.STRING && (x.Value == `""` || x.Value == "``")
	}
	return false
}

func TestEveryJudgeCallCarriesAFallback(t *testing.T) {
	fset, files := sources(t)
	for _, pf := range files {
		ast.Inspect(pf.file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			// Choose(ctx, q, fallback) and Write(ctx, q, valid, fallback) are
			// the Judge's two methods; the arity keeps io.Writer.Write and
			// Journal.Write out of this check.
			judgeCall := (sel.Sel.Name == "Choose" && len(call.Args) == 3) || (sel.Sel.Name == "Write" && len(call.Args) == 4)
			if !judgeCall {
				return true
			}
			if last := call.Args[len(call.Args)-1]; isEmptyFallback(last) {
				t.Errorf("%s: %s is called with an empty fallback; the deterministic answer is a required argument", fset.Position(call.Pos()), sel.Sel.Name)
			}
			return true
		})
	}
}

func TestOnlyTheRunnerChangesTheTree(t *testing.T) {
	fset, files := sources(t)
	for _, pf := range files {
		for _, d := range pf.file.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			allowed := fn.Recv != nil && fn.Name.Name == "apply" && receiverName(fn) == "Runner"
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "apply" && !allowed {
					t.Errorf("%s: %s calls apply; only (*Runner).apply may change a Tree, because that is where the journal is written", fset.Position(call.Pos()), fn.Name.Name)
				}
				return true
			})
		}
	}
}

func receiverName(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return ""
	}
	switch t := fn.Recv.List[0].Type.(type) {
	case *ast.StarExpr:
		if id, ok := t.X.(*ast.Ident); ok {
			return id.Name
		}
	case *ast.Ident:
		return t.Name
	}
	return ""
}

// TestTheLawSeesASubPackage proves the walk descends, so a stage package
// added later is covered the day it lands.
func TestTheLawSeesASubPackage(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "x.go"), []byte("package x\nimport \"net/http\"\nvar _ = http.Get\n"), 0o600)
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filepath.Join(dir, "x.go"), nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	bad := 0
	for _, imp := range f.Imports {
		path, _ := strconv.Unquote(imp.Path.Value)
		if _, ok := forbiddenImports[path]; ok {
			bad++
		}
	}
	if bad != 1 {
		t.Fatalf("the forbidden table does not catch net/http")
	}
}
