package blocks

import (
	"strconv"
	"strings"
	"testing"
	"time"
)

var base = time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)

// fixed is a test block whose rows, version and finalization are all under the
// test's thumb, including the ability to lie about them.
type fixed struct {
	id      string
	rows    []string
	final   bool
	version uint64
	end     EndState
	renders int
	out     []string
}

func newFixed(id string, height int) *fixed {
	f := &fixed{id: id, final: true, end: EndCompleted}
	for i := 0; i < height; i++ {
		f.rows = append(f.rows, id+"-"+strconv.Itoa(i))
	}
	return f
}

func (f *fixed) ID() string        { return f.id }
func (f *fixed) IsFinalized() bool { return f.final }
func (f *fixed) Version() uint64   { return f.version }
func (f *fixed) End() EndState     { return f.end }

func (f *fixed) SettledRows(int) int {
	if f.final {
		return len(f.rows)
	}
	return max(0, len(f.rows)-1)
}

func (f *fixed) Rows(width int) []string {
	f.renders++
	f.out = f.out[:0]
	for _, r := range f.rows {
		f.out = append(f.out, truncate(r, width))
	}
	return f.out
}

// setRows changes the block honestly.
func (f *fixed) setRows(rows ...string) {
	f.rows = rows
	f.version++
}

// lie changes the block's bytes without bumping the version — the contract
// violation [Transcript.Strict] exists to catch.
func (f *fixed) lie(rows ...string) { f.rows = rows }

// spin is a live block that draws the shared clock's spinner, so its rows are a
// pure function of the animation frame.
type spin struct {
	id   string
	c    *Clock
	n    int
	rows []string
}

func (s *spin) ID() string          { return s.id }
func (s *spin) IsFinalized() bool   { return false }
func (s *spin) Version() uint64     { return 0 }
func (s *spin) End() EndState       { return EndLive }
func (s *spin) SettledRows(int) int { return 0 }

func (s *spin) Rows(width int) []string {
	s.rows = s.rows[:0]
	for i := 0; i < max(1, s.n); i++ {
		s.rows = append(s.rows, truncate(s.c.Glyph()+" working "+strconv.Itoa(i), width))
	}
	return s.rows
}

// fill builds a transcript of n finalized blocks, each `height` rows tall.
func fill(t *testing.T, n, height, width, view int) (*Transcript, []*fixed) {
	t.Helper()
	tr := New(width, view)
	blocks := make([]*fixed, 0, n)
	for i := 0; i < n; i++ {
		f := newFixed("b"+strconv.Itoa(i), height)
		blocks = append(blocks, f)
		tr.Append(f)
	}
	return tr, blocks
}

func rowsOf(f Frame) string { return strings.Join(f.Rows, "\n") }

// allocs reports allocations per call, for the no-churn assertions.
func allocs(f func()) float64 { return testing.AllocsPerRun(50, f) }
