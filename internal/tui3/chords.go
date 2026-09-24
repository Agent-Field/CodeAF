package tui3

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// ── ONE SPELLING OF THE CHORDS, AND WHAT THIS TERMINAL CAN ACTUALLY SEND ─────
//
// The owner's ask was "ensure you properly have alt or ctrl etc according to
// windows or mac", and the honest half of the answer has to be said first: A
// TUI CANNOT SEE WHETHER OPTION IS META. It sees what the emulator sent. On a
// Mac, `opt+1` reaches this program as escape-then-`1` when the profile says
// "option as meta" and as the single character `¡` when it does not, and there
// is no query that asks which. So this file does three separate things and
// never pretends one of them is another:
//
//  1. SPELLING. What the chord is CALLED on screen. On macOS the modifier's
//     name is `opt+` and everywhere else it is `alt+`, which is a fact about the
//     platform's keycaps and needs no capability at all. [chordSpelling.say] is
//     the one door, and every sentence a person reads about a chord goes
//     through it.
//  2. A SECOND ENCODING, WHERE THE TERMINAL SAYS IT HAS ONE. `ctrl+1` has no
//     legacy encoding — which is why SCREEN 3a chose `alt` in the first place —
//     but a terminal that took the kitty keyboard protocol's disambiguation
//     flag sends exactly the keys that have no legacy encoding as CSI-u, and
//     THAT terminal answers a query and says so ([app.keysDisambiguated]). So
//     the alias is bound off the terminal's own reply and never off a guess.
//  3. THE ONE NOTE, ON macOS, WHEN THE CHARACTER ARRIVES INSTEAD OF THE CHORD.
//     `¡ ™ £ ¢ ∞ § ¶ ≥ © œ ß ∑ ø ∫ ƒ` are what the US layout produces from the
//     chords this surface binds, and a person typing one of them on a place is
//     a person whose Option key is composing accents. One dim line names the
//     setting, and a single real `alt+` chord retires it for the session.
//
// WHY THE CAPABILITY QUESTION IS NOT AN ENVIRONMENT TABLE. tui2's
// [tokens.DetectGlyphSet] may only VETO, because no terminal reports its font;
// here a terminal DOES report whether it disambiguates, Bubble Tea asks on
// every frame, and bargein.go already spends that answer on `ctrl+shift+enter` for
// this exact reason. A veto table over TERM_PROGRAM would be this file guessing
// at a fact it can simply be told, and it would guess wrong in both directions:
// kitty behind an unlisted TERM would lose an alias it can send, and a terminal
// behind a multiplexer that strips CSI-u would be promised one it cannot.
//
// The environment is read for the two things nothing answers back about: which
// emulator is running, and what its option-as-meta setting is called.

// chordSpelling is how ONE terminal spells the chord classes and what it can send.
// The zero value is the `alt+` spelling with no alias and no terminal named,
// which is every surface built without a door — and is exactly right, because
// `alt+` is what the constants already say.
type chordSpelling struct {
	// meta is what `alt+` is called on this platform's keycaps: "" or "alt+"
	// everywhere but macOS, where it is "opt+".
	meta string
	// terminal is the emulator's own name, for the option-as-meta note, and ""
	// where the environment named nothing we can give a setting for.
	terminal string
	// setting is where that terminal's "use option as meta" lives, spelled as
	// that terminal spells it — a menu path for the two that have menus and a
	// config line for the four that have config files.
	setting string
}

