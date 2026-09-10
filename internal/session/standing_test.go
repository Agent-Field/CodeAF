package session

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/exec/bare"
	"github.com/Agent-Field/aforge-v2/internal/manual"
	"github.com/Agent-Field/aforge-v2/internal/standing"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// ── a store this package can watch ──────────────────────────────────────────

// fakeStanding is [standingStore] with the files taken out. It exists because
// what these tests are about is the CARD — what a yes writes, what a no does
// not, what silence does — and the real store's writes are internal/standing's
// own business and its own tests.
type fakeStanding struct {
	root    string
	created []standing.Item
	saved   []standing.Item
	items   map[string]standing.Item
	fail    error
	next    int
}

func newFakeStanding(t *testing.T) *fakeStanding {
	t.Helper()
	return &fakeStanding{root: t.TempDir(), items: map[string]standing.Item{}}
}

func (f *fakeStanding) Root() string { return f.root }

// ExchangeDir is the real store's own arithmetic, because what the tests below
// check is that the ITEM points at it — not where internal/standing decided to
// put it.
func (f *fakeStanding) ExchangeDir(id string) string {
	return filepath.Join(f.root, id, "exchange")
}

func (f *fakeStanding) Create(item standing.Item) (standing.Item, error) {
	if err := item.Validate(); err != nil {
		return standing.Item{}, err
	}
	if f.fail != nil {
		return standing.Item{}, f.fail
	}
	f.next++
	item.ID = "item" + string(rune('0'+f.next))
	item.Status = standing.StatusActive
	item.Created, item.Updated = time.Now(), time.Now()
	f.created = append(f.created, item)
	f.items[item.ID] = item
	return item, nil
}

func (f *fakeStanding) Save(item standing.Item) error {
	if err := item.Validate(); err != nil {
		return err
	}
	f.saved = append(f.saved, item)
	f.items[item.ID] = item
	return nil
}

func (f *fakeStanding) Get(id string) (standing.Item, error) {
	if item, found := f.items[id]; found {
		return item, nil
	}
	return standing.Item{}, standing.ErrNotFound
}

func (f *fakeStanding) ForWorkspace(workspace string) ([]standing.Item, error) {
	var found []standing.Item
	for _, item := range f.items {
		if item.Workspace == workspace {
			found = append(found, item)
		}
	}
	return found, nil
}

// standCall is one scripted `stand` call, in the shape the model writes them.
func standCall(id, arguments string) step {
	return func(context.Context, []ai.Message) (*ai.Response, error) {
		return toolResponse(id, "stand", arguments), nil
	}
}

// aReminder is the smallest complete proposal: one moment, one line.
func aReminder() string {
	at := time.Now().Add(2 * time.Hour).Format(time.RFC3339)
	body := map[string]any{
		"op":         "propose",
		"words":      "remind me at 6 to leave",
		"when":       map[string]any{"kind": "at", "at": at},
		"does":       map[string]any{"kind": "say", "say": "time to leave"},
		"when_words": "at 6 this evening",
		"cost_words": "nothing to speak of — one line, once",
	}
	raw, _ := json.Marshal(body)
	return string(raw)
}

// standingAgent is a conversation with the ambient side wired to a fake store.
func standingAgent(t *testing.T, completer Completer, store *fakeStanding, mutate func(*Config)) *Agent {
	t.Helper()
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.Standing = &Standing{}
		config.standingItems = store
		config.AskConsent = true
		config.TaskAutoApproveSeconds = 0
		if mutate != nil {
			mutate(config)
		}
	})
	return agent
}

// drainAnsweringStanding is [drainAnsweringTasks] for the other card.
func drainAnsweringStanding(t *testing.T, events <-chan Event, answer func(Event)) []Event {
	t.Helper()
	var collected []Event
	deadline := time.After(10 * time.Second)
	for {
		select {
		case event, open := <-events:
			if !open {
				return collected
			}
			collected = append(collected, event)
			if event.Kind == EventStandingProposal && answer != nil {
				answer(event)
			}
		case <-deadline:
			t.Fatalf("the turn never finished; events so far: %v", kinds(collected))
			return nil
		}
	}
}

// ── the belt ────────────────────────────────────────────────────────────────

// A CAPABILITY THAT CANNOT WORK IS ABSENT, NOT BROKEN. With no store behind it
// the model is not given the verb at all, so it cannot plan a reply around a
// reminder this build could never keep.
func TestStandIsOnTheBeltOnlyWhenSomethingIsBehindIt(t *testing.T) {
	bare := &Agent{config: Config{Workspace: t.TempDir()}}
	if standingOnBelt(bare.belt()) {
		t.Fatal("a session with no standing store was given the stand verb")
	}
	wired := &Agent{config: Config{Workspace: t.TempDir(), standingItems: newFakeStanding(t)}}
	if !standingOnBelt(wired.belt()) {
		t.Fatal("a session with a standing store was not given the stand verb")
	}
	// THE MANUAL LAW. internal/session's manual_test.go checks the belt it can
	// build, and it builds one with no standing seam — so the one tool that is
	// conditional on that seam has to be checked where it actually lives.
	// The term is BACKTICKED because "stand" is a substring of "understand" and
	// of "standing", both of which the corpus already uses: a gate that matched
	// those would be satisfied by a page that never names the tool.
	if !manual.Chat().Mentions("`stand`") {
		t.Error("no chat manual page mentions the stand tool — add it to internal/manual/chat/")
	}
}

func standingOnBelt(tools []bare.Tool) bool {
	for _, tool := range tools {
		if tool.Name == "stand" {
			return true
		}
	}
	return false
}

// ── the card's endings ──────────────────────────────────────────────────────

// A YES IS THE ONLY THING THAT CREATES ONE, and what it creates carries the
// person's own sentence, the project it was said in, and the rails the card
// quoted.
func TestStandingApprovedCreatesTheItemItShowed(t *testing.T) {
	store := newFakeStanding(t)
	completer := &scriptedCompleter{steps: []step{
		standCall("s1", aReminder()),
		finalText("set up"),
	}}
	agent := standingAgent(t, completer, store, func(config *Config) {
		config.Standing.DailyRailUSD = 20
	})

	events, err := agent.Submit(context.Background(), "remind me at 6 to leave")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collected := drainAnsweringStanding(t, events, func(event Event) {
		agent.ResolveStanding(event.Standing.ID, StandingAnswer{Approved: true})
	})

	if len(store.created) != 1 {
		t.Fatalf("a yes created %d items", len(store.created))
	}
	created := store.created[0]
	if created.Words != "remind me at 6 to leave" {
		t.Fatalf("the item lost the person's words: %q", created.Words)
	}
	if created.Workspace == "" {
		t.Fatal("the item has no workspace")
	}
	if created.Rails.MaxPerDay != standDefaultMaxPerDay || created.Rails.PerRunUSD != standDefaultPerRunUSD {
		t.Fatalf("rails = %+v, want the defaults", created.Rails)
	}
	if created.Origin.SessionID == "" || len(created.Origin.TurnIDs) != 1 {
		t.Fatalf("origin = %+v, want the conversation and its turn", created.Origin)
	}
	card, found := firstOfKind(collected, EventStandingProposal)
	if !found || card.Standing.WhenWords != "at 6 this evening" {
		t.Fatalf("the card did not carry the cadence in words: %+v", card.Standing)
	}
	if card.Standing.CostWords != "shares the day's $20.00 allowance" {
		t.Fatalf("card cost = %q, want the shared allowance", card.Standing.CostWords)
	}
	update, found := firstOfKind(collected, EventStandingUpdate)
	if !found || update.Standing.Update != "stood" {
		t.Fatalf("nothing reported that it now stands: %v", kinds(collected))
	}
	if output := toolOutput(t, collected, "stand"); !strings.Contains(output, "set up "+created.ID) {
		t.Fatalf("tool result = %q, want the id back", output)
	}
}

func TestStandingPersonNamedRailsSurviveAndTheCardQuotesThem(t *testing.T) {
	store := newFakeStanding(t)
	at := time.Now().Add(2 * time.Hour).Format(time.RFC3339)
	call := `{"op":"propose","words":"remind me later, spend at most a dollar",` +
		`"when":{"kind":"at","at":` + strconv.Quote(at) + `},` +
		`"does":{"kind":"say","say":"time to leave"},` +
		`"rails":{"per_run_usd":1,"max_per_day":2},` +
		`"when_words":"later","cost_words":"at most a dollar a run, twice today"}`
	completer := &scriptedCompleter{steps: []step{standCall("s1", call), finalText("set up")}}
	agent := standingAgent(t, completer, store, func(config *Config) {
		config.Standing.DailyRailUSD = 20
	})

	events, err := agent.Submit(context.Background(), "remind me later, spend at most a dollar")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collected := drainAnsweringStanding(t, events, func(event Event) {
		agent.ResolveStanding(event.Standing.ID, StandingAnswer{Approved: true})
	})

	if len(store.created) != 1 || store.created[0].Rails.PerRunUSD != 1 || store.created[0].Rails.MaxPerDay != 2 {
		t.Fatalf("created rails = %+v, want the person's 1 and 2", store.created)
	}
	card, found := firstOfKind(collected, EventStandingProposal)
	if !found || card.Standing.CostWords != "at most a dollar a run, twice today" {
		t.Fatalf("card cost = %+v, want the person's words verbatim", card.Standing)
	}
}

// A RULE STANDS UP WITH NOTHING BUT ITS SENTENCE AND ITS REACH.
//
// The three fields every other kind carries are all about waking — a cadence to
// say back, an action to be the content of a firing, a budget to bound one — and
// a hold does none of those. So the item carries no rails and no action, and the
// CARD is told nothing about cost, which is what stops it quoting the day's
// allowance under something that can never draw on it.
func TestAHoldStandsWithNoRailsNoActionAndNothingAboutMoney(t *testing.T) {
	store := newFakeStanding(t)
	call := `{"op":"propose","words":"always run the tests before you say you are done",` +
		`"when":{"kind":"hold"},"when_words":"always","title":"tests before done"}`
	completer := &scriptedCompleter{steps: []step{standCall("s1", call), finalText("set up")}}
	agent := standingAgent(t, completer, store, func(config *Config) {
		config.Standing.DailyRailUSD = 20
	})

	events, err := agent.Submit(context.Background(), "always run the tests before you say you are done")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collected := drainAnsweringStanding(t, events, func(event Event) {
		agent.ResolveStanding(event.Standing.ID, StandingAnswer{Approved: true})
	})

	if len(store.created) != 1 {
		t.Fatalf("a yes created %d items", len(store.created))
	}
	created := store.created[0]
	if created.When.Kind != standing.WhenHold {
		t.Fatalf("the item wakes on %q", created.When.Kind)
	}
	if created.Rails.PerRunUSD != 0 || created.Rails.MaxPerDay != 0 {
		t.Fatalf("a rule was given a budget: %+v", created.Rails)
	}
	if created.Does.Kind != "" {
		t.Fatalf("a rule was given something to do: %+v", created.Does)
	}
	// AND NO CADENCE, however the model said it back. "always" is not a moment,
	// and a `when ·` band under a rule would be the card reading a rhythm into it.
	if created.When.Words != "" {
		t.Fatalf("a rule was given a cadence: %q", created.When.Words)
	}
	card, found := firstOfKind(collected, EventStandingProposal)
	if !found {
		t.Fatalf("no card was drawn: %v", kinds(collected))
	}
	if card.Standing.CostWords != "" || card.Standing.WhenWords != "" {
		t.Fatalf("the card for a rule says when=%q cost=%q, wanted neither",
			card.Standing.WhenWords, card.Standing.CostWords)
	}
}

