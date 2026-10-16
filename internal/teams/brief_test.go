package teams

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"
)

func briefTeam(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	must(t, Update(dir, func(f *File) error {
		f.Teams = append(f.Teams, Team{ID: "t1", Name: "Release", Manager: "kmgr", Members: []Member{{Key: "kmgr", Handle: "release"}, {Key: "krev", Handle: "review"}}})
		return nil
	}))
	return dir
}

func revise(dir, key, by, byKey, text, before string, at time.Time) (BriefRevision, error) {
	return ReviseBrief(dir, "t1", key, BriefChange{By: by, ByKey: byKey, Text: text, Before: before, At: at})
}

func briefsOf(t *testing.T, dir, key string) Member {
	t.Helper()
	f, err := Load(dir)
	must(t, err)
	team, _ := f.Team("t1")
	m, _ := team.Member(key)
	return m
}

// A BRIEF CHANGED IN PLACE IS A DURABLE FACT: the membership keeps who, when,
// the words before and after, and the log line that delivered it, and a reopened
// file reads the same; an unchanged brief writes nothing.
func TestReviseBriefIsDurableAndDeliveredAsALogLine(t *testing.T) {
	dir := briefTeam(t)
	at := time.Date(2026, 10, 15, 14, 25, 0, 0, time.UTC)
	rev, err := revise(dir, "krev", FromYou, "", "  Check the retry backoff and skip the e2e suite.  ", "Check the retry backoff.", at)
	must(t, err)
	if rev.Text != "Check the retry backoff and skip the e2e suite." || rev.Before != "Check the retry backoff." || rev.By != FromYou || !rev.At.Equal(at) || rev.Entry == "" {
		t.Fatalf("revision = %+v", rev)
	}
	// The log line is addressed to the member, by handle and key, kind brief.
	e, ok, err := ReadEntry(dir, "t1", rev.Entry)
	if err != nil || !ok || e.Kind != KindBrief || e.To != "review" || e.ToKey != "krev" || e.From != FromYou || e.Text != rev.Text {
		t.Fatalf("log line = %+v %v %v", e, ok, err)
	}
	// Reopened from disk: the same revision; the second one's before is the first's after.
	if cur, ok := briefsOf(t, dir, "krev").CurrentBrief(); !ok || cur != rev {
		t.Fatalf("reopened = %+v %v", cur, ok)
	}
	second, err := revise(dir, "krev", FromManager, "kmgr", "Only the backoff.", "ignored: the membership knows its own", at.Add(time.Minute))
	must(t, err)
	if second.Before != rev.Text || second.ByKey != "kmgr" {
		t.Fatalf("second = %+v", second)
	}
	if _, err := revise(dir, "krev", FromYou, "", "Only the backoff.", "", at); !errors.Is(err, ErrBriefUnchanged) {
		t.Fatalf("unchanged = %v", err)
	}
	if _, err := revise(dir, "kzzz", FromYou, "", "x", "", at); !errors.Is(err, ErrBriefNoMember) {
		t.Fatalf("a stranger's brief = %v", err)
	}
	// The manager's own membership was untouched.
	if mgr := briefsOf(t, dir, "kmgr"); len(mgr.Briefs) != 0 {
		t.Fatalf("manager briefs = %+v", mgr.Briefs)
	}
}

// A MEMBERSHIP KEEPS ITS NEWEST CHANGES ONLY, each still saying what it
// replaced, while the file stores each brief's words once; its revisions
// survive the file's JSON beside a field from a later build.
func TestReviseBriefKeepsTheNewestAndRoundTrips(t *testing.T) {
	dir := briefTeam(t)
	for i := 0; i < BriefRevisionsKept+4; i++ {
		before := ""
		if i == 0 {
			before = "start"
		}
		_, err := revise(dir, "krev", FromYou, "", "brief "+strconv.Itoa(i), before, time.Time{})
		must(t, err)
	}
	m := briefsOf(t, dir, "krev")
	if len(m.Briefs) != BriefRevisionsKept || m.Briefs[0].Text != "brief 4" || m.Briefs[len(m.Briefs)-1].Text != "brief 19" {
		t.Fatalf("kept %d: first %q", len(m.Briefs), m.Briefs[0].Text)
	}
	// Stored: only the oldest kept says its before (the dropped one's words).
	if m.Briefs[0].Before != "brief 3" {
		t.Fatalf("oldest kept before = %q", m.Briefs[0].Before)
	}
	for _, r := range m.Briefs[1:] {
		if r.Before != "" {
			t.Fatalf("a later revision stored its before again: %+v", r)
		}
	}
	// Read: every revision says what it replaced.
	revs := m.BriefRevisions()
	for i, r := range revs {
		if want := "brief " + strconv.Itoa(i+3); r.Before != want {
			t.Fatalf("revision %d before = %q, want %q", i, r.Before, want)
		}
	}
	if cur, _ := m.CurrentBrief(); cur.Before != "brief 18" || cur.Text != "brief 19" {
		t.Fatalf("current = %+v", cur)
	}
	raw, _ := json.Marshal(m)
	var back Member
	must(t, json.Unmarshal([]byte(string(raw[:len(raw)-1])+`,"future":1}`), &back))
	again, _ := json.Marshal(back)
	if len(back.Briefs) != BriefRevisionsKept || string(again) != string(raw[:len(raw)-1])+`,"future":1}` {
		t.Fatalf("round trip lost a field:\n%s\n%s", raw, again)
	}
}

