package tui3

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// ── THE CONVERSATION'S TIP, ITS KEYS ROW'S PROJECT, AND TWO DOORS ───────────
//
// A conversation's tip stands at the RIGHT END OF ITS KEYS ROW, over the
// project, with home's bulb and a cross, once the conversation has been quiet
// for fifteen seconds (2026-09-24). The project came down off the seam to the
// right end of the keys row, as it did on home. And two of the owner's bug
// reports from the same day: enter on `/attach` in the list opens the browser
// at once, and the search place finds conversations by name when memory is off.

// chatTipLab is a conversation over a clock the test turns by hand, with a
// door that can open a conversation, so home is somewhere to go back to
// ([app.homeDoorOpen]).
func chatTipLab(t *testing.T) (*app, func(time.Duration)) {
	t.Helper()
	a, _ := sheetApp(t)
	a.open = func(string, string) (Conversation, error) { return Conversation{}, nil }
	now := time.Date(2026, 9, 22, 10, 0, 0, 0, time.UTC)
	a.clock = func() time.Time { return now }
	return a, func(d time.Duration) { now = now.Add(d) }
}

// tipRowOf is the frame row carrying the tip, and -1 when none does.
func tipRowOf(a *app, tip string) (int, []string) {
	rows := strings.Split(plain(frame(a)), "\n")
	for i, row := range rows {
		if strings.Contains(row, tip) {
			return i, rows
		}
	}
	return -1, rows
}

// THE TIP COVERS THE PROJECT WHILE IT IS UP (2026-09-24): the right end of
// the keys row, with home's bulb and a cross, one cell in from the edge. The
// controls keep their place at the row's left.
func TestAConversationsTipCoversTheProjectOnTheKeysRow(t *testing.T) {
	a, _ := chatTipLab(t)
	a.tilde, a.workspace = "/home/person", "/home/person/projects/parser"
	b := &a.notices
	makeDeliverable(t, a)
	if b.current[slotHint] != "files-after-first-deliverable" {
		t.Fatalf("an export landing armed %q", b.current[slotHint])
	}
	if got := a.chatTip(); got != deliverTip {
		t.Fatalf("a window nothing has stirred does not say its tip: %q", got)
	}
	y, rows := tipRowOf(a, deliverTip)
	if y != markedRowY(a, a.hintRowKind(), 0) {
		t.Fatalf("the tip is not on the keys row:\n%s", strings.Join(rows, "\n"))
	}
	row := plain(a.hintRow(a.width))
	cross := a.pal.glyph(tokens.GFailed)
	if got := ansi.StringWidth(row); got != a.width {
		t.Fatalf("the keys row measures %d cells on a %d-cell frame", got, a.width)
	}
	if !strings.HasSuffix(row, homeTipLead+homeTipGap+deliverTip+homeTipGap+cross+" ") {
		t.Fatalf("the keys row does not end with the bulb, the tip and its cross: %q", row)
	}
	if !strings.HasPrefix(row, " "+plain(a.footHint(a.width))) {
		t.Fatalf("the tip cost the controls their place: %q", row)
	}
	if strings.Contains(row, targetProjectLead) || a.seamProjectSpan.pressable() {
		t.Fatalf("the project is still on the row under the tip: %q", row)
	}
	for width := 1; width <= 240; width++ {
		if line := plain(a.hintRow(width)); ansi.StringWidth(line) > width {
			t.Fatalf("at %d cells the keys row overflowed: %q", width, line)
		}
	}

	// A RUNNING TURN, A PLACE, A LETTER IN THE BOX: each takes the tip away,
	// and the project is back.
	a.state = stateWorking
	if got := plain(a.hintRow(a.width)); strings.Contains(got, deliverTip) || !strings.Contains(got, targetProjectLead) {
		t.Fatalf("the tip drew over a running turn: %q", got)
	}
	a.state = stateIdle
	a.showPage(pageSpend)
	if got := a.chatTip(); got != "" {
		t.Fatalf("the tip drew under a place: %q", got)
	}
	a.leavePlace()
	a.input.setText("x")
	if got := a.chatTip(); got != "" {
		t.Fatalf("the tip drew over a box with a letter in it: %q", got)
	}
}

