package tui3

import (
	"os"
	"strings"
	"time"
	"unicode"

	tea "charm.land/bubbletea/v2"
)

// A DROP IS A DROP IN WHATEVER SHAPE THE TERMINAL SENDS IT.
//
// imagepaste.go reads a dragged file the way most terminals deliver one: a
// BRACKETED PASTE holding the file's escaped path, which arrives as one
// tea.PasteMsg and goes through [app.paste]. That has been true and working for
// a while, and it is why the chip machinery already understands escaped spaces,
// quotes, `file://` URLs and several files in one gesture.
//
// IT IS NOT THE ONLY SHAPE. Some terminals — and some multiplexers in front of
// them — deliver the same gesture as KEYSTROKES: the path is TYPED into the
// program, one tea.KeyPressMsg per character, with no bracket around it at all.
// Nothing above notices, because nothing above is looking at keys. So the path
// went into the draft as text, its leading `/` put the composer into command
// mode, and enter walked the whole thing into the slash router, which answered
// what it answers about any word it does not know:
//
//	unknown command: /var/folders/…/Screenshot · try /help
//
// That is what the owner met on a real terminal over `--host`, twice, on the
// same screenshot. The parsing was never the problem — the same bytes delivered
// as a paste attach perfectly. THE DELIVERY SHAPE WAS.
//
// So this file gives the keystroke shape the same door. It watches for a run of
// characters that spells a path, and when the run goes quiet it hands what it
// found to [app.pasteFilesInto] — the same function, the same tray, the same
// chips, the same hosted upload. There is no second tagging system and no second
// idea of what a dropped file is.
//
// AND IT WATCHES WHICHEVER BOX HAS THE KEYBOARD. The fold was the conversation
// draft's alone, so the one screen a person is most likely to be looking at when
// they drag something in — home, fullscreen, with its own line at the foot —
// held the raw path until enter caught it, filtering its list by a path no
// conversation matches while it sat there. The fold carries the box it is
// watching now, and one box at a time: home's line, the errand pane's and the
// draft are three boxes and a person types into exactly one of them.
//
// WHAT IT COSTS, AND THE LAW THAT KEEPS IT THERE. PERF.md's doctrine is that a
// gate counts work rather than time, and the work this file is allowed is
// counted in dropkeys_test.go:
//
//   - A KEY THAT COULD NOT BE PART OF A PATH COSTS NOTHING. Prose arms no timer,
//     asks the disk nothing and builds no extra frame. The fold opens only on a
//     character that can BEGIN a dropped path — `/`, `~`, a quote, or the `f` of
//     `file://` — standing at a word boundary, and it closes again on the first
//     character that proves the run is an ordinary word.
//   - A SLASH COMMAND COSTS NOTHING EITHER. `/help`, `/model`, `/export ~/x` —
//     none of them ever reaches the disk, because a dropped path is told from a
//     command by a SEPARATOR INSIDE IT: `/var/folders` has one, `/help` does not.
//     That test is string work on runes already in memory.
//   - A BURST ARMS ONE TIMER, however many characters it holds. The wakeup is
//     the pointer fold's shape exactly (coalesce.go): one is in flight or none
//     is, and the one in flight re-arms itself while characters are still
//     arriving rather than a second one being asked for.
//   - A SETTLED BURST ASKS THE DISK ONCE PER WORD IT HOLDS, and only after the
//     string gate above has passed. A burst that names nothing changes nothing
//     and BUILDS NO FRAME, because it provably mutated nothing [app.View] reads.
//
// AND IT IS SILENT UNLESS IT IS CERTAIN. A burst still arriving spells a great
// many paths that do not exist yet — `/var/f`, `/var/fo` — and the settle may
// land on any of them. So the fold converts only when every word it holds is an
// existing REGULAR FILE on this machine, and says nothing at all otherwise: a
// note about a half-typed path would be this surface interrupting somebody who
// is still typing. The refusals a person should see — a folder, a file over the
// ceiling — belong to the door at enter, where the gesture is finished.

