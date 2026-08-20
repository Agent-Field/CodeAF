package tui3

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/tui2/prose"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// A workspace with one real file in it, so that the honesty rule has something
// to say yes to.
func linkFixture(t *testing.T) (root string, l linker) {
	t.Helper()
	root = t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "internal", "tui3"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "internal", "tui3", "pathlink.go"), []byte("package tui3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pal := newPalette(tokens.TrueColor, false)
	return root, linker{pal: pal, on: true, root: root, home: root, seen: map[string]string{}}
}

// The sequence itself. It is asserted on the bytes rather than through a
// helper, because the whole feature is those bytes reaching a terminal.
func TestPathLinkIsAnOSC8Anchor(t *testing.T) {
	root, l := linkFixture(t)
	target := filepath.Join(root, "README.md")
	out := l.anchor("README.md", target)
	if !strings.HasPrefix(out, "\x1b]8;;file://"+target) {
		t.Fatalf("no OSC 8 opener on %q", out)
	}
	if !strings.HasSuffix(out, "\x1b]8;;\x07") {
		t.Fatalf("no OSC 8 terminator on %q", out)
	}
	if !strings.Contains(out, sgrUnderOn) || !strings.Contains(out, sgrUnderOff) {
		t.Fatalf("the affordance is missing from %q", out)
	}
	if ansi.Strip(out) != "README.md" {
		t.Fatalf("the visible text changed: %q", ansi.Strip(out))
	}
}

// The affordance is SGR and the profile owns SGR: a terminal that draws none
// still gets the link, because a link is not a colour.
func TestPathLinkKeepsTheLinkWithoutTheUnderline(t *testing.T) {
	root, l := linkFixture(t)
	l.pal = newPalette(tokens.NoColor, true)
	out := l.anchor("README.md", filepath.Join(root, "README.md"))
	if strings.Contains(out, sgrUnderOn) {
		t.Fatalf("a colourless terminal was sent an underline: %q", out)
	}
	if !strings.Contains(out, "\x1b]8;;file://") {
		t.Fatalf("the link was dropped with the colour: %q", out)
	}
}

// THE HONESTY RULE. Everything in this table has the shape of a path and only
// one of the rows is a file.
func TestOnlyAPathThatExistsBecomesALink(t *testing.T) {
	root, l := linkFixture(t)
	for _, tc := range []struct {
		word string
		want bool
	}{
		{"internal/tui3/pathlink.go", true},
		{"README.md", true},
		{root + "/README.md", true},
		{"~/README.md", true},
		{"(internal/tui3/pathlink.go)", true},
		{"internal/tui3/pathlink.go:12:4", true},
		{"`README.md`", true},
		{"file://" + root + "/README.md", true},

		{"internal/tui3/nothing.go", false},
		{"a/internal/tui3/pathlink.go", false}, // a diff's left side
		{"b/internal/tui3/pathlink.go", false}, // and its right
		{"v2.0", false},
		{"1/2", false},
		{"4:3", false},
		{"github.com/Agent-Field/aforge-v2", false},
		{"https://example.com/README.md", false},
		{"../../../etc/passwd", false}, // climbs out of the workspace
		{"", false},
	} {
		_, _, _, ok := l.path(tc.word)
		if ok != tc.want {
			t.Errorf("path(%q) linked = %v, want %v", tc.word, ok, tc.want)
		}
	}
}

// Emphasis markers are gone by the time a row reaches this pass, so a `*` or a
// `_` on a rendered row is a character of the name. Two language communities
// write filenames that would not survive peeling them.
func TestUnderscoresAreNotPunctuation(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"__init__.py", "_test.go", "a_b.txt"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	l := linker{pal: newPalette(tokens.TrueColor, false), on: true, root: root, home: root, seen: map[string]string{}}
	for _, name := range []string{"__init__.py", "_test.go", "a_b.txt"} {
		if _, _, _, ok := l.path(name); !ok {
			t.Errorf("%q did not resolve", name)
		}
	}
}

// A line and column are part of the word a person aims at and no part of the
// file that opens.
func TestALineNumberStaysInTheAnchorAndOutOfTheTarget(t *testing.T) {
	root, l := linkFixture(t)
	from, to, target, ok := l.path("internal/tui3/pathlink.go:12:4")
	if !ok {
		t.Fatal("a compiler's own spelling of a file did not resolve")
	}
	if got := "internal/tui3/pathlink.go:12:4"[from:to]; got != "internal/tui3/pathlink.go:12:4" {
		t.Fatalf("the anchor lost the position: %q", got)
	}
	if want := filepath.Join(root, "internal", "tui3", "pathlink.go"); target != want {
		t.Fatalf("target = %q, want %q", target, want)
	}
}

// ZERO CELLS. Everything on this surface measures with ansi.StringWidth, and a
// link that measured as anything at all would push the row it is on sideways.
func TestALinkedPathOccupiesNoExtraCells(t *testing.T) {
	root, l := linkFixture(t)
	plain := "README.md"
	linked := l.anchor(plain, filepath.Join(root, "README.md"))
	if got, want := ansi.StringWidth(linked), ansi.StringWidth(plain); got != want {
		t.Fatalf("width %d, want %d", got, want)
	}
}

