package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
)

// The wire answers from the index the engine writes: list, count, get, read,
// keep (never overwriting), remove (index only), snapshot and branches.
func TestAppDeliverablesRoundTripThroughTheIndex(t *testing.T) {
	backend, _, agent := workBackend(t, &fakeConversation{script: []session.Event{{Kind: session.EventTurnDone}}})
	index := filepath.Join(t.TempDir(), "artifacts.jsonl")
	old := appMediaIndex
	appMediaIndex = func() string { return index }
	t.Cleanup(func() { appMediaIndex = old })
	id, _, err := backend.mediaScope(agent.ID)
	if err != nil {
		t.Fatal(err)
	}
	doc := filepath.Join(agent.Workspace, "notes.md")
	os.WriteFile(doc, []byte("# notes\n"), 0o600)
	fact := session.DeliverableFact{ID: session.NewDeliverableID(), At: time.Now(), Origin: session.DeliverOriginAgent, Authored: true, Run: "run1", Turn: "scope#4"}
	if !session.RecordArtifact(index, session.Artifact{Path: doc, Session: id, Title: "notes.md", Kind: "markdown", Created: time.Now(), Deliver: &fact}) {
		t.Fatal("row not written")
	}
	legacy := filepath.Join(agent.Workspace, "old.md")
	os.WriteFile(legacy, []byte("old"), 0o600)
	session.RecordArtifact(index, session.Artifact{Path: legacy, Session: id, Title: "old.md", Kind: "export", Created: time.Now().Add(-time.Hour)})

	page, err := backend.ListDeliverables(appwire.DeliverablesQuery{Scope: "agent", Agent: agent.ID, Limit: 10})
	if err != nil || len(page.Deliverables) != 2 || page.Next != "" {
		t.Fatalf("list %+v %v", page, err)
	}
	old2, made := page.Deliverables[0], page.Deliverables[1]
	if made.ID != fact.ID || made.Turn != "scope#4" || made.Run != "run1" || made.Live != "available" || made.Origin != "agent" || made.State != "" {
		t.Fatalf("made %+v", made)
	}
	if old2.State != "" || old2.Turn != "" || old2.Origin != "unknown" || old2.Kind != "export" {
		t.Fatalf("legacy %+v", old2)
	}
	if page, _ := backend.ListDeliverables(appwire.DeliverablesQuery{Scope: "global", Query: "NOTES"}); len(page.Deliverables) != 1 {
		t.Fatalf("global search %+v", page)
	}
	if n, err := backend.CountDeliverables(appwire.DeliverablesQuery{Scope: "agent", Agent: agent.ID}); err != nil || n.Count != 2 || n.Next != "" {
		t.Fatalf("count %+v %v", n, err)
	}
	if _, err := backend.ListDeliverables(appwire.DeliverablesQuery{Scope: "crew", Crew: "nope"}); err == nil {
		t.Fatal("unknown crew accepted")
	}
	text, err := backend.ReadDeliverable(agent.ID, made.ID, "", appwire.DeliverableReadMax)
	if err != nil || text.Text != "# notes\n" || text.Truncated {
		t.Fatalf("read %+v %v", text, err)
	}
	if short, _ := backend.ReadDeliverable(agent.ID, made.ID, "", 3); !short.Truncated || short.Text != "# n" {
		t.Fatalf("bounded read %+v", short)
	}
	if _, err := backend.ReadDeliverable(agent.ID, made.ID, "file:1:1:1:1:1", 100); !hasCode(err, appwire.CodeConflict) {
		t.Fatalf("stale read: %v", err)
	}

	dest := t.TempDir()
	os.WriteFile(filepath.Join(dest, "notes.md"), []byte("mine"), 0o600)
	if _, err := backend.KeepDeliverable(agent.ID, made.ID, dest, "", ""); !hasCode(err, appwire.CodeConflict) {
		t.Fatalf("collision: %v", err)
	}
	kept, err := backend.KeepDeliverable(agent.ID, made.ID, dest, "notes-2.md", "")
	if err != nil || kept.State != "kept" || kept.Kept == nil || kept.Kept.How != "copied" || kept.Kept.Path != filepath.Join(canon(dest), "notes-2.md") {
		t.Fatalf("keep %+v %v", kept, err)
	}
	if page, _ := backend.ListDeliverables(appwire.DeliverablesQuery{Scope: "agent", Agent: agent.ID, State: []string{"draft"}}); len(page.Deliverables) != 0 {
		t.Fatalf("stale base matched draft: %+v", page)
	}

	if err := backend.RemoveDeliverable(agent.ID, old2.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(legacy); err != nil {
		t.Fatal("remove deleted the file")
	}
	if _, err := backend.GetDeliverable(agent.ID, old2.ID); !hasCode(err, appwire.CodeNotFound) {
		t.Fatalf("removed still found: %v", err)
	}
	if page, _ := backend.ListDeliverables(appwire.DeliverablesQuery{Scope: "agent", Agent: agent.ID}); len(page.Deliverables) != 1 {
		t.Fatalf("after remove %+v", page)
	}

	// A snapshot must live in the agent's own folders and name a loopback page.
	held, err := backend.find(agent.ID)
	if err != nil {
		t.Fatal(err)
	}
	backend.locked(func() { held.transcript = filepath.Join(t.TempDir(), agent.ID, "transcript.jsonl") })
	folder := filepath.Join(filepath.Dir(held.transcript), "artifacts")
	os.MkdirAll(folder, 0o700)
	shot := filepath.Join(folder, "shot.png")
	writePNG(t, shot, 4, 4)
	row, err := backend.SnapshotDeliverable(agent.ID, shot, "http://localhost:3000/", "")
	if err != nil || row.Origin != "saved_by_you" || row.Kind != "image" || row.Media == "" || len(row.From) != 1 {
		t.Fatalf("snapshot %+v %v", row, err)
	}
	again, err := backend.SnapshotDeliverable(agent.ID, shot, "http://localhost:3000/", "")
	if err != nil || again.ID != row.ID {
		t.Fatalf("snapshot twice %+v %v", again, err)
	}
	if _, err := backend.SnapshotDeliverable(agent.ID, doc, "http://localhost:3000/", ""); err == nil {
		t.Fatal("a non-snapshot file was registered")
	}
	if _, err := backend.SnapshotDeliverable(agent.ID, shot, "https://example.com/", ""); err == nil {
		t.Fatal("an off-machine page was accepted")
	}

	branches, err := backend.TaskBranches(appwire.BranchesQuery{Agent: agent.ID})
	if err != nil || branches.Branches == nil {
		t.Fatalf("branches %+v %v", branches, err)
	}
}

func hasCode(err error, code string) bool {
	var e *appwire.Error
	return errors.As(err, &e) && e.Code == code
}

func canon(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return p
}

// The same creation arriving on two lanes is said once, the window is bounded,
// and the event names the conversation whose stream carried it.
func TestCreatedDeliverableIsSaidOnceAndBounded(t *testing.T) {
	recorder := newAppRecorder()
	backend := &appBackend{}
	live := &appLive{box: appwire.NewOutbox(recorder)}
	first := session.ArtifactChange{ID: "dlv_" + strings.Repeat("a", 32), Kind: "markdown", Run: "run_actual"}
	for range 20 {
		backend.artifactCreated("agent", live, first)
	}
	for i := range 1000 {
		backend.artifactCreated("agent", live, session.ArtifactChange{ID: fmt.Sprintf("dlv_%032x", i)})
	}
	backend.artifactCreated("agent", live, session.ArtifactChange{})
	if len(live.artifactSeen) != appMediaRecentMax || len(live.artifactRecent) != appMediaRecentMax {
		t.Fatalf("unbounded: %d %d", len(live.artifactSeen), len(live.artifactRecent))
	}
	live.box.Close()
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	if len(recorder.events) != 1001 {
		t.Fatalf("%d events for 1001 unique creations", len(recorder.events))
	}
	if recorder.events[0] != appwire.EventDeliverableChanged {
		t.Fatalf("event name %q", recorder.events[0])
	}
	data, ok := recorder.data[0].(appwire.DeliverableChanged)
	if !ok || data.ID != first.ID || data.Agent != "agent" || data.Reason != "created" {
		t.Fatalf("data %+v", recorder.data[0])
	}
}

// A scripted conversation reports a creation; the app says it, and the list a
// subscriber then asks for - and a fresh one after a reopen - holds the row.
func TestCreationEventThenListThenReplay(t *testing.T) {
	backend, rec, agent := workBackend(t, &fakeConversation{script: []session.Event{{Kind: session.EventTurnDone}}})
	index := filepath.Join(t.TempDir(), "artifacts.jsonl")
	old := appMediaIndex
	appMediaIndex = func() string { return index }
	t.Cleanup(func() { appMediaIndex = old })
	id, _, err := backend.mediaScope(agent.ID)
	if err != nil {
		t.Fatal(err)
	}
	doc := filepath.Join(agent.Workspace, "plan.md")
	os.WriteFile(doc, []byte("# plan\n"), 0o600)
	fact := session.DeliverableFact{ID: session.NewDeliverableID(), At: time.Now(), Origin: session.DeliverOriginAgent, Authored: true, Run: "run1"}
	session.RecordArtifact(index, session.Artifact{Path: doc, Session: id, Title: "plan.md", Kind: "markdown", Created: time.Now(), Deliver: &fact})
	held, err := backend.find(agent.ID)
	if err != nil {
		t.Fatal(err)
	}
	live := backend.liveOf(held)
	if live == nil {
		t.Skip("the fake conversation is not held live")
	}
	backend.workEvent(agent.ID, live, session.Event{Kind: session.EventArtifactUpdate, Artifact: &session.ArtifactChange{ID: fact.ID, Kind: "markdown", Run: "run1"}})
	rec.waitFor(t, 2*time.Second, func(event string, data any) bool {
		d, ok := data.(appwire.DeliverableChanged)
		return event == appwire.EventDeliverableChanged && ok && d.ID == fact.ID && d.Agent == agent.ID && d.Reason == "created"
	})
	for range 2 { // the first list, then the one a fresh subscriber makes
		page, err := backend.ListDeliverables(appwire.DeliverablesQuery{Scope: "agent", Agent: agent.ID})
		if err != nil || len(page.Deliverables) != 1 || page.Deliverables[0].ID != fact.ID {
			t.Fatalf("list %+v %v", page, err)
		}
	}
}

// A kept picture whose original is gone is still openable from its copy, but it
// does not hand out the dead media source id as a preview.
func TestAKeptPictureWithoutItsSourceOffersNoDeadPreview(t *testing.T) {
	backend, _, agent := workBackend(t, &fakeConversation{script: []session.Event{{Kind: session.EventTurnDone}}})
	index := filepath.Join(t.TempDir(), "artifacts.jsonl")
	old := appMediaIndex
	appMediaIndex = func() string { return index }
	t.Cleanup(func() { appMediaIndex = old })
	id, _, err := backend.mediaScope(agent.ID)
	if err != nil {
		t.Fatal(err)
	}
	pic := filepath.Join(agent.Workspace, "made.png")
	data := writePNG(t, pic, 8, 6)
	fact := session.DeliverableFact{ID: session.NewDeliverableID(), At: time.Now(), Origin: session.DeliverOriginAgent, Authored: true}
	sid := session.RecordMedia(index, session.Artifact{Path: pic, Session: id, Title: "made.png", Kind: "image", Created: time.Now(), Deliver: &fact},
		session.GeneratedMedia(pic, data, "paint/model"))
	if sid == "" {
		t.Fatal("not recorded")
	}
	before, err := backend.GetDeliverable(agent.ID, fact.ID)
	if err != nil || before.Media != sid || before.PreviewUnavailable != "" {
		t.Fatalf("before keep: %+v %v", before, err)
	}
	dest := t.TempDir()
	if _, err := backend.KeepDeliverable(agent.ID, fact.ID, dest, "", ""); err != nil {
		t.Fatal(err)
	}
	os.Remove(pic)
	after, err := backend.GetDeliverable(agent.ID, fact.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Media != "" || after.PreviewUnavailable != "source_gone" || after.Live != "available" || after.OpenFrom != "kept" || after.Open != filepath.Join(canon(dest), "made.png") {
		t.Fatalf("after the source is gone: %+v", after)
	}
}

// A global listing and a get agree about a conversation this machine cannot
// locate: the row is described, and its file is not vouched for.
func TestGetAnswersForAConversationTheMachineCannotLocate(t *testing.T) {
	backend, _, _ := workBackend(t, &fakeConversation{script: []session.Event{{Kind: session.EventTurnDone}}})
	index := filepath.Join(t.TempDir(), "artifacts.jsonl")
	old := appMediaIndex
	appMediaIndex = func() string { return index }
	t.Cleanup(func() { appMediaIndex = old })
	fact := session.DeliverableFact{ID: session.NewDeliverableID(), At: time.Now(), Origin: session.DeliverOriginAgent}
	session.RecordArtifact(index, session.Artifact{Path: "/gone/doc.md", Session: "ffffffffffffffff", Title: "doc.md", Kind: "markdown", Created: time.Now(), Deliver: &fact})
	page, err := backend.ListDeliverables(appwire.DeliverablesQuery{Scope: "global"})
	if err != nil || len(page.Deliverables) != 1 {
		t.Fatalf("list %+v %v", page, err)
	}
	got, err := backend.GetDeliverable("ffffffffffffffff", fact.ID)
	if err != nil || got.ID != fact.ID || got.Open != "" {
		t.Fatalf("get %+v %v", got, err)
	}
	if _, err := backend.ReadDeliverable("ffffffffffffffff", fact.ID, "", 100); err == nil {
		t.Fatal("read a file nothing vouches for")
	}
}

// branchWorld is a conversation whose checkout is a real repository with an
// unmerged task branch holding plan.md, and the worker's row for it recorded
// the way a worktree write records one: branch, rel, size and sha256, no
// checkout identity.
type branchWorld struct {
	backend *appBackend
	agent   string
	repo    string
	id      string
	run     func(args ...string)
}

const branchBody = "# on the branch\n"

func newBranchWorld(t *testing.T) *branchWorld {
	t.Helper()
	backend, _, agent := workBackend(t, &fakeConversation{script: []session.Event{{Kind: session.EventTurnDone}}})
	index := filepath.Join(t.TempDir(), "artifacts.jsonl")
	old := appMediaIndex
	appMediaIndex = func() string { return index }
	t.Cleanup(func() { appMediaIndex = old })
	id, _, err := backend.mediaScope(agent.ID)
	if err != nil {
		t.Fatal(err)
	}
	repo := agent.Workspace
	run := func(args ...string) {
		t.Helper()
		out, err := exec.Command("git", append([]string{"-C", repo, "-c", "user.name=t", "-c", "user.email=t@t"}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	run("init", "-q", "-b", "main")
	os.WriteFile(filepath.Join(repo, "a.txt"), []byte("a\n"), 0o644)
	run("add", ".")
	run("commit", "-q", "-m", "base")
	run("checkout", "-q", "-b", "task/doc-aaaaaa")
	os.WriteFile(filepath.Join(repo, "plan.md"), []byte(branchBody), 0o644)
	run("add", ".")
	run("commit", "-q", "-m", "work")
	run("checkout", "-q", "main")
	sum := sha256.Sum256([]byte(branchBody))
	fact := session.DeliverableFact{ID: session.NewDeliverableID(), At: time.Now(), Origin: session.DeliverOriginAgent, Authored: true,
		Task: "7", Branch: "task/doc-aaaaaa", Rel: "plan.md", Version: "sha256:" + hex.EncodeToString(sum[:]),
		File: &session.DeliverableFile{Bytes: int64(len(branchBody)), SHA256: hex.EncodeToString(sum[:])}}
	session.RecordArtifact(index, session.Artifact{Path: filepath.Join(canon(repo), "plan.md"), Session: id, Title: "plan.md", Kind: "markdown", Created: time.Now(), Deliver: &fact})
	return &branchWorld{backend: backend, agent: agent.ID, repo: repo, id: fact.ID, run: run}
}

// refused says get offers nothing to open, read serves nothing, and Keep
// copies nothing.
func (w *branchWorld) refused(t *testing.T, why string) appwire.Deliverable {
	t.Helper()
	got, err := w.backend.GetDeliverable(w.agent, w.id)
	if err != nil || got.Live == "available" || got.Open != "" {
		t.Fatalf("%s: described as %q open %q (%v)", why, got.Live, got.Open, err)
	}
	if text, err := w.backend.ReadDeliverable(w.agent, w.id, "", 1000); err == nil {
		t.Fatalf("%s: read served %q", why, text.Text)
	}
	if kept, err := w.backend.KeepDeliverable(w.agent, w.id, t.TempDir(), "", ""); err == nil {
		t.Fatalf("%s: kept %+v", why, kept.Kept)
	}
	return got
}

// A document a task made on a branch not yet merged is described as on that
// branch and read from git, pinned to its blob; merged (and never settled), it
// is the checkout's file only while that file is exactly the authored bytes.
func TestABranchOnlyDocumentIsDescribedAndRead(t *testing.T) {
	w := newBranchWorld(t)
	got, err := w.backend.GetDeliverable(w.agent, w.id)
	if err != nil || got.Live != "on_branch" || got.Open != "" || got.Branch != "task/doc-aaaaaa" || got.Rel != "plan.md" || got.Task != "7" || !strings.HasPrefix(got.LiveVersion, "git:") {
		t.Fatalf("%+v %v", got, err)
	}
	text, err := w.backend.ReadDeliverable(w.agent, w.id, got.LiveVersion, 1000)
	if err != nil || text.Text != branchBody || text.Version != got.LiveVersion {
		t.Fatalf("%+v %v", text, err)
	}
	if _, err := w.backend.ReadDeliverable(w.agent, w.id, "git:0000", 1000); !hasCode(err, appwire.CodeConflict) {
		t.Fatalf("stale pin: %v", err)
	}
	if _, err := w.backend.KeepDeliverable(w.agent, w.id, t.TempDir(), "", ""); !hasCode(err, appwire.CodeNotFound) {
		t.Fatalf("a branch-only file was kept as though it were on disk: %v", err)
	}
	// The branch moves on: the old pin is stale, the new blob is what is read.
	w.run("checkout", "-q", "task/doc-aaaaaa")
	os.WriteFile(filepath.Join(w.repo, "plan.md"), []byte("# moved on\n"), 0o644)
	w.run("commit", "-q", "-am", "more")
	w.run("checkout", "-q", "main")
	if _, err := w.backend.ReadDeliverable(w.agent, w.id, got.LiveVersion, 1000); !hasCode(err, appwire.CodeConflict) {
		t.Fatalf("a pin to a blob the branch no longer holds: %v", err)
	}
	moved, _ := w.backend.GetDeliverable(w.agent, w.id)
	if text, err := w.backend.ReadDeliverable(w.agent, w.id, moved.LiveVersion, 1000); err != nil || text.Text != "# moved on\n" || moved.LiveVersion == got.LiveVersion {
		t.Fatalf("the moved branch: %+v %v", text, err)
	}
	// Merged with later bytes than the authored ones: the checkout file is not
	// the deliverable the worker made.
	w.run("merge", "-q", "--no-edit", "task/doc-aaaaaa")
	if after := w.refused(t, "merged with other bytes"); after.Live != "changed" {
		t.Fatalf("merged with other bytes is %q", after.Live)
	}
	// Put back exactly the authored bytes: now it is, and it can be kept.
	os.WriteFile(filepath.Join(w.repo, "plan.md"), []byte(branchBody), 0o644)
	after, _ := w.backend.GetDeliverable(w.agent, w.id)
	if after.Live != "available" || after.Open == "" || after.OpenFrom != "source" {
		t.Fatalf("the authored bytes after the merge: %+v", after)
	}
	if text, err := w.backend.ReadDeliverable(w.agent, w.id, after.LiveVersion, 1000); err != nil || text.Text != branchBody {
		t.Fatalf("read after the merge: %+v %v", text, err)
	}
	dest := t.TempDir()
	kept, err := w.backend.KeepDeliverable(w.agent, w.id, dest, "", "")
	sum := sha256.Sum256([]byte(branchBody))
	if err != nil || kept.Kept == nil || kept.Kept.SHA256 != hex.EncodeToString(sum[:]) || kept.State != "kept" {
		t.Fatalf("keep after the merge: %+v %v", kept, err)
	}
	// The checkout file is replaced by the person; the kept copy is still it.
	os.WriteFile(filepath.Join(w.repo, "plan.md"), []byte("# theirs now\n"), 0o644)
	copied, _ := w.backend.GetDeliverable(w.agent, w.id)
	if copied.Live != "available" || copied.OpenFrom != "kept" || copied.Open != kept.Kept.Path {
		t.Fatalf("the kept copy after the checkout file was replaced: %+v", copied)
	}
	if text, err := w.backend.ReadDeliverable(w.agent, w.id, "", 1000); err != nil || text.Text != branchBody {
		t.Fatalf("read from the kept copy: %+v %v", text, err)
	}
}

// A file at the future checkout path is never the deliverable by being there:
// unmerged it is shadowed by the branch, and with the branch gone unmerged a
// stranger's bytes, a link to the same bytes, and nothing at all are each
// refused - nothing to open, read or keep.
func TestAnUnrelatedCheckoutFileNeverStandsInForABranchDeliverable(t *testing.T) {
	w := newBranchWorld(t)
	path := filepath.Join(w.repo, "plan.md")
	os.WriteFile(path, []byte("MY OWN UNRELATED NOTES\n"), 0o644)
	got, err := w.backend.GetDeliverable(w.agent, w.id)
	if err != nil || got.Live != "on_branch" || got.Open != "" {
		t.Fatalf("unmerged, shadowed: %+v %v", got, err)
	}
	if text, err := w.backend.ReadDeliverable(w.agent, w.id, "", 1000); err != nil || text.Text != branchBody {
		t.Fatalf("read served the person's notes: %+v %v", text, err)
	}
	if _, err := w.backend.KeepDeliverable(w.agent, w.id, t.TempDir(), "", ""); err == nil {
		t.Fatal("the person's notes were kept as the deliverable")
	}
	w.run("branch", "-q", "-D", "task/doc-aaaaaa") // gone, never merged
	if after := w.refused(t, "a stranger's bytes"); after.Live != "changed" {
		t.Fatalf("a stranger's bytes are %q", after.Live)
	}
	same := filepath.Join(t.TempDir(), "same.md")
	os.WriteFile(same, []byte(branchBody), 0o644)
	os.Remove(path)
	os.Symlink(same, path)
	w.refused(t, "a link to the same bytes")
	os.Remove(path)
	if after := w.refused(t, "nothing there"); after.Live != "gone" {
		t.Fatalf("nothing there is %q", after.Live)
	}
	// The same size, other bytes: refused on the digest, not the size.
	os.WriteFile(path, []byte(strings.Repeat("x", len(branchBody))), 0o644)
	w.refused(t, "the same size and other bytes")
}

// Keep's unconfirmed outcome is its own word on the wire: the app's "may still
// be finishing" and never retried, so it can never become a second copy. It
// carries no path and no detail.
func TestAnUnconfirmedKeepIsUncertainOnTheWire(t *testing.T) {
	err := appKeepError(fmt.Errorf("wrapped: %w", session.ErrKeepUnconfirmed))
	var wire *appwire.Error
	if !errors.As(err, &wire) || wire.Code != appwire.CodeUncertain || wire.Detail != nil || strings.Contains(wire.Message, "/") {
		t.Fatalf("unconfirmed Keep on the wire: %#v", err)
	}
	if err := appKeepError(session.ErrKeepRecord); hasCode(err, appwire.CodeUncertain) {
		t.Fatalf("a copy that was removed is not uncertain: %v", err)
	}
}

// A task worker's creation reaches the app on the IDLE conversation's own
// standing task lane - no turn running, nothing asked - as deliverable.changed
// created, said once however often the lane repeats it.
func TestAWorkersCreationIsSaidOnTheIdleConversationsLane(t *testing.T) {
	conv := &fakeConversation{script: []session.Event{{Kind: session.EventTurnDone}}}
	backend, rec, agent := workBackend(t, conv)
	held, err := backend.find(agent.ID)
	if err != nil {
		t.Fatal(err)
	}
	if backend.liveOf(held) == nil {
		t.Fatal("the conversation is not held live, so it has no standing lane")
	}
	id := session.NewDeliverableID()
	said := func() int {
		n := 0
		rec.mu.Lock()
		defer rec.mu.Unlock()
		for i, event := range rec.events {
			if d, ok := rec.data[i].(appwire.DeliverableChanged); ok && event == appwire.EventDeliverableChanged && d.ID == id && d.Agent == agent.ID && d.Reason == "created" {
				n++
			}
		}
		return n
	}
	change := session.Event{Kind: session.EventArtifactUpdate, Artifact: &session.ArtifactChange{ID: id, Kind: "markdown", Run: "worker-run"}}
	conv.lane(change)
	rec.waitFor(t, 2*time.Second, func(event string, data any) bool {
		d, ok := data.(appwire.DeliverableChanged)
		return event == appwire.EventDeliverableChanged && ok && d.ID == id && d.Reason == "created"
	})
	conv.lane(change)
	marker := session.NewDeliverableID()
	conv.lane(session.Event{Kind: session.EventArtifactUpdate, Artifact: &session.ArtifactChange{ID: marker, Kind: "markdown"}})
	rec.waitFor(t, 2*time.Second, func(event string, data any) bool {
		d, ok := data.(appwire.DeliverableChanged)
		return ok && d.ID == marker
	})
	if n := said(); n != 1 {
		t.Fatalf("the creation was said %d times", n)
	}
}

// manyBranchRows is a repository with an unmerged task branch holding n files
// in each of the top folder and docs/, a row for each, and then `plain`
// ordinary rows recorded after them (so they list first).
func manyBranchRows(t *testing.T, n, plain int) (*appBackend, appwire.Agent, map[string]bool) {
	t.Helper()
	backend, _, agent := workBackend(t, &fakeConversation{script: []session.Event{{Kind: session.EventTurnDone}}})
	index := filepath.Join(t.TempDir(), "artifacts.jsonl")
	old := appMediaIndex
	appMediaIndex = func() string { return index }
	t.Cleanup(func() { appMediaIndex = old })
	id, _, err := backend.mediaScope(agent.ID)
	if err != nil {
		t.Fatal(err)
	}
	repo := agent.Workspace
	run := func(args ...string) {
		t.Helper()
		out, err := exec.Command("git", append([]string{"-C", repo, "-c", "user.name=t", "-c", "user.email=t@t"}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	run("init", "-q", "-b", "main")
	os.WriteFile(filepath.Join(repo, "a.txt"), []byte("a\n"), 0o644)
	run("add", ".")
	run("commit", "-q", "-m", "base")
	run("checkout", "-q", "-b", "task/many-aaaaaa")
	os.MkdirAll(filepath.Join(repo, "docs"), 0o755)
	var rels []string
	for i := 0; i < n; i++ {
		for _, folder := range []string{"", "docs/"} {
			rel := fmt.Sprintf("%sp%03d.md", folder, i)
			os.WriteFile(filepath.Join(repo, filepath.FromSlash(rel)), []byte("x\n"), 0o644)
			rels = append(rels, rel)
		}
	}
	run("add", ".")
	run("commit", "-q", "-m", "work")
	run("checkout", "-q", "main")
	want := map[string]bool{}
	sum := sha256.Sum256([]byte("x\n"))
	for _, rel := range rels {
		fact := session.DeliverableFact{ID: session.NewDeliverableID(), At: time.Now(), Origin: session.DeliverOriginAgent, Authored: true,
			Task: "7", Branch: "task/many-aaaaaa", Rel: rel, File: &session.DeliverableFile{Bytes: 2, SHA256: hex.EncodeToString(sum[:])}}
		session.RecordArtifact(index, session.Artifact{Path: filepath.Join(canon(repo), filepath.FromSlash(rel)), Session: id, Title: path.Base(rel), Kind: "markdown", Created: time.Now(), Deliver: &fact})
		want[fact.ID] = true
	}
	for i := 0; i < plain; i++ {
		doc := filepath.Join(repo, fmt.Sprintf("plain%d.md", i))
		os.WriteFile(doc, []byte("plain\n"), 0o644)
		fact := session.DeliverableFact{ID: session.NewDeliverableID(), At: time.Now(), Origin: session.DeliverOriginAgent, Authored: true}
		session.RecordArtifact(index, session.Artifact{Path: canon(doc), Session: id, Title: filepath.Base(doc), Kind: "markdown", Created: time.Now(), Deliver: &fact})
		want[fact.ID] = true
	}
	return backend, agent, want
}

// watchGit counts the git processes each deliverables request starts.
func watchGit(t *testing.T) func() []int {
	t.Helper()
	var mu sync.Mutex
	var envs []*appDeliverEnv
	old := appDeliverEnvMade
	appDeliverEnvMade = func(env *appDeliverEnv) { mu.Lock(); envs = append(envs, env); mu.Unlock() }
	t.Cleanup(func() { appDeliverEnvMade = old })
	return func() []int {
		mu.Lock()
		defer mu.Unlock()
		var calls []int
		for _, env := range envs {
			calls = append(calls, env.cache.GitCalls())
		}
		envs = nil
		return calls
	}
}

// Two hundred rows a task left on one unmerged branch, in two folders: a page
// of a hundred and a count of all of them each ask git a handful of questions
// - the branch once, each folder once - not five per row, and every row is
// there, on its branch.
func TestManyBranchRowsListInAFewGitCalls(t *testing.T) {
	backend, agent, want := manyBranchRows(t, 100, 0)
	calls := watchGit(t)
	seen := map[string]bool{}
	pages := 0
	for cursor := ""; ; pages++ {
		started := time.Now()
		page, err := backend.ListDeliverables(appwire.DeliverablesQuery{Scope: "agent", Agent: agent.ID, Limit: 100, Cursor: cursor})
		if err != nil {
			t.Fatal(err)
		}
		if took := time.Since(started); took > appDeliverListTime+time.Second {
			t.Fatalf("a page took %v", took)
		}
		for _, d := range page.Deliverables {
			if d.Live != "on_branch" || seen[d.ID] {
				t.Fatalf("row %s: %q (seen before: %v)", d.ID, d.Live, seen[d.ID])
			}
			seen[d.ID] = true
		}
		if page.Next == "" {
			break
		}
		cursor = page.Next
	}
	if len(seen) != len(want) {
		t.Fatalf("listed %d of %d rows", len(seen), len(want))
	}
	for i, n := range calls() {
		if n > 6 {
			t.Fatalf("page %d asked git %d times", i, n)
		}
	}
	count, err := backend.CountDeliverables(appwire.DeliverablesQuery{Scope: "agent", Agent: agent.ID})
	if err != nil || count.Count != len(want) || count.Next != "" {
		t.Fatalf("count %+v %v", count, err)
	}
	if got := calls(); len(got) != 1 || got[0] > 6 {
		t.Fatalf("the count asked git %v times", got)
	}
	t.Logf("%d rows over %d pages", len(seen), pages+1)
}

// A git that stalls is cancelled at the listing's deadline: the page ends at
// the first row it could not decide, with a cursor that resumes AT it - never
// a row left out, never one called gone - and once git answers again the
// cursor finds every row.
func TestAStalledGitEndsThePageAndTheCursorResumes(t *testing.T) {
	backend, agent, want := manyBranchRows(t, 20, 3)
	real, err := exec.LookPath("git")
	if err != nil {
		t.Skip("no git")
	}
	bin := t.TempDir()
	stall := filepath.Join(bin, "stall")
	script := "#!/bin/sh\nif [ -e '" + stall + "' ]; then exec sleep 30; fi\nexec '" + real + "' \"$@\"\n"
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	os.WriteFile(stall, nil, 0o644)
	started := time.Now()
	page, err := backend.ListDeliverables(appwire.DeliverablesQuery{Scope: "agent", Agent: agent.ID, Limit: 100})
	if took := time.Since(started); took > appDeliverListTime+2*time.Second {
		t.Fatalf("a stalled git held the page for %v", took)
	}
	if err != nil || len(page.Deliverables) != 3 || page.Next == "" || !page.Incomplete {
		t.Fatalf("the page before the stall: %d rows next=%q incomplete=%v err=%v", len(page.Deliverables), page.Next, page.Incomplete, err)
	}
	seen := map[string]bool{}
	for _, d := range page.Deliverables {
		if d.Live != "available" {
			t.Fatalf("a plain row: %+v", d)
		}
		seen[d.ID] = true
	}
	// Still stalled, the cursor makes no progress and says so: busy.
	if _, err := backend.ListDeliverables(appwire.DeliverablesQuery{Scope: "agent", Agent: agent.ID, Limit: 100, Cursor: page.Next}); !hasCode(err, appwire.CodeBusy) {
		t.Fatalf("a stalled page with nothing to give: %v", err)
	}
	if _, err := backend.CountDeliverables(appwire.DeliverablesQuery{Scope: "agent", Agent: agent.ID, Cursor: page.Next}); !hasCode(err, appwire.CodeBusy) {
		t.Fatalf("a stalled count with nothing to give: %v", err)
	}
	os.Remove(stall)
	for cursor := page.Next; cursor != ""; {
		next, err := backend.ListDeliverables(appwire.DeliverablesQuery{Scope: "agent", Agent: agent.ID, Limit: 100, Cursor: cursor})
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range next.Deliverables {
			if seen[d.ID] || d.Live != "on_branch" {
				t.Fatalf("row %s: %q (seen before: %v)", d.ID, d.Live, seen[d.ID])
			}
			seen[d.ID] = true
		}
		cursor = next.Next
	}
	if len(seen) != len(want) {
		t.Fatalf("the cursor found %d of %d rows", len(seen), len(want))
	}
}

// A row git cannot decide with time to spare - the checkout is no repository
// any more - is described as unreadable, never gone and never a page that
// stops on it for ever.
func TestARowGitCannotDecideWithTimeLeftIsUnreadableNotStuck(t *testing.T) {
	backend, agent, want := manyBranchRows(t, 3, 0)
	if err := os.RemoveAll(filepath.Join(agent.Workspace, ".git")); err != nil {
		t.Fatal(err)
	}
	page, err := backend.ListDeliverables(appwire.DeliverablesQuery{Scope: "agent", Agent: agent.ID, Limit: 100})
	if err != nil || len(page.Deliverables) != len(want) || page.Next != "" {
		t.Fatalf("%d of %d rows next=%q err=%v", len(page.Deliverables), len(want), page.Next, err)
	}
	for _, d := range page.Deliverables {
		if d.Live != "unreadable" || d.Open != "" {
			t.Fatalf("an undecidable row: %q open %q", d.Live, d.Open)
		}
	}
}

// branches.list's page bounds are the session's, and every refusal the
// listing can give is a coded error, never an empty answer.
func TestBranchListRefusalsAreCodedAndBoundsAgree(t *testing.T) {
	if appwire.BranchPageDefault != session.TaskBranchPageDefault || appwire.BranchPageMax != session.TaskBranchPageMax {
		t.Fatal("the wire's page bounds drifted from the session's")
	}
	for err, code := range map[error]string{
		session.ErrTaskBranchCursor:                                   appwire.CodeBadParams,
		session.ErrTaskBranchBusy:                                     appwire.CodeBusy,
		session.ErrTaskBranchRecordsTooLarge:                          appwire.CodeTooLarge,
		fmt.Errorf("%w: torn", session.ErrTaskBranchRecords):          appwire.CodeEngine,
		fmt.Errorf("%w: broken repository", session.ErrTaskBranchGit): appwire.CodeEngine,
	} {
		if got := appBranchError(err); !hasCode(got, code) {
			t.Fatalf("%v answered %v, want %s", err, got, code)
		}
	}
	raw, err := json.Marshal(appwire.TaskBranch{Name: "task/a-1", Head: strings.Repeat("a", 40)})
	if err != nil || strings.Contains(string(raw), "files") || strings.Contains(string(raw), "added") {
		t.Fatalf("unknown counts on the wire: %s %v", raw, err)
	}
}

func TestAppReadDeliverableUndecidedSourceIsBusyNotMissing(t *testing.T) {
	w := newBranchWorld(t)
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte("#!/bin/sh\nexec /bin/sleep 5\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	done := make(chan struct{})
	server := appwire.NewServer(outW)
	go func() {
		defer close(done)
		defer outW.Close()
		_ = server.Serve(context.Background(), inR, appwire.Hello{Version: "test"}, w.backend)
	}()
	t.Cleanup(func() { _ = inW.Close(); _ = outR.Close(); <-done })
	scanner := bufio.NewScanner(outR)
	if !scanner.Scan() {
		t.Fatal("no hello")
	}
	dest := t.TempDir()
	for n, method := range []string{"deliverables.read", "deliverables.keep"} {
		request := map[string]any{"id": n + 1, "method": method, "params": map[string]any{"agent": w.agent, "id": w.id, "dir": dest, "max_bytes": 1024}}
		encoded, _ := json.Marshal(request)
		if _, err := inW.Write(append(encoded, '\n')); err != nil {
			t.Fatal(err)
		}
		var result struct {
			ID    int            `json:"id"`
			Error *appwire.Error `json:"error"`
		}
		for scanner.Scan() {
			if err := json.Unmarshal(scanner.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if result.ID == n+1 {
				break
			}
		}
		if result.ID != n+1 || result.Error == nil || result.Error.Code != appwire.CodeBusy {
			t.Fatalf("%s: %s (%v)", method, scanner.Bytes(), scanner.Err())
		}
	}
	if entries, _ := os.ReadDir(dest); len(entries) != 0 {
		t.Fatalf("undecided source copied: %v", entries)
	}

}

func TestAppKeepCanceledAdmissionIsBusyNotUncertain(t *testing.T) {
	err := appKeepError(errors.Join(session.ErrMediaBusy, context.Canceled))
	var wire *appwire.Error
	if !errors.As(err, &wire) || wire.Code != appwire.CodeBusy {
		t.Fatalf("canceled before copy: %v", err)
	}
	err = appKeepError(errors.Join(session.ErrKeepUnconfirmed, session.ErrMediaBusy))
	if !errors.As(err, &wire) || wire.Code != appwire.CodeUncertain {
		t.Fatalf("actual uncertain copy must stay uncertain: %v", err)
	}
}
