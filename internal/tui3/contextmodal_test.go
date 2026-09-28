package tui3

// contextmodal_test.go is the CHOOSER AS A MODAL: that it is one surface both
// doors open, that it is a bounded framed sheet rather than a list in the bottom
// chrome, that it owns the keyboard and the pointer while it is up, that the
// conversation under it neither competes nor acts, and that leaving it changes
// nothing.
//
// The owner met every one of these as a defect on a real Mac binary
// (../reports/modal.md records the screenshot and the four roots), so each test
// below is written against the FRAME — what a person would actually see — rather
// than against the browser's own state.

import (
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// modalLab is a surface over a tree with siblings above, children below and a
// folder, some source and a picture to preview — the fixture the real-terminal
// evidence uses, in miniature.
func modalLab(t *testing.T) (*app, *fakeAgent, string) {
	t.Helper()
	root := t.TempDir()
	for _, dir := range []string{
		"alpha", "beta", "gamma", "work/nested/deeper", "work/notes", "work/a space",
	} {
		if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(dir)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "work", "nested", "main.go"),
		[]byte("package nested\n\nfunc Greet() string {\n\treturn \"hello\"\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	agent := &fakeAgent{model: "m"}
	a := newApp(t.Context(), Options{Agent: agent, Workspace: filepath.Join(root, "work"), ProfileDir: t.TempDir()})
	a.width, a.height = 200, 46
	a.pal = newPalette(tokens.ANSI256, false)
	a.entries = nil
	a.folderStoreRead = true
	a.touch()
	return a, agent, root
}

// frameOf is the whole screen as a person sees it, plain.
func frameOf(t *testing.T, a *app) []string {
	t.Helper()
	body, _, _ := a.frameBody()
	return strings.Split(plain(body), "\n")
}

// ── one sheet, both doors ───────────────────────────────────────────────────

// BOTH DOORS OPEN THE SAME SHEET, BROWSING, IN THE SAME PLACE.
//
// This is the owner's first report in one test. `/attach` opened the columns
// immediately and `/folder` opened a flat list of remembered places, so the two
// commands showed two different surfaces — and on a machine that remembered
// nothing at all `/folder` refused with a sentence instead of opening.
func TestBothDoorsOpenTheSameBrowsingSheet(t *testing.T) {
	a, _, root := modalLab(t)
	work := filepath.Join(root, "work")

	settleFolder(t, a, a.openFolderPick(""))
	if !a.folder.open || !a.folder.browsing {
		t.Fatalf("/folder opened browsing=%v open=%v", a.folder.browsing, a.folder.open)
	}
	folderAt := a.folder.cols.dir
	folderNames := strings.Join(a.folder.cols.here.names, ",")
	a.folder.close()

	settleFolder(t, a, a.openContextPick("", false))
	if !a.folder.open || !a.folder.browsing {
		t.Fatalf("a bare /attach opened browsing=%v open=%v", a.folder.browsing, a.folder.open)
	}
	if a.folder.cols.dir != folderAt {
		t.Fatalf("the two doors opened on %s and %s", folderAt, a.folder.cols.dir)
	}
	if got := strings.Join(a.folder.cols.here.names, ","); got != folderNames {
		t.Fatalf("the two doors drew %q and %q", folderNames, got)
	}
	if folderAt != work {
		t.Fatalf("the chooser opened on %s, want the folder this window works in", folderAt)
	}
}

// AND A MACHINE THAT REMEMBERS NOTHING IS STILL SHOWN ITS OWN TREE. The refusal
// that used to stand here — `nothing to offer yet · type a path after /folder` —
// met the one person least able to type the path.
func TestAFreshProfileIsBrowsedRatherThanRefused(t *testing.T) {
	a, _, root := modalLab(t)
	a.home.world = session.World{}
	a.folderStore = folderStore{}
	a.entries = nil
	notes := len(a.entries)

	settleFolder(t, a, a.openFolderPick(""))
	if !a.folder.open || !a.folder.browsing {
		t.Fatal("/folder refused a profile with nothing remembered")
	}
	if a.folder.cols.dir != filepath.Join(root, "work") {
		t.Fatalf("the chooser opened on %s", a.folder.cols.dir)
	}
	if len(a.entries) != notes {
		t.Fatalf("opening the chooser wrote %d lines into the conversation", len(a.entries)-notes)
	}
}

// THE FOLDER THIS CONVERSATION IS ALREADY ABOUT IS WHERE IT OPENS, ahead of the
// window's own working directory ([app.contextStart]).
func TestTheChooserOpensOnTheFolderTheConversationHolds(t *testing.T) {
	a, agent, root := modalLab(t)
	held := filepath.Join(root, "alpha")
	agent.places = []session.PlaceRef{{Path: held, Arrival: session.PlaceSaid}}

	settleFolder(t, a, a.openFolderPick(""))
	if a.folder.cols.dir != held {
		t.Fatalf("the chooser opened on %s, want %s", a.folder.cols.dir, held)
	}
}

// ── it is a sheet, and it is bounded ────────────────────────────────────────

// THE SHEET IS FRAMED, CENTRED AND BOUNDED, and it does not grow with the
// terminal. The screenshot the owner sent is a two-hundred-column terminal with
// a column of names on the left and a hundred cells of nothing beside it.
func TestTheSheetIsBoundedAndFramedOnAWideTerminal(t *testing.T) {
	a, _, root := modalLab(t)
	openBrowse(t, a, filepath.Join(root, "work"))

	for _, width := range []int{120, 200, 340} {
		a.width = width
		a.touch()
		rows := frameOf(t, a)
		win := a.folder.win
		if win.width > contextSheetWide {
			t.Fatalf("at %d cells the sheet is %d wide", width, win.width)
		}
		if win.height > contextSheetTall {
			t.Fatalf("at %d cells the sheet is %d rows tall", width, win.height)
		}
		if win.left < 1 || win.left+win.width > width {
			t.Fatalf("at %d cells the sheet sits at %d..%d", width, win.left, win.left+win.width)
		}
		// It is centred within a cell of the middle.
		if off := win.left - (width-win.width)/2; off != 0 {
			t.Fatalf("at %d cells the sheet is %d cells off centre", width, off)
		}
		// Every row of the sheet is the same width, which is what makes it read as
		// a rectangle rather than as a list with a line drawn near it.
		for at := win.top; at < win.top+win.height && at < len(rows); at++ {
			row := rows[at]
			if len(row) <= win.left || row[win.left] == ' ' {
				t.Fatalf("at %d cells row %d has no left edge: %q", width, at, row)
			}
		}
		if got := len(rows); got != a.height {
			t.Fatalf("at %d cells the frame drew %d rows into %d", width, got, a.height)
		}
	}
}

// THE TITLE, THE LOCATION AND BOTH ACTIONS ARE ON THE SHEET. A modal whose only
// way out is a key nobody was told about is a modal people get stuck in.
func TestTheSheetNamesItselfAndBothWaysOut(t *testing.T) {
	a, _, root := modalLab(t)
	openBrowse(t, a, filepath.Join(root, "work"))
	drawn := strings.Join(frameOf(t, a), "\n")

	for _, want := range []string{contextTitleWord, contextCancelWord, folderAddWord, "work"} {
		if !strings.Contains(drawn, want) {
			t.Fatalf("%q is not on the sheet:\n%s", want, drawn)
		}
	}
}

// THE CARET IS IN THE SHEET'S OWN BOX, and not down in a draft the sheet is
// drawn over. It used to borrow the message box's position, which is where the
// conversation's own draft lives.
func TestTheCaretIsInTheSheetsBox(t *testing.T) {
	a, _, root := modalLab(t)
	openBrowse(t, a, filepath.Join(root, "work"))
	_, caretX, caretY := a.frameBody()
	win := a.folder.win
	if caretY != win.boxY {
		t.Fatalf("the caret is on row %d, want the box at %d", caretY, win.boxY)
	}
	if caretX < win.left || caretX >= win.left+win.width {
		t.Fatalf("the caret is at column %d, outside the sheet at %d..%d",
			caretX, win.left, win.left+win.width)
	}
}

// ── the conversation underneath ─────────────────────────────────────────────

// A PRESS OUTSIDE THE SHEET REACHES NOTHING. It may not switch a tab, open a
// tool call or answer a question behind a sheet somebody is looking at — and it
// does not dismiss either, because a sheet holding chosen things must not throw
// them away because an aim was off.
func TestAPressOutsideTheSheetReachesNothing(t *testing.T) {
	a, _, root := modalLab(t)
	openBrowse(t, a, filepath.Join(root, "work"))
	drive(t, a, key(folderMarkKey))
	if len(a.folder.marks) != 1 {
		t.Fatalf("the mark did not land: %+v", a.folder.marks)
	}
	at, marks := a.folder.cols.dir, len(a.folder.marks)
	page := a.page

	// The tab bar, the transcript above the sheet, and the draft below it.
	for _, y := range []int{0, 1, a.folder.win.top - 2, a.height - 2, a.height - 1} {
		if y < 0 || y >= a.height {
			continue
		}
		drive(t, a, tea.MouseClickMsg{X: 2, Y: y, Button: tea.MouseLeft})
		drive(t, a, tea.MouseReleaseMsg{X: 2, Y: y, Button: tea.MouseLeft})
	}
	if !a.folder.open {
		t.Fatal("a press on the backdrop closed the sheet")
	}
	if a.folder.cols.dir != at || len(a.folder.marks) != marks {
		t.Fatalf("a press on the backdrop moved the sheet to %s with %d marks",
			a.folder.cols.dir, len(a.folder.marks))
	}
	if a.page != page {
		t.Fatalf("a press on the backdrop opened %v", a.page)
	}
}

// AND NEITHER DOES THE WHEEL OR THE POINTER. The conversation under a modal is
// not live: it may not scroll, and nothing in it may light.
func TestTheBackdropNeitherScrollsNorLights(t *testing.T) {
	a, _, root := modalLab(t)
	openBrowse(t, a, filepath.Join(root, "work"))
	scroll := a.bodyScroll()

	drive(t, a, tea.MouseWheelMsg{X: 2, Y: 1, Button: tea.MouseWheelUp})
	if a.bodyScroll() != scroll {
		t.Fatalf("a wheel over the backdrop scrolled the conversation to %d", a.bodyScroll())
	}
	drive(t, a, tea.MouseMotionMsg{X: 2, Y: 1})
	if a.hot.kind != hoverNothing {
		t.Fatalf("the pointer over the backdrop lit %v", a.hot.kind)
	}
	// And over the sheet's own rows it lights the sheet.
	geom := chooserGeom(t, a)
	drive(t, a, tea.MouseMotionMsg{X: chooserX(t, a, geom.here.from+2),
		Y: chooserRowY(t, a, geom.head)})
	if a.hot.kind != hoverOverlay {
		t.Fatalf("the pointer over the columns lit %v", a.hot.kind)
	}
}

// ── leaving ─────────────────────────────────────────────────────────────────

// contextExitMessages opens a batch just as the terminal loop does, so a close
// can be checked for both the repaint message and any work it still carries.
func contextExitMessages(msg tea.Msg) []tea.Msg {
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		return []tea.Msg{msg}
	}
	var out []tea.Msg
	for _, cmd := range batch {
		if cmd != nil {
			out = append(out, contextExitMessages(cmd())...)
		}
	}
	return out
}

// contextExitAsksForTheScreen reports whether a close carried Bubble Tea's
// whole-screen repaint message. Its concrete type is deliberately private, so
// the public constructor supplies the type the test compares.
func contextExitAsksForTheScreen(msgs []tea.Msg) bool {
	want := reflect.TypeOf(tea.ClearScreen())
	for _, msg := range msgs {
		if reflect.TypeOf(msg) == want {
			return true
		}
	}
	return false
}

// EVERY DOOR OUT ASKS FOR THE WHOLE SCREEN BACK. The action door also keeps the
// work it was carrying; repainting may not turn choosing something into merely
// closing the sheet.
func TestClosingTheSheetAsksForTheWholeScreenBack(t *testing.T) {
	t.Run("esc", func(t *testing.T) {
		a, _, root := modalLab(t)
		openBrowse(t, a, filepath.Join(root, "work"))
		cmd := a.folderKey(tea.KeyPressMsg{Code: tea.KeyEscape})
		if a.folder.open {
			t.Fatal("esc left the sheet open")
		}
		if cmd == nil || !contextExitAsksForTheScreen(contextExitMessages(cmd())) {
			t.Fatal("esc did not ask for the whole screen back")
		}
	})

	t.Run("cancel words", func(t *testing.T) {
		a, _, root := modalLab(t)
		openBrowse(t, a, filepath.Join(root, "work"))
		_ = frameOf(t, a)
		win := a.folder.win
		cmd, took := a.contextModalPress(win.cancel.from, win.cancelY)
		if !took || a.folder.open {
			t.Fatalf("cancel took=%v open=%v", took, a.folder.open)
		}
		if cmd == nil || !contextExitAsksForTheScreen(contextExitMessages(cmd())) {
			t.Fatal("the cancel words did not ask for the whole screen back")
		}
	})

	t.Run("chosen things", func(t *testing.T) {
		a, _, root := modalLab(t)
		openBrowse(t, a, filepath.Join(root, "work"))
		if say := a.folder.mark(); say != "" || len(a.folder.marks) != 1 {
			t.Fatalf("the tray holds %+v and said %q", a.folder.marks, say)
		}
		cmd := a.folderConfirm()
		if cmd == nil || a.folder.open {
			t.Fatalf("confirm returned %v with open=%v", cmd, a.folder.open)
		}
		msg := cmd()
		if _, ok := msg.(tea.BatchMsg); !ok {
			t.Fatalf("confirm returned %T, want the repaint and its work in a batch", msg)
		}
		msgs := contextExitMessages(msg)
		if !contextExitAsksForTheScreen(msgs) {
			t.Fatal("confirm did not ask for the whole screen back")
		}
		work := false
		for _, got := range msgs {
			if _, ok := got.(folderTakenMsg); ok {
				work = true
			}
		}
		if !work {
			t.Fatal("confirm lost the work that adds the chosen thing")
		}
	})

	t.Run("nothing to take", func(t *testing.T) {
		a, _, _ := modalLab(t)
		settleFolder(t, a, a.openFolderPick("nothing-could-match-this"))
		if len(a.folder.takes()) != 0 {
			t.Fatalf("the empty action unexpectedly offered %+v", a.folder.takes())
		}
		cmd := a.folderConfirm()
		if cmd == nil || a.folder.open {
			t.Fatalf("empty confirm returned %v with open=%v", cmd, a.folder.open)
		}
		if !contextExitAsksForTheScreen(contextExitMessages(cmd())) {
			t.Fatal("an empty confirm did not ask for the whole screen back")
		}
	})

	t.Run("home esc", func(t *testing.T) {
		a, _, _ := mixedLab(t)
		runCmd(a.openHome())
		settleFolder(t, a, a.homeSlash("/project"))
		cmd := a.folderKey(tea.KeyPressMsg{Code: tea.KeyEscape})
		if a.folder.open || !a.at(pageHome) {
			t.Fatalf("home esc left open=%v page=%v", a.folder.open, a.page)
		}
		if cmd == nil || !contextExitAsksForTheScreen(contextExitMessages(cmd())) {
			t.Fatal("home esc did not ask for the whole screen back")
		}
	})

	t.Run("home choice", func(t *testing.T) {
		a, _, _ := mixedLab(t)
		runCmd(a.openHome())
		settleFolder(t, a, a.homeSlash("/project"))
		onFolderRow(t, a, "inner")
		cmd := a.folderConfirm()
		if a.folder.open || !a.at(pageHome) {
			t.Fatalf("home confirm left open=%v page=%v", a.folder.open, a.page)
		}
		if cmd == nil || !contextExitAsksForTheScreen(contextExitMessages(cmd())) {
			t.Fatal("home confirm did not ask for the whole screen back")
		}
	})
}

// THE ORDINARY FRAME AFTER A RESIZED SHEET HOLDS NONE OF THE SHEET'S CELLS, and
// the close asks the renderer to repaint cells beyond the shorter rows too.
func TestTheFrameAfterACloseHoldsNoCellOfTheSheet(t *testing.T) {
	a, _, root := modalLab(t)
	a.width, a.height = 150, 42
	a.touch()
	openBrowse(t, a, filepath.Join(root, "work"))
	_ = frameOf(t, a)
	a.width, a.height = 52, 30
	a.touch()
	_ = frameOf(t, a)
	a.width, a.height = 150, 42
	a.touch()
	_ = frameOf(t, a)

	cmd := a.folderKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	if cmd == nil || !contextExitAsksForTheScreen(contextExitMessages(cmd())) {
		t.Fatal("the resized close did not ask for the whole screen back")
	}
	drawn := strings.Join(frameOf(t, a), "\n")
	for _, left := range []string{contextTitleWord, contextCancelWord, "╭", "╮", "╰", "╯"} {
		if strings.Contains(drawn, left) {
			t.Fatalf("the frame after close still holds %q:\n%s", left, drawn)
		}
	}
}

// A SHORT TERMINAL GIVES UP BROWSER ROWS, THEN THE THIN RULE, THEN THE BOX. The
// title and the foot are the last two rows, and whatever fits ends with the
// visible way out.
func TestAShortTerminalStillDrawsTheWayOut(t *testing.T) {
	for height := 1; height <= 6; height++ {
		t.Run(itoa(height), func(t *testing.T) {
			a, _, root := modalLab(t)
			a.width, a.height = 100, height
			a.touch()
			openBrowse(t, a, filepath.Join(root, "work"))
			rows := frameOf(t, a)
			win := a.folder.win
			if win.height > height || win.top+win.height > height {
				t.Fatalf("the sheet drew %d rows from %d into a %d-row terminal", win.height, win.top, height)
			}
			last := rows[win.top+win.height-1]
			if !strings.Contains(last, contextCancelWord) {
				t.Fatalf("the last sheet row at height %d has no way out: %q", height, last)
			}
			if height == 1 && strings.Contains(rows[win.top], contextTitleWord) {
				t.Fatalf("one row kept the head instead of the foot: %q", rows[win.top])
			}
			if height >= 2 && !strings.Contains(rows[win.top], contextTitleWord) {
				t.Fatalf("height %d gave up the head rule: %q", height, rows[win.top])
			}
			wantBox := height >= 3
			if got := win.boxY >= 0; got != wantBox {
				t.Fatalf("height %d box drawn=%v, want %v", height, got, wantBox)
			}
			if got, want := win.bodyRows, max(height-contextSheetChrome, 0); got != want {
				t.Fatalf("height %d drew %d browser rows, want %d", height, got, want)
			}
			if a.caret != wantBox {
				t.Fatalf("height %d caret=%v with box drawn=%v", height, a.caret, wantBox)
			}
		})
	}
}

// THE HIT MAP NEVER CLAIMS A ROW THE PAINT DID NOT PUT ON SCREEN. A press below
// the terminal is swallowed by the modal, but cannot close it or move its box.
func TestTheSheetNeverClaimsARowTheTerminalDoesNotHave(t *testing.T) {
	for height := 1; height <= 40; height++ {
		t.Run(itoa(height), func(t *testing.T) {
			a, _, root := modalLab(t)
			a.width, a.height = 100, height
			a.touch()
			openBrowse(t, a, filepath.Join(root, "work"))
			_ = frameOf(t, a)
			win := a.folder.win
			if win.top+win.height > height {
				t.Fatalf("the window claims rows through %d on a %d-row terminal", win.top+win.height-1, height)
			}
			if win.cancelY >= height {
				t.Fatalf("cancel is offered on row %d of a %d-row terminal", win.cancelY, height)
			}
			for x := 0; x < a.width; x++ {
				if win.holds(x, height) {
					t.Fatalf("column %d claims the first row below a %d-row terminal", x, height)
				}
			}
			cursor := a.folder.filter.cursor
			cmd, took := a.contextModalPress(win.cancel.from, height)
			if !took || cmd != nil || !a.folder.open || a.folder.filter.cursor != cursor {
				t.Fatalf("below-screen press took=%v cmd=%v open=%v cursor=%d, want swallowed and unchanged",
					took, cmd, a.folder.open, a.folder.filter.cursor)
			}
		})
	}

	for _, height := range []int{-1, 0} {
		a, _, root := modalLab(t)
		openBrowse(t, a, filepath.Join(root, "work"))
		a.caret = true
		rows, _, _ := a.contextSheet(100, height)
		if len(rows) != 0 || a.folder.win != (contextWin{}) || a.caret {
			t.Fatalf("height %d drew %d rows with window %+v and caret=%v", height, len(rows), a.folder.win, a.caret)
		}
	}
}

// THE NATIVE FIXTURE'S PICTURE OPENS AS THE PICTURE IT CLAIMS TO BE. A corrupt
// sample turns the preview row into an error and leaves the fixture unable to
// exercise its reason for existing.
func TestTheNativeFixtureWritesAPictureThatOpens(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is not available")
	}
	root := t.TempDir()
	cmd := exec.Command(bash, "../../scripts/context-modal-native-fixture.sh", "setup", root)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("fixture setup: %v\n%s", err, out)
	}
	locked := filepath.Join(root, "modal project", "no access")
	defer func() {
		if err := os.Chmod(locked, 0o755); err != nil {
			t.Errorf("restore fixture permissions: %v", err)
		}
	}()
	picture, err := os.Open(filepath.Join(root, "modal project", "pixel.png"))
	if err != nil {
		t.Fatal(err)
	}
	defer picture.Close()
	config, err := png.DecodeConfig(picture)
	if err != nil {
		t.Fatalf("pixel.png does not open: %v", err)
	}
	if config.Width != 16 || config.Height != 8 {
		t.Fatalf("pixel.png is %dx%d, want 16x8", config.Width, config.Height)
	}
}

