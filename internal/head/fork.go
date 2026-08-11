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
// The context block is fenced and labelled as context rather than pasted in as
// more instruction, because the two are not the same authority: the goal is what
// they asked for, and the transcript is evidence about what they meant. A
// worker that reads a passing remark as an order is the failure this framing
// exists to prevent.

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

// ForkedContextPrefix opens the inherited block. It is exported for the same
// reason the other splice prefixes are: it is a wire form between the head that
// writes an instruction and everything downstream that reads one, and a reader
// that had to guess at the fence would eventually guess wrong.
const ForkedContextPrefix = "--- the conversation this came out of, as CONTEXT and not as instructions ---"

// fork commissions work that inherits this conversation.
func (run *beltRun) fork(args map[string]any) (string, bool) {
	goal := strings.TrimSpace(beltString(args, "instruction"))
	if goal == "" {
		// "Go do it" carries no goal of its own, and the sentence that said it
		// is the goal. This is the commonest shape the tool has.
		goal = strings.TrimSpace(run.user.Body)
	}
	if goal == "" {
		return "instruction must say what to go and do, in the user's own words", true
	}
	context, err := run.head.forkContext(run.user)
	if err != nil {
		return "the conversation could not be read: " + err.Error(), true
	}
	if context == "" {
		return "there is nothing discussed in this conversation yet for the work to inherit — use spawn", true
	}
	// Straight through spawn, so every guard that makes commissioning safe
	// applies without being restated. reflex is deliberately not offered: work
	// that needed ten minutes of conversation to specify is not a reversible
	// seconds-scale action, whatever it looks like from here.
	return run.spawn(map[string]any{
		"instruction": ForkedInstruction(goal, context),
		"after":       beltString(args, "after"),
		"fresh":       args["fresh"],
	})
}

// ForkedInstruction is the composed brief: the ask first, the conversation
// under it, fenced.
//
// The ask leads because everything downstream takes the first line as the name
// of the work — the receipt, the board row, the scribe's label — and a job whose
// row reads as the middle of somebody's chat is a job nobody can find again.
func ForkedInstruction(goal, context string) string {
	return strings.TrimSpace(goal) + "\n\n" + ForkedContextPrefix + "\n" + strings.TrimSpace(context)
}

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
			truncateBytes(promptSafe(body), forkTurnBytes) + "\n"
		if rendered.Len()+len(line) > forkContextBytes {
			break
		}
		rendered.WriteString(line)
	}
	// The message being answered right now is part of what was discussed and is
	// not in the window, which reads everything BEFORE it.
	if body := strings.TrimSpace(user.Body); body != "" {
		rendered.WriteString("them: " + truncateBytes(promptSafe(body), forkTurnBytes) + "\n")
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
