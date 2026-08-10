package chat

import (
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/Agent-Field/aforge-v2/internal/sanitize"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// The engine seam.
//
// Three interfaces, declared here at their narrowest, and satisfied
// structurally by the objects cmd/aforge already builds for the old surface:
// *store.Store is a Backend, the chat commander is a Commander. Nothing is
// forked and nothing is adapted — the v2 entry constructs the SAME engine the
// old entry constructs and differs only in the surface it hands it to
// (Decision 7, the lens law).
//
// They are declared here rather than imported from internal/tui for one
// reason: internal/tui is the surface being replaced. The engine's shape must
// outlive it, and a v2 package that imported the v1 window would have to be
// rewritten on the day that window is deleted — which is a wave after parity,
// not a wave after never. Go's structural typing means this costs nothing: the
// same concrete objects satisfy both sets, and a method that changed shape
// fails the build at the entry point where both are named.

// Backend is the slice of the durable store this surface reads and writes. It
// is a strict subset of internal/tui's Backend, and *store.Store satisfies it.
type Backend interface {
	// Messages is the thread read, watermarked: every poll asks only for what
	// it has not seen.
	Messages(sessionID string, afterSeq int64, limit int) ([]store.Message, error)
	// PostMessage is the one write this surface makes. It goes through
	// internal/thread's single door at the call site, never straight here.
	PostMessage(store.Message) (store.Message, error)
	// LatestEventSeq is the cheapest possible proof that nothing happened: one
	// indexed row, and an unchanged answer means the whole thread is unchanged.
	// It is what keeps the poll from being a scan.
	LatestEventSeq() (int64, error)
}

// Commander is the slice of the live engine this surface commands. A nil
// Commander is an honest state — a window with no head behind it — and every
// door that needs one degrades rather than failing: the awaiting line stops
// advertising an interrupt that would do nothing, and the status line says the
// model is unknown instead of inventing one.
type Commander interface {
	// Interrupt stops the turn being answered right now, carrying in the words
	// the reader has already seen so the durable line that ends the turn is the
	// same reply, marked where it stopped. It reports whether there was a turn
	// to stop.
	Interrupt(partial string) bool
	// CurrentModel names the model in a role slot, for the status line.
	CurrentModel(role string) string
}

// StreamKind names the provider-stream boundary. The ordinals match
// internal/tui's, and the entry point translates one to the other in the four
// lines it takes; the v2 surface keeps its own vocabulary so a field added here
// never reaches the old window.
type StreamKind uint8

const (
	// StreamStarted is a provider call opening.
	StreamStarted StreamKind = iota
	// StreamDelta carries raw response bytes.
	StreamDelta
	// StreamThinking is a phase with nothing to draw: the model is reasoning.
	// The line says the wait has a reason, and nothing else.
	StreamThinking
	// StreamFinished is the call ending on its own terms.
	StreamFinished
	// StreamFailed is the call ending on someone else's.
	StreamFailed
)

// StreamEvent is one boundary on the live feed. Session names the room it
// belongs to; an event whose session disagrees with the one on screen is
// dropped rather than drawn, which is what keeps a second room's tokens out of
// the first room's transcript.
type StreamEvent struct {
	Kind    StreamKind
	Delta   string
	Session string
}

// sanitizeChokepoint is the v2 surface's half of 10.2.6/7, and the only place
// model- or tool-authored text is cleaned.
//
// It differs from the old window's in exactly one way, which is the entire
// point of the token layer's remap table (12.4.4): tool output that arrives in
// someone else's colours is re-slotted onto ours on the way through, so a
// tool's dark red becomes a legible bright one and nothing in someone else's
// text may outrank our own primary tier. The old window keeps sanitize.Identity
// and renders byte-identically to yesterday.
var sanitizeChokepoint = sanitize.Table(tokens.ANSI16Remap)

// sanitizeText is the one call every seam in this package routes through:
// every message body and part the poll delivers, and every decoded reply the
// live stream draws before a durable message for it exists.
//
// User drafts do not pass here. What the reader just typed is trusted input
// they are watching themselves type; it is sanitized on the way back, when the
// store hands it to the poll like every other message.
func sanitizeText(s string) string {
	return sanitize.TextWithPalette(s, sanitizeChokepoint)
}

// partialReply decodes as much of the head's streamed reply string as has
// arrived. The head answers with a structured object and streams its bytes, so
// what a delta carries is a fragment of JSON rather than a fragment of prose —
// drawing it raw would put `{"reply":"` on screen.
//
// It tolerates a chunk ending anywhere, including inside an escape or a
// surrogate pair, without inventing a replacement rune: the next delta resumes
// from the same raw prefix and decodes further. found is false until the reply
// key itself has arrived, which is the honest answer for a stream that has so
// far said nothing the reader can be shown.
func partialReply(raw string) (string, bool) {
	const marker = `"reply"`
	index := strings.Index(raw, marker)
	if index < 0 {
		return "", false
	}
	rest := strings.TrimLeft(raw[index+len(marker):], " \t\r\n")
	if !strings.HasPrefix(rest, ":") {
		return "", false
	}
	rest = strings.TrimLeft(rest[1:], " \t\r\n")
	if !strings.HasPrefix(rest, `"`) {
		return "", false
	}
	rest = rest[1:]

	var decoded strings.Builder
	for offset := 0; offset < len(rest); {
		if rest[offset] == '"' {
			return decoded.String(), true
		}
		if rest[offset] != '\\' {
			char, size := utf8.DecodeRuneInString(rest[offset:])
			if char == utf8.RuneError && size == 1 {
				return decoded.String(), true
			}
			decoded.WriteRune(char)
			offset += size
			continue
		}
		if offset+1 >= len(rest) {
			return decoded.String(), true
		}
		switch escape := rest[offset+1]; escape {
		case '"', '\\', '/':
			decoded.WriteByte(escape)
			offset += 2
		case 'b':
			decoded.WriteByte('\b')
			offset += 2
		case 'f':
			decoded.WriteByte('\f')
			offset += 2
		case 'n':
			decoded.WriteByte('\n')
			offset += 2
		case 'r':
			decoded.WriteByte('\r')
			offset += 2
		case 't':
			decoded.WriteByte('\t')
			offset += 2
		case 'u':
			if offset+6 > len(rest) {
				return decoded.String(), true
			}
			value, err := strconv.ParseUint(rest[offset+2:offset+6], 16, 16)
			if err != nil {
				return decoded.String(), true
			}
			char := rune(value)
			offset += 6
			if char >= 0xd800 && char <= 0xdbff {
				if offset+6 > len(rest) || !strings.HasPrefix(rest[offset:], `\u`) {
					return decoded.String(), true
				}
				second, secondErr := strconv.ParseUint(rest[offset+2:offset+6], 16, 16)
				if secondErr != nil || second < 0xdc00 || second > 0xdfff {
					return decoded.String(), true
				}
				char = utf16.DecodeRune(char, rune(second))
				offset += 6
			} else if char >= 0xdc00 && char <= 0xdfff {
				return decoded.String(), true
			}
			decoded.WriteRune(char)
		default:
			return decoded.String(), true
		}
	}
	return decoded.String(), true
}
