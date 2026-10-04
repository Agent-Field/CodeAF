package inventory

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/executor"
)

// ── THE L13 LAW ─────────────────────────────────────────────────────────────
//
// Only the harness writes .cell/env/inventory.json, and only through this
// package (docs/ARCHITECTURE.md section 13). A manifest that a repo, a cell or
// the model can write is a channel for installing software, so:
//
//	(a) no other package may name the file: any string literal containing
//	    "inventory.json" outside internal/inventory is a violation;
//	(b) the executor refuses a tool call that reaches into .cell/ by directory
//	    or by argument, so a tool call cannot write it.
const inventoryFile = "inventory.json"

// namesInventory returns the position of each string literal in src that
// contains the inventory file name.
func namesInventory(t *testing.T, filename, src string) []string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filename, src, 0)
	if err != nil {
		t.Fatal(err)
	}
	var hits []string
	ast.Inspect(f, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		if s, err := strconv.Unquote(lit.Value); err == nil && strings.Contains(s, inventoryFile) {
			hits = append(hits, fset.Position(lit.Pos()).String())
		}
		return true
	})
	return hits
}

func TestL13CheckerBites(t *testing.T) {
	src := "package p\nvar _ = \".cell/env/inventory.json\"\nvar _ = `x/inventory.json`"
	if hits := namesInventory(t, "f.go", src); len(hits) != 2 {
		t.Fatalf("want 2 hits, got %v", hits)
	}
	if hits := namesInventory(t, "f.go", "package p\nvar _ = \"meta.json\""); len(hits) != 0 {
		t.Fatalf("false hit: %v", hits)
	}
}

func TestL13OnlyTheInventoryPackageNamesTheFile(t *testing.T) {
	root := repoRoot(t)
	own := filepath.Join(root, "internal", "inventory") + string(filepath.Separator)
	for _, dir := range []string{"internal", "cmd"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") || strings.HasPrefix(p, own) {
				return nil
			}
			src, rerr := os.ReadFile(p)
			if rerr != nil {
				return rerr
			}
			for _, hit := range namesInventory(t, p, string(src)) {
				t.Errorf("%s names %s outside internal/inventory", hit, inventoryFile)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestL13ExecutorRefusesCallsIntoTheCellDirectory(t *testing.T) {
	ex := executor.Local{Root: t.TempDir()}
	for _, req := range []executor.ExecRequest{
		{Argv: []string{"true"}, Dir: ".cell/env"},
		{Argv: []string{"tee", ".cell/env/" + inventoryFile}},
		{Argv: []string{"cp", "x", "./.cell/env/" + inventoryFile}},
		{Argv: []string{"sh", "-c", "echo {} > .cell/env/" + inventoryFile}},
		{Argv: []string{"tool", "--out=.cell/env/" + inventoryFile}},
		{Argv: []string{"tee", "/some/cell/.cell/env/" + inventoryFile}},
	} {
		if _, err := ex.Exec(t.Context(), req, nil); err == nil {
			t.Errorf("executor ran %v", req)
		}
	}
	// A path that merely resembles the directory is fine.
	if _, err := ex.Exec(t.Context(), executor.ExecRequest{Argv: []string{"true", "my.cell/x", ".cellar"}}, nil); err != nil {
		t.Errorf("executor refused a look-alike: %v", err)
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for ; dir != filepath.Dir(dir); dir = filepath.Dir(dir) {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
	}
	t.Fatal("go.mod not found")
	return ""
}

func equal(a, b Inventory) bool { return reflect.DeepEqual(a, b) }
