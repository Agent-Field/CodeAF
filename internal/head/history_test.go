package head

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// The head was handed a time-ordered board it could not read as time and asked
// a question it had no clock to interpret. One line fixes half of that.
func TestRouterPromptCarriesAClockBelowTheVolatileFloor(t *testing.T) {
	graph := openHeadStore(t)
	seedResultBoard(t, graph)
	prompt := routerPrompt(t, graph, "clock", "what did you do yesterday")

	clock := strings.Index(prompt, "\nnow: ")
	floor := strings.Index(prompt, "\n\nLive board (the work you can read and act on):")
	message := strings.Index(prompt, "\n\nCurrent user message (verbatim):")
	switch {
	case clock < 0:
		t.Fatalf("the prompt carries no clock:\n%s", prompt)
	case floor < 0:
		t.Fatalf("the prompt carries no board:\n%s", prompt)
	case clock < floor:
		t.Fatalf("the clock sits above the volatile floor: clock=%d floor=%d", clock, floor)
	case clock > message:
		t.Fatalf("the clock sits below the message: clock=%d message=%d", clock, message)
	}
	if !strings.HasSuffix(strings.SplitN(prompt[clock+1:], "\n", 2)[0], " local") {
		t.Fatalf("the clock does not say which clock it is: %q", prompt[clock:clock+40])
	}
}

