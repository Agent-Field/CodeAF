package rail

// Pointing at the map (5.15, 5.22 rule 5).
//
// The rail is a map, and the one thing a map has always afforded is putting a
// finger on a place. This file is the whole of what that costs: a table saying
// which model row each screen line was drawn for, written by the same loop that
// wrote the lines, read back by a coordinate.
//
// Two properties are worth stating because they are what the old surface got
// wrong (Part 2's anti-pattern 13/14: ~30 hand-maintained hit-test rectangles
// written during View()).
//
//   - The table is not a second description of the layout. It is produced by
//     [View.push] — the one appender — so it has exactly as many entries as
//     there are lines, always, including the lines the height budget dropped.
//     There is no arrangement of scope, fold and width under which it can
//     disagree with the picture, because it is not computed from the same
//     inputs; it is computed from the same ACT.
//   - Reading it changes nothing. [View.RowAt] and [View.ScopeUpAt] are pure
//     lookups against the last frame, so a pointer moving over the rail cannot
//     move the cursor, cannot commit, and cannot make the next frame differ
//     from the one the pointer was aimed at.

// The sentinel marks. Rows are their own non-negative model index, which is the
// index [Model.Select] takes, so nothing has to be translated on the way back.
const (
	// markChrome is a line that belongs to no row: the hairline, a fold line,
	// the scope header, and any padding.
	markChrome int32 = -1
	// markScopeUp is the way out of the scope, as a hover target. It is not a
	// mark any LINE carries — the header line is chrome, and the merged lead is
	// a column range inside row 0 — so it exists only in [View.SetHover].
	markScopeUp int32 = -2
	// markNoHover is "the pointer is not on this rail". Distinct from
	// markChrome so a pointer resting on a hairline still clears whatever row
	// was lit a moment ago.
	markNoHover int32 = -3
)

// SetHover lights the row a pointer is resting on, and reports whether the
// frame moved. It is the only mutating call in this file and it is deliberately
// not on the click path: nothing here selects, enters, or commits.
//
// A caller passes a model row index, [markScopeUp] for the way out, or
// [markNoHover] for nothing. Everything else clears.
func (v *View) SetHover(row int32) bool {
	if row < markNoHover {
		row = markNoHover
	}
	if v.hover == row {
		return false
	}
	v.hover = row
	return true
}

// HoverRow reports what is currently lit.
func (v *View) HoverRow() int32 { return v.hover }

// HoverAt resolves a pane-local cell to the hover mark it deserves: the way out
// where the ‹ was drawn, the row where a row was drawn, nothing anywhere else.
func (v *View) HoverAt(x, y int) int32 {
	if v.ScopeUpAt(x, y) {
		return markScopeUp
	}
	if row, ok := v.RowAt(y); ok {
		return int32(row)
	}
	return markNoHover
}

// RowAt answers which model row was drawn on a pane-local screen line.
//
// It reports false for chrome and for a line past the end of the frame, and a
// caller must treat that as "nothing here" rather than as row 0 — clicking the
// empty space under a short rail is not clicking the room at the top of it.
func (v *View) RowAt(y int) (int, bool) {
	if y < 0 || y >= len(v.marks) {
		return 0, false
	}
	mark := v.marks[y]
	if mark < 0 {
		return 0, false
	}
	return int(mark), true
}

// ScopeUpAt reports that a pane-local cell is the way out of the current scope:
// the whole scope-header line where the header has one of its own, and the ‹
// glyph alone where the header has been merged into row 0 (see renderMap).
//
// The distinction is not fussiness. On a merged row the rest of the line IS
// row 0, and a click that popped the scope from anywhere on it would take the
// reader out of the room whenever they aimed at the room's own name.
func (v *View) ScopeUpAt(x, y int) bool {
	if v.upLine < 0 || y != v.upLine {
		return false
	}
	return x >= v.upFrom && x < v.upTo
}

// Lines is how many lines the last frame drew. It bounds a hit test that wants
// to know whether a coordinate landed on the map at all.
func (v *View) Lines() int { return len(v.lines) }

// RowAt on the pane forwards to the View that drew the last frame, so a caller
// holding a [Pane] never has to reach past it.
func (p *Pane) RowAt(y int) (int, bool) {
	if p == nil || p.View == nil {
		return 0, false
	}
	return p.View.RowAt(y)
}

// ScopeUpAt on the pane forwards to the View.
func (p *Pane) ScopeUpAt(x, y int) bool {
	if p == nil || p.View == nil {
		return false
	}
	return p.View.ScopeUpAt(x, y)
}

// Hover lights whatever a pane-local cell is over, and reports whether the
// frame moved. Passing inside=false clears — that is the pointer leaving the
// rail, which nothing inside the rail could otherwise observe.
func (p *Pane) Hover(x, y int, inside bool) bool {
	if p == nil || p.View == nil {
		return false
	}
	if !inside {
		return p.View.SetHover(markNoHover)
	}
	return p.View.SetHover(p.View.HoverAt(x, y))
}
