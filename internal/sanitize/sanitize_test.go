package sanitize

import (
	"strings"
	"testing"
)

func TestTextBenignPassthrough(t *testing.T) {
	cases := []string{
		"",
		"plain text",
		"line one\nline two\n",
		"tab\tseparated\tvalues",
		"unicode: café, 日本語, emoji 🎉",
		"combining: é (e + acute accent)",
		"a string with 100% ordinary punctuation! @#$%^&*()",
		// A codepoint whose UTF-8 continuation byte lands inside the raw
		// C1 range (0x80-0x9F) purely by coincidence: U+0100 'Ā' encodes
		// as 0xC4 0x80. A byte-blind scanner would corrupt this.
		"ĀŁ-adjacent codepoints stay whole",
	}
	for _, s := range cases {
		if got := Text(s); got != s {
			t.Errorf("Text(%q) = %q, want unchanged (byte-identical)", s, got)
		}
	}
}

func TestTextPreservesSGR(t *testing.T) {
	cases := []string{
		"\x1b[31mred\x1b[0m",
		"\x1b[1;31mbold red\x1b[0m",
		"\x1b[38;5;196mtruecolor-ish 256\x1b[0m",
		"\x1b[38;2;255;0;0mtruecolor\x1b[0m",
		"prefix \x1b[32mgreen\x1b[39m suffix",
	}
	for _, s := range cases {
		if got := Text(s); got != s {
			t.Errorf("Text(%q) = %q, want SGR preserved unchanged", s, got)
		}
	}
}

func TestTextStripsOSC52Clipboard(t *testing.T) {
	// The canonical clipboard-hijack payload: OSC 52 with a base64 body,
	// BEL-terminated. Only the surrounding visible text should survive.
	hostile := "look at this: \x1b]52;c;aGVsbG8gd29ybGQ=\x07 neat right?"
	want := "look at this:  neat right?"
	if got := Text(hostile); got != want {
		t.Errorf("Text(OSC52) = %q, want %q", got, want)
	}
}

func TestTextStripsOSC52STTerminated(t *testing.T) {
	hostile := "before\x1b]52;c;aGVsbG8=\x1b\\after"
	want := "beforeafter"
	if got := Text(hostile); got != want {
		t.Errorf("Text(OSC52/ST) = %q, want %q", got, want)
	}
}

func TestTextStripsTitleRewrite(t *testing.T) {
	for _, osc := range []string{"0", "1", "2"} {
		hostile := "hi \x1b]" + osc + ";pwned\x07 bye"
		want := "hi  bye"
		if got := Text(hostile); got != want {
			t.Errorf("Text(OSC%s) = %q, want %q", osc, got, want)
		}
	}
}

func TestTextStripsHyperlinkOSC8(t *testing.T) {
	// OSC 8 policy: strip from untrusted input, leaving the visible text.
	// ESC ] 8 ; params ; uri ST  visible-text  ESC ] 8 ; ; ST
	hostile := "\x1b]8;;https://evil.example/phish\x1b\\click here\x1b]8;;\x1b\\"
	want := "click here"
	if got := Text(hostile); got != want {
		t.Errorf("Text(OSC8) = %q, want %q", got, want)
	}
}

func TestTextStripsMiscOSC(t *testing.T) {
	for _, osc := range []string{"7", "9", "133", "777", "99", "4;1;rgb:ff/00/00", "104"} {
		hostile := "x\x1b]" + osc + ";payload\x07y"
		want := "xy"
		if got := Text(hostile); got != want {
			t.Errorf("Text(OSC%s) = %q, want %q", osc, got, want)
		}
	}
}

