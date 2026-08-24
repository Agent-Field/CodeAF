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
// ── THE CHIP IS A PROMISE ──
//
// A CHIP MARKS A WORD THAT WILL ACT: a command at the head of the draft, or a
// send-door tag anywhere else. Other commands inside prose stay prose. A person
// may make a live tag plain by pressing backspace immediately after it, and its
// chip leaves on that first press without deleting a letter.
//
// THE TWO TAG DOORS BOTH END AT A PERSON-VISIBLE DECISION. /standing raises its
// ratification card and /task opens its sizing choice; pasted text cannot turn a
// tinted word into silent work or spent money. This safety fact is why the chip
// may honestly promise that enter will act on a tag.

type sendDoor uint8

const (
	sendDoorNone sendDoor = iota
	sendDoorStanding
	sendDoorTask
)

const (
	slashTagHintStanding = "enter keeps this true"
	slashTagHintTask     = "enter sizes this task"
	slashTagRefusal      = "one tag per send — backspace one to make it plain words"
)

func commandDoor(word string) sendDoor {
	name := canonicalCommand(word)
	for _, c := range commands {
		if c.name == name && c.door != sendDoorNone {
			return c.door
		}
	}
	return sendDoorNone
}

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
func recognizedCommandSpans(value []rune, boundary bool) []segment {
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

func commandSpans(value []rune, boundary bool) []segment {
	all := recognizedCommandSpans(value, boundary)
	out := all[:0]
	for _, s := range all {
		word := string(value[s.from+1 : s.to])
		// A leading recognized word runs through the command dispatcher. Away
		// from the head, only a row that names a send door is a promise.
		if s.from == 0 && boundary || commandDoor(word) != sendDoorNone {
			out = append(out, s)
		}
	}
	return out
}

func containsSegment(list []segment, want segment) bool {
	for _, got := range list {
		if got == want {
			return true
		}
	}
	return false
}

// liveTags returns the actionable send-door words away from the head command.
func (a *app) liveTags() []segment {
	value := a.input.value
	var out []segment
	for _, s := range commandSpans(value, true) {
		if s.from == 0 || containsSegment(a.input.demotedTags, s) {
			continue
		}
		if commandDoor(string(value[s.from+1:s.to])) != sendDoorNone {
			out = append(out, s)
		}
	}
	return out
}

// editTags carries demotions through an edit. An edit before a tag shifts its
// range; an edit that overlaps or enters the word dissolves it, allowing the
// scanner to recognize the resulting spelling afresh.
func (a *app) editTags(from, to, inserted int) {
	delta := inserted - (to - from)
	out := a.input.demotedTags[:0]
	for _, s := range a.input.demotedTags {
		if to <= s.from {
			s.from += delta
			s.to += delta
			out = append(out, s)
			continue
		}
		if from >= s.to {
			out = append(out, s)
			continue
		}
		// The edit touched the annotation, so plainness is no longer banked.
	}
	a.input.demotedTags = out
}

func (a *app) demoteTagBehindCaret() bool {
	for _, s := range a.liveTags() {
		if s.to == a.input.cursor {
			a.input.demotedTags = append(a.input.demotedTags, s)
			return true
		}
	}
	return false
}

func (a *app) slashTagHint() string {
	tags := a.liveTags()
	if len(tags) != 1 {
		return ""
	}
	word := string(a.input.value[tags[0].from+1 : tags[0].to])
	if commandDoor(word) == sendDoorStanding {
		return slashTagHintStanding
	}
	return slashTagHintTask
}

func removeSlashTag(value []rune, s segment) string {
	left, right := strings.TrimRight(string(value[:s.from]), " \t\n"), strings.TrimLeft(string(value[s.to:]), " \t\n")
	if left == "" {
		return strings.TrimSpace(right)
	}
	if right == "" {
		return strings.TrimSpace(left)
	}
	return strings.TrimSpace(left + " " + right)
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
	return paintCommandSpans(line, spans, pal, ink)
}

func paintCommandSpans(line string, spans []segment, pal palette, ink func(string) string) string {
	value := []rune(line)
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

// transcriptCommandSpans keeps the chip's promise after a send: a leading
// command did act, and only the tag ranges recorded on that entry did act.
func transcriptCommandSpans(value []rune, acted []segment) []segment {
	var out []segment
	for _, s := range commandSpans(value, true) {
		if s.from == 0 || containsSegment(acted, s) {
			out = append(out, s)
		}
	}
	return out
}

func paintDraftCommands(line string, pal palette, ink func(string) string, offset int, boundary bool, demoted []segment) string {
	value := []rune(line)
	spans := commandSpans(value, boundary)
	kept := spans[:0]
	for _, s := range spans {
		s.from += offset
		s.to += offset
		if !containsSegment(demoted, s) {
			s.from -= offset
			s.to -= offset
			kept = append(kept, s)
		}
	}
	if len(kept) == 0 {
		return ink(line)
	}
	var b strings.Builder
	at := 0
	for _, s := range kept {
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
