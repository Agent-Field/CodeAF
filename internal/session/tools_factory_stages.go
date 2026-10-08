package session

// THE CHAT'S DOOR ONTO ONE ITEM'S STAGES — BEHIND A CARD.
//
// An item on the factory floor runs through stages (plan · write · test ·
// review · proof, and whatever its repository's recipe adds). An item's own
// conversation (the floor's `T`, cmd/codeaf's talk maker) is where a person
// thinks out loud about it — "should we skip review on this one?", "this
// touches billing, add a security pass" — and `factory_stages` is the verb that
// carries the change they settle on to the item. It carries it as a QUESTION,
// never as a write: `factory_recipe`'s shape (tools_factory_recipe.go) with a
// different payload and a different door.
//
// ── THE LAWS THIS FILE APPLIES ──
//
//   - NOTHING CHANGES UNTIL THE PERSON SAYS YES. The tool raises a card; only
//     `change it` reaches the door. There is no argument and no phrasing that
//     changes the item without that answer.
//
//   - THE BOUNDS ARE THE PLAN STAGE'S, HELD IN CODE. The door applies the edit
//     through [factory.Adapt], which refuses a skipped proof, a skipped gate, a
//     stage a policy names, anything before a stage that already ran, and
//     every change at all under a `fixed` recipe. A yes cannot carry the item
//     past a bound, and a refusal comes back in Adapt's own sentence.
//
//   - IT CHANGES STAGES AND NOTHING ELSE. No cap, no gate, no launch: the edit
//     is a [factory.PlanEdit], which has no field for any of them.
//
//   - THERE IS NO CLOCK THAT CHANGES. The window bounds the tool call, as
//     [factoryCardWindow] does, and its ending is nothing changed.
//
//   - WORDS ARE A CHANGE, NOT A YES, as on the factory card.

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/exec/bare"
	"github.com/Agent-Field/codeaf/internal/factory"
)

// The sentences the model reads back, and the card's own words. The manual
// quotes them (internal/manual/chat/factory.md) and a surface drawing the card
// spells the same head.
const (
	// StagesCardLead opens every stages card's head; [StagesHead] finishes it.
	StagesCardLead = "wants to change "
	// StagesChangeLabel and StagesNotNowLabel are the card's two answers.
	StagesChangeLabel = "change it"
	StagesNotNowLabel = "not now"
	// StagesChangePrompt is the line over the card's text box.
	StagesChangePrompt = FactoryChangePrompt

	stagesChangedLead = "the person changed it: "
	stagesChangedTail = "\nNothing changed. Propose it again with that"
	stagesDeclined    = "nothing changed: the person said no."
	stagesUnanswered  = "nothing changed: the card was never answered"
	// stagesRefusedLead opens what a door that would not change the item
	// returns; the door's own sentence follows it.
	stagesRefusedLead = "nothing changed: "
	// stagesNowLead sits between the item's name and its stages after a yes:
	// `#12's stages are now: plan · write · test · review · security · proof`.
	stagesNowLead = "'s stages are now: "
)

// The card's two keys, the factory card's: `1` is the yes and `2` the answer
// that changes nothing.
const (
	StagesChangeKey = "1"
	StagesNotNowKey = "2"
)

// The marks the head spells a change with: `+security` for a stage added or
// switched on, `−neaten` (a minus sign, not a hyphen) for one skipped.
const (
	stagesOnMark   = "+"
	stagesSkipMark = "−"
)

const factoryStagesDescription = "Offer to change the stages of the ONE factory item this conversation is about, when the person settles on it: \"skip review on this one\", \"add a security pass\", \"switch neaten on\". " +
	"Use it only for the item this conversation was opened for, by its floor id; never for another item. " +
	"add is new stage sentences, a place word first when it matters (\"after test, read it for auth holes\"); skip and on are stage names the item already has. " +
	"NOTHING CHANGES BY CALLING THIS: the person is shown a card, and only their key changes anything. " +
	"The recipe's bounds hold whatever is asked: proof, a person's gate and a stage the policy names are never skipped, a stage that already ran is never touched, and a `fixed` recipe refuses every change; a refusal comes back with its reason. " +
	"If they type a change instead, nothing changes and you are told their words: propose again with them."

func factoryStagesSchemaJSON() string {
	return `{"type":"object","properties":{` +
		`"item":{"type":"integer","description":"The floor id of the item this conversation is about."},` +
		`"add":{"type":"array","items":{"type":"string"},"description":"Stage sentences to add, e.g. \"after review, read it for auth holes\"."},` +
		`"skip":{"type":"array","items":{"type":"string"},"description":"Stage names to switch off for this item."},` +
		`"on":{"type":"array","items":{"type":"string"},"description":"Stage names to switch on for this item."},` +
		`"why":{"type":"string","description":"One sentence saying why."}` +
		`},"required":["item"],"additionalProperties":false}`
}

