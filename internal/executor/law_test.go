package executor

import (
	"fmt"
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

// ── THE L4 LAW ──────────────────────────────────────────────────────────────
//
// The executor is the only path to a process (docs/ARCHITECTURE.md 8.2). In the
// packages that run tools, every call to os/exec's Command or CommandContext
// must therefore carry a tag comment on its own line or the line above it:
//
//	//codeaf:plumbing <reason>   harness plumbing (git, ps, an opener); the
//	                             reason is required and is read by a reviewer
//	//codeaf:tool-pending        a tool spawn awaiting migration onto Executor
//	                             (task 4.2); the tests list them, and the tag
//	                             disappears when the site moves
//
// An untagged spawn fails the test. Everything else in the tree is out of this
// law's scope: it constrains tool execution, not every process the harness
// ever starts.

// lawScope lists the tool-execution paths the law covers: directories, or
// single files where only part of a package runs tools.
var lawScope = []string{
	"internal/exec",
	"internal/executor",
	"internal/seniordev",
	"internal/resident",
	"internal/verify",
	"internal/delegate",
	"internal/session/jobs.go",
	"internal/session/tools_watch.go",
}

const (
	tagPlumbing = "//codeaf:plumbing"
	tagPending  = "//codeaf:tool-pending"
)

// site is one spawn call and the tag found for it.
type site struct {
	file string
	line int
	tag  string // "", tagPlumbing or tagPending
	text string // the tag comment, for the reason check
}

func (s site) String() string { return fmt.Sprintf("%s:%d", s.file, s.line) }

// violation names why a site breaks the law, or "" when it does not.
func (s site) violation() string {
	switch s.tag {
	case tagPending:
		return ""
	case tagPlumbing:
		if strings.TrimSpace(strings.TrimPrefix(s.text, tagPlumbing)) == "" {
			return "carries //codeaf:plumbing with no reason"
		}
		return ""
	}
	return "spawns a process without going through internal/executor and carries no tag"
}

func TestL4_ToolSpawnsGoThroughTheExecutor(t *testing.T) {
	sites := scopedSites(t)
	var pending []string
	for _, s := range sites {
		if reason := s.violation(); reason != "" {
			t.Errorf("%s %s", s, reason)
		}
		if s.tag == tagPending {
			pending = append(pending, s.String())
		}
	}
	sort.Strings(pending)
	t.Logf("%d spawn sites scanned; %d tool-pending (task 4.2 inventory):\n  %s",
		len(sites), len(pending), strings.Join(pending, "\n  "))
}

func TestL4_UntaggedSpawnFails(t *testing.T) {
	cases := map[string]struct {
		source string
		want   string
	}{
		"untagged":        {"exec.Command(\"ls\")", "no tag"},
		"tagged pending":  {"//codeaf:tool-pending\n\texec.Command(\"ls\")", ""},
		"tagged plumbing": {"exec.Command(\"ls\") //codeaf:plumbing lists a directory", ""},
		"no reason":       {"//codeaf:plumbing\n\texec.Command(\"ls\")", "no reason"},
		"stale tag":       {"//codeaf:tool-pending\n\n\texec.Command(\"ls\")", "no tag"},
	}
	for name, c := range cases {
		src := "package p\nimport \"os/exec\"\nfunc f() {\n\t" + c.source + "\n}\n"
		found := sitesIn(t, "fixture.go", src)
		if len(found) != 1 {
			t.Fatalf("%s: want one site, got %d", name, len(found))
		}
		if got := found[0].violation(); !strings.Contains(got, c.want) || (c.want == "") != (got == "") {
			t.Errorf("%s: violation %q, want it to contain %q", name, got, c.want)
		}
	}
}

func TestL4_AliasedImportIsStillASpawn(t *testing.T) {
	src := "package p\nimport osexec \"os/exec\"\nfunc f() { osexec.CommandContext(nil, \"ls\") }\n"
	if got := sitesIn(t, "fixture.go", src); len(got) != 1 {
		t.Fatalf("want the aliased spawn found, got %d sites", len(got))
	}
}

// scopedSites parses every non-test file in scope and returns its spawn sites.
func scopedSites(t *testing.T) []site {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	var sites []site
	for _, path := range lawFiles(t, root) {
		source, err := os.ReadFile(filepath.Join(root, path))
		if err != nil {
			t.Fatal(err)
		}
		sites = append(sites, sitesIn(t, path, string(source))...)
	}
	if len(sites) == 0 {
		t.Fatal("no spawn sites found in scope, so this law would pass vacuously")
	}
	return sites
}

// lawFiles is every non-test .go file under lawScope, relative to root.
func lawFiles(t *testing.T, root string) []string {
	t.Helper()
	var files []string
	for _, entry := range lawScope {
		err := filepath.WalkDir(filepath.Join(root, entry), func(path string, d fs.DirEntry, err error) error {
			if err == nil && !d.IsDir() && isLawSource(path) {
				rel, _ := filepath.Rel(root, path)
				files = append(files, filepath.ToSlash(rel))
			}
			return err
		})
		if err != nil {
			t.Fatalf("walk %s: %v", entry, err)
		}
	}
	return files
}

func isLawSource(path string) bool {
	return strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go")
}

// sitesIn finds every os/exec spawn in one source file. A file that does not
// parse fails the test: a law that skips what it cannot read proves nothing.
func sitesIn(t *testing.T, name, source string) []site {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, name, source, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
	local := execImportName(file)
	if local == "" {
		return nil
	}
	tags := tagsByLine(fset, file)
	var sites []site
	ast.Inspect(file, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok && isSpawn(call, local) {
			line := fset.Position(call.Pos()).Line
			text := tags.near(line)
			sites = append(sites, site{file: name, line: line, tag: tagOf(text), text: text})
		}
		return true
	})
	return sites
}

