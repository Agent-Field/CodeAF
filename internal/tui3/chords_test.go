package tui3

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// THE CHORDS, AS A PERSON ON EACH PLATFORM MEETS THEM.
//
// Three separate claims, and the tests are separate for the same reason the file
// is: what a chord is CALLED is a fact about the keycaps, what a terminal can
// SEND is a fact the terminal reports, and whether Option is meta is a fact only
// the arriving keystroke can betray. A test that mixed two of them would be a
// test that passed because the wrong half was right.

// ── the spelling table ──────────────────────────────────────────────────────

// envOf is one fixed environment as a [tokens.Env] closure, which is the shape
// every capability decision on this surface is a table test over
// ([tokens.DetectProfile], [tokens.DetectGlyphSet]).
func envOf(pairs map[string]string) func(string) string {
	return func(name string) string { return pairs[name] }
}

func TestTheChordSpellingIsOnePerPlatformAndTerminal(t *testing.T) {
	for _, tc := range []struct {
		name     string
		goos     string
		env      map[string]string
		meta     string
		terminal string
		setting  string
	}{
		{name: "linux says alt", goos: "linux", env: map[string]string{"TERM": "xterm-256color"},
			meta: "alt+"},
		{name: "windows says alt", goos: "windows", env: map[string]string{"WT_SESSION": "abc"},
			meta: "alt+"},
		{name: "freebsd says alt", goos: "freebsd", meta: "alt+"},
		{name: "a mac in iTerm2", goos: "darwin", env: map[string]string{"TERM_PROGRAM": "iTerm.app"},
			meta: chordMetaWord, terminal: "iTerm2", setting: "Profiles › Keys › Left Option: Esc+"},
		{name: "a mac in Terminal.app", goos: "darwin", env: map[string]string{"TERM_PROGRAM": "Apple_Terminal"},
			meta: chordMetaWord, terminal: "Terminal", setting: "Profiles › Keyboard › Use Option as Meta key"},
		{name: "a mac in kitty", goos: "darwin", env: map[string]string{"TERM": "xterm-kitty"},
			meta: chordMetaWord, terminal: "kitty", setting: "macos_option_as_alt yes"},
		{name: "a mac in kitty by its window id", goos: "darwin", env: map[string]string{"KITTY_WINDOW_ID": "3"},
			meta: chordMetaWord, terminal: "kitty", setting: "macos_option_as_alt yes"},
		{name: "a mac in ghostty", goos: "darwin", env: map[string]string{"TERM_PROGRAM": "ghostty"},
			meta: chordMetaWord, terminal: "ghostty", setting: "macos-option-as-alt = true"},
		{name: "a mac in wezterm", goos: "darwin", env: map[string]string{"TERM_PROGRAM": "WezTerm"},
			meta: chordMetaWord, terminal: "WezTerm", setting: "send_composed_key_when_left_alt_is_pressed = false"},
		{name: "a mac in alacritty", goos: "darwin", env: map[string]string{"ALACRITTY_WINDOW_ID": "7"},
			meta: chordMetaWord, terminal: "alacritty", setting: `option_as_alt = "Both"`},
		{name: "a mac in something nobody named", goos: "darwin", env: map[string]string{"TERM": "xterm-256color"},
			meta: chordMetaWord},
		{name: "a mac with no environment at all", goos: "darwin", meta: chordMetaWord},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var env func(string) string
			if tc.env != nil {
				env = envOf(tc.env)
			}
			got := detectChords(tc.goos, env)
			if got.meta != tc.meta {
				t.Fatalf("the modifier is called %q there and this says %q", tc.meta, got.meta)
			}
			if got.terminal != tc.terminal || got.setting != tc.setting {
				t.Fatalf("the terminal is %q/%q there and this says %q/%q",
					tc.terminal, tc.setting, got.terminal, got.setting)
			}
		})
	}
}

