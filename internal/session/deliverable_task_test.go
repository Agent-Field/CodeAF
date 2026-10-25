package session

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/exec/bare"
)

// taskWorld is a conversation with a task node, a worktree directory apart from
// its checkout, and a worker agent built the way the conversation builds one.
type taskWorld struct {
	owner     *Agent
	index     string
	checkout  string
	worktree  string
	node      *TaskNode
	sink      *deliverableSink
	write     bare.Tool
	sessionID string
}

func newTaskWorld(t *testing.T, branch string) *taskWorld {
	t.Helper()
	owner, checkout, index := deliverableAgent(t, false)
	checkout = canonicalFile(checkout)
	// A real repository with a real worktree cut from its HEAD, the way a
	// task's tree is made; the node records the commit it was cut from.
	gitT(t, checkout, "init", "-q", "-b", "main")
	os.WriteFile(filepath.Join(checkout, "README.md"), []byte("readme\n"), 0o644)
	os.WriteFile(filepath.Join(checkout, "old.md"), []byte("old\n"), 0o644)
	gitT(t, checkout, "add", ".")
	gitT(t, checkout, "commit", "-q", "-m", "base")
	base := strings.TrimSpace(gitOut(t, checkout, "rev-parse", "HEAD"))
	worktree := filepath.Join(canonicalFile(t.TempDir()), "tree")
	gitT(t, checkout, "worktree", "add", "-q", "-b", branch, worktree)
	graph := stubbedGraph(owner, func(n *TaskNode) { n.finish("done", nil, "", ""); n.graph.complete(n, TaskDone) })
	id := graph.reserve()
	graph.admit(id, taskSpec{title: "Write the plan", named: true, brief: "b", acceptance: "a",
		source: &TaskSource{ID: "scope#3", Kind: TaskSourceMessage}})
	node := graph.node(id)
	waitDoneNode(t, node)
	node.setTree(taskTree{dir: worktree, branch: branch, homeSha: base})
	owner.mu.Lock()
	sink := owner.deliverSinkLocked(worktree, node)
	owner.mu.Unlock()
	if sink == nil {
		t.Fatal("the owning conversation made no sink")
	}
	node.setDeliverSink(sink)
	worker, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
		c.Workspace = worktree
		c.InTask = true
		c.deliverSink = sink
	})
	var write bare.Tool
	for _, tool := range bare.AllToolsCapped(worktree, worker.resultCaps()) {
		if tool.Name == "write" {
			write = worker.deliverableWrite(tool)
		}
	}
	return &taskWorld{owner: owner, index: index, checkout: checkout, worktree: worktree, node: node, sink: sink, write: write, sessionID: owner.journalID()}
}

// land commits the worktree's work on its branch and merges it into the
// checkout the way a landing does.
func (w *taskWorld) land(t *testing.T, branch string) {
	t.Helper()
	gitT(t, w.worktree, "add", "-A")
	gitT(t, w.worktree, "commit", "-q", "-m", "work")
	gitT(t, w.checkout, "merge", "-q", "--no-edit", branch)
}

func (w *taskWorld) byTitle(t *testing.T) map[string]DeliverableRow {
	t.Helper()
	out := map[string]DeliverableRow{}
	for _, row := range listDeliverables(t, w.index, DeliverableQuery{Sessions: map[string]bool{w.sessionID: true}}, 100) {
		out[row.Artifact.Title] = row
	}
	return out
}

func (w *taskWorld) writeFile(t *testing.T, rel, body string) {
	t.Helper()
	args := []byte(`{"path":"` + rel + `","content":"` + body + `"}`)
	if out, bad, err := w.write.Execute(withRunIdentity(context.Background()), args); err != nil || bad {
		t.Fatalf("write: %v %s", err, out)
	}
}