// THE TIP STANDS WITH THE TASK COLUMN OPEN OR PUT AWAY (the owner's report,
// 2026-09-24): a column put away — whose keys row then says `ctrl+g tasks` —
// does not silence it.
func TestTheConversationsTipStandsWithTheColumnOpenOrAway(t *testing.T) {
	a, _, _ := taskApp(t)
	a.profileDir = t.TempDir()
	a.open = func(string, string) (Conversation, error) { return Conversation{}, nil }
	a.width, a.height = 180, 40
	railRun(a)
	makeDeliverable(t, a)
	a.tipQuietFrom = time.Time{}
	check := func(state string) {
		t.Helper()
		y, rows := tipRowOf(a, deliverTip)
		if y < 0 || y != markedRowY(a, a.hintRowKind(), 0) {
			t.Fatalf("%s: the tip is not on the keys row:\n%s", state, strings.Join(rows, "\n"))
		}
	}
	if !a.railShowing() {
		t.Fatal("the task column is not up for the open half of this test")
	}
	check("column open")
	drive(t, a, ctrlG())
	a.tipQuietFrom = time.Time{}
	if got := plain(a.footHint(a.width)); got != a.sideBackHint() {
		t.Fatalf("the keys row of a put-away column reads %q, want %q", got, a.sideBackHint())
	}
	check("column put away")
}

// FIFTEEN SECONDS OF QUIET, ON CONVERSATIONS ONLY (2026-09-24). A key starts
// the wait again and takes the row down, and so does a turn ending; the tip
// is back once the conversation has been left alone for [chatTipQuiet], and
// the one alarm that draws it is set by the loop itself.
func TestTheConversationsTipWaitsForFifteenQuietSeconds(t *testing.T) {
	a, advance := chatTipLab(t)
	makeDeliverable(t, a)
	drive(t, a, key("x"), key("backspace"))
	if got := a.chatTip(); got != "" {
		t.Fatalf("the tip drew straight after a key: %q", got)
	}
	if y, _ := tipRowOf(a, deliverTip); y >= 0 {
		t.Fatal("the tip is on the frame straight after a key")
	}
	// THE LOOP SET ONE ALARM for the end of the wait, and only one however
	// many keys arrive while it is pending.
	if a.tipAlarm.IsZero() {
		t.Fatal("no alarm was set for the end of the wait")
	}
	first := a.tipAlarm
	advance(5 * time.Second)
	drive(t, a, key("x"), key("backspace"))
	if a.tipAlarm != first {
		t.Fatalf("a second alarm was set while one was pending: %v then %v", first, a.tipAlarm)
	}
	advance(chatTipQuiet - time.Second)
	if got := a.chatTip(); got != "" {
		t.Fatalf("the tip drew a second before the wait was up: %q", got)
	}
	// The first alarm lands early — the wait moved — and the loop sets the next.
	drive(t, a, chatTipDueMsg{})
	if a.tipAlarm.IsZero() {
		t.Fatal("the alarm that landed early did not set the next one")
	}
	advance(time.Second)
	if got := a.chatTip(); got != deliverTip {
		t.Fatalf("the tip is not up after fifteen quiet seconds: %q", got)
	}
	drive(t, a, chatTipDueMsg{})
	if y, rows := tipRowOf(a, deliverTip); y < 0 {
		t.Fatalf("the tip is not on the frame after the wait:\n%s", strings.Join(rows, "\n"))
	}
	if !a.tipAlarm.IsZero() {
		t.Fatal("an alarm is still set for a tip that is already up")
	}

	// A TURN ENDING STARTS THE WAIT AGAIN: the answer is what is being read.
	a.settle()
	if got := a.chatTip(); got != "" {
		t.Fatalf("the tip drew straight after a turn ended: %q", got)
	}
	// AND HOME DOES NOT WAIT: its row is its own and is said at once.
	advance(time.Second)
	runCmd(a.showPage(pageHome))
	if a.noticeHomeHint() == "" {
		t.Fatal("home's row waited on the conversation's quiet clock")
	}
}

