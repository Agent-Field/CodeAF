package tui3

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/session"
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
	teamsFlush(t, a)
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
	teamsFlush(t, a)
	a.traffic.reading = false
	spend(t, a, a.trafficRead())
	teamsFlush(t, a)
}

// THE RAIL IS THE CACHE, BESIDE THE MANAGER, AND IT HOLDS THE RIGHT. With the
// manager in front on a wide frame, the right of the body is the team's
// traffic under a `Traffic` header, the newest thread straight under it, each
// headed `from → to  do|fyi  age` over its words; the person's own lines are
// not drawn; the conversation is narrowed by exactly the rail; the composer
// says the words go to the manager; and a handle pressed goes to its member.
func TestTrafficRailBesideTheManager(t *testing.T) {
	a, harbor, _, _ := trafficApp(t)
	a.width, a.height = 180, 40
	price, priceKey := trafficHandle(t, a, harbor, "openrouter")
	rail, _ := trafficHandle(t, a, harbor, "Refactor")
	trafficAppend(t, a, harbor,
		teamstore.Entry{Kind: teamstore.KindDirective, From: teamstore.FromManager, To: rail, Text: "take the scope model"},
		teamstore.Entry{Kind: teamstore.KindNote, From: price, To: rail, Text: "prices are in"},
		teamstore.Entry{Kind: teamstore.KindYou, From: teamstore.FromYou, To: teamstore.ToManager, Text: "my own words"},
	)
	trafficReadNow(t, a)
	if got := len(a.traffic.rows[harbor]); got != 3 {
		t.Fatalf("the cache holds %d entries", got)
	}

	frame, _, _ := a.frame()
	rows := strings.Split(ansi.Strip(frame), "\n")
	cols := a.trafficWidth()
	if cols < trafficColsMin || a.bodyWidth() != a.width-a.railWidth()-cols {
		t.Fatalf("the rail takes %d columns and leaves the body %d of %d", cols, a.bodyWidth(), a.width)
	}
	var head, directive, note = -1, -1, -1
	for y, r := range rows {
		right := plainCells(r, a.width-cols, a.width)
		if strings.Contains(right, trafficWord) && strings.Contains(right, "hide "+trafficKey) {
			head = y
		}
		if strings.Contains(right, teamManagerGlyph+" manager → @"+rail+"  do") {
			directive = y
		}
		if strings.Contains(right, "@"+price+" → @"+rail+"  fyi") {
			note = y
		}
		if strings.Contains(right, "my own words") {
			t.Fatalf("the person's own line is on the rail: %q", right)
		}
	}
	if head < 0 || directive < 0 || note != head+1 || directive <= note {
		t.Fatalf("the rail does not draw its header and the newest thread straight under it (%d, %d, %d):\n%s", head, directive, note, strings.Join(rows, "\n"))
	}
	if !strings.Contains(plainCells(rows[note+1], a.width-cols, a.width), "prices are in") ||
		!strings.Contains(plainCells(rows[directive+1], a.width-cols, a.width), "take the scope model") {
		t.Fatalf("a message is not on its own line under its header:\n%s", strings.Join(rows, "\n"))
	}
	if !strings.Contains(ansi.Strip(frame), "to "+teamManagerGlyph+" manager") {
		t.Fatalf("the composer does not say where the words go:\n%s", ansi.Strip(frame))
	}

	// Under the pointer the handle names its member, and the words say
	// themselves whole.
	at, ok := a.trafficHoverAt(a.width-cols+2, note)
	if !ok || at.kind != hoverTraffic {
		t.Fatalf("the handle does not answer the pointer: %+v", at)
	}
	a.hot = at
	if words := a.dockHoverWords(); !strings.Contains(words, "@"+price) || !strings.Contains(words, "click") {
		t.Fatalf("the hint line over the handle says %q", words)
	}
	a.hot, _ = a.trafficHoverAt(a.width-cols+4, note+1)
	if words := a.dockHoverWords(); !strings.Contains(words, "prices are in") {
		t.Fatalf("the hint line over the words says %q", words)
	}
	a.hot = hoverAt{}

	// The note's handle goes to the member who wrote it.
	if _, took := a.trafficPress(a.width-cols+2, note); !took {
		t.Fatal("the rail did not take a press on its row")
	}
	if a.frontTabKey() != priceKey {
		t.Fatalf("the row went to %q, want %q", a.frontTabKey(), priceKey)
	}
	// And away from the manager the column is gone and the body has it back.
	if a.trafficWidth() != 0 || a.bodyWidth() != a.width-a.railWidth() {
		t.Fatalf("the rail stayed beside a member: %d", a.trafficWidth())
	}
	// A member of a managed team is told where its words go too.
	if got := a.trafficHint(); got != "to @"+price {
		t.Fatalf("a member's composer says %q", got)
	}
}