// And the other half: a settled row says when it settled.
//
// Which row that is has moved. The board enumerates work that is moving, so a
// job that finished cleanly is no longer a row on it at all and its age travels
// with the read that does reach it — the deep slice the question buys, and the
// aimed board read behind it. The claim is unchanged: a reader can tell how old
// the thing they are being told about is, and work that has not finished is
// never given a finish age.
func TestSnapshotRowsCarryTheirAge(t *testing.T) {
	graph := openHeadStore(t)
	seedResultBoard(t, graph)
	head := New(nil, graph)

	deep, opened := head.renderDeep("close the finance books for Q3", "")
	if !opened["finance-close"] {
		t.Fatalf("the settled job was never opened:\n%s", deep)
	}
	if !strings.Contains(deep, "- finance-close | done | Finance close | finished ") {
		t.Fatalf("a settled row carries no age:\n%s", deep)
	}
	if strings.Contains(deep, "line-scans") {
		t.Fatalf("work that has not finished was opened as though it had:\n%s", deep)
	}

	// The board's own rows carry an age too, on the read that reaches settled
	// work, and the row for work that has not finished carries no finish age.
	rows, err := head.boardRows("age", "close the finance books", "", "")
	if err != nil {
		t.Fatal(err)
	}
	board := renderBoard(rows)
	if !strings.Contains(board, "- finance-close | ") || !strings.Contains(board, "ago") &&
		!strings.Contains(board, "just now") {
		t.Fatalf("an aimed board read lost the row's age:\n%s", board)
	}
	live, err := head.renderTurnBoard("age", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(live, "finished") {
		t.Fatalf("work that has not finished was given a finish age:\n%s", live)
	}
}

// The belt read itself: a window in, settled work out, newest first.
func TestHistoryBeltReadAnswersAWindow(t *testing.T) {
	graph := openHeadStore(t)
	deliverJob(t, graph, "history", "monday-audit", "Monday audit",
		"audit the ledger", "the ledger balances to the cent")
	head := New(nil, graph)
	run := &beltRun{head: head, user: store.Message{SessionID: "history"}}

	rendered, failed := run.history(map[string]any{})
	if failed {
		t.Fatalf("an unbounded history read failed: %s", rendered)
	}
	if !strings.Contains(rendered, "monday-audit") || !strings.Contains(rendered, "the ledger balances to the cent") {
		t.Fatalf("history did not name the settled job or what it found:\n%s", rendered)
	}

	// A window that ends before the work landed is honestly empty rather than
	// quietly wrong.
	past := time.Now().Add(-48 * time.Hour).Format("2006-01-02")
	empty, failed := run.history(map[string]any{"since": past, "until": past})
	if failed {
		t.Fatalf("a bounded history read failed: %s", empty)
	}
	if !strings.Contains(empty, "nothing settled") {
		t.Fatalf("a window with nothing in it did not say so: %s", empty)
	}

	// A bound nobody can parse is a tool error the model can read and fix, not
	// a silently unbounded read.
	if message, failed := run.history(map[string]any{"since": "last tuesday"}); !failed {
		t.Fatalf("an unparseable bound was accepted: %s", message)
	}
}

// A bare date used as an upper bound means through that whole day. "until
// Friday" that stopped at Friday midnight would drop Friday's work entirely.
func TestHistoryUpperBoundCoversTheWholeDay(t *testing.T) {
	lower, ok := parseHistoryBound("2026-08-06", false)
	if !ok || lower.Hour() != 0 || lower.Minute() != 0 {
		t.Fatalf("a bare date as a lower bound = %v ok=%t, want the first instant", lower, ok)
	}
	upper, ok := parseHistoryBound("2026-08-06", true)
	if !ok || upper.Day() != 6 || upper.Hour() != 23 {
		t.Fatalf("a bare date as an upper bound = %v ok=%t, want the last instant of the day", upper, ok)
	}
}

// The end-to-end claim: "what did you do yesterday" reaches the belt, the belt
// has a history read, and the answer is composed from what it returned. This is
// the journey that had neither a clock nor a retrieval path.
func TestWhatDidYouDoYesterdayIsAnswerableThroughTheBelt(t *testing.T) {
	graph := openHeadStore(t)
	deliverJob(t, graph, "yesterday", "monday-audit", "Monday audit",
		"audit the ledger", "the ledger balances to the cent")

	client := &toolClient{}
	user := postUser(t, graph, "yesterday", "what did you do yesterday?")
	if err := New(client, graph).answer(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	if !client.called {
		t.Fatal("the belt never reached a history read")
	}
	if !strings.Contains(client.result, "monday-audit") {
		t.Fatalf("the history read returned nothing about yesterday's work: %q", client.result)
	}
	reply := waitForAgentReply(t, graph, "yesterday", user.Seq)
	if !strings.Contains(reply.Body, "ledger") {
		t.Fatalf("the answer was not composed from the read: %q", reply.Body)
	}
}

// Tuesday's job, asked about on Thursday. Territory packing hides a settled job
// from ActiveNodes, and every other read in this package — the router's
// snapshot, the board, the deep slice, SearchSurgeryTargets — is derived from
// ActiveNodes, so packed work was gone from the head's world entirely. history
// reads the settled rows instead, and the id it hands back opens through the
// belt's ordinary result read.
func TestHistoryReachesTerritoryPackedWorkTheBoardCannotSee(t *testing.T) {
	graph := openHeadStore(t)
	members := packTerritory(t, graph)
	head := New(nil, graph)

	rows, err := head.boardRows("packed", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		for _, id := range members {
			if row.node.ID == id {
				t.Fatalf("the fixture is not packed: %s is still on the board", id)
			}
		}
	}

	run := &beltRun{head: head, user: store.Message{SessionID: "packed"}}
	rendered, failed := run.history(map[string]any{})
	if failed {
		t.Fatalf("history read failed: %s", rendered)
	}
	for _, id := range members {
		if !strings.Contains(rendered, id) {
			t.Fatalf("packed job %s is invisible to history:\n%s", id, rendered)
		}
	}
	// The topical half of the same gap: named in the user's own words, a packed
	// job comes back through the board's aimed read.
	named, failed := run.board(map[string]any{"q": "ledger packed-2"})
	if failed {
		t.Fatalf("an aimed board read failed: %s", named)
	}
	if !strings.Contains(named, "packed-2") {
		t.Fatalf("a packed job cannot be found by the words it is about:\n%s", named)
	}

	// And what history names, result can open.
	opened, failed := run.result(map[string]any{"id": members[0]})
	if failed {
		t.Fatalf("a packed job history named could not be opened: %s", opened)
	}
	if !strings.Contains(opened, "balanced the ledger") {
		t.Fatalf("the packed job's finding did not come back:\n%s", opened)
	}
}

// packTerritory settles four jobs, folds them, and packs them into a territory
// — the state a retrospective leaves behind within hours of the work landing.
func packTerritory(t *testing.T, graph *store.Store) []string {
	t.Helper()
	members := []string{"packed-1", "packed-2", "packed-3", "packed-4"}
	for _, id := range members {
		spliceSurgeryJob(t, graph, id, "Ledger "+id, "audit the ledger for "+id)
		completeNodeWith(t, graph, id, "balanced the ledger in "+id)
		if err := graph.Fold(id, "balanced the ledger in "+id, nil); err != nil {
			t.Fatalf("fold %s: %v", id, err)
		}
	}
	if err := graph.FormTerritory("territory-ledger", "Ledger work",
		"4 jobs balanced the ledger", nil, members); err != nil {
		t.Fatalf("form territory: %v", err)
	}
	return members
}

// toolClient drives one belt turn: call history, then speak. It is the smallest
// fake that proves the tool is reachable and its result is what the reply is
// written from.
type toolClient struct {
	called bool
	result string
	turn   int
}

func (client *toolClient) CompleteWithMessages(_ context.Context, messages []ai.Message, _ ...ai.Option) (*ai.Response, error) {
	client.turn++
	if client.turn == 1 {
		arguments, _ := json.Marshal(map[string]any{})
		return &ai.Response{Choices: []ai.Choice{{Message: ai.Message{
			Role: "assistant",
			ToolCalls: []ai.ToolCall{{ID: "1", Type: "function", Function: ai.ToolCallFunction{
				Name: beltToolHistory, Arguments: string(arguments),
			}}},
		}}}}, nil
	}
	client.called = true
	for _, message := range messages {
		if message.Role == "tool" && len(message.Content) > 0 {
			client.result = message.Content[0].Text
		}
	}
	return textResponse("Yesterday the ledger audit landed and balanced to the cent."), nil
}
