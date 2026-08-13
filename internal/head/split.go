package head

import (
	"fmt"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/thread"
)

// Split-on-divergence (chat-simplify J1) and the one cross-seam contract (5.4).
//
// A working conversation drifts. Somewhere in the middle of the pricing thread
// the person says "unrelated, but can you look at the deploy failures" — and
// until now that sentence had exactly one place to go: the pricing thread, where
// it stayed forever, taking the room's name with it and burying both subjects in
// one transcript. The product's answer is an OFFER, never chrome: the head asks
// whether that should be its own thread, and consistent yeses teach the ask gate
// to stop asking and simply do it.
//
// Settling it is the only place in the system where an answer moves the SURFACE,
// and the whole design of the seam is in one sentence: the coupling is
// journal-only. The gate writes two rows and nothing else.
//
//  1. In the OLD room, a system row whose body is "continuing in a new thread"
//     and which carries a store.PartRoomSwitch part naming the new room. The
//     surface applies it on its ordinary poll — composer and view re-point — and
//     a surface that has never heard of the part shows a readable line and is
//     merely out of date.
//  2. In the NEW room, the person's pivot message reposted VERBATIM under their
//     own role, so the new thread opens with what they actually said and the
//     head answers it there on the ordinary poll.
//
// The row in the old room is the one system-voiced thing the head writes, and it
// is written directly rather than through a postSystem helper because 13.18's
// law stands: the head has three things it may SAY, and this is not one of them.
// It is not speech. It is the window moving, drawn as a ruled line rather than
// as a sentence somebody said, and giving it a voice would make the colleague
// narrate its own furniture.
//
// The new room is not named here. The scribe names it from its first exchange on
// its ordinary post-turn lane, which is moments later and is the same lane every
// other room is named on — inventing a title at mint time would be a second
// namer to keep in step with the first.

// roomSwitchBody is the old room's last line, and it is a wire form: the surface
// renders the part rather than the prose, but a reader scrolling the old thread
// weeks later reads this.
const roomSwitchBody = "continuing in a new thread"

// splitThread mints the new room, journals the switch in the old one, and
// reposts the pivot into the new one. It returns the new room's id.
//
// The order of the two writes is the order a reader needs them in: the switch
// lands first, so a surface polling between the two writes moves to a room that
// already exists and then watches the person's own words arrive in it. The
// reverse order would show an empty room for a poll tick and then rewrite it.
func (h *Head) splitThread(oldSessionID string, pivot store.Message) (string, error) {
	oldSessionID = strings.TrimSpace(oldSessionID)
	if h == nil || h.store == nil || oldSessionID == "" {
		return "", fmt.Errorf("split thread: no conversation to split")
	}
	words := strings.TrimSpace(pivot.Body)
	if words == "" {
		return "", fmt.Errorf("split thread: there are no words to carry over")
	}

	// The new room inherits the old one's surface, because it is the same person
	// at the same window and the surface is how a room says which kind of lens
	// opened it.
	surface := "tui"
	if session, found, err := h.store.Session(oldSessionID); err == nil && found {
		if named := strings.TrimSpace(session.Surface); named != "" {
			surface = named
		}
	}
	opened, err := h.store.OpenSession(store.NewSessionID(), "", surface)
	if err != nil {
		return "", fmt.Errorf("split thread: open the new room: %w", err)
	}

	if _, err := thread.Post(h.store, store.Message{
		SessionID: oldSessionID,
		Role:      store.RoleSystem,
		Body:      roomSwitchBody,
		Parts:     []store.MessagePart{store.RoomSwitchRef(opened.ID)},
	}); err != nil {
		return "", fmt.Errorf("split thread: mark the switch: %w", err)
	}

	// Verbatim, under their own role, with what they attached and the model they
	// asked this turn for. Anything less than verbatim would mean the new thread
	// opens on the head's paraphrase of the person, which is the one thing a
	// transcript may never contain.
	if _, err := thread.Post(h.store, store.Message{
		SessionID:   opened.ID,
		Role:        store.RoleUser,
		Body:        words,
		Attachments: pivot.Attachments,
		Model:       pivot.Model,
	}); err != nil {
		return "", fmt.Errorf("split thread: carry their words over: %w", err)
	}
	return opened.ID, nil
}

// splitPivot is the message a split is about: the person's own words that
// diverged, which is the newest thing they said before the answer that settled
// the offer. The offer itself is an agent row and the answer is the row after
// it, so the pivot is one step further back.
//
// It falls back to the answering message only when there is nothing before it,
// which is a shape that should not occur and would otherwise open a new thread
// with the word "yes" in it.
func (h *Head) splitPivot(sessionID string, beforeSeq int64) (store.Message, bool) {
	messages, err := h.store.MessageTail(sessionID, threadBriefTail)
	if err != nil {
		return store.Message{}, false
	}
	for index := len(messages) - 1; index >= 0; index-- {
		message := messages[index]
		if message.Seq >= beforeSeq || message.Role != store.RoleUser {
			continue
		}
		if strings.TrimSpace(message.Body) == "" {
			continue
		}
		return message, true
	}
	return store.Message{}, false
}