// AT 110 COLUMNS THE RAIL STANDS. A 110-column laptop terminal is not narrow:
// the Traffic takes the right column, and the task column folds to its edge
// rather than pushing the rail under its floor.
func TestTrafficRailStandsAt110(t *testing.T) {
	a, harbor, _, _ := trafficApp(t)
	a.width, a.height = 110, 30
	a.welcome.open = false
	trafficReadNow(t, a)
	_ = harbor
	if got := a.trafficWidth(); got < trafficColsMin {
		t.Fatalf("at 110 the rail has %d columns", got)
	}
	if a.bodyWidth() < trafficBodyFloor {
		t.Fatalf("the conversation is left %d columns", a.bodyWidth())
	}
	if !a.railQuiet() && !a.railStowed() {
		t.Fatalf("the task column still stands beside the rail: %d", a.railWidth())
	}
}

// THE RAIL IS PUT AWAY BY A WORD AND BROUGHT BACK BY ITS EDGE OR ITS KEY. The
// header's `hide` puts the column away and leaves the edge, the word Traffic
// with a count of what came in since; the key brings it back; and asking the
// task column back (ctrl+g's road) puts the Traffic away, since the two share
// the one column.
func TestTrafficRailHidesAndShows(t *testing.T) {
	a, harbor, _, _ := trafficApp(t)
	a.width, a.height = 160, 40
	price, _ := trafficHandle(t, a, harbor, "openrouter")
	trafficReadNow(t, a)
	_, _, _ = a.frame()
	d := a.traffic.drawn
	if d.mode != trafficColumn || !d.hide.pressable() {
		t.Fatalf("the column has no hide word: %+v", d)
	}
	if _, took := a.trafficPress(d.hide.from, a.bodyTop()+d.hideY); !took || !a.traffic.hidden {
		t.Fatal("hide did not put the rail away")
	}
	if got := a.trafficWidth(); got != trafficGripCols {
		t.Fatalf("a hidden rail costs %d columns", got)
	}
	trafficAppend(t, a, harbor, teamstore.Entry{Kind: teamstore.KindNote, From: price, To: teamstore.ToManager, Text: "done"})
	trafficReadNow(t, a)
	frame, _, _ := a.frame()
	var edge strings.Builder
	for _, r := range strings.Split(ansi.Strip(frame), "\n") {
		edge.WriteString(strings.TrimSpace(plainCells(r, a.width-trafficGripCols, a.width)))
	}
	if !strings.Contains(edge.String(), trafficWord+"1") {
		t.Fatalf("the edge does not say Traffic and its one new entry: %q", edge.String())
	}
	a.hot = hoverAt{kind: hoverTrafficGrip}
	if words := a.dockHoverWords(); !strings.Contains(words, "Show the team's traffic") || !strings.Contains(words, trafficKey) {
		t.Fatalf("the edge's hint says %q", words)
	}
	a.hot = hoverAt{}
	if _, took := a.trafficKeyPress(tea.KeyPressMsg{Code: 'l', Mod: tea.ModAlt}); !took || a.traffic.hidden {
		t.Fatalf("%s did not bring the rail back", trafficKey)
	}
	a.railStow(false)
	if !a.traffic.hidden {
		t.Fatal("asking for the tasks back left the Traffic holding the column")
	}
}

