package tui3

// THE THUMBNAIL UNDER A ROW NOBODY OPENED.
//
// imagepreview_test.go is about what a person who CLICKED a picture call gets.
// This file is about the row on its own, which is where a generated picture is
// actually met: the call finishes, and the picture is already there.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// hungRowsAt is everything the first tool call hangs beneath its line at a
// given width, plain. Unlike [previewRowsAt] it tolerates a call that hangs
// nothing, which is the answer half the tests below are checking for.
func hungRowsAt(t *testing.T, a *app, width int) []string {
	t.Helper()
	d := a.conversation()
	for i := range d.entries {
		if d.entries[i].kind != entryTool {
			continue
		}
		rows := a.toolRows(d, i, true, width)
		out := make([]string, 0, len(rows))
		for _, r := range rows[1:] {
			out = append(out, plain(r.text))
		}
		return out
	}
	t.Fatal("no tool entry")
	return nil
}

// firstToolEntry is the call the tests are about, so an assertion about a row's
// state cannot land on the person's own line by accident.
func firstToolEntry(t *testing.T, a *app) *entry {
	t.Helper()
	for i := range a.entries {
		if a.entries[i].kind == entryTool {
			return &a.entries[i]
		}
	}
	t.Fatal("no tool entry")
	return nil
}

// pictureRowApp is one finished picture call over a real file on disk, in a
// workspace of its own.
func pictureRowApp(t *testing.T, pal palette, name string, events []session.Event) (*app, string) {
	t.Helper()
	dir := t.TempDir()
	path := writePicture(t, dir, name, wideTestPicture())
	agent := &fakeAgent{model: "m", turns: [][]session.Event{
		append(events, session.Event{Kind: session.EventTurnDone}),
	}}
	a := newTestApp(agent)
	a.pal = pal
	a.workspace = dir
	runTurn(t, a, agent, "draw me a harbour")
	return a, path
}

// THE HEADLINE. A finished `generate_image` draws its picture with no click.
func TestAFinishedGeneratedPictureDrawsItselfUnderTheRow(t *testing.T) {
	a, _ := pictureRowApp(t, newPalette(tokens.TrueColor, false),
		".aforge-v3/images/harbour.png",
		call("generate_image", `{"prompt":"a harbour at dawn"}`,
			".aforge-v3/images/harbour.png — 64×32 png, 1.2KB, generated on paint/model"))

	if firstToolEntry(t, a).open {
		t.Fatal("the test opened the row it exists to test closed")
	}
	hung := hungRowsAt(t, a, 60)
	if paintedRows(hung) == 0 {
		t.Fatalf("a finished picture hung nothing:\n%s", strings.Join(hung, "\n"))
	}
	// AND NOT ONE WORD MORE. The thumbnail is the whole block; the path, the
	// shape and the size are the expansion's, one click away.
	for _, r := range hung {
		if !strings.Contains(r, halfBlock) {
			t.Fatalf("the thumbnail hung a line of chrome: %q", r)
		}
	}
}

// The looking tool is the same row and the same rule.
func TestAFinishedLookDrawsThePictureItLookedAt(t *testing.T) {
	a, _ := pictureRowApp(t, newPalette(tokens.TrueColor, false),
		".aforge-v3/images/harbour.png",
		call("view_image", `{"path":".aforge-v3/images/harbour.png"}`,
			"seen by look/model: a harbour, the mast leaning left"))

	if hung := hungRowsAt(t, a, 60); paintedRows(hung) == 0 {
		t.Fatalf("a finished look hung no picture:\n%s", strings.Join(hung, "\n"))
	}
}

// A CALL STILL RUNNING HANGS NOTHING. `generate_image` writes the file last, so
// a thumbnail under a spinner would be a picture of whatever was there before.
func TestAnUnfinishedPictureCallHangsNoThumbnail(t *testing.T) {
	a, _ := pictureRowApp(t, newPalette(tokens.TrueColor, false),
		".aforge-v3/images/harbour.png",
		[]session.Event{{Kind: session.EventToolBegin, Tool: "generate_image",
			Hint: "generate_image", Args: `{"path":".aforge-v3/images/harbour.png"}`}})

	if hung := hungRowsAt(t, a, 60); paintedRows(hung) != 0 {
		t.Fatalf("a running call drew a picture:\n%s", strings.Join(hung, "\n"))
	}
}

