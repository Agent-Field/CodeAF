package exec

import (
	"bytes"
	"io"
)

// cappedOutput collects a subprocess's merged output in the shape the result is
// going to take anyway.
//
// The old path read the whole of it into memory and then threw all but twelve
// kilobytes away. `go test ./...` on a large tree, a verbose build, a `find /`
// that went wrong — each is hundreds of megabytes, held twice while the string
// conversion is made, inside a call that is allowed to run for fifteen minutes,
// once per concurrent leaf. Nothing downstream ever sees those bytes.
//
// So this keeps exactly what the result bound keeps — a ring holding the tail,
// the counts a truncation notice has to state, and a file on the side holding
// everything — and renders what boundResult would have rendered from the whole.
// Memory is bounded at the carry limit no matter how much the command prints.
//
// The tail is what is kept because this is COMMAND output, and a command's most
// valuable line is its last: the compiler's error, the test summary, the
// traceback, the exit reason. The head is not lost, it is moved — the moment
// the output outgrows what can be carried, the collector opens the spill file
// and everything goes there, so the notice can hand the model the exact command
// that reads back the part that was cut.
type cappedOutput struct {
	// carry is the largest output that is worth putting in context whole, and
	// keep is what survives when it is not. Both are the consuming leaf's, from
	// toolBudgets: a model with a larger working set keeps proportionally more
	// of what its commands printed.
	carry int
	keep  int

	ring  []byte // the last carry bytes, oldest at next
	next  int
	round bool
	total int

	newlines    int
	endsNewline bool

	// The full output, teed as it arrives. open is called at most once, on the
	// first byte past carry — a command whose output fits needs no file, and
	// most commands' output fits.
	open      func() (io.WriteCloser, string, bool)
	file      io.WriteCloser
	spillPath string
	written   int
	partial   bool
	teeFailed bool

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

// spillFileMultiple bounds the file the whole output is teed to, as a multiple
// of what the leaf could have carried.
//
// Disk is not context, and the file exists precisely so that truncation is
// recoverable rather than lossy, so the multiple is generous — two megabytes of
// build log on a 200k-token leaf. It is not unbounded because a command may
// print at line rate for two minutes, and a workspace that filled a disk would
// be a worse failure than a log that stops. When it stops, the notice says so
// and the file still holds the beginning, which is the half the context does
// not have.
const spillFileMultiple = 64

// newCappedOutput returns a collector bounded at carry, which must be the same
// carry the result bound uses — the writer keeps exactly what boundResult
// keeps, and a mismatch would render something boundResult would not have
// rendered. keep is what survives a truncation and must be smaller than carry,
// so that the whole of the kept region is inside the ring. drop may be nil,
// which keeps every line — and keeping every line is byte-for-byte the same as
// not filtering at all, because the filter reassembles the lines it kept with
// the separators that were between them. open may be nil, which is a collector
// with nowhere to spill: it still bounds memory, and its notice says the rest
// was not saved.
func newCappedOutput(drop func([]byte) bool, carry, keep int, open func() (io.WriteCloser, string, bool)) *cappedOutput {
	if carry <= 0 {
		carry = maxToolResultBytes
	}
	if keep <= 0 || keep >= carry {
		keep = carry / 2
	}
	return &cappedOutput{carry: carry, keep: keep, ring: make([]byte, carry), drop: drop, open: open}
}

// Write takes the child's output as it arrives. One collector is used for both
// stdout and stderr — the same value, so os/exec gives the child a single pipe
// and the two streams merge in the child exactly as CombinedOutput merged them.
func (c *cappedOutput) Write(p []byte) (int, error) {
	written := len(p)
	if c.drop == nil {
		c.keepBytes(p)
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
		c.keepBytes(piece)
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
		c.keepBytes(piece)
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
		c.keepBytes([]byte{'\n'})
	}
	c.preceded = true
	c.keepBytes(c.pending)
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
// nudge and the newline before it goes with it — and closes the spill file.
func (c *cappedOutput) finish() {
	if c.finished {
		return
	}
	c.finished = true
	if c.drop != nil && c.state == lineUndecided {
		c.decide()
	}
	if c.file != nil {
		_ = c.file.Close()
		c.file = nil
	}
}

// keepBytes is the bounded part: the file first, then the ring, then the counts
// a notice is written from.
func (c *cappedOutput) keepBytes(b []byte) {
	if len(b) == 0 {
		return
	}
	c.tee(b)
	c.total += len(b)
	c.newlines += bytes.Count(b, []byte{'\n'})
	c.endsNewline = b[len(b)-1] == '\n'
	if len(b) >= c.carry {
		copy(c.ring, b[len(b)-c.carry:])
		c.next, c.round = 0, true
		return
	}
	n := copy(c.ring[c.next:], b)
	if n < len(b) {
		copy(c.ring, b[n:])
		c.next, c.round = len(b)-n, true
		return
	}
	if c.next += n; c.next == c.carry {
		c.next, c.round = 0, true
	}
}

// tee sends everything to the spill file, opening it on the first byte that
// will not fit in the ring. Until that moment the ring holds the whole output,
// so nothing has been lost and no file is needed; at that moment the ring is
// flushed to the file and every later byte goes straight through.
func (c *cappedOutput) tee(b []byte) {
	if c.open == nil || c.teeFailed || c.finished {
		return
	}
	if c.file == nil {
		// Either cap crossing is a reason to start the file, because either one
		// means something will be cut. The line cap matters here on its own: a
		// thin four-thousand-line listing is nowhere near the byte limit and is
		// still going to lose most of itself, and a notice that could not name a
		// file would be telling the model to run the command again.
		if c.total+len(b) <= c.carry && c.newlines+bytes.Count(b, []byte{'\n'}) <= maxResultLines {
			return
		}
		file, path, ok := c.open()
		if !ok {
			c.teeFailed = true
			return
		}
		c.file, c.spillPath = file, path
		c.writeSpill(c.ordered())
	}
	c.writeSpill(b)
}

func (c *cappedOutput) writeSpill(b []byte) {
	if c.file == nil || len(b) == 0 {
		return
	}
	room := c.carry*spillFileMultiple - c.written
	if room <= 0 {
		c.stopSpilling()
		return
	}
	if room < len(b) {
		b = b[:room]
		defer c.stopSpilling()
	}
	n, err := c.file.Write(b)
	c.written += n
	if err != nil {
		c.stopSpilling()
	}
}

// stopSpilling closes the file and records that it holds only the beginning.
// The context still holds the end, so between them the two ends of a runaway
// output are both readable — which is more than either had before.
func (c *cappedOutput) stopSpilling() {
	if c.file == nil {
		return
	}
	_ = c.file.Close()
	c.file = nil
	c.partial = true
}

func (c *cappedOutput) ordered() []byte {
	if !c.round {
		return c.ring[:c.next]
	}
	held := make([]byte, 0, c.carry)
	held = append(held, c.ring[c.next:]...)
	return append(held, c.ring[:c.next]...)
}

// lines is the whole output's line count, the same count countLines makes of a
// string that was held whole.
func (c *cappedOutput) lines() int {
	if c.total == 0 {
		return 0
	}
	if c.endsNewline {
		return c.newlines
	}
	return c.newlines + 1
}

// truncated reports whether anything was cut, which is what tells the caller
// the result already carries its own notice and must not be bounded twice.
func (c *cappedOutput) truncated() bool {
	c.finish()
	return c.total > c.carry || c.lines() > maxResultLines
}

// String renders what boundResult would have rendered from the whole output.
func (c *cappedOutput) String() string {
	c.finish()
	held := string(c.ordered())
	if !c.truncated() {
		// Nothing was lost: the ring holds every byte that was written.
		return held
	}
	// Taken from the ring rather than from the whole, which is the same answer:
	// keep is smaller than carry, so the kept region and the line boundary in
	// front of it are both inside the ring. Only the two totals have to come
	// from the counters, because the ring cannot know them.
	cut := truncateTail(held, c.keep, maxResultLines)
	cut.totalBytes, cut.totalLines = c.total, c.lines()
	cut.byteWindow, cut.lineWindow = c.keep, maxResultLines
	cut.tail = true
	cut.spill = spillRef{path: c.spillPath, bytes: c.written, partial: c.partial}
	return cut.render()
}