// A worker's new document is recorded as the CONVERSATION's, with the node's
// facts, and announced on the conversation; nothing else is.
func TestATaskWorkersNewDocumentIsRecordedUnderItsOwner(t *testing.T) {
	w := newTaskWorld(t, "task/write-the-plan-abc123")
	// The task lane is the door that outlives any one turn, so a turn the
	// landing woke cannot take the announcement from under it.
	lane, stop := w.owner.WatchTaskUpdates()
	defer stop()
	w.writeFile(t, "docs/plan.md", "# plan\\n")
	w.writeFile(t, "docs/plan.md", "# plan edited\\n") // an overwrite
	w.writeFile(t, "main.go", "package main\\n")       // another kind
	rows := listDeliverables(t, w.index, DeliverableQuery{Sessions: map[string]bool{w.sessionID: true}}, 10)
	said := laneArtifacts(t, lane, 3)
	if len(rows) != 2 || len(said) != 3 || said[0].ID != said[1].ID {
		t.Fatalf("rows=%d events=%d", len(rows), len(said))
	}
	row := rows[1]
	f := row.Fact
	if f.Task == "" || f.Turn != "scope#3" || f.Run == "" || f.Branch != "task/write-the-plan-abc123" || f.Rel != "docs/plan.md" ||
		f.State != "" || f.Kept != nil || !f.Authored || said[0].ID != row.ID {
		t.Fatalf("fact %+v", f)
	}
	if want := filepath.Join(canonicalFile(w.checkout), "docs", "plan.md"); row.Artifact.Path != want {
		t.Fatalf("path %q want the checkout path %q", row.Artifact.Path, want)
	}
	if f.File == nil || f.File.Canonical != "" || f.File.SHA256 == "" || f.Version != "sha256:"+f.File.SHA256 {
		t.Fatalf("worktree identity leaked into the row: %+v", f.File)
	}
	// nothing was written under the worker's own journal id
	if all := ReadArtifacts(w.index); len(all) != 3 || all[0].Session != w.sessionID {
		t.Fatalf("rows under the wrong session: %+v", all)
	}
	// a path outside the worker's directory is not the worker's to claim
	outside := filepath.Join(t.TempDir(), "x.md")
	args := []byte(`{"path":"` + outside + `","content":"x"}`)
	w.write.Execute(context.Background(), args)
	if len(listDeliverables(t, w.index, DeliverableQuery{}, 10)) != 2 {
		t.Fatal("a file outside the worktree was claimed")
	}
}

// A worker with no sink - one nobody owns - records nothing, however it writes.
func TestAWorkerWithoutAnOwnerRecordsNothing(t *testing.T) {
	owner, _, index := deliverableAgent(t, false)
	dir := t.TempDir()
	worker, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) { c.Workspace = dir; c.InTask = true; c.ArtifactsIndex = index })
	for _, tool := range bare.AllToolsCapped(dir, worker.resultCaps()) {
		if tool.Name == "write" {
			wrapped := worker.deliverableWrite(tool)
			wrapped.Execute(context.Background(), []byte(`{"path":"a.md","content":"x"}`))
		}
	}
	if rows := ReadArtifacts(index); len(rows) != 0 {
		t.Fatalf("an unowned worker recorded: %+v (owner %s)", rows, owner.journalID())
	}
}

