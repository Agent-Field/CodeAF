package session

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/provider"
	"github.com/Agent-Field/codeaf/internal/standing"
)

// #1550 THROUGH THE REAL PASS. An order the person approved with the separate
// worktree shown on its card is written to a real store on disk, found due by
// the real [standing.Ticker], and fired through the real [standingRunner] into
// a real session whose bash tool runs real git against a real repository. The
// only thing scripted is the model's two replies, and the second one says what
// the #1550 firing said: "committed on branch vet-fix, not merged".
//
// THE LAW IT PINS: while the card says the work is kept on a separate branch,
// no commit reaches the host branch, the host checkout does not change branch,
// and its working tree is left clean.
//
// The order is written as the JSON the store keeps, so this file compiles on a
// tree whose Action has no Isolate field — there the field is dropped, the
// firing runs in the host checkout, and the host branch moves, which is #1550.
func TestStandingIsolatedFiringThroughTheRealPassLeavesTheHostBranchAlone(t *testing.T) {
	commit := `git -c user.name=t -c user.email=t@t commit`
	cases := []struct {
		name    string
		command string
	}{
		{"commits where it stands", commit + ` --allow-empty -m "fix the vet warning"`},
		{"edits a tracked file and commits all", `printf 'fixed\n' >> shared.txt && ` + commit + ` -am "fix the vet warning"`},
		{"cuts vet-fix as the order asked", `git checkout -b vet-fix && printf 'fixed\n' > vet.txt && git add vet.txt && ` + commit + ` -m "fix the vet warning"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := newTestRepo(t)
			hostBranch := currentBranch(repo)
			hostHead := branchCommit(repo, hostBranch)
			if hostBranch == "" || hostHead == "" {
				t.Fatalf("test repository has no branch: %q %q", hostBranch, hostHead)
			}

			store, err := standing.Open(filepath.Join(t.TempDir(), "standing"))
			if err != nil {
				t.Fatal(err)
			}
			raw := fmt.Sprintf(`{"words":"every night fix the vet warnings on a branch called vet-fix and never merge it","workspace":%q,`+
				`"when":{"kind":"every","words":"every night","every":"0 2 * * *"},`+
				`"does":{"kind":"task","brief":"Fix the vet warnings. Commit them to a branch called vet-fix; do not merge.","isolate":true},`+
				`"rails":{"perRunUsd":0.5,"maxPerDay":1}}`, repo)
			var item standing.Item
			if err := json.Unmarshal([]byte(raw), &item); err != nil {
				t.Fatal(err)
			}
			created, err := store.Create(item)
			if err != nil {
				t.Fatal(err)
			}

			completer := &scriptedCompleter{steps: []step{
				func(context.Context, []ai.Message) (*ai.Response, error) {
					args, _ := json.Marshal(map[string]string{"command": tc.command})
					return toolResponse("c1", "bash", string(args)), nil
				},
				func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
					text := "committed on branch vet-fix, not merged"
					provider.Emit(ctx, provider.StreamDelta, text)
					return textResponse(text), nil
				},
			}}
			due := created.NextDue.Add(time.Minute)
			ticker := &standing.Ticker{
				Store:  store,
				Runner: standingChildRunner(t, store.Root(), completer),
				Now:    func() time.Time { return due },
			}
			pass, err := ticker.Tick(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if pass.Fired != 1 {
				t.Fatalf("the order did not fire: %+v", pass)
			}

			if got := branchCommit(repo, hostBranch); got != hostHead {
				log, _ := git(repo, "log", "--oneline", "-3", hostBranch)
				t.Fatalf("a commit landed on the host branch %s while the card said separate worktree: %s -> %s\n%s", hostBranch, hostHead, got, log)
			}
			if got := currentBranch(repo); got != hostBranch {
				t.Fatalf("the host checkout moved from %s to %q", hostBranch, got)
			}
			if status, _ := git(repo, "status", "--porcelain"); strings.TrimSpace(status) != "" {
				t.Fatalf("the host checkout was left dirty:\n%s", status)
			}

			after, err := store.Get(created.ID)
			if err != nil {
				t.Fatal(err)
			}
			refs, _ := git(repo, "for-each-ref", "--format=%(refname:short) %(objectname:short) %(subject)", "refs/heads/")
			t.Logf("outcome %q; previous %q\nrefs:\n%s", after.LastOutcome, after.Previous, refs)
			for _, seen := range completer.seen {
				for _, m := range seen {
					if m.Role == "tool" {
						t.Logf("tool said: %.300s", fmt.Sprint(m.Content))
					}
				}
			}
		})
	}
}

// THE GAP #1560 STATES AND DOES NOT CLOSE: the separate worktree is where the
// firing STANDS, not a sandbox, so a shell command that names the host path —
// which a self-contained brief naming the project's absolute path invites —
// still commits onto the host branch, and nothing after the run notices. This
// is expected to fail on #1560's head; it is the probe for a follow-up.
func TestStandingIsolatedFiringWhoseShellNamesTheHostPathLeavesTheHostBranchAlone(t *testing.T) {
	repo := newTestRepo(t)
	hostBranch := currentBranch(repo)
	hostHead := branchCommit(repo, hostBranch)
	store, err := standing.Open(filepath.Join(t.TempDir(), "standing"))
	if err != nil {
		t.Fatal(err)
	}
	raw := fmt.Sprintf(`{"words":"every night fix the vet warnings on a branch and never merge","workspace":%q,`+
		`"when":{"kind":"every","words":"every night","every":"0 2 * * *"},`+
		`"does":{"kind":"task","brief":"In %s fix the vet warnings and commit them on a branch; do not merge.","isolate":true},`+
		`"rails":{"perRunUsd":0.5,"maxPerDay":1}}`, repo, repo)
	var item standing.Item
	if err := json.Unmarshal([]byte(raw), &item); err != nil {
		t.Fatal(err)
	}
	created, err := store.Create(item)
	if err != nil {
		t.Fatal(err)
	}
	command := fmt.Sprintf(`cd %q && git -c user.name=t -c user.email=t@t commit --allow-empty -m "fix the vet warning"`, repo)
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			args, _ := json.Marshal(map[string]string{"command": command})
			return toolResponse("c1", "bash", string(args)), nil
		},
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			text := "committed on a branch, not merged"
			provider.Emit(ctx, provider.StreamDelta, text)
			return textResponse(text), nil
		},
	}}
	due := created.NextDue.Add(time.Minute)
	ticker := &standing.Ticker{Store: store, Runner: standingChildRunner(t, store.Root(), completer), Now: func() time.Time { return due }}
	if pass, err := ticker.Tick(context.Background()); err != nil || pass.Fired != 1 {
		t.Fatalf("the order did not fire: %+v %v", pass, err)
	}
	after, _ := store.Get(created.ID)
	refs, _ := git(repo, "for-each-ref", "--format=%(refname:short) %(objectname:short) %(subject)", "refs/heads/")
	t.Logf("outcome %q; previous %q\nrefs:\n%s", after.LastOutcome, after.Previous, refs)
	for _, seen := range completer.seen {
		for _, m := range seen {
			if m.Role == "tool" {
				t.Logf("tool said: %.400s", fmt.Sprint(m.Content))
			}
		}
	}
	if got := branchCommit(repo, hostBranch); got != hostHead {
		t.Fatalf("a commit landed on the host branch %s while the card said separate worktree (outcome %q, previous %q)", hostBranch, after.LastOutcome, after.Previous)
	}
}

// THE SHAPES A SHELL CAN TAKE AT THE HOST, each fired through the real pass
// against its own repository. Every row is a way a worker could reach the host
// checkout from inside the separate worktree; the task guard ([InTask]) is what
// is being asked about, and each row reports on its own.
func TestStandingIsolatedFiringShellShapesAgainstTheHost(t *testing.T) {
	commit := `git -c user.name=t -c user.email=t@t commit --allow-empty -m "fix the vet warning"`
	rows := []struct{ name, command string }{
		{"git -C host commit", `git -C HOST -c user.name=t -c user.email=t@t commit --allow-empty -m x`},
		{"shell redirect into host", `printf leaked > HOST/host-leak.txt`},
		{"cd host then redirect", `cd HOST && printf leaked > host-leak.txt`},
		{"switch -c vet-fix in the copy", `git switch -c vet-fix && ` + commit},
		{"update-ref the host branch", commit + ` && git update-ref refs/heads/work HEAD`},
		{"push . HEAD:work", commit + ` && git push . HEAD:work`},
		{"branch -f work", commit + ` && git branch -f work HEAD`},
		{"host merges the copy", commit + ` && git -C HOST merge --ff-only "$(git rev-parse HEAD)"`},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			repo := newTestRepo(t)
			hostBranch := currentBranch(repo)
			hostHead := branchCommit(repo, hostBranch)
			store, err := standing.Open(filepath.Join(t.TempDir(), "standing"))
			if err != nil {
				t.Fatal(err)
			}
			raw := fmt.Sprintf(`{"words":"every night fix the vet warnings on a branch and never merge","workspace":%q,`+
				`"when":{"kind":"every","words":"every night","every":"0 2 * * *"},`+
				`"does":{"kind":"task","brief":"Fix the vet warnings on a branch; do not merge.","isolate":true},`+
				`"rails":{"perRunUsd":0.5,"maxPerDay":1}}`, repo)
			var item standing.Item
			if err := json.Unmarshal([]byte(raw), &item); err != nil {
				t.Fatal(err)
			}
			created, err := store.Create(item)
			if err != nil {
				t.Fatal(err)
			}
			command := strings.ReplaceAll(row.command, "HOST", repo)
			completer := &scriptedCompleter{steps: []step{
				func(context.Context, []ai.Message) (*ai.Response, error) {
					args, _ := json.Marshal(map[string]string{"command": command})
					return toolResponse("c1", "bash", string(args)), nil
				},
				func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
					text := "committed on branch vet-fix, not merged"
					provider.Emit(ctx, provider.StreamDelta, text)
					return textResponse(text), nil
				},
			}}
			due := created.NextDue.Add(time.Minute)
			ticker := &standing.Ticker{Store: store, Runner: standingChildRunner(t, store.Root(), completer), Now: func() time.Time { return due }}
			if pass, err := ticker.Tick(context.Background()); err != nil || pass.Fired != 1 {
				t.Fatalf("the order did not fire: %+v %v", pass, err)
			}
			after, _ := store.Get(created.ID)
			said := ""
			for _, seen := range completer.seen {
				for _, m := range seen {
					if m.Role == "tool" {
						said = fmt.Sprint(m.Content)
					}
				}
			}
			refs, _ := git(repo, "for-each-ref", "--format=%(refname:short) %(objectname:short)", "refs/heads/")
			status, _ := git(repo, "status", "--porcelain")
			moved := branchCommit(repo, hostBranch) != hostHead || currentBranch(repo) != hostBranch || strings.TrimSpace(status) != ""
			t.Logf("HOST-REACHED=%v outcome=%q previous=%q\nrefs: %s\nstatus: %q\ntool: %.260s", moved, after.LastOutcome, after.Previous, strings.ReplaceAll(strings.TrimSpace(refs), "\n", " | "), status, said)
			if moved {
				t.Errorf("the host checkout was reached")
			}
		})
	}
}

// A PROJECT OPENED FROM A SUBFOLDER OF ITS REPOSITORY, and a shell that hides
// its target in a script. Diagnostic rows for the review; each logs where the
// work went.
func TestStandingIsolatedFiringSubfolderAndScriptProbes(t *testing.T) {
	rows := []struct{ name, sub, tool, args string }{
		{"subfolder relative bash write", "sub", "bash", `{"command":"pwd && printf rel > rel.txt && git add rel.txt && git -c user.name=t -c user.email=t@t commit -m rel"}`},
		{"subfolder write tool at the host path", "sub", "write", `{"path":"HOST/sub/w.txt","content":"w\n"}`},
		{"script hides the host", "", "bash", `{"command":"printf 'cd HOST && git -c user.name=t -c user.email=t@t commit --allow-empty -m hidden\\n' > s.sh && sh s.sh"}`},
		{"bash -c hides the host", "", "bash", `{"command":"bash -c \"cd HOST && printf leaked > host-leak.txt\""}`},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			repo := newTestRepo(t)
			writeFile(t, filepath.Join(repo, "sub", "keep.txt"), "k\n")
			mustGit(t, repo, "add", "-A")
			mustGit(t, repo, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-m", "sub")
			hostBranch := currentBranch(repo)
			hostHead := branchCommit(repo, hostBranch)
			workspace := repo
			if row.sub != "" {
				workspace = filepath.Join(repo, row.sub)
			}
			store, err := standing.Open(filepath.Join(t.TempDir(), "standing"))
			if err != nil {
				t.Fatal(err)
			}
			raw := fmt.Sprintf(`{"words":"every night tidy on a branch","workspace":%q,`+
				`"when":{"kind":"every","words":"every night","every":"0 2 * * *"},`+
				`"does":{"kind":"task","brief":"Tidy up on a branch; do not merge.","isolate":true},`+
				`"rails":{"perRunUsd":0.5,"maxPerDay":1}}`, workspace)
			var item standing.Item
			if err := json.Unmarshal([]byte(raw), &item); err != nil {
				t.Fatal(err)
			}
			created, err := store.Create(item)
			if err != nil {
				t.Fatal(err)
			}
			args := strings.ReplaceAll(row.args, "HOST", repo)
			completer := &scriptedCompleter{steps: []step{
				func(context.Context, []ai.Message) (*ai.Response, error) {
					return toolResponse("c1", row.tool, args), nil
				},
				func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
					provider.Emit(ctx, provider.StreamDelta, "done")
					return textResponse("done"), nil
				},
			}}
			due := created.NextDue.Add(time.Minute)
			ticker := &standing.Ticker{Store: store, Runner: standingChildRunner(t, store.Root(), completer), Now: func() time.Time { return due }}
			if pass, err := ticker.Tick(context.Background()); err != nil || pass.Fired != 1 {
				t.Fatalf("the order did not fire: %+v %v", pass, err)
			}
			after, _ := store.Get(created.ID)
			said := ""
			for _, seen := range completer.seen {
				for _, m := range seen {
					if m.Role == "tool" {
						said = fmt.Sprint(m.Content)
					}
				}
			}
			status, _ := git(repo, "status", "--porcelain")
			moved := branchCommit(repo, hostBranch) != hostHead || currentBranch(repo) != hostBranch || strings.TrimSpace(status) != ""
			trees, _ := git(repo, "worktree", "list", "--porcelain")
			files := ""
			for _, line := range strings.Split(trees, "\n") {
				if dir, ok := strings.CutPrefix(line, "worktree "); ok && dir != repo {
					out, _ := git(dir, "status", "--porcelain", "--untracked-files=all")
					logs, _ := git(dir, "log", "--name-only", "--format=%s", "-1")
					files += dir + " status=" + strings.TrimSpace(out) + " last=" + strings.ReplaceAll(strings.TrimSpace(logs), "\n", ",")
				}
			}
			t.Logf("HOST-REACHED=%v outcome=%q\ntool: %.300s\ncopy: %s\nlast text: %q", moved, after.LastOutcome, said, files, after.Previous)
		})
	}
}
