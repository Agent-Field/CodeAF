package head

import (
	"fmt"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// The re-entry brief, and the alive glance (chat-simplify J4, J5).
//
// A thread is a working conversation: alive for days, commissioning tasks along
// the way, taking their results back, and resumable after weeks. What made
// resuming one feel like starting over was not memory — the thread window has
// always carried the recent transcript — but ARC. The head could read the last
// few messages and had no way to say what the conversation was FOR, what it had
// set going, or what was still hanging; so it answered the new message from a
// standing start while the person carried the whole history in their head.
//
// This file is the projection that closes that gap, and its most important
// property is what it does NOT do:
//
//   - It costs no provider call. Every line is read off the journal — the room's
//     title, its opening ask, the jobs its own splices stamped with it, the
//     artifacts its messages referenced, the end of the transcript. A brief that
//     had to be composed would be a second of silence in front of the first
//     answer after every gap, which is exactly where a conversation can least
//     afford one.
//   - It stores nothing. There is no brief table and no cached arc: the journal
//     is the record and this is a read over it, so a brief is correct after a
//     rebuild for the same reason the transcript is.
//   - It says only what it can derive. The v1 arc is structural, and the one
//     judgment in it — what is still OPEN — is decided by the shape of the
//     thread's end rather than by reading its meaning. Being honestly narrow is
//     the point: the model is the party that turns this into a sentence, and it
//     does that better from four true facts than from a paragraph of guesswork.

const (
	// threadGap is how long a room has to have been silent before its next
	// message is a RE-ENTRY rather than the next line of a conversation. It
	// matches the arrival brief's own default (config.DefaultBriefAfter) on
	// purpose: the two answer the same question about the same person, and a
	// window that briefed on arrival while the head carried on mid-sentence
	// would be two components disagreeing about whether they had been away.
	threadGap = 4 * time.Hour
	// threadBriefBytes bounds the injected block. It is a paragraph of grounding
	// under a prompt that already carries the transcript, the board and the
	// notebook; past this it starts crowding out the thing it is grounding.
	threadBriefBytes = 1536
	// threadBriefJobs is how many of the thread's own jobs are named. A
	// conversation that commissioned a dozen is described by its recent ones —
	// the rest are a read away, and the board is in the same prompt.
	threadBriefJobs = 4
	// threadBriefFiles is the same bound for deliverables.
	threadBriefFiles = 3
	// threadBriefLineBytes keeps one quoted line to a line.
	threadBriefLineBytes = 180
	// threadBriefTail is how far back the room is read to build the arc.
	threadBriefTail = 40
	// threadParked is how long a thread sits on its open thing before the head
	// may mention it unprompted. A thread parked since this morning is an
	// ordinary working conversation; one parked since yesterday is something the
	// person has lost track of.
	threadParked = 18 * time.Hour
)

// reentryBlock is the prompt's re-entry brief, or the empty string when the
// conversation never went away.
//
// The gap is measured from the last thing the room said BACK — the newest
// non-user row before this message — rather than from the newest row of any
// kind. Two reasons, and both are bugs the obvious reading would have: a run of
// messages typed in one breath is folded into a single turn, so the newest row
// before the turn is often the person's own first sentence a second earlier;
// and a person who typed into a room at midnight and got no answer has not
// re-entered anything when they type again at nine, they are still waiting.
func (h *Head) reentryBlock(user store.Message, now time.Time) string {
	if h == nil || h.store == nil {
		return ""
	}
	previous, found := h.lastRoomVoice(user.SessionID, user.Seq)
	if !found {
		return ""
	}
	idle := now.Sub(previous.Time)
	if idle < threadGap {
		return ""
	}
	arc := h.threadBrief(user.SessionID, user.Seq, now)
	if arc == "" {
		return ""
	}
	return "Picking this thread back up — it has been quiet for " + threadElapsed(idle) + ".\n" +
		arc + "\n" +
		"Open your reply by re-entering that arc in your own words — one sentence saying where the two of you left it — before you answer what they just said."
}

// lastRoomVoice is the newest row in the room that was not the person typing,
// before the given sequence. It is the room's own clock for "when did we last
// have an exchange in here".
func (h *Head) lastRoomVoice(sessionID string, beforeSeq int64) (store.Message, bool) {
	messages, err := h.store.MessageTail(sessionID, threadBriefTail)
	if err != nil {
		return store.Message{}, false
	}
	for index := len(messages) - 1; index >= 0; index-- {
		message := messages[index]
		if beforeSeq > 0 && message.Seq >= beforeSeq {
			continue
		}
		if message.Role == store.RoleUser || strings.TrimSpace(message.Body) == "" {
			continue
		}
		return message, true
	}
	return store.Message{}, false
}

// threadBrief is the arc itself: what this thread is about, what it set going
// and what came of it, what it produced, where it was left, and the one thing
// still open. Empty when there is no conversation behind the message yet.
//
// beforeSeq excludes the message being answered, because the arc is what was
// true when the person came back — their new sentence is the thing the arc is
// grounding, not part of it.
func (h *Head) threadBrief(sessionID string, beforeSeq int64, now time.Time) string {
	sessionID = strings.TrimSpace(sessionID)
	if h == nil || h.store == nil || sessionID == "" {
		return ""
	}
	messages, err := h.store.MessageTail(sessionID, threadBriefTail)
	if err != nil {
		return ""
	}
	kept := make([]store.Message, 0, len(messages))
	for _, message := range messages {
		if beforeSeq > 0 && message.Seq >= beforeSeq {
			continue
		}
		if strings.TrimSpace(message.Body) == "" {
			continue
		}
		kept = append(kept, message)
	}
	if len(kept) == 0 {
		return ""
	}

	var lines []string
	lines = append(lines, "This thread: "+h.threadSubject(sessionID))
	if opening := h.threadOpening(sessionID); opening != "" {
		lines = append(lines, "It opened on: "+opening)
	}
	if commissioned := h.threadWork(sessionID, now); commissioned != "" {
		lines = append(lines, "Work it commissioned: "+commissioned)
	}
	if files := threadFiles(kept); files != "" {
		lines = append(lines, "Files it produced: "+files)
	}
	if left := threadLastExchange(kept); left != "" {
		lines = append(lines, "Where it was left: "+left)
	}
	if open := h.threadOpenThing(sessionID, kept, now); open != "" {
		lines = append(lines, "Still open: "+open)
	}
	return truncateBytes(strings.Join(lines, "\n"), threadBriefBytes)
}

// threadSubject is the room's name, or an honest admission that it has none
// yet. The scribe names a room from its first exchange, so an unnamed room is
// almost always a very young one.
func (h *Head) threadSubject(sessionID string) string {
	session, found, err := h.store.Session(sessionID)
	if err != nil || !found {
		return "this conversation, unnamed so far"
	}
	if title := strings.TrimSpace(session.Title); title != "" {
		return quoted(promptSafe(title))
	}
	return "this conversation, unnamed so far"
}

// threadOpening is the person's first words in the room. It is read from the
// start rather than from the window, because what a conversation is ABOUT is
// said at its beginning and the window has long since cut past it.
func (h *Head) threadOpening(sessionID string) string {
	messages, err := h.store.Messages(sessionID, 0, 24)
	if err != nil {
		return ""
	}
	for _, message := range messages {
		if message.Role != store.RoleUser || strings.TrimSpace(message.Body) == "" {
			continue
		}
		return quoted(threadLine(message.Body))
	}
	return ""
}

// threadWork is the jobs this thread commissioned and what came of them. The
// provenance stamp is the whole of the link: a splice records the room that
// asked for it on every node it admits, so this is a fact rather than a guess.
func (h *Head) threadWork(sessionID string, now time.Time) string {
	nodes, err := h.store.SessionNodes(sessionID, threadBriefJobs*3)
	if err != nil || len(nodes) == 0 {
		return ""
	}
	parts := make([]string, 0, threadBriefJobs)
	for _, node := range nodes {
		if len(parts) >= threadBriefJobs {
			break
		}
		if node.ID == store.RootID || !beltAddressable(node) {
			continue
		}
		parts = append(parts, surgeryTargetLabel(node)+" — "+lensNodeStateWord(node, now))
	}
	return strings.Join(parts, "; ")
}

// threadFiles is what this thread put on disk, newest first. Artifacts are read
// off the message parts rather than off the graph because the artifact law puts
// every deliverable through the same typed reference, whoever produced it — the
// head writing a document in conversation and a job returning one both land
// here.
func threadFiles(messages []store.Message) string {
	seen := make(map[string]bool)
	paths := make([]string, 0, threadBriefFiles)
	for index := len(messages) - 1; index >= 0; index-- {
		for _, part := range messages[index].Parts {
			if part.Kind != store.PartArtifact || part.Artifact == nil {
				continue
			}
			path := strings.TrimSpace(part.Artifact.Path)
			if path == "" || seen[path] {
				continue
			}
			seen[path] = true
			if paths = append(paths, path); len(paths) >= threadBriefFiles {
				return strings.Join(paths, ", ")
			}
		}
	}
	return strings.Join(paths, ", ")
}

// threadLastExchange is the end of the conversation in one line each way. It is
// the smallest honest answer to "where were we": the transcript window above it
// carries the detail, and this says which end of it to look at.
func threadLastExchange(messages []store.Message) string {
	var them, you string
	for index := len(messages) - 1; index >= 0; index-- {
		message := messages[index]
		if them == "" && message.Role == store.RoleUser {
			them = threadLine(message.Body)
		}
		if you == "" && message.Role == store.RoleAgent {
			you = threadLine(message.Body)
		}
		if them != "" && you != "" {
			break
		}
	}
	switch {
	case them != "" && you != "":
		return "them, " + quoted(them) + "; you, " + quoted(you)
	case them != "":
		return "them, " + quoted(them)
	case you != "":
		return "you, " + quoted(you)
	default:
		return ""
	}
}

// threadOpenThing is the one unresolved shape at the end of this thread, said in
// the words a reply would use. It goes through the same store reader the alive
// glance and the surface's board home use, so the three cannot disagree about
// whether a thread is still hanging.
// messages is the room as it stood BEFORE the message being answered, and that
// bound is the whole reason this does not simply ask OpenThreads: their new
// sentence is unanswered by construction, so a read that included it would
// report every re-entry as "they said something and you never answered" — which
// is the arriving message describing itself.
func (h *Head) threadOpenThing(sessionID string, messages []store.Message, now time.Time) string {
	session, found, err := h.store.Session(sessionID)
	if err != nil || !found {
		session = store.Session{ID: sessionID}
	}
	seenSeq, err := h.store.SessionSeenSeq(sessionID)
	if err != nil {
		return ""
	}
	arc, open := store.ThreadArcOf(session, messages, seenSeq)
	if !open {
		return ""
	}
	return threadOpenPhrase(arc, now)
}

// threadOpenPhrase says one arc's open thing as a sentence. It is the single
// wording for all three shapes, so the brief, the status read and the nudge say
// the same thing about the same fact.
func threadOpenPhrase(arc store.ThreadArc, now time.Time) string {
	ago := ""
	if !arc.Since.IsZero() && now.After(arc.Since) {
		ago = ", " + threadElapsed(now.Sub(arc.Since)) + " ago"
	}
	switch arc.Open {
	case store.ThreadOpenQuestion:
		return "you asked " + quoted(arc.Left) + " and they never answered" + ago
	case store.ThreadOpenUnanswered:
		return "they said " + quoted(arc.Left) + " and you never answered" + ago
	case store.ThreadOpenDelivery:
		return "work landed here — " + quoted(arc.Left) + " — and they have not been back since" + ago
	default:
		return ""
	}
}

// threadLine is one message reduced to a quotable line: first line, sanitized
// for a prompt, bounded.
func threadLine(body string) string {
	return truncateBytes(promptSafe(firstLine(strings.TrimSpace(body))), threadBriefLineBytes)
}

// threadElapsed is coarse relative time for a conversation rather than for a
// job. boardElapsed counts a three-day gap in hours because a job that has been
// running for three days is news; a thread that has been quiet for three days is
// just Tuesday's conversation, and the word for it is "3 days".
func threadElapsed(elapsed time.Duration) string {
	switch {
	case elapsed < time.Minute:
		return "under a minute"
	case elapsed < time.Hour:
		return fmt.Sprintf("%d %s", int(elapsed/time.Minute), pluralWord(int(elapsed/time.Minute), "minute", "minutes"))
	case elapsed < 24*time.Hour:
		hours := int(elapsed / time.Hour)
		return fmt.Sprintf("%d %s", hours, pluralWord(hours, "hour", "hours"))
	default:
		days := int(elapsed / (24 * time.Hour))
		return fmt.Sprintf("%d %s", days, pluralWord(days, "day", "days"))
	}
}

// openThreads is the alive glance: the conversations with something unresolved
// in them, as lines a reply can be built from. It is deliberately a RENDER over
// store.OpenThreads and not a second query — the surface's board home reads the
// same rows, and the one-renderer law is what keeps the head from calling a
// thread alive that the board has already sunk.
func (h *Head) openThreads(current string, now time.Time) string {
	if h == nil || h.store == nil {
		return "no other conversation has anything open in it."
	}
	arcs, err := h.store.OpenThreads(store.OpenThreadsDefaultLimit)
	if err != nil {
		return "the open threads could not be read: " + err.Error()
	}
	lines := make([]string, 0, len(arcs))
	for _, arc := range arcs {
		name := strings.TrimSpace(arc.Title)
		if name == "" {
			name = "an unnamed conversation"
		}
		if arc.SessionID == strings.TrimSpace(current) {
			name += " (this one)"
		}
		line := name + " — left at: " + quoted(promptSafe(arc.Left))
		if !arc.LastActive.IsZero() {
			line += " · " + threadElapsed(now.Sub(arc.LastActive)) + " ago"
		}
		if arc.UnseenDelivery {
			line += " · something landed here they have not seen"
		}
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		return "nothing is hanging in any conversation."
	}
	return strings.Join(lines, "\n")
}

// ParkedThreads is the nudge's read, exported because the party that speaks a
// presence line is the surface and the party that knows which threads are
// parked is the store. It hands back the arcs, not a sentence: what a presence
// line may say is the surface's law, and this is the fact underneath it.
//
// A thread qualifies once it has been sitting on its open thing longer than
// threadParked. Anything shorter is a working conversation between two
// exchanges, and naming that would be nagging.
func (h *Head) ParkedThreads(now time.Time) []store.ThreadArc {
	if h == nil || h.store == nil {
		return nil
	}
	arcs, err := h.store.ParkedThreads(now, threadParked, store.OpenThreadsDefaultLimit)
	if err != nil {
		return nil
	}
	return arcs
}

// ParkedThreadLine is the one sentence a presence line may say about a parked
// thread, or the empty string when there is nothing worth saying. It is here
// rather than in the surface for the reason every other wording in this package
// is: the head owns how the colleague talks, and a second component composing
// its sentences is how two voices appear.
func (h *Head) ParkedThreadLine(now time.Time) string {
	arcs := h.ParkedThreads(now)
	if len(arcs) == 0 {
		return ""
	}
	arc := arcs[0]
	name := strings.TrimSpace(arc.Title)
	if name == "" {
		name = "an older conversation"
	} else {
		name += " thread"
	}
	switch arc.Open {
	case store.ThreadOpenQuestion:
		return name + " is parked on my question"
	case store.ThreadOpenDelivery:
		return name + " has something you have not read"
	default:
		return name + " is parked on your last message"
	}
}
