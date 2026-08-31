package tui3

import (
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"
)

// DRAGGING A FILE IN IS A FILE, NOT A SENTENCE.
//
// A terminal has no idea what an image is. Drop a screenshot on iTerm2, Ghostty
// or Terminal.app and what arrives is a BRACKETED PASTE of the file's path —
// `/var/folders/.../Screenshot 2026-08-21 at 5.21.40 PM.png`, usually with its
// spaces backslashed, sometimes quoted, sometimes as a `file://` URL. cmd+V of a
// file copied in Finder does the same thing.
//
// Until this file existed that text went into the draft as text and the message
// went out through [app.submit]: the model was handed a PATH and never the
// pixels. It could sometimes recover — `view_image` reads a file by path — but
// only by spending a tool call on a second model, and only if it guessed that
// the string was worth opening. A screenshot path with four spaces in it usually
// did not survive the guess at all.
//
// So a paste that is NOTHING BUT PATHS TO REAL FILES is read as attachments:
//
//   - a picture goes on the tray and leaves `[image #1]` in the draft, where
//     /image and the @ completion already put them (attach.go);
//   - an ordinary file goes on the same tray without putting its local path in
//     the sentence, so a hosted send can carry its bytes.
//
// The token is the point. It is what the person sees, edits around and can say
// out loud — "what font is image #1", "compare image #1 with image #2" — and it
// goes to the model inside the message text, in the position the person put it,
// while the pictures ride the same message as content parts in tray order. The
// number in the token, the number on the chip and the picture's place in the
// message are ONE number, kept in agreement by [app.forgetToken] whenever a chip
// comes off.
//
// IT IS ALL OR NOTHING. A paste substitutes only when one complete reading of
// it resolves on this machine; anything else — a sentence that mentions a png,
// a diff, a stack trace — is inserted as the text it plainly is. That is what
// keeps this out of the way of the paste the surface sees a thousand times more
// often. The several readings below are terminal spellings of the same gesture,
// not several ideas of what a file is.

// imageTokenHead is the token's opening, spelled once. It is also the cheap
// reject that decides whether a draft is worth rewriting at all.
const imageTokenHead = "[image #"

// imageToken is what the draft holds in place of the nth attached picture.
func imageToken(n int) string { return imageTokenHead + strconv.Itoa(n) + "]" }

// pasteFiles recognizes the file form of a terminal drop. A desktop drop
// arrives only as pasted local paths, so putting those files on the existing
// tray is what lets hosted sends carry their bytes instead of handing the
// engine names from the wrong disk.
func (a *app) pasteFiles(text string) bool {
	if strings.HasPrefix(strings.TrimSpace(a.input.String()), "/") {
		return false
	}
	hits, _ := a.pasteResolve(text, false)
	if hits == nil {
		return false
	}
	for _, hit := range hits {
		candidate, info := hit.path, hit.info
		if info.IsDir() {
			a.note(filepath.Base(candidate) + " is a folder · attach a file")
			return true
		}
		if isImagePath(candidate) {
			if info.Size() > maxAttachBytes {
				a.note(oversizeAttachment(chip{path: candidate}).Error())
				return false
			}
		} else if a.hosted() && info.Size() > maxAttachedFileBytes {
			a.note(oversizeFile(filepath.Base(candidate), info.Size()))
			return false
		}
	}
	marks := make([]string, 0, len(hits))
	for _, hit := range hits {
		candidate := hit.path
		if isImagePath(candidate) {
			a.attach(candidate)
			marks = append(marks, imageToken(a.chipNumber(candidate)))
			continue
		}
		a.attachFile(candidate)
	}
	if len(marks) > 0 {
		inserted := a.spacedTokens(marks)
		at := a.input.cursor
		a.input.insert(inserted)
		a.editTags(at, at, len([]rune(inserted)))
	}
	a.touch()
	return true
}

// spacedTokens is the run of tokens as it is inserted: separated from the word
// the caret was standing after, and followed by a space so the next thing typed
// is a new word rather than a longer token.
func (a *app) spacedTokens(marks []string) string {
	text := strings.Join(marks, " ") + " "
	if a.input.cursor > 0 && !unicode.IsSpace(a.input.value[a.input.cursor-1]) {
		text = " " + text
	}
	return text
}

