package tui3

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
	"testing"
)

// HINT LINES SPELL esc ONE WAY. The majority of them say `esc cancel`, the
// imperative, and a few said `esc cancels`. The same split on any other verb
// is the same defect: one key reading as two gestures. This walks the hint
// word constants, and the hint functions that inline the same words, and
// fails on the form the majority does not use.
func TestHintWordsSpellEscTheWayTheMajorityDoes(t *testing.T) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var lits []string
	for _, pkg := range pkgs {
		for name, file := range pkg.Files {
			if strings.HasSuffix(name, "_test.go") {
				continue
			}
			ast.Inspect(file, func(n ast.Node) bool {
				switch n := n.(type) {
				case *ast.ValueSpec:
					if n.Names == nil {
						return true
					}
					hint := false
					for _, id := range n.Names {
						if hintWordName(id.Name) {
							hint = true
						}
					}
					if !hint {
						return true
					}
					for _, v := range n.Values {
						if s, ok := stringLit(v); ok && strings.Contains(s, "esc ") {
							lits = append(lits, s)
						}
					}
				case *ast.FuncDecl:
					if n.Name == nil || !hintWordName(n.Name.Name) || n.Body == nil {
						return true
					}
					ast.Inspect(n.Body, func(m ast.Node) bool {
						if s, ok := stringLit(m); ok && strings.Contains(s, "esc ") {
							lits = append(lits, s)
						}
						return true
					})
					return false
				}
				return true
			})
		}
	}
	if len(lits) == 0 {
		t.Fatal("no hint word mentioned esc, so the scan is looking in the wrong place")
	}
	count := map[string]int{}
	for _, lit := range lits {
		for _, verb := range escVerbs(lit) {
			count[verb]++
		}
	}
	odd := map[string]string{}
	for verb, n := range count {
		if !strings.HasSuffix(verb, "s") || len(verb) < 2 {
			continue
		}
		bare := strings.TrimSuffix(verb, "s")
		if count[bare] > n {
			odd[verb] = bare
		}
	}
	if len(odd) == 0 {
		return
	}
	for _, lit := range lits {
		for _, verb := range escVerbs(lit) {
			bare, bad := odd[verb]
			if !bad {
				continue
			}
			t.Errorf("a hint says %q, and the majority of hint lines say %q:\n  %s",
				"esc "+verb, "esc "+bare, lit)
		}
	}
}

// hintWordName reports whether a Go name is one of the hint-word constants or
// the functions that return a hint line.
func hintWordName(name string) bool {
	return strings.Contains(name, "Hint") || strings.Contains(name, "Keys")
}

func stringLit(n ast.Node) (string, bool) {
	lit, ok := n.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	s, err := strconv.Unquote(lit.Value)
	if err != nil {
		return "", false
	}
	return s, true
}

// escVerbs is every word standing immediately after `esc ` in s.
func escVerbs(s string) []string {
	var out []string
	rest := s
	for {
		i := strings.Index(rest, "esc ")
		if i < 0 {
			return out
		}
		rest = rest[i+len("esc "):]
		word := rest
		if cut := strings.IndexAny(word, " ·\t"); cut >= 0 {
			word = word[:cut]
		}
		if word != "" {
			out = append(out, word)
		}
	}
}
