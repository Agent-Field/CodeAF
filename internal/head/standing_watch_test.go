package head

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

func TestStandingWatchQuestionChoicesBecomeGlobalCommandsWithoutProviderCall(t *testing.T) {
	for _, test := range []struct {
		reply string
		kind  store.CommandKind
	}{
		{reply: "yes", kind: store.CommandStandingWatchEnable},
		{reply: "no", kind: store.CommandStandingWatchDecline},
	} {
		t.Run(test.reply, func(t *testing.T) {
			graph := openHeadStore(t)
			charter, err := store.NewCharter("watch-choice", "Keep releases documented.", store.WatchSpec{
				Kind: store.WatchPoll, Poll: &store.PollWatch{Condition: "look for a release", Cadence: time.Hour},
			}, "Did a release land?", store.CharterAction{Template: "Update the release notes"},
				store.CharterRails{PerFiringBudgetUSD: 0.1, MaxFiringsPerDay: 3}, store.CharterActive,
				store.Ratification{Origin: store.OriginUser, SessionID: "watch-head", Evidence: "yes"})
			if err != nil {
				t.Fatal(err)
			}
			if err := graph.CreateCharter(charter); err != nil {
				t.Fatal(err)
			}
			if posted, err := graph.OfferStandingWatch("watch-head", charter.ID); err != nil || !posted {
				t.Fatalf("offer posted=%t err=%v", posted, err)
			}
			user, err := graph.PostMessage(store.Message{SessionID: "watch-head", Role: store.RoleUser, Body: test.reply})
			if err != nil {
				t.Fatal(err)
			}
			client := &fakeClient{}
			if err := New(client, graph).answer(context.Background(), user); err != nil {
				t.Fatal(err)
			}
			if client.callCount() != 0 {
				t.Fatal("standing-watch choice fell through to provider")
			}
			commands, err := graph.PendingCommands(0)
			if err != nil || len(commands) != 1 || commands[0].Kind != test.kind || commands[0].Target != "" {
				t.Fatalf("commands = %+v err=%v", commands, err)
			}
		})
	}
}

