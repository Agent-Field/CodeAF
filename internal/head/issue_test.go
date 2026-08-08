package head

import (
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/resident"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

// theFalseClaim is the sentence the head posted over a store that said the
// opposite. It is written once and asserted against everywhere below, because
// the thing being tested is not a code path — it is that this sentence is never
// visible unless the journal has been made to agree with it.
const theFalseClaim = "Done — it won't fire anymore."

// The reproduced J8 failure, five runs out of five: the person says "stand down
// the stretch reminder", the head emits a cancel with no target, the store
// refuses a targetless cancel, nothing is journaled, and the reply says it
// won't fire anymore while the rule stays active. The head made a claim about
// the world that the store contradicted.
//
// What must happen instead is not an error message. The thread named the thing,
// the store can address it, and the honest outcome is the act: a command row
// against that rule, a receipt that describes what was actually done, and a
// status that has moved by the time the reconciler has run.
func TestStandingDownARuleResolvesTheRuleTheThreadNamed(t *testing.T) {
	graph := openHeadStore(t)
	charter := activateHeadCharter(t, graph, "stretch",
		"Every hour, the stretch reminder fires.")
	client := &fakeClient{responses: []string{
		`{"reply":"` + theFalseClaim + `","command":{"kind":"cancel","target":"","instruction":"stand down the stretch reminder"}}`,
	}}
	user, err := graph.PostMessage(store.Message{
		SessionID: "j8", Role: store.RoleUser, Body: "stand down the stretch reminder",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := New(client, graph).answer(context.Background(), user); err != nil {
		t.Fatal(err)
	}

	commands, err := graph.PendingCommands(0)
	if err != nil || len(commands) != 1 {
		t.Fatalf("commands = %+v err=%v", commands, err)
	}
	if commands[0].Target != charter.ID || commands[0].Kind != store.CommandCharterRetire {
		t.Fatalf("journaled command = %+v, want a retirement of %q", commands[0], charter.ID)
	}
	reply := waitForAgentReply(t, graph, "j8", user.Seq)
	if reply.CommandSeq != commands[0].Seq {
		t.Fatalf("reply is not the receipt for the journaled row: %+v", reply)
	}

	if err := resident.New(graph, nil, nil).Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	settled, found, err := graph.Charter(charter.ID)
	if err != nil || !found || settled.Status != store.CharterRetired {
		t.Fatalf("charter after the reconciler = %+v found=%t err=%v", settled, found, err)
	}
}

// The same shape with two rules a description could equally mean. One plain
// question naming both is the right answer; acting on a coin flip and saying it
// is done is the same lie by another route, so nothing may be journaled here.
func TestStandingDownAnAmbiguousRuleAsksInsteadOfClaiming(t *testing.T) {
	graph := openHeadStore(t)
	activateHeadCharter(t, graph, "stretch", "Every hour, the stretch reminder fires.")
	activateHeadCharter(t, graph, "water", "Every evening, the water reminder fires.")
	client := &fakeClient{responses: []string{
		`{"reply":"` + theFalseClaim + `","command":{"kind":"cancel","target":"","instruction":"stand down the reminder"}}`,
	}}
	user, err := graph.PostMessage(store.Message{
		SessionID: "j8-ambiguous", Role: store.RoleUser, Body: "stand down the reminder",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := New(client, graph).answer(context.Background(), user); err != nil {
		t.Fatal(err)
	}

	reply := waitForAgentReply(t, graph, "j8-ambiguous", user.Seq)
	if !strings.HasPrefix(reply.Body, "Which rule do you mean?") || len(reply.Options) != 2 {
		t.Fatalf("ambiguous reply = %+v", reply)
	}
	assertNothingClaimed(t, graph, "j8-ambiguous", user.Seq)
	commands, err := graph.PendingCommands(0)
	if err != nil || len(commands) != 0 {
		t.Fatalf("an ambiguous stand-down journaled work: %+v err=%v", commands, err)
	}
}

// A job and a standing rule that both answer to the description. The head
// genuinely does not know which was meant, and the two are ranked by machinery
// that shares no scale, so guessing is the one thing it must not do — the
// question carries both, each in the words the person would recognise.
func TestATargetlessStopReachesJobsAndRulesAlike(t *testing.T) {
	graph := openHeadStore(t)
	spliceSurgeryJob(t, graph, "stretch-report", "Stretch goals report", "write the stretch goals report")
	activateHeadCharter(t, graph, "stretch", "Every hour, the stretch reminder fires.")
	client := &fakeClient{responses: []string{
		// Live work puts the control loop in front of the router. It reaches
		// for no tool here, which is how it hands a message back, and the
		// second response is the routing decision under test.
		"nothing I can settle from the board",
		`{"reply":"` + theFalseClaim + `","command":{"kind":"cancel","target":"","instruction":"stand down the stretch one"}}`,
	}}
	user, err := graph.PostMessage(store.Message{
		SessionID: "j8-mixed", Role: store.RoleUser, Body: "stand down the stretch one",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := New(client, graph).answer(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	reply := waitForAgentReply(t, graph, "j8-mixed", user.Seq)
	if !strings.HasPrefix(reply.Body, "Which one do you mean?") || len(reply.Options) != 2 {
		t.Fatalf("mixed reply = %+v", reply)
	}
	var sawJob, sawRule bool
	for _, option := range reply.Options {
		sawJob = sawJob || strings.HasPrefix(option.Value, "surgery:select:")
		sawRule = sawRule || option.Value == "charter:retire:stretch"
	}
	if !sawJob || !sawRule {
		t.Fatalf("the question dropped one half of the world: %+v", reply.Options)
	}
	assertNothingClaimed(t, graph, "j8-mixed", user.Seq)
}

// Nothing to point at. The person still gets a sentence, and the sentence says
// what is true: there is no such thing running.
func TestStandingDownSomethingThatIsNotRunningSaysSoPlainly(t *testing.T) {
	graph := openHeadStore(t)
	client := &fakeClient{responses: []string{
		`{"reply":"` + theFalseClaim + `","command":{"kind":"cancel","target":"","instruction":"stand down the stretch reminder"}}`,
	}}
	user, err := graph.PostMessage(store.Message{
		SessionID: "j8-empty", Role: store.RoleUser, Body: "stand down the stretch reminder",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := New(client, graph).answer(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	reply := waitForAgentReply(t, graph, "j8-empty", user.Seq)
	if reply.Body != noSuchTargetReply {
		t.Fatalf("reply = %q, want the honest miss", reply.Body)
	}
	assertNothingClaimed(t, graph, "j8-empty", user.Seq)
	commands, err := graph.PendingCommands(0)
	if err != nil || len(commands) != 0 {
		t.Fatalf("a miss journaled work: %+v err=%v", commands, err)
	}
}

// The general seam, which is the part that matters more than the case that
// found it. The reply is worded in the same breath as the command it describes,
// so EVERY way a command can be refused is a way for that wording to become a
// lie. These are the refusals that do not resolve to anything: the head has to
// end the turn with a sentence about what it could not do, and it may never end
// it with the sentence the model wrote.
func TestARefusedCommandNeverShipsTheReplyThatAssumedItWorked(t *testing.T) {
	tests := []struct {
		name     string
		response string
		want     string
	}{
		{
			// Rejected before the store: the router's own contract does not
			// know this verb, so there is nothing to journal and nothing to
			// resolve.
			name:     "a verb the head does not have",
			response: `{"reply":"` + theFalseClaim + `","command":{"kind":"obliterate","target":"anything","instruction":"get rid of it"}}`,
			want:     unclearCommandReply,
		},
		{
			// Rejected by the store: well-formed, aimed at work that is not
			// there. This is the refusal that arrives after every check the
			// head can make on its own has passed.
			name:     "work that is not there",
			response: `{"reply":"` + theFalseClaim + `","command":{"kind":"splice","target":"ghost-node","instruction":"carry on with that"}}`,
			want:     commandErrorReply,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			graph := openHeadStore(t)
			user, err := graph.PostMessage(store.Message{
				SessionID: "seam", Role: store.RoleUser, Body: "stand down the stretch reminder",
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := New(&fakeClient{responses: []string{test.response}}, graph).
				answer(context.Background(), user); err != nil {
				t.Fatal(err)
			}
			reply := waitForAgentReply(t, graph, "seam", user.Seq)
			if reply.Body != test.want {
				t.Fatalf("reply = %q, want %q", reply.Body, test.want)
			}
			assertNothingClaimed(t, graph, "seam", user.Seq)
			commands, err := graph.PendingCommands(0)
			if err != nil || len(commands) != 0 {
				t.Fatalf("a refused command journaled work: %+v err=%v", commands, err)
			}
		})
	}
}

// The other refusal the store makes: an unattended reflex is untargeted by
// definition, and one that names something is a field set by mistake, not an
// intention to throw away. Dropping the field is the honest repair; the whole
// decision used to be dropped instead, and the person got the reply anyway.
//
// The consequence gate rides the same door. It cannot be observed here, because
// a consequential reflex is downgraded before validation ever runs — but that
// order is not a property this door may depend on, so it checks again on the
// last line before the row exists.
func TestATargetedReflexDropsTheFieldNotTheIntention(t *testing.T) {
	graph := openHeadStore(t)
	user, err := graph.PostMessage(store.Message{
		SessionID: "gate", Role: store.RoleUser, Body: "tidy up the notes from yesterday",
	})
	if err != nil {
		t.Fatal(err)
	}
	client := &fakeClient{responses: []string{
		`{"reply":"Doing that now.","command":{"kind":"reflex","target":"whatever","instruction":"tidy the notes"}}`,
	}}
	if err := New(client, graph).answer(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	commands, err := graph.PendingCommands(0)
	if err != nil || len(commands) != 1 {
		t.Fatalf("commands = %+v err=%v", commands, err)
	}
	if !commands[0].Reflex || commands[0].Target != "" {
		t.Fatalf("journaled command = %+v, want an untargeted reflex", commands[0])
	}
	reply := waitForAgentReply(t, graph, "gate", user.Seq)
	if reply.CommandSeq != commands[0].Seq {
		t.Fatalf("reply is not the receipt for the journaled row: %+v", reply)
	}
}

// A command the model forgot to write words for is still an intention it said
// out loud. The person's own sentence is what it was about, and journaling that
// is what makes the reply beside it true.
func TestACommandWithNoWordsCarriesThePersonsOwn(t *testing.T) {
	graph := openHeadStore(t)
	user, err := graph.PostMessage(store.Message{
		SessionID: "wordless", Role: store.RoleUser, Body: "write up the migration notes",
	})
	if err != nil {
		t.Fatal(err)
	}
	client := &fakeClient{responses: []string{
		`{"reply":"On it.","command":{"kind":"splice","target":"","instruction":""}}`,
	}}
	if err := New(client, graph).answer(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	commands, err := graph.PendingCommands(0)
	if err != nil || len(commands) != 1 || commands[0].Instruction != user.Body {
		t.Fatalf("commands = %+v err=%v", commands, err)
	}
	reply := waitForAgentReply(t, graph, "wordless", user.Seq)
	if reply.Body != "On it." || reply.CommandSeq != commands[0].Seq {
		t.Fatalf("reply = %+v, want the receipt for the journaled row", reply)
	}
}

// assertNothingClaimed is the whole point of this file expressed once: whatever
// the head said, it did not say the thing the model wrote as though the change
// had already happened.
func assertNothingClaimed(t *testing.T, graph *store.Store, sessionID string, afterSeq int64) {
	t.Helper()
	messages, err := graph.Messages(sessionID, afterSeq, 0)
	if err != nil {
		t.Fatalf("list replies: %v", err)
	}
	for _, message := range messages {
		if strings.Contains(message.Body, theFalseClaim) {
			t.Fatalf("the thread carries a claim the store contradicts: %q", message.Body)
		}
	}
}
