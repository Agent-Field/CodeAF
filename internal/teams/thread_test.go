package teams

import (
	"strings"
	"testing"
)

// A MESSAGE TO SEVERAL MEMBERS IS ONE ENTRY, and each of them is addressed by
// it; a member it does not name is not.
func TestSeveralIsOneEntryAddressedToEach(t *testing.T) {
	dir := t.TempDir()
	e := Entry{Kind: KindDirective, From: FromManager, To: ToSeveral, Handles: []string{"agent", "checking", "review"}, Text: "a brief status, please"}
	id, err := AppendTrafficID(dir, "t1", e)
	if err != nil || id != "000000000001" {
		t.Fatalf("append: %q %v", id, err)
	}
	got, _ := ReadTraffic(dir, "t1", "", 0)
	if len(got) != 1 || strings.Join(got[0].Handles, ",") != "agent,checking,review" {
		t.Fatalf("the log holds %+v", got)
	}
	for _, h := range []string{"agent", "checking", "review"} {
		if !got[0].Addressed(h) {
			t.Errorf("@%s is not addressed", h)
		}
	}
	if got[0].Addressed("web") || got[0].Addressed("") {
		t.Error("a member the message does not name is addressed")
	}
	if _, err := AppendTrafficID(dir, "t1", Entry{Kind: KindNote, From: FromManager, To: ToSeveral, Text: "x"}); err == nil {
		t.Error("several with nobody named was written")
	}
	if r := (Entry{To: ToEveryone}).Recipients(); r != nil {
		t.Errorf("everyone named %v", r)
	}
	if r := (Entry{To: "web"}).Recipients(); len(r) != 1 || r[0] != "web" {
		t.Errorf("one member named %v", r)
	}
}

// THREADS ARE ORDERED BY THEIR NEWEST ACTIVITY, newest first, and inside a
// thread everything is in the order it was written. A reply to a reply joins
// the first message's thread; an old entry that answers nothing is a thread of
// its own; an answer to an id outside the window begins its own.
func TestThreadsGroupAndOrder(t *testing.T) {
	log := []Entry{
		{ID: "000000000001", Kind: KindDirective, From: FromManager, To: ToSeveral, Handles: []string{"a", "b"}, Text: "q1"},
		{ID: "000000000002", Kind: KindNote, From: FromManager, To: "c", Text: "q2"},
		{ID: "000000000003", Kind: KindNote, From: "a", To: ToManager, Text: "r1a", Answers: "000000000001"},
		{ID: "000000000004", Kind: KindEvent, From: "old", To: ToManager, State: StateFinished},
		{ID: "000000000005", Kind: KindNote, From: "c", To: ToManager, Text: "r2", Answers: "000000000002"},
		{ID: "000000000006", Kind: KindEvent, From: "a", To: ToManager, State: StateFinished, Answers: "000000000003"},
		{ID: "000000000007", Kind: KindNote, From: "z", To: ToManager, Text: "orphan", Answers: "000000000000"},
	}
	th := Threads(log)
	if len(th) != 4 {
		t.Fatalf("%d threads: %+v", len(th), th)
	}
	if th[0].Root.ID != "000000000007" || th[1].Root.ID != "000000000001" || th[2].Root.ID != "000000000002" || th[3].Root.ID != "000000000004" {
		t.Fatalf("order: %s %s %s %s", th[0].Root.ID, th[1].Root.ID, th[2].Root.ID, th[3].Root.ID)
	}
	if r := th[1].Replies; len(r) != 2 || r[0].ID != "000000000003" || r[1].ID != "000000000006" || th[1].Latest != "000000000006" {
		t.Fatalf("q1's replies: %+v", r)
	}
	if len(th[3].Replies) != 0 {
		t.Fatal("an old unlinked event gathered replies")
	}
}

// A thread number reads back from what a model writes.
func TestThreadNumbers(t *testing.T) {
	if got := ThreadNumber("000000000042"); got != "#42" {
		t.Fatalf("number %q", got)
	}
	for _, s := range []string{"#42", "42", " 000000000042 "} {
		if id, ok := ThreadID(s); !ok || id != "000000000042" {
			t.Errorf("%q read as %q %v", s, id, ok)
		}
	}
	for _, s := range []string{"", "#", "abc", "-3", "0"} {
		if _, ok := ThreadID(s); ok {
			t.Errorf("%q read as an id", s)
		}
	}
	if !(Entry{Kind: KindEvent, State: StateRunning, Text: "woke @web"}).Wake() || (Entry{Kind: KindEvent, State: StateRunning, Text: "answered"}).Wake() {
		t.Error("a wake is not told from a turn carrying on")
	}
}
