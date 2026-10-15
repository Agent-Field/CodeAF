package head

import (
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/store"
)

// theFalseClaim is the sentence the head posted over a store that said the
// opposite. It is written once and asserted against everywhere below, because
// the thing being tested is not a code path — it is that this sentence is never
// visible unless the journal has been made to agree with it.
const theFalseClaim = "Done — it won't fire anymore."

// Nothing to point at. The person still gets a sentence, and the sentence says
// what is true: there is no such thing running. The tool is what makes that
// answer available — it reports the honest miss, and only then can the loop say
// it.
func TestStandingDownSomethingThatIsNotRunningSaysSoPlainly(t *testing.T) {
	graph := openHeadStore(t)
	user := postUser(t, graph, "j8-empty", "stand down the stretch reminder")

	run := &beltRun{head: New(nil, graph), user: user}
	missed, failed := run.execute(beltToolStop, beltArguments(t, map[string]any{
		"words": user.Body}))
	if failed || !strings.Contains(missed, "nothing on the board matches those words") {
		t.Fatalf("the miss was not reported plainly: %q", missed)
	}
	if run.acted {
		t.Fatal("a miss acted on something")
	}

	head, _ := beltHead(graph,
		beltTurn{calls: []ai.ToolCall{beltCall("c1", beltToolStop, map[string]any{
			"words": user.Body})}},
		beltTurn{text: noSuchTargetReply})
	if err := head.answer(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	reply := waitForAgentReply(t, graph, "j8-empty", user.Seq)
	if reply.Body != noSuchTargetReply {
		t.Fatalf("reply = %q, want the honest miss", reply.Body)
	}
	if reply.CommandSeq != 0 {
		t.Fatalf("a miss carried a command seq: %+v", reply)
	}
	assertNothingClaimed(t, graph, "j8-empty", user.Seq)
	commands, err := graph.PendingCommands(0)
	if err != nil || len(commands) != 0 {
		t.Fatalf("a miss journaled work: %+v err=%v", commands, err)
	}
}

// The general seam, which is the part that matters more than the case that
// found it. The reply used to be worded in the same breath as the command it
// described, so every way a command could be refused was a way for that wording
// to become a lie. The wording is no longer written in advance: an act that
// cannot be carried out returns an ERROR the loop has to answer to, nothing is
// journaled, and the run never records having acted — so there is no receipt to
// ship and nothing for a fallback summary to claim.
func TestARefusedCommandNeverShipsTheReplyThatAssumedItWorked(t *testing.T) {
	tests := []struct {
		name string
		tool string
		args map[string]any
		want string
	}{
		{
			// Rejected before the store: a withdrawal with nothing to aim at and
			// no words to look one up with. Nothing is journaled, nothing is
			// resolved, and the loop has to speak to the refusal.
			name: "a withdrawal with nothing to aim at",
			tool: beltToolStop,
			args: map[string]any{"targets": []string{}},
			want: "targets must name at least one id from a read",
		},
		{
			// Rejected by the store's own reality: well-formed, aimed at work that
			// is not there. Ids come from reads, and one that came from anywhere
			// else fails at the door rather than after the reply.
			name: "work that is not there",
			tool: beltToolChange,
			args: map[string]any{"target": "ghost-node", "words": "stop"},
			want: `no learned way of working is called "ghost-node"`,
		},
		{
			// The same door on the commissioning side: work that follows on from
			// something that does not exist is not queued as work that follows on
			// from nothing.
			name: "continuing work that is not there",
			tool: beltToolTask,
			args: map[string]any{"instruction": "carry on with that", "after": "ghost-node"},
			want: `there is no work of the user's with id "ghost-node"`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			graph := openHeadStore(t)
			user := postUser(t, graph, "seam", "stand down the stretch reminder")
			run := &beltRun{head: New(nil, graph), user: user}
			result, failed := run.execute(test.tool, beltArguments(t, test.args))
			if !failed {
				t.Fatalf("a refused act reported success: %q", result)
			}
			if !strings.Contains(result, test.want) {
				t.Fatalf("refusal = %q, want it to say %q", result, test.want)
			}
			if run.acted || run.commandSeq != 0 {
				t.Fatalf("a refused act recorded a receipt: acted=%t seq=%d", run.acted, run.commandSeq)
			}
			if run.summary() != "Done." {
				t.Fatalf("a refused act left something for the reply to claim: %q", run.summary())
			}
			commands, err := graph.PendingCommands(0)
			if err != nil || len(commands) != 0 {
				t.Fatalf("a refused command journaled work: %+v err=%v", commands, err)
			}
		})
	}
}

