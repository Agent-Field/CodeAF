package head

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// The laws this wave exists for, tested where they can actually fail.
//
// Everything else in this package tests a behaviour that survived the
// replacement. These test the four things that did not exist before it: one
// prompt, one belt, a door to disk, and a stop that is not a keypress.

// ── The artifact law (12.5.1) ───────────────────────────────────────────────

// Session bd3c78ed's whole shape: a deliverable authored inline, cut by an
// output cap, and unrecoverable because the only copy was the truncated one.
// The fix is not a bigger cap. It is that "answer inline" stopped being the
// only route a deliverable has.
func TestADeliverableIsBornOnDiskAndReferencedByPath(t *testing.T) {
	graph := openHeadStore(t)
	workspace := t.TempDir()
	const diagram = `<svg xmlns="http://www.w3.org/2000/svg"><rect width="10" height="10"/></svg>`

	client := &beltClient{turns: []beltTurn{
		{calls: []ai.ToolCall{beltCall("c1", beltToolWrite, map[string]any{
			"name": "architecture.svg", "body": diagram, "what": "the architecture diagram"})}},
		{text: "The diagram is at architecture.svg — open it and tell me what to change."},
	}}
	session := "artifact"
	user := postUser(t, graph, session, "draw me the architecture as an svg")
	head := New(client, graph).WithWorkspace(workspace)
	if err := head.answer(context.Background(), user); err != nil {
		t.Fatal(err)
	}

	written, err := os.ReadFile(filepath.Join(workspace, "architecture.svg"))
	if err != nil {
		t.Fatalf("the deliverable was never born on disk: %v", err)
	}
	if string(written) != diagram {
		t.Fatalf("the file is not what was written:\n%s", written)
	}
	reply := waitForAgentReply(t, graph, session, user.Seq)
	if !strings.Contains(reply.Body, "architecture.svg") {
		t.Fatalf("the reply does not name the path the person has to open: %q", reply.Body)
	}
	// And the prompt says the law out loud, because a door nobody is told about
	// is a door nobody uses.
	if !strings.Contains(orchestratorPrompt, "THE ARTIFACT LAW") {
		t.Fatal("the prompt no longer states the artifact law")
	}
	if !strings.Contains(orchestratorPrompt, "born on disk and referenced by its path") {
		t.Fatal("the prompt no longer says where a deliverable is born")
	}
}

// The write door is a door, not a filesystem. What it refuses, it refuses in
// words the model can act on — a sanitized path would be an artifact nobody
// could find, and a silent overwrite would destroy the person's own edits in
// the exact case this tool is called for.
func TestTheWriteDoorRefusesPathsAndNeverClobbers(t *testing.T) {
	graph := openHeadStore(t)
	workspace := t.TempDir()
	run := &beltRun{head: New(nil, graph).WithWorkspace(workspace),
		user: postUser(t, graph, "artifact", "write it down")}

	for _, name := range []string{"../escape.md", "notes/report.md", "/etc/passwd", ".hidden.md", "report"} {
		message, failed := run.execute(beltToolWrite, beltArguments(t, map[string]any{
			"name": name, "body": "x"}))
		if !failed {
			t.Fatalf("%q was accepted as a filename: %s", name, message)
		}
	}
	if entries, err := os.ReadDir(workspace); err != nil || len(entries) != 0 {
		t.Fatalf("a refused write left something behind: %+v err=%v", entries, err)
	}

	if _, failed := run.execute(beltToolWrite, beltArguments(t, map[string]any{
		"name": "report.md", "body": "first"})); failed {
		t.Fatal("an ordinary write was refused")
	}
	second, failed := run.execute(beltToolWrite, beltArguments(t, map[string]any{
		"name": "report.md", "body": "second"}))
	if failed {
		t.Fatalf("a colliding write failed instead of minting a name: %s", second)
	}
	first, err := os.ReadFile(filepath.Join(workspace, "report.md"))
	if err != nil || string(first) != "first" {
		t.Fatalf("the existing file was clobbered: %q err=%v", first, err)
	}
	minted, err := os.ReadFile(filepath.Join(workspace, "report-2.md"))
	if err != nil || string(minted) != "second" {
		t.Fatalf("the second write did not land beside the first: %q err=%v", minted, err)
	}
	if !strings.Contains(second, "report-2.md") {
		t.Fatalf("the receipt did not say where the file actually went: %s", second)
	}
}