// THE CROSS PUTS THE TIP AWAY, and gives the project back, until the
// conversation is left and come back to, as home's does until home is
// (2026-09-24). The tip put away is not retired.
func TestTheCrossPutsTheConversationsTipAwayUntilItIsLeft(t *testing.T) {
	a, _ := chatTipLab(t)
	a.tilde, a.workspace = "/home/person", "/home/person/projects/parser"
	makeDeliverable(t, a)
	y, rows := tipRowOf(a, deliverTip)
	if y < 0 {
		t.Fatalf("the tip is not on the frame:\n%s", strings.Join(rows, "\n"))
	}
	if !a.chatTipClose.pressable() {
		t.Fatal("the tip row recorded no cross")
	}
	// A press beside the cross is a press on nothing.
	if a.chatTipPress(a.chatTipClose.from-3, y) {
		t.Fatal("a press on the tip's words was taken as the cross")
	}
	// THE REAL PRESS, through the loop: the click restarts the quiet clock, and
	// it must do so only after the cross has been found under it.
	drive(t, a, tea.MouseClickMsg{X: a.chatTipClose.from, Y: y, Button: tea.MouseLeft})
	if !a.notices.hidden[slotHint] {
		t.Fatal("a click on the cross, through the loop, did not put the row away")
	}
	a.tipQuietFrom = time.Time{}
	if got := a.chatTip(); got != "" {
		t.Fatalf("the row still says %q after its cross", got)
	}
	if y, _ := tipRowOf(a, deliverTip); y >= 0 {
		t.Fatal("the tip is still on the frame after its cross")
	}
	if got := plain(a.hintRow(a.width)); !strings.Contains(got, targetProjectLead+"~/projects/parser") {
		t.Fatalf("the project did not come back after the cross: %q", got)
	}
	if a.notices.retired("files-after-first-deliverable") {
		t.Fatal("the cross retired the tip it put away")
	}
	// Staying put does not bring it back; leaving and coming back does.
	a.settle()
	if got := a.chatTip(); got != "" {
		t.Fatalf("a turn ending lifted the cross: %q", got)
	}
	a.showPage(pageSpend)
	a.leavePlace()
	a.tipQuietFrom = time.Time{}
	if got := a.chatTip(); got != deliverTip {
		t.Fatalf("coming back to the conversation did not lift the cross: %q", got)
	}
}

// THE TIP ABOUT THE DOOR (2026-09-24) is said in a conversation after its first
// exchange and never on home.
func TestTheWayHomeTipIsSaidAwayFromHomeOnly(t *testing.T) {
	var row notice
	for _, n := range notices {
		if n.id == "home-by-two-spaces" {
			row = n
		}
	}
	if row.id == "" {
		t.Fatal("the way-home tip is not on the table")
	}
	if row.text != "space space takes you back to home" || row.retire != eventHomeGesture {
		t.Fatalf("the way-home tip reads %q and retires on %q", row.text, row.retire)
	}
	a, _ := chatTipLab(t)
	if row.armed(a) {
		t.Fatal("the way-home tip is armed before the first exchange")
	}
	a.turn = 1
	if !row.armed(a) {
		t.Fatal("the way-home tip is not armed after the first exchange")
	}
	runCmd(a.showPage(pageHome))
	if row.armed(a) {
		t.Fatal("the way-home tip is armed on home itself")
	}
}

