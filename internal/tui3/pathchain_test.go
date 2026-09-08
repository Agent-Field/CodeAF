package tui3

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/tui2/prose"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// WHETHER A PATH IS A LINK IS A FACT ABOUT THE PATH, NOT ABOUT THE WINDOW.
//
// The chain that puts a wrapped path back together was bounded by how many ROWS
// it spanned, and how many rows a path takes is a fact about the FRAME: the same
// name is three rows at 160 columns and ten at 28. So the bound was loosest
// exactly where paths are shortest, and tightest exactly where the narrow frame
// this pass exists for needed it most — a generated file a few directories deep
// was a link at 55 columns and not at 28, which is the opposite of the point.
//
// The assertion is the invariant rather than the mechanism: ONE path, EVERY
// width, always a link. A bound measured in rows cannot pass it at every width
// no matter what number is chosen, because there is always a narrower frame.
func TestWhetherAPathIsALinkDoesNotDependOnHowWideTheWindowIs(t *testing.T) {
	root := t.TempDir()
	// DEEP ENOUGH THAT THE ROW BOUND MUST FAIL, and deep by construction rather
	// than by luck. My first draft of this test used a handful of directories
	// and PASSED against the very row bound it names, because a short TMPDIR
	// left the path inside eight rows even at 24 columns — the same accident
	// that let the defect live. The name is built to exceed the old bound's
	// reach at the narrowest frame a terminal is allowed to be, whatever
	// directory the test happens to be given.
	deep := root
	for _, segment := range []string{
		"internal", "sessions", "workspaces", "generated",
		"chapters", "drafts", "revisions", "attachments", "rendered",
	} {
		deep = filepath.Join(deep, segment)
	}
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	name := filepath.Join(deep, "a-very-long-generated-file-name.md")
	if err := os.WriteFile(name, []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	l := linker{pal: newPalette(tokens.TrueColor, false), on: true, root: root, home: root, seen: map[string]string{}}
	st := tokens.NewStyler(tokens.TrueColor, tokens.FocusNormal)

	// 24 is the floor a terminal is allowed to be; 200 is more room than anyone
	// gives it. The path is the same on every one of them.
	for _, width := range []int{24, 28, 33, 40, 55, 80, 120, 200} {
		rows := prose.Render("Saved to "+name+" just now.",
			prose.Options{Width: width, Measure: prose.DefaultMeasure, Styler: st})
		joined := strings.Join(l.rows(rows), "\n")
		if !strings.Contains(joined, "\x1b]8;;file://") {
			t.Fatalf("at %d columns the path stopped being a link, though it is a link at every other width — "+
				"whether a name can be clicked is not supposed to depend on the size of the window:\n%s",
				width, joined)
		}
	}
}
