package publish

import (
	"errors"
	"io/fs"
	"testing"
)

func TestEveryFailureHasOneStableCodeAndNextStep(t *testing.T) {
	tests := []struct {
		err             error
		operation, code string
	}{
		{errors.New("GH006: protected branch"), "push", "branch_protected"},
		{fs.ErrPermission, "push", "no_push_permission"},
		{errors.New("rejected (non-fast-forward)"), "push", "push_rejected"},
		{errors.New("HTTP 403 forbidden"), "pr", "pr_create_forbidden"},
		{errors.New("repository is archived"), "push", "repo_archived"},
		{errors.New("could not resolve host github.com"), "push", "github_unreachable"},
		{errors.New("API rate limit exceeded"), "pr", "rate_limited"},
		{fs.ErrNotExist, "worktree", "worktree_missing"},
		{errors.New("unexpected reply"), "pr", "unknown"},
	}
	seen := map[string]bool{}
	for _, row := range Codes() {
		if seen[row.Code] || row.Next == "" {
			t.Fatalf("invalid code row: %+v", row)
		}
		seen[row.Code] = true
	}
	if !seen["machine_offline"] || len(seen) != 10 {
		t.Fatalf("codes = %v", seen)
	}
	for _, tt := range tests {
		t.Run(tt.code, func(t *testing.T) {
			if got := ErrorCode(Wrap(tt.err, tt.operation)); got != tt.code || !seen[got] {
				t.Fatalf("code = %q", got)
			}
		})
	}
	copy := Codes()
	copy[0].Next = "changed"
	if Codes()[0].Next == "changed" {
		t.Fatal("caller changed the table")
	}
}