// splitAnswered is the deterministic gate: the split offer came back, and the
// answer was the offer's own first option.
//
// Option one is the whole test, and it is not a guess about wording. A
// categorized ask puts its LEARNED DEFAULT on option one — that is what
// askLearned journals as the offered default and what the acceptance projection
// measures — so "the option the gate would eventually assume" and "yes, its own
// thread" are the same row by construction. Anything else, including free text,
// is not a yes and travels on to the loop as an ordinary answer.
func splitAnswered(question store.AgentQuestion, body string) bool {
	if question.Category != store.QuestionCategoryThreadSplit || len(question.Options) == 0 {
		return false
	}
	option, selected := selectQuestionOption(body, question.Options)
	return selected && option.Value == question.Options[0].Value
}

// settleThreadSplit performs a settled split and reports whether it did. It is
// called from the answer gate, after the row has been settled for the
// measurement, and it ENDS the turn: the pivot is answered in the new room by
// the ordinary poll, so a reply here would be the head answering in a room the
// person has already been moved out of.
func (h *Head) settleThreadSplit(user store.Message) (bool, error) {
	pivot, found := h.splitPivot(user.SessionID, user.Seq)
	if !found {
		return false, nil
	}
	if _, err := h.splitThread(user.SessionID, pivot); err != nil {
		return false, err
	}
	return true, nil
}

// -- naming a thread ---------------------------------------------------------

// threadTarget reads the words a person points at a conversation with. There is
// no id for "this thread": the room's id is never shown, so pointing is the only
// way they could ever name it.
func threadTarget(target string) bool {
	switch strings.ToLower(strings.TrimSpace(target)) {
	case "thread", "this thread", "the thread", "conversation", "this conversation",
		"the conversation", "room", "this room", "the room", "chat", "this chat":
		return true
	default:
		return false
	}
}

// changeThread is the one thing a thread can be changed into: a different name.
//
// Naming is the scribe's job and stays so — this is the door for the person
// overriding it, which is the fourth clause of the chats law ("naming is the
// scribe's job, silently; 'call this thread X' works as a change"). It is
// deterministic rather than a command, because there is nothing on the far side
// to interpret: a rename is a rename, and the revision judge holds plans.
func (run *beltRun) changeThread(sessionID, words string) (string, bool) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return "there is no conversation to rename here", true
	}
	title := threadNameFrom(words)
	if title == "" {
		return "that did not say what to call it — pass the new name as the words, or say \"call this thread <name>\"", true
	}
	renamed, err := run.head.store.RenameSession(sessionID, title)
	if err != nil {
		return "that conversation could not be renamed: " + err.Error(), true
	}
	run.acted = true
	return "this conversation is now called " + quoted(renamed.Title), false
}

// threadNameFrom pulls the name out of the person's sentence.
//
// The lead-ins below are the shapes people actually use, and everything after
// one of them is the name; a sentence with no lead-in IS the name when it is
// short enough to be one. The bound is what stops "we should probably rename
// this at some point when the pricing work settles down" from becoming a room
// title — a name is a few words, and a sentence is a request that was not one.
func threadNameFrom(words string) string {
	name := strings.TrimSpace(words)
	lower := strings.ToLower(name)
	for _, lead := range []string{
		"call this thread", "call this conversation", "call this room", "call this chat",
		"call it", "name this thread", "name this conversation", "name this room",
		"name it", "rename this thread to", "rename this conversation to",
		"rename this thread", "rename this conversation", "rename this to", "rename this",
		"title this", "let's call this", "lets call this",
	} {
		if strings.HasPrefix(lower, lead) {
			name = strings.TrimSpace(name[len(lead):])
			break
		}
	}
	name = strings.TrimSpace(strings.TrimLeft(name, `:"'“‘`))
	if fields := strings.Fields(name); len(fields) > scribeTitleWords {
		return ""
	}
	return roomTitle(name)
}

// splitAssumed is the same settlement reached without asking: the gate has
// learned that this person always wants the new thread, so the offer is skipped
// and the split simply happens.
//
// It runs inside the ask tool rather than after it because that is where the
// assumption is made, and the words it hands back are what the loop is told: the
// turn is over, the switch row is the clause, and the person's message is
// answered where it now lives.
func (run *beltRun) splitAssumed(preferred string, samples int) (string, bool) {
	if _, err := run.head.splitThread(run.user.SessionID, run.user); err != nil {
		return "that could not be taken into its own thread: " + err.Error(), true
	}
	run.acted, run.spoke = true, true
	return fmt.Sprintf("taken as its own thread (assumed: %s, learned from %d earlier answers). "+
		"Their words are carried over verbatim and are answered there; this room has already said so and needs nothing more.",
		preferred, samples), false
}
