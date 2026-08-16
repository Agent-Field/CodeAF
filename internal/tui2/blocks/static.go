package blocks

// Static is a finalized block of fixed rows: separators, hairlines, receipts
// already rendered elsewhere, and the golden harness's fixtures. It is the
// smallest possible honest [Block].
type Static struct {
	id      string
	lines   []string
	rows    []string
	width   int
	end     EndState
	version uint64
}

var _ Block = (*Static)(nil)

// NewStatic returns a finalized block of the given logical rows. Each row is
// flattened and cut to the render width; a row is one screen row, always.
func NewStatic(id string, rows ...string) *Static {
	return &Static{id: id, lines: rows, end: EndCompleted, width: -1}
}

// ID is the block's identity.
func (s *Static) ID() string { return s.id }

// IsFinalized is always true: a static block has nothing left to do.
func (s *Static) IsFinalized() bool { return true }

// SettledRows is every row.
func (s *Static) SettledRows(width int) int { return len(s.Rows(width)) }

// Version increments when the rows are replaced.
func (s *Static) Version() uint64 { return s.version }

// End is how the block ended.
func (s *Static) End() EndState { return s.end }

// SetEnd marks how the block ended and bumps the version.
func (s *Static) SetEnd(end EndState) {
	s.end, s.width = end, -1
	s.version++
}

// SetRows replaces the rows and bumps the version — the only coherent way to
// change committed bytes (8.1.1).
func (s *Static) SetRows(rows ...string) {
	s.lines, s.width = rows, -1
	s.version++
}

// Rows renders the block at width.
func (s *Static) Rows(width int) []string {
	if width < 1 {
		width = 1
	}
	if s.width == width {
		return s.rows
	}
	s.rows = s.rows[:0]
	for _, line := range s.lines {
		s.rows = append(s.rows, truncate(flatten(line), width))
	}
	if rule := CutRule(s.end, width, Plain); rule != "" {
		s.rows = append(s.rows, rule)
	}
	s.width = width
	return s.rows
}
