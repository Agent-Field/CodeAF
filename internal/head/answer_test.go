package head

import (
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// beltToolText returns what one tool call handed back to the model. Everything
// the loop is allowed to say comes from these strings and nothing else, so a
// claim about what the head can answer is a claim about this.
func beltToolText(t *testing.T, graph *store.Store, user store.Message, name string, args map[string]any) (string, bool) {
	t.Helper()
	run := &beltRun{head: New(nil, graph), user: user}
	return run.execute(name, beltArguments(t, args))
}

func beltArguments(t *testing.T, args map[string]any) string {
	t.Helper()
	return beltCall("c", "x", args).Function.Arguments
}

// The failure, closed at the belt: result names a file and says the work was
// verified; read turns that pointer into the verdict the user asked for.
func TestTheBeltCanOpenTheFileTheResultOnlyNames(t *testing.T) {
	graph := openHeadStore(t)
	node, _ := seedAssessmentJob(t, graph, assessmentVerdict)
	user := postUser(t, graph, "answer", "so is the plan valid or not")

	recorded, failed := beltToolText(t, graph, user, beltToolResult, map[string]any{"id": node.ID})
	if failed {
		t.Fatalf("result read failed: %s", recorded)
	}
	if strings.Contains(recorded, assessmentVerdict) {
		t.Fatal("the fixture is not the live failure: the verdict was already in the summary")
	}
	if !strings.Contains(recorded, "architecture_plan_assessment.md") {
		t.Fatalf("result did not even name the file:\n%s", recorded)
	}

	opened, failed := beltToolText(t, graph, user, beltToolRead, map[string]any{"job": node.ID})
	if failed {
		t.Fatalf("read failed: %s", opened)
	}
	if !strings.Contains(opened, assessmentVerdict) {
		t.Fatalf("read did not hand the loop the verdict:\n%s", opened)
	}
}

// A read is a read: like board, manual and result, asking what a document says
// changed nothing, so nothing is journalled and no receipt is owed.
func TestReadingAFileJournalsNothing(t *testing.T) {
	graph := openHeadStore(t)
	node, _ := seedAssessmentJob(t, graph, assessmentVerdict)
	user := postUser(t, graph, "answer", "what did it conclude")
	run := &beltRun{head: New(nil, graph), user: user}
	if _, failed := run.execute(beltToolRead, beltArguments(t, map[string]any{"job": node.ID})); failed {
		t.Fatal("read of a recorded artifact failed")
	}
	if run.acted || run.commandSeq != 0 || len(run.did) != 0 {
		t.Fatalf("a read recorded an act: acted=%t seq=%d did=%v", run.acted, run.commandSeq, run.did)
	}
	if commands, _ := graph.PendingCommands(20); len(commands) != 0 {
		t.Fatalf("a read journalled commands: %+v", commands)
	}
}

// The whole of fix C in one assertion. "Always answer with the result" was
// answered warmly and recorded nowhere, so the next session repeated the
// failure. Now it lands in the notebook through the ordinary fact machinery,
// and the notebook is what every later message is read against.
func TestADurableReplyPreferenceLandsInTheNotebookAndComesBack(t *testing.T) {
	graph := openHeadStore(t)
	const preference = "always answer the question from the task result itself, never with a description of it"
	user := postUser(t, graph, "learn", "make sure you always answer the question using task result")
	run := &beltRun{head: New(nil, graph), user: user}
	receipt, failed := run.execute(beltToolNote, beltArguments(t, map[string]any{
		"body": preference, "scope": "user", "kind": "preference"}))
	if failed {
		t.Fatalf("note failed: %s", receipt)
	}
	// A note is durable without being what the message was ABOUT: one sentence
	// can carry a preference and a piece of work, and a note that claimed the
	// run would let the loop answer with its receipt and swallow the work.
	if run.acted || run.commandSeq != 0 {
		t.Fatalf("a note claimed the whole message: acted=%t seq=%d", run.acted, run.commandSeq)
	}

	facts, err := graph.RecentFacts(10)
	if err != nil {
		t.Fatal(err)
	}
	var landed *store.Fact
	for index, fact := range facts {
		if fact.Body == preference {
			landed = &facts[index]
		}
	}
	if landed == nil {
		t.Fatalf("the preference was never journalled: %+v", facts)
	}
	if landed.Kind != store.FactPreference || landed.Scope != "user" {
		t.Fatalf("preference landed as %s/%s", landed.Scope, landed.Kind)
	}
	if landed.Channel != store.ChannelForWriter(store.FactWriterHead) {
		t.Fatalf("preference landed on channel %q, not the head's", landed.Channel)
	}
	// The receipt is the record. A reply may only say it is noted because this
	// call returned the sequence it was noted under.
	if !strings.Contains(receipt, "notebook") || len(run.did) != 1 {
		t.Fatalf("note receipt = %q, did = %v", receipt, run.did)
	}
	if rendered := renderNotebook(graph, "how should you answer me", ""); !strings.Contains(rendered, preference) {
		t.Fatalf("the notebook does not read the preference back:\n%s", rendered)
	}
}

// A note that cannot be written must not produce a reply that says it was.
func TestAnEmptyNoteIsRefusedRatherThanReceipted(t *testing.T) {
	graph := openHeadStore(t)
	user := postUser(t, graph, "learn", "remember that")
	run := &beltRun{head: New(nil, graph), user: user}
	message, failed := run.execute(beltToolNote, beltArguments(t, map[string]any{"body": "   "}))
	if !failed {
		t.Fatalf("an empty note was accepted: %s", message)
	}
	if len(run.did) != 0 {
		t.Fatalf("a refused note still produced a receipt: %v", run.did)
	}
}

// The consequence of a note not claiming the message: a sentence carrying both
// a durable preference and a piece of work keeps the preference AND queues the
// work. It used to take two brains to do that — a note on the belt, then a
// sentinel handing the sentence to the router, which was the only thing that
// could spawn. One loop does both in one turn, and the property under test is
// unchanged: neither half swallows the other.
func TestANoteBesideOtherWorkKeepsTheFactAndStillQueuesTheWork(t *testing.T) {
	graph := openHeadStore(t)
	seedExceptBoard(t, graph)
	const preference = "always answer from the task result itself"
	client := &beltClient{
		turns: []beltTurn{
			{calls: []ai.ToolCall{beltCall("c1", beltToolNote, map[string]any{"body": preference})}},
			{calls: []ai.ToolCall{beltCall("c2", beltToolSpawn, map[string]any{
				"instruction": "rerun the scans"})}},
			{text: "Noted, and I'm rerunning the scans."},
		},
	}
	user := postUser(t, graph, "both", "always answer from the task result itself, and rerun the scans")
	if err := New(client, graph).answer(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	facts, err := graph.RecentFacts(10)
	if err != nil {
		t.Fatal(err)
	}
	kept := false
	for _, fact := range facts {
		kept = kept || fact.Body == preference
	}
	if !kept {
		t.Fatalf("the preference was lost beside the work: %+v", facts)
	}
	commands, err := graph.PendingCommands(20)
	if err != nil {
		t.Fatal(err)
	}
	if len(commands) != 1 || commands[0].Kind != store.CommandSplice ||
		commands[0].Instruction != "rerun the scans" {
		t.Fatalf("the work in the same sentence was swallowed: %+v", commands)
	}
	// One sentence, one answer, and the answer is the loop's — not the note's
	// receipt standing in for it.
	if replies := agentRepliesAfter(t, graph, "both", user.Seq); len(replies) != 1 ||
		replies[0].Body != "Noted, and I'm rerunning the scans." {
		t.Fatalf("the turn spoke %d times: %+v", len(replies), replies)
	}
}

// The other half of the same failure: a loop that could change the user's work
// while being blind to what the user had already told it. The notebook rides in
// its own budget, after the board, which is the floor and is never starved.
//
// There is one prompt now and the board's header moved with it, but the
// ordering law it encodes is the same one and is asserted here for the reason it
// always was: a memory block that could evict a board row would make the head
// deny the existence of work it had described a sentence earlier.
func TestTheControlLoopCarriesTheNotebookUnderTheBoard(t *testing.T) {
	graph := openHeadStore(t)
	seedExceptBoard(t, graph)
	const preference = "always answer the question from the task result itself"
	if _, err := graph.RecordFactFrom(store.FactWriterHead, store.RootID, "user",
		store.FactPreference, preference); err != nil {
		t.Fatal(err)
	}
	client := &beltClient{turns: []beltTurn{
		{calls: []ai.ToolCall{beltCall("c1", beltToolBoard, map[string]any{})}},
		{text: "Nothing has stopped."},
	}}
	user := postUser(t, graph, "notebook", "kill everything except the finance one")
	if err := New(client, graph).answer(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	prompt := ""
	for _, message := range client.seen {
		if message.Role == "user" && len(message.Content) > 0 {
			prompt = message.Content[0].Text
		}
	}
	if !strings.Contains(prompt, preference) {
		t.Fatalf("the control loop never saw the notebook:\n%s", prompt)
	}
	board := strings.Index(prompt, "Live board (the work you can read and act on):")
	notebook := strings.Index(prompt, "Notebook (")
	if board < 0 || notebook < board {
		t.Fatalf("the notebook was written before the board: board=%d notebook=%d", board, notebook)
	}
	// The notebook block runs from its header to the clock, which is the next
	// unconditional block under it.
	clock := strings.Index(prompt, "\n\nnow: ")
	if clock < notebook {
		t.Fatalf("the notebook block has no end: notebook=%d clock=%d", notebook, clock)
	}
	if notebookBlock := prompt[notebook:clock]; len(notebookBlock) > notebookContextBytes+200 {
		t.Fatalf("the notebook block is %d bytes, past its budget", len(notebookBlock))
	}
	for _, id := range []string{"finance", "research", "scans"} {
		if !strings.Contains(prompt, "- "+id+" | ") {
			t.Fatalf("memory evicted %s from the board:\n%s", id, prompt)
		}
	}
}

// The head's grammar has to state what it can do, or the model offers to do
// what it will not: the failure's second sentence was "I can pull the specific
// verdict from that file if you want", an offer nothing behind it could fulfil.
//
// There were two prompts making this promise and they made it in different
// vocabularies — which was the disease rather than belt-and-braces, because a
// sentence that tripped no cue reached neither of them. One prompt now states
// what the hands are, and every hand it names has to exist.
func TestTheBeltAndTheRouterBothStateWhatTheyCanActuallyDo(t *testing.T) {
	names := map[string]bool{}
	for _, definition := range beltDefinitions() {
		names[definition.Function.Name] = true
	}
	// Every hand the loop reaches for must exist on the belt. The prompt no
	// longer recites all of them — that was the belt's own schemas said twice —
	// so what is checked is that the capability groups it describes are really
	// there, and that the few verbs it names are real tools.
	for _, name := range []string{
		beltToolBoard, beltToolResult, beltToolPlan, beltToolRead, beltToolManual,
		beltToolCompetence, beltToolStanding, beltToolSpending, beltToolHistory, beltToolSearch,
		beltToolSpawn, beltToolAct, beltToolControl, beltToolSteer, beltToolRevise, beltToolExpedite,
		beltToolCorrect, beltToolRule, beltToolService, beltToolNote, beltToolWrite,
		beltToolAnswerQuestion, beltToolAwait, beltToolAsk, beltToolInterrupt,
	} {
		if !names[name] {
			t.Fatalf("the belt does not offer %q, so the prompt describes a hand that is not there", name)
		}
	}
	// And nothing is named as a hand that is not one: a verb in the prompt with
	// no tool behind it is what the loop then offers to do and cannot.
	for _, word := range strings.Fields(orchestratorHands) {
		word = strings.Trim(word, "(),.:;")
		if strings.HasSuffix(word, "_question") && !names[word] {
			t.Fatalf("the prompt names %q as a hand and the belt has no such tool", word)
		}
	}

	if !strings.Contains(orchestratorPrompt, "never") && !strings.Contains(orchestratorPrompt, "Never") {
		t.Fatal("the head's prompt lost its prohibitions entirely")
	}
	// Values, not phrases: what is pinned is that the prompt refuses an unbacked
	// promise and an offer to fetch what is already reachable.
	if !strings.Contains(orchestratorPrompt, "Say a thing will hold from now on only when note recorded it this turn") {
		t.Error("the prompt no longer forbids promising a behaviour nothing recorded")
	}
	if !strings.Contains(orchestratorPrompt, "offer only routes you have a tool to take") {
		t.Error("the prompt no longer forbids offering a route it cannot take")
	}
	// The failure's own sentence, named in the tool that answers it: an offer to
	// fetch what this call could have fetched already.
	openable := ""
	for _, definition := range beltDefinitions() {
		if definition.Function.Name == beltToolRead {
			openable = definition.Function.Description
		}
	}
	if !strings.Contains(openable, "Never offer to fetch something you can fetch with this call right now") {
		t.Errorf("the read tool no longer refuses the offer it exists to replace: %q", openable)
	}
}