// dropQuiet is how long a run of characters must go quiet before it is read as
// a finished drop.
//
// It is a QUIET WINDOW rather than a deadline, which is the difference between
// converting the path and converting a prefix of it. A terminal that types a
// drop delivers the whole path in one write, so its characters arrive with
// nothing between them; a multiplexer replaying one paces itself at a few
// milliseconds an event. Either way two frames of silence means the sender has
// stopped, and stopping is the only moment the run is the whole path — settling
// on a fixed deadline instead would convert `/a/b.png` while `/a/b.png.orig`
// was still arriving, and leave `.orig` typed after a chip.
//
// It is two frames rather than one because the surface animates at 30Hz and a
// window shorter than a frame would settle between two characters of the same
// burst on a loaded machine.
const dropQuiet = 2 * frameInterval

// dropMsg is the wakeup a burst asks for. It carries nothing: what settled is
// in the fold, and the fold is the surface's.
type dropMsg struct{}

// dropFold is the run of characters that may turn out to be a dropped path.
//
// IT HOLDS NO TEXT. Unlike the paste bracket (app.go's [app.pasteKey]) it never
// takes a character out of the person's way: every key types itself into the
// draft exactly as it always did, in order, and the fold only remembers WHERE
// the run began. So a burst that turns out to be an ordinary sentence needs
// nothing put back, and the box is never a character behind the keyboard.
type dropFold struct {
	// box is the box the run is being typed into and chips is the tray that
	// box's next message carries — the two things the one door is told
	// (imagepaste.go's [app.pasteFilesInto]).
	//
	// ONE FOLD, ONE BOX AT A TIME. A character landing in a box the run did not
	// begin in is not that run continuing, so the fold resets rather than
	// keeping a range in a place nobody is typing. And the box is checked again
	// on the way OUT, against the box that still has the keyboard: home opens
	// over the draft and closes back onto it, and a run left standing behind a
	// screen that went away must never become a token in a line off screen
	// ([app.keyboardBox]).
	box   *editor
	chips *[]chip

	// at and end bound the run inside that box, in runes. A key that does not
	// land exactly at end is not part of this run — a caret moved, a word
	// deleted — and starts the question again.
	at, end int

	// open says the run so far could still become a path.
	open bool

	// settling says a [dropMsg] is already on its way, so a whole burst asks for
	// one wakeup and not sixty. It is [pointerFold.settling]'s idea and is held
	// for the same reason.
	settling bool

	// typedAt is when the last character of the run arrived, which is what the
	// wakeup compares itself against to decide whether the sender has stopped.
	typedAt time.Time

	// watched counts the keys this fold was asked about, armed counts the
	// wakeups it asked for, looked counts the paths it asked the disk about and
	// took counts the drops it converted. They exist so the tests can state
	// this file's ceilings as COUNTS rather than as a stopwatch, which PERF.md's
	// doctrine forbids.
	watched, armed, looked, took int
}

// dropWatch is called with every character that types itself into a box, and it
// is the whole of what ordinary typing pays for this file: two integer
// comparisons and, for a character that could open a path, one string test.
//
// box is the line the character went into and chips is the tray that box's next
// message carries; at is where the character landed, before it was inserted.
func (a *app) dropWatch(box *editor, chips *[]chip, at int, text string) tea.Cmd {
	a.drop.watched++
	if a.drop.box != box {
		// A character in a box this run did not begin in. See [dropFold.box].
		a.drop.open = false
	}
	a.drop.box, a.drop.chips = box, chips
	runes := len([]rune(text))
	switch {
	case a.drop.open && a.drop.end == at:
		// The run continuing, character by character.
		a.drop.end = at + runes
	case dropOpener(text) && dropWordStart(box.value, at):
		// A character that could BEGIN a dropped path, standing where a word
		// begins. Anything else — a letter mid-word, a character after a caret
		// jump — is somebody typing, and the fold stays shut.
		a.drop.at, a.drop.end, a.drop.open = at, at+runes, true
	default:
		a.drop.open = false
		return nil
	}
	if a.drop.end > len(box.value) {
		// The box moved underneath the fold. It is not a run any more.
		a.drop.open = false
		return nil
	}
	run := string(box.value[a.drop.at:a.drop.end])
	if !dropCouldGrowInto(run) {
		// The run has proved itself an ordinary word — `fix` rather than
		// `file://…`. It costs one comparison to find out and nothing after.
		a.drop.open = false
		return nil
	}
	if !droppedPathShape(run) {
		// Still too short to be a path anybody dropped. The fold stays open so
		// the next character is cheap, and NOTHING is armed: a run with no
		// separator inside it is every slash command there is.
		//
		// THE CLOCK IS READ ONLY WHILE A WAKEUP IS IN FLIGHT, because that is the
		// only thing that ever reads it back. A burst that has already armed one
		// still has to say it is arriving — `/a/b /` is not a path and the run it
		// is halfway through is — while somebody typing `/help` touches no clock
		// at all.
		if a.drop.settling {
			a.drop.typedAt = a.now()
		}
		return nil
	}
	a.drop.typedAt = a.now()
	return a.dropWake()
}

