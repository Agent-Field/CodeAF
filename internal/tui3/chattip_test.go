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

// ── THE CONVERSATION'S TIP ROW, ITS KEYS ROW'S PROJECT, AND TWO DOORS ────────
//
// Since 2026-09-22 a conversation says its tips the way home does — on the row
// over the rule, right-aligned, with a bulb and a cross — and only once the
// person has been quiet for a minute (notice.go's THE CONVERSATION'S CLOCK).
// The project came down off the seam to the right end of the keys row, as it
// did on home. And two of the owner's bug reports from the same day: enter on
// `/attach` in the list opens the browser at once, and the search place finds
// conversations by name when memory is off.

// chatTipLab is a conversation over a clock the test turns by hand.
func chatTipLab(t *testing.T) (*app, func(time.Duration)) {
	t.Helper()
	a, _ := sheetApp(t)
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

// A conversation's row says nothing until the person has been quiet for
// [chatHintIdle]; the one clock measures from the last key; the row then draws
// over the rule with the bulb and the cross, the keys row does not carry it,
// the cross puts it away, the next beat moves the row on, and a key hides it
// again until the next quiet minute.
func TestAConversationSaysATipOnlyAfterAQuietMinute(t *testing.T) {
	a, advance := chatTipLab(t)
	b := &a.notices
	if a.noticeArmIdle() == nil {
		t.Fatal("the surface coming up did not start the clock")
	}
	if a.noticeArmIdle() != nil {
		t.Fatal("a second start armed a second clock")
	}
	startTask(t, a)
	if b.current[slotHint] != "task-page-after-first-task" {
		t.Fatalf("a task starting armed %q", b.current[slotHint])
	}
	if got := a.noticeHint(); got != "" {
		t.Fatalf("the tip drew before a quiet minute: %q", got)
	}
	// A BEAT BEFORE THE MINUTE GOES BACK TO SLEEP for what is left.
	advance(30 * time.Second)
	if cmd := a.noticeIdleBeat(b.idleGen); cmd == nil || b.due {
		t.Fatal("a beat inside the minute did not go back to sleep")
	}
	// A KEY STAMPS THE CLOCK AGAIN, so the minute is measured from it.
	drive(t, a, key("x"), key("backspace"))
	advance(45 * time.Second)
	if cmd := a.noticeIdleBeat(b.idleGen); cmd == nil || b.due {
		t.Fatal("the beat did not measure the minute from the last key")
	}
	advance(chatHintIdle)
	if cmd := a.noticeIdleBeat(b.idleGen); cmd == nil || !b.due {
		t.Fatal("a quiet minute did not make the tip due")
	}
	if got := a.noticeHint(); got != taskPageTip {
		t.Fatalf("after a quiet minute the row reads %q, want the tip", got)
	}
	// ON THE FRAME: the row directly over the rule, right-aligned, bulb and cross.
	y, rows := tipRowOf(a, taskPageTip)
	if y < 0 {
		t.Fatalf("the tip is not on the frame:\n%s", strings.Join(rows, "\n"))
	}
	row := strings.TrimRight(rows[y], " ")
	cross := a.pal.glyph(tokens.GFailed)
	if !strings.HasSuffix(row, homeTipLead+homeTipGap+taskPageTip+homeTipGap+cross) {
		t.Fatalf("the tip row does not end with the bulb, the tip and the cross: %q", row)
	}
	if got := ansi.StringWidth(row); got != a.width-1 {
		t.Fatalf("the tip row measures %d cells on a %d-cell frame, want %d", got, a.width, a.width-1)
	}
	if y+1 >= len(rows) || !strings.HasPrefix(rows[y+1], "─") {
		t.Fatalf("the rule is not the row under the tip:\n%s", strings.Join(rows, "\n"))
	}
	if got := a.footHint(a.width); strings.Contains(got, taskPageTip) {
		t.Fatalf("the keys row still carries the tip: %q", got)
	}
	// THE CROSS. A press on it puts the tip away; a press beside it does not.
	if !a.tipCloseSpan.pressable() {
		t.Fatal("the draw recorded no columns for the cross")
	}
	if a.tipClosePress(a.tipCloseSpan.from-4, y) {
		t.Fatal("a press on the tip's words was taken as the cross")
	}
	if !a.tipClosePress(a.tipCloseSpan.from, y) {
		t.Fatal("a press on the cross was not taken")
	}
	if got := a.noticeHint(); got != "" {
		t.Fatalf("the cross did not put the tip away: %q", got)
	}
	if strings.Contains(plain(frame(a)), taskPageTip) {
		t.Fatal("the tip is still drawn after its cross was pressed")
	}
	if b.retired("task-page-after-first-task") {
		t.Fatal("putting a tip away retired it")
	}
	// THE NEXT BEAT BRINGS A TIP BACK — the ring has one eligible tip here, so
	// it is the same one.
	advance(hintEvery)
	if cmd := a.noticeIdleBeat(b.idleGen); cmd == nil {
		t.Fatal("the beat after a quiet minute did not re-arm")
	}
	if got := a.noticeHint(); got != taskPageTip {
		t.Fatalf("the beat did not bring the tip back: %q", got)
	}
	// AND A KEY HIDES IT until the next quiet minute.
	drive(t, a, key("y"), key("backspace"))
	if b.due || a.noticeHint() != "" {
		t.Fatalf("a key did not stand the tip down: due=%v hint=%q", b.due, a.noticeHint())
	}
	// A PLACE IN FRONT SLEEPS THE MINUTE AGAIN, showing nothing, and a beat
	// from an older arming is dropped.
	a.showPage(pageSpend)
	advance(2 * chatHintIdle)
	if cmd := a.noticeIdleBeat(b.idleGen); cmd == nil || b.due {
		t.Fatal("a beat over a place did not sleep the minute again")
	}
	if cmd := a.noticeIdleBeat(b.idleGen - 1); cmd != nil {
		t.Fatal("a beat from an older arming was not dropped")
	}
	a.leavePlace()
	if a.showing() != nil {
		t.Fatalf("the conversation did not come back; %v is showing", a.showing().id())
	}
	advance(2 * chatHintIdle)
	if cmd := a.noticeIdleBeat(b.idleGen); cmd == nil || !b.due {
		t.Fatal("the conversation coming back and going quiet did not bring the tip")
	}
}

// Silencing hints silences the conversation's row along with home's.
func TestDisableHintsSilencesTheConversationRow(t *testing.T) {
	a, _ := chatTipLab(t)
	startTask(t, a)
	a.notices.due = true
	if a.noticeHint() == "" {
		t.Fatal("the tip is not up before the toggle")
	}
	a.notices.enabled = false
	if got := a.noticeHint(); got != "" {
		t.Fatalf("a silenced profile still says %q in a conversation", got)
	}
	// The clock keeps ticking over a silenced profile, showing nothing, so
	// turning hints back on needs no restart.
	a.noticeArmIdle()
	if cmd := a.noticeIdleBeat(a.notices.idleGen); cmd == nil {
		t.Fatal("a silenced profile stopped the tip clock")
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

// With no conversation store behind the window — memory off — the search place
// matches conversations by their name and project, the way home's box does,
// and says that is what it matched by.
func TestWithNoIndexTheSearchPlaceMatchesConversationsByName(t *testing.T) {
	a := placeApp(t)
	a.searchStore = nil
	a.searchArm = func(int) tea.Cmd { return nil }
	a.showPage(pageSearch)
	_, world := searchFixture()
	a.search.world = world
	a.rebuildSearch()
	if text := placeFrameText(a); !strings.Contains(text, searchByNameWord) {
		t.Fatalf("the empty place does not say it matches by name:\n%s", text)
	}
	typeInto(t, a, "swarm")
	if cmd := a.searchTick(searchTickMsg{gen: a.search.ask.gen}); cmd != nil {
		t.Fatal("a search by name went out as a store read")
	}
	// The row carries the name as home spells it ([homeName]).
	text := strings.ToLower(placeFrameText(a))
	if !strings.Contains(text, "swarm splitting") || strings.Contains(text, "lead research") {
		t.Fatalf("the search by name did not find the conversation called that:\n%s", text)
	}
	if strings.Contains(text, searchNothingSaid("swarm")) {
		t.Fatalf("a search by name claimed nothing was said:\n%s", text)
	}
	// A project name matches too.
	a.search.query.reset()
	typeInto(t, a, "leadgen")
	a.searchTick(searchTickMsg{gen: a.search.ask.gen})
	if text := strings.ToLower(placeFrameText(a)); !strings.Contains(text, "leadgen") || strings.Contains(text, "swarm splitting") {
		t.Fatalf("the search by name did not match on the project:\n%s", text)
	}
	// And nothing named that says so, without claiming nothing was said.
	a.search.query.reset()
	typeInto(t, a, "zzz")
	a.searchTick(searchTickMsg{gen: a.search.ask.gen})
	if text := placeFrameText(a); !strings.Contains(text, "no conversation on this machine is named") {
		t.Fatalf("a miss by name did not say so:\n%s", text)
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
