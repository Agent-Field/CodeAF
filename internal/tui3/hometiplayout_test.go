package tui3

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// ── THE TIP ROW'S SHAPE, THE KEYS ROW'S RIGHT, AND THE ROW THAT SILENCES THEM ──

// homeFrameLines is home's frame as a reader sees it, one string per row.
func homeFrameLines(a *app) []string {
	width, height := a.size()
	lines, _, _, _ := a.homeFrame(width, height)
	out := make([]string, len(lines))
	for i, line := range lines {
		out[i] = ansi.Strip(line)
	}
	return out
}

// The tip is right-aligned over the rule, led by the bulb and closed by the
// cross, ending one cell in from the edge — and the cross blanks the row for
// the rest of this visit to home.
func TestHomeTipIsRightAlignedWithABulbAndACrossThatBlanksTheRow(t *testing.T) {
	lab := newHomeLab(t)
	a := lab.door("")
	a.showPage(pageHome)
	tip := a.noticeHomeHint()
	if tip == "" {
		t.Fatal("home opened with no tip")
	}
	width, _ := a.size()
	rows := homeFrameLines(a)
	if a.tipRow < 0 || a.tipRow >= len(rows) {
		t.Fatalf("the draw recorded the tip on row %d of %d", a.tipRow, len(rows))
	}
	row := rows[a.tipRow]
	cross := a.pal.glyph(tokens.GFailed)
	if !strings.HasSuffix(row, homeTipLead+homeTipGap+tip+homeTipGap+cross) {
		t.Fatalf("the tip row does not end with the bulb, the tip and the cross: %q", row)
	}
	if got := ansi.StringWidth(row); got != width-1 {
		t.Fatalf("the tip row measures %d cells on a %d-cell frame, want %d", got, width, width-1)
	}
	if !strings.HasPrefix(row, " ") {
		t.Fatalf("the tip row is not right-aligned: %q", row)
	}
	// The rule is the very next row.
	if a.targetRow != a.tipRow+1 {
		t.Fatalf("the tip is on row %d and the rule on row %d; they should be neighbours", a.tipRow, a.targetRow)
	}
	// THE CROSS. A press on it says ENOUGH FOR NOW and the row goes blank; a
	// press beside it does nothing.
	if !a.tipCloseSpan.pressable() {
		t.Fatal("the draw recorded no columns for the cross")
	}
	a.homePress(a.tipCloseSpan.from-4, a.tipRow)
	if a.noticeHomeHint() != tip {
		t.Fatal("a press on the tip's words put it away")
	}
	was := a.notices.current[slotHome]
	a.homePress(a.tipCloseSpan.from, a.tipRow)
	if got := a.noticeHomeHint(); got != "" {
		t.Fatalf("the cross answered with another tip: %q", got)
	}
	if strings.Contains(homeText(a), tip) {
		t.Fatal("the tip is still drawn after its cross was pressed")
	}
	// AND THE ONE PUT AWAY KEEPS ITS WHOLE ALLOWANCE: it was not retired, and
	// no showing was spent on the gesture.
	if a.notices.retired(was) {
		t.Fatal("putting a tip away retired it")
	}
	if got := a.notices.ledger.shown(was); got != 0 {
		t.Fatalf("the cross spent %d showings of the tip it put away", got)
	}
	// LEAVING HOME AND COMING BACK IS WHAT BRINGS ONE, and it is a different
	// one.
	runCmd(a.showPage(pageNone))
	runCmd(a.showPage(pageHome))
	back := a.noticeHomeHint()
	if back == "" {
		t.Fatal("the next visit to home brought no tip back")
	}
	if back == tip {
		t.Fatalf("the next visit brought back the tip the cross put away: %q", back)
	}
}

// A frame too narrow for the bulb, a word and the cross draws no tip row at
// all rather than a bulb beside nothing.
func TestHomeTipRowSaysNothingOnAFrameTooNarrowForIt(t *testing.T) {
	lab := newHomeLab(t)
	a := lab.door("")
	a.showPage(pageHome)
	line, span := a.tipLine("/ask answers right here", 12, a.pal)
	if line != "" || span.pressable() {
		t.Fatalf("a 12-cell frame drew a tip row: %q", line)
	}
	line, span = a.tipLine("/ask answers right here without opening a conversation", 40, a.pal)
	if line == "" || !span.pressable() {
		t.Fatal("a 40-cell frame drew no tip row")
	}
	if got := ansi.StringWidth(ansi.Strip(line)); got != 39 {
		t.Fatalf("the cut tip row measures %d cells, want 39", got)
	}
}

