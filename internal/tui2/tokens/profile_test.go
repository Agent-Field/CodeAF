package tokens

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// env builds an Env from a map so the detection table below is a table and not
// a fixture. DetectProfile takes its reader as a parameter precisely so this
// file never touches os.Setenv and never has to run serially.
func env(pairs map[string]string) Env {
	return func(name string) string { return pairs[name] }
}

// TestDetectProfile is 10.1.2's governing rule made executable: COLORTERM is
// never trusted on its own, because inside a multiplexer it is the
// multiplexer's claim about itself.
func TestDetectProfile(t *testing.T) {
	cases := []struct {
		name string
		envs map[string]string
		want Profile
	}{
		{"nil environment is no colour", nil, NoColor},
		{"no TERM", map[string]string{}, NoColor},
		{"TERM=dumb", map[string]string{"TERM": "dumb"}, NoColor},
		{"NO_COLOR wins over every capability claim",
			map[string]string{"NO_COLOR": "1", "TERM": "xterm-256color", "COLORTERM": "truecolor"}, NoColor},
		{"a -direct terminfo entry is the one truecolor claim accepted alone",
			map[string]string{"TERM": "xterm-direct"}, TrueColor},
		{"a -direct entry is believed even inside tmux",
			map[string]string{"TERM": "tmux-direct", "TMUX": "/tmp/tmux-1000/default"}, TrueColor},
		{"COLORTERM is capped to 256 inside tmux",
			map[string]string{"TERM": "screen-256color", "TMUX": "/tmp/x", "COLORTERM": "truecolor"}, ANSI256},
		{"COLORTERM is capped to 256 under a screen TERM with no TMUX",
			map[string]string{"TERM": "screen", "COLORTERM": "truecolor"}, ANSI256},
		{"Apple_Terminal is capped to 256",
			map[string]string{"TERM": "xterm-256color", "TERM_PROGRAM": "Apple_Terminal", "COLORTERM": "truecolor"}, ANSI256},
		{"COLORTERM=truecolor outside a multiplexer",
			map[string]string{"TERM": "xterm-256color", "COLORTERM": "truecolor"}, TrueColor},
		{"COLORTERM=24bit outside a multiplexer",
			map[string]string{"TERM": "xterm", "COLORTERM": "24bit"}, TrueColor},
		{"TERM naming 256", map[string]string{"TERM": "xterm-256color"}, ANSI256},
		{"anything else with a TERM", map[string]string{"TERM": "xterm"}, ANSI16},
		{"TERM=linux", map[string]string{"TERM": "linux"}, ANSI16},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var e Env
			if c.envs != nil {
				e = env(c.envs)
			}
			if got := DetectProfile(e); got != c.want {
				t.Errorf("DetectProfile = %s, want %s", got, c.want)
			}
		})
	}
}

// TestProfileDegradationIsOrdered: the profiles are ordered by capability so
// `p >= ANSI256` is a meaningful question, and the two capability answers that
// hang off that ordering must agree with it.
func TestProfileDegradationIsOrdered(t *testing.T) {
	if !(NoColor < ANSI16 && ANSI16 < ANSI256 && ANSI256 < TrueColor) {
		t.Fatal("profiles are not ordered by capability")
	}
	for p := Profile(0); p < profileCount; p++ {
		if got, want := p.IdentityDistinct(), p >= ANSI256; got != want {
			t.Errorf("%s.IdentityDistinct() = %v", p, got)
		}
		if got, want := p.SelectionStyle() == SelectionBand, p >= ANSI256; got != want {
			t.Errorf("%s.SelectionStyle() = %v", p, got)
		}
		if p.String() == "invalid" {
			t.Errorf("profile %d has no name", p)
		}
	}
	// At 16 colours the eight identity hues collapse onto six chromatic slots,
	// which is WHY IdentityDistinct is false there. Prove the collapse is real
	// rather than assumed, so the promise and the data cannot drift.
	seen := map[uint8]int{}
	for i := range IdentityCount {
		seen[Identity(i).Index(ANSI16, FocusNormal)]++
	}
	if len(seen) == IdentityCount {
		t.Error("the identity wheel no longer collapses at 16 colours; IdentityDistinct should be revisited")
	}
	// The band tint cannot survive 256 either, and BandTintDistinct says so.
	tints := map[uint8]bool{}
	for i := range IdentityCount {
		tints[BandFor(Identity(i)).Index(ANSI256, FocusNormal)] = true
	}
	if got := len(tints) > 1; got != ANSI256.BandTintDistinct() {
		t.Errorf("BandTintDistinct(256) = %v but %d distinct tinted bands resolve there",
			ANSI256.BandTintDistinct(), len(tints))
	}
}

