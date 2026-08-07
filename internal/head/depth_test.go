package head

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// The live failure, seeded. A settled finance job whose findings are in its
// summary and in a file it wrote, a settled podcast job whose findings must not
// leak into an answer about finance, and one job still queued.
const (
	financeFinding = "Net revenue landed at $4.21M, up 8.4% on Q2, and both disputed vendor invoices resolved in our favour."
	financeDetail  = "Three accruals were reclassified and the ledger balances to the cent."
	financeFile    = "/tmp/aforge/finance/q3-close.md"
	podcastFinding = "Episode 12 is cut to 31 minutes."
	podcastDetail  = "Levels were normalised to -16 LUFS and the intro sting was replaced."
	podcastFile    = "/tmp/aforge/podcast/ep12.mp3"
)

func seedResultBoard(t *testing.T, graph *store.Store) {
	t.Helper()
	spliceSurgeryJob(t, graph, "finance-close", "Finance close", "close the finance books for Q3")
	completeNodeWith(t, graph, "finance-close",
		financeFinding+"\n"+financeDetail+"\n"+financeFile)
	spliceSurgeryJob(t, graph, "podcast-edit", "Podcast edit", "edit the podcast episode")
	completeNodeWith(t, graph, "podcast-edit",
		podcastFinding+"\n"+podcastDetail+"\n"+podcastFile)
	spliceSurgeryJob(t, graph, "line-scans", "Line scans", "scan the lines")
}

func completeNodeWith(t *testing.T, graph *store.Store, id, summary string) {
	t.Helper()
	claim, ok, err := graph.Claim(id, "tester")
	if err != nil || !ok {
		t.Fatalf("claim %s: ok=%t err=%v", id, ok, err)
	}
	if err := graph.Start(claim); err != nil {
		t.Fatalf("start %s: %v", id, err)
	}
	if err := graph.Complete(claim, summary); err != nil {
		t.Fatalf("complete %s: %v", id, err)
	}
}

// routerPrompt runs one message all the way through the head and returns what
// the routing client was actually asked. Everything below is an assertion about
// that one string, because that string is the whole of what the model knows.
func routerPrompt(t *testing.T, graph *store.Store, session, body string) string {
	t.Helper()
	client := &fakeClient{responses: []string{`{"reply":"noted","command":null}`}}
	user := postUser(t, graph, session, body)
	if err := New(client, graph).answer(context.Background(), user); err != nil {
		t.Fatalf("answer %q: %v", body, err)
	}
	if len(client.seen) < 2 {
		t.Fatalf("%q never reached the router: %+v", body, client.seen)
	}
	return client.seen[1].Content[0].Text
}

// The failure itself: "it was completed" was all the head could say because the
// findings were never in the prompt. Now they are — and the podcast's are not.
func TestFinanceQuestionCarriesTheFindingsAndNotTheOtherJobs(t *testing.T) {
	graph := openHeadStore(t)
	seedResultBoard(t, graph)
	prompt := routerPrompt(t, graph, "finance", "what happened with the finance thing")

	for _, wanted := range []string{financeFinding, financeDetail, financeFile} {
		if !strings.Contains(prompt, wanted) {
			t.Fatalf("router prompt missed the finance substance %q:\n%s", wanted, prompt)
		}
	}
	for _, unwanted := range []string{podcastDetail, podcastFile} {
		if strings.Contains(prompt, unwanted) {
			t.Fatalf("an unmatched job leaked into the prompt: %q", unwanted)
		}
	}
	// The board is the floor, not the casualty. Every job still has its line.
	for _, id := range []string{"finance-close", "podcast-edit", "line-scans"} {
		if !strings.Contains(prompt, "- "+id+" | ") {
			t.Fatalf("depth evicted %s from the board:\n%s", id, prompt)
		}
	}
}

// The regression guard. A message about nothing on the graph must produce the
// prompt it produced before any of this existed, to the byte.
func TestGreetingProducesTodaysContextExactly(t *testing.T) {
	graph := openHeadStore(t)
	seedResultBoard(t, graph)
	greeting := "good morning"
	prompt := routerPrompt(t, graph, "greeting", greeting)

	snapshot, err := graph.ActiveSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	want := "Live graph snapshot:\n" + renderGraph(snapshot) +
		"\n\nNotebook (durable memory across jobs and conversations):\n" + renderNotebook(graph, greeting) +
		"\n\nRecent thread before this message:\n(no earlier messages in this session)" +
		"\n\nCurrent user message (verbatim):\n" + greeting
	if prompt != want {
		t.Fatalf("greeting context drifted from today's:\ngot:\n%s\n\nwant:\n%s", prompt, want)
	}
	if deep := New(nil, graph).renderDeep(greeting, ""); deep != "" {
		t.Fatalf("a greeting bought depth: %q", deep)
	}
}

// A new work request matches nothing settled and stays as cheap as a greeting.
func TestNewWorkRequestBuysNoDepth(t *testing.T) {
	graph := openHeadStore(t)
	seedResultBoard(t, graph)
	head := New(nil, graph)
	for _, message := range []string{
		"build me a websocket echo server",
		"thanks!",
		"hey",
	} {
		if deep := head.renderDeep(message, ""); deep != "" {
			t.Fatalf("%q bought depth it did not earn:\n%s", message, deep)
		}
	}
}

