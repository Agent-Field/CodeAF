package session

// THE CHAT'S DOOR ONTO THE FACTORY FLOOR — BEHIND A CARD.
//
// The factory floor is where work stands in rows until somebody launches it
// (internal/factory). A conversation is often where that work is first noticed:
// "that belongs on the floor", or a piece of work the model can see is whole
// enough to run on its own later. `factory_add` is the one verb that carries it
// there, and it carries it as a QUESTION, never as a write.
//
// ── THE LAWS THIS FILE APPLIES ──
//
//   - NOTHING IS ADDED UNTIL THE PERSON SAYS YES. The tool raises a card; only
//     `add it` writes the item. There is no argument and no phrasing that
//     writes without that answer.
//
//   - THERE IS NO CLOCK THAT ADDS. This is propose_subharness's law and not
//     propose_task's: a task card's countdown ends in a yes because it is a
//     window to redirect work already agreed to, and this card may not, because
//     a row nobody agreed to is a row somebody else has to clear. The window
//     below bounds the TOOL CALL — a tool that waits on a person without a bound
//     of its own is the one way left to hang a turn's batch — and its ending is
//     nothing added.
//
//   - NOTHING STARTS FROM THE CHAT. A yes writes one item as `new`. Launching it
//     is the factory page's own key, in front of a person, like every other row;
//     the conversation has no verb that reaches it.
//
//   - WORDS ARE A CHANGE, NOT A YES. A person who typed a correction has said the
//     card is nearly right and named what is wrong with it, so nothing is
//     written and the model is told to propose again with their words — the
//     standing card's reading of a sentence (tools_standing.go), for the same
//     reason.

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

// factoryCardWindow is how long a raised card holds the tool call open.
//
// IT IS NOT A DEADLINE ON A PERSON and its expiry adds NOTHING, exactly as
// [subharnessCardWindow]'s does: what it bounds is the tool call, and when it
// ends the model is told the card was never answered.
const factoryCardWindow = 15 * time.Minute

// The sentences the model reads back, and the card's own words. They are
// constants because the manual quotes them (internal/manual/chat/factory.md)
// and a surface drawing the card must spell the same head.
const (
	// FactoryCardLead opens the card's head; the title follows it.
	FactoryCardLead = "wants to put this on the factory floor: "
	// FactoryAddLabel and FactoryNotNowLabel are the card's two answers.
	FactoryAddLabel    = "add it"
	FactoryNotNowLabel = "not now"
	// FactoryChangePrompt is the line over the card's text box.
	FactoryChangePrompt = "say what to change… (enter sends it)"

	// factoryAddedTail ends the sentence a yes returns: `#<id> <title>` before it.
	factoryAddedTail = " is on the factory floor"
	// factoryChangedLead and factoryChangedTail wrap the person's own words.
	factoryChangedLead = "the person changed it: "
	factoryChangedTail = "\nNothing is on the floor yet. Propose it again with that"
	// factoryDeclined is what a `not now` returns.
	factoryDeclined = "nothing was added: the person said no."
	// factoryUnanswered is what the window ending, or the turn being
	// interrupted, returns.
	factoryUnanswered = "nothing was added: the card was never answered"
)

// The card's two keys. `1` is the yes on every card in this engine and `2` the
// answer that changes nothing ([AnswerOptions] marks it safe).
const (
	FactoryAddKey    = "1"
	FactoryNotNowKey = "2"
)

// factoryKinds and factorySizes are the schema's enums, held once so the schema
// and the handler's check read the same list.
var (
	factoryKinds = []string{"bug", "feat", "chore", "question", "pr"}
	factorySizes = []string{"S", "M", "L"}
)

