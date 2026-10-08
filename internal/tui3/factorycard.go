package tui3

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// THE FACTORY OFFER, IN THE CONVERSATION: ONE CARD, AND THE ITEM AFTERWARDS.
//
// `factory_add` is the chat's one door onto the factory floor, and it is a
// QUESTION, never a write (internal/session's tools_factory.go). This file draws
// what the question is ABOUT, in the transcript, as [StandingCardRows] draws a
// standing offer: the same corner, the same question hue, the same stem.
//
// EVERY CARD IS A QUESTION IN THE PERSON'S WORDS. The head is the question
// itself, and what it is about is drawn under it: for `factory_add`, the item
// in THE ITEM CARD'S SHAPE (factoryitemcard.go), so the row a person is asked
// about is the row they will find on the floor.
//
//	╭─ ? put this on the factory floor? ─────────────────────────────────────
//	│ ▤ paste drops the last line                            codeaf · bug · M
//	│   new · ~$1.20                     ○ plan  ○ write  ○ test  ○ proof
//	│ Pasting three lines into the box keeps two. The third is lost when the
//	│ paste ends without a newline, and the fix is in the paste path.
//	╰──────────────────────────────────────────────────────────────────────────
//
// THE ANSWERS ARE NOT DRAWN HERE. `1 add it`, `2 not now` and the words box with
// its prompt are the question, and every question this engine hands a person is
// drawn once, above the box, by the block (question.go's
// [app.questionDrawnHere]). A card that drew them as well would be one decision
// on screen twice, and a person answering the second copy is the failure the
// one-renderer wave was built to end. The block knows this card is the
// question's subject ([app.questionSubjectAt]), so it does not say the body
// again either.
//
// AFTERWARDS THE CARD FOLDS TO ITS HEAD AND ONE FOOT, and the foot is what it
// came to, in the place the task card puts `approved` or `declined`:
//
//	added · #12              the floor wrote it, and this is its number there
//	not now                  the person said no
//	changed in words         they typed a correction; nothing was written
//	expired · nothing added  the card came down unanswered
//
// AND A CARD THE FLOOR TOOK KEEPS THE ITEM BETWEEN THE TWO: its body is
// replaced by the item's LIVE card, read from the floor, which goes on saying
// what the item is doing for as long as the conversation is on screen.
//
// THE NUMBER IS THE FLOOR'S OWN. It arrives on EventFactoryAdded after the yes,
// and between the two the foot says the answer that was given (`add it`), which
// is true and not yet the whole story.

// The words a settled card keeps. They are sentences rather than states because
// the row is read once, later, by somebody reconstructing what happened.
const (
	// factoryAddedWord leads the foot of a card the floor took; the floor's own
	// `#<id>` follows it, which is how the engine's sentence names it too.
	factoryAddedWord = "added · "
	// factoryNotNowWord is the person's no, in the card's own word for it.
	factoryNotNowWord = session.FactoryNotNowLabel
	// factoryChangedWord is a correction: the person said what is wrong with the
	// card, nothing was written, and the model is proposing again.
	factoryChangedWord = "changed in words"
	// factoryExpiredWord is a card that came down unanswered — the tool call's
	// window ended or the turn was interrupted — and NOTHING WAS ADDED, which is
	// the half a person looking back actually needs.
	factoryExpiredWord = "expired · nothing added"
)

// factoryBodyRows is how many rows the model's reason may take on the card. It
// is three because the reason is what a person reads before deciding, and a
// card taller than that is pushing the conversation it came from off the
// screen; the rest is a `…`, and the whole of it is on the item page once the
// item is on the floor.
const factoryBodyRows = 3