// GOING HOME BY TWO SPACES TURNS HOME'S RING like any other road there (the
// review of #1487). The gesture's event used to be said before home opened, so
// home's row was decided over the conversation, where `/project`'s home-only
// tip reads as unarmed; it came back fresh on every trip and jumped the ring.
func TestGoingHomeByTwoSpacesStillTurnsHomesTips(t *testing.T) {
	trips := func(bySpaces bool) []string {
		a, _ := chatTipLab(t)
		a.turn = 1
		var seen []string
		for i := 0; i < 4; i++ {
			if bySpaces {
				drive(t, a, key(" "), key(" "))
			} else {
				runCmd(a.showPage(pageHome))
			}
			if !a.at(pageHome) {
				t.Fatalf("trip %d did not reach home", i+1)
			}
			seen = append(seen, a.notices.current[slotHome])
			a.leavePlace()
		}
		if bySpaces && !a.notices.retired("home-by-two-spaces") {
			t.Fatal("two spaces did not retire the way-home tip")
		}
		return seen
	}
	bySpaces, byTab := trips(true), trips(false)
	if strings.Join(bySpaces, " ") != strings.Join(byTab, " ") {
		t.Fatalf("home's row by two spaces went %q, by the tab %q", bySpaces, byTab)
	}
	distinct := map[string]bool{}
	for _, id := range bySpaces {
		distinct[id] = true
	}
	if len(distinct) < len(bySpaces) {
		t.Fatalf("home's row did not move on across trips by two spaces: %q", bySpaces)
	}
}

// A TIP NOBODY SAW IS NOT A SHOWING (the review of #1487). The slot takes a
// tip the moment it is true, but the keys row draws it only after the quiet
// clock; a person who never pauses must not spend its showings unseen.
func TestAConversationsTipIsCountedWhenDrawnNotWhenTaken(t *testing.T) {
	a, advance := chatTipLab(t)
	const id = "files-after-first-deliverable"
	drive(t, a, key("x"), key("backspace"))
	makeDeliverable(t, a)
	frame(a)
	drive(t, a, chatTipDueMsg{})
	if a.notices.current[slotHint] != id {
		t.Fatalf("the slot holds %q, want %q", a.notices.current[slotHint], id)
	}
	if got := a.notices.ledger.shown(id); got != 0 {
		t.Fatalf("a tip the keys row never drew counted %d showings", got)
	}
	advance(chatTipQuiet)
	frame(a)
	drive(t, a, chatTipDueMsg{})
	if got := a.notices.ledger.shown(id); got != 1 {
		t.Fatalf("the tip the keys row drew counted %d showings, want 1", got)
	}
}

// THE WAIT HOLDS AT LAUNCH TOO (the review of #1487): a window that has just
// opened is one somebody has just started reading.
func TestTheConversationsTipWaitsFromLaunch(t *testing.T) {
	a, advance := chatTipLab(t)
	makeDeliverable(t, a)
	a.Init()
	if got := a.chatTip(); got != "" {
		t.Fatalf("the tip drew the moment the window opened: %q", got)
	}
	advance(chatTipQuiet)
	if got := a.chatTip(); got != deliverTip {
		t.Fatalf("the tip is not up fifteen seconds after launch: %q", got)
	}
}

// Silencing hints silences the conversation's row along with home's.
func TestDisableHintsSilencesTheConversationRow(t *testing.T) {
	a, _ := chatTipLab(t)
	makeDeliverable(t, a)
	if a.noticeHint() == "" {
		t.Fatal("the tip is not up before the toggle")
	}
	a.notices.enabled = false
	if got := a.noticeHint(); got != "" {
		t.Fatalf("a silenced profile still says %q in a conversation", got)
	}
	if got := a.chatTip(); got != "" {
		t.Fatalf("a silenced profile still draws the conversation's tip row: %q", got)
	}
	if y, _ := tipRowOf(a, deliverTip); y >= 0 {
		t.Fatal("a silenced profile still draws the tip on the frame")
	}
}

