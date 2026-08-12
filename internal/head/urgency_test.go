package head

import (
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// Impatience is only impatience over work that is already live. The same words
// with nothing to hurry are a description of the thing being asked for. The
// reading is evidence now rather than a terminal answer, so the law is stated
// against what the loop is handed; the lane it points at is expedite, which is
// asserted separately and still never queues anything.
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
		// The cue still reads, and with nothing live it anchors on nothing — which
		// is the whole difference between a reading and a decision.
		{"urgent words, no live work", "the finance research asap please", false, "urgency", false},
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
			user := postUser(t, graph, "urgency", test.message)
			cue, fires := redirectReading(t, head, user)
			if fires != test.fires || cue != test.cue {
				t.Fatalf("fires/cue = %t/%q, want %t/%q", fires, cue, test.fires, test.cue)
			}
			if !fires {
				return
			}
			active, err := head.activeUserJobs()
			if err != nil {
				t.Fatal(err)
			}
			reading := head.renderHints(user, active)
			if !strings.Contains(reading, "finance") {
				t.Fatalf("anchored elsewhere:\n%s", reading)
			}
			if test.cue != urgencyCue {
				return
			}
			// Which job impatience is about is the one thing no board read
			// answers, so the reading still works it out: the longest-running
			// thing, with started work outranking work that has not.
			if !strings.Contains(reading, "the longest-running thing is finance research") {
				t.Fatalf("the reading does not name what the person is waiting on:\n%s", reading)
			}
			if !strings.Contains(reading, "never queue a second job for it") {
				t.Fatalf("the reading does not forbid the second job:\n%s", reading)
			}
		})
	}
}