// factoryCard is one offer, from the question to what it came to — or, when
// liveOnly, one item's live card and nothing else.
//
// It is a pointer held by the transcript entry that draws it, and the task lane
// finds it there by the proposal's id when the answer and the floor's number
// arrive ([app.factoryCardFor]): there is no second copy of it anywhere, so the
// row and what was decided cannot disagree.
type factoryCard struct {
	// notice is the card as the engine raised it. Its ID is THE PROPOSAL'S, not
	// a floor number (session's FactoryNotice says why).
	notice session.FactoryNotice
	// answer is what the person pressed or said, and verdict what it came to.
	// Both are empty for exactly as long as the question is open; a yes sets
	// answer first, and the floor's number sets verdict when it arrives.
	answer, verdict string
	// item is the floor's own id once the item is written, and zero before.
	item int
	// recipe is set when this card is `factory_recipe`'s rather than
	// `factory_add`'s (the recipe card, below): one line for a repository's
	// recipe. notice.ID is then the recipe proposal's id, so the one lookup
	// ([app.factoryCardFor]) finds both kinds of card.
	recipe *session.RecipeNotice
	// change is set when this card is `factory_item`'s: a change to one item
	// already on the floor (the item card, below), found by the same lookup on
	// its proposal's id.
	change *session.ItemNotice
	// live is the item this card shows LIVE (factoryitemcard.go): the item a
	// yes put on the floor, once its number arrives, or — for a card that
	// never asked anything (liveOnly) — the item a conversation is about.
	live *factoryItemLive
	// liveOnly says this card is only the item's live card: drawn at the top
	// of an item's own conversation, or under a reply that named the item. It
	// asks nothing and settles into nothing.
	liveOnly bool
}

// settled reports whether this offer has been answered or has come down. A
// live card is never a question, so it is settled from the start.
func (c *factoryCard) settled() bool { return c.liveOnly || c.answer != "" || c.verdict != "" }

// factoryProposal folds one EventFactoryProposal in: a new card, or the
// rebroadcast that settles one already drawn.
//
// THE ENGINE SENDS THE SAME KIND TWICE (session's askFactory): once when the
// card is raised, and once more when it is answered (Decided set) or comes down
// unanswered (Withdrawn set). A settling notice for a card this window never
// drew — it opened after the raise — draws nothing, because a foot with no card
// above it is an answer to a question nobody here saw.
//
// A CARD THAT SETTLES READS THE FLOOR AGAIN, so every live card on screen says
// what the answer came to (factoryitemcard.go).
func (a *app) factoryProposal(ev session.Event) tea.Cmd {
	notice := ev.Factory
	if notice == nil || strings.TrimSpace(notice.ID) == "" {
		return nil
	}
	card := a.factoryCardFor(notice.ID, "")
	switch {
	case notice.Decided != nil:
		if card == nil || card.settled() {
			return nil
		}
		switch {
		case strings.TrimSpace(notice.Decided.Change) != "":
			// WORDS ARE A CHANGE, NOT A YES, whatever else came with them: the
			// engine writes nothing on a change (session's FactoryAnswer).
			card.verdict = factoryChangedWord
		case notice.Decided.Approved:
			card.answer = session.FactoryAddLabel
		default:
			card.verdict = factoryNotNowWord
		}
	case strings.TrimSpace(notice.Withdrawn) != "":
		if card == nil || card.settled() {
			return nil
		}
		card.verdict = factoryExpiredWord
	default:
		if card != nil {
			// The same card raised twice is one card: the block replaces its
			// question by token, and the transcript keeps the row it has.
			return nil
		}
		card = &factoryCard{notice: *notice}
		a.closeLive()
		a.entries = append(a.entries, entry{kind: entryFactory, turn: a.turn, fac: card})
		a.follow()
		a.touch()
		return nil
	}
	a.markFactoryStale(card)
	a.touch()
	return a.factoryRead()
}

// factoryAdded folds one EventFactoryAdded in: the card that asked says the
// floor's number, its body becomes the item's live card, and the floor is read
// again.
//
// THE CARD IS FOUND BY THE PROPOSAL'S ID. The engine's Added event carries the
// whole notice the card was raised with, ID included (session's factory_add
// copies it before setting Item), so the pairing is exact. The title is the
// fallback for an engine that sent the event without the id: the newest card
// with that title that was answered yes and has no number yet.
//
// THE READ IS ASKED EVEN WHEN NO CARD MATCHES. The floor has one more row
// whichever window drew the question, and the bar's `? N` is about the floor.
func (a *app) factoryAdded(ev session.Event) tea.Cmd {
	notice := ev.Factory
	if notice == nil {
		return a.factoryRead()
	}
	if card := a.factoryCardFor(notice.ID, notice.Title); card != nil && notice.Item > 0 {
		card.item = notice.Item
		if card.answer == "" {
			card.answer = session.FactoryAddLabel
		}
		card.verdict = factoryAddedWord + "#" + itoa(notice.Item)
		// THE BODY BECOMES THE ITEM'S LIVE CARD, drawn from the floor's read
		// and, until that read lands or where there is no floor here at all,
		// from the item the news carried.
		card.live = &factoryItemLive{id: notice.Item, ref: "#" + itoa(notice.Item), repo: notice.Repo,
			title: notice.Title, kind: notice.Kind, size: notice.Size, last: notice.Now}
		a.markFactoryStale(card)
		a.touch()
	}
	return tea.Batch(a.factoryRead(), a.factoryCardPollArm())
}

