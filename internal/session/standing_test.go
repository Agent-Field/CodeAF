package session

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	agent := standingAgent(t, completer, store, nil)

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
	// A reminder is the one shape whose day cap is one: it fires once and
	// retires, and ten would be arithmetic about something that cannot happen.
	if created.Rails.MaxPerDay != standingReminderPerDay || created.Rails.PerRunUSD != standingPerRunUSD {
		t.Fatalf("rails = %+v, want the defaults", created.Rails)
	}
	if created.Origin.SessionID == "" || len(created.Origin.TurnIDs) != 1 {
		t.Fatalf("origin = %+v, want the conversation and its turn", created.Origin)
	}
	card, found := firstOfKind(collected, EventStandingProposal)
	if !found || card.Standing.WhenWords != "at 6 this evening" {
		t.Fatalf("the card did not carry the cadence in words: %+v", card.Standing)
	}
	update, found := firstOfKind(collected, EventStandingUpdate)
	if !found || update.Standing.Update != "stood" {
		t.Fatalf("nothing reported that it now stands: %v", kinds(collected))
	}
	if output := toolOutput(t, collected, "stand"); !strings.Contains(output, "set up "+created.ID) {
		t.Fatalf("tool result = %q, want the id back", output)
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
	if text, _, _ := agent.standTool(context.Background(), json.RawMessage(`{"op":"list"}`)); text != "Nothing stands in this project yet." {
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

// ── the one-time offer ──────────────────────────────────────────────────────

// THE YES IS JOURNALED BEFORE THE HOST IS TOUCHED, and the question is asked
// once ever: a marker beside the items is the whole memory of it.
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
	if line != "◦ tell me when CI goes red: the last run on main failed" {
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
	if want := "◦ remind me in 1 min to eat medicines: Time to eat your medicines."; queued[0] != want {
		t.Fatalf("steering line = %q, want %q", queued[0], want)
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
