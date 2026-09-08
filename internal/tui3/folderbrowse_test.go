package tui3

// folderbrowse_test.go is the folder BROWSER: the three successive columns, the
// breadcrumb, the pointer, the separate add action, the folders a conversation
// carries and the one gesture that takes one back off — and, under all of it,
// the law this wave exists for: THE SURFACE DOES NOT SAY A FOLDER WAS ADDED
// UNLESS IT REACHED THE CONVERSATION.

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// browseLab is a surface standing over a tree with something of every kind in
// it: two levels below `here`, a name with a space, a name in another script, a
// hidden folder, one the `@` walk prunes, and one nobody may read.
//
// The store is marked READ so nothing in these tests can start the background
// walk of a real home directory: the picker opens from memory, and memory is
// what this file is about.
func browseLab(t *testing.T) (*app, *fakeAgent, string) {
	t.Helper()
	root := t.TempDir()
	for _, dir := range []string{
		"here/deep/one", "here/deep/two", "here/other", "here/.hidden",
		"here/node_modules", "here/a space", "here/ünïcødé", "sibling",
	} {
		if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(dir)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	agent := &fakeAgent{model: "m"}
	a := newApp(t.Context(), Options{Agent: agent, Workspace: filepath.Join(root, "here")})
	a.width, a.height = 120, 32
	a.pal = newPalette(tokens.ANSI256, false)
	a.entries = nil
	a.folderStoreRead = true
	a.home.world = session.World{Projects: []session.Project{
		{Path: filepath.Join(root, "sibling")},
		{Path: filepath.Join(root, "here")},
	}}
	a.touch()
	return a, agent, root
}

// settle runs one command the way the loop would and feeds everything it
// produced back in, which is how the readdirs this surface asks for land.
func settleFolder(t *testing.T, a *app, cmd tea.Cmd) {
	t.Helper()
	for _, msg := range runCmd(cmd) {
		drive(t, a, msg)
	}
}

// openBrowse opens the picker on one directory and lets every level it wants
// arrive.
func openBrowse(t *testing.T, a *app, dir string) {
	t.Helper()
	settleFolder(t, a, a.openFolderPick(dir+"/"))
	if !a.folder.open || !a.folder.browsing {
		t.Fatalf("the browser did not open on %s", dir)
	}
}

// ── the columns ─────────────────────────────────────────────────────────────

// THE THIRD COLUMN IS THE CHILDREN OF THE ROW UNDER THE CURSOR. That is what
// makes these columns successive rather than two columns and a caption: the
// level you are about to walk into is already drawn when you get there.
func TestTheColumnsAreParentHereAndTheChildrenOfTheCursor(t *testing.T) {
	a, _, root := browseLab(t)
	openBrowse(t, a, filepath.Join(root, "here"))

	if want := []string{"a space", "deep", "other", "ünïcødé"}; strings.Join(a.folder.cols.here.names, ",") != strings.Join(want, ",") {
		t.Fatalf("the middle column is %v, want %v", a.folder.cols.here.names, want)
	}
	if a.folder.cols.upAt < 0 || a.folder.cols.up.names[a.folder.cols.upAt] != "here" {
		t.Fatalf("the parent column does not mark `here`: %v at %d", a.folder.cols.up.names, a.folder.cols.upAt)
	}
	// The cursor is on `a space`, so the third column is what is under it —
	// nothing — and one press down puts `deep`'s two children there.
	drive(t, a, key("down"))
	child := a.folder.read(filepath.Join(root, "here", "deep"))
	if want := []string{"one", "two"}; strings.Join(child.names, ",") != strings.Join(want, ",") {
		t.Fatalf("the children column holds %v, want %v", child.names, want)
	}
	drawn := plain(strings.Join(a.folder.rows(a.width, a.overlayHeight(), a.pal, -1, ""), "\n"))
	for _, want := range []string{"deep", "one", "two", "here"} {
		if !strings.Contains(drawn, want) {
			t.Fatalf("%q is not on the sheet:\n%s", want, drawn)
		}
	}
	// And a name with a space and a name in another script survive whole.
	for _, want := range []string{"a space", "ünïcødé"} {
		if !strings.Contains(drawn, want) {
			t.Fatalf("%q was not drawn as it is spelled:\n%s", want, drawn)
		}
	}
}

// THE PATH IS DRAWN ABOVE THE COLUMNS AND EVERY SEGMENT OF IT IS A PLACE TO GO
// BACK TO.
func TestTheBreadcrumbSaysWhereYouAreAndClicksBackToIt(t *testing.T) {
	a, _, root := browseLab(t)
	openBrowse(t, a, filepath.Join(root, "here", "deep"))

	y := markedRowY(a, chromeOverlay, 0)
	if y < 0 {
		t.Fatal("the sheet drew no rows at all")
	}
	crumbs := a.folder.geom.crumbs
	if len(crumbs) < 2 {
		t.Fatalf("the breadcrumb has %d segments: %+v", len(crumbs), crumbs)
	}
	last := crumbs[len(crumbs)-1]
	if last.name != "deep" || last.path != filepath.Join(root, "here", "deep") {
		t.Fatalf("the breadcrumb ends on %+v, want deep", last)
	}
	// The segment before it is `here`, and pressing it walks back out — with the
	// cursor left on the folder we came from, which is what makes a breadcrumb a
	// way back rather than a jump.
	back := crumbs[len(crumbs)-2]
	if back.name != "here" {
		t.Fatalf("the segment before deep is %q", back.name)
	}
	drive(t, a, tea.MouseClickMsg{X: back.span.from, Y: y, Button: tea.MouseLeft})
	if a.folder.cols.dir != filepath.Join(root, "here") {
		t.Fatalf("the breadcrumb landed on %s", a.folder.cols.dir)
	}
	if got, _ := a.folder.here(); got != filepath.Join(root, "here", "deep") {
		t.Fatalf("the cursor came out onto %q, not the folder it left", got)
	}
	// And the box agrees with the columns, so nothing has to be retyped.
	if !strings.HasSuffix(a.folder.filter.String(), "here/") {
		t.Fatalf("the box says %q", a.folder.filter.String())
	}
}

// A SEARCH RESULT OPENS FOR BROWSING, by the pointer and by `→`. That is the
// whole answer to "the path I searched for is on the screen and I have to type
// it out again".
func TestASearchResultOpensForBrowsingWithoutBeingRetyped(t *testing.T) {
	a, _, root := browseLab(t)
	settleFolder(t, a, a.openFolderPick(""))
	typeFolder(t, a, "sibl")
	if a.folder.browsing {
		t.Fatal("a word turned into a browse")
	}

	// `→` with the caret at the end of the box opens the row under the cursor.
	drive(t, a, key("right"))
	if !a.folder.browsing || a.folder.cols.dir != filepath.Join(root, "sibling") {
		t.Fatalf("→ left the browser on %q (browsing=%v)", a.folder.cols.dir, a.folder.browsing)
	}
	// THE BOX HOLDS A PATH THAT RESOLVES, whole, with only `~` abbreviated —
	// anything else and the next keystroke would look up a directory that is not
	// there ([folderPick.writeBack]).
	if want := tildePath(filepath.Join(root, "sibling"), a.tilde) + "/"; a.folder.filter.String() != want {
		t.Fatalf("the box says %q, want %q — a path nobody had to type", a.folder.filter.String(), want)
	}
	if a.resolvePath(strings.TrimSuffix(a.folder.filter.String(), "/")) != filepath.Join(root, "sibling") {
		t.Fatalf("what the box holds does not resolve back: %q", a.folder.filter.String())
	}

	// And a click on a list row does the same thing.
	settleFolder(t, a, a.openFolderPick(""))
	y := markedRowY(a, chromeOverlay, 0)
	if y < 0 {
		t.Fatal("the list drew no rows")
	}
	first, _ := a.folder.here()
	drive(t, a, tea.MouseClickMsg{X: 4, Y: y, Button: tea.MouseLeft})
	if !a.folder.browsing || a.folder.cols.dir != first {
		t.Fatalf("a click on the first row left the browser on %q, want %s", a.folder.cols.dir, first)
	}
}

// A CLICK IN THE CHILDREN COLUMN WALKS IN AND LANDS ON THE NAME PRESSED, and a
// click in the parent column walks out. Those two are what make the pointer a
// way through a tree rather than a way of moving a cursor.
func TestTheColumnsWalkUnderThePointer(t *testing.T) {
	a, _, root := browseLab(t)
	openBrowse(t, a, filepath.Join(root, "here"))
	drive(t, a, key("down")) // onto `deep`, whose children are `one` and `two`

	// THE GEOMETRY IS READ AFTER A PAINT AND NEVER BEFORE ONE: it is what the
	// last layout put on the screen, which is exactly what the pointer resolves
	// against.
	markedRowY(a, chromeOverlay, 0)
	body := markedRowY(a, chromeOverlay, a.folder.geom.head+1)
	if body < 0 {
		t.Fatal("the sheet drew no second body row")
	}
	drive(t, a, tea.MouseClickMsg{X: a.folder.geom.kids.from, Y: body, Button: tea.MouseLeft})
	if a.folder.cols.dir != filepath.Join(root, "here", "deep") {
		t.Fatalf("a press in the children column landed on %s", a.folder.cols.dir)
	}
	if got, _ := a.folder.here(); got != filepath.Join(root, "here", "deep", "two") {
		t.Fatalf("the cursor is on %q, want the name that was pressed", got)
	}

	// And back out through the parent column.
	markedRowY(a, chromeOverlay, 0)
	up := markedRowY(a, chromeOverlay, a.folder.geom.head)
	drive(t, a, tea.MouseClickMsg{X: a.folder.geom.up.from, Y: up, Button: tea.MouseLeft})
	if a.folder.cols.dir != filepath.Join(root, "here") {
		t.Fatalf("a press in the parent column landed on %s", a.folder.cols.dir)
	}
}

// WHAT LIGHTS IS WHAT A PRESS ACTS ON. The three columns do three different
// things to a press, so which of them the pointer is over is a question about x
// — a band across the row would offer to do one of them wherever the pointer
// happened to be.
func TestThePointerLightsTheColumnItIsActuallyOver(t *testing.T) {
	a, _, root := browseLab(t)
	openBrowse(t, a, filepath.Join(root, "here"))
	markedRowY(a, chromeOverlay, 0)
	geom, row := a.folder.geom, a.folder.geom.head+1

	for _, probe := range []struct {
		where string
		x     int
		want  string
	}{
		{"the parent column", geom.up.from, folderColUp},
		{"the middle column", geom.here.from + 2, folderColHere},
		{"the children column", geom.kids.from, folderColKids},
	} {
		if got, ok := a.folderHoverColumn(probe.x, row); !ok || got != probe.want {
			t.Errorf("%s at x=%d answered %q", probe.where, probe.x, got)
		}
	}
	if got, _ := a.folderHoverColumn(0, 0); got != folderColCrumb {
		t.Errorf("the breadcrumb row answered %q", got)
	}
	if got, _ := a.folderHoverColumn(0, geom.action); got != folderColRow {
		t.Errorf("the action row is a whole-row target and answered %q", got)
	}

	// And only the middle column's row takes the band.
	// The band is INK and not text, so the rows are compared unstripped: plain()
	// would throw away the only thing that changed.
	cold := a.folder.rows(a.width, a.overlayHeight(), a.pal, -1, "")[row]
	lit := func(col string) bool {
		return a.folder.rows(a.width, a.overlayHeight(), a.pal, row, col)[row] != cold
	}
	if !lit(folderColHere) {
		t.Fatal("the pointer over the middle column lights nothing")
	}
	for _, col := range []string{folderColUp, folderColKids} {
		if lit(col) {
			t.Fatalf("the pointer over the %s column lit a row it does not act on", col)
		}
	}
}

// THE WHEEL WALKS THE CURSOR over the sheet's own rows, and belongs to whatever
// is under it anywhere else.
func TestTheWheelWalksTheBrowserOverItsOwnRows(t *testing.T) {
	a, _, root := browseLab(t)
	openBrowse(t, a, filepath.Join(root, "here"))
	before, _ := a.folder.here()

	markedRowY(a, chromeOverlay, 0)
	y := markedRowY(a, chromeOverlay, a.folder.geom.head)
	drive(t, a, tea.MouseWheelMsg{X: 4, Y: y, Button: tea.MouseWheelDown})
	after, _ := a.folder.here()
	if after == before {
		t.Fatalf("the wheel left the cursor on %s", before)
	}
	// Off the sheet it takes nothing: the transcript is still a transcript.
	if _, took := a.folderWheel(0, 3); took {
		t.Fatal("the browser took a wheel turned over the top of the frame")
	}
}

// ── the separate add action ─────────────────────────────────────────────────

// NAVIGATING AND CHOOSING ARE TWO ACTS WITH TWO GESTURES. The action row says
// what it would add, and pressing it is what adds it.
func TestTheActionRowAddsTheFolderTheCursorIsOn(t *testing.T) {
	a, agent, root := browseLab(t)
	openBrowse(t, a, filepath.Join(root, "here"))
	drive(t, a, key("down")) // onto `deep`

	rows := a.folder.rows(a.width, a.overlayHeight(), a.pal, -1, "")
	if a.folder.geom.action <= 0 {
		t.Fatalf("the sheet drew no action row (%d)", a.folder.geom.action)
	}
	if got := plain(rows[a.folder.geom.action]); !strings.Contains(got, folderAddWord) || !strings.Contains(got, "deep") {
		t.Fatalf("the action row says %q", got)
	}
	y := markedRowY(a, chromeOverlay, a.folder.geom.action)
	if y < 0 {
		t.Fatal("the action row is not on the frame")
	}
	drive(t, a, tea.MouseClickMsg{X: 4, Y: y, Button: tea.MouseLeft})

	want := filepath.Join(root, "here", "deep")
	if a.placeChosen != want {
		t.Fatalf("the action row chose %q, want %s", a.placeChosen, want)
	}
	if len(agent.places) != 1 || agent.places[0].Path != want {
		t.Fatalf("the conversation was not told: %+v", agent.places)
	}
	if a.folder.open {
		t.Fatal("adding a folder left the sheet open")
	}
}

// ── what a column with no rows is actually saying ───────────────────────────

// A FOLDER NOBODY MAY READ IS NOT A FOLDER WITH NOTHING IN IT, and the browser
// says which of the two it is looking at.
func TestAnUnreadableFolderSaysSoRatherThanLookingEmpty(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads every directory, so there is no refusal to observe")
	}
	a, _, root := browseLab(t)
	closed := filepath.Join(root, "closed")
	if err := os.Mkdir(closed, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(closed, 0o755) })

	openBrowse(t, a, closed)
	if a.folder.cols.here.err == nil {
		t.Fatal("the refusal was flattened into an empty folder")
	}
	drawn := plain(strings.Join(a.folder.rows(a.width, a.overlayHeight(), a.pal, -1, ""), "\n"))
	if !strings.Contains(drawn, folderClosedWord) {
		t.Fatalf("the sheet says nothing about the refusal:\n%s", drawn)
	}
	if strings.Contains(drawn, folderLeafWord) {
		t.Fatalf("a folder nobody may read was called empty:\n%s", drawn)
	}
	// An empty folder IS the other word, on the same sheet.
	empty := filepath.Join(root, "sibling")
	openBrowse(t, a, empty)
	drawn = plain(strings.Join(a.folder.rows(a.width, a.overlayHeight(), a.pal, -1, ""), "\n"))
	if !strings.Contains(drawn, folderLeafWord) {
		t.Fatalf("an empty folder says nothing:\n%s", drawn)
	}
}

