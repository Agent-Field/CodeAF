package tui3

// homefate_test.go is THE FATE OF A COMMAND AT HOME: what `enter` will do with
// it on a screen that is not a conversation, read off the row BEFORE the key is
// pressed, and done by the same table that drew it (homeslash.go's [homeFate]).
//
// The owner's ask, in their words: "see what other slash commands need clear
// indication like that even if it's not usable". So the two claims under test
// are that every command has a fate, and that the fate on the row is the road
// the dispatch actually takes.

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// ── the table ───────────────────────────────────────────────────────────────

// EVERY ROW OF THE COMMAND TABLE HAS A FATE, AND ITS ALIASES HAVE THE SAME ONE.
// A command added without one draws a margin that says only what the command
// does elsewhere — which is the exact silence this wave closed — so the build
// fails here rather than on somebody's screen.
func TestEveryCommandHasAFateAtHome(t *testing.T) {
	for _, c := range commands {
		fate := homeFate(c.name, c.args)
		if fate == "" {
			t.Fatalf("/%s %s has no fate at home · add it to homeFate", c.name, c.args)
		}
		// AND THE OTHER WORDS FOR IT REACH THE SAME ONE. An alias is the same
		// command (commands.go's [canonicalCommand]), so a person who types
		// /clear must be told what /new does here and not nothing at all.
		for _, other := range c.alias {
			if got := homeFate(other, c.args); got != fate {
				t.Fatalf("/%s says %q where /%s says %q", other, got, c.name, fate)
			}
		}
	}
}

// THE FATE IS NEVER THE HALF THAT IS CUT. At eighty columns the margin is built
// fate-first and the command's own note takes what is left of it — whole, or not
// at all — so no row can promise half a sentence.
func TestTheFateLeadsTheCommandRowAndIsNeverCut(t *testing.T) {
	for _, c := range commands {
		label := c.typed()
		margin := commandMargin(label, c, 80, chordSpelling{meta: chordAltWord})
		fate := homeFate(c.name, c.args)
		if !strings.HasPrefix(margin, fate) {
			t.Fatalf("/%s %s draws %q, which does not lead with %q", c.name, c.args, margin, fate)
		}
		if room := overlayNoteRoom(label, 80); ansi.StringWidth(margin) > room {
			t.Fatalf("/%s %s draws %d cells into a %d-cell margin: %q",
				c.name, c.args, ansi.StringWidth(margin), room, margin)
		}
	}
}

// AND THE PAIRS THAT MEAN TWO THINGS ARE TOLD APART BY THEIR ARGUMENT. Four
// commands are a place bare and work with words after them, and a table keyed on
// the name alone would send a person to the wrong one of each pair.
func TestTheFateReadsTheArgumentWhereItChangesTheAnswer(t *testing.T) {
	for _, want := range []struct {
		word, rest, fate string
	}{
		{"standing", "", fatePlace},
		{"standing", "keep the tests green", fateNeedsChat},
		{"crew", "", fateNeedsChat},
		{"crew", "frugal", fateAnswers},
		{"task", "", fatePlace},
		{"task", "port the parser", fateNeedsChat},
		{"memory", "", fatePlace},
		{"memory", "branches", fateAnswers},
		// /folder MEANS ONE THING EVERYWHERE since 2026-09-22: give THIS
		// conversation a folder, so on home it needs one opened first. The pin
		// it used to be here is /project (projectcmd.go).
		{"folder", "", fateNeedsChat},
		{"folder", "~/src", fateNeedsChat},
		{"project", "", fateTargetFolder},
		{"project", "~/src", fateTargetFolder},
		{"attach", "", fateTray},
		{"attach", "shot.png", fateTray},
		{"pricing", "", ""},
	} {
		if got := homeFate(want.word, want.rest); got != want.fate {
			t.Fatalf("/%s %s has fate %q, want %q", want.word, want.rest, got, want.fate)
		}
	}
}

// ── /project: the pin, and the browser behind it ────────────────────────────