// A SENTENCE IS AUTHORED ONCE AND SPELLED AT THE DOOR. Every chord constant on
// this surface is written `alt+`, which is what the manual quotes and what Linux
// draws unchanged; the Mac's `opt+` is one substitution at the moment of drawing.
func TestTheSpellingDoorRewritesOnlyTheModifier(t *testing.T) {
	mac := chordSpelling{meta: chordMetaWord}
	got := mac.say(placeHintWords)
	if strings.Contains(got, chordAltWord) {
		t.Fatalf("a mac's hint still says alt+:\n%s", got)
	}
	for _, want := range []string{"opt+enter send it off as a task", "opt+. for the map", "tab next place"} {
		if !strings.Contains(got, want) {
			t.Fatalf("the mac spelling lost %q:\n%s", want, got)
		}
	}
	// AND EVERYWHERE ELSE IT IS THE SENTENCE ITSELF, byte for byte. A zero value
	// is the `alt+` spelling, which is what a surface built with no door draws.
	if plainSpelling := (chordSpelling{}).say(placeHintWords); plainSpelling != placeHintWords {
		t.Fatalf("the zero spelling changed the sentence:\n%s", plainSpelling)
	}
	if linux := detectChords("linux", nil).say(placeHintWords); linux != placeHintWords {
		t.Fatalf("linux changed the sentence:\n%s", linux)
	}
}

// ── every place, at both spellings ──────────────────────────────────────────

// THE WHOLE POINT OF ONE DOOR IS THAT NOTHING SLIPS PAST IT. So this walks all
// seven places and reads what a person actually reads on each — the hint under
// the composer, the map drawn over it, and the note slot — and asserts that on a
// Mac not one cell of any of them says `alt+`.
func TestEveryPlaceSpellsItsChordsTheWayThisTerminalDoes(t *testing.T) {
	a := placeApp(t)
	for _, id := range pages() {
		a.showPage(id)

		a.chords = chordSpelling{meta: chordAltWord}
		for what, line := range placeChordLines(a) {
			if strings.Contains(line, chordMetaWord) {
				t.Fatalf("%s's %s draws a mac keycap off a mac:\n%s", id.word(), what, line)
			}
		}

		a.chords = detectChords("darwin", envOf(map[string]string{"TERM_PROGRAM": "iTerm.app"}))
		for what, line := range placeChordLines(a) {
			if strings.Contains(line, chordAltWord) {
				t.Fatalf("%s's %s still says alt+ on a mac:\n%s", id.word(), what, line)
			}
		}
	}
}

// placeChordLines is every sentence about a key the standing place puts in front
// of a person, by the name of the slot it is drawn in.
func placeChordLines(a *app) map[string]string {
	was := a.mapShowing
	defer func() { a.mapShowing = was }()

	a.mapShowing = false
	out := map[string]string{"hint": a.placeHint()}
	a.mapShowing = true
	out["map"] = a.placeHint()
	a.mapShowing = false

	// The note slot with the option-as-meta line armed, which is the one row this
	// file puts there and the one a Mac must never see spelled `alt+`.
	wasLost := a.chordLost
	a.chordLost = true
	out["note"] = plain(strings.Join(a.placeNote(a.width), "\n"))
	a.chordLost = wasLost
	return out
}

// ── the second encoding ─────────────────────────────────────────────────────