// EVERY WAY A READ CAN FAIL HAS ITS OWN SENTENCE, and a read that has not
// happened yet has none at all — the emptiness law, where a level still in
// flight draws nothing rather than a placeholder saying so.
func TestEachWayAReadFailsHasItsOwnWord(t *testing.T) {
	for _, row := range []struct {
		name string
		read folderRead
		want string
	}{
		{"not asked yet", folderRead{}, ""},
		{"read and empty", folderRead{done: true}, folderLeafWord},
		{"refused", folderRead{err: os.ErrPermission, done: true}, folderClosedWord},
		{"gone", folderRead{err: os.ErrNotExist, done: true}, folderMissingWord},
		{"a file", folderRead{err: syscall.ENOTDIR, done: true}, folderNotDirWord},
		{"anything else", folderRead{err: errors.New("io"), done: true}, folderUnreadableWord},
	} {
		if got := folderStateWord(row.read); got != row.want {
			t.Errorf("%s says %q, want %q", row.name, got, row.want)
		}
	}
	// A read that names a real error is never the empty-folder word, whatever
	// else it is.
	for _, err := range []error{os.ErrPermission, os.ErrNotExist, syscall.ENOTDIR, errors.New("io")} {
		if folderStateWord(folderRead{err: err, done: true}) == folderLeafWord {
			t.Fatalf("%v was reported as an empty folder", err)
		}
	}
}

