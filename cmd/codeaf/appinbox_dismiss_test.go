package main

import (
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/automation"
	"github.com/Agent-Field/codeaf/internal/service"
	"github.com/Agent-Field/codeaf/internal/session"
	teamstore "github.com/Agent-Field/codeaf/internal/teams"
)

// fakeDismissals is what each fake conversation was asked to dismiss.
var fakeDismissals sync.Map

// DismissQuestion is the fake's half of a dismissal: it withdraws the model's
// question with the person's act as the reason, as the session does.
func (c *fakeConversation) DismissQuestion(token string) error {
	if seen, ok := fakeDismissals.Load(c); ok {
		seen.(chan string) <- token
	}
	id, _ := strconv.ParseUint(token, 10, 64)
	q := session.Question{ID: id, Kind: session.QuestionAsk, Withdrawn: &session.Withdrawal{Reason: "you dismissed it"}}
	c.raise(session.Event{Kind: session.EventQuestionWithdrawn, Question: &q})
	return nil
}

// resolvedEvent waits for the Inbox to resolve one item.
func resolvedEvent(t *testing.T, rec *appRecorder, id string) appwire.InboxResolvedEvent {
	t.Helper()
	i := rec.waitFor(t, 5*time.Second, func(_ string, data any) bool {
		resolved, ok := data.(appwire.InboxResolvedEvent)
		return ok && resolved.ID == id
	})
	rec.mu.Lock()
	defer rec.mu.Unlock()
	return rec.data[i].(appwire.InboxResolvedEvent)
}

// DISMISS CLOSES THE QUESTION AND MOVES THE ITEM TO DONE: the model's own
// question is withdrawn by its conversation, any other question is answered
// with its one answer that changes nothing, and each leaves "Dismissed · you".
// A question with no such answer is refused and stays waiting.
func TestAppInboxDismissClosesTheQuestionWithNothingDecided(t *testing.T) {
	conv := &fakeConversation{}
	backend, rec, agent := workBackend(t, conv)
	seen := make(chan string, 1)
	fakeDismissals.Store(conv, seen)
	t.Cleanup(func() { fakeDismissals.Delete(conv) })
	answered := make(chan session.Answer, 1)
	conv.mu.Lock()
	conv.onAnswer = func(answer session.Answer) {
		answered <- answer
		q := session.Question{ID: answer.ID, Kind: answer.Kind}
		conv.raise(session.Event{Kind: session.EventQuestionAnswered, Question: &q, Answer: &answer})
	}
	conv.mu.Unlock()
	added := func(id string) {
		t.Helper()
		rec.waitFor(t, 5*time.Second, func(_ string, data any) bool {
			a, ok := data.(appwire.InboxAddedEvent)
			return ok && a.Item.ID == id
		})
	}

	// The model's own question, one its turn did not wait for.
	ask := session.Question{ID: 9, Kind: session.QuestionAsk, Head: "Which store?", Later: true, Options: []session.AnswerOption{{Key: "sqlite", Label: "SQLite"}}}
	conv.raise(session.Event{Kind: session.EventQuestion, Question: &ask})
	askItem := appInboxID(agent.ID, "ask:9")
	added(askItem)
	if err := backend.DismissInbox(askItem); err != nil {
		t.Fatal(err)
	}
	if token := <-seen; token != "9" {
		t.Fatalf("dismissed question %q, want 9", token)
	}
	if got := resolvedEvent(t, rec, askItem); got.By != appwire.ResolvedDismissed || !strings.HasPrefix(got.Receipt, "Dismissed · you · ") {
		t.Fatalf("the model's question resolved as %+v", got)
	}

	// A permission: dismissed as denied, the one answer that changes nothing.
	consent := session.Question{ID: 4, Kind: session.QuestionConsent, Head: "bash rm -rf build", Options: session.AnswerOptions(session.QuestionConsent)}
	conv.raise(session.Event{Kind: session.EventQuestion, Question: &consent})
	consentItem := appInboxID(agent.ID, "consent:4")
	added(consentItem)
	if err := backend.DismissInbox(consentItem); err != nil {
		t.Fatal(err)
	}
	if answer := <-answered; answer.Key != "3" || answer.Change != appDismissedNote {
		t.Fatalf("the permission was answered %+v, want deny with the dismissal", answer)
	}
	if got := resolvedEvent(t, rec, consentItem); got.By != appwire.ResolvedDismissed || !strings.HasPrefix(got.Receipt, "Dismissed · you · ") {
		t.Fatalf("the permission resolved as %+v", got)
	}

	// A fuel gate has no answer that changes nothing: dismissing would decide.
	fuel := session.Question{ID: 5, Kind: session.QuestionFuel, Head: "The run is out of fuel", Options: session.AnswerOptions(session.QuestionFuel)}
	conv.raise(session.Event{Kind: session.EventQuestion, Question: &fuel})
	fuelItem := appInboxID(agent.ID, appAskID(agent.ID, fuel))
	added(fuelItem)
	if err := backend.DismissInbox(fuelItem); err == nil || !strings.Contains(err.Error(), "changes nothing") {
		t.Fatalf("a fuel gate was dismissed: %v", err)
	}
	if waiting, _, _ := backend.ListInbox(appwire.InboxWaiting, ""); len(waiting) != 1 || waiting[0].ID != fuelItem {
		t.Fatalf("waiting after a refused dismissal = %+v", waiting)
	}

	// A landed task's "your call": its safe-looking "not right" refutes the task
	// and fails what depends on it, so dismissing it is refused, not answered.
	landing := session.Question{ID: 6, Kind: session.QuestionLanding, Head: "Task 2 landed unverified", Options: []session.AnswerOption{{Key: "y", Label: "Accept"}, {Key: session.LandingNoKey, Label: "Not right", Safe: true}}}
	conv.raise(session.Event{Kind: session.EventQuestion, Question: &landing})
	landingItem := appInboxID(agent.ID, appAskID(agent.ID, landing))
	added(landingItem)
	if err := backend.DismissInbox(landingItem); err == nil || !strings.Contains(err.Error(), "decides the task") {
		t.Fatalf("a landing was dismissed: %v", err)
	}
	select {
	case answer := <-answered:
		t.Fatalf("dismissing a landing answered it %+v", answer)
	default:
	}
	if waiting, _, _ := backend.ListInbox(appwire.InboxWaiting, ""); len(waiting) != 2 {
		t.Fatalf("waiting after a refused landing dismissal = %+v", waiting)
	}

	done, counts, _ := backend.ListInbox(appwire.InboxDone, "")
	if len(done) != 2 || counts.DoneToday != 2 {
		t.Fatalf("done = %+v %+v", done, counts)
	}
	for _, item := range done {
		if item.Answered == nil || item.Answered.By != appwire.ResolvedDismissed || item.Answered.Where != "inbox" {
			t.Fatalf("a dismissed item reads %+v", item.Answered)
		}
	}
	if err := backend.DismissInbox(askItem); err == nil {
		t.Fatal("a done item was dismissed again")
	}
	if err := backend.DismissInbox("library:page/conflict"); err == nil {
		t.Fatal("a Library choice was dismissed")
	}
}

