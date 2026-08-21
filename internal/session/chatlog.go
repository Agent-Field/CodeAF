package session

// The chat log is the LOSSLESS FLOOR under compaction.
//
// Until this file existed, a compaction was lossy at the journal level: the
// marker's meaning is "discard everything above me", so a resume came back
// holding a summary and nothing else, and the transcript those words were
// written from could never be read again by anybody. That was tolerable only
// while the summary was the record. It is not the record any more — the new pass
// stubs and folds instead of summarizing (loop.go) — so the full text has to
// live somewhere a person and a later session can still reach.
//
// It lives in the store. Every user message, every assistant reply and every
// tool result is posted to the store's thread as it lands, under the SESSION'S
// OWN ID, which is the id the transcript header already carries and every resume
// already keeps. So the thread is minted once, by the journal, and reused
// forever after; there is no second identity to keep in step with the first.
//
// ── WHAT A POST ACTUALLY CARRIES ──
//
// A message that fits ([store.MaxMessageBytes], 16KiB) is posted whole. Anything
// larger is spilled to a file of this session's own — the same content-addressed
// droppings a stub writes into (stub.go) — and the posted body names it. The
// store's own content-addressed blobs would be the better home, and the reason
// they are not used is that [store.Store] exposes no writer for them; the spill
// is the honest fallback rather than a bad copy of one.
//
// ── AND IT IS BEST-EFFORT, ALWAYS ──
//
// No store is memory off, which is most builds, and it posts nothing at all. A
// post that fails is dropped in silence. NEVER FAIL A TURN OVER IT: the session
// file is still the record it always was, and the announce line after a
// compaction says which of the two floors this session actually has.
//
// ── WHY A GOROUTINE AND NOT A CALL ──
//
// A post is a SQLite transaction against a database the resident may also be
// writing, and one that loses the race for the write lock waits seconds for it.
// The record seams ([Agent.recordLocked], [Agent.recordUserLocked]) run under
// a.mu, which is the lock Interrupt has to be able to take, so a write there
// could park a person's stop for as long as the lock is held. Instead the seam
// appends to an unbounded queue — one append, no blocking, no dropping — and one
// writer goroutine drains it in order.
//
// ── AND WHY THE DRAIN AT CLOSE HAS A DEADLINE ──
//
// [chatJournal.close] is on the quit path, and until it had a clock it was the
// place a quit could hang: it waited for the queue with no bound at all, behind
// a store write that itself waited ten uncancellable seconds for a lock another
// process was holding. Quitting is not negotiable — a person who has pressed
// ctrl+c twice is owed their terminal back — so the drain gets [closeSettle],
// and what has not landed by then is said out loud in the log rather than
// waited for or pretended about. The session file still holds every word.

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// closeSettle is how long a quit waits for the store's copy of the conversation
// to finish landing. It is [closeGrace], the same two seconds Close already
// gives a cancelled turn and a background job, because it is owed to the same
// person for the same reason: this is the last thing between them and their
// prompt. Two seconds is enough for a queue drained against an uncontended
// database — those writes are milliseconds — and nowhere near long enough to
// sit through a wedged one.
const closeSettle = closeGrace

// chatRefPrefix opens every pointer at a store message. It is a scheme rather
// than a bare number so a stub line's reader — a person, or the model deciding
// whether the bytes are worth fetching — can tell a store id from a path at a
// glance.
const chatRefPrefix = "store:"

// chatJournal posts this session's transcript into the store's thread and
// remembers where each message landed.
type chatJournal struct {
	store     *store.Store
	thread    string
	workspace string
	place     Place

	mu      sync.Mutex
	cond    *sync.Cond
	pending []ai.Message
	writing bool
	closed  bool
	// idle carries one nudge each time the writer finishes a batch, for the one
	// waiter that has a deadline and therefore cannot use the condition — a
	// [sync.Cond] wait is a promise to wait forever, and the quit path cannot
	// make that promise. It is buffered and never blocks: a nudge nobody is
	// waiting for is dropped, and a waiter re-reads the queue rather than
	// trusting a signal.
	idle chan struct{}
	// refs is where each posted message can be read back from, under a
	// fingerprint of the message itself ([chatRefKey]).
	//
	// A fingerprint rather than an index, for [sessionFile.images]'s reason: the
	// thing that asks is a compaction pass holding a message, long after the
	// slice position it once had stopped meaning anything.
	refs map[string]string
}

// newChatJournal opens the floor, or answers nil when there is no store to
// write into — the absent-rather-than-broken law, so every caller below is one
// nil check and nothing else.
func newChatJournal(brain *store.Store, thread, workspace string, place Place) *chatJournal {
	if brain == nil || strings.TrimSpace(thread) == "" {
		return nil
	}
	journal := &chatJournal{
		store:     brain,
		thread:    strings.TrimSpace(thread),
		workspace: strings.TrimSpace(workspace),
		place:     place,
		refs:      make(map[string]string, 64),
		idle:      make(chan struct{}, 1),
	}
	journal.cond = sync.NewCond(&journal.mu)
	go journal.run()
	return journal
}

// post queues one message. It is called from under a.mu and does exactly one
// append and one signal.
func (j *chatJournal) post(message ai.Message) {
	if j == nil {
		return
	}
	j.mu.Lock()
	if !j.closed {
		j.pending = append(j.pending, message)
		j.cond.Signal()
	}
	j.mu.Unlock()
}

