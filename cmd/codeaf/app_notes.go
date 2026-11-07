package main

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
	"github.com/Agent-Field/codeaf/internal/wireauth"
)

// A message written here to a chat another computer holds goes in this
// device's outbox (syncsetup.SendNote); the computer holding the chat puts it
// in the chat's queue, so it is sent when the turn there ends, or at once when
// nothing runs there. Both halves ride the chat list's own directory read: a
// record change wakes every watching device, so no new request or timer exists
// for them.

// appNoteWords are the sentences a refused message says, in the person's words.
var appNoteWords = map[error]string{
	syncsetup.ErrNotesUnsupported: "This sync service can't carry a message to another computer yet. Bring the chat here to write to it.",
	syncsetup.ErrOutboxFull:       appNoteSpelled(directory.MaxNotes) + " messages sent from this computer, in this chat or others, haven't been delivered yet. Send this one once some of them are.",
	syncsetup.ErrNoteEmpty:        "The message is empty.",
	syncsetup.ErrNoteLong:         "Messages to another computer are limited to 2000 characters.",
	syncsetup.ErrNoOwnRecord:      "This computer isn't listed by sync yet. Try again in a moment.",
}

// appNoteUnreachable is a message this computer could not leave because its
// relay did not answer. Nothing was kept on the way, so the sentence says the
// message did not go and the draft is the person's to send again.
const appNoteUnreachable = "This computer can't reach your relay right now, so the message didn't go. Send it again once it's back."

// appNoteSpelled is the outbox limit as a sentence starts it. THE LIMIT IS
// directory.MaxNotes, never a hand-typed word, so the refusal moves with it.
func appNoteSpelled(n int) string {
	words := []string{"No", "One", "Two", "Three", "Four", "Five", "Six", "Seven", "Eight", "Nine", "Ten", "Eleven", "Twelve"}
	if n >= 0 && n < len(words) {
		return words[n]
	}
	return strconv.Itoa(n)
}

// appNotes remembers which messages this engine already put in a queue, until
// its took list says so to every reader, so a second read in between does not
// queue one twice.
type appNotes struct {
	mu     sync.Mutex
	queued map[string]bool
	// dropping is the outbox ids a drop is already on its way for.
	dropping map[string]bool
	// delivered is, by chat, this device's messages a holder took, kept while
	// this engine runs so a row can still say so after the outbox drops them.
	delivered map[string][]appwire.ChatNote
}

// maxDelivered is how many delivered messages a chat's row keeps.
const maxDelivered = 20

// deliver remembers messages a holder took, once each.
func (n *appNotes) deliver(taken []syncsetup.InNote) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.delivered == nil {
		n.delivered = map[string][]appwire.ChatNote{}
	}
	for _, t := range taken {
		kept := n.delivered[t.Cell]
		if slices.ContainsFunc(kept, func(c appwire.ChatNote) bool { return c.ID == t.ID }) {
			continue
		}
		kept = append(kept, appNote(t, appwire.ChatNoteDelivered))
		n.delivered[t.Cell] = kept[max(0, len(kept)-maxDelivered):]
	}
}

// deliveredTo is a chat's delivered messages; a chat that is here has none,
// because its transcript says what was sent.
func (n *appNotes) deliveredTo(cell string, here bool) []appwire.ChatNote {
	n.mu.Lock()
	defer n.mu.Unlock()
	if here {
		delete(n.delivered, cell)
		return nil
	}
	return slices.Clone(n.delivered[cell])
}

func (n *appNotes) claim(id string) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.queued == nil {
		n.queued = map[string]bool{}
	}
	if n.queued[id] {
		return false
	}
	n.queued[id] = true
	return true
}

func (n *appNotes) release(id string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	delete(n.queued, id)
}

func (n *appNotes) startDrop(ids []string) []string {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.dropping == nil {
		n.dropping = map[string]bool{}
	}
	var out []string
	for _, id := range ids {
		if !n.dropping[id] {
			n.dropping[id] = true
			out = append(out, id)
		}
	}
	return out
}

func (n *appNotes) endDrop(ids []string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	for _, id := range ids {
		delete(n.dropping, id)
	}
}