// The repair doctrine's second half. "Just say the word and I'll redo it",
// followed by discovering it cannot, is the worst shape available — so what the
// head wrote, the head can open again to fix.
func TestWhatTheHeadWroteItCanReadBackToRepair(t *testing.T) {
	graph := openHeadStore(t)
	workspace := t.TempDir()
	head := New(nil, graph).WithWorkspace(workspace)
	run := &beltRun{head: head, user: postUser(t, graph, "repair", "write the notes")}

	if _, failed := run.execute(beltToolWrite, beltArguments(t, map[string]any{
		"name": "notes.md", "body": "the conclusion is on the last line"})); failed {
		t.Fatal("the write was refused")
	}
	reopened, failed := run.execute(beltToolRead, beltArguments(t, map[string]any{"file": "notes.md"}))
	if failed || !strings.Contains(reopened, "the conclusion is on the last line") {
		t.Fatalf("the head could not reopen what it wrote failed=%t: %s", failed, reopened)
	}

	// And the boundary still holds: only paths the system itself recorded are
	// openable, so a name the model invents opens nothing.
	invented, failed := run.execute(beltToolRead, beltArguments(t, map[string]any{"file": "/etc/hosts"}))
	if !failed {
		t.Fatalf("an invented path was opened: %s", invented)
	}
	if !strings.Contains(orchestratorPrompt, "THE REPAIR DOCTRINE") {
		t.Fatal("the prompt no longer states the repair doctrine")
	}
	if !strings.Contains(orchestratorPrompt, "check deliverability BEFORE you offer") {
		t.Fatal("the prompt lost the rule against offering what it cannot deliver")
	}
}

// ── The conscious cap (12.6.3) ──────────────────────────────────────────────

