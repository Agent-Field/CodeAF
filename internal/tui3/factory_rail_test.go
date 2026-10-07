package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// factoryRailHeadings reads the rail's headings off its rows, word and count.
func factoryRailHeadings(rows []factoryRailRow) map[string]int {
	out := map[string]int{}
	for _, r := range rows {
		if r.kind == factoryRowHeading {
			out[r.heading] = r.count
		}
	}
	return out
}

// factoryWalkRefs is the walk's refs, top to bottom.
func factoryWalkRefs(snap factory.Snapshot, walk []int) []string {
	var out []string
	for _, i := range walk {
		out = append(out, snap.Items[i].Ref())
	}
	return out
}

// THE FIXTURE GROUPS AND COUNTS: one waiting, two in streams, five new, one
// landed, one shipped, and the strip above them all.
func TestFactoryRailGroupsAndCountsTheFixture(t *testing.T) {
	snap := factory.Fixture(factoryTestNow)
	rows := factoryRailRows(snap)
	if len(rows) == 0 || rows[0].kind != factoryRowStrip || rows[0].heading != "" {
		t.Fatalf("the rail does not open on the all-repos strip: %+v", rows)
	}
	got := factoryRailHeadings(rows)
	want := map[string]int{"needs you": 1, "streams": 2, "new": 5, "landed": 1, "shipped": 1}
	for word, n := range want {
		if got[word] != n {
			t.Errorf("heading %q counts %d, want %d (all: %v)", word, got[word], n, got)
		}
	}
	if len(got) != len(want) {
		t.Errorf("the rail has headings %v, want exactly %v", got, want)
	}
	// ONE BLANK BETWEEN BLOCKS, never two in a row.
	for i := 1; i < len(rows); i++ {
		if rows[i].kind == factoryRowBlank && rows[i-1].kind == factoryRowBlank {
			t.Fatalf("two blank rows in a row at %d", i)
		}
	}
	if walk := factoryWalk(snap); len(walk) != 10 {
		t.Fatalf("the walk holds %d items, want 10", len(walk))
	}
	// A DISMISSED ITEM NEVER DRAWS.
	snap.Items[0].State = factory.StateDismissed
	if got := factoryRailHeadings(factoryRailRows(snap)); got["needs you"] != 0 {
		t.Fatalf("a dismissed item still draws under %v", got)
	}
}

// THE ROW CARRIES ITS STATE'S ONE FACT, and drops it on a rail under thirty.
func TestFactoryRailRowFacts(t *testing.T) {
	a := factoryPlaceLab(t)
	a.fp.cursor = -1
	want := map[string]string{
		"#1538": "7h",
		"#1551": "review · $1.42",
		"#1660": "queued",
		"#1662": "pr",
		"ci":    "ci",
		"#31":   "bug M",
		"#1661": "3" + a.icon(tokens.GSettled) + " 1" + a.icon(tokens.GFailed),
		"#1663": "06:00",
	}
	for _, it := range a.fp.snap.Items {
		fact, ok := want[it.Ref()]
		if !ok {
			continue
		}
		row := ansi.Strip(a.factoryRailItem(it, 39, false))
		if ansi.StringWidth(row) != 39 {
			t.Fatalf("%s's row is %d cells: %q", it.Ref(), ansi.StringWidth(row), row)
		}
		if !strings.HasSuffix(strings.TrimRight(row, " "), fact) {
			t.Errorf("%s's row does not end on %q: %q", it.Ref(), fact, row)
		}
		narrow := ansi.Strip(a.factoryRailItem(it, factoryFactFloor-1, false))
		if fact != "ci" && strings.Contains(narrow, fact) {
			t.Errorf("%s keeps its fact on a %d-cell rail: %q", it.Ref(), factoryFactFloor-1, narrow)
		}
	}
}

// THE FILTER WORDS NARROW, each to the items it names.
func TestFactoryRailFilterWordsNarrow(t *testing.T) {
	snap := factory.Fixture(factoryTestNow)
	for _, c := range []struct {
		q    string
		want string
	}{
		{"risky", "#1538"},
		{"cheap", "#1660,#1662,ci,#1540,#1663"},
		{"strangers", "#31"},
		{"spend", "#1538,#702,#1663"},
		{"ui", "#1551,#1662,#1540"},
		{"prs", "#1662"},
		{"bugs", "#1551,#1660,#31,#1540,#1661,#1663"},
		{"thin", "#31"},
		{"mine", "#1538,#702,#1540,#1661"},
		{"whisper", "ci,#31"},
		{"mine bugs", "#1540,#1661"},
		{"nothing-matches-this", ""},
	} {
		got := strings.Join(factoryWalkRefs(snap, factoryWalk(snap, factoryView{query: c.q, backlog: true})), ",")
		if got != c.want {
			t.Errorf("%q narrows to %q, want %q", c.q, got, c.want)
		}
	}
}