// BARE /project OPENS THE ONE BROWSER, AIMED AT THE TARGET, and a folder
// confirmed there PINS THE NEXT CONVERSATION'S FOLDER rather than moving the
// conversation behind home. Home comes back under it with the rule already
// saying the new folder, which is the whole of what the owner asked to see.
func TestProjectAtHomeBrowsesForTheTargetAndPinsIt(t *testing.T) {
	a, _, root := mixedLab(t)
	runCmd(a.openHome())

	settleFolder(t, a, a.homeSlash("/project"))
	if !a.folder.open || !a.folder.forTarget {
		t.Fatalf("bare /project did not open the browser for the target: open=%v target=%v",
			a.folder.open, a.folder.forTarget)
	}
	// The action row says what enter would do, in the target's own words.
	if row := plain(strings.Join(chooserRows(t, a, -1, ""), "\n")); !strings.Contains(row, folderTargetWord) {
		t.Fatalf("the sheet does not offer the target's own verb:\n%s", row)
	}
	// And nothing on it offers to remove a folder from the conversation behind
	// home, because none of these folders is that conversation's business.
	if len(a.folder.held) > 0 {
		t.Fatalf("the target's sheet marked folders as already held: %v", a.folder.held)
	}

	onFolderRow(t, a, "inner")
	settleFolder(t, a, a.folderConfirm())

	inner := filepath.Join(root, "here", "inner")
	if a.target.where != inner {
		t.Fatalf("the pick pinned %q, want %q", a.target.where, inner)
	}
	if !a.at(pageHome) {
		t.Fatal("the pick did not land back on home")
	}
	if a.home.msg != "" {
		t.Fatalf("the project selection added a footer message: %q", a.home.msg)
	}
	// THE KEYS ROW UNDER THE BOX SAYS IT ON THE VERY NEXT FRAME (hometip.go), read
	// at a width where a temp-dir path is not cut.
	if text := ansi.Strip(a.homeFootLine(400, a.pal)); !strings.Contains(text, targetPathWord(a)) {
		t.Fatalf("the keys row does not name the folder that was just pinned:\n%s", text)
	}
	// AND NOTHING REACHED THE CONVERSATION BEHIND HOME. A pin is a decision about
	// a conversation that does not exist yet.
	if len(a.attachedPlaces()) > 0 {
		t.Fatalf("the pick was referred to the conversation behind home: %v", a.attachedPlaces())
	}
}

// THE STORE LANDING DOES NOT CHANGE WHO THE SHEET IS ABOUT. The first browser of
// a launch opens before the background read of the pick counts answers, and
// that answer rebuilds the sheet; a rebuild that forgot [folderPick.forTarget]
// turned home's `the next conversation's folder` into `add context`, and the
// folder chosen a second later was referred to the conversation BEHIND home —
// caught in a real terminal on a fresh profile, where every test here had
// already read the store.
func TestTheStoreLandingKeepsHomesSheetAboutTheNextConversation(t *testing.T) {
	a, _, root := mixedLab(t)
	runCmd(a.openHome())

	settleFolder(t, a, a.homeSlash("/project"))
	if !a.folder.open || !a.folder.forTarget {
		t.Fatalf("bare /project did not open the browser for the target: open=%v target=%v",
			a.folder.open, a.folder.forTarget)
	}

	// The store lands, exactly as the background read delivers it.
	settleFolder(t, a, func() tea.Msg {
		return folderStoreMsg{store: folderStore{Roots: []string{filepath.Join(root, "here")}}}
	})
	if !a.folder.open || !a.folder.forTarget {
		t.Fatalf("the store landing turned home's sheet into the conversation's: open=%v target=%v",
			a.folder.open, a.folder.forTarget)
	}
	if len(a.folder.held) > 0 {
		t.Fatalf("the store landing marked the conversation behind home's folders as held: %v", a.folder.held)
	}

	onFolderRow(t, a, "inner")
	settleFolder(t, a, a.folderConfirm())

	inner := filepath.Join(root, "here", "inner")
	if a.target.where != inner {
		t.Fatalf("the pick pinned %q, want %q", a.target.where, inner)
	}
	if !a.at(pageHome) {
		t.Fatal("the pick did not land back on home")
	}
	if len(a.attachedPlaces()) > 0 {
		t.Fatalf("the pick was referred to the conversation behind home: %v", a.attachedPlaces())
	}
}

