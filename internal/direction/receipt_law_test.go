package direction

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// ── THE RECEIPT LAWS ────────────────────────────────────────────────────────
//
// Only a person makes a rule govern, and the store can only know that from a
// PersonReceipt. These laws are what keep that sentence true as callers are
// added: a receipt is minted at a runtime site a reviewer named, never in a
// tool a model calls, and never spelled field by field anywhere but receipt.go.
// A second path to the collections store's raw transactions would bypass every
// check the write path makes, so those doors belong to this package alone.

const directionImport = "github.com/Agent-Field/aforge-v2/internal/direction"
const workspaceImport = "github.com/Agent-Field/aforge-v2/internal/workspace"

// receiptConstructors are the four doors a person's act reaches the runtime by.
var receiptConstructors = map[string]bool{
	"FromCardAnswer": true, "FromTerminal": true, "FromPage": true, "FromVerifiedStatement": true,
}

// receiptSites is THE ALLOW-LIST: "path/to/file.go" → the constructors it may
// call. It is empty until the lane that answers a card, runs a terminal verb,
// serves the direction page or captures a statement lands; that lane adds its
// one site here with the reason, and review reads the line.
var receiptSites = map[string][]string{}

type lawFile struct {
	rel  string
	fset *token.FileSet
	file *ast.File
}

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("%s is not the repository root: %v", root, err)
	}
	return root
}

// sources parses every non-test Go file in the checkout. A file that does not
// parse is counted and skipped: several sessions share the tree, and another
// lane's half-written file is not this law's business — but a walk where
// nothing parsed cannot pass.
func sources(t *testing.T) []lawFile {
	t.Helper()
	root := repoRoot(t)
	var files []lawFile
	unparsed := 0
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := entry.Name()
		if entry.IsDir() {
			if name == ".git" || name == "bin" || name == "third_party" || name == "node_modules" || name == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			unparsed++
			return nil
		}
		files = append(files, lawFile{rel: filepath.ToSlash(rel), fset: fset, file: file})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) < 100 {
		t.Fatalf("only %d files parsed (%d did not); the law saw too little to pass", len(files), unparsed)
	}
	return files
}

// localName is the name a file refers to an imported package by, or "".
func localName(file *ast.File, path string) string {
	for _, spec := range file.Imports {
		p, err := strconv.Unquote(spec.Path.Value)
		if err != nil || p != path {
			continue
		}
		if spec.Name != nil {
			return spec.Name.Name
		}
		return filepath.Base(path)
	}
	return ""
}

// selections lists every pkg.Name the file spells for the given import.
func selections(f lawFile, path string) []struct {
	name string
	pos  token.Position
} {
	local := localName(f.file, path)
	var found []struct {
		name string
		pos  token.Position
	}
	if local == "" || local == "_" {
		return found
	}
	ast.Inspect(f.file, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if x, ok := sel.X.(*ast.Ident); ok && x.Name == local {
			found = append(found, struct {
				name string
				pos  token.Position
			}{sel.Sel.Name, f.fset.Position(sel.Pos())})
		}
		return true
	})
	return found
}

// A RECEIPT IS MINTED ONLY AT AN ALLOW-LISTED RUNTIME SITE. No other file in
// the tree calls a receipt constructor, and no tool a model can call mentions
// the receipt, its constructors or AsPerson at all.
func TestPersonReceiptsAreMintedOnlyAtAllowListedSites(t *testing.T) {
	for _, f := range sources(t) {
		isTool := strings.HasPrefix(f.rel, "internal/session/tools_")
		for _, sel := range selections(f, directionImport) {
			if isTool && (receiptConstructors[sel.name] || sel.name == "PersonReceipt" || sel.name == "AsPerson") {
				t.Errorf("%s: a model's tool names direction.%s; a tool proposes and the person accepts", sel.pos, sel.name)
				continue
			}
			if !receiptConstructors[sel.name] {
				continue
			}
			allowed := false
			for _, name := range receiptSites[f.rel] {
				allowed = allowed || name == sel.name
			}
			if !allowed {
				t.Errorf("%s: direction.%s is called at a site not on receiptSites; add the site with its reason or remove the call", sel.pos, sel.name)
			}
		}
	}
}

// Inside this package, a PersonReceipt is spelled with fields only in
// receipt.go, and exactly the four constructors return one.
func TestOnlyTheFourConstructorsSpellAPersonReceipt(t *testing.T) {
	var constructors []string
	for _, f := range sources(t) {
		if filepath.Dir(f.rel) != "internal/direction" {
			continue
		}
		ast.Inspect(f.file, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.CompositeLit:
				if id, ok := n.Type.(*ast.Ident); ok && id.Name == "PersonReceipt" && len(n.Elts) > 0 && filepath.Base(f.rel) != "receipt.go" {
					t.Errorf("%s: a PersonReceipt is spelled field by field outside receipt.go", f.fset.Position(n.Pos()))
				}
			case *ast.FuncDecl:
				if n.Recv != nil || !n.Name.IsExported() || n.Type.Results == nil {
					return true
				}
				for _, result := range n.Type.Results.List {
					if id, ok := result.Type.(*ast.Ident); ok && id.Name == "PersonReceipt" {
						constructors = append(constructors, n.Name.Name)
					}
				}
			}
			return true
		})
	}
	sort.Strings(constructors)
	want := []string{"FromCardAnswer", "FromPage", "FromTerminal", "FromVerifiedStatement"}
	if strings.Join(constructors, ",") != strings.Join(want, ",") {
		t.Fatalf("the functions that return a PersonReceipt are %v, want exactly %v", constructors, want)
	}
}

// THE RAW TRANSACTION DOORS ON THE COLLECTIONS STORE BELONG TO THIS PACKAGE.
// Anything else writing through them would skip validation, fences, the
// authority check and the live index.
func TestOnlyTheDirectionPackageUsesTheStoresRawTransactions(t *testing.T) {
	for _, f := range sources(t) {
		if strings.HasPrefix(f.rel, "internal/direction/") || strings.HasPrefix(f.rel, "internal/workspace/") {
			continue
		}
		ast.Inspect(f.file, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if ok && (sel.Sel.Name == "ReadSnapshot" || sel.Sel.Name == "WriteImmediate") && localName(f.file, workspaceImport) != "" {
				t.Errorf("%s: %s is the direction package's door", f.fset.Position(sel.Pos()), sel.Sel.Name)
			}
			return true
		})
	}
}
