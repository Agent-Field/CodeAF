package session

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/decide"
	"github.com/Agent-Field/codeaf/internal/placegraph"
)

// A LIVE SESSION USES THE PLACES IT WAS FILED IN. The hook tests install a
// fake graph. These follow the door a desktop conversation actually gets:
// the place graph on the config, the ledger beside it, and the chat filed
// under the session's own id.

func TestDecidingPlaceAnswersGoTestWithoutSurfacing(t *testing.T) {
	f := newPlaceFixture(t)
	release := f.place(t, "Release", placegraph.Context{})
	if err := PrepareDecideLedger(f.path); err != nil {
		t.Fatal(err)
	}
	ledger := DecideLedgerDir(f.path)
	store := decidingGoTestStore(t, ledger, release.ID)
	agent := wiredChat(t, f)
	f.file(t, agent.id, release)

	letGo := agent.presenceAskingWhole(goTestPermission(7), nil)
	letGo()
	if deskRows(agent) != 0 {
		t.Fatalf("the place answered and the question was still shown: %d", deskRows(agent))
	}
	if agent.NeedsPerson() {
		t.Fatal("needs you stayed up after the place answered")
	}
	if len(agent.OpenQuestions()) != 0 {
		t.Fatalf("open questions = %+v", agent.OpenQuestions())
	}
	records := agent.Decisions()
	if len(records) != 1 || records[0].By != DecidedByDial || strings.Join(records[0].Picked, ",") != "1" {
		t.Fatalf("session record = %+v", records)
	}
	if !strings.Contains(records[0].Why, "You allowed go test here 6 times") {
		t.Fatalf("why = %q", records[0].Why)
	}
	listed, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	var taken *decide.Decision
	for i := range listed {
		if listed[i].By == release.ID && listed[i].Command == "go test" && listed[i].QuestionRef.ID == "7" {
			taken = &listed[i]
		}
	}
	if taken == nil || !strings.Contains(taken.Because, "You allowed go test here 6 times") || taken.QuestionRef.Session != agent.id {
		t.Fatalf("ledger = %+v", listed)
	}

	t.Run("a chat in no place still asks", func(t *testing.T) {
		// A fresh graph, so this chat's id is not the one the parent filed.
		// The place is just as sure. It still cannot see a chat it does not hold.
		alone := newPlaceFixture(t)
		place := alone.place(t, "Release", placegraph.Context{})
		decidingGoTestStore(t, DecideLedgerDir(alone.path), place.ID)
		other := wiredChat(t, alone)
		letGo := other.presenceAskingWhole(goTestPermission(7), nil)
		defer letGo()
		if deskRows(other) != 1 {
			t.Fatalf("an unfiled chat hid the question: desk %d", deskRows(other))
		}
		if len(other.Decisions()) != 0 {
			t.Fatalf("an unfiled chat was decided: %+v", other.Decisions())
		}
	})
}