// TestIdentityWheelSurvives256 is the promise [Profile.IdentityDistinct] makes,
// checked against the resolved indices rather than against intent. The xterm
// cube is coarse in this palette's pastel corner, and a plain nearest-neighbour
// walk collapses identity.1 onto GREEN and identity.2 onto CYAN — a task accent
// drawn in the colour that means "success". buildTable's two-pass resolution
// exists to prevent exactly that, and this is the test that would notice if it
// stopped working.
func TestIdentityWheelSurvives256(t *testing.T) {
	for _, f := range []Focus{FocusNormal, FocusDimmed} {
		// Distinct from each other.
		seen := map[uint8]Token{}
		for i := range IdentityCount {
			tok := Identity(i)
			idx := tok.Index(ANSI256, f)
			if other, dup := seen[idx]; dup {
				t.Errorf("%s and %s both resolve to 256-index %d at %s focus", other, tok, idx, f)
			}
			seen[idx] = tok
		}
		// Distinct from every hue that carries MEANING, which is the failure
		// that actually misinforms a reader.
		for _, s := range []Token{Amber, Cyan, Green, Coral, TextPrimary, TextSecondary, TextTertiary} {
			for i := range IdentityCount {
				if Identity(i).Index(ANSI256, f) == s.Index(ANSI256, f) {
					t.Errorf("identity %d resolves to the same 256-index as %s at %s focus", i, s, f)
				}
			}
		}
		// And visibly apart, not merely numerically apart.
		for i := range IdentityCount {
			for j := i + 1; j < IdentityCount; j++ {
				a := color256(int(Identity(i).Index(ANSI256, f)))
				b := color256(int(Identity(j).Index(ANSI256, f)))
				if d := distance(a, b); d < minSeparation256 {
					t.Errorf("identity %d and %d are %d apart at %s focus, below the %d floor",
						i, j, d, f, minSeparation256)
				}
			}
		}
		// The wheel keeps its colourfulness: an identity that degraded to grey
		// would still be distinct and would no longer be an identity.
		for i := range IdentityCount {
			want := chroma(Identity(i).Color(f))
			got := chroma(color256(int(Identity(i).Index(ANSI256, f))))
			if got*2 < want {
				t.Errorf("identity %d degrades from chroma %d to %d at %s focus", i, want, got, f)
			}
		}
	}
}

// TestSGRSequencesAreValidAndPrecomputed: every escape string the table holds
// must be a real SGR sequence that a terminal will consume without printing
// anything. Measuring them with the same ANSI-aware ruler the renderer uses
// proves they add zero cells — the invariant blocks' Styler contract depends
// on.
func TestSGRSequencesAreValidAndPrecomputed(t *testing.T) {
	for _, tok := range All() {
		for p := Profile(0); p < profileCount; p++ {
			for _, f := range []Focus{FocusNormal, FocusDimmed} {
				for _, seq := range []string{tok.Fg(p, f), tok.Bg(p, f)} {
					if p == NoColor {
						if seq != "" {
							t.Errorf("%s under NoColor emitted %q", tok, seq)
						}
						continue
					}
					if !strings.HasPrefix(seq, "\x1b[") || !strings.HasSuffix(seq, "m") {
						t.Errorf("%s/%s/%s = %q is not an SGR sequence", tok, p, f, seq)
						continue
					}
					if w := ansi.StringWidth(seq); w != 0 {
						t.Errorf("%s/%s/%s = %q measures %d cells; painting must not change width", tok, p, f, seq, w)
					}
					if got := ansi.Strip(seq); got != "" {
						t.Errorf("%s/%s/%s leaves printable bytes %q", tok, p, f, got)
					}
				}
			}
		}
	}
	for _, p := range []Profile{NoColor, ANSI16, ANSI256, TrueColor} {
		for _, seq := range []string{ResetFg(p), ResetBg(p), Reset(p), Reverse(p)} {
			if p == NoColor && seq != "" {
				t.Errorf("NoColor emitted %q", seq)
			}
			if ansi.StringWidth(seq) != 0 {
				t.Errorf("%q measures more than zero cells", seq)
			}
		}
	}
}