// AND AN ACTION SENT WITH A HOLD IS REFUSED RATHER THAN QUIETLY DROPPED. A model
// that asked for a rule AND a line to say meant one of the two, and standing one
// up with an action nothing will ever run would leave the person holding a card
// whose promise cannot be kept.
func TestAHoldWithSomethingToDoIsRefused(t *testing.T) {
	store := newFakeStanding(t)
	agent := standingAgent(t, &scriptedCompleter{}, store, nil)
	args := json.RawMessage(`{"op":"propose","words":"always use tabs",` +
		`"when":{"kind":"hold"},"does":{"kind":"say","say":"use tabs"}}`)

	text, isError, err := agent.standTool(context.Background(), args)
	if err != nil || !isError || !strings.Contains(text, "a hold does nothing") {
		t.Fatalf("a hold with an action = %q isError=%v err=%v", text, isError, err)
	}
	if len(store.created) != 0 {
		t.Fatal("a refused hold created an item")
	}
}

// ZERO IS NO LIMIT AND A NEGATIVE NUMBER IS STILL A REFUSAL. Zero used to be
// refused here, which made [standing.Item]'s own documented contract
// unreachable — [Agent.runStandingItem] has only ever stopped a firing when the
// rail is positive — so the one thing a person could not ask for was the
// standing order bounded by nothing but the machine's daily rail.
func TestStandingValidateRefusesANegativeRailAndNotAnExplicitZero(t *testing.T) {
	store := newFakeStanding(t)
	agent := standingAgent(t, &scriptedCompleter{}, store, nil)
	at := time.Now().Add(2 * time.Hour).Format(time.RFC3339)
	args := json.RawMessage(`{"op":"propose","words":"remind me later",` +
		`"when":{"kind":"at","at":` + strconv.Quote(at) + `},` +
		`"does":{"kind":"say","say":"time to leave"},` +
		`"rails":{"per_run_usd":-1}}`)

	text, isError, err := agent.standTool(context.Background(), args)
	if err != nil || !isError || !strings.Contains(text, "a per-run budget cannot be negative") {
		t.Fatalf("negative rail = %q isError=%v err=%v", text, isError, err)
	}
	if len(store.created) != 0 {
		t.Fatal("a negative rail created an item")
	}
}

// NO CLOCK WHILE SOMEBODY IS THERE. The card carries no deadline whatever the
// countdown setting says, because a person reading their own sentence, its
// cadence and its cost must never watch the thing end itself mid-read.
func TestStandingProposalCarriesNoClockWhenSomebodyIsWatching(t *testing.T) {
	store := newFakeStanding(t)
	completer := &scriptedCompleter{steps: []step{
		standCall("s1", aReminder()),
		finalText("set up"),
	}}
	agent := standingAgent(t, completer, store, func(config *Config) {
		// The setting a task proposal would have counted down on. A standing
		// card ignores it in both directions: no bar, and no expiry.
		config.TaskAutoApproveSeconds = 1
	})

	events, err := agent.Submit(context.Background(), "remind me at 6")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	// Answered LATE — well past the countdown that used to decline it — and it
	// still stands, which is the whole of the fix.
	collected := drainAnsweringStanding(t, events, func(event Event) {
		time.Sleep(1200 * time.Millisecond)
		agent.ResolveStanding(event.Standing.ID, StandingAnswer{Approved: true})
	})

	card, found := firstOfKind(collected, EventStandingProposal)
	if !found {
		t.Fatalf("no card was drawn: %v", kinds(collected))
	}
	if !card.Standing.Deadline.IsZero() {
		t.Fatalf("the card carried a deadline of %v", card.Standing.Deadline)
	}
	if len(store.created) != 1 {
		t.Fatalf("an answer given after the old countdown created %d items", len(store.created))
	}
}

// AND SILENCE STILL ARMS NOTHING. A turn that ended with the card still up —
// the person pressed esc, or the window went away — leaves nothing behind, and
// the model is told exactly that rather than a refusal nobody made.
func TestStandingProposalLeftUnansweredSetsNothingUp(t *testing.T) {
	store := newFakeStanding(t)
	completer := &scriptedCompleter{steps: []step{
		standCall("s1", aReminder()),
		finalText("nothing was set up"),
	}}
	agent := standingAgent(t, completer, store, nil)

	events, err := agent.Submit(context.Background(), "remind me at 6")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collected := drainAnsweringStanding(t, events, func(Event) { agent.Interrupt() })

	if len(store.created) != 0 {
		t.Fatalf("an unanswered card created %d items", len(store.created))
	}
	// The card is forgotten, so a click that arrives after the turn has gone
	// delivers into nothing rather than into a channel with no reader.
	agent.mu.Lock()
	waiting := len(agent.standingAnswers)
	agent.mu.Unlock()
	if waiting != 0 {
		t.Fatalf("%d cards are still waiting for an answer nobody will give", waiting)
	}
	if output := toolOutput(t, collected, "stand"); !strings.Contains(output, "the card was left unanswered — nothing was set up") {
		t.Fatalf("tool result = %q, want the silence said as silence", output)
	}
}

// NOBODY WATCHING CANNOT RATIFY. A headless run has no one to answer, and a
// clock that armed it anyway would be the harness agreeing on their behalf.
func TestStandingRefusesWhenNobodyIsWatching(t *testing.T) {
	store := newFakeStanding(t)
	completer := &scriptedCompleter{steps: []step{
		standCall("s1", aReminder()),
		finalText("told them"),
	}}
	agent := standingAgent(t, completer, store, func(config *Config) {
		config.AskConsent = false
		// A countdown that would have approved a task proposal outright.
		config.TaskAutoApproveSeconds = 1
	})

	events, err := agent.Submit(context.Background(), "remind me at 6")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collected := collect(t, events)

	if len(store.created) != 0 {
		t.Fatalf("an unwatched session created %d items", len(store.created))
	}
	if _, found := firstOfKind(collected, EventStandingProposal); found {
		t.Fatal("an unwatched session drew a card nobody could answer")
	}
	if output := toolOutput(t, collected, "stand"); !strings.Contains(output, "nobody is here to say yes") {
		t.Fatalf("tool result = %q, want the refusal in the person's words", output)
	}
}

// AN ERRAND SAID AT HOME IS FILED UNDER THE THING IT MADE. The exchange's
// folder moves under the item the instant something stands (tui3's
// homeexchange.go does the rename on the "stood" update), so the origin has to
// name where it LANDS — a record pointing at exchanges/ would point at a path
// that stops existing one instant later, and "why did I get this reminder?"
// would open nothing.
func TestStandingItemMadeFromHomeIsFiledUnderItself(t *testing.T) {
	store := newFakeStanding(t)
	exchange := "a1b2c3d4e5f60718"
	dir := filepath.Join(standing.ExchangesRoot(store.Root()), exchange)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("the exchange folder: %v", err)
	}
	completer := &scriptedCompleter{steps: []step{
		standCall("s1", aReminder()),
		finalText("set up"),
	}}
	agent := standingAgent(t, completer, store, func(config *Config) {
		config.SessionFile = filepath.Join(dir, "transcript.jsonl")
	})

	events, err := agent.Submit(context.Background(), "remind me at 6 to leave")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	drainAnsweringStanding(t, events, func(event Event) {
		agent.ResolveStanding(event.Standing.ID, StandingAnswer{Approved: true})
	})
	if len(store.created) != 1 {
		t.Fatalf("a yes created %d items", len(store.created))
	}
	// The item as the store now holds it, which is the one a surface reads.
	item, err := store.Get(store.created[0].ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	filed := store.ExchangeDir(item.ID)
	if item.Origin.Exchange != filed {
		t.Fatalf("origin.Exchange = %q, want %q", item.Origin.Exchange, filed)
	}
	if want := filepath.Join(filed, "transcript.jsonl"); item.Origin.Transcript != want {
		t.Fatalf("origin.Transcript = %q, want %q", item.Origin.Transcript, want)
	}
	// AND THE CONVERSATION IS STILL THE CONVERSATION. Its id is what a live
	// firing is addressed to, and moving a folder does not rename it.
	if item.Origin.SessionID != agent.id {
		t.Fatalf("origin.SessionID = %q, want the exchange's own id %q", item.Origin.SessionID, agent.id)
	}
}

// AND AN ORDINARY CONVERSATION IS NOT ONE. The only thing that makes a session
// an errand is a transcript under the standing root's exchanges/, so a session
// anywhere else keeps its own folder as its record.
func TestStandingItemMadeInAConversationKeepsItsSessionOrigin(t *testing.T) {
	store := newFakeStanding(t)
	dir := t.TempDir()
	completer := &scriptedCompleter{steps: []step{
		standCall("s1", aReminder()),
		finalText("set up"),
	}}
	agent := standingAgent(t, completer, store, func(config *Config) {
		config.SessionFile = filepath.Join(dir, "transcript.jsonl")
	})

	events, err := agent.Submit(context.Background(), "remind me at 6 to leave")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	drainAnsweringStanding(t, events, func(event Event) {
		agent.ResolveStanding(event.Standing.ID, StandingAnswer{Approved: true})
	})
	if len(store.created) != 1 {
		t.Fatalf("a yes created %d items", len(store.created))
	}
	item := store.created[0]
	if item.Origin.Exchange != "" {
		t.Fatalf("a project conversation was filed as an errand: %q", item.Origin.Exchange)
	}
	if item.Origin.Transcript != filepath.Join(dir, "transcript.jsonl") {
		t.Fatalf("origin.Transcript = %q, want the conversation's own journal", item.Origin.Transcript)
	}
}

