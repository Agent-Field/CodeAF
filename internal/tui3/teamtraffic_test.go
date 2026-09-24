package tui3

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	teamstore "github.com/Agent-Field/codeaf/internal/teams"
)

// trafficApp is the strip's three conversations as one team, harbor, on a
// profile of the test's own, with the conversation in front its manager and
// the two behind it its members. older and newer are the agents behind.
func trafficApp(t *testing.T) (a *app, harbor string, older, newer *fakeAgent) {
	t.Helper()
	a, older, newer = tabApp(t)
	a.profileDir = t.TempDir()
	var err error
	if harbor, err = a.teamMake("harbor", a.tabList()); err != nil {
		t.Fatal(err)
	}
	a.teamActivate(harbor)
	front := a.frontTabKey()
	for _, tab := range a.tabList() {
		if tab.key == front {
			if err := a.teamMakeManager(harbor, tab); err != nil {
				t.Fatal(err)
			}
		}
	}
	if got := mustTeam(t, a, harbor); got.Manager != front {
		t.Fatalf("the fixture's manager is %q", got.Manager)
	}
	return a, harbor, older, newer
}

// trafficHandle is the handle of the member whose tab says word.
func trafficHandle(t *testing.T, a *app, id, word string) (string, string) {
	t.Helper()
	for _, m := range mustTeam(t, a, id).Members {
		if strings.Contains(m.Word, word) {
			if m.Handle == "" {
				t.Fatalf("member %q has no handle", m.Word)
			}
			return m.Handle, m.Key
		}
	}
	t.Fatalf("no member says %q: %+v", word, mustTeam(t, a, id).Members)
	return "", ""
}

// trafficAppend writes entries to team id's log, as the team tools do.
func trafficAppend(t *testing.T, a *app, id string, entries ...teamstore.Entry) {
	t.Helper()
	for _, e := range entries {
		if err := teamstore.AppendTraffic(a.profileDir, id, e); err != nil {
			t.Fatal(err)
		}
	}
}

// trafficReadNow is one turn of the Traffic read, run and folded in.
func trafficReadNow(t *testing.T, a *app) {
	t.Helper()
	a.traffic.reading = false
	spend(t, a, a.trafficRead())
}

// THE RAIL IS THE CACHE, BESIDE THE MANAGER. With the manager in front on a
// wide frame, the right of the body is the team's traffic, newest at the
// bottom, each row spelled `from → to  text`; the conversation is narrowed by
// exactly the column; the composer says the words go to the manager; and a row
// pressed goes to the member it is about.
func TestTrafficRailBesideTheManager(t *testing.T) {
	a, harbor, _, _ := trafficApp(t)
	a.width, a.height = 180, 40
	price, priceKey := trafficHandle(t, a, harbor, "openrouter")
	rail, _ := trafficHandle(t, a, harbor, "Refactor")
	trafficAppend(t, a, harbor,
		teamstore.Entry{Kind: teamstore.KindDirective, From: teamstore.FromManager, To: rail, Text: "take the scope model"},
		teamstore.Entry{Kind: teamstore.KindNote, From: price, To: rail, Text: "prices are in"},
	)
	trafficReadNow(t, a)
	if got := len(a.traffic.rows[harbor]); got != 2 {
		t.Fatalf("the cache holds %d entries", got)
	}

	frame, _, _ := a.frame()
	rows := strings.Split(ansi.Strip(frame), "\n")
	cols := a.trafficWidth()
	if cols < trafficColsMin || a.bodyWidth() != a.width-a.railWidth()-cols {
		t.Fatalf("the rail takes %d columns and leaves the body %d of %d", cols, a.bodyWidth(), a.width)
	}
	var directive, note = -1, -1
	for y, r := range rows {
		right := plainCells(r, a.width-cols, a.width)
		if strings.Contains(right, teamManagerGlyph+" → @"+rail) && strings.Contains(right, "take the scope") {
			directive = y
		}
		if strings.Contains(right, "@"+price+" → @"+rail) {
			note = y
		}
	}
	if directive < 0 || note < 0 || note <= directive {
		t.Fatalf("the rail does not draw the traffic newest at the bottom (%d, %d):\n%s", directive, note, strings.Join(rows, "\n"))
	}
	if !strings.Contains(ansi.Strip(frame), "to "+teamManagerGlyph+" manager") {
		t.Fatalf("the composer does not say where the words go:\n%s", ansi.Strip(frame))
	}

	// The note's row goes to the member who wrote it.
	if _, took := a.trafficPress(a.width-cols+3, note); !took {
		t.Fatal("the rail did not take a press on its row")
	}
	if a.frontTabKey() != priceKey {
		t.Fatalf("the row went to %q, want %q", a.frontTabKey(), priceKey)
	}
	// And away from the manager the column is gone and the body has it back.
	if a.trafficWidth() != 0 || a.bodyWidth() != a.width-a.railWidth() {
		t.Fatalf("the rail stayed beside a member: %d", a.trafficWidth())
	}
}