// THE RUNGS ARE THE EXPANSION'S OWN. The thumbnail is the same renderer, so a
// terminal that could not be shown the big picture is not shown a small one
// either — and keeps exactly the row it had before this feature existed.
func TestTheThumbnailDegradesByTheSameRungsAsTheExpansion(t *testing.T) {
	for _, c := range []struct {
		name  string
		pal   palette
		draws bool
	}{
		{"truecolor", newPalette(tokens.TrueColor, false), true},
		{"the xterm cube", newPalette(tokens.ANSI256, false), true},
		{"sixteen colours", newPalette(tokens.ANSI16, false), false},
		{"no colour", newPalette(tokens.NoColor, false), false},
		{"no box drawing", newPalette(tokens.TrueColor, true), false},
		{"read aloud", linearPalette(), false},
	} {
		t.Run(c.name, func(t *testing.T) {
			a, _ := pictureRowApp(t, c.pal, ".aforge-v3/images/harbour.png",
				call("generate_image", `{"prompt":"a harbour"}`,
					".aforge-v3/images/harbour.png — 64×32 png, 1.2KB, generated on paint/model"))
			painted := paintedRows(hungRowsAt(t, a, 60))
			if c.draws && painted == 0 {
				t.Fatal("no picture on a terminal that can carry one")
			}
			if !c.draws && painted != 0 {
				t.Fatalf("%d rows of picture on a terminal that cannot carry one", painted)
			}
		})
	}
}

// THE CAP IS THE TIER'S, which is the bound the live preview takes and for the
// same reason: twelve rows nobody asked for is a third of a laptop's body and
// the whole of a phone's.
func TestTheThumbnailKeepsTheTiersCap(t *testing.T) {
	a, _ := pictureRowApp(t, newPalette(tokens.TrueColor, false),
		".aforge-v3/images/harbour.png",
		call("generate_image", `{"prompt":"a harbour"}`,
			".aforge-v3/images/harbour.png — 64×32 png, 1.2KB, generated on paint/model"))

	if wide := paintedRows(hungRowsAt(t, a, 100)); wide == 0 || wide > previewWindow {
		t.Fatalf("a wide frame hung %d rows, want 1..%d", wide, previewWindow)
	}
	phone := hungRowsAt(t, a, phoneWidth)
	painted := paintedRows(phone)
	if painted == 0 {
		t.Fatal("the phone hung no picture at all")
	}
	if painted > previewPhoneWindow {
		t.Fatalf("the phone hung %d rows, past its %d cap", painted, previewPhoneWindow)
	}
	// And it is the RENDER that was bounded, not a taller picture trimmed: a
	// thumbnail never grows the clickable "… N more lines" foot a capped block
	// would, because half a picture is not half an answer.
	for _, r := range phone {
		if strings.Contains(r, "more lines") {
			t.Fatalf("the thumbnail was trimmed rather than sized: %q", r)
		}
	}
}

// A file that cannot be drawn leaves the row EXACTLY as it was: the emptiness
// law on a block, and imagepreview.go's own rule that a preview is an addition
// to a row that already works.
func TestAnUndecodableFileLeavesTheRowUntouched(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "art"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "art", "lies.png"),
		[]byte("this is not a png"), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, c := range []struct{ name, path string }{
		{"a file that is not a picture", "art/lies.png"},
		{"a file that is not there", "art/gone.png"},
	} {
		t.Run(c.name, func(t *testing.T) {
			agent := &fakeAgent{model: "m", turns: [][]session.Event{append(
				call("generate_image", `{"path":"`+c.path+`"}`,
					c.path+" — 17B, generated on paint/model"),
				session.Event{Kind: session.EventTurnDone})}}
			a := newTestApp(agent)
			a.pal = newPalette(tokens.TrueColor, false)
			a.workspace = dir
			runTurn(t, a, agent, "draw me a harbour")

			if hung := hungRowsAt(t, a, 60); len(hung) != 0 {
				t.Fatalf("the row grew %d lines:\n%s", len(hung), strings.Join(hung, "\n"))
			}
		})
	}
}

// ── a room's rows are the same rows ─────────────────────────────────────────