// ON A NARROW FRAME THE RAIL IS AN EDGE, and the edge lays a card over the
// lower body: bordered, titled Traffic, `Close esc` in its foot, with the top
// of the conversation still drawn above it. esc takes it away, and a row
// pressed goes to its member.
func TestTrafficRailNarrowIsACard(t *testing.T) {
	a, harbor, _, _ := trafficApp(t)
	a.width, a.height = 80, 30
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
		t.Fatal("the edge did not lay the card over the body")
	}
	frame, _, _ = a.frame()
	rows := strings.Split(ansi.Strip(frame), "\n")
	at, card := -1, -1
	for y, r := range rows {
		if strings.Contains(r, "@"+price+" → ") && y+1 < len(rows) && strings.Contains(rows[y+1], "done with the scrape") {
			at = y
		}
		if strings.Contains(r, trafficWord) && card < 0 && y > top {
			card = y
		}
	}
	if at < 0 || card <= top || !strings.Contains(ansi.Strip(frame), "Close esc") {
		t.Fatalf("no card with its close word over the lower body (row %d, card %d):\n%s", at, card, strings.Join(rows, "\n"))
	}
	if _, took := a.trafficKeyPress(tea.KeyPressMsg{Code: tea.KeyEscape}); !took || a.traffic.over {
		t.Fatal("esc did not close the card")
	}
	a.trafficShow(true)
	_, _, _ = a.frame()
	if _, took := a.trafficPress(6, at); !took {
		t.Fatal("a row of the card did not take the press")
	}
	if a.frontTabKey() != priceKey || a.traffic.over {
		t.Fatalf("the row went to %q with the card still over: %v", a.frontTabKey(), a.traffic.over)
	}
}

// A QUIET TURN OF THE CLOCK READS NOTHING AND DRAWS NOTHING. The turn asks the
// seam, which answers a log that has not moved and a teams file at its stamp
// with nothing; the fold changes nothing, the frame before stands, and the
// clock counts the quiet turn. A log that moved is read on the next turn.
func TestTrafficQuietTickReadsNothing(t *testing.T) {
	a, harbor, _, _ := trafficApp(t)
	trafficReadNow(t, a)
	a.traffic.ticking = true
	turn := func() {
		t.Helper()
		cmd, quiet := a.trafficTick(trafficTickMsg{})
		if !quiet || cmd == nil {
			t.Fatalf("a turn of the clock drew (quiet %v) or asked nothing", quiet)
		}
		door, ok := cmd().(doorMsg)
		if !ok {
			t.Fatal("the turn's read is not a door beside the line")
		}
		if next := door.fold(true); next == nil {
			t.Fatal("the turn did not set the next one")
		}
	}
	rows, idle := len(a.traffic.rows[harbor]), a.traffic.idle
	a.drawn, a.ptr.still = true, false
	turn()
	if len(a.traffic.rows[harbor]) != rows || a.traffic.idle != idle+1 || !a.ptr.still || a.traffic.reading {
		t.Fatalf("a quiet turn changed something (rows %d, idle %d, still %v)", len(a.traffic.rows[harbor]), a.traffic.idle, a.ptr.still)
	}
	trafficAppend(t, a, harbor, teamstore.Entry{Kind: teamstore.KindNote, From: teamstore.FromManager, To: teamstore.ToRoom, Text: "moved"})
	turn()
	if got := a.traffic.rows[harbor]; len(got) != rows+1 || got[len(got)-1].Text != "moved" || a.traffic.idle != 0 {
		t.Fatalf("a log that moved was not read: %+v (idle %d)", got, a.traffic.idle)
	}
	// No manager, no clock.
	b, _, _, _ := trafficApp(t)
	for i := range b.wall.teams {
		b.wall.teams[i].Manager = ""
	}
	if b.trafficWanted() {
		t.Fatal("a team with no manager keeps the clock turning")
	}
}

