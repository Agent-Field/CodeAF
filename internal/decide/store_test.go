package decide

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

var t0 = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

func dec(id string, at time.Time) Decision {
	return Decision{ID: id, PlaceID: "p1", AskKind: "merge", At: at, By: "p1", Undo: Undo{Token: "u-" + id}}
}

func open(t *testing.T, dir string) *Store {
	t.Helper()
	s, err := Open(dir, "p1", func() time.Time { return t0 })
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestPersistsAcrossReopen(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)
	if err := s.Append(dec("a", t0)); err != nil {
		t.Fatal(err)
	}
	if err := s.SetMode("merge", ModeDeciding); err != nil {
		t.Fatal(err)
	}
	if err := s.Overturn("a", t0.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	s2 := open(t, dir)
	got, _ := s2.List()
	if len(got) != 1 || got[0].OverturnedAt == nil || got[0].Undo.Token != "u-a" {
		t.Fatalf("got %+v", got)
	}
	st, _ := s2.Mode("merge")
	if st.Mode != ModeDeciding || len(st.Recent) != 1 || !st.Recent[0].Overturned {
		t.Fatalf("mode %+v", st)
	}
	if st, _ := s2.Mode("other"); st.Mode != ModeLearning {
		t.Fatalf("unknown kind should learn, got %+v", st)
	}
	if err := s2.Overturn("zzz", t0); err != ErrNotFound {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestRingKeepsLastTwenty(t *testing.T) {
	s := open(t, t.TempDir())
	for i := 0; i < RingSize+5; i++ {
		if err := s.RecordOutcome("merge", Outcome{DecisionID: fmt.Sprint(i), At: t0}); err != nil {
			t.Fatal(err)
		}
	}
	st, _ := s.Mode("merge")
	if len(st.Recent) != RingSize || st.Recent[0].DecisionID != "5" {
		t.Fatalf("ring %d first %q", len(st.Recent), st.Recent[0].DecisionID)
	}
}

func TestBoundedByCapAndAge(t *testing.T) {
	s := open(t, t.TempDir())
	if err := s.Append(dec("old", t0.Add(-MaxAge-time.Hour))); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.List(); len(got) != 0 {
		t.Fatalf("aged decision kept: %+v", got)
	}
	for i := 0; i < MaxDecisions+3; i++ {
		if err := s.Append(dec(fmt.Sprintf("d%04d", i), t0.Add(-time.Duration(i)*time.Second))); err != nil {
			t.Fatal(err)
		}
	}
	got, _ := s.List()
	if len(got) != MaxDecisions || got[0].ID != "d0003" {
		t.Fatalf("len %d first %s", len(got), got[0].ID)
	}
}

func TestConcurrentWritersLoseNothing(t *testing.T) {
	dir := t.TempDir()
	// Two Store values on one file stand in for two processes: they share only
	// the flock, not the goroutine mutex.
	a, b := open(t, dir), open(t, dir)
	var wg sync.WaitGroup
	for w, s := range []*Store{a, b} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 25; i++ {
				if err := s.Append(dec(fmt.Sprintf("w%d-%d", w, i), t0)); err != nil {
					t.Error(err)
				}
			}
		}()
	}
	wg.Wait()
	if got, _ := a.List(); len(got) != 50 {
		t.Fatalf("want 50, got %d", len(got))
	}
}

func TestCorruptFileIsSetAside(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)
	if err := s.Append(dec("a", t0)); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "p1.decisions.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := s.List(); err != nil || len(got) != 0 {
		t.Fatalf("got %v err %v", got, err)
	}
	if !strings.Contains(s.Recovered(), ".corrupt-") {
		t.Fatalf("recovered %q", s.Recovered())
	}
	if b, _ := os.ReadFile(s.Recovered()); string(b) != "{not json" {
		t.Fatal("damaged bytes not preserved")
	}
	if err := s.Append(dec("b", t0)); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.List(); len(got) != 1 || got[0].ID != "b" {
		t.Fatalf("after recovery: %+v", got)
	}
}

func TestRejectsBadPlaceIDAndDuplicates(t *testing.T) {
	for _, id := range []string{"", "..", "a/b"} {
		if _, err := Open(t.TempDir(), id, nil); err == nil {
			t.Fatalf("accepted %q", id)
		}
	}
	s := open(t, t.TempDir())
	_ = s.Append(dec("a", t0))
	if err := s.Append(dec("a", t0)); err == nil {
		t.Fatal("duplicate accepted")
	}
	if err := s.SetMode("merge", "bogus"); err == nil {
		t.Fatal("bad mode accepted")
	}
}
