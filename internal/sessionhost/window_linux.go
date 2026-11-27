//go:build linux

package sessionhost

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"unicode"
)

// The two request headers the organization's server sets on a chat OWNER's wire, and that nothing a window sends can
// reach, because the server carries over only what a WebSocket handshake needs and sets these itself.
//
//	X-Codeaf-Window         the name every other window of the chat reads for this one: the person's handle, or
//	                        "<handle>'s bot" for a bot using that person's rights.
//	X-Codeaf-Window-Device  "own" says the window is one of the person's own computers, so the word it sent for
//	                        itself ("phone", its machine's name) is kept after the name: "kai's phone". Absent, the
//	                        name stands alone.
//
// They are spelled the same in internal/factory/serve (hostedWindowHeader), the only place that sets them.
const (
	windowHeader       = "X-Codeaf-Window"
	windowDeviceHeader = "X-Codeaf-Window-Device"
	windowDeviceOwn    = "own"
)

// windowNameMost is how much of a window's name the engine draws (internal/remote's machineNameMost). The person's
// name comes first, so a long device word is what is cut.
const windowNameMost = 32

// windowLineMost bounds how much of the first line the renamer holds waiting for its newline. Past it the stream is
// not a hello a window sends, and it is passed along untouched for the engine to refuse.
const windowLineMost = 256 << 10

// windowNamed is the name the server asked for on this wire, with whether the device word is kept.
type windowNamed struct {
	name   string
	device bool
}

// windowOf reads what the server asked for. An absent or unusable name asks for nothing, which leaves the wire as it
// was before the header existed (an older server, or a guest's wire, which the server renames itself).
func windowOf(h http.Header) (windowNamed, bool) {
	name := windowText(h.Get(windowHeader))
	if name == "" {
		return windowNamed{}, false
	}
	return windowNamed{name: name, device: h.Get(windowDeviceHeader) == windowDeviceOwn}, true
}

// windowText makes one word safe to draw on another person's screen: control characters go and whitespace collapses.
func windowText(said string) string {
	said = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && !unicode.IsSpace(r) {
			return -1
		}
		return r
	}, said)
	return strings.Join(strings.Fields(said), " ")
}

// label is what the window is called, given the word it sent for itself.
//
// THE NAME IS THE PERSON AND THE DEVICE WORD IS ONLY EVER AFTER IT. The engine draws a window's name on the others'
// screens ("typing from kai's phone now") and reads two equal names as one machine (a window on the same machine is
// "another window"). The word a computer chose for itself cannot stand alone there, because "mia" would then read as
// Mia whoever chose it, but "kai's mia" cannot be mistaken for her; and keeping it after the name leaves two of one
// person's computers apart, which a bare name would not.
func (w windowNamed) label(said string) string {
	device := windowText(said)
	if !w.device || device == "" || strings.EqualFold(device, w.name) {
		return w.name
	}
	label := []rune(w.name + "'s " + device)
	if len(label) > windowNameMost {
		label = label[:windowNameMost]
	}
	return strings.TrimSpace(string(label))
}

// windowRenamer is the window's half of the bridge: it hands the engine the window's bytes unchanged, except that the
// window's first line, its hello, names the window as the server asked. Every later byte, in every later read, is the
// window's own.
//
// WHY HERE AND NOT IN THE SERVER. An owner's wire is a byte-for-byte proxy on purpose (the guest's is not), and a
// proxy that read the stream would have to keep message boundaries, refusals and the upgrade as the chats' host gives
// them. This is where the stream is already a stream of lines, one reader before the engine.
type windowRenamer struct {
	src     io.Reader
	window  windowNamed
	pending []byte
	out     []byte
	err     error
	done    bool
}

func (r *windowRenamer) Read(p []byte) (int, error) {
	for !r.done && len(r.out) == 0 && r.err == nil {
		if i := bytes.IndexByte(r.pending, '\n'); i >= 0 {
			line, rest := r.pending[:i+1], r.pending[i+1:]
			r.out = append(r.rename(line), rest...)
			r.pending, r.done = nil, true
			break
		}
		if len(r.pending) > windowLineMost {
			r.out, r.pending, r.done = r.pending, nil, true
			break
		}
		chunk := make([]byte, 4096)
		n, err := r.src.Read(chunk)
		r.pending = append(r.pending, chunk[:n]...)
		if err != nil {
			// What was read is the window's, whole or not; it goes first and the end of the stream after it.
			r.out, r.pending, r.done, r.err = r.pending, nil, true, err
		}
	}
	if len(r.out) > 0 {
		n := copy(p, r.out)
		r.out = r.out[n:]
		return n, nil
	}
	if r.err != nil {
		return 0, r.err
	}
	return r.src.Read(p)
}

// rename is the first line with the window's name replaced, or the line as it came when it is not a hello.
func (r *windowRenamer) rename(line []byte) []byte {
	// Decoded the way the engine decodes a frame, which matches field names whatever their case and keeps the last of
	// a repeated one, so a hello spelled "Kind" or "SURFACE" is the hello the engine would read.
	var typed struct {
		Kind    string          `json:"kind"`
		Payload json.RawMessage `json:"payload"`
	}
	var hello struct {
		Surface string `json:"surface"`
	}
	var fields, payload map[string]json.RawMessage
	if json.Unmarshal(line, &typed) != nil || typed.Kind != "hello" ||
		json.Unmarshal(line, &fields) != nil || fields == nil ||
		json.Unmarshal(typed.Payload, &payload) != nil || payload == nil {
		return line
	}
	// A surface that is not text is read as no word at all: the hello is renamed either way, never passed through.
	_ = json.Unmarshal(typed.Payload, &hello)
	for _, name := range []string{"surface", "remote"} {
		// Remote is what a door that authenticated a viewer stamps, and the engine names a window by it before it
		// looks at the surface. A person's own window has no such door, so neither is the window's to say.
		deleteFold(payload, name)
	}
	payload["surface"], _ = json.Marshal(r.window.label(hello.Surface))
	deleteFold(fields, "payload")
	deleteFold(fields, "kind")
	fields["kind"], _ = json.Marshal("hello")
	fields["payload"], _ = json.Marshal(payload)
	out, err := json.Marshal(fields)
	if err != nil {
		return line
	}
	return append(out, '\n')
}

func deleteFold(fields map[string]json.RawMessage, name string) {
	for field := range fields {
		if strings.EqualFold(field, name) {
			delete(fields, field)
		}
	}
}