// chipNumber is the PICTURE's number, one-based, and 0 when that path is not on
// the tray. It is the number the token, the chip and the message's content parts
// all share.
//
// IT COUNTS PICTURES AND NOT CHIPS, which is the whole of the fix: the tray
// holds attached files as well now, and a position on the tray stopped being a
// position among the pictures the moment it did. Numbering by tray position
// meant a screenshot pasted while a log file sat in front of it was announced as
// `[image #2]` when it was the first — and the token, the chip and the content
// part would then disagree about which picture the person meant.
func (a *app) chipNumber(path string) int {
	for i, held := range a.chips {
		if held.path == path {
			return pictureOrdinal(a.chips, i)
		}
	}
	return 0
}

// pastedWords splits a paste the way the terminal that wrote it meant it to be
// split: on ASCII IFS, EXCEPT the separators a drag-and-drop escaped or quoted.
//
// This is the whole reason a screenshot's path needs parsing at all. Every
// terminal that implements the drop writes `Screenshot\ 2026-08-21\ at\ 5.png`
// or `'Screenshot 2026-08-21 at 5.png'`, because the text it is writing is meant
// for a shell; a splitter that took every space would hand back five words and
// call none of them a picture. Two files dropped together arrive as two such
// words on one line, and some terminals use a newline between them instead.
func pastedWords(text string) []string {
	runes := []rune(text)
	out := make([]string, 0, 4)
	var word strings.Builder
	quote := rune(0)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
				continue
			}
			word.WriteRune(r)
		case r == '\'' || r == '"':
			quote = r
		case r == '\\' && i+1 < len(runes):
			// The escaped character is itself, which is what the shell this
			// escaping was written for would do with it.
			i++
			word.WriteRune(runes[i])
		case asciiPasteSpace(r):
			// A TERMINAL ESCAPES WHAT THE SHELL WOULD SPLIT ON AND NOTHING ELSE.
			// In particular, the narrow no-break space in a macOS screenshot name
			// arrives literally and belongs to that name rather than between words.
			if word.Len() > 0 {
				out = append(out, word.String())
				word.Reset()
			}
		default:
			word.WriteRune(r)
		}
	}
	if word.Len() > 0 {
		out = append(out, word.String())
	}
	return out
}

// asciiPasteSpaces is the shell whitespace a terminal escapes in a dropped
// path, named once so the splitter and the literal readings cannot drift.
const asciiPasteSpaces = " \t\n\r\v\f"

func asciiPasteSpace(r rune) bool {
	return strings.ContainsRune(asciiPasteSpaces, r)
}

// pasteReadings returns the terminal spellings one drop may mean, most literal
// first. The alternatives are admitted only for a path-shaped first token and
// only where the literal split left evidence that it may not be the whole
// story, so prose never grows a more permissive reading around a path it names.
func pasteReadings(text string) [][]string {
	words := pastedWords(text)
	if len(words) == 0 {
		return nil
	}
	readings := make([][]string, 0, 4)
	readings = appendReading(readings, words)
	if !droppedWordShape(words[0]) ||
		(len(words) == 1 && !strings.ContainsAny(text, "\n%")) {
		return readings
	}

	lines := strings.Split(text, "\n")
	lineReading := make([]string, 0, len(lines))
	for _, line := range lines {
		candidate := literalPastePath(line)
		if candidate == "" {
			lineReading = nil
			break
		}
		lineReading = append(lineReading, candidate)
	}
	readings = appendReading(readings, lineReading)
	readings = appendReading(readings, []string{literalPastePath(text)})

	decoded := append([]string(nil), words...)
	changed := false
	for i, word := range decoded {
		if !strings.Contains(word, "%") {
			continue
		}
		if path, err := url.PathUnescape(word); err == nil {
			decoded[i] = path
			changed = changed || path != word
		}
	}
	if changed {
		readings = appendReading(readings, decoded)
	}
	return readings
}

// literalPastePath removes exactly one balanced quote pair because a filename
// may itself contain quotes, then removes the shell escaping from its spelling.
func literalPastePath(text string) string {
	text = strings.Trim(text, asciiPasteSpaces)
	if len(text) >= 2 && (text[0] == '\'' || text[0] == '"') && text[len(text)-1] == text[0] {
		text = text[1 : len(text)-1]
	}
	var out strings.Builder
	runes := []rune(text)
	for i := 0; i < len(runes); i++ {
		if runes[i] == '\\' && i+1 < len(runes) {
			i++
		}
		out.WriteRune(runes[i])
	}
	return strings.Trim(out.String(), asciiPasteSpaces)
}

