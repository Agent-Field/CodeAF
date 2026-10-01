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
var bannedWords = regexp.MustCompile(`(?i)\b(relays?|nodes?|leases?|leased|manifests?|takeovers?|take|takes|taking|taken|took)\b`)

// spelledForTheMachine are the pieces of a string that name a flag or a value
// a person types. They are removed before the check, so the flag stays and the
// sentence around it is still judged.
var spelledForTheMachine = strings.NewReplacer("CODEAF_TASK_BELT=node", "", "--node", "")

// typedLiterals are whole literals that are a word a person types, a wire name
// or a pattern over an engine message, never a sentence they read.
var typedLiterals = map[string]bool{
	"relay":       true, // the `codeaf relay` command word
	"nodes":       true, // a key of the --json envelope
	"node ":       true, // the prefix the engine puts on its own error
	`^node \d+: `: true, // the pattern that strips that prefix
}

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
	"internal/pair/lines.go",
	"internal/remote/driver.go",
	"internal/pair/errors.go",
	"internal/pair/offer.go",
	"internal/pair/join.go",
	"cmd/codeaf/pair.go",
}

func TestVocabularyLawUIStrings(t *testing.T) {
	root := moduleRoot(t)
	for _, file := range append(uiStringFiles, commandFiles(t, root)...) {
		for _, found := range bannedInStrings(t, filepath.Join(root, file)) {
			t.Errorf("%s: %s", file, found)
		}
	}
}

// commandFiles are every non-test source file of the command, whose usage and
// help text a person reads (module-relative).
func commandFiles(t *testing.T, root string) []string {
	t.Helper()
	all, err := filepath.Glob(filepath.Join(root, "cmd/codeaf/*.go"))
	if err != nil {
		t.Fatal(err)
	}
	var files []string
	for _, path := range all {
		if !strings.HasSuffix(path, "_test.go") {
			files = append(files, "cmd/codeaf/"+filepath.Base(path))
		}
	}
	return files
}

// manualPages are manual pages whose fenced blocks quote what the program
// prints (module-relative). The prose around a block may explain the engine;
// the block is what a person sees on the screen.
var manualPages = []string{
	"internal/manual/chat/reaching-this-machine-without-ssh.md",
}

func TestVocabularyLawManualQuotes(t *testing.T) {
	root := moduleRoot(t)
	for _, page := range manualPages {
		body, err := os.ReadFile(filepath.Join(root, page))
		if err != nil {
			t.Fatal(err)
		}
		for _, found := range bannedInFences(string(body)) {
			t.Errorf("%s: %s", page, found)
		}
	}
}

// bannedInFences returns each fenced-block line that holds a banned word.
func bannedInFences(body string) []string {
	var found []string
	inside := false
	for i, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "```") {
			inside = !inside
			continue
		}
		if word := bannedWords.FindString(spelledForTheMachine.Replace(line)); inside && word != "" {
			found = append(found, "line "+strconv.Itoa(i+1)+": "+strconv.Quote(word)+" in "+strconv.Quote(line))
		}
	}
	return found
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
		if word := bannedWords.FindString(spelledForTheMachine.Replace(text)); word != "" && !typedLiterals[text] {
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
			if isOldSpelling(n) {
				return false
			}
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

// isOldSpelling reports renamedFlag(flags, "old", "now"): both are words a
// person types, never sentences, and the old one is hidden from --help.
func isOldSpelling(call *ast.CallExpr) bool {
	name, ok := call.Fun.(*ast.Ident)
	return ok && name.Name == "renamedFlag"
}

// isFlagDeclaration reports flags.String("name", "", "help"): the name is a
// word the person types, the help is a sentence they read.
func isFlagDeclaration(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	shape, ok := flagDeclarations[sel.Sel.Name]
	return ok && len(call.Args) > shape.help && isStringLit(call.Args[shape.name])
}

// flagShape says where a declaring method keeps the flag's name and where its
// help text starts.
type flagShape struct{ name, help int }

var flagDeclarations = map[string]flagShape{
	"String": {0, 2}, "Bool": {0, 2}, "Int": {0, 2}, "Duration": {0, 2}, "Float64": {0, 2},
	"StringVar": {1, 3}, "BoolVar": {1, 3}, "IntVar": {1, 3}, "DurationVar": {1, 3}, "Float64Var": {1, 3},
}

func isStringLit(expr ast.Expr) bool {
	lit, ok := expr.(*ast.BasicLit)
	return ok && lit.Kind == token.STRING
}

func flagHelp(call *ast.CallExpr) []*ast.BasicLit {
	var out []*ast.BasicLit
	sel := call.Fun.(*ast.SelectorExpr)
	for _, arg := range call.Args[flagDeclarations[sel.Sel.Name].help:] {
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
	src := "package p\nvar a = \"set it to your relay's address\"\nvar b = \"a task node\"\nvar c = \"set --via or CODEAF_TASK_BELT=node\"\nvar d = \"Move here\"\nfunc f() { flags.String(\"relay\", \"\", \"the sync address\") }\n"
	dir := t.TempDir()
	path := filepath.Join(dir, "p.go")
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := bannedInStrings(t, path); len(got) != 2 {
		t.Fatalf("want 2 findings (relay, node), got %v", got)
	}
}

func TestVocabularyLawSeesBannedWordsInFences(t *testing.T) {
	body := "the relay explains\n```\nno relay is set up\nMove here\n```\n"
	if got := bannedInFences(body); len(got) != 1 {
		t.Fatalf("want 1 finding, got %v", got)
	}
}
