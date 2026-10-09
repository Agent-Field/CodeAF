package session

// THE CHAT'S DOOR ONTO ONE ITEM ON THE FLOOR — BEHIND A CARD.
//
// An item's own conversation (the floor's `T`, cmd/codeaf's talk maker) is the
// item's hub: where a person thinks out loud about it — "skip review on this
// one", "hold for me after plan, budget $8", "tell the stages the fixture is flaky" —
// and `factory_item` is the one verb that carries what they settle on to the
// item. It replaces `factory_stages`, which could change the stages and
// nothing else, and it carries every change as a QUESTION, never as a write:
// `factory_recipe`'s shape (tools_factory_recipe.go) with a different payload
// and a different door.
//
// ── THE LAWS THIS FILE APPLIES ──
//
//   - EVERY CARD IS A QUESTION IN THE PERSON'S WORDS, WITH BEFORE AND AFTER AND
//     THE WHY. The head is one question built from what changes (`#1 · skip the
//     review stage?`, `#1 · add an approve step?`); the body says the
//     item's stages now and after, each chip as `before → after`, the note and
//     the reason, and nothing about a field the change leaves alone.
//
//   - NOTHING CHANGES UNTIL THE PERSON SAYS YES. The tool raises a card; only
//     `yes` reaches the door's Apply. There is no argument and no phrasing that
//     changes the item without that answer.
//
//   - THE BOUNDS ARE HELD IN CODE. The stages go through [factory.Adapt] inside
//     [ApplyItemChange], which refuses a skipped proof, a stage a
//     policy names, a fixed stage, anything before a stage that already ran, and every stage
//     change at all under a `fixed` recipe. The door previews the change BEFORE
//     the card is raised, so a refused change is never offered, and a refusal
//     comes back in Adapt's own sentence. A CHANGE IS WHOLE OR NOTHING: a card
//     whose stages are refused changes no chip either.
//
//   - NOTHING LAUNCHES. There is no field for a launch, a stop or an approval;
//     those are the floor's keys, in front of a person.
//
//   - THERE IS NO CLOCK THAT CHANGES. The window bounds the tool call, as
//     [factoryCardWindow] does, and its ending is nothing changed.
//
//   - WORDS ARE A CHANGE, NOT A YES, as on the factory card.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/exec/bare"
	"github.com/Agent-Field/codeaf/internal/factory"
)

// The card's own words and the sentences the model reads back. The manual
// quotes them (internal/manual/chat/factory.md) and a surface drawing the card
// spells the same head.
const (
	// ItemYesLabel and ItemKeepLabel are the card's two answers.
	ItemYesLabel  = "yes"
	ItemKeepLabel = "keep it"
	// ItemChangePrompt is the line over the card's text box.
	ItemChangePrompt = FactoryChangePrompt

	itemChangedLead = "the person changed it: "
	itemChangedTail = "\nNothing changed. Propose it again with that"
	itemDeclined    = "nothing changed: the person said no."
	itemUnanswered  = "nothing changed: the card was never answered"
	// itemRefusedLead opens what a door that would not change the item returns;
	// the door's own sentence follows it.
	itemRefusedLead = "nothing changed: "
	// itemAlready is what a change that would leave the item exactly as it is
	// returns, with no card raised: there is nothing for a person to decide.
	itemAlready = "nothing changed: the item already runs that way"
	// itemNowLead sits between the item's name and its facts after a yes:
	// `#1 now: plan · approve · write · test · proof · budget $8`.
	itemNowLead = " now: "
	// itemDone is the line under the facts a yes returns.
	itemDone = "The person said yes and the floor has the change; there is nothing left to answer."
)

// The card's two keys, the factory card's: `1` is the yes and `2` the answer
// that changes nothing.
const (
	ItemYesKey  = "1"
	ItemKeepKey = "2"
)