// The two spellings, and the one substring every chord constant on this surface
// is written with. A sentence is authored in the `alt+` spelling — which is what
// the manual quotes and what a test greps for — and [chordSpelling.say] is what
// turns it into the other one at the moment it is drawn.
const (
	chordAltWord = "alt+"
	// chordMetaWord is what the same modifier is called on a Mac, and it is the
	// WORD rather than the keycap's glyph `⌥`.
	//
	// EVERY MODIFIER ON A MAC KEYCAP CARRIES BOTH A WORD AND A GLYPH — Control is
	// `control` and `⌃`, Option is `option` and `⌥`, Command is `command` and `⌘`
	// — so "what the keycap says" does not on its own choose between them, and
	// this surface was picking differently for different keys: `ctrl+`, the
	// keycap's word abbreviated, beside `⌥`, the keycap's glyph. Nothing
	// predicted which one a reader would get. `opt+` is `ctrl+`'s own move made
	// twice: the keycap word, cut short, joined with `+`.
	//
	// AND IT COSTS NOTHING TO MEASURE, which is what makes it safe to draw in
	// every ladder on the surface: `opt+` is four cells, exactly as `alt+` is, so
	// every line that fits the chord on a Linux box fits it on a Mac. The glyph
	// was one cell by [ansi.StringWidth] and NOT reliably one cell on screen —
	// U+2325 is East-Asian Ambiguous, which a terminal set to draw ambiguous
	// characters wide renders in two, and a font without the codepoint
	// substitutes at a width nobody measured. Every other mark a person sees is
	// reached through `tokens.GlyphSet.Glyph` for that exact reason; this one was
	// a literal that bypassed it, and a word needs no repertoire at all.
	chordMetaWord = "opt+"
	// chordMetaBare is the same key NAMED ON ITS OWN rather than in front of
	// another: `if opt types a character instead`, not `if opt+ types one`. The
	// trailing `+` is a joiner and a sentence that keeps it is a sentence with a
	// dangling operator in it.
	chordMetaBare = "opt"
	// chordCmdWord is THE SECOND MODIFIER WITH TWO KEYCAPS, and it is here for
	// the first one's reason exactly. The send-and-wait chord is authored
	// `cmd+enter` (steer.go's [parkKey]) because that is what it is called on the
	// keyboard it was chosen for — and until this wave that literal was drawn on
	// every platform, so the key sheet on a Linux box named a modifier that
	// keyboard does not have. The keystroke itself arrives as `super+enter` or
	// `meta+enter` there, which is why steer.go binds both names; this is the
	// half a person READS.
	chordCmdWord = "cmd+"
	// chordCmdGlyph is what a Mac's keycap says, and chordSuperWord is what every
	// other keyboard says on the same key — the Windows key, the Super key, the
	// one with a diamond on it.
	chordCmdGlyph  = "⌘"
	chordSuperWord = "super+"
	// chordCtrlWord is the SECOND encoding's prefix, spelled once for the same
	// reason: it is `ctrl+` on every keyboard there is — a Mac's Control key wears
	// the same word — so it has one spelling and no platform reading at all.
	chordCtrlWord = "ctrl+"
)

// detectChords is the spelling and the terminal, decided once at boot from the
// same two facts every other adaptation on this surface reads: the operating
// system, and the environment the shell handed down.
//
// It takes the [tokens.Env] closure rather than reading os.Getenv itself, for
// [tokens.DetectProfile]'s reason exactly — a decision table is a table test
// rather than a fixture — and it takes GOOS as a string for the same reason.
func detectChords(goos string, env tokens.Env) chordSpelling {
	if goos != "darwin" {
		// EVERYWHERE ELSE THE KEYCAP SAYS ALT AND ALT IS ALREADY META. Linux
		// terminals, Windows Terminal and the WSL consoles all send escape-then-key
		// with no setting to turn on, so there is nothing to spell differently and
		// nothing to warn anybody about.
		return chordSpelling{meta: chordAltWord}
	}
	name, setting := chordTerminal(env)
	return chordSpelling{meta: chordMetaWord, terminal: name, setting: setting}
}

