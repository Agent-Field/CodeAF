package remote

// The factory card across a connection. `factory_add` raises its card on the
// task lane (internal/session's emitFactory) and takes its answer through the
// one question door, so the ordinary launch — whose conversation lives in the
// session host and whose window is a client of it — draws the card and answers
// it over this wire exactly as an in-process window does.

import (
	"reflect"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/session"
)

// factoryNotice is a card with something in every field a surface draws from,
// so a field this wire quietly drops fails here rather than on a screen.
func factoryNotice() session.FactoryNotice {
	return session.FactoryNotice{
		ID:       "f3",
		Title:    "fix the double count in the ledger",
		Body:     "the ledger counts a refund twice when it lands on the hour",
		Repo:     "ledger",
		Kind:     "bug",
		Size:     "S",
		Estimate: 1.5,
	}
}

// THE CARD, ITS ANSWER AND THE FLOOR'S NUMBER ALL REACH A HOSTED WINDOW WHOLE,
// in the order they were raised, on the lane the window already holds.
func TestAFactoryCardCrossesTheTaskLaneWhole(t *testing.T) {
	far := &railAgent{fakeAgent: &fakeAgent{}}
	loop := laneLoop(t, far)
	lane, stop := loop.Client.Agent().WatchTaskUpdates()
	t.Cleanup(stop)
	waitFor(t, "the engine opened the surface's task lane", func() bool { return far.opened() == 1 })

	card := factoryNotice()
	far.land(session.Event{Kind: session.EventFactoryProposal, Tool: "factory_add", Text: card.Title, Factory: &card})
	got := nextTask(t, lane)
	if got.Kind != session.EventFactoryProposal || got.Factory == nil || !reflect.DeepEqual(*got.Factory, card) {
		t.Fatalf("the card arrived changed: %v / %+v, want %+v", got.Kind, got.Factory, card)
	}

	decided := card
	decided.Decided = &session.FactoryAnswer{Approved: true}
	far.land(session.Event{Kind: session.EventFactoryProposal, Tool: "factory_add", Text: card.Title, Factory: &decided})
	got = nextTask(t, lane)
	if got.Factory == nil || got.Factory.Decided == nil || !got.Factory.Decided.Approved || got.Factory.ID != card.ID {
		t.Fatalf("the settled card arrived without its answer: %+v", got.Factory)
	}

	added := card
	added.Item = 12
	far.land(session.Event{Kind: session.EventFactoryAdded, Tool: "factory_add", Text: "#12", Factory: &added})
	got = nextTask(t, lane)
	if got.Kind != session.EventFactoryAdded || got.Factory == nil || got.Factory.Item != 12 || got.Factory.ID != card.ID {
		t.Fatalf("the floor's number arrived as %v / %+v", got.Kind, got.Factory)
	}
}

// A WINDOW THAT ATTACHES AFTER THE CARD WAS RAISED IS HANDED IT, because the
// lane opens with what the engine replays onto it (internal/session replays a
// card still standing), and a withdrawn card that crosses afterwards settles it.
func TestAFactoryCardReachesAWindowThatAttachedLate(t *testing.T) {
	far := &railAgent{fakeAgent: &fakeAgent{}}
	card := factoryNotice()
	far.roster = []session.Event{{Kind: session.EventFactoryProposal, Tool: "factory_add", Text: card.Title, Factory: &card}}
	loop := laneLoop(t, far)

	lane, stop := loop.Client.Agent().WatchTaskUpdates()
	t.Cleanup(stop)
	got := nextTask(t, lane)
	if got.Kind != session.EventFactoryProposal || got.Factory == nil || got.Factory.ID != card.ID || got.Factory.Decided != nil {
		t.Fatalf("a window attaching late was handed %v / %+v, want the standing card", got.Kind, got.Factory)
	}

	gone := card
	gone.Withdrawn = "nothing was added to the factory floor"
	far.land(session.Event{Kind: session.EventFactoryProposal, Tool: "factory_add", Text: card.Title, Factory: &gone})
	got = nextTask(t, lane)
	if got.Factory == nil || got.Factory.Withdrawn != gone.Withdrawn {
		t.Fatalf("the withdrawal arrived as %+v", got.Factory)
	}
}