// HIDDEN FOLDERS ARE REACHABLE, by the toggle and by typing a name that starts
// with a dot — which is what somebody reaching for `.config` already does.
func TestHiddenFoldersAreReachable(t *testing.T) {
	a, _, root := browseLab(t)
	here := filepath.Join(root, "here")
	openBrowse(t, a, here)
	if strings.Join(a.folder.cols.here.names, ",") == "" ||
		strings.Contains(strings.Join(a.folder.cols.here.names, ","), ".hidden") {
		t.Fatalf("the hidden folder was on offer without being asked for: %v", a.folder.cols.here.names)
	}

	drive(t, a, key(folderHiddenKey))
	settleFolder(t, a, a.askFolderKids())
	names := strings.Join(a.folder.cols.here.names, ",")
	for _, want := range []string{".hidden", "node_modules"} {
		if !strings.Contains(names, want) {
			t.Fatalf("%s is still hidden after the toggle: %v", want, a.folder.cols.here.names)
		}
	}
	drive(t, a, key(folderHiddenKey))
	settleFolder(t, a, a.askFolderKids())
	if strings.Contains(strings.Join(a.folder.cols.here.names, ","), ".hidden") {
		t.Fatalf("the toggle does not toggle back: %v", a.folder.cols.here.names)
	}

	// And a half-typed dot name reveals them without the toggle.
	typeFolder(t, a, ".hid")
	settleFolder(t, a, a.askFolderKids())
	if !a.folder.hidden {
		t.Fatal("typing a name that starts with a dot did not reveal the hidden folders")
	}
	if got, _ := a.folder.here(); got != filepath.Join(here, ".hidden") {
		t.Fatalf("the cursor did not follow `.hid` onto .hidden: %q", got)
	}
}