// THE CANCEL TARGET ON THE FOOT RULE IS THE POINTER'S OWN WAY OUT, and it leaves
// exactly as much behind as `esc` does: nothing.
func TestTheCancelTargetLeavesHavingChangedNothing(t *testing.T) {
	a, _, root := modalLab(t)
	typeInto(t, a, "half a sentence")
	draft, notes := a.input.String(), len(a.entries)
	openBrowse(t, a, filepath.Join(root, "work"))
	drive(t, a, key(folderMarkKey))

	_, _, _ = a.frameBody()
	win := a.folder.win
	if !win.cancel.pressable() {
		t.Fatal("the sheet drew no cancel target")
	}
	// It lights before it acts.
	drive(t, a, tea.MouseMotionMsg{X: win.cancel.from, Y: win.cancelY})
	if a.hot.kind != hoverContextCancel {
		t.Fatalf("the pointer over cancel lit %v", a.hot.kind)
	}
	drive(t, a, tea.MouseClickMsg{X: win.cancel.from, Y: win.cancelY, Button: tea.MouseLeft})

	if a.folder.open {
		t.Fatal("cancel left the sheet open")
	}
	if a.input.String() != draft {
		t.Fatalf("cancel came back to the draft %q, want %q", a.input.String(), draft)
	}
	if a.placeChosen != "" || len(a.chips) != 0 {
		t.Fatalf("cancel chose %q / %+v", a.placeChosen, a.chips)
	}
	if len(a.entries) != notes {
		t.Fatalf("cancel wrote %d lines into the conversation", len(a.entries)-notes)
	}
	// And the conversation is back: the sheet is not still on the frame.
	if strings.Contains(strings.Join(frameOf(t, a), "\n"), contextTitleWord) {
		t.Fatal("the sheet is still drawn after cancel")
	}
}