// Pollution guard. A result the model can already read in the thread is not
// worth a second copy, and the budget it would spend belongs to another job.
func TestResultAlreadyInTheThreadGetsNoDeepSlice(t *testing.T) {
	graph := openHeadStore(t)
	seedResultBoard(t, graph)
	head := New(nil, graph)
	message := "what happened with the finance thing"

	if deep := head.renderDeep(message, "(no earlier messages in this session)"); deep == "" {
		t.Fatal("the finance question earned no depth at all")
	}
	thread := "agent: " + financeFinding
	if deep := head.renderDeep(message, thread); deep != "" {
		t.Fatalf("a result already in the thread was sent twice:\n%s", deep)
	}
}

// The two budgets are separate and both hold. Breadth is never evicted by
// depth, and depth is never allowed to grow into the prompt on its own.
func TestBreadthAndDepthKeepTheirOwnBudgets(t *testing.T) {
	graph := openHeadStore(t)
	long := strings.Repeat("the reconciliation notes go on and on and on. ", 60)
	for index := 0; index < 40; index++ {
		id := fmt.Sprintf("routine-%02d", index)
		spliceSurgeryJob(t, graph, id, fmt.Sprintf("Routine errand %02d", index),
			fmt.Sprintf("run errand %02d", index))
		completeNodeWith(t, graph, id, fmt.Sprintf("Errand %02d closed clean.\n%s", index, long))
	}
	for index := 0; index < 4; index++ {
		id := fmt.Sprintf("ledger-%02d", index)
		spliceSurgeryJob(t, graph, id, fmt.Sprintf("Ledger reconciliation, part %02d", index),
			"reconcile the ledger")
		// The path trails a summary far longer than one slice's share, so it can
		// only reach the prompt if the files line names what truncation cut.
		completeNodeWith(t, graph, id, fmt.Sprintf("Ledger pass %02d closed clean.\n%s\n/tmp/aforge/ledger/%02d.md",
			index, long, index))
	}
	snapshot, err := graph.ActiveSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if skeleton := renderGraph(snapshot); len(skeleton) > maxGraphContextBytes {
		t.Fatalf("board skeleton = %d bytes, over its %d budget", len(skeleton), maxGraphContextBytes)
	}
	deep := New(nil, graph).renderDeep("what did the ledger reconciliation conclude", "")
	if deep == "" {
		t.Fatal("a question about settled work earned no depth")
	}
	if len(deep) > maxDeepContextBytes {
		t.Fatalf("deep block = %d bytes, over its %d budget", len(deep), maxDeepContextBytes)
	}
	if !strings.Contains(deep, "files: /tmp/aforge/ledger/") {
		t.Fatalf("truncation swallowed the artifact path with nothing naming it:\n%s", deep)
	}
	// Every slice starts on its own line, so the count of them is the count of
	// jobs opened; the result bodies below them are indented and never match.
	if opened := strings.Count(deep, "\n- "); opened > deepSliceLimit {
		t.Fatalf("deep block opened %d jobs, over the %d limit", opened, deepSliceLimit)
	}
}

// The other half of the same fix: depth the model asks for, after it has read
// the board and knows which row the user meant. It records nothing.
func TestResultToolReadsTheWholeFindingAndRecordsNothing(t *testing.T) {
	graph := openHeadStore(t)
	seedResultBoard(t, graph)
	spliceJobTree(t, graph, store.OriginUser, "",
		spec("audit", "", "Vendor audit", "audit the vendors"),
		spec("audit-a", "audit", "Read the contracts", "read the contracts"))
	completeNodeWith(t, graph, "audit-a", "Four contracts renew in March.")
	completeNodeWith(t, graph, "audit", "Two vendors are overcharging.\n/tmp/aforge/audit/vendors.md")

	run := &beltRun{head: New(nil, graph), user: store.Message{Body: "what did the audit find"}}
	whole, failed := run.result(map[string]any{"id": "audit"})
	if failed {
		t.Fatalf("result read failed: %s", whole)
	}
	for _, wanted := range []string{
		"Two vendors are overcharging.", "/tmp/aforge/audit/vendors.md",
		"done", "Four contracts renew in March.", "audit-a",
	} {
		if !strings.Contains(whole, wanted) {
			t.Fatalf("result read missed %q:\n%s", wanted, whole)
		}
	}
	if run.acted {
		t.Fatal("reading a result recorded an action")
	}

	missing, failed := run.result(map[string]any{"id": "no-such-job"})
	if !failed || !strings.Contains(missing, "read the board again") {
		t.Fatalf("unknown id failed=%v: %q", failed, missing)
	}
	if empty, failed := run.result(map[string]any{}); !failed || !strings.Contains(empty, "board read") {
		t.Fatalf("missing id failed=%v: %q", failed, empty)
	}
	if _, failed := run.result(map[string]any{"id": store.RootID}); !failed {
		t.Fatal("the spine answered a result read")
	}
}
