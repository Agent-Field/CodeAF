package head

import (
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// 8.2.9's whole bargain in one test: the record keeps the exchange, the
// orchestrator's context does not.
//
// Both halves have to hold at once, which is why they are asserted together. A
// version that dropped the exchange would be cheap and dishonest; a version
// that journaled it as an ordinary turn would be honest and would make every
// later prompt in that room carry a question nobody will refer to again.
func TestAnAsideIsDurableAsAStubAndAbsentFromTheNextPrompt(t *testing.T) {
	graph := openHeadStore(t)
	postUser(t, graph, "curious", "start the market research")
	aside := &fakeClient{responses: []string{
		"It picked the filings first because they are the only source that is dated."}}
	head := New(aside, graph)

	answer, err := head.Ask(context.Background(), "curious",
		"why did it start with the filings rather than the news?")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(answer, "only source that is dated") {
		t.Fatalf("the aside answered %q", answer)
	}

	// DURABLE: one row, collapsed, with the whole exchange under it.
	stub := lastSystemRow(t, graph, "curious")
	if !strings.HasPrefix(stub.Body, AsideStub) {
		t.Fatalf("the stub is not the collapsed line: %q", stub.Body)
	}
	if !strings.Contains(stub.Body, "why did it start with the filings") {
		t.Fatalf("the stub does not say what was asked, so nothing can refer back to it: %q", stub.Body)
	}
	if strings.Contains(stub.Body, "only source that is dated") {
		t.Fatalf("the answer is in the BODY, which is the one place it costs every later turn: %q", stub.Body)
	}
	part, found := asidePartOf(stub)
	if !found {
		t.Fatalf("the exchange was not kept: %+v", stub.Parts)
	}
	if !strings.Contains(part.Question, "rather than the news") || !strings.Contains(part.Answer, "only source that is dated") {
		t.Fatalf("the expandable half lost a side of the exchange: %+v", part)
	}

	// ABSENT: the next ordinary turn's prompt carries the one line and neither
	// half of what was actually said.
	loop := &beltClient{turns: []beltTurn{{text: "Going well."}}}
	next := postUser(t, graph, "curious", "how is it going?")
	if err := New(loop, graph).answer(context.Background(), next); err != nil {
		t.Fatal(err)
	}
	opening := loop.openingPrompt()
	if strings.Contains(opening, "only source that is dated") {
		t.Fatalf("the aside's answer rode into the orchestrator's context:\n%s", opening)
	}
	if !strings.Contains(opening, AsideStub) {
		t.Fatalf("the collapsed line never reached the conversation — the person's echo is missing:\n%s", opening)
	}
}

// The aside runs under its own cache key rather than below the orchestrator's
// volatile floor. 12.4.1's prefix ordering is regression-tested and paid for on
// every ordinary message; an ephemeral turn that shared that prefix would
// compete with it for one entry and cost more than the turn it saved.
func TestAnAsideSharesNoPrefixWithTheOrchestratorAndCarriesNoTools(t *testing.T) {
	graph := openHeadStore(t)
	postUser(t, graph, "cache", "run the audit")
	aside := &fakeClient{responses: []string{"It is on the second of four steps."}}
	head := New(aside, graph)
	if _, err := head.Ask(context.Background(), "cache", "which step is it on?"); err != nil {
		t.Fatal(err)
	}

	system := aside.systemPrompt()
	if system != asidePrompt {
		t.Fatalf("the aside did not use its own system message:\n%s", system)
	}
	// Not one shared byte at the front: a different prefix from byte zero is
	// what "its own cache key" means on an endpoint that bills by prefix.
	if shared := sharedPrefix(system, orchestratorPrompt); shared != 0 {
		t.Fatalf("the aside shares %d bytes of prefix with the orchestrator's prompt", shared)
	}
	// And no belt. An aside is curiosity, curiosity has no authority, and the
	// definitions are the largest single block a call can avoid paying for.
	if len(aside.seen) != 2 {
		t.Fatalf("the aside sent %d messages, want a system and a user", len(aside.seen))
	}
	if commands, _ := graph.PendingCommands(10); len(commands) != 0 {
		t.Fatalf("an aside changed the world: %+v", commands)
	}
}

// The ordering the whole cache rests on is unchanged by any of this — the same
// assertion 12.4.1 pinned, re-run with an aside in the room, because the one
// way an ephemeral turn earns its keep and then loses it is by moving a block.
func TestAsidesLeaveTheVolatilityOrderAlone(t *testing.T) {
	graph := openHeadStore(t)
	postUser(t, graph, "order", "start the audit")
	if _, err := New(&fakeClient{responses: []string{"On step two."}}, graph).
		Ask(context.Background(), "order", "which step?"); err != nil {
		t.Fatal(err)
	}

	client := &fakeClient{responses: []string{"noted"}}
	head := New(client, graph).WithDailyBudgetUSD(20).
		WithSelfKnowledge(func() string { return "reflex: median 200 tokens" })
	message := postUser(t, graph, "order", "and now?")
	if err := head.answer(context.Background(), message); err != nil {
		t.Fatal(err)
	}
	prompt := client.userPrompt()
	if !strings.HasPrefix(prompt, "Recent thread before this message:\n") {
		t.Fatalf("the append-only block is no longer first:\n%s", prompt)
	}
	positions := []struct {
		name   string
		marker string
	}{
		{"measured history", "\n\nMeasured execution history"},
		{"board", "\n\nLive board (the work you can read and act on):"},
		{"notebook", "\n\nNotebook ("},
		{"clock", "\n\nnow: "},
		{"spend", "today's spend: $"},
		{"message", "\n\nCurrent user message (verbatim):"},
	}
	previous := 0
	for _, block := range positions {
		at := strings.Index(prompt, block.marker)
		if at < 0 {
			t.Fatalf("the %s block vanished:\n%s", block.name, prompt)
		}
		if at < previous {
			t.Fatalf("the %s block moved above the one before it", block.name)
		}
		previous = at
	}
}

// An aside is asked beside a turn, never through it, so it must leave the
// conversation's own turn machinery exactly as it found it.
func TestAnAsideDoesNotDisturbTheTurnInFlight(t *testing.T) {
	graph := openHeadStore(t)
	postUser(t, graph, "busy", "run it")
	head := New(&fakeClient{responses: []string{"Halfway."}}, graph)

	stopped := make(chan struct{})
	head.turnMu.Lock()
	head.turnCancel = func() { close(stopped) }
	head.turnPartial = "half an answer"
	head.turnMu.Unlock()

	if _, err := head.Ask(context.Background(), "busy", "how far in is it?"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-stopped:
		t.Fatal("an aside cancelled the turn it was asked beside")
	default:
	}
	head.turnMu.Lock()
	partial := head.turnPartial
	head.turnMu.Unlock()
	if partial != "half an answer" {
		t.Fatalf("an aside overwrote the turn's partial: %q", partial)
	}
}

// The stub is a system row on purpose, and the purpose is three separate
// failures it forecloses at once.
func TestTheStubIsASystemRowSoNothingMistakesItForATurn(t *testing.T) {
	graph := openHeadStore(t)
	postUser(t, graph, "shape", "go")
	head := New(&fakeClient{responses: []string{"Because it was cheaper."}}, graph)
	// A question mark at the end is the shape that would be misread as an
	// askback by a reader that scans agent prose for one.
	if _, err := head.Ask(context.Background(), "shape", "why that one?"); err != nil {
		t.Fatal(err)
	}
	stub := lastSystemRow(t, graph, "shape")
	if stub.Role != store.RoleSystem {
		t.Fatalf("the stub is a %q row", stub.Role)
	}
	if len(stub.Options) != 0 {
		t.Fatalf("the stub offers answers: %+v", stub.Options)
	}
	// One line, because this line lands in every later prompt for this room.
	if strings.Contains(stub.Body, "\n") {
		t.Fatalf("the collapsed row is not one line: %q", stub.Body)
	}
	if _, err := head.Ask(context.Background(), "shape", ""); err != ErrAsideEmpty {
		t.Fatalf("an empty aside was accepted: %v", err)
	}
}

// The deltas are keyed apart from the room's own turn. 8.2.9 named this as the
// prerequisite, and one key per ROOM is not enough: an aside can run while that
// room's turn is streaming, and the two sharing a key is exactly the bug where
// an ephemeral turn types itself into the transcript.
func TestAsideDeltasAreKeyedApartFromTheRoomsOwnTurn(t *testing.T) {
	if AsideStreamSession("room") == "room" {
		t.Fatal("an aside streams under the room's own key")
	}
	if !strings.HasPrefix(AsideStreamSession("room"), "room") {
		t.Fatalf("the aside key does not name its room: %q", AsideStreamSession("room"))
	}
}

func asidePartOf(message store.Message) (store.AsidePart, bool) {
	for _, part := range message.Parts {
		if part.Kind == store.PartAside && part.Aside != nil {
			return *part.Aside, true
		}
	}
	return store.AsidePart{}, false
}

func lastSystemRow(t *testing.T, graph *store.Store, session string) store.Message {
	t.Helper()
	messages, err := graph.Messages(session, 0, 0)
	if err != nil {
		t.Fatalf("read thread: %v", err)
	}
	for index := len(messages) - 1; index >= 0; index-- {
		if messages[index].Role == store.RoleSystem {
			return messages[index]
		}
	}
	t.Fatalf("nothing in %s posted a system row: %+v", session, messages)
	return store.Message{}
}
