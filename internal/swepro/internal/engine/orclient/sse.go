package orclient

// The SSE frame decoder — the Go replacement for
// `createEventSourceResponseHandler` → `parseJsonEventStream` →
// `TextDecoderStream` → `EventSourceParserStream` (`internal/index.mjs:2266-2277`,
// `:2104-2118`).
//
// Hand-rolled per ENGINE-DESIGN F3 (no third-party dependencies). The rules
// that matter, all of which the design calls out:
//
//   - fields are separated by a newline; an event is dispatched on a BLANK
//     line. Both LF and CRLF terminate a line, and a bare CR does too (the SSE
//     spec's three line terminators).
//   - a line beginning with `:` is a comment. OpenRouter sends
//     `: OPENROUTER PROCESSING` keepalives every few seconds; they must not
//     produce an event and must not disturb the data buffer.
//   - `data:` takes the rest of the line with ONE optional leading space
//     stripped. Multiple `data:` lines in one frame are joined with `\n`.
//   - an event with an EMPTY data buffer is not dispatched at all.
//   - `data: [DONE]` is swallowed by parseJsonEventStream (`:2107-2109`), not
//     by the frame decoder, so it terminates nothing on its own — the stream
//     ends when the body does.
//   - a trailing frame with no terminating blank line IS dispatched at EOF.
//     (The eventsource-parser flushes on stream end.)

import (
	"bufio"
	"io"
	"strings"
)

// SSEEvent is one dispatched event. Only `data` is consumed downstream; `event`
// and `id` are decoded because the grammar requires skipping them correctly.
type SSEEvent struct {
	Event string
	Data  string
	ID    string
}

// SSEDecoder splits a byte stream into events.
type SSEDecoder struct {
	r *bufio.Reader

	data    strings.Builder
	hasData bool
	event   string
	lastID  string
	done    bool
	started bool
}

// NewSSEDecoder wraps a reader. The buffer is generous because a single
// reasoning-heavy chunk can exceed the default 4 KiB line limit by a lot.
func NewSSEDecoder(r io.Reader) *SSEDecoder {
	return &SSEDecoder{r: bufio.NewReaderSize(r, 64*1024)}
}

// Next returns the next event, or io.EOF when the stream ends.
func (d *SSEDecoder) Next() (SSEEvent, error) {
	for {
		if d.done {
			return SSEEvent{}, io.EOF
		}
		line, err := d.readLine()
		if !d.started {
			d.started = true
			line = strings.TrimPrefix(line, "\uFEFF")
		}
		if err != nil {
			d.done = true
			if err != io.EOF {
				return SSEEvent{}, err
			}
			if len(line) == 0 && !d.hasData {
				return SSEEvent{}, io.EOF
			}
			if ev, ok := d.feed(line); ok {
				return ev, nil
			}
			if ev, ok := d.dispatch(); ok {
				return ev, nil
			}
			return SSEEvent{}, io.EOF
		}
		if ev, ok := d.feed(line); ok {
			return ev, nil
		}
	}
}

// feed consumes one line, returning an event when the line dispatches one.
func (d *SSEDecoder) feed(line string) (SSEEvent, bool) {
	if line == "" {
		return d.dispatch()
	}
	if strings.HasPrefix(line, ":") {
		// Comment / keepalive.
		return SSEEvent{}, false
	}
	field, value := line, ""
	if colon := strings.IndexByte(line, ':'); colon >= 0 {
		field = line[:colon]
		value = line[colon+1:]
		// Exactly ONE leading space is stripped.
		if strings.HasPrefix(value, " ") {
			value = value[1:]
		}
	}
	switch field {
	case "data":
		if d.hasData {
			d.data.WriteByte('\n')
		}
		d.data.WriteString(value)
		d.hasData = true
	case "event":
		d.event = value
	case "id":
		// The spec ignores an id containing NUL; nothing downstream reads it.
		if !strings.ContainsRune(value, 0) {
			d.lastID = value
		}
	case "retry":
		// Reconnection time — the SDK never reconnects.
	}
	return SSEEvent{}, false
}

func (d *SSEDecoder) dispatch() (SSEEvent, bool) {
	if !d.hasData {
		d.event = ""
		return SSEEvent{}, false
	}
	ev := SSEEvent{Event: d.event, Data: d.data.String(), ID: d.lastID}
	d.data.Reset()
	d.hasData = false
	d.event = ""
	return ev, true
}

// readLine reads one line, accepting LF, CRLF and a bare CR as terminators.
func (d *SSEDecoder) readLine() (string, error) {
	var sb strings.Builder
	for {
		b, err := d.r.ReadByte()
		if err != nil {
			return sb.String(), err
		}
		switch b {
		case '\n':
			return sb.String(), nil
		case '\r':
			next, err := d.r.ReadByte()
			if err == nil && next != '\n' {
				_ = d.r.UnreadByte()
			}
			return sb.String(), nil
		default:
			sb.WriteByte(b)
		}
	}
}

// DoneSentinel is the payload parseJsonEventStream drops before parsing
// (`:2107`).
const DoneSentinel = "[DONE]"
