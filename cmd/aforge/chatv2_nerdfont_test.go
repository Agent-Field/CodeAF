package main

import (
	"flag"
	"log"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

func nerdFontFlagSet() (*flag.FlagSet, nerdFontFlags) {
	flags := flag.NewFlagSet("test", flag.ContinueOnError)
	return flags, registerNerdFont(flags)
}

// TestNerdFontFlagsSayWhatWasTyped: the two spellings of one switch, and the
// asymmetry between them. Both readings are natural to type, and the one that
// turns the tier OFF wins a contradiction, because the two wrong answers are
// not equally bad — one fills a screen with boxes.
func TestNerdFontFlagsSayWhatWasTyped(t *testing.T) {
	cases := []struct {
		args   []string
		value  bool
		chosen bool
	}{
		{nil, false, false},
		{[]string{"--nerd-font"}, true, true},
		{[]string{"--nerd-font=true"}, true, true},
		{[]string{"--nerd-font=false"}, false, true},
		{[]string{"--no-nerd-font"}, false, true},
		{[]string{"--no-nerd-font=false"}, false, false},
		{[]string{"--nerd-font", "--no-nerd-font"}, false, true},
	}
	for _, c := range cases {
		flags, choice := nerdFontFlagSet()
		if err := flags.Parse(c.args); err != nil {
			t.Fatalf("%v: %v", c.args, err)
		}
		value, chosen := choice.typed(flags)
		if value != c.value || chosen != c.chosen {
			t.Errorf("%v: typed = %v, %v; want %v, %v", c.args, value, chosen, c.value, c.chosen)
		}
	}
}

// TestGlyphSetDecision walks the whole ladder of 12.7 E.1, including the two
// overrides that sit above an explicit flag because they are correctness and
// not taste.
func TestGlyphSetDecision(t *testing.T) {
	patched := func(string) string { return "" }
	linuxConsole := func(name string) string {
		if name == "TERM" {
			return "linux"
		}
		return ""
	}
	good := func(name string) string {
		if name == "TERM" {
			return "xterm-256color"
		}
		return ""
	}

	t.Run("linear mode outranks even an explicit flag", func(t *testing.T) {
		t.Setenv("AFORGE_NERD_FONT", "")
		set, why := glyphSetDecision(true, true, true, t.TempDir(), good)
		if set != tokens.Plain {
			t.Fatalf("linear mode did not force the plain tier: %s (%s)", set, why)
		}
	})

	t.Run("a typed flag outranks the environment and the veto", func(t *testing.T) {
		t.Setenv("AFORGE_NERD_FONT", "off")
		set, _ := glyphSetDecision(true, true, false, t.TempDir(), linuxConsole)
		if set != tokens.NerdFont {
			t.Fatalf("--nerd-font lost to a pin or a veto: %s", set)
		}
		set, _ = glyphSetDecision(false, true, false, t.TempDir(), good)
		if set != tokens.Plain {
			t.Fatalf("--no-nerd-font did not turn the tier off: %s", set)
		}
	})

	t.Run("the pin outranks the veto", func(t *testing.T) {
		t.Setenv("AFORGE_NERD_FONT", "1")
		set, why := glyphSetDecision(false, false, false, t.TempDir(), linuxConsole)
		if set != tokens.NerdFont {
			t.Fatalf("AFORGE_NERD_FONT=1 lost to the veto: %s (%s)", set, why)
		}
		t.Setenv("AFORGE_NERD_FONT", "0")
		if set, _ := glyphSetDecision(false, false, false, t.TempDir(), good); set != tokens.Plain {
			t.Fatalf("AFORGE_NERD_FONT=0 did not turn the tier off: %s", set)
		}
	})

	t.Run("the veto answers when nobody chose", func(t *testing.T) {
		t.Setenv("AFORGE_NERD_FONT", "")
		set, why := glyphSetDecision(false, false, false, t.TempDir(), linuxConsole)
		if set != tokens.Plain {
			t.Fatalf("the Linux console did not veto: %s", set)
		}
		if why == "" {
			t.Error("a veto with no reason is a veto nobody can debug")
		}
	})

	t.Run("a malformed pin is not a choice and does not suppress the veto", func(t *testing.T) {
		t.Setenv("AFORGE_NERD_FONT", "sure")
		if set, _ := glyphSetDecision(false, false, false, t.TempDir(), linuxConsole); set != tokens.Plain {
			t.Fatal("an unparseable pin was read as a choice")
		}
	})

	t.Run("on by default", func(t *testing.T) {
		t.Setenv("AFORGE_NERD_FONT", "")
		set, why := glyphSetDecision(false, false, false, t.TempDir(), patched)
		// An empty environment has no TERM, which is itself a veto, so the
		// default case needs a terminal that claims something.
		if set != tokens.Plain {
			t.Fatalf("an environment with no TERM must not draw icons: %s", set)
		}
		if set, _ = glyphSetDecision(false, false, false, t.TempDir(), good); set != tokens.NerdFont {
			t.Fatalf("the tier is on by default: %s (%s)", set, why)
		}
	})
}

// TestGlyphSetDecisionReadsThePersistedRow closes the ladder's third rung
// against the real registry, the way resolveLinear's test does: the persisted
// choice has to come through untouched when nothing louder was said.
func TestGlyphSetDecisionReadsThePersistedRow(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AFORGE_NERD_FONT", "")
	if set, _ := glyphSetDecision(false, false, false, dir, func(name string) string {
		if name == "TERM" {
			return "xterm-256color"
		}
		return ""
	}); set != tokens.NerdFont {
		t.Fatalf("an untouched profile does not default on: %s", set)
	}
}

// TestResolveGlyphSetWritesItsReason: the reason is the whole reason the
// resolver returns one. It goes into the chat.log the entry point has already
// opened, so a user asking "why is it drawing boxes" is answered by a grep.
func TestResolveGlyphSetWritesItsReason(t *testing.T) {
	t.Setenv("AFORGE_NERD_FONT", "")
	t.Setenv("AFORGE_PROFILE_DIR", t.TempDir())
	t.Setenv("TERM", "linux")

	var out strings.Builder
	previous := log.Writer()
	log.SetOutput(&out)
	log.SetFlags(0)
	defer log.SetOutput(previous)

	flags, choice := nerdFontFlagSet()
	if err := flags.Parse(nil); err != nil {
		t.Fatal(err)
	}
	if set := resolveGlyphSet(flags, choice, false); set != tokens.Plain {
		t.Fatalf("the Linux console did not veto: %s", set)
	}
	line := out.String()
	if !strings.Contains(line, "plain") || !strings.Contains(line, "TERM=linux") {
		t.Errorf("the log line does not say what happened or why: %q", line)
	}
}