// dismissedAs dismisses one item and answers how the Inbox resolved it.
func dismissedAs(t *testing.T, b *appBackend, rec *appRecorder, id string) appwire.InboxResolvedEvent {
	t.Helper()
	if err := b.DismissInbox(id); err != nil {
		t.Fatalf("dismiss %s: %v", id, err)
	}
	got := resolvedEvent(t, rec, id)
	if got.By != appwire.ResolvedDismissed || !strings.HasPrefix(got.Receipt, "Dismissed · you · ") {
		t.Fatalf("%s resolved as %+v", id, got)
	}
	return got
}

// EVERY OTHER KIND THAT REACHES THE INBOX CLOSES THROUGH ITS OWN DOOR: a budget
// ask closes with the budget unchanged, an order's run is skipped, a crew's
// decision is decided as dismissed and its raiser told, a crew proposal is let
// go, and a kept-running process's line closes with the process left alone.
func TestAppInboxDismissClosesEveryKindThroughItsOwnDoor(t *testing.T) {
	t.Run("budget", func(t *testing.T) {
		t.Setenv("CODEAF_HOME", t.TempDir())
		conv := &fakeConversation{cost: 4.2}
		b, rec, agent := workBackend(t, conv)
		b.profileDir = t.TempDir()
		budget := 5.0
		if _, err := b.SetAgentBudget(appwire.AgentBudgetSet{ID: agent.ID, USD: &budget}); err != nil {
			t.Fatal(err)
		}
		if _, err := b.Send(agent.ID, "keep going", ""); err != nil {
			t.Fatal(err)
		}
		i := rec.waitFor(t, 5*time.Second, func(_ string, data any) bool {
			v, ok := data.(appwire.InboxAddedEvent)
			return ok && v.Item.Ask.Subkind == appwire.SubkindBudget
		})
		rec.mu.Lock()
		id := rec.data[i].(appwire.InboxAddedEvent).Item.ID
		rec.mu.Unlock()
		dismissedAs(t, b, rec, id)
		if got, _ := b.agentBudget(agent.ID, time.Now()); got != budget {
			t.Fatalf("a dismissed budget ask changed the budget to %v", got)
		}
	})
	t.Run("order run", func(t *testing.T) {
		b, rec, agent := workBackend(t, &fakeConversation{})
		b.profileDir = t.TempDir()
		store, err := automation.Open(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { store.Close() })
		b.orderStore = func() *automation.Store { return store }
		order, err := store.Create(automation.Automation{
			Title: "weekly update", Schedule: automation.Schedule{Every: "1h", Zone: "UTC"},
			Action: automation.Action{Do: "draft the update"}, Workspace: t.TempDir(),
			Status: automation.StatusProposed, Trust: automation.TrustAsksFirst,
			Origin: automation.Origin{Transcript: appFakeTranscript},
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.Ratify(order.ID); err != nil {
			t.Fatal(err)
		}
		run, err := store.QueueAsking(order.ID)
		if err != nil {
			t.Fatal(err)
		}
		open := map[int64]appwire.Ask{}
		b.sayOrderAsks(store, open)
		id := appInboxID(agent.ID, appOrderAskID(run.ID))
		rec.waitFor(t, 5*time.Second, func(_ string, data any) bool { _, ok := data.(appwire.InboxAddedEvent); return ok })
		if err := b.DismissInbox(id); err != nil {
			t.Fatal(err)
		}
		b.sayOrderAsks(store, open)
		if got := resolvedEvent(t, rec, id); got.By != appwire.ResolvedDismissed || !strings.HasPrefix(got.Receipt, "Dismissed · you · ") {
			t.Fatalf("the run resolved as %+v", got)
		}
		if r, err := store.Run(run.ID); err != nil || r.Line != automation.LineSkipped {
			t.Fatalf("the dismissed run = %+v %v, want skipped", r, err)
		}
	})
	t.Run("crew decision", func(t *testing.T) {
		b, rec, agent, crew := crewBackend(t)
		run := b.crews()
		packet, err := teamstore.Raise(b.profileDir, teamstore.Packet{
			Kind: teamstore.PacketQuestion, Team: teamstore.Person, Origin: crew,
			RaisedBy: teamstore.FromManager, Question: "Which day?",
			Options: []teamstore.Option{{ID: "fri", Label: "Friday", Consequence: "ship before the weekend"}},
		})
		if err != nil {
			t.Fatal(err)
		}
		b.crewTick(run, false)
		id := appInboxID(agent.ID, appCrewAskPrefix+packet.ID)
		rec.waitFor(t, 5*time.Second, func(_ string, data any) bool { _, ok := data.(appwire.InboxAddedEvent); return ok })
		dismissedAs(t, b, rec, id)
		decided, err := teamstore.PacketByID(b.profileDir, packet.ID)
		if err != nil || decided.State != teamstore.PacketDecided || decided.Decision != appDismissedWords {
			t.Fatalf("the packet = %+v %v, want decided as dismissed", decided, err)
		}
	})
	t.Run("crew proposal", func(t *testing.T) {
		conv := &proposingConversation{fakeConversation: &fakeConversation{}, answer: helpersProposal}
		b, rec, agent := proposingBackend(t, conv)
		if err := b.CrewPropose(agent.ID); err != nil {
			t.Fatal(err)
		}
		i := rec.waitFor(t, 5*time.Second, func(_ string, data any) bool {
			v, ok := data.(appwire.InboxAddedEvent)
			return ok && v.Item.Ask.Subkind == appwire.SubkindCrewProposal
		})
		rec.mu.Lock()
		id := rec.data[i].(appwire.InboxAddedEvent).Item.ID
		rec.mu.Unlock()
		dismissedAs(t, b, rec, id)
		if crews, _ := b.CrewList(false); len(crews) != 0 {
			t.Fatalf("a dismissed proposal made a crew: %+v", crews)
		}
	})
	t.Run("kept-running process", func(t *testing.T) {
		w := newSvcWorld(t)
		row := w.keep(t, w.agent.ID, service.PolicyNever)
		w.rt.Failing(true)
		for fails := 1; fails <= 3; fails++ {
			w.check(t, w.agent.ID+":"+row.ID, fails)
		}
		i := w.rec.waitFor(t, 5*time.Second, func(_ string, data any) bool {
			v, ok := data.(appwire.InboxAddedEvent)
			return ok && v.Item.Ask.Subkind == appwire.SubkindServiceHealth
		})
		w.rec.mu.Lock()
		id := w.rec.data[i].(appwire.InboxAddedEvent).Item.ID
		w.rec.mu.Unlock()
		dismissedAs(t, w.backend, w.rec, id)
		if waiting, _, _ := w.backend.ListInbox(appwire.InboxWaiting, ""); len(waiting) != 0 {
			t.Fatalf("the process's line came back: %+v", waiting)
		}
		// AND AFTER A RESTART: the episode is still open, and still dismissed.
		w = w.relaunch(t, nil)
		if waiting, _, _ := w.backend.ListInbox(appwire.InboxWaiting, ""); len(waiting) != 0 {
			t.Fatalf("the dismissed line came back after a restart: %+v", waiting)
		}
		// A NEW EPISODE OPENS ITS LINE: the process recovers, then fails again.
		w.rt.Failing(false)
		w.check(t, w.agent.ID+":"+row.ID, 0)
		w.changed(t, w.agent.ID+":"+row.ID, func(p appwire.Process) bool { return !p.Service.EpisodeOpen })
		w.rt.Failing(true)
		for fails := 1; fails <= 3; fails++ {
			w.check(t, w.agent.ID+":"+row.ID, fails)
		}
		w.rec.waitFor(t, 5*time.Second, func(_ string, data any) bool {
			v, ok := data.(appwire.InboxAddedEvent)
			return ok && v.Item.Ask.Subkind == appwire.SubkindServiceHealth
		})
		var kept map[string]string
		_ = w.backend.state.read(func(f *appStateFile) { kept = f.ServiceDismissed })
		if len(kept) != 0 {
			t.Fatalf("a closed episode's dismissal is still kept: %v", kept)
		}
	})
}