// /project WITH A PATH TAKES THE PATH AND OPENS NOTHING. A person who typed the
// folder has already answered the question the browser exists to ask, and the
// pin is a string on this window rather than a round trip — so the keys row
// says the new folder on the very next frame.
func TestProjectWithAPathAtHomePinsItWithoutTheBrowser(t *testing.T) {
	a, _, root := mixedLab(t)
	runCmd(a.openHome())

	inner := filepath.Join(root, "here", "inner")
	runCmd(a.homeSlash("/project " + inner))

	if a.folder.open {
		t.Fatal("/project <path> opened the browser instead of taking the path")
	}
	if a.target.where != inner {
		t.Fatalf("/project <path> pinned %q, want %q", a.target.where, inner)
	}
	if !a.at(pageHome) {
		t.Fatal("/project <path> left home")
	}
	// AND IT SAYS NOTHING, because the row it would be drawn over is the row
	// that answers. Home's sentence is drawn IN PLACE OF the keys row and
	// stands until the next keystroke, so a success reported there hid the
	// keys and said what `project: <path>` at their right end was already
	// saying (projectcmd.go states the law).
	if a.home.msg != "" {
		t.Fatalf("a taken path wrote %q over home's keys row", a.home.msg)
	}
	text := ansi.Strip(a.homeFootLine(400, a.pal))
	if !strings.Contains(text, targetPathWord(a)) {
		t.Fatalf("the keys row does not name the folder that was just pinned:\n%s", text)
	}
	if !strings.Contains(text, homeOptionsWord) {
		t.Fatalf("the keys are missing from the row that just pinned a folder:\n%s", text)
	}
}

// AND A PATH THAT IS NOT A FOLDER IS REFUSED IN THE WORDS THAT WERE TYPED.
// Nothing is pinned: a destination that is not there would be found out one
// `enter` later, in the conversation that could not open.
func TestProjectRefusesAPathThatIsNotAFolder(t *testing.T) {
	a, _, root := mixedLab(t)
	runCmd(a.openHome())

	for _, rest := range []string{
		filepath.Join(root, "here", "notes.md"),
		filepath.Join(root, "nowhere-at-all"),
	} {
		runCmd(a.homeSlash("/project " + rest))
		if a.target.where != "" {
			t.Fatalf("/project %s pinned %q", rest, a.target.where)
		}
		if want := projectNoFolderWord + rest; a.home.msg != want {
			t.Fatalf("home said %q, want %q", a.home.msg, want)
		}
		if a.folder.open {
			t.Fatalf("/project %s opened the browser", rest)
		}
	}
}

// /project IS HOME'S, AND A CONVERSATION SAYS SO. It used to be the home half
// of /folder, which is one keystroke apart in spelling from the command that
// does the neighbouring job here — so the answer names both.
func TestProjectInAConversationSaysItIsHomes(t *testing.T) {
	a := newTestApp(&fakeAgent{})
	runCmd(a.slash("/project ~/src"))

	if a.target.where != "" {
		t.Fatalf("/project in a conversation pinned %q", a.target.where)
	}
	if a.folder.open {
		t.Fatal("/project in a conversation opened the browser")
	}
	if text := transcriptText(a); !strings.Contains(text, projectIsHomesWord) {
		t.Fatalf("the conversation does not say where /project lives:\n%s", text)
	}
}

// AND /folder ON HOME OPENS A CONVERSATION FIRST. It means one thing
// everywhere now — give THIS conversation a folder — and home has no this.
func TestFolderAtHomeOpensAConversationAndBrowsesThere(t *testing.T) {
	lab := newHomeLab(t)
	a := lab.door("")
	goHome(t, a)
	if got := homeFate("folder", ""); got != fateNeedsChat {
		t.Fatalf("the drop-up says /folder %q on home", got)
	}

	settleFolder(t, a, a.homeSlash("/folder"))

	if a.at(pageHome) {
		t.Fatal("/folder at home stayed on home")
	}
	if !a.folder.open {
		t.Fatal("/folder at home did not open the browser in the conversation it started")
	}
	if a.folder.forTarget {
		t.Fatal("/folder at home opened the target's sheet, which is /project's")
	}
	if a.target.where != "" {
		t.Fatalf("/folder at home pinned %q", a.target.where)
	}
}

// ESC OUT OF THE SHEET HOME OPENED LANDS BACK ON HOME, with nothing pinned. The
// browser only replaced home because a place cannot draw a modal, and a person
// who pressed esc has not asked to leave the screen they were standing on.
func TestEscOutOfTheTargetBrowserLandsBackOnHome(t *testing.T) {
	a, _, _ := mixedLab(t)
	runCmd(a.openHome())
	was := a.targetWhere()

	settleFolder(t, a, a.homeSlash("/project"))
	drive(t, a, key("esc"))

	if a.folder.open {
		t.Fatal("esc left the sheet up")
	}
	if !a.at(pageHome) {
		t.Fatal("esc out of the target's browser did not land back on home")
	}
	if a.target.where != "" || a.targetWhere() != was {
		t.Fatalf("esc changed the target to %q", a.targetWhere())
	}
}

// ── /attach and /image: home's own tray ─────────────────────────────────────

