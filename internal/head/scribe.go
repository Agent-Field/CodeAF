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
	// scribeMaxTokens is the ceiling on the naming call.
	//
	// IT WAS 32, AND 32 IS WHY NOTHING WAS EVER NAMED. The old number was
	// arithmetic on the ANSWER — "a 2-5 word title is under ten tokens, leave
	// room for a preamble" — and it was correct arithmetic about a world that
	// stopped existing. On a model that reasons before it speaks, the thinking
	// is spent out of this same budget BEFORE the first character of the title
	// is written: measured live against the owner's own endpoint, the naming
	// call came back `finish_reason:"length"` with `content:null` every single
	// time. roomTitle("") is empty, nameRoom returned in silence, and the room
	// was handed back to the scribe after the next turn to fail identically
	// forever. Every room in the rail wore the placeholder.
	//
	// A max_tokens cap is a CAP AND NOT A PURCHASE — a call that stops early is
	// billed for what it wrote — so the number that matters is what the reply
	// can legitimately need, not what four words usually cost. The same
	// correction was made for the same reason on the standing compiler
	// (standing.go) and the ordinary one beside it. Several hundred is the
	// honest floor for a labeller in a reasoning world, and it is the register
	// the small calls around here already sit in (aside.go's 600).
	scribeMaxTokens = 512
	// scribeReliefTokens is the one escalation. A reply that came back with no
	// visible text and a length-shaped finish is a model that ran out of room
	// mid-thought, and that is the ONE failure a bigger ceiling can actually
	// fix — so it gets exactly one more try with room to spare.
	//
	// It is a mechanism rather than a bigger magic number. The next model whose
	// reasoning appetite exceeds the default corrects itself here on the second
	// call instead of waiting for somebody to notice every room is untitled
	// again and edit a constant. It is not a loop: once, and then silence.
	scribeReliefTokens = 8192
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
//
// It asks for TWO things in one call, and they are one job rather than two: a
// title is what this conversation is called and the tags are what it is about,
// and both come out of the same single reading of the opening exchange. A
// second call for the second line would double the bill for one act of
// judgment. The tags exist for the switcher's filter — a person who remembers
// the subject of a conversation but not the name it ended up with — which is
// why they are asked for as SUBJECTS and not as a summary in list form.
const scribePrompt = `You name conversations.

Given the first exchange of a conversation, answer with exactly two lines.

Line 1 — the title: 2 to 5 words naming what the conversation is about, so a
reader can tell it from a dozen others in a list.
- 2 to 5 words. No sentence, no punctuation at the end, no quotes.
- Name the subject, not the act: "billing audit", not "user asks for help".

Line 2 — the tags: up to 3 topic words, lowercase, separated by commas.
- One or two words each. They are for searching, so use the words a person
  would type months later when looking for this conversation again.
- Name subjects, not sentiments and not the shape of the request.
- If nothing beyond the title is worth filing it under, leave the line empty.

No preamble, no explanation, no labels. The two lines are the whole answer.`

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
	messages := []ai.Message{
		textMessage("system", scribePrompt),
		textMessage("user", "Them:\n"+ask+"\n\nYou:\n"+answer),
	}
	response, err := client.CompleteWithMessages(ctx, messages, ai.WithMaxTokens(scribeMaxTokens))
	if err != nil || response == nil {
		return
	}
	if spentOnThinking(response) {
		// The whole ceiling went on reasoning and nothing was said. One more
		// try with room to spare, and then silence — see [scribeReliefTokens].
		response, err = client.CompleteWithMessages(ctx, messages, ai.WithMaxTokens(scribeReliefTokens))
		if err != nil || response == nil {
			return
		}
	}
	title, tags := roomLabel(response.Text())
	if title == "" {
		return
	}
	_, _ = h.store.RenameSessionTagged(sessionID, title, tags)
}

