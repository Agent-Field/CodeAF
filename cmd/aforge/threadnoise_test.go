package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/thread"
)

func openNoiseStore(t *testing.T) *store.Store {
	t.Helper()
	graph, err := store.Open(filepath.Join(t.TempDir(), "noise.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = graph.Close() })
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "job", Brief: "ship the thing", Stage: 2},
		{ID: "job-n1", Parent: "job", Brief: "a part of it", Stage: 1},
	}}, store.Provenance{Origin: store.OriginUser, SessionID: "room", Intent: "ship the thing"}); err != nil {
		t.Fatal(err)
	}
	return graph
}

func threadBodies(t *testing.T, graph *store.Store, sessionID string) []string {
	t.Helper()
	messages, err := graph.Messages(sessionID, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	bodies := make([]string, 0, len(messages))
	for _, message := range messages {
		bodies = append(bodies, message.Body)
	}
	return bodies
}

// The four things this surface says while a job runs — the ruler's verdict, a
// split that queued more pieces, a gap that bought another round, the files a
// failed part had already written — all go to the work's own record and none of
// them to the conversation. Live screenshots on 2026-08-11 showed every one of
// them in the thread; the cause was a session id riding beside the node.
func TestTheSurfacesRunNotesStayOffTheConversation(t *testing.T) {
	graph := openNoiseStore(t)
	for _, body := range []string{
		"ruler: median 19 turns, 4 of 18 overran — the ruler holds",
		continuationMessage(1),
		"it stopped before finishing, but it had already written these — they are yours to keep or hand to a retry:\n/tmp/x.md",
	} {
		recordOnNode(graph, "job-n1", body, store.RoleSystem)
	}

	if bodies := threadBodies(t, graph, "room"); len(bodies) != 0 {
		t.Fatalf("the conversation carries the machinery's own notes: %q", bodies)
	}
	recorded, err := graph.NodeMessages("job-n1", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(recorded) != 3 {
		t.Fatalf("the record holds %d of the 3 lines written to it", len(recorded))
	}
	for _, message := range recorded {
		if message.SessionID != "" {
			t.Fatalf("a record line carries a room: %+v", message)
		}
	}
	// Nothing to say is written nowhere: an empty recalibration report used to
	// be guarded at the call site, and the guard now lives with the writer.
	recordOnNode(graph, "job-n1", "   ", store.RoleSystem)
	recordOnNode(graph, "", "a line with no work behind it", store.RoleSystem)
	if after, _ := graph.NodeMessages("job-n1", 0, 0); len(after) != 3 {
		t.Fatalf("an empty line was recorded: %d", len(after))
	}
}

// The job board is workers writing to workers. Dropping the room from those
// lines must not stop the leaves reading them: the board is read off the job
// root BY NODE, which is exactly why the room was never needed.
func TestTheJobBoardIsStillDeliveredWithoutBeingOverheard(t *testing.T) {
	graph := openNoiseStore(t)
	if _, err := thread.Record(graph, store.Message{
		Role: store.RoleAgent, NodeID: "job", Body: jobNoteBody("a part of it", "the API returns pages, not a list"),
	}); err != nil {
		t.Fatal(err)
	}

	if bodies := threadBodies(t, graph, "room"); len(bodies) != 0 {
		t.Fatalf("a worker's note to its siblings reached the conversation: %q", bodies)
	}
	board, err := graph.NodeMessages("job", 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(board) != 1 {
		t.Fatalf("the board holds %d notes", len(board))
	}
	line, ok := jobNoteLine(board[0])
	if !ok || !strings.Contains(line, "the API returns pages") {
		t.Fatalf("the note is no longer readable as a board line: %q (ok=%v)", board[0].Body, ok)
	}
}
