package automation

import (
	"testing"
	"time"
)

// A RUN PARKED LATE KEEPS WHEN IT BEGAN TO WAIT, not its slot: the clock may
// park a run long after it was due, after codeaf was away, and a window dates
// the question by when it was put.
func TestARunParkedLateKeepsWhenItAsked(t *testing.T) {
	s, now := openStore(t)
	a := routine("1h")
	a.Trust = TrustAsksFirst
	saved, err := s.Create(a)
	if err != nil {
		t.Fatal(err)
	}
	queued, err := s.QueueNow(saved.ID)
	if err != nil {
		t.Fatal(err)
	}
	*now = now.Add(3 * time.Hour)
	if err := s.AskRun(queued.ID); err != nil {
		t.Fatal(err)
	}
	asking, err := s.Asking()
	if err != nil || len(asking) != 1 {
		t.Fatalf("asking = %+v, %v", asking, err)
	}
	if !asking[0].Due.Equal(queued.Due) || !asking[0].Asked.Equal(*now) {
		t.Fatalf("due %v asked %v, want due %v asked %v", asking[0].Due, asking[0].Asked, queued.Due, *now)
	}
	// Asked again by a store that is not waiting, it keeps the first time.
	*now = now.Add(time.Hour)
	if err := s.AskRun(queued.ID); err != nil {
		t.Fatal(err)
	}
	if again, _ := s.Asking(); len(again) != 1 || !again[0].Asked.Equal(asking[0].Asked) {
		t.Fatalf("asked again = %+v", again)
	}
	// A run asked for now waits from the moment it was asked.
	other, err := s.Create(routine("1h"))
	if err != nil {
		t.Fatal(err)
	}
	now2 := *now
	run, err := s.QueueAsking(other.ID)
	if err != nil || !run.Asked.Equal(now2) {
		t.Fatalf("queued asking = %+v, %v", run, err)
	}
	// Its row goes with the run.
	if err := s.Delete(saved.ID); err != nil {
		t.Fatal(err)
	}
	var rows int
	if err := s.db.QueryRow(`SELECT count(*) FROM run_asked WHERE run_id = ?`, queued.ID).Scan(&rows); err != nil || rows != 0 {
		t.Fatalf("a deleted run left %d asked rows, %v", rows, err)
	}
}

// A STORE AN OLDER BUILD MADE GAINS THE TABLE ON OPEN, without moving its
// version, and a run that build parked reads its slot.
func TestAnOlderStoreGainsWhenRunsAsked(t *testing.T) {
	root := t.TempDir()
	s, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := s.Create(routine("1h"))
	if err != nil {
		t.Fatal(err)
	}
	queued, err := s.QueueNow(saved.ID)
	if err != nil {
		t.Fatal(err)
	}
	// The store as an older build left it: no table, and a run it parked.
	if _, err := s.db.Exec(`DROP TRIGGER run_asked_delete; DROP TABLE run_asked`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`UPDATE runs SET phase = ? WHERE id = ?`, string(PhaseAsking), queued.ID); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	again, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	asking, err := again.Asking()
	if err != nil || len(asking) != 1 || !asking[0].Asked.IsZero() {
		t.Fatalf("asking = %+v, %v", asking, err)
	}
	var version int
	if err := again.db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil || version != schemaVersion {
		t.Fatalf("user_version = %d, %v", version, err)
	}
}
