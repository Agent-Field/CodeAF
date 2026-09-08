package head

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// money.go is the ONLY file in internal/head that spells a dollar figure.
//
// Both spellings live there: moneyUSD for measurements such as a plan step, a
// result, a finished window, a receipt or a rail, and dimeUSD for the live board
// and depth block alone. Keeping the exemption on their one home avoids a
// blessing scattered over the package. This walk skips _test.go because it is
// about what a person reads; a test's own failure message may legitimately
// spell $%.2f.
//
// The go/ast and go/parser imports put this law on the pull-request gate through
// scripts/laws.sh. Naming that seam here keeps a later import cleanup from
// silently taking the law off the gate.
func TestOnlyMoneyGoSpellsADollarFigureInTheHead(t *testing.T) {
	directory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	moneyFormat := regexp.MustCompile(`\$%[-+ #0-9.*']*[fgeEv]`)
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || filepath.Ext(name) != ".go" || strings.HasSuffix(name, "_test.go") || name == "money.go" {
			continue
		}
		files := token.NewFileSet()
		parsed, err := parser.ParseFile(files, filepath.Join(directory, name), nil, 0)
		if err != nil {
			t.Errorf("parse %s: %v", name, err)
			continue
		}
		ast.Inspect(parsed, func(node ast.Node) bool {
			literal, ok := node.(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				return true
			}
			value, err := strconv.Unquote(literal.Value)
			if err != nil || !moneyFormat.MatchString(value) {
				return true
			}
			position := files.Position(literal.Pos())
			t.Errorf("%s:%d: money format literal %s must route through moneyUSD", name, position.Line, literal.Value)
			return true
		})
	}
}
