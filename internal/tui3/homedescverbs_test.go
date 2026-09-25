package tui3

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestHomeOptionsAppearBelowTheDescriptionWithoutMovingTheList(t *testing.T) {
	for _, width := range []int{170, 180, 240} {
		t.Run(itoa(width), func(t *testing.T) {
			a := newSwitchLab(t).open(width, 35)
			for at, line := range a.home.lines {
				if line.kind == homeSession && line.row.Transcript == a.file {
					a.home.cursor = at
					break
				}
			}
			before := strings.Split(homeText(a), "\n")
			xs, widths := homeGridGeometry(width, 3)
			drive(t, a, key("right"))
			after := strings.Split(homeText(a), "\n")
			if !a.strip.open || len(before) != len(after) {
				t.Fatal("opening options changed the frame height or did not activate")
			}
			for y := placeHeadRows; y < placeHeadRows+a.home.room; y++ {
				for _, col := range []int{0, 2} {
					if ansi.Cut(before[y], xs[col], xs[col]+widths[col]) != ansi.Cut(after[y], xs[col], xs[col]+widths[col]) {
						t.Fatalf("options moved column %d on row %d", col, y)
					}
				}
			}
			first := len(after)
			for _, option := range []string{"x close", "c copy name", "n new in project", "o open folder"} {
				row, col := homeRowOf(strings.Join(after, "\n"), option)
				if row < 0 || col < xs[1] || col+len(option) > xs[1]+widths[1] {
					t.Fatalf("%q is outside the description column:\n%s", option, strings.Join(after, "\n"))
				}
				first = min(first, row)
			}
			foundDescription := false
			for y := placeHeadRows; y < first; y++ {
				if strings.TrimSpace(ansi.Cut(after[y], xs[1], xs[1]+widths[1])) != "" {
					foundDescription = true
				}
			}
			if !foundDescription {
				t.Fatal("options did not follow the selected description")
			}
			drive(t, a, key("left"))
			if strings.Contains(homeText(a), "x close") {
				t.Fatal("closing options left their shortcuts visible")
			}
		})
	}
}

func TestHomeDescriptionOptionsStayVisibleWithLongProseAndResize(t *testing.T) {
	a := newSwitchLab(t).open(180, 24)
	for at, line := range a.home.lines {
		if line.kind == homeSession && line.row.Transcript == a.file {
			a.home.cursor = at
			line.cell.sub = strings.Repeat("A long description of the selected conversation. ", 50)
			break
		}
	}
	drive(t, a, key("right"))
	for _, width := range []int{180, 120, 180} {
		a.width = width
		frame := homeText(a)
		for _, option := range []string{"x close", "c copy name", "n new in project", "o open folder"} {
			if strings.Count(frame, option) != 1 {
				t.Fatalf("at width %d, %q must appear exactly once:\n%s", width, option, frame)
			}
		}
		for _, row := range strings.Split(frame, "\n") {
			if ansi.StringWidth(row) > width {
				t.Fatalf("options overflowed a %d-cell frame", width)
			}
		}
	}
}