// execImportName is the local name os/exec is imported under, or "".
func execImportName(file *ast.File) string {
	for _, imp := range file.Imports {
		if path, _ := strconv.Unquote(imp.Path.Value); path != "os/exec" {
			continue
		}
		if imp.Name != nil {
			return imp.Name.Name
		}
		return "exec"
	}
	return ""
}

func isSpawn(call *ast.CallExpr, local string) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Name == local && (sel.Sel.Name == "Command" || sel.Sel.Name == "CommandContext")
}

func tagOf(text string) string {
	switch {
	case strings.HasPrefix(text, tagPlumbing):
		return tagPlumbing
	case strings.HasPrefix(text, tagPending):
		return tagPending
	}
	return ""
}

// lineTags indexes comment text by the line each comment group touches.
type lineTags struct {
	trailing map[int]string // comment starting on the line
	above    map[int]string // comment group ending on the line
}

func tagsByLine(fset *token.FileSet, file *ast.File) lineTags {
	tags := lineTags{trailing: map[int]string{}, above: map[int]string{}}
	for _, group := range file.Comments {
		for _, c := range group.List {
			tags.trailing[fset.Position(c.Pos()).Line] = c.Text
		}
		last := group.List[len(group.List)-1]
		tags.above[fset.Position(last.End()).Line] = tagLine(group)
	}
	return tags
}

// tagLine is the group's first comment that is a tag, or its last comment.
func tagLine(group *ast.CommentGroup) string {
	for _, c := range group.List {
		if tagOf(c.Text) != "" {
			return c.Text
		}
	}
	return group.List[len(group.List)-1].Text
}

// near is the tag comment for a call on line: on the line, else ending above.
func (l lineTags) near(line int) string {
	if text := l.trailing[line]; tagOf(text) != "" {
		return text
	}
	return l.above[line-1]
}

// A production binary never has the test seat: only test files may reach
// executortest, and only that package may call UseInTests.
func TestTestSeatIsReachableOnlyFromTests(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	err = filepath.WalkDir(filepath.Join(root, "internal"), func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		if strings.HasPrefix(filepath.ToSlash(rel), "internal/executor/") {
			return nil
		}
		source, readErr := os.ReadFile(path)
		if readErr == nil && (strings.Contains(string(source), "executortest") || strings.Contains(string(source), "UseInTests(")) {
			t.Errorf("%s reaches the test seat from production code", rel)
		}
		return readErr
	})
	if err != nil {
		t.Fatal(err)
	}
}