// A terminal that made no capability claim is sent none of this.
func TestALinkIsNotWrittenToATerminalThatNeverSaidWhatItIs(t *testing.T) {
	for _, term := range []string{"", "dumb", "DUMB", "  "} {
		if terminalTakesLinks(func(string) string { return term }) {
			t.Errorf("TERM=%q was offered a hyperlink", term)
		}
	}
	for _, term := range []string{"xterm-256color", "xterm-ghostty", "screen-256color"} {
		if !terminalTakesLinks(func(string) string { return term }) {
			t.Errorf("TERM=%q was refused a hyperlink", term)
		}
	}
	root, l := linkFixture(t)
	l.on = false
	target := filepath.Join(root, "README.md")
	if out := l.anchor("README.md", target); out != "README.md" {
		t.Fatalf("a link was written anyway: %q", out)
	}
	rows := []string{"see internal/tui3/pathlink.go for it"}
	if out := l.rows(rows); out[0] != rows[0] {
		t.Fatalf("the row pass wrote to a terminal that takes no links: %q", out[0])
	}
}

// The pass over rendered rows: prose's own bytes in, the same bytes with an
// anchor around the one word that is a file.
func TestTheRowPassLinksOnlyTheFile(t *testing.T) {
	root, l := linkFixture(t)
	st := tokens.NewStyler(tokens.TrueColor, tokens.FocusNormal)
	rows := prose.Render("I put the guard in `internal/tui3/pathlink.go` and left v2.0 alone.",
		prose.Options{Width: 80, Measure: prose.DefaultMeasure, Styler: st})
	out := l.rows(rows)
	joined := strings.Join(out, "\n")
	if opens := strings.Count(joined, "\x1b]8;;file://"); opens != 1 {
		t.Fatalf("%d links, want 1: %q", opens, joined)
	}
	if !strings.Contains(joined, "\x1b]8;;file://"+filepath.Join(root, "internal", "tui3", "pathlink.go")) {
		t.Fatalf("the link does not point at the file: %q", joined)
	}
	// THE VISIBLE TEXT IS UNTOUCHED, byte for byte. A pass that reworded a
	// sentence to link it would be a pass rewriting the model's answer.
	if got, want := ansi.Strip(strings.Join(out, "\n")), ansi.Strip(strings.Join(rows, "\n")); got != want {
		t.Fatalf("the sentence changed:\n got %q\nwant %q", got, want)
	}
}

