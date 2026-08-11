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
			head := New(&fakeClient{}, graph)
			user := store.Message{SessionID: "urgency", Role: store.RoleUser, Body: test.message}
			cue, cued := redirectCue(test.message)
			if !cued {
				cue = ""
			}
			if test.fires && cue != test.cue {
				t.Fatalf("cue = %q, want %q", cue, test.cue)
			}
			active, err := head.activeUserJobs()
			if err != nil {
				t.Fatal(err)
			}
			readings := head.renderHints(user, active)
			// Impatience is the one class that may never ask, so the reading has
			// to hand the loop the job as well as the class: what a person
			// waiting is waiting on is the longest-running thing.
			if test.fires && test.cue == urgencyCue {
				if !strings.Contains(readings, "reads as pressure on delivery") {
					t.Fatalf("impatience did not reach the loop as impatience:\n%s", readings)
				}
				if !strings.Contains(readings, "the longest-running thing is finance research") {
					t.Fatalf("impatience reached the loop with nothing to press:\n%s", readings)
				}
			}
			if !test.fires && strings.Contains(readings, "reads as pressure on delivery") {
				t.Fatalf("a requirement about speed was read as impatience:\n%s", readings)
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
	// One model call, and it is the voice that acknowledges the expedite — never
	// the router, whose only move here would be to compile a second job.
	if calls := client.callCount(); calls != 1 {
		t.Fatalf("urgency made %d model calls, want the single acknowledgement", calls)
	}
	if prompt := client.systemPrompt(); !strings.Contains(prompt, revisionVoicePrompt) {
		t.Fatalf("urgency consulted the routing model: %q", prompt)
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
	if calls := client.callCount(); calls != 1 {
		t.Fatalf("the correction made %d model calls, want the single acknowledgement", calls)
	}
	if prompt := client.systemPrompt(); !strings.Contains(prompt, revisionVoicePrompt) {
		t.Fatalf("correction consulted the routing model: %q", prompt)
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
//
// Which used to be enforced by denying the capability outright — "you have no
// way to make existing work go faster from here" — while the belt's expedite
// tool sitting one arm away made work go faster, and the manual said so. The
// same sentence got opposite answers depending on which arm caught it. So the
// denial is gone and the constraint stayed: the mechanism is named, exactly what
// it does is named, and promising a time is still forbidden.
func TestSpliceReceiptIsForbiddenFromPromisingAcceleration(t *testing.T) {
	for _, phrase := range []string{
		"queued and starts when the workforce reaches it",
		"queued behind it",
		"never a completion time",
		// What the router may claim is bounded by what its own command does.
		// reprioritize raises claim order and nothing else; trimming the
		// unstarted tail is the belt's expedite, and saying otherwise here would
		// be the same dishonesty from the generous side.
		"goes next, ahead of the rest of what is queued",
	} {
		if !strings.Contains(orchestratorPrompt, phrase) {
			t.Fatalf("the router prompt no longer constrains the splice receipt: %q", phrase)
		}
	}
	if strings.Contains(orchestratorPrompt, "no way to make existing work go faster") {
		t.Error("the router still denies a capability the belt exercises")
	}
	// Both prompts tell one story about it, which is the whole of the fix.
	if !strings.Contains(orchestratorPrompt, "expedite makes a job arrive sooner") {
		t.Error("the control prompt lost the capability the router now defers to")
	}
}