// A STORE THAT COULD NOT WRITE IS SAID OUT LOUD. The person answered yes, so
// the one thing that must never happen is the conversation carrying on as
// though something now stands.
func TestStandingSurfacesAStoreThatWouldNotWrite(t *testing.T) {
	store := newFakeStanding(t)
	store.fail = errors.New("standing: Create is not built yet")
	completer := &scriptedCompleter{steps: []step{
		standCall("s1", aReminder()),
		finalText("it did not take"),
	}}
	agent := standingAgent(t, completer, store, nil)

	events, err := agent.Submit(context.Background(), "remind me at 6")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collected := drainAnsweringStanding(t, events, func(event Event) {
		agent.ResolveStanding(event.Standing.ID, StandingAnswer{Approved: true})
	})
	output := toolOutput(t, collected, "stand")
	if !strings.Contains(output, "nothing was set up: standing: Create is not built yet") {
		t.Fatalf("tool result = %q, want the store's own words surfaced", output)
	}
}

// "DO IT ONCE" CREATES NOTHING and tells the model to do the thing here.
func TestStandingOnceCreatesNothing(t *testing.T) {
	store := newFakeStanding(t)
	completer := &scriptedCompleter{steps: []step{
		standCall("s1", aReminder()),
		finalText("doing it now"),
	}}
	agent := standingAgent(t, completer, store, nil)

	events, err := agent.Submit(context.Background(), "remind me at 6")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collected := drainAnsweringStanding(t, events, func(event Event) {
		agent.ResolveStanding(event.Standing.ID, StandingAnswer{Once: true})
	})
	if len(store.created) != 0 {
		t.Fatalf("a once answer created %d items", len(store.created))
	}
	if output := toolOutput(t, collected, "stand"); !strings.Contains(output, "do it once, now, as an ordinary turn") {
		t.Fatalf("tool result = %q", output)
	}
}

// A CHANGE GOES BACK TO THE MODEL IN THE PERSON'S OWN WORDS, and nothing is
// created until they see it again.
func TestStandingChangeSendsTheWordsBack(t *testing.T) {
	store := newFakeStanding(t)
	completer := &scriptedCompleter{steps: []step{
		standCall("s1", aReminder()),
		finalText("proposing again"),
	}}
	agent := standingAgent(t, completer, store, nil)

	events, err := agent.Submit(context.Background(), "remind me at 6")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collected := drainAnsweringStanding(t, events, func(event Event) {
		agent.ResolveStanding(event.Standing.ID, StandingAnswer{Change: "make it 8"})
	})
	if len(store.created) != 0 {
		t.Fatal("a change created an item")
	}
	if output := toolOutput(t, collected, "stand"); !strings.Contains(output, "the person changed it: make it 8") {
		t.Fatalf("tool result = %q", output)
	}
}

// ── stopping one ────────────────────────────────────────────────────────────

