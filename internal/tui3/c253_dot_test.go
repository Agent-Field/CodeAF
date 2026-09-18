package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

func TestPlanProgressDotBoundaries(t *testing.T) {
	pal := palette{}
	cases := []struct {
		name  string
		row   session.PlanTaskRow
		width int
		want  string
	}{
		{"one has word only", session.PlanTaskRow{Status: "running", Running: 1, Total: 1}, 100, "running"},
		{"two direct cells", session.PlanTaskRow{Done: 1, Running: 1, Total: 2}, 100, pal.glyph(tokens.GDoneCell) + pal.glyph(tokens.GRunningCell) + "  1 of 2 · 1 running"},
		{"ten direct cells", session.PlanTaskRow{Done: 4, Running: 1, Queued: 5, Total: 10}, 70, strings.Repeat(pal.glyph(tokens.GDoneCell), 4) + pal.glyph(tokens.GRunningCell) + strings.Repeat(pal.glyph(tokens.GEmptyCell), 5) + "  4/10"},
		{"eleven compresses", session.PlanTaskRow{Done: 5, Running: 1, Queued: 5, Total: 11}, 70, strings.Repeat(pal.glyph(tokens.GDoneCell), 4) + pal.glyph(tokens.GRunningCell) + strings.Repeat(pal.glyph(tokens.GEmptyCell), 5) + "  5/11"},
		{"failure owns share", session.PlanTaskRow{Done: 8, Failed: 2, Total: 10}, 70, strings.Repeat(pal.glyph(tokens.GDoneCell), 8) + strings.Repeat(pal.glyph(tokens.GFailedCell), 2) + "  8/10"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := planProgress(tc.row, tc.width, pal); got != tc.want {
				t.Fatalf("planProgress() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestPlanProgressWidthTiersAndWording(t *testing.T) {
	pal := palette{}
	row := session.PlanTaskRow{Done: 6, Running: 2, Queued: 2, Total: 10}
	cases := []struct {
		width, cells int
		suffix       string
	}{{100, 10, "6 of 10 · 2 running"}, {70, 10, "6/10"}, {50, 5, "6/10"}, {30, 0, "6/10"}}
	for _, tc := range cases {
		got := planProgress(row, tc.width, pal)
		if strings.Contains(got, "\n") {
			t.Fatalf("width %d wrapped: %q", tc.width, got)
		}
		if !strings.HasSuffix(got, tc.suffix) {
			t.Errorf("width %d = %q, want suffix %q", tc.width, got, tc.suffix)
		}
		n := 0
		for _, id := range []tokens.GlyphID{tokens.GDoneCell, tokens.GRunningCell, tokens.GEmptyCell, tokens.GFailedCell} {
			n += strings.Count(got, pal.glyph(id))
		}
		if n != tc.cells {
			t.Errorf("width %d drew %d cells, want %d: %q", tc.width, n, tc.cells, got)
		}
	}
	done := planProgress(session.PlanTaskRow{Done: 10, Total: 10}, 100, pal)
	if done != "done" || strings.Contains(done, "10 of 10") {
		t.Fatalf("finished progress = %q", done)
	}
	failed := planProgress(session.PlanTaskRow{Done: 8, Failed: 2, Total: 10}, 100, pal)
	if failed != "8 of 10 · 2 failed" {
		t.Fatalf("failed progress = %q", failed)
	}
}
