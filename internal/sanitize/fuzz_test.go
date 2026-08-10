package sanitize

import (
	"strings"
	"testing"
)

// FuzzText feeds render (via both Text and a non-identity palette, so the
// SGR-rewrite path gets fuzzed too) an adversarial corpus of truncated
// escape sequences, nested escapes, mixed valid/invalid bytes, and huge
// inputs. The only real assertions available to a fuzzer here are the
// safety invariants the build brief asked for — it must never panic, and
// it must never leave anything OSC/DCS/APC/PM/SOS/C1-shaped in the output
// — go test -fuzz=FuzzText -fuzztime=30s is the way to run it under the Go
// fuzzing engine; `go test ./internal/sanitize/` runs the seed corpus
// below as ordinary table cases even without -fuzz.
func FuzzText(f *testing.F) {
	seeds := []string{
		"",
		"plain text, nothing to see",
		"line\nwith\ttabs and \\n literal backslash-n",
		"\x1b[31mred\x1b[0m",
		"\x1b[1;38;5;196mtruecolor-ish\x1b[0m",
		"\x1b]52;c;aGVsbG8gd29ybGQ=\x07",
		"\x1b]52;c;aGVsbG8=\x1b\\",
		"\x1b]0;pwned title\x07",
		"\x1b]8;;https://evil.example\x1b\\click\x1b]8;;\x1b\\",
		"\x1b]7;file:///etc/passwd\x07",
		"\x1b]9;notification\x07",
		"\x1b]133;A\x07",
		"\x1b]777;notify\x07",
		"\x1b]99;;payload\x07",
		"\x1bPq#sixel-ish\x1b\\",
		"\x1b_apc payload\x1b\\",
		"\x1b^pm payload\x1b\\",
		"\x1bXsos payload\x1b\\",
		"\x1b",
		"\x1b[",
		"\x1b[1;2;3",
		"\x1b]",
		"\x1b]52;c;",
		"\x1b]52;c;\x1b",
		"\x1bP",
		"\x1b_",
		"\x1b^",
		"\x1bX",
		"\x1b]8;;\x1b]52;c;aGk=\x07",
		"\x1b\x1b\x1b\x1b\x1b",
		"\x9b31m",
		"\x9d52;c;aGk=\x07",
		"\x90junk\x9c",
		"\x9fjunk\x9c",
		"\x9ejunk\x9c",
		"\x98junk\x9c",
		"\x9c",
		"\x85",
		"\xff\xfe\xfd",
		"\x80\x81\x82",
		"\xc2",
		"\xe2\x82",
		"\xf0\x9f\x8e",
		"café 日本語 🎉",
		"\x1b[2J\x1b[H",
		"\x1b[?25l\x1b[?25h",
		"mixed \x1b[31mred\x1b]52;c;evil\x07 tail \x1b[0m",
		strings.Repeat("\x1b[", 200),
		strings.Repeat("\x1b]52;", 200),
		strings.Repeat("a", 100000),
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, s string) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("Text/TextWithPalette panicked on %q: %v", s, r)
			}
		}()

		out := Text(s)
		assertNoDangerousSequences(t, s, out)

		palette := Identity
		palette[0], palette[7] = palette[7], palette[0]
		outPalette := TextWithPalette(s, palette)
		assertNoDangerousSequences(t, s, outPalette)

		// Sanitizing already-sanitized output must be a no-op: the output
		// alphabet (plain text + SGR) is a subset of the input alphabet,
		// so a second pass has nothing left to strip.
		if again := Text(out); again != out {
			t.Fatalf("Text is not idempotent: Text(%q) = %q, Text(that) = %q", s, out, again)
		}
	})
}

// assertNoDangerousSequences checks the invariant every seed and every
// fuzzer-generated input must satisfy: nothing OSC/DCS/APC/PM/SOS-shaped,
// no raw C1 control byte, and no non-SGR CSI survives sanitization. It
// walks whole UTF-8 runes, not raw bytes — a legitimate multi-byte rune
// (an emoji, an accented letter) can have a continuation byte that numerically
// falls inside the C1 range, exactly the corruption risk render() itself
// guards against, so the check has to respect the same rune boundaries or
// it would flag valid text as a violation.
func assertNoDangerousSequences(t *testing.T, in, out string) {
	t.Helper()
	for i := 0; i < len(out); {
		b := out[i]
		if b == esc {
			// Every ESC that survives must open a CSI sequence terminated
			// by 'm' (SGR) — scanCSIBody is the same parser render used
			// to admit it in the first place, so re-running it here is
			// the exact re-check of the allowlist invariant.
			if i+1 >= len(out) || out[i+1] != '[' {
				t.Fatalf("Text(%q) = %q leaves a non-CSI ESC at index %d", in, out, i)
			}
			next, sgr := scanCSIBody(out, i+2)
			if !sgr {
				t.Fatalf("Text(%q) = %q leaves a non-SGR CSI at index %d", in, out, i)
			}
			i = next
			continue
		}
		if b < 0x80 {
			i++
			continue
		}
		// b >= 0x80: either a genuine standalone/lead-position C1-range
		// byte (dangerous, render() must have stripped it) or the lead
		// byte of a valid multi-byte rune, in which case skip the whole
		// rune the same way render() does.
		size := runeSize(out[i:])
		if size == 1 && b >= c1Lo && b <= c1Hi {
			t.Fatalf("Text(%q) = %q leaves a raw C1 byte at index %d", in, out, i)
		}
		i += size
	}
}