// dropWake asks for the one wakeup a whole burst gets.
func (a *app) dropWake() tea.Cmd {
	if a.drop.settling {
		return nil
	}
	a.drop.settling = true
	a.drop.armed++
	return tea.Tick(dropQuiet, func(time.Time) tea.Msg { return dropMsg{} })
}

// dropSettled is the wakeup landing. It re-arms itself while characters are
// still arriving, so a burst of any length is one timer at a time.
func (a *app) dropSettled() tea.Cmd {
	a.drop.settling = false
	if !a.drop.open {
		a.ptr.still = true
		return nil
	}
	if a.now().Sub(a.drop.typedAt) < dropQuiet {
		// Still arriving. Waiting again changes nothing on the screen, so the
		// frame Bubble Tea is about to ask for is the frame it already has.
		a.ptr.still = true
		return a.dropWake()
	}
	draft := a.drop.box == &a.input
	if !a.spendDrop() {
		a.ptr.still = true
		return nil
	}
	if !draft {
		// A DRAFT'S EDIT FOLLOW-UPS ARE THE DRAFT'S. [app.edited] syncs the
		// completion lists over the conversation's box and arms its crash
		// insurance (draft.go); home's line and the errand pane's have neither,
		// and what they do owe — the row that counts a held file as something
		// typed, the list under it — the door's own follow-up has already paid
		// ([app.dropLanded]).
		return nil
	}
	return a.edited()
}

// spendDrop converts the run the fold is holding, and reports whether it was a
// drop. It is also what enter calls before it reads the line (input.go), for
// the pointer fold's own reason: a gesture the surface is still holding must be
// spent before anything acts on what it changed, and a person who dropped a
// file and pressed enter inside the same two frames means the drop.
func (a *app) spendDrop() bool {
	if !a.drop.open {
		return false
	}
	// AND IT IS ONLY EVER SPENT INTO THE BOX THAT STILL HAS THE KEYBOARD. Home
	// opens over the draft fullscreen and closes back onto it, and the errand
	// pane takes the keys from home's own line — each of those leaves a run
	// standing in a box the person is no longer typing into, and converting
	// there would put a token in a line nobody can see. The question is asked by
	// IDENTITY rather than by page, which is what makes it true of a home that
	// was closed and opened again: [app.dropHome] replaces the view with a fresh
	// one and the fold's range means nothing in it.
	box, chips := a.keyboardBox()
	if a.drop.box != box {
		a.drop.open = false
		return false
	}
	at, end := a.drop.at, a.drop.end
	if at < 0 || end > len(box.value) || end <= at {
		// The box moved underneath the fold; there is no run to spend.
		a.drop.open = false
		return false
	}
	run := string(box.value[at:end])
	// A RUN THAT IS NOT A DROP LEAVES THE FOLD OPEN, which is what makes a drop
	// delivered one character at a time work at all: `/var/f` names nothing and
	// `/var/folders/x/shot.png` names something, and they are the same run three
	// hundred milliseconds apart. Closing on the first miss would shut the fold
	// on the second character of every slowly-typed path there is.
	if !droppedPathShape(run) || !a.droppedFiles(run) {
		return false
	}
	a.drop.open = false
	// THE RUN COMES OUT OF THE BOX BEFORE THE DOOR IS ASKED, because the door
	// reads the box to decide. [app.pasteFilesInto] refuses a box that starts
	// with `/` on purpose — `/attach ` followed by a dropped file is somebody
	// using the command exactly as documented — and a keystroke drop into an
	// EMPTY box puts its own `/` at the front of it. Taking the run out first is
	// what tells the two apart: what is left is the command, or nothing at all.
	box.value = append(box.value[:at], box.value[end:]...)
	box.cursor = at
	box.editTags(at, end, 0)
	took := a.pasteFilesInto(box, chips, run)
	if took {
		a.drop.took++
	} else {
		// The door said no — the box holds a command and this is its argument,
		// or a file is over the ceiling and has been named. The characters go
		// back exactly where they were typed, in order, and the box is the box
		// the person was looking at.
		box.cursor = at
		box.insert(run)
		box.editTags(at, at, end-at)
	}
	// AND THE SURFACE OWES THE SAME FOLLOW-UP EITHER WAY, because either way the
	// box and the tray under it have changed ([app.dropLanded]).
	a.dropLanded(box)
	return took
}