// ref answers where one message's full text can be read back, empty when it has
// not landed yet or never will.
func (j *chatJournal) ref(message ai.Message) string {
	if j == nil {
		return ""
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.refs[chatRefKey(message)]
}

// settle waits for the queue to drain. Close calls it so a session's last words
// reach the store before the process ends, and the tests call it because a
// losslessness claim tested against a queue that had not run yet is a claim
// about nothing.
func (j *chatJournal) settle() {
	if j == nil {
		return
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	for len(j.pending) > 0 || j.writing {
		j.cond.Wait()
	}
}

// settleWithin is settle with a clock on it. It answers whether the queue
// actually drained, so a caller that gave up can say so.
func (j *chatJournal) settleWithin(limit time.Duration) bool {
	if j == nil {
		return true
	}
	deadline := time.Now().Add(limit)
	for {
		j.mu.Lock()
		busy := len(j.pending) > 0 || j.writing
		j.mu.Unlock()
		if !busy {
			return true
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return false
		}
		timer := time.NewTimer(remaining)
		select {
		case <-j.idle:
		case <-timer.C:
		}
		timer.Stop()
	}
}

// close drains what is queued, within [closeSettle], and stops the writer.
//
// What did not land is SAID rather than swallowed. It goes to the log and not to
// the screen deliberately: the chat log has always been best-effort — a post
// that fails is dropped in silence and never fails a turn — so a quit, a /new
// and a /resume must not start reporting a second copy of the transcript as a
// failure when the first copy is complete and on disk.
func (j *chatJournal) close() {
	if j == nil {
		return
	}
	if !j.settleWithin(closeSettle) {
		log.Printf("session: the store's copy of the conversation was still being written after %s; "+
			"closing without it — the session file has the whole transcript", closeSettle)
	}
	j.mu.Lock()
	j.closed = true
	j.cond.Broadcast()
	j.mu.Unlock()
}

func (j *chatJournal) run() {
	for {
		j.mu.Lock()
		for len(j.pending) == 0 && !j.closed {
			j.cond.Wait()
		}
		if len(j.pending) == 0 && j.closed {
			j.mu.Unlock()
			return
		}
		batch := j.pending
		j.pending = nil
		j.writing = true
		j.mu.Unlock()

		written := make(map[string]string, len(batch))
		for _, message := range batch {
			if ref := j.write(message); ref != "" {
				written[chatRefKey(message)] = ref
			}
		}

		j.mu.Lock()
		for key, ref := range written {
			j.refs[key] = ref
		}
		j.writing = false
		j.cond.Broadcast()
		select {
		case j.idle <- struct{}{}:
		default:
		}
		j.mu.Unlock()
	}
}

// write posts one message and answers the pointer it landed under.
func (j *chatJournal) write(message ai.Message) string {
	role, ok := chatRole(message.Role)
	if !ok {
		return ""
	}
	body, attachment := j.body(message)
	if body == "" {
		return ""
	}
	posted := store.Message{SessionID: j.thread, Role: role, Body: body}
	if attachment != "" {
		posted.Attachments = []string{attachment}
	}
	landed, err := j.store.PostMessage(posted)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%s%d", chatRefPrefix, landed.Seq)
}

// body is the text to post and the spill file it may have to name.
//
// The whole message when it fits, and a pointer when it does not. The pointer
// case is not a truncation: the bytes are written first, and a spill that failed
// posts a bounded prefix rather than a path to nothing — the one thing this must
// never do is claim a record exists somewhere it does not.
func (j *chatJournal) body(message ai.Message) (string, string) {
	text := messageContentText(message)
	if strings.TrimSpace(text) == "" {
		// A message with no words is an assistant turn that was pure tool calls.
		// The calls themselves are what it said, so they are what is posted.
		text = strings.TrimSpace(chatToolCallLines(message))
		if text == "" {
			return "", ""
		}
	}
	if len(text) <= store.MaxMessageBytes {
		return text, ""
	}
	path, err := writeStub(j.place, j.workspace, text)
	if err != nil {
		return chatClipped(text), ""
	}
	return fmt.Sprintf("[full text · %d bytes · %s]\n\n%s", len(text), path, chatClipped(text)), path
}

// chatClipped is the head of an over-long message, cut well inside the store's
// limit so the prefix plus the pointer sentence above it still fits.
func chatClipped(text string) string {
	const room = store.MaxMessageBytes - 1024
	if len(text) <= room {
		return text
	}
	return text[:room] + "\n[…]"
}

// chatToolCallLines renders an assistant message's calls the way the transcript
// flattener always did: one line each, name and arguments.
func chatToolCallLines(message ai.Message) string {
	var out strings.Builder
	for _, call := range message.ToolCalls {
		fmt.Fprintf(&out, "[tool call: %s(%s)]\n", call.Function.Name, call.Function.Arguments)
	}
	return out.String()
}

// chatRole maps a transcript role onto the store's three. A tool result is
// SYSTEM: the store's roles say who authored a line, and nobody authored a file
// read — the machine did.
func chatRole(role string) (store.Role, bool) {
	switch role {
	case "user":
		return store.RoleUser, true
	case "assistant":
		return store.RoleAgent, true
	case "tool":
		return store.RoleSystem, true
	}
	return "", false
}

// chatRefKey fingerprints one message so a pointer can be found again from the
// message itself. Role, tool-call id and text: the three things that identify a
// line of a transcript, and none of them changes between the post and the
// compaction that looks the pointer up.
func chatRefKey(message ai.Message) string {
	sum := sha256.Sum256([]byte(message.Role + "\x00" + message.ToolCallID + "\x00" +
		messageContentText(message) + "\x00" + chatToolCallLines(message)))
	return hex.EncodeToString(sum[:12])
}
