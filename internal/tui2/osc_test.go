package tui2

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/sanitize"
)

// The payload filter is the thing standing between an error message somebody
// else's process wrote and an OSC string we author. Everything it removes is
// something that would end the string early and spill the rest into the frame.
func TestOSCTextRemovesWhatWouldBreakTheSequence(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"plain text passes through", "build failed", "build failed"},
		{"unicode passes through", "café — 3 workers ✓", "café — 3 workers ✓"},
		{"esc goes", "a\x1bb", "ab"},
		{"a whole SGR run goes", "a\x1b[31mred\x1b[0m", "a[31mred[0m"},
		{"bel goes", "ding\a", "ding"},
		{"newline and tab go", "one\ntwo\tthree", "onetwothree"},
		{"del goes", "a\x7fb", "ab"},
		{"a C1 control goes", "ab", "ab"},
		{"a raw C1 byte goes", "a\x9bb", "ab"},
		{"semicolon becomes a comma", "a;b", "a,b"},
		{"an OSC 52 clipboard write is defused", "\x1b]52;c;Zm9v\a", "]52,c,Zm9v"},
		{"invalid utf-8 is dropped", "a\xffb", "ab"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := oscText(tc.in, 200); got != tc.want {
				t.Fatalf("oscText(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// The bound is in runes and the cut never severs one, so the payload a terminal
// receives is always decodable.
func TestOSCTextBoundsLengthOnRuneBoundaries(t *testing.T) {
	got := oscText("ααααααα", 3)
	if got != "ααα" {
		t.Fatalf("truncated to %q, want %q", got, "ααα")
	}
	if !strings.ContainsRune(got, 'α') || len(got) != 6 {
		t.Fatalf("a rune was severed: %q is %d bytes", got, len(got))
	}
	if oscText("anything", 0) != "" {
		t.Fatal("a zero bound must yield nothing, not everything")
	}
}

// Benign text is the common case and must not be rebuilt. The em dash is the
// specific trap: a byte-blind scan reads its continuation bytes as C1 controls
// and sends ordinary chat text down the slow path forever (the same defect
// internal/sanitize's needsSanitize was caught by).
func TestOSCTextReturnsBenignTextWithoutAllocating(t *testing.T) {
	const in = "delivery ready — 4 files ✓"
	if got := oscText(in, 200); got != in {
		t.Fatalf("benign text was rewritten: %q", got)
	}
	allocs := testing.AllocsPerRun(100, func() {
		if oscText(in, 200) != in {
			t.Fatal("fast path changed the string")
		}
	})
	if allocs != 0 {
		t.Fatalf("the fast path allocated %.0f times", allocs)
	}
}

// The ladder, rung by rung, as bytes. This is the capability matrix: the same
// event on three terminals is three different writes and never a silent one.
func TestNotifyBytesPerTier(t *testing.T) {
	cases := []struct {
		tier  notifyTier
		title string
		body  string
		want  string
	}{
		{tier99, "aforge", "delivery ready", "\x1b]99;i=aforge:d=0:p=title;aforge\x07\x1b]99;i=aforge:d=1:p=body;delivery ready\x07"},
		{tier99, "aforge", "", "\x1b]99;i=aforge:p=title;aforge\x07"},
		{tier99, "", "just a body", "\x1b]99;i=aforge:p=body;just a body\x07"},
		{tier99, "", "", ""},
		{tier777, "aforge", "delivery ready", "\x1b]777;notify;aforge;delivery ready\x07"},
		{tier777, "aforge", "", "\x1b]777;notify;aforge\x07"},
		{tier777, "", "", ""},
		{tierBell, "aforge", "delivery ready", "\a"},
		{tierBell, "", "", "\a"},
	}
	for _, tc := range cases {
		got := notifyBytes(tc.tier, tc.title, tc.body)
		if got != tc.want {
			t.Fatalf("notifyBytes(%v, %q, %q) = %q, want %q", tc.tier, tc.title, tc.body, got, tc.want)
		}
	}
}

// A hostile title cannot climb out of the sequence it is inside. This is the
// half of 10.2.6 that faces the other way: the sanitizer stops somebody else's
// escapes reaching the screen, and this stops somebody else's escapes reaching
// the terminal THROUGH a sequence we authored.
func TestNotifyPayloadCannotEscapeTheSequence(t *testing.T) {
	hostile := "\x1b]52;c;cm0gLXJmIH4=\a and \x1b]0;pwned\a"
	for _, tier := range []notifyTier{tier99, tier777} {
		got := notifyBytes(tier, hostile, hostile)
		// Every ESC in the result must be one we wrote: the introducer of one
		// of our own OSCs. Nothing else may carry an ESC at all.
		for i := 0; i < len(got); i++ {
			if got[i] != 0x1b {
				continue
			}
			if i+1 >= len(got) || got[i+1] != ']' {
				t.Fatalf("tier %v: stray ESC at %d in %q", tier, i, got)
			}
			rest := got[i:]
			if !strings.HasPrefix(rest, "\x1b]99;") && !strings.HasPrefix(rest, "\x1b]777;") {
				t.Fatalf("tier %v: an OSC we did not author at %d in %q", tier, i, got)
			}
		}
		// BEL is our terminator; a payload-borne BEL would end a sequence early.
		if want := strings.Count(got, "\x1b]"); strings.Count(got, "\a") != want {
			t.Fatalf("tier %v: %d terminators for %d sequences in %q",
				tier, strings.Count(got, "\a"), want, got)
		}
	}
}

// The two-state progress channel, and the reason it has no third state: there
// is no denominator for a turn, so a percentage would be an estimate rendered
// as a fact (10.2.8).
func TestProgressBytes(t *testing.T) {
	if got, want := progressBytes(true), "\x1b]9;4;3\x07"; got != want {
		t.Fatalf("running = %q, want %q", got, want)
	}
	if got, want := progressBytes(false), "\x1b]9;4;0\x07"; got != want {
		t.Fatalf("idle = %q, want %q", got, want)
	}
}

func TestPromptMarkIsTheOneMark(t *testing.T) {
	if promptMarkBytes != "\x1b]133;A\x07" {
		t.Fatalf("prompt mark is %q", promptMarkBytes)
	}
}

func TestNotifyProbeIsAQueryAndNotANotification(t *testing.T) {
	if !strings.HasPrefix(notifyProbe, "\x1b]99;") {
		t.Fatalf("the probe is not an OSC 99: %q", notifyProbe)
	}
	if !strings.Contains(notifyProbe, "p=?") {
		t.Fatalf("the probe does not ask anything: %q", notifyProbe)
	}
	// A query with a payload would pop a notification on every start.
	if !strings.HasSuffix(notifyProbe, ";\x1b\\") {
		t.Fatalf("the probe carries a payload: %q", notifyProbe)
	}
}

func TestAttentionTitleStaysCalm(t *testing.T) {
	cases := []struct {
		app  string
		n    int
		want string
	}{
		{"aforge", 0, "aforge"},
		{"aforge", -3, "aforge"},
		{"aforge", 1, "aforge (1)"},
		{"aforge", 12, "aforge (12)"},
		{"aforge", 99, "aforge (99)"},
		{"aforge", 100, "aforge (99+)"},
		{"", 2, "aforge (2)"},
	}
	for _, tc := range cases {
		if got := attentionTitle(tc.app, tc.n); got != tc.want {
			t.Fatalf("attentionTitle(%q, %d) = %q, want %q", tc.app, tc.n, got, tc.want)
		}
	}
}

// OSC 8 degrades to the label, always, and never to a half-written sequence.
func TestLinkerDegrades(t *testing.T) {
	off := Linker{}
	if got := off.Wrap("navigate.rs", "file:///src/navigate.rs"); got != "navigate.rs" {
		t.Fatalf("a disabled linker linked: %q", got)
	}
	if off.Enabled() {
		t.Fatal("the zero linker claims to be enabled")
	}

	on := Linker{on: true}
	got := on.Wrap("navigate.rs", "file:///src/navigate.rs")
	want := "\x1b]8;;file:///src/navigate.rs\x07navigate.rs\x1b]8;;\x07"
	if got != want {
		t.Fatalf("Wrap = %q, want %q", got, want)
	}

	// A URI is never repaired. A link whose target we edited points somewhere
	// the caller did not name, which is the spoof OSC 8 is stripped for.
	for _, uri := range []string{"", "http://x\x1b]0;pwn\a", "http://x\ay", "http://x\ny", "http://" + strings.Repeat("x", oscURIMax)} {
		if got := on.Wrap("label", uri); got != "label" {
			t.Fatalf("Wrap(label, %q) = %q, want the plain label", uri, got)
		}
	}
	if got := on.Wrap("", "file:///x"); got != "" {
		t.Fatalf("an empty label produced %q", got)
	}
}

// THE SANITIZER ROUND TRIP (10.2.6). Two claims, proved against the real
// sanitizer rather than against a description of it:
//
//  1. Untrusted text still loses every OSC — including the ones this file
//     authors — so nothing downstream can mint a notification, a title change
//     or a link by writing bytes into a model reply.
//  2. Our chrome path does not go through the sanitizer, which is why our
//     sequences survive at all. The two facts are the same rule read from both
//     ends, and a change that broke either would fail here.
func TestSanitizerStripsEveryOSCWeAuthor(t *testing.T) {
	ours := map[string]string{
		"notify 99":    notifyBytes(tier99, "aforge", "delivery ready"),
		"notify 777":   notifyBytes(tier777, "aforge", "delivery ready"),
		"progress on":  progressBytes(true),
		"progress off": progressBytes(false),
		"prompt mark":  promptMarkBytes,
		"probe":        notifyProbe,
		"hyperlink":    Linker{on: true}.Wrap("navigate.rs", "file:///src/navigate.rs"),
	}
	for name, seq := range ours {
		if seq == "" {
			t.Fatalf("%s produced nothing to test", name)
		}
		// As untrusted content it is neutered: no ESC, no OSC introducer, no
		// BEL survives the chokepoint.
		clean := sanitize.Text(seq)
		if strings.ContainsRune(clean, 0x1b) || strings.ContainsRune(clean, 0x07) {
			t.Fatalf("%s survived sanitize.Text as %q", name, clean)
		}
		if strings.Contains(clean, "]99;") || strings.Contains(clean, "]9;4") ||
			strings.Contains(clean, "]133;") || strings.Contains(clean, "]777;") ||
			strings.Contains(clean, "]8;") {
			t.Fatalf("%s left its OSC body behind as %q", name, clean)
		}
	}

	// The visible half of the hyperlink rule: the label stays, the link goes.
	link := Linker{on: true}.Wrap("navigate.rs", "file:///src/navigate.rs")
	if got := sanitize.Text(link); got != "navigate.rs" {
		t.Fatalf("sanitized link = %q, want the bare label", got)
	}
}

// The reason the notifier cannot simply reuse sanitize.Text on its arguments,
// pinned as a test so nobody "simplifies" it into a security hole: the
// sanitizer keeps SGR, SGR contains ESC, and an ESC inside an OSC payload ends
// the OSC.
func TestSanitizeTextIsNotAnOSCPayloadFilter(t *testing.T) {
	styled := "\x1b[31mred\x1b[0m"
	if !strings.ContainsRune(sanitize.Text(styled), 0x1b) {
		t.Fatal("sanitize.Text stopped keeping SGR; revisit oscText's rationale")
	}
	if strings.ContainsRune(oscText(styled, 200), 0x1b) {
		t.Fatal("oscText kept an ESC")
	}
}
