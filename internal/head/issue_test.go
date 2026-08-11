package head

import (
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/resident"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
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
// against that rule, a receipt tied to that row, and a status that has moved by
// the time the reconciler has run. The verb no longer arrives as a router's
// terminal decision — it is the rule tool, which resolves the description
// against the standing rules themselves — and the sentence is allowed on screen
// for exactly one reason: the row underneath it makes it true.
func TestStandingDownARuleResolvesTheRuleTheThreadNamed(t *testing.T) {
	graph := openHeadStore(t)
	charter := activateHeadCharter(t, graph, "stretch",
		"Every hour, the stretch reminder fires.")
	user := postUser(t, graph, "j8", "stand down the stretch reminder")
	head, _ := beltHead(graph,
		beltTurn{calls: []ai.ToolCall{beltCall("c1", beltToolRule, map[string]any{
			"verb": "retire", "describes": user.Body})}},
		beltTurn{text: theFalseClaim})
	if err := head.answer(context.Background(), user); err != nil {
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
	if reply.Body != theFalseClaim {
		t.Fatalf("reply = %q, want the claim the row underneath it makes true", reply.Body)
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
//
// The question is no longer posted by the recognizer that failed to choose — the
// tool hands back the rules the words reach and refuses to pick, and the ask
// tool posts the durable numbered question. Both halves of the old law survive:
// the candidates are named the way the person would recognise them, and the
// journal is empty until they answer.
func TestStandingDownAnAmbiguousRuleAsksInsteadOfClaiming(t *testing.T) {
	graph := openHeadStore(t)
	activateHeadCharter(t, graph, "stretch", "Every hour, the stretch reminder fires.")
	activateHeadCharter(t, graph, "water", "Every evening, the water reminder fires.")
	user := postUser(t, graph, "j8-ambiguous", "stand down the reminder")
	head, _ := beltHead(graph,
		beltTurn{calls: []ai.ToolCall{beltCall("c1", beltToolRule, map[string]any{
			"verb": "retire", "describes": user.Body})}},
		beltTurn{calls: []ai.ToolCall{beltCall("c2", beltToolAsk, map[string]any{
			"question": "Which rule do you mean?",
			"options": []string{"Every hour, the stretch reminder fires.",
				"Every evening, the water reminder fires."}})}})
	if err := head.answer(context.Background(), user); err != nil {
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

	// And the refusal the loop was handed says why, with both rules in the words
	// they were ratified in — the model cannot ask well about rules it was never
	// shown.
	run := &beltRun{head: New(nil, graph), user: user}
	candidates, failed := run.execute(beltToolRule, beltArguments(t, map[string]any{
		"verb": "retire", "describes": user.Body}))
	if failed {
		t.Fatalf("an ambiguous rule edit errored instead of offering candidates: %s", candidates)
	}
	if !strings.Contains(candidates, "more than one standing rule matches") ||
		!strings.Contains(candidates, "stretch reminder") || !strings.Contains(candidates, "water reminder") {
		t.Fatalf("the candidate list is not both rules by name:\n%s", candidates)
	}
	if run.acted {
		t.Fatal("offering candidates journaled something")
	}
}

// A job and a standing rule that both answer to the description. The head
// genuinely does not know which was meant, and the two are ranked by machinery
// that shares no scale, so guessing is the one thing it must not do — one
// description reaches both halves of the world, and both come back.
func TestATargetlessStopReachesJobsAndRulesAlike(t *testing.T) {
	graph := openHeadStore(t)
	spliceSurgeryJob(t, graph, "stretch-report", "Stretch goals report", "write the stretch goals report")
	activateHeadCharter(t, graph, "stretch", "Every hour, the stretch reminder fires.")
	user := postUser(t, graph, "j8-mixed", "stand down the stretch one")

	run := &beltRun{head: New(nil, graph), user: user}
	candidates, failed := run.execute(beltToolControl, beltArguments(t, map[string]any{
		"verb": "cancel", "describes": user.Body}))
	if failed {
		t.Fatalf("a targetless stop errored instead of offering candidates: %s", candidates)
	}
	sawJob := strings.Contains(candidates, "stretch-report") &&
		strings.Contains(candidates, "Stretch goals report")
	sawRule := strings.Contains(candidates, "standing rule stretch") &&
		strings.Contains(candidates, "the stretch reminder fires")
	if !sawJob || !sawRule {
		t.Fatalf("the candidate list dropped one half of the world:\n%s", candidates)
	}
	// A rule is not cancelled by the verb aimed at work, and the list says so
	// rather than offering a candidate that would have to be refused.
	if !strings.Contains(candidates, "use the rule tool, not control") {
		t.Fatalf("the rule half does not say how it is acted on:\n%s", candidates)
	}
	if run.acted {
		t.Fatal("a targetless stop acted on a description")
	}

	head, _ := beltHead(graph,
		beltTurn{calls: []ai.ToolCall{beltCall("c1", beltToolControl, map[string]any{
			"verb": "cancel", "describes": user.Body})}},
		beltTurn{calls: []ai.ToolCall{beltCall("c2", beltToolAsk, map[string]any{
			"question": "Which one do you mean?",
			"options":  []string{"Stretch goals report", "the hourly stretch reminder"}})}})
	if err := head.answer(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	reply := waitForAgentReply(t, graph, "j8-mixed", user.Seq)
	if !strings.HasPrefix(reply.Body, "Which one do you mean?") || len(reply.Options) != 2 {
		t.Fatalf("mixed reply = %+v", reply)
	}
	assertNothingClaimed(t, graph, "j8-mixed", user.Seq)
	if commands, err := graph.PendingCommands(0); err != nil || len(commands) != 0 {
		t.Fatalf("a mixed stand-down journaled work: %+v err=%v", commands, err)
	}
}

// Nothing to point at. The person still gets a sentence, and the sentence says
// what is true: there is no such thing running. The tool is what makes that
// answer available — it reports the honest miss, and only then can the loop say
// it.
func TestStandingDownSomethingThatIsNotRunningSaysSoPlainly(t *testing.T) {
	graph := openHeadStore(t)
	user := postUser(t, graph, "j8-empty", "stand down the stretch reminder")

	run := &beltRun{head: New(nil, graph), user: user}
	missed, failed := run.execute(beltToolControl, beltArguments(t, map[string]any{
		"verb": "cancel", "describes": user.Body}))
	if failed || !strings.Contains(missed, "nothing on the board and no standing rule matches those words") {
		t.Fatalf("the miss was not reported plainly: %q", missed)
	}
	if run.acted {
		t.Fatal("a miss acted on something")
	}

	head, _ := beltHead(graph,
		beltTurn{calls: []ai.ToolCall{beltCall("c1", beltToolControl, map[string]any{
			"verb": "cancel", "describes": user.Body})}},
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
			// Rejected before the store: the belt does not have this verb, so
			// there is nothing to journal and nothing to resolve.
			name: "a verb the head does not have",
			tool: beltToolControl,
			args: map[string]any{"verb": "obliterate", "ids": []string{"anything"}},
			want: "verb must be one of cancel, pause, resume, restart, reprioritize",
		},
		{
			// Rejected by the store's own reality: well-formed, aimed at work that
			// is not there. Ids come from reads, and one that came from anywhere
			// else fails at the door rather than after the reply.
			name: "work that is not there",
			tool: beltToolControl,
			args: map[string]any{"verb": "cancel", "ids": []string{"ghost-node"}},
			want: `there is no live work with id "ghost-node"`,
		},
		{
			// The same door on the commissioning side: work that follows on from
			// something that does not exist is not queued as work that follows on
			// from nothing.
			name: "continuing work that is not there",
			tool: beltToolSpawn,
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
	result, failed := run.execute(beltToolSpawn, beltArguments(t, map[string]any{
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
	if result, failed := consequential.execute(beltToolSpawn, beltArguments(t, map[string]any{
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
	if result, failed := run.execute(beltToolRevise, beltArguments(t, map[string]any{
		"job": "migration"})); failed {
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
	result, failed := empty.execute(beltToolSpawn, beltArguments(t, map[string]any{}))
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
