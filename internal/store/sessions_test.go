package store

import (
	"errors"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

// sameSession is what `==` was before a room carried a tag list: every field,
// with the two times compared as instants rather than as structs.
func sameSession(a, b Session) bool {
	return a.ID == b.ID && a.Title == b.Title && a.Surface == b.Surface &&
		slices.Equal(a.Tags, b.Tags) &&
		a.Created.Equal(b.Created) && a.LastActive.Equal(b.LastActive)
}

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
	if !sameSession(rebuilt, filled) {
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

// A room is a place before it is a transcript. The thread switcher opens one
// and it has to exist — listed, readable, and datable — with nothing said in
// it yet.
func TestAnEmptyRoomExistsBeforeItsFirstMessage(t *testing.T) {
	s := openThreadStore(t)

	opened, err := s.OpenSession("chat-new", "Rewrite the importer", "tui")
	if err != nil {
		t.Fatalf("open session: %v", err)
	}
	if opened.ID != "chat-new" || opened.Title != "Rewrite the importer" || opened.Surface != "tui" {
		t.Fatalf("opened room is %+v, want its id, title and surface", opened)
	}
	if !opened.Created.Equal(opened.LastActive) {
		t.Fatalf("a room born at %v is last active at %v, want its own birthday", opened.Created, opened.LastActive)
	}

	listed, err := s.Sessions()
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || !sameSession(listed[0], opened) {
		t.Fatalf("sessions = %+v, want the empty room %+v", listed, opened)
	}
	read, ok, err := s.Session("chat-new")
	if err != nil || !ok || !sameSession(read, opened) {
		t.Fatalf("read empty room = %+v (found %v, err %v), want %+v", read, ok, err, opened)
	}

	// Nothing has been said in it, so it owes no reply and holds no cursor.
	messages, err := s.Messages("chat-new", 0, 0)
	if err != nil || len(messages) != 0 {
		t.Fatalf("empty room has %d messages (%v), want none", len(messages), err)
	}
	resume, err := s.SessionLastNonUserMessageSeq("chat-new")
	if err != nil || resume != 0 {
		t.Fatalf("empty room resumes at %d (%v), want the beginning of its own thread", resume, err)
	}
	cursors, err := s.SessionMessageCursors()
	if err != nil {
		t.Fatal(err)
	}
	if _, present := cursors[""]; present || len(cursors) != 0 {
		t.Fatalf("cursors = %v, want none until something is said", cursors)
	}

	// And when it is finally spoken in, it is the same room: the birthday is
	// the opening, the activity mark is the message.
	said, err := s.PostMessage(Message{SessionID: "chat-new", Role: RoleUser, Body: "start here"})
	if err != nil {
		t.Fatal(err)
	}
	spoken, _, err := s.Session("chat-new")
	if err != nil {
		t.Fatal(err)
	}
	if !spoken.Created.Equal(opened.Created) {
		t.Fatalf("the birthday moved to %v, want the opening %v", spoken.Created, opened.Created)
	}
	if spoken.LastActive.Before(said.Time) {
		t.Fatalf("last active is %v, want the message's %v", spoken.LastActive, said.Time)
	}
	if spoken.Title != opened.Title || spoken.Surface != opened.Surface {
		t.Fatalf("speaking in the room rewrote it to %+v, want title %q surface %q",
			spoken, opened.Title, opened.Surface)
	}
}

// Opening is a mint, not a touch: a room that already exists has already been
// opened, whether by an earlier open or by the message that named it.
func TestOpeningAnExistingRoomJournalsNothing(t *testing.T) {
	s := openThreadStore(t)

	first, err := s.OpenSession("chat-1", "First name", "tui")
	if err != nil {
		t.Fatal(err)
	}
	before := countEvents(t, s, EventSessionOpened)
	again, err := s.OpenSession("  chat-1  ", "Second name", "web")
	if err != nil {
		t.Fatalf("re-open: %v", err)
	}
	if !sameSession(again, first) {
		t.Fatalf("re-opening produced %+v, want the room as it stands %+v", again, first)
	}
	if after := countEvents(t, s, EventSessionOpened); after != before {
		t.Fatalf("re-opening journaled %d birthdays, want %d", after, before)
	}

	// A room born from a message is a room; opening it is the same no-op, and
	// it keeps the birthday its first message gave it.
	born, err := s.PostMessage(Message{SessionID: "chat-2", Role: RoleUser, Body: "spoken into being"})
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := s.OpenSession("chat-2", "Late title", "tui")
	if err != nil {
		t.Fatalf("open a spoken-in room: %v", err)
	}
	if !reopened.Created.Equal(born.Time) || reopened.Title != "" {
		t.Fatalf("opening a spoken-in room produced %+v, want the message's birthday %v and no title",
			reopened, born.Time)
	}
	if countEvents(t, s, EventSessionOpened) != before {
		t.Fatal("opening a room that messages already minted journaled a birthday")
	}

	if _, err := s.OpenSession("   ", "", "tui"); err == nil {
		t.Fatal("opening a nameless room should be refused")
	}
	if _, err := s.OpenSession("chat-3", strings.Repeat("x", MaxSessionTitleBytes*2), ""); err != nil {
		t.Fatalf("an over-long title should be bounded, not refused: %v", err)
	}
	long, _, err := s.Session("chat-3")
	if err != nil {
		t.Fatal(err)
	}
	if len(long.Title) > MaxSessionTitleBytes {
		t.Fatalf("title is %d bytes, want at most %d", len(long.Title), MaxSessionTitleBytes)
	}
}

// Journal-is-truth: an empty room is a journal fact, so a rebuild that drops
// the sessions table has to put it back. Without the replay arm a rebuild
// would silently delete every room nobody had spoken in yet.
func TestEmptyRoomsSurviveARebuild(t *testing.T) {
	s := openThreadStore(t)
	opened, err := s.OpenSession("chat-empty", "Nothing said yet", "tui")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.PostMessage(Message{SessionID: "chat-spoken", Role: RoleUser, Body: "hello"}); err != nil {
		t.Fatal(err)
	}
	beforeRebuild := readSessionRows(t, s)

	if err := s.Rebuild(); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	afterRebuild := readSessionRows(t, s)
	if !reflect.DeepEqual(beforeRebuild, afterRebuild) {
		t.Fatalf("a rebuild produced %v, want the same rows %v", afterRebuild, beforeRebuild)
	}
	rebuilt, ok, err := s.Session("chat-empty")
	if err != nil || !ok {
		t.Fatalf("the empty room did not survive the rebuild: found %v, err %v", ok, err)
	}
	if !sameSession(rebuilt, opened) {
		t.Fatalf("the rebuilt empty room is %+v, want %+v", rebuilt, opened)
	}
}

// A rename is not a birthday and not activity: it changes the one field it
// says it changes and leaves everything else — including the room's own
// existence, which OpenSession refuses to grant twice — alone.
func TestRenameSessionRetitlesWithoutTouchingBirthdayOrActivity(t *testing.T) {
	s := openThreadStore(t)

	opened, err := s.OpenSession("chat-1", "First name", "tui")
	if err != nil {
		t.Fatal(err)
	}
	renamed, err := s.RenameSession("chat-1", "Second name")
	if err != nil {
		t.Fatalf("rename: %v", err)
	}
	if renamed.Title != "Second name" {
		t.Fatalf("title = %q, want %q", renamed.Title, "Second name")
	}
	if !renamed.Created.Equal(opened.Created) {
		t.Fatalf("rename moved the birthday to %v, want %v", renamed.Created, opened.Created)
	}
	if !renamed.LastActive.Equal(opened.LastActive) {
		t.Fatalf("rename moved the activity mark to %v, want %v — a rename is not activity",
			renamed.LastActive, opened.LastActive)
	}
	read, _, err := s.Session("chat-1")
	if err != nil || !sameSession(read, renamed) {
		t.Fatalf("read after rename = %+v (err %v), want %+v", read, err, renamed)
	}

	// Renaming works on a room a message minted too, and on a room with
	// nothing said in it yet (open, no message, rename).
	if _, err := s.PostMessage(Message{SessionID: "chat-2", Role: RoleUser, Body: "hi"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RenameSession("chat-2", "Named later"); err != nil {
		t.Fatalf("rename a message-born room: %v", err)
	}
	if _, err := s.OpenSession("chat-empty", "", "tui"); err != nil {
		t.Fatal(err)
	}
	stillEmpty, err := s.RenameSession("chat-empty", "Named before anyone spoke")
	if err != nil {
		t.Fatalf("rename an empty room: %v", err)
	}
	messages, err := s.Messages("chat-empty", 0, 0)
	if err != nil || len(messages) != 0 {
		t.Fatalf("renaming said something in the room: %d messages (%v)", len(messages), err)
	}
	if stillEmpty.Title != "Named before anyone spoke" {
		t.Fatalf("title = %q, want the rename to have landed", stillEmpty.Title)
	}

	if _, err := s.RenameSession("  ", "anything"); err == nil {
		t.Fatal("renaming a nameless room should be refused")
	}
	if _, err := s.RenameSession("never-opened", "anything"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("renaming a room that does not exist returned %v, want %v", err, ErrNotFound)
	}
}

// RenameSession bounds an over-long title instead of refusing it, the same
// promise OpenSession makes: what a person types into a rename box should
// never fail to save.
func TestRenameSessionBoundsAnOverLongTitle(t *testing.T) {
	s := openThreadStore(t)
	if _, err := s.OpenSession("chat-1", "short", "tui"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RenameSession("chat-1", strings.Repeat("x", MaxSessionTitleBytes*2)); err != nil {
		t.Fatalf("an over-long rename should be bounded, not refused: %v", err)
	}
	long, _, err := s.Session("chat-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(long.Title) > MaxSessionTitleBytes {
		t.Fatalf("title is %d bytes, want at most %d", len(long.Title), MaxSessionTitleBytes)
	}
}

// Renaming to the title a room already has is a no-op fact, not a new one:
// two switchers racing to set the identical name must not both mint a
// journal entry for one true state.
func TestRenamingToTheSameTitleJournalsNothing(t *testing.T) {
	s := openThreadStore(t)
	opened, err := s.OpenSession("chat-1", "Same name", "tui")
	if err != nil {
		t.Fatal(err)
	}
	before := countEvents(t, s, EventSessionRenamed)

	same, err := s.RenameSession("  chat-1  ", "Same name")
	if err != nil {
		t.Fatalf("rename to the same title: %v", err)
	}
	if !sameSession(same, opened) {
		t.Fatalf("renaming to the same title produced %+v, want the room as it stands %+v", same, opened)
	}
	if after := countEvents(t, s, EventSessionRenamed); after != before {
		t.Fatalf("renaming to the same title journaled %d renames, want %d", after, before)
	}
}

// Replay-compat for the rename event: an open-then-rename journal replays to
// the renamed title, and a rename that landed before any message was ever
// posted survives the same way an empty room does.
func TestSessionRenameSurvivesARebuild(t *testing.T) {
	s := openThreadStore(t)
	if _, err := s.OpenSession("chat-1", "Original", "tui"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RenameSession("chat-1", "Renamed"); err != nil {
		t.Fatal(err)
	}
	// Rename-before-any-message: an empty room, renamed, still empty.
	if _, err := s.OpenSession("chat-empty", "", "tui"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RenameSession("chat-empty", "Named while empty"); err != nil {
		t.Fatal(err)
	}
	beforeRebuild := readSessionRows(t, s)

	if err := s.Rebuild(); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	afterRebuild := readSessionRows(t, s)
	if !reflect.DeepEqual(beforeRebuild, afterRebuild) {
		t.Fatalf("a rebuild produced %v, want the identical rows %v", afterRebuild, beforeRebuild)
	}

	renamed, ok, err := s.Session("chat-1")
	if err != nil || !ok {
		t.Fatalf("read rebuilt room: %v (found %v)", err, ok)
	}
	if renamed.Title != "Renamed" {
		t.Fatalf("rebuilt title = %q, want %q", renamed.Title, "Renamed")
	}
	stillEmpty, ok, err := s.Session("chat-empty")
	if err != nil || !ok {
		t.Fatalf("read rebuilt empty room: %v (found %v)", err, ok)
	}
	if stillEmpty.Title != "Named while empty" {
		t.Fatalf("rebuilt empty-room title = %q, want %q", stillEmpty.Title, "Named while empty")
	}
	messages, err := s.Messages("chat-empty", 0, 0)
	if err != nil || len(messages) != 0 {
		t.Fatalf("the rebuilt empty room has %d messages (%v), want none", len(messages), err)
	}
}

// Replay-compat: a journal written before session_renamed existed contains
// only message_posted (and, after the sibling lane landed, session_opened)
// events, and replays to the byte-identical sessions table it always
// replayed to. Nothing this lane added may appear in, or change, an old
// journal.
func TestOldJournalsReplayToIdenticalSessionsAcrossRename(t *testing.T) {
	s := openThreadStore(t)
	if _, err := s.PostMessage(Message{SessionID: "chat-1", Role: RoleUser, Body: "review the PR"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.OpenSession("chat-2", "Opened, never renamed", "tui"); err != nil {
		t.Fatal(err)
	}

	journalBefore := readEventRows(t, s)
	if countEvents(t, s, EventSessionRenamed) != 0 {
		t.Fatal("the fixture contains a session_renamed event; it is not a pre-rename journal")
	}
	sessionsBefore := readSessionRows(t, s)

	if err := s.Rebuild(); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if journalAfter := readEventRows(t, s); !reflect.DeepEqual(journalBefore, journalAfter) {
		t.Fatalf("the rebuild rewrote the journal:\n after %v\n want %v", journalAfter, journalBefore)
	}
	sessionsAfter := readSessionRows(t, s)
	if !reflect.DeepEqual(sessionsBefore, sessionsAfter) {
		t.Fatalf("a pre-rename journal replayed to %v, want the identical %v", sessionsAfter, sessionsBefore)
	}
}

// Replay-compat: a journal written before this event existed contains only the
// kinds it always contained, and replays to the byte-identical sessions table
// it always replayed to. The proof is the whole projection compared row for
// row, plus the journal itself compared event for event — nothing this lane
// added may appear in, or change, an old journal.
func TestOldJournalsReplayToIdenticalSessions(t *testing.T) {
	s := openThreadStore(t)

	// Exactly the writes a pre-session_opened build could make.
	if _, err := s.PostMessage(Message{SessionID: "chat-1", Role: RoleUser, Body: "review the PR"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PostMessage(Message{SessionID: "chat-1", Role: RoleAgent, Body: "on it"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PostMessage(Message{SessionID: "chat-2", Role: RoleUser, Body: "and this"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PostMessage(Message{Role: RoleSystem, Body: "recovered"}); err != nil {
		t.Fatal(err)
	}

	journalBefore := readEventRows(t, s)
	if countEvents(t, s, EventSessionOpened) != 0 {
		t.Fatal("the old-journal fixture contains a session_opened event; it is not an old journal")
	}
	sessionsBefore := readSessionRows(t, s)

	if err := s.Rebuild(); err != nil {
		t.Fatalf("rebuild: %v", err)
	}

	if journalAfter := readEventRows(t, s); !reflect.DeepEqual(journalBefore, journalAfter) {
		t.Fatalf("the rebuild rewrote the journal:\n after %v\n want %v", journalAfter, journalBefore)
	}
	sessionsAfter := readSessionRows(t, s)
	if !reflect.DeepEqual(sessionsBefore, sessionsAfter) {
		t.Fatalf("an old journal replayed to %v, want the identical projection %v",
			sessionsAfter, sessionsBefore)
	}
	// Adding a room to that same old journal changes nothing about it: the new
	// event only ever adds a row, and every row the messages minted comes back
	// from replay exactly as it was.
	if _, err := s.OpenSession("chat-3", "Opened later", "tui"); err != nil {
		t.Fatal(err)
	}
	if err := s.Rebuild(); err != nil {
		t.Fatalf("rebuild after opening a room: %v", err)
	}
	mixed := readSessionRows(t, s)
	if len(mixed) != len(sessionsBefore)+1 {
		t.Fatalf("a mixed journal replayed to %d rooms, want the old %d plus one",
			len(mixed), len(sessionsBefore))
	}
	if !reflect.DeepEqual(mixed[:len(sessionsBefore)], sessionsBefore) {
		t.Fatalf("the message-born rooms replayed to %v, want the identical %v",
			mixed[:len(sessionsBefore)], sessionsBefore)
	}
}

// The projection writes that are NOT journaled are stated here rather than
// discovered later: EnsureSession's surface claim and TouchSession's activity
// mark write the table directly, so a rebuild — which derives the table from
// the journal alone — does not reproduce them. That predates this event and is
// deliberately not changed by it; a lens that wants a durable surface opens the
// room with one. The day those writes gain events of their own, this test is
// where the old boundary is written down.
func TestUnjournaledSessionWritesDoNotSurviveARebuild(t *testing.T) {
	s := openThreadStore(t)
	if _, err := s.PostMessage(Message{SessionID: "chat-1", Role: RoleUser, Body: "review the PR"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.EnsureSession("chat-1", "tui"); err != nil {
		t.Fatal(err)
	}
	if err := s.Rebuild(); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	replayed, ok, err := s.Session("chat-1")
	if err != nil || !ok {
		t.Fatalf("read replayed session: %v (found %v)", err, ok)
	}
	if replayed.Surface != "" {
		t.Fatalf("surface = %q; EnsureSession is journaled now, and this test needs rewriting", replayed.Surface)
	}

	// A room opened with a surface keeps it, because that one IS journaled.
	if _, err := s.OpenSession("chat-2", "", "tui"); err != nil {
		t.Fatal(err)
	}
	if err := s.Rebuild(); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	durable, _, err := s.Session("chat-2")
	if err != nil {
		t.Fatal(err)
	}
	if durable.Surface != "tui" {
		t.Fatalf("an opened room replayed with surface %q, want %q", durable.Surface, "tui")
	}
}

// readSessionRows renders the sessions projection as comparable text, so a
// test can assert on the whole table rather than on the fields it remembered
// to read back.
func readSessionRows(t *testing.T, s *Store) []string {
	t.Helper()
	rows, err := s.db.Query(`
		SELECT id || '|' || title || '|' || surface || '|' || created_at || '|' || last_active_at
		FROM sessions ORDER BY id`)
	if err != nil {
		t.Fatalf("read session rows: %v", err)
	}
	defer rows.Close()
	var rendered []string
	for rows.Next() {
		var row string
		if err := rows.Scan(&row); err != nil {
			t.Fatalf("scan session row: %v", err)
		}
		rendered = append(rendered, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read session rows: %v", err)
	}
	return rendered
}

func readEventRows(t *testing.T, s *Store) []string {
	t.Helper()
	rows, err := s.db.Query(`
		SELECT seq || '|' || ts || '|' || node_id || '|' || kind || '|' || payload
		FROM events ORDER BY seq`)
	if err != nil {
		t.Fatalf("read events: %v", err)
	}
	defer rows.Close()
	var rendered []string
	for rows.Next() {
		var row string
		if err := rows.Scan(&row); err != nil {
			t.Fatalf("scan event: %v", err)
		}
		rendered = append(rendered, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read events: %v", err)
	}
	return rendered
}

func countEvents(t *testing.T, s *Store, kind EventKind) int {
	t.Helper()
	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM events WHERE kind = ?`, kind).Scan(&count); err != nil {
		t.Fatalf("count %s events: %v", kind, err)
	}
	return count
}
