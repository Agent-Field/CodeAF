package lawcheck

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

// THE VOCABULARY LAW (docs: anywhere UX lane). A person's machines are
// devices, the verbs are "Move here" and "Continue here", and the words below
// are the engine's, never theirs. The check reads string literals only, so a
// type, a package or a comment may keep the engine's name; what a person can
// read may not.
var bannedWords = regexp.MustCompile(`(?i)\b(relays?|nodes?|leases?|leased|manifests?|takeovers?|take over|took over|taken over)\b`)

// spelledForTheMachine are the pieces of a string that name a flag or a value
// a person types. They are removed before the check, so the flag stays and the
// sentence around it is still judged.
var spelledForTheMachine = strings.NewReplacer("--relay", "", "CODEAF_TASK_BELT=node", "")

// uiStringFiles are the tables of text a person reads (module-relative).
var uiStringFiles = []string{
	"internal/chatlist/copy.go",
	"internal/pair/serve.go",
	"internal/pair/list.go",
	"cmd/codeaf/pool.go",
	"cmd/codeaf/do.go",
	"cmd/codeaf/identity_rotate.go",
	"cmd/codeaf/chatv3_at.go",
	"internal/tui3/settings.go",
	"internal/tui3/wallbar.go",
	"internal/tui3/commands.go",
	"internal/config/settings.go",
}

// deferredUIStringFiles still carry the old words and are owned by another
// lane until it lands (t-ux-vocab2); the list only ever shrinks.
var deferredUIStringFiles = []string{
	"internal/pair/lines.go",
	"internal/pair/errors.go",
	"internal/pair/offer.go",
	"internal/pair/join.go",
	"cmd/codeaf/pair.go",
}

func TestVocabularyLawUIStrings(t *testing.T) {
	root := moduleRoot(t)
	for _, file := range uiStringFiles {
		for _, found := range bannedInStrings(t, filepath.Join(root, file)) {
			t.Errorf("%s: %s", file, found)
		}
	}
}

// The deferred list must name real files, so it cannot rot into a hiding place.
func TestVocabularyLawDeferredFilesExist(t *testing.T) {
	root := moduleRoot(t)
	for _, file := range deferredUIStringFiles {
		if _, err := parser.ParseFile(token.NewFileSet(), filepath.Join(root, file), nil, parser.PackageClauseOnly); err != nil {
			t.Errorf("deferred file %s: %v (remove it from the list once it is clean or gone)", file, err)
		}
	}
}

func bannedInStrings(t *testing.T, path string) []string {
	t.Helper()
	fset := token.NewFileSet()
	tree, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	var found []string
	for _, lit := range uiLiterals(tree) {
		text, err := strconv.Unquote(lit.Value)
		if err != nil {
			continue
		}
		if word := bannedWords.FindString(spelledForTheMachine.Replace(text)); word != "" {
			found = append(found, fset.Position(lit.Pos()).String()+": "+strconv.Quote(word)+" in "+strconv.Quote(text))
		}
	}
	return found
}

// uiLiterals are the string literals a person could read: not an import path,
// not a struct tag, and not the bare name of a flag.
func uiLiterals(tree *ast.File) []*ast.BasicLit {
	var out []*ast.BasicLit
	ast.Inspect(tree, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.ImportSpec:
			return false
		case *ast.CallExpr:
			if isFlagDeclaration(n) {
				out = append(out, flagHelp(n)...)
				return false
			}
		case *ast.BasicLit:
			if n.Kind == token.STRING {
				out = append(out, n)
			}
		}
		return true
	})
	return dropTags(out, tree)
}

// isFlagDeclaration reports flags.String("name", "", "help"): the name is a
// word the person types, the help is a sentence they read.
func isFlagDeclaration(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || len(call.Args) < 3 {
		return false
	}
	switch sel.Sel.Name {
	case "String", "Bool", "Int", "Duration", "Float64":
		first, ok := call.Args[0].(*ast.BasicLit)
		return ok && first.Kind == token.STRING
	}
	return false
}

func flagHelp(call *ast.CallExpr) []*ast.BasicLit {
	var out []*ast.BasicLit
	for _, arg := range call.Args[2:] {
		ast.Inspect(arg, func(n ast.Node) bool {
			if lit, ok := n.(*ast.BasicLit); ok && lit.Kind == token.STRING {
				out = append(out, lit)
			}
			return true
		})
	}
	return out
}

// dropTags removes struct-tag literals, which are wire names.
func dropTags(lits []*ast.BasicLit, tree *ast.File) []*ast.BasicLit {
	tags := map[*ast.BasicLit]bool{}
	ast.Inspect(tree, func(n ast.Node) bool {
		if f, ok := n.(*ast.Field); ok && f.Tag != nil {
			tags[f.Tag] = true
		}
		return true
	})
	kept := lits[:0]
	for _, lit := range lits {
		if !tags[lit] {
			kept = append(kept, lit)
		}
	}
	return kept
}

// The checker itself is judged on a sample, so a green run means it looks.
func TestVocabularyLawSeesBannedWords(t *testing.T) {
	src := "package p\nvar a = \"set it to your relay's address\"\nvar b = \"a task node\"\nvar c = \"set --relay or CODEAF_TASK_BELT=node\"\nvar d = \"Move here\"\nfunc f() { flags.String(\"relay\", \"\", \"the sync address\") }\n"
	dir := t.TempDir()
	path := filepath.Join(dir, "p.go")
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := bannedInStrings(t, path); len(got) != 2 {
		t.Fatalf("want 2 findings (relay, node), got %v", got)
	}
}
