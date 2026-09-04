package exec

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/plan"
	"github.com/Agent-Field/aforge-v2/internal/provider"
)

// plan.Node.Checked is the marker because it is a string an ending replaces;
// nothing accumulates into it, so a door copying a leaf's ending writes a plain
// assignment and this walk catches it. The tree's other Checked is an unrelated
// counter on resident.WatchPass in cmd/aforge/wake.go and is reached only by +=;
// naming it here keeps the law from being widened until ordinary watchkeeping
// makes it red and somebody deletes it. Reading the plain assignments across
// the checkout makes a second copy of the ending fail in the file that
// introduces it instead of leaving a comment to be remembered.
func TestOnlyTheSettlingSeamWritesALeafsEnding(t *testing.T) {
	root := repoRoot(t)
	var writers []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", "third_party", "bin":
				if path != root {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		fset := token.NewFileSet()
		file, parseErr := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if parseErr != nil {
			return parseErr
		}
		ast.Inspect(file, func(node ast.Node) bool {
			assignment, ok := node.(*ast.AssignStmt)
			if !ok || assignment.Tok != token.ASSIGN {
				return true
			}
			for _, left := range assignment.Lhs {
				selector, ok := left.(*ast.SelectorExpr)
				if !ok || selector.Sel.Name != "Checked" {
					continue
				}
				position := fset.Position(selector.Pos())
				relative, relErr := filepath.Rel(root, position.Filename)
				if relErr != nil {
					relative = position.Filename
				}
				writers = append(writers, fmt.Sprintf("%s:%d", filepath.ToSlash(relative), position.Line))
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walking the checkout for leaf-ending writers: %v", err)
	}
	if len(writers) != 1 || !strings.HasPrefix(writers[0], "internal/exec/settle.go:") {
		t.Fatalf("a leaf's ending writes Checked at %v; internal/exec/settle.go must be its one writer. "+
			"Call exec.Settle from every door instead of copying the ending's assignments.", writers)
	}
}

// The chat and headless doors both settle a leaf, but only the chat door has a
// journal, so reaching the same call is what keeps their endings from drifting.
// declaredFunction matches a name alone; recordOutcome and apply are each unique
// in their file. If either door is renamed or moved, this test moves with it
// rather than deleting the line that keeps the doors together.
func TestBothDoorsSettleThroughTheSeam(t *testing.T) {
	tests := []struct {
		path     string
		function string
		call     string
		door     string
	}{
		{"cmd/aforge/chat.go", "recordOutcome", "exec.Settle", "chat recordOutcome"},
		{"internal/exec/schedule.go", "apply", "Settle", "headless scheduler apply"},
	}
	for _, test := range tests {
		t.Run(test.door, func(t *testing.T) {
			path := filepath.Join(repoRoot(t), filepath.FromSlash(test.path))
			file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
			if err != nil {
				t.Fatalf("parse %s: %v", test.path, err)
			}
			function := declaredFunction(file, test.function)
			if function == nil || function.Body == nil {
				t.Fatalf("%s no longer has %s; move this law with the door that settles its node", test.path, test.function)
			}
			if !bodyCalls(function.Body, test.call) {
				t.Fatalf("the %s door stopped reaching %s; every door that settles a leaf must call the one seam", test.door, test.call)
			}
		})
	}
}

func TestSettleWritesTheWholeEndingAndThenJournals(t *testing.T) {
	node := &plan.Node{}
	outcome := &Outcome{
		Text:      "the finished result",
		Artifacts: []string{"answer.txt", "report.json"},
		Turns:     4,
		Usage: Usage{
			PromptTokens:     50,
			CompletionTokens: 7,
			Cost:             1.25,
		},
		Stop:    StopDone,
		Verdict: provider.VerdictVerifiedSuccess,
		Account: &Account{
			Files:  []FileChange{{Path: "answer.txt", Change: ChangeAdded, Added: 3}},
			Checks: []Check{{Command: "go test ./...", Passed: true}},
		},
		Calibration: []string{"comfortably inside the envelope"},
	}
	journaled := 0
	Settle(node, outcome, nil, func() {
		journaled++
		assertSettledEnding(t, node, outcome)
		if node.State != plan.StateDone {
			t.Errorf("state at journal time = %q, want %q", node.State, plan.StateDone)
		}
	})
	assertSettledEnding(t, node, outcome)
	if node.State != plan.StateDone {
		t.Errorf("state = %q, want %q", node.State, plan.StateDone)
	}
	if journaled != 1 {
		t.Errorf("journal calls = %d, want 1", journaled)
	}
}

func TestSettleWithNoJournalStillSettlesTheNode(t *testing.T) {
	tests := []struct {
		name    string
		outcome *Outcome
		err     error
	}{
		{"an error", &Outcome{Text: "partial"}, errors.New("the attempt failed")},
		{"no outcome", nil, nil},
		{"an empty result", &Outcome{Text: "  \n"}, nil},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			node := &plan.Node{}
			Settle(node, test.outcome, test.err, nil)
			if node.State != plan.StateFailed {
				t.Errorf("state = %q, want %q", node.State, plan.StateFailed)
			}
		})
	}
}

func assertSettledEnding(t *testing.T, node *plan.Node, outcome *Outcome) {
	t.Helper()
	if node.Turns != outcome.Turns {
		t.Errorf("turns = %d, want %d", node.Turns, outcome.Turns)
	}
	wantTokens := outcome.Usage.PromptTokens + outcome.Usage.CompletionTokens
	if node.Tokens != wantTokens {
		t.Errorf("tokens = %d, want %d", node.Tokens, wantTokens)
	}
	if node.Cost != outcome.Usage.Cost {
		t.Errorf("cost = %v, want %v", node.Cost, outcome.Usage.Cost)
	}
	if node.Stop != string(outcome.Stop) {
		t.Errorf("stop = %q, want %q", node.Stop, outcome.Stop)
	}
	if node.Verdict != outcome.Verdict {
		t.Errorf("verdict = %q, want %q", node.Verdict, outcome.Verdict)
	}
	if !reflect.DeepEqual(node.Artifacts, outcome.Artifacts) {
		t.Errorf("artifacts = %#v, want %#v", node.Artifacts, outcome.Artifacts)
	}
	if node.Result != outcome.Text {
		t.Errorf("result = %q, want %q", node.Result, outcome.Text)
	}
	if node.Checked != outcome.Account.Summary() {
		t.Errorf("checked = %q, want %q", node.Checked, outcome.Account.Summary())
	}
	if !reflect.DeepEqual(node.Calibration, outcome.Calibration) {
		t.Errorf("calibration = %#v, want %#v", node.Calibration, outcome.Calibration)
	}
}

func declaredFunction(file *ast.File, name string) *ast.FuncDecl {
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if ok && function.Name.Name == name {
			return function
		}
	}
	return nil
}

func bodyCalls(body *ast.BlockStmt, wanted string) bool {
	found := false
	ast.Inspect(body, func(node ast.Node) bool {
		if found {
			return false
		}
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		switch function := call.Fun.(type) {
		case *ast.Ident:
			if wanted == function.Name {
				found = true
			}
		case *ast.SelectorExpr:
			owner, ok := function.X.(*ast.Ident)
			if ok && wanted == owner.Name+"."+function.Sel.Name {
				found = true
			}
		}
		return !found
	})
	return found
}
