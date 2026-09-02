package main

import (
	"path/filepath"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// reopen closes one handle on the graph and opens another over the same file,
// which is as close to a relaunch as a test can get without a process: nothing
// in memory survives it, so anything the next launch knows it knows from the
// journal.
func reopen(t *testing.T, graph *store.Store, path string) *store.Store {
	t.Helper()
	if err := graph.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	return reopened
}

// The morning after. Yesterday's thread, yesterday's questions and yesterday's
// overnight deliverable all hang off the session id, so the only thing that
// makes them reachable is coming back to it.
func TestChatSessionResumesAcrossARelaunch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "graph.db")
	graph, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	yesterday := "yesterday-session"
	if _, err := graph.TouchSeen("tui", yesterday, store.SeenAttached); err != nil {
		t.Fatal(err)
	}
	if _, err := graph.PostMessage(store.Message{
		SessionID: yesterday, Role: store.RoleUser, Body: "audit the billing code",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := graph.TouchSeen("tui", yesterday, store.SeenDetached); err != nil {
		t.Fatal(err)
	}
	// The overnight job lands after the terminal closed, addressed — as every
	// announcement is — to the session that created it.
	if _, err := graph.PostMessage(store.Message{
		SessionID: yesterday, Role: store.RoleSystem, Body: "the billing audit is done",
	}); err != nil {
		t.Fatal(err)
	}

	graph = reopen(t, graph, path)

	resumed, err := resolveChatSession(graph, "")
	if err != nil {
		t.Fatal(err)
	}
	if resumed != yesterday {
		t.Fatalf("a bare launch minted %q instead of resuming %q", resumed, yesterday)
	}
	messages, err := graph.Messages(resumed, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 2 {
		t.Fatalf("the resumed thread carries %d messages, want the ask and the answer", len(messages))
	}
	if messages[1].Body != "the billing audit is done" {
		t.Fatalf("the overnight answer is not in the resumed thread: %q", messages[1].Body)
	}
}

// Starting over is the explicit act, and it has to be reachable both ways: the
// flag before the surface is up, /new once it is.
func TestChatSessionStartsFreshOnlyWhenAsked(t *testing.T) {
	path := filepath.Join(t.TempDir(), "graph.db")
	graph, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = graph.Close() })
	if _, err := graph.TouchSeen("tui", "yesterday-session", store.SeenAttached); err != nil {
		t.Fatal(err)
	}

	fresh, err := resolveChatSession(graph, "new")
	if err != nil {
		t.Fatal(err)
	}
	if fresh == "" || fresh == "yesterday-session" || fresh == "new" {
		t.Fatalf("--session new resolved to %q", fresh)
	}
	again, err := resolveChatSession(graph, "new")
	if err != nil {
		t.Fatal(err)
	}
	if again == fresh {
		t.Fatal("--session new handed out the same id twice")
	}
	named, err := resolveChatSession(graph, "  a-named-session  ")
	if err != nil {
		t.Fatal(err)
	}
	if named != "a-named-session" {
		t.Fatalf("an explicit --session resolved to %q", named)
	}
}

