package teams

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
)

// A MEMBER'S BRIEF FOLLOWS ITS RECORD, NOT ITS HANDLE: a start line is found by
// the id the membership kept, so a rename, and a later member taking the old
// name, change neither member's answer.
func TestStartForFollowsTheRecordedLineThroughRenameAndReuse(t *testing.T) {
	dir := t.TempDir()
	team := Team{ID: "t1"}
	first, err := AppendTrafficID(dir, "t1", Entry{Kind: KindStart, From: FromManager, To: "web", Text: "first brief"})
	must(t, err)
	must(t, AppendTraffic(dir, "t1", Entry{Kind: KindYou, From: FromYou, To: "web", Text: "a later person message"}))
	second, err := AppendTrafficID(dir, "t1", Entry{Kind: KindStart, From: FromManager, To: "web", Text: "second brief"})
	must(t, err)

	// The first member was renamed; the second took "web".
	a := Member{Key: "ka", Handle: "api", Started: true, StartEntry: first}
	b := Member{Key: "kb", Handle: "web", Started: true, StartEntry: second}
	for _, tc := range []struct {
		m    Member
		want string
	}{{a, "first brief"}, {b, "second brief"}} {
		e, ok, err := StartFor(dir, team, tc.m)
		if err != nil || !ok || e.Text != tc.want || e.ID != tc.m.StartEntry {
			t.Fatalf("%s: %+v %v %v", tc.m.Key, e, ok, err)
		}
	}
	// The id survives the teams file's JSON round trip, beside a future field.
	raw, _ := json.Marshal(a)
	var back Member
	must(t, json.Unmarshal([]byte(string(raw[:len(raw)-1])+`,"future":1}`), &back))
	if back.StartEntry != first {
		t.Fatalf("start_entry lost: %s", raw)
	}
	// A recorded line that is not a manager's start, or is addressed to someone
	// else, is no brief.
	for _, m := range []Member{{Key: "kc", StartEntry: "000000000002"}, {Key: "kc", StartEntry: "000000000099"}} {
		if _, ok, _ := StartFor(dir, team, m); ok {
			t.Fatalf("%+v returned a brief", m)
		}
	}
}

// OLD RECORDS ANSWER ONLY WHEN THE LOG PROVES IT: a lone start to the handle in
// a log that begins at its first entry; two starts to one name are unknown, and
// so is a member nobody started.
func TestStartForLegacyMembershipsAreUnknownUnlessUnambiguous(t *testing.T) {
	dir := t.TempDir()
	team := Team{ID: "t1"}
	only := Member{Key: "k1", Handle: "web", Started: true}
	if _, ok, _ := StartFor(dir, team, only); ok {
		t.Fatal("an empty log returned a brief")
	}
	_, err := AppendTrafficID(dir, "t1", Entry{Kind: KindStart, From: FromManager, To: "web", Text: "only brief"})
	must(t, err)
	if e, ok, err := StartFor(dir, team, only); err != nil || !ok || e.Text != "only brief" {
		t.Fatalf("lone start: %+v %v %v", e, ok, err)
	}
	if _, ok, _ := StartFor(dir, team, Member{Key: "k2", Handle: "web"}); ok {
		t.Fatal("a member nobody started got a brief")
	}
	// The handle was held by a former member: not provably this one's.
	former := Team{ID: "t1", FormerMembers: []Member{{Key: "old", Handle: "web"}}}
	if _, ok, _ := StartFor(dir, former, only); ok {
		t.Fatal("a reused handle returned a brief")
	}
	// A second start to the same name makes both ambiguous.
	_, err = AppendTrafficID(dir, "t1", Entry{Kind: KindStart, From: FromManager, To: "web", Text: "other brief"})
	must(t, err)
	if _, ok, _ := StartFor(dir, team, only); ok {
		t.Fatal("two starts to one handle returned a brief")
	}
}

// A LOG THAT CANNOT BE READ THROUGH IS AN ERROR, NOT A SHORT LOG: a line past
// the scan's bound would otherwise be skipped, and the start line it hid
// reported as absent.
func TestStartForLegacyScanRefusesAnOverlongLine(t *testing.T) {
	dir := t.TempDir()
	team := Team{ID: "t1"}
	_, err := AppendTrafficID(dir, "t1", Entry{Kind: KindStart, From: FromManager, To: "web", Text: "only brief"})
	must(t, err)
	f, err := os.OpenFile(TrafficPath(dir, "t1"), os.O_APPEND|os.O_WRONLY, 0o600)
	must(t, err)
	_, err = f.WriteString(`{"id":"000000000002","kind":"you","text":"` + strings.Repeat("x", maxScanLine+10) + "\"}\n")
	must(t, err)
	must(t, f.Close())
	_, ok, err := StartFor(dir, team, Member{Key: "k1", Handle: "web", Started: true})
	if ok || !errors.Is(err, ErrTrafficUnreadable) {
		t.Fatalf("overlong line: ok=%v err=%v", ok, err)
	}
}

// A start line recorded by id but filed under another team is no brief, and a
// scan stops counting at a second match.
func TestStartForIgnoresAnotherTeamsLine(t *testing.T) {
	dir := t.TempDir()
	id, err := AppendTrafficID(dir, "t1", Entry{Kind: KindStart, From: FromManager, To: "web", Team: "other", Text: "not ours"})
	must(t, err)
	if _, ok, _ := StartFor(dir, Team{ID: "t1"}, Member{Key: "k1", Handle: "web", Started: true, StartEntry: id}); ok {
		t.Fatal("a line of another team was a brief by id")
	}
	if _, ok, _ := StartFor(dir, Team{ID: "t1"}, Member{Key: "k1", Handle: "web", Started: true}); ok {
		t.Fatal("a line of another team was a brief by handle")
	}
}