// A FILE ATTACHED AT HOME RIDES ON HOME'S TRAY and opens no conversation. The
// tray belongs to the person, home already carries it into the conversation it
// opens next, and the line under the box says exactly that.
func TestAttachAtHomeLandsOnHomesTrayAndSaysSo(t *testing.T) {
	a, _, root := mixedLab(t)
	runCmd(a.openHome())

	runCmd(a.homeSlash("/attach " + filepath.Join(root, "here", "notes.md")))
	if !a.at(pageHome) {
		t.Fatal("/attach <path> at home opened a conversation")
	}
	if len(a.chips) != 1 || a.chips[0].name() != "notes.md" {
		t.Fatalf("the file did not reach home's tray: %+v", a.chips)
	}
	if want := folderAttachedWord + "notes.md" + homeRidesWord; a.home.msg != want {
		t.Fatalf("home said %q, want %q", a.home.msg, want)
	}
	// AND THE FRAME DRAWS THE CHIP WHILE HOME IS UP (placebodies.go's tray).
	if text := homeText(a); !strings.Contains(text, "notes.md") {
		t.Fatalf("home does not draw the chip it is carrying:\n%s", text)
	}

	// A picture goes the same way, through the same tray.
	runCmd(a.homeSlash("/attach " + filepath.Join(root, "here", "shot.png")))
	if !a.at(pageHome) {
		t.Fatal("/attach <picture> at home opened a conversation")
	}
	if len(a.chips) != 2 || a.chips[1].name() != "shot.png" {
		t.Fatalf("the picture did not reach home's tray: %+v", a.chips)
	}
	if want := folderAttachedWord + "shot.png" + homeRidesWord; a.home.msg != want {
		t.Fatalf("home said %q, want %q", a.home.msg, want)
	}
}

// A BARE /attach AT HOME OPENS THE BROWSER, aimed at the next conversation's
// folder the way a bare /folder is, and a file chosen there lands on home's
// tray (folderact.go's [app.targetFolderConfirm]). It used to answer `type
// the path after /attach`, a correction where a person wanted a door.
func TestBareAttachAtHomeOpensTheBrowserForTheTarget(t *testing.T) {
	a, _, _ := mixedLab(t)
	runCmd(a.openHome())

	runCmd(a.homeSlash("/attach"))
	if !a.folder.open || !a.folder.forTarget {
		t.Fatalf("a bare /attach at home did not open the target's browser: open=%v target=%v",
			a.folder.open, a.folder.forTarget)
	}
	if a.home.msg != "" {
		t.Fatalf("a bare /attach at home said %q instead of opening the sheet", a.home.msg)
	}
}

// A FOLDER HANDED TO /attach AT HOME IS THE TARGET'S. The dispatcher gives one
// to the conversation behind the screen, where the answer cannot be read; here
// it is the same decision /folder makes, in the same words.
func TestAttachAFolderAtHomePinsTheTarget(t *testing.T) {
	a, _, root := mixedLab(t)
	runCmd(a.openHome())

	inner := filepath.Join(root, "here", "inner")
	runCmd(a.homeSlash("/attach " + inner))
	if a.target.where != inner {
		t.Fatalf("a folder after /attach pinned %q, want %q", a.target.where, inner)
	}
	if len(a.chips) != 0 {
		t.Fatalf("a folder reached the tray: %+v", a.chips)
	}
	if a.home.msg != "" {
		t.Fatalf("the project selection added a footer message: %q", a.home.msg)
	}
}

// ── /new: the conversation behind the screen ────────────────────────────────

// /new AT HOME SAYS WHICH CONVERSATION IT REPLACED. Its own road notes
// `new session · <path>`, which on home reads as though it were about the
// conversation `enter` is going to open — and the one it actually replaced is
// the one nobody can see.
func TestNewAtHomeSaysItWasTheConversationBehindHome(t *testing.T) {
	lab := newHomeLab(t)
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "porting the resume picker", "/tmp/alpha", time.Now())
	a := lab.app(mine)
	runCmd(a.openHome())

	next := &fakeAgent{model: "m"}
	a.start = func(string) (Conversation, error) {
		return Conversation{Agent: next, SessionFile: "/tmp/alpha/next/transcript.jsonl"}, nil
	}
	typeHome(a, "/new")
	runCmd(a.key(key("enter")))

	if !a.at(pageHome) {
		t.Fatal("/new at home left the screen")
	}
	if a.home.msg != homeFreshBehindWord {
		t.Fatalf("/new said %q, want %q", a.home.msg, homeFreshBehindWord)
	}
}
