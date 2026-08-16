package composer

import "testing"

func TestEdit_MoveLeftRightClampAtBounds(t *testing.T) {
	m := New(Options{})
	typeString(m, "ab")
	m.moveRight() // already at end: no-op
	if m.cursor != 2 {
		t.Fatalf("cursor = %d, want 2 (clamped at end)", m.cursor)
	}
	m.cursor = 0
	m.moveLeft() // already at start: no-op
	if m.cursor != 0 {
		t.Fatalf("cursor = %d, want 0 (clamped at start)", m.cursor)
	}
	m.moveRight()
	if m.cursor != 1 {
		t.Fatalf("cursor = %d, want 1", m.cursor)
	}
}

func TestEdit_DeleteAtBoundsIsNoop(t *testing.T) {
	m := New(Options{})
	m.deleteBackward() // empty draft: no-op, must not panic
	m.deleteForward()  // empty draft: no-op, must not panic
	if m.Value() != "" {
		t.Fatalf("Value() = %q, want empty", m.Value())
	}
	typeString(m, "a")
	m.cursor = 0
	m.deleteBackward() // at start: no-op
	if m.Value() != "a" {
		t.Fatalf("Value() = %q, want %q (backspace at start is a no-op)", m.Value(), "a")
	}
	m.cursor = 1
	m.deleteForward() // at end: no-op
	if m.Value() != "a" {
		t.Fatalf("Value() = %q, want %q (delete at end is a no-op)", m.Value(), "a")
	}
}

func TestEdit_MoveDownWalksLogicalLinesAndStopsAtLast(t *testing.T) {
	m := New(Options{NewlineKeys: []string{"alt+enter"}})
	typeString(m, "aa")
	m.Key(altEnterKey())
	typeString(m, "bbbb")
	m.cursor = 1 // column 1 on the first line ("a|a")
	if !m.moveDown() {
		t.Fatalf("moveDown() = false, want true (a second line exists)")
	}
	if m.cursor != 3+1 { // "aa\n" is 3 runes, plus column 1
		t.Fatalf("cursor = %d, want 4", m.cursor)
	}
	if m.moveDown() {
		t.Fatalf("moveDown() = true on the last line, want false")
	}
}

func TestEdit_MoveDownClampsShorterColumn(t *testing.T) {
	m := New(Options{NewlineKeys: []string{"alt+enter"}})
	typeString(m, "abcdef")
	m.Key(altEnterKey())
	typeString(m, "xy")
	m.cursor = 5 // column 5 on the long first line
	m.moveDown()
	// Second line "xy" is only 2 runes long: cursor clamps to its end.
	if got := string(m.value[m.cursor:]); got != "" {
		t.Fatalf("cursor not clamped to end of shorter line: rest = %q", got)
	}
}

func TestEdit_ResetClearsWithoutTouchingHistory(t *testing.T) {
	m := New(Options{OnSubmit: func(string) {}})
	typeString(m, "sent")
	m.Key(enterKey())
	typeString(m, "unsent")
	m.reset()
	if m.Value() != "" {
		t.Fatalf("Value() = %q, want empty after reset", m.Value())
	}
	if len(m.history) != 1 || m.history[0] != "sent" {
		t.Fatalf("history = %v, want reset to leave it untouched", m.history)
	}
}