// TestSGRStringsAreStable: the table is built once, so two reads must return
// the identical string — not an equal one. A render that formatted a sequence
// per frame would still pass an equality check and fail this one.
func TestSGRStringsAreStable(t *testing.T) {
	a, b := Amber.Fg(TrueColor, FocusNormal), Amber.Fg(TrueColor, FocusNormal)
	if a != b {
		t.Fatal("the SGR table is not stable")
	}
	if a != "\x1b[38;2;238;206;150m" {
		t.Errorf("amber truecolor foreground = %q", a)
	}
	if got := Amber.Fg(ANSI256, FocusNormal); got != "\x1b[38;5;222m" {
		t.Errorf("amber 256 foreground = %q", got)
	}
	if got := Amber.Fg(ANSI16, FocusNormal); got != "\x1b[93m" {
		t.Errorf("amber 16 foreground = %q", got)
	}
	if got := Band.Bg(ANSI256, FocusNormal); !strings.HasPrefix(got, "\x1b[48;5;") {
		t.Errorf("a background must use the 48 family, got %q", got)
	}
}

// TestIndex256AvoidsTheThemedSlots: indices 0-15 are whatever the user's theme
// says they are. Mapping a token onto one would hand our palette to a stranger,
// which is the exact failure this layer exists to end.
func TestIndex256AvoidsTheThemedSlots(t *testing.T) {
	for _, tok := range All() {
		for _, f := range []Focus{FocusNormal, FocusDimmed} {
			if idx := tok.Index(ANSI256, f); idx < 16 {
				t.Errorf("%s/%s resolves to 256-index %d, inside the theme-owned range", tok, f, idx)
			}
		}
	}
	// The 256 approximation must stay close enough to be the same colour. A
	// nearest-cube entry more than a visible step away would make the
	// degradation look like mud rather than like the palette.
	//
	// The identity wheel gets a wider budget than everything else, and the
	// difference is the design decision made in nearestDistinct256: an identity
	// accent spends LIGHTNESS accuracy to keep its HUE and its distinctness,
	// because a task accent that has drifted a shade is still that task and one
	// that has turned grey — or turned green — is not.
	for _, tok := range All() {
		budget := 1.25
		if _, isIdentity := IdentityIndex(tok); isIdentity && !tok.IsSurface() {
			budget = 1.5
		}
		for _, f := range []Focus{FocusNormal, FocusDimmed} {
			want := tok.Color(f)
			got := color256(int(tok.Index(ANSI256, f)))
			if ratio := Contrast(want, got); ratio > budget {
				t.Errorf("%s/%s degrades from %s to %s, a contrast step of %.2f (budget %.2f)",
					tok, f, want.Hex(), got.Hex(), ratio, budget)
			}
		}
	}
}

// TestParseProfile is the override door. This package mints no environment pin
// of its own — capability is a decided value the shell passes down — so the
// only thing to get right here is that a recognized spelling is obeyed and an
// unrecognized one is refused rather than guessed at (5.20).
func TestParseProfile(t *testing.T) {
	for _, c := range []struct {
		in   string
		want Profile
	}{
		{"none", NoColor}, {"NONE", NoColor}, {" off ", NoColor}, {"0", NoColor},
		{"16", ANSI16}, {"ansi", ANSI16},
		{"256", ANSI256},
		{"truecolor", TrueColor}, {"24bit", TrueColor}, {"RGB", TrueColor},
	} {
		got, ok := ParseProfile(c.in)
		if !ok || got != c.want {
			t.Errorf("ParseProfile(%q) = %s,%v; want %s,true", c.in, got, ok, c.want)
		}
	}
	for _, in := range []string{"", "bright", "yes", "24", "color"} {
		if _, ok := ParseProfile(in); ok {
			t.Errorf("ParseProfile(%q) claimed to understand it", in)
		}
	}
	// Every profile round-trips through its own name, so a settings row and a
	// log line cannot disagree about what a value is called.
	for p := Profile(0); p < profileCount; p++ {
		got, ok := ParseProfile(p.String())
		if !ok || got != p {
			t.Errorf("%s does not round-trip: got %s,%v", p, got, ok)
		}
	}
}
