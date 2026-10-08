package tui3

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// ONE GRID, NAMED: every width on the factory floor is a name in
// factory_grid.go. This walks every factory source but that one and fails on
// a number used as a width or a pad, on a run of spaces spelled as a literal,
// and on a width constant declared anywhere else. 0 and 1 stay free, because
// index arithmetic needs them and they are never a column.
//
// WHAT COUNTS AS A WIDTH is read off the code's own shape rather than guessed:
// a number inside the arguments of a call that lays text out in cells
// ([factoryWidthCalls]), and a number added to or taken from a name that is a
// width or a room ([factoryWidthNames]).
func TestFactoryWidthsComeFromTheGrid(t *testing.T) {
	files, err := filepath.Glob("factory_*.go")
	if err != nil {
		t.Fatal(err)
	}
	files = append(files, "place_factory.go")
	fset := token.NewFileSet()
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") || path == "factory_grid.go" || path == "factorycard.go" {
			continue
		}
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		report := func(n ast.Node, why string) {
			t.Errorf("%s: %s — name it in factory_grid.go", fset.Position(n.Pos()), why)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.GenDecl:
				if n.Tok != token.CONST {
					return true
				}
				for _, sp := range n.Specs {
					for _, name := range sp.(*ast.ValueSpec).Names {
						if factoryWidthConst.MatchString(name.Name) {
							report(name, "the width constant "+name.Name+" is declared outside the grid")
						}
					}
				}
			case *ast.BasicLit:
				if n.Kind == token.STRING {
					if s, err := strconv.Unquote(n.Value); err == nil && len(s) >= 2 && strings.Trim(s, " ") == "" {
						report(n, "a run of spaces is spelled as a literal")
					}
				}
			case *ast.CallExpr:
				if factoryWidthCalls[factoryCallName(n.Fun)] {
					for _, arg := range n.Args {
						factoryEachNumber(arg, func(lit *ast.BasicLit) {
							report(lit, "the number "+lit.Value+" lays out text in "+factoryCallName(n.Fun))
						})
					}
				}
			case *ast.BinaryExpr:
				if n.Op != token.ADD && n.Op != token.SUB {
					return true
				}
				for _, side := range [][2]ast.Expr{{n.X, n.Y}, {n.Y, n.X}} {
					if lit, ok := side[0].(*ast.BasicLit); ok && factoryBigNumber(lit) && factoryWidthName(side[1]) {
						report(lit, "the number "+lit.Value+" is added to a width")
					}
				}
			}
			return true
		})
	}
}

// factoryWidthConst is a constant's name that says it is a width.
var factoryWidthConst = regexp.MustCompile(`(Cols|W|Wrap|Gap|Lead|Width|Margin|Floor|Gutter)$`)

// factoryWidthCalls are the calls that lay text out in cells.
var factoryWidthCalls = map[string]bool{
	"strings.Repeat": true, "fit": true, "factoryPad": true, "factorySpread": true,
	"ansi.Truncate": true, "ansi.Cut": true, "wrap": true, "factoryJoinWhole": true,
	"factoryMetaLine": true, "noteFit": true, "factoryLed": true, "factorySpaces": true,
	"placeTeachProse": true, "factoryPeekWidth": true, "factoryStripRow": true,
	"factoryPaneWithFoot": true, "factoryPane": true,
}

// factoryWidthNames are names that hold a width, a measure or a room.
var factoryWidthNames = regexp.MustCompile(`(?i)^(width|measure|w|lead|room|avail|cols|inner|rest|leadW|rowsW|paneW|railW|footW)$|W$|Cols$`)

func factoryWidthName(e ast.Expr) bool {
	switch e := e.(type) {
	case *ast.Ident:
		return factoryWidthNames.MatchString(e.Name)
	case *ast.SelectorExpr:
		return factoryWidthNames.MatchString(e.Sel.Name)
	}
	return false
}

func factoryBigNumber(lit *ast.BasicLit) bool {
	if lit.Kind != token.INT {
		return false
	}
	v, err := strconv.Atoi(lit.Value)
	return err == nil && v > 1
}

func factoryEachNumber(e ast.Expr, fn func(*ast.BasicLit)) {
	ast.Inspect(e, func(n ast.Node) bool {
		if lit, ok := n.(*ast.BasicLit); ok && factoryBigNumber(lit) {
			fn(lit)
		}
		return true
	})
}

func factoryCallName(e ast.Expr) string {
	switch e := e.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.SelectorExpr:
		if x, ok := e.X.(*ast.Ident); ok && (x.Name == "strings" || x.Name == "ansi") {
			return x.Name + "." + e.Sel.Name
		}
		return e.Sel.Name
	}
	return ""
}
