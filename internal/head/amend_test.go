package head

import (
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// liveJob puts one piece of the person's own work on the board, running.
func liveJob(t *testing.T, graph *store.Store, id, title, intent string) {
	t.Helper()
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: id, Title: title, Brief: intent, Stage: 1},
	}}, store.Provenance{Origin: store.OriginUser, SessionID: "room", Intent: intent}); err != nil {
		t.Fatal(err)
	}
	if _, won, err := graph.Claim(id, "worker"); err != nil || !won {
		t.Fatalf("claim %s: won=%v err=%v", id, won, err)
	}
}

// The bug, replayed from the user's own journal: a task is commissioned, and
// forty seconds later the person changes one of its constraints. That is the
// task being corrected, and it used to become a second identical task — two
// plans, two workforces, two bills, and two identical commitment lines in the
// thread.
func TestCorrectingWorkInFlightRevisesItAndCommissionsNothing(t *testing.T) {
	graph := openHeadStore(t)
	liveJob(t, graph, "stocks", "Twenty best stocks",
		"Produce a ranked list of the 20 best stocks with the reasoning behind each")
	user := postUser(t, graph, "room", "no you can use internet search")
	run := &beltRun{head: New(nil, graph), user: user}

	answer, failed := run.execute(beltToolSpawn, beltArguments(t, map[string]any{
		"instruction": "deep research the 20 best stocks, using internet search",
	}))
	if failed {
		t.Fatalf("the correction was refused outright: %s", answer)
	}
	commands, err := graph.PendingCommands(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(commands) != 1 {
		t.Fatalf("one correction journaled %d commands: %+v", len(commands), commands)
	}
	if commands[0].Kind != store.CommandRedirect || commands[0].Target != "stocks" {
		t.Fatalf("the correction did not go to the running work: %+v", commands[0])
	}
	// Their words, verbatim, are what the running plan is revised against —
	// never the model's paraphrase of them.
	if commands[0].Instruction != user.Body {
		t.Fatalf("the revision carries %q rather than what they said", commands[0].Instruction)
	}
	// And the loop is told the truth, in terms it has to speak to: this is in
	// hand for the existing work, and nothing new was started.
	if !strings.Contains(answer, "NO second job was commissioned") {
		t.Fatalf("the loop was not told what actually happened: %q", answer)
	}
	if len(run.did) != 1 || !strings.Contains(run.did[0], "Twenty best stocks") {
		t.Fatalf("the receipt does not name the work it changed: %q", run.did)
	}
}

// The guard is about REFERENCE, not resemblance. A second ask that happens to
// be about the same subject as running work is a second ask, and the person is
// entitled to it.
func TestANewAskThatMerelyResemblesRunningWorkStillCommissions(t *testing.T) {
	graph := openHeadStore(t)
	liveJob(t, graph, "stocks", "Twenty best stocks",
		"Produce a ranked list of the 20 best stocks with the reasoning behind each")
	// Same subject, same vocabulary, no reference to the work in flight: the
	// person is asking for a second thing, not correcting the first.
	user := postUser(t, graph, "room", "a second list of the 20 best stocks, this time by sector")
	run := &beltRun{head: New(nil, graph), user: user}

	if answer, failed := run.execute(beltToolSpawn, beltArguments(t, map[string]any{
		"instruction": "a second list of the 20 best stocks, this time by sector",
	})); failed {
		t.Fatalf("a new ask was refused: %s", answer)
	}
	commands, err := graph.PendingCommands(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(commands) != 1 || commands[0].Kind != store.CommandSplice {
		t.Fatalf("a new ask did not commission work: %+v", commands)
	}
}

// A quiet board can never turn a sentence into an amendment: with nothing
// running there is nothing to correct, however the sentence opens.
func TestACorrectionWithNothingRunningIsOrdinaryWork(t *testing.T) {
	graph := openHeadStore(t)
	user := postUser(t, graph, "room", "actually, look at the bond market instead")
	run := &beltRun{head: New(nil, graph), user: user}

	if answer, failed := run.execute(beltToolSpawn, beltArguments(t, map[string]any{
		"instruction": "look at the bond market",
	})); failed {
		t.Fatalf("spawn refused with nothing running: %s", answer)
	}
	commands, err := graph.PendingCommands(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(commands) != 1 || commands[0].Kind != store.CommandSplice {
		t.Fatalf("commands = %+v", commands)
	}
}

// Several live jobs and a correction that names none of them is the one case
// nothing may be decided: the candidates come back for the loop to ask about,
// and nothing at all is journaled.
func TestAnAmbiguousCorrectionAsksInsteadOfChoosingOrForking(t *testing.T) {
	graph := openHeadStore(t)
	liveJob(t, graph, "stocks", "Twenty best stocks", "rank the 20 best stocks")
	liveJob(t, graph, "bonds", "Bond market chart", "chart the bond market")
	user := postUser(t, graph, "room", "no, use internet search")
	run := &beltRun{head: New(nil, graph), user: user}

	answer, failed := run.execute(beltToolSpawn, beltArguments(t, map[string]any{
		"instruction": "use internet search",
	}))
	if failed {
		t.Fatalf("the ambiguous correction errored: %s", answer)
	}
	if !strings.Contains(answer, "ask which") ||
		!strings.Contains(answer, "stocks") || !strings.Contains(answer, "bonds") {
		t.Fatalf("the candidates were not handed back: %q", answer)
	}
	if commands, err := graph.PendingCommands(0); err != nil || len(commands) != 0 {
		t.Fatalf("an ambiguous correction journaled %+v err=%v", commands, err)
	}
	if run.acted {
		t.Fatal("handing back candidates counted as acting")
	}
}

// "also X" while something runs is an ADDITION to it, which is what this
// product's own cue vocabulary has always said scope-add means — and now what
// it does, rather than what the prompt asked the model to do about it.
func TestAnAdditionToRunningWorkGoesToThatWork(t *testing.T) {
	graph := openHeadStore(t)
	liveJob(t, graph, "stocks", "Twenty best stocks", "rank the 20 best stocks")
	user := postUser(t, graph, "room", "also include the dividend yield for each")
	run := &beltRun{head: New(nil, graph), user: user}

	if answer, failed := run.execute(beltToolSpawn, beltArguments(t, map[string]any{
		"instruction": "include the dividend yield for each stock",
	})); failed {
		t.Fatalf("the addition errored: %s", answer)
	}
	commands, err := graph.PendingCommands(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(commands) != 1 || commands[0].Kind != store.CommandRedirect || commands[0].Target != "stocks" {
		t.Fatalf("an addition to running work did not reach it: %+v", commands)
	}
}

// Impatience is not an amendment. "hurry up" changes when, not what, and it has
// a verb of its own — reading it as a revision would edit a plan because
// somebody was waiting.
func TestImpatienceIsNotReadAsACorrection(t *testing.T) {
	graph := openHeadStore(t)
	liveJob(t, graph, "stocks", "Twenty best stocks", "rank the 20 best stocks")
	user := postUser(t, graph, "room", "this is taking forever")

	if _, amending, err := New(nil, graph).amendmentFor(user); err != nil || amending {
		t.Fatalf("impatience read as an amendment: amending=%v err=%v", amending, err)
	}
}

// The override exists for the sentence that says, in so many words, that this
// is a job beside the running one — and for nothing else.
func TestAnExplicitlySeparateJobIsStillCommissioned(t *testing.T) {
	graph := openHeadStore(t)
	liveJob(t, graph, "stocks", "Twenty best stocks", "rank the 20 best stocks")
	user := postUser(t, graph, "room", "no, keep that running and start a separate one on bonds")
	run := &beltRun{head: New(nil, graph), user: user}

	if answer, failed := run.execute(beltToolSpawn, beltArguments(t, map[string]any{
		"instruction": "chart the bond market", "separate": true,
	})); failed {
		t.Fatalf("an explicitly separate job was refused: %s", answer)
	}
	commands, err := graph.PendingCommands(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(commands) != 1 || commands[0].Kind != store.CommandSplice {
		t.Fatalf("commands = %+v", commands)
	}
}

// The reading itself, without the door around it: what fires, what does not.
func TestWhatReadsAsACorrectionOfWorkInFlight(t *testing.T) {
	for message, want := range map[string]bool{
		"no you can use internet search":                true,
		"no, use the internal data instead":             true,
		"actually make that quarterly":                  true,
		"don't use the API for this":                    true,
		"instead of the API, read the CSV":              true,
		"chart the bond market too":                     false,
		"how is it going":                               false,
		"make it warmer":                                false,
		"always answer from the result, and rerun that": false,
	} {
		if got := correctiveOpener(message); got != want {
			t.Errorf("correctiveOpener(%q) = %v, want %v", message, got, want)
		}
	}
}

// The whole turn, end to end, on the sentence from the live journal: the loop
// tries to commission, the door turns it into a revision of the running work,
// and the reply the person reads is tied to that revision rather than to a
// second job — one commitment line, one job, one bill.
func TestTheTurnThatUsedToForkNowSaysItIsInHandForTheRunningWork(t *testing.T) {
	graph := openHeadStore(t)
	liveJob(t, graph, "stocks", "Twenty best stocks",
		"Produce a ranked list of the 20 best stocks with the reasoning behind each")
	user := postUser(t, graph, "room", "no you can use internet search")

	const said = "Noted — that's in hand on the stocks research; it can search the web now."
	conversationalHead, _ := beltHead(graph,
		beltTurn{calls: []ai.ToolCall{beltCall("c1", beltToolSpawn, map[string]any{
			"instruction": "deep research the 20 best stocks using internet search",
		})}},
		beltTurn{text: said})
	if err := conversationalHead.answer(context.Background(), user); err != nil {
		t.Fatal(err)
	}

	commands, err := graph.PendingCommands(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(commands) != 1 || commands[0].Kind != store.CommandRedirect || commands[0].Target != "stocks" {
		t.Fatalf("the turn journaled %+v, want one revision of the running work", commands)
	}
	reply := waitForAgentReply(t, graph, "room", user.Seq)
	if reply.Body != said {
		t.Fatalf("reply = %q", reply.Body)
	}
	// The receipt is tied to the revision, so every surface that reads "which
	// work is this reply about" is pointed at the job that already exists.
	if reply.CommandSeq != commands[0].Seq {
		t.Fatalf("the reply is not the receipt for the revision: %+v", reply)
	}
}
