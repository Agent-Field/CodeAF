package main

import (
	"flag"
	"log"
	"os"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// The glyph tier's launch door (12.7 E.1), kept in its own file for the same
// reason chatv2_linear.go is: the assembly wave is editing chatv2.go, and a
// seam that lives beside the thing it seams does not collide with it.
//
// The tier's settings row landed in internal/config (KeyNerdFont, NerdFontAt,
// AFORGE_NERD_FONT). This file is the ladder that decides which tier THIS
// invocation renders in, and the one line that writes the reason into chat.log
// — so "why is it drawing boxes" ends in a grep rather than in a guess.

// nerdFontFlags are the two spellings of the same switch. Two flags rather than
// one because both readings are natural to type — `--no-nerd-font` is what a
// user reaches for when the screen is full of boxes, and `--nerd-font=false` is
// what someone who read the help reaches for — and neither should be the one
// that does not work.
type nerdFontFlags struct {
	on  *bool
	off *bool
}

// registerNerdFont adds the two flags to a command's flag set. Both are boolean
// and neither takes a value, so [reorder] needs no entry for them.
func registerNerdFont(flags *flag.FlagSet) nerdFontFlags {
	return nerdFontFlags{
		on: flags.Bool("nerd-font", true,
			"draw chrome with Nerd Font icons; --nerd-font=false for the plain glyphs"),
		off: flags.Bool("no-nerd-font", false,
			"draw chrome with the plain glyphs (the same marks, no patched font needed)"),
	}
}

// typed reports the tier the command line asked for, and whether it asked at
// all. A flag typed now outranks a variable exported once, which is the rule
// wantChatV2 already states.
//
// If both flags are typed, off wins. That is not a coin toss: the two answers
// are not symmetrical, because guessing "on" wrongly fills a screen with boxes
// and guessing "off" wrongly costs a user some icons they can turn back on.
func (f nerdFontFlags) typed(flags *flag.FlagSet) (bool, bool) {
	value, chosen := false, false
	forcedOff := false
	flags.Visit(func(fl *flag.Flag) {
		switch fl.Name {
		case "nerd-font":
			value, chosen = *f.on, true
		case "no-nerd-font":
			if *f.off {
				forcedOff = true
			}
		}
	})
	if forcedOff {
		return false, true
	}
	return value, chosen
}

// glyphSetDecision is the resolution ladder as a pure function, so the
// precedence is a table test rather than a launch.
//
// Highest first:
//
//  1. Linear mode, unconditionally, above even an explicit flag. 10.1.5's
//     accessible rendering exists for screen readers, and a screen reader reads
//     a private-use codepoint as nothing or as garbage. A tier that made the
//     accessible mode LESS accessible would be the affordance lying (5.20).
//  2. --nerd-font / --no-nerd-font / --nerd-font=false typed on this command
//     line.
//  3. AFORGE_NERD_FONT, then the persisted nerd_font row — the registry
//     resolves those two in that order and says which one answered.
//  4. The terminal veto (tokens.DetectGlyphSet), which may only ever turn the
//     tier OFF, because no environment variable can see a font.
//  5. On, which is the default.
func glyphSetDecision(typed bool, chosen bool, linear bool, profileDir string, env tokens.Env) (tokens.GlyphSet, string) {
	if linear {
		return tokens.Plain, "linear mode renders the plain glyphs: a screen reader reads a private-use codepoint as nothing"
	}
	if chosen {
		if typed {
			return tokens.NerdFont, "--nerd-font on this command line"
		}
		return tokens.Plain, "--no-nerd-font on this command line"
	}
	if value, source := config.NerdFontChosenAt(profileDir); source != config.NerdFontSourceNone {
		if value {
			return tokens.NerdFont, "on: " + source
		}
		return tokens.Plain, "off: " + source
	}
	if set, why := tokens.DetectGlyphSet(env); set != tokens.NerdFont {
		return set, "vetoed by the terminal — " + why
	}
	return tokens.NerdFont, "on by default (no flag, no setting, no veto)"
}

// resolveGlyphSet is [glyphSetDecision] against the real environment, and it
// writes the reason to the log the caller has already pointed at chat.log —
// which is why it belongs after that redirection and not beside the parse.
func resolveGlyphSet(flags *flag.FlagSet, choice nerdFontFlags, linear bool) tokens.GlyphSet {
	typed, chosen := choice.typed(flags)
	set, why := glyphSetDecision(typed, chosen, linear,
		strings.TrimSpace(os.Getenv("AFORGE_PROFILE_DIR")), os.Getenv)
	log.Printf("chat v2: glyph tier %s — %s", set, why)
	return set
}
