package tui3

import (
	"net/url"
	"os"
	"path/filepath"
	"regexp"
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

// imageTokenPattern is the token as a shape rather than as a number, and it is
// spelled from [imageTokenHead] so the two can never drift apart.
var imageTokenPattern = regexp.MustCompile(regexp.QuoteMeta(imageTokenHead) + `\d+\]`)

// withoutImageTokens is a sentence with its picture tokens taken out, for the
// one reader that wants the WORDS and not the cargo: home's box is a live query
// over every project on the machine as well as the first line of a conversation
// (home.go), and a dropped screenshot filled it with `[image #1]` — a query no
// conversation on earth matches, so the list under it emptied.
//
// The cheap reject is first because every keystroke on home asks this.
func withoutImageTokens(text string) string {
	if !strings.Contains(text, imageTokenHead) {
		return text
	}
	return imageTokenPattern.ReplaceAllString(text, " ")
}

// pasteFilesInto is THE ONE DOOR every box on this surface drops a file
// through, and it is parametrized by the box because there is more than one box
// a person can drop on: the conversation's draft, home's own line at the foot,
// and the errand pane's ([app.paste] routes them). It used to be bound to
// [app.input] alone, so a screenshot dropped on home became the raw escaped path
// it arrived as — the model was handed a string and the person was handed a mess
// to clean up.
//
// box is the line the text was aimed at and chips is the tray THAT box's next
// message carries. Every one of them carries a picture as content and an
// ordinary file as a path (attach.go), so there is one rule and not three.
//
// It reports whether the text was files. When it was, they are on that tray and
// the tokens are in that box; when it was not, nothing has happened and the
// caller inserts the text as the text it plainly is.
func (a *app) pasteFilesInto(box *editor, chips *[]chip, text string) bool {
	// A SLASH COMMAND'S ARGUMENT IS A PATH AND MUST STAY ONE. `/image ` followed
	// by a dropped file is somebody using the command exactly as documented, and
	// turning its argument into `[image #1]` would break the one line on this
	// surface whose whole job is to take a path. The same is true of `/export `.
	// It is THIS box that is asked, because the command is in the box the drop
	// landed in and nowhere else.
	if strings.HasPrefix(strings.TrimSpace(box.String()), "/") {
		return false
	}
	words := pastedWords(text)
	if len(words) == 0 {
		return false
	}
	paths := make([]string, len(words))
	found := make([]os.FileInfo, len(words))
	missing, first := 0, ""
	for i, word := range words {
		paths[i] = a.resolvePath(pastedPath(word))
		info, err := os.Stat(paths[i])
		if err != nil {
			missing++
			if first == "" {
				first = filepath.Base(paths[i])
			}
			continue
		}
		found[i] = info
	}
	// A DROP FROM ANOTHER MACHINE IS SAID OUT LOUD. iTerm2 → ssh → tmux hands a
	// TUI running on the Linux box a path that means something on the Mac, and
	// this door used to answer that by silently inserting it — a person watching
	// their screenshot turn into text with nothing on the screen to explain it.
	// The sentence is owed only where the paste was PLAINLY a drop
	// ([droppedPathShape]: every word an absolute path with a separator in it),
	// so a sentence that merely mentions a file is inserted in silence as before.
	if missing > 0 {
		if droppedPathShape(text) {
			a.trayNote(notOnThisMachine(first, missing))
		}
		return false
	}
	for i, info := range found {
		if info.IsDir() {
			a.trayNote(filepath.Base(paths[i]) + " is a folder · attach a file")
			return true
		}
		if isImagePath(paths[i]) {
			if info.Size() > maxAttachBytes {
				a.trayNote(oversizeAttachment(chip{path: paths[i]}).Error())
				return false
			}
			continue
		}
		if info.Size() > maxAttachedFileBytes {
			a.trayNote(oversizeFile(filepath.Base(paths[i]), info.Size()))
			return false
		}
	}
	marks := make([]string, 0, len(paths))
	for _, candidate := range paths {
		if isImagePath(candidate) {
			attachChipTo(chips, chip{path: candidate})
			marks = append(marks, imageToken(chipNumberIn(*chips, candidate)))
			continue
		}
		attachChipTo(chips, chip{path: candidate, file: true})
	}
	if len(marks) > 0 {
		inserted := box.spacedTokens(marks)
		at := box.cursor
		box.insert(inserted)
		box.editTags(at, at, len([]rune(inserted)))
	}
	a.touch()
	return true
}

// pasteFiles is the conversation's own draft going through that door — the
// caller this file was written for, and now one of three.
func (a *app) pasteFiles(text string) bool {
	return a.pasteFilesInto(&a.input, &a.chips, text)
}

// keyboardBox is the box a character typed right now would land in, with the
// tray that box's next message carries.
//
// IT IS ONE ANSWER BECAUSE THERE IS ONE KEYBOARD. Three boxes on this surface
// can start a message and every door that acts on "the box in front of the
// person" has to agree about which one that is — the paste below, and the
// keystroke fold's check that the run it is holding is still somewhere anybody
// can see it (dropkeys.go's [app.spendDrop]). Two spellings of this routing is
// two answers, and the day they disagree is the day a dropped picture becomes a
// token in a line that is off the screen.
func (a *app) keyboardBox() (*editor, *[]chip) {
	if a.at(pageHome) {
		if ex := a.paneExchange(); ex != nil && ex.focused {
			// THE ERRAND'S TRAY IS ITS OWN, because an errand is its own
			// conversation with its own next message (homeexchange.go).
			return &ex.box, &ex.chips
		}
		// HOME'S TRAY IS THE CONVERSATION'S TRAY, because what home's box starts
		// IS a conversation: [app.renew] hands the chips to the one it opens, on
		// the law that the draft goes with the person (detach.go).
		return &a.home.box, &a.chips
	}
	return &a.input, &a.chips
}

// dropLanded is what the surface owes after files went through the door into
// one box, whichever road brought them — a bracketed paste, or the keystroke
// fold settling (dropkeys.go).
//
// ONLY HOME OWES ANYTHING BEYOND THE TOUCH THE DOOR ALREADY MADE, and it owes it
// because its box is a live query over every project on the machine as well as
// the first line of a conversation: the action row counts a held file as
// something typed ([homeView.searching]) and the list under it is drawn from the
// words with the tokens taken out. Neither is right again until the view is
// rebuilt.
func (a *app) dropLanded(box *editor) {
	if box != &a.home.box {
		return
	}
	a.home.carrying = len(a.chips) > 0
	a.home.build()
}

// notOnThisMachine is what a drop that named nothing here says. ONE SENTENCE
// FOR ANY NUMBER OF FILES: a person who dragged four screenshots off a Mac onto
// a session running on a Linux box has one thing wrong, not four.
func notOnThisMachine(name string, missing int) string {
	if missing == 1 {
		return name + " is not on this machine"
	}
	return strconv.Itoa(missing) + " files are not on this machine"
}

// trayNote is where this door's refusals are said, and it picks the voice by
// WHERE THE PERSON IS STANDING. A note is a line in the conversation
// ([app.note]), and while home has the frame the conversation is not on the
// screen at all — so a folder refused there would be a sentence written
// somewhere nobody can read it. Home has a line of its own under the rule
// ([homeView.say], drawn by pages.go's [app.placeMsgLine]).
func (a *app) trayNote(msg string) {
	if a.at(pageHome) {
		a.home.say(msg, "")
		return
	}
	a.note(msg)
}

// spacedTokens is the run of tokens as it is inserted into one box: separated
// from the word the caret was standing after, and followed by a space so the
// next thing typed is a new word rather than a longer token.
func (e *editor) spacedTokens(marks []string) string {
	text := strings.Join(marks, " ") + " "
	if e.cursor > 0 && !unicode.IsSpace(e.value[e.cursor-1]) {
		text = " " + text
	}
	return text
}

// spacedTokens is the main draft's, for the callers that only ever mean it
// (pastechip.go).
func (a *app) spacedTokens(marks []string) string { return a.input.spacedTokens(marks) }

// chipNumber is the PICTURE's number on the conversation's own tray, one-based,
// and 0 when that path is not on it.
func (a *app) chipNumber(path string) int { return chipNumberIn(a.chips, path) }

// chipNumberIn is that question asked of ANY tray, which is what lets home's box
// and the errand pane's number their own pictures without a second idea of what
// a number means. It is the number the token, the chip and the message's content
// parts all share.
//
// IT COUNTS PICTURES AND NOT CHIPS, which is the whole of the fix: the tray
// holds attached files as well now, and a position on the tray stopped being a
// position among the pictures the moment it did. Numbering by tray position
// meant a screenshot pasted while a log file sat in front of it was announced as
// `[image #2]` when it was the first — and the token, the chip and the content
// part would then disagree about which picture the person meant.
func chipNumberIn(chips []chip, path string) int {
	for i, held := range chips {
		if held.path == path {
			return pictureOrdinal(chips, i)
		}
	}
	return 0
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
