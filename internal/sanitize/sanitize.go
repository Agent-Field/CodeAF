// Package sanitize is the one output chokepoint for terminal-unsafe text
// (chat-rebuild audit notes, Part 10.2 items 6-7).
//
// Anything that reaches the terminal and was not authored by our own
// renderer — a model reply, a tool result, a worker trace line — is a
// channel someone else's bytes can drive. Escape sequences are how a
// terminal emulator is remote-controlled: OSC 52 writes the system
// clipboard, OSC 0/1/2 rewrite the window/tab title, OSC 8 can point a
// clickable span at an arbitrary URL, and the DCS/APC/PM/SOS string types
// exist for the same reason CSI does — to make the emulator DO something,
// not print something. Sanitize.Text (and its palette-remapping sibling,
// TextWithPalette) is the single place that content gets to cross from
// "somebody else's bytes" to "safe to draw."
//
// # What survives (the allowlist)
//
//   - plain text, including all valid UTF-8 beyond ASCII
//   - '\n' and '\t'
//   - SGR sequences: CSI parameter-string 'm' (ESC '[' ... 'm'), the color
//     and style codes lipgloss/our renderers already emit. Nothing else
//     introduced by ESC survives — cursor movement, screen/line erase,
//     mode-setting, and every other CSI final byte are stripped, along with
//     both 2- and 3-byte ESC forms (ESC 'c', ESC '(' 'B', ...).
//
// # What is neutralized
//
// OSC of any kind (52 clipboard, 0/1/2 title, 8 hyperlink, 7/9/133/777/99
// and anything unrecognized — the list is not an allowlist, so a new OSC
// number needs no code change here), DCS/APC/PM/SOS strings, every C1
// control byte (0x80-0x9F, both as a raw byte and via the classic
// ESC-prefixed 7-bit forms), and any C0 control other than '\n'/'\t'.
// Malformed input (a truncated OSC, a lone trailing ESC, an escape nested
// inside another escape's payload, invalid UTF-8) is consumed defensively
// and never panics — see the fuzz corpus in sanitize_test.go, written after
// this codebase's escape-blind-truncate incident (internal/tui's
// ANSI-aware truncate/wrapText).
//
// # OSC 8 policy
//
// The product itself adopts OSC 8 hyperlinks (audit notes 5.21) — the
// threat is not the sequence, it's letting UNTRUSTED text mint one. So OSC
// 8 arriving inside model/tool/trace output is stripped like any other
// OSC, leaving the visible link text behind; our own renderer is the only
// thing allowed to wrap a span in a real OSC 8 link, and it does so after
// this chokepoint, from our own chrome strings, never from sanitized
// content.
//
// # Palette remap (10.2.7)
//
// TextWithPalette additionally remaps raw ANSI-16 SGR color codes (the
// classic 30-37/40-47/90-97/100-107 families) through a caller-supplied
// Table, so a tool's own colors cohere with ours instead of clashing with
// whatever the token layer eventually defines (Wave 2). The mechanism
// lands now; Identity is the only table anyone plugs in until then, and
// calling with Identity is a single 16-int array compare — no extra
// allocation, no second pass over the text.
package sanitize

import "strings"

const (
	esc = 0x1b // ESC
	bel = 0x07 // BEL, the classic (if non-standard) OSC terminator
	del = 0x7f

	// C1 control range, both as raw bytes and (defensively) as the
	// second byte of an invalid two-byte UTF-8 lead we choose to treat
	// the same way rather than pass through.
	c1Lo = 0x80
	c1Hi = 0x9f

	// The individual C1 controls this package gives ESC-prefixed
	// equivalents to. Everything else in [c1Lo, c1Hi] is a single
	// control byte with no string body and is simply dropped.
	c1CSI = 0x9b // same grammar as ESC '['
	c1OSC = 0x9d // same grammar as ESC ']'
	c1DCS = 0x90 // same grammar as ESC 'P'
	c1SOS = 0x98 // same grammar as ESC 'X'
	c1PM  = 0x9e // same grammar as ESC '^'
	c1APC = 0x9f // same grammar as ESC '_'
	c1ST  = 0x9c // string terminator; alone, it is a no-op to drop
)

// Table remaps the 16 classic ANSI SGR colors (indices 0-7 standard,
// 8-15 bright) onto replacement color indices. Table[i] is the index tool
// output's color i is redrawn as; Identity leaves every color where it
// found it.
type Table [16]int

// Identity is the zero-cost default palette: every color maps to itself.
// Wave 2's token layer plugs a real Table in here; until then every caller
// either omits the palette (Text) or passes Identity explicitly.
var Identity = Table{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}

// IsIdentity reports whether t performs no remapping. Callers holding a
// Table from settings/config can use this to skip TextWithPalette entirely
// and call Text instead — the two are behaviorally identical for an
// identity table, but this avoids even the one array-compare per call.
func (t Table) IsIdentity() bool { return t == Identity }

