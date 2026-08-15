package head

import (
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// fork-this-conversation-into-a-task (8.2.12): "take what we just discussed and
// go do it."
//
// 8.2.12 is explicit that this is a SPAWN VARIANT, not a new mechanism, and that
// is the whole of the design. Everything that makes commissioning work safe —
// the fan-out cap, the consequence gate applied at the journaling door, the
// dedupe, the receipt assembled from what was actually journaled, the person's
// words travelling verbatim — lives inside the spawn tool, and a second door
// beside it would be a second set of those guards to keep in step. So this
// composes the instruction and then calls spawn, once. If a guard changes, it
// changes here too, because there is no "here".
//
// What it adds is the one thing spawn cannot do: carry the conversation. A
// spawned job sees its instruction and nothing else, which is right — a worker
// with the whole chat in its brief is a worker optimizing for the chat. But a
// person who has spent ten minutes settling what they want and then says "okay,
// go do it" has put the requirements in the conversation, and an instruction
// that arrives without them is a job that asks all of it again.
//
// The context travels as its OWN FIELD rather than pasted in as more
// instruction, because the two are not the same authority: the goal is what
// they asked for, and the transcript is evidence about what they meant. A
// worker that reads a passing remark as an order is the failure this framing
// exists to prevent — and so is a recognizer that reads the transcript as the
// ask, which is what it did for as long as the two shared one string (ask.go).

// The byte numbers here are floors now rather than ceilings: this is what
// they get on a window nobody could size, and budget.go raises them in
// proportion on a window with room to spare.
const (
	// forkContextTurns is how much conversation a fork inherits. It is the
	// person-window's own size: what was settled in this room's recent turns is
	// what "what we just discussed" means, and a fork that carried the whole day
	// would price every job by how long the chat had been open.
	forkContextTurns = threadWindowKeep
	// forkContextBytes bounds the inherited block. An instruction is a brief,
	// and a brief that is mostly transcript has stopped being one.
	forkContextBytes = 3 << 10
	// forkTurnBytes keeps one inherited turn to a paragraph, so one pasted file
	// cannot become the whole of what the work is told.
	forkTurnBytes = 300
)

// ForkedContextPrefix opens the inherited block in a pre-split instruction. It
// is the store's constant because the store is what reads old journal rows, and
// it stays named here because this is where the fence was minted for as long as
// there was a fence to mint.
//
// NOTHING WRITES IT ANY MORE. The two halves travel as two typed fields on the
// command (store.Command.Instruction and .Context), which is the whole of this
// fix: a fence in one string is only a fence to a reader that looks for it, and
// every reader downstream was reading the string as the person's own words. The
// composition still exists — store.Command.Brief — but it exists for the ONE
// consumer that is entitled to both halves, the compiler.
const ForkedContextPrefix = store.ForkedContextPrefix

// forkContext renders the recent conversation for the brief.
//
// It reuses the head's own window rather than reading the room again, which is
// not only cheaper: the window is the fold the head has already decided is this
// conversation, so the work inherits what the head was actually reasoning over
// rather than a second, differently-bounded opinion about the same room.
func (h *Head) forkContext(user store.Message) (string, error) {
	recent, err := h.recentThread(user.SessionID, user.Seq)
	if err != nil {
		return "", err
	}
	if len(recent) > forkContextTurns {
		recent = recent[len(recent)-forkContextTurns:]
	}
	var rendered strings.Builder
	for _, message := range recent {
		body := strings.TrimSpace(message.Body)
		if body == "" {
			continue
		}
		line := forkSpeaker(message) + ": " +
			truncateBytes(promptSafe(body), h.budget.forkTurn) + "\n"
		if rendered.Len()+len(line) > h.budget.fork {
			break
		}
		rendered.WriteString(line)
	}
	// The message being answered right now is part of what was discussed and is
	// not in the window, which reads everything BEFORE it.
	if body := strings.TrimSpace(user.Body); body != "" {
		rendered.WriteString("them: " + truncateBytes(promptSafe(body), h.budget.forkTurn) + "\n")
	}
	return strings.TrimSpace(rendered.String()), nil
}

// forkSpeaker names who said a line, in terms a worker reading the brief can
// act on. "them" is the person whose work this is; everything else is the
// system's own side of the conversation and carries less authority.
func forkSpeaker(message store.Message) string {
	switch message.Role {
	case store.RoleUser:
		return "them"
	case store.RoleSystem:
		return "the system"
	default:
		return "you, earlier"
	}
}
