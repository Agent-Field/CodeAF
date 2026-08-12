package store

import "testing"

func sayInRoom(t *testing.T, s *Store, sessionID string, roles ...Role) {
	t.Helper()
	for _, role := range roles {
		if _, err := s.PostMessage(Message{SessionID: sessionID, Role: role, Body: "something said in " + sessionID}); err != nil {
			t.Fatalf("post in %s: %v", sessionID, err)
		}
	}
}

// The read behind the room-name backfill answers one precise question: which
// rooms would a naming pass still have something to say about. A room nobody
// answered in is not a conversation yet; a room somebody named is theirs.
func TestUnnamedSessionsWithExchangeFindsOnlyRealConversations(t *testing.T) {
	s := openThreadStore(t)
	sayInRoom(t, s, "both-sides", RoleUser, RoleAgent)
	sayInRoom(t, s, "asked-only", RoleUser)
	sayInRoom(t, s, "machine-only", RoleSystem, RoleAgent)
	sayInRoom(t, s, "named", RoleUser, RoleAgent)
	if _, err := s.RenameSession("named", "My own name"); err != nil {
		t.Fatalf("rename: %v", err)
	}

	rooms, err := s.UnnamedSessionsWithExchange(8)
	if err != nil {
		t.Fatalf("unnamed sessions: %v", err)
	}
	if len(rooms) != 1 || rooms[0].ID != "both-sides" {
		t.Fatalf("rooms = %+v, want only both-sides", rooms)
	}
}

// It is bounded and newest first, because every row it returns is a model call
// somebody has to pay for and the rooms a reader is looking at are the recent
// ones. What is left keeps its turn for the next launch.
func TestUnnamedSessionsWithExchangeIsBoundedAndNewestFirst(t *testing.T) {
	s := openThreadStore(t)
	for _, room := range []string{"first", "second", "third"} {
		sayInRoom(t, s, room, RoleUser, RoleAgent)
	}

	rooms, err := s.UnnamedSessionsWithExchange(2)
	if err != nil {
		t.Fatalf("unnamed sessions: %v", err)
	}
	if len(rooms) != 2 {
		t.Fatalf("rooms = %+v, want two", rooms)
	}
	if rooms[0].ID != "third" || rooms[1].ID != "second" {
		t.Fatalf("rooms came back %s, %s; want the most recently active first", rooms[0].ID, rooms[1].ID)
	}
	if empty, err := s.UnnamedSessionsWithExchange(0); err != nil || len(empty) != 0 {
		t.Fatalf("a limit of nothing returned %+v err=%v", empty, err)
	}
}
