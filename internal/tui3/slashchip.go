package tui3

import "strings"

// THE COMMAND CHIP: a slash command does not look like a word.
//
// "/task" is not a noun in a sentence, and until this wave it was drawn as one
// — the same ink as the words either side of it, in the box and in the message
// after it was sent. A person who typed "/tsak" found out it was not a command
// from the answer, one keystroke too late, and a person who typed "/task" into
// the middle of a sentence had no way at all to tell that this surface even
// knew the word.
//
// So a RECOGNIZED command wears a background: the same tint the cursor's row
// wears elsewhere, behind exactly the cells the token already occupies
// ([palette.chip]). It is the smallest mark that says "this is a specifier, not
// prose" — no brackets, no border, nothing added to the line.
//
// TWO LAWS HOLD THE WHOLE THING UP.
//
// The first is that A CHIP ADDS NO CELLS. It is a repaint of the runes that are
// already there and never a space of padding around them, because the composer
// counts the caret's column off the draft's own runes (input.go's
// [caretColumnIn]): a chip a cell wider than its token would put the caret in
// the wrong place on every row that held one.
//
// The second is that ONLY A COMMAND THIS SURFACE RUNS GETS ONE. The word is
// resolved through the one command table, aliases included ([canonicalCommand]),
// so "/task" and "/clear" are chipped and "/tsak" and "/Users" stay plain text.
// A chip on a word this surface would answer with "unknown command: /tsak" would
// be the surface promising something it is about to refuse.
//
// ── WHAT RUNS, AND WHAT IS ONLY MENTIONED ──
//
// A chip is a fact about the WORD and never a promise about what happens at
// submit. [app.enter] sends a draft to [app.slash] when its FIRST character is a
// slash, and that has not changed: a line that opens with "/task" is a command,
// and a "/task" anywhere else in a sentence is a MENTION — it travels to the
// model as the literal text a person typed, exactly as an "@path" does.
//
// Both wear the same chip, deliberately. The distinction is one the eye already
// has — a command is at the head of the line or it is not — and a second tint
// for it would be a colour that has to be learned to read a sentence.

// knownCommand reports whether word — a slash command's word, with the slash
// already taken off — is one this surface actually runs. The word is resolved
// through the table the same way the dispatch resolves it, so every other word
// for a command ("/clear", "/q", "/?") is known here too.
func knownCommand(word string) bool {
	if word == "" {
		return false
	}
	name := canonicalCommand(word)
	for _, c := range commands {
		if c.name == name {
			return true
		}
	}
	return false
}

// commandSpans finds every recognized slash command in value, as rune ranges,
// left to right.
//
// A CANDIDATE STARTS AT A WORD BOUNDARY and runs to the next space or newline.
// That one rule is what keeps a path out of this: "/Users/santosh" is a single
// candidate whose word is "Users/santosh" and matches nothing, rather than two
// candidates one of which might. A slash with a letter in front of it — the one
// in "http://", the one in "cmd/aforge" — is not a candidate at all.
//
// boundary says whether position 0 of value counts as a word boundary. The
// composer paints one soft-wrapped ROW at a time, and a row that begins in the
// middle of a word begins in the middle of a word.
func commandSpans(value []rune, boundary bool) []segment {
	var out []segment
	for i := 0; i < len(value); i++ {
		if value[i] != '/' {
			continue
		}
		switch {
		case i == 0:
			if !boundary {
				continue
			}
		case value[i-1] != ' ' && value[i-1] != '\n':
			continue
		}
		end := i + 1
		for end < len(value) && value[end] != ' ' && value[end] != '\n' {
			end++
		}
		if knownCommand(string(value[i+1 : end])) {
			out = append(out, segment{from: i, to: end})
		}
		// Everything up to the end of this token has been decided, chip or no
		// chip: the slashes inside a path are not boundaries, and re-examining
		// them is how "/Users/santosh" would grow a chip on its second half.
		i = end
	}
	return out
}

// paintCommands paints one line of a person's own words: the ink the caller
// asked for over the prose, and the chip over every command in it.
//
// The ink is passed in rather than chosen here because the two callers paint the
// same text in two different hues — the draft is [palette.ink] under the caret
// (input.go), the sent message is [palette.accent] in the transcript
// (render.go's [app.renderEntry]) — and a chip has to be able to sit inside
// either without the caller losing its own hue after it. Painting the runs
// SEPARATELY is what does that: [palette.paint] closes a colour with SGR 39,
// which resets the foreground rather than restoring whatever was under it, so a
// chip painted inside one long ink() call would leave the rest of the row
// unpainted.
func paintCommands(line string, pal palette, ink func(string) string, boundary bool) string {
	value := []rune(line)
	spans := commandSpans(value, boundary)
	if len(spans) == 0 {
		return ink(line)
	}
	var b strings.Builder
	at := 0
	for _, s := range spans {
		if s.from > at {
			b.WriteString(ink(string(value[at:s.from])))
		}
		b.WriteString(pal.chip(string(value[s.from:s.to])))
		at = s.to
	}
	if at < len(value) {
		b.WriteString(ink(string(value[at:])))
	}
	return b.String()
}

// slashToken finds the slash-word the caret is standing in: the run back to a
// space, a newline or the start of the draft, which must begin with '/'.
//
// It is [atToken] (files.go) with a different opening rune, and it is that
// deliberately — the two overlays this surface opens by typing answer to the
// same shape of question, and a token rule that differed between them would be
// two things to learn about one box. What it returns is where the '/' is and
// what has been typed after it UP TO THE CARET, which is what the list filters
// on.
func slashToken(value []rune, cursor int) (int, string, bool) {
	cursor = min(max(cursor, 0), len(value))
	start := cursor
	for start > 0 && value[start-1] != ' ' && value[start-1] != '\n' {
		start--
	}
	if start >= cursor || value[start] != '/' {
		return 0, "", false
	}
	return start, string(value[start+1 : cursor]), true
}

// tokenEnd is where the token that opens at `at` finishes: the next space or
// newline, or the end of the draft. [slashToken] stops at the caret because the
// filter is what has been typed so far; this is the whole word, which is what a
// chosen row REPLACES ([app.runMenu]).
func tokenEnd(value []rune, at int) int {
	for at < len(value) && value[at] != ' ' && value[at] != '\n' {
		at++
	}
	return at
}
