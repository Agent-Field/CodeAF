package store

import (
	"path/filepath"
	"testing"
)

// The room a launch comes back to is the room the PERSON was last in, and the
// case that decides it is a machine speaking somewhere else afterwards. An
// overnight job delivering into the room that commissioned it raises that
// room's activity mark; coming back to it the next morning would be the
// machine choosing where the conversation continues.
func TestLatestSessionIsWhereThePersonLastSpoke(t *testing.T) {
	s := openThreadStore(t)

	if _, err := s.PostMessage(Message{SessionID: "old", Role: RoleUser, Body: "the billing audit"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PostMessage(Message{SessionID: "here", Role: RoleUser, Body: "and now this"}); err != nil {
		t.Fatal(err)
	}
	// The overnight deliverable, addressed to the room that commissioned it.
	if _, err := s.PostMessage(Message{SessionID: "old", Role: RoleAgent, Body: "the billing audit is done"}); err != nil {
		t.Fatal(err)
	}

	latest, found, err := s.LatestSession()
	if err != nil || !found {
		t.Fatalf("latest session: %v (found %v)", err, found)
	}
	if latest.ID != "here" {
		t.Fatalf("a launch would come back to %q, want the room the person last spoke in", latest.ID)
	}
}

// A store with rooms and no words in any of them still has a room to come back
// to: the newest one. Nothing but an empty journal may produce a mint.
func TestLatestSessionFallsBackToTheNewestRoomAndThenToNothing(t *testing.T) {
	s := openThreadStore(t)

	if _, _, err := s.LatestSession(); err != nil {
		t.Fatal(err)
	} else if _, found, _ := s.LatestSession(); found {
		t.Fatal("an empty journal named a room to come back to")
	}

	if _, err := s.OpenSession("first", "", "tui"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.OpenSession("second", "", "tui"); err != nil {
		t.Fatal(err)
	}
	latest, found, err := s.LatestSession()
	if err != nil || !found {
		t.Fatalf("latest session: %v (found %v)", err, found)
	}
	if latest.ID != "second" {
		t.Fatalf("the fallback named %q, want the newest room", latest.ID)
	}
}

// `+ new room` twice in a row is one room. An empty room has nothing in it to
// be older than a fresh one, so the second request is answered with the first
// room and journals nothing at all.
func TestOpenOrReuseSessionWalksIntoTheEmptyRoomAlreadyStanding(t *testing.T) {
	s := openThreadStore(t)

	first, reused, err := s.OpenOrReuseSession("room-1", "tui")
	if err != nil {
		t.Fatal(err)
	}
	if reused || first.ID != "room-1" {
		t.Fatalf("the first room was %+v (reused=%v), want a fresh mint", first, reused)
	}
	before, err := s.LatestEventSeq()
	if err != nil {
		t.Fatal(err)
	}

	second, reused, err := s.OpenOrReuseSession("room-2", "tui")
	if err != nil {
		t.Fatal(err)
	}
	if !reused || second.ID != "room-1" {
		t.Fatalf("the second request minted %+v, want the empty room already standing", second)
	}
	after, err := s.LatestEventSeq()
	if err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatalf("reusing a room journaled %d event(s); a request that changed nothing must write nothing", after-before)
	}

	// Once somebody speaks in it, it is a conversation and the next request is
	// a real mint.
	if _, err := s.PostMessage(Message{SessionID: "room-1", Role: RoleUser, Body: "hello"}); err != nil {
		t.Fatal(err)
	}
	third, reused, err := s.OpenOrReuseSession("room-3", "tui")
	if err != nil {
		t.Fatal(err)
	}
	if reused || third.ID != "room-3" {
		t.Fatalf("a spoken-in room was reused as empty: %+v (reused=%v)", third, reused)
	}
}

// A room somebody named is a place they made on purpose. Emptying it is not the
// same as never having used it, so the title keeps it out of both doors.
func TestANamedRoomIsNeverReusedOrReaped(t *testing.T) {
	s := openThreadStore(t)
	if _, err := s.OpenSession("kept", "quarterly numbers", "tui"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.OpenSession("empty-1", "", "tui"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.OpenSession("empty-2", "", "tui"); err != nil {
		t.Fatal(err)
	}

	reusedRoom, reused, err := s.OpenOrReuseSession("fresh", "tui")
	if err != nil {
		t.Fatal(err)
	}
	if !reused || reusedRoom.ID == "kept" {
		t.Fatalf("reuse handed back %+v, want an unnamed empty room", reusedRoom)
	}
	discarded, err := s.ReapEmptySessions("")
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range discarded {
		if id == "kept" {
			t.Fatal("the reap took back a room somebody had named")
		}
	}
	if _, found, err := s.Session("kept"); err != nil || !found {
		t.Fatalf("the named room is gone: %v (found %v)", err, found)
	}
}

// The five untitled rooms. The reap takes back every empty unnamed room except
// the newest one and the one this launch resolved — a window may not open onto
// a row it is about to remove — and it survives a rebuild, which is why it
// journals instead of deleting quietly.
func TestReapEmptySessionsLeavesOneRoomAndSurvivesARebuild(t *testing.T) {
	path := filepath.Join(t.TempDir(), "graph.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })

	for _, id := range []string{"room-1", "room-2", "room-3", "room-4", "room-5"} {
		if _, err := s.OpenSession(id, "", "tui"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.PostMessage(Message{SessionID: "spoken", Role: RoleUser, Body: "the real conversation"}); err != nil {
		t.Fatal(err)
	}

	discarded, err := s.ReapEmptySessions("room-2")
	if err != nil {
		t.Fatal(err)
	}
	if len(discarded) != 3 {
		t.Fatalf("the reap took back %v, want three of the five empty rooms", discarded)
	}
	sessions, err := s.Sessions()
	if err != nil {
		t.Fatal(err)
	}
	left := map[string]bool{}
	for _, session := range sessions {
		left[session.ID] = true
	}
	if len(left) != 3 || !left["spoken"] || !left["room-2"] {
		t.Fatalf("the rail would still show %v, want the conversation, the kept room and one empty room", left)
	}

	if err := s.Rebuild(); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	rebuilt, err := s.Sessions()
	if err != nil {
		t.Fatal(err)
	}
	if len(rebuilt) != len(sessions) {
		t.Fatalf("a rebuild resurrected the reaped rooms: %d rows, want %d", len(rebuilt), len(sessions))
	}
	for _, id := range discarded {
		if _, found, err := s.Session(id); err != nil || found {
			t.Fatalf("%q came back from the journal (found %v, err %v)", id, found, err)
		}
	}
}

// One empty room is not an accumulation, so there is nothing to reap.
func TestReapEmptySessionsLeavesASingleEmptyRoomAlone(t *testing.T) {
	s := openThreadStore(t)
	if _, err := s.OpenSession("only", "", "tui"); err != nil {
		t.Fatal(err)
	}
	discarded, err := s.ReapEmptySessions("")
	if err != nil {
		t.Fatal(err)
	}
	if len(discarded) != 0 {
		t.Fatalf("the reap took back %v, want the one empty room left standing", discarded)
	}
}