// THE ANSWER GOES BACK THROUGH THE ONE QUESTION DOOR with the proposal id as
// its Ref, which is a string, and arrives exactly as it was given — the key, the
// words and the kind the session's ResolveQuestion routes to ResolveFactory on.
func TestAFactoryAnswerCrossesAsAQuestion(t *testing.T) {
	far := newAskingAgent()
	loop := laneLoop(t, far)
	for _, given := range []session.Answer{
		{Kind: session.QuestionFactory, Ref: "f3", Key: "1", Picked: []string{"1"}},
		{Kind: session.QuestionFactory, Ref: "f4", Change: "that is the web repo"},
	} {
		if err := loop.Client.Agent().ResolveQuestion(given); err != nil {
			t.Fatalf("answering a factory card over the wire: %v", err)
		}
		heard := far.heard()
		if !reflect.DeepEqual(heard[len(heard)-1], given) {
			t.Fatalf("the factory answer arrived changed:\n got %+v\nwant %+v", heard[len(heard)-1], given)
		}
	}
}

// BOTH KINDS MOVE WHAT A FRAME READS: a card is a turn parked on a person, and
// a hidden window's attention has to hear it before the event wakes it.
func TestFactoryEventsMoveTheFacts(t *testing.T) {
	for _, kind := range []session.EventKind{session.EventFactoryProposal, session.EventFactoryAdded} {
		if !factsMoved(kind) {
			t.Fatalf("factsMoved(%v) is false", kind)
		}
	}
}

// THE RECIPE CARD CROSSES THE SAME LANE WHOLE, and its answer the same door.
func TestAFactoryRecipeCardCrossesTheTaskLaneWhole(t *testing.T) {
	far := &railAgent{fakeAgent: &fakeAgent{}}
	loop := laneLoop(t, far)
	lane, stop := loop.Client.Agent().WatchTaskUpdates()
	t.Cleanup(stop)
	waitFor(t, "the engine opened the surface's task lane", func() bool { return far.opened() == 1 })

	card := session.RecipeNotice{ID: "r2", Repo: "web", Kind: "issue", Line: "security · chat · read it for auth holes · when touches auth"}
	far.land(session.Event{Kind: session.EventRecipeProposal, Tool: "factory_recipe", Text: session.RecipeHead(card), Recipe: &card})
	got := nextTask(t, lane)
	if got.Kind != session.EventRecipeProposal || got.Recipe == nil || !reflect.DeepEqual(*got.Recipe, card) {
		t.Fatalf("the card arrived changed: %v / %+v, want %+v", got.Kind, got.Recipe, card)
	}

	decided := card
	decided.Decided = &session.RecipeAnswer{Approved: true}
	far.land(session.Event{Kind: session.EventRecipeProposal, Tool: "factory_recipe", Recipe: &decided})
	got = nextTask(t, lane)
	if got.Recipe == nil || got.Recipe.Decided == nil || !got.Recipe.Decided.Approved {
		t.Fatalf("the settled card arrived without its answer: %+v", got.Recipe)
	}

	far.land(session.Event{Kind: session.EventRecipeBanked, Tool: "factory_recipe", Recipe: &card})
	got = nextTask(t, lane)
	if got.Kind != session.EventRecipeBanked || got.Recipe == nil || got.Recipe.ID != card.ID {
		t.Fatalf("the bank arrived as %v / %+v", got.Kind, got.Recipe)
	}

	asking := newAskingAgent()
	answers := laneLoop(t, asking)
	given := session.Answer{Kind: session.QuestionRecipe, Ref: "r2", Key: "1", Picked: []string{"1"}}
	if err := answers.Client.Agent().ResolveQuestion(given); err != nil {
		t.Fatalf("answering a recipe card over the wire: %v", err)
	}
	if heard := asking.heard(); !reflect.DeepEqual(heard[len(heard)-1], given) {
		t.Fatalf("the recipe answer arrived changed: %+v", heard[len(heard)-1])
	}
	for _, kind := range []session.EventKind{session.EventRecipeProposal, session.EventRecipeBanked} {
		if !factsMoved(kind) {
			t.Fatalf("factsMoved(%v) is false", kind)
		}
	}
}