// factoryCardFor is the card this proposal id names, newest first. With no id,
// it is the newest card with this title that was answered yes and has no floor
// number yet — the one an Added event without an id can only be about. A live
// card asked nothing, so it is never one a lookup finds.
func (a *app) factoryCardFor(id, title string) *factoryCard {
	id, title = strings.TrimSpace(id), strings.TrimSpace(title)
	for i := len(a.entries) - 1; i >= 0; i-- {
		e := &a.entries[i]
		if e.kind != entryFactory || e.fac == nil || e.fac.liveOnly {
			continue
		}
		if id != "" {
			if e.fac.notice.ID == id {
				return e.fac
			}
			continue
		}
		if title != "" && e.fac.item == 0 && e.fac.answer == session.FactoryAddLabel && e.fac.notice.Title == title {
			return e.fac
		}
	}
	return nil
}

// markFactoryStale drops the cached rows of the entry that draws this card.
func (a *app) markFactoryStale(card *factoryCard) {
	for i := range a.entries {
		if a.entries[i].kind == entryFactory && a.entries[i].fac == card {
			a.entries[i].stale = true
			return
		}
	}
}

// ── the card, drawn ─────────────────────────────────────────────────────────

// FactoryCardRows draws one factory card: the head, the item it is about and
// the reason while it is a question; the head and the foot that says what it
// came to once it is not, with the item's live card between them when the
// floor took it. A live-only card is the item's live card alone.
//
// IT IS PACKAGE-LEVEL ON PURPOSE, as [StandingCardRows] is: any pane in this
// package that holds a card draws it with this and a width, so a second
// factory card can never grow somewhere else.
//
// A card is never given a width under four cells: below that there is no room
// for a corner and a glyph, and half a question is worse than none.
func FactoryCardRows(a *app, card *factoryCard, width int, sel bool) []string {
	if a == nil || card == nil || width < 4 {
		return nil
	}
	if card.liveOnly {
		return a.factoryItemCardRows(card.live, width, sel)
	}
	head := a.factoryCardHead(card, width, sel)
	room := max(width-ansi.StringWidth(a.blockStem()), 1)
	if card.settled() {
		if card.live != nil {
			// THE FLOOR TOOK IT: the body is the item's live card, under the
			// question it answered and over the foot that says so.
			stem := a.pal.dim(a.blockStem())
			out := []string{head}
			for _, line := range a.factoryItemLines(a.factoryLiveItem(card.live), room, sel) {
				out = append(out, stem+line)
			}
			return append(out, a.factoryCardFoot(card, width))
		}
		return []string{head, a.factoryCardFoot(card, width)}
	}
	stem := a.pal.ask(a.blockStem())
	out := []string{head}
	if card.recipe != nil {
		return append(append(out, a.recipeCardBody(card.recipe, stem, room)...), a.factoryCardFoot(card, width))
	}
	if card.change != nil {
		return append(append(out, a.itemChangeCardBody(card.change, stem, room)...), a.factoryCardFoot(card, width))
	}
	// THE ITEM IN THE ITEM CARD'S SHAPE, as it will stand on the floor, then
	// the model's reason under it.
	for _, line := range a.factoryItemLines(factoryOfferItem(card.notice), room, false) {
		out = append(out, stem+line)
	}
	for _, line := range a.factoryCardBody(card.notice.Body, room) {
		out = append(out, stem+a.pal.ink(line))
	}
	return append(out, a.factoryCardFoot(card, width))
}

// factoryOfferItem is the offer as the row it would be on the floor: new, of
// the card's kind and size, with the model's estimate. It is drawn with the
// live card's own rows (factoryitemcard.go), so the shape asked about is the
// shape found.
func factoryOfferItem(n session.FactoryNotice) factory.Item {
	return factory.Item{
		Title:  n.Title,
		Repo:   n.Repo,
		Kind:   factoryCardKind(n.Kind),
		State:  factory.StateNew,
		Triage: factory.Triage{Type: n.Kind, Size: n.Size, Est: n.Estimate},
	}
}