// THE DELTA RULE keeps old new items behind A and says how many; A shows them.
func TestFactoryRailDeltaRuleAndBacklog(t *testing.T) {
	snap := factory.Fixture(factoryTestNow)
	for i := range snap.Items {
		if snap.Items[i].Ref() == "#31" || snap.Items[i].Ref() == "#702" {
			snap.Items[i].Created = factoryTestNow.Add(-10 * 24 * time.Hour)
		}
	}
	rows := factoryRailRows(snap)
	if got := factoryRailHeadings(rows)["new"]; got != 3 {
		t.Fatalf("new counts %d with the backlog kept back, want 3", got)
	}
	more := -1
	for _, r := range rows {
		if r.kind == factoryRowMore {
			more = r.count
		}
	}
	if more != 2 {
		t.Fatalf("the kept-back line counts %d, want 2", more)
	}
	open := factoryRailRows(snap, factoryView{backlog: true})
	if got := factoryRailHeadings(open)["new"]; got != 5 {
		t.Fatalf("new counts %d with the backlog shown, want 5", got)
	}
	for _, r := range open {
		if r.kind == factoryRowMore {
			t.Fatal("the backlog is shown and the kept-back line still draws")
		}
	}

	// AND THROUGH THE PAGE: the line reads in words, and A brings them in.
	a := factoryPlaceLab(t)
	a.factoryFold(snap)
	a.height = 40
	text := factoryFrameText(a)
	if !strings.Contains(text, "2 older open items behind A") {
		t.Fatalf("the kept-back line is not drawn:\n%s", text)
	}
	drive(t, a, key("A"))
	if text := factoryFrameText(a); strings.Contains(text, "behind A") || !strings.Contains(text, "#702") {
		t.Fatalf("A did not bring the backlog in:\n%s", text)
	}
}

// THE KEYS: `/` types live, esc clears before it leaves, `[` `]` cycle the
// repos, `space` marks a new item.
func TestFactoryRailKeys(t *testing.T) {
	a := factoryPlaceLab(t)
	a.height = 40
	drive(t, a, key("/"))
	if !a.fp.typing {
		t.Fatal("/ did not open the words box")
	}
	for _, r := range "prs" {
		drive(t, a, key(string(r)))
	}
	if got := factoryWalkRefs(a.fp.snap, a.factoryWalkNow()); strings.Join(got, ",") != "#1662" {
		t.Fatalf("typing prs narrowed to %v", got)
	}
	if it, _ := a.factoryCursorItem(); it.Ref() != "#1662" {
		t.Fatalf("the cursor is on %s after narrowing", it.Ref())
	}
	drive(t, a, key("enter"))
	if a.fp.typing || a.fp.query != "prs" {
		t.Fatalf("enter did not keep the words: typing %v query %q", a.fp.typing, a.fp.query)
	}
	drive(t, a, key("esc"))
	if !a.at(pageFactory) || a.fp.query != "" {
		t.Fatalf("esc over a filter left the page or kept the words (%q)", a.fp.query)
	}
	if it, _ := a.factoryCursorItem(); it.Ref() != "#1662" {
		t.Fatalf("clearing the filter moved the cursor to %s", it.Ref())
	}

	drive(t, a, key("]"))
	if a.fp.repo != 1 {
		t.Fatalf("] put the repo at %d", a.fp.repo)
	}
	if text := factoryFrameText(a); !strings.Contains(text, "codeaf · 7") {
		t.Fatalf("the strip does not name the repo and its count:\n%s", text)
	}
	drive(t, a, key("["), key("["))
	if a.fp.repo != 3 {
		t.Fatalf("[ [ put the repo at %d, want the last", a.fp.repo)
	}
	for _, i := range a.factoryWalkNow() {
		if a.fp.snap.Items[i].Repo != "santoshkumar/whisper" {
			t.Fatalf("the whisper rail holds %s", a.fp.snap.Items[i].Ref())
		}
	}
	drive(t, a, key("esc"))
	if !a.at(pageFactory) || a.fp.repo != 0 {
		t.Fatal("esc over a repo filter did not clear it first")
	}

	// SPACE MARKS A NEW ITEM, and a re-read keeps the mark.
	for {
		it, _ := a.factoryCursorItem()
		if it.State == factory.StateNew {
			break
		}
		drive(t, a, key("down"))
	}
	marked, _ := a.factoryCursorItem()
	drive(t, a, key(" "))
	if !a.factoryMarked(marked) {
		t.Fatalf("space did not mark %s", marked.Ref())
	}
	a.factoryFold(factory.Fixture(factoryTestNow))
	if it, _ := a.factoryCursorItem(); it.ID != marked.ID || !a.factoryMarked(it) {
		t.Fatal("a re-read lost the mark or the cursor")
	}
	drive(t, a, key(" "))
	if a.factoryMarked(marked) {
		t.Fatal("a second space did not unmark")
	}

	if got := (placeFactory{}).hint(a); got != "↑↓ walk · space mark · / filter · [ ] repo · A backlog · esc back" {
		t.Fatalf("the hint is %q", got)
	}
	drive(t, a, key("esc"))
	if a.at(pageFactory) {
		t.Fatal("esc on an unnarrowed rail did not leave")
	}
}

// THE BODY IS EXACTLY ROOM × WIDTH at the three widths a person sees, and the
// plain floor draws no escape at all.
func TestFactoryRailBodyIsExactAndPlainAtTheFloor(t *testing.T) {
	for _, width := range []int{150, 100, 70} {
		a := factoryPlaceLab(t)
		a.pal = newPalette(tokens.NoColor, false)
		a.fp.query, a.fp.typing = "", true
		for _, room := range []int{1, 7, 20, 40} {
			rows := a.factoryBody(width, room)
			if len(rows) != room {
				t.Fatalf("at %d×%d the body drew %d rows", width, room, len(rows))
			}
			for i, r := range rows {
				if got := ansi.StringWidth(r.text); got != width {
					t.Fatalf("at %d×%d row %d is %d cells: %q", width, room, i, got, ansi.Strip(r.text))
				}
				if strings.Contains(r.text, "\x1b[") {
					t.Fatalf("at %d×%d row %d carries an escape on the plain floor: %q", width, room, i, r.text)
				}
			}
		}
	}
}
