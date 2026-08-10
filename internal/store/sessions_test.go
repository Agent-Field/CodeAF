package store

import (
	"path/filepath"
	"testing"
	"time"
)

func TestASessionIsMintedByTheFirstMessageThatNamesIt(t *testing.T) {
	s := openThreadStore(t)

	first, err := s.PostMessage(Message{SessionID: "chat-1", Role: RoleUser, Body: "review the PR"})
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	session, ok, err := s.Session("chat-1")
	if err != nil || !ok {
		t.Fatalf("read session: %v (found %v)", err, ok)
	}
	if !session.Created.Equal(first.Time) || !session.LastActive.Equal(first.Time) {
		t.Fatalf("session times are %v/%v, want the first message's %v",
			session.Created, session.LastActive, first.Time)
	}

	later, err := s.PostMessage(Message{SessionID: "chat-1", Role: RoleAgent, Body: "on it"})
	if err != nil {
		t.Fatalf("post reply: %v", err)
	}
	session, _, err = s.Session("chat-1")
	if err != nil {
		t.Fatal(err)
	}
	if !session.Created.Equal(first.Time) {
		t.Fatalf("the birthday moved to %v, want the first message's %v", session.Created, first.Time)
	}
	if session.LastActive.Before(later.Time) {
		t.Fatalf("last active is %v, want the newest message's %v", session.LastActive, later.Time)
	}

	// A message posted to no room in particular is not a room.
	if _, err := s.PostMessage(Message{Role: RoleSystem, Body: "recovered"}); err != nil {
		t.Fatalf("post unhomed: %v", err)
	}
	sessions, err := s.Sessions()
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(sessions) != 1 || sessions[0].ID != "chat-1" {
		t.Fatalf("sessions = %+v, want only the one that was spoken in", sessions)
	}
	if _, ok, err := s.Session(""); err != nil || ok {
		t.Fatalf("the empty session reports found=%v err=%v, want neither", ok, err)
	}
}