// A CHANGE MADE FROM AN OLD READ IS REFUSED, and so are a closed team's, an
// over-long brief, and a member that left: nothing is written for any of them.
func TestReviseBriefRefusesStaleClosedLongAndGone(t *testing.T) {
	dir := briefTeam(t)
	none := ""
	first, err := ReviseBrief(dir, "t1", "krev", BriefChange{By: FromYou, Text: "one", Base: &none})
	must(t, err)
	// Two editors read the same brief; the first to save wins, the second is stale.
	base := first.Entry
	_, err = ReviseBrief(dir, "t1", "krev", BriefChange{By: FromYou, Text: "two", Base: &base})
	must(t, err)
	if _, err := ReviseBrief(dir, "t1", "krev", BriefChange{By: FromYou, Text: "three", Base: &base}); !errors.Is(err, ErrBriefStale) {
		t.Fatalf("stale = %v", err)
	}
	if _, err := ReviseBrief(dir, "t1", "krev", BriefChange{By: FromYou, Text: "three", Base: &none}); !errors.Is(err, ErrBriefStale) {
		t.Fatalf("a never-changed base on a changed brief = %v", err)
	}
	if _, err := revise(dir, "krev", FromYou, "", strings.Repeat("x", BriefTextMax+1), "", time.Time{}); !errors.Is(err, ErrBriefTooLong) {
		t.Fatalf("too long = %v", err)
	}
	if _, err := revise(dir, "krev", FromYou, "", strings.Repeat("é", BriefTextMax), "", time.Time{}); err != nil {
		t.Fatalf("a brief of exactly the limit in characters = %v", err)
	}
	must(t, Update(dir, func(f *File) error {
		for i := range f.Teams {
			f.Teams[i].State = TeamClosed
		}
		return nil
	}))
	if _, err := revise(dir, "krev", FromYou, "", "after close", "", time.Time{}); !errors.Is(err, ErrClosed) {
		t.Fatalf("closed = %v", err)
	}
	m := briefsOf(t, dir, "krev")
	if len(m.Briefs) != 3 || m.Briefs[1].Text != "two" {
		t.Fatalf("written = %+v", m.Briefs)
	}
}

// A LINE IS READ WITH ITS NEIGHBOURS BY ITS ID: the window is the id's own, a
// line the log no longer holds is false (never a nearby one), and who a line
// involves is read from its keys and its address, never its words.
func TestReadAroundIsTheIDsOwnWindow(t *testing.T) {
	dir := briefTeam(t)
	var ids []string
	for i := 0; i < 9; i++ {
		id, err := AppendTrafficID(dir, "t1", Entry{Kind: KindNote, From: "review", To: FromManager, Text: "same words"})
		must(t, err)
		ids = append(ids, id)
	}
	got, ok, err := ReadAround(dir, "t1", ids[4], 2)
	if err != nil || !ok || len(got) != 5 || got[0].ID != ids[2] || got[2].ID != ids[4] || got[4].ID != ids[6] {
		t.Fatalf("window = %+v %v %v", got, ok, err)
	}
	if got, ok, _ := ReadAround(dir, "t1", ids[0], 2); !ok || got[0].ID != ids[0] || len(got) != 3 {
		t.Fatalf("first line's window = %+v", got)
	}
	if _, ok, _ := ReadAround(dir, "t1", "000000000099", 2); ok {
		t.Fatal("a line the log does not hold was found")
	}
	rev := Member{Key: "krev", Handle: "review"}
	if !Involves(got[2], rev) || Involves(got[2], Member{Key: "kweb", Handle: "web"}) {
		t.Fatalf("involves = %+v", got[2])
	}
	if !Involves(Entry{To: ToSeveral, Handles: []string{"web", "review"}}, rev) || Involves(Entry{To: "web"}, rev) {
		t.Fatal("involves by address")
	}
}