// A LEVEL THAT ARRIVES AFTER THE PICKER THAT ASKED FOR IT IS DROPPED. A slow
// mount answers whenever it answers, and filing that answer into the browser
// somebody opened afterwards would put one folder's names in another's column.
func TestAStaleDirectoryAnswerIsDropped(t *testing.T) {
	a, _, root := browseLab(t)
	openBrowse(t, a, filepath.Join(root, "here"))
	was := a.folder.gen

	a.folder.close()
	settleFolder(t, a, a.openFolderPick(filepath.Join(root, "sibling")+"/"))
	fake := folderRead{names: []string{"not-really-here"}, done: true}
	a.tookFolderKids(folderKidsMsg{path: filepath.Join(root, "sibling"), read: fake, gen: was})
	if strings.Contains(strings.Join(a.folder.cols.here.names, ","), "not-really-here") {
		t.Fatalf("a stale answer landed in the columns: %v", a.folder.cols.here.names)
	}
	// The same answer stamped with THIS opening is taken.
	a.tookFolderKids(folderKidsMsg{path: filepath.Join(root, "sibling"), read: fake, gen: a.folder.gen})
	if !strings.Contains(strings.Join(a.folder.cols.here.names, ","), "not-really-here") {
		t.Fatalf("a live answer was dropped: %v", a.folder.cols.here.names)
	}
}

