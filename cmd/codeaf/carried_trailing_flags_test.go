package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/delegate"
	"github.com/Agent-Field/codeaf/internal/session"
)

// FLAGS TYPED AFTER THE BRIEF REACH A SHELL RUN'S CHILD AS FLAGS. Since the
// parser started reading a trailing ceiling (#1700), `codeaf senior-dev
// "brief" --max-cost 0.5` in a repository said `up to $0.50` and then died:
// the child line kept the person's order, the folder step took its last word
// for the brief, and the note saying where the copy is landed between
// --max-cost and its value, which the child refused to parse. Whatever order
// the person typed, the child reads back the same ceiling, works in the copy
// rather than the person's --dir, and is handed the note ahead of the brief.
func TestAShellRunsTrailingFlagsReachItsChildAsFlags(t *testing.T) {
	program := fakeFolderProgram()
	copied := &session.ProgramFolder{Dir: "/tmp/copy", Repo: "/r/repo", Branch: "task/fix-it-abc123"}
	for _, typed := range [][]string{
		{"fix", "it", "--max-cost", "0.5"},
		{"fix", "it", "--max-cost=0.5", "--dir", "/r/repo/sub"},
		{"--dir", "/r/repo/sub", "fix", "--max-cost", "0.5", "it"},
		{"--max-cost", "0.5", "--dir", "/r/repo/sub", "fix", "it"},
	} {
		inv, err := delegate.Parse(program, typed, &bytes.Buffer{})
		if err != nil {
			t.Fatalf("%q: %v", typed, err)
		}
		child := carriedInFolder(carriedChildLine(inv), inv, copied)
		again, err := delegate.Parse(program, child[1:], &bytes.Buffer{})
		if err != nil {
			t.Fatalf("%q: the child cannot read its own line %q: %v", typed, child, err)
		}
		if again.Ceilings.CostUSD != 0.5 {
			t.Fatalf("%q: the child's ceiling is %v, want 0.5 (line %q)", typed, again.Ceilings.CostUSD, child)
		}
		if again.Workspace != copied.Dir {
			t.Fatalf("%q: the child works in %q, want the copy %q (line %q)", typed, again.Workspace, copied.Dir, child)
		}
		brief := again.Brief()
		if !strings.HasPrefix(brief, copied.BriefNote()) || !strings.HasSuffix(brief, "fix it") {
			t.Fatalf("%q: the child's brief is %q, want the copy's note and then %q", typed, brief, "fix it")
		}
	}
}