// itemNone is how the card spells a chip that has no value, before or after:
// `thinking — → strong`. It is a dash rather than nothing because the row is a
// comparison, and a comparison with one side missing reads as a broken row.
const itemNone = "—"

// itemAddedMark marks a stage on the `after:` row that the item did not run
// before: `plan · write · +security · test · proof`.
const itemAddedMark = "+"

// itemEfforts is the schema's enum, held once so the schema and the
// handler's check read the same list. `default` is the thinking word for the
// knee, which the floor stores as nothing.
var itemEfforts = []string{"cheap", "strong", "default"}

const factoryItemDescription = "Offer to change the ONE factory item this conversation is about, when the person settles on it: its stages (\"skip review on this one\", \"add a security pass\"; where the run holds for the person is an approve step: add \"after plan, approve\" or skip \"approve\", and an item with no approve step ships itself when its proof is green), its budget in dollars (the field is cap; the person calls it \"budget\"), how hard the stages think (the field is effort; the person calls it \"thinking\": cheap, strong, default), or a note the item's stages will read (\"the fixture in testdata is flaky\"). " +
	"This conversation is the item's hub: change it with this tool, leave notes here, and once the item runs, its stages report into this conversation. " +
	"Use it only for the item this conversation was opened for; never for another item. The floor id goes in the item field and nowhere else: in words, name the item by its ref (#12), never by its floor id. " +
	"add is new stage sentences, a place word first when it matters (\"after test, read it for auth holes\"); skip and on are stage names the item already has. Give only what changes. " +
	"NOTHING CHANGES BY CALLING THIS: the person is shown a card with the item before and after, and only their `yes` changes anything. Nothing launches either; that is the person's, on the floor. " +
	"The recipe's bounds hold whatever is asked: proof and a stage the policy names are never skipped, a stage that already ran is never touched, and a `fixed` recipe refuses every stage change; a refusal comes back with its reason before any card. " +
	"If they type a change instead, nothing changes and you are told their words: propose again with them."

func factoryItemSchemaJSON() string {
	quoted := func(list []string) string {
		out := make([]string, len(list))
		for i, item := range list {
			out[i] = strconv.Quote(item)
		}
		return strings.Join(out, ",")
	}
	return `{"type":"object","properties":{` +
		`"item":{"type":"integer","description":"The floor id the conversation was told for this item (a store number, not the ref the person says)."},` +
		`"add":{"type":"array","items":{"type":"string"},"description":"Stage sentences to add, e.g. \"after review, read it for auth holes\"."},` +
		`"skip":{"type":"array","items":{"type":"string"},"description":"Stage names to switch off for this item."},` +
		`"on":{"type":"array","items":{"type":"string"},"description":"Stage names to switch on for this item."},` +
		`"cap":{"type":"number","description":"The item's budget in dollars: the most it may spend; the person calls this \"budget\"."},` +
		`"effort":{"type":"string","enum":[` + quoted(itemEfforts) + `],"description":"How hard the stages that have not run yet think; the person calls this \"thinking\"."},` +
		`"note":{"type":"string","description":"One sentence the item's stages will read."},` +
		`"why":{"type":"string","description":"One sentence saying why."}` +
		`},"required":["item"],"additionalProperties":false}`
}

// The gloss a person reads beside the call is the reason.
func init() { glossField["factory_item"] = "why" }

// itemOffer is one item card still waiting on somebody, [recipeOffer]'s twin.
type itemOffer struct {
	answers chan ItemAnswer
	notice  ItemNotice
	asked   time.Time
}

// ── the change, applied ─────────────────────────────────────────────────────

// ItemStaged is the item with its own copy of the stages it runs: its own
// when it has them, its recipe's for its kind when it has none yet. The door
// reads an item through it before comparing, so `now:` never says nothing for
// an item that simply has not copied its recipe.
func ItemStaged(it factory.Item, recipe factory.Recipe) factory.Item {
	if len(it.Stages) > 0 {
		return it
	}
	kind := it.Kind
	if kind == "" {
		kind = factory.KindIssue
	}
	it.Stages = factory.CopyStages(recipe.For(kind))
	return it
}

