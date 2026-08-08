package head

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/resident"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

// sharedPrefix is the whole economics of these tests in one function: an
// OpenAI-compatible endpoint bills everything from the first differing byte
// onward at full price, so the length of this string is the part of a prompt
// that did not have to be paid for twice.
func sharedPrefix(first, second string) int {
	limit := len(first)
	if len(second) < limit {
		limit = len(second)
	}
	for index := 0; index < limit; index++ {
		if first[index] != second[index] {
			return index
		}
	}
	return limit
}

// The router's prompt across one ordinary state tick — a cent of spend, one more
// message in the thread. Everything that moved must be behind everything that
// did not, which is the only property that makes the prefix worth anything.
func TestRouterPromptChurnStaysInTheSuffix(t *testing.T) {
	graph := openHeadStore(t)
	seedResultBoard(t, graph)
	for index := 0; index < 4; index++ {
		postUser(t, graph, "steady", fmt.Sprintf("earlier message %d", index))
	}

	route := func(body string) (system, user string) {
		t.Helper()
		client := &fakeClient{responses: []string{`{"reply":"noted","command":null}`}}
		message := postUser(t, graph, "steady", body)
		head := New(client, graph).WithDailyBudgetUSD(20).
			WithSelfKnowledge(func() string { return "reflex: median 200 tokens, 2 turns; n=10" })
		if err := head.answer(context.Background(), message); err != nil {
			t.Fatalf("answer %q: %v", body, err)
		}
		if len(client.seen) < 2 {
			t.Fatalf("%q never reached the router: %+v", body, client.seen)
		}
		return client.seen[0].Content[0].Text, client.seen[1].Content[0].Text
	}

	firstSystem, first := route("what is running?")
	if err := graph.RecordUsage(store.NodeUsage{NodeID: store.RootID, Cost: 0.01}); err != nil {
		t.Fatal(err)
	}
	if _, err := graph.RecordFact("", "user", store.FactPreference, "keep replies short with no preamble"); err != nil {
		t.Fatal(err)
	}
	secondSystem, second := route("and now?")

	// The system message is one constant plus standing voice. A new voice
	// preference may extend it; the message the user just typed may not change
	// it at all, which is what dropping the retrieval cue bought.
	if !strings.HasPrefix(secondSystem, headSystemPrompt) || !strings.HasPrefix(firstSystem, headSystemPrompt) {
		t.Fatal("the head's system message no longer opens with its constant prompt")
	}
	if sharedPrefix(firstSystem, secondSystem) < len(headSystemPrompt) {
		t.Fatalf("system message diverged inside the constant prompt at byte %d", sharedPrefix(firstSystem, secondSystem))
	}

	floor := strings.Index(first, "\n\nLive graph snapshot:")
	if floor <= 0 {
		t.Fatalf("no volatile floor in the router prompt:\n%s", first)
	}
	if shared := sharedPrefix(first, second); shared < floor {
		t.Fatalf("router prompt churned at byte %d, before the volatile floor at %d:\n%s", shared, floor, first[:floor])
	}
	// And the fast-moving facts really are down there, under everything else.
	spend := strings.Index(first, "today's spend: $")
	if spend < floor {
		t.Fatalf("the spend line sits above the volatile floor: spend=%d floor=%d", spend, floor)
	}
	if message := strings.Index(first, "\n\nCurrent user message (verbatim):"); spend > message {
		t.Fatalf("the spend line is not the last thing before the message: spend=%d message=%d", spend, message)
	}
}