// A merged landing keeps exactly the files whose authored bytes arrived
// through it, and nothing else that merely exists at the same path.
func TestLandingKeepsOnlyTheExactAuthoredBytes(t *testing.T) {
	const branch = "task/write-the-plan-abc123"
	w := newTaskWorld(t, branch)
	w.writeFile(t, "arrived.md", "# a\\n")
	w.writeFile(t, "docs/nested.md", "# n\\n")
	w.writeFile(t, "edited.md", "# e\\n")
	w.writeFile(t, "edited.md", "# e changed\\n") // latest authored bytes
	w.writeFile(t, "lost.md", "# l\\n")
	os.Remove(filepath.Join(w.worktree, "lost.md")) // never committed: never arrives
	os.Remove(filepath.Join(w.worktree, "old.md"))  // a file that was there at the cut ...
	w.writeFile(t, "old.md", "# replaced\\n")       // ... made again: a replacement, not new
	w.writeFile(t, "later.md", "# later\\n")
	w.writeFile(t, "linked.md", "# linked\\n")
	w.writeFile(t, "gone.md", "# gone\\n")
	if n := w.node.settleDeliverablesIn(context.Background(), mergeKept); n != 0 {
		t.Fatalf("a branch kept, not merged, settled %d", n)
	}
	w.land(t, branch)
	// After the merge the person changes one, deletes one, and puts a link with
	// the very same bytes in place of another.
	os.WriteFile(filepath.Join(w.checkout, "later.md"), []byte("# theirs\n"), 0o644)
	os.Remove(filepath.Join(w.checkout, "gone.md"))
	same := filepath.Join(t.TempDir(), "same.md")
	os.WriteFile(same, []byte("# linked\n"), 0o644)
	os.Remove(filepath.Join(w.checkout, "linked.md"))
	os.Symlink(same, filepath.Join(w.checkout, "linked.md"))
	if n := w.node.settleDeliverablesIn(context.Background(), mergeMerged); n != 3 {
		t.Fatalf("settled %d, want 3", n)
	}
	rows := w.byTitle(t)
	for _, title := range []string{"arrived.md", "nested.md", "edited.md"} {
		f := rows[title].Fact
		want := filepath.Join(w.checkout, filepath.FromSlash(f.Rel))
		if f.State != DeliverKept || f.Kept == nil || f.Kept.How != KeptCreatedInRepo || f.Kept.Path != want ||
			f.File == nil || f.File.Canonical != want || f.File.Ino == 0 || f.Turn != "scope#3" || f.Run == "" {
			t.Fatalf("%s: %+v", title, f)
		}
		if live := ResolveDeliverable(rows[title], []string{w.checkout}); live.State != MediaAvailable {
			t.Fatalf("%s: a settled file is %s", title, live.State)
		}
	}
	for _, title := range []string{"lost.md", "old.md", "later.md", "linked.md", "gone.md"} {
		if f := rows[title].Fact; f.State != "" || f.Kept != nil {
			t.Fatalf("%s was claimed without proof: %+v", title, f)
		}
	}
	if n := w.node.settleDeliverablesIn(context.Background(), mergeMerged); n != 0 {
		t.Fatalf("settling again settled %d more", n)
	}
}

// A landing that has run out of time settles nothing and claims nothing; the
// rows stay unknown, and a later settling finishes them all, past any count
// the old in-memory list was capped at.
func TestSettlingIsBoundedAndResumable(t *testing.T) {
	const branch = "task/many-docs-abc123"
	w := newTaskWorld(t, branch)
	const files = 300
	for i := 0; i < files; i++ {
		w.writeFile(t, "docs/d"+strconv.Itoa(i)+".md", "# "+strconv.Itoa(i))
	}
	w.land(t, branch)
	gone, cancel := context.WithCancel(context.Background())
	cancel()
	if n := w.node.settleDeliverablesIn(gone, mergeMerged); n != 0 {
		t.Fatalf("a cancelled settling settled %d", n)
	}
	for _, row := range w.byTitleAll(t) {
		if row.Fact.State != "" {
			t.Fatalf("a cancelled settling claimed %s", row.Artifact.Title)
		}
	}
	started := time.Now()
	ctx, stop := context.WithTimeout(context.Background(), time.Minute)
	defer stop()
	if n := w.node.settleDeliverablesIn(ctx, mergeMerged); n != files {
		t.Fatalf("settled %d of %d", n, files)
	}
	t.Logf("settled %d rows in %v", files, time.Since(started))
	for _, row := range w.byTitleAll(t) {
		if row.Fact.State != DeliverKept {
			t.Fatalf("%s left unsettled", row.Artifact.Title)
		}
	}
}

func (w *taskWorld) byTitleAll(t *testing.T) []DeliverableRow {
	t.Helper()
	return listDeliverables(t, w.index, DeliverableQuery{Sessions: map[string]bool{w.sessionID: true}}, 1000)
}