// The project is at the right end of a conversation's keys row, off the seam,
// still a door — onto the folder chooser — and the keys keep their room.
func TestAConversationKeysRowCarriesTheProjectAtItsRight(t *testing.T) {
	_, a := gated(t)
	a.tilde, a.workspace = "/home/person", "/home/person/projects/parser"
	a.target.where = "/tmp/next-project"
	frame(a)
	if text := ansi.Strip(a.legend(a.width)); strings.Contains(text, targetProjectLead) {
		t.Fatalf("the seam still names the project: %q", text)
	}
	foot := ansi.Strip(a.hintRow(a.width))
	if !strings.HasSuffix(strings.TrimRight(foot, " "), targetProjectLead+"~/projects/parser") || strings.Contains(foot, "next-project") {
		t.Fatalf("the keys row does not end with this conversation's project: %q", foot)
	}
	if got := ansi.StringWidth(foot); got != a.width {
		t.Fatalf("the keys row measures %d cells on a %d-cell frame", got, a.width)
	}
	if !strings.HasPrefix(foot, " "+a.footHint(a.width)) {
		t.Fatalf("the keys row does not begin with the keys: %q", foot)
	}
	if !a.seamProjectSpan.pressable() {
		t.Fatal("the keys row recorded no columns for the path")
	}
	if got := ansi.Cut(foot, a.seamProjectSpan.from, a.seamProjectSpan.to); got != "~/projects/parser" {
		t.Fatalf("the recorded span holds %q, want the path", got)
	}
	// THE DOOR: a press on the path opens the folder chooser, on the keys row
	// and nowhere else.
	frame(a)
	y := markedRowY(a, chromeStatus, 0)
	if _, took := a.seamProjectPress(a.seamProjectSpan.from, seamRowY(a)); took {
		t.Fatal("a press on the seam where the path used to be still opened the chooser")
	}
	cmd, took := a.seamProjectPress(a.seamProjectSpan.from, y)
	if !took {
		t.Fatal("a press on the path was not taken as the folder door")
	}
	if cmd != nil {
		if msg := waitOut(cmd); msg != nil {
			drive(t, a, msg)
		}
	}
	if !a.folder.open {
		t.Fatal("the folder chooser did not open")
	}
	// THE KEYS KEEP THEIR ROOM: a long path is cut on the right, its root kept.
	a.closeModals()
	a.workspace = "/home/person/" + strings.Repeat("nested/", 30)
	cut := ansi.Strip(a.hintRow(a.width))
	if !strings.HasPrefix(cut, " "+a.footHint(a.width)) {
		t.Fatalf("a long path cost the keys a clause: %q", cut)
	}
	if !strings.Contains(cut, targetProjectLead+"~/nested/") || !strings.HasSuffix(strings.TrimRight(cut, " "), "…") {
		t.Fatalf("a long path was not cut on the right with its root kept: %q", cut)
	}
	for width := 1; width <= 240; width++ {
		line := ansi.Strip(a.hintRow(width))
		if ansi.StringWidth(line) > width {
			t.Fatalf("at %d cells the keys row overflowed: %q", width, line)
		}
	}
	a.workspace = ""
	if text := ansi.Strip(a.hintRow(a.width)); strings.Contains(text, targetProjectLead) {
		t.Fatalf("unknown project left a label behind: %q", text)
	}
}

// Enter on `/attach` in the command list opens the browser at once — in a
// conversation and on home — the way enter on `/folder` does; the row with a
// placeholder is still there for a typed path.
func TestEnterOnAttachInTheListOpensTheBrowserAtOnce(t *testing.T) {
	a, _ := sheetApp(t)
	drive(t, a, key("/"), key("a"), key("t"), key("t"), key("a"), key("c"), key("h"))
	if !a.menu.open {
		t.Fatal("typing /attach did not open the command list")
	}
	chosen, ok := a.menu.choice()
	if !ok || chosen.name != "attach" || chosen.args != "" {
		t.Fatalf("the cursor is on %q %q, want the bare /attach row", chosen.name, chosen.args)
	}
	drive(t, a, key("enter"))
	if !a.folder.open {
		t.Fatalf("enter on /attach did not open the browser; the box holds %q", a.input.String())
	}
	if !a.input.empty() {
		t.Fatalf("enter on /attach left %q in the box", a.input.String())
	}
	// And on home, over the lab whose home can open the browser
	// (homefate_test.go's [TestBareAttachAtHomeOpensTheBrowserForTheTarget]).
	h, _, _ := mixedLab(t)
	runCmd(h.openHome())
	drive(t, h, key("/"), key("a"), key("t"), key("t"), key("a"), key("c"), key("h"))
	if !h.home.cmd.open {
		t.Fatal("typing /attach on home did not open the command list")
	}
	drive(t, h, key("enter"))
	if !h.folder.open || !h.folder.forTarget {
		t.Fatalf("enter on /attach on home did not open the browser aimed at the target; the box holds %q", h.home.box.String())
	}
}

