package session

// FORMING CALLS — the phase between "nothing" and "the model has asked".
//
// A tool call arrives over the wire in fragments, and for a long one — a write
// whose body is the file, a propose_task whose brief is three paragraphs — the
// arrival takes seconds. Until this existed the turn said nothing for those
// seconds: EventToolAnnounced fires when the call is WHOLE, and a surface
// waiting on it draws silence while the model is visibly working.
//
// This file is what stands in that gap. It takes the provider's per-fragment
// StreamToolCallForming events (provider/stream.go) and answers two questions
// the read loop deliberately does not: HOW OFTEN a person needs to be told, and
// WHAT to say. Both answers are best-effort by construction — the text being
// described is half a JSON object — and neither may ever fail: a scan that
// panicked or a hint that errored would take down a turn to decorate a row.

import (
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/Agent-Field/aforge-v2/internal/provider"
)

// formingInterval is the fastest a single forming call may speak. The provider
// raises one event per fragment, which for an endpoint streaming arguments a
// token at a time is hundreds a second — every one of them fanned out to every
// subscriber's queue and drawn by every surface.
//
// Ten frames a second is already faster than a person reads a growing byte
// count, and the two things that are NOT time-based — the name landing, and an
// argument field closing — bypass it entirely, because those are the moments the
// row actually changes what it says.
const formingInterval = 100 * time.Millisecond

// formingValueLimit bounds one captured argument value. The scanner does not
// know which fields it will be asked about, so it captures every top-level
// string — and one of those is a write's whole file body. A hint is eighty
// columns; anything past this cannot appear in one.
const formingValueLimit = 512

// formingBatch tracks the calls of ONE response as they arrive.
//
// Its lifetime is the warm batch's, and for the warm batch's reason: a retry is
// a new response whose calls are its own, so the half-formed state of the dead
// attempt is thrown away with everything else it started (loop.go's reset).
//
// Calls are keyed by INDEX rather than id, because the index is the one part of
// a call's identity the wire has from the first fragment — an id may be three
// fragments away, and a tracker keyed on "" would fuse two parallel calls into
// one row.
type formingBatch struct {
	mu    sync.Mutex
	calls map[int]*formingState
	// now is the clock the throttle reads, injected by tests. A nil clock is
	// time.Now: the loop builds this with a bare literal.
	now func() time.Time
}

// formingState is one call's progress: what the wire has said of its identity,
// how far the scanner has read, and when this call last spoke.
type formingState struct {
	tool     string
	args     partialArgs
	hint     string
	lastEmit time.Time
	spoken   bool
}

func (b *formingBatch) clock() time.Time {
	if b.now != nil {
		return b.now()
	}
	return time.Now()
}

// reset forgets every call in flight. See warmBatch.reset: same boundary, same
// reason, called in the same breath.
func (b *formingBatch) reset() {
	if b == nil {
		return
	}
	b.mu.Lock()
	b.calls = nil
	b.mu.Unlock()
}

