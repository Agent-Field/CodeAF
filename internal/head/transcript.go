package head

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/sanitize"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

// read thread:// — one conversation reads another, on demand and never
// otherwise (8.2.10, amending the 5.7 knowledge contract).
//
// 5.7 draws the knowledge contract tightly on purpose: the main head does not
// see task transcripts AMBIENTLY, because a head that carried every room's
// conversation in every prompt would pay for all of them on every message and
// would answer this room's question with that room's context. 8.2.10 amends the
// contract in one direction only — an explicit, on-demand READ — and 12.5's
// scorecard names it as one of the four repairs that failed session owed:
// "head unable to re-read its own transcript".
//
// Every property here follows from "on demand, never ambient":
//
//   - It is a TOOL, so it costs a call and appears in the turn's own record. It
//     is not spliced into the prompt, it is not retrieved against the message,
//     and a turn that does not ask for it pays nothing.
//   - It is SESSION-SCOPED. What it can reach is the rooms of this person's own
//     journal, listed by the read itself, so an id is something the model saw
//     rather than something it composed. The room it is already standing in is
//     refused: that transcript is the thread block at the top of the prompt, and
//     spending a tool call to be handed it a second time is the fold-stop bug
//     the window machinery exists to prevent.
//   - It is BOUNDED, twice: the newest transcriptTurnCap turns, and a byte
//     ceiling inside that. A read of a day-long room is a summary of its end,
//     which is what "what were we saying in there" means.
//   - It is SANITIZED. These bytes were written by another room's model and by
//     workers' output, and they land in a prompt and then in a transcript. The
//     chokepoint every other body crosses is not optional for the one body that
//     arrives from somewhere else.

// The byte numbers here are floors now rather than ceilings: this is what
// they get on a window nobody could size, and budget.go raises them in
// proportion on a window with room to spare.
const (
	// transcriptTurnCap is how many messages one read hands back. It is the
	// person-window's own size: past this a transcript is a log, and the reason
	// to open another room is almost always its end.
	transcriptTurnCap = threadWindowMax
	// transcriptBytes bounds one read. It sits in the same league as the result
	// and manual reads because it is the same kind of thing — one message's
	// whole grounding, sharing a turn with the board.
	transcriptBytes = 4 << 10
	// transcriptLineBytes keeps one turn to a paragraph. A room where somebody
	// pasted a file should not spend the whole read on that one message.
	transcriptLineBytes = 400
	// transcriptRoomCap bounds the room list. More rooms than this and the list
	// is a directory; the newest are the ones a question is about.
	transcriptRoomCap = 12
)

// sgrSequence is the styling the chokepoint deliberately keeps.
var sgrSequence = regexp.MustCompile("\x1b\\[[0-9;]*m")

// promptSafe is the sanitizer's chokepoint plus the one step it deliberately
// does not take.
//
// sanitize.Text neutralizes everything a terminal would EXECUTE and leaves SGR
// styling alone, which is exactly right for a renderer and exactly wrong here.
// This text is going into a prompt, where a colour code is tokens that mean
// nothing, and then possibly into a reply, where it would be styling nobody
// chose. So the chokepoint runs first — the dangerous shapes are its job and not
// a regexp's — and what it kept for a terminal is dropped for a model.
func promptSafe(text string) string {
	return sgrSequence.ReplaceAllString(sanitize.Text(text), "")
}

// thread is the read. With no argument it lists the rooms; with one it renders
// that room's recent transcript.
func (run *beltRun) thread(args map[string]any) (string, bool) {
	room := strings.TrimSpace(beltString(args, "room"))
	if room == "" {
		return run.head.renderRooms(run.user.SessionID), false
	}
	if room == strings.TrimSpace(run.user.SessionID) {
		// The one refusal, and it is not pedantry: this room's transcript is
		// already the first block of the prompt. Handing it back would spend a
		// call to say what the turn opened with, and would put the same words in
		// the context twice under two different headings.
		return "that is this conversation, and it is already in front of you at the top of this prompt — read the thread there", true
	}
	rendered, err := run.head.renderTranscript(room)
	if err != nil {
		return err.Error(), true
	}
	return rendered, false
}