// THE ALIAS IS THE TERMINAL'S OWN ANSWER AND NOT A GUESS. `ctrl+2` reaches a
// program only from a terminal that took the kitty keyboard protocol's
// disambiguation flag — and that terminal says so, which is the one honest
// signal available (chords.go, bargein.go's own note on the same law).
func TestTheCtrlDigitAliasIsClaimedOnlyWhereTheTerminalSaidItCan(t *testing.T) {
	a := placeApp(t)
	a.showPage(pageHome)

	a.keysDisambiguated = false
	a.placeKeyPress(ctrlKey('3'))
	if a.page != pageHome {
		t.Fatalf("ctrl+3 moved off home on a terminal that never said it can send it, to %s", a.page.word())
	}
	a.placeKeyPress(ctrlKey('.'))
	if a.mapShowing {
		t.Fatal("ctrl+. drew the map on a terminal that cannot send it")
	}

	a.keysDisambiguated = true
	a.placeKeyPress(ctrlKey('3'))
	if want := pages()[2]; a.page != want {
		t.Fatalf("ctrl+3 went to %s and the third place is %s", a.page.word(), want.word())
	}
	// AND IT IS THE SAME PLACE THE alt SPELLING REACHES. One digit reading behind
	// both encodings is what makes that true by construction.
	a.showPage(pageHome)
	a.placeKeyPress(tea.KeyPressMsg{Code: '3', Mod: tea.ModAlt})
	if want := pages()[2]; a.page != want {
		t.Fatalf("alt+3 went to %s and the third place is %s", a.page.word(), want.word())
	}
	a.showPage(pageHome)
	a.placeKeyPress(ctrlKey('.'))
	if !a.mapShowing {
		t.Fatal("ctrl+. drew no map on a terminal that reported it disambiguates")
	}
	// AND THE NEXT KEY PUTS IT AWAY, exactly as the alt spelling's does — the
	// alias is the same key, not a second one with its own state.
	a.placeKeyPress(ctrlKey('.'))
	if a.mapShowing {
		t.Fatal("ctrl+. twice left the map up")
	}
}

// NO KEY DOES ANYTHING THAT IS NOT DRAWN ON SCREEN RIGHT NOW (SCREEN 3a). The
// map is where the jump class is named, so the alias is named there exactly when
// it is bound — and never when it is not, which would be a phantom.
func TestTheMapNamesTheCtrlAliasExactlyWhenItIsBound(t *testing.T) {
	a := placeApp(t)
	a.showPage(pageHome)
	a.mapShowing = true

	a.keysDisambiguated = false
	if line := a.placeHint(); strings.Contains(line, "ctrl+1…8") {
		t.Fatalf("the map offers a chord this terminal cannot send:\n%s", line)
	}
	a.keysDisambiguated = true
	line := a.placeHint()
	if !strings.Contains(line, "alt+1…8 or ctrl+1…8 go to a place") {
		t.Fatalf("the map hides an alias that is bound:\n%s", line)
	}
	// AND ON A MAC IT IS THE MAC'S SPELLING OF THE FIRST AND THE PLAIN ONE OF THE
	// SECOND: `ctrl` is `ctrl` on every keyboard there is.
	a.chords = detectChords("darwin", envOf(map[string]string{"TERM_PROGRAM": "kitty"}))
	if mac := a.placeHint(); !strings.Contains(mac, "opt+1…8 or ctrl+1…8 go to a place") {
		t.Fatalf("the mac map reads wrong:\n%s", mac)
	}
}

// ── option as meta ──────────────────────────────────────────────────────────

// THE ONE NOTE, AND WHEN IT MAY BE SAID. A Mac whose Option key is composing
// accents sends `¡` where `alt+1` was aimed; one dim line names the setting, and
// a real chord retires it for good.
func TestTheOptionAsMetaNoteAppearsOnceAndNeverAfterARealChord(t *testing.T) {
	a := placeApp(t)
	a.chords = detectChords("darwin", envOf(map[string]string{"TERM_PROGRAM": "iTerm.app"}))
	a.showPage(pageHome)

	a.key(tea.KeyPressMsg{Code: '¡', Text: "¡"})
	if !a.chordLost {
		t.Fatal("a place met the character option composes for alt+1 and said nothing")
	}
	note := plain(strings.Join(a.placeNote(a.width), "\n"))
	want := `your terminal sends opt as a letter — turn on "use option as meta" in iTerm2: Profiles › Keys › Left Option: Esc+`
	if !strings.Contains(note, "use option as meta") || !strings.Contains(note, "iTerm2") {
		t.Fatalf("the note does not name the setting:\n%s\nwanted the words of\n%s", note, want)
	}
	// ONE LINE, NOT ONE PER KEYSTROKE.
	a.key(tea.KeyPressMsg{Code: '™', Text: "™"})
	a.key(tea.KeyPressMsg{Code: '≥', Text: "≥"})
	if rows := a.placeNote(a.width); len(rows) != 1 {
		t.Fatalf("three composed characters drew %d note rows and the note is one line", len(rows))
	}

	// AND A REAL CHORD ENDS IT FOREVER.
	a.key(tea.KeyPressMsg{Code: '1', Mod: tea.ModAlt})
	if a.chordLost {
		t.Fatal("a real alt chord arrived and the note about option not being meta stayed up")
	}
	a.showPage(pageHome)
	a.key(tea.KeyPressMsg{Code: '¡', Text: "¡"})
	if a.chordLost {
		t.Fatal("the note came back after this terminal had already delivered a real chord")
	}
}

