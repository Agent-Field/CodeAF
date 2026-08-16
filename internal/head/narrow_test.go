package head

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// beltWorkVerbNames is the narrowed belt as the model would see it, read from
// the definitions themselves so a verb added to it cannot be asserted against a
// list that has gone stale.
func beltWorkVerbNames() []string {
	names := make([]string, 0, 2)
	for _, definition := range beltWorkDefinitions() {
		names = append(names, definition.Function.Name)
	}
	return names
}

func sameStrings(first, second []string) bool {
	if len(first) != len(second) {
		return false
	}
	for index := range first {
		if first[index] != second[index] {
			return false
		}
	}
	return true
}

// THE INCIDENT, as a turn (narrow.go).
//
// A person asks for the figures in a paper. The head works on it — sixteen calls
// of a campaign with the crops still ahead of it — and reaches the bound in the
// middle. What used to happen there was that every hand went away at once, so
// the turn said the belt was spent and promised to finish next turn, which is a
// promise made about a turn nobody owns. What happens now is that the belt
// narrows to the verbs that hand work over, and the work goes to the workforce
// carrying what the turn found out on the way.
func TestTheNarrowedBeltHandsTheUnfinishedCampaignOver(t *testing.T) {
	graph := openHeadStore(t)
	// The discovery: a job whose title is a thing only a tool result says. It is
	// in no message of this conversation, so finding it in the handoff can only
	// mean it travelled from a read this turn took.
	spliceSurgeryJob(t, graph, "paper", "Figure crops at 200 DPI", "crop the figures out of the paper")

	turns := make([]beltTurn, 0, orchestratorToolCallCap+1)
	for index := 0; index < orchestratorToolCallCap; index++ {
		turns = append(turns, beltTurn{calls: []ai.ToolCall{
			beltCall(fmt.Sprintf("look%d", index), beltToolBoard, map[string]any{})}})
	}
	// The round past the bound, holding the work verbs and nothing else.
	turns = append(turns, beltTurn{calls: []ai.ToolCall{
		beltCall("hand", beltToolTask, map[string]any{
			"instruction": "extract the figures from the paper itself",
			"context":     true})}})
	client := &beltClient{turns: turns}
	session := "figures"
	user := postUser(t, graph, session, "no, extract them from the paper itself")
	if err := New(client, graph).answer(context.Background(), user); err != nil {
		t.Fatal(err)
	}

	if narrowed := client.beltOffered(orchestratorToolCallCap); !sameStrings(narrowed, beltWorkVerbNames()) {
		t.Fatalf("the round past the bound was offered %v, want the work verbs %v",
			narrowed, beltWorkVerbNames())
	}
	commands := pendingCommandsOf(t, graph)
	if len(commands) != 1 {
		t.Fatalf("the narrowed belt commissioned %d pieces of work, want one: %+v", len(commands), commands)
	}
	command := commands[0]
	if command.Instruction != "extract the figures from the paper itself" {
		t.Fatalf("the ask is not the person's own words: %q", command.Instruction)
	}
	// The whole value of the handoff: what this turn found out is in front of the
	// work, in the context column beside the conversation rather than inside the
	// ask.
	if !strings.Contains(command.Context, "Figure crops at 200 DPI") {
		t.Fatalf("the turn's own discoveries never reached the work:\n%s", command.Context)
	}
	if !strings.Contains(command.Context, "no, extract them from the paper itself") {
		t.Fatalf("the conversation stopped travelling with the work:\n%s", command.Context)
	}

	reply := waitForAgentReply(t, graph, session, user.Seq)
	if !strings.Contains(strings.ToLower(reply.Body), "queued") {
		t.Fatalf("the reply never says the work is in hand: %q", reply.Body)
	}
	for _, leak := range []string{"belt", "tool call", "next turn", "this message"} {
		if strings.Contains(strings.ToLower(reply.Body), leak) {
			t.Fatalf("the machinery leaked into the room (%q): %q", leak, reply.Body)
		}
	}
}