// The project is at the right end of the keys row, and the keys keep their
// room: the path is cut on the right where they leave it too little, and gone
// where they leave it less than a word. It is still the folder door.
func TestHomeKeysRowCarriesTheProjectAtItsRight(t *testing.T) {
	lab := newHomeLab(t)
	a := lab.door("")
	a.showPage(pageHome)
	width, _ := a.size()
	rows := homeFrameLines(a)
	if a.footRow != len(rows)-1 {
		t.Fatalf("the keys row is recorded on row %d of %d", a.footRow, len(rows))
	}
	foot := rows[a.footRow]
	project := a.targetProject()
	if project == "" {
		t.Fatal("the lab's home has no project to name")
	}
	if !strings.HasSuffix(foot, targetProjectLead+project) {
		t.Fatalf("the keys row does not end with the project: %q", foot)
	}
	if got := ansi.StringWidth(foot); got != width-1 {
		t.Fatalf("the keys row measures %d cells on a %d-cell frame, want %d", got, width, width-1)
	}
	if !strings.HasPrefix(foot, " "+homeOptionsWord) {
		t.Fatalf("the keys row does not begin with the keys: %q", foot)
	}
	// AND THE RULE NO LONGER NAMES IT.
	if strings.Contains(rows[a.targetRow], targetProjectLead) {
		t.Fatalf("the rule still carries the project: %q", rows[a.targetRow])
	}
	// THE DOOR. The path's columns are the folder door, on the keys row.
	if !a.targetFolderSpan.pressable() {
		t.Fatal("the keys row recorded no columns for the path")
	}
	if got := ansi.Cut(foot, a.targetFolderSpan.from, a.targetFolderSpan.to); got != project {
		t.Fatalf("the recorded span holds %q, want the path %q", got, project)
	}
	if _, took := a.placeTargetPress(a.targetFolderSpan.from, a.footRow); !took {
		t.Fatal("a press on the path was not taken as the folder door")
	}
	if _, took := a.placeTargetPress(a.targetFolderSpan.from, a.targetRow); took {
		t.Fatal("a press on the rule where the path used to be still opened the door")
	}

	// THE KEYS KEEP THEIR ROOM. A long path is cut on the right, one ellipsis,
	// and the keys are whole.
	a.target.where = "/tmp/" + strings.Repeat("nested/", 30)
	keys := hintFit(a.placeHint(), width-2)
	cut := ansi.Strip(a.homeFootLine(width, a.pal))
	if !strings.HasPrefix(cut, " "+keys) {
		t.Fatalf("a long path cost the keys a clause: %q", cut)
	}
	if !strings.Contains(cut, targetProjectLead+"/tmp/nested/") || !strings.HasSuffix(cut, "…") {
		t.Fatalf("a long path was not cut on the right with its root kept: %q", cut)
	}
	if got := ansi.StringWidth(cut); got != width-1 {
		t.Fatalf("the cut keys row measures %d cells, want %d", got, width-1)
	}
	// And where the keys leave less than a word, the path goes entirely.
	narrow := ansi.Strip(a.homeFootLine(ansi.StringWidth(keys)+2+hudGap+len(targetProjectLead)+2, a.pal))
	if strings.Contains(narrow, targetProjectLead) {
		t.Fatalf("a keys row with no room for a word of path still drew the label: %q", narrow)
	}
	if a.targetFolderSpan.pressable() {
		t.Fatal("a keys row with no path left a door recorded")
	}
}

// The row that silences the tips is `disable hints` on the Workspace tab, off
// by default, and it reads the other way up from the key underneath it.
func TestDisableHintsIsAWorkspaceRowThatReadsTheOtherWayUp(t *testing.T) {
	a, dir := sheetApp(t)
	a.openSettings()
	cursorTo(t, a, config.KeyHints)
	item := a.sheet.items[a.sheet.cursor]
	if item.meta.tab != tabWorkspace || item.meta.label != "disable hints" {
		t.Fatalf("the hints row is %q on the %s tab, want \"disable hints\" on Workspace", item.meta.label, item.meta.tab)
	}
	if got := item.row.Value(); got != "off" {
		t.Fatalf("a fresh profile reads %q, want off (hints shown)", got)
	}
	if !config.HintsAt(dir) {
		t.Fatal("a fresh profile has hints off underneath")
	}
	drive(t, a, key("enter"))
	if config.HintsAt(dir) {
		t.Fatal("flipping disable hints on did not silence the tips")
	}
	a.closeSettings()
	a.openSettings()
	cursorTo(t, a, config.KeyHints)
	if got := a.sheet.items[a.sheet.cursor].row.Value(); got != "on" {
		t.Fatalf("after the flip the row reads %q, want on", got)
	}
}