// A STOP IS DONE ONCE, AND A START IS DONE ONCE, AND A WINDOW OPENING DOES
// NEITHER FOR WHAT CAME BEFORE IT. The manager stops a member this window holds
// behind the manager: that member's turn is stopped, once, however many reads
// go past the entry. The manager starts a member: one conversation opens in the
// team's folder BEHIND the one in front, joins under the manager's handle for
// it, and is sent nothing: the person's focus does not move, `tab` does not go
// to it, and its tab reads `@lexer`. A second window reading the same log from
// its tail does neither.
func TestTrafficStopAndStartAreDoneOnceAndNeverReplayed(t *testing.T) {
	a, harbor, older, _ := trafficApp(t)
	price, _ := trafficHandle(t, a, harbor, "openrouter")
	trafficReadNow(t, a) // the first look: an empty log, read from its start after this
	if got := a.traffic.cursor[harbor]; got != trafficFromStart {
		t.Fatalf("an empty log's cursor is %q", got)
	}

	trafficAppend(t, a, harbor, teamstore.Entry{Kind: teamstore.KindStop, From: teamstore.FromManager, To: price, Text: "stuck in a retry loop"})
	trafficReadNow(t, a)
	trafficReadNow(t, a)
	if older.stops != 1 {
		t.Fatalf("the stop was done %d times", older.stops)
	}
	// A read that hands the same entries back again changes nothing.
	entries, _ := teamstore.ReadTraffic(a.profileDir, harbor, trafficFromStart, 0)
	a.trafficTake([]trafficGot{{trafficJob: trafficJob{id: harbor, after: a.traffic.cursor[harbor]}, entries: entries}}, nil, a.traffic.stamp, a.traffic.edits, false)
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
	front := a.frontTabKey()
	back, _ := a.lastBehind()
	a.input.insert("half a sentence to the manager")
	trafficAppend(t, a, harbor, teamstore.Entry{Kind: teamstore.KindStart, From: teamstore.FromManager, To: "lexer", Text: "rewrite the lexer"})
	trafficReadNow(t, a)
	trafficReadNow(t, a)
	if n != 1 {
		t.Fatalf("the start opened %d conversations", n)
	}
	if a.frontTabKey() != front || string(a.input.value) != "half a sentence to the manager" {
		t.Fatalf("the start moved the focus: front %q (was %q), box %q", a.frontTabKey(), front, string(a.input.value))
	}
	if got, _ := a.lastBehind(); got != back {
		t.Fatalf("tab now goes to %q, not %q", got, back)
	}
	m, ok := mustTeam(t, a, harbor).ByHandle("lexer")
	if !ok || a.behind[m.Key] == nil {
		t.Fatalf("the started conversation is not @lexer in harbor, held behind: %+v", mustTeam(t, a, harbor).Members)
	}
	if len(fresh.sent) != 0 {
		t.Fatalf("the brief was sent as the person's words: %q", fresh.sent)
	}
	a.touch()
	if row := plain(a.tabsRow(a.width)); !strings.Contains(row, "@lexer") {
		t.Fatalf("the new member's tab does not read @lexer: %q", row)
	}
	teamsFlush(t, a)
	disk, _ := loadTeams(a.profileDir, nil)
	if got, ok := disk[0].ByHandle("lexer"); !ok || got.Key != m.Key {
		t.Fatalf("the new member did not reach the disk: %+v", disk[0].Members)
	}
	// The rail says what the stop and the start were for.
	a.width, a.height = 180, 40
	frame := ansi.Strip(func() string { f, _, _ := a.frame(); return f }())
	if !strings.Contains(frame, "stopped @"+price+" · stuck in") || !strings.Contains(frame, "started @lexer") || !strings.Contains(frame, "  rewrite the lexer") {
		t.Fatalf("the rail drops the stop's reason or the start's brief:\n%s", frame)
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

// THE MANAGER'S PLACE IS PINNED. At 80 columns with a member in front the
// manager's tab is still on the strip, first, and `+ Manager` leaves a strip
// that narrow.
func TestTeamManagerPlaceIsPinnedAt80(t *testing.T) {
	a, harbor, _, _ := trafficApp(t)
	_, priceKey := trafficHandle(t, a, harbor, "openrouter")
	spend(t, a, a.trafficGo(priceKey))
	a.width, a.height = 80, 30
	a.touch()
	row := plain(a.tabsRow(a.width))
	if !strings.Contains(row, teamManagerGlyph+" "+teamManagerWord) {
		t.Fatalf("the manager's place scrolled off at 80: %q", row)
	}
	b, _ := managerApp(t)
	b.width = 80
	b.touch()
	if row := plain(b.tabsRow(b.width)); strings.Contains(row, teamManagerSlotWord) {
		t.Fatalf("+ Manager takes a slot at 80: %q", row)
	}
	b.width = 160
	b.touch()
	_ = b.tabsRow(b.width)
	slot := stripHitKind(t, b, tabManager)
	b.hot = hoverAt{kind: hoverTab, index: slot.span.from}
	if words := b.dockHoverWords(); !strings.HasPrefix(words, "Start a manager") {
		t.Fatalf("+ Manager explains itself as %q", words)
	}
}

// A TEAM'S NOTE IS A QUOTED CARD. The session's note is read into its lines,
// each headed by who said it to whom, and a directive carries `do`.
func TestTeamAsideReadsAsCards(t *testing.T) {
	text := "Team traffic in \"harbor\" for you (@lexer). These are the team's messages, not the person's words:\n" +
		"◆ directive from manager: rewrite the lexer\n" +
		"from @web to the room: the build is green\n" +
		"(The person's own words in this conversation outrank the manager.)"
	cards, ok := teamAsideCards(text, teamManagerGlyph)
	if !ok || len(cards) != 2 {
		t.Fatalf("the note read as %+v", cards)
	}
	if c := cards[0]; c.from != teamManagerGlyph+" manager" || c.to != "@lexer" || c.tag != "do" || c.text != "rewrite the lexer" {
		t.Fatalf("the manager's line read as %+v", c)
	}
	if c := cards[1]; c.from != "@web" || c.to != "room" {
		t.Fatalf("the teammate's line read as %+v", c)
	}
}

// A MEMBER THAT JOINED BEFORE IT HAD A TITLE IS TITLED BY THE READ. Nothing a
// person does has to happen for it: the Traffic read sees the member's tab has
// a name now, writes it through the store, and the member has a handle the
// manager can address it by.
func TestTrafficReadTitlesAMemberThatJoinedUntitled(t *testing.T) {
	a, harbor, _, _ := trafficApp(t)
	if err := a.teamEdit(func(f *teamstore.File) error {
		return f.AddMember(harbor, teamstore.Member{Key: "/tmp/lab/late.jsonl", File: "/tmp/lab/late.jsonl"})
	}); err != nil {
		t.Fatal(err)
	}
	if m, _ := mustTeam(t, a, harbor).Member("/tmp/lab/late.jsonl"); m.Handle != "" {
		t.Fatalf("an untitled member has handle %q", m.Handle)
	}
	a.chatTabs = append(a.chatTabs, chatTab{key: "/tmp/lab/late.jsonl", file: "/tmp/lab/late.jsonl", word: "benchmark sweep"})
	trafficReadNow(t, a)
	if m, _ := mustTeam(t, a, harbor).Member("/tmp/lab/late.jsonl"); m.Handle != "benchmark" {
		t.Fatalf("the read left the member as %+v", m)
	}
	teamsFlush(t, a)
	disk, _ := loadTeams(a.profileDir, nil)
	if m, _ := disk[0].Member("/tmp/lab/late.jsonl"); m.Handle != "benchmark" {
		t.Fatalf("the title did not reach the disk: %+v", m)
	}
}

// A REPLAYED BRIEF IS THE MANAGER'S CARD. The session hands the lines it
// delivered on the aside ([session.TeamLine]); the start's reads
// `◆ manager → @lexer` over the quoted brief, never the person's `›`.
func TestAReplayedBriefIsTheManagersCard(t *testing.T) {
	a, _, _, _ := trafficApp(t)
	text := "Team traffic in \"harbor\" for you (@lexer). These are the team's messages, not the person's words:\n◆ brief from manager: rewrite the lexer"
	e := entry{kind: entryTeam, text: text, team: []session.TeamLine{{Team: "harbor", From: teamstore.FromManager, Kind: teamstore.KindStart, Text: "rewrite the lexer"}}}
	rows := ansi.Strip(strings.Join(a.teamCardRows(e, 60), "\n"))
	if !strings.Contains(rows, teamManagerGlyph+" manager → @lexer") || !strings.Contains(rows, "│ rewrite the lexer") || strings.Contains(rows, "›") {
		t.Fatalf("the brief draws as:\n%s", rows)
	}
}