// ── the box is a search ─────────────────────────────────────────────────────

// TYPING A WORD SEARCHES AND CLEARING THE BOX COMES BACK. That is the whole of
// how the remembered places, the projects and the index stay reachable inside
// the sheet now that it opens on the tree rather than on that list.
func TestTypingSearchesAndClearingReturnsToTheColumns(t *testing.T) {
	a, _, root := modalLab(t)
	a.home.world = session.World{Projects: []session.Project{
		{Path: filepath.Join(root, "alpha")},
		{Path: filepath.Join(root, "beta")},
	}}
	settleFolder(t, a, a.openFolderPick(""))
	if !a.folder.browsing {
		t.Fatal("the chooser did not open browsing")
	}
	at := a.folder.cols.dir

	// THE BOX IS EMPTY ON THE OPEN, which is what makes the very first keystroke
	// a search rather than four characters on the end of a path.
	if got := a.folder.filter.String(); got != "" {
		t.Fatalf("the chooser opened with %q in the box", got)
	}
	typeFolder(t, a, "alph")
	if a.folder.browsing {
		t.Fatal("a word turned into a browse")
	}
	if got, ok := a.folder.here(); !ok || got != filepath.Join(root, "alpha") {
		t.Fatalf("the search stopped on %q", got)
	}
	drive(t, a, key("ctrl+u"))
	if !a.folder.browsing || a.folder.cols.dir != at {
		t.Fatalf("clearing the box left the sheet on %q (browsing=%v)", a.folder.cols.dir, a.folder.browsing)
	}
}

