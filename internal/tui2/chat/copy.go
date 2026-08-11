package chat

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// Taking things out (JOURNEY 18, 7.2's T3).
//
// The registry has carried "copy answer" and "copy file" since before this
// surface existed, and both were unrunnable here: they name bare `y` and `Y`,
// and in a composer-first surface every printable character is text. Both rows
// now carry a chord beside the bare key — the projection Entry.On already
// performs for the receipts fold — so the strip names the key this surface
// really binds and the palette can run the row.
//
// The bytes leave through [tea.SetClipboard], which is OSC 52 written by the
// runtime beside the frame. That is the same discipline the terminal hooks
// keep and for the same reason: nothing in this package writes to the terminal
// itself, because a write outside Bubble Tea's output is the bug atomic frames
// exist to prevent.
//
// WHAT IS COPIED IS WHAT THE READER IS LOOKING AT. There is no block focus in
// this surface yet (7.2 T6 is unbuilt), so "the focused answer" resolves to the
// newest one in the room on screen — which is where a reader's eye is by
// construction, because the transcript is pinned to its live edge. The day
// block focus lands, these two functions take the focused block instead and
// nothing else moves.

// copyAnswerKey and copyFileKey are what this surface binds. They are named
// once so the ladder, the footer strip and the registry projection cannot
// drift apart.
const (
	copyAnswerKey = "ctrl+y"
	copyFileKey   = "alt+y"
)

// copyAnswer puts the newest answer in the room on screen on the clipboard.
func (a *App) copyAnswer() tea.Cmd {
	text := a.latestAnswer()
	if text == "" {
		a.status.err = "nothing to copy — no answer in this room yet"
		a.shell.Invalidate()
		return nil
	}
	a.status.err = ""
	a.shell.Invalidate()
	return tea.SetClipboard(text)
}

// copyFile puts the path of the newest artifact on the clipboard.
//
// It is a separate door from copyAnswer because 12.5.1's artifact law makes it
// one: a deliverable is a thing on disk referenced by a path, never prose, so
// "copy what it made" and "copy what it said" are two different acts on two
// different objects and a single key that guessed between them would be wrong
// half the time.
func (a *App) copyFile() tea.Cmd {
	path := a.latestArtifact()
	if path == "" {
		a.status.err = "nothing to copy — no file in this room yet"
		a.shell.Invalidate()
		return nil
	}
	a.status.err = ""
	a.shell.Invalidate()
	return tea.SetClipboard(path)
}

// latestAnswer is the newest answer's prose, read off the journal rows this
// window holds rather than off the screen.
//
// Off the JOURNAL and not off the rendered rows, deliberately: the rendering
// carries indents, glyphs, fold hints and a header, none of which a person
// wants in a paste. What they asked for is what was said.
func (a *App) latestAnswer() string {
	for i := a.messagesLen() - 1; i >= 0; i-- {
		message, ok := a.messageAt(i)
		if !ok || message.Role != store.RoleAgent {
			continue
		}
		if body := strings.TrimSpace(message.Body); body != "" {
			return body
		}
	}
	return ""
}

// latestArtifact is the newest deliverable's path.
func (a *App) latestArtifact() string {
	for i := a.messagesLen() - 1; i >= 0; i-- {
		message, ok := a.messageAt(i)
		if !ok {
			continue
		}
		for j := len(message.Parts) - 1; j >= 0; j-- {
			part := message.Parts[j]
			if part.Kind != store.PartArtifact || part.Artifact == nil {
				continue
			}
			if path := strings.TrimSpace(part.Artifact.Path); path != "" {
				return path
			}
		}
	}
	return ""
}

// messagesLen and messageAt read the rows the room on screen is drawing.
//
// The blocks keep their message because copying needs the record and not the
// rendering; holding it costs one pointer per row and saves a second read of
// the store on a keystroke a person expects to be instant.
func (a *App) messagesLen() int {
	transcript := a.pane.transcript
	if transcript == nil {
		return 0
	}
	return transcript.Len()
}

func (a *App) messageAt(i int) (store.Message, bool) {
	transcript := a.pane.transcript
	if transcript == nil {
		return store.Message{}, false
	}
	block, ok := transcript.Block(i).(*messageBlock)
	if !ok || block.source == nil {
		return store.Message{}, false
	}
	return *block.source, true
}