// note folds one provider forming event in and answers with the Event to send,
// or false to stay quiet this fragment.
//
// It is called from the provider's read loop, which must not work, so what
// happens here is one map lookup and a scan of the bytes that arrived SINCE THE
// LAST FRAGMENT — never a re-scan of the whole argument text, which would be
// quadratic in the length of a call the model is spelling out a token at a time.
func (b *formingBatch) note(event provider.StreamEvent) (Event, bool) {
	if b == nil {
		return Event{}, false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.calls == nil {
		b.calls = make(map[int]*formingState, 2)
	}
	state, known := b.calls[event.Index]
	if !known {
		state = &formingState{}
		b.calls[event.Index] = state
	}

	// THE TWO CHANGES THAT ALWAYS SPEAK. A name landing is what turns "something
	// is arriving" into "it is a write", and a field closing is what turns "a
	// write" into "a write of internal/foo.go". Both are the row saying a
	// different sentence, and a throttle that swallowed them would leave the
	// person reading the previous sentence for another tenth of a second — which
	// is the whole complaint this event exists to answer, in miniature.
	named := event.Tool != "" && state.tool == ""
	if event.Tool != "" {
		state.tool = event.Tool
	}
	closed := state.args.feed(event.Delta)
	if named || closed {
		state.hint = formingHint(state.tool, &state.args)
	}

	now := b.clock()
	switch {
	case !state.spoken, named, closed:
	case now.Sub(state.lastEmit) >= formingInterval:
	default:
		return Event{}, false
	}
	state.spoken = true
	state.lastEmit = now

	return Event{
		Kind:   EventToolForming,
		CallID: event.ID,
		Tool:   state.tool,
		Hint:   state.hint,
		// The raw text is capped like every other display copy on this struct
		// (Args, Output): a surface cannot draw four megabytes of file body, and
		// Bytes beside it is the honest size of what is actually arriving.
		ArgsText: clip(event.Delta, argsLimit),
		Bytes:    len(event.Delta),
	}, true
}

// formingHintFields names the argument fields a forming call may be glossed by,
// in the order they are preferred. Most tools have exactly one and it is already
// written down — [glossField], the same field the finished call is glossed by,
// so a row does not rename itself the moment it is announced.
//
// The one addition is `tasks`, which says what it is doing across two fields:
// WHICH task, and what is being done to it. Whichever has closed first is what
// the row can say, and the id is preferred because a person watching wants to
// know which piece of work is being touched before they want to know how.
var formingHintFields = map[string][]string{
	"tasks": {"id", "resolve", "say", "query"},
}

func formingHintFieldsFor(tool string) []string {
	if fields, known := formingHintFields[tool]; known {
		return fields
	}
	if field, known := glossField[tool]; known {
		return []string{field}
	}
	return nil
}

// formingHint reads the best gloss the closed fields support, in [gloss]'s
// shape — the tool name and the one argument that says what the work is — so the
// forming row, the announced row and the running row all read as one line that
// gained detail rather than three lines about the same call.
//
// It is empty until a hint-bearing field has CLOSED. A field still arriving is
// a value nobody has seen the end of, and half a path is a lie a surface would
// print as if it were a fact.
func formingHint(tool string, args *partialArgs) string {
	if tool == "" {
		return ""
	}
	for _, field := range formingHintFieldsFor(tool) {
		value, closed := args.value(field)
		if !closed {
			continue
		}
		value = strings.TrimSpace(firstLine(value))
		if value == "" {
			continue
		}
		return clip(tool+" "+value, hintLimit)
	}
	return ""
}

// ── the partial-argument scanner ────────────────────────────────────────────

// partialArgs reads half-sent tool arguments for the top-level fields that have
// CLOSED, and is the reason none of this can fail.
//
// IT IS NOT A JSON PARSER AND MUST NEVER BECOME ONE. json.Unmarshal answers
// "this is not valid JSON" about every prefix of a call the model is still
// sending, which is the only input this ever sees. This is a byte scanner with
// no error return and no opinion about well-formedness: it tracks quoting,
// escaping and nesting well enough to know when a top-level string value ends,
// and everything it cannot make sense of it simply passes over.
//
// Only TOP-LEVEL STRINGS are captured. Every field any tool is glossed by is
// one, and a scanner that walked into nested objects would be keeping state
// about arguments no row will ever show.
type partialArgs struct {
	// consumed is how much of the accumulated text has been scanned. The
	// provider re-sends the whole text each fragment; this is what makes reading
	// it linear rather than quadratic.
	consumed int

	depth    int
	inString bool
	escape   bool
	// unicode counts the hex digits still expected after a \u, collected in hex.
	unicode int
	hex     [4]byte

	// isKey says the string being read is a top-level KEY; capture says it is a
	// top-level string VALUE. Both are false inside a nested object, where
	// nothing is captured.
	isKey   bool
	capture bool
	expect  bool // a ':' at depth 1 has been seen
	key     string
	buf     strings.Builder

	values map[string]string
	closes int
}

// feed scans whatever arrived since the last call and says whether a top-level
// field CLOSED in it — the signal that the hint may now say something new.
//
// A text shorter than what was already consumed means the accumulation restarted
// under this scanner (nothing does that today; a provider that resent a call
// from the beginning would). Starting over is the only honest answer: the state
// describes bytes that are no longer there.
func (p *partialArgs) feed(text string) bool {
	if len(text) < p.consumed {
		*p = partialArgs{}
	}
	before := p.closes
	for index := p.consumed; index < len(text); index++ {
		p.step(text[index])
	}
	p.consumed = len(text)
	return p.closes > before
}

// value returns one closed top-level field. A field still arriving is not here:
// the scanner records a value at its closing quote and never before.
func (p *partialArgs) value(field string) (string, bool) {
	value, closed := p.values[field]
	return value, closed
}

func (p *partialArgs) step(b byte) {
	if p.inString {
		p.stepString(b)
		return
	}
	switch b {
	case '"':
		// At depth 1 a string is a key when no ':' has been seen since the last
		// comma, and the value of that key when one has. Anywhere deeper it is
		// part of an argument no gloss reads, and is scanned for its quoting
		// alone.
		p.isKey = p.depth == 1 && !p.expect
		p.capture = p.depth == 1 && p.expect
		p.inString = true
		p.buf.Reset()
	case '{', '[':
		p.depth++
		p.expect = false
	case '}', ']':
		p.depth--
	case ':':
		if p.depth == 1 {
			p.expect = true
		}
	case ',':
		if p.depth == 1 {
			p.expect = false
			p.key = ""
		}
	}
}

func (p *partialArgs) stepString(b byte) {
	switch {
	case p.unicode > 0:
		p.hex[len(p.hex)-p.unicode] = b
		p.unicode--
		if p.unicode == 0 {
			p.writeUnicode()
		}
	case p.escape:
		p.escape = false
		p.writeEscape(b)
	case b == '\\':
		p.escape = true
	case b == '"':
		p.inString = false
		p.closeString()
	default:
		p.write(b)
	}
}

// closeString records the string that just ended: a key becomes the key the
// next value belongs to, and a captured value becomes a closed field.
func (p *partialArgs) closeString() {
	text := p.buf.String()
	p.buf.Reset()
	switch {
	case p.isKey:
		p.key = text
	case p.capture && p.key != "":
		if p.values == nil {
			p.values = make(map[string]string, 4)
		}
		p.values[p.key] = text
		p.closes++
	}
	p.isKey, p.capture = false, false
}

func (p *partialArgs) write(b byte) {
	// Past the cap the bytes are still SCANNED — the quoting has to stay
	// tracked or the end of the string would be missed — and simply not kept.
	if !p.keeping() {
		return
	}
	p.buf.WriteByte(b)
}

func (p *partialArgs) writeRune(r rune) {
	if !p.keeping() {
		return
	}
	p.buf.WriteRune(r)
}

// keeping says whether the string being scanned is one whose text is worth
// holding: a top-level key or a top-level value, and not yet at the cap.
func (p *partialArgs) keeping() bool {
	return (p.isKey || p.capture) && p.buf.Len() < formingValueLimit
}

func (p *partialArgs) writeEscape(b byte) {
	switch b {
	case 'n':
		p.write('\n')
	case 't':
		p.write('\t')
	case 'r':
		p.write('\r')
	case 'b', 'f':
		p.write(' ')
	case 'u':
		p.unicode = len(p.hex)
	default:
		// The quote, the backslash and the solidus stand for themselves, and an
		// escape this scanner does not know is passed through as its own
		// character rather than dropped: a gloss is allowed to be approximate,
		// but it is never allowed to invent.
		p.write(b)
	}
}

// writeUnicode resolves a \uXXXX escape. A half of a surrogate pair is written
// as the replacement character: pairing them would mean holding state across two
// escapes to spell one glyph that a one-line hint is going to clip anyway.
func (p *partialArgs) writeUnicode() {
	r := unicode.ReplacementChar
	if code, err := strconv.ParseUint(string(p.hex[:]), 16, 32); err == nil {
		if decoded := rune(code); decoded < 0xD800 || decoded > 0xDFFF {
			r = decoded
		}
	}
	p.writeRune(r)
}
