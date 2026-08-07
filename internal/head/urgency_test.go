package head

import (
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// Impatience is only impatience over work that is already live. The same words
// with nothing to hurry are a description of the thing being asked for.
func TestUrgencyNeedsBothTheCueAndTheAnchor(t *testing.T) {
	tests := []struct {
		name    string
		message string
		jobs    bool
		cue     string
		fires   bool
	}{
		{"the live failure", "please complete the dinance research fast and give me result immediatly", true, "urgency", true},
		{"asap", "the finance research asap please", true, "urgency", true},
		{"hand me what you have", "give me the results of the finance research now", true, "urgency", true},
		{"deictic hurry", "hurry up with that job", true, "urgency", true},
		{"speed as a requirement", "write a fast json parser", true, "", false},
		{"speed as a requirement over the live job", "make the finance research code faster", true, "", false},
		{"urgent words, no live work", "the finance research asap please", false, "", false},
		{"how long alone is a status question", "how long will the finance research take", true, "", false},
		{"how long with pressure", "how long is the finance research still going to take!", true, "urgency", true},
		{"scope cut keeps its class", "don't bother with the finance appendix, quickly", true, "scope-cut", true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			graph := openHeadStore(t)
			if test.jobs {
				spliceSurgeryJob(t, graph, "finance", "finance research",
					"research the finance question the user asked about")
			}
			intent, fires, err := New(&fakeClient{}, graph).recognizeRedirect(
				store.Message{SessionID: "urgency", Role: store.RoleUser, Body: test.message})
			if err != nil {
				t.Fatal(err)
			}
			if fires != test.fires || intent.Cue != test.cue {
				t.Fatalf("fires/cue = %t/%q, want %t/%q", fires, intent.Cue, test.fires, test.cue)
			}
			if fires && intent.Candidates[0].Node.ID != "finance" {
				t.Fatalf("anchored elsewhere: %+v", intent.Candidates[0].Node)
			}
		})
	}
}

// The failure this whole path exists to end: impatience compiled into a second
// job that queued behind the first, so asking for speed bought delay. It must
// now expedite the live job and call no routing model at all.
func TestImpatienceExpeditesTheLiveJobInsteadOfCompilingASecondOne(t *testing.T) {
	graph := openHeadStore(t)
	spliceSurgeryJob(t, graph, "finance", "finance research",
		"research the finance question the user asked about")
	client := &fakeClient{}
	user, err := graph.PostMessage(store.Message{
		SessionID: "impatient", Role: store.RoleUser,
		Body: "please complete the dinance research fast and give me result immediatly",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := New(client, graph).answer(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	commands, err := graph.PendingCommands(10)
	if err != nil || len(commands) != 1 {
		t.Fatalf("commands = %+v err=%v", commands, err)
	}
	if commands[0].Kind != store.CommandExpedite || commands[0].Target != "finance" ||
		commands[0].Instruction != "please complete the dinance research fast and give me result immediatly" {
		t.Fatalf("expedite command = %+v", commands[0])
	}
	if calls := client.callCount(); calls != 0 {
		t.Fatalf("urgency consulted the routing model %d times", calls)
	}
	// Urgency does not ask. A question spends the one thing the user is short of.
	if questions, err := graph.UnresolvedQuestions(10); err != nil || len(questions) != 0 {
		t.Fatalf("urgency asked: %+v err=%v", questions, err)
	}
}

// The second live message: a correction with none of correction's vocabulary in
// it. "not just X, I want Y" is the user saying what the work is for.
func TestNotJustXIWantYRedirectsTheLiveJob(t *testing.T) {
	graph := openHeadStore(t)
	spliceSurgeryJob(t, graph, "finance", "finance research",
		"research the finance question the user asked about")
	client := &fakeClient{}
	user, err := graph.PostMessage(store.Message{
		SessionID: "corrected", Role: store.RoleUser,
		Body: "not jsut summary i want the answer to the problem we started",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := New(client, graph).answer(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	commands, err := graph.PendingCommands(10)
	if err != nil || len(commands) != 1 || commands[0].Kind != store.CommandRedirect ||
		commands[0].Target != "finance" ||
		commands[0].Instruction != "not jsut summary i want the answer to the problem we started" {
		t.Fatalf("redirect command = %+v err=%v", commands, err)
	}
	if calls := client.callCount(); calls != 0 {
		t.Fatalf("correction consulted the routing model %d times", calls)
	}
}

// Speed asked for as a property of the deliverable is ordinary work, and it
// must reach the ordinary path with the user's words untouched.
func TestSpeedAsARequirementStillCompilesAsNewWork(t *testing.T) {
	graph := openHeadStore(t)
	client := &fakeClient{responses: []string{
		`{"reply":"On it — queued as new work.","command":{"kind":"splice","target":"","instruction":"write a fast json parser"}}`,
	}}
	user, err := graph.PostMessage(store.Message{
		SessionID: "ordinary", Role: store.RoleUser, Body: "write a fast json parser",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := New(client, graph).answer(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	commands, err := graph.PendingCommands(10)
	if err != nil || len(commands) != 1 || commands[0].Kind != store.CommandSplice ||
		commands[0].Instruction != "write a fast json parser" {
		t.Fatalf("ordinary compile = %+v err=%v", commands, err)
	}
}

// The ack that promised acceleration had no mechanism behind it. The router's
// receipt may only describe what the command it emits actually does.
func TestSpliceReceiptIsForbiddenFromPromisingAcceleration(t *testing.T) {
	for _, phrase := range []string{
		"queued and starts when the workforce reaches it",
		"queued behind it",
		"never say you will speed it up",
	} {
		if !strings.Contains(headSystemPrompt, phrase) {
			t.Fatalf("the router prompt no longer constrains the splice receipt: %q", phrase)
		}
	}
}
