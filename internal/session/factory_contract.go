package session

// The factory contract: how a conversation offers a piece of work to the
// factory floor and hears the person's answer. It is the standing contract's
// shape (standing_contract.go) with one verb behind it — add — and the whole of
// what travels over the wire between the engine and a surface drawing the card.
//
// NOTHING IS ADDED UNTIL THE PERSON SAYS YES, AND NOTHING STARTS EVEN THEN. The
// chat proposes; the card is answered; only a yes writes one item to the floor,
// as `new`, where it waits for a person to launch it like any other row. There
// is no clock that adds: silence, an interrupted turn and the tool call's own
// window all end with nothing written.

import (
	"context"
	"strings"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// FactoryDoor is the one thing the chat may do to the factory floor: put an
// item on it. The store lane implements it; Add answers the floor's own id for
// the item it wrote.
//
// IT IS ONE VERB ON PURPOSE. Launching, steering and signing off are the
// factory page's own keys, in front of a person; a conversation that could
// reach them would be a second door onto work somebody else is watching.
type FactoryDoor interface {
	Add(ctx context.Context, it factory.Item) (int, error)
}

// mayFactory says whether `factory_add` belongs on this belt: there is a door
// behind it. It is the belt's predicate and the page's (beltfacts.go), asked of
// one field, so the two cannot disagree.
func (c Config) mayFactory() bool { return c.Factory != nil }

// FactoryNotice is the card. It is the payload of EventFactoryProposal (ID is
// the token a surface hands back to [Agent.ResolveFactory]) and of
// EventFactoryAdded (Item is the floor's id for what was written).
type FactoryNotice struct {
	// ID is THE PROPOSAL'S id, not the item's: no item exists until the person
	// says yes, and an id that looked like a floor number would be a row a
	// surface went looking for and never found.
	ID string
	// Title, Body, Repo, Kind and Size are the proposal as the model wrote it,
	// and the card draws them as they are.
	Title string
	Body  string
	Repo  string
	Kind  string
	Size  string
	// Estimate is the model's guess at what the work will cost, in dollars.
	// ZERO IS NO GUESS and draws nothing (the emptiness law).
	Estimate float64
	// Item is the floor's own id, set on EventFactoryAdded and zero on a
	// proposal.
	Item int
	// Now is the item as the floor wrote it, set on EventFactoryAdded when the
	// door can read it back, so a surface with no floor of its own (a window
	// over --host) still draws the item's live card from the event.
	Now *factory.Item `json:",omitempty"`
	// Decided is set on the one rebroadcast of a card somebody answered: what
	// they said. Nil while the card stands.
	Decided *FactoryAnswer
	// Withdrawn is set on the one rebroadcast of a card that came down
	// unanswered — the window ended, or the turn was interrupted — and says so
	// in a person's words. Empty while the card stands.
	Withdrawn string
}

// FactoryAnswer is what the person said to a factory card.
type FactoryAnswer struct {
	// Approved puts the item on the floor, exactly as the card showed it.
	Approved bool
	// Change is the person's own correction — "make it a chore", "that is the
	// web repo" — which goes back to the model to propose again. NOTHING IS
	// WRITTEN ON A CHANGE, whatever else the answer says.
	Change string
}

// ResolveFactory answers one EventFactoryProposal. An id nobody is waiting on —
// a card whose window ended, a second press, an interrupted turn — is ignored,
// exactly as [Agent.ResolveStanding] ignores a late answer.
func (a *Agent) ResolveFactory(id string, answer FactoryAnswer) {
	id = strings.TrimSpace(id)
	a.mu.Lock()
	offer := a.factoryOffers[id]
	delete(a.factoryOffers, id)
	a.mu.Unlock()
	if offer == nil {
		return
	}
	offer.answers <- answer
}

// ── the recipe card ─────────────────────────────────────────────────────────

// RecipeDoor is the one thing the chat may do to a repository's recipe: add a
// line to it. The launch implements it over the repository's own
// `.codeaf/factory.md` (internal/factory's recipefile.go), found the way the
// floor finds it; each verb writes exactly one line and answers why not in a
// person's words when it cannot.
//
// IT ADDS AND NEVER TAKES AWAY. Removing a stage, a policy or a habit is an
// edit a person makes in the file or on the floor, in front of the whole
// recipe; a conversation that could delete one would be rewriting the rules
// somebody else's work is held to.
type RecipeDoor interface {
	// BankStage puts one stage line into the section for kind. A stage of the
	// same name already there is replaced in place, so a yes is still one line.
	BankStage(ctx context.Context, repo string, kind factory.Kind, line string) error
	// BankPolicy adds one sentence under `## policy`.
	BankPolicy(ctx context.Context, repo, sentence string) error
	// BankHabit adds one sentence under `## habits`.
	BankHabit(ctx context.Context, repo, sentence string) error
}

// RecipeNoter is what a [RecipeDoor] may also be: one that banks a notice as a
// change for the team (a branch and a pull request in a git repository) and
// answers what happened, in one of four sentences (factory.RecipeNote*). The
// tool prefers it to the three Bank methods, which stay the plain write.
type RecipeNoter interface {
	BankNote(ctx context.Context, n RecipeNotice) (string, error)
}

// mayRecipe says whether `factory_recipe` belongs on this belt: there is a door
// behind it. It is the belt's predicate and the page's, asked of one field.
func (c Config) mayRecipe() bool { return c.Recipe != nil }

// RecipeNotice is the recipe card. It is the payload of EventRecipeProposal (ID
// is the token a surface hands back to [Agent.ResolveRecipe]) and of
// EventRecipeBanked, which says the line is in the file.
//
// EXACTLY ONE OF Line, Policy AND Habit IS SET, which is the one thing the card
// proposes: a stage line for Kind's section, a policy sentence or a habit.
type RecipeNotice struct {
	// ID is THE PROPOSAL'S id; nothing exists in the file until the person
	// says yes.
	ID string
	// Repo is the repository's short name, as the floor shows it.
	Repo string
	// Kind is the section a stage line goes in (issue, pr, ci); empty for a
	// policy or a habit, which belong to the whole repository.
	Kind string
	// Line is one stage in the file's own grammar, without its number.
	Line string
	// Policy and Habit are one sentence each.
	Policy string
	Habit  string
	// Now and After are the stage names the kind runs today and after a yes,
	// the new one marked `+`, read before the card is raised; both are empty
	// for a policy or a habit, and when the door cannot read the recipe.
	Now   []string
	After []string
	// Why is the model's one sentence for the line, when it gave one.
	Why string
	// Note is what happened to the line after a yes, set on the banked event:
	// `written · pull request #N opened for the team` and its three siblings.
	Note string
	// Decided is set on the one rebroadcast of a card somebody answered.
	Decided *RecipeAnswer
	// Withdrawn is set on the one rebroadcast of a card that came down
	// unanswered, in a person's words.
	Withdrawn string
}

// RecipeAnswer is what the person said to a recipe card.
type RecipeAnswer struct {
	// Approved banks the line exactly as the card showed it.
	Approved bool
	// Change is the person's correction. NOTHING IS BANKED ON A CHANGE.
	Change string
}

// ResolveRecipe answers one EventRecipeProposal. An id nobody is waiting on is
// ignored, as [Agent.ResolveFactory] ignores one.
func (a *Agent) ResolveRecipe(id string, answer RecipeAnswer) {
	id = strings.TrimSpace(id)
	a.mu.Lock()
	offer := a.recipeOffers[id]
	delete(a.recipeOffers, id)
	a.mu.Unlock()
	if offer == nil {
		return
	}
	offer.answers <- answer
}

// ── the item card ───────────────────────────────────────────────────────────

// ItemDoor is the one thing a conversation may do to one item already on the
// floor: change its stages, where the run asks (the field is Gate), its budget (Cap) or its thinking (Effort), or leave its
// stages a note. The launch implements it over the store (cmd/codeaf's
// factory_talk.go), and the change itself is [ApplyItemChange], WHICH IS WHERE
// EVERY BOUND IS HELD: the stages go through [factory.Adapt], never a prompt.
//
// IT IS ASKED TWICE. Preview answers the item as it stands and as it would
// stand after the change, without writing anything, BEFORE any card is raised:
// the card shows both, and a change the bounds refuse is refused with Adapt's
// own sentence and never offered. Apply makes the change after a yes, against
// the item as it is then, and answers the item as the floor now holds it.
//
// THERE IS NO FIELD FOR A LAUNCH, a stop or an approval: [ItemChange] is the
// whole of what travels.
type ItemDoor interface {
	Preview(ctx context.Context, item int, change ItemChange) (before, after factory.Item, err error)
	Apply(ctx context.Context, item int, change ItemChange) (factory.Item, error)
}

// mayItem says whether `factory_item` belongs on this belt: there is a door
// behind it. It is the belt's predicate, asked of one field.
func (c Config) mayItem() bool { return c.FactoryItem != nil }

// ItemChange is what one item card changes, every field optional and the
// zero value of each one "leave it as it is".
type ItemChange struct {
	// Edit is the stages: sentences to add, names to skip or switch on.
	Edit factory.PlanEdit
	// Gate is plan, ship or none; "" leaves it. The person calls it `ask me at`
	// (plan, pull request, never).
	Gate factory.Gate
	// Cap is the item's budget in dollars (the person's word); zero leaves it.
	Cap float64
	// Effort is cheap, strong or default (the knee, stored as ""); "" leaves
	// it. The person calls it `thinking`: the thinking of every conversation
	// stage that has not run.
	Effort string
	// Note is one sentence appended to the item's notes for its stages.
	Note string
}

// Empty says the change changes nothing.
func (c ItemChange) Empty() bool {
	return c.Edit.Empty() && c.Gate == "" && c.Cap == 0 && c.Effort == "" && strings.TrimSpace(c.Note) == ""
}

// ItemFacts is one item as the card compares it, before and after: the stages
// it runs, its gate, its cap and its effort (`ask me at`, `budget`, `thinking` on the card). EVERY FIELD IS THE FLOOR'S OWN
// SPELLING; the card draws `—` where a field is empty.
type ItemFacts struct {
	Stages []string
	Gate   string
	Cap    float64
	Effort string
}

// ItemNotice is the item card. It is the payload of EventItemProposal (ID is
// the token a surface hands back to [Agent.ResolveItem]) and of
// EventItemChanged, whose Now is the item after the change.
type ItemNotice struct {
	// ID is THE PROPOSAL'S id; nothing about the item changes until the person
	// says yes.
	ID string
	// Item is the floor's own id for the item, and Ref how the floor names it
	// (`#12`), which is what the card's head says.
	Item int
	Ref  string
	// Add, Skip and On are the stages as the model asked: stage sentences to
	// add, and stage names to skip or switch on.
	Add  []string
	Skip []string
	On   []string
	// Gate, Cap and Effort are the chips as the model asked; empty or zero is
	// unchanged.
	Gate   string
	Cap    float64
	Effort string
	// Note is the sentence the stages will read, and Why the model's one
	// sentence for the change.
	Note string
	Why  string
	// Before and After are the item as it stands and as it would stand, read by
	// the door's preview before the card is raised. After is the item as the
	// floor holds it on EventItemChanged.
	Before ItemFacts
	After  ItemFacts
	// Now is the whole item as the floor holds it after a yes, set on
	// EventItemChanged only, so a surface with no floor of its own still draws
	// the item's live card from the event.
	Now *factory.Item `json:",omitempty"`
	// Decided is set on the one rebroadcast of a card somebody answered.
	Decided *ItemAnswer
	// Withdrawn is set on the one rebroadcast of a card that came down
	// unanswered, in a person's words.
	Withdrawn string
}

// ItemAnswer is what the person said to an item card.
type ItemAnswer struct {
	// Approved changes the item exactly as the card showed it.
	Approved bool
	// Change is the person's correction. NOTHING CHANGES ON A CHANGE.
	Change string
}

// ResolveItem answers one EventItemProposal. An id nobody is waiting on is
// ignored, as [Agent.ResolveFactory] ignores one.
func (a *Agent) ResolveItem(id string, answer ItemAnswer) {
	id = strings.TrimSpace(id)
	a.mu.Lock()
	offer := a.itemOffers[id]
	delete(a.itemOffers, id)
	a.mu.Unlock()
	if offer == nil {
		return
	}
	offer.answers <- answer
}
