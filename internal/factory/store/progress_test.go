package store

import (
	"testing"
	"time"
)

// Where a read is is written beside its polling mark, taken off with it, and
// taken off when a process starts, so a read a crash cut short never says it
// is still on a repository.
func TestReadingIsWrittenAndClearedWithPolling(t *testing.T) {
	st := open(t)
	_ = st.SetSourceMeta("github", SourceMeta{Trouble: "kept"})
	_ = st.SetPolling("github", true)
	if err := st.SetReading("github", "acme/web", 1, 3); err != nil {
		t.Fatal(err)
	}
	m, _ := st.SourceMeta("github")
	if !m.Polling || m.Reading != "acme/web" || m.Read != 1 || m.Of != 3 || m.Trouble != "kept" {
		t.Fatalf("record mid-read: %+v", m)
	}
	if err := st.SetReadingItems("github", "acme/web", 1, 3, 200); err != nil {
		t.Fatal(err)
	}
	if m, _ = st.SourceMeta("github"); m.Items != 200 || m.Reading != "acme/web" || m.Read != 1 || m.Of != 3 {
		t.Fatalf("the items so far were not written: %+v", m)
	}
	_ = st.SetPolling("github", false)
	if m, _ = st.SourceMeta("github"); m.Reading != "" || m.Read != 0 || m.Of != 0 || m.Items != 0 {
		t.Fatalf("the end of a read left where it was: %+v", m)
	}
	_ = st.SetPolling("github", true)
	_ = st.SetReadingItems("github", "acme/api", 0, 2, 40)
	if err := st.ClearBusy(); err != nil {
		t.Fatal(err)
	}
	if m, _ = st.SourceMeta("github"); m.Polling || m.Reading != "" || m.Read != 0 || m.Of != 0 || m.Items != 0 {
		t.Fatalf("a process starting left a crashed read's place: %+v", m)
	}
}

// The nudge mark is the zero time until somebody nudges, and moves when they
// do, as another handle on the same floor sees it.
func TestNudgePollMovesTheMark(t *testing.T) {
	st := open(t)
	if !st.PollNudged().IsZero() {
		t.Fatal("a floor nobody nudged has a mark")
	}
	other, err := Open(st.Root())
	if err != nil {
		t.Fatal(err)
	}
	if err := other.NudgePoll(); err != nil {
		t.Fatal(err)
	}
	first := st.PollNudged()
	if first.IsZero() {
		t.Fatal("the nudge left no mark")
	}
	time.Sleep(5 * time.Millisecond)
	_ = other.NudgePoll()
	if !st.PollNudged().After(first) {
		t.Fatal("a second nudge did not move the mark")
	}
}