// ── the tiers ───────────────────────────────────────────────────────────────

// THE SHEET HOLDS AT THE PLAIN FLOOR: a terminal told not to be styled and one
// that cannot be trusted with a box-drawing character, both at once, which is
// the worst terminal codeaf claims to run on (designlanguage_test.go's own
// statement of this law for the places).
func TestTheSheetHoldsAtThePlainFloor(t *testing.T) {
	a, _, root := modalLab(t)
	a.pal = newThemedPalette(tokens.NoColor, true, themeDark, nil)
	for _, size := range [][2]int{{60, 20}, {100, 30}, {200, 46}} {
		a.width, a.height = size[0], size[1]
		a.touch()
		openBrowse(t, a, filepath.Join(root, "work"))
		frame, _, _ := a.frameBody()
		if strings.Contains(frame, "\x1b[") {
			t.Fatalf("at %dx%d the sheet draws an SGR sequence at NO_COLOR:\n%q", size[0], size[1], frame)
		}
		lines := strings.Split(frame, "\n")
		if len(lines) != a.height {
			t.Fatalf("at %dx%d the sheet drew %d rows into %d", size[0], size[1], len(lines), a.height)
		}
		for _, line := range lines {
			if ansi.StringWidth(line) > a.width {
				t.Fatalf("at %dx%d a row runs past the frame: %q", size[0], size[1], line)
			}
		}
		// The way out is still named, and so is the sheet.
		if !strings.Contains(frame, contextTitleWord) || !strings.Contains(frame, "cancel") {
			t.Fatalf("at %dx%d the plain sheet names neither itself nor the way out:\n%s",
				size[0], size[1], frame)
		}
	}
}