// ON A NARROW FRAME THE COLUMN IS AN EDGE, and the edge is a toggle: pressed, the
// traffic is laid over the body; a row pressed goes to its member and takes the
// traffic away again.
func TestTrafficRailNarrowIsAToggle(t *testing.T) {
	a, harbor, _, _ := trafficApp(t)
	a.width, a.height = 90, 30
	price, priceKey := trafficHandle(t, a, harbor, "openrouter")
	trafficAppend(t, a, harbor, teamstore.Entry{Kind: teamstore.KindNote, From: price, To: teamstore.ToManager, Text: "done with the scrape"})
	trafficReadNow(t, a)
	if got := a.trafficWidth(); got != trafficGripCols {
		t.Fatalf("a narrow frame gave the rail %d columns", got)
	}
	frame, _, _ := a.frame()
	if strings.Contains(ansi.Strip(frame), "done with the scrape") {
		t.Fatal("the traffic is drawn before the edge was pressed")
	}
	top := a.bodyTop()
	if _, took := a.trafficPress(a.width-1, top+2); !took || !a.traffic.over {
		t.Fatal("the edge did not lay the traffic over the body")
	}
	frame, _, _ = a.frame()
	rows := strings.Split(ansi.Strip(frame), "\n")
	at := -1
	for y, r := range rows {
		if strings.Contains(r, "@"+price) && strings.Contains(r, "done with the scrape") {
			at = y
		}
	}
	if at < 0 {
		t.Fatalf("the traffic is not over the body:\n%s", strings.Join(rows, "\n"))
	}
	if _, took := a.trafficPress(4, at); !took {
		t.Fatal("a row over the body did not take the press")
	}
	if a.frontTabKey() != priceKey || a.traffic.over {
		t.Fatalf("the row went to %q with the traffic still over: %v", a.frontTabKey(), a.traffic.over)
	}
}

// A STOP IS DONE ONCE, AND A START IS DONE ONCE, AND A WINDOW OPENING DOES
// NEITHER FOR WHAT CAME BEFORE IT. The manager stops a member this window holds
// behind the manager: that member's turn is stopped, once, however many reads
// go past the entry. The manager starts a member: one conversation opens in the
// team's folder, joins under the manager's handle for it, and is sent the brief
// marked as the manager's. A second window reading the same log from its tail
// does neither.
func TestTrafficStopAndStartAreDoneOnceAndNeverReplayed(t *testing.T) {
	a, harbor, older, _ := trafficApp(t)
	price, _ := trafficHandle(t, a, harbor, "openrouter")
	trafficReadNow(t, a) // the first look: an empty log, read from its start after this
	if got := a.traffic.cursor[harbor]; got != trafficFromStart {
		t.Fatalf("an empty log's cursor is %q", got)
	}

	trafficAppend(t, a, harbor, teamstore.Entry{Kind: teamstore.KindStop, From: teamstore.FromManager, To: price})
	trafficReadNow(t, a)
	trafficReadNow(t, a)
	if older.stops != 1 {
		t.Fatalf("the stop was done %d times", older.stops)
	}
	// A read that hands the same entries back again changes nothing.
	entries, _ := teamstore.ReadTraffic(a.profileDir, harbor, trafficFromStart, 0)
	a.trafficTake([]trafficGot{{trafficJob: trafficJob{id: harbor, after: a.traffic.cursor[harbor]}, entries: entries}}, nil, a.traffic.stamp, a.traffic.edits)
	if older.stops != 1 {
		t.Fatalf("a replayed read stopped the member again: %d", older.stops)
	}

	n := 0
	var fresh *fakeAgent
	a.start = func(workspace string) (Conversation, error) {
		n++
		fresh = &fakeAgent{model: "m"}
		return Conversation{Agent: fresh, SessionFile: fmt.Sprintf("/tmp/lab/started-%d.jsonl", n), Workspace: workspace}, nil
	}
	trafficAppend(t, a, harbor, teamstore.Entry{Kind: teamstore.KindStart, From: teamstore.FromManager, To: "lexer", Text: "rewrite the lexer"})
	trafficReadNow(t, a)
	trafficReadNow(t, a)
	if n != 1 {
		t.Fatalf("the start opened %d conversations", n)
	}
	started := a.frontTabKey()
	m, ok := mustTeam(t, a, harbor).ByHandle("lexer")
	if !ok || m.Key != started {
		t.Fatalf("the started conversation is not @lexer in harbor: %+v", mustTeam(t, a, harbor).Members)
	}
	if len(fresh.sent) != 1 || fresh.sent[0] != teamManagerGlyph+" from manager: rewrite the lexer" {
		t.Fatalf("the brief was sent as %q", fresh.sent)
	}
	disk, _ := loadTeams(a.profileDir, nil)
	if got, ok := disk[0].ByHandle("lexer"); !ok || got.Key != started {
		t.Fatalf("the new member did not reach the disk: %+v", disk[0].Members)
	}

	// A second window on the same log, holding the same conversations.
	b, _, olderB, _ := trafficApp(t)
	b.profileDir = a.profileDir
	b.wall.loaded = false
	b.teamsEnsure()
	b.teamActivate(harbor)
	nb := 0
	b.start = func(string) (Conversation, error) {
		nb++
		return Conversation{}, fmt.Errorf("no")
	}
	trafficReadNow(t, b)
	trafficReadNow(t, b)
	if olderB.stops != 0 || nb != 0 {
		t.Fatalf("a window opening did what was asked before it: %d stops, %d starts", olderB.stops, nb)
	}
	if len(b.traffic.rows[harbor]) != 2 {
		t.Fatalf("the history is not on the new window's rail: %+v", b.traffic.rows[harbor])
	}
}
