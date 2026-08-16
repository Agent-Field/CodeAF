package head

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/manual"
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

// deliverJob settles one job and announces it into the thread the way the
// reconciler does: a system message anchored to the node that produced it. That
// anchor is what every adjacency reading in this package is built on, so a
// fixture about delivered work has to carry it.
func deliverJob(t *testing.T, graph *store.Store, session, id, title, intent, summary string) {
	t.Helper()
	spliceSurgeryJob(t, graph, id, title, intent)
	completeNodeWith(t, graph, id, summary)
	if _, err := graph.PostMessage(store.Message{
		SessionID: session, Role: store.RoleSystem, Body: summary, NodeID: id,
	}); err != nil {
		t.Fatalf("announce %s: %v", id, err)
	}
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
// the model was actually asked. Everything below is an assertion about that one
// string, because that string is the whole of what the model knows.
//
// It keeps its name because other test files call it, and because what it means
// did not change: there is one prompt now instead of a router's and a belt's,
// and this is it. The client is scripted to speak once and call nothing, so the
// prompt returned is the opening one — the turn as it was handed over, before
// any tool result was appended to it.
func routerPrompt(t *testing.T, graph *store.Store, session, body string) string {
	t.Helper()
	client := &beltClient{turns: []beltTurn{{text: "noted"}}}
	user := postUser(t, graph, session, body)
	if err := New(client, graph).answer(context.Background(), user); err != nil {
		t.Fatalf("answer %q: %v", body, err)
	}
	opening := client.openingPrompt()
	if opening == "" {
		t.Fatalf("%q never reached the loop", body)
	}
	return opening
}

// The failure itself: "it was completed" was all the head could say because the
// findings were never in the prompt. Now they are — and the podcast's are not.
func TestFinanceQuestionCarriesTheFindingsAndNotTheOtherJobs(t *testing.T) {
	graph := openHeadStore(t)
	seedResultBoard(t, graph)
	prompt := routerPrompt(t, graph, "finance", "what happened with the finance thing")

	for _, wanted := range []string{financeFinding, financeDetail, financeFile} {
		if !strings.Contains(prompt, wanted) {
			t.Fatalf("prompt missed the finance substance %q:\n%s", wanted, prompt)
		}
	}
	for _, unwanted := range []string{podcastFinding, podcastDetail, podcastFile} {
		if strings.Contains(prompt, unwanted) {
			t.Fatalf("an unmatched job leaked into the prompt: %q", unwanted)
		}
	}
	// The board is the floor, not the casualty: the depth budget is spent
	// entirely on one job and the work that is still moving keeps its line.
	if !strings.Contains(prompt, "- line-scans | ") {
		t.Fatalf("depth evicted live work from the board:\n%s", prompt)
	}
	// What the board no longer carries is settled work, and that is the design
	// rather than a loss: the board is what is moving, and a job that is over is
	// reached by a read aimed with the user's own words — which is also the only
	// place a finding can come from.
	rows, err := New(nil, graph).boardRows("finance", "podcast episode", "", "")
	if err != nil {
		t.Fatal(err)
	}
	aimed := renderBoard(rows)
	if !strings.Contains(aimed, "- podcast-edit | ") || !strings.Contains(aimed, podcastFinding) {
		t.Fatalf("settled work is unreachable by the words the user would use:\n%s", aimed)
	}
}

// splitNowLine removes the one volatile clock line from a prompt and returns it
// beside the rest, so a byte-for-byte assertion can be made about everything
// that is not the current time.
func splitNowLine(t *testing.T, prompt string) (string, string) {
	t.Helper()
	start := strings.Index(prompt, "\n\nnow: ")
	if start < 0 {
		t.Fatalf("the prompt carries no clock:\n%s", prompt)
	}
	end := strings.Index(prompt[start+2:], "\n")
	if end < 0 {
		t.Fatalf("the clock line never ends:\n%s", prompt)
	}
	end += start + 2
	return prompt[start+2 : end], prompt[:start] + prompt[end:]
}

// The regression guard. A message about nothing on the graph must produce the
// prompt it produces today, to the byte — no depth, no readings, no services.
//
// The blocks moved when the router and the belt became one turn: the manual's
// page list joined the stable half, the snapshot became the live board under its
// own heading, and depth, services and the deterministic readings each appear
// only when they have something to say. The ORDER is still position by
// volatility, and a greeting still pays nothing for machinery it did not use —
// which is what this test exists to hold.
func TestGreetingProducesTodaysContextExactly(t *testing.T) {
	graph := openHeadStore(t)
	seedResultBoard(t, graph)
	greeting := "good morning"
	prompt := routerPrompt(t, graph, "greeting", greeting)

	thread := "(no earlier messages in this session)"
	// The clock is lifted out before the comparison rather than reconstructed
	// into it: it is the one block whose bytes are a function of the wall clock,
	// and a test that rebuilt it would fail once a minute by arithmetic.
	clock, prompt := splitNowLine(t, prompt)
	if _, err := time.ParseInLocation(nowLineLayout, strings.TrimSuffix(strings.TrimPrefix(clock, "now: "), " local"), time.Local); err != nil {
		t.Fatalf("the clock line does not parse as its own layout: %q", clock)
	}
	board, err := New(nil, graph).renderTurnBoard("greeting", thread, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := "Recent thread before this message:\n" + thread +
		"\n\nManual pages available: " + strings.Join(manual.Pages(), ", ") +
		"\n\nLive board (the work you can read and act on):\n" + board +
		"\n\nNotebook (durable memory across jobs and conversations):\n" + renderNotebook(graph, greeting, thread, notebookContextBytes) +
		"\n\nCurrent user message (verbatim):\n" + greeting
	if prompt != want {
		t.Fatalf("greeting context drifted from today's:\ngot:\n%s\n\nwant:\n%s", prompt, want)
	}
	if deep, _ := New(nil, graph).renderDeep(greeting, ""); deep != "" {
		t.Fatalf("a greeting bought depth: %q", deep)
	}
	// The readings block is absent byte for byte from a message no recognizer
	// fires on, which is the other half of the same promise.
	if reading := New(nil, graph).renderHints(postUser(t, graph, "greeting", greeting), nil); reading != "" {
		t.Fatalf("a greeting bought a reading: %q", reading)
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
	// Live work with fat briefs, so the board is genuinely up against its own
	// ceiling while the depth block is being bought beside it.
	for index := 0; index < 40; index++ {
		spliceSurgeryJob(t, graph, fmt.Sprintf("routine-%02d", index), "",
			fmt.Sprintf("run errand %02d. %s", index, long))
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
	skeleton, err := New(nil, graph).renderTurnBoard("budget", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(skeleton) > maxGraphContextBytes {
		t.Fatalf("board skeleton = %d bytes, over its %d budget", len(skeleton), maxGraphContextBytes)
	}
	if !strings.Contains(skeleton, "- routine-") {
		t.Fatalf("the board dropped the live work it is the floor for:\n%s", skeleton)
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

	files := unnamedFiles(node, body, deepFileCap)
	if len(files) != 1 || files[0] != hidden {
		t.Fatalf("files line = %v, want only the path the result never shows (%s)", files, hidden)
	}
	if len(unnamedFiles(node, "", deepFileCap)) != deepFileCap {
		t.Errorf("with nothing visible the line holds %d paths, want the %d cap",
			len(unnamedFiles(node, "", deepFileCap)), deepFileCap)
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
//
// The board reaches that ceiling differently now. BoardRowCap bounds a board to
// a dozen rows before bytes ever bite, so a spread of narrow rows can no longer
// overflow it and the sweep is over row widths wide enough that a dozen of them
// do — which is the same property being tested, at the width where it is now
// reachable.
func TestTruncationMarkersFitTheirBudget(t *testing.T) {
	// Uniform 64-byte lines tile the 4KB budget exactly, so the last line that
	// fits leaves no slack at all and the marker has to have been reserved.
	const line = 64
	body := strings.Repeat("m", line-len("user: \n"))
	messages := make([]store.Message, 0, 2*maxThreadContextBytes/line)
	for index := 0; index < cap(messages); index++ {
		messages = append(messages, store.Message{Role: store.RoleUser, Body: body})
	}
	thread := New(nil, nil).renderThread(messages)
	if !strings.Contains(thread, strings.TrimSpace(threadTruncatedMark)) {
		t.Fatalf("the thread never truncated, so the marker is untested:\n%s", thread)
	}
	if len(thread) > maxThreadContextBytes {
		t.Errorf("thread = %d bytes with its marker, over the %d budget", len(thread), maxThreadContextBytes)
	}

	// The board's rows are the store's to shape, so the budget is checked across
	// a spread of row widths: whatever the last row that fits leaves behind, the
	// marker has to fit inside it.
	for width := 460; width <= 476; width++ {
		graph := openHeadStore(t)
		prefix := len("- board-00 |  | queued | 0 running, 1 queued | $0.00\n")
		if width <= prefix {
			t.Fatalf("row width %d leaves no room for a brief", width)
		}
		brief := strings.Repeat("b", width-prefix)
		for index := 0; index < BoardRowCap; index++ {
			spliceSurgeryJob(t, graph, fmt.Sprintf("board-%02d", index), "", brief)
		}
		board, err := New(nil, graph).renderTurnBoard("", "", nil)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(board, strings.TrimSpace(snapshotTruncatedMark)) {
			t.Fatalf("width %d: the board never truncated, so the marker is untested", width)
		}
		if len(board) > maxGraphContextBytes {
			t.Errorf("width %d: board = %d bytes with its marker, over the %d budget",
				width, len(board), maxGraphContextBytes)
		}
	}
}