func TestUnrelatedReplyNeverSpendsTheSingleStandingWatchQuestion(t *testing.T) {
	graph := openHeadStore(t)
	charter, err := store.NewCharter("watch-freetext", "Keep releases documented.", store.WatchSpec{
		Kind: store.WatchPoll, Poll: &store.PollWatch{Condition: "look for a release", Cadence: time.Hour},
	}, "Did a release land?", store.CharterAction{Template: "Update the release notes"},
		store.CharterRails{PerFiringBudgetUSD: 0.1, MaxFiringsPerDay: 3}, store.CharterActive,
		store.Ratification{Origin: store.OriginUser, SessionID: "watch-head", Evidence: "yes"})
	if err != nil {
		t.Fatal(err)
	}
	if err := graph.CreateCharter(charter); err != nil {
		t.Fatal(err)
	}
	if posted, err := graph.OfferStandingWatch("watch-head", charter.ID); err != nil || !posted {
		t.Fatalf("offer posted=%t err=%v", posted, err)
	}
	questions, err := graph.UnresolvedQuestions(10)
	if err != nil || len(questions) != 1 {
		t.Fatalf("unresolved questions = %+v err=%v", questions, err)
	}
	user, err := graph.PostMessage(store.Message{
		SessionID: "watch-head", Role: store.RoleUser, Body: "what does that actually change?",
	})
	if err != nil {
		t.Fatal(err)
	}
	client := &fakeClient{responses: []string{
		`{"reply":"It decides whether checks continue with no terminal open.","command":null}`,
	}}
	if err := New(client, graph).answer(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	commands, err := graph.PendingCommands(0)
	if err != nil || len(commands) != 0 {
		t.Fatalf("free text produced commands = %+v err=%v", commands, err)
	}
	decision, err := graph.StandingWatchDecisionState()
	if err != nil || decision != store.StandingWatchOffered {
		t.Fatalf("decision = %q err=%v", decision, err)
	}
	still, err := graph.UnresolvedQuestions(10)
	if err != nil || len(still) != 1 {
		t.Fatalf("standing-watch question was spent by free text: %+v err=%v", still, err)
	}
}

// TestStandingWatchIsABeltReadNotAPhraseList is the values assertion for the
// deletion. Two substring gates decided whether the measured competence map and
// the standing-watch status reached the prompt at all — twelve phrases and
// fourteen — and neither list contained "am I asking too much of you lately?"
// or any of the thousand other ways a person asks it. The law lives in the tool
// description now; the gates must stay gone rather than come back one phrase at
// a time, so this test asserts about the package rather than about a sentence.
func TestStandingWatchIsABeltReadNotAPhraseList(t *testing.T) {
	source := readHeadSource(t, "head.go")
	for _, gate := range []string{"asksForCompetence", "asksForStandingWatch"} {
		if strings.Contains(source, "func "+gate) {
			t.Errorf("%s is back: presence is a substring match again", gate)
		}
	}
	// And what replaced them is reachable: both reads are on the belt the loop
	// is handed, described well enough that the model knows when to spend a call.
	named := map[string]bool{}
	for _, definition := range beltDefinitions() {
		named[definition.Function.Name] = true
		if definition.Function.Name == beltToolCompetence || definition.Function.Name == beltToolStanding {
			if len(definition.Function.Description) < 80 {
				t.Errorf("%s has no description worth reading", definition.Function.Name)
			}
		}
	}
	for _, tool := range []string{beltToolCompetence, beltToolStanding, beltToolSpending} {
		if !named[tool] {
			t.Errorf("the %s read never reached the belt", tool)
		}
	}
}

// The reads themselves: what the surface registered is what the model is handed,
// byte-capped, and a surface that registered nothing says so rather than erroring.
func TestSelfReadsReturnTheirGroundTruthAndRecordNothing(t *testing.T) {
	graph := openHeadStore(t)
	head := New(nil, graph).
		WithCompetenceMap(func() string {
			return `- {"scope":"tool:go","class":"strong","samples":8,"failure_rate":0}`
		}).
		WithStandingWatch(func() string {
			return "standing watch   installed · last wake 2m ago · next check in 3m\n" +
				"active charters (id · cadence · what it watches for):\n" +
				"charter-1 · every weekday at 9 · keep the release notes current"
		}).
		WithDailyBudgetUSD(20)
	run := &beltRun{head: head, user: store.Message{SessionID: "self", Body: "how are you doing lately?"}}

	competence, failed := run.competence()
	if failed || !strings.Contains(competence, `"scope":"tool:go"`) {
		t.Fatalf("competence read failed=%t: %s", failed, competence)
	}
	standing, failed := run.standing()
	if failed || !strings.Contains(standing, "next check in 3m") ||
		!strings.Contains(standing, "keep the release notes current") {
		t.Fatalf("standing read failed=%t: %s", failed, standing)
	}
	spending, failed := run.spending(nil)
	if failed || !strings.Contains(spending, "daily rail") ||
		!strings.Contains(spending, "your own upkeep today: $0.00") {
		t.Fatalf("spending read failed=%t: %s", failed, spending)
	}
	if run.acted {
		t.Fatal("reading the employee's own account recorded an action")
	}

	bare := &beltRun{head: New(nil, graph), user: run.user}
	if answer, failed := bare.competence(); failed || strings.Contains(answer, "error") {
		t.Fatalf("an unregistered competence map errored instead of answering: %s", answer)
	}
	if answer, failed := bare.standing(); failed || strings.Contains(answer, "error") {
		t.Fatalf("an unregistered watch status errored instead of answering: %s", answer)
	}
}

func readHeadSource(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(raw)
}
