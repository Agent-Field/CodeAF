package main

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/automation"
	"github.com/Agent-Field/codeaf/internal/service"
	"github.com/Agent-Field/codeaf/internal/session"
	teamstore "github.com/Agent-Field/codeaf/internal/teams"
)

// EVERY OPEN ASK IS AN INBOX ITEM with the turns before it; an answer from the
// Inbox is held and can be taken back, and an answer anywhere else clears the
// item with its receipt.
func TestAppInboxHoldsUndoesAndClearsEverywhere(t *testing.T) {
	conv := &fakeConversation{script: []session.Event{
		{Kind: session.EventTextDelta, Text: "Looking at the build."},
		{Kind: session.EventTurnDone},
	}}
	backend, rec, agent := workBackend(t, conv)
	conv.mu.Lock()
	conv.onAnswer = func(answer session.Answer) {
		closed := session.Question{Kind: answer.Kind, ID: answer.ID}
		conv.raise(session.Event{Kind: session.EventQuestionAnswered, Question: &closed, Answer: &answer})
	}
	conv.mu.Unlock()
	if _, err := backend.Send(agent.ID, "clean the build dir", ""); err != nil {
		t.Fatal(err)
	}
	rec.waitFor(t, 5*time.Second, appTurnDone)

	first := session.Question{ID: 4, Kind: session.QuestionConsent, Head: "bash rm -rf build", Reason: "bash asks first", Options: session.AnswerOptions(session.QuestionConsent)}
	conv.raise(session.Event{Kind: session.EventQuestion, Question: &first})
	i := rec.waitFor(t, 5*time.Second, func(event string, data any) bool { _, ok := data.(appwire.InboxAddedEvent); return ok })
	rec.mu.Lock()
	item := rec.data[i].(appwire.InboxAddedEvent).Item
	rec.mu.Unlock()
	if item.ID != agent.ID+"/consent:4" || item.List != appwire.InboxWaiting || item.Where.Agent != agent.ID || len(item.Context) != 2 || item.Context[0].Text != "clean the build dir" {
		t.Fatalf("the item = %+v", item)
	}
	items, counts, _ := backend.ListInbox(appwire.InboxWaiting, "rm -rf")
	if len(items) != 1 || counts.Waiting != 1 {
		t.Fatalf("waiting = %+v %+v", items, counts)
	}

	until, err := backend.AnswerInbox(item.ID, appwire.OptionAllowOnce, "", "", "")
	if err != nil || until.Before(time.Now().Add(4*time.Second)) {
		t.Fatalf("the held answer = %v %v", until, err)
	}
	if err := backend.UndoInbox(item.ID); err != nil {
		t.Fatal(err)
	}
	time.Sleep(appInboxHold + 500*time.Millisecond)
	conv.mu.Lock()
	sent := len(conv.answers)
	conv.mu.Unlock()
	if sent != 0 {
		t.Fatal("an answer taken back was sent")
	}

	if err := backend.LaterInbox(item.ID, true); err != nil {
		t.Fatal(err)
	}
	rec.waitFor(t, 5*time.Second, func(event string, data any) bool {
		added, ok := data.(appwire.InboxAddedEvent)
		return ok && added.Item.ID == item.ID && added.Item.List == appwire.InboxLater
	})
	// Answered in the dock, it leaves the Inbox with its receipt.
	if err := backend.AnswerAsk(agent.ID, "consent:4", appwire.OptionAllowOnce, ""); err != nil {
		t.Fatal(err)
	}
	j := rec.waitFor(t, 5*time.Second, func(event string, data any) bool { _, ok := data.(appwire.InboxResolvedEvent); return ok })
	rec.mu.Lock()
	resolved := rec.data[j].(appwire.InboxResolvedEvent)
	rec.mu.Unlock()
	if resolved.ID != item.ID || resolved.By != appwire.ClosedByApp || !strings.HasPrefix(resolved.Receipt, "Allowed once · you · ") {
		t.Fatalf("resolved = %+v", resolved)
	}
	done, counts, _ := backend.ListInbox(appwire.InboxDone, "")
	if len(done) != 1 || counts.DoneToday != 1 || counts.Later != 0 || done[0].Answered == nil {
		t.Fatalf("done = %+v %+v", done, counts)
	}
	if a, task, err := backend.OpenInbox(item.ID); err != nil || a != agent.ID || task != "" {
		t.Fatalf("open = %s %s %v", a, task, err)
	}
}

