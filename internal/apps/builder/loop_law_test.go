package builder

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"strconv"
	"strings"
	"testing"
)

// TestTheRoadSwitchIsReadInOnePlace holds the law that CODEAF_APPS_BUILDER_ROAD
// is read once, in road.go: the variable's name is spelled once, and nothing
// outside road.go mentions the constant that holds it. New is the one caller of the reader. A second reader would be
// a switch two doors could disagree about.
func TestTheRoadSwitchIsReadInOnePlace(t *testing.T) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(info fs.FileInfo) bool { return !strings.HasSuffix(info.Name(), "_test.go") }, 0)
	if err != nil {
		t.Fatal(err)
	}
	spelled, outside := 0, 0
	for _, pkg := range pkgs {
		for name, file := range pkg.Files {
			ast.Inspect(file, func(n ast.Node) bool {
				switch x := n.(type) {
				case *ast.BasicLit:
					if x.Kind == token.STRING {
						if v, _ := strconv.Unquote(x.Value); v == "CODEAF_APPS_BUILDER_ROAD" {
							spelled++
						}
					}
				case *ast.Ident:
					if x.Name == "RoadEnv" && name != "road.go" {
						outside++
						t.Errorf("%s reads the road switch outside road.go", fset.Position(x.Pos()))
					}
				}
				return true
			})
		}
	}
	if spelled != 1 {
		t.Errorf("the road variable is spelled %d times in non-test code, want 1", spelled)
	}
	if outside != 0 {
		t.Errorf("%d reads of the road switch outside road.go", outside)
	}
}