func TestTextStripsDCSAPCPMSOS(t *testing.T) {
	cases := map[string]string{
		"a\x1bPq#0;2;0;0;0#1;2;100;100;100\x1b\\b": "ab", // DCS (sixel-ish)
		"a\x1b_payload\x1b\\b":                     "ab", // APC
		"a\x1b^privacy\x1b\\b":                     "ab", // PM
		"a\x1bXstart of string\x1b\\b":             "ab", // SOS
	}
	for in, want := range cases {
		if got := Text(in); got != want {
			t.Errorf("Text(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTextStripsRawC1Bytes(t *testing.T) {
	// Raw single-byte C1 forms, not ESC-prefixed.
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"C1 CSI", "a\x9b31mb", "ab"},
		{"C1 OSC", "a\x9d52;c;aGk=\x07b", "ab"},
		{"C1 DCS", "a\x90junk\x9cb", "ab"},
		{"C1 APC", "a\x9fjunk\x9cb", "ab"},
		{"C1 PM", "a\x9ejunk\x9cb", "ab"},
		{"C1 SOS", "a\x98junk\x9cb", "ab"},
		{"lone C1 ST", "a\x9cb", "ab"},
		{"other C1 (NEL)", "a\x85b", "ab"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Text(c.in); got != c.want {
				t.Errorf("Text(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestTextStripsNonSGRCSI(t *testing.T) {
	cases := map[string]string{
		"a\x1b[2Jb":     "ab", // clear screen
		"a\x1b[10;20Hb": "ab", // cursor position
		"a\x1b[?25lb":   "ab", // hide cursor (private mode)
		"a\x1b[Kb":      "ab", // erase in line
	}
	for in, want := range cases {
		if got := Text(in); got != want {
			t.Errorf("Text(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTextStripsSimpleEscapes(t *testing.T) {
	cases := map[string]string{
		"a\x1bcb":  "ab", // full reset
		"a\x1b7b":  "ab", // save cursor
		"a\x1b8b":  "ab", // restore cursor
		"a\x1b=b":  "ab", // application keypad
		"a\x1b(Bb": "ab", // charset designation (has an intermediate)
		"a\x1bMb":  "ab", // reverse index
	}
	for in, want := range cases {
		if got := Text(in); got != want {
			t.Errorf("Text(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTextStripsOtherC0Controls(t *testing.T) {
	cases := map[string]string{
		"a\rb":                        "ab", // bare CR
		"a\ab":                        "ab", // BEL
		"a\bb":                        "ab", // backspace
		"a\fb":                        "ab", // form feed
		"a\vb":                        "ab", // vertical tab
		"a" + string(rune(del)) + "b": "ab", // DEL
	}
	for in, want := range cases {
		if got := Text(in); got != want {
			t.Errorf("Text(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTextKeepsNewlineAndTab(t *testing.T) {
	in := "line1\n\tindented line2\n"
	if got := Text(in); got != in {
		t.Errorf("Text(%q) = %q, want unchanged", in, got)
	}
}

// TestOSC52RendersAsVisibleTextOnly is the specific regression the build
// brief asked for: a message body carrying an OSC 52 clipboard-write
// payload must render as its visible text only, never executing the
// clipboard write.
func TestOSC52RendersAsVisibleTextOnly(t *testing.T) {
	body := "Sure, here's the summary\x1b]52;c;" +
		"ZXZpbCBjbGlwYm9hcmQgcGF5bG9hZA==" + // base64("evil clipboard payload")
		"\x07 — hope that helps!"
	got := Text(body)
	if strings.Contains(got, "\x1b]52") {
		t.Fatalf("Text left an OSC 52 sequence in the output: %q", got)
	}
	if strings.Contains(got, "\x1b") {
		t.Fatalf("Text left an ESC byte in the output: %q", got)
	}
	want := "Sure, here's the summary — hope that helps!"
	if got != want {
		t.Fatalf("Text(OSC52 body) = %q, want %q", got, want)
	}
}

func TestTextMalformedInputNeverPanics(t *testing.T) {
	adversarial := []string{
		"\x1b",                           // lone trailing ESC
		"\x1b[",                          // truncated CSI, no final byte
		"\x1b[1;2;3",                     // truncated CSI with params, no final byte
		"\x1b]",                          // truncated OSC, no terminator
		"\x1b]52;c;",                     // truncated OSC mid-payload
		"\x1b]52;c;\x1b",                 // truncated OSC ending in a lone ESC
		"\x1bP",                          // truncated DCS
		"\x1b_",                          // truncated APC
		"\x1b^",                          // truncated PM
		"\x1bX",                          // truncated SOS
		"\x1b]8;;\x1b]52;c;aGk=\x07",     // nested/nonsense OSC inside OSC
		"\x1b\x1b\x1b\x1b\x1b",           // nothing but ESC bytes
		"\x9b",                           // lone C1 CSI introducer
		"\x9d",                           // lone C1 OSC introducer
		"\xff\xfe\xfd",                   // invalid UTF-8, no ESC/C1 at all... but is
		"\x80\x81\x82",                   // raw C1 range bytes back to back
		"\xc2",                           // truncated 2-byte UTF-8 lead
		"\xe2\x82",                       // truncated 3-byte UTF-8 lead
		"\xf0\x9f\x8e",                   // truncated 4-byte UTF-8 lead
		strings.Repeat("\x1b[", 5000),    // pathological CSI-entry storm
		strings.Repeat("\x1b]52;", 2000), // pathological OSC-entry storm
		"",
	}
	for _, s := range adversarial {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("Text(%q) panicked: %v", s, r)
				}
			}()
			_ = Text(s)
			_ = TextWithPalette(s, testShuffleTable())
		}()
	}
}

func TestTextHugeInput(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 200000; i++ {
		b.WriteString("plain ")
		if i%37 == 0 {
			b.WriteString("\x1b[31mred\x1b[0m")
		}
		if i%53 == 0 {
			b.WriteString("\x1b]52;c;aGk=\x07")
		}
	}
	in := b.String()
	got := Text(in)
	if strings.Contains(got, "\x1b]52") {
		t.Fatalf("huge input still contains an OSC 52 sequence")
	}
	if !strings.Contains(got, "\x1b[31m") {
		t.Fatalf("huge input lost its SGR sequences")
	}
}

func TestFastPathReturnsSameString(t *testing.T) {
	// For input with no ESC/C1 byte, Text must return the identical
	// string value (the fast path's whole point is no allocation).
	in := "nothing dangerous here, just words and\nnewlines\tand tabs"
	got := Text(in)
	if got != in {
		t.Fatalf("fast path changed benign text: got %q want %q", got, in)
	}
}

func testShuffleTable() Table {
	return Table{7, 6, 5, 4, 3, 2, 1, 0, 15, 14, 13, 12, 11, 10, 9, 8}
}

func TestPaletteIdentityIsNoOp(t *testing.T) {
	in := "\x1b[1;31mhello\x1b[0m \x1b[92mworld\x1b[49m"
	if got := TextWithPalette(in, Identity); got != Text(in) {
		t.Errorf("TextWithPalette(Identity) = %q, want same as Text = %q", got, Text(in))
	}
}

func TestPaletteRemapsStandardColors(t *testing.T) {
	table := Identity
	table[1] = 4 // red -> blue
	in := "\x1b[31mred text\x1b[0m"
	want := "\x1b[34mred text\x1b[0m"
	if got := TextWithPalette(in, table); got != want {
		t.Errorf("TextWithPalette = %q, want %q", got, want)
	}
}

func TestPaletteRemapsBackgroundKeepsFamily(t *testing.T) {
	table := Identity
	table[1] = 4 // red -> blue
	in := "\x1b[41mred bg\x1b[0m"
	want := "\x1b[44mred bg\x1b[0m"
	if got := TextWithPalette(in, table); got != want {
		t.Errorf("TextWithPalette = %q, want %q", got, want)
	}
}

func TestPaletteRemapAcrossBrightBoundary(t *testing.T) {
	table := Identity
	table[1] = 9 // red -> bright red
	in := "\x1b[31mred\x1b[0m"
	want := "\x1b[91mred\x1b[0m"
	if got := TextWithPalette(in, table); got != want {
		t.Errorf("TextWithPalette = %q, want %q", got, want)
	}

	table2 := Identity
	table2[9] = 1 // bright red -> red
	in2 := "\x1b[91mbright red\x1b[0m"
	want2 := "\x1b[31mbright red\x1b[0m"
	if got := TextWithPalette(in2, table2); got != want2 {
		t.Errorf("TextWithPalette = %q, want %q", got, want2)
	}
}

func TestPaletteLeavesExtendedColorsAlone(t *testing.T) {
	table := Identity
	table[1] = 4
	cases := []string{
		"\x1b[38;5;196mtool 256\x1b[0m",
		"\x1b[48;2;10;20;30mtool truecolor bg\x1b[0m",
		"\x1b[1;38;5;9;4mstyled\x1b[0m",
	}
	for _, in := range cases {
		if got := TextWithPalette(in, table); got != in {
			t.Errorf("TextWithPalette(%q) = %q, want unchanged (extended colors out of scope)", in, got)
		}
	}
}

func TestPaletteLeavesNonColorSGRAlone(t *testing.T) {
	table := testShuffleTable()
	in := "\x1b[1;4;31;7mstyled\x1b[0m"
	got := TextWithPalette(in, table)
	if !strings.Contains(got, "1;") || !strings.Contains(got, "4;") || !strings.Contains(got, ";7m") {
		t.Errorf("TextWithPalette(%q) = %q, lost a non-color SGR attribute", in, got)
	}
}

func TestIsIdentity(t *testing.T) {
	if !Identity.IsIdentity() {
		t.Error("Identity.IsIdentity() = false, want true")
	}
	custom := Identity
	custom[3] = 9
	if custom.IsIdentity() {
		t.Error("modified table reports IsIdentity() = true")
	}
}