// ApplyItemChange is the one place an item card's change is made, and it is
// pure: the door previews with it and applies with it, so the card and the
// floor cannot disagree about what a yes does.
//
// THE STAGES GO THROUGH [factory.Adapt] AND ONLY THROUGH IT, which is where
// every bound is held; a refusal there refuses the whole change. The
// cap and effort are checked here against the floor's own words, and the note
// is appended to [factory.Item.Notes].
func ApplyItemChange(it factory.Item, change ItemChange, recipe factory.Recipe) (factory.Item, error) {
	it = ItemStaged(it, recipe)
	next, _, err := factory.Adapt(it, change.Edit, recipe)
	if err != nil {
		return it, err
	}
	if change.Cap < 0 {
		return it, errors.New("a budget is never below nothing")
	}
	if change.Cap > 0 {
		next.Cap = change.Cap
	}
	if e := strings.TrimSpace(change.Effort); e != "" {
		if !oneOf(e, itemEfforts) {
			return it, fmt.Errorf("%q is not a thinking level", e)
		}
		if e == "default" {
			e = ""
		}
		started := itemStarted(next)
		next.Stages = factory.CopyStages(next.Stages)
		for i, st := range next.Stages {
			if itemChatStage(st) && !started[st.Name] {
				next.Stages[i].Effort = e
			}
		}
	}
	if note := strings.Join(strings.Fields(change.Note), " "); note != "" {
		next.Notes = append(append([]string(nil), next.Notes...), note)
	}
	return next, nil
}

// itemChatStage says whether a stage is a conversation, the only kind effort
// means anything to. A stage that names no kind is one (factory.Stage's Kind).
func itemChatStage(st factory.Stage) bool {
	return st.Kind == "" || st.Kind == factory.StageChat
}

// itemStarted is the stages the item's stream has begun, by name.
func itemStarted(it factory.Item) map[string]bool {
	out := map[string]bool{}
	if it.Stream == nil {
		return out
	}
	for _, ph := range it.Stream.Phases {
		if ph.State != "" && ph.State != factory.PhasePending {
			out[ph.Name] = true
		}
	}
	return out
}

// ItemEffort is the item's effort as the card compares it: the effort of the
// first conversation stage that is on and has not run, or of the first that
// is on when every one has run.
func ItemEffort(it factory.Item) string {
	started := itemStarted(it)
	first, found := "", false
	for _, st := range it.Stages {
		if !st.On || !itemChatStage(st) {
			continue
		}
		if !started[st.Name] {
			return st.Effort
		}
		if !found {
			first, found = st.Effort, true
		}
	}
	return first
}

// ItemFactsOf is the item as the card compares it.
func ItemFactsOf(it factory.Item) ItemFacts {
	f := ItemFacts{Cap: it.Cap, Effort: ItemEffort(it)}
	for _, st := range it.Stages {
		if st.On {
			f.Stages = append(f.Stages, st.Name)
		}
	}
	return f
}

// ── the card's words ────────────────────────────────────────────────────────

// itemRef is the item's name on the card: the floor's own when the door said
// it, `#<id>` otherwise.
func itemRef(n ItemNotice) string {
	if ref := strings.TrimSpace(n.Ref); ref != "" {
		return ref
	}
	return "#" + strconv.Itoa(n.Item)
}

// itemStagesMoved, itemCapMoved and the rest say which parts of the item the
// card changes, read off before and after rather than off the ask, so a
// switch-on of a stage that is already on is no change at all.
func itemStagesMoved(n ItemNotice) bool {
	return strings.Join(n.Before.Stages, "\x00") != strings.Join(n.After.Stages, "\x00")
}
func itemCapMoved(n ItemNotice) bool { return n.Before.Cap != n.After.Cap }
func itemEffortMoved(n ItemNotice) bool {
	return n.Before.Effort != n.After.Effort
}
func itemHasNote(n ItemNotice) bool { return strings.TrimSpace(n.Note) != "" }