// The cap was 600 and 600 is the exact number that cut the diagram in half.
// Raising it was never the fix; the artifact door is. What this pins is that
// the number is chosen rather than inherited, and that it is the ONE number —
// there is no second answering call on a different ceiling any more.
func TestTheAnswerTurnCarriesOneDeliberateCap(t *testing.T) {
	graph := openHeadStore(t)
	seen := &capturingClient{reply: "Nothing is running."}
	user := postUser(t, graph, "caps", "what is running?")
	if err := New(seen, graph).answer(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	if len(seen.maxTokens) != 1 {
		t.Fatalf("one message cost %d answering calls: %v", len(seen.maxTokens), seen.maxTokens)
	}
	if seen.maxTokens[0] != orchestratorMaxTokens {
		t.Fatalf("the answering call ran at %d tokens, want the deliberate cap %d",
			seen.maxTokens[0], orchestratorMaxTokens)
	}
	if orchestratorMaxTokens == 600 {
		t.Fatal("the cap is still the one that cut session bd3c78ed's diagram in half")
	}
	if !seen.tooled[0] {
		t.Fatal("the answering call carried no tools, so the head cannot act at all")
	}
}

// capturingClient records what each call was given rather than what it said.
type capturingClient struct {
	reply     string
	maxTokens []int
	tooled    []bool
	systems   []string
}

func (client *capturingClient) CompleteWithMessages(_ context.Context, messages []ai.Message,
	options ...ai.Option) (*ai.Response, error) {
	request := ai.Request{Messages: messages}
	for _, option := range options {
		_ = option(&request)
	}
	tokens := 0
	if request.MaxTokens != nil {
		tokens = *request.MaxTokens
	}
	client.maxTokens = append(client.maxTokens, tokens)
	client.tooled = append(client.tooled, len(request.Tools) > 0)
	system := ""
	if len(messages) > 0 && len(messages[0].Content) > 0 {
		system = messages[0].Content[0].Text
	}
	client.systems = append(client.systems, system)
	return textResponse(client.reply), nil
}

// ── One prompt ──────────────────────────────────────────────────────────────

// Part 2.1's indictment: two prompts, restating overlapping law in two
// vocabularies, above one product. The cache cares about exactly one property
// and it is the one this asserts — the system message is the same bytes on
// every turn, whatever the person typed.
func TestTheSystemPromptIsOneConstantAcrossEveryKindOfMessage(t *testing.T) {
	graph := openHeadStore(t)
	seedExceptBoard(t, graph)
	client := &capturingClient{reply: "Right."}
	head := New(client, graph)
	for _, message := range []string{
		"hello",
		"cancel the queued ones",
		"what did the market research find?",
		"draw me an svg of the architecture",
		"whenever a new pr lands, check it",
	} {
		user := postUser(t, graph, "one-prompt", message)
		if err := head.answer(context.Background(), user); err != nil {
			t.Fatalf("%q: %v", message, err)
		}
	}
	if len(client.systems) < 5 {
		t.Fatalf("some message never reached the loop: %d calls", len(client.systems))
	}
	for index, system := range client.systems {
		if system != client.systems[0] {
			t.Fatalf("call %d carried a different system prompt — the split brain is back", index)
		}
	}
}

// ── Spawn: the guards moved into the tool (Part 6 decision 2) ───────────────

// Past the cap the message is not a handful of asks, it is a list — and a list
// is one job that enumerates. Nothing the person said is lost by collapsing it,
// because the whole message travels.
func TestSpawnCollapsesAFanOutPastItsCap(t *testing.T) {
	graph := openHeadStore(t)
	const whole = "fix issues 12, 41, 77, 93, 104, 118 and 122"
	user := postUser(t, graph, "fanout", whole)
	run := &beltRun{head: New(nil, graph), user: user}
	orders := make([]string, 0, fanOutLimit+1)
	for index := 0; index <= fanOutLimit; index++ {
		orders = append(orders, "fix issue "+string(rune('a'+index)))
	}
	if message, failed := run.execute(beltToolSpawn, beltArguments(t,
		map[string]any{"orders": orders})); failed {
		t.Fatalf("an over-long fan-out was refused outright: %s", message)
	}
	commands, err := graph.PendingCommands(20)
	if err != nil || len(commands) != 1 {
		t.Fatalf("commands = %+v err=%v, want exactly one", commands, err)
	}
	if commands[0].Instruction != whole {
		t.Fatalf("the collapsed order lost the person's own sentence: %q", commands[0].Instruction)
	}
}

// Under the cap, independent work stays independent: its own goal, its own
// plan, its own price, its own deliverable.
func TestSpawnJournalsOneCommandPerIndependentPieceOfWork(t *testing.T) {
	graph := openHeadStore(t)
	user := postUser(t, graph, "fanout", "fix issues 12, 41 and 77")
	run := &beltRun{head: New(nil, graph), user: user}
	orders := []string{"fix issue 12", "fix issue 41", "fix issue 77"}
	if message, failed := run.execute(beltToolSpawn, beltArguments(t,
		map[string]any{"orders": orders})); failed {
		t.Fatalf("a fan-out was refused: %s", message)
	}
	commands, err := graph.PendingCommands(20)
	if err != nil || len(commands) != 3 {
		t.Fatalf("commands = %+v err=%v, want three", commands, err)
	}
	for index, command := range commands {
		if command.Kind != store.CommandSplice || command.Instruction != orders[index] {
			t.Fatalf("order %d = %+v, want a splice carrying %q", index, command, orders[index])
		}
		if command.Reflex || command.Target != "" {
			t.Fatalf("independent work inherited a claim about one ask: %+v", command)
		}
	}
	if run.commandSeq != commands[0].Seq {
		t.Fatalf("the reply ties to seq %d, want the first order's %d", run.commandSeq, commands[0].Seq)
	}
}

// ── await: the feedback loop async commands never had (Part 2.6) ────────────

func TestAwaitReportsHowTheCommandItJustIssuedSettled(t *testing.T) {
	graph := openHeadStore(t)
	user := postUser(t, graph, "await", "start the audit")
	run := &beltRun{head: New(nil, graph), user: user}
	if message, failed := run.execute(beltToolSpawn, beltArguments(t,
		map[string]any{"instruction": "start the audit"})); failed {
		t.Fatalf("spawn refused: %s", message)
	}
	commands, err := graph.PendingCommands(10)
	if err != nil || len(commands) != 1 {
		t.Fatalf("commands = %+v err=%v", commands, err)
	}

	// Still queued: the honest answer is that it is in hand, never that it is done.
	queued, failed := run.execute(beltToolAwait, beltArguments(t, map[string]any{}))
	if failed {
		t.Fatalf("await errored on a live command: %s", queued)
	}
	if !strings.Contains(queued, "still queued") {
		t.Fatalf("await claimed something about work that has not been reached: %s", queued)
	}

	// Refused: the loop is told, in words it must speak to, that nothing changed.
	if err := graph.ResolveCommand(commands[0].Seq, store.CommandRejected, "no such target"); err != nil {
		t.Fatal(err)
	}
	refused, failed := run.execute(beltToolAwait, beltArguments(t,
		map[string]any{"command": commands[0].Seq}))
	if failed {
		t.Fatalf("await errored on a settled command: %s", refused)
	}
	if !strings.Contains(refused, "REFUSED") || !strings.Contains(refused, "no such target") {
		t.Fatalf("a refusal did not come back as one: %s", refused)
	}
	if !strings.Contains(refused, "Nothing changed") {
		t.Fatalf("the loop was not told that nothing changed: %s", refused)
	}
}

// ── answer_question: Part 6 decision 1 held open (9.4, 12.1.4) ──────────────

// 12.1.4 surveyed every AskQuestion producer in the product and found all of
// them consent-bearing and none labeled informational. So the autonomy half of
// this tool has no question it could legitimately answer, and granting it
// anyway would be the head settling consent in the person's name. What the tool
// buys today is the half that was missing entirely: the head can SEE what a
// worker is blocked on.
func TestAnsweringAConsentQuestionIsRefusedAndTheQuestionStaysOpen(t *testing.T) {
	graph := openHeadStore(t)
	spliceSurgeryJob(t, graph, "audit", "Ledger audit", "audit the ledger")
	question, err := graph.AskQuestion(store.AgentQuestion{
		SessionID: "consent", Text: "Should I delete the stale rows?", OriginNodeID: "audit",
		Urgency: store.QuestionBlocking,
	})
	if err != nil {
		t.Fatal(err)
	}
	if question.Class != store.QuestionConsent {
		t.Fatalf("an unlabeled question defaulted to %q, want the conservative class", question.Class)
	}
	user := postUser(t, graph, "consent", "just say yes for me")
	run := &beltRun{head: New(nil, graph), user: user}

	listed, failed := run.execute(beltToolAnswerQuestion, beltArguments(t, map[string]any{}))
	if failed {
		t.Fatalf("listing open questions errored: %s", listed)
	}
	if !strings.Contains(listed, "Should I delete the stale rows?") {
		t.Fatalf("the head still cannot see what the work is blocked on: %s", listed)
	}
	if !strings.Contains(listed, "the user's to answer, never yours") {
		t.Fatalf("the class was not said out loud: %s", listed)
	}

	message, failed := run.execute(beltToolAnswerQuestion, beltArguments(t, map[string]any{
		"question": question.Seq, "answer": "yes, delete them"}))
	if !failed {
		t.Fatalf("a consent question was answered on the person's behalf: %s", message)
	}
	if !strings.Contains(message, "CONSENT question") {
		t.Fatalf("the refusal did not say why: %s", message)
	}
	reread, found, err := graph.AgentQuestionBySeq(question.Seq)
	if err != nil || !found || reread.Status != store.QuestionPending {
		t.Fatalf("the question did not stay open: %+v found=%t err=%v", reread, found, err)
	}
	if run.acted {
		t.Fatal("a refused answer recorded an action")
	}
}

// The other side of the same rule: a producer that has earned the informational
// label is answerable, so the axis is capability rather than decoration.
func TestAnInformationalQuestionIsAnsweredAndTheWorkCarriesOn(t *testing.T) {
	graph := openHeadStore(t)
	spliceSurgeryJob(t, graph, "audit", "Ledger audit", "audit the ledger")
	question, err := graph.AskQuestion(store.AgentQuestion{
		SessionID: "informational", Text: "Which quarter does the ledger cover?",
		OriginNodeID: "audit", Urgency: store.QuestionBlocking,
		Class: store.QuestionInformational,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := graph.SurfaceQuestion(question.Seq); err != nil {
		t.Fatal(err)
	}
	user := postUser(t, graph, "informational", "it's Q3")
	run := &beltRun{head: New(nil, graph), user: user}
	message, failed := run.execute(beltToolAnswerQuestion, beltArguments(t, map[string]any{
		"question": question.Seq, "answer": "Q3"}))
	if failed {
		t.Fatalf("an informational question was refused: %s", message)
	}
	reread, found, err := graph.AgentQuestionBySeq(question.Seq)
	if err != nil || !found || reread.Status != store.QuestionAnswered {
		t.Fatalf("the question was not settled: %+v found=%t err=%v", reread, found, err)
	}
	if reread.Resolution != "Q3" {
		t.Fatalf("the answer was not what was given: %q", reread.Resolution)
	}
}

// ── ask: the numbered question kept as a mechanism (5.22) ───────────────────

// Ambiguity used to end in a durable question minted by whichever recognizer
// noticed it. The loop mints it now, and it must still be OPTIONS a person
// picks rather than prose they have to retype — and it must come back to the
// loop, because the loop is the only party that knows what the answer settles.
func TestAskPostsDurableOptionsAndTheAnswerReturnsToTheLoop(t *testing.T) {
	graph := openHeadStore(t)
	session := "ambiguity"
	user := postUser(t, graph, session, "cancel it")
	head := New(nil, graph)
	run := &beltRun{head: head, user: user}

	message, failed := run.execute(beltToolAsk, beltArguments(t, map[string]any{
		"question": "Which one do you mean?",
		"options":  []string{"Ledger audit", "Market research"},
	}))
	if failed {
		t.Fatalf("the question could not be asked: %s", message)
	}
	if !run.spoke {
		t.Fatal("a posted question did not claim the turn, so the loop would speak over it")
	}
	messages, err := graph.Messages(session, user.Seq, 10)
	if err != nil || len(messages) != 1 {
		t.Fatalf("messages = %+v err=%v", messages, err)
	}
	if len(messages[0].Options) != 2 || messages[0].Options[0].Label != "Ledger audit" {
		t.Fatalf("the choices are not rows a person can pick: %+v", messages[0].Options)
	}
	if commands, _ := graph.PendingCommands(10); len(commands) != 0 {
		t.Fatalf("something was journaled before the person answered: %+v", commands)
	}

	answer := postUser(t, graph, session, "1")
	handled, err := head.answerPendingQuestion(context.Background(), answer)
	if err != nil {
		t.Fatal(err)
	}
	if handled {
		t.Fatal("the question machinery applied an answer only the loop can read")
	}
}

// ── interrupt: turn-cancel beyond the keypress (Part 2.4, 12.3.3) ───────────

// The journal is tried first on purpose: the funnel is the product's one
// authority path, and a stop that rode past it would be the second engine Part 3
// forbids. The store's kind list is closed today, so the door reports which road
// it took rather than degrading silently.
func TestInterruptTriesTheJournalAndSaysWhichRoadItTook(t *testing.T) {
	graph := openHeadStore(t)
	head := New(nil, graph)

	// Nothing running: a surface that asked at the wrong moment must be able to
	// tell that nothing happened.
	route, err := head.RequestInterrupt("stop", "the user said stop", "")
	if err != nil {
		t.Fatal(err)
	}
	if route != InterruptNothingRunning {
		t.Fatalf("route = %q with no turn in flight, want %q", route, InterruptNothingRunning)
	}

	// A turn in flight: stopped, and the road is named.
	stopped := make(chan struct{})
	head.turnMu.Lock()
	head.turnCancel = func() { close(stopped) }
	head.turnMu.Unlock()
	route, err = head.RequestInterrupt("stop", "the user said stop", "half an answer")
	if err != nil {
		t.Fatal(err)
	}
	if route != InterruptInProcess && route != InterruptJournaled {
		t.Fatalf("route = %q, want one of the two real roads", route)
	}
	if route == InterruptInProcess {
		select {
		case <-stopped:
		default:
			t.Fatal("the in-process route reported a stop that did not happen")
		}
		if partial, was, _ := head.endTurn(); !was || partial != "half an answer" {
			t.Fatalf("the words the reader had already seen were dropped: %q was=%t", partial, was)
		}
	}
	if commands, _ := graph.PendingCommands(10); route == InterruptJournaled && len(commands) != 1 {
		t.Fatalf("the journaled route journaled nothing: %+v", commands)
	}
}

// ApplyInterrupt is the reconciler's arm, written here so the one line the
// resident needs is a call rather than a design. It is tested from this side
// because this lane may not edit resident.go — see interrupt.go for the exact
// edit and 12.3.3 for why the seam is closed.
func TestApplyInterruptIsTheArmTheReconcilerWouldCall(t *testing.T) {
	graph := openHeadStore(t)
	head := New(nil, graph)
	stopped := make(chan struct{})
	head.turnMu.Lock()
	head.turnCancel = func() { close(stopped) }
	head.turnMu.Unlock()

	if head.ApplyInterrupt(store.Command{Kind: store.CommandSplice}) {
		t.Fatal("the arm acted on a command that is not a stop")
	}
	if !head.ApplyInterrupt(store.Command{Kind: HeadInterruptKind}) {
		t.Fatal("the arm did not stop the turn in flight")
	}
	select {
	case <-stopped:
	default:
		t.Fatal("the arm reported a stop that did not happen")
	}
	head.endTurn()
	if head.ApplyInterrupt(store.Command{Kind: HeadInterruptKind}) {
		t.Fatal("the arm claimed to stop a turn that had already ended")
	}
}

// ── The gates, unchanged (4.1) ─────────────────────────────────────────────

// Authority expanded; the gates did not move. A set over the cascade gate still
// stops, still asks with the count named, and still journals nothing until the
// person answers — reached now from a tool instead of from a prefix test.
func TestTheConsentGateStillStopsASetTheLoopAskedFor(t *testing.T) {
	graph := openHeadStore(t)
	spliceJobTree(t, graph, store.OriginUser, "",
		spec("wide", "", "Wide job", "do the wide job"),
		spec("wide-1", "wide", "First", "first"),
		spec("wide-2", "wide", "Second", "second"),
		spec("wide-3", "wide", "Third", "third"),
		spec("wide-4", "wide", "Fourth", "fourth"),
		spec("wide-5", "wide", "Fifth", "fifth"),
		spec("wide-6", "wide", "Sixth", "sixth"))

	session := "gate"
	user := postUser(t, graph, session, "drop all of that")
	client := &beltClient{turns: []beltTurn{
		{calls: []ai.ToolCall{beltCall("c1", beltToolControl, map[string]any{
			"verb": "cancel", "ids": []string{"wide"}})}},
		{text: "Cancelled the lot."},
	}}
	if err := New(client, graph).answer(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	if commands, _ := graph.PendingCommands(20); len(commands) != 0 {
		t.Fatalf("a gated set was journaled before the person answered: %+v", commands)
	}
	questions, err := graph.UnresolvedQuestions(10)
	if err != nil || len(questions) != 1 {
		t.Fatalf("the person was not asked: %+v err=%v", questions, err)
	}
	if questions[0].Category != store.QuestionCategorySurgeryConfirm {
		t.Fatalf("the gate's own category was lost: %q", questions[0].Category)
	}
	// And the loop's prose never lands beside the gate's question: only one of
	// the two knows what the consent actually covers.
	messages, err := graph.Messages(session, user.Seq, 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, message := range messages {
		if message.Body == "Cancelled the lot." {
			t.Fatal("the loop said the change happened over the top of the consent question")
		}
	}
}

// ── Visible dispatch (5.20.1) ──────────────────────────────────────────────

// Prose turned into work is never a silent side effect: the reply carries the
// command it commissioned, so the transcript can ink the decision. And a turn
// whose words fail still says what it did, out of what the tools reported and
// never out of intent.
func TestWorkCommissionedInATurnAlwaysLeavesAReceipt(t *testing.T) {
	graph := openHeadStore(t)
	session := "dispatch"
	user := postUser(t, graph, session, "look into the pricing question")
	client := &beltClient{turns: []beltTurn{
		{calls: []ai.ToolCall{beltCall("c1", beltToolSpawn, map[string]any{
			"instruction": "look into the pricing question"})}},
		{text: ""},
	}}
	if err := New(client, graph).answer(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	commands, err := graph.PendingCommands(10)
	if err != nil || len(commands) != 1 {
		t.Fatalf("commands = %+v err=%v", commands, err)
	}
	reply := waitForAgentReply(t, graph, session, user.Seq)
	if reply.CommandSeq != commands[0].Seq {
		t.Fatalf("the reply does not tie to the work it commissioned: %+v", reply)
	}
	if strings.TrimSpace(reply.Body) == "" {
		t.Fatal("work was commissioned and the thread said nothing")
	}
	if !strings.Contains(reply.Body, "pricing") {
		t.Fatalf("the receipt does not name what was commissioned: %q", reply.Body)
	}
}

// ── forget: the retraction door the router used to be (12.8.11) ─────────────

// The router carried a `retract` field and the wave nearly dropped it, which
// would have left a head that can accumulate beliefs and never let one go. This
// is the door back: one numbered line, quarantined rather than superseded,
// because the person is throwing a belief away rather than giving you its next
// version.
func TestForgettingRetiresExactlyTheNumberedBeliefAndNothingElse(t *testing.T) {
	graph := openHeadStore(t)
	kept, err := graph.RecordFact("", "user", store.FactPreference, "always cc finance on invoices")
	if err != nil {
		t.Fatal(err)
	}
	doomed, err := graph.RecordFact("", "user", store.FactPreference, "I prefer the long form report")
	if err != nil {
		t.Fatal(err)
	}
	user := postUser(t, graph, "forget", "forget that, I don't work that way any more")
	run := &beltRun{head: New(nil, graph), user: user}

	for _, seq := range []int64{0, -1, doomed.Seq + 500} {
		if message, failed := run.execute(beltToolForget, beltArguments(t,
			map[string]any{"belief": seq})); !failed {
			t.Fatalf("belief %d was accepted: %s", seq, message)
		}
	}

	message, failed := run.execute(beltToolForget, beltArguments(t,
		map[string]any{"belief": doomed.Seq}))
	if failed {
		t.Fatalf("an active belief could not be let go: %s", message)
	}
	gone, found, err := graph.FactBySeq(doomed.Seq)
	if err != nil || !found || gone.Status == store.FactActive {
		t.Fatalf("the belief is still active: %+v found=%t err=%v", gone, found, err)
	}
	survivor, found, err := graph.FactBySeq(kept.Seq)
	if err != nil || !found || survivor.Status != store.FactActive {
		t.Fatalf("an unrelated belief was taken with it: %+v found=%t err=%v", survivor, found, err)
	}
	if len(run.did) != 1 || !strings.Contains(run.did[0], "long form report") {
		t.Fatalf("the receipt does not say which belief went: %v", run.did)
	}
	if run.commandSeq != 0 {
		t.Fatalf("letting go of a belief journaled a graph command: %d", run.commandSeq)
	}
}
