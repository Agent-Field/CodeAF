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
	// The order is stable-first: thread, then the snapshot and the notebook that
	// move with every message, then the message. It changed once, deliberately,
	// when the router was reshaped for prefix caching; the blocks and their
	// bytes did not.
	thread := "(no earlier messages in this session)"
	want := "Recent thread before this message:\n" + thread +
		"\n\nLive graph snapshot:\n" + renderGraph(snapshot, "greeting", thread, nil) +
		"\n\nNotebook (durable memory across jobs and conversations):\n" + renderNotebook(graph, greeting, thread) +
		"\n\nCurrent user message (verbatim):\n" + greeting
	if prompt != want {
		t.Fatalf("greeting context drifted from today's:\ngot:\n%s\n\nwant:\n%s", prompt, want)
	}
	if deep, _ := New(nil, graph).renderDeep(greeting, ""); deep != "" {
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
		if deep, _ := head.renderDeep(message, ""); deep != "" {
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

	if deep, _ := head.renderDeep(message, "(no earlier messages in this session)"); deep == "" {
		t.Fatal("the finance question earned no depth at all")
	}
	thread := "agent: " + financeFinding
	if deep, _ := head.renderDeep(message, thread); deep != "" {
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
	if skeleton := renderGraph(snapshot, "", "", nil); len(skeleton) > maxGraphContextBytes {
		t.Fatalf("board skeleton = %d bytes, over its %d budget", len(skeleton), maxGraphContextBytes)
	}
	deep, _ := New(nil, graph).renderDeep("what did the ledger reconciliation conclude", "")
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

// TestFilesLineNamesWhatTheResultDoesNot pins the order of cap and filter. The
// files line exists for the paths the rendered result does not already show, so
// capping the collected set before dropping the visible ones spends the cap on
// exactly the paths that needed no second mention — and a job whose first six
// paths are all quoted in its result prints no files line at all, losing the
// seventh, which was the only one worth printing.
func TestFilesLineNamesWhatTheResultDoesNot(t *testing.T) {
	visible := make([]string, 0, deepFileCap)
	for index := 0; index < deepFileCap; index++ {
		visible = append(visible, fmt.Sprintf("/tmp/aforge/seen/%02d.md", index))
	}
	hidden := "/tmp/aforge/cut/late.md"
	node := store.Node{
		ID:      "wide",
		Summary: strings.Join(visible, "\n") + "\n" + hidden,
	}
	body := strings.Join(visible, "\n")

	files := unnamedFiles(node, body)
	if len(files) != 1 || files[0] != hidden {
		t.Fatalf("files line = %v, want only the path the result never shows (%s)", files, hidden)
	}
	if len(unnamedFiles(node, "")) != deepFileCap {
		t.Errorf("with nothing visible the line holds %d paths, want the %d cap",
			len(unnamedFiles(node, "")), deepFileCap)
	}
}

// TestDedupProbeMeasuresBeforeItTrims pins the order of floor and trim. The
// floor asks whether the result's first line is substantial enough to match on;
// measuring it after the truncation ellipsis has been trimmed off let a line
// just over the floor fall under it and decline a dedup that was real, sending
// the same paragraph into the prompt twice.
func TestDedupProbeMeasuresBeforeItTrims(t *testing.T) {
	// One byte over the floor, ending in the ellipsis the trim removes.
	result := strings.Repeat("a", deepDedupFloorBytes-len("…")+1) + "…"
	if len(result) <= deepDedupFloorBytes {
		t.Fatalf("probe fixture is %d bytes, must sit above the %d floor", len(result), deepDedupFloorBytes)
	}
	if !deepAlreadyInThread("agent: "+result, result) {
		t.Error("a first line above the floor was refused a dedup because the trim shortened it")
	}
	short := strings.Repeat("b", deepDedupFloorBytes-1)
	if deepAlreadyInThread("agent: "+short, short) {
		t.Error("a first line under the floor deduped on too little evidence")
	}
}

// TestTruncationMarkersFitTheirBudget counts the marker against the ceiling it
// announces. Written after the check rather than reserved before it, it put the
// block over the very budget the check exists to hold.
func TestTruncationMarkersFitTheirBudget(t *testing.T) {
	// Uniform 64-byte lines tile the 4KB budget exactly, so the last line that
	// fits leaves no slack at all and the marker has to have been reserved.
	const line = 64
	body := strings.Repeat("m", line-len("user: \n"))
	messages := make([]store.Message, 0, 2*maxThreadContextBytes/line)
	for index := 0; index < cap(messages); index++ {
		messages = append(messages, store.Message{Role: store.RoleUser, Body: body})
	}
	thread := renderThread(messages)
	if !strings.Contains(thread, strings.TrimSpace(threadTruncatedMark)) {
		t.Fatalf("the thread never truncated, so the marker is untested:\n%s", thread)
	}
	if len(thread) > maxThreadContextBytes {
		t.Errorf("thread = %d bytes with its marker, over the %d budget", len(thread), maxThreadContextBytes)
	}

	// The board's rows are the store's to shape, so the budget is checked across
	// a spread of row widths: whatever the last row that fits leaves behind, the
	// marker has to fit inside it.
	for width := 56; width <= 72; width++ {
		graph := openHeadStore(t)
		prefix := len("- board-00 | " + string(store.Pending) + " | \n")
		if width <= prefix {
			t.Fatalf("row width %d leaves no room for a brief", width)
		}
		brief := strings.Repeat("b", width-prefix)
		for index := 0; index < 2*maxGraphContextBytes/width; index++ {
			spliceSurgeryJob(t, graph, fmt.Sprintf("board-%02d", index), "", brief)
		}
		snapshot, err := graph.ActiveSnapshot()
		if err != nil {
			t.Fatal(err)
		}
		board := renderGraph(snapshot, "", "", nil)
		if !strings.Contains(board, strings.TrimSpace(snapshotTruncatedMark)) {
			t.Fatalf("width %d: the board never truncated, so the marker is untested", width)
		}
		if len(board) > maxGraphContextBytes {
			t.Errorf("width %d: board = %d bytes with its marker, over the %d budget",
				width, len(board), maxGraphContextBytes)
		}
	}
}