// ItemHead is the card's head: the item's ref and ONE question in words, built
// from what changes. One kind of change asks about itself (`skip the review
// stage?`, `raise the budget to $8?`), and anything more is `change the plan?`.
func ItemHead(n ItemNotice) string {
	return itemRef(n) + DecisionSep + itemQuestionWords(n)
}

func itemQuestionWords(n ItemNotice) string {
	var kinds []string
	if itemStagesMoved(n) {
		kinds = append(kinds, "stages")
	}
	if itemCapMoved(n) {
		kinds = append(kinds, "cap")
	}
	if itemEffortMoved(n) {
		kinds = append(kinds, "effort")
	}
	if itemHasNote(n) {
		kinds = append(kinds, "note")
	}
	switch strings.Join(kinds, "+") {
	case "stages":
		return itemStagesQuestion(n)
	case "cap":
		switch {
		case n.Before.Cap <= 0:
			return "set the budget at " + itemMoney(n.After.Cap) + "?"
		case n.After.Cap > n.Before.Cap:
			return "raise the budget to " + itemMoney(n.After.Cap) + "?"
		}
		return "lower the budget to " + itemMoney(n.After.Cap) + "?"
	case "effort":
		if n.After.Effort == "" {
			return "think the usual amount?"
		}
		return "think " + n.After.Effort + "?"
	case "note":
		return "add a note for the stages?"
	}
	return "change the plan?"
}

// itemStagesQuestion is a stages-only change as one question: what was added,
// switched off or switched on, read off before and after.
func itemStagesQuestion(n ItemNotice) string {
	before, after := map[string]bool{}, map[string]bool{}
	for _, s := range n.Before.Stages {
		before[s] = true
	}
	for _, s := range n.After.Stages {
		after[s] = true
	}
	var gained, lost []string
	for _, s := range n.After.Stages {
		if !before[s] {
			gained = append(gained, s)
		}
	}
	for _, s := range n.Before.Stages {
		if !after[s] {
			lost = append(lost, s)
		}
	}
	switch {
	case len(lost) == 0 && len(gained) == 1 && factory.ApproveWord(gained[0]):
		return "add an approve step?"
	case len(gained) == 0 && len(lost) == 1 && factory.ApproveWord(lost[0]):
		return "skip the approve step?"
	case len(gained) == 0 && len(lost) == 1:
		return "skip the " + lost[0] + " stage?"
	case len(gained) == 0 && len(lost) > 1:
		return "skip the " + itemAnd(lost) + " stages?"
	case len(lost) == 0 && len(gained) == 1:
		return "add a " + gained[0] + " stage?"
	case len(lost) == 0 && len(gained) > 1:
		return "add the " + itemAnd(gained) + " stages?"
	}
	return "change the stages?"
}

// itemAnd is a list in words: `review and neaten`, `a, b and c`.
func itemAnd(list []string) string {
	if len(list) < 2 {
		return strings.Join(list, "")
	}
	return strings.Join(list[:len(list)-1], ", ") + " and " + list[len(list)-1]
}

// ItemRows is the card's body, one row per thing that changes and nothing
// about what does not: `now:` and `after:` when the stages move, then
// `budget  $5 → $8`, `thinking  — → strong`, the note and the
// reason.
func ItemRows(n ItemNotice) []string {
	var rows []string
	if itemStagesMoved(n) {
		rows = append(rows, "now: "+itemList(n.Before.Stages), "after: "+itemAfterList(n.Before.Stages, n.After.Stages))
	}
	if itemCapMoved(n) {
		rows = append(rows, "budget  "+itemMoneyOr(n.Before.Cap)+" → "+itemMoneyOr(n.After.Cap))
	}
	if itemEffortMoved(n) {
		rows = append(rows, "thinking  "+itemOr(n.Before.Effort)+" → "+itemOr(n.After.Effort))
	}
	if note := strings.TrimSpace(n.Note); note != "" {
		rows = append(rows, "note: "+note)
	}
	if why := strings.TrimSpace(n.Why); why != "" {
		rows = append(rows, "why: "+why)
	}
	return rows
}