// WITH NO CONVERSATION STORE BEHIND THE WINDOW — memory off — THE PLACE
// REFUSES, and says which silence this is. It matched conversations by their
// name and project for one build on 2026-09-22, the way home's box does, and
// the owner took that back on 2026-09-23: a place called `search` that
// searches something narrower than it says is worse than one that refuses,
// because half a search reads exactly like a whole one that found nothing.
func TestWithNoIndexTheSearchPlaceSaysSoAndSearchesNothing(t *testing.T) {
	a := placeApp(t)
	a.searchStore = nil
	a.searchArm = func(int) tea.Cmd { return nil }
	a.showPage(pageSearch)
	_, world := searchFixture()
	a.search.world = world
	a.rebuildSearch()
	if text := placeFrameText(a); !strings.Contains(text, "no index of this machine's conversations") {
		t.Fatalf("the place does not say it has no index:\n%s", text)
	}
	// And typing does not send a read, nor draw a result under the words.
	typeInto(t, a, "swarm")
	if cmd := a.searchTick(searchTickMsg{gen: a.search.ask.gen}); cmd != nil {
		t.Fatal("a place with no index sent a store read")
	}
	text := placeFrameText(a)
	if !strings.Contains(text, "no index of this machine's conversations") {
		t.Fatalf("the refusal went away once words were typed:\n%s", text)
	}
	if strings.Contains(strings.ToLower(text), "swarm splitting") {
		t.Fatalf("a place with no index drew a conversation it matched by name:\n%s", text)
	}
	if strings.Contains(text, searchNothingSaid("swarm")) {
		t.Fatalf("a search that never happened claimed nothing was said:\n%s", text)
	}
}

// `/search` typed on home opens the place, and typing there searches — the
// road the owner walked.
func TestSlashSearchOnHomeOpensThePlaceAndTypingSearches(t *testing.T) {
	lab := newHomeLab(t)
	a := lab.door("")
	a.searchStore = nil
	a.searchArm = func(int) tea.Cmd { return nil }
	a.showPage(pageHome)
	drive(t, a, key("/"), key("s"), key("e"), key("a"), key("r"), key("c"), key("h"), key("enter"))
	if !a.at(pageSearch) {
		t.Fatalf("/search on home did not open the search place; %v is showing", a.showing())
	}
	drive(t, a, key("p"), key("a"), key("r"))
	if got := a.search.query.String(); got != "par" {
		t.Fatalf("typing on the search place put %q in its box", got)
	}
	if a.search.ask.query != "par" {
		t.Fatalf("the place is answering for %q", a.search.ask.query)
	}
}

// A quoted path with spaces still reaches the tray from the list's typed row.
func TestTheTypedAttachRowStillTakesAPath(t *testing.T) {
	a, _ := sheetApp(t)
	dir := t.TempDir()
	shot := filepath.Join(dir, "Screen Shot.png")
	if err := os.WriteFile(shot, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	a.slash("/attach '" + shot + "'")
	if len(a.chips) != 1 || a.chips[0].path != shot {
		t.Fatalf("the typed row did not attach the picture: %+v", a.chips)
	}
}
