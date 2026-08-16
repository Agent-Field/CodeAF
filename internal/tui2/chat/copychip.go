package chat

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/tui2/clip"
)

// The copy chip's act (§3b: "hover any message → dim `copy` chip at its right
// edge → OSC 52 clipboard write").
//
// WHY THIS IS A THIRD COPY DOOR AND NOT A THIRD COPY RULE. copy.go's two chords
// take "the newest answer" and "the newest file" — the right things to bind to a
// key, because a keyboard has no way to point at a row. A pointer does, and what
// a reader means when they reach for the mouse is THIS message: the one they are
// reading, which may be six turns up. So the chip takes the block it is drawn
// on, and the bytes leave through the same door the chords use — internal/tui2/
// clip, which is [tea.SetClipboard], which is OSC 52 written by the runtime
// beside the frame. Nothing in this package writes to the terminal itself.
//
// WHAT IS COPIED IS THE RECORD, NOT THE RENDERING. The block keeps its journal
// row for exactly this (message.go's source field), so a paste carries the words
// and not the gutter glyph, the indents, the fold hint or the escape sequences
// that made them legible. That is the same rule copy.go states for the chords,
// held in one place: [messageBlock.copyText].

// copiedFor is how long the chip says `copied` before it goes back to saying
// `copy`.
//
// Long enough to be read, short enough that it is gone before the reader's next
// decision. It is not one of the three motions of §11 — nothing moves, one word
// is replaced by another for a moment — which is the only kind of feedback this
// surface has room for.
const copiedFor = 1200 * time.Millisecond

// copiedMsg clears the proof on one block. It carries the block's ID rather than
// the block, because a room rebuilds its list several times a second while a job
// runs (13.15's decision 2) and a pointer held across that rebuild would be
// clearing a flag on a block nobody is drawing any more.
type copiedMsg struct{ id string }

// copyMessage takes one message's words out and leaves one frame of proof.
//
// The order is what makes the feedback honest: the chip flips to `copied` in the
// same frame the write is issued, and it is armed to flip back on a tick rather
// than on the next thing that happens — a surface that cleared its own proof
// when the poll landed would say "copied" for a different length of time
// depending on how busy the room was.
func (a *App) copyMessage(block *messageBlock) tea.Cmd {
	if block == nil {
		return nil
	}
	text := block.copyText()
	if text == "" {
		// A row with no words in it — a bare artifact reference, a progress
		// line — has nothing to put on the clipboard, and 5.20's rule is that a
		// door which cannot open says so rather than pretending it did.
		a.status.err = "nothing to copy — this row carries no words"
		a.shell.Invalidate()
		return nil
	}
	a.status.err = ""
	block.SetCopied(true)
	a.shell.Invalidate()
	id := block.ID()
	return tea.Batch(clip.Write(text), tea.Tick(copiedFor, func(time.Time) tea.Msg {
		return copiedMsg{id: id}
	}))
}

// applyCopied puts the chip back to `copy` on the block the tick names.
//
// It looks the block up by id in the transcript that is on screen NOW, so a tick
// that outlived its block — the room changed, the list was rebuilt — clears
// nothing and costs nothing, rather than reaching through a stale pointer.
func (a *App) applyCopied(msg copiedMsg) {
	if a.transcript == nil {
		return
	}
	index, ok := a.transcript.IndexOf(msg.id)
	if !ok {
		return
	}
	block, isMessage := a.transcript.Block(index).(*messageBlock)
	if !isMessage {
		return
	}
	if block.SetCopied(false) {
		a.shell.Invalidate()
	}
}
