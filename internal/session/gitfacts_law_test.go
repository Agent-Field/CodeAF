package session

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

// NOBODY EVER WAITS ON GIT (gitfacts.go).
//
// The state of a repository is read so the model does not have to spend a shell
// command discovering it, and the whole value of that trade evaporates if the
// reading is ever waited on: [Agent.refreshGitLocked] runs under a.mu at the
// start of a turn, on the person's own path, and `git status` is seconds on a
// large repository. The reading is [offpath.Take]'s and it is settled with a
// ZERO-length bound — what has arrived is used, what has not is taken for free
// at the start of the next turn.
//
// It is a structural law and not a comment because the failure it prevents is
// invisible: a `Settle(someDuration)` here would pass every test, be fast on
// every machine that runs them, and cost a second per turn on somebody's
// monorepo. The one number allowed is 0.
func TestTheGitReadingIsNeverWaitedOn(t *testing.T) {
	set := token.NewFileSet()
	file, err := parser.ParseFile(set, "gitfacts.go", nil, 0)
	if err != nil {
		t.Fatalf("parsing gitfacts.go: %v", err)
	}
	settles := 0
	ast.Inspect(file, func(node ast.Node) bool {
		callExpr, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := callExpr.Fun.(*ast.SelectorExpr)
		if !ok || selector.Sel.Name != "Settle" {
			return true
		}
		settles++
		if len(callExpr.Args) != 1 {
			t.Errorf("%s: Settle takes one bound; this call has %d", set.Position(callExpr.Pos()), len(callExpr.Args))
			return true
		}
		literal, ok := callExpr.Args[0].(*ast.BasicLit)
		if !ok || literal.Value != "0" {
			t.Errorf("%s: the git reading is settled with a bound that is not 0, so a turn can wait on `git status`",
				set.Position(callExpr.Pos()))
		}
		return true
	})
	if settles == 0 {
		t.Fatal("gitfacts.go settles no reading at all — either the reading moved, and this law moves with it, or the facts are being gathered on the person's path")
	}
}

// AND THE READING IS NOT RUN UNDER THE LOCK. [Agent.refreshGitLocked] is called
// with a.mu held, so the one thing it must never do is call git itself: the
// gathering belongs on the goroutine [offpath.Take] starts, and the function
// under the lock only starts it and folds in what came back.
func TestTheRefreshUnderTheLockRunsNoGit(t *testing.T) {
	set := token.NewFileSet()
	file, err := parser.ParseFile(set, "gitfacts.go", nil, 0)
	if err != nil {
		t.Fatalf("parsing gitfacts.go: %v", err)
	}
	for _, decl := range file.Decls {
		function, ok := decl.(*ast.FuncDecl)
		if !ok || function.Name.Name != "refreshGitLocked" {
			continue
		}
		ast.Inspect(function.Body, func(node ast.Node) bool {
			callExpr, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			// A bare `git(...)` or a call to the reading itself, anywhere but
			// inside the closure handed to offpath.Take, would put a shell
			// command under a.mu.
			if name, ok := callExpr.Fun.(*ast.Ident); ok && (name.Name == "git" || name.Name == "gitFacts" || name.Name == "gitFactsLine") {
				t.Errorf("%s: refreshGitLocked calls %s directly, under a.mu and on the person's path",
					set.Position(callExpr.Pos()), name.Name)
			}
			return true
		})
		return
	}
	t.Fatal("refreshGitLocked is gone from gitfacts.go; this law has to move with it")
}

// AND A CONVERSATION WITH NO WORKSPACE ASKS GIT NOTHING. `exec.Cmd` reads an
// empty Dir as "the calling process's working directory", so a headless run or
// a test with no workspace would otherwise report the state of whatever
// checkout the binary was launched from as though it were the model's own
// project — the same hole task_run.go's [git] carries a real incident about.
func TestAConversationWithNoWorkspaceReadsNoGit(t *testing.T) {
	if line := gitFactsLine("   "); line != "" {
		t.Fatalf("a blank root answered %q, which is some other repository's state", line)
	}
	if block := gitFacts("", nil); block != "" {
		t.Fatalf("a conversation with no workspace and no folders rendered %q", block)
	}
}