// chordTerminal is which emulator this is and what its option-as-meta setting
// is called there.
//
// EACH SIGNAL SAYS WHICH TERMINAL IS RUNNING AND NOTHING ELSE, which is the one
// question it can answer honestly — [tokens.DetectGlyphSet]'s note refuses these
// same variables because it is asking them about a FONT, which they do not know.
// The name of a menu item in a program is a property of the program.
//
// An unrecognised terminal answers "", and the note says "your terminal's
// keyboard settings" rather than naming a setting that may not exist.
func chordTerminal(env tokens.Env) (name, setting string) {
	if env == nil {
		return "", ""
	}
	term := strings.ToLower(strings.TrimSpace(env("TERM")))
	switch strings.TrimSpace(env("TERM_PROGRAM")) {
	case "iTerm.app":
		return "iTerm2", "Profiles › Keys › Left Option: Esc+"
	case "Apple_Terminal":
		return "Terminal", "Profiles › Keyboard › Use Option as Meta key"
	case "ghostty":
		return "ghostty", "macos-option-as-alt = true"
	case "WezTerm":
		return "WezTerm", "send_composed_key_when_left_alt_is_pressed = false"
	}
	switch {
	case strings.TrimSpace(env("KITTY_WINDOW_ID")) != "" || strings.Contains(term, "kitty"):
		return "kitty", "macos_option_as_alt yes"
	case strings.TrimSpace(env("GHOSTTY_RESOURCES_DIR")) != "" || strings.Contains(term, "ghostty"):
		return "ghostty", "macos-option-as-alt = true"
	case strings.TrimSpace(env("WEZTERM_EXECUTABLE")) != "" || strings.TrimSpace(env("WEZTERM_PANE")) != "":
		return "WezTerm", "send_composed_key_when_left_alt_is_pressed = false"
	case strings.TrimSpace(env("ALACRITTY_WINDOW_ID")) != "" || strings.Contains(term, "alacritty"):
		return "alacritty", `option_as_alt = "Both"`
	}
	return "", ""
}

// say is THE ONE DOOR EVERY PERSON-FACING SENTENCE ABOUT A CHORD GOES THROUGH.
// The constants stay authored in the `alt+` spelling — that is what the manual
// quotes, what the gates grep for, and what a Linux terminal draws unchanged —
// and this is where a Mac's `opt+` is substituted in, once, at the moment of
// drawing.
//
// IT IS A SUBSTITUTION AND NOT A TABLE OF CHORDS, deliberately. A table would
// have to list `alt+1`…`alt+7`, `alt+.`, `alt+enter`, `alt+g`, `alt+q`, `alt+s`,
// `alt+w`, `alt+o`, `alt+b` and `alt+f`, and the first chord a lane added
// without touching it would be the first chord spelled two ways on one screen.
// The modifier is the only part that differs, so the modifier is the only part
// this knows about.
// IT SUBSTITUTES TWO MODIFIERS AND NOT ONE. `alt+` is the class the places are
// built on; `cmd+` is the one chord on the send (steer.go), and it is spelled
// here for the same reason and through the same door — a sentence that named a
// modifier straight out of a constant was the defect on both.
//
// ONLY ONE OF THE TWO CHANGES WIDTH. `alt+` and `opt+` are four cells each, so
// the Mac reading of an `alt+` chord measures exactly as the Linux one does;
// `cmd+` becomes the single cell `⌘`. That is why every caller still has to say
// this BEFORE it measures rather than on the way to the paint.
func (c chordSpelling) say(sentence string) string {
	if c.meta == chordMetaWord {
		sentence = strings.ReplaceAll(sentence, chordAltWord, c.meta)
	}
	return strings.ReplaceAll(sentence, chordCmdWord, c.cmdWord())
}

// cmdWord is what the send modifier is CALLED on this terminal's keycaps: `⌘` on
// a Mac and `super+` everywhere else.
//
// THE ZERO VALUE ANSWERS `super+`, which is right rather than merely safe: a
// spelling nobody detected is a spelling built without a door, and every
// keyboard that is not a Mac's wears the same word on this key. The Mac reading
// is the one that has to be discovered, exactly as `opt+` is.
func (c chordSpelling) cmdWord() string {
	if c.meta == chordMetaWord {
		return chordCmdGlyph
	}
	return chordSuperWord
}

// placeHint is the line under the composer, IN THIS TERMINAL'S OWN SPELLING.
// pages.go builds the sentence — which place is standing, whether the map or a
// strip is up, what the layer has taken — and this is the single line that
// reads the spelling table on the way out.
func (a *app) placeHint() string { return a.chords.say(a.placeHintSaid()) }

