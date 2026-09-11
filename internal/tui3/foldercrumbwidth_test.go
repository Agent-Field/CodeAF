package tui3

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

func TestFolderBreadcrumbBudgetsEveryPaintedCell(t *testing.T) {
	paths := []string{
		"/tmp/aforge-ci/TestTheBrowserFitsANarrowFrame2140938909/001/here",
		"/one/two/" + strings.Repeat("oversized-leaf", 12),
		"/projects/日本語/設計資料/会話表示",
		"/projects/cafe\u0301/👩‍💻 work/東京",
		"/a space/ trailing space ",
		"/", "", "/a/b",
	}
	for pathAt, dir := range paths {
		for _, profile := range []tokens.Profile{tokens.NoColor, tokens.ANSI256} {
			t.Run(fmt.Sprintf("path%d/profile%d", pathAt, profile), func(t *testing.T) {
				f := &folderPick{}
				f.cols.dir = dir
				for width := 0; width <= 120; width++ {
					row := f.crumbRow(width, newPalette(profile, false))
					if cells := ansi.StringWidth(row); cells > width {
						t.Errorf("width %d paints %d cells: %q", width, cells, plain(row))
						continue
					}
					for at, crumb := range f.geom.crumbs {
						if crumb.span.from < 0 || crumb.span.to > width || crumb.span.to <= crumb.span.from {
							t.Fatalf("width %d has invalid clickable span: %+v", width, crumb)
						}
						paint := plain(ansi.Cut(row, crumb.span.from, crumb.span.to))
						if at < len(f.geom.crumbs)-1 && paint != crumb.name {
							t.Fatalf("width %d clipped ancestor %q to %q", width, crumb.name, paint)
						}
					}
					if dir != "" && width > folderPadCells && len(f.geom.crumbs) == 0 {
						t.Fatalf("width %d lost the current directory", width)
					}
					if len(f.geom.crumbs) > 0 {
						leaf := f.geom.crumbs[len(f.geom.crumbs)-1]
						if leaf.path != dir {
							t.Fatalf("display changed raw destination %q to %q", dir, leaf.path)
						}
						if width >= folderPadCells+ansi.StringWidth(folderCrumbCut)+ansi.StringWidth(leaf.name) && plain(ansi.Cut(row, leaf.span.from, leaf.span.to)) != leaf.name {
							t.Fatalf("width %d clipped leaf that fits: %q", width, plain(row))
						}
					}
				}
			})
		}
	}
}

func TestFolderBreadcrumbKeepsTheWholeTrailAtExactFit(t *testing.T) {
	f := &folderPick{}
	f.cols.dir, f.tilde = "/home/me/a/b", "/home/me"
	want := folderPad + "~" + folderCrumbGap + "a" + folderCrumbGap + "b"
	got := plain(f.crumbRow(ansi.StringWidth(want), newPalette(tokens.NoColor, false)))
	if got != want || len(f.geom.crumbs) != 3 {
		t.Fatalf("exact-fit trail = %q (%d destinations), want %q", got, len(f.geom.crumbs), want)
	}
}

func TestFolderBreadcrumbClickPreservesSpaceBearingDestination(t *testing.T) {
	for _, name := range []string{" folder with trailing space ", " " + strings.Repeat("長い名前", 16) + " "} {
		t.Run(name, func(t *testing.T) {
			a, _, root := browseLab(t)
			dir := filepath.Join(root, "here", name)
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			a.width = 84
			openBrowse(t, a, dir)
			y := chooserRowY(t, a, 0)
			crumbs := a.folder.geom.crumbs
			if len(crumbs) == 0 {
				t.Fatal("no breadcrumb destination")
			}
			leaf := crumbs[len(crumbs)-1]
			if ansi.StringWidth(name) > a.contextInner() && leaf.span.to-leaf.span.from >= ansi.StringWidth(name) {
				t.Fatal("oversized leaf did not exercise clipped click geometry")
			}
			drive(t, a, tea.MouseClickMsg{X: chooserX(t, a, leaf.span.from), Y: y, Button: tea.MouseLeft})
			if got := a.folder.cols.dir; got != dir {
				t.Fatalf("click changed filesystem destination: got %q, want %q", got, dir)
			}
		})
	}
}