// appendReading rejects an empty reading and deduplicates equivalent spellings
// so one path is never stat'ed twice merely because a line is also the whole paste.
func appendReading(readings [][]string, reading []string) [][]string {
	if len(reading) == 0 || (len(reading) == 1 && reading[0] == "") {
		return readings
	}
	for _, held := range readings {
		if sameReading(held, reading) {
			return readings
		}
	}
	return append(readings, reading)
}

func sameReading(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

type pasteHit struct {
	path string
	info os.FileInfo
}

// pasteResolve walks the admitted readings until every candidate in one exists.
// It reports the stats it performed without owning the keystroke road's count;
// that counter belongs to the two callers that promise it to the performance
// tests. regularOnly is the quiet fold's stricter certainty about real files.
func (a *app) pasteResolve(text string, regularOnly bool) (hits []pasteHit, looked int) {
	for _, reading := range pasteReadings(text) {
		resolved := make([]pasteHit, 0, len(reading))
		for _, word := range reading {
			candidate := a.resolvePath(pastedPath(word))
			info, err := os.Stat(candidate)
			looked++
			if err != nil || (regularOnly && !info.Mode().IsRegular()) {
				resolved = nil
				break
			}
			resolved = append(resolved, pasteHit{path: candidate, info: info})
		}
		if resolved != nil {
			return resolved, looked
		}
	}
	return nil, looked
}

// pastedPath turns one word of a paste into the path it means. A `file://` URL
// is what a desktop's drag protocol carries and several terminals pass straight
// through, and its percent-escapes have to come off — `%20` is a space in a name
// and a stat for `%20` finds nothing.
func pastedPath(word string) string {
	if !strings.HasPrefix(strings.ToLower(word), "file://") {
		return word
	}
	parsed, err := url.Parse(word)
	if err != nil || parsed.Path == "" {
		return word
	}
	return parsed.Path
}

// ── keeping the numbers true ────────────────────────────────────────────────

// forgetToken rewrites the draft after the picture numbered gone came off a tray
// that was holding held of them.
//
// THE NUMBER ON A CHIP IS ITS PLACE IN THE TRAY, so taking one away renumbers
// every picture behind it — and a draft still saying `[image #3]` about the
// picture that is now second would be the surface lying about which one the
// model will be looking at. The gone token comes out of the sentence, with the
// space that was holding it apart, and the ones behind it count down.
//
// Descending numbers are rewritten in ASCENDING order deliberately: each new
// number is lower than the old one and lower than every number still to be
// visited, so no rewrite can be rewritten again.
func (a *app) forgetToken(gone, held int) {
	text := a.input.String()
	if !strings.Contains(text, imageTokenHead) {
		return
	}
	// The space that was holding the token apart from the words goes with it, in
	// whichever of the three places it can be: between two words, at the front of
	// the line, or at the end of one. What is left is the sentence the person
	// would have typed if they had never dropped that picture.
	token := imageToken(gone)
	text = strings.ReplaceAll(text, " "+token+" ", " ")
	text = strings.TrimPrefix(text, token+" ")
	text = strings.ReplaceAll(text, " "+token, "")
	text = strings.ReplaceAll(text, token, "")
	for n := gone + 1; n <= held; n++ {
		text = strings.ReplaceAll(text, imageToken(n), imageToken(n-1))
	}
	a.input.rewrite(text)
}

// imageSentence is the text an image-bearing message actually sends, and it is
// the same text the transcript shows.
//
// EVERY PICTURE IN THE MESSAGE IS NAMED IN IT. A picture pasted in already has
// its token where the person put it and nothing is added. One that came off
// `/image` or the @ completion has none — those two never touch the sentence
// (attach.go) — and neither does one whose token the person deleted while
// keeping the chip. Those get their tokens appended, so "look at image 2" means
// something no matter which door the picture came in by.
func imageSentence(text string, chips []chip) string {
	missing := make([]string, 0, len(chips))
	for i := range chips {
		if token := imageToken(i + 1); !strings.Contains(text, token) {
			missing = append(missing, token)
		}
	}
	if len(missing) == 0 {
		return text
	}
	marks := strings.Join(missing, " ")
	if strings.TrimSpace(text) == "" {
		return marks
	}
	return strings.TrimRight(text, " \t") + " " + marks
}