// ── the second encoding ─────────────────────────────────────────────────────

// chordCtrlJumpWords is the alias clause the map grows where the terminal can
// send it, and [chordSpelling.mapLine] is the only thing that adds it.
const chordCtrlJumpWords = " or " + chordCtrlWord + "1…8"

// chordJumpWords is the map's own name for the jump class, and it is spelled
// here so [chordSpelling.mapLine] and [placeMapWords] cannot drift apart about
// where the alias clause goes.
const chordJumpWords = chordAltWord + "1…8"

// chordMapAlias is `alt+.`'s second encoding, on the same terms as the digits:
// live only where the terminal answered the keyboard query.
//
// IT MEANS SOMETHING ELSE IN A CONVERSATION, and that is not a collision. In the
// conversation `ctrl+.` is every task this project has run (`/history`); a place
// has taken the whole frame and never reaches that switch, so the two never
// contend for one keystroke on one screen. The manual's own page about chords
// that mean more than one thing has the table.
const chordMapAlias = chordCtrlWord + "."

// ctrlDigits is whether `ctrl+1`…`ctrl+7` and `ctrl+.` can arrive at all, and
// it is THE TERMINAL'S OWN ANSWER rather than anybody's guess.
//
// `ctrl+<digit>` has no legacy encoding — SCREEN 3a chose `alt` for exactly that
// reason, and the manual said for a long while that the chord "cannot be" bound.
// What changed is not the encoding but who can be asked: a terminal that took
// the kitty keyboard protocol's disambiguation flag sends the keys with no
// legacy encoding as CSI-u, and it REPORTS having taken it. Bubble Tea asks on
// every frame and hands the reply back as a tea.KeyboardEnhancementsMsg; that
// reply is [app.keysDisambiguated], and bargein.go already spends it on
// `ctrl+shift+enter` under the same law.
//
// THE ONE DIRECTION IT ERRS IN IS THE SAFE ONE, as it is there: a terminal that
// speaks modifyOtherKeys and not the kitty protocol never replies, so the alias
// is absent where it might have worked. The reverse — a chord drawn on the map
// that the terminal will never deliver — is the phantom this whole surface's key
// law exists to forbid.
func (a *app) ctrlDigits() bool { return a.keysDisambiguated }

// mapLine is the map's chord list with the alias named where it is real. The map
// is the one surface that has to carry it: SCREEN 3a's clause is that no key does
// anything that is not drawn on screen right now, so a bound alias nobody is told
// about would break the same law as an unbound one that is advertised.
func (c chordSpelling) mapLine(base string, alias bool) string {
	if !alias {
		return base
	}
	return strings.Replace(base, chordJumpWords, chordJumpWords+chordCtrlJumpWords, 1)
}

// chordCtrlDigit is `ctrl+1`…`ctrl+7`, read exactly as [placeDigit] reads the
// `alt+` spelling — one function behind both, so the two encodings can never
// disagree about which position is which place.
func chordCtrlDigit(key string) (page, bool) { return placeDigitAt(chordCtrlWord, key) }

// ── option as meta, and the one line that says so ───────────────────────────

// chordDeadKeys is what a Mac's US layout produces from the chords this surface
// binds, when Option is composing accents instead of being meta.
//
// IT IS EXACTLY THE BOUND CHORDS AND NOTHING ELSE. A wider rule — any non-ASCII
// printable arriving where a chord was expected — catches more terminals and
// costs a person typing `café` or `größe` into a filter a line about a setting
// they have no reason to change. These sixteen characters are what somebody
// AIMING AT A CHORD produces, which is the only population the note is for. The
// cost that remains is honest and small: a Danish `ø` or a German `ß` typed into
// a place's composer draws one dim line, once, that the next real chord retires.
var chordDeadKeys = map[rune]string{
	'¡': "alt+1", '™': "alt+2", '£': "alt+3", '¢': "alt+4",
	'∞': "alt+5", '§': "alt+6", '¶': "alt+7", '•': "alt+8",
	'≥': "alt+.",
	'©': "alt+g", 'œ': "alt+q", 'ß': "alt+s",
	'π': "alt+p", '¥': "alt+y",
	'∑': "alt+w", 'ø': "alt+o", '∫': "alt+b", 'ƒ': "alt+f",
	// AND THE ROSTER'S OWN HAND-OFF, which took this modifier when ctrl+t became
	// the new-tab chord (task.go's [railHoldChord]). Option+t composes to `†` on
	// a US layout exactly as Option+w composes to `∑`, so the chord belongs in
	// this table for the same reason its neighbour does.
	'†': "alt+t",
	// AND THE SWITCHER, which took this modifier when `ctrl+k` went back to the
	// draft (hop.go's [hopOpenKey]). Option+k composes to `˚`.
	'˚': hopOpenKey,
}

