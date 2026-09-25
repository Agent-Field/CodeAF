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

// THE COLUMN IS THE CACHE, BESIDE THE MANAGER, AND IT OPENS ON THE TRAFFIC.
// With the manager in front on a wide frame the right of the body is the side
// column, its header `Tasks 0 · Traffic N` with the Traffic in front; the
// directive is a row of work, the note that answers nothing is General's; the
// person's own lines are not drawn; the conversation is narrowed by exactly
// the column; the composer says the words go to the manager; and a handle
// pressed goes to its member, where the same column, the same width, shows
// that member's own chat.
func TestTrafficColumnBesideTheManager(t *testing.T) {
	a, harbor, _, _ := trafficApp(t)
	a.width, a.height = 180, 40
	price, priceKey := trafficHandle(t, a, harbor, "openrouter")
	rail, _ := trafficHandle(t, a, harbor, "Refactor")
	d, _ := teamstore.AppendTrafficID(a.profileDir, harbor, teamstore.Entry{Kind: teamstore.KindDirective, From: teamstore.FromManager, To: rail, Text: "take the scope model"})
	trafficAppend(t, a, harbor,
		teamstore.Entry{Kind: teamstore.KindNote, From: price, To: rail, Text: "prices are in"},
		teamstore.Entry{Kind: teamstore.KindYou, From: teamstore.FromYou, To: teamstore.ToManager, Text: "my own words"},
	)
	trafficReadNow(t, a)
	if got := len(a.traffic.rows[harbor]); got != 3 {
		t.Fatalf("the cache holds %d entries", got)
	}
	if a.sideView() != sideTraffic {
		t.Fatal("a manager's column does not open on the Traffic")
	}
	a.sideToggleThread(sideThreadKey(harbor, sideGeneral))
	rows := railLines(t, a)
	joined := strings.Join(rows, "\n")
	cols := a.railWidth()
	if cols != sideColsFor(a.width) || a.bodyWidth() != a.width-cols {
		t.Fatalf("the column takes %d columns and leaves the body %d of %d", cols, a.bodyWidth(), a.width)
	}
	head := railRowOf(rows, sideTasksWord+" 0"+sideWordSep+sideTrafficWord)
	general := railRowOf(rows, "General")
	work := railRowOf(rows, "@"+rail+"  take the scope model")
	note := railRowOf(rows, "@"+price+" → ")
	if head < 0 || !strings.HasSuffix(rows[head], sideHideKey) || general != head+1 || note != general+1 || work <= note {
		t.Fatalf("the column does not draw its header, General open with the note, and the work under it (%d, %d, %d, %d):\n%s", head, general, note, work, joined)
	}
	if strings.Contains(joined, "my own words") {
		t.Fatalf("the person's own line is on the column:\n%s", joined)
	}
	frame, _, _ := a.frame()
	if !strings.Contains(ansi.Strip(frame), "to "+teamManagerGlyph+" manager") {
		t.Fatalf("the composer does not say where the words go:\n%s", ansi.Strip(frame))
	}

	// Under the pointer the handle names its member, and the row says itself
	// whole.
	x, y := sideDoorOf(t, a, sideActJump)
	a.setHover(x, y)
	if a.hot.kind != hoverSide || a.hot.index < 0 {
		t.Fatalf("the handle does not answer the pointer: %+v", a.hot)
	}
	if words := a.dockHoverWords(); !strings.Contains(words, "@") || !strings.Contains(words, "click") {
		t.Fatalf("the hint line over the handle says %q", words)
	}
	wx, wy := sideRowOn(t, a, "thread/"+d)
	a.setHover(wx, wy)
	if words := a.dockHoverWords(); !strings.Contains(words, "take the scope model") {
		t.Fatalf("the hint line over the row says %q", words)
	}
	a.dropHover()

	// The note's handle goes to the member who wrote it.
	px, py := sideRowDoor(t, a, railKeyOfReply(t, a, "prices are in"), sideActJump)
	sideClick(t, a, px, py)
	if a.frontTabKey() != priceKey {
		t.Fatalf("the handle went to %q, want %q", a.frontTabKey(), priceKey)
	}
	// AND IN THE MEMBER'S CHAT THE COLUMN HOLDS STILL: the same width, the
	// tasks in front, and its Traffic that member's own messages.
	if a.railWidth() != cols || a.bodyWidth() != a.width-cols {
		t.Fatalf("the column moved beside a member: %d, was %d", a.railWidth(), cols)
	}
	if a.sideKind() != sideKindMember || a.sideView() != sideTasks {
		t.Fatalf("a member's column is kind %d view %d", a.sideKind(), a.sideView())
	}
	a.sideSetView(sideTraffic)
	rows = railLines(t, a)
	joined = strings.Join(rows, "\n")
	if railRowOf(rows, "@"+rail+"  prices are in") < 0 || strings.Contains(joined, "take the scope model") {
		t.Fatalf("the member's Traffic is not its own messages only:\n%s", joined)
	}
	// A member of a managed team is told where its words go too.
	if got := a.trafficHint(); got != "to @"+price {
		t.Fatalf("a member's composer says %q", got)
	}
}