func TestAPersonAnswerRecordsTheLearningOutcome(t *testing.T) {
	f := newPlaceFixture(t)
	release := f.place(t, "Release", placegraph.Context{})
	ledger := DecideLedgerDir(f.path)
	store, err := decide.Open(ledger, release.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Append(goTestAllow(release.ID, "seed")); err != nil {
		t.Fatal(err)
	}
	agent := wiredChat(t, f)
	f.file(t, agent.id, release)

	letGo := agent.presenceAskingWhole(goTestPermission(7), nil)
	agent.presence.mu.Lock()
	full := agent.presence.asks[0].question.Full
	agent.presence.mu.Unlock()
	if full == nil || full.Pick == nil || full.Pick.Key != "1" || full.Proposal == nil || full.Proposal.Place != "Release" {
		t.Fatalf("learning did not propose: %+v", full)
	}
	if err := agent.ResolveQuestion(Answer{Kind: QuestionConsent, ID: 7, Key: "1"}); err != nil {
		t.Fatal(err)
	}
	letGo()

	key := decide.KindKey(string(AskPermission), "shell-read")
	st, err := store.Mode(key)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode != decide.ModeLearning || decide.Agreements(st) < 1 || len(st.Recent) < 1 || !st.Recent[0].Agreed {
		t.Fatalf("learning ring = %+v", st)
	}
	if !ledgerHasPersonAnswer(t, store, agent.id, "7", "allow once") {
		t.Fatal("the person's allow was not kept for the next score")
	}

	letGo = agent.presenceAskingWhole(goTestPermission(8), nil)
	if err := agent.ResolveQuestion(Answer{Kind: QuestionConsent, ID: 8, Key: "3"}); err != nil {
		t.Fatal(err)
	}
	letGo()
	st, err = store.Mode(key)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode != decide.ModeLearning || len(st.Recent) < 2 || st.Recent[len(st.Recent)-1].Agreed {
		t.Fatalf("a denial was stored as agreement: %+v", st)
	}

	t.Run("a knows line proposes when there is no history", func(t *testing.T) {
		fresh := newPlaceFixture(t)
		place := fresh.place(t, "Release", placegraph.Context{})
		if _, _, err := fresh.store.AddLine(placegraph.Line{PlaceID: place.ID, Text: "Run go test before a release", Source: placegraph.LineSource{Kind: placegraph.LineYouWrote}}); err != nil {
			t.Fatal(err)
		}
		chat := wiredChat(t, fresh)
		fresh.file(t, chat.id, place)
		hold := chat.presenceAskingWhole(goTestPermission(7), nil)
		defer hold()
		chat.presence.mu.Lock()
		shown := chat.presence.asks[0].question.Full
		chat.presence.mu.Unlock()
		if shown == nil || shown.Pick == nil || shown.Pick.Key != "1" || shown.Pick.Percent != 92 || !strings.Contains(shown.Pick.Reason, "matches what Release knows") {
			t.Fatalf("knows proposal = %+v", shown)
		}
	})
}

func wiredChat(t *testing.T, f placeFixture) *Agent {
	t.Helper()
	chatDir := filepath.Join(t.TempDir(), "chat")
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
		c.Place.Dir = chatDir
		c.SessionFile = filepath.Join(chatDir, "session.jsonl")
		c.PlaceGraph = &PlaceGraphDoor{Path: f.path, ChoicesPath: f.choices, Sources: placegraph.SourcePolicy{Deny: []string{}}}
	})
	if agent.id != "chat" {
		t.Fatalf("session id = %q, want the folder name", agent.id)
	}
	return agent
}

func decidingGoTestStore(t *testing.T, ledger, placeID string) *decide.Store {
	t.Helper()
	store, err := decide.Open(ledger, placeID, nil)
	if err != nil {
		t.Fatal(err)
	}
	key := decide.KindKey(string(AskPermission), "shell-read")
	if err := store.SetMode(key, decide.ModeDeciding); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 6; i++ {
		row := goTestAllow(placeID, "prior-"+string(rune('a'+i)))
		if err := store.Append(row); err != nil {
			t.Fatal(err)
		}
	}
	return store
}

func goTestAllow(placeID, id string) decide.Decision {
	return decide.Decision{
		ID: id, PlaceID: placeID,
		AskKind: string(AskPermission), Subject: "shell-read", Command: "go test",
		Action: "allow once", By: "person", Reversible: true, At: time.Now(),
	}
}

func goTestPermission(id uint64) Question {
	q := wellFormed()
	q.ID = id
	q.Head = "needs your ok to run bash"
	q.Stakes = StakesCostly
	q.Attach = []Block{{Kind: BlockCode, Title: "Command · this request only", Body: "go test ./..."}}
	return q
}

func ledgerHasPersonAnswer(t *testing.T, store *decide.Store, chatID, token, action string) bool {
	t.Helper()
	listed, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range listed {
		if row.By == string(DecidedByPerson) && row.Command == "go test" && row.Action == action &&
			row.QuestionRef.Session == chatID && row.QuestionRef.ID == token {
			return true
		}
	}
	return false
}