// ── telling a dropped path from a typed word ────────────────────────────────

// dropOpener reports whether one typed character could begin a dropped path.
//
// `/` and `~` are the two roots a terminal writes; `'` and `"` are the quoting
// the ones that do not backslash their spaces use instead; and `f` is there for
// `file://`, which is what a desktop's own drag protocol carries and several
// terminals pass straight through. `f` is the expensive-looking one and it is
// not: it merely OPENS the question, and [dropCouldGrowInto] shuts it again on
// the second character of every `f` word that is not `file://`.
func dropOpener(text string) bool {
	r := []rune(text)
	if len(r) == 0 {
		return false
	}
	switch r[0] {
	case '/', '~', '\'', '"', 'f':
		return true
	}
	return false
}

// dropWordStart reports whether at is where a word begins in the draft. A `/`
// in the middle of `and/or` is not somebody dropping anything.
func dropWordStart(value []rune, at int) bool {
	return at == 0 || (at <= len(value) && unicode.IsSpace(value[at-1]))
}

// dropCouldGrowInto reports whether the run so far could still grow into a path
// somebody dropped. It is the fold's early close, and it is what keeps an `f`
// word from holding the fold open for the length of a sentence.
func dropCouldGrowInto(run string) bool {
	switch {
	case strings.HasPrefix(run, "/"), strings.HasPrefix(run, "~"),
		strings.HasPrefix(run, "'"), strings.HasPrefix(run, `"`):
		return true
	}
	// `file://` one character at a time, and its own prefixes.
	head := strings.ToLower(run)
	if len(head) > len(fileScheme) {
		head = head[:len(fileScheme)]
	}
	return strings.HasPrefix(fileScheme, head)
}

// fileScheme is the URL form of a drop, spelled once.
const fileScheme = "file://"

// droppedPathShape reports whether a run of characters is shaped like the paths
// a terminal writes when a file is dropped on it — WITHOUT asking the disk
// anything, which is the whole point of it. Every word must be an absolute
// local path, because that is the only thing a drop ever writes.
//
// THE SEPARATOR INSIDE IT IS WHAT TELLS A DROP FROM A COMMAND. `/help`,
// `/model`, `/export` and every other word this surface answers are one segment
// with nothing after them; `/var/folders/…` and `~/Desktop/…` are not. So a
// slash command never reaches a syscall, and the cost of typing one is exactly
// what it was before this file existed.
//
// It also means a file sitting at the root of the disk — `/notes.md` — is not
// recognised while it is being typed. That is deliberate: the enter door below
// stats what it is given, so the drop still lands, and no plausible slash
// command is ever mistaken for a file on the way there.
func droppedPathShape(text string) bool {
	words := pastedWords(text)
	if len(words) == 0 {
		return false
	}
	for _, word := range words {
		if !droppedWordShape(word) {
			return false
		}
	}
	return true
}

func droppedWordShape(word string) bool {
	if rest, ok := cutFileScheme(word); ok {
		return strings.Contains(rest, "/")
	}
	switch {
	case strings.HasPrefix(word, "~/"):
		return len(word) > len("~/")
	case strings.HasPrefix(word, "/"):
		return strings.Contains(word[1:], "/")
	}
	return false
}

