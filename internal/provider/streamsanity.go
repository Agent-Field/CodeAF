package provider

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// rescuedPrintableFloor is the fraction of runes in a rescued/hedged reply
// that must be printable (or space) before the stream may be the turn.
//
// A degraded endpoint can close a stream cleanly and still hand back
// control-character soup; F20 persisted that soup because "the stream
// finished" was treated as "this is the answer". Below this floor the
// bytes are not language, whatever finish_reason said.
const rescuedPrintableFloor = 0.85

// rescuedStreamError is why a finished hedge/rescue arm cannot be the turn.
//
// Three readings, and only these three — they are the sanity check F20/F21
// asked for. A stream that fails any of them is rejected: the race walks
// to another arm, or the caller sees an honest error. The response is
// never returned, so nothing here can reach the transcript.
//
// IT IS CUTBABBLE ON PURPOSE. A rescued stream that is not language is
// the same failure plane the degeneration guard already names, and the
// session already knows not to keep that text.
func rescuedStreamError(response *ai.Response) error {
	if response == nil {
		return &StreamCut{Reason: CutBabble}
	}
	if !rescuedFinishOK(FinishReason(response)) {
		return &StreamCut{Reason: CutBabble}
	}
	text := responseText(response)
	if text == "" {
		return nil
	}
	if !rescuedDecodeOK(text) || rescuedPrintableRatio(text) < rescuedPrintableFloor {
		return &StreamCut{Reason: CutBabble}
	}
	return nil
}

// rescuedFinishOK accepts a clean end and refuses a stream that named its
// own failure. Empty is allowed: several endpoints omit the field on a
// tidy close, and rejecting that would turn a working rescue into an
// outage. "error" and "content_filter" are the provider saying the bytes
// are not an answer.
func rescuedFinishOK(reason string) bool {
	switch strings.ToLower(strings.TrimSpace(reason)) {
	case "error", "content_filter":
		return false
	default:
		return true
	}
}

// rescuedDecodeOK is UTF-8 that did not already lose a rune. U+FFFD is
// valid UTF-8 and is also the mark a decoder leaves when it could not
// read what it was given — that is the mojibake F20 stored.
func rescuedDecodeOK(text string) bool {
	return utf8.ValidString(text) && !strings.ContainsRune(text, utf8.RuneError)
}

func rescuedPrintableRatio(text string) float64 {
	if text == "" {
		return 1
	}
	total, printable := 0, 0
	for _, r := range text {
		total++
		if unicode.IsPrint(r) || unicode.IsSpace(r) {
			printable++
		}
	}
	return float64(printable) / float64(total)
}
