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

// The consequence of a note not claiming the turn: a sentence carrying both a
// durable preference and a piece of work keeps the preference AND commissions
// the work — in one turn, which is the whole difference the loop makes. Under
// the ladder the note tool had to hand the sentence back to a router to get the
// work queued, and the fall-through was the only mechanism there was.
func TestANoteBesideWorkKeepsTheFactAndStillCommissionsTheWork(t *testing.T) {
	graph := openHeadStore(t)
	seedExceptBoard(t, graph)
	const preference = "always answer from the task result itself"
	client := &beltClient{
		turns: []beltTurn{
			{calls: []ai.ToolCall{
				beltCall("c1", beltToolNote, map[string]any{"body": preference}),
				beltCall("c2", beltToolSpawn, map[string]any{"instruction": "rerun the scans"}),
			}},
			{text: "Noted, and the scans are queued."},
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
		t.Fatalf("the preference was lost: %+v", facts)
	}
	commands, err := graph.PendingCommands(20)
	if err != nil {
		t.Fatal(err)
	}
	if len(commands) != 1 || commands[0].Kind != store.CommandSplice {
		t.Fatalf("the work in the same sentence was swallowed: %+v", commands)
	}
	if commands[0].Instruction != "rerun the scans" {
		t.Fatalf("the work did not carry the user's own words: %q", commands[0].Instruction)
	}
}

// The other half of the same failure: a loop that could change the user's work
// while being blind to what the user had already told it. The notebook rides in
// its own budget, after the board, which is the floor and is never starved.
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
	board, notebook := strings.Index(prompt, "Board (the user's live work):"), strings.Index(prompt, "Notebook (")
	if board < 0 || notebook < board {
		t.Fatalf("the notebook was written before the board: board=%d notebook=%d", board, notebook)
	}
	if notebookBlock := prompt[notebook:strings.Index(prompt, "Manual pages available:")]; len(notebookBlock) > notebookContextBytes+200 {
		t.Fatalf("the notebook block is %d bytes, past its budget", len(notebookBlock))
	}
	for _, id := range []string{"finance", "research", "scans"} {
		if !strings.Contains(prompt, "- "+id+" | ") {
			t.Fatalf("memory evicted %s from the board:\n%s", id, prompt)
		}
	}
}

// The belt's grammar has to state what it can do, or the model offers to do
// what it will not: the failure's second sentence was "I can pull the specific
// verdict from that file if you want", an offer nothing behind it could fulfil.
func TestTheBeltAndTheRouterBothStateWhatTheyCanActuallyDo(t *testing.T) {
	names := map[string]bool{}
	for _, definition := range beltDefinitions() {
		names[definition.Function.Name] = true
	}
	for _, name := range []string{beltToolRead, beltToolNote} {
		if !names[name] {
			t.Fatalf("the belt does not offer %q, so the prompt's law describes a tool that is not there", name)
		}
		if !strings.Contains(orchestratorPrompt, "- "+name+" ") {
			t.Fatalf("the control prompt does not introduce %q", name)
		}
	}
	for name, prompt := range map[string]string{
		"control": orchestratorPrompt, "router": orchestratorPrompt,
	} {
		if !strings.Contains(prompt, "never") && !strings.Contains(prompt, "Never") {
			t.Fatalf("%s prompt lost its prohibitions entirely", name)
		}
	}
	// Values, not phrases: what is pinned is that both prompts refuse an
	// unbacked promise and an offer to fetch what is already reachable.
	for name, prompt := range map[string]string{
		"control": orchestratorPrompt, "router": orchestratorPrompt,
	} {
		if !strings.Contains(prompt, "never offer") && !strings.Contains(prompt, "never say you will") &&
			!strings.Contains(prompt, "never offer a capability") && !strings.Contains(prompt, "Never promise a behaviour") &&
			!strings.Contains(prompt, "Never promise a lasting change") {
			t.Fatalf("%s prompt no longer forbids promising what it cannot do", name)
		}
	}
}
