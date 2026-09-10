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
		margin := commandMargin(label, c, 80)
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
		{"folder", "", fateTargetFolder},
		{"folder", "~/src", fateTargetFolder},
		{"attach", "", fateTray},
		{"image", "shot.png", fateTray},
		{"pricing", "", ""},
	} {
		if got := homeFate(want.word, want.rest); got != want.fate {
			t.Fatalf("/%s %s has fate %q, want %q", want.word, want.rest, got, want.fate)
		}
	}
}

// ── /folder: the browser, aimed at the target ───────────────────────────────

// BARE /folder OPENS THE ONE BROWSER, AIMED AT THE TARGET, and a folder
// confirmed there PINS THE NEXT CONVERSATION'S FOLDER rather than moving the
// conversation behind home. Home comes back under it with the rule already
// saying the new folder, which is the whole of what the owner asked to see.
func TestFolderAtHomeBrowsesForTheTargetAndPinsIt(t *testing.T) {
	a, _, root := mixedLab(t)
	runCmd(a.openHome())

	settleFolder(t, a, a.homeSlash("/folder"))
	if !a.folder.open || !a.folder.forTarget {
		t.Fatalf("bare /folder did not open the browser for the target: open=%v target=%v",
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
	if want := targetMovedWord + a.hostedPath(shortPath(inner, a.tilde, 0)); a.home.msg != want {
		t.Fatalf("home said %q, want %q", a.home.msg, want)
	}
	// THE RULE ABOVE THE BOX SAYS IT ON THE VERY NEXT FRAME.
	if text := homeText(a); !strings.Contains(text, targetPathWord(a)) {
		t.Fatalf("the rule does not name the folder that was just pinned:\n%s", text)
	}
	// AND NOTHING REACHED THE CONVERSATION BEHIND HOME. A pin is a decision about
	// a conversation that does not exist yet.
	if len(a.attachedPlaces()) > 0 {
		t.Fatalf("the pick was referred to the conversation behind home: %v", a.attachedPlaces())
	}
}

// /folder WITH A PATH IS THE SAME SHEET, opened on that path — one question, one
// surface, whichever way it was asked.
func TestFolderWithAPathAtHomeIsTheSameTargetSheet(t *testing.T) {
	a, _, root := mixedLab(t)
	runCmd(a.openHome())

	settleFolder(t, a, a.homeSlash("/folder "+filepath.Join(root, "here")+"/"))
	if !a.folder.open || !a.folder.forTarget {
		t.Fatalf("/folder <path> at home did not open the target's browser: open=%v target=%v",
			a.folder.open, a.folder.forTarget)
	}
	if a.folder.cols.dir != filepath.Join(root, "here") {
		t.Fatalf("the sheet opened on %q, want the path that was typed", a.folder.cols.dir)
	}
}

// ESC OUT OF THE SHEET HOME OPENED LANDS BACK ON HOME, with nothing pinned. The
// browser only replaced home because a place cannot draw a modal, and a person
// who pressed esc has not asked to leave the screen they were standing on.
func TestEscOutOfTheTargetBrowserLandsBackOnHome(t *testing.T) {
	a, _, _ := mixedLab(t)
	runCmd(a.openHome())
	was := a.targetWhere()

	settleFolder(t, a, a.homeSlash("/folder"))
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
	runCmd(a.homeSlash("/image " + filepath.Join(root, "here", "shot.png")))
	if !a.at(pageHome) {
		t.Fatal("/image <path> at home opened a conversation")
	}
	if len(a.chips) != 2 || a.chips[1].name() != "shot.png" {
		t.Fatalf("the picture did not reach home's tray: %+v", a.chips)
	}
	if want := folderAttachedWord + "shot.png" + homeRidesWord; a.home.msg != want {
		t.Fatalf("home said %q, want %q", a.home.msg, want)
	}
}

// A BARE /attach ASKS FOR THE PATH WHERE IT WAS TYPED. It used to open a
// conversation to hold a browser, which is a conversation started for a
// question — and the two ways a file reaches home's tray are named instead.
func TestBareAttachAtHomeAsksForThePath(t *testing.T) {
	a, _, _ := mixedLab(t)
	runCmd(a.openHome())

	runCmd(a.homeSlash("/attach"))
	if !a.at(pageHome) {
		t.Fatal("a bare /attach at home left the screen")
	}
	if a.folder.open {
		t.Fatal("a bare /attach at home opened the browser")
	}
	if a.home.msg != homeTypeThePathWord {
		t.Fatalf("home said %q, want %q", a.home.msg, homeTypeThePathWord)
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
	if want := targetMovedWord + a.hostedPath(shortPath(inner, a.tilde, 0)); a.home.msg != want {
		t.Fatalf("home said %q, want %q", a.home.msg, want)
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