// THE ITEM CARD CROSSES THE SAME LANE WHOLE — its before and after, its
// answer, and the item the change left behind, which is what a hosted window
// draws the live card from — and its answer the same door.
func TestAFactoryItemCardCrossesTheTaskLaneWhole(t *testing.T) {
	far := &railAgent{fakeAgent: &fakeAgent{}}
	loop := laneLoop(t, far)
	lane, stop := loop.Client.Agent().WatchTaskUpdates()
	t.Cleanup(stop)
	waitFor(t, "the engine opened the surface's task lane", func() bool { return far.opened() == 1 })

	card := session.ItemNotice{ID: "g2", Item: 1, Ref: "#1", Skip: []string{"review"}, Cap: 8, Note: "the fixture is flaky", Why: "a small change",
		Before: session.ItemFacts{Stages: []string{"plan", "approve", "write", "test", "review", "proof"}, Cap: 5},
		After:  session.ItemFacts{Stages: []string{"plan", "approve", "write", "test", "proof"}, Cap: 8}}
	far.land(session.Event{Kind: session.EventItemProposal, Tool: "factory_item", Text: session.ItemHead(card), FactoryItem: &card})
	got := nextTask(t, lane)
	if got.Kind != session.EventItemProposal || got.FactoryItem == nil || !reflect.DeepEqual(*got.FactoryItem, card) {
		t.Fatalf("the card arrived changed: %v / %+v, want %+v", got.Kind, got.FactoryItem, card)
	}

	changed := card
	changed.Now = &factory.Item{ID: 1, Title: "Total double-counts an entry added twice", Repo: "factory-demo", State: factory.StateNew, Cap: 8,
		Notes: []string{"the fixture is flaky"}}
	far.land(session.Event{Kind: session.EventItemChanged, Tool: "factory_item", FactoryItem: &changed})
	got = nextTask(t, lane)
	if got.Kind != session.EventItemChanged || got.FactoryItem == nil || got.FactoryItem.Now == nil ||
		got.FactoryItem.Now.Cap != 8 || got.FactoryItem.Now.Title != changed.Now.Title ||
		!reflect.DeepEqual(got.FactoryItem.Now.Notes, changed.Now.Notes) {
		t.Fatalf("the change arrived as %v / %+v", got.Kind, got.FactoryItem)
	}

	added := factoryNotice()
	added.Item = 12
	added.Now = &factory.Item{ID: 12, Title: added.Title, Repo: added.Repo, State: factory.StateNew}
	far.land(session.Event{Kind: session.EventFactoryAdded, Tool: "factory_add", Factory: &added})
	got = nextTask(t, lane)
	if got.Factory == nil || got.Factory.Now == nil || got.Factory.Now.ID != 12 || got.Factory.Now.Title != added.Title {
		t.Fatalf("the added item arrived as %+v", got.Factory)
	}

	asking := newAskingAgent()
	answers := laneLoop(t, asking)
	given := session.Answer{Kind: session.QuestionItem, Ref: "g2", Key: "1", Picked: []string{"1"}}
	if err := answers.Client.Agent().ResolveQuestion(given); err != nil {
		t.Fatalf("answering an item card over the wire: %v", err)
	}
	if heard := asking.heard(); !reflect.DeepEqual(heard[len(heard)-1], given) {
		t.Fatalf("the item answer arrived changed: %+v", heard[len(heard)-1])
	}
	for _, kind := range []session.EventKind{session.EventItemProposal, session.EventItemChanged} {
		if !factsMoved(kind) {
			t.Fatalf("factsMoved(%v) is false", kind)
		}
	}
}