// Text returns s with every terminal-executable sequence neutralized,
// leaving SGR styling, plain text, and newlines/tabs untouched. Benign
// input — the overwhelming common case on the render hot path — costs one
// linear scan for an ESC or C1 byte and returns the original string with
// no allocation: the fast-path check lives directly in Text/TextWithPalette,
// not inside render, specifically so the common case never enters a function
// whose body declares a strings.Builder (see render's doc comment) and
// therefore never pays for one, even in the failure mode where the
// compiler's escape analysis can't prove that builder stack-allocatable.
func Text(s string) string {
	if !needsSanitize(s) {
		return s
	}
	return render(s, Identity)
}

// TextWithPalette sanitizes s exactly like Text and, in the same pass,
// remaps ANSI-16 SGR color codes through t. Passing Identity is exactly as
// cheap as calling Text.
func TextWithPalette(s string, t Table) string {
	if !needsSanitize(s) {
		return s
	}
	return render(s, t)
}

// needsSanitize is the fast-path scan: one pass over the bytes, memchr-
// style, checking for exactly the same things render's slow path acts on —
// an ESC byte, a genuine (lead-position) C1 control byte, DEL, or a C0
// control other than '\n'/'\t'. No regex, no allocation, no second pass:
// benign text (the overwhelming case on the render hot path) is ASCII/UTF-8
// with none of those present and returns here without ever reaching
// render's builder.
//
// It has to walk whole UTF-8 runes, not raw bytes, for the same reason
// render's default branch does: a byte in [c1Lo, c1Hi] is only a raw C1
// control when it appears at a rune's LEAD position. As a continuation byte
// of some other, perfectly ordinary codepoint — an em dash (U+2014, UTF-8
// E2 80 94 — the 0x80 is a continuation byte, not a control) is the common
// case that would trip a byte-blind check — it is just part of the
// character. A version of this scan that checked raw bytes without rune
// awareness was caught exactly this way: it silently sent ordinary text
// containing accented letters, em dashes, or emoji down the slow path on
// every call, which produced correct output (render's own scan is
// rune-aware) but defeated the fast path's entire purpose for a very
// common shape of real chat text. Caught by BenchmarkTextFastPath showing
// a nonzero alloc count against a seed string with exactly this em dash.
func needsSanitize(s string) bool {
	for i := 0; i < len(s); {
		b := s[i]
		switch {
		case b == esc:
			return true
		case b == del:
			return true
		case b < 0x20:
			if b != '\n' && b != '\t' {
				return true
			}
			i++
		case b < c1Lo: // ordinary ASCII, 0x20-0x7e
			i++
		case b <= c1Hi: // c1Lo <= b <= c1Hi at a lead position: a real C1 control
			return true
		default: // b >= 0xA0: the lead byte of a rune (or a malformed byte
			// DecodeRuneInString safely steps over) — skip it whole, exactly
			// like render's default branch, so its continuation bytes are
			// never independently mistaken for C1 controls.
			i += runeSize(s[i:])
		}
	}
	return false
}

// render is the single scanner behind Text and TextWithPalette, called only
// once they have already established (via needsSanitize) that s has
// something in it worth acting on — render itself does not re-check, so
// that its strings.Builder is declared, and can be escape-analyzed, only on
// a code path both callers already know is not the hot one. It walks s
// once, copying printable content (including whole multi-byte UTF-8 runes,
// so a continuation byte that happens to land in the C1 range is never
// mistaken for a control byte) and either stripping or rewriting every
// control construct it recognizes. It never indexes past len(s) and always
// advances, so it terminates on any input, however malformed.
func render(s string, t Table) string {
	var out strings.Builder
	out.Grow(len(s))
	i := 0
	for i < len(s) {
		b := s[i]
		switch {
		case b == esc:
			j, kept := scanEscape(s, i+1)
			if kept {
				writeSGR(&out, s[i:j], t)
			}
			i = j
		case b == c1CSI:
			// The 8-bit CSI introducer gets no SGR carve-out: only the
			// 7-bit ESC '[' form our own tooling and every mainstream
			// renderer actually emits is allowlisted. An 8-bit CSI is
			// exotic enough in practice that treating it as untrusted by
			// default, always, is the safer reading of "C1 controls."
			j, _ := scanCSIBody(s, i+1)
			i = j
		case b == c1OSC, b == c1DCS, b == c1SOS, b == c1PM, b == c1APC:
			i = scanStringBody(s, i+1)
		case b == c1ST:
			i++ // an orphan terminator with nothing open; drop it
		case b >= c1Lo && b <= c1Hi:
			i++ // any other single-byte C1 control
		case b == del:
			i++
		case b < 0x20:
			if b == '\n' || b == '\t' {
				out.WriteByte(b)
			}
			i++
		case b < 0x80:
			out.WriteByte(b)
			i++
		default:
			// b is >= 0xA0: either the lead byte of a valid multi-byte
			// UTF-8 rune (in which case DecodeRuneInString hands back the
			// whole rune, continuation bytes included, so they never get
			// individually mistaken for C1 controls) or a malformed byte
			// that DecodeRuneInString safely advances past by exactly
			// one. Either way i always moves forward.
			size := runeSize(s[i:])
			out.WriteString(s[i : i+size])
			i += size
		}
	}
	return out.String()
}