// The consequence gate, at the door where it can still be true. An unattended
// reflex is untargeted by definition and never consequential; a flag set on
// either kind of sentence is dropped rather than honoured, and the intention it
// was set on is journaled anyway, in the person's own words.
func TestATargetedReflexDropsTheFieldNotTheIntention(t *testing.T) {
	graph := openHeadStore(t)
	spliceSurgeryJob(t, graph, "notes", "Yesterday's notes", "collect yesterday's notes")
	user := postUser(t, graph, "gate", "tidy up the notes from yesterday")
	run := &beltRun{head: New(nil, graph), user: user}
	result, failed := run.execute(beltToolTask, beltArguments(t, map[string]any{
		"instruction": "tidy the notes", "reflex": true, "after": "notes"}))
	if failed {
		t.Fatalf("a targeted reflex was refused outright: %s", result)
	}
	commands, err := graph.PendingCommands(0)
	if err != nil || len(commands) != 1 {
		t.Fatalf("commands = %+v err=%v", commands, err)
	}
	if commands[0].Reflex || commands[0].Target != "notes" || commands[0].Instruction != "tidy the notes" {
		t.Fatalf("journaled command = %+v, want ordinary work carrying the same words", commands[0])
	}

	// The other half of the same door, on the same last line: words that spend,
	// send or delete are never a reflex however the flag arrived.
	spending := postUser(t, graph, "gate", "buy the tickets")
	consequential := &beltRun{head: New(nil, graph), user: spending}
	if result, failed := consequential.execute(beltToolTask, beltArguments(t, map[string]any{
		"instruction": "buy the tickets", "reflex": true})); failed {
		t.Fatalf("consequential work was dropped instead of downgraded: %s", result)
	}
	commands, err = graph.PendingCommands(0)
	if err != nil || len(commands) != 2 {
		t.Fatalf("commands = %+v err=%v", commands, err)
	}
	if commands[1].Reflex || commands[1].Instruction != "buy the tickets" {
		t.Fatalf("a consequential reflex survived the gate: %+v", commands[1])
	}
}

// A command the model forgot to write words for is still an intention it said
// out loud. The person's own sentence is what it was about, and journaling that
// is what makes the reply beside it true. Commissioning is the one act that
// refuses instead: new work with no words is not new work, and inventing the
// brief is worse than saying so.
func TestACommandWithNoWordsCarriesThePersonsOwn(t *testing.T) {
	graph := openHeadStore(t)
	spliceSurgeryJob(t, graph, "migration", "Schema migration", "migrate the schema")
	user := postUser(t, graph, "wordless", "make it cover the rollback too")

	run := &beltRun{head: New(nil, graph), user: user}
	if result, failed := run.execute(beltToolChange, beltArguments(t, map[string]any{
		"target": "migration"})); failed {
		t.Fatalf("a wordless revision was refused: %s", result)
	}
	commands, err := graph.PendingCommands(0)
	if err != nil || len(commands) != 1 || commands[0].Instruction != user.Body {
		t.Fatalf("commands = %+v err=%v", commands, err)
	}
	if !run.acted || run.commandSeq != commands[0].Seq {
		t.Fatalf("the receipt is not tied to the journaled row: acted=%t seq=%d", run.acted, run.commandSeq)
	}

	empty := &beltRun{head: New(nil, graph), user: user}
	result, failed := empty.execute(beltToolTask, beltArguments(t, map[string]any{}))
	if !failed || !strings.Contains(result, "instruction must carry the user's own words") {
		t.Fatalf("wordless new work was invented rather than refused: %q", result)
	}
	if commands, _ := graph.PendingCommands(0); len(commands) != 1 {
		t.Fatalf("wordless new work journaled something: %+v", commands)
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