// AND IT IS SAID NOWHERE ELSE. Off a Mac there is no setting to turn on, and in
// a conversation a character is a person writing a sentence.
func TestTheOptionAsMetaNoteIsNeitherDrawnOffAMacNorInAConversation(t *testing.T) {
	a := placeApp(t)
	a.chords = chordSpelling{meta: chordAltWord}
	a.showPage(pageHome)
	a.key(tea.KeyPressMsg{Code: '¡', Text: "¡"})
	if a.chordLost {
		t.Fatal("a linux terminal was told to turn on a macOS setting")
	}

	a.chords = detectChords("darwin", envOf(map[string]string{"TERM_PROGRAM": "iTerm.app"}))
	a.leavePlace()
	if a.pageShowing() {
		t.Fatal("the conversation half of this test needs no place standing")
	}
	a.key(tea.KeyPressMsg{Code: 'ø', Text: "ø"})
	if a.chordLost {
		t.Fatal("a letter typed into a conversation raised a note about the option key")
	}
}

// THE FIRST-RUN LINE SAYS THE SAME THING BEFORE ANYTHING HAS GONE WRONG, and it
// is a condition rather than a diagnosis, because nothing has been pressed yet.
func TestTheFirstRunLineNamesTheChordsAndTheSettingOnAMacOnly(t *testing.T) {
	mac := detectChords("darwin", envOf(map[string]string{"TERM_PROGRAM": "Apple_Terminal"}))
	words := mac.chordSetupWords()
	for _, want := range []string{"opt+1…opt+8", "if opt types a character instead", "use option as meta", "Terminal: Profiles › Keyboard › Use Option as Meta key"} {
		if !strings.Contains(words, want) {
			t.Fatalf("the first-run line lost %q:\n%s", want, words)
		}
	}
	if strings.Contains(words, chordAltWord) {
		t.Fatalf("the first-run line on a mac still says alt+:\n%s", words)
	}
	if other := detectChords("linux", nil).chordSetupWords(); other != "" {
		t.Fatalf("linux is told to turn on a macOS setting:\n%s", other)
	}
}

// AND THE TABLE IS THE CHORDS THIS SURFACE BINDS, which is what makes the note
// about somebody aiming at a key rather than about somebody typing a word.
func TestTheOptionCharacterTableIsExactlyTheBoundChords(t *testing.T) {
	for r, chord := range chordDeadKeys {
		switch {
		case strings.HasPrefix(chord, "alt+") && len(chord) == 5:
		default:
			t.Fatalf("%q is filed under %q, which is not a chord this surface binds", r, chord)
		}
	}
	// The seven places, the map, and the letters the composer and the two places
	// that have a view actually take.
	for _, chord := range []string{"alt+1", "alt+7", "alt+.", "alt+g", "alt+q", "alt+s", "alt+w", "alt+p", "alt+y", "alt+o", "alt+b", "alt+f"} {
		found := false
		for _, have := range chordDeadKeys {
			if have == chord {
				found = true
			}
		}
		if !found {
			t.Fatalf("%s is bound and its composed character is not in the table", chord)
		}
	}
	// AND A CHARACTER A PERSON MIGHT SIMPLY TYPE IS NOT IN IT. `é`, `ü` and `ñ`
	// are what accents are FOR, and a note fired by one of them would be the
	// surface talking over somebody's own words.
	for _, r := range []rune{'é', 'ü', 'ñ', 'å', '£' + 1} {
		if _, ok := chordDeadKeys[r]; ok && r != '£' {
			t.Fatalf("%q is read as a missed chord and it is a letter people type", r)
		}
	}
}