// The window fills to threadWindowMax and then drops to threadWindowKeep in one
// cut. What is being asserted is the cut's rarity: ten messages in a row that
// append to an unchanged front, and exactly one that moves it.
func TestThreadWindowMovesInBigSteps(t *testing.T) {
	graph := openHeadStore(t)
	head := New(nil, graph)

	fronts := make([]string, 0, threadWindowMax+2)
	var lengths []int
	for count := 1; count <= threadWindowMax+2; count++ {
		message := postUser(t, graph, "window", fmt.Sprintf("message %d", count))
		recent, err := head.recentThread("window", message.Seq)
		if err != nil {
			t.Fatal(err)
		}
		if count <= threadWindowMax+1 && len(recent) != count-1 {
			t.Fatalf("window at %d messages held %d", count-1, len(recent))
		}
		front := ""
		if len(recent) > 0 {
			front = recent[0].Body
		}
		fronts = append(fronts, front)
		lengths = append(lengths, len(recent))
	}

	// Reading before message N sees N-1 earlier ones. The front is message 1
	// until the window overflows, which happens on the read before message 22 —
	// the first read whose thread holds threadWindowMax+1 messages.
	for index := 2; index <= threadWindowMax+1; index++ {
		if fronts[index-1] != "message 1" {
			t.Fatalf("front moved early: read before message %d starts at %q", index, fronts[index-1])
		}
	}
	final := fronts[threadWindowMax+1]
	if want := fmt.Sprintf("message %d", threadWindowMax+2-threadWindowKeep); final != want {
		t.Fatalf("the big step landed wrong: front is %q, want %q", final, want)
	}
	if got := lengths[threadWindowMax+1]; got != threadWindowKeep {
		t.Fatalf("after the cut the window held %d messages, want %d", got, threadWindowKeep)
	}
}

// Reading the same session twice must render the same bytes; a window that
// depended on how the store paged its messages would be a cache miss by
// accident and a different prompt by accident.
func TestThreadWindowIsAPureFunctionOfTheSession(t *testing.T) {
	graph := openHeadStore(t)
	head := New(nil, graph)
	for index := 0; index < threadWindowMax+5; index++ {
		postUser(t, graph, "repeat", fmt.Sprintf("message %d", index))
	}
	latest := postUser(t, graph, "repeat", "the current one")
	first, err := head.recentThread("repeat", latest.Seq)
	if err != nil {
		t.Fatal(err)
	}
	second, err := head.recentThread("repeat", latest.Seq)
	if err != nil {
		t.Fatal(err)
	}
	if head.renderThread(first) != head.renderThread(second) {
		t.Fatal("two reads of one session rendered different threads")
	}
}

// Cents on the board rewrote the control loop's first block between messages.
// Dimes hold still for as long as a person's decision would.
func TestBoardCostRendersInDimes(t *testing.T) {
	rows := []boardRow{{node: store.Node{ID: "job", Status: store.Running}, running: 1, cost: 0.37}}
	ticked := []boardRow{{node: store.Node{ID: "job", Status: store.Running}, running: 1, cost: 0.38}}
	if renderBoard(rows) != renderBoard(ticked) {
		t.Fatalf("a cent of spend rewrote the board:\n%s\n%s", renderBoard(rows), renderBoard(ticked))
	}
	if !strings.Contains(renderBoard(rows), "$0.40") {
		t.Fatalf("board cost was not rounded to a dime: %s", renderBoard(rows))
	}
	dear := []boardRow{{node: store.Node{ID: "job", Status: store.Running}, running: 1, cost: 1.44}}
	if !strings.Contains(renderBoard(dear), "$1.40") {
		t.Fatalf("board cost rounded away from the nearest dime: %s", renderBoard(dear))
	}
}

// Voice preferences are standing style, so the section must not be rewritten by
// whatever the user happened to type — and a newly learned one must append.
func TestVoiceSectionAppendsRatherThanReordering(t *testing.T) {
	graph := openHeadStore(t)
	if _, err := graph.RecordFact("", "user", store.FactPreference, "keep replies short"); err != nil {
		t.Fatal(err)
	}
	before := resident.VoicePrompt(graph, headSystemPrompt)
	if _, err := graph.RecordFact("", "user", store.FactPreference, "never open with an apology in a reply"); err != nil {
		t.Fatal(err)
	}
	after := resident.VoicePrompt(graph, headSystemPrompt)
	if before == after {
		t.Fatal("the new preference never reached the voice section")
	}
	if !strings.HasPrefix(after, before) {
		t.Fatalf("a new preference rewrote the section instead of appending:\nbefore:\n%s\n\nafter:\n%s", before, after)
	}
}