// The launch that made five untitled rooms. Two windows opened in a row, with
// nothing but the journal between them, must be ONE conversation — JOURNEY's
// law, "session resumes by default; /new starts fresh" (rail-rooms grooming
// A2). The two launches here are two openChatWindow calls over one file, which
// is what a relaunch is.
func TestTwoLaunchesInARowAreOneRoom(t *testing.T) {
	path := filepath.Join(t.TempDir(), "graph.db")
	first, err := openChatWindow(path, path, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := first.graph.PostMessage(store.Message{
		SessionID: first.session, Role: store.RoleUser, Body: "audit the billing code",
	}); err != nil {
		t.Fatal(err)
	}
	first.close()

	second, err := openChatWindow(path, path, "")
	if err != nil {
		t.Fatal(err)
	}
	defer second.close()
	if second.session != first.session {
		t.Fatalf("the second launch minted %q instead of resuming %q", second.session, first.session)
	}
	sessions, err := second.graph.Sessions()
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 {
		t.Fatalf("two launches left %d rooms in the rail, want one", len(sessions))
	}
}

// The room somebody switched INTO is the room they were in, and switching does
// not journal an attach edge — only speaking does. So the resume read follows
// the person's own words rather than the window's opening address.
func TestALaunchResumesTheRoomTheReaderSwitchedInto(t *testing.T) {
	path := filepath.Join(t.TempDir(), "graph.db")
	graph, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := graph.TouchSeen("tui", "opened-here", store.SeenAttached); err != nil {
		t.Fatal(err)
	}
	if _, err := graph.PostMessage(store.Message{
		SessionID: "opened-here", Role: store.RoleUser, Body: "morning",
	}); err != nil {
		t.Fatal(err)
	}
	// The switch: a room opened from the rail, spoken in, and never announced
	// with an attach edge of its own.
	if _, err := graph.OpenSession("switched-into", "", "tui"); err != nil {
		t.Fatal(err)
	}
	if _, err := graph.PostMessage(store.Message{
		SessionID: "switched-into", Role: store.RoleUser, Body: "and the evening's work",
	}); err != nil {
		t.Fatal(err)
	}
	graph = reopen(t, graph, path)

	resumed, err := resolveChatSession(graph, "")
	if err != nil {
		t.Fatal(err)
	}
	if resumed != "switched-into" {
		t.Fatalf("the launch came back to %q, want the room the reader was actually in", resumed)
	}
}

// Starting over reuses the empty room already standing. Two of them are the
// same row printed twice, and printing it five times is the bug A2 filed.
func TestStartingOverWalksIntoTheEmptyRoomAlreadyStanding(t *testing.T) {
	path := filepath.Join(t.TempDir(), "graph.db")
	graph, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = graph.Close() })
	if _, err := graph.PostMessage(store.Message{
		SessionID: "yesterday", Role: store.RoleUser, Body: "the billing audit",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := graph.OpenSession("empty-room", "", "tui"); err != nil {
		t.Fatal(err)
	}
	fresh, err := resolveChatSession(graph, "new")
	if err != nil {
		t.Fatal(err)
	}
	if fresh != "empty-room" {
		t.Fatalf("--session new resolved to %q, want the empty room already standing", fresh)
	}
}

// The rooms that piled up before any of this existed are taken back at the next
// launch — all but the newest empty one and the room the launch just resolved.
func TestALaunchReapsTheRoomsThatPiledUp(t *testing.T) {
	path := filepath.Join(t.TempDir(), "graph.db")
	graph, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := graph.PostMessage(store.Message{
		SessionID: "the-conversation", Role: store.RoleUser, Body: "audit the billing code",
	}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"untitled-1", "untitled-2", "untitled-3", "untitled-4", "untitled-5"} {
		if _, err := graph.OpenSession(id, "", "tui"); err != nil {
			t.Fatal(err)
		}
	}
	if err := graph.Close(); err != nil {
		t.Fatal(err)
	}

	window, err := openChatWindow(path, path, "")
	if err != nil {
		t.Fatal(err)
	}
	defer window.close()
	if window.session != "the-conversation" {
		t.Fatalf("the launch resumed %q, want the conversation", window.session)
	}
	sessions, err := window.graph.Sessions()
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 2 {
		t.Fatalf("the rail still has %d rooms, want the conversation and one empty room", len(sessions))
	}
}

// A graph nobody has ever attached to has no conversation to return to, and
// the first launch must still get a session rather than an empty string.
func TestChatSessionMintsOnAFreshGraph(t *testing.T) {
	path := filepath.Join(t.TempDir(), "graph.db")
	graph, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = graph.Close() })
	first, err := resolveChatSession(graph, "")
	if err != nil {
		t.Fatal(err)
	}
	if first == "" {
		t.Fatal("a fresh graph produced an empty session id")
	}
}