// A worker standing in the checkout creates in the REPOSITORY only when git
// says so: untracked and not ignored in a real repository. A folder that is no
// repository, and a file git ignores, are new files of unknown state.
func TestAWorkerInTheCheckoutCreatesInTheRepoOnlyWhenGitSaysSo(t *testing.T) {
	inPlace := func(t *testing.T, repo bool, merge string) (*Agent, string, string, bare.Tool) {
		owner, checkout, index := deliverableAgent(t, false)
		checkout = canonicalFile(checkout)
		if repo {
			gitT(t, checkout, "init", "-q", "-b", "main")
			os.WriteFile(filepath.Join(checkout, ".gitignore"), []byte("ignored/\n"), 0o644)
			gitT(t, checkout, "add", ".")
			gitT(t, checkout, "commit", "-q", "-m", "base")
		}
		graph := stubbedGraph(owner, func(n *TaskNode) { n.finish("done", nil, "", ""); n.graph.complete(n, TaskDone) })
		id := graph.reserve()
		graph.admit(id, taskSpec{title: "In place", named: true, brief: "b", acceptance: "a"})
		node := graph.node(id)
		waitDoneNode(t, node)
		node.setTree(taskTree{dir: checkout, merge: merge})
		owner.mu.Lock()
		sink := owner.deliverSinkLocked(checkout, node)
		owner.mu.Unlock()
		worker, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) { c.Workspace = checkout; c.InTask = true; c.deliverSink = sink })
		var write bare.Tool
		for _, tool := range bare.AllToolsCapped(checkout, worker.resultCaps()) {
			if tool.Name == "write" {
				write = worker.deliverableWrite(tool)
			}
		}
		return owner, checkout, index, write
	}
	states := func(t *testing.T, index string) map[string]DeliverableFact {
		out := map[string]DeliverableFact{}
		for _, row := range listDeliverables(t, index, DeliverableQuery{}, 10) {
			out[row.Artifact.Title] = row.Fact
		}
		return out
	}
	// a real repository
	_, _, index, write := inPlace(t, true, "")
	write.Execute(withRunIdentity(context.Background()), []byte(`{"path":"notes.md","content":"x"}`))
	write.Execute(withRunIdentity(context.Background()), []byte(`{"path":"ignored/scratch.md","content":"x"}`))
	got := states(t, index)
	if f := got["notes.md"]; f.State != DeliverKept || f.Kept == nil || f.Kept.How != KeptCreatedInRepo || f.Task == "" || f.Branch != "" {
		t.Fatalf("a new file in a repository: %+v", f)
	}
	if f := got["scratch.md"]; f.State != "" || f.Kept != nil {
		t.Fatalf("an ignored file was called created in the repository: %+v", f)
	}
	// a folder that is not a repository, as the in-place tree has
	_, _, index, write = inPlace(t, false, mergeInPlace)
	write.Execute(withRunIdentity(context.Background()), []byte(`{"path":"notes.md","content":"x"}`))
	if f := states(t, index)["notes.md"]; f.State != "" || f.Kept != nil || f.Task == "" || !f.Authored {
		t.Fatalf("a new file in a folder that is no repository: %+v", f)
	}
}

// A file that exists only on an unmerged task branch is real, readable from git
// bounded, and stops being "on the branch" the moment the branch is merged.
func TestABranchOnlyFileIsReadFromGit(t *testing.T) {
	repo := t.TempDir()
	gitT(t, repo, "init", "-q", "-b", "main")
	os.WriteFile(filepath.Join(repo, "a.txt"), []byte("a\n"), 0o644)
	gitT(t, repo, "add", ".")
	gitT(t, repo, "commit", "-q", "-m", "base")
	gitT(t, repo, "checkout", "-q", "-b", "task/doc-aaaaaa")
	os.WriteFile(filepath.Join(repo, "plan.md"), []byte("# plan\n"+strings.Repeat("x", 100)), 0o644)
	gitT(t, repo, "add", ".")
	gitT(t, repo, "commit", "-q", "-m", "work")
	gitT(t, repo, "checkout", "-q", "main")
	if !BranchFileOnUnmergedBranch(repo, "task/doc-aaaaaa", "plan.md") {
		t.Fatal("the branch file was not seen")
	}
	if BranchFileOnUnmergedBranch(repo, "task/doc-aaaaaa", "nope.md") || BranchFileOnUnmergedBranch(repo, "main", "a.txt") || BranchFileOnUnmergedBranch(repo, "task/doc-aaaaaa", "../a.txt") {
		t.Fatal("an unrelated file or branch was vouched for")
	}
	data, truncated, version, err := ReadBranchBlob(repo, "task/doc-aaaaaa", "plan.md", 10)
	if err != nil || !truncated || string(data) != "# plan\nxxx" || !strings.HasPrefix(version, "git:") {
		t.Fatalf("%q %v %q %v", data, truncated, version, err)
	}
	gitT(t, repo, "merge", "-q", "--no-edit", "task/doc-aaaaaa")
	if BranchFileOnUnmergedBranch(repo, "task/doc-aaaaaa", "plan.md") {
		t.Fatal("a merged branch is still 'on the branch'")
	}
	_ = time.Second
}