// A NARROW TERMINAL GETS THE WHOLE WINDOW rather than a sheet with margins it
// cannot afford, and the names stay legible in it.
func TestANarrowTerminalGivesTheSheetTheWholeWindow(t *testing.T) {
	a, _, root := modalLab(t)
	a.width, a.height = 40, 18
	a.touch()
	openBrowse(t, a, filepath.Join(root, "work"))
	rows := frameOf(t, a)
	win := a.folder.win
	if win.left != 0 || win.width != a.width {
		t.Fatalf("at %d cells the sheet sits at %d and is %d wide", a.width, win.left, win.width)
	}
	drawn := strings.Join(rows, "\n")
	if !strings.Contains(drawn, "nested") {
		t.Fatalf("the narrow sheet lost the names:\n%s", drawn)
	}
	for _, line := range rows {
		if ansi.StringWidth(line) > a.width {
			t.Fatalf("a narrow row runs past the frame: %q", line)
		}
	}
}

// ── the late answer ─────────────────────────────────────────────────────────

// A PREVIEW THAT ARRIVES AFTER THE PAINT CANNOT MISROUTE A PRESS.
//
// The row map is written by the paint and a preview is read off the loop, so
// there is a frame in which one folder's entries sit behind another folder's
// geometry. A press landing in it would join a row number from one directory
// onto the path of another; the identity check refuses instead
// ([folderPick.paneEntry]).
func TestALatePreviewCannotMisrouteAPressInThePane(t *testing.T) {
	a, _, root := modalLab(t)
	openBrowse(t, a, filepath.Join(root, "work"))
	onFolderRow(t, a, "nested")
	_ = chooserRows(t, a, -1, "")
	if a.folder.geom.paneDir == "" || a.folder.geom.paneBody < 1 {
		t.Fatalf("the pane drew no entry rows for `nested`: %+v", a.folder.geom)
	}
	at := a.folder.cols.dir
	// The cell is resolved BEFORE the stale answer lands, because resolving it
	// paints — and a paint is exactly what closes this window.
	body := chooserRowY(t, a, a.folder.geom.head)
	x := chooserX(t, a, a.folder.geom.pane.from+1)

	// Another folder's answer lands with the geometry still describing this one.
	a.folder.preview = filePreview{
		Kind: previewFolder,
		Key:  previewKey{Path: filepath.Join(root, "alpha")},
		Entries: []previewEntry{
			{Name: "somewhere", Raw: "somewhere", Dir: true},
			{Name: "else", Raw: "else", Dir: true},
		},
	}
	if name, ok := a.folder.paneEntry(0); ok {
		t.Fatalf("a press in the stale pane offered %q", name)
	}
	drive(t, a, tea.MouseClickMsg{X: x, Y: body, Button: tea.MouseLeft})
	if a.folder.cols.dir != at {
		t.Fatalf("a late preview walked the columns to %s", a.folder.cols.dir)
	}
}