// railKeyOfReply is the key of the drawn row that says words.
func railKeyOfReply(t *testing.T, a *app, words string) string {
	t.Helper()
	view, _ := a.railDrawnView(a.viewHeight())
	for _, line := range view {
		if line.side != nil && strings.Contains(ansi.Strip(line.text), words) {
			return line.side.key
		}
	}
	t.Fatalf("no row of the column says %q", words)
	return ""
}

// AT 110 COLUMNS THE COLUMN STANDS. A 110-column laptop terminal is not
// narrow: the one column takes the right, at the width every chat gives it,
// and leaves the conversation its floor.
func TestTrafficColumnStandsAt110(t *testing.T) {
	a, _, _, _ := trafficApp(t)
	a.width, a.height = 110, 30
	a.welcome.open = false
	trafficReadNow(t, a)
	if got := a.railWidth(); got != sideColsFor(110) || got < sideColsMin {
		t.Fatalf("at 110 the column has %d columns", got)
	}
	if a.bodyWidth() < sideBodyFloor {
		t.Fatalf("the conversation is left %d columns", a.bodyWidth())
	}
	if !a.railShowing() || a.railStowed() {
		t.Fatal("the column does not stand at 110")
	}
	rows := railLines(t, a)
	if railRowOf(rows, sideTasksWord+" 0"+sideWordSep+sideTrafficWord) < 0 {
		t.Fatalf("the header does not carry both words:\n%s", strings.Join(rows, "\n"))
	}
}

// THE COLUMN IS PUT AWAY BY ITS KEY AND BROUGHT BACK BY ITS EDGE OR ITS KEY.
// The header's `alt+l` puts it away and leaves the edge with a count of what
// came in since; the edge's hint says so; alt+l brings it back, and ctrl+g is
// the same key under its older name.
func TestTrafficColumnHidesAndShows(t *testing.T) {
	a, harbor, _, _ := trafficApp(t)
	a.width, a.height = 160, 40
	price, _ := trafficHandle(t, a, harbor, "openrouter")
	trafficReadNow(t, a)
	_ = railLines(t, a)
	x, y := sideDoorOf(t, a, sideActHide)
	sideClick(t, a, x, y)
	if !a.railAway || a.railShowing() {
		t.Fatal("the header's key did not put the column away")
	}
	if got := a.railWidth(); got != railGripCols {
		t.Fatalf("a hidden column costs %d columns", got)
	}
	trafficAppend(t, a, harbor, teamstore.Entry{Kind: teamstore.KindNote, From: price, To: teamstore.ToManager, Text: "done"})
	trafficReadNow(t, a)
	frame, _, _ := a.frame()
	var edge strings.Builder
	for _, r := range strings.Split(ansi.Strip(frame), "\n") {
		edge.WriteString(strings.TrimSpace(plainCells(r, a.width-railGripCols, a.width)))
	}
	if !strings.Contains(edge.String(), "1") {
		t.Fatalf("the edge does not count its one new entry: %q", edge.String())
	}
	a.hot = hoverAt{kind: hoverRailGrip}
	if words := a.dockHoverWords(); !strings.Contains(words, "Show this column") || !strings.Contains(words, trafficKey) || !strings.Contains(words, "1 new") {
		t.Fatalf("the edge's hint says %q", words)
	}
	a.hot = hoverAt{}
	if _, took := a.trafficKeyPress(tea.KeyPressMsg{Code: 'l', Mod: tea.ModAlt}); !took || a.railAway {
		t.Fatalf("%s did not bring the column back", trafficKey)
	}
	drive(t, a, ctrlG())
	if !a.railAway {
		t.Fatal("ctrl+g is not the same key")
	}
	drive(t, a, ctrlG())
	if a.railAway || a.railWidth() != sideColsFor(a.width) {
		t.Fatal("ctrl+g did not bring the column back")
	}
}