// A branch read reads only a REGULAR FILE at exactly the path named: a link on
// the branch (even to a real file), a folder, a submodule and a pattern are
// not files there, and a path that would have to be resolved is refused.
func TestABranchReadIsOfARegularFileAtExactlyThatPath(t *testing.T) {
	repo := t.TempDir()
	gitT(t, repo, "init", "-q", "-b", "main")
	os.WriteFile(filepath.Join(repo, "a.txt"), []byte("secret\n"), 0o644)
	gitT(t, repo, "add", ".")
	gitT(t, repo, "commit", "-q", "-m", "base")
	gitT(t, repo, "checkout", "-q", "-b", "task/doc-bbbbbb")
	os.WriteFile(filepath.Join(repo, "plan.md"), []byte("# plan\n"), 0o644)
	os.Symlink("a.txt", filepath.Join(repo, "link.md"))
	os.MkdirAll(filepath.Join(repo, "dir.md"), 0o755)
	os.WriteFile(filepath.Join(repo, "dir.md", "in.md"), []byte("in\n"), 0o644)
	gitT(t, repo, "add", ".")
	sub := strings.TrimSpace(gitOut(t, repo, "rev-parse", "HEAD"))
	gitT(t, repo, "update-index", "--add", "--cacheinfo", "160000,"+sub+",mod.md")
	gitT(t, repo, "commit", "-q", "-m", "work")
	gitT(t, repo, "checkout", "-q", "main")
	const branch = "task/doc-bbbbbb"
	if data, _, version, err := ReadBranchBlob(repo, branch, "plan.md", 100); err != nil || string(data) != "# plan\n" || version != BranchBlobVersion(repo, branch, "plan.md") {
		t.Fatalf("the regular file: %q %q %v", data, version, err)
	}
	if data, _, _, err := ReadBranchBlob(repo, branch, "dir.md/in.md", 100); err != nil || string(data) != "in\n" {
		t.Fatalf("a nested regular file: %q %v", data, err)
	}
	for _, rel := range []string{"link.md", "dir.md", "mod.md", "*.md", "pl?n.md", "./plan.md", "dir.md/../plan.md", "dir.md//in.md", "/plan.md", "-p", ""} {
		if BranchFileOnUnmergedBranch(repo, branch, rel) || BranchBlobVersion(repo, branch, rel) != "" {
			t.Fatalf("%q was vouched for as a file on the branch", rel)
		}
		if data, _, _, err := ReadBranchBlob(repo, branch, rel, 100); err == nil {
			t.Fatalf("%q was read: %q", rel, data)
		}
	}
}

// laneArtifacts reads the artifact announcements on a task lane until want of
// them have come (or five seconds pass), then takes any more already waiting.
func laneArtifacts(t *testing.T, lane <-chan Event, want int) []ArtifactChange {
	t.Helper()
	var said []ArtifactChange
	expire := time.After(5 * time.Second)
	for len(said) < want {
		select {
		case event := <-lane:
			if event.Kind == EventArtifactUpdate && event.Artifact != nil {
				said = append(said, *event.Artifact)
			}
		case <-expire:
			return said
		}
	}
	for {
		select {
		case event := <-lane:
			if event.Kind == EventArtifactUpdate && event.Artifact != nil {
				said = append(said, *event.Artifact)
			}
		case <-time.After(50 * time.Millisecond):
			return said
		}
	}
}