// A task room is drawn by this very code from its own list (room.go), and its
// entries come off a journal rather than off the event stream: the hint is
// never set, and the arguments and result are whatever the journal recorded.
// The thumbnail must not care.
func TestARoomJournalledPictureRowDrawsTheSameThumbnail(t *testing.T) {
	dir := t.TempDir()
	path := writePicture(t, dir, "book/cover.png", wideTestPicture())
	a := newTestApp(&fakeAgent{model: "m"})
	a.pal = newPalette(tokens.TrueColor, false)
	a.workspace = dir

	journaled := deck{
		entries: []entry{{
			kind: entryTool, tool: "generate_image", status: toolOK,
			detail: toolDetail{
				Args:   `{"prompt":"a book cover"}`,
				Output: path + " — 64×32 png, 1.2KB, generated on paint/model",
			},
		}},
		unfolded: map[int]bool{}, workOpen: map[int]bool{}, showsWork: true,
	}
	rows := a.toolRows(journaled, 0, true, 60)
	painted := 0
	for _, r := range rows[1:] {
		if strings.Contains(plain(r.text), halfBlock) {
			painted++
		}
	}
	if painted == 0 {
		t.Fatal("a room's finished picture row hung no thumbnail")
	}
	if painted > previewWindow {
		t.Fatalf("a room's thumbnail ran to %d rows", painted)
	}
}

// THE BUG A ROOM ACTUALLY HAD. A node runs in a worktree of its own and this
// surface is never told where that is, so a relative path off a room's row was
// joined onto THIS conversation's workspace — naming a file that is not there
// and, where the conversation happens to hold one of the same name, drawing
// somebody else's picture entirely. So the absolute answer wins over the
// relative one, whichever of the two carries it.
func TestAnAbsolutePathBeatsARelativeOne(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.workspace = "/tmp/lab"

	room := &entry{tool: "generate_image", detail: toolDetail{
		Args:   `{"path":"book/cover.jpg"}`,
		Output: "/var/nodes/n1/book/cover.jpg — 768×1376 jpeg, 776.9KB, generated on paint/model",
	}}
	if got, ok := a.picturePath(room); !ok || got != "/var/nodes/n1/book/cover.jpg" {
		t.Fatalf("a room's picture resolved to %q, %v — want the worktree's own file", got, ok)
	}

	// With nothing absolute anywhere the workspace is still the answer, which
	// is every ordinary conversation row.
	ordinary := &entry{tool: "generate_image", detail: toolDetail{
		Args:   `{"path":"book/cover.jpg"}`,
		Output: "book/cover.jpg — 768×1376 jpeg, 776.9KB, generated on paint/model",
	}}
	if got, ok := a.picturePath(ordinary); !ok || got != "/tmp/lab/book/cover.jpg" {
		t.Fatalf("a conversation's picture resolved to %q, %v", got, ok)
	}
}

// ── the words a terminal that cannot draw gets instead ──────────────────────

// A terminal below the xterm cube gets no picture, and then the path is the
// only thing that matters — so it goes down WHOLE. Every other block on this
// surface truncates to its width; a path with an ellipsis in it is a path
// nobody can open, and this is the one case where that is the whole answer.
func TestWhereNoPictureCanBeDrawnTheWholePathIsShownInstead(t *testing.T) {
	for _, tool := range []struct{ name, args, output string }{
		{"generate_image", `{"prompt":"a harbour"}`,
			".aforge-v3/images/harbour.png — 64×32 png, 1.2KB, generated on paint/model"},
		{"view_image", `{"path":".aforge-v3/images/harbour.png"}`,
			"seen by look/model: a harbour at dawn"},
	} {
		t.Run(tool.name, func(t *testing.T) {
			a, path := pictureRowApp(t, newPalette(tokens.ANSI16, false),
				".aforge-v3/images/harbour.png",
				call(tool.name, tool.args, tool.output))

			rows := openFirst(t, a)
			// Read back off the stem, because a temporary directory's name is
			// longer than the test frame: the path WRAPS onto as many rows as it
			// needs and every character of it survives. The result's own line
			// under it truncates like any other evidence, which is why this
			// looks for the path rather than for the absence of an ellipsis.
			if !strings.Contains(stemless(rows), path) {
				t.Fatalf("the whole path was not shown:\n%s", strings.Join(rows, "\n"))
			}
		})
	}
}

// And what the call actually said is kept alongside it — a look's answer is the
// only thing that call returned, and losing it to make room for a path would
// trade one missing half for another.
func TestTheFallbackKeepsWhatTheLookSaid(t *testing.T) {
	a, _ := pictureRowApp(t, newPalette(tokens.ANSI16, false),
		".aforge-v3/images/harbour.png",
		call("view_image", `{"path":".aforge-v3/images/harbour.png"}`,
			"seen by look/model: the mast leans a little to the left"))

	body := strings.Join(openFirst(t, a), "\n")
	if !strings.Contains(body, "the mast leans a little to the left") {
		t.Fatalf("the look's answer was lost:\n%s", body)
	}
}
