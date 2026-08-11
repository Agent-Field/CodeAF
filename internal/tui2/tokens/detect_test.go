package tokens

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// TestDetectGlyphSet is the veto ladder as a table, mirroring TestDetectProfile
// — which is the whole reason detection is a pure function over an [Env]
// closure rather than something that reads the process environment.
func TestDetectGlyphSet(t *testing.T) {
	cases := []struct {
		name string
		envs map[string]string
		want GlyphSet
		why  string
	}{
		{"nil environment", nil, Plain, glyphReasonNoEnv},
		{"no TERM", map[string]string{}, Plain, glyphReasonNoTerm},
		{"TERM=dumb", map[string]string{"TERM": "dumb"}, Plain, glyphReasonNoTerm},
		{"the Linux console cannot draw private use at all",
			map[string]string{"TERM": "linux"}, Plain, glyphReasonLinuxTTY},
		{"Terminal.app ships no patched font",
			map[string]string{"TERM": "xterm-256color", "TERM_PROGRAM": "Apple_Terminal"}, Plain, glyphReasonAppleTerm},
		{"a Japanese locale vetoes on the ambiguous-width fact",
			map[string]string{"TERM": "xterm-256color", "LANG": "ja_JP.UTF-8"}, Plain, glyphReasonCJKLocale},
		{"a Chinese locale in LC_ALL",
			map[string]string{"TERM": "xterm-256color", "LC_ALL": "zh_CN.UTF-8"}, Plain, glyphReasonCJKLocale},
		{"a Korean locale in LC_CTYPE",
			map[string]string{"TERM": "xterm-256color", "LC_CTYPE": "ko_KR.UTF-8"}, Plain, glyphReasonCJKLocale},
		{"LC_ALL wins the locale ladder, so a C override lifts the veto",
			map[string]string{"TERM": "xterm-256color", "LC_ALL": "C", "LANG": "ja_JP.UTF-8"}, NerdFont, glyphReasonDefault},
		{"a legacy Windows console",
			map[string]string{"TERM": "xterm", "MSYSTEM": "MINGW64"}, Plain, glyphReasonLegacyConIn},
		{"Windows Terminal says so itself and is not vetoed",
			map[string]string{"TERM": "xterm-256color", "MSYSTEM": "MINGW64", "WT_SESSION": "abc"}, NerdFont, glyphReasonDefault},
		{"ConEmu says so itself and is not vetoed",
			map[string]string{"TERM": "xterm-256color", "MSYSTEM": "MINGW64", "ConEmuANSI": "ON"}, NerdFont, glyphReasonDefault},

		// The non-vetoes, each stated as a case so a future edit has to argue
		// with a test rather than with a comment.
		{"tmux is NOT a veto: the font belongs to the outer terminal",
			map[string]string{"TERM": "tmux-256color", "TMUX": "/tmp/tmux-1000/default"}, NerdFont, glyphReasonDefault},
		{"screen is NOT a veto either",
			map[string]string{"TERM": "screen-256color"}, NerdFont, glyphReasonDefault},
		{"NO_COLOR is about colour and says nothing about a font",
			map[string]string{"TERM": "xterm-256color", "NO_COLOR": "1"}, NerdFont, glyphReasonDefault},
		{"an English locale is not a veto",
			map[string]string{"TERM": "xterm-256color", "LANG": "en_US.UTF-8"}, NerdFont, glyphReasonDefault},
		{"a Japanese-looking prefix on another language is not a veto",
			map[string]string{"TERM": "xterm-256color", "LANG": "jam_NG"}, NerdFont, glyphReasonDefault},

		// The weak positives buy nothing, because the default is already on.
		{"WezTerm names a terminal, never a font",
			map[string]string{"TERM": "xterm-256color", "TERM_PROGRAM": "WezTerm"}, NerdFont, glyphReasonDefault},
		{"kitty names a terminal, never a font",
			map[string]string{"TERM": "xterm-kitty", "KITTY_WINDOW_ID": "1"}, NerdFont, glyphReasonDefault},
		{"the plain case", map[string]string{"TERM": "xterm-256color"}, NerdFont, glyphReasonDefault},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var e Env
			if c.envs != nil {
				e = env(c.envs)
			}
			got, why := DetectGlyphSet(e)
			if got != c.want {
				t.Errorf("DetectGlyphSet = %s, want %s (reason %q)", got, c.want, why)
			}
			if why != c.why {
				t.Errorf("reason = %q, want %q", why, c.why)
			}
		})
	}
}

// TestDetectionOnlyVetoes is the law stated as a test rather than as a comment:
// no environment may TURN THE TIER ON, because no environment can see a font. A
// signal that flipped Plain to NerdFont would be this package claiming to know
// something it cannot know, and the failure mode of that claim is tofu on a
// user's screen.
func TestDetectionOnlyVetoes(t *testing.T) {
	vetoed := map[string]string{"TERM": "linux"}
	for _, positive := range []string{
		"TERM_PROGRAM", "KITTY_WINDOW_ID", "WEZTERM_EXECUTABLE",
		"GHOSTTY_RESOURCES_DIR", "ALACRITTY_WINDOW_ID", "LC_TERMINAL", "COLORTERM",
	} {
		envs := map[string]string{}
		for k, v := range vetoed {
			envs[k] = v
		}
		envs[positive] = "WezTerm"
		if got, why := DetectGlyphSet(env(envs)); got != Plain {
			t.Errorf("%s lifted a veto (%s): detection may only veto, never confirm", positive, why)
		}
	}
}

// TestGlyphReasonsAreSentences: the reason is written into chat.log so a
// support question ends in a grep rather than in a guess, which only works
// while every reason says what it saw and why that decided anything.
func TestGlyphReasonsAreSentences(t *testing.T) {
	seen := map[string]bool{}
	for _, r := range []string{
		glyphReasonNoEnv, glyphReasonNoTerm, glyphReasonLinuxTTY, glyphReasonAppleTerm,
		glyphReasonCJKLocale, glyphReasonLegacyConIn, glyphReasonDefault,
	} {
		if len(strings.Fields(r)) < 4 {
			t.Errorf("%q is not a reason, it is a label", r)
		}
		if seen[r] {
			t.Errorf("two causes share the reason %q, so the log cannot tell them apart", r)
		}
		seen[r] = true
	}
}

// TestGlyphProbeLineIsWidthStable: the probe line is the one line rendered in
// both tiers side by side, so if anything in this package must measure the same
// in both, it is this. It is also the settings sheet's live preview, where the
// two are drawn one above the other and a width difference would be visible as
// a ragged edge.
func TestGlyphProbeLineIsWidthStable(t *testing.T) {
	plain := GlyphProbeLine(Plain)
	nf := GlyphProbeLine(NerdFont)
	if plain == nf {
		t.Fatal("the two tiers render the same probe line, so the probe asks nothing")
	}
	if a, b := ansi.StringWidth(plain), ansi.StringWidth(nf); a != b {
		t.Errorf("probe widths differ under the grapheme ruler: plain %d, nerdfont %d", a, b)
	}
	if a, b := ansi.StringWidthWc(plain), ansi.StringWidthWc(nf); a != b {
		t.Errorf("probe widths differ under wcwidth: plain %d, nerdfont %d", a, b)
	}
	if got := GlyphProbeLine(GlyphSet(200)); got != plain {
		t.Errorf("an out-of-range tier probed %q, want the plain floor", got)
	}
}
