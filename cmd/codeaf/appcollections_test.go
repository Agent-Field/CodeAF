package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
)

// collectionsBackend is a work backend whose collections store is a path in
// a fresh folder, not yet made.
func collectionsBackend(t *testing.T) (*appBackend, *appRecorder, appwire.Agent, string) {
	t.Helper()
	backend, rec, agent := workBackend(t, &fakeConversation{})
	backend.collectionsPath = filepath.Join(t.TempDir(), "v3", "collections.db")
	return backend, rec, agent, backend.collectionsPath
}

// changedEvents is every collection.changed the recorder has seen.
func changedEvents(rec *appRecorder) []appwire.CollectionChanged {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	var out []appwire.CollectionChanged
	for _, d := range rec.data {
		if c, ok := d.(appwire.CollectionChanged); ok {
			out = append(out, c)
		}
	}
	return out
}

// A READ OF A FRESH HOME CREATES NOTHING, and a change to a collection that
// cannot exist yet is not_found without making the file either.
func TestAppCollectionsReadsDoNotCreateTheStore(t *testing.T) {
	backend, _, _, path := collectionsBackend(t)
	list, err := backend.ListCollections(nil)
	if err != nil || list == nil || len(list) != 0 {
		t.Fatalf("fresh list %v, %v", list, err)
	}
	if _, err := backend.RenameCollection("nope", "x"); wireCode(err) != appwire.CodeNotFound {
		t.Fatalf("rename in a fresh home: %v", err)
	}
	if _, err := backend.OpenCollection("nope", 1, 500); wireCode(err) != appwire.CodeNotFound {
		t.Fatalf("open in a fresh home: %v", err)
	}
	if _, err := os.Stat(filepath.Dir(path)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a read made the store's folder: %v", err)
	}
}

