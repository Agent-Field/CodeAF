package tui3

import (
	"fmt"
	"strings"
	"testing"
)

// Blank draft lines are still rows the caret can stand on. Check the rendered
// position, not only the editor offset, including the compact home frame.
func TestHomeShiftEnterHidesPlaceholderAndDrawsCaretOnNewLine(t *testing.T) {
	for _, width := range []int{40, 80, 200} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			_, a := drafting(t)
			a.width, a.height = width, 30
			a.showPage(pageHome)
			for count := 1; count <= 2; count++ {
				drive(t, a, key("shift+enter"))
				a.caret = true
				lines, _, x, y := a.homeFrame(width, a.height)
				first := -1
				for i, line := range lines {
					line = plain(line)
					if strings.Contains(line, "type to search") {
						t.Fatal("newline draft still shows the placeholder")
					}
					if strings.HasPrefix(line, " "+prompt) {
						first = i
					}
				}
				if first < 0 || y != first+count || x != 3 || !a.caret {
					t.Fatalf("%d newlines: caret (%d,%d), first row %d, visible=%v", count, x, y, first, a.caret)
				}
			}
			drive(t, a, key("backspace"), key("backspace"))
			if text := placeFrameText(a); !strings.Contains(text, "type to search") {
				t.Fatal("deleting the draft did not restore the placeholder")
			}
		})
	}
}

func TestHomeArrowsFollowTheMultilineDraftCaret(t *testing.T) {
	for _, width := range []int{40, 80, 200} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			_, a := drafting(t)
			a.width, a.height = width, 30
			a.showPage(pageHome)
			drive(t, a, key("shift+enter"), key("shift+enter"))
			for _, step := range []struct {
				key string
				at  int
			}{{"up", 1}, {"up", 0}, {"right", 1}, {"left", 0}, {"down", 1}, {"down", 2}} {
				drive(t, a, key(step.key))
				if got := a.home.box.cursor; got != step.at {
					t.Fatalf("%s moved caret to %d, want %d", step.key, got, step.at)
				}
			}
			a.home.box.setText("one\ntwo")
			drive(t, a, key("up"))
			if a.home.box.cursor != 3 {
				t.Fatalf("up did not keep the column: %d", a.home.box.cursor)
			}
			a.caret = true
			lines, _, x, y := a.homeFrame(width, a.height)
			if x != 6 || !strings.Contains(plain(lines[y]), "one") {
				t.Fatalf("caret (%d,%d) is not after the first line: %q", x, y, plain(lines[y]))
			}
			drive(t, a, key("down"), key("left"))
			if a.home.box.cursor != 6 {
				t.Fatalf("down/left missed the second line: %d", a.home.box.cursor)
			}
		})
	}
}

func TestHomeBlankDraftLinesCanBeClicked(t *testing.T) {
	_, a := drafting(t)
	a.showPage(pageHome)
	drive(t, a, key("shift+enter"), key("shift+enter"))
	placeFrameText(a)
	if !a.placeBoxPress(3, a.boxRow+1) || a.home.box.cursor != 1 {
		t.Fatalf("click on blank line did not place caret: %d", a.home.box.cursor)
	}
}