// THE DEFECT THIS FEATURE EXISTS FOR. A path wider than the pane is broken
// across rows by prose, and every fragment of it has to open the same file.
func TestALongPathWrappedAcrossRowsIsOneLink(t *testing.T) {
	root := t.TempDir()
	deep := filepath.Join(root, "internal", "sessions", "workspaces", "generated", "chapters")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	name := filepath.Join(deep, "a-very-long-generated-file-name.md")
	if err := os.WriteFile(name, []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pal := newPalette(tokens.TrueColor, false)
	l := linker{pal: pal, on: true, root: root, home: root, seen: map[string]string{}}

	st := tokens.NewStyler(tokens.TrueColor, tokens.FocusNormal)
	for _, width := range []int{28, 33, 40, 55} {
		rows := prose.Render("Saved to "+name+" just now.",
			prose.Options{Width: width, Measure: prose.DefaultMeasure, Styler: st})
		out := l.rows(rows)
		joined := strings.Join(out, "\n")
		opens := strings.Count(joined, "\x1b]8;;file://")
		closes := strings.Count(joined, "\x1b]8;;\x07")
		if opens == 0 {
			t.Fatalf("width %d: the wrapped path was not linked: %q", width, joined)
		}
		// EVERY ANCHOR IS CLOSED ON THE ROW IT OPENED ON. A link carried across a
		// row break would swallow the padding, the indent and the viewport's own
		// bytes; re-opening on each visual line is what a terminal joins back up.
		if opens != closes {
			t.Fatalf("width %d: %d openers, %d terminators: %q", width, opens, closes, joined)
		}
		for _, row := range out {
			if strings.Count(row, "\x1b]8;;file://") != strings.Count(row, "\x1b]8;;\x07") {
				t.Fatalf("width %d: a row carries an unbalanced anchor: %q", width, row)
			}
		}
		// EVERY ANCHOR CARRIES THE WHOLE FILE. A sequence cut mid-payload, or a
		// fragment linked to the directory above it, is the corruption this pass
		// must never produce — so the count of openers naming the file has to be
		// the count of openers.
		if strings.Count(joined, "\x1b]8;;"+fileURI(name)) != opens {
			t.Fatalf("width %d: a link points somewhere other than the file: %q", width, joined)
		}
		if got, want := ansi.Strip(strings.Join(out, "\n")), ansi.Strip(strings.Join(rows, "\n")); got != want {
			t.Fatalf("width %d: the wrap changed:\n got %q\nwant %q", width, got, want)
		}
		// And the rows still measure what they measured.
		for i := range out {
			if ansi.StringWidth(out[i]) != ansi.StringWidth(rows[i]) {
				t.Fatalf("width %d row %d: the link took cells", width, i)
			}
		}
	}
}

// The task-reference pass runs after this one, over rows that now carry OSC 8.
// It reads a row by walking its escapes, so an OSC it did not understand would
// put every task link on the wrong column.
func TestTheTaskPassStillReadsARowThatCarriesALink(t *testing.T) {
	root, l := linkFixture(t)
	pal := newPalette(tokens.TrueColor, false)
	row := "I read internal/tui3/pathlink.go for task 7."
	linked := l.rows([]string{row})[0]
	if !strings.Contains(linked, "\x1b]8;;file://"+root) {
		t.Fatalf("the path was not linked: %q", linked)
	}
	out, links := linkifyTasks(linked, pal, func(uint64) (string, bool) { return "a title", true }, -1)
	if len(links) != 1 {
		t.Fatalf("%d task links, want 1: %q", len(links), out)
	}
	if ansi.Strip(out) != row {
		t.Fatalf("the row was corrupted: %q", ansi.Strip(out))
	}
	// The columns the task link claims are the columns the words are actually on.
	plain := ansi.Strip(out)
	if got := plain[links[0].span.from:links[0].span.to]; got != "task 7" {
		t.Fatalf("the task link landed on %q", got)
	}
}

// flatten and the splice both walk escapes, and neither may take an OSC apart.
func TestFlattenSkipsAHyperlinkWhole(t *testing.T) {
	painted := "before \x1b]8;;file:///tmp/x.go\x07\x1b[4mx.go\x1b[24m\x1b]8;;\x07 after"
	flat, ground := flatten(painted)
	if flat != "before x.go after" {
		t.Fatalf("flat = %q", flat)
	}
	if len(ground) != len(flat) {
		t.Fatalf("ground is %d long for %d bytes", len(ground), len(flat))
	}
}

// END TO END, through the frame a person is actually looking at: the model's
// prose, aforge's own note, and a tool row's target all carry a link, and the
// tool row's does so even though the row was too narrow to show the path.
func TestTheFrameLinksProseNotesAndToolTargets(t *testing.T) {
	a, _, dir := attachLab(t, map[string]int{"internal/tui3/pathlink.go": 8})
	// See the note in the attach test: TERM belongs to whoever ran the tests.
	a.pathLinks = true
	target := filepath.Join(dir, "internal", "tui3", "pathlink.go")

	a.entries = []entry{
		{kind: entryAssistant, text: "I put the guard in `internal/tui3/pathlink.go`.", settled: true},
		{kind: entryNote, text: "exported · " + target},
		{kind: entryTool, tool: "read", text: "internal/tui3/pathlink.go",
			detail: toolDetail{Args: `{"path":"internal/tui3/pathlink.go"}`}},
	}
	a.touch()
	body := frame(a)
	if got := strings.Count(body, "\x1b]8;;"+fileURI(target)); got < 3 {
		t.Fatalf("%d of the three surfaces linked the file:\n%q", got, body)
	}
	// AND THE READER SEES THE SAME WORDS. Nothing on screen moved.
	a.pathLinks = false
	a.touch()
	if got, want := ansi.Strip(body), ansi.Strip(frame(a)); got != want {
		t.Fatalf("the frame changed shape:\n got %q\nwant %q", got, want)
	}
}

// THE TWO PLACES THE PATHS ARE NOT THIS MACHINE'S. Over a connection they are
// on the other end of it; on a task's page they are in that node's own worktree.
// Both would resolve here against the wrong tree, so neither gets a link.
func TestNoLinksWhereThePathsBelongToAnotherTree(t *testing.T) {
	a, _, _ := attachLab(t, nil)
	a.pathLinks = true
	if !a.linker().on {
		t.Fatal("an ordinary local conversation was refused links")
	}
	a.room = &taskRoom{}
	if a.linker().on {
		t.Fatal("a task's page linked a path against this session's workspace")
	}
	a.room = nil

	hosted := newApp(context.Background(), Options{Agent: &fakeAgent{}, Host: "devbox", Workspace: "/srv/app"})
	if hosted.pathLinks {
		t.Fatal("a hosted session linked the other machine's files against this one")
	}
}

// The sentence a reader sees never changes, and neither does what they copy:
// copy mode and /export both strip escapes, so a linked path leaves this
// surface as the plain path it always was.
func TestALinkedRowStripsBackToWhatItSays(t *testing.T) {
	_, l := linkFixture(t)
	row := "· exported · internal/tui3/pathlink.go"
	if got := ansi.Strip(l.rows([]string{row})[0]); got != row {
		t.Fatalf("stripped to %q, want %q", got, row)
	}
}
