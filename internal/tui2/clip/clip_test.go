package clip

import (
	"encoding/base64"
	"strings"
	"testing"
	"unicode/utf8"
)

// The sequence is the product here: everything else in this package is one call
// to the runtime. So the tests are about the bytes.

func TestCopyBuildsTheOSC52Sequence(t *testing.T) {
	const text = "the plan, in full"
	got := Copy(text)

	if !strings.HasPrefix(got, "\x1b]52;c;") {
		t.Fatalf("the sequence does not open an OSC 52 system-clipboard write: %q", got)
	}
	if !strings.HasSuffix(got, "\x07") {
		t.Fatalf("the sequence is not terminated: %q", got)
	}
	payload := strings.TrimSuffix(strings.TrimPrefix(got, "\x1b]52;c;"), "\x07")
	decoded, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		t.Fatalf("the payload is not base64: %v (%q)", err, payload)
	}
	if string(decoded) != text {
		t.Fatalf("the payload decodes to %q, want %q", decoded, text)
	}
}

// A payload the terminal refuses is a copy that silently did nothing, so the
// cut happens here rather than at the emulator.
func TestAHugePayloadIsCutToOneWrite(t *testing.T) {
	huge := strings.Repeat("a", Limit*3)
	fitted := Fit(huge)
	if len(fitted) != Limit {
		t.Fatalf("the text was cut to %d bytes, want %d", len(fitted), Limit)
	}
	encoded := len(base64.StdEncoding.EncodeToString([]byte(fitted)))
	if encoded > 74_000 {
		t.Fatalf("the encoded payload is %d bytes, past what one write carries", encoded)
	}
	if got := len(Copy(huge)); got > 74_000+len("\x1b]52;c;\x07") {
		t.Fatalf("the whole sequence is %d bytes, past what one write carries", got)
	}
}

// Half a rune is not a character, and a paste is a thing a person reads.
func TestTheCutNeverSeversARune(t *testing.T) {
	// Three-byte runes, so the cap lands mid-rune whatever the remainder is.
	text := strings.Repeat("あ", Limit)
	fitted := Fit(text)
	if len(fitted) > Limit {
		t.Fatalf("the cut text is %d bytes, past the limit %d", len(fitted), Limit)
	}
	if !utf8.ValidString(fitted) {
		t.Fatal("the cut left invalid UTF-8 on the clipboard")
	}
}

// Text that fits is untouched — the ordinary message, and every one of them.
func TestOrdinaryTextIsNotCut(t *testing.T) {
	const text = "what is the plan for navctx?"
	if got := Fit(text); got != text {
		t.Fatalf("ordinary text was rewritten: %q", got)
	}
}

// A copy door that found nothing must leave the clipboard alone: an empty OSC 52
// payload is the sequence terminals read as "clear it".
func TestNothingToCopyWritesNothing(t *testing.T) {
	if cmd := Write(""); cmd != nil {
		t.Fatal("an empty copy still reached for the clipboard")
	}
}

// The coupling this package hides from every caller, pinned here: [Written] must
// recognize what [Write] emits. It fails on the day the runtime changes its
// clipboard message, which is the day to find out — not the day a surface's copy
// test starts passing vacuously.
func TestWriteEmitsAClipboardMessageThisPackageCanRead(t *testing.T) {
	const text = "workspace/wisp/plan.md"
	cmd := Write(text)
	if cmd == nil {
		t.Fatal("a copy with text in it produced no command")
	}
	got, ok := Written(cmd())
	if !ok {
		t.Fatalf("the emitted message is not readable as a clipboard write: %#v", cmd())
	}
	if got != text {
		t.Fatalf("the clipboard was given %q, want %q", got, text)
	}
}

// Anything else is not a clipboard write, however string-shaped it is.
func TestWrittenRefusesEverythingElse(t *testing.T) {
	type lookalike string
	for _, msg := range []any{nil, "plain string", lookalike("dressed up"), 42, struct{}{}} {
		if _, ok := Written(msg); ok {
			t.Fatalf("%#v was read as a clipboard write", msg)
		}
	}
}