// itemList is stage names in a row.
func itemList(names []string) string {
	if len(names) == 0 {
		return itemNone
	}
	return strings.Join(names, DecisionSep)
}

// itemAfterList is the stages after the change, every one the item did not
// run before marked with a plus.
func itemAfterList(before, after []string) string {
	had := map[string]bool{}
	for _, s := range before {
		had[s] = true
	}
	out := make([]string, 0, len(after))
	for _, s := range after {
		if !had[s] {
			s = itemAddedMark + s
		}
		out = append(out, s)
	}
	return itemList(out)
}

func itemOr(s string) string {
	if s = strings.TrimSpace(s); s != "" {
		return s
	}
	return itemNone
}

func itemMoneyOr(v float64) string {
	if v > 0 {
		return itemMoney(v)
	}
	return itemNone
}

// itemMoney is dollars as the card spells them: `$8` for a whole amount and
// `$1.50` otherwise.
func itemMoney(v float64) string {
	if v == math.Trunc(v) {
		return "$" + strconv.FormatFloat(v, 'f', 0, 64)
	}
	return "$" + strconv.FormatFloat(v, 'f', 2, 64)
}

// itemNow is the sentence a yes returns: the item's facts after the change,
// only what is set: `#1 now: plan · approve · write · test · proof · budget $8`.
func itemNow(n ItemNotice) string {
	parts := []string{}
	if len(n.After.Stages) > 0 {
		parts = append(parts, strings.Join(n.After.Stages, DecisionSep))
	}
	if n.After.Cap > 0 {
		parts = append(parts, "budget "+itemMoney(n.After.Cap))
	}
	if e := strings.TrimSpace(n.After.Effort); e != "" {
		parts = append(parts, "thinking "+e)
	}
	// THE SECOND LINE SAYS IT IS DONE. A model that read only the facts told
	// the person the card was "waiting on your key" after the key had been
	// pressed, because the tool's description says nothing changes until then.
	out := itemRef(n) + itemNowLead + strings.Join(parts, DecisionSep) + "\n" + itemDone
	if itemHasNote(n) {
		out += " The note is kept on the item for its stages."
	}
	return out
}

// itemChange is the card as the change the door applies. An added sentence is
// a conversation stage with that ask; Adapt names and places it.
func itemChange(n ItemNotice) ItemChange {
	change := ItemChange{
		Edit:   factory.PlanEdit{On: n.On, Skip: n.Skip, Why: n.Why},
		Cap:    n.Cap,
		Effort: n.Effort,
		Note:   n.Note,
	}
	for _, add := range n.Add {
		change.Edit.Add = append(change.Edit.Add, factory.Stage{Ask: add})
	}
	return change
}

