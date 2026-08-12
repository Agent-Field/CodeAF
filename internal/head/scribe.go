package head

import (
	"context"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/guard"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// The scribe that names rooms (8.2.13, filed as 13.3 bug 4).
//
// "untitled room" was always the honest placeholder — the rail draws a room's
// title and never its id (13.3.4), so a room nobody has named has nothing else
// to be called. What made it a P0 was that NOTHING ever named one: the naming
// clerk was designed as a head-side, post-turn, tiny-model call and never wired,
// so every room in the rail wore the placeholder and the reader could not tell
// one from another.
//
// Four constraints shape everything below, and each of them is a way this could
// have gone wrong:
//
//   - It never blocks the reply. The person's answer is posted, the turn ends,
//     and only then does the poll hand the room to a goroutine. A title that took
//     a second to think of would otherwise be a second of silence in front of
//     every first answer.
//   - It fires once per room, and the room's own title is the record of that.
//     There is no "named" flag to keep in step with the thing it describes: a
//     titled room is a named room, and the check is the same read the rail does.
//   - It costs pennies. The scribe role (5.23's ladder) resolves the model — a
//     labeller, not a judge — and the call carries two clipped messages and a
//     ceiling of a few dozen tokens, because the whole answer is four words.
//   - The title goes through the same normalizer every other title in this
//     package goes through, so a model that answers `"Billing audit."` and one
//     that answers `Billing audit` land on the same row.
const (
	// scribeMaxTokens is the ceiling on the naming call. A 2-5 word title is
	// under ten tokens; the rest is room for a model that starts with a
	// preamble, which normalizeTitle's first-line rule then throws away.
	scribeMaxTokens = 32
	// scribeExcerptBytes is how much of each side of the first exchange the
	// scribe is shown. A room is named for what it is ABOUT, and what it is
	// about is in the opening sentences — paying for the whole of a long
	// deliverable to produce four words is the one way a clerk becomes
	// expensive.
	scribeExcerptBytes = 480
	// scribeTitleWords bounds the name. Five words is what a 28-column rail can
	// hold beside a glyph and a money cell; a model that writes a sentence gets
	// its first five words rather than a refusal, because a slightly clipped
	// name still tells a reader which room this is and "untitled room" does not.
	scribeTitleWords = 5
	// scribePatience bounds the naming call itself. Nothing waits on it, so a
	// provider that hangs must cost a goroutine and no more.
	scribePatience = 30 * time.Second
)

// scribePrompt is the whole of the clerk's brief. It says what a title is FOR —
// telling one room from another in a list — because that is the difference
// between a name and a summary, and a clerk given no purpose writes summaries.
const scribePrompt = `You name conversations.

Given the first exchange of a conversation, answer with a title of 2 to 5 words
naming what it is about, so a reader can tell this conversation from a dozen
others in a list.

Rules:
- 2 to 5 words. No sentence, no punctuation at the end, no quotes.
- Name the subject, not the act: "billing audit", not "user asks for help".
- No preamble and no explanation. The title alone is the whole answer.`

// nameRoomLater is the poll's post-turn hook: the room this turn happened in
// may now have a name, and finding out is nobody's critical path.
//
// It is fire-and-forget on purpose. The turn is over, the reply is journaled,
// and the only thing this can still affect is a row in the rail — so a naming
// call that fails, times out, or is cut off by the process shutting down leaves
// exactly what was there before, which is a room called "untitled room" and a
// scribe that will try again after the next turn.
func (h *Head) nameRoomLater(ctx context.Context, sessionID string) {
	if h == nil || h.store == nil || !h.namesRooms || strings.TrimSpace(sessionID) == "" {
		return
	}
	// The cheap half of the test runs here, on the poll's own goroutine: a room
	// that already has a name never even costs a goroutine.
	if named, err := h.roomNamed(sessionID); err != nil || named {
		return
	}
	guard.Go("head/name-room", func() {
		// The context is the head's own — the poll's, not the turn's — so this
		// outlives the answer it followed and dies when the head does, which is
		// the honest lifetime for work the head is doing on its own behalf.
		//
		// One thing comes off it: the surface's stream observer. A naming call
		// made inside a serving context would type its four words into the
		// transcript as though somebody were saying them, and a label is not a
		// reply.
		called, cancel := context.WithTimeout(provider.WithoutStream(ctx), scribePatience)
		defer cancel()
		h.nameRoom(called, sessionID)
	})
}

// roomBackfillCap is how many rooms one launch may name. It is a money bound
// before it is anything else: each row is a provider call the person did not
// ask for, and a store with two hundred unnamed rooms in it must not turn one
// launch into two hundred calls. Whatever is left keeps its turn for the next
// launch, which is why this is a cap and not a page cursor — the naming is
// idempotent, so "the ones still unnamed, newest first" is the correct query
// every single time it runs.
const roomBackfillCap = 8

// BackfillRoomNames names the rooms that existed before the scribe did.
//
// The post-turn clerk only ever fires at the end of a turn, so every room whose
// last turn was already over when it landed stays "untitled room" forever,
// however much conversation is in it. That is the reported bug, and it is not a
// naming failure — it is a room that has never had a moment when naming was
// anyone's job. This is that moment, once per launch.
//
// It is the same pass, not a second one: same claim, same read of what the room
// is about, same model rung, same normalizer, same one-per-room guard. A room
// somebody named by hand is never touched, because a title IS the record that
// this has run. And it happens off the launch entirely — the caller gets a
// goroutine and its own turn back — so a window opens at the speed it always
// did whether the naming works, fails, or never finishes.
//
// The rooms are named one after another rather than all at once. Eight
// concurrent provider calls at the exact moment a window is opening is the kind
// of thundering start that makes a launch feel slow through no fault of the
// thing the person is waiting for.
func (h *Head) BackfillRoomNames(ctx context.Context) {
	if h == nil || h.store == nil || !h.namesRooms {
		return
	}
	guard.Go("head/backfill-room-names", func() {
		rooms, err := h.store.UnnamedSessionsWithExchange(roomBackfillCap)
		if err != nil || len(rooms) == 0 {
			return
		}
		for _, room := range rooms {
			if ctx.Err() != nil {
				return
			}
			// One timeout per room rather than one for the batch: a provider that
			// hangs on the first room must not eat the other seven's chance.
			called, cancel := context.WithTimeout(provider.WithoutStream(ctx), scribePatience)
			h.nameRoom(called, room.ID)
			cancel()
		}
	})
}

// nameRoom is the naming pass itself, synchronous, and the only place a room
// gets its title from a model.
//
// Every reason not to name a room is a quiet return rather than an error: this
// is a nicety over a working conversation, and the one thing it may never do is
// make a turn fail. The caller has nothing to do with what it learns.
func (h *Head) nameRoom(ctx context.Context, sessionID string) {
	sessionID = strings.TrimSpace(sessionID)
	if h == nil || h.store == nil || sessionID == "" {
		return
	}
	if !h.claimNaming(sessionID) {
		return
	}
	defer h.releaseNaming(sessionID)

	// Read again inside the claim: two turns can settle in one poll, and the
	// room may have been named between the cheap check and this goroutine
	// starting.
	if named, err := h.roomNamed(sessionID); err != nil || named {
		return
	}
	ask, answer, ok := h.firstExchange(sessionID)
	if !ok {
		// A room with words in only one direction is not a conversation yet.
		// Naming it from the ask alone would name the question rather than the
		// room, and the next turn is a better moment than a worse name.
		return
	}

	client := h.scribeClient()
	if client == nil {
		return
	}
	response, err := client.CompleteWithMessages(ctx, []ai.Message{
		textMessage("system", scribePrompt),
		textMessage("user", "Them:\n"+ask+"\n\nYou:\n"+answer),
	}, ai.WithMaxTokens(scribeMaxTokens))
	if err != nil || response == nil {
		return
	}
	title := roomTitle(response.Text())
	if title == "" {
		return
	}
	_, _ = h.store.RenameSession(sessionID, title)
}

// roomTitle is the sanitize chokepoint for a room's name: normalizeTitle, which
// every title in this package already passes through, plus the one bound that is
// this surface's own — a rail row is a name and not a sentence, so a model that
// answered with one keeps its first few words.
func roomTitle(raw string) string {
	title := normalizeTitle(raw)
	// A model that leads with a label has said the label, not the name.
	for _, prefix := range []string{"title:", "name:"} {
		if len(title) >= len(prefix) && strings.EqualFold(title[:len(prefix)], prefix) {
			title = strings.TrimSpace(title[len(prefix):])
		}
	}
	title = normalizeTitle(title)
	fields := strings.Fields(title)
	if len(fields) == 0 {
		return ""
	}
	if len(fields) > scribeTitleWords {
		fields = fields[:scribeTitleWords]
	}
	title = normalizeTitle(strings.Join(fields, " "))
	if len(title) > store.MaxSessionTitleBytes {
		title = strings.TrimSpace(title[:store.MaxSessionTitleBytes])
	}
	return title
}

// roomNamed reports that this room already has a name — the durable record that
// the scribe has run, kept as the thing it produced rather than as a flag beside
// it. A room the store has never heard of is not named and not nameable yet;
// both answers are "leave it alone".
func (h *Head) roomNamed(sessionID string) (bool, error) {
	session, found, err := h.store.Session(sessionID)
	if err != nil {
		return false, err
	}
	if !found {
		return true, nil
	}
	return strings.TrimSpace(session.Title) != "", nil
}

// firstExchange is the room's opening: the person's first words and the first
// answer that followed them. Both clipped, because the clerk is being asked what
// this room is about and the opening sentences say it.
//
// System messages are not part of it. A resident notice ("taking over as
// resident"), a brief, a delivery posted by a job — none of them is somebody
// talking to somebody, and a room named from one would be named after the
// machine rather than after the conversation.
func (h *Head) firstExchange(sessionID string) (string, string, bool) {
	messages, err := h.store.Messages(sessionID, 0, 24)
	if err != nil {
		return "", "", false
	}
	ask, answer := "", ""
	for _, message := range messages {
		body := strings.TrimSpace(message.Body)
		if body == "" {
			continue
		}
		switch message.Role {
		case store.RoleUser:
			if ask == "" {
				ask = truncateBytes(body, scribeExcerptBytes)
			}
		case store.RoleAgent:
			if ask != "" && answer == "" {
				answer = truncateBytes(body, scribeExcerptBytes)
			}
		}
		if ask != "" && answer != "" {
			return ask, answer, true
		}
	}
	return "", "", false
}

// scribeClient resolves the clerk's model through the role ladder and hands back
// a client pinned to it.
//
// RoleScribe is the labeller's rung (5.23): titles, folds, one-liners. With
// nothing bound it resolves to the work model, which is the honest floor the
// ladder states — steering nowhere must not cost more than not steering — and
// with no model client selector in this window at all it is the head's own talk
// client, which is what every other call here uses.
func (h *Head) scribeClient() Client {
	model := ""
	if resolved, err := h.store.ResolveRole(store.RoleScribe, ""); err == nil {
		model = strings.TrimSpace(resolved.Model)
	}
	client, err := h.clientFor(store.Message{Model: model})
	if err != nil || client == nil {
		// A pin that cannot be built is not a reason to skip the name: the head's
		// own client can write four words.
		return h.client
	}
	return client
}

// claimNaming keeps two turns settling at once from buying the same title twice.
// It is in-process state guarding an in-process race; the durable answer to "has
// this room been named" is the title itself.
func (h *Head) claimNaming(sessionID string) bool {
	h.scribeMu.Lock()
	defer h.scribeMu.Unlock()
	if h.scribing == nil {
		h.scribing = make(map[string]bool)
	}
	if h.scribing[sessionID] {
		return false
	}
	h.scribing[sessionID] = true
	return true
}

func (h *Head) releaseNaming(sessionID string) {
	h.scribeMu.Lock()
	defer h.scribeMu.Unlock()
	delete(h.scribing, sessionID)
}