// Contract 2: each owning lane refuses invalid payloads before Undo starts.
func TestAppInboxValidatesOwnedAnswerPayloads(t *testing.T) {
	refused := func(t *testing.T, b *appBackend, id, option, text, want string) {
		t.Helper()
		until, err := b.AnswerInbox(id, option, text, "", "")
		undo := b.UndoInbox(id)
		if wireCode(err) != want || !until.IsZero() || wireCode(undo) != appwire.CodeNotFound {
			t.Errorf("answer = %v %v; Undo = %v; want immediate %s and no hold", until, err, undo, want)
		}
		if items, counts, err := b.ListInbox(appwire.InboxWaiting, ""); err != nil || len(items) != 1 || counts.Waiting != 1 || counts.DoneToday != 0 {
			t.Fatalf("validation changed Waiting: %+v %+v %v", items, counts, err)
		}
	}
	for _, tc := range []struct{ name, option, text, code string }{
		{"raise needs money", appwire.OptionRaise, "not money", appwire.CodeBadParams},
		{"tell needs words", appwire.OptionTell, " ", appwire.CodeBadParams},
		{"budget needs option", "", "", appwire.CodeBadParams},
		{"tell needs idle agent", appwire.OptionTell, "keep going", appwire.CodeBusy},
	} {
		t.Run(tc.name, func(t *testing.T) {
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
			if tc.code == appwire.CodeBusy {
				a := mustFind(t, b, agent.ID)
				b.locked(func() { a.live.running = true })
				defer b.locked(func() { a.live.running = false })
			}
			refused(t, b, id, tc.option, tc.text, tc.code)
		})
	}
	t.Run("proposal needs option", func(t *testing.T) {
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
		refused(t, b, id, "", "", appwire.CodeBadParams)
	})
	t.Run("service needs offered option", func(t *testing.T) {
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
		for _, option := range []string{"invented", ""} {
			refused(t, w.backend, id, option, "", appwire.CodeBadParams)
		}
	})
	t.Run("order needs option", func(t *testing.T) {
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
		b.sayOrderAsks(store, map[int64]appwire.Ask{})
		rec.waitFor(t, 5*time.Second, func(_ string, data any) bool { _, ok := data.(appwire.InboxAddedEvent); return ok })
		refused(t, b, appInboxID(agent.ID, appOrderAskID(run.ID)), "", "", appwire.CodeBadParams)
	})
	t.Run("crew decision needs answer", func(t *testing.T) {
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
		rec.waitFor(t, 5*time.Second, func(_ string, data any) bool { _, ok := data.(appwire.InboxAddedEvent); return ok })
		id := appInboxID(agent.ID, appCrewAskPrefix+packet.ID)
		refused(t, b, id, "", "", appwire.CodeBadParams)
		until, err := b.AnswerInbox(id, "", "Friday", "", "")
		if err != nil || until.IsZero() {
			t.Fatalf("crew text-only answer = %v %v", until, err)
		}
		if err := b.UndoInbox(id); err != nil {
			t.Fatal(err)
		}
	})
}

// Contract 2: invalid answers never enter the undo hold or reach the engine.
func TestAppInboxRefusesInvalidAnswersBeforeHolding(t *testing.T) {
	for _, scenario := range []string{"closed", "unoffered option", "unknown conversation", "empty consent", "empty task"} {
		t.Run(scenario, func(t *testing.T) {
			t.Setenv("CODEAF_HOME", t.TempDir())
			conv := &fakeConversation{}
			backend, rec, agent := workBackend(t, conv)
			backend.profileDir = t.TempDir()
			q := session.Question{ID: 9, Kind: session.QuestionAsk, Head: "Which store?", Options: []session.AnswerOption{{Key: "sqlite", Label: "SQLite"}}}
			if scenario == "empty consent" {
				q.Kind, q.Options = session.QuestionConsent, session.AnswerOptions(session.QuestionConsent)
			} else if scenario == "empty task" {
				q.Kind, q.Options = session.QuestionTask, session.AnswerOptions(session.QuestionTask)
			}
			conv.raise(session.Event{Kind: session.EventQuestion, Question: &q})
			rec.waitFor(t, 5*time.Second, func(_ string, data any) bool { _, ok := data.(appwire.InboxAddedEvent); return ok })
			id, option, want := appInboxID(agent.ID, "ask:9"), "sqlite", appwire.CodeNotFound
			if strings.HasPrefix(scenario, "empty ") {
				id, option, want = appInboxID(agent.ID, appAskID(q)), "", appwire.CodeBadParams
			}
			switch scenario {
			case "closed":
				conv.raise(session.Event{Kind: session.EventQuestionWithdrawn, Question: &q})
				rec.waitFor(t, 5*time.Second, func(_ string, data any) bool { _, ok := data.(appwire.InboxResolvedEvent); return ok })
			case "unoffered option":
				option, want = "invented", appwire.CodeBadParams
			case "unknown conversation":
				id = "missing/ask:9"
				backend.emit.Emit(appwire.EventAskOpened, appwire.AskOpened{Ask: appwire.Ask{ID: "ask:9", Agent: "missing", Options: []appwire.AskOption{{ID: "sqlite", Label: "SQLite"}}}})
			}
			until, err := backend.AnswerInbox(id, option, "", "", "")
			undo := backend.UndoInbox(id)
			if wireCode(err) != want || !until.IsZero() || wireCode(undo) != appwire.CodeNotFound {
				t.Fatalf("answer = %v %v; Undo = %v; want immediate %s and no hold", until, err, undo, want)
			}
			conv.mu.Lock()
			defer conv.mu.Unlock()
			if len(conv.answers) != 0 {
				t.Fatalf("validation sent answers: %+v", conv.answers)
			}
		})
	}
}

