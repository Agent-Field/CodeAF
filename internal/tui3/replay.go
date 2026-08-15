package tui3

import (
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// REPLAY ON RESUME: a resumed conversation opens showing itself.
//
// A session that is picked up rather than created has a transcript the model
// can see and the person cannot, and a surface that opened on an empty screen
// would be asking somebody to hold in their head the thing it is holding on
// disk. So the tail is drawn — in the SAME shapes the live surface draws, from
// the same renderers, because two renderings of one conversation is how a
// replayed screen starts lying about what happened.
//
// What is NOT redrawn is as deliberate: no tool results (the entries carry
// none — internal/session's [session.DisplayEntry] is the conversation, not the
// wire), no spinners, no costs. A replayed call is a call that finished.

// replayTail is how much of a resumed conversation is drawn. Forty entries is
// about two screens of scrollback — enough to remember where you were, short of
// re-rendering an hour of work nobody is going to scroll back through.
const replayTail = 40

// replay folds the agent's transcript into entries. It runs once, at
// construction, before the surface has drawn anything.
func (a *app) replay() {
	if a.agent == nil {
		return
	}
	entries := a.agent.Transcript()
	if len(entries) > replayTail {
		entries = entries[len(entries)-replayTail:]
	}
	for _, e := range entries {
		text := strings.TrimSpace(e.Text)
		switch e.Role {
		case "user":
			if text == "" {
				continue
			}
			// The turn counter moves with the person's messages, exactly as it
			// does live: it is what groups a cluster and what ctrl+o folds.
			a.turn++
			a.entries = append(a.entries, entry{kind: entryUser, text: text, turn: a.turn})

		case "assistant":
			if text == "" {
				continue // a step that only called tools; its calls follow
			}
			a.entries = append(a.entries, entry{
				kind: entryAssistant, text: text, turn: a.turn, settled: true,
			})

		case "tool":
			// A tool entry with no name is a RESULT message from the wire, not
			// a call. The cluster shows calls.
			if strings.TrimSpace(e.Tool) == "" {
				continue
			}
			a.entries = append(a.entries, entry{
				kind: entryTool, tool: e.Tool, text: e.Hint, turn: a.turn, status: toolOK,
			})

		case "note":
			if text == "" {
				continue
			}
			a.entries = append(a.entries, entry{
				kind: entryDivider, text: firstLine(text), turn: a.turn,
			})
		}
	}
	a.touch()
}

// contextBytes is how much conversation the model is carrying, in bytes, as the
// replay accessor can measure it.
func contextBytes(entries []session.DisplayEntry) int {
	total := 0
	for _, e := range entries {
		total += len(e.Text) + len(e.Hint) + len(e.Tool)
	}
	return total
}
