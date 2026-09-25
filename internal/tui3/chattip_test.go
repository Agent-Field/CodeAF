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
// A conversation's tip is the LOWEST RUNG OF THE KEYS ROW at the foot, decided
// by the events that prove what is happening and drawn whenever the frame is
// quiet. It had a row of its own over the rule for one build on 2026-09-22 —
// with a quiet minute before it appeared, a two-minute rotation and a cross —
// and the owner put it back here. Home's row keeps that newer shape, and
// hometip_test.go holds it to that. The project came down off the seam to the
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

// THE TIP IS THE KEYS ROW'S LOWEST RUNG, on no clock at all: the event that
// arms it puts it there, and it is drawn from that moment on while the frame is
// quiet. A key in the box takes the row back because the row belongs to the
// sentence being written, and emptying the box gives it back at once — no
// minute, no beat, no cross.
func TestAConversationSaysItsTipOnTheKeysRow(t *testing.T) {
	a, _ := chatTipLab(t)
	b := &a.notices
	makeDeliverable(t, a)
	if b.current[slotHint] != "files-after-first-deliverable" {
		t.Fatalf("an export landing armed %q", b.current[slotHint])
	}
	if got := a.noticeHint(); got != deliverTip {
		t.Fatalf("the tip is not up the moment it arms: %q", got)
	}

	// ON THE FRAME: the foot, under the box, and NOT a row of its own over the
	// rule.
	if got := plain(a.footHint(a.width)); !strings.Contains(got, deliverTip) {
		t.Fatalf("the keys row does not carry the tip: %q", got)
	}
	y, rows := tipRowOf(a, deliverTip)
	if y < 0 {
		t.Fatalf("the tip is not on the frame:\n%s", strings.Join(rows, "\n"))
	}
	if y+1 < len(rows) && strings.HasPrefix(rows[y+1], "─") {
		t.Fatalf("the tip is sitting over the rule again:\n%s", strings.Join(rows, "\n"))
	}
	// AND IT WEARS NO BULB AND NO CROSS. Those belong to home's row.
	cross := a.pal.glyph(tokens.GFailed)
	if row := rows[y]; strings.Contains(row, homeTipLead) || strings.HasSuffix(strings.TrimRight(row, " "), cross) {
		t.Fatalf("the conversation's tip wears home's bulb or cross: %q", row)
	}

	// A LETTER IN THE BOX TAKES THE ROW; emptying it gives the row back.
	drive(t, a, key("x"))
	if got := a.noticeHint(); got != "" {
		t.Fatalf("the tip drew over a box with a letter in it: %q", got)
	}
	drive(t, a, key("backspace"))
	if got := a.noticeHint(); got != deliverTip {
		t.Fatalf("emptying the box did not give the row back: %q", got)
	}

	// A RUNNING TURN TAKES IT TOO, and every state with keys of its own.
	a.state = stateWorking
	if got := a.noticeHint(); got != "" {
		t.Fatalf("the tip drew over a running turn: %q", got)
	}
	a.state = stateIdle
	a.showPage(pageSpend)
	if got := a.noticeHint(); got != "" {
		t.Fatalf("the tip drew under a place: %q", got)
	}
	a.leavePlace()
	if got := a.noticeHint(); got != deliverTip {
		t.Fatalf("leaving the place did not give the row back: %q", got)
	}
}

// THE DOOR HOME STAYS BESIDE THE TIP (2026-09-24). The tip used to take the
// whole keys row, so from a conversation's first exchange onward
// `space space home` was named nowhere on it. It rides after the tip now, and a
// narrow frame gives up the tip before the door.
func TestTheDoorHomeStaysBesideTheConversationsTip(t *testing.T) {
	a, _ := chatTipLab(t)
	makeDeliverable(t, a)
	want := deliverTip + hintSegment + homeDoorWord
	if got := plain(a.footHint(a.width)); got != want {
		t.Fatalf("the keys row reads %q, want %q", got, want)
	}
	if y, rows := tipRowOf(a, want); y < 0 {
		t.Fatalf("the frame does not carry the tip and the door together:\n%s", strings.Join(rows, "\n"))
	}
	if !a.homeDoor.holds(a.homeDoor.from) {
		t.Fatal("the door beside the tip was drawn but not recorded for a click")
	}
	if got := a.hintShorter(want); got != homeDoorWord {
		t.Fatalf("a narrow frame kept %q, want the door alone", got)
	}
	if got := a.hintShorter(homeDoorWord); got != "" {
		t.Fatalf("the door alone shortened to %q", got)
	}

	// A letter in the box takes the tip and the door together.
	drive(t, a, key("x"))
	if got := plain(a.footHint(a.width)); strings.Contains(got, homeDoorWord) {
		t.Fatalf("the door is still named over a box with a letter in it: %q", got)
	}
}

// THE TIP ABOUT THE DOOR (2026-09-24) is said in a conversation after its first
// exchange and never on home, and while it stands the row does not name the
// door a second time after it.
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
	a.leavePlace()
	if got := a.tipWithHomeDoor(row.text); got != row.text {
		t.Fatalf("the way-home tip named the door twice: %q", got)
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
	if got := plain(a.footHint(a.width)); strings.Contains(got, deliverTip) {
		t.Fatalf("a silenced profile still draws the tip on the keys row: %q", got)
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