// The gloss a person reads beside the call is the reason.
func init() { glossField["factory_stages"] = "why" }

// stagesOffer is one stages card still waiting on somebody, [recipeOffer]'s
// twin.
type stagesOffer struct {
	answers chan StagesAnswer
	notice  StagesNotice
	asked   time.Time
}

// stagesNamer is what a [StagesDoor] may also answer: the floor's own name for
// an item (`#12`, or its forge number), or the reason there is no such item.
// A door that answers it is asked BEFORE any card is raised, so a card never
// names an item the floor does not have; one that does not is named by its id.
type stagesNamer interface {
	Ref(ctx context.Context, item int) (string, error)
}

// StagesHead is the card's head for one notice: `wants to change #12's
// stages: +security · −neaten`.
func StagesHead(n StagesNotice) string {
	return StagesCardLead + stagesRef(n) + "'s stages: " + StagesWords(n)
}

// StagesWords is the change itself in one row: every stage added or switched
// on with a plus, every one skipped with a minus, in the order added, on,
// skipped.
func StagesWords(n StagesNotice) string {
	var parts []string
	for _, add := range n.Add {
		if name := factory.ParseStage(add).Name; name != "" {
			parts = append(parts, stagesOnMark+name)
		}
	}
	for _, name := range n.On {
		parts = append(parts, stagesOnMark+name)
	}
	for _, name := range n.Skip {
		parts = append(parts, stagesSkipMark+name)
	}
	return strings.Join(parts, DecisionSep)
}

// StagesSubject is the card's dim facts row: `stages · #12`, then the reason
// when the model gave one.
func StagesSubject(n StagesNotice) string {
	parts := []string{"stages", stagesRef(n)}
	if why := strings.TrimSpace(n.Why); why != "" {
		parts = append(parts, why)
	}
	return strings.Join(parts, DecisionSep)
}

// stagesRef is the item's name on the card: the floor's own when the door
// said it, `#<id>` otherwise.
func stagesRef(n StagesNotice) string {
	if ref := strings.TrimSpace(n.Ref); ref != "" {
		return ref
	}
	return "#" + strconv.Itoa(n.Item)
}

// stagesNow is the sentence a yes returns: `#12's stages are now: plan ·
// write · …`.
func stagesNow(n StagesNotice) string {
	return stagesRef(n) + stagesNowLead + strings.Join(n.Now, DecisionSep)
}

