package tui3

import (
	"net/url"
	"os"
	"strconv"
	"strings"
	"unicode"
)

// DRAGGING A PICTURE IN IS A PICTURE, NOT A SENTENCE.
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
// So a paste that is NOTHING BUT PICTURES is read as pictures:
//
//   - each file goes on the attachment tray, where /image and the @ completion
//     already put them (attach.go), so it travels as bytes;
//   - and the draft gets `[image #1]` where the path would have been.
//
// The token is the point. It is what the person sees, edits around and can say
// out loud — "what font is image #1", "compare image #1 with image #2" — and it
// goes to the model inside the message text, in the position the person put it,
// while the pictures ride the same message as content parts in tray order. The
// number in the token, the number on the chip and the picture's place in the
// message are ONE number, kept in agreement by [app.forgetToken] whenever a chip
// comes off.
//
// IT IS ALL OR NOTHING. A paste substitutes only when every word in it resolves
// to a picture on this machine; anything else — a sentence that mentions a png,
// a diff, a stack trace — is inserted as the text it plainly is. That is what
// keeps this out of the way of the paste the surface sees a thousand times more
// often.

// imageTokenHead is the token's opening, spelled once. It is also the cheap
// reject that decides whether a draft is worth rewriting at all.
const imageTokenHead = "[image #"

// imageToken is what the draft holds in place of the nth attached picture.
func imageToken(n int) string { return imageTokenHead + strconv.Itoa(n) + "]" }

// pasteImages takes one pasted string, and reports whether it was pictures. When
// it was, the files are on the tray and the tokens are in the draft; when it was
// not, nothing has happened and the caller inserts the text.
func (a *app) pasteImages(text string) bool {
	// A SLASH COMMAND'S ARGUMENT IS A PATH AND MUST STAY ONE. `/image ` followed
	// by a dropped file is somebody using the command exactly as documented, and
	// turning its argument into `[image #1]` would break the one line on this
	// surface whose whole job is to take a path. The same is true of `/export `.
	if strings.HasPrefix(strings.TrimSpace(a.input.String()), "/") {
		return false
	}
	paths := a.pastedImages(text)
	if len(paths) == 0 {
		return false
	}
	// THE CEILING IS CHECKED AT THE DOOR, and a picture over it is refused here
	// with its name rather than attached and refused at enter. The path stays in
	// the draft as the text it arrived as, so nothing the person dropped is lost
	// — they can still ask aforge to look at the file where it lies.
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			// Not a file on this machine, so the paste was never a picture. No
			// note: there is nothing to tell somebody who pasted a sentence.
			return false
		}
		if info.Size() > maxAttachBytes {
			a.note(oversizeAttachment(chip{path: path}).Error())
			return false
		}
	}

	marks := make([]string, 0, len(paths))
	for _, path := range paths {
		a.attach(path)
		marks = append(marks, imageToken(a.chipNumber(path)))
	}
	inserted := a.spacedTokens(marks)
	at := a.input.cursor
	a.input.insert(inserted)
	a.editTags(at, at, len([]rune(inserted)))
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

// pastedImages resolves a paste to the pictures it names, or nil when it names
// anything else at all.
//
// The all-or-nothing rule lives here: one word that is not one of the five
// picture extensions and the whole paste is text. Existence is NOT asked here —
// that is a syscall per word, and the caller stats only the pastes that got this
// far.
func (a *app) pastedImages(text string) []string {
	words := pastedWords(text)
	if len(words) == 0 {
		return nil
	}
	out := make([]string, 0, len(words))
	for _, word := range words {
		path := a.resolvePath(pastedPath(word))
		if !isImagePath(path) {
			return nil
		}
		out = append(out, path)
	}
	return out
}

// pastedWords splits a paste the way the terminal that wrote it meant it to be
// split: on whitespace, EXCEPT the whitespace a drag-and-drop escaped or quoted.
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
		case unicode.IsSpace(r):
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
