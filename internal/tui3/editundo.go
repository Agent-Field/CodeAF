package tui3

// ── TAKING BACK WHAT YOU JUST TYPED ─────────────────────────────────────────
//
// `ctrl+z` puts the box back the way it was, and `ctrl+shift+z` puts it forward
// again. Every text field a person has ever used answers those two, and no box
// on this surface answered either: a sentence rewritten by a `ctrl+u` that went
// one word too far, a paste that landed in the wrong draft, a word kill on the
// wrong side of the caret — all of them were gone, and the only way back was to
// type the words again. The owner asked for it in as many words ("maybe even
// ctrl+z as well like undo or redo like shift z").
//
// ── ctrl+z REACHES US, AND THAT IS NOT AN ACCIDENT ──
//
// `ctrl+z` is the shell's own suspend, and it is only the shell's while the
// terminal is in cooked mode. This program runs raw — bubbletea puts the tty in
// raw mode, which clears ISIG, and it suspends only on a `tea.Suspend` command
// this surface never issues — so the byte arrives here as an ordinary keypress
// and binding it takes nothing away from anybody.
//
// `ctrl+shift+z` is the other half and it is NOT universal. Shift is invisible
// on a control byte: a terminal that has not taken the kitty keyboard
// protocol's disambiguation flag sends 0x1a for both chords, so on that
// terminal the redo arrives as a second undo. That is stated on the keys page
// rather than papered over, and no second redo spelling is bound to hide it —
// `ctrl+y` is already the copy-a-path key on home and in `/files`, and taking a
// working key off a screen to fix a chord that works on most terminals is the
// trade this codebase refuses everywhere else.
//
// ── A STEP IS A WORD, NOT A KEYSTROKE ──
//
// An undo that gave back one character per press is an undo nobody uses. So a
// run of ordinary typing FOLDS into one step and the step breaks where a word
// does — at the first letter after a space — which is what every editor has
// meant by ctrl+z since editors had one. A run of deletes folds the same way, a
// paste is a step of its own, and switching from typing to deleting starts a
// new step because the hand plainly changed what it was doing.
//
// The break is decided by the TEXT and never by a clock. A time-based
// coalescer would be a test that passes on a fast machine and fails on a loaded
// one, and this package's suite already runs on a two-core runner.

// undoDepth is how many steps back a box remembers, and undoRuneBudget how much
// text those steps may hold between them.
//
// THE BUDGET IS THE ONE THAT MATTERS, because a snapshot is a COPY of the whole
// draft: the box holds pasted stack traces and four-thousand-line files, and
// sixty-four snapshots of one of those is megabytes kept alive for a chord
// nobody may press. Depth bounds the ordinary case and the budget bounds the
// pathological one — the oldest steps are dropped until the whole stack fits,
// so a big paste keeps a few steps and a typed sentence keeps all of them.
const (
	undoDepth      = 64
	undoRuneBudget = 1 << 20
)

// editSnap is one draft as it stood before an edit: the text and where the
// caret was in it, so an undo puts the person back where they were typing and
// not at the end of a sentence they did not write.
//
// THE VALUE IS COPIED AND NOT ALIASED. [editor.insert] and the deletes rewrite
// the rune slice IN PLACE — `append(e.value[:e.cursor], …)` — so a snapshot
// that held the same backing array would be quietly rewritten by the very edit
// it exists to undo.
type editSnap struct {
	value  []rune
	cursor int
}

// editRun names what a run of keystrokes is doing, so a new step is kept only
// when the hand changes what it is doing.
type editRun int

const (
	runNone editRun = iota
	runInsert
	runDelete
	// runWhole is an edit that is a step on its own: a paste, a word kill, a
	// line kill, a selection replaced, a draft swapped whole.
	runWhole
)

// remember keeps the draft as it stands so an undo can come back to it. It is
// called BEFORE the edit, by the editor's own mutators, which is what makes it
// impossible for a box on this surface to have an edit the undo does not know
// about — there is one place text changes and this is inside it.
//
// breaks is what a run of one kind of edit splits on: for typing it is the
// first letter after a space, which is where a word begins.
func (e *editor) remember(run editRun, breaks bool) {
	// A NEW EDIT RETIRES THE FUTURE. Typing after an undo is the branch a person
	// chose, and a redo that stepped back onto the branch they abandoned would
	// hand them a sentence they had already rejected.
	e.ahead = nil
	if len(e.past) > 0 && run != runWhole && run == e.run && !breaks {
		return
	}
	e.past = append(e.past, editSnap{value: append([]rune(nil), e.value...), cursor: e.cursor})
	e.run = run
	e.trimUndo()
}

// trimUndo drops the oldest steps until the stack is inside both of its bounds.
func (e *editor) trimUndo() {
	held := 0
	for _, snap := range e.past {
		held += len(snap.value)
	}
	for len(e.past) > 1 && (len(e.past) > undoDepth || held > undoRuneBudget) {
		held -= len(e.past[0].value)
		e.past = e.past[1:]
	}
}

// forgetUndo throws the whole history away. It is what a SENT message does:
// the words have left the box, the recall history is where they live now
// (`↑` walks it), and an undo that pulled a sent sentence back into the draft
// would be one gesture with two unrelated meanings.
func (e *editor) forgetUndo() {
	e.past, e.ahead, e.run, e.runSpace = nil, nil, runNone, false
}

// undo puts the draft back one step, and reports whether there was one.
func (e *editor) undo() bool {
	if len(e.past) == 0 {
		return false
	}
	back := e.past[len(e.past)-1]
	e.past = e.past[:len(e.past)-1]
	e.ahead = append(e.ahead, editSnap{value: append([]rune(nil), e.value...), cursor: e.cursor})
	e.restore(back)
	return true
}

// redo puts it forward again, and reports whether there was anywhere to go.
func (e *editor) redo() bool {
	if len(e.ahead) == 0 {
		return false
	}
	forward := e.ahead[len(e.ahead)-1]
	e.ahead = e.ahead[:len(e.ahead)-1]
	e.past = append(e.past, editSnap{value: append([]rune(nil), e.value...), cursor: e.cursor})
	e.restore(forward)
	return true
}

// restore is the half both of them share.
//
// THE DEMOTED TAGS GO. They are offsets into the text that was, and carrying
// them onto text that is would mark a slash word plain somewhere in the middle
// of an unrelated sentence — so a step back leaves every tag in the restored
// draft live again, which is what it was before the person demoted it.
func (e *editor) restore(snap editSnap) {
	e.value = append(e.value[:0], snap.value...)
	e.cursor = min(max(snap.cursor, 0), len(e.value))
	e.demotedTags = nil
	e.picked = false
	// The next edit starts a step of its own: a run that folded into the step
	// this undo just took off would be an edit with nowhere to go back to.
	e.run, e.runSpace = runNone, false
}