// stagesEdit is the card as the edit the door applies. An added sentence is a
// conversation stage with that ask; Adapt names and places it.
func stagesEdit(n StagesNotice) factory.PlanEdit {
	edit := factory.PlanEdit{On: n.On, Skip: n.Skip, Why: n.Why}
	for _, add := range n.Add {
		edit.Add = append(edit.Add, factory.Stage{Ask: add})
	}
	return edit
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

// factoryStagesTool raises one stages card and waits for its answer.
func (a *Agent) factoryStagesTool() bare.Tool {
	return bare.Tool{
		Name:        "factory_stages",
		Description: factoryStagesDescription,
		Schema:      json.RawMessage(factoryStagesSchemaJSON()),
		Execute: func(ctx context.Context, args json.RawMessage) (string, bool, error) {
			var parsed struct {
				Item int      `json:"item"`
				Add  []string `json:"add"`
				Skip []string `json:"skip"`
				On   []string `json:"on"`
				Why  string   `json:"why"`
			}
			if len(args) > 0 {
				if err := decodeToolArguments(args, &parsed); err != nil {
					return "Invalid arguments: " + err.Error(), true, nil
				}
			}
			notice := StagesNotice{
				Item: parsed.Item,
				Add:  cleanNames(parsed.Add),
				Skip: cleanNames(parsed.Skip),
				On:   cleanNames(parsed.On),
				Why:  strings.Join(strings.Fields(parsed.Why), " "),
			}
			if notice.Item <= 0 {
				return "Invalid arguments: factory_stages needs the floor id of the item this conversation is about.", true, nil
			}
			if len(notice.Add)+len(notice.Skip)+len(notice.On) == 0 {
				return "Invalid arguments: factory_stages needs at least one of add, skip or on.", true, nil
			}
			for _, add := range notice.Add {
				if factory.ParseStage(add).Ask == "" {
					return "Invalid arguments: a stage to add needs to say what it does: " + add + ".", true, nil
				}
			}
			door := a.config.Stages
			if namer, ok := door.(stagesNamer); ok {
				ref, err := namer.Ref(ctx, notice.Item)
				if err != nil {
					return stagesRefusedLead + oneLine(err.Error()), true, nil
				}
				notice.Ref = ref
			}
			answer, err := a.askStages(ctx, &notice)
			if err != nil {
				return stagesUnanswered, false, nil
			}
			if change := strings.TrimSpace(answer.Change); change != "" {
				return stagesChangedLead + change + stagesChangedTail, false, nil
			}
			if !answer.Approved {
				return stagesDeclined, false, nil
			}
			now, err := door.Apply(ctx, notice.Item, stagesEdit(notice))
			if err != nil {
				return stagesRefusedLead + oneLine(err.Error()), true, nil
			}
			changed := notice
			changed.Now = append([]string(nil), now...)
			a.emitFactory(Event{Kind: EventStagesChanged, Tool: "factory_stages", Text: StagesHead(notice), Stages: &changed})
			return stagesNow(changed), false, nil
		},
	}
}

// askStages raises one stages card and waits for the person, for as long as the
// window allows. It is [Agent.askRecipe] with the payload changed, and every
// ending but an answer is an error the tool reads as nothing changed.
func (a *Agent) askStages(ctx context.Context, notice *StagesNotice) (StagesAnswer, error) {
	answers := make(chan StagesAnswer, 1)
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return StagesAnswer{}, errAgentClosed
	}
	a.stagesSeq++
	notice.ID = "g" + strconv.FormatUint(a.stagesSeq, 10)
	if a.stagesOffers == nil {
		a.stagesOffers = make(map[string]*stagesOffer, 1)
	}
	offer := &stagesOffer{answers: answers, notice: *notice, asked: time.Now()}
	a.stagesOffers[notice.ID] = offer
	a.mu.Unlock()
	card := *notice
	// THE CARD COMES DOWN WITH THE TOOL CALL, as the factory card does.
	defer a.forgetStages(card)
	defer a.raiseQuestion(a.stagesQuestion(card.ID, card, offer.asked), func() {
		a.emitFactory(Event{Kind: EventStagesProposal, Tool: "factory_stages", Text: StagesHead(card), Stages: &card})
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
		a.emitFactory(Event{Kind: EventStagesProposal, Tool: "factory_stages", Text: StagesHead(card), Stages: &decided})
		return answer, nil
	case <-timer.C:
		// NOTHING CHANGED, and that is the whole of what the window decides.
		return StagesAnswer{}, errStagesUnanswered
	case <-ctx.Done():
		return StagesAnswer{}, ctx.Err()
	}
}

// errStagesUnanswered is the window ending with the card still up.
var errStagesUnanswered = errors.New("session: the stages card went unanswered")

// stagesGoneReason is the sentence a card that came down unanswered retires
// with, on the card and on the question alike.
const stagesGoneReason = "nothing changed in the item's stages"

// forgetStages drops one card and, where it was still standing, says on the
// task lane that it came down unanswered ([Agent.forgetRecipe]'s reading).
func (a *Agent) forgetStages(card StagesNotice) {
	a.mu.Lock()
	offer := a.stagesOffers[card.ID]
	delete(a.stagesOffers, card.ID)
	a.mu.Unlock()
	if offer == nil {
		return
	}
	gone := card
	gone.Withdrawn = stagesGoneReason
	a.emitFactory(Event{Kind: EventStagesProposal, Tool: "factory_stages", Text: StagesHead(card), Stages: &gone})
}

// standingStagesCardsLocked is every stages card still waiting on somebody, as
// the events that raised them, oldest first. a.mu is held. It is what a window
// that arrives late is handed, for [Agent.standingFactoryCardsLocked]'s reason.
func (a *Agent) standingStagesCardsLocked() []Event {
	if len(a.stagesOffers) == 0 {
		return nil
	}
	offers := make([]*stagesOffer, 0, len(a.stagesOffers))
	for _, offer := range a.stagesOffers {
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
		cards = append(cards, Event{Kind: EventStagesProposal, Tool: "factory_stages", Text: StagesHead(card), Stages: &card})
	}
	return cards
}

// stagesQuestion is a stages card as a question. The subject line is `stages ·
// #12 · <why>`, and the reason is the change itself, which is what the person
// is deciding about.
func (a *Agent) stagesQuestion(id string, notice StagesNotice, asked time.Time) Question {
	return a.said(QuestionStages, id, Question{
		Ref:      id,
		Kind:     QuestionStages,
		Ask:      AskPermission,
		Form:     FormCard,
		Asker:    Asker{Kind: AskerModel},
		Head:     StagesHead(notice),
		Reason:   StagesWords(notice),
		Subject:  SubjectRef{Ref: id, Name: StagesSubject(notice)},
		Options:  AnswerOptions(QuestionStages),
		Input:    InputShape{Kind: InputText, Prompt: StagesChangePrompt},
		Stakes:   StakesReversible,
		Blocking: Blocking{Turn: true},
		Asked:    asked,
	})
}