// WALKING OUT LANDS ON THE FOLDER YOU CAME FROM even when the level above has
// never been read — the cursor is a NAME held until the readdir answers, not an
// index applied to a column that is not there yet.
func TestWalkingOutWaitsForTheLevelAboveToArrive(t *testing.T) {
	a, _, root := browseLab(t)
	openBrowse(t, a, filepath.Join(root, "here", "deep"))
	// Nothing above `deep` has been read on purpose: forget every level, then
	// walk out and let only the parent's own readdir land.
	a.folder.forget()
	drive(t, a, key("left"))
	if a.folder.cols.dir != filepath.Join(root, "here") {
		t.Fatalf("← landed on %s", a.folder.cols.dir)
	}
	settleFolder(t, a, a.askFolderKids())
	if got, _ := a.folder.here(); got != filepath.Join(root, "here", "deep") {
		t.Fatalf("the cursor came out onto %q, not the folder it left", got)
	}
	if a.folder.cols.upAt < 0 {
		t.Fatal("the parent column does not know which row we are standing in")
	}
}

// THE PICKS ARRIVING FROM DISK DO NOT BLANK THE COLUMNS. They re-rank a list;
// they say nothing about any directory, and a sheet that flushed three columns
// a person is reading to redraw them a frame later would be the background
// reaching into their hands.
func TestThePicksArrivingDoNotFlushTheColumns(t *testing.T) {
	a, _, root := browseLab(t)
	openBrowse(t, a, filepath.Join(root, "here"))
	drive(t, a, key("down"))
	was, _ := a.folder.here()

	a.tookFolderStore(folderStoreMsg{store: folderStore{
		Picks: map[string]folderPickCount{filepath.Join(root, "sibling"): {N: 3, At: a.now()}},
	}})
	if !a.folder.browsing {
		t.Fatal("the store closed the columns")
	}
	if len(a.folder.cols.here.names) == 0 {
		t.Fatal("the columns were blanked by a file of pick counts")
	}
	if got, _ := a.folder.here(); got != was {
		t.Fatalf("the cursor moved to %q, want %s", got, was)
	}
}

// ── the law: the surface does not claim what did not happen ─────────────────