// cutFileScheme takes `file://` off the front of a word, case-insensitively,
// and reports whether it was there.
func cutFileScheme(word string) (string, bool) {
	if len(word) < len(fileScheme) || !strings.EqualFold(word[:len(fileScheme)], fileScheme) {
		return word, false
	}
	return word[len(fileScheme):], true
}

// droppedFiles reports whether every word of a run names an existing regular
// file on this machine. It is the fold's certainty, and it is SILENT: a run
// that names a folder, or nothing at all, simply is not a drop yet, and a
// surface that said so would be talking over somebody who is still typing.
func (a *app) droppedFiles(text string) bool {
	words := pastedWords(text)
	if len(words) == 0 {
		return false
	}
	for _, word := range words {
		a.drop.looked++
		info, err := os.Stat(a.resolvePath(pastedPath(word)))
		if err != nil || !info.Mode().IsRegular() {
			return false
		}
	}
	return true
}

// ── the net under enter ─────────────────────────────────────────────────────

// droppedLine is the safety net the slash router falls into before it refuses
// (app.go's [app.slash]). A line that begins with `/` and names files that are
// really there is a drop that got all the way to enter — through a terminal
// shape nothing above recognised, through a paste this surface never saw the
// bracket of — and answering it with `unknown command` is the surface telling
// somebody their file does not exist.
//
// IT IS THE ONLY PLACE A DROP MAY SPEAK UP ABOUT A FOLDER. Everything above is
// silent while a person is mid-gesture; here the gesture is finished, enter has
// been pressed, and "that is a folder" is the answer to what they just did.
//
// An unknown command that names nothing on the disk still refuses exactly as it
// always did: this returns false and the caller writes its own sentence.
func (a *app) droppedLine(line string) bool {
	return a.droppedLineInto(&a.input, &a.chips, line)
}

// droppedLineInto is that net under WHICHEVER box the line was typed into, so
// home's own dispatcher falls into it too (home.go's [app.homeEnter]). Home
// answered a dropped path with `unknown command`, the same sentence chat stopped
// answering with when this net was written, because home ran the slash router
// over its own box and never reached here.
func (a *app) droppedLineInto(box *editor, chips *[]chip, line string) bool {
	words := pastedWords(line)
	if len(words) == 0 {
		return false
	}
	files := 0
	for _, word := range words {
		a.drop.looked++
		info, err := os.Stat(a.resolvePath(pastedPath(word)))
		if err != nil {
			// A LINE SHAPED LIKE A DROP THAT NAMES NOTHING HERE IS A DROP FROM
			// ANOTHER MACHINE, and the door says so in its own sentence rather
			// than leaving the router to answer `unknown command` about a file
			// that exists perfectly well on the laptop it was dragged from
			// (imagepaste.go's [app.pasteFilesInto]). Anything else is the
			// unknown command it looks like, and is refused as one.
			if droppedPathShape(line) {
				a.pasteFilesInto(box, chips, line)
				return true
			}
			return false
		}
		if info.Mode().IsRegular() {
			files++
		}
	}
	held := len(*chips)
	if !a.pasteFilesInto(box, chips, line) {
		return false
	}
	a.drop.took++
	// WHAT HAPPENED IS SAID OUT LOUD, because this door is reached by somebody
	// who has just watched their file turn into a line of text and press enter.
	// The chips are the proof; the sentence is what stops them typing it again.
	//
	// A FOLDER SAYS ITS OWN SENTENCE AND IS NOT GIVEN A SECOND ONE.
	// [app.pasteFilesInto] answers "<name> is a folder · attach a file" and
	// attaches nothing, and "attached" underneath that would be this surface
	// contradicting itself.
	if files == len(words) && len(*chips) > held {
		a.trayNote(droppedNote(files))
	}
	return true
}

// droppedNote is what the enter door says about a drop it caught.
func droppedNote(files int) string {
	if files == 1 {
		return "that was a file, not a command · attached"
	}
	return "those were files, not a command · attached"
}
