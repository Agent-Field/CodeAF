package store

import (
	"fmt"
	"path/filepath"
	"testing"
)

// A settled database may later acquire missing session rows from an older
// writer. Opening it must still discover those rooms without touching rooms
// whose title, tags, surface and activity were already recorded.
func TestOpenBackfillsLaterLegacySessionsAndPreservesExistingRooms(t *testing.T) {
	path := filepath.Join(t.TempDir(), "graph.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	existing, err := s.OpenSession("existing", "Mock named room", "mock-surface")
	if err != nil {
		t.Fatal(err)
	}
	existing, err = s.RenameSessionTagged(existing.ID, existing.Title, []string{"mock"})
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{"legacy", "with spaces", "odd-\xff"}
	expected := map[string]Session{}
	for _, id := range ids {
		first, err := s.PostMessage(Message{SessionID: id, Role: RoleUser, Body: "Mock first message"})
		if err != nil {
			t.Fatal(err)
		}
		last, err := s.PostMessage(Message{SessionID: id, Role: RoleAgent, Body: "Mock last message"})
		if err != nil {
			t.Fatal(err)
		}
		expected[id] = Session{ID: id, Created: first.Time, LastActive: last.Time}
	}
	if _, err := s.PostMessage(Message{Role: RoleUser, Body: "Mock unhomed message"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	// Reopen once while settled, so a cached migration-completed flag would
	// wrongly suppress the missing rows introduced after this point.
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`DELETE FROM sessions WHERE id <> ?`, existing.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	rooms, err := s.Sessions()
	if err != nil {
		t.Fatal(err)
	}
	if len(rooms) != len(expected)+1 {
		t.Fatalf("got %d rooms, want %d", len(rooms), len(expected)+1)
	}
	for _, room := range rooms {
		want := existing
		if room.ID != existing.ID {
			var ok bool
			want, ok = expected[room.ID]
			if !ok {
				t.Fatalf("unexpected room %q", room.ID)
			}
		}
		if !sameSession(room, want) {
			t.Fatalf("room %q = %+v, want %+v", room.ID, room, want)
		}
	}
}

// BenchmarkOpenSettledSessions includes the whole exported Open/Close path.
// Fixture creation and the first legacy migration are outside the timer.
func BenchmarkOpenSettledSessions(b *testing.B) {
	for _, count := range []int{100000, 1000000} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			path := filepath.Join(b.TempDir(), "graph.db")
			s, err := Open(path)
			if err != nil {
				b.Fatal(err)
			}
			// Bulk fixture writes keep setup affordable. Events and messages have
			// matching payloads; no personal database or provider is involved.
			_, err = s.db.Exec(`WITH RECURSIVE fixture(i) AS (VALUES(1) UNION ALL SELECT i+1 FROM fixture WHERE i < ?)
    INSERT INTO events(ts,node_id,kind,payload)
    SELECT '2026-01-01T00:00:00Z','','message_posted',json_object('session_id','mock-'||(i%1000),'role','user','body','Mock message') FROM fixture`, count)
			if err != nil {
				b.Fatal(err)
			}
			_, err = s.db.Exec(`INSERT INTO messages(seq,ts,session_id,role,body)
    SELECT seq,ts,json_extract(payload,'$.session_id'),'user','Mock message' FROM events WHERE kind='message_posted'`)
			if err != nil {
				b.Fatal(err)
			}
			if err := s.Close(); err != nil {
				b.Fatal(err)
			}
			s, err = Open(path)
			if err != nil {
				b.Fatal(err)
			}
			rooms, err := s.Sessions()
			if err != nil || len(rooms) != 1000 {
				b.Fatalf("settled fixture: %d rooms, %v", len(rooms), err)
			}
			if err := s.Close(); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				s, err := Open(path)
				if err != nil {
					b.Fatal(err)
				}
				if err := s.Close(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