// A CONVERSATION WITH NO WAY TO REMEMBER A FOLDER IS NOT OFFERED ONE. This is
// the defect this wave was called for: `--host` was the only refusal, and a
// LOCAL engine reached down the same wire has an empty far hostname — so the
// picker opened, ranked a hundred directories and printed `folder · …` at a
// session that never heard of it.
func TestAConversationThatCannotHoldAFolderIsNotOfferedOne(t *testing.T) {
	a, _, root := browseLab(t)
	a.agent = doorlessAgent{a.agent}
	a.entries = nil

	if cmd := a.openFolderPick(""); cmd != nil {
		t.Fatal("a picker with nothing behind it started work")
	}
	if a.folder.open {
		t.Fatal("the picker opened over a conversation that cannot hold a folder")
	}
	if got := plain(lastNote(t, a)); got != folderNoDoorWord {
		t.Fatalf("the refusal said %q", got)
	}

	// And the seam every other road ends in refuses in the same words rather
	// than printing the line that says it worked.
	a.entries = nil
	a.referPlace(chosenPlace{Path: filepath.Join(root, "here"), Door: placeFromAttach})
	if a.placeChosen != "" {
		t.Fatalf("a folder was chosen with nothing behind it: %s", a.placeChosen)
	}
	if got := plain(lastNote(t, a)); got != folderNoDoorWord {
		t.Fatalf("the seam said %q", got)
	}
}

// AND THE ENGINE'S OWN WORD OUTRANKS THE METHODS BEING THERE. The ordinary
// local launch goes through the wire, so the client carries ReferPlace, Places
// and RemovePlace whatever is on the far end of the pipe — a type assertion is
// true for every connection there has ever been, and only the far side can say
// whether the methods do anything.
func TestAnEngineThatSaysItCannotKeepFoldersIsBelieved(t *testing.T) {
	a, agent, _ := browseLab(t)
	a.entries = nil
	a.agent = statedAgent{Agent: a.agent, door: agent}

	if a.canReferPlace() {
		t.Fatal("the surface believed the methods over the engine's own answer")
	}
	if cmd := a.openFolderPick(""); cmd != nil || a.folder.open {
		t.Fatal("the picker opened over an engine that says it cannot keep a folder")
	}
	if got := plain(lastNote(t, a)); got != folderNoDoorWord {
		t.Fatalf("the refusal said %q", got)
	}
	// And with the same methods and a yes, everything is offered again.
	a.agent = statedAgent{Agent: a.agent, door: agent, yes: true}
	if !a.canReferPlace() {
		t.Fatal("an engine that says yes was refused")
	}
}

// statedAgent carries the whole folder door AND the engine's own statement about
// whether it does anything — internal/remote's `KeepsFolders`, off
// `Welcome.Folders`.
type statedAgent struct {
	Agent
	door *fakeAgent
	yes  bool
}

func (s statedAgent) KeepsFolders() bool            { return s.yes }
func (s statedAgent) Places() []session.PlaceRef    { return s.door.Places() }
func (s statedAgent) RemovePlace(path string) error { return s.door.RemovePlace(path) }
func (s statedAgent) ReferPlace(path string, arrival session.PlaceArrival) (session.PlaceRef, error) {
	return s.door.ReferPlace(path, arrival)
}

// THE INDICATOR IS A CLAIM ABOUT WHAT SOMEBODY DID, so it draws the folders they
// attached and not the ones the ground ladder worked out and wrote down.
func TestTheTrayDrawsWhatWasAttachedAndNotWhatWasWorkedOut(t *testing.T) {
	a, agent, root := browseLab(t)
	agent.places = []session.PlaceRef{
		{Path: filepath.Join(root, "here"), Arrival: session.PlaceSaid},
		{Path: filepath.Join(root, "sibling"), Arrival: session.PlaceKept},
	}
	cells := a.placeTrayCells()
	if len(cells) != 1 || !strings.Contains(cells[0], "here") {
		t.Fatalf("the tray drew %v — only what was attached belongs there", cells)
	}
	// The picker's own rows are a RANKING and may have both: they are places to
	// choose from, not a claim that anybody chose them.
	if len(a.referredPlaces()) != 2 {
		t.Fatalf("the picker's first layer lost a place: %v", a.referredPlaces())
	}
}