// factoryCardBody is the model's reason, wrapped to the card, at most
// [factoryBodyRows] rows, the last one ending on the vocabulary's ellipsis when
// anything was left out.
func (a *app) factoryCardBody(body string, room int) []string {
	body = strings.TrimSpace(body)
	if body == "" {
		return nil
	}
	lines := wrap(strings.Join(strings.Fields(body), " "), room)
	if len(lines) <= factoryBodyRows {
		return lines
	}
	lines = lines[:factoryBodyRows]
	mark := a.icon(tokens.GEllipsis)
	last := lines[factoryBodyRows-1]
	if keep := room - ansi.StringWidth(mark) - 1; ansi.StringWidth(last) > keep {
		last = ansi.Truncate(last, max(keep, 0), "")
	}
	lines[factoryBodyRows-1] = strings.TrimRight(last, " ") + " " + mark
	return lines
}

// factoryCardKind is the floor's kind for the card's kind word, read the way
// the engine writes the item (session's factoryItem): a pull request is a PR, a
// chore is a chore, and every other word — bug, feat, question — is an issue.
func factoryCardKind(word string) factory.Kind {
	switch strings.ToLower(strings.TrimSpace(word)) {
	case "pr":
		return factory.KindPR
	case "chore":
		return factory.KindChore
	}
	return factory.KindIssue
}

// factoryCardHead is the block's top: the corner, the question glyph, the
// engine's own head, and the rule out to the frame's edge — [app.standHead]'s
// row with the engine's sentence in it.
//
// THE HEAD IS THE ENGINE'S SPELLING ([session.FactoryCardHead],
// [session.RecipeHead], [session.ItemHead]), so the card and the question
// above the box say the same words.
func (a *app) factoryCardHead(card *factoryCard, width int, sel bool) string {
	paint, rule := a.factoryCardPaint(card), a.blockRule()
	corner := taskHeadCorner
	if a.pal.ascii {
		corner = taskCornerASCII
	}
	glyph := a.icon(tokens.GNeedsHuman)
	if sel {
		glyph = a.pal.bold(glyph)
	}
	lead := corner + " " + glyph + " "
	words := session.FactoryCardHead
	if card.recipe != nil {
		words = strings.Join(strings.Fields(session.RecipeHead(*card.recipe)), " ")
	}
	if card.change != nil {
		words = strings.Join(strings.Fields(session.ItemHead(*card.change)), " ")
	}
	title := fit(words, max(width-ansi.StringWidth(lead)-2, 1))
	line := paint(lead)
	if card.settled() {
		line += a.pal.muted(title)
	} else {
		line += a.pal.askBold(title)
	}
	if fill := width - ansi.StringWidth(lead) - ansi.StringWidth(title) - 1; fill > 0 {
		line += paint(" " + strings.Repeat(rule, fill))
	}
	return line
}

// factoryCardPaint is the hue the frame takes: the question hue while it is a
// question, and the furniture grey the moment it is not.
func (a *app) factoryCardPaint(card *factoryCard) func(string) string {
	if card.settled() {
		return a.pal.dim
	}
	return a.pal.ask
}

// factoryCardFoot closes the block — and, once the question is answered, IS
// the answer, which is [app.standFoot]'s arrangement and its reason.
func (a *app) factoryCardFoot(card *factoryCard, width int) string {
	paint, rule := a.factoryCardPaint(card), a.blockRule()
	corner := taskFootCorner
	if a.pal.ascii {
		corner = taskCornerASCII
	}
	if !card.settled() {
		if fill := width - ansi.StringWidth(corner); fill > 0 {
			return paint(corner + strings.Repeat(rule, fill))
		}
		return paint(corner)
	}
	return paint(corner+" ") + a.pal.dim(fit(factoryCardWord(card), width-ansi.StringWidth(corner)-1))
}

// factoryCardWord is what a settled card's foot says: the verdict when there is
// one, and the answer that was given while the floor's number is on its way.
func factoryCardWord(card *factoryCard) string {
	if card.verdict != "" {
		return card.verdict
	}
	return card.answer
}

// factoryLabelledRows paints a card's body rows: a row's label — the words up
// to its first `: ` or its first two spaces, `now:`, `gate`, `why:` — dim,
// and the rest in ink, each row cut to the card. The rows are the engine's own
// spelling ([session.ItemRows], [session.RecipeRows]), so the card and the
// question's reason say the same words.
func (a *app) factoryLabelledRows(rows []string, stem string, room int) []string {
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		row = fit(row, room)
		cut := -1
		if i := strings.Index(row, ": "); i >= 0 {
			cut = i + 2
		} else if i := strings.Index(row, "  "); i >= 0 {
			cut = i + 2
		}
		// A LABEL IS ONE WORD. A policy sentence that happens to hold a colon
		// is a sentence, and is drawn whole in ink.
		if cut > 0 && strings.ContainsAny(strings.TrimRight(row[:cut], ": "), " ") {
			cut = -1
		}
		if cut <= 0 || cut > len(row) {
			out = append(out, stem+a.pal.ink(row))
			continue
		}
		out = append(out, stem+a.pal.dim(row[:cut])+a.pal.ink(row[cut:]))
	}
	return out
}