// SendToChat leaves text for the computer holding p.Cell. It answers once the
// relay has this device's record; the chat's row lists the message under notes
// until that computer takes it.
func (b *appBackend) SendToChat(p appwire.ChatSendParams) (appwire.ChatNote, error) {
	if !cell.ValidID(p.Cell) {
		return appwire.ChatNote{}, appwire.Errorf(appwire.CodeNotFound, "that chat is not available")
	}
	s, ok, err := syncsetup.Open(b.relayHome())
	if err != nil {
		return appwire.ChatNote{}, appEngineError(err)
	}
	if !ok {
		return appwire.ChatNote{}, appwire.Errorf(appwire.CodeEngine, appSyncOff)
	}
	ctx, cancel := context.WithTimeout(context.Background(), syncsetup.DeviceRequestWithin)
	defer cancel()
	at := time.Now().UTC()
	id, err := s.SendNote(ctx, p.Cell, p.Text)
	for refusal, words := range appNoteWords {
		if errors.Is(err, refusal) {
			return appwire.ChatNote{}, appwire.Errorf(appwire.CodeEngine, "%s", words)
		}
	}
	// A relay this computer cannot reach is not the engine failing: the
	// message was never left, the draft is still the person's, and the
	// sentence says so instead of the transport's own words.
	if errors.Is(err, directory.ErrUnreachable) || errors.Is(err, wireauth.ErrUnavailable) || errors.Is(err, context.DeadlineExceeded) {
		return appwire.ChatNote{}, appwire.Errorf(appwire.CodeUnreachable, "%s", appNoteUnreachable)
	}
	if err != nil {
		return appwire.ChatNote{}, appEngineError(err)
	}
	return appwire.ChatNote{ID: id, Text: strings.TrimSpace(p.Text), At: at, State: appwire.ChatNoteWaiting}, nil
}

// carryNotes is one chat-list read's share of the mail: the messages waiting
// for chats held here go into their queues, and this device's own messages a
// holder took leave its outbox. Both run apart from the read, which holds the
// list's lock, and each says what it did in the device's record, whose change
// is the next read.
func (b *appBackend) carryNotes(s *syncsetup.Sync, mail syncsetup.Mail, local map[string]appLocalChat) {
	var took []string
	for _, n := range mail.In {
		l, ok := local[n.Cell]
		if !ok || l.agent == "" || !b.chats.notes.claim(n.ID) {
			continue
		}
		if _, err := b.Queue(l.agent, n.Text, "note:"+n.ID); err != nil {
			b.chats.notes.release(n.ID)
			continue
		}
		took = append(took, n.ID)
	}
	taken := make([]string, 0, len(mail.Taken))
	for _, n := range mail.Taken {
		taken = append(taken, n.ID)
	}
	drop := b.chats.notes.startDrop(taken)
	if len(took) == 0 && len(drop) == 0 {
		return
	}
	guard.Go("app/chat-notes-write", func() {
		ctx, cancel := context.WithTimeout(context.Background(), syncsetup.DeviceRequestWithin)
		defer cancel()
		if len(took) > 0 {
			// A failed write is written again by the next read, which finds
			// the ids still claimed here and the messages already queued.
			// Ids stay claimed after the write too: a read begun before it
			// lands still lists them as waiting.
			_ = s.TookNotes(ctx, took)
		}
		if len(drop) > 0 {
			_ = s.DropNotes(ctx, drop)
			b.chats.notes.endDrop(drop)
		}
	})
}

// appChatNotes is a row's notes: this device's own messages to the chat, the
// ones its holder took first, then the ones still waiting, each oldest first.
// It runs before carryNotes on the same read, so a message taken in this read
// is remembered here already and never missing between the two.
func (b *appBackend) appChatNotes(mail syncsetup.Mail, cell string, here bool) []appwire.ChatNote {
	b.chats.notes.deliver(mail.Taken)
	out := b.chats.notes.deliveredTo(cell, here)
	if here {
		return nil
	}
	for _, n := range mail.Sent[cell] {
		out = append(out, appNote(n, appwire.ChatNoteWaiting))
	}
	return out
}

func appNote(n syncsetup.InNote, state string) appwire.ChatNote {
	return appwire.ChatNote{ID: n.ID, Text: n.Text, At: time.UnixMilli(n.At).UTC(), State: state}
}