// THE CELL NAMES WHAT THE PERSON POINTED AT. A folder inside a repository is
// held as the repository, and a row that read `agentfield` for a person who
// chose `agentfield/internal/session` would be showing them a scope they did not
// pick.
func TestTheTrayNamesTheDirectoryThatWasActuallyChosen(t *testing.T) {
	a, agent, root := browseLab(t)
	deep := filepath.Join(root, "here", "deep")
	agent.places = []session.PlaceRef{{Path: root, Arrival: session.PlaceSaid}}

	was := placeScope
	t.Cleanup(func() { placeScope = was })
	placeScope = func(ref session.PlaceRef) string {
		if ref.Path == root {
			return deep
		}
		return ""
	}
	if cells := a.placeTrayCells(); len(cells) != 1 || !strings.Contains(cells[0], "deep") {
		t.Fatalf("the tray drew %v, want the folder that was chosen", cells)
	}
	// And the removal still works on the folder the conversation actually holds.
	cmd, ok := a.dropPlaceChip(0)
	if !ok {
		t.Fatal("the cell offered no way off")
	}
	settleFolder(t, a, cmd)
	if len(agent.places) != 0 {
		t.Fatalf("the removal did not act on the held path: %+v", agent.places)
	}
}

// A REFUSAL FROM THE CONVERSATION IS THE LINE, and no pick is written down for
// a place it did not gain.
func TestARefusedFolderIsNotReportedAsAdded(t *testing.T) {
	a, agent, root := browseLab(t)
	agent.placeErr = os.ErrPermission
	a.entries = nil

	a.referPlace(chosenPlace{Path: filepath.Join(root, "here"), Door: placeFromPicker})
	if a.placeChosen != "" {
		t.Fatalf("a refused folder was chosen: %s", a.placeChosen)
	}
	if len(a.folderStore.Picks) != 0 {
		t.Fatalf("a refused folder was written down: %+v", a.folderStore.Picks)
	}
	if got := plain(lastNote(t, a)); !strings.Contains(got, os.ErrPermission.Error()) {
		t.Fatalf("the refusal said %q", got)
	}
}

// A DIRECTORY INSIDE A REPOSITORY COMES BACK AS THE REPOSITORY, and the sentence
// says so rather than quietly printing a scope nobody picked.
func TestTheRepositorySnapIsSaidOutLoud(t *testing.T) {
	a, agent, root := browseLab(t)
	agent.placeAs = root
	a.entries = nil

	a.referPlace(chosenPlace{Path: filepath.Join(root, "here", "deep"), Door: placeFromPicker})
	got := plain(lastNote(t, a))
	if !strings.Contains(got, folderInsideWord) {
		t.Fatalf("the snap was silent: %q", got)
	}
	if !strings.Contains(got, "deep") {
		t.Fatalf("the folder that was actually chosen is not named: %q", got)
	}
	if a.placeChosen != root {
		t.Fatalf("the conversation gained %q, want %s", a.placeChosen, root)
	}
}

// ── the indicator ───────────────────────────────────────────────────────────

// THE FOLDERS A CONVERSATION IS ABOUT ARE VISIBLE, AND ONE CLICK TAKES ONE OFF.
func TestTheTrayShowsTheFoldersAndTakesOneOff(t *testing.T) {
	a, agent, root := browseLab(t)
	a.entries = nil
	if got := a.placeTrayCells(); len(got) != 0 {
		t.Fatalf("a conversation about nowhere drew %v", got)
	}

	here := filepath.Join(root, "here")
	agent.places = []session.PlaceRef{{Path: here, Arrival: session.PlaceSaid}}
	cells := a.placeTrayCells()
	if len(cells) != 1 || !strings.Contains(cells[0], "here") {
		t.Fatalf("the tray drew %v", cells)
	}
	if !strings.Contains(cells[0], glyphChipDrop) {
		t.Fatalf("the cell offers no way off: %q", cells[0])
	}
	if !strings.Contains(plain(a.chipStrip(a.width)), "here") {
		t.Fatalf("the tray row does not carry the folder: %q", plain(a.chipStrip(a.width)))
	}

	// The press goes off the loop and the cell comes off when the answer lands.
	_, height := a.size()
	rows, _, _, _ := a.chrome(a.width)
	at := len(rows) - 1 - a.overlayHeight() - a.inputHeight()
	drive(t, a, tea.MouseClickMsg{X: len(inputPad), Y: height - len(rows) + at, Button: tea.MouseLeft})
	if len(agent.places) != 0 {
		t.Fatalf("the folder is still on the conversation: %+v", agent.places)
	}
	if got := plain(lastNote(t, a)); !strings.HasPrefix(got, placeDroppedWord) {
		t.Fatalf("taking a folder off said %q", got)
	}
	if len(a.placeTrayCells()) != 0 {
		t.Fatalf("the cell is still on the tray: %v", a.placeTrayCells())
	}
}