// ON A NARROW FRAME THERE IS NO COLUMN AND NO EDGE, and the key lays the
// column over the frame: the same header, the same rows. esc takes it away,
// and a handle pressed on it goes to its member and takes the overlay with it.
func TestTrafficColumnNarrowIsAnOverlay(t *testing.T) {
	a, harbor, _, _ := trafficApp(t)
	a.width, a.height = 80, 30
	price, priceKey := trafficHandle(t, a, harbor, "openrouter")
	q, _ := teamstore.AppendTrafficID(a.profileDir, harbor, teamstore.Entry{Kind: teamstore.KindDirective, From: teamstore.FromManager, To: price, Text: "scrape the prices"})
	trafficReadNow(t, a)
	if a.railWidth() != 0 || a.railStowed() {
		t.Fatalf("a narrow frame gave the column %d columns", a.railWidth())
	}
	frame, _, _ := a.frame()
	if strings.Contains(ansi.Strip(frame), "scrape the prices") {
		t.Fatal("the traffic is drawn before the key was pressed")
	}
	drive(t, a, key(trafficKey))
	if !a.railFull() {
		t.Fatal("alt+l did not lay the column over the frame")
	}
	frame, _, _ = a.frame()
	if !strings.Contains(ansi.Strip(frame), "scrape the prices") || !strings.Contains(ansi.Strip(frame), sideTrafficWord) {
		t.Fatalf("the overlay does not draw the Traffic:\n%s", ansi.Strip(frame))
	}
	drive(t, a, key("esc"))
	if a.railFull() {
		t.Fatal("esc did not take the overlay away")
	}
	drive(t, a, key(trafficKey))
	_, _, _ = a.frame()
	x, y := sideRowDoor(t, a, "thread/"+q, sideActJump)
	sideClick(t, a, x, y)
	if a.frontTabKey() != priceKey || a.railFull() {
		t.Fatalf("the handle went to %q with the overlay still up: %v", a.frontTabKey(), a.railFull())
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
	// The column says what the stop and the start were for, under General.
	// The route takes the cells the brief used to have, so a cut word is on
	// the hint line, whole.
	a.width, a.height = 180, 40
	a.sideToggleThread(sideThreadKey(harbor, sideGeneral))
	rows := strings.Join(railLines(t, a), "\n")
	if !strings.Contains(rows, teamManagerGlyph+" → @lexer") || !strings.Contains(rows, "started @lexer") || !strings.Contains(rows, "stopped @"+price) {
		t.Fatalf("the column drops the stop or the start:\n%s", rows)
	}
	if hint := a.sideRowOf(railKeyOfReply(t, a, "started @lexer")).hint; !strings.Contains(hint, "rewrite") {
		t.Fatalf("the start's brief is not on the hint: %q", hint)
	}
	if hint := a.sideRowOf(railKeyOfReply(t, a, "stopped @"+price)).hint; !strings.Contains(hint, "stuck") {
		t.Fatalf("the stop's reason is not on the hint: %q", hint)
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
		t.Fatalf("the history is not in the new window's cache: %+v", b.traffic.rows[harbor])
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