// Contract 2: landing verdicts and fuel amounts are checked before any hold,
// while valid answers keep the same Undo path and validation settles nothing.
func TestAppInboxValidatesLandingAndFuelAnswers(t *testing.T) {
	for _, tc := range []struct {
		name              string
		kind              session.QuestionKind
		option, text, why string
		valid             bool
	}{
		{"landing needs verdict", session.QuestionLanding, "", "", "", false},
		{"landing text is not verdict", session.QuestionLanding, "", "looks good", "", false},
		{"landing why is not verdict", session.QuestionLanding, "", "", "looks good", false},
		{"landing tell needs words", session.QuestionLanding, session.LandingTellKey, "", "", false},
		{"landing tell needs nonblank words", session.QuestionLanding, session.LandingTellKey, " \t", "\n", false},
		{"conflict tell needs words", session.QuestionConflict, session.LandingTellKey, "", "", false},
		{"fuel needs answer", session.QuestionFuel, "", "", "", false},
		{"fuel needs gate words", session.QuestionFuel, "", "not money", "", false},
		{"fuel amount alone is not answer", session.QuestionFuel, "", "2.50", "", false},
		{"fuel topup needs money", session.QuestionFuel, "1", "not money", "", false},
		{"fuel topup needs amount", session.QuestionFuel, "1", "", "", false},
		{"fuel topup needs positive amount", session.QuestionFuel, "1", "0", "", false},
		{"fuel topup rejects negative amount", session.QuestionFuel, "1", "-2.50", "", false},
		{"fuel topup validates why", session.QuestionFuel, "1", "", "not money", false},
		{"fuel typed topup needs money", session.QuestionFuel, "", "topup:not money", "", false},
		{"fuel typed topup needs amount", session.QuestionFuel, "", "topup:", "", false},
		{"fuel typed topup needs positive amount", session.QuestionFuel, "", "topup:0", "", false},
		{"landing accept", session.QuestionLanding, session.LandingYesKey, "", "", true},
		{"landing refuse with note", session.QuestionLanding, session.LandingNoKey, "try again", "", true},
		{"landing tell words", session.QuestionLanding, session.LandingTellKey, "use the existing reader", "", true},
		{"landing tell why", session.QuestionLanding, session.LandingTellKey, "", "use the existing reader", true},
		{"conflict resolve", session.QuestionConflict, session.LandingYesKey, "", "", true},
		{"fuel topup", session.QuestionFuel, "1", "2.50", "", true},
		{"fuel topup dollars", session.QuestionFuel, "1", " $2.50 ", "", true},
		{"fuel topup why", session.QuestionFuel, "1", "", "$2.50", true},
		{"fuel finish with note", session.QuestionFuel, "2", "wrap up", "", true},
		{"fuel stop", session.QuestionFuel, "3", "", "", true},
		{"fuel typed finish", session.QuestionFuel, "", " FINISH ", "", true},
		{"fuel typed stop", session.QuestionFuel, "", "stop", "", true},
		{"fuel typed topup", session.QuestionFuel, "", " TOPUP: $2.50 ", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("CODEAF_HOME", t.TempDir())
			conv := &fakeConversation{}
			b, rec, agent := workBackend(t, conv)
			b.profileDir = t.TempDir()
			q := session.Question{ID: 9, Kind: tc.kind, Head: "What next?", Options: session.AnswerOptions(tc.kind)}
			conv.raise(session.Event{Kind: session.EventQuestion, Question: &q})
			rec.waitFor(t, 5*time.Second, func(_ string, data any) bool { _, ok := data.(appwire.InboxAddedEvent); return ok })
			id := appInboxID(agent.ID, appAskID(q))
			until, err := b.AnswerInbox(id, tc.option, tc.text, tc.why, "")
			undo := b.UndoInbox(id)
			if tc.valid {
				if err != nil || until.Before(time.Now().Add(4*time.Second)) || undo != nil {
					t.Errorf("valid answer = %v %v; Undo = %v; want the normal hold", until, err, undo)
				}
			} else if wireCode(err) != appwire.CodeBadParams || !until.IsZero() || wireCode(undo) != appwire.CodeNotFound {
				t.Errorf("invalid answer = %v %v; Undo = %v; want immediate bad_params and no hold", until, err, undo)
			}
			if items, counts, err := b.ListInbox(appwire.InboxWaiting, ""); err != nil || len(items) != 1 || counts.Waiting != 1 || counts.DoneToday != 0 {
				t.Errorf("validation changed Waiting: %+v %+v %v", items, counts, err)
			}
			if done, _, err := b.ListInbox(appwire.InboxDone, ""); err != nil || len(done) != 0 {
				t.Errorf("validation wrote Done: %+v %v", done, err)
			}
			if asks, err := b.ListAsks(agent.ID); err != nil || len(asks) != 1 || asks[0].ID != appAskID(q) {
				t.Errorf("validation changed asks: %+v %v", asks, err)
			}
			a := mustFind(t, b, agent.ID)
			b.locked(func() {
				if len(a.live.answered) != 0 {
					t.Errorf("validation marked answers: %+v", a.live.answered)
				}
			})
			conv.mu.Lock()
			defer conv.mu.Unlock()
			if len(conv.answers) != 0 {
				t.Errorf("validation sent answers: %+v", conv.answers)
			}
		})
	}
}