// WITH NOTHING BEHIND THE REMOVAL THE GESTURE IS ABSENT, not broken: the cells
// still say what the conversation is about, because that much is true.
func TestWithoutARemovalDoorTheCellsAreStillDrawnAndOfferNothing(t *testing.T) {
	a, agent, root := browseLab(t)
	agent.places = []session.PlaceRef{{Path: filepath.Join(root, "here"), Arrival: session.PlaceSaid}}
	a.agent = halfDoorAgent{a.agent, agent}

	cells := a.placeTrayCells()
	if len(cells) != 1 || !strings.Contains(cells[0], "here") {
		t.Fatalf("the tray drew %v", cells)
	}
	if strings.Contains(cells[0], glyphChipDrop) {
		t.Fatalf("a way off is offered with nothing behind it: %q", cells[0])
	}
	if _, ok := a.dropPlaceChip(0); ok {
		t.Fatal("the removal answered with nothing behind it")
	}
}

// halfDoorAgent has the accrual door and not the removal — the shape the wire
// has while the two land on their own timetables.
type halfDoorAgent struct {
	Agent
	door *fakeAgent
}

func (h halfDoorAgent) Places() []session.PlaceRef { return h.door.Places() }
func (h halfDoorAgent) ReferPlace(path string, arrival session.PlaceArrival) (session.PlaceRef, error) {
	return h.door.ReferPlace(path, arrival)
}

// THE TRAY COUNTS WHAT IT HAS NO ROOM TO NAME rather than drawing a wall above
// the box.
func TestTheTrayCountsTheFoldersItCannotName(t *testing.T) {
	a, agent, root := browseLab(t)
	for _, name := range []string{"one", "two", "three", "four", "five"} {
		agent.places = append(agent.places, session.PlaceRef{Path: filepath.Join(root, name), Arrival: session.PlaceSaid})
	}
	cells := a.placeTrayCells()
	if len(cells) != placeTrayCap+1 {
		t.Fatalf("the tray drew %d cells: %v", len(cells), cells)
	}
	if last := cells[len(cells)-1]; !strings.Contains(last, "+2"+placeTrayMoreWord) {
		t.Fatalf("the counting cell says %q", last)
	}
	// The counting cell answers to nothing — pressing a sentence does nothing on
	// this surface — and neither does anything past the bound.
	if _, ok := a.dropPlaceChip(placeTrayCap); ok {
		t.Fatal("the counting cell took a press")
	}
}

// ── the frame ───────────────────────────────────────────────────────────────

// THE SHEET SURVIVES A SMALL TERMINAL: the columns give way in order, the names
// are never cut to make room for a column, and nothing runs past the frame.
func TestTheBrowserFitsANarrowFrame(t *testing.T) {
	a, _, root := browseLab(t)
	for _, width := range []int{40, 60, 84, 120} {
		a.width, a.height = width, 20
		a.touch()
		openBrowse(t, a, filepath.Join(root, "here"))
		up, here, kids := folderColumns(width)
		if here < 1 {
			t.Fatalf("at %d cells the middle column is %d wide", width, here)
		}
		if width < folderKidsAt && kids > 0 {
			t.Fatalf("at %d cells the children column was still drawn", width)
		}
		if width < folderWideAt && up > 0 {
			t.Fatalf("at %d cells the parent column was still drawn", width)
		}
		rows := a.folder.rows(width, a.overlayHeight(), a.pal, -1, "")
		if len(rows) == 0 {
			t.Fatalf("at %d cells the sheet drew nothing", width)
		}
		for _, line := range rows {
			if ansi.StringWidth(line) > width {
				t.Fatalf("at %d cells a row runs past the frame: %q", width, plain(line))
			}
		}
	}
}

// A FRAME WITH ONE ROW TO GIVE SPENDS IT ON THE DIRECTORIES. A sheet showing
// only the way to add something, with no way to see what would be added, is not
// a browser.
func TestTheSmallestSheetIsStillDirectories(t *testing.T) {
	a, _, root := browseLab(t)
	openBrowse(t, a, filepath.Join(root, "here"))
	rows := a.folder.rows(a.width, 1, a.pal, -1, "")
	if len(rows) != 1 {
		t.Fatalf("one row of frame drew %d rows", len(rows))
	}
	if a.folder.geom.action >= 0 {
		t.Fatalf("the action row took the only row there was")
	}
	if got := plain(rows[0]); strings.Contains(got, folderAddWord) {
		t.Fatalf("the only row is the action row: %q", got)
	}
}