// ── the recipe card ─────────────────────────────────────────────────────────

// THE RECIPE OFFER, IN THE CONVERSATION: THE FACTORY CARD'S SHAPE, ONE LINE.
//
// `factory_recipe` asks to add one line to a repository's recipe, its policy or
// its habits (internal/session's tools_factory_recipe.go). Its card is drawn by
// [FactoryCardRows] with the factory card's corner, hue, head and foot, and a
// body of its own: for a stage, the kind's stages now and after with the new
// one marked `+`, then the line itself; for a policy or a habit, the sentence;
// and the reason when the model gave one.
//
//	╭─ ? add this to web's recipe for issue? ─────────────────────────────────
//	│ now: plan · write · test · review · proof
//	│ after: plan · write · test · review · proof · +security
//	│ security · chat · read it for auth holes · when touches auth
//	│ why: you said auth changes always get a second look
//	╰──────────────────────────────────────────────────────────────────────────
//
// AFTERWARDS IT FOLDS TO ITS HEAD AND ONE FOOT, in the factory card's place:
//
//	banked                    the line is in the file
//	not now                   the person said no
//	changed in words          they typed a correction; nothing was written
//	expired · nothing banked  the card came down unanswered
//
// The answers and the words box are the question's, drawn once above the box
// (question.go's [app.questionDrawnHere]), exactly as on the factory card.

// The words a settled recipe card keeps.
const (
	// recipeBankedWord is the line in the file. It arrives on EventRecipeBanked
	// after the yes; between the two the foot says the answer that was given.
	recipeBankedWord = "banked"
	// recipeNotNowWord is the person's no, in the card's own word for it.
	recipeNotNowWord = session.RecipeNotNowLabel
	// recipeExpiredWord is a card that came down unanswered, and NOTHING WAS
	// BANKED, which is the half a person looking back needs.
	recipeExpiredWord = "expired · nothing banked"
)

// recipeProposal folds one EventRecipeProposal in: a new card, or the
// rebroadcast that settles one already drawn — [app.factoryProposal]'s reading.
func (a *app) recipeProposal(ev session.Event) {
	notice := ev.Recipe
	if notice == nil || strings.TrimSpace(notice.ID) == "" {
		return
	}
	card := a.factoryCardFor(notice.ID, "")
	switch {
	case notice.Decided != nil:
		if card == nil || card.settled() {
			return
		}
		switch {
		case strings.TrimSpace(notice.Decided.Change) != "":
			// WORDS ARE A CHANGE, NOT A YES: the engine banks nothing on one.
			card.verdict = factoryChangedWord
		case notice.Decided.Approved:
			card.answer = session.RecipeBankLabel
		default:
			card.verdict = recipeNotNowWord
		}
	case strings.TrimSpace(notice.Withdrawn) != "":
		if card == nil || card.settled() {
			return
		}
		card.verdict = recipeExpiredWord
	default:
		if card != nil {
			return
		}
		held := *notice
		card = &factoryCard{notice: session.FactoryNotice{ID: notice.ID}, recipe: &held}
		a.closeLive()
		a.entries = append(a.entries, entry{kind: entryFactory, turn: a.turn, fac: card})
		a.follow()
		a.touch()
		return
	}
	a.markFactoryStale(card)
	a.touch()
}

// recipeBanked folds one EventRecipeBanked in: the card that asked says
// `banked`, and the floor is read again, because a recipe is what the floor's
// stages are drawn from and its recipe page (when it has one) reads the file.
func (a *app) recipeBanked(ev session.Event) tea.Cmd {
	if notice := ev.Recipe; notice != nil {
		if card := a.factoryCardFor(notice.ID, ""); card != nil && card.recipe != nil {
			if card.answer == "" {
				card.answer = session.RecipeBankLabel
			}
			card.verdict = recipeBankedWord
			a.markFactoryStale(card)
			a.touch()
		}
	}
	return a.factoryRead()
}

