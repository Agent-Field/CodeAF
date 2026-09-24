package tui3

// foldercontext_test.go is THE CONTEXT BROWSER as the owner asked for it: files
// and folders in one deliberate sheet, a real preview beside them, a mark tray
// and a confirm that does what the row says — and, under all of it, the law
// that makes the sheet safe to explore:
//
//	BROWSING, FOCUSING AND PREVIEWING ATTACH NOTHING.
//
// Every test here either proves that law or proves one deliberate act crossing
// it on purpose.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// mixedLab is a surface over a tree with BOTH kinds of thing in it: two
// subdirectories, a Go file, a JSON file, a picture, a file nobody may read and
// a file that is not text at all.
func mixedLab(t *testing.T) (*app, *fakeAgent, string) {
	t.Helper()
	root := t.TempDir()
	for _, dir := range []string{"here/inner", "here/other"} {
		if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(dir)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(root, "here", name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("main.go", "package main\n\nfunc main() {\n\tprintln(\"hello\")\n}\n")
	write("shape.json", `{"one":1,"two":[2,3],"three":{"deep":true}}`)
	write("notes.md", "# Notes\n\nsomething written down\n")
	write("server.log", "started\nlistening on 8080\nstopped\n")
	write("blob.bin", "\x00\x01\x02\x03binary\x00")
	// A tiny valid PNG, so the picture road is a real decode and not a guess.
	write("shot.png", pngFixture)
	agent := &fakeAgent{model: "m"}
	a := newApp(t.Context(), Options{Agent: agent, Workspace: filepath.Join(root, "here"), ProfileDir: t.TempDir()})
	a.width, a.height = 140, 34
	a.pal = newPalette(tokens.TrueColor, false)
	a.entries = nil
	a.folderStoreRead = true
	a.file = filepath.Join(root, "session.jsonl")
	a.touch()
	return a, agent, root
}

// pngFixture is a 2×2 PNG. It is bytes rather than a generated image so the
// decode path under test is the real one and the fixture cannot drift.
const pngFixture = "\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x02\x00\x00\x00\x02\x08\x02\x00\x00\x00\xfd\xd4\x9as" +
	"\x00\x00\x00\x16IDATx\x9cbd`\xf8\xcf\xc0\xc0\xc0\xc0\xc0\xc0\x00\x00\x00\x00\xff\xff\x03\x00\x0e\xfd\x03\xfd" +
	"\x8e\xe9\x93\xbf\x00\x00\x00\x00IEND\xaeB`\x82"

// onRow walks the cursor to a named row of the middle column and lets the
// preview for it arrive.
func onFolderRow(t *testing.T, a *app, name string) {
	t.Helper()
	at := a.folder.cols.here.rowAt(name)
	if at < 0 {
		t.Fatalf("%q is not a row of %s: %v / %v", name, a.folder.cols.dir,
			a.folder.cols.here.names, a.folder.cols.here.files)
	}
	for a.folder.cols.cursor < at {
		drive(t, a, key("down"))
	}
	for a.folder.cols.cursor > at {
		drive(t, a, key("up"))
	}
	settleFolder(t, a, a.folderWork())
}

// ── the two kinds of row ────────────────────────────────────────────────────

// ONE SHEET SHOWS BOTH KINDS, directories first and then the files with their
// sizes — the reference screenshot's order and its alignment.
func TestTheBrowserListsFoldersThenFilesWithTheirSizes(t *testing.T) {
	a, _, root := mixedLab(t)
	openBrowse(t, a, filepath.Join(root, "here"))

	here := a.folder.cols.here
	if want := "inner,other"; strings.Join(here.names, ",") != want {
		t.Fatalf("the directories are %v, want %s", here.names, want)
	}
	var files []string
	for _, file := range here.files {
		if !file.sized {
			t.Fatalf("%s came back with no size", file.name)
		}
		files = append(files, file.name)
	}
	if want := "blob.bin,main.go,notes.md,server.log,shape.json,shot.png"; strings.Join(files, ",") != want {
		t.Fatalf("the files are %v, want %s", files, want)
	}
	// The directories lead, so the row arithmetic can be an index comparison.
	if !here.isDir(0) || !here.isDir(1) || here.isDir(2) {
		t.Fatalf("the directories do not lead: rows=%d dirs=%d", here.rows(), len(here.names))
	}
	drawn := plain(strings.Join(chooserRows(t, a, -1, ""), "\n"))
	// A directory wears its slash; a file wears its size.
	if !strings.Contains(drawn, "inner/") {
		t.Fatalf("a directory drew without its slash:\n%s", drawn)
	}
	if !strings.Contains(drawn, "main.go") {
		t.Fatalf("the files are not on the sheet:\n%s", drawn)
	}
	if !strings.Contains(drawn, " B") && !strings.Contains(drawn, "KB") {
		t.Fatalf("no size was drawn beside any file:\n%s", drawn)
	}
}

// ── the law ─────────────────────────────────────────────────────────────────

// MOVING ONTO A FILE SHOWS IT AND DOES NOT ATTACH IT. Nothing on the tray,
// nothing on the conversation, and no sentence in the transcript — the whole
// point of a browser you can explore.
func TestWalkingOntoAFileShowsItAndAttachesNothing(t *testing.T) {
	a, agent, root := mixedLab(t)
	openBrowse(t, a, filepath.Join(root, "here"))
	before := len(a.entries)

	for _, name := range []string{"main.go", "shape.json", "notes.md", "blob.bin", "shot.png"} {
		onFolderRow(t, a, name)
		if a.folder.preview.empty() {
			t.Fatalf("the pane says nothing at all about %s", name)
		}
		if len(a.chips) != 0 {
			t.Fatalf("looking at %s put %d things on the tray", name, len(a.chips))
		}
		if len(agent.places) != 0 {
			t.Fatalf("looking at %s gave the conversation a folder", name)
		}
	}
	// The folders too.
	for _, name := range []string{"inner", "other"} {
		onFolderRow(t, a, name)
		if len(agent.places) != 0 {
			t.Fatalf("looking at %s/ gave the conversation a folder", name)
		}
	}
	if len(a.entries) != before {
		t.Fatalf("browsing wrote %d lines into the conversation", len(a.entries)-before)
	}
}

// EACH KIND OF THING IS PREVIEWED AS THAT KIND OF THING, through the helpers
// contextpreview.go owns — and the source comes back with syntax colour rather
// than as flat text.
func TestThePreviewKnowsWhatKindOfThingItIsShowing(t *testing.T) {
	a, _, root := mixedLab(t)
	openBrowse(t, a, filepath.Join(root, "here"))

	for _, probe := range []struct {
		name string
		want previewKind
	}{
		{"inner", previewFolder},
		{"main.go", previewSource},
		{"shape.json", previewSource},
		// Markdown is a language chroma claims, so it is highlighted — headings
		// and emphasis and nothing else — and comes back as source. A log file
		// is what chroma has no lexer for, and comes back as flat prose: the
		// rule [codeLang] keeps, so a fallback lexer never paints a log as
		// though it were code (reports/preview.md).
		{"notes.md", previewSource},
		{"server.log", previewProse},
		{"shot.png", previewPicture},
		{"blob.bin", previewOpaque},
	} {
		onFolderRow(t, a, probe.name)
		if got := a.folder.preview.Kind; got != probe.want {
			t.Errorf("%s previewed as kind %v, want %v (note %q)", probe.name, got, probe.want, a.folder.preview.Note)
		}
	}

	// SOURCE IS PAINTED AND NUMBERED. The pane's rows carry SGR that the plain
	// text does not, and the gutter is there at this width.
	onFolderRow(t, a, "main.go")
	rows := a.folder.paneRows(a.pal, a.styler(), a.width, 12, -1)
	painted := strings.Join(rows, "\n")
	if painted == plain(painted) {
		t.Fatalf("a Go file drew with no syntax colour at all:\n%s", painted)
	}
	if !strings.Contains(plain(painted), "package main") {
		t.Fatalf("the pane does not hold the file's first line:\n%s", plain(painted))
	}
	if !strings.Contains(plain(painted), " 1 ") {
		t.Fatalf("the line-number gutter is missing at %d cells:\n%s", a.width, plain(painted))
	}
	if a.folder.preview.Lang == "" {
		t.Fatal("the language was not detected for a .go file")
	}
}

// NO CONTROL SEQUENCE FROM SOMEBODY ELSE'S FILE REACHES THE FRAME. A file is
// data being shown, never instructions being followed.
func TestAFilesOwnEscapesCannotRepaintTheSheet(t *testing.T) {
	a, _, root := mixedLab(t)
	nasty := filepath.Join(root, "here", "nasty.txt")
	if err := os.WriteFile(nasty, []byte("\x1b[31mred\x1b[0m\x1b]0;title\x07\x1b[2J done\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	openBrowse(t, a, filepath.Join(root, "here"))
	onFolderRow(t, a, "nasty.txt")

	for _, row := range a.folder.paneRows(a.pal, a.styler(), a.width, 8, -1) {
		if strings.Contains(plain(row), "\x1b") || strings.Contains(plain(row), "\x07") {
			t.Fatalf("a raw control byte reached the frame: %q", row)
		}
		if strings.Contains(row, "\x1b[2J") || strings.Contains(row, "\x1b]0;") {
			t.Fatalf("the file's own escape reached the frame: %q", row)
		}
	}
	if !strings.Contains(plain(strings.Join(a.folder.paneRows(a.pal, a.styler(), a.width, 8, -1), "\n")), "red") {
		t.Fatal("the readable text was thrown away with the escapes")
	}
}

// ── the marks, and the sentence they make ───────────────────────────────────

// THE ACTION ROW SAYS WHAT ENTER WILL DO, with the verb that belongs to the kind
// of thing under the cursor. Adding a folder and attaching a file are two
// different acts on two different objects.
func TestTheActionRowSpellsTheVerbForTheKindOfThing(t *testing.T) {
	a, _, root := mixedLab(t)
	openBrowse(t, a, filepath.Join(root, "here"))

	for _, probe := range []struct{ name, want string }{
		{"inner", folderAddWord},
		{"main.go", folderFileWord},
		{"shot.png", folderPictureWord},
	} {
		onFolderRow(t, a, probe.name)
		row := plain(a.folder.actionRow(a.width, a.pal, -1))
		if !strings.Contains(row, probe.want) {
			t.Errorf("on %s the action row reads %q, want %q in it", probe.name, row, probe.want)
		}
	}
}

// alt+m CHOOSES, THE TRAY COUNTS, AND THE ROW NAMES BOTH VERBS. A mixed
// selection has to say which of the two things is about to happen to each half.
func TestChoosingSeveralThingsCountsThemAndNamesBothVerbs(t *testing.T) {
	a, _, root := mixedLab(t)
	openBrowse(t, a, filepath.Join(root, "here"))

	onFolderRow(t, a, "inner")
	drive(t, a, key(folderMarkKey))
	onFolderRow(t, a, "main.go")
	drive(t, a, key(folderMarkKey))
	if len(a.folder.marks) != 2 {
		t.Fatalf("two presses of %s left %d marks", folderMarkKey, len(a.folder.marks))
	}
	tray := plain(a.folder.trayRow(a.width, a.pal, -1))
	if !strings.Contains(tray, "2"+folderMarkedWord) {
		t.Fatalf("the tray says %q, want a count", tray)
	}
	for _, want := range []string{"inner/", "main.go"} {
		if !strings.Contains(tray, want) {
			t.Fatalf("the tray does not name %s: %q", want, tray)
		}
	}
	row := plain(a.folder.actionRow(a.width, a.pal, -1))
	for _, want := range []string{"add 1 folder", "attach 1 file"} {
		if !strings.Contains(row, want) {
			t.Fatalf("the action row reads %q, want %q in it", row, want)
		}
	}
	// AND THE MARKED ROW LOOKS MARKED, apart from the cursor: focus and choice
	// are two facts and a person has to be able to see both.
	drawn := chooserRows(t, a, -1, "")
	if !strings.Contains(plain(strings.Join(drawn, "\n")), folderMarkGlyph(a.pal)) {
		t.Fatalf("no chosen row wears the mark:\n%s", plain(strings.Join(drawn, "\n")))
	}
	// Pressing the key again on the same row takes the mark off.
	onFolderRow(t, a, "main.go")
	drive(t, a, key(folderMarkKey))
	if len(a.folder.marks) != 1 {
		t.Fatalf("pressing %s twice on one row left %d marks", folderMarkKey, len(a.folder.marks))
	}
}

// A PRESS ON A TRAY CELL TAKES THAT MARK OFF, which is the same gesture the
// message tray above the box gives a picture.
func TestAPressOnATrayCellUnchoosesThatThing(t *testing.T) {
	a, _, root := mixedLab(t)
	openBrowse(t, a, filepath.Join(root, "here"))
	onFolderRow(t, a, "inner")
	drive(t, a, key(folderMarkKey))
	onFolderRow(t, a, "main.go")
	drive(t, a, key(folderMarkKey))

	geom := chooserGeom(t, a)
	y := chooserRowY(t, a, geom.tray)
	if y < 0 || len(geom.trayCells) != 2 {
		t.Fatalf("the tray drew at row %d with %d cells", geom.tray, len(geom.trayCells))
	}
	x := geom.trayCells[0].from
	// It lights before it acts, which is the law that what brightens is what a
	// press takes off.
	if got, _ := a.folderHoverColumn(x, geom.tray); got != folderColTray || a.folder.trayHot != 0 {
		t.Fatalf("the pointer over the first cell answered %q / %d", got, a.folder.trayHot)
	}
	drive(t, a, tea.MouseClickMsg{X: chooserX(t, a, x), Y: y, Button: tea.MouseLeft})
	if len(a.folder.marks) != 1 || a.folder.marks[0].dir {
		t.Fatalf("a press on the first cell left %+v", a.folder.marks)
	}
}

// THE CAP IS A NUMBER AND IT IS SAID. "Too many" is not something a person can
// act on.
func TestTheChoiceCapNamesItsOwnNumber(t *testing.T) {
	f := &folderPick{open: true, browsing: true, trayHot: -1}
	f.cols.dir = "/tmp/lab"
	f.cols.here.done = true
	for i := 0; i < folderMarkCap+2; i++ {
		f.cols.here.files = append(f.cols.here.files, folderFile{name: "f" + itoa(i)})
	}
	for i := 0; i < folderMarkCap; i++ {
		f.cols.cursor = i
		if say := f.mark(); say != "" {
			t.Fatalf("mark %d refused with %q", i, say)
		}
	}
	f.cols.cursor = folderMarkCap
	if say := f.mark(); say != folderMarkFullWord {
		t.Fatalf("the cap said %q, want %q", say, folderMarkFullWord)
	}
	if !strings.Contains(folderMarkFullWord, itoa(folderMarkCap)) {
		t.Fatalf("the refusal does not name the cap: %q", folderMarkFullWord)
	}
	if len(f.marks) != folderMarkCap {
		t.Fatalf("the cap let %d through", len(f.marks))
	}
}

// ── the confirm ─────────────────────────────────────────────────────────────

// A MIXED CONFIRM DOES BOTH THINGS AND EACH TO THE RIGHT PLACE: the folder to
// the conversation through the scoped reference door, the file onto the next
// message's tray. And ALL OF ITS WORK IS OFF THE LOOP — nothing has changed
// until the command's answer lands.
func TestOneConfirmAddsTheFolderAndAttachesTheFile(t *testing.T) {
	a, agent, root := mixedLab(t)
	a.input.setText("look at these")
	openBrowse(t, a, filepath.Join(root, "here"))

	onFolderRow(t, a, "inner")
	drive(t, a, key(folderMarkKey))
	onFolderRow(t, a, "main.go")
	drive(t, a, key(folderMarkKey))
	onFolderRow(t, a, "shot.png")
	drive(t, a, key(folderMarkKey))

	cmd := a.folderConfirm()
	if cmd == nil {
		t.Fatal("the confirm did no work at all")
	}
	// NOTHING YET: the registration is a round trip and the stats are a disk.
	if len(agent.places) != 0 || len(a.chips) != 0 {
		t.Fatalf("the confirm acted under the keystroke: %d places, %d chips", len(agent.places), len(a.chips))
	}
	if a.folder.open {
		t.Fatal("the sheet stayed open after a confirm that adds")
	}
	settleFolder(t, a, cmd)

	if len(agent.places) != 1 || agent.places[0].Path != filepath.Join(root, "here", "inner") {
		t.Fatalf("the conversation gained %+v", agent.places)
	}
	if len(a.chips) != 2 {
		t.Fatalf("the tray holds %d things, want the file and the picture: %+v", len(a.chips), a.chips)
	}
	// A PICTURE TRAVELS AS A PICTURE AND EVERYTHING ELSE AS A FILE, whichever row
	// it came off (attach.go's law).
	byName := map[string]bool{}
	for _, held := range a.chips {
		byName[held.name()] = held.file
	}
	if byName["main.go"] != true {
		t.Fatalf("main.go did not go on as a file: %+v", a.chips)
	}
	if byName["shot.png"] != false {
		t.Fatalf("shot.png did not go on as a picture: %+v", a.chips)
	}
	// AND THE DRAFT IS UNTOUCHED. The browser hangs over a sentence somebody is
	// still writing.
	if a.input.String() != "look at these" {
		t.Fatalf("the draft came back as %q", a.input.String())
	}
	// One line names what the message now carries, because four chips appearing
	// at once above the box while somebody was looking at the browser is easy to
	// miss.
	said := transcriptText(a)
	for _, want := range []string{folderAttachedWord, "main.go", "shot.png", folderChoseWord} {
		if !strings.Contains(said, want) {
			t.Fatalf("the conversation does not say %q:\n%s", want, said)
		}
	}
}

// ESC CHANGES NOTHING — not the draft, not the tray, not the conversation, and
// not the marks, which do not survive to the next opening either.
func TestEscapeFromTheBrowserKeepsTheDraftAndTheTray(t *testing.T) {
	a, agent, root := mixedLab(t)
	a.input.setText("half a sentence")
	a.chips = []chip{{path: filepath.Join(root, "here", "notes.md"), file: true}}
	openBrowse(t, a, filepath.Join(root, "here"))

	onFolderRow(t, a, "inner")
	drive(t, a, key(folderMarkKey))
	onFolderRow(t, a, "main.go")
	drive(t, a, key(folderMarkKey))
	drive(t, a, key("esc"))

	if a.folder.open {
		t.Fatal("esc left the sheet open")
	}
	if a.input.String() != "half a sentence" {
		t.Fatalf("esc changed the draft to %q", a.input.String())
	}
	if len(a.chips) != 1 || a.chips[0].name() != "notes.md" {
		t.Fatalf("esc changed the tray to %+v", a.chips)
	}
	if len(agent.places) != 0 {
		t.Fatalf("esc gave the conversation %+v", agent.places)
	}
	// And the marks are gone from the next opening.
	openBrowse(t, a, filepath.Join(root, "here"))
	if len(a.folder.marks) != 0 {
		t.Fatalf("the next opening still holds %+v", a.folder.marks)
	}
}

// A CONVERSATION THAT MOVED UNDER A CONFIRM STILL GETS ITS FILES AND NOT THE
// SENTENCE. The folders reached the conversation they were chosen in — the door
// was captured in the command — and a line claiming them somewhere else would be
// a claim about the wrong place.
func TestAConfirmThatLandsInAnotherConversationSaysNothingAboutTheFolder(t *testing.T) {
	a, _, root := mixedLab(t)
	openBrowse(t, a, filepath.Join(root, "here"))
	before := len(a.entries)

	a.tookFolderTaken(folderTakenMsg{
		file:  filepath.Join(root, "somebody-elses.jsonl"),
		chips: []chip{{path: filepath.Join(root, "here", "main.go"), file: true}},
		added: []folderAdded{{path: filepath.Join(root, "here", "inner"), chose: filepath.Join(root, "here", "inner")}},
		notes: []string{"this should not be said here"},
	})
	if len(a.chips) != 1 {
		t.Fatalf("the tray travels with the person and holds %+v", a.chips)
	}
	if len(a.entries) != before {
		t.Fatalf("a sentence was printed into a conversation that never gained the folder:\n%s", transcriptText(a))
	}
}

// A FOLDER THE CONVERSATION ALREADY HOLDS IS TAKEN OFF BY THE SAME ROW, and the
// sheet stays open because clearing several is a tidy-up.
func TestTheRowTakesAHeldFolderBackOffAndStaysOpen(t *testing.T) {
	a, agent, root := mixedLab(t)
	inner := filepath.Join(root, "here", "inner")
	agent.places = []session.PlaceRef{{Path: inner, Arrival: session.PlaceSaid}}
	openBrowse(t, a, inner)
	a.markFolderHeld()

	// Standing in a leaf, the row under the cursor IS the folder itself.
	if got, held := a.folder.holds(); !held || got != inner {
		t.Fatalf("holds answered %q / %v", got, held)
	}
	if !strings.Contains(plain(a.folder.actionRow(a.width, a.pal, -1)), folderDropWord) {
		t.Fatalf("the row does not offer to remove it: %q", plain(a.folder.actionRow(a.width, a.pal, -1)))
	}
	settleFolder(t, a, a.folderConfirm())
	if len(agent.places) != 0 {
		t.Fatalf("the folder is still on the conversation: %+v", agent.places)
	}
	if !a.folder.open {
		t.Fatal("removing closed the list somebody was tidying")
	}
}

// ── the pane ────────────────────────────────────────────────────────────────

// THE PREVIEW HIDES AND EXPANDS BY TWO KEYS, and the legend beside the box says
// so — every chord on this surface keeps a visible, self-teaching door.
func TestThePreviewHidesAndTakesTheWholeSheet(t *testing.T) {
	a, _, root := mixedLab(t)
	openBrowse(t, a, filepath.Join(root, "here"))
	onFolderRow(t, a, "main.go")

	wide := folderDivide(a.width, a.folder.pane)
	if wide.pane <= 0 || wide.here <= 0 {
		t.Fatalf("a %d-cell sheet drew %+v", a.width, wide)
	}
	drive(t, a, key("alt+w"))
	if off := folderDivide(a.width, a.folder.pane); off.pane != 0 {
		t.Fatalf("%s left the pane %d cells wide", folderPaneKey, off.pane)
	}
	drive(t, a, key("alt+w"))
	if back := folderDivide(a.width, a.folder.pane); back.pane != wide.pane {
		t.Fatalf("%s did not put the pane back: %d", folderPaneKey, back.pane)
	}
	drive(t, a, key(folderWideKey))
	full := folderDivide(a.width, a.folder.pane)
	if full.here != 0 || full.up != 0 || full.pane <= wide.pane {
		t.Fatalf("%s drew %+v", folderWideKey, full)
	}
	// With the preview alone the plain arrows are its own, and the legend says
	// which keys are live.
	if !strings.Contains(a.folder.folderHintAt(a.width), "scroll") {
		t.Fatalf("the wide legend reads %q", a.folder.folderHintAt(a.width))
	}
	drive(t, a, key(folderWideKey))
	// EVERY CHORD THE SHEET OWNS IS NAMED EXACTLY ONCE, across the two lines that
	// carry them: the box's own placeholder and the foot row under the columns.
	// Naming them all in both places was the wall of shortcuts the owner's review
	// asked us to stop drawing (folderpick.go's [folderBrowseHintFields]).
	said := a.folder.folderHintAt(a.width) + " · " + a.folder.controlLegend(a.width)
	for _, want := range []string{folderPaneKey, folderWideKey, folderMarkKey, "esc", "enter"} {
		if !strings.Contains(said, want) {
			t.Fatalf("nothing on the sheet names %s: %q", want, said)
		}
	}
	if strings.Contains(a.folder.folderHintAt(a.width), folderWideKey) &&
		strings.Contains(a.folder.controlLegend(a.width), folderWideKey) {
		t.Fatalf("%s is named twice: %q", folderWideKey, said)
	}
}

// THE PANE SCROLLS AND SLIDES, and its scroll is clamped so the last line of a
// file is reachable and no further.
func TestThePreviewScrollsAndSlidesAndStops(t *testing.T) {
	a, _, root := mixedLab(t)
	long := make([]string, 0, 120)
	for i := 0; i < 120; i++ {
		long = append(long, "line "+itoa(i)+" "+strings.Repeat("x", 200))
	}
	if err := os.WriteFile(filepath.Join(root, "here", "long.txt"), []byte(strings.Join(long, "\n")), 0o644); err != nil {
		t.Fatal(err)
	}
	openBrowse(t, a, filepath.Join(root, "here"))
	onFolderRow(t, a, "long.txt")

	// The rows the COLUMNS were given, as the last paint gave them — which is the
	// height the pane's own scroll is clamped against.
	_ = chooserRows(t, a, -1, "")
	rows := a.folder.geom.body
	if rows < 4 {
		t.Fatalf("the sheet only has %d body rows to scroll", rows)
	}
	first := plain(strings.Join(a.folder.paneRows(a.pal, a.styler(), a.width, rows, -1), "\n"))
	drive(t, a, key("shift+down"))
	drive(t, a, key("shift+down"))
	moved := plain(strings.Join(a.folder.paneRows(a.pal, a.styler(), a.width, rows, -1), "\n"))
	if moved == first {
		t.Fatal("shift+down did not scroll the preview")
	}
	// Near the end, three more presses cross the ceiling and prove the same
	// clamp without rendering four hundred complete surface turns.
	a.folder.paneTop = len(long) - 2
	for range 3 {
		drive(t, a, key("shift+down"))
	}
	a.folder.paneRows(a.pal, a.styler(), a.width, rows, -1)
	if a.folder.paneTop > len(long) {
		t.Fatalf("the scroll ran past the file: top=%d", a.folder.paneTop)
	}
	end := plain(strings.Join(a.folder.paneRows(a.pal, a.styler(), a.width, rows, -1), "\n"))
	if !strings.Contains(end, "line 119") {
		t.Fatalf("the last line is unreachable:\n%s", end)
	}
	// And sideways, in cells, with its own floor at zero.
	before := a.folder.paneLeft
	drive(t, a, key("shift+right"))
	if a.folder.paneLeft <= before {
		t.Fatal("shift+right did not slide the preview")
	}
	for i := 0; i < 20; i++ {
		drive(t, a, key("shift+left"))
	}
	if a.folder.paneLeft != 0 {
		t.Fatalf("the slide went past the left margin: %d", a.folder.paneLeft)
	}
	// MOVING ONTO ANOTHER FILE OPENS IT AT ITS OWN TOP.
	drive(t, a, key("shift+down"))
	drive(t, a, key("shift+down"))
	onFolderRow(t, a, "main.go")
	if a.folder.paneTop != 0 || a.folder.paneLeft != 0 {
		t.Fatalf("the next file opened at %d/%d", a.folder.paneTop, a.folder.paneLeft)
	}
}

// THE WHEEL FOLLOWS THE POINTER. Turned over the preview it scrolls the
// preview; turned over the names it walks the names.
func TestTheWheelBelongsToWhicheverPaneItIsOver(t *testing.T) {
	a, _, root := mixedLab(t)
	openBrowse(t, a, filepath.Join(root, "here"))
	onFolderRow(t, a, "notes.md")
	geom := chooserGeom(t, a)
	body := chooserRowY(t, a, geom.head+1)
	if body < 0 {
		t.Fatal("the sheet drew no body row")
	}

	was, _ := a.folder.here()
	// Over the preview: the cursor stays where it is.
	drive(t, a, tea.MouseMotionMsg{X: chooserX(t, a, geom.pane.from+2), Y: body})
	drive(t, a, tea.MouseWheelMsg{X: chooserX(t, a, geom.pane.from+2), Y: body, Button: tea.MouseWheelDown})
	if got, _ := a.folder.here(); got != was {
		t.Fatalf("a wheel over the preview moved the cursor to %q", got)
	}
	// Over the names: it walks.
	drive(t, a, tea.MouseMotionMsg{X: chooserX(t, a, geom.here.from+2), Y: body})
	drive(t, a, tea.MouseWheelMsg{X: chooserX(t, a, geom.here.from+2), Y: body, Button: tea.MouseWheelDown})
	if got, _ := a.folder.here(); got == was {
		t.Fatal("a wheel over the names did not walk the cursor")
	}
}

// A PRESS IN A FILE'S PREVIEW DOES NOTHING. Source, prose and a picture are
// there to be READ: they have no rows to select, and letting the press fall
// through would move the cursor to whatever row happened to be beside the line
// somebody clicked.
func TestAPressInAFilePreviewMovesNothing(t *testing.T) {
	a, _, root := mixedLab(t)
	openBrowse(t, a, filepath.Join(root, "here"))
	onFolderRow(t, a, "notes.md")
	_ = chooserRows(t, a, -1, "")
	body := chooserRowY(t, a, a.folder.geom.head+2)
	if body < 0 {
		t.Fatal("the sheet drew no third body row")
	}
	if !a.folder.geom.pane.pressable() {
		t.Fatal("the sheet drew no preview column to press in")
	}
	was, _ := a.folder.here()
	drive(t, a, tea.MouseClickMsg{X: chooserX(t, a, a.folder.geom.pane.from+3), Y: body, Button: tea.MouseLeft})
	if got, _ := a.folder.here(); got != was {
		t.Fatalf("a press in a file preview moved the cursor to %q", got)
	}
	if len(a.chips) != 0 {
		t.Fatalf("a press in the preview attached %+v", a.chips)
	}
}

// A PRESS IN A FOLDER'S PREVIEW OPENS THE DIRECTORY IT IS ON, which is
// the whole of the dead-column defect this wave was opened for: the pane drew a
// directory's contents in rows that looked exactly like the column beside them
// and answered to no pointer at all.
//
// The preview row is already a fully specified navigation target, so one press
// drills down. A file row instead becomes the selected preview subject and
// neither gesture attaches anything.
func TestAPressInAFolderPreviewOpensTheDirectoryItIsOn(t *testing.T) {
	a, _, root := mixedLab(t)
	// Something for the pane to LIST. A directory row whose preview is empty has
	// no rows to press, which is a different fact and is tested elsewhere.
	if err := os.MkdirAll(filepath.Join(root, "here", "inner", "leaf"), 0o755); err != nil {
		t.Fatal(err)
	}
	openBrowse(t, a, filepath.Join(root, "here"))
	onFolderRow(t, a, "inner")
	if a.folder.preview.Kind != previewFolder {
		t.Fatalf("the preview beside `inner` is kind %v", a.folder.preview.Kind)
	}
	if len(a.folder.preview.Entries) == 0 {
		t.Fatal("the fixture's `inner` has nothing in it to press")
	}
	_ = chooserRows(t, a, -1, "")
	want := a.folder.preview.Entries[0].Raw
	body := chooserRowY(t, a, a.folder.geom.head)
	if body < 0 || a.folder.geom.paneBody < 1 {
		t.Fatalf("the pane drew %d entry rows at row %d", a.folder.geom.paneBody, body)
	}
	// IT LIGHTS BEFORE IT ACTS, which is the law that what brightens under the
	// pointer is what a press acts on.
	drive(t, a, tea.MouseMotionMsg{X: chooserX(t, a, a.folder.geom.pane.from+1), Y: body})
	if a.folder.paneHot != 0 {
		t.Fatalf("the pointer over the first preview row lit %d", a.folder.paneHot)
	}
	drive(t, a, tea.MouseClickMsg{X: chooserX(t, a, a.folder.geom.pane.from+1), Y: body, Button: tea.MouseLeft})
	if a.folder.cols.dir != filepath.Join(root, "here", "inner", want) {
		t.Fatalf("the press left the columns on %s", a.folder.cols.dir)
	}
	// AND IT CHOSE NOTHING. Navigating and choosing are two acts with two
	// gestures, in the preview exactly as in the columns.
	if len(a.folder.marks) != 0 || len(a.chips) != 0 {
		t.Fatalf("a press in the preview chose %+v / %+v", a.folder.marks, a.chips)
	}
}

// A NARROW TERMINAL KEEPS THE NAMES LEGIBLE AND OFFERS THE PREVIEW ALONE
// INSTEAD OF SQUEEZING IT IN, and nothing ever runs past the frame.
func TestANarrowSheetKeepsTheNamesAndStillPreviews(t *testing.T) {
	a, _, root := mixedLab(t)
	for _, width := range []int{38, 52, 64, 80, 100, 140, 200} {
		a.width, a.height = width, 30
		a.touch()
		openBrowse(t, a, filepath.Join(root, "here"))
		onFolderRow(t, a, "main.go")
		div := folderDivide(width, a.folder.pane)
		if div.here < 1 {
			t.Fatalf("at %d cells the names are %d wide", width, div.here)
		}
		if div.pane > 0 && div.here < folderNameFloor {
			t.Fatalf("at %d cells the pane squeezed the names to %d", width, div.here)
		}
		for _, line := range chooserRows(t, a, -1, "") {
			if ansi.StringWidth(line) > width {
				t.Fatalf("at %d cells a row runs past the frame: %q", width, plain(line))
			}
		}
		// And the preview alone fits too.
		drive(t, a, key(folderWideKey))
		for _, line := range chooserRows(t, a, -1, "") {
			if ansi.StringWidth(line) > width {
				t.Fatalf("at %d cells the wide preview runs past the frame: %q", width, plain(line))
			}
		}
		if folderDivide(width, a.folder.pane).pane < 1 {
			t.Fatalf("at %d cells %s gave the preview nothing", width, folderWideKey)
		}
		drive(t, a, key(folderWideKey))
	}
}

// THE PICKS AND THE INDEX LANDING MID-BROWSE DO NOT TAKE ANYTHING AWAY. They
// arrive a second or two after the sheet opens, from a background read, and the
// sheet is rebuilt around them — so everything a person has done in that second
// has to survive it, and the read the rebuild cancelled has to be made again.
//
// This is a real defect this wave caused and a real terminal capture caught: the
// first `/folder` of a launch drew an empty preview pane until the next
// keystroke, and a folder chosen in that second was silently discarded.
func TestThePicksLandingMidBrowseKeepTheChoicesAndThePreview(t *testing.T) {
	a, _, root := mixedLab(t)
	a.folderStoreRead = false
	settleFolder(t, a, a.openFolderPick(filepath.Join(root, "here")+"/"))
	onFolderRow(t, a, "inner")
	drive(t, a, key(folderMarkKey))
	onFolderRow(t, a, "main.go")
	drive(t, a, key(folderPaneKey)) // the preview off, deliberately
	drive(t, a, key(folderPaneKey)) // and back on
	for i := 0; i < 3; i++ {
		drive(t, a, key("shift+down"))
	}
	was := a.folder.preview
	if was.empty() {
		t.Fatal("the pane says nothing about main.go before the store lands")
	}
	scrolled, pane := a.folder.paneTop, a.folder.pane

	// The store lands, exactly as the background read delivers it.
	settleFolder(t, a, func() tea.Msg {
		return folderStoreMsg{store: folderStore{Roots: []string{filepath.Join(root, "here")}}}
	})

	if len(a.folder.marks) != 1 || !a.folder.marks[0].dir {
		t.Fatalf("the store landing discarded the choices: %+v", a.folder.marks)
	}
	if a.folder.pane != pane || a.folder.paneTop != scrolled {
		t.Fatalf("the store landing reset the pane to %v/%d, want %v/%d",
			a.folder.pane, a.folder.paneTop, pane, scrolled)
	}
	if a.folder.preview.empty() {
		t.Fatal("the store landing left the preview pane blank")
	}
	if a.folder.preview.Key.Path != was.Key.Path {
		t.Fatalf("the pane now shows %q, want %q", a.folder.preview.Key.Path, was.Key.Path)
	}
}

// ── the doors ───────────────────────────────────────────────────────────────

// A BARE /attach OPENS THE BROWSER rather than correcting the person. Somebody
// who typed the word without a path is somebody who does not know the path.
func TestABareAttachOpensTheBrowserWhereTheConversationStands(t *testing.T) {
	a, _, root := mixedLab(t)
	settleFolder(t, a, a.slash("/attach"))
	if !a.folder.open || !a.folder.browsing {
		t.Fatalf("a bare /attach left the browser open=%v browsing=%v", a.folder.open, a.folder.browsing)
	}
	if a.folder.cols.dir != filepath.Join(root, "here") {
		t.Fatalf("it opened on %s, want where the conversation stands", a.folder.cols.dir)
	}
	if a.folder.cols.here.rows() == 0 {
		t.Fatal("it opened on a level with nothing on it")
	}
	// And /attach with a path is untouched: straight onto the tray.
	a.folder.close()
	a.attachFilePath(filepath.Join(root, "here", "notes.md"))
	if len(a.chips) != 1 || !a.chips[0].file {
		t.Fatalf("/attach <path> put %+v on the tray", a.chips)
	}
}

// A CONVERSATION WITH NO FOLDER DOOR STILL GETS THE BROWSER FOR FILES, and the
// folder half refuses in the sentence that says what would be missing.
func TestWithoutAFolderDoorTheBrowserStillAttachesFiles(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "here", "inner"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "here", "one.txt"), []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := newApp(t.Context(), Options{Agent: &fakeAgent{model: "m"}, Workspace: filepath.Join(root, "here")})
	// The door is taken away AFTER construction, the way folderbrowse_test.go
	// does it: [doorlessAgent] embeds the interface, so it has exactly the
	// methods [Agent] declares and ReferPlace and Places are not among them.
	a.agent = doorlessAgent{a.agent}
	a.width, a.height = 140, 34
	a.pal = newPalette(tokens.ANSI256, false)
	a.entries = nil
	a.folderStoreRead = true
	a.file = filepath.Join(root, "session.jsonl")

	// /folder is a request to give the conversation a directory, and refuses.
	settleFolder(t, a, a.openFolderPick(""))
	if a.folder.open {
		t.Fatal("/folder opened a browser nothing could be chosen from")
	}
	if !strings.Contains(transcriptText(a), folderNoDoorWord) {
		t.Fatalf("the refusal was not said:\n%s", transcriptText(a))
	}
	// A bare /attach is a request about the next message, and opens.
	settleFolder(t, a, a.openContextPick("", false))
	if !a.folder.open {
		t.Fatal("a bare /attach refused for want of a folder door it does not need")
	}
	onFolderRow(t, a, "one.txt")
	settleFolder(t, a, a.folderConfirm())
	if len(a.chips) != 1 || a.chips[0].name() != "one.txt" {
		t.Fatalf("the file did not reach the tray: %+v", a.chips)
	}
	// And a folder row on the same sheet says what would be missing.
	settleFolder(t, a, a.openContextPick("", false))
	onFolderRow(t, a, "inner")
	if cmd := a.folderConfirm(); cmd != nil {
		t.Fatal("a folder was registered on a conversation that cannot hold one")
	}
	if !strings.Contains(transcriptText(a), folderNoDoorWord) {
		t.Fatalf("the folder half did not refuse:\n%s", transcriptText(a))
	}
}

// `→` ON A FILE DOES NOT NAVIGATE AND DOES NOT OPEN ANYTHING. This browser
// reads a file into a pane and never runs it.
func TestRightOnAFileGoesNowhere(t *testing.T) {
	a, _, root := mixedLab(t)
	openBrowse(t, a, filepath.Join(root, "here"))
	onFolderRow(t, a, "main.go")
	was := a.folder.cols.dir
	drive(t, a, key("right"))
	if a.folder.cols.dir != was {
		t.Fatalf("→ on a file walked to %s", a.folder.cols.dir)
	}
	if len(a.chips) != 0 {
		t.Fatalf("→ on a file attached %+v", a.chips)
	}
}

// A FILE THAT WENT AWAY BETWEEN THE ROW AND THE CONFIRM IS A SENTENCE AND NOT A
// CHIP, and so is a directory that is no longer there.
func TestAThingThatVanishedUnderTheConfirmSaysSo(t *testing.T) {
	a, _, root := mixedLab(t)
	openBrowse(t, a, filepath.Join(root, "here"))
	onFolderRow(t, a, "main.go")
	drive(t, a, key(folderMarkKey))
	onFolderRow(t, a, "inner")
	drive(t, a, key(folderMarkKey))
	if err := os.Remove(filepath.Join(root, "here", "main.go")); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(root, "here", "inner")); err != nil {
		t.Fatal(err)
	}
	settleFolder(t, a, a.folderConfirm())

	if len(a.chips) != 0 {
		t.Fatalf("a file that is not there reached the tray: %+v", a.chips)
	}
	said := transcriptText(a)
	if !strings.Contains(said, "no such file: main.go") {
		t.Fatalf("the missing file was not named:\n%s", said)
	}
	if !strings.Contains(said, folderGoneWord) {
		t.Fatalf("the missing folder was not named:\n%s", said)
	}
}

// THE WAY OUT AND THE PREVIEW'S OWN DOOR ARE ON THE SHEET AT EVERY WIDTH, and
// which of the two places names them is contextual.
//
// The foot row used to name every chord the sheet owns, at every width, which is
// the wall of shortcuts the owner's review asked us to stop drawing. `alt+o` is
// on the foot exactly where the preview is NOT drawn — there it is the only way
// to read a file at all — and in the box's own placeholder where it is, because
// the box is empty while browsing now and its placeholder is on screen.
func TestThePreviewDoorAndTheWayOutAreOnTheSheetAtEveryWidth(t *testing.T) {
	a, _, root := mixedLab(t)
	for _, width := range []int{52, 100, 170} {
		a.width, a.height = width, 32
		a.touch()
		openBrowse(t, a, filepath.Join(root, "here"))
		frame, _, _ := a.frameBody()
		drawn := ansi.Strip(frame)
		for _, word := range []string{"alt+o", "esc"} {
			if !strings.Contains(drawn, word) {
				t.Fatalf("%d columns hide %q: %s", width, word, drawn)
			}
		}
	}
}

// Home has its own composer, so this is a real way to browse without replacing
// the conversation's unsent draft with a slash command first.
func TestContextBrowserFromHomeRevealsTheSheetAndPreservesTheChatDraft(t *testing.T) {
	a, _, _ := mixedLab(t)
	a.input.setText("keep this unsent draft")
	a.openHome()
	settleFolder(t, a, a.homeSlash("/project"))
	if a.at(pageHome) || !a.folder.open {
		t.Fatal("Home hides the context browser it just opened")
	}
	drive(t, a, key("esc"))
	if a.input.String() != "keep this unsent draft" {
		t.Fatal("cancel lost the suspended chat draft")
	}
}
