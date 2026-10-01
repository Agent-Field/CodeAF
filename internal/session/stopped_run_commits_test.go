package session

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// A worker's own commits are clean in its index when the person stops it.
// The stop still owes the person the branch holding those files, rather than
// sending them away with a sentence saying the run had changed nothing.
func TestAStoppedRunNamesWorkTheWorkerAlreadyCommitted(t *testing.T) {
	for _, pending := range []bool{false, true} {
		name := "committed"
		if pending {
			name = "committed_and_pending"
		}
		t.Run(name, func(t *testing.T) {
			agent, double, conversation, dir := stoppableBeltRun(t, 71)
			double.mu.Lock()
			workspace := double.spec.Workspace
			double.mu.Unlock()
			mustGit(t, workspace, "add", "half.txt")
			mustGit(t, workspace, "-c", "user.name=Worker", "-c", "user.email=worker@example.test", "commit", "-m", "first FAQ item")
			want := []string{"half.txt"}
			if pending {
				runCommitFile(t, workspace, "committed.txt", "another FAQ item")
				for path, body := range map[string]string{"half.txt": "half made\nmore work\n", "pending.txt": "not yet committed\n"} {
					if err := os.WriteFile(filepath.Join(workspace, path), []byte(body), 0o644); err != nil {
						t.Fatal(err)
					}
				}
				want = []string{"committed.txt", "half.txt", "pending.txt"}
			}
			before := gitOut(t, conversation, "rev-parse", "HEAD")
			if _, err := agent.Cancel(CancelTask + ":71"); err != nil {
				t.Fatal(err)
			}
			beltRunWaitFor(t, "the stopped run to settle", func() bool {
				agent.beltMu.Lock()
				defer agent.beltMu.Unlock()
				return agent.beltRun == nil
			})
			rows := agent.graph().runRows(71)
			if len(rows) != 1 || rows[0].Branch == "" || !rows[0].Stopped || rows[0].Merge != mergeAborted {
				t.Fatalf("a stopped run with committed work names no kept branch: %+v", rows)
			}
			changed := append([]string(nil), rows[0].Changed...)
			sort.Strings(changed)
			if !reflect.DeepEqual(changed, want) {
				t.Fatalf("the stopped row lists %v, want each committed and pending file once: %v", changed, want)
			}
			where := "stopped · its work so far is kept on " + rows[0].Branch + " and did not go into " + canonicalPath(conversation) +
				" · merge that branch to bring it in, or delete it to drop it"
			if said := strings.Join(beltRunNotes(t, dir, "71"), "\n"); !strings.Contains(said, where) || strings.Contains(said, "changed nothing") {
				t.Fatalf("the stopped page does not name the worker's committed work:\n%s", said)
			}
			if got := conversationNotes(agent, where); got != 1 {
				t.Fatalf("the stop receipt names the committed work %d times, want once", got)
			}
			for _, path := range want {
				if out, err := git(conversation, "show", rows[0].Branch+":"+path); err != nil || out == "" {
					t.Fatalf("the kept branch does not hold %s: %q, %v", path, out, err)
				}
				if _, err := os.Stat(filepath.Join(conversation, path)); !os.IsNotExist(err) {
					t.Fatalf("the stopped work appeared in the person's checkout: %s, %v", path, err)
				}
			}
			if after := gitOut(t, conversation, "rev-parse", "HEAD"); after != before {
				t.Fatalf("stopping moved the person's checkout from %s to %s", before, after)
			}
		})
	}
}

// A stop uses the same history rule as a landing: commits that cancelled
// their file changes still leave work on a branch the person can inspect.
func TestAStoppedRunNamesCommittedThenRevertedWork(t *testing.T) {
	agent, double, conversation, dir := stoppableBeltRun(t, 71)
	double.mu.Lock()
	workspace := double.spec.Workspace
	double.mu.Unlock()
	mustGit(t, workspace, "add", "half.txt")
	mustGit(t, workspace, "-c", "user.name=Worker", "-c", "user.email=worker@example.test", "commit", "-m", "first FAQ item")
	mustGit(t, workspace, "-c", "user.name=Worker", "-c", "user.email=worker@example.test", "revert", "--no-edit", "HEAD")
	if _, err := agent.Cancel(CancelTask + ":71"); err != nil {
		t.Fatal(err)
	}
	beltRunWaitFor(t, "the stopped run to settle", func() bool {
		agent.beltMu.Lock()
		defer agent.beltMu.Unlock()
		return agent.beltRun == nil
	})
	rows := agent.graph().runRows(71)
	if len(rows) != 1 || rows[0].Branch == "" || !reflect.DeepEqual(rows[0].Changed, []string{"half.txt"}) {
		t.Fatalf("a stopped run hid its committed then reverted work: %+v", rows)
	}
	if said := strings.Join(beltRunNotes(t, dir, "71"), "\n"); strings.Contains(said, "changed nothing") || !strings.Contains(said, "kept on "+rows[0].Branch) {
		t.Fatalf("the stopped page hides the retained history:\n%s", said)
	}
	if commits := strings.Fields(gitOut(t, conversation, "rev-list", "HEAD.."+rows[0].Branch)); len(commits) != 2 {
		t.Fatalf("the kept branch lost the worker's commits: %v", commits)
	}
}
