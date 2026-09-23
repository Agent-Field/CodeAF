package tui3

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/session"
)

func wallMiniFixture() []session.DisplayEntry {
	return []session.DisplayEntry{
		{Role: "user", Text: "why is the fan loud? check what is eating cpu"},
		{Role: "tool", Tool: "bash", Hint: "ps -Ao pid,pcpu,comm -r | head -5"},
		{Role: "tool", Tool: "read", Hint: "internal/tui3/keeper.go"},
		{Role: "assistant", Text: "## The culprit\n\n`mds_stores` is at **180%**. It is Spotlight indexing:\n\n- started 4m ago\n- stops on its own\n\n```go\nfor _, p := range procs {\n\tif p.CPU > 100 {\n\t\tfmt.Println(p.Name)\n\t}\n}\n```\n"},
	}
}

func TestWallMiniDrawsTheChatsOwnPieces(t *testing.T) {
	a := newTestApp(nil)
	for _, width := range []int{20, 40, 56, 90} {
		rows := a.wallDraw(wallMiniFixture(), width)
		if len(rows) == 0 {
			t.Fatalf("width %d: no rows", width)
		}
		for i, r := range rows {
			if w := ansi.StringWidth(r); w > width {
				t.Errorf("width %d row %d is %d cells: %q", width, i, w, ansi.Strip(r))
			}
		}
		plain := ansi.Strip(strings.Join(rows, "\n"))
		for _, want := range []string{"› why is the fan", "bash", "The culprit", "mds_stores", "range procs"} {
			if width >= 56 && !strings.Contains(plain, want) {
				t.Errorf("width %d lacks %q\n%s", width, want, plain)
			}
		}
		if width == 56 {
			t.Logf("\n%s", strings.Join(rows, "\n"))
		}
	}
}

func TestWallMiniRowsAreKeptPerReading(t *testing.T) {
	a := newTestApp(nil)
	tail := &wallTail{recent: wallMiniFixture(), ver: 1}
	first := a.wallMiniRows(tail, 40)
	if &a.wallMiniRows(tail, 40)[0] != &first[0] {
		t.Fatal("a quiet frame redrew the tile")
	}
	tail.ver++
	if &a.wallMiniRows(tail, 40)[0] == &first[0] {
		t.Fatal("a new reading kept the old rows")
	}
}