const factoryAddDescription = "Offer one piece of work to the factory floor, where work waits in rows until a person launches it. " +
	"WHEN THE PERSON NAMES THE FLOOR (\"factory\", \"factory floor\", \"put it on the floor\"), CALL IT AT ONCE with what you have: never ask which repository (use this workspace's name), never ask for more detail first; a short title and a one-line body are enough, because the person edits the card. " +
	"Also call it, unasked, when you judge a piece of work is self-contained enough to run on its own later — a bug with a clear repro, a chore with a clear end. " +
	"NOTHING IS ADDED BY CALLING THIS: the person is shown a card with your title, repo, kind and reason, and only their `add it` writes the item. " +
	"NOTHING STARTS FROM THE CHAT EITHER: a yes puts the item on the floor as new, and launching it is the person's, on the factory page. " +
	"If they type a change instead, nothing is written and you are told their words: propose again with them. " +
	"Do not use this for work you are about to do here, and do not offer the same work twice after a no."

func factoryAddSchemaJSON() string {
	quoted := func(list []string) string {
		out := make([]string, len(list))
		for i, item := range list {
			out[i] = strconv.Quote(item)
		}
		return strings.Join(out, ",")
	}
	return `{"type":"object","properties":{` +
		`"title":{"type":"string","description":"One line naming the work, as it would read on the floor."},` +
		`"body":{"type":"string","description":"What the work is and why, in a few sentences: what is wrong or wanted, and how somebody would know it is done. This is the reason the person reads on the card."},` +
		`"repo":{"type":"string","description":"The repository's short name as the person says it, or this workspace's name when they do not name one."},` +
		`"kind":{"type":"string","enum":[` + quoted(factoryKinds) + `],"description":"What sort of work it is."},` +
		`"size":{"type":"string","enum":[` + quoted(factorySizes) + `],"description":"Your guess at its size, when you have one."},` +
		`"estimate_usd":{"type":"number","description":"Your guess at what running it will cost, in dollars, when you have one."}` +
		`},"required":["title","body","repo","kind"],"additionalProperties":false}`
}

// The gloss a person reads beside the call is the work's title, which is what
// they are about to be asked about — propose_task's reason, one door over.
func init() { glossField["factory_add"] = "title" }

// factoryTools is the one verb, or nothing at all when there is no floor behind
// it — memoryTools' law: a model told it can put work on the floor plans around
// that for the rest of the conversation, so a session with no door does not
// have the verb.
//
// `factory_recipe` (tools_factory_recipe.go) rides beside it on its own door,
// and is absent on the same law when that door is nil.
func (a *Agent) factoryTools() []bare.Tool {
	var tools []bare.Tool
	if a.config.mayFactory() {
		tools = append(tools, a.factoryAddTool())
	}
	if a.config.mayRecipe() {
		tools = append(tools, a.factoryRecipeTool())
	}
	return tools
}

// factoryOffer is one card still waiting on somebody: the channel its answer
// arrives on, the card itself for a surface that subscribes late, and when it
// was asked, which orders the open questions.
type factoryOffer struct {
	answers chan FactoryAnswer
	notice  FactoryNotice
	asked   time.Time
}

