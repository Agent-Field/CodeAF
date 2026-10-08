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

// ── the stages card ─────────────────────────────────────────────────────────

// StagesDoor is the one thing a conversation may do to one item already on
// the floor: change which stages it runs. The launch implements it over the
// store (cmd/codeaf's factory.go): it loads the item, reads its repository's
// recipe, applies the edit through [factory.Adapt] — WHICH IS WHERE EVERY
// BOUND IS HELD, never a prompt — and saves it. Apply answers the stage names
// the item runs after the change, in order, or the reason it did not change in
// a person's words (a `fixed` recipe's refusal is Adapt's own sentence).
//
// IT CHANGES STAGES AND NOTHING ELSE. There is no field for the cap, the gate
// or a launch: [factory.PlanEdit] is the whole of what travels.
type StagesDoor interface {
	Apply(ctx context.Context, item int, edit factory.PlanEdit) ([]string, error)
}

// mayStages says whether `factory_stages` belongs on this belt: there is a
// door behind it. It is the belt's predicate, asked of one field.
func (c Config) mayStages() bool { return c.Stages != nil }

// StagesNotice is the stages card. It is the payload of EventStagesProposal
// (ID is the token a surface hands back to [Agent.ResolveStages]) and of
// EventStagesChanged, whose Now is the item's stages after the change.
type StagesNotice struct {
	// ID is THE PROPOSAL'S id; nothing about the item changes until the person
	// says yes.
	ID string
	// Item is the floor's own id for the item, and Ref how the floor names it
	// (`#12`), which is what the card's head says.
	Item int
	Ref  string
	// Add, Skip and On are the edit as the model asked for it: stage sentences
	// to add, and stage names to skip or switch on.
	Add  []string
	Skip []string
	On   []string
	// Why is the model's one sentence for the change.
	Why string
	// Now is the item's stage names after the change, set on
	// EventStagesChanged and empty on a proposal.
	Now []string
	// Decided is set on the one rebroadcast of a card somebody answered.
	Decided *StagesAnswer
	// Withdrawn is set on the one rebroadcast of a card that came down
	// unanswered, in a person's words.
	Withdrawn string
}

// StagesAnswer is what the person said to a stages card.
type StagesAnswer struct {
	// Approved changes the stages exactly as the card showed it.
	Approved bool
	// Change is the person's correction. NOTHING CHANGES ON A CHANGE.
	Change string
}

// ResolveStages answers one EventStagesProposal. An id nobody is waiting on is
// ignored, as [Agent.ResolveFactory] ignores one.
func (a *Agent) ResolveStages(id string, answer StagesAnswer) {
	id = strings.TrimSpace(id)
	a.mu.Lock()
	offer := a.stagesOffers[id]
	delete(a.stagesOffers, id)
	a.mu.Unlock()
	if offer == nil {
		return
	}
	offer.answers <- answer
}
