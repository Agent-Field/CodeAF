package tui3

import (
	"path/filepath"
	"strings"
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
// THE SAME SHAPES MEANS THE SAME PAYLOAD. A replayed call used to be a line and
// nothing else: the entries carried a gloss, so clicking one opened an expansion
// with nothing in it — the write whose content was the reason to click, gone.
// The journal had it all along (the arguments ride the assistant message's
// tool_calls, the result is the tool message keyed by the same id), so
// [session.DisplayEntry] now carries both and a replayed row expands to the real
// thing: a write to its content, an edit to its diff, a bash to its output.
//
// What is still NOT redrawn is as deliberate: no spinners, no costs, no
// reasoning. A replayed call is a call that finished, and it is drawn quiet.
//
// And a row with NO payload — a file written before either was journaled, a call
// whose result never reached the file — is drawn as the line it always was and
// is NOT interactive: see [replayInert]. An expansion that opens on a blank is
// the defect this wave came to end, and offering one for a row that genuinely
// has nothing behind it would be the same defect wearing the fix's clothes.

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
			// The pictures are part of what was said, so a message that was only
			// a picture is still a message: the markers alone are the line, and
			// only a message with neither words nor attachments is skipped.
			line := replayUserLine(text, e.ImageRefs, a.pal)
			if line == "" {
				continue
			}
			// The turn counter moves with the person's messages, exactly as it
			// does live: it is what groups a cluster and what ctrl+o folds.
			a.turn++
			a.entries = append(a.entries, entry{kind: entryUser, text: line, turn: a.turn})

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
				// The detail is carried through UNPARSED, which is what makes a
				// replayed row the same row: everything the expansion shows — the
				// diff, the content preview, the highlighted command and its
				// output — is derived from these two fields at render time
				// (toolview.go), so a replayed call and a live one go through one
				// renderer and cannot disagree.
				detail: toolDetail{Args: e.Args, Output: e.Output},
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

// replayUserLine is a replayed message as the person sent it: their words, and
// the names of the pictures that went with them.
//
// It is [userLine]'s rule applied to what the journal kept — the same markers,
// the same hue, the same separator — because a message drawn one way when it is
// sent and another way when it is resumed is two records of one thing. The paths
// come from the journal (session's DisplayEntry.ImageRefs); the NAME is what is
// drawn, for the reason [chipMarkers] states: a terminal cell is not a place to
// show a picture, and a full path is not a thing anybody reads.
func replayUserLine(text string, refs []string, pal palette) string {
	pictures := make([]chip, 0, len(refs))
	for _, ref := range refs {
		ref = strings.TrimSpace(ref)
		if base := filepath.Base(ref); ref != "" && base != "." && base != string(filepath.Separator) {
			// A chip is exactly what the live tray holds, so the markers are drawn
			// by the same function from the same shape (attach.go): the path in,
			// the base name out.
			pictures = append(pictures, chip{path: ref})
		}
	}
	return userLine(text, pictures, pal)
}

// replayInert reports whether one entry is a tool row with NOTHING behind it:
// a call replayed from a journal that carried neither its arguments nor its
// result.
//
// It is the one row on this surface that must not answer the pointer. Every
// other tool row expands into something — the diff, the content, the output, or
// at worst the honest "—" of a call that returned nothing — but a row from an
// older file has no payload at all, and a hover that brightens and a click that
// opens a blank are a surface promising an answer it does not have.
//
// It is derived rather than flagged, so nothing has to be remembered: a live
// call always arrives with its arguments (they are what the model sent, and even
// a no-argument call sends `{}`), so an unresolved-status row is never inert and
// an empty payload on a finished row means exactly one thing.
//
// The two places that must ask it are the row builder — a row that is inert
// takes hitNone rather than hitTool, which takes it out of hover, click and the
// ↑/↓ walk in one move — and [app.openTool], which is reachable by key.
func replayInert(e *entry) bool {
	return e != nil && e.kind == entryTool && !e.status.live() &&
		e.detail.Args == "" && e.detail.Output == ""
}