// Contract 2: model answers and task redirects still accept words without a pick.
func TestAppInboxRetainsFreeTextAnswers(t *testing.T) {
	for _, kind := range []session.QuestionKind{session.QuestionAsk, session.QuestionTask} {
		t.Run(string(kind), func(t *testing.T) {
			t.Setenv("CODEAF_HOME", t.TempDir())
			conv := &fakeConversation{}
			b, rec, agent := workBackend(t, conv)
			b.profileDir = t.TempDir()
			q := session.Question{ID: 9, Kind: kind, Head: "What next?", Options: session.AnswerOptions(kind)}
			conv.raise(session.Event{Kind: session.EventQuestion, Question: &q})
			rec.waitFor(t, 5*time.Second, func(_ string, data any) bool { _, ok := data.(appwire.InboxAddedEvent); return ok })
			id := appInboxID(agent.ID, appAskID(q))
			until, err := b.AnswerInbox(id, "", "use the existing reader", "", "")
			if err != nil || until.IsZero() {
				t.Fatalf("text-only answer = %v %v", until, err)
			}
			if err := b.UndoInbox(id); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// Contract 2: a valid answer is held, then sent once with its words intact.
func TestAppInboxSendsAValidAnswerAfterTheHold(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	conv := &fakeConversation{}
	backend, rec, agent := workBackend(t, conv)
	backend.profileDir = t.TempDir()
	answered := make(chan session.Answer, 1)
	conv.mu.Lock()
	conv.onAnswer = func(answer session.Answer) {
		answered <- answer
		q := session.Question{ID: answer.ID, Kind: answer.Kind}
		conv.raise(session.Event{Kind: session.EventQuestionAnswered, Question: &q, Answer: &answer})
	}
	conv.mu.Unlock()
	q := session.Question{ID: 9, Kind: session.QuestionAsk, Head: "Which store?", Options: []session.AnswerOption{{Key: "sqlite", Label: "SQLite"}}}
	conv.raise(session.Event{Kind: session.EventQuestion, Question: &q})
	rec.waitFor(t, 5*time.Second, func(_ string, data any) bool { _, ok := data.(appwire.InboxAddedEvent); return ok })
	id := appInboxID(agent.ID, "ask:9")
	until, err := backend.AnswerInbox(id, "sqlite", "keep the reader", "already supported", "")
	if err != nil || until.Before(time.Now().Add(4*time.Second)) {
		t.Fatalf("valid hold = %v %v", until, err)
	}
	select {
	case answer := <-answered:
		t.Fatalf("sent before the hold: %+v", answer)
	default:
	}
	select {
	case answer := <-answered:
		if answer.Key != "sqlite" || answer.Change != "keep the reader\nalready supported" {
			t.Fatalf("sent answer = %+v", answer)
		}
	case <-time.After(appInboxHold + 2*time.Second):
		t.Fatal("the valid held answer was not sent")
	}
	rec.waitFor(t, 5*time.Second, func(_ string, data any) bool { _, ok := data.(appwire.InboxResolvedEvent); return ok })
	if done, counts, err := backend.ListInbox(appwire.InboxDone, ""); err != nil || len(done) != 1 || counts.Waiting != 0 || counts.DoneToday != 1 {
		t.Fatalf("Done = %+v %+v %v", done, counts, err)
	}
}