// The bound still bounds. A narrowed round holds two verbs, a call to anything
// else is refused rather than dispatched, and the turn ends after that one round
// however much work the model still thinks it has.
func TestTheNarrowedRoundCannotWorkAndEndsTheTurn(t *testing.T) {
	graph := openHeadStore(t)
	seedExceptBoard(t, graph)
	turns := make([]beltTurn, 0, orchestratorToolCallCap+2)
	for index := 0; index < orchestratorToolCallCap; index++ {
		turns = append(turns, beltTurn{calls: []ai.ToolCall{
			beltCall(fmt.Sprintf("look%d", index), beltToolBoard, map[string]any{})}})
	}
	// A round that carries on as though nothing had changed: one read, one hand.
	turns = append(turns, beltTurn{calls: []ai.ToolCall{
		beltCall("read", beltToolBoard, map[string]any{}),
		beltCall("hand", beltToolNote, map[string]any{
			"body": "they want the crops at 200 DPI", "scope": "user"})}})
	// And a round that must never happen.
	turns = append(turns, beltTurn{text: "Here are the crops."})
	client := &beltClient{turns: turns}
	session := "runaway"
	user := postUser(t, graph, session, "keep going")
	if err := New(client, graph).answer(context.Background(), user); err != nil {
		t.Fatal(err)
	}

	narrowed := client.beltOffered(orchestratorToolCallCap)
	for _, gone := range []string{beltToolBoard, beltToolNote, beltToolBash, beltToolWrite} {
		for _, offered := range narrowed {
			if offered == gone {
				t.Fatalf("the narrowed belt still carries %q: %v", gone, narrowed)
			}
		}
	}
	if _, tooled := client.counts(); tooled != orchestratorToolCallCap+1 {
		t.Fatalf("the turn ran %d tooled rounds, want the cap plus one narrowed round", tooled)
	}
	facts, err := graph.RecentFacts(5)
	if err != nil {
		t.Fatal(err)
	}
	if len(facts) != 0 {
		t.Fatalf("a hand the narrowed belt does not carry was dispatched: %+v", facts)
	}
	// Nothing was journaled and nothing was said, so the turn lands on the floor
	// every wordless turn has always landed on. It is reachable here only because
	// the scripted model calls tools it was not given; a provider that honours
	// its own tool list ends this round in words or in a receipt.
	if reply := waitForAgentReply(t, graph, session, user.Seq); reply.Body != providerErrorReply {
		t.Fatalf("a turn that did nothing and said nothing replied %q", reply.Body)
	}
}

// What the handoff carries is what the reads came back with, newest kept when it
// will not all fit: the end of an investigation is the part the work cannot
// cheaply repeat.
func TestTheHandoffKeepsTheEndOfTheInvestigation(t *testing.T) {
	run := &beltRun{}
	for index := 0; index < 40; index++ {
		run.learned(beltToolBoard, mustJSON(map[string]any{"q": fmt.Sprintf("look %d", index)}),
			strings.Repeat("x", turnFindingBytes*2))
	}
	findings := run.findings()
	if len(findings) > turnFindingsBytes+turnFindingBytes {
		t.Fatalf("the handoff block is %d bytes, past its bound of %d", len(findings), turnFindingsBytes)
	}
	if !strings.Contains(findings, "look 39") {
		t.Fatal("the last thing the turn found out was dropped")
	}
	if strings.Contains(findings, "look 0»") {
		t.Fatal("the first read survived a block that could not hold everything")
	}
	if !strings.HasPrefix(findings, turnFindingsHeader) {
		t.Fatalf("the block does not say what it is:\n%s", findings)
	}
	// A tool result that came back as a page stays under its own bullet.
	paged := &beltRun{}
	paged.learned(beltToolBoard, mustJSON(map[string]any{}), "first row\nsecond row")
	if !strings.Contains(paged.findings(), "\n  second row") {
		t.Fatalf("a finding's second line escaped its bullet:\n%s", paged.findings())
	}
}

// A turn that never narrowed hands over exactly what it always did: the ask, and
// the conversation when the model asked for it.
func TestAnOrdinaryTaskCarriesNoTurnFindings(t *testing.T) {
	graph := openHeadStore(t)
	client := &beltClient{turns: []beltTurn{
		{calls: []ai.ToolCall{beltCall("c1", beltToolBoard, map[string]any{})}},
		{calls: []ai.ToolCall{beltCall("c2", beltToolTask, map[string]any{
			"instruction": "write the quarterly summary", "context": true})}},
		{text: "That's in hand."},
	}}
	session := "ordinary"
	user := postUser(t, graph, session, "write the quarterly summary")
	if err := New(client, graph).answer(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	commands := pendingCommandsOf(t, graph)
	if len(commands) != 1 {
		t.Fatalf("want one commissioned piece of work, got %+v", commands)
	}
	if strings.Contains(commands[0].Context, turnFindingsHeader) {
		t.Fatalf("an unnarrowed turn sent its own tool results along:\n%s", commands[0].Context)
	}
	if !strings.Contains(commands[0].Context, "write the quarterly summary") {
		t.Fatalf("the conversation stopped travelling: %q", commands[0].Context)
	}
}