// The failure this whole path exists to end: impatience compiled into a second
// job that queued behind the first, so asking for speed bought delay. It must
// expedite the live job, and expedite is a lane that cannot queue work even if
// the model asks it to.
func TestImpatienceExpeditesTheLiveJobInsteadOfCompilingASecondOne(t *testing.T) {
	graph := openHeadStore(t)
	spliceSurgeryJob(t, graph, "finance", "finance research",
		"research the finance question the user asked about")
	const ask = "please complete the dinance research fast and give me result immediatly"
	client := &beltClient{turns: []beltTurn{
		{calls: []ai.ToolCall{beltCall("c1", beltToolExpedite, map[string]any{"job": "finance"})}},
		{text: ""},
	}}
	user := postUser(t, graph, "impatient", ask)
	if err := New(client, graph).answer(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	commands, err := graph.PendingCommands(10)
	if err != nil || len(commands) != 1 {
		t.Fatalf("commands = %+v err=%v", commands, err)
	}
	if commands[0].Kind != store.CommandExpedite || commands[0].Target != "finance" ||
		commands[0].Instruction != ask {
		t.Fatalf("expedite command = %+v", commands[0])
	}
	// The lane itself is the guarantee: nothing about it can splice, so the
	// second job cannot be queued by a model that misreads the sentence.
	for _, command := range commands {
		if command.Kind == store.CommandSplice {
			t.Fatalf("impatience bought a second job: %+v", command)
		}
	}
	// Urgency does not ask. A question spends the one thing the user is short of.
	if messages, err := graph.Messages("impatient", user.Seq, 0); err != nil {
		t.Fatal(err)
	} else {
		for _, message := range messages {
			if len(message.Options) > 0 {
				t.Fatalf("urgency asked: %+v", message)
			}
		}
	}
}

// The second live message: a correction with none of correction's vocabulary in
// it. "not just X, I want Y" is the user saying what the work is for.
func TestNotJustXIWantYRedirectsTheLiveJob(t *testing.T) {
	graph := openHeadStore(t)
	spliceSurgeryJob(t, graph, "finance", "finance research",
		"research the finance question the user asked about")
	const ask = "not jsut summary i want the answer to the problem we started"
	client := &beltClient{turns: []beltTurn{
		{calls: []ai.ToolCall{beltCall("c1", beltToolRevise, map[string]any{
			"job": "finance", "words": ask})}},
		{text: ""},
	}}
	user := postUser(t, graph, "corrected", ask)
	if err := New(client, graph).answer(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	commands, err := graph.PendingCommands(10)
	if err != nil || len(commands) != 1 || commands[0].Kind != store.CommandRedirect ||
		commands[0].Target != "finance" || commands[0].Instruction != ask {
		t.Fatalf("redirect command = %+v err=%v", commands, err)
	}
	// The sentence borrows none of the job's words, so the deictic arm is the
	// only thing that could have found it — and it has to be in the prompt.
	if opening := client.openingPrompt(); !strings.Contains(opening,
		"points at work already underway without naming it") {
		t.Fatalf("the loop was given nothing to resolve the referent with:\n%s", opening)
	}
}

// Speed asked for as a property of the deliverable is ordinary work, and it
// must reach the ordinary path with the user's words untouched.
func TestSpeedAsARequirementStillCompilesAsNewWork(t *testing.T) {
	graph := openHeadStore(t)
	const ask = "write a fast json parser"
	client := &beltClient{turns: []beltTurn{
		{calls: []ai.ToolCall{beltCall("c1", beltToolSpawn, map[string]any{"instruction": ask})}},
		{text: ""},
	}}
	user := postUser(t, graph, "ordinary", ask)
	head := New(client, graph)
	// Nothing about this sentence is a reading at all: no cue, no anchor, no
	// deixis. The hint block is absent byte for byte, which is what keeps an
	// ordinary request as cheap as it was before any of this existed.
	if reading := head.renderHints(user, nil); reading != "" {
		t.Fatalf("ordinary work bought a reading:\n%s", reading)
	}
	if err := head.answer(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	commands, err := graph.PendingCommands(10)
	if err != nil || len(commands) != 1 || commands[0].Kind != store.CommandSplice ||
		commands[0].Instruction != ask {
		t.Fatalf("ordinary compile = %+v err=%v", commands, err)
	}
}

// The ack that promised acceleration had no mechanism behind it. A receipt may
// only describe what the command it emits actually does.
//
// This used to be enforced twice, in two prompts that disagreed: the router was
// told it had "no way to make existing work go faster" while the belt's expedite
// tool one arm away made work go faster, so the same sentence got opposite
// answers depending on which arm caught it. There is one prompt now, and the
// constraint survives inside it: the mechanism is named, what it does is named,
// and promising a time is still forbidden.
//
// The clause this used to pin — "say the new work is queued behind it" — was a
// promise in the other direction, and 13.6 measured it false: the runner claims
// every ready leaf it has a slot for, so a second job commissioned while a first
// one runs starts beside it rather than after it. The receipt is still forbidden
// to promise a time; what it may no longer do is invent a queue.
func TestSpliceReceiptIsForbiddenFromPromisingAcceleration(t *testing.T) {
	for _, phrase := range []string{
		"Never say a job is waiting its turn unless a board row you read this turn says it is queued",
		"never that it is done, never a completion time",
	} {
		if !strings.Contains(orchestratorPrompt, phrase) {
			t.Fatalf("the prompt no longer constrains the splice receipt: %q", phrase)
		}
	}
	if strings.Contains(orchestratorPrompt, "no way to make existing work go faster") {
		t.Error("the prompt denies a capability the belt exercises")
	}
	// The capability the denial used to contradict, stated once and stated
	// exactly: sooner, trimmed, and never larger.
	if !strings.Contains(orchestratorPrompt, "expedite (sooner, never different)") {
		t.Error("the prompt no longer names the lane that does make work arrive sooner")
	}
	expedite := ""
	for _, definition := range beltDefinitions() {
		if definition.Function.Name == beltToolExpedite {
			expedite = definition.Function.Description
		}
	}
	if !strings.Contains(expedite, "It never adds work.") {
		t.Errorf("the expedite tool no longer promises only what it does:\n%s", expedite)
	}
}
