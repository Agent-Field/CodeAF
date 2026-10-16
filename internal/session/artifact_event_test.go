package session

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// artifactEvents attaches a recording hub to the agent and answers what it saw.
func artifactEvents(agent *Agent) *[]ArtifactChange {
	var seen []ArtifactChange
	hub := newEventHub()
	hub.observe = func(event Event) {
		if event.Kind == EventArtifactUpdate && event.Artifact != nil {
			seen = append(seen, *event.Artifact)
		}
	}
	agent.mu.Lock()
	agent.hub = hub
	agent.mu.Unlock()
	return &seen
}

// A normal write of a new document announces the deliverable it recorded, once,
// after the row is on the index; an edit and a failed append announce nothing.
func TestAWrittenDocumentIsAnnouncedAfterTheAppendAndOnlyThen(t *testing.T) {
	agent, workspace, index := deliverableAgent(t, false)
	session := agent.journalID()
	seen := artifactEvents(agent)

	ctx := withCallID(withRunIdentity(context.Background()), "call_write")
	args := []byte(`{"path":"notes.md","content":"# notes\n"}`)
	if out, bad, err := beltTool(t, agent, "write").Execute(ctx, args); err != nil || bad {
		t.Fatalf("write: %v %s", err, out)
	}
	rows := listDeliverables(t, index, DeliverableQuery{Sessions: map[string]bool{session: true}}, 10)
	if len(rows) != 1 || len(*seen) != 1 {
		t.Fatalf("rows=%d events=%d", len(rows), len(*seen))
	}
	got := (*seen)[0]
	if got.ID != rows[0].ID || got.Kind != DeliverableMarkdown || got.Run != rows[0].Fact.Run || got.Run == "" {
		t.Fatalf("event %+v does not name the recorded deliverable %+v", got, rows[0].Fact)
	}
	// the event names something a fresh reader can already find
	if _, err := FindDeliverable(index, session, got.ID, time.Time{}); err != nil {
		t.Fatalf("event preceded the commit: %v", err)
	}

	// an edit of the same file, and a file of another kind: nothing
	beltTool(t, agent, "write").Execute(ctx, []byte(`{"path":"notes.md","content":"# edited\n"}`))
	beltTool(t, agent, "write").Execute(ctx, []byte(`{"path":"main.go","content":"package main\n"}`))
	if len(*seen) != 1 {
		t.Fatalf("an edit or an unsupported kind announced a deliverable: %+v", *seen)
	}

	// a failed append: the index is a directory, the write still happens, no event
	if err := os.Remove(index); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(index, 0o700); err != nil {
		t.Fatal(err)
	}
	beltTool(t, agent, "write").Execute(ctx, []byte(`{"path":"other.md","content":"# other\n"}`))
	if _, err := os.Stat(filepath.Join(workspace, "other.md")); err != nil {
		t.Fatal("the write itself failed")
	}
	if len(*seen) != 1 {
		t.Fatal("a failed append announced a deliverable")
	}
}

// A generated picture announces its deliverable beside the media source event.
func TestAGeneratedPictureAnnouncesItsDeliverableToo(t *testing.T) {
	dir := t.TempDir()
	index := filepath.Join(dir, "artifacts.jsonl")
	path := mediaFile(t, dir, "made.png")
	agent, _ := newMediaAgent(t, nil, func(c *Config) { c.ArtifactsIndex = index; c.SessionFile = filepath.Join(dir, "session.jsonl") })
	var kinds []EventKind
	var change ArtifactChange
	hub := newEventHub()
	hub.observe = func(event Event) {
		switch event.Kind {
		case EventMediaUpdate:
			kinds = append(kinds, event.Kind)
		case EventArtifactUpdate:
			kinds = append(kinds, event.Kind)
			change = *event.Artifact
		}
	}
	agent.mu.Lock()
	agent.hub = hub
	agent.mu.Unlock()
	fact := GeneratedMedia(path, pngOfSize(t, 4, 3), "painter")
	fact.Run, fact.Tool = "run_actual", "call_actual"
	agent.recordMedia(Artifact{Path: path, Title: "made.png", Kind: "image", Created: time.Now()}, fact)
	if len(kinds) != 2 || change.Run != "run_actual" || change.Call != "call_actual" || change.Kind != "image" {
		t.Fatalf("kinds=%v change=%+v", kinds, change)
	}
	row, err := FindDeliverable(index, agent.journalID(), change.ID, time.Time{})
	if err != nil || row.Fact.Run != "run_actual" {
		t.Fatalf("%v %+v", err, row.Fact)
	}
}

// Nothing is announced for a row with no deliverable fact to name.
func TestNoDeliverableNoAnnouncement(t *testing.T) {
	agent, _, _ := deliverableAgent(t, false)
	seen := artifactEvents(agent)
	agent.emitArtifactChange(Artifact{Path: "/x", Title: "x", Kind: "image"})
	agent.emitArtifactChange(Artifact{Path: "/x", Title: "x", Kind: "image", Deliver: &DeliverableFact{ID: "dlv_x", Update: true}})
	if len(*seen) != 0 {
		t.Fatalf("invented announcement: %+v", *seen)
	}
}
