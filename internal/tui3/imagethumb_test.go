package tui3

// Image-tool path resolution and fallback words remain independent of the
// compact media controls tested in picturefold_test.go.

import (
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