// ── the backdrop, on BOTH sides ─────────────────────────────────────────────

// THE SHEET IS WRITTEN INTO THE FADED FRAME, NOT OVER THE ROW.
//
// It used to be pasted as `spaces + sheet`, which dropped everything to the
// RIGHT of it: a window with a rail or a wide status line went blank down one
// side while the same rows on the left stayed faded. A backdrop dim on one side
// and absent on the other is not a backdrop.
func TestTheBackdropSurvivesOnBothSidesOfTheSheet(t *testing.T) {
	a, _, root := modalLab(t)
	// A status line is the widest thing this frame draws and it reaches the right
	// edge, so its rows are the ones that show the defect.
	openBrowse(t, a, filepath.Join(root, "work"))
	over := frameOf(t, a)
	win := a.folder.win

	a.folder.close()
	a.touch()
	under := frameOf(t, a)

	if len(over) != len(under) {
		t.Fatalf("the sheet changed the frame's height: %d against %d", len(over), len(under))
	}
	for at := win.top; at < win.top+win.height && at < len(over); at++ {
		if ansi.StringWidth(over[at]) > a.width {
			t.Fatalf("row %d runs past the frame: %q", at, over[at])
		}
		// Whatever the frame drew past the sheet's right edge is still there.
		tail := func(row string) string {
			if ansi.StringWidth(row) <= win.left+win.width {
				return ""
			}
			return strings.TrimRight(ansi.Cut(row, win.left+win.width, a.width), " ")
		}
		if want, got := tail(under[at]), tail(over[at]); want != got {
			t.Fatalf("row %d lost the frame to the right of the sheet: %q became %q", at, want, got)
		}
	}
}