// factoryAddTool raises one card and waits for its answer.
func (a *Agent) factoryAddTool() bare.Tool {
	return bare.Tool{
		Name:        "factory_add",
		Description: factoryAddDescription,
		Schema:      json.RawMessage(factoryAddSchemaJSON()),
		Execute: func(ctx context.Context, args json.RawMessage) (string, bool, error) {
			var parsed struct {
				Title    string  `json:"title"`
				Body     string  `json:"body"`
				Repo     string  `json:"repo"`
				Kind     string  `json:"kind"`
				Size     string  `json:"size"`
				Estimate float64 `json:"estimate_usd"`
			}
			if len(args) > 0 {
				if err := decodeToolArguments(args, &parsed); err != nil {
					return "Invalid arguments: " + err.Error(), true, nil
				}
			}
			notice := FactoryNotice{
				Title: strings.Join(strings.Fields(parsed.Title), " "),
				Body:  strings.TrimSpace(parsed.Body),
				Repo:  strings.TrimSpace(parsed.Repo),
				Kind:  strings.ToLower(strings.TrimSpace(parsed.Kind)),
				Size:  strings.ToUpper(strings.TrimSpace(parsed.Size)),
			}
			if parsed.Estimate > 0 {
				notice.Estimate = parsed.Estimate
			}
			if notice.Title == "" || notice.Body == "" || notice.Repo == "" {
				return "Invalid arguments: factory_add needs a title, a body saying what and why, and the repo it belongs to.", true, nil
			}
			if !oneOf(notice.Kind, factoryKinds) {
				return "Invalid arguments: kind is one of " + strings.Join(factoryKinds, ", ") + ".", true, nil
			}
			if notice.Size != "" && !oneOf(notice.Size, factorySizes) {
				return "Invalid arguments: size is one of " + strings.Join(factorySizes, ", ") + ", or left out.", true, nil
			}
			answer, err := a.askFactory(ctx, &notice)
			if err != nil {
				return factoryUnanswered, false, nil
			}
			if change := strings.TrimSpace(answer.Change); change != "" {
				return factoryChangedLead + change + factoryChangedTail, false, nil
			}
			if !answer.Approved {
				return factoryDeclined, false, nil
			}
			id, err := a.config.Factory.Add(ctx, factoryItem(notice, time.Now()))
			if err != nil {
				return "nothing was added: the factory floor would not take it (" + oneLine(err.Error()) + ")", true, nil
			}
			added := notice
			added.Item = id
			a.emitFactory(Event{Kind: EventFactoryAdded, Tool: "factory_add", Text: "#" + strconv.Itoa(id), Factory: &added})
			return "#" + strconv.Itoa(id) + " " + notice.Title + factoryAddedTail, false, nil
		},
	}
}

// factoryItem is the card as the floor's row: new, from chat, the owner's own,
// with its stages left for the store to fill from the repo's recipe.
//
// AUTHOR IS LEFT EMPTY ON PURPOSE. The engine behind the door knows whose floor
// this is; a name written here would be the model's guess at it.
func factoryItem(notice FactoryNotice, now time.Time) factory.Item {
	kind := factory.KindIssue
	triageType := notice.Kind
	switch notice.Kind {
	case "pr":
		kind, triageType = factory.KindPR, "review"
	case "chore":
		kind = factory.KindChore
	}
	return factory.Item{
		Repo:    notice.Repo,
		Places:  []string{notice.Repo},
		Kind:    kind,
		Title:   notice.Title,
		Body:    notice.Body,
		Tier:    factory.TierOwner,
		Origin:  factory.OriginChat,
		Created: now,
		Changed: now,
		State:   factory.StateNew,
		Triage:  factory.Triage{Type: triageType, Size: notice.Size, Est: notice.Estimate},
	}
}

// askFactory raises one card and waits for the person, for as long as the
// window allows.
//
// It is [Agent.askSubharnessCard] with the payload changed: the card goes out
// on the task lane rather than the harness lane, and the question through the
// one door ([Agent.raiseQuestion]) beside it. Every ending but an answer is an
// error, and the tool reads every error as nothing added.
func (a *Agent) askFactory(ctx context.Context, notice *FactoryNotice) (FactoryAnswer, error) {
	answers := make(chan FactoryAnswer, 1)
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return FactoryAnswer{}, errAgentClosed
	}
	a.factorySeq++
	notice.ID = "f" + strconv.FormatUint(a.factorySeq, 10)
	if a.factoryOffers == nil {
		a.factoryOffers = make(map[string]*factoryOffer, 1)
	}
	offer := &factoryOffer{answers: answers, notice: *notice, asked: time.Now()}
	a.factoryOffers[notice.ID] = offer
	a.mu.Unlock()
	card := *notice
	// THE CARD COMES DOWN WITH THE TOOL CALL. Every road out of here but an
	// answer leaves a question nobody is listening to, so the forgetting — and
	// the withdrawal it announces — is deferred from the same place.
	defer a.forgetFactory(card)
	defer a.raiseQuestion(a.factoryQuestion(card.ID, card, offer.asked), func() {
		a.emitFactory(Event{Kind: EventFactoryProposal, Tool: "factory_add", Text: card.Title, Factory: &card})
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
		a.emitFactory(Event{Kind: EventFactoryProposal, Tool: "factory_add", Text: card.Title, Factory: &decided})
		return answer, nil
	case <-timer.C:
		// NOTHING WAS ADDED, and that is the whole of what the window decides.
		// It is not a no the person said and it is not recorded as one.
		return FactoryAnswer{}, errFactoryUnanswered
	case <-ctx.Done():
		return FactoryAnswer{}, ctx.Err()
	}
}

