package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
)

// collectionsBackend is a work backend whose collections store is a path in
// a fresh folder, not yet made.
func collectionsBackend(t *testing.T) (*appBackend, *appRecorder, appwire.Agent, string) {
	t.Helper()
	// The agent is spoken in first: a collection resolves a conversation by
	// the list, and a blank one is never listed (appblank.go).
	backend, rec, agent := workBackend(t, &fakeConversation{script: []session.Event{{Kind: session.EventTurnDone}}})
	if _, err := backend.Send(agent.ID, "plan the launch", ""); err != nil {
		t.Fatal(err)
	}
	rec.waitFor(t, 5*time.Second, appTurnDone)
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
	list, err := backend.ListCollections(nil, "", appwire.CollectionsListMax)
	if err != nil || list.Collections == nil || len(list.Collections) != 0 || list.Total != 0 || list.Next != "" {
		t.Fatalf("fresh list %+v, %v", list, err)
	}
	if _, err := backend.RenameCollection("nope", "x"); wireCode(err) != appwire.CodeNotFound {
		t.Fatalf("rename in a fresh home: %v", err)
	}
	if _, err := backend.OpenCollection("nope", 1, 500, ""); wireCode(err) != appwire.CodeNotFound {
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
	open, err := backend.OpenCollection(c.ID, 1, 500, "")
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
	page, err := backend.ListCollections(&appwire.Ref{Kind: appwire.RefCollection, ID: b.ID}, "", appwire.CollectionsListMax)
	list := page.Collections
	if err != nil || len(list) != 2 || page.Total != 2 || list[0].Count != 0 || list[0].Holds == nil || *list[0].Holds {
		t.Fatalf("list after delete %+v, %v", page, err)
	}
}

// A REF WHOSE OWNER CANNOT BE ASKED IS UNAVAILABLE, NEVER MISSING. Open says
// it is unknown with a reason naming the owner and why, without the path;
// expand counts it in unavailable and names it, keeps missing for what is
// really gone, and counts the nested collection it did not read.
func TestAppExpandCountsUnavailableWithAReasonApartFromMissing(t *testing.T) {
	backend, _, agent, _ := collectionsBackend(t)
	// A name longer than any file system allows: Lstat cannot say whether
	// it is there, which is the one unknown a test can make as any user.
	unaskable := filepath.Join(t.TempDir(), strings.Repeat("x", 300))
	c, _ := backend.CreateCollection("Launch")
	inner, _ := backend.CreateCollection("Inner")
	deeper, _ := backend.CreateCollection("Deeper")
	if _, err := backend.AddToCollection(inner.ID, []appwire.Member{{Ref: appwire.Ref{Kind: appwire.RefCollection, ID: deeper.ID}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := backend.AddToCollection(c.ID, []appwire.Member{
		{Ref: appwire.Ref{Kind: appwire.RefArtifact, ID: unaskable}},
		{Ref: appwire.Ref{Kind: appwire.RefConversation, ID: "0000000000000000"}},
		{Ref: appwire.Ref{Kind: appwire.RefConversation, ID: agent.ID}},
		{Ref: appwire.Ref{Kind: appwire.RefCollection, ID: inner.ID}},
	}); err != nil {
		t.Fatal(err)
	}
	open, err := backend.OpenCollection(c.ID, 0, 500, "")
	if err != nil {
		t.Fatal(err)
	}
	var reason string
	for _, n := range open.Members {
		if n.ID == unaskable {
			if n.Target.State != appwire.TargetUnknown {
				t.Fatalf("unaskable file is %+v", n.Target)
			}
			reason = n.Target.Reason
		} else if n.Target.Reason != "" {
			t.Fatalf("%s is %s and still carries a reason %q", n.ID, n.Target.State, n.Target.Reason)
		}
	}
	if !strings.HasPrefix(reason, "the file system could not be asked: ") || strings.Contains(reason, "/") || strings.Contains(reason, "xxxx") {
		t.Fatalf("reason %q", reason)
	}
	e, err := backend.ExpandCollection(c.ID, 2, 200)
	if err != nil {
		t.Fatal(err)
	}
	if e.Unavailable != 1 || e.Missing != 1 || len(e.Refs) != 1 || e.Unread != 1 || e.Collections != 1 || e.More {
		t.Fatalf("expand %+v", e)
	}
	if len(e.UnavailableRefs) != 1 || e.UnavailableRefs[0].ID != unaskable || e.UnavailableRefs[0].Target.Reason != reason || e.UnavailableRefs[0].AddedAt == nil {
		t.Fatalf("unavailable refs %+v", e.UnavailableRefs)
	}
	// The limit cuts live refs only, and everything past it is still
	// counted: the gone one is missing however small the limit.
	if e, err := backend.ExpandCollection(c.ID, 1, 1); err != nil || e.Missing != 1 || e.Unavailable != 1 || len(e.Refs) != 1 || e.More || e.Unread != 1 {
		t.Fatalf("expand at limit 1 %+v, %v", e, err)
	}
}

// THE APP PAGES THROUGH EVERYTHING: a listing walked by its cursor reaches
// every collection with the total, an opened collection of more than one
// page is walked to its end without a repeat, and a cursor the engine did
// not give is bad_params.
func TestAppCollectionPagesWalkToTheEnd(t *testing.T) {
	backend, _, _, _ := collectionsBackend(t)
	want := map[string]bool{}
	for i := 0; i < 5; i++ {
		c, err := backend.CreateCollection(fmt.Sprintf("C%d", i))
		if err != nil {
			t.Fatal(err)
		}
		want[c.ID] = true
	}
	after, read := "", 0
	for {
		page, err := backend.ListCollections(nil, after, 2)
		if err != nil || page.Total != 5 || len(page.Collections) > 2 {
			t.Fatalf("page %+v, %v", page, err)
		}
		for _, c := range page.Collections {
			if !want[c.ID] {
				t.Fatalf("%s read twice or never made", c.ID)
			}
			delete(want, c.ID)
			read++
		}
		if page.Next == "" {
			break
		}
		after = page.Next
	}
	if read != 5 {
		t.Fatalf("read %d of 5", read)
	}
	if _, err := backend.ListCollections(nil, "garbage", 2); wireCode(err) != appwire.CodeBadParams {
		t.Fatalf("a garbage cursor: %v", err)
	}
	big, _ := backend.CreateCollection("Big")
	for batch := 0; batch < 3; batch++ {
		refs := make([]appwire.Member, 0, appwire.CollectionRefsMax)
		for i := 0; i < appwire.CollectionRefsMax; i++ {
			refs = append(refs, appwire.Member{Ref: appwire.Ref{Kind: appwire.RefConversation, ID: fmt.Sprintf("c-%d-%d", batch, i)}})
		}
		if _, err := backend.AddToCollection(big.ID, refs); err != nil {
			t.Fatal(err)
		}
	}
	seen := map[string]bool{}
	after = ""
	for pages := 0; ; pages++ {
		open, err := backend.OpenCollection(big.ID, 0, appwire.CollectionOpenLimit, after)
		if err != nil {
			t.Fatal(err)
		}
		for _, n := range open.Members {
			if seen[n.ID] {
				t.Fatalf("%s on two pages", n.ID)
			}
			seen[n.ID] = true
		}
		if open.Next == "" {
			if open.More || pages != 1 {
				t.Fatalf("last page more %v after %d pages", open.More, pages)
			}
			break
		}
		after = open.Next
	}
	if len(seen) != 3*appwire.CollectionRefsMax {
		t.Fatalf("opened %d of %d", len(seen), 3*appwire.CollectionRefsMax)
	}
	first, _ := backend.CreateCollection("Other")
	page, _ := backend.OpenCollection(big.ID, 0, 1, "")
	if _, err := backend.OpenCollection(first.ID, 0, 1, page.Next); wireCode(err) != appwire.CodeBadParams {
		t.Fatalf("another collection's cursor: %v", err)
	}
}

// RELINK ASKS THE NEW TARGET'S OWNER FIRST AND WRITES ONCE: a file that is
// there takes the old ref's moment and place and says collection.changed
// once; a file that is not there, a folder, a conversation that is gone and
// a non-member are not_found; an owner that cannot be asked is the engine's
// failure with the reason; a cycle is cycle; a ref already there is already.
func TestAppRelinkChecksTheNewTargetBeforeWriting(t *testing.T) {
	backend, rec, agent, _ := collectionsBackend(t)
	dir := t.TempDir()
	old, moved := filepath.Join(dir, "old.md"), filepath.Join(dir, "moved.md")
	if err := os.WriteFile(moved, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	c, _ := backend.CreateCollection("Launch")
	parent, _ := backend.CreateCollection("Parent")
	added, err := backend.AddToCollection(c.ID, []appwire.Member{
		{Ref: appwire.Ref{Kind: appwire.RefArtifact, ID: old}},
		{Ref: appwire.Ref{Kind: appwire.RefConversation, ID: agent.ID}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := backend.AddToCollection(c.ID, []appwire.Member{{Ref: appwire.Ref{Kind: appwire.RefCollection, ID: parent.ID}}}); err != nil {
		t.Fatal(err)
	}
	before := len(changedEvents(rec))
	from := appwire.Ref{Kind: appwire.RefArtifact, ID: old}
	r, err := backend.RelinkCollection(c.ID, from, appwire.Ref{Kind: appwire.RefArtifact, ID: moved})
	if err != nil || r.Already || r.Member.ID != moved || !r.Member.AddedAt.Equal(*added.Added[0].AddedAt) {
		t.Fatalf("relink %+v, %v", r, err)
	}
	if events := changedEvents(rec)[before:]; len(events) != 1 || events[0].ID != c.ID {
		t.Fatalf("relink events %+v", events)
	}
	from = appwire.Ref{Kind: appwire.RefArtifact, ID: moved}
	for name, to := range map[string]appwire.Ref{
		"a file that is not there":    {Kind: appwire.RefArtifact, ID: filepath.Join(dir, "nothing.md")},
		"a folder":                    {Kind: appwire.RefArtifact, ID: dir},
		"a conversation that is gone": {Kind: appwire.RefConversation, ID: "0000000000000000"},
		"a collection that is gone":   {Kind: appwire.RefCollection, ID: "nope"},
	} {
		if _, err := backend.RelinkCollection(c.ID, from, to); wireCode(err) != appwire.CodeNotFound {
			t.Errorf("relink to %s: %v", name, err)
		}
	}
	_, err = backend.RelinkCollection(c.ID, from, appwire.Ref{Kind: appwire.RefArtifact, ID: filepath.Join(dir, "nothing.md")})
	if wire := (*appwire.Error)(nil); !errors.As(err, &wire) || wire.Message != "nothing is at that path now" {
		t.Fatalf("missing file says %v", err)
	}
	_, err = backend.RelinkCollection(c.ID, from, appwire.Ref{Kind: appwire.RefArtifact, ID: filepath.Join(dir, strings.Repeat("y", 300))})
	if wire := (*appwire.Error)(nil); !errors.As(err, &wire) || wire.Code != appwire.CodeEngine || !strings.HasPrefix(wire.Message, "the file system could not be asked: ") {
		t.Fatalf("an unaskable file: %v", err)
	}
	if _, err := backend.RelinkCollection(c.ID, appwire.Ref{Kind: appwire.RefConversation, ID: "never-added"}, appwire.Ref{Kind: appwire.RefArtifact, ID: moved}); wireCode(err) != appwire.CodeNotFound {
		t.Fatalf("relink of a non-member: %v", err)
	}
	if _, err := backend.AddToCollection(parent.ID, []appwire.Member{{Ref: appwire.Ref{Kind: appwire.RefConversation, ID: "x"}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := backend.RelinkCollection(parent.ID, appwire.Ref{Kind: appwire.RefConversation, ID: "x"}, appwire.Ref{Kind: appwire.RefCollection, ID: c.ID}); wireCode(err) != appwire.CodeCycle {
		t.Fatalf("relink making a cycle: %v", err)
	}
	r, err = backend.RelinkCollection(c.ID, from, appwire.Ref{Kind: appwire.RefConversation, ID: agent.ID})
	if err != nil || !r.Already || r.Member.ID != agent.ID {
		t.Fatalf("relink onto a member %+v, %v", r, err)
	}
	if open, _ := backend.OpenCollection(c.ID, 0, 500, ""); len(open.Members) != 2 {
		t.Fatalf("after relinking onto a member %+v", open.Members)
	}
}
