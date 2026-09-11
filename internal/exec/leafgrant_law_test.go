package exec

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// THE LEAF'S GRANT IS ONE NUMBER AND NOBODY SPELLS IT TWICE.
//
// [DefaultLeafTokens] is calibrated against measured work, and recalibrating it
// is a reading somebody takes and writes down. That reading is worthless if a
// second copy of the figure sits at a door: `aforge exec`, `aforge run` and the
// chat surface each carried their own `150000`, and chat's even carried a
// comment promising it "mirrors the headless run defaults exactly" — which is
// an intention where this repository's one-source-of-truth law wants an
// interpolation. Four numbers had to be moved together, by somebody who knew
// all four were there, and the help text at a door could tell a person a figure
// the loop had stopped using.
//
// So this walks the tree for the literal and fails on it wherever it is not the
// constant's own declaration. It is written with go/parser rather than as a
// grep so that it reads DECLARATIONS — a figure that happens to appear inside a
// string, a comment, or a test's own fixture is not a second default and is not
// what this law is about.
func TestTheLeafGrantIsSpelledOnce(t *testing.T) {
	root := repoRootForGrantLaw(t)
	// The figure as the tree may spell it, in the two notations Go accepts.
	spellings := map[string]bool{
		strconv.Itoa(DefaultLeafTokens):                             true,
		strings.ReplaceAll(commaGroup(DefaultLeafTokens), ",", "_"): true,
	}
	var found []string
	for _, dir := range []string{"cmd/aforge", "internal/exec", "internal/session"} {
		walkGoFiles(t, filepath.Join(root, dir), func(path string, file *ast.File, fset *token.FileSet) {
			ast.Inspect(file, func(n ast.Node) bool {
				spec, ok := n.(*ast.ValueSpec)
				if !ok {
					return true
				}
				for i, value := range spec.Values {
					lit, ok := value.(*ast.BasicLit)
					if !ok || lit.Kind != token.INT || !spellings[lit.Value] {
						continue
					}
					// The declaration of the constant itself is the one place it belongs.
					if i < len(spec.Names) && spec.Names[i].Name == "DefaultLeafTokens" {
						continue
					}
					where := fset.Position(lit.Pos())
					name := "_"
					if i < len(spec.Names) {
						name = spec.Names[i].Name
					}
					found = append(found, name+" = "+lit.Value+" at "+
						strings.TrimPrefix(where.Filename, root+"/")+":"+strconv.Itoa(where.Line))
				}
				return true
			})
		})
	}
	for _, one := range found {
		t.Errorf("the leaf's grant is spelled a second time: %s\n"+
			"read it from exec.DefaultLeafTokens instead — the number is calibrated in one place "+
			"and a copy of it drifts the first time somebody re-measures", one)
	}
}

// commaGroup writes an integer the way Go's underscore notation groups it, so
// the law recognises `250_000` as well as `250000`.
func commaGroup(n int) string {
	digits := strconv.Itoa(n)
	var out []string
	for len(digits) > 3 {
		out = append([]string{digits[len(digits)-3:]}, out...)
		digits = digits[:len(digits)-3]
	}
	return strings.Join(append([]string{digits}, out...), ",")
}

// walkGoFiles parses every non-generated Go file under dir and hands it over.
func walkGoFiles(t *testing.T, dir string, visit func(string, *ast.File, *token.FileSet)) {
	t.Helper()
	fset := token.NewFileSet()
	err := filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		file, parseErr := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if parseErr != nil {
			// A file this package cannot parse is not this law's business to report.
			return nil
		}
		visit(path, file, fset)
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", dir, err)
	}
}

// repoRootForGrantLaw climbs to the directory holding go.mod.
func repoRootForGrantLaw(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("working directory: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("no go.mod above %s", dir)
		}
		dir = parent
	}
}