func TestSessionsListNewestActiveFirstAndTakeASurfaceOnce(t *testing.T) {
	s := openThreadStore(t)
	if _, err := s.PostMessage(Message{SessionID: "older", Role: RoleUser, Body: "first"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PostMessage(Message{SessionID: "newer", Role: RoleUser, Body: "second"}); err != nil {
		t.Fatal(err)
	}
	sessions, err := s.Sessions()
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 2 || sessions[0].ID != "newer" || sessions[1].ID != "older" {
		t.Fatalf("sessions = %+v, want the most recently active first", sessions)
	}

	// The message path knows no surface; a lens that names itself fills it in,
	// and a later one does not overwrite it.
	tagged, err := s.EnsureSession("older", "tui")
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if tagged.Surface != "tui" {
		t.Fatalf("surface = %q, want the one that claimed it", tagged.Surface)
	}
	again, err := s.EnsureSession("older", "web")
	if err != nil {
		t.Fatalf("re-ensure: %v", err)
	}
	if again.Surface != "tui" {
		t.Fatalf("surface = %q, want the first answer kept", again.Surface)
	}
	if !again.Created.Equal(tagged.Created) {
		t.Fatalf("ensure moved the birthday to %v, want %v", again.Created, tagged.Created)
	}

	// An activity mark only ever rises, whatever order two writers commit in.
	past := tagged.LastActive.Add(-time.Hour)
	if err := s.TouchSession("older", past); err != nil {
		t.Fatalf("touch: %v", err)
	}
	stayed, _, err := s.Session("older")
	if err != nil {
		t.Fatal(err)
	}
	if stayed.LastActive.Before(tagged.LastActive) {
		t.Fatalf("last active fell back to %v, want no earlier than %v", stayed.LastActive, tagged.LastActive)
	}
	if _, err := s.EnsureSession("  ", "tui"); err == nil {
		t.Fatal("ensuring a nameless session should be refused")
	}
}

// Projections are rebuildable, so an existing database needs no ceremony: the
// rows that were never minted are filled in when it is opened, and a rebuild
// from the journal produces exactly the same table.
func TestSessionsBackfillOnOpenAndSurviveARebuild(t *testing.T) {
	path := filepath.Join(t.TempDir(), "graph.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	first, err := s.PostMessage(Message{SessionID: "chat-1", Role: RoleUser, Body: "review the PR"})
	if err != nil {
		t.Fatal(err)
	}
	last, err := s.PostMessage(Message{SessionID: "chat-1", Role: RoleAgent, Body: "on it"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.PostMessage(Message{SessionID: "chat-2", Role: RoleUser, Body: "and this"}); err != nil {
		t.Fatal(err)
	}
	// The state an older build leaves behind: messages naming sessions that
	// have no row.
	if _, err := s.db.Exec(`DELETE FROM sessions`); err != nil {
		t.Fatalf("simulate an older database: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	sessions, err := reopened.Sessions()
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 2 {
		t.Fatalf("backfill produced %d sessions, want one per session_id in the messages", len(sessions))
	}
	filled, ok, err := reopened.Session("chat-1")
	if err != nil || !ok {
		t.Fatalf("read backfilled session: %v (found %v)", err, ok)
	}
	if !filled.Created.Equal(first.Time) || !filled.LastActive.Equal(last.Time) {
		t.Fatalf("backfilled times are %v/%v, want %v/%v",
			filled.Created, filled.LastActive, first.Time, last.Time)
	}

	if err := reopened.Rebuild(); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	rebuilt, ok, err := reopened.Session("chat-1")
	if err != nil || !ok {
		t.Fatalf("read rebuilt session: %v (found %v)", err, ok)
	}
	if rebuilt != filled {
		t.Fatalf("a rebuild produced %+v, want the same row %+v", rebuilt, filled)
	}
	after, err := reopened.Sessions()
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(sessions) {
		t.Fatalf("a rebuild produced %d sessions, want %d", len(after), len(sessions))
	}
}

// The resume point is per room, because one number cannot state it for two: a
// reply in either would carry it past the other's unanswered rows.
func TestResumePointsAreScopedToOneRoom(t *testing.T) {
	s := openThreadStore(t)
	waiting, err := s.PostMessage(Message{SessionID: "that-window", Role: RoleUser, Body: "what is running?"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.PostMessage(Message{SessionID: "this-window", Role: RoleUser, Body: "totals?"}); err != nil {
		t.Fatal(err)
	}
	answered, err := s.PostMessage(Message{SessionID: "this-window", Role: RoleAgent, Body: "12,004."})
	if err != nil {
		t.Fatal(err)
	}

	global, err := s.LastNonUserMessageSeq()
	if err != nil {
		t.Fatal(err)
	}
	if global != answered.Seq {
		t.Fatalf("the journal-wide resume point is %d, want the newest reply %d", global, answered.Seq)
	}
	if global <= waiting.Seq {
		t.Fatal("this test only means something when the shared number sits above the waiting row")
	}

	scoped, err := s.SessionLastNonUserMessageSeq("that-window")
	if err != nil {
		t.Fatal(err)
	}
	if scoped != 0 {
		t.Fatalf("the waiting room resumes at %d, want the beginning of its own thread", scoped)
	}
	scoped, err = s.SessionLastNonUserMessageSeq("this-window")
	if err != nil {
		t.Fatal(err)
	}
	if scoped != answered.Seq {
		t.Fatalf("the answered room resumes at %d, want its own reply %d", scoped, answered.Seq)
	}

	cursors, err := s.SessionMessageCursors()
	if err != nil {
		t.Fatal(err)
	}
	if len(cursors) != 2 {
		t.Fatalf("cursors = %v, want one per room", cursors)
	}
	if cursors["that-window"] != 0 || cursors["this-window"] != answered.Seq {
		t.Fatalf("cursors = %v, want the waiting room at 0 and the answered one at %d", cursors, answered.Seq)
	}

	// A room nobody has spoken in yet, and the unhomed rows, both answer
	// without error rather than borrowing someone else's watermark.
	scoped, err = s.SessionLastNonUserMessageSeq("never-opened")
	if err != nil || scoped != 0 {
		t.Fatalf("an unknown room resumes at %d (%v), want the beginning", scoped, err)
	}
	if _, err := s.PostMessage(Message{Role: RoleSystem, Body: "recovered"}); err != nil {
		t.Fatal(err)
	}
	cursors, err = s.SessionMessageCursors()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := cursors[""]; !ok {
		t.Fatalf("cursors = %v, want the unhomed rows to carry their own resume point", cursors)
	}
}