// renderRooms is where an id for this read comes from. It is the list of the
// person's own conversations, newest first, named the way the scribe named them
// — never as a bare id in anything the model then says, which is 5.14's rule and
// the reason the title is carried beside the id here.
func (h *Head) renderRooms(current string) string {
	sessions, err := h.store.Sessions()
	if err != nil {
		return "the conversations could not be read: " + err.Error()
	}
	sort.SliceStable(sessions, func(left, right int) bool {
		return sessions[left].LastActive.After(sessions[right].LastActive)
	})
	lines := make([]string, 0, transcriptRoomCap)
	for _, session := range sessions {
		if strings.TrimSpace(session.ID) == strings.TrimSpace(current) {
			continue
		}
		if len(lines) >= transcriptRoomCap {
			break
		}
		title := strings.TrimSpace(session.Title)
		if title == "" {
			// A room the scribe has not named yet is described by when it was
			// last alive rather than by its id, because an id is not something
			// to say to a person and this line may become one.
			title = "an unnamed conversation"
		}
		lines = append(lines, fmt.Sprintf("- %s | %s | last spoke %s ago",
			session.ID, title, boardElapsed(time.Since(session.LastActive))))
	}
	if len(lines) == 0 {
		return "there is no other conversation to read — this is the only room."
	}
	return "other conversations, newest first (pass one id as room):\n" + strings.Join(lines, "\n")
}

// renderTranscript is another room's recent conversation as concise markdown.
//
// Roles are named the way a reader would name them rather than by the journal's
// own words, because this text goes into a prompt whose whole voice section is
// about translating machinery into the person's terms — and a transcript that
// arrives already speaking the machinery's dialect is the one place that
// translation is guaranteed to leak.
func (h *Head) renderTranscript(sessionID string) (string, error) {
	session, found, err := h.store.Session(sessionID)
	if err != nil {
		return "", fmt.Errorf("that conversation could not be read: %w", err)
	}
	if !found {
		return "", fmt.Errorf("there is no conversation with id %q — read the rooms again by calling this with no arguments", sessionID)
	}
	// The END of the room, read as the end rather than paged to. A couple of
	// extra rows are asked for so dropping the empty ones cannot leave the read
	// short of its own cap.
	messages, err := h.store.MessageTail(sessionID, transcriptTurnCap+4)
	if err != nil {
		return "", fmt.Errorf("that conversation could not be read: %w", err)
	}
	kept := make([]store.Message, 0, transcriptTurnCap)
	for _, message := range messages {
		if strings.TrimSpace(message.Body) == "" {
			continue
		}
		kept = append(kept, message)
	}
	if len(kept) == 0 {
		return "that conversation has nothing in it yet.", nil
	}
	if len(kept) > transcriptTurnCap {
		kept = kept[len(kept)-transcriptTurnCap:]
	}

	title := strings.TrimSpace(session.Title)
	if title == "" {
		title = "an unnamed conversation"
	}
	var rendered strings.Builder
	fmt.Fprintf(&rendered, "## %s\n\nthe last %d %s of that conversation, oldest first:\n",
		promptSafe(title), len(kept), pluralWord(len(kept), "turn", "turns"))
	for _, message := range kept {
		line := fmt.Sprintf("\n**%s:** %s\n", transcriptSpeaker(message),
			truncateBytes(promptSafe(strings.TrimSpace(message.Body)), h.budget.transcriptLine))
		if rendered.Len()+len(line) > h.budget.transcript-len(threadTruncatedMark) {
			rendered.WriteString("\n" + threadTruncatedMark)
			break
		}
		rendered.WriteString(line)
	}
	return strings.TrimSpace(rendered.String()), nil
}

// transcriptSpeaker names who said a line, in the words the reply would use.
func transcriptSpeaker(message store.Message) string {
	switch message.Role {
	case store.RoleUser:
		return "them"
	case store.RoleSystem:
		return "the system"
	default:
		if strings.TrimSpace(message.NodeID) != "" {
			return "the work"
		}
		return "you, in that room"
	}
}