// chordAnywhereDeadKey is the ONE character in [chordDeadKeys] that arms the note
// in a conversation as well as on a place, and it is the switcher's.
//
// THE RULE UNDER IT IS "where is the chord bound", not "where is the table
// convenient". Every other chord the table knows is a place's own — the jump,
// the map, the views — and none of them does anything in a conversation, so a
// `ø` typed there is a person writing a sentence and the note would be noise
// ([app.chordWatch] states that gate). The switcher is the exception in the
// surface and therefore here: it is drawn over the conversation and over every
// place alike, so a Mac whose Option is composing accents loses it in the one
// screen people spend their hours in — and would, without this, never be told
// why. `˚` is not a rune English prose reaches for, which is what makes the
// exception cheap.
const chordAnywhereDeadKey = '˚'

// chordWatch is the whole of the check, read at the top of the key router so
// both roads feed it — the places, where the note is drawn, and the conversation,
// where a real chord can still settle the question for good.
//
// ONE REAL CHORD ENDS IT FOREVER. A person whose Option key is already meta may
// still type `ø` into a filter, and a line telling them to turn on a setting
// they have on is the surface being wrong out loud. So the first `alt+` anything
// records that this terminal delivers them, drops the note if it was up, and
// nothing arms it again for the life of the process.
func (a *app) chordWatch(msg tea.KeyPressMsg) {
	if a.chords.meta != chordMetaWord || a.chordReal {
		// Not a Mac's spelling, or the question is already settled. Everywhere
		// else Alt is meta with no setting to turn on, so there is nothing to say.
		return
	}
	if key := msg.String(); strings.HasPrefix(key, chordAltWord) && !chordSynthesised(key) {
		a.chordReal, a.chordLost = true, false
		return
	}
	// AND ONLY WHILE A PLACE IS STANDING, WITH ONE CHARACTER EXCEPTED. The chords
	// the table is built from are the places' own — the jump, the map, the views
	// — and a character typed into a conversation is a person writing a sentence,
	// not a person missing a room. The switcher is the one chord bound in the
	// conversation too, so its own dead key is read there as well
	// ([chordAnywhereDeadKey] states the rule and why it is the only one).
	here := a.pageShowing()
	for _, r := range msg.Key().Text {
		if _, ok := chordDeadKeys[r]; !ok || !here && r != chordAnywhereDeadKey {
			continue
		}
		a.chordLost = true
		a.touch()
		return
	}
}

// chordSynthesised is the two `alt+` chords that say NOTHING about whether the
// option key is meta, because a key mapping can send them with option composing
// accents exactly as before.
//
// iTerm2's Natural Text Editing preset — the commonest Mac profile there is —
// maps Option+← to the escape sequence `esc b` and Option+→ to `esc f`, which arrive here
// as `alt+b` and `alt+f` and are indistinguishable from the real chords. A
// terminal doing that delivers the WORD JUMPS perfectly and still types `¡` for
// Option+1, because a mapping was written for the two arrows and not for the digits.
//
// SO THEY MAY NOT RETIRE THE NOTE. [app.chordWatch]'s bargain is that one real
// `alt+` chord proves this terminal sends the class and settles the question for
// the session — and these two do not prove it. Left in, they settled it wrongly
// on exactly the profile most people have: the first word jump of the session
// silenced the line that would have explained why the places do not answer.
func chordSynthesised(key string) bool {
	return key == chordAltWord+"b" || key == chordAltWord+"f"
}