// ── the type marks ──────────────────────────────────────────────────────────

// THE MARK IS ONE CELL, AND IT IS ONE CELL AT BOTH FLOORS.
//
// A row's columns line up because every lead is the same width. A Nerd Font
// private-use codepoint is tofu on a font that does not carry it and an emoji is
// two cells on some terminals and one on others; either way the sizes stop
// lining up down the column (foldertype.go states the rule).
func TestEveryTypeMarkIsOneCellAtBothFloors(t *testing.T) {
	names := []string{"src", "main.go", "notes.md", "shot.png", "clip.mp4",
		"bundle.tar.gz", "Makefile", ".gitignore"}
	for _, ascii := range []bool{false, true} {
		pal := newThemedPalette(tokens.TrueColor, ascii, themeDark, nil)
		for _, name := range names {
			for _, dir := range []bool{false, true} {
				got := folderTypeGlyph(pal, name, dir)
				if w := ansi.StringWidth(got); w != 1 {
					t.Fatalf("ascii=%v %q dir=%v draws %q at %d cells", ascii, name, dir, got, w)
				}
				if ascii {
					for _, r := range got {
						if r > 127 {
							t.Fatalf("the ascii floor drew %q for %q", got, name)
						}
					}
				}
			}
		}
	}
}

// AND THE KIND COMES FROM THE NAME, cheaply, with a dotfile answered before
// filepath.Ext gets to call the whole of `.gitignore` an extension.
func TestTheTypeOfAThingIsReadFromItsName(t *testing.T) {
	for name, want := range map[string]folderKind{
		"main.go":     folderKindSource,
		"go.mod":      folderKindSource,
		"config.yaml": folderKindSource,
		".gitignore":  folderKindSource,
		"README.md":   folderKindText,
		"server.log":  folderKindText,
		"paper.pdf":   folderKindText,
		"shot.PNG":    folderKindPicture,
		"clip.mp4":    folderKindMedia,
		"song.flac":   folderKindMedia,
		"src.tar.gz":  folderKindBundle,
		"Makefile":    folderKindPlain,
		"codeaf":      folderKindPlain,
	} {
		if got := folderKindOf(name); got != want {
			t.Errorf("%q is kind %v, want %v", name, got, want)
		}
	}
}