// standingFactoryCardsLocked is every factory card still waiting on somebody,
// as the events that raised them, oldest first. a.mu is held.
//
// IT IS WHAT A WINDOW THAT ARRIVES LATE IS HANDED. The card goes out on the task
// lane once, to whoever is subscribed at that moment, and a window that opens
// while the tool call is still parked — a conversation switched back to, the
// session host's next window, a link repaired — would otherwise hold a question
// whose card it never drew ([Agent.WatchTaskUpdates] replays these). A card
// that was answered or came down is out of the map already, so a settled card
// is never replayed as a standing one.
func (a *Agent) standingFactoryCardsLocked() []Event {
	if len(a.factoryOffers) == 0 {
		return nil
	}
	offers := make([]*factoryOffer, 0, len(a.factoryOffers))
	for _, offer := range a.factoryOffers {
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
		cards = append(cards, Event{Kind: EventFactoryProposal, Tool: "factory_add", Text: card.Title, Factory: &card})
	}
	return cards
}

// errFactoryUnanswered is the window ending with the card still up.
var errFactoryUnanswered = errors.New("session: the factory card went unanswered")

// forgetFactory drops one card and, WHERE IT WAS STILL STANDING, says on the
// task lane that it came down unanswered. A card somebody answered is already
// out of the map ([Agent.ResolveFactory] took it), so this never echoes a
// decision as a withdrawal.
func (a *Agent) forgetFactory(card FactoryNotice) {
	a.mu.Lock()
	offer := a.factoryOffers[card.ID]
	delete(a.factoryOffers, card.ID)
	a.mu.Unlock()
	if offer == nil {
		return
	}
	gone := card
	gone.Withdrawn = factoryGoneReason
	a.emitFactory(Event{Kind: EventFactoryProposal, Tool: "factory_add", Text: card.Title, Factory: &gone})
}

// factoryGoneReason is the sentence a card that came down unanswered retires
// with, on the card and on the question alike.
const factoryGoneReason = "nothing was added to the factory floor"

// emitFactory sends one factory event to every standing task-lane watcher, as
// [Agent.emitStandingNews] does: the card and the news both belong to a floor
// that outlives the turn, and the task lane is the subscription a surface holds
// for the whole life of the session.
func (a *Agent) emitFactory(event Event) {
	a.mu.Lock()
	watchers := make([]*eventStream, len(a.taskWatchers))
	copy(watchers, a.taskWatchers)
	a.mu.Unlock()
	for _, watcher := range watchers {
		watcher.send(event)
	}
}

// factoryQuestion is a factory card as a question. The subject line is the
// card's own facts, repo · kind · size, and the reason is the model's body —
// what the person reads before deciding.
func (a *Agent) factoryQuestion(id string, notice FactoryNotice, asked time.Time) Question {
	parts := make([]string, 0, 3)
	for _, part := range []string{notice.Repo, notice.Kind, notice.Size} {
		if part = strings.TrimSpace(part); part != "" {
			parts = append(parts, part)
		}
	}
	return a.said(QuestionFactory, id, Question{
		Ref:      id,
		Kind:     QuestionFactory,
		Ask:      AskPermission,
		Form:     FormCard,
		Asker:    Asker{Kind: AskerModel},
		Head:     FactoryCardLead + notice.Title,
		Reason:   notice.Body,
		Subject:  SubjectRef{Ref: id, Name: strings.Join(parts, DecisionSep)},
		Options:  AnswerOptions(QuestionFactory),
		Input:    InputShape{Kind: InputText, Prompt: FactoryChangePrompt},
		Stakes:   StakesReversible,
		Blocking: Blocking{Turn: true},
		Asked:    asked,
	})
}

// oneOf reports whether value is one of list.
func oneOf(value string, list []string) bool {
	for _, item := range list {
		if value == item {
			return true
		}
	}
	return false
}