// PEOPLE NAME THESE BY WHAT THEY SAID, never by an id, so their own words are a
// first-class handle — and stopping one is permanent and says so.
func TestStandingStopsByThePersonsOwnWords(t *testing.T) {
	store := newFakeStanding(t)
	agent := standingAgent(t, &scriptedCompleter{}, store, nil)
	item, err := store.Create(standing.Item{
		Words:     "tell me when CI on main goes red",
		Workspace: agent.standingWorkspace(),
		When:      standing.When{Kind: standing.WhenEvery, Every: "10m", Words: "every ten minutes"},
		Does:      standing.Action{Kind: standing.ActionSay, Say: "CI is red"},
		Rails:     standing.Rails{PerRunUSD: 0.15, MaxPerDay: 10},
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	text, isError, err := agent.standTool(context.Background(),
		json.RawMessage(`{"op":"stop","words":"CI"}`))
	if err != nil || isError {
		t.Fatalf("stop = %q isError=%v err=%v", text, isError, err)
	}
	if !strings.Contains(text, "stopped: tell me when CI on main goes red") {
		t.Fatalf("stop said %q", text)
	}
	stopped, _ := store.Get(item.ID)
	if stopped.Status != standing.StatusRetired || stopped.RetiredWhy != "stopped by you" {
		t.Fatalf("stopped item = %+v", stopped)
	}
}

// AN AMBIGUOUS NAME IS NOT GUESSED AT. Pausing the wrong watch is a silence
// nobody notices until it matters, so the candidates come back instead.
func TestStandingAmbiguousWordsAnswerWithTheCandidates(t *testing.T) {
	store := newFakeStanding(t)
	agent := standingAgent(t, &scriptedCompleter{}, store, nil)
	for _, words := range []string{"tell me when CI goes red", "tell me when CI goes green"} {
		if _, err := store.Create(standing.Item{
			Words:     words,
			Workspace: agent.standingWorkspace(),
			When:      standing.When{Kind: standing.WhenEvery, Every: "10m"},
			Does:      standing.Action{Kind: standing.ActionSay, Say: "…"},
			Rails:     standing.Rails{PerRunUSD: 0.15, MaxPerDay: 10},
		}); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	text, isError, _ := agent.standTool(context.Background(),
		json.RawMessage(`{"op":"pause","words":"CI"}`))
	if !isError || !strings.Contains(text, "matches more than one") {
		t.Fatalf("pause = %q isError=%v", text, isError)
	}
	if len(store.saved) != 0 {
		t.Fatal("an ambiguous name changed something anyway")
	}
}

// The list is what stands HERE, in a person's own vocabulary.
func TestStandingListSpeaksThePersonsWords(t *testing.T) {
	store := newFakeStanding(t)
	agent := standingAgent(t, &scriptedCompleter{}, store, nil)
	// The clock line every result carries is stripped here rather than asserted:
	// which line this op's answer opens with is what this test is about, and
	// TestEveryStandResultEndsWithTheTime is what the tail is about.
	if text, _, _ := agent.standTool(context.Background(), json.RawMessage(`{"op":"list"}`)); standWithoutNow(text) != "Nothing stands in this project yet." {
		t.Fatalf("an empty list said %q", text)
	}
	if _, err := store.Create(standing.Item{
		Words:     "every Monday draft the weekly update",
		Workspace: agent.standingWorkspace(),
		When:      standing.When{Kind: standing.WhenEvery, Every: "24h", Words: "Mondays at 9am"},
		Does:      standing.Action{Kind: standing.ActionTask, Brief: "draft it"},
		Rails:     standing.Rails{PerRunUSD: 0.15, MaxPerDay: 10},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	text, isError, _ := agent.standTool(context.Background(), json.RawMessage(`{"op":"list"}`))
	if isError {
		t.Fatalf("list failed: %q", text)
	}
	for _, want := range []string{"every Monday draft the weekly update", "Mondays at 9am", "◦"} {
		if !strings.Contains(text, want) {
			t.Fatalf("list = %q, want %q in it", text, want)
		}
	}
}

// ── background checks, on by default, said once ─────────────────────────────

// fakeWatch is this machine's scheduler, stood in for. Nothing in these tests
// goes near launchd.
type fakeWatch struct {
	installs   int
	uninstalls int
	fail       error
	installed  bool
}

func (w *fakeWatch) Install(context.Context) error {
	w.installs++
	if w.fail != nil {
		return w.fail
	}
	w.installed = true
	return nil
}

func (w *fakeWatch) Uninstall(context.Context) error {
	w.uninstalls++
	w.installed = false
	return nil
}

func (w *fakeWatch) Status() (standing.WatchStatus, error) {
	return standing.WatchStatus{Installed: w.installed}, nil
}

// standRatify runs one whole proposal through to a yes, on the store and timer
// it is handed.
func standRatify(t *testing.T, store *fakeStanding, watch standing.Watch, profileDir string) []Event {
	t.Helper()
	completer := &scriptedCompleter{steps: []step{
		standCall("s1", aReminder()),
		finalText("set up"),
	}}
	agent := standingAgent(t, completer, store, func(config *Config) {
		config.Standing.Watch = watch
		config.ProfileDir = profileDir
	})
	events, err := agent.Submit(context.Background(), "remind me at 6 to leave")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	return drainAnsweringStanding(t, events, func(event Event) {
		agent.ResolveStanding(event.Standing.ID, StandingAnswer{Approved: true})
	})
}

// backgroundLine is the sentence one of these turns said about the machine, or
// empty if it said nothing.
func backgroundLine(events []Event) string {
	for _, event := range events {
		if event.Kind == EventStandingUpdate && event.Standing != nil &&
			event.Standing.Update == standingBackgroundUpdate {
			return event.Standing.Text
		}
	}
	return ""
}

// THE FIRST THING THAT EVER STANDS TURNS THE BACKGROUND CHECKS ON, WITHOUT
// ASKING, AND SAYS SO ONCE. There used to be a question here and it had one
// sensible answer; what is owed instead is the fact and the switch, in one dim
// line the person cannot miss and never has to read twice.
func TestTheFirstThingThatStandsTurnsBackgroundChecksOnAndSaysSo(t *testing.T) {
	store := newFakeStanding(t)
	watch := &fakeWatch{}
	events := standRatify(t, store, watch, t.TempDir())

	if watch.installs != 1 {
		t.Fatalf("the timer was installed %d times, want exactly 1", watch.installs)
	}
	if line := backgroundLine(events); line != standingBackgroundLine {
		t.Fatalf("the line said %q, want %q", line, standingBackgroundLine)
	}
	// AND NOBODY WAS ASKED ANYTHING. A card carrying a second question is the
	// thing this replaced.
	for _, event := range events {
		if event.Kind == EventStandingProposal && strings.Contains(event.Standing.Text, "no window") {
			t.Fatalf("the card asked about the window: %+v", event.Standing)
		}
	}
	// The marker says it was TOLD rather than asked, journaled before the host
	// was touched.
	marker, told := standingWatchAsked(store.Root())
	if !told || !marker.Told || !marker.Answer {
		t.Fatalf("marker = %+v told=%v", marker, told)
	}
}

// AND IT IS SAID ONCE, EVER. The second thing that stands installs nothing and
// says nothing: the machine is already checking, and a line repeated every time
// is a line nobody reads.
func TestTheBackgroundLineIsSaidOnceEver(t *testing.T) {
	store := newFakeStanding(t)
	watch := &fakeWatch{}
	standRatify(t, store, watch, t.TempDir())
	events := standRatify(t, store, watch, t.TempDir())

	if watch.installs != 1 {
		t.Fatalf("the timer was installed %d times across two items", watch.installs)
	}
	if line := backgroundLine(events); line != "" {
		t.Fatalf("the second item said it again: %q", line)
	}
}

// AN INSTALL THAT DID NOT TAKE IS SAID HONESTLY. The person is about to walk
// away from a machine they think is watching something for them.
func TestABackgroundInstallThatFailedSaysSo(t *testing.T) {
	store := newFakeStanding(t)
	watch := &fakeWatch{fail: errors.New("launchctl bootstrap:\nDomain does not support\nspecified action")}
	events := standRatify(t, store, watch, t.TempDir())

	line := backgroundLine(events)
	if !strings.HasPrefix(line, standingBackgroundFailed) || !strings.HasSuffix(line, standingBackgroundWhere) {
		t.Fatalf("a failed install said %q", line)
	}
	if strings.Contains(line, "\n") {
		t.Fatalf("the reason came through in more than one line: %q", line)
	}
	// It is still remembered, so the sentence is not said again tomorrow.
	if _, told := standingWatchAsked(store.Root()); !told {
		t.Fatal("a failed install will be attempted and announced all over again")
	}
}

// THE ROW OUTRANKS THE DEFAULT. Somebody who turned background checks off
// before anything ever stood has answered this already, and installing over
// that answer would make the switch a suggestion.
func TestNothingIsInstalledWhenTheRowIsAlreadyOff(t *testing.T) {
	store := newFakeStanding(t)
	watch := &fakeWatch{}
	profile := t.TempDir()
	row, ok := config.NewSettings(config.SettingsOptions{
		ProfileDir: profile, BackgroundChecks: watch,
	}).Row(config.KeyStandingBackground)
	if !ok {
		t.Fatal("the registry has no background checks row to turn")
	}
	if err := row.Apply(config.BackgroundOff); err != nil {
		t.Fatalf("turning the row off: %v", err)
	}
	watch.uninstalls = 0
	events := standRatify(t, store, watch, profile)

	if watch.installs != 0 {
		t.Fatalf("a timer was installed over a row the person turned off")
	}
	if line := backgroundLine(events); line != "" {
		t.Fatalf("something was said about a switch the person had already thrown: %q", line)
	}
}

// THE MARKER IS JOURNALED BEFORE THE HOST IS TOUCHED, and it is the whole
// memory of the one line: a marker beside the items.
func TestStandingWatchOfferIsRememberedOnceEver(t *testing.T) {
	root := t.TempDir()
	if _, asked := standingWatchAsked(root); asked {
		t.Fatal("a fresh store claims somebody was already asked")
	}
	standingRememberWatch(root, true)
	marker, asked := standingWatchAsked(root)
	if !asked || !marker.Answer || marker.At.IsZero() {
		t.Fatalf("marker = %+v asked=%v", marker, asked)
	}
	raw, err := os.ReadFile(filepath.Join(root, standingWatchOffer))
	if err != nil {
		t.Fatalf("the marker is not on disk: %v", err)
	}
	if !strings.Contains(string(raw), `"asked":true`) {
		t.Fatalf("marker file = %s", raw)
	}
}

// ── delivering ──────────────────────────────────────────────────────────────

// AN OPEN WINDOW HEARS IT IN THE ROOM. The line rides the steering lane a task
// landing and a watch delta ride, so the model answers it rather than banking
// it for whenever somebody next types.
func TestStandingSayReachesALiveConversation(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	registerLiveSession(agent)
	t.Cleanup(func() { forgetLiveSession(agent) })

	runner := &standingRunner{}
	outcome, err := runner.Say(context.Background(), standing.Item{
		Words:  "tell me when CI goes red",
		Origin: standing.Origin{SessionID: agent.id},
	}, "the last run on main failed")
	if err != nil || outcome.Kind != "said" {
		t.Fatalf("Say = %+v err=%v", outcome, err)
	}
	agent.mu.Lock()
	queued := len(agent.steering)
	line := ""
	if queued > 0 {
		line = agent.steering[queued-1].text()
	}
	agent.mu.Unlock()
	if queued != 1 {
		t.Fatalf("the live conversation has %d queued lines", queued)
	}
	// The news, whole, inside the framing that keeps the model from reading it
	// as a request ([TestTheSteeringLineReadsAsNewsAndNotAsARequest]).
	if !strings.Contains(line, "◦ tell me when CI goes red: the last run on main failed") {
		t.Fatalf("steering line = %q", line)
	}
}

// A WINDOW THAT IS NOT OPEN HERE GETS THE INBOX, and the inbox is the session
// folder the transcript sits in. The append itself is internal/standing's
// ([standing.Deliver]); what this package answers for is WHERE.
func TestStandingSayWithNoLiveWindowAddressesTheSessionFolder(t *testing.T) {
	dir := t.TempDir()
	item := standing.Item{
		Words:  "tell me when CI goes red",
		Origin: standing.Origin{SessionID: "nobody-has-this-open", Transcript: filepath.Join(dir, "transcript.jsonl")},
	}
	if got := standingSessionDir(item); got != dir {
		t.Fatalf("the inbox would go to %q, want %q", got, dir)
	}
	if got := standing.InboxPath(standingSessionDir(item)); got != filepath.Join(dir, "inbox.jsonl") {
		t.Fatalf("inbox path = %q", got)
	}
	runner := &standingRunner{}
	outcome, err := runner.Say(context.Background(), item, "the last run on main failed")
	if err != nil || outcome.Kind != "said" {
		t.Fatalf("Say = %+v err=%v", outcome, err)
	}
}

// ── the fold ────────────────────────────────────────────────────────────────

// ONE NOTE AND NEVER A NOTE PER FIRING. Somebody who was away for a week comes
// back to a conversation, not to a mailbox.
func TestStandingAwayNoteIsOneFold(t *testing.T) {
	at := time.Date(2026, 8, 19, 9, 0, 0, 0, time.Local)
	note := standingAwayNote([]standing.Note{
		{At: at, ItemID: "a", Words: "keep main green", Kind: "landed", Text: "the fix landed", Run: "/runs/1"},
		{At: at.Add(time.Hour), ItemID: "b", Words: "tell me when CI goes red", Kind: "said", Text: "the last run failed"},
	})
	if !strings.HasPrefix(note, "while you were away") {
		t.Fatalf("the fold does not open with the fold: %q", note)
	}
	lines := strings.Split(note, "\n")
	if len(lines) != 3 {
		t.Fatalf("two notes made %d lines: %q", len(lines)-1, note)
	}
	if !strings.Contains(lines[1], "keep main green · the fix landed · /runs/1") {
		t.Fatalf("first line = %q", lines[1])
	}
	if !strings.Contains(lines[2], "tell me when CI goes red · the last run failed") {
		t.Fatalf("second line = %q", lines[2])
	}
}

// An empty inbox says nothing at all. Twenty minutes away is nothing to report,
// which is the ambient side's whole posture.
func TestStandingDrainSaysNothingWhenNothingHappened(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.Place = Place{Dir: t.TempDir()}
	})
	agent.mu.Lock()
	agent.steering = nil
	agent.mu.Unlock()
	agent.drainStandingInbox()
	agent.mu.Lock()
	queued := len(agent.steering)
	agent.mu.Unlock()
	if queued != 0 {
		t.Fatalf("an empty inbox queued %d lines", queued)
	}
}

// ── the sentinel's verdict ──────────────────────────────────────────────────

// THE FIRST WORD IS THE WHOLE VERDICT. A model that explains itself instead of
// answering has not answered a binary contract, and reading a "yes" out of the
// middle of a paragraph is how a watch starts firing on "no, this is not yes".
func TestStandingVerdictReadsOnlyTheFirstWord(t *testing.T) {
	for _, probe := range []struct {
		reply string
		yes   bool
		line  string
	}{
		{"yes — the last run on main failed", true, "the last run on main failed"},
		{"Yes: nothing has changed since yesterday", true, "nothing has changed since yesterday"},
		{"no, everything is green", false, "everything is green"},
		{"NO", false, ""},
		{"yes", true, ""},
		{"It looks like yes, CI is red", false, "there was no clear answer, so nothing was said"},
		{"", false, "there was no clear answer, so nothing was said"},
		{"  yes\nthe cert has 9 days left", true, "the cert has 9 days left"},
	} {
		yes, line := standingVerdict(probe.reply)
		if yes != probe.yes || line != probe.line {
			t.Errorf("%q → (%v, %q), want (%v, %q)", probe.reply, yes, line, probe.yes, probe.line)
		}
	}
}

// The evidence is folded in where the item asked for it, and appended under a
// heading where it did not — a brief written without the placeholder is still a
// brief about something the look found.
func TestStandingEvidenceIsFoldedIn(t *testing.T) {
	if got := standingEvidence("CI is red: {{evidence}}", "run 12 failed"); got != "CI is red: run 12 failed" {
		t.Fatalf("placeholder = %q", got)
	}
	got := standingEvidence("look into it", "run 12 failed")
	if !strings.Contains(got, "WHAT THE CHECK FOUND:\nrun 12 failed") {
		t.Fatalf("appended = %q", got)
	}
	if got := standingEvidence("look into it", ""); got != "look into it" {
		t.Fatalf("no evidence = %q", got)
	}
}

// A probe is clipped from the TAIL: a command's news is at the end of its
// output, and clipping from the front hands the judgment the banner.
func TestStandingProbeIsClippedFromTheTail(t *testing.T) {
	text := strings.Repeat("noise\n", 100) + "the last line"
	got := standingTail(text, 40)
	if !strings.HasSuffix(got, "the last line") {
		t.Fatalf("the tail lost the tail: %q", got)
	}
	if len(got) > 60 {
		t.Fatalf("the clip kept %d bytes", len(got))
	}
	if got := standingTail("short", 40); got != "short" {
		t.Fatalf("a short output was changed: %q", got)
	}
}

// ── saying when: the stamp, and the distance from now ───────────────────────

// THE MODEL DOES NOT DO THE ARITHMETIC AND DOES NOT READ A CLOCK. Written from
// a person's own transcripts: every "remind me in 2 mins" opened with a
// `bash date +"%Y-%m-%dT%H:%M:%S%z"`. `when.in` is the answer — aforge resolves
// the duration against the clock at the instant of the call — and the moment it
// landed on is SAID BACK, on the card and in the tool result, so nobody has to
// take it on trust.
func TestStandInResolvesTheDurationAndSaysTheMomentBack(t *testing.T) {
	store := newFakeStanding(t)
	body := `{"op":"propose","words":"remind me in 2 mins to eat medicines",` +
		`"when":{"kind":"at","in":"2m"},` +
		`"does":{"kind":"say","say":"Time to eat your medicines."},` +
		`"cost_words":"nothing to speak of — one line, once"}`
	completer := &scriptedCompleter{steps: []step{standCall("s1", body), finalText("set up")}}
	agent := standingAgent(t, completer, store, nil)

	before := time.Now()
	events, err := agent.Submit(context.Background(), "remind me in 2 mins to eat medicines")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	var card StandingNotice
	collected := drainAnsweringStanding(t, events, func(event Event) {
		card = *event.Standing
		agent.ResolveStanding(event.Standing.ID, StandingAnswer{Approved: true})
	})
	after := time.Now()

	if len(store.created) != 1 {
		t.Fatalf("a yes created %d items", len(store.created))
	}
	item := store.created[0]
	if item.When.Kind != standing.WhenAt {
		t.Fatalf("when.kind = %q, want at", item.When.Kind)
	}
	// Two minutes from the moment of the call, with the whole turn's own
	// duration as the slack — anything outside that is arithmetic off a stale
	// stamp rather than off the clock.
	if item.When.At.Before(before.Add(2*time.Minute)) || item.When.At.After(after.Add(2*time.Minute)) {
		t.Fatalf("when.at = %s, want two minutes after a moment between %s and %s",
			item.When.At.Format(time.RFC3339), before.Format(time.RFC3339), after.Format(time.RFC3339))
	}
	// AND IT IS SAID BACK, in both places a person and the model read.
	want := "in 2 minutes — " + item.When.At.Format("15:04")
	if item.When.Words != want {
		t.Fatalf("the cadence in words = %q, want %q", item.When.Words, want)
	}
	if card.WhenWords != want {
		t.Fatalf("the card said %q, want %q", card.WhenWords, want)
	}
	if output := toolOutput(t, collected, "stand"); !strings.Contains(output, want) {
		t.Fatalf("the tool result = %q, want %q in it", output, want)
	}
}

// The model's own when_words still wins when it sent any: the engine's echo is
// a fallback for the case there is none, and never an override of the sentence
// the person's cadence was said back in.
func TestStandInLeavesTheModelsOwnWordsAlone(t *testing.T) {
	store := newFakeStanding(t)
	body := `{"op":"propose","words":"remind me in a couple of minutes",` +
		`"when":{"kind":"at","in":"2m"},"when_words":"in a couple of minutes",` +
		`"does":{"kind":"say","say":"here you go"},"cost_words":"a cent"}`
	completer := &scriptedCompleter{steps: []step{standCall("s1", body), finalText("set up")}}
	agent := standingAgent(t, completer, store, nil)
	events, err := agent.Submit(context.Background(), "remind me in a couple of minutes")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	drainAnsweringStanding(t, events, func(event Event) {
		agent.ResolveStanding(event.Standing.ID, StandingAnswer{Approved: true})
	})
	if len(store.created) != 1 {
		t.Fatalf("a yes created %d items", len(store.created))
	}
	if got := store.created[0].When.Words; got != "in a couple of minutes" {
		t.Fatalf("the cadence in words = %q, want the model's own", got)
	}
}

// TWO ANSWERS TO ONE QUESTION ARE REFUSED, and a duration that is not one is
// refused in the grammar the model has to fix the call in.
func TestStandingAtMomentReadsAStampOrADistanceAndNeverBoth(t *testing.T) {
	now := time.Date(2026, 8, 21, 6, 52, 0, 0, time.Local)
	for _, probe := range []struct {
		name    string
		at, in  string
		moment  time.Time
		echo    string
		problem string
	}{
		{name: "a stamp", at: "2026-08-21T18:00:00Z", moment: time.Date(2026, 8, 21, 18, 0, 0, 0, time.UTC)},
		{name: "two minutes", in: "2m", moment: now.Add(2 * time.Minute), echo: "in 2 minutes — 06:54"},
		{name: "one minute", in: "60s", moment: now.Add(time.Minute), echo: "in 1 minute — 06:53"},
		{name: "ninety seconds", in: "90s", moment: now.Add(90 * time.Second), echo: "in 2 minutes — 06:53"},
		{name: "under a minute", in: "30s", moment: now.Add(30 * time.Second), echo: "in 30 seconds — 06:52"},
		{name: "an hour and a half", in: "1h30m", moment: now.Add(90 * time.Minute), echo: "in 1 hour 30 minutes — 08:22"},
		{name: "both", at: "2026-08-21T18:00:00Z", in: "2m", problem: "two answers to one question"},
		{name: "backwards", in: "-2m", problem: "a distance into the future"},
		{name: "not a duration", in: "two minutes", problem: "when.in is a duration"},
		{name: "neither", problem: "when.at is required"},
	} {
		t.Run(probe.name, func(t *testing.T) {
			moment, echo, problem := standingAtMoment(probe.at, probe.in, now)
			if probe.problem != "" {
				if !strings.Contains(problem, probe.problem) {
					t.Fatalf("problem = %q, want %q in it", problem, probe.problem)
				}
				return
			}
			if problem != "" {
				t.Fatalf("problem = %q, want none", problem)
			}
			if !moment.Equal(probe.moment) {
				t.Fatalf("moment = %s, want %s", moment.Format(time.RFC3339), probe.moment.Format(time.RFC3339))
			}
			if echo != probe.echo {
				t.Fatalf("echo = %q, want %q", echo, probe.echo)
			}
		})
	}
}

// ── where a firing lands ────────────────────────────────────────────────────

// standingLiveAgent is one open conversation of a named workspace, with a
// folder of its own so [liveSessionTouched] has a meta.json to read.
func standingLiveAgent(t *testing.T, workspace string, mutate func(*Config)) *Agent {
	t.Helper()
	dir := t.TempDir()
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.Workspace = workspace
		config.Place = Place{Dir: dir, Workspace: workspace}
		config.SessionFile = config.Place.Transcript()
		if mutate != nil {
			mutate(config)
		}
	})
	// IT IS OPEN BUT IT MAY NOT SPEAK. A steered line wakes an idle session and
	// the turn it starts is what CONSUMES the queue ([Agent.wakeLocked]) — which
	// is the product working and a race for a test that wants to read the lane.
	// Holding the session unopened is the same posture recovery uses: the lines
	// queue, and nothing answers them.
	agent.mu.Lock()
	agent.opened = false
	agent.mu.Unlock()
	return agent
}

// standingQueued is what one conversation has waiting on its steering lane.
func standingQueued(agent *Agent) []string {
	agent.mu.Lock()
	defer agent.mu.Unlock()
	lines := make([]string, 0, len(agent.steering))
	for _, note := range agent.steering {
		lines = append(lines, note.text())
	}
	return lines
}

// AN ERRAND IS NEVER STEERED INTO, AND THE PERSON IS TOLD ANYWAY.
//
// This is the firing that wrote the rule. A reminder made from home's `ask
// here` box fired, found the exchange agent still open in the pane, and put
// "◦ remind me in 1 min to eat medicines: Time to eat your medicines." into the
// exchange's transcript — while the person sat in an ordinary conversation in
// the same window and was never told.
func TestAFiringNeverStreersIntoAnErrandAndReachesTheRoomInstead(t *testing.T) {
	workspace := t.TempDir()
	errand := standingLiveAgent(t, workspace, func(config *Config) { config.Errand = true })
	room := standingLiveAgent(t, workspace, nil)

	runner := &standingRunner{root: t.TempDir()}
	item := standing.Item{
		Words:     "remind me in 1 min to eat medicines",
		Workspace: workspace,
		Origin: standing.Origin{
			SessionID:  errand.id,
			Exchange:   filepath.Join(runner.root, "item1", "exchange"),
			Transcript: filepath.Join(runner.root, "item1", "exchange", "transcript.jsonl"),
		},
	}
	if _, err := runner.Say(context.Background(), item, "Time to eat your medicines."); err != nil {
		t.Fatalf("Say: %v", err)
	}
	if queued := standingQueued(errand); len(queued) != 0 {
		t.Fatalf("the exchange was steered into: %q", queued)
	}
	queued := standingQueued(room)
	if len(queued) != 1 {
		t.Fatalf("the conversation the person is in has %d queued lines", len(queued))
	}
	// The news itself, whole. What is around it is the framing that stops the
	// model re-proposing the item it just fired
	// ([TestTheSteeringLineReadsAsNewsAndNotAsARequest]).
	if want := "◦ remind me in 1 min to eat medicines: Time to eat your medicines."; !strings.Contains(queued[0], want) {
		t.Fatalf("steering line = %q, want it to carry %q", queued[0], want)
	}
}

// A CLOSED ORIGIN STILL REACHES THE WINDOW SOMEBODY IS SITTING IN, as long as
// it is the same project: the file is the last resort and never the first.
func TestAFiringWhoseOriginIsClosedReachesAnotherWindowOfTheProject(t *testing.T) {
	workspace := t.TempDir()
	room := standingLiveAgent(t, workspace, nil)
	elsewhere := standingLiveAgent(t, t.TempDir(), nil)

	runner := &standingRunner{root: t.TempDir()}
	dir := t.TempDir()
	item := standing.Item{
		Words:     "tell me when CI goes red",
		Workspace: workspace,
		Origin:    standing.Origin{SessionID: "nobody-has-this-open", Transcript: filepath.Join(dir, "transcript.jsonl")},
	}
	if _, err := runner.Say(context.Background(), item, "the last run on main failed"); err != nil {
		t.Fatalf("Say: %v", err)
	}
	if queued := standingQueued(room); len(queued) != 1 {
		t.Fatalf("the open window of this project has %d queued lines", len(queued))
	}
	if queued := standingQueued(elsewhere); len(queued) != 0 {
		t.Fatalf("a window of another project was steered into: %q", queued)
	}
	// AND NOTHING WAS FILED. A line delivered into a room is not also a line
	// waiting in a fold tomorrow.
	if _, err := os.Stat(standing.InboxPath(dir)); !os.IsNotExist(err) {
		t.Fatalf("the origin's inbox was written as well: %v", err)
	}
}

// THE WINDOW THE PERSON IS ACTUALLY IN, and there are two readings of that: the
// newest window is the one they just opened, and the one they last SPOKE in
// beats it ([Meta.LastUserAt], never a file mtime).
func TestAFiringPrefersTheWindowThePersonLastTouched(t *testing.T) {
	workspace := t.TempDir()
	older := standingLiveAgent(t, workspace, nil)
	newer := standingLiveAgent(t, workspace, nil)

	runner := &standingRunner{root: t.TempDir()}
	item := standing.Item{
		Words:     "remind me at 6 to leave",
		Workspace: workspace,
		Origin:    standing.Origin{SessionID: "closed", Transcript: filepath.Join(t.TempDir(), "transcript.jsonl")},
	}
	if _, err := runner.Say(context.Background(), item, "time to leave"); err != nil {
		t.Fatalf("Say: %v", err)
	}
	if len(standingQueued(newer)) != 1 || len(standingQueued(older)) != 0 {
		t.Fatalf("with nothing typed the newest window should have it: older=%d newer=%d",
			len(standingQueued(older)), len(standingQueued(newer)))
	}

	// Now the person speaks in the older one. That is where they are.
	if err := SaveMeta(older.config.Place.Dir, Meta{
		ID:         older.id,
		Workspace:  workspace,
		LastUserAt: time.Now().Add(time.Minute),
	}); err != nil {
		t.Fatalf("SaveMeta: %v", err)
	}
	if _, err := runner.Say(context.Background(), item, "time to leave"); err != nil {
		t.Fatalf("Say: %v", err)
	}
	if len(standingQueued(older)) != 1 {
		t.Fatalf("the window they last spoke in has %d queued lines", len(standingQueued(older)))
	}
	if len(standingQueued(newer)) != 1 {
		t.Fatalf("the second firing went to the wrong window: newer has %d", len(standingQueued(newer)))
	}
}

// WITH NOTHING OPEN, AN EXCHANGE'S NEWS WAITS SOMEWHERE A SCREEN READS.
//
// The exchange's own folder is a dead letter office: home lists what is under
// v3/projects, which is exactly what an errand's folder is kept out of. So the
// note goes to the PROJECT's inbox — which home draws and the next ordinary
// conversation in that project folds into its own "while you were away".
func TestAFiringFromAnExchangeWithNothingOpenWaitsOnTheProject(t *testing.T) {
	root, workspace := t.TempDir(), t.TempDir()
	exchange := filepath.Join(root, "item1", "exchange")
	item := standing.Item{
		ID:        "item1",
		Words:     "remind me in 1 min to eat medicines",
		Workspace: workspace,
		Origin: standing.Origin{
			SessionID:  "the-exchange-is-closed",
			Exchange:   exchange,
			Transcript: filepath.Join(exchange, "transcript.jsonl"),
		},
	}
	runner := &standingRunner{root: root}
	if _, err := runner.Say(context.Background(), item, "Time to eat your medicines."); err != nil {
		t.Fatalf("Say: %v", err)
	}
	if _, err := os.Stat(standing.InboxPath(exchange)); !os.IsNotExist(err) {
		t.Fatalf("the note was written into the exchange's folder, which no screen reads: %v", err)
	}
	notes := standing.PeekProjectInbox(root, workspace)
	if len(notes) != 1 {
		t.Fatalf("the project inbox holds %d notes", len(notes))
	}
	if notes[0].Words != item.Words || notes[0].Text != "Time to eat your medicines." {
		t.Fatalf("the note is %+v", notes[0])
	}
	// A PEEK LEAVES IT THERE. Home draws this on every redraw; a read that
	// emptied the file would take the fold away from the person it is for.
	if len(standing.PeekProjectInbox(root, workspace)) != 1 {
		t.Fatal("peeking at the project inbox emptied it")
	}

	// AND THE NEXT CONVERSATION IN THAT PROJECT FOLDS IT IN. The drain runs in
	// newAgent, so by the time this agent exists the note is on its lane.
	store := &fakeStanding{root: root, items: map[string]standing.Item{}}
	agent := standingAgent(t, &scriptedCompleter{}, store, func(config *Config) {
		config.Workspace = workspace
		config.Place = Place{Dir: t.TempDir(), Workspace: workspace}
		config.SessionFile = config.Place.Transcript()
	})
	queued := standingQueued(agent)
	if len(queued) != 1 {
		t.Fatalf("the new conversation queued %d notes, want one fold", len(queued))
	}
	if !strings.HasPrefix(queued[0], "while you were away") || !strings.Contains(queued[0], item.Words) {
		t.Fatalf("the fold reads %q", queued[0])
	}
	if left := standing.PeekProjectInbox(root, workspace); len(left) != 0 {
		t.Fatalf("the project inbox still holds %d notes after a conversation drained it", len(left))
	}
}

// AN ORDINARY CONVERSATION'S ITEM STILL USES THE CONVERSATION'S OWN INBOX. It
// is a row on home and a chat somebody reopens, so its news belongs to it and
// not to the project.
func TestAnOrdinaryOriginStillWaitsInItsOwnConversation(t *testing.T) {
	root, workspace := t.TempDir(), t.TempDir()
	dir := t.TempDir()
	item := standing.Item{
		ID:        "item2",
		Words:     "tell me when CI goes red",
		Workspace: workspace,
		Origin:    standing.Origin{SessionID: "closed", Transcript: filepath.Join(dir, "transcript.jsonl")},
	}
	runner := &standingRunner{root: root}
	if _, err := runner.Say(context.Background(), item, "the last run on main failed"); err != nil {
		t.Fatalf("Say: %v", err)
	}
	notes, err := standing.Drain(dir)
	if err != nil || len(notes) != 1 {
		t.Fatalf("the session inbox holds %d notes (err %v)", len(notes), err)
	}
	if got := standing.PeekProjectInbox(root, workspace); len(got) != 0 {
		t.Fatalf("the project inbox was written too: %+v", got)
	}
}

// ── a moment that has already passed ────────────────────────────────────────

// standWithoutNow is one `stand` result with the clock line every one of them
// ends with taken off, so a test about WHAT AN OP SAID is not also a test about
// the tail ([standingWithNow]).
func standWithoutNow(text string) string {
	trimmed := strings.TrimRight(text, "\n")
	if at := strings.LastIndex(trimmed, "\nnow: "); at >= 0 {
		return trimmed[:at]
	}
	if strings.HasPrefix(trimmed, "now: ") {
		return ""
	}
	return trimmed
}

// A REMINDER CAN NEVER BE SET FOR A MOMENT THAT HAS PASSED, and the refusal
// carries the current time so the model can work the stamp out again without
// spending another call to find out what now is.
//
// The defect this pins: a session two hours old proposed 05:42 at 07:34, the
// engine took it, and the person answered a card for a thing that could never
// fire.
func TestAMomentAlreadyPassedIsRefusedWithTheTimeItIsNow(t *testing.T) {
	now := time.Date(2026, 8, 21, 7, 34, 0, 0, time.Local)
	gone := time.Date(2026, 8, 21, 5, 42, 0, 0, time.Local)
	moment, echo, problem := standingAtMoment(gone.Format("2006-01-02T15:04:05"), "", now)
	want := "Invalid arguments: when.at " + standingClock(gone) +
		" has already passed — it is now " + standingClock(now) +
		" (Friday 2026-08-21). " +
		`For a distance from now send when.in ("1m"); for a clock time compute it from now.`
	if problem != want {
		t.Fatalf("the refusal is\n%q\nwant\n%q", problem, want)
	}
	if !moment.IsZero() || echo != "" {
		t.Fatalf("a refused moment answered %s / %q", moment.Format(time.RFC3339), echo)
	}
}

// THE GRACE IS FOR ARITHMETIC AND NOT FOR MISTAKES. A stamp a few seconds
// behind the clock was right when it was worked out a beat ago; half a minute
// is where that stops being the honest reading.
func TestTheGraceTakesASecondsOldStampAndNotAnOlderOne(t *testing.T) {
	now := time.Date(2026, 8, 21, 7, 34, 0, 0, time.Local)
	for _, probe := range []struct {
		name    string
		behind  time.Duration
		refused bool
	}{
		{name: "right now", behind: 0},
		{name: "a beat ago", behind: 5 * time.Second},
		{name: "at the edge", behind: standingPastGrace},
		{name: "past the edge", behind: standingPastGrace + time.Second, refused: true},
		{name: "two hours ago", behind: 2 * time.Hour, refused: true},
	} {
		t.Run(probe.name, func(t *testing.T) {
			at := now.Add(-probe.behind)
			moment, _, problem := standingAtMoment(at.Format("2006-01-02T15:04:05"), "", now)
			if probe.refused {
				if !strings.Contains(problem, "has already passed") {
					t.Fatalf("%s behind was taken: problem = %q", probe.behind, problem)
				}
				return
			}
			if problem != "" {
				t.Fatalf("%s behind was refused: %q", probe.behind, problem)
			}
			if !moment.Equal(at) {
				t.Fatalf("moment = %s, want %s", moment.Format(time.RFC3339), at.Format(time.RFC3339))
			}
		})
	}
}

// AND THE DISTANCE STILL WORKS, which is the answer the refusal points at: "in
// 1 minute" is resolved against the clock at the instant of the call and can
// never land behind it.
func TestADistanceFromNowIsUntouchedByTheRefusal(t *testing.T) {
	now := time.Date(2026, 8, 21, 7, 34, 0, 0, time.Local)
	moment, echo, problem := standingAtMoment("", "1m", now)
	if problem != "" {
		t.Fatalf("when.in was refused: %q", problem)
	}
	if !moment.Equal(now.Add(time.Minute)) {
		t.Fatalf("moment = %s, want one minute on", moment.Format(time.RFC3339))
	}
	if echo != "in 1 minute — 07:35" {
		t.Fatalf("echo = %q", echo)
	}
}

// AN EXPIRY ALREADY GONE WOULD RETIRE THE ITEM BEFORE IT EVER FIRED, so it is
// refused in the same grammar and with the same time on it.
func TestAnExpiryAlreadyPassedIsRefused(t *testing.T) {
	now := time.Date(2026, 8, 21, 7, 34, 0, 0, time.Local)
	gone := time.Date(2026, 8, 21, 5, 42, 0, 0, time.Local)
	var parsed standArguments
	parsed.Rails.Expires = gone.Format("2006-01-02T15:04:05")
	rails, problem := standingRails(parsed, standing.When{Kind: standing.WhenAt}, now)
	want := "Invalid arguments: rails.expires " + standingClock(gone) +
		" has already passed — it is now " + standingClock(now) +
		" (Friday 2026-08-21). Work it out from that time, or leave it out for something that never expires."
	if problem != want {
		t.Fatalf("the refusal is\n%q\nwant\n%q", problem, want)
	}
	if !rails.Expires.IsZero() {
		t.Fatal("a refused expiry was kept anyway")
	}
	// And one in the future is untouched.
	parsed.Rails.Expires = now.Add(time.Hour).Format("2006-01-02T15:04:05")
	if rails, problem = standingRails(parsed, standing.When{Kind: standing.WhenAt}, now); problem != "" {
		t.Fatalf("a future expiry was refused: %q", problem)
	}
	if !rails.Expires.Equal(now.Add(time.Hour)) {
		t.Fatalf("expires = %s", rails.Expires.Format(time.RFC3339))
	}
}

// AN EXPIRY THAT STANDS BEFORE THE ITEM'S OWN MOMENT IS THE SAME DEFECT, and
// until issue #188 it was accepted: the clock was the only thing an end was
// measured against, so an end still in the future but earlier than the reminder
// it was attached to stood up an item the pass could only ever retire.
//
// The receipt this pins is the one from that issue, to the second. The model
// wrote "in 1 minute — 23:11" for the words and took 23:11 for the end out of
// the same words, while the engine resolved the moment to 23:11:11 — so the
// item was born eleven seconds past its own end, ran zero times, and was retired
// as `expired` by rail one of the pass (internal/standing/tick.go).
func TestAnExpiryBeforeTheItemsOwnMomentIsRefused(t *testing.T) {
	now := time.Date(2026, 8, 31, 23, 10, 11, 0, time.Local)
	due := time.Date(2026, 8, 31, 23, 11, 11, 0, time.Local)
	end := time.Date(2026, 8, 31, 23, 11, 0, 0, time.Local)
	var parsed standArguments
	parsed.Rails.Expires = end.Format("2006-01-02T15:04:05")
	rails, problem := standingRails(parsed, standing.When{Kind: standing.WhenAt, At: due}, now)
	want := "Invalid arguments: rails.expires " + standingClockExact(end) +
		" is not after when.at " + standingClockExact(due) +
		", so it would retire before it ever fired. " +
		"Put it after that moment, or leave it out — a one-off retires as it fires and needs no end at all."
	if problem != want {
		t.Fatalf("the refusal is\n%q\nwant\n%q", problem, want)
	}
	if !rails.Expires.IsZero() {
		t.Fatal("a refused expiry was kept anyway")
	}
	// AND THE REFUSAL IS LEGIBLE, which is the whole reason it is spelled to the
	// second: at the minute both stamps read 23:11 and the sentence would say a
	// moment is not after itself.
	if strings.Count(problem, "23:11:") != 2 {
		t.Fatalf("the refusal does not name both moments to the second: %q", problem)
	}
}

// THE EDGE IS ONE CHECK PAST THE MOMENT, not the moment itself, because the
// pass that would deliver the item is the same pass that asks about the end and
// it only comes around every [standing.Interval]. An end at exactly when.at
// retires the item in the instant it becomes deliverable; an end a second later
// is the identical death arriving a minute after, since the pass that lands
// between them finds the item out of time and retires it unsaid. So everything
// short of a whole check past the firing is refused, and the two refusals are
// spelled differently — one says the end stands before the firing, the other
// names the cadence — because the fix is different: move the end, or accept
// that a near end and a near firing cannot both be had.
func TestTheEndHasToBeLaterThanTheMomentItOutlives(t *testing.T) {
	now := time.Date(2026, 8, 31, 23, 10, 11, 0, time.Local)
	due := time.Date(2026, 8, 31, 23, 11, 11, 0, time.Local)
	for _, probe := range []struct {
		name string
		end  time.Time
		says string
	}{
		{name: "eleven seconds before the moment", end: due.Add(-11 * time.Second), says: "so it would retire before it ever fired"},
		{name: "one second before the moment", end: due.Add(-time.Second), says: "so it would retire before it ever fired"},
		{name: "the moment itself", end: due, says: "so it would retire before it ever fired"},
		{name: "one second after the moment", end: due.Add(time.Second), says: "is less than one check after"},
		{name: "twenty-five seconds after the moment", end: due.Add(25 * time.Second), says: "is less than one check after"},
		{name: "a second short of a whole check", end: due.Add(standing.Interval - time.Second), says: "is less than one check after"},
		{name: "exactly one check after the moment", end: due.Add(standing.Interval)},
		{name: "an hour after the moment", end: due.Add(time.Hour)},
	} {
		t.Run(probe.name, func(t *testing.T) {
			var parsed standArguments
			parsed.Rails.Expires = probe.end.Format("2006-01-02T15:04:05")
			rails, problem := standingRails(parsed, standing.When{Kind: standing.WhenAt, At: due}, now)
			if probe.says != "" {
				if !strings.Contains(problem, probe.says) {
					t.Fatalf("an end at %s was taken: problem = %q", probe.end.Format(time.RFC3339), problem)
				}
				if !rails.Expires.IsZero() {
					t.Fatalf("a refused end at %s was kept anyway", probe.end.Format(time.RFC3339))
				}
				return
			}
			if problem != "" {
				t.Fatalf("an end at %s was refused: %q", probe.end.Format(time.RFC3339), problem)
			}
			if !rails.Expires.Equal(probe.end) {
				t.Fatalf("expires = %s, want %s", rails.Expires.Format(time.RFC3339), probe.end.Format(time.RFC3339))
			}
		})
	}
}

// A RHYTHM IS THE SAME LAW WITH THE FIRST FIRING IN PLACE OF THE MOMENT: an end
// before the rhythm's first due is a routine that could never run once. The
// refusal names it as `its first firing` rather than as a field, because there
// is no field to go and edit — the model has to move the end or widen the
// rhythm.
//
// AND THE SHAPES WHOSE WAKING IS THE WORLD'S BUSINESS ARE LEFT ALONE. A file
// watch, an idle watch and a probe may wake in a second or never, so an end in
// the future is the only thing that can honestly be asked of them; a hold never
// wakes at all and an end on one is what keeps it.
func TestARhythmsEndHasToOutliveItsFirstFiring(t *testing.T) {
	now := time.Date(2026, 8, 31, 9, 0, 0, 0, time.Local)
	var parsed standArguments
	parsed.Rails.Expires = now.Add(10 * time.Minute).Format("2006-01-02T15:04:05")
	rhythm := standing.When{Kind: standing.WhenEvery, Every: "1h"}
	_, problem := standingRails(parsed, rhythm, now)
	want := "Invalid arguments: rails.expires " + standingClockExact(now.Add(10*time.Minute)) +
		" is not after its first firing " + standingClockExact(now.Add(time.Hour)) +
		", so it would retire before it ever fired. " +
		"Put it after that moment, or leave it out for something that never expires."
	if problem != want {
		t.Fatalf("the refusal is\n%q\nwant\n%q", problem, want)
	}
	// Past the first firing it is an ordinary end.
	parsed.Rails.Expires = now.Add(90 * time.Minute).Format("2006-01-02T15:04:05")
	rails, problem := standingRails(parsed, rhythm, now)
	if problem != "" {
		t.Fatalf("an end after the first firing was refused: %q", problem)
	}
	if !rails.Expires.Equal(now.Add(90 * time.Minute)) {
		t.Fatalf("expires = %s", rails.Expires.Format(time.RFC3339))
	}
	// A watch on the world keeps whatever end it was given, since nothing here
	// knows when it would first wake.
	for _, when := range []standing.When{
		{Kind: standing.WhenFile, Glob: "*.go"},
		{Kind: standing.WhenIdle, IdleFor: time.Hour},
		{Kind: standing.WhenProbe, Probe: standing.Probe{Command: "true"}},
		{Kind: standing.WhenHold},
	} {
		parsed.Rails.Expires = now.Add(time.Minute).Format("2006-01-02T15:04:05")
		rails, problem := standingRails(parsed, when, now)
		if problem != "" {
			t.Fatalf("a %s was refused an end a minute out: %q", when.Kind, problem)
		}
		if !rails.Expires.Equal(now.Add(time.Minute)) {
			t.Fatalf("a %s lost its end", when.Kind)
		}
	}
}

// AND THE REFUSAL REACHES THE MODEL BEFORE ANYBODY IS ASKED, which is the whole
// point of it: the person never sees a card for a reminder that was already
// dead, so no item lands with `runs: 0` and `retiredWhy: expired` for them to
// find in a log a week later.
func TestProposingAReminderThatEndsBeforeItFiresNeverDrawsACard(t *testing.T) {
	store := newFakeStanding(t)
	agent := standingAgent(t, &scriptedCompleter{}, store, nil)
	due := time.Now().Add(time.Minute).Round(time.Second)
	body := `{"op":"propose","words":"remind me in 1 minute to drink water",` +
		`"when":{"kind":"at","at":"` + due.Format("2006-01-02T15:04:05") + `"},` +
		`"does":{"kind":"say","say":"time to drink water"},` +
		`"rails":{"expires":"` + due.Add(-11*time.Second).Format("2006-01-02T15:04:05") + `"}}`
	text, isError, err := agent.standTool(context.Background(), json.RawMessage(body))
	if err != nil {
		t.Fatalf("standTool: %v", err)
	}
	if !isError || !strings.Contains(text, "so it would retire before it ever fired") {
		t.Fatalf("a reminder that ends before it fires was taken: %q (isError=%v)", text, isError)
	}
	if len(store.created) != 0 {
		t.Fatalf("something stood that could only ever be retired: %+v", store.created)
	}
}

// THE REFUSAL REACHES THE MODEL AS A TOOL RESULT and never as a card: nothing
// is proposed, so there is nothing for the person to answer.
func TestProposingAPastMomentNeverDrawsACard(t *testing.T) {
	store := newFakeStanding(t)
	agent := standingAgent(t, &scriptedCompleter{}, store, nil)
	body := `{"op":"propose","words":"remind me to sleep in 1 min",` +
		`"when":{"kind":"at","at":"2020-01-01T05:42:00"},` +
		`"does":{"kind":"say","say":"time to sleep"}}`
	text, isError, err := agent.standTool(context.Background(), json.RawMessage(body))
	if err != nil {
		t.Fatalf("standTool: %v", err)
	}
	if !isError || !strings.Contains(text, "has already passed") {
		t.Fatalf("a moment in 2020 was taken: %q (isError=%v)", text, isError)
	}
	if len(store.created) != 0 {
		t.Fatal("something was created for a moment that has passed")
	}
}

// EVERY RESULT OF THIS TOOL ENDS WITH THE TIME — every op, and whether it
// worked or not. It is called at the moment the clock matters most, and the
// `Now` line in the instructions may be minutes old (prompt.go's clockRefresh).
func TestEveryStandResultEndsWithTheTime(t *testing.T) {
	store := newFakeStanding(t)
	agent := standingAgent(t, &scriptedCompleter{}, store, nil)
	for _, body := range []string{
		`{"op":"list"}`,
		`{"op":"change"}`,
		`{}`,
		`{"op":"nonsense"}`,
		`{"op":"stop","id":"nothing-like-this"}`,
		`{"op":"propose","words":"x","when":{"kind":"at","at":"2020-01-01T05:42:00"},"does":{"kind":"say","say":"x"}}`,
	} {
		text, _, err := agent.standTool(context.Background(), json.RawMessage(body))
		if err != nil {
			t.Fatalf("%s: %v", body, err)
		}
		lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
		last := lines[len(lines)-1]
		if !strings.HasPrefix(last, "now: ") {
			t.Fatalf("%s ended with %q, not the clock:\n%s", body, last, text)
		}
		if _, err := time.Parse("15:04 -07:00", strings.TrimPrefix(last, "now: ")); err != nil {
			t.Fatalf("%s ended with %q, which is not a time: %v", body, last, err)
		}
	}
}

// ── which answers a card offers ─────────────────────────────────────────────

// `ONCE, NOT STANDING` IS NOT AN ANSWER TO A ONE-OFF REMINDER. Doing a `say` at
// a moment "now" says the wrong thing at the wrong time, or nothing at all — so
// the engine does not offer the chip, and both surfaces read that from here.
func TestAOneOffReminderOffersNoOnce(t *testing.T) {
	reminder := standing.Item{
		When: standing.When{Kind: standing.WhenAt},
		Does: standing.Action{Kind: standing.ActionSay},
	}
	if StandingOnceIsAnAnswer(reminder) {
		t.Fatal("a one-off reminder was offered `once`")
	}
	for _, option := range StandingOptions(reminder) {
		if option.Key == StandingOnceKey {
			t.Fatalf("StandingOptions still carries %q for a one-off reminder", option.Key)
		}
	}
	if len(StandingOptions(reminder)) == 0 {
		t.Fatal("a one-off reminder was left with no answers at all")
	}
}

// AND EVERYWHERE ELSE IT KEEPS IT: a watch, a rule, a routine and overnight
// work are all things a person may reasonably want done once, now.
func TestEverythingButAOneOffReminderKeepsOnce(t *testing.T) {
	for _, item := range []standing.Item{
		{When: standing.When{Kind: standing.WhenProbe}, Does: standing.Action{Kind: standing.ActionSay}},
		{When: standing.When{Kind: standing.WhenEvery}, Does: standing.Action{Kind: standing.ActionTask}},
		{When: standing.When{Kind: standing.WhenFile}, Does: standing.Action{Kind: standing.ActionTask}},
		{When: standing.When{Kind: standing.WhenIdle}, Does: standing.Action{Kind: standing.ActionTask}},
		{When: standing.When{Kind: standing.WhenAt}, Does: standing.Action{Kind: standing.ActionTask}},
	} {
		if !StandingOnceIsAnAnswer(item) {
			t.Fatalf("%s/%s lost its `once` answer", item.When.Kind, item.Does.Kind)
		}
		var found bool
		for _, option := range StandingOptions(item) {
			found = found || option.Key == StandingOnceKey
		}
		if !found {
			t.Fatalf("%s/%s draws no `once` chip", item.When.Kind, item.Does.Kind)
		}
	}
}

// THE CARD SAYS WHICH ANSWERS IT HAS, so the conversation's chips, home's chips
// and the keys this session will take are one decision made once.
func TestTheCardCarriesTheAnswersItOffers(t *testing.T) {
	store := newFakeStanding(t)
	body := `{"op":"propose","words":"remind me in 1 min to sleep",` +
		`"when":{"kind":"at","in":"1m"},"when_words":"in a minute",` +
		`"does":{"kind":"say","say":"time to sleep"},"cost_words":"a cent"}`
	completer := &scriptedCompleter{steps: []step{standCall("s1", body), finalText("set up")}}
	agent := standingAgent(t, completer, store, nil)
	events, err := agent.Submit(context.Background(), "remind me in 1 min to sleep")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	var offered []AnswerOption
	drainAnsweringStanding(t, events, func(event Event) {
		offered = event.Standing.Options
		agent.ResolveStanding(event.Standing.ID, StandingAnswer{Approved: true})
	})
	if len(offered) == 0 {
		t.Fatal("the card named no answers at all")
	}
	for _, option := range offered {
		if option.Key == StandingOnceKey {
			t.Fatalf("a one-minute reminder's card offered %q %s", option.Key, option.Label)
		}
	}
}

// ── THE FIRING IS DRAWN WHERE IT LANDED ─────────────────────────────────────
//
// The defect the real-binary suite found: a reminder fired into the
// conversation the person was sitting in, the line went onto the steering queue,
// the model answered it — AND THE SCREEN SHOWED NOTHING. Nothing ever emitted
// EventStandingUpdate, so internal/tui3's `◦ <words> · said: <text>` row was
// unreachable in production and the manual's promise could not come true.
//
// It takes the STANDING lane and not a turn's hub, because a firing arrives
// when no turn is running — which is the whole of what ambient means.
func TestAFiringIntoALiveConversationIsDrawnAtOnce(t *testing.T) {
	workspace := t.TempDir()
	room := standingLiveAgent(t, workspace, nil)
	lane := room.TaskUpdates()

	runner := &standingRunner{root: t.TempDir()}
	item := standing.Item{
		ID:        "item1",
		Words:     "remind me in 1 minute to drink water",
		Workspace: workspace,
		Origin:    standing.Origin{SessionID: room.id, Transcript: room.config.SessionFile},
	}
	if _, err := runner.Say(context.Background(), item, "💧 Time to drink water!"); err != nil {
		t.Fatalf("Say: %v", err)
	}

	event := standingNextUpdate(t, lane)
	if event.Standing.Update != "fired" {
		t.Fatalf("the firing reported %q, want \"fired\"", event.Standing.Update)
	}
	if event.Standing.Item.Words != item.Words || event.Standing.Item.ID != item.ID {
		t.Fatalf("the row has nothing to name the item with: %+v", event.Standing.Item)
	}
	if event.Standing.Text != "💧 Time to drink water!" {
		t.Fatalf("the row carries %q, not what the firing said", event.Standing.Text)
	}
	// AND THE MODEL IS TOLD TOO. The row is the news; the steering line is what
	// makes the conversation able to talk about it.
	if queued := standingQueued(room); len(queued) != 1 {
		t.Fatalf("the conversation has %d queued lines, want the steering line as well", len(queued))
	}
}

// A run that stopped on something only a person can allow wears the accent, and
// one that broke says so. Both are the same lane and the same row.
func TestAFiringThatNeedsSomebodyReportsItOnTheStandingLane(t *testing.T) {
	for _, probe := range []struct{ kind, want string }{
		{"said", "fired"},
		{"landed", "fired"},
		{"needs-you", "needs-you"},
		{"failed", "failed"},
	} {
		if word := standingUpdateWord(probe.kind); word != probe.want {
			t.Fatalf("a %q outcome draws %q, want %q", probe.kind, word, probe.want)
		}
	}

	workspace := t.TempDir()
	room := standingLiveAgent(t, workspace, nil)
	lane := room.TaskUpdates()
	runner := &standingRunner{root: t.TempDir()}
	item := standing.Item{
		ID:        "item2",
		Words:     "keep main green",
		Workspace: workspace,
		Origin:    standing.Origin{SessionID: room.id, Transcript: room.config.SessionFile},
	}
	runner.deliver(item, "needs-you", "the fix touches migrations", "")
	event := standingNextUpdate(t, lane)
	if event.Standing.Update != "needs-you" || event.Standing.Text != "the fix touches migrations" {
		t.Fatalf("the row is %+v", event.Standing)
	}
}

// ── "WHILE YOU WERE AWAY" IS VISIBLE ON OPEN ────────────────────────────────
//
// The fold reached the MODEL and nobody else: enqueueAmbientNote queues and
// never wakes, so a person opening a conversation with news in its inbox saw an
// empty screen until they next typed. The same rows the live road draws are
// handed to the first surface that opens the lane.
func TestWhatFiredWhileTheWindowWasShutIsDrawnWhenItOpens(t *testing.T) {
	root, workspace := t.TempDir(), t.TempDir()
	dir := t.TempDir()
	now := time.Now()
	for _, note := range []standing.Note{
		{At: now, ItemID: "item1", Words: "tell me when CI goes red", Kind: "said", Text: "the last run on main failed"},
		{At: now.Add(time.Minute), ItemID: "item2", Words: "keep main green", Kind: "needs-you", Text: "the fix touches migrations"},
	} {
		if err := standing.Deliver(dir, note); err != nil {
			t.Fatalf("Deliver: %v", err)
		}
	}

	store := &fakeStanding{root: root, items: map[string]standing.Item{}}
	agent := standingAgent(t, &scriptedCompleter{}, store, func(config *Config) {
		config.Workspace = workspace
		config.Place = Place{Dir: dir, Workspace: workspace}
		config.SessionFile = config.Place.Transcript()
	})

	// THE LANE IS OPENED AFTER THE AGENT EXISTS, which is the ordering that made
	// the fold invisible: the drain runs inside New, where nobody is subscribed.
	lane := agent.TaskUpdates()
	first := standingNextUpdate(t, lane)
	if first.Standing.Update != "fired" || first.Standing.Item.Words != "tell me when CI goes red" {
		t.Fatalf("the first row is %+v", first.Standing)
	}
	if first.Standing.Text != "the last run on main failed" {
		t.Fatalf("the first row says %q", first.Standing.Text)
	}
	second := standingNextUpdate(t, lane)
	if second.Standing.Update != "needs-you" || second.Standing.Item.Words != "keep main green" {
		t.Fatalf("the second row is %+v", second.Standing)
	}
	// AND THE MODEL STILL GETS ONE FOLD AND NOT TWO NOTES. The two readers have
	// two different laws (standing_run.go's queueStandingNews).
	queued := standingQueued(agent)
	if len(queued) != 1 || !strings.HasPrefix(queued[0], "while you were away") {
		t.Fatalf("the model was handed %d notes: %q", len(queued), queued)
	}
}

// ── THE MODEL DOES NOT RE-PROPOSE ITS OWN FIRING ────────────────────────────
//
// Both end-to-end suites watched the model read `◦ remind me in 1 min to eat
// medicines: …` as a fresh request and call `stand` again, so a one-off reminder
// proposed itself a second time the moment it fired. The fix is the text the
// engine injects, and this pins it there.
func TestTheSteeringLineReadsAsNewsAndNotAsARequest(t *testing.T) {
	item := standing.Item{Words: "remind me in 1 minute to drink water"}
	line := standingSteeringLine(item, "💧 Time to drink water!")
	if !strings.HasPrefix(line, standingNewsFrame) {
		t.Fatalf("the injected line does not open by saying what it is: %q", line)
	}
	if !strings.Contains(line, "◦ remind me in 1 minute to drink water: 💧 Time to drink water!") {
		t.Fatalf("the injected line lost the news itself: %q", line)
	}
	if !strings.Contains(line, "Do not call stand again") {
		t.Fatalf("the injected line does not forbid setting it up again: %q", line)
	}
	// The actual prompt must carry the same rule as the injected news. Standing
	// instructions are composed from capability availability, so checking the raw
	// template would miss the page the standing-capable agent actually receives.
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.System = ""
		config.standingItems = &fakeStanding{}
	})
	page := systemTextOf(agent)
	if !strings.Contains(page, standingNewsFrame) {
		t.Fatalf("the rendered prompt never mentions %q", standingNewsFrame)
	}
	if !strings.Contains(page, "never call `stand`\nagain for it") {
		t.Fatal("the rendered prompt does not tell the model to leave a fired item alone")
	}
}

// standingNextUpdate takes the next EventStandingUpdate off a standing lane, or
// fails. A lane that says nothing is the defect itself, so the wait is short and
// the failure is the point.
func standingNextUpdate(t *testing.T, lane <-chan Event) Event {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case event, ok := <-lane:
			if !ok {
				t.Fatal("the standing lane closed with no firing on it")
			}
			if event.Kind != EventStandingUpdate {
				continue
			}
			if event.Standing == nil {
				t.Fatal("an update with no card on it")
			}
			return event
		case <-deadline:
			t.Fatal("nothing was drawn: no EventStandingUpdate reached the standing lane")
		}
	}
}