// THE ROW THE KEYBOARD IS ON, THE ROW THE POINTER IS ON AND THE ROWS A PERSON
// HAS CHOSEN ARE THREE DIFFERENT STATEMENTS, and the band under the first two
// is the COLUMN's width and never the terminal's.
func TestSelectionHoverAndMarksAreToldApart(t *testing.T) {
	a, _, root := modalLab(t)
	a.pal = newThemedPalette(tokens.TrueColor, false, themeDark, nil)
	openBrowse(t, a, filepath.Join(root, "work"))

	rows := chooserRows(t, a, -1, "")
	geom := a.folder.geom
	cursorRow := geom.head + (a.folder.cols.cursor - a.folder.cols.top)
	otherRow := cursorRow + 1
	if otherRow >= geom.head+geom.body {
		t.Fatal("the fixture has too few rows to tell two of them apart")
	}
	// The selected row carries a ground; the row under it does not.
	if rows[cursorRow] == ansi.Strip(rows[cursorRow]) {
		t.Fatalf("the selected row is unpainted: %q", rows[cursorRow])
	}
	// Hovering another row paints THAT one differently, and does not move the
	// selection.
	hovered := chooserRows(t, a, otherRow, folderColHere)
	if hovered[otherRow] == rows[otherRow] {
		t.Fatalf("the pointer over row %d lit nothing", otherRow)
	}
	if hovered[cursorRow] != rows[cursorRow] {
		t.Fatal("hovering one row repainted the selected one")
	}
	// THE BAND IS THE MIDDLE COLUMN'S WIDTH AND NOTHING WIDER. A stripe across
	// the whole frame would light the ancestry and the preview beside a row that
	// has nothing to do with either.
	here := folderDivide(a.contextInner(), a.folder.pane).here
	for _, banded := range []string{
		folderCellBand("name", here, a.pal, true, false),
		folderCellBand("name", here, a.pal, false, true),
	} {
		if got := ansi.StringWidth(banded); got != here {
			t.Fatalf("a banded row is %d cells wide, want the column's %d", got, here)
		}
	}
	// And a MARK survives moving off the row, which a ground cannot.
	drive(t, a, key(folderMarkKey))
	drive(t, a, key("down"))
	marked := plain(strings.Join(chooserRows(t, a, -1, ""), "\n"))
	if !strings.Contains(marked, folderMarkGlyph(a.pal)) {
		t.Fatalf("the mark did not survive the cursor leaving it:\n%s", marked)
	}
}

// Backdrop cells beside an actionable row are still backdrop. A modal's
// vertical row number alone must never authorize navigation or confirmation.
func TestContextModalLateralPressCannotChooseOrConfirm(t *testing.T) {
	for _, side := range []string{"left", "right"} {
		for _, target := range []string{"list", "action"} {
			t.Run(side+"/"+target, func(t *testing.T) {
				a, _, root := modalLab(t)
				openBrowse(t, a, filepath.Join(root, "work"))
				geom := chooserGeom(t, a)
				win := a.folder.win
				x := win.left - 1
				if side == "right" {
					x = win.left + win.width
				}
				row := geom.head + 1
				if target == "action" {
					row = geom.action
				}
				y := win.bodyY + row
				if win.holds(x, y) || y < win.bodyY || y >= win.bodyY+win.bodyRows {
					t.Fatal("fixture did not target lateral backdrop beside a body row")
				}
				dir, cursor, draft := a.folder.cols.dir, a.folder.cols.cursor, a.input.String()
				drive(t, a, tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft}, tea.MouseReleaseMsg{X: x, Y: y, Button: tea.MouseLeft})
				if !a.folder.open || a.folder.cols.dir != dir || a.folder.cols.cursor != cursor || a.input.String() != draft || a.placeChosen != "" || len(a.chips) != 0 {
					t.Fatalf("lateral backdrop press acted on %s: open=%v dir=%q cursor=%d place=%q", target, a.folder.open, a.folder.cols.dir, a.folder.cols.cursor, a.placeChosen)
				}
				// The identically aligned row remains actionable inside the sheet.
				drive(t, a, tea.MouseClickMsg{X: win.bodyX + geom.here.from + 2, Y: y, Button: tea.MouseLeft})
				if target == "action" {
					if a.folder.open {
						t.Fatal("inside action press no longer confirms")
					}
				} else if a.folder.cols.dir == dir && a.folder.cols.cursor == cursor {
					t.Fatal("inside list press no longer selects")
				}
			})
		}
	}
}

func TestContextModalLateralWheelAndHoverStayOnBackdrop(t *testing.T) {
	for _, side := range []string{"left", "right"} {
		t.Run(side, func(t *testing.T) {
			a, _, root := modalLab(t)
			openBrowse(t, a, filepath.Join(root, "work"))
			geom := chooserGeom(t, a)
			win := a.folder.win
			x := win.left - 1
			if side == "right" {
				x = win.left + win.width
			}
			y := win.bodyY + geom.head + 1
			cursor, scroll := a.folder.cols.cursor, a.bodyScroll()
			drive(t, a, tea.MouseWheelMsg{X: x, Y: y, Button: tea.MouseWheelDown})
			if a.folder.cols.cursor != cursor || a.bodyScroll() != scroll {
				t.Fatal("wheel beside sheet moved selection or backdrop")
			}
			drive(t, a, tea.MouseMotionMsg{X: x, Y: y})
			if a.hot.kind != hoverNothing {
				t.Fatalf("lateral backdrop lit %v", a.hot.kind)
			}
			drive(t, a, tea.MouseWheelMsg{X: win.bodyX + geom.here.from + 2, Y: y, Button: tea.MouseWheelDown})
			if a.folder.cols.cursor == cursor {
				t.Fatal("wheel inside list stopped working")
			}
		})
	}
}
