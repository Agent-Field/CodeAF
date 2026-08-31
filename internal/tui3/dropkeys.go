package tui3

import (
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
// found to [app.pasteFiles] — the same function, the same tray, the same chips,
// the same hosted upload. There is no second tagging system and no second idea
// of what a dropped file is.
//
// WHAT IT COSTS, AND THE LAW THAT KEEPS IT THERE. PERF.md's doctrine is that a
// gate counts work rather than time, and the work this file is allowed is
// counted in dropkeys_test.go:
//
//   - A KEY THAT COULD NOT BE PART OF A PATH COSTS NOTHING. Prose whose first
//     token is not path-shaped arms no timer, asks the disk nothing and builds no
//     extra frame. Once a path-shaped first token arrives the fold stays open
//     through later words: until the disk is asked, a raw-space filename and a
//     sentence beginning with a path are the same characters.
//   - A SLASH COMMAND COSTS NOTHING EITHER. `/help`, `/model`, `/export ~/x` —
//     none of them ever reaches the disk, because a dropped path is told from a
//     command by a SEPARATOR INSIDE IT: `/var/folders` has one, `/help` does not.
//     That test is string work on runes already in memory.
//   - A BURST ARMS ONE TIMER, however many characters it holds. The wakeup is
//     the pointer fold's shape exactly (coalesce.go): one is in flight or none
//     is, and the one in flight re-arms itself while characters are still
//     arriving rather than a second one being asked for.
//   - A SETTLED BURST ASKS THE DISK ONCE PER CANDIDATE IN AT MOST FOUR READINGS,
//     and only after the string gate above has passed. A burst that names nothing
//     changes nothing and BUILDS NO FRAME, because it provably mutated nothing
//     [app.View] reads.
//
// AND IT IS SILENT UNLESS IT IS CERTAIN. A burst still arriving spells a great
// many paths that do not exist yet — `/var/f`, `/var/fo` — and the settle may
// land on any of them. So the fold converts only when one complete terminal
// reading names existing REGULAR FILES on this machine, and says nothing at all
// otherwise: a note about a half-typed path would be this surface interrupting
// somebody who is still typing. The refusals a person should see — a folder, a
// file over the ceiling — belong to the door at enter, where the gesture is
// finished.

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
	// at and end bound the run inside the draft, in runes. A key that does not
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

// dropWatch is called with every character that types itself into the draft,
// and it is the whole of what ordinary typing pays for this file: two integer
// comparisons and, for a character that could open a path, one string test.
//
// at is where the character landed, before it was inserted.
func (a *app) dropWatch(at int, text string) tea.Cmd {
	a.drop.watched++
	runes := len([]rune(text))
	switch {
	case a.drop.open && a.drop.end == at:
		// The run continuing, character by character.
		a.drop.end = at + runes
	case dropOpener(text) && dropWordStart(a.input.value, at):
		// A character that could BEGIN a dropped path, standing where a word
		// begins. Anything else — a letter mid-word, a character after a caret
		// jump — is somebody typing, and the fold stays shut.
		a.drop.at, a.drop.end, a.drop.open = at, at+runes, true
	default:
		a.drop.open = false
		return nil
	}
	if a.drop.end > len(a.input.value) {
		// The draft moved underneath the fold. It is not a run any more.
		a.drop.open = false
		return nil
	}
	run := string(a.input.value[a.drop.at:a.drop.end])
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
	if !a.spendDrop() {
		a.ptr.still = true
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
	at, end := a.drop.at, a.drop.end
	if at < 0 || end > len(a.input.value) || end <= at {
		// The draft moved underneath the fold; there is no run to spend.
		a.drop.open = false
		return false
	}
	run := string(a.input.value[at:end])
	// A RUN THAT IS NOT A DROP LEAVES THE FOLD OPEN, which is what makes a drop
	// delivered one character at a time work at all: `/var/f` names nothing and
	// `/var/folders/x/shot.png` names something, and they are the same run three
	// hundred milliseconds apart. Closing on the first miss would shut the fold
	// on the second character of every slowly-typed path there is.
	if !droppedPathShape(run) || !a.droppedFiles(run) {
		return false
	}
	a.drop.open = false
	// THE RUN COMES OUT OF THE DRAFT BEFORE THE DOOR IS ASKED, because the door
	// reads the draft to decide. [app.pasteFiles] refuses a draft that starts
	// with `/` on purpose — `/attach ` followed by a dropped file is somebody
	// using the command exactly as documented — and a keystroke drop into an
	// EMPTY box puts its own `/` at the front of that draft. Taking the run out
	// first is what tells the two apart: what is left is the command, or
	// nothing at all.
	a.input.value = append(a.input.value[:at], a.input.value[end:]...)
	a.input.cursor = at
	a.editTags(at, end, 0)
	if a.pasteFiles(run) {
		a.drop.took++
		return true
	}
	// The door said no — the draft is a command and this is its argument, or a
	// file is over the ceiling and has been named. The characters go back
	// exactly where they were typed, in order, and the box is the box the
	// person was looking at.
	a.input.cursor = at
	a.input.insert(run)
	a.editTags(at, at, end-at)
	return false
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

// droppedPathShape reports whether a run of characters begins like the paths a
// terminal writes when a file is dropped on it — WITHOUT asking the disk
// anything, which is the whole point of it. The first whitespace-delimited token
// decides because the terminal writes the path first; later words may be the raw,
// unescaped spaces of that same filename.
//
// THE SEPARATOR INSIDE THE FIRST TOKEN IS WHAT TELLS A DROP FROM A COMMAND. `/help`,
// `/model`, `/export` and every other word this surface answers are one segment
// with nothing after them; `/var/folders/…` and `~/Desktop/…` are not. So a
// slash command never reaches a syscall, and the cost of typing one is exactly
// what it was before this file existed.
//
// A RUN THAT BEGINS WITH A REAL PATH STAYS OPEN THROUGH PROSE AFTER IT. That is
// the admitted cost of raw-space filenames: `/tmp/Screen Shot.png` and
// `/tmp/server.log is broken` cannot be told apart until the readings meet disk.
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
	return droppedWordShape(words[0])
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

// droppedFiles reports whether one complete terminal reading of a run names
// existing regular files on this machine. It is the fold's certainty, and it is
// SILENT: a run that names a folder, or nothing at all, simply is not a drop yet,
// and a surface that said so would be talking over somebody who is still typing.
func (a *app) droppedFiles(text string) bool {
	hits, looked := a.pasteResolve(text, true)
	a.drop.looked += looked
	return hits != nil
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
	hits, looked := a.pasteResolve(line, false)
	a.drop.looked += looked
	if hits == nil {
		return false
	}
	files := 0
	for _, hit := range hits {
		if hit.info.Mode().IsRegular() {
			files++
		}
	}
	held := len(a.chips)
	if !a.pasteFiles(line) {
		return false
	}
	a.drop.took++
	// WHAT HAPPENED IS SAID OUT LOUD, because this door is reached by somebody
	// who has just watched their file turn into a line of text and press enter.
	// The chips are the proof; the sentence is what stops them typing it again.
	//
	// A FOLDER SAYS ITS OWN SENTENCE AND IS NOT GIVEN A SECOND ONE. [app.pasteFiles]
	// answers "<name> is a folder · attach a file" and attaches nothing, and
	// "attached" underneath that would be this surface contradicting itself.
	if files == len(hits) && len(a.chips) > held {
		a.note(droppedNote(files))
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