// chordFixWords is the REMEDY half of the sentence — what to turn on and where —
// spelled once because two surfaces say it after two different diagnoses: the
// place's note says it after a character arrived instead of a chord, and the
// first-run setup says it before anything has been pressed at all.
func (c chordSpelling) chordFixWords() string {
	where := "your terminal's keyboard settings"
	if c.terminal != "" {
		where = c.terminal + ": " + c.setting
	}
	return `turn on "use option as meta" in ` + where
}

// chordOptionWords is the one sentence a place says, and it is the same sentence
// the manual's own page about alt on macOS quotes.
func (c chordSpelling) chordOptionWords() string {
	return "your terminal sends " + chordMetaBare + " as a letter — " + c.chordFixWords()
}

// chordShortWords is the SAME DIAGNOSIS IN THE CELLS THE LEGEND CAN SPARE, and
// it exists because the switcher moved onto this modifier (hop.go's
// [hopOpenKey]). Until then every chord the dead-key table knew was a place's
// own, so the long sentence had a place's note slot to live in and the
// conversation needed no line at all. Now one of them is bound in the
// conversation too, and a flag armed there that only ever drew somewhere else
// would be machinery that half works — which this codebase would rather not
// have than have.
//
// IT KEEPS THE REMEDY AND SPENDS THE DIAGNOSIS, which is the opposite of
// [app.chordNote]'s trade and right for the opposite reason. The note has room
// for both halves and drops the remedy when it does not; this slot has room for
// one clause, and a person reading a hint slot in a conversation has just
// pressed the chord and watched a character land in their message — they have
// the diagnosis in front of them already. What they do not have is the words
// "option as meta" to go looking for.
func (c chordSpelling) chordShortWords() string {
	return chordMetaBare + " types a letter · turn on option as meta"
}

// chordNote is that sentence as a row of the place's note slot: one dim line,
// under the rule and above the composer, where every other fact about the whole
// body is drawn (placebodies.go's [app.placeNote]).
func (a *app) chordNote(width int) string {
	// THE LINE IS macOS'S AND THE GUARD IS STRUCTURAL. [app.chordWatch] arms this
	// only on a Mac already; saying so here too is what makes it impossible for a
	// Linux terminal to be handed a sentence about a menu it does not have, however
	// a later lane comes to set the flag.
	if !a.chordLost || a.chords.meta != chordMetaWord || width < 4 {
		return ""
	}
	// AND IT DROPS ITS REMEDY WHOLE RATHER THAN SLICING IT. The sentence is a
	// diagnosis with the fix hung off a dash — `your terminal sends alt as a
	// letter — turn on "use option as meta" in Terminal: Profiles → Keyboard` —
	// and a character ruler took sixty columns straight through the middle of the
	// quoted menu item, so the line named a setting and then ate it. [noteFit] is
	// the statement fitter — this row is a note and not a key sheet — and it takes
	// the dash clause whole, leaving the diagnosis, which is the half that tells a
	// person the chords are not broken rather than the terminal misconfigured.
	return " " + a.pal.dim(noteFit(a.chords.chordOptionWords(), width-2))
}

// chordSetupWords is what the first-run setup leaves behind on a Mac, and it is
// the one place on this surface that says the sentence BEFORE anything has gone
// wrong — which is why it is written as a condition rather than as a diagnosis.
//
// A first run cannot know whether Option is meta: nothing has been pressed yet.
// What it can do is name the chords and say what to do if they type a character
// instead, at the one moment a person is being told how the program works.
func (c chordSpelling) chordSetupWords() string {
	if c.meta != chordMetaWord {
		return ""
	}
	return "the eight places answer " + chordMetaWord + "1…" + chordMetaWord +
		"8 · if " + chordMetaBare + " types a character instead, " + c.chordFixWords()
}