// writeGitTree makes a work tree with a branch checked out and, when origin
// is given, an origin remote.
func writeGitTree(t *testing.T, root, branch, origin string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".git", "HEAD"), []byte("ref: refs/heads/"+branch+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	config := "[core]\n\tbare = false\n"
	if origin != "" {
		config += "[remote \"upstream\"]\n\turl = git@github.com:someone/else.git\n[remote \"origin\"]\n\turl = " + origin + "\n"
	}
	if err := os.WriteFile(filepath.Join(root, ".git", "config"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
}

// OPENING SAYS WHAT EACH MEMBER IS NOW: a conversation that is there is live
// under its title, one that is gone is deleted, a moved file is missing, a
// file in a git work tree names its repository, branch and path, and a task
// or a process of an agent that is gone is deleted with it.
func TestAppOpenCollectionResolvesEachMember(t *testing.T) {
	backend, rec, agent, _ := collectionsBackend(t)
	repo := filepath.Join(t.TempDir(), "widgets")
	writeGitTree(t, repo, "feature/collections", "git@github.com:acme/widgets.git")
	source := filepath.Join(repo, "sub", "main.go")
	if err := os.MkdirAll(filepath.Dir(source), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	plain := filepath.Join(t.TempDir(), "notes")
	writeGitTree(t, plain, "main", "")
	note := filepath.Join(plain, "todo.md")
	if err := os.WriteFile(note, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	c, err := backend.CreateCollection("Launch")
	if err != nil || c.CreatedAt == nil || c.Count != 0 {
		t.Fatalf("create %+v, %v", c, err)
	}
	nested, _ := backend.CreateCollection("Nested")
	refs := []appwire.Member{
		{Ref: appwire.Ref{Kind: appwire.RefConversation, ID: agent.ID}},
		{Ref: appwire.Ref{Kind: appwire.RefConversation, ID: "0000000000000000"}},
		{Ref: appwire.Ref{Kind: appwire.RefArtifact, ID: filepath.Join(repo, "gone.txt")}},
		{Ref: appwire.Ref{Kind: appwire.RefArtifact, ID: source}},
		{Ref: appwire.Ref{Kind: appwire.RefArtifact, ID: note}},
		{Ref: appwire.Ref{Kind: appwire.RefTask, ID: "4", SessionID: "0000000000000000"}},
		{Ref: appwire.Ref{Kind: appwire.RefTask, ID: "99", SessionID: agent.ID}},
		{Ref: appwire.Ref{Kind: appwire.RefProcess, ID: "0000000000000000:p1", SessionID: "0000000000000000"}},
		{Ref: appwire.Ref{Kind: appwire.RefCollection, ID: nested.ID}},
		{Ref: appwire.Ref{Kind: appwire.RefCrew, ID: "no-such-crew"}},
	}
	added, err := backend.AddToCollection(c.ID, refs)
	if err != nil || len(added.Added) != len(refs) || len(added.Already) != 0 {
		t.Fatalf("add %+v, %v", added, err)
	}
	open, err := backend.OpenCollection(c.ID, 1, 500)
	if err != nil {
		t.Fatal(err)
	}
	if open.Collection.Count != len(refs) || len(open.Members) != len(refs) || open.More {
		t.Fatalf("open %+v", open)
	}
	byID := map[string]appwire.Target{}
	for _, n := range open.Members {
		byID[n.Kind+":"+n.ID] = n.Target
		if n.AddedAt == nil {
			t.Fatalf("%s has no added_at", n.ID)
		}
	}
	check := func(key, state string) appwire.Target {
		t.Helper()
		got := byID[key]
		if got.State != state {
			t.Fatalf("%s is %+v, want %s", key, got, state)
		}
		return got
	}
	if live := check("conversation:"+agent.ID, appwire.TargetLive); live.Title != agent.Title || live.Status == "" {
		t.Fatalf("live conversation %+v (agent %+v)", live, agent)
	}
	check("conversation:0000000000000000", appwire.TargetDeleted)
	check("artifact:"+filepath.Join(repo, "gone.txt"), appwire.TargetMissing)
	if f := check("artifact:"+source, appwire.TargetLive); f.Repo != "acme/widgets" || f.Branch != "feature/collections" || f.Path != "sub/main.go" || f.Title != "main.go" {
		t.Fatalf("file in a repo %+v", f)
	}
	if f := check("artifact:"+note, appwire.TargetLive); f.Repo != "notes" || f.Branch != "main" || f.Path != "todo.md" {
		t.Fatalf("file in a repo with no origin %+v", f)
	}
	check("task:4", appwire.TargetDeleted)
	check("task:99", appwire.TargetDeleted)
	check("process:0000000000000000:p1", appwire.TargetDeleted)
	check("crew:no-such-crew", appwire.TargetDeleted)
	if n := check("collection:"+nested.ID, appwire.TargetLive); n.Title != "Nested" || n.Count == nil || *n.Count != 0 {
		t.Fatalf("nested %+v", n)
	}
	if events := changedEvents(rec); len(events) != 3 || events[2].ID != c.ID {
		t.Fatalf("events %+v", events)
	}

	e, err := backend.ExpandCollection(c.ID, 3, 200)
	if err != nil || e.Missing != 6 || len(e.Refs) != 3 || e.Collections != 1 {
		t.Fatalf("expand %+v, %v", e, err)
	}
}

// THE STORE'S REFUSALS ARRIVE IN THE WIRE'S CODES, and delete says so of the
// collection and of every collection that lost a membership naming it.
func TestAppCollectionsRefusalsAndDeleteEvents(t *testing.T) {
	backend, rec, _, _ := collectionsBackend(t)
	a, _ := backend.CreateCollection("Alpha")
	b, _ := backend.CreateCollection("Beta")
	if _, err := backend.CreateCollection("ALPHA"); wireCode(err) != appwire.CodeConflict {
		t.Fatalf("taken name: %v", err)
	}
	if _, err := backend.RenameCollection(a.ID, "alpha"); err != nil {
		t.Fatalf("own name in another case: %v", err)
	}
	if _, err := backend.AddToCollection(a.ID, []appwire.Member{{Ref: appwire.Ref{Kind: appwire.RefCollection, ID: b.ID}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := backend.AddToCollection(b.ID, []appwire.Member{{Ref: appwire.Ref{Kind: appwire.RefCollection, ID: a.ID}}}); wireCode(err) != appwire.CodeCycle {
		t.Fatalf("cycle: %v", err)
	}
	dup, err := backend.DuplicateCollection(a.ID, "Gamma")
	if err != nil || dup.Count != 1 {
		t.Fatalf("duplicate %+v, %v", dup, err)
	}
	removed, err := backend.RemoveFromCollection(dup.ID, []appwire.Ref{{Kind: appwire.RefCollection, ID: b.ID}})
	if err != nil || len(removed.Removed) != 1 || removed.Removed[0].AddedAt == nil {
		t.Fatalf("remove %+v, %v", removed, err)
	}
	restored, err := backend.AddToCollection(dup.ID, []appwire.Member{removed.Removed[0]})
	if err != nil || !restored.Added[0].AddedAt.Equal(*removed.Removed[0].AddedAt) {
		t.Fatalf("restore %+v, %v", restored, err)
	}
	before := len(changedEvents(rec))
	if err := backend.DeleteCollection(b.ID); err != nil {
		t.Fatal(err)
	}
	events := changedEvents(rec)[before:]
	if len(events) != 3 || events[0] != (appwire.CollectionChanged{ID: b.ID, Deleted: true}) || events[1].ID != a.ID || events[2].ID != dup.ID {
		t.Fatalf("delete events %+v", events)
	}
	if err := backend.DeleteCollection(b.ID); wireCode(err) != appwire.CodeNotFound {
		t.Fatalf("second delete: %v", err)
	}
	list, err := backend.ListCollections(&appwire.Ref{Kind: appwire.RefCollection, ID: b.ID})
	if err != nil || len(list) != 2 || list[0].Count != 0 || list[0].Holds == nil || *list[0].Holds {
		t.Fatalf("list after delete %+v, %v", list, err)
	}
}