// spentOnThinking reports the one failure shape a bigger ceiling can fix: a
// reply that said nothing visible and stopped because it ran out of room.
//
// The finish reason is the honest signal and the provider layer surfaces it —
// `ai.Choice.FinishReason`, which internal/plan already reads for the same
// question about truncated JSON. Both spellings are accepted because both are
// in the wild ("length" from the OpenAI shape, "max_tokens" from the Anthropic
// one) and neither is a model name.
//
// A finish reason that is ABSENT counts, because a provider that reports none
// has told us nothing and empty-text-on-a-successful-call is then the only
// signal there is. A finish reason that is present and says something else —
// the model stopped of its own accord, a filter cut it off — does NOT count: a
// model that chose to say nothing will choose it again with eight thousand
// tokens, and paying twice to hear the same silence is a loop with a bill.
func spentOnThinking(response *ai.Response) bool {
	if response == nil || strings.TrimSpace(response.Text()) != "" {
		return false
	}
	if len(response.Choices) == 0 {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(response.Choices[0].FinishReason)) {
	case "", "length", "max_tokens":
		return true
	}
	return false
}

// roomLabel splits the clerk's two-line answer into the name and the subjects.
//
// IT IS DEFENSIVE IN ONE DIRECTION ONLY: a tags line that is missing, empty,
// mangled or full of prose costs the room its tags and never its title. The
// title is what a rail row is; tags are a finding aid over it, and a parser
// that let the second line take down the first would have reintroduced the
// exact bug this file exists to close — a room left untitled because something
// downstream of the name went wrong.
func roomLabel(raw string) (string, []string) {
	head, rest, _ := strings.Cut(strings.TrimSpace(raw), "\n")
	title := roomTitle(head)
	if title == "" {
		// The first line was a preamble rather than the name. roomTitle's own
		// first-line rule has already been spent on it, so the whole answer goes
		// through again — which is what an unsplit answer used to get, and the
		// tags are forfeit rather than guessed at from a line whose position no
		// longer means anything.
		return roomTitle(raw), nil
	}
	return title, roomTags(rest)
}

// roomTags reads the subjects off the clerk's second line.
//
// A model asked for "up to 3 lowercase topic words, comma separated" answers
// with those, and sometimes with a label in front of them, a bulleted list, or
// a sentence. All of it goes through one splitter and then the store's own
// normalizer, and anything that survives as a word or two is a tag: this is a
// filter's index, so a slightly odd tag costs a person nothing and a refused
// one costs them the search.
//
// A LINE THAT IS A SENTENCE IS NOT TAGS. The count bound is the whole test —
// a "tag" of several words is prose, and prose in a subsequence filter matches
// everything — so it is dropped rather than clipped.
func roomTags(raw string) []string {
	line, _, _ := strings.Cut(strings.TrimSpace(raw), "\n")
	line = strings.TrimSpace(line)
	// A label in front of the list is the label, not a tag — the same
	// correction roomTitle makes for the same reason.
	for _, prefix := range []string{"tags:", "topics:"} {
		if len(line) >= len(prefix) && strings.EqualFold(line[:len(prefix)], prefix) {
			line = strings.TrimSpace(line[len(prefix):])
		}
	}
	if line == "" {
		return nil
	}
	tags := make([]string, 0, store.MaxSessionTags)
	for _, field := range strings.Split(line, ",") {
		tag := strings.TrimSpace(normalizeTitle(strings.Trim(field, " \t-*#")))
		if tag == "" || len(strings.Fields(tag)) > scribeTagWords {
			continue
		}
		tags = append(tags, tag)
	}
	// The store lower-cases, de-duplicates, bounds and caps them, so a replay
	// lands exactly what this write landed.
	return tags
}

// scribeTagWords is how long a tag may be before it is prose. Two: a subject is
// a word or a compound of two, and a filter's index is worth nothing once its
// entries are phrases.
const scribeTagWords = 2

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