// recipeCardBody is the recipe card's body: the engine's rows
// ([session.RecipeRows]), labels dim and the rest in ink.
func (a *app) recipeCardBody(n *session.RecipeNotice, stem string, room int) []string {
	return a.factoryLabelledRows(session.RecipeRows(*n), stem, room)
}

// ── the item card ───────────────────────────────────────────────────────────

// THE CHANGE TO ONE ITEM, IN ITS OWN CONVERSATION: THE RECIPE CARD'S SHAPE.
//
// `factory_item` asks to change the one item a conversation is about — its
// stages, gate, cap or effort — or to leave its stages a note
// (internal/session's tools_factory_item.go). Its card is drawn by
// [FactoryCardRows] with the factory card's corner, hue, head and foot; the
// head is the engine's one question and the body says before and after, only
// for what changes.
//
//	╭─ ? #1 · plan first with a $8 cap? ──────────────────────────────────────
//	│ gate  ship → plan
//	│ cap  $5 → $8
//	│ why: the person wants to see the plan before any code
//	╰──────────────────────────────────────────────────────────────────────────
//
// AFTERWARDS IT FOLDS TO ITS HEAD AND ONE FOOT, in the factory card's place:
//
//	changed                    the item is changed on the floor
//	kept as it was             the person said no
//	changed in words           they typed a correction; nothing was changed
//	expired · nothing changed  the card came down unanswered
//
// The answers and the words box are the question's, drawn once above the box
// (question.go's [app.questionDrawnHere]), exactly as on the factory card.

// The words a settled item card keeps.
const (
	// itemChangedWord is the item changed on the floor. It arrives on
	// EventItemChanged after the yes; between the two the foot says the
	// answer that was given.
	itemChangedWord = "changed"
	// itemKeptWord is the person's no: the item stays as it was.
	itemKeptWord = "kept as it was"
	// itemExpiredWord is a card that came down unanswered, and NOTHING
	// CHANGED, which is the half a person looking back needs.
	itemExpiredWord = "expired · nothing changed"
)

// itemProposal folds one EventItemProposal in: a new card, or the rebroadcast
// that settles one already drawn — [app.recipeProposal]'s reading — and a card
// that settles reads the floor again, for [app.factoryProposal]'s reason.
func (a *app) itemProposal(ev session.Event) tea.Cmd {
	notice := ev.FactoryItem
	if notice == nil || strings.TrimSpace(notice.ID) == "" {
		return nil
	}
	card := a.factoryCardFor(notice.ID, "")
	switch {
	case notice.Decided != nil:
		if card == nil || card.settled() {
			return nil
		}
		switch {
		case strings.TrimSpace(notice.Decided.Change) != "":
			// WORDS ARE A CHANGE, NOT A YES: the engine changes nothing on one.
			card.verdict = factoryChangedWord
		case notice.Decided.Approved:
			card.answer = session.ItemYesLabel
		default:
			card.verdict = itemKeptWord
		}
	case strings.TrimSpace(notice.Withdrawn) != "":
		if card == nil || card.settled() {
			return nil
		}
		card.verdict = itemExpiredWord
	default:
		if card != nil {
			return nil
		}
		held := *notice
		card = &factoryCard{notice: session.FactoryNotice{ID: notice.ID}, change: &held}
		a.closeLive()
		a.entries = append(a.entries, entry{kind: entryFactory, turn: a.turn, fac: card})
		a.follow()
		a.touch()
		return nil
	}
	a.markFactoryStale(card)
	a.touch()
	return a.factoryRead()
}

// itemChanged folds one EventItemChanged in: the card that asked says
// `changed`, every live card of the item takes the item the news carried, and
// the floor is read again, so the item page standing behind this conversation
// draws the change the moment the person goes back.
func (a *app) itemChanged(ev session.Event) tea.Cmd {
	if notice := ev.FactoryItem; notice != nil {
		if card := a.factoryCardFor(notice.ID, ""); card != nil && card.change != nil {
			if card.answer == "" {
				card.answer = session.ItemYesLabel
			}
			card.verdict = itemChangedWord
			a.markFactoryStale(card)
			a.touch()
		}
		if notice.Now != nil {
			a.factoryLiveTake(*notice.Now)
		}
	}
	return a.factoryRead()
}

// itemChangeCardBody is the item card's body: the engine's rows
// ([session.ItemRows]), labels dim and the rest in ink.
func (a *app) itemChangeCardBody(n *session.ItemNotice, stem string, room int) []string {
	return a.factoryLabelledRows(session.ItemRows(*n), stem, room)
}
