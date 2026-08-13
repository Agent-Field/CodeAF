package exec

import (
	"bytes"
	"fmt"
)

// cappedOutput collects a subprocess's merged output in the shape the result is
// going to take anyway.
//
// The old path read the whole of it into memory and then threw all but twelve
// kilobytes away. `go test ./...` on a large tree, a verbose build, a `find /`
// that went wrong — each is hundreds of megabytes, held twice while the string
// conversion is made, inside a call that is allowed to run for fifteen minutes,
// once per concurrent leaf. Nothing downstream ever sees those bytes: clamp
// keeps the first two thirds of the limit and the last third, and says how many
// went missing in between.
//
// So this keeps exactly what clamp would keep — a head, a ring buffer holding
// the tail, and a count of everything that passed — and renders what clamp
// would have rendered from the whole. Memory is bounded at the limit itself no
// matter how much the command prints.
type cappedOutput struct {
	// limit and the two windows it splits into are this collector's own,
	// because the limit is the consuming leaf's — a model with a larger window
	// keeps proportionally more of what its commands printed. See toolBudgets.
	limit    int
	keepHead int
	keepTail int

	head  []byte
	tail  []byte // a ring: the last keepTail bytes, oldest at next
	next  int
	round bool
	total int

	// drop decides, from the beginning of a line, whether that line is one the
	// caller would have removed afterwards. It is how rtk's nudge stripping
	// survives being done a line at a time — see cappedLineDecision.
	drop func([]byte) bool

	pending  []byte
	state    lineState
	preceded bool
	finished bool
}

type lineState int

const (
	lineUndecided lineState = iota
	lineKept
	lineDropped
)

// cappedLineDecision is how much of a line is held while deciding whether to
// keep it. The only decision made here is rtk's, and rtk's is a prefix test on
// a short marker, so the beginning of a line settles it and a line of any
// length is passed through without being held.
const cappedLineDecision = 64

// newCappedOutput returns a collector bounded at limit, which must be the same
// limit the result will later be clamped at — the writer keeps exactly the two
// windows clamp keeps, and a mismatch would render something clamp would not
// have rendered. drop may be nil, which keeps every line — and keeping every
// line is byte-for-byte the same as not filtering at all, because the filter
// reassembles the lines it kept with the separators that were between them.
func newCappedOutput(drop func([]byte) bool, limit int) *cappedOutput {
	if limit <= 0 {
		limit = maxToolResultBytes
	}
	head := limit * 2 / 3
	tail := limit - head
	return &cappedOutput{limit: limit, keepHead: head, keepTail: tail,
		head: make([]byte, 0, head), tail: make([]byte, tail), drop: drop}
}

// Write takes the child's output as it arrives. One collector is used for both
// stdout and stderr — the same value, so os/exec gives the child a single pipe
// and the two streams merge in the child exactly as CombinedOutput merged them.
func (c *cappedOutput) Write(p []byte) (int, error) {
	written := len(p)
	if c.drop == nil {
		c.keep(p)
		return written, nil
	}
	for len(p) > 0 {
		piece := p
		terminated := false
		if index := bytes.IndexByte(p, '\n'); index >= 0 {
			piece, p, terminated = p[:index], p[index+1:], true
		} else {
			p = nil
		}
		c.line(piece)
		if terminated {
			c.endLine()
		}
	}
	return written, nil
}

// line takes one line's bytes, up to but not including its newline.
func (c *cappedOutput) line(piece []byte) {
	switch c.state {
	case lineDropped:
		return
	case lineKept:
		c.keep(piece)
		return
	}
	if room := cappedLineDecision - len(c.pending); room > 0 {
		if room > len(piece) {
			room = len(piece)
		}
		c.pending = append(c.pending, piece[:room]...)
		piece = piece[room:]
	}
	if len(c.pending) < cappedLineDecision {
		return
	}
	c.decide()
	if c.state == lineKept {
		c.keep(piece)
	}
}

// decide settles the line held in pending and, if it is kept, writes it out
// with the separator that belongs in front of it.
//
// The separator goes in *front* because that is what StripNudge does: it splits
// on newlines, drops lines, and joins the rest — so the newline that vanishes
// with a dropped last line is the one before it, not the one after.
func (c *cappedOutput) decide() {
	if c.drop(c.pending) {
		c.state = lineDropped
		c.pending = c.pending[:0]
		return
	}
	c.state = lineKept
	if c.preceded {
		c.keep([]byte{'\n'})
	}
	c.preceded = true
	c.keep(c.pending)
	c.pending = c.pending[:0]
}

func (c *cappedOutput) endLine() {
	if c.state == lineUndecided {
		c.decide()
	}
	c.state, c.pending = lineUndecided, c.pending[:0]
}

// finish settles a last line that never got its newline — a command whose
// output does not end in one, which includes the case where that line is the
// nudge and the newline before it goes with it.
func (c *cappedOutput) finish() {
	if c.finished {
		return
	}
	c.finished = true
	if c.drop != nil && c.state == lineUndecided {
		c.decide()
	}
}

// keep is the bounded part: the head until it is full, the tail always, and the
// count of everything either way.
func (c *cappedOutput) keep(b []byte) {
	c.total += len(b)
	if room := c.keepHead - len(c.head); room > 0 {
		if room > len(b) {
			room = len(b)
		}
		c.head = append(c.head, b[:room]...)
	}
	if len(b) >= c.keepTail {
		copy(c.tail, b[len(b)-c.keepTail:])
		c.next, c.round = 0, true
		return
	}
	n := copy(c.tail[c.next:], b)
	if n < len(b) {
		copy(c.tail, b[n:])
		c.next, c.round = len(b)-n, true
		return
	}
	if c.next += n; c.next == c.keepTail {
		c.next, c.round = 0, true
	}
}

func (c *cappedOutput) tailBytes() []byte {
	if !c.round {
		return c.tail[:c.next]
	}
	ordered := make([]byte, 0, c.keepTail)
	ordered = append(ordered, c.tail[c.next:]...)
	return append(ordered, c.tail[:c.next]...)
}

// String renders what clamp would have rendered from the whole output.
func (c *cappedOutput) String() string {
	c.finish()
	tail := c.tailBytes()
	if c.total <= c.limit {
		// Nothing was lost: the head and the ring overlap, and between them
		// they hold every byte that was written.
		return string(c.head) + string(tail[len(tail)-(c.total-len(c.head)):])
	}
	head := wholeRunesHead(string(c.head))
	kept := wholeRunesTail(string(tail))
	return head +
		fmt.Sprintf("\n\n... [%d bytes elided] ...\n\n", c.total-len(head)-len(kept)) +
		kept
}