// cleanNames trims every entry and drops the empty ones.
func cleanNames(in []string) []string {
	var out []string
	for _, s := range in {
		if s = strings.Join(strings.Fields(s), " "); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// ── the tool ────────────────────────────────────────────────────────────────

// factoryItemTool raises one item card and waits for its answer.
func (a *Agent) factoryItemTool() bare.Tool {
	return bare.Tool{
		Name:        "factory_item",
		Description: factoryItemDescription,
		Schema:      json.RawMessage(factoryItemSchemaJSON()),
		Execute: func(ctx context.Context, args json.RawMessage) (string, bool, error) {
			var parsed struct {
				Item   int      `json:"item"`
				Add    []string `json:"add"`
				Skip   []string `json:"skip"`
				On     []string `json:"on"`
				Cap    float64  `json:"cap"`
				Effort string   `json:"effort"`
				Note   string   `json:"note"`
				Why    string   `json:"why"`
			}
			if len(args) > 0 {
				if err := decodeToolArguments(args, &parsed); err != nil {
					return "Invalid arguments: " + err.Error(), true, nil
				}
			}
			notice := ItemNotice{
				Item:   parsed.Item,
				Add:    cleanNames(parsed.Add),
				Skip:   cleanNames(parsed.Skip),
				On:     cleanNames(parsed.On),
				Cap:    parsed.Cap,
				Effort: strings.ToLower(strings.TrimSpace(parsed.Effort)),
				Note:   strings.Join(strings.Fields(parsed.Note), " "),
				Why:    strings.Join(strings.Fields(parsed.Why), " "),
			}
			if notice.Item <= 0 {
				return "Invalid arguments: factory_item needs the floor id of the item this conversation is about.", true, nil
			}
			if notice.Effort != "" && !oneOf(notice.Effort, itemEfforts) {
				return "Invalid arguments: effort is one of " + strings.Join(itemEfforts, ", ") + ", or left out.", true, nil
			}
			if notice.Cap < 0 {
				return "Invalid arguments: cap is a number of dollars above nothing, or left out.", true, nil
			}
			for _, add := range notice.Add {
				if factory.ParseStage(add).Ask == "" {
					return "Invalid arguments: a stage to add needs to say what it does: " + add + ".", true, nil
				}
			}
			change := itemChange(notice)
			if change.Empty() {
				return "Invalid arguments: factory_item needs something to change: add, skip, on, cap, effort or note.", true, nil
			}
			door := a.config.FactoryItem
			// THE CHANGE IS PREVIEWED BEFORE ANY CARD, so the card shows the item
			// before and after as the floor would make it, and a change the
			// bounds refuse is refused here in their own words, never offered.
			before, after, err := door.Preview(ctx, notice.Item, change)
			if err != nil {
				return itemRefusedLead + oneLine(err.Error()), true, nil
			}
			notice.Ref = before.Ref()
			notice.Before, notice.After = ItemFactsOf(before), ItemFactsOf(after)
			if !itemStagesMoved(notice) && !itemCapMoved(notice) && !itemEffortMoved(notice) && !itemHasNote(notice) {
				return itemAlready, false, nil
			}
			answer, err := a.askItem(ctx, &notice)
			if err != nil {
				return itemUnanswered, false, nil
			}
			if words := strings.TrimSpace(answer.Change); words != "" {
				return itemChangedLead + words + itemChangedTail, false, nil
			}
			if !answer.Approved {
				return itemDeclined, false, nil
			}
			now, err := door.Apply(ctx, notice.Item, change)
			if err != nil {
				return itemRefusedLead + oneLine(err.Error()), true, nil
			}
			changed := notice
			changed.After = ItemFactsOf(now)
			changed.Now = &now
			a.emitFactory(Event{Kind: EventItemChanged, Tool: "factory_item", Text: ItemHead(notice), FactoryItem: &changed})
			return itemNow(changed), false, nil
		},
	}
}

// askItem raises one item card and waits for the person, for as long as the
// window allows. It is [Agent.askRecipe] with the payload changed, and every
// ending but an answer is an error the tool reads as nothing changed.
func (a *Agent) askItem(ctx context.Context, notice *ItemNotice) (ItemAnswer, error) {
	answers := make(chan ItemAnswer, 1)
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return ItemAnswer{}, errAgentClosed
	}
	a.itemSeq++
	notice.ID = "g" + strconv.FormatUint(a.itemSeq, 10)
	if a.itemOffers == nil {
		a.itemOffers = make(map[string]*itemOffer, 1)
	}
	offer := &itemOffer{answers: answers, notice: *notice, asked: time.Now()}
	a.itemOffers[notice.ID] = offer
	a.mu.Unlock()
	card := *notice
	// THE CARD COMES DOWN WITH THE TOOL CALL, as the factory card does.
	defer a.forgetItem(card)
	defer a.raiseQuestion(a.itemQuestion(card.ID, card, offer.asked), func() {
		a.emitFactory(Event{Kind: EventItemProposal, Tool: "factory_item", Text: ItemHead(card), FactoryItem: &card})
	})()

	window := a.config.factoryWindow
	if window <= 0 {
		window = factoryCardWindow
	}
	timer := time.NewTimer(window)
	defer timer.Stop()
	select {
	case answer := <-answers:
		decided := card
		settled := answer
		decided.Decided = &settled
		a.emitFactory(Event{Kind: EventItemProposal, Tool: "factory_item", Text: ItemHead(card), FactoryItem: &decided})
		return answer, nil
	case <-timer.C:
		// NOTHING CHANGED, and that is the whole of what the window decides.
		return ItemAnswer{}, errItemUnanswered
	case <-ctx.Done():
		return ItemAnswer{}, ctx.Err()
	}
}

// errItemUnanswered is the window ending with the card still up.
var errItemUnanswered = errors.New("session: the item card went unanswered")

// itemGoneReason is the sentence a card that came down unanswered retires
// with, on the card and on the question alike.
const itemGoneReason = "nothing changed on the item"

// forgetItem drops one card and, where it was still standing, says on the
// task lane that it came down unanswered ([Agent.forgetRecipe]'s reading).
func (a *Agent) forgetItem(card ItemNotice) {
	a.mu.Lock()
	offer := a.itemOffers[card.ID]
	delete(a.itemOffers, card.ID)
	a.mu.Unlock()
	if offer == nil {
		return
	}
	gone := card
	gone.Withdrawn = itemGoneReason
	a.emitFactory(Event{Kind: EventItemProposal, Tool: "factory_item", Text: ItemHead(card), FactoryItem: &gone})
}

// standingItemCardsLocked is every item card still waiting on somebody, as the
// events that raised them, oldest first. a.mu is held. It is what a window
// that arrives late is handed, for [Agent.standingFactoryCardsLocked]'s reason.
func (a *Agent) standingItemCardsLocked() []Event {
	if len(a.itemOffers) == 0 {
		return nil
	}
	offers := make([]*itemOffer, 0, len(a.itemOffers))
	for _, offer := range a.itemOffers {
		offers = append(offers, offer)
	}
	sort.Slice(offers, func(i, j int) bool {
		if !offers[i].asked.Equal(offers[j].asked) {
			return offers[i].asked.Before(offers[j].asked)
		}
		return offers[i].notice.ID < offers[j].notice.ID
	})
	cards := make([]Event, 0, len(offers))
	for _, offer := range offers {
		card := offer.notice
		cards = append(cards, Event{Kind: EventItemProposal, Tool: "factory_item", Text: ItemHead(card), FactoryItem: &card})
	}
	return cards
}

// itemQuestion is an item card as a question. The head is the card's own
// question, the reason is its body in one line, and the subject is the item.
func (a *Agent) itemQuestion(id string, notice ItemNotice, asked time.Time) Question {
	return a.said(QuestionItem, id, Question{
		Ref:      id,
		Kind:     QuestionItem,
		Ask:      AskPermission,
		Form:     FormCard,
		Asker:    Asker{Kind: AskerModel},
		Head:     ItemHead(notice),
		Reason:   strings.Join(ItemRows(notice), "; "),
		Subject:  SubjectRef{Ref: id, Name: itemRef(notice)},
		Options:  AnswerOptions(QuestionItem),
		Input:    InputShape{Kind: InputText, Prompt: ItemChangePrompt},
		Stakes:   StakesReversible,
		Blocking: Blocking{Turn: true},
		Asked:    asked,
	})
}
