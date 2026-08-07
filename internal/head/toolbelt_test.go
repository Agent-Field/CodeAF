package head

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// beltTurn is one scripted model turn: the tools it asks for, and what it says
// when it stops asking.
type beltTurn struct {
	calls []ai.ToolCall
	text  string
}

// beltClient scripts the control loop and, separately, the ordinary router. It
// records which calls carried tool definitions, because "the deterministic
// layer handled this" and "the loop declined" are only distinguishable by
// whether a tooled call was ever made.
type beltClient struct {
	mutex  sync.Mutex
	turns  []beltTurn
	plain  []string
	calls  int
	tooled int
	// opening is the first tooled prompt: what the loop was actually given to
	// reason over, which is the only proof the belt opened with the board.
	opening string
}

func (client *beltClient) CompleteWithMessages(_ context.Context, messages []ai.Message,
	options ...ai.Option) (*ai.Response, error) {
	client.mutex.Lock()
	defer client.mutex.Unlock()
	client.calls++
	request := ai.Request{Messages: messages}
	for _, option := range options {
		_ = option(&request)
	}
	if len(request.Tools) > 0 {
		client.tooled++
		if client.opening == "" && len(messages) > 1 && len(messages[1].Content) > 0 {
			client.opening = messages[1].Content[0].Text
		}
		if len(client.turns) == 0 {
			return nil, errors.New("no scripted belt turn left")
		}
		turn := client.turns[0]
		client.turns = client.turns[1:]
		response := textResponse(turn.text)
		response.Choices[0].Message.ToolCalls = turn.calls
		return response, nil
	}
	if len(client.plain) == 0 {
		return nil, errors.New("no scripted router reply left")
	}
	reply := client.plain[0]
	client.plain = client.plain[1:]
	return textResponse(reply), nil
}

func (client *beltClient) counts() (calls int, tooled int) {
	client.mutex.Lock()
	defer client.mutex.Unlock()
	return client.calls, client.tooled
}

func (client *beltClient) openingPrompt() string {
	client.mutex.Lock()
	defer client.mutex.Unlock()
	return client.opening
}

func beltCall(id, name string, args map[string]any) ai.ToolCall {
	encoded, err := json.Marshal(args)
	if err != nil {
		panic(err)
	}
	return ai.ToolCall{ID: id, Type: "function",
		Function: ai.ToolCallFunction{Name: name, Arguments: string(encoded)}}
}

// seedExceptBoard is the shape the architectural failure takes: three jobs the
// user asked for, one of which they want spared, named by a sentence no cue
// list anticipated.
func seedExceptBoard(t *testing.T, graph *store.Store) {
	t.Helper()
	spliceSurgeryJob(t, graph, "finance", "Finance close", "close the finance books")
	spliceJobTree(t, graph, store.OriginUser, "",
		spec("research", "", "Market research", "research the market"),
		spec("research-a", "research", "Read the filings", "read the filings"),
		spec("research-b", "research", "Write it up", "write it up"))
	spliceJobTree(t, graph, store.OriginUser, "",
		spec("scans", "", "Line scans", "scan the lines"),
		spec("scans-a", "scans", "First pass", "first pass"),
		spec("scans-b", "scans", "Second pass", "second pass"))
}

// The whole point of the belt, end to end: a sentence no recognizer knows,
// composed into a read and one gated verb, asked about once, and settled.
func TestKillEverythingExceptTheFinanceOneAsksThenCancelsTheRest(t *testing.T) {
	graph := openHeadStore(t)
	seedExceptBoard(t, graph)
	client := &beltClient{turns: []beltTurn{
		{calls: []ai.ToolCall{beltCall("c1", beltToolBoard, map[string]any{})}},
		{calls: []ai.ToolCall{beltCall("c2", beltToolControl, map[string]any{
			"verb": "cancel", "ids": []string{"research", "scans"}})}},
		{text: "I've asked you to confirm before anything stops."},
	}}
	session := "except"
	user := postUser(t, graph, session, "kill everything except the finance one")
	head := New(client, graph)
	if err := head.answer(context.Background(), user); err != nil {
		t.Fatal(err)
	}

	reply := waitForAgentReply(t, graph, session, user.Seq)
	if !strings.Contains(reply.Body, `"kind":"confirm"`) {
		t.Fatalf("gated belt set skipped its confirm: %q", reply.Body)
	}
	if !strings.Contains(reply.Body, "Cancel 6 tasks") {
		t.Fatalf("confirm did not count the set: %q", reply.Body)
	}
	if commands, _ := graph.PendingCommands(20); len(commands) != 0 {
		t.Fatalf("belt acted before its confirm: %+v", commands)
	}

	answer := postUser(t, graph, session, "1")
	if err := head.answer(context.Background(), answer); err != nil {
		t.Fatal(err)
	}
	targets := pendingTargets(t, graph, store.CommandCancel)
	if !equalTargets(targets, []string{"research", "scans"}) {
		t.Fatalf("cancelled targets = %v, want the two non-finance jobs", targets)
	}
	for _, target := range targets {
		if target == "finance" {
			t.Fatal("the spared job was cancelled")
		}
	}
	settled := waitForAgentReply(t, graph, session, answer.Seq)
	if !strings.Contains(settled.Body, "Cancelling 6 tasks") ||
		!strings.Contains(settled.Body, "Market research") {
		t.Fatalf("belt receipt does not say what happened: %q", settled.Body)
	}
}

// Declining the belt's question costs nothing, exactly as declining the class
// path's question does.
func TestKeepingTheBeltSetCancelsNothing(t *testing.T) {
	graph := openHeadStore(t)
	seedExceptBoard(t, graph)
	client := &beltClient{turns: []beltTurn{
		{calls: []ai.ToolCall{beltCall("c1", beltToolControl, map[string]any{
			"verb": "cancel", "ids": []string{"research", "scans"}})}},
		{text: "asked"},
	}}
	session := "belt-keep"
	user := postUser(t, graph, session, "kill everything except the finance one")
	head := New(client, graph)
	if err := head.answer(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	waitForAgentReply(t, graph, session, user.Seq)
	answer := postUser(t, graph, session, "2")
	if err := head.answer(context.Background(), answer); err != nil {
		t.Fatal(err)
	}
	if commands, _ := graph.PendingCommands(20); len(commands) != 0 {
		t.Fatalf("declined belt set still journaled commands: %+v", commands)
	}
	settled := waitForAgentReply(t, graph, session, answer.Seq)
	if !strings.Contains(settled.Body, "Keeping them") {
		t.Fatalf("decline receipt = %q", settled.Body)
	}
}

// The load-bearing guarantee: everything the deterministic layer already reads
// keeps its free, instant path and never reaches a model at all.
func TestDeterministicMessagesNeverReachTheControlLoop(t *testing.T) {
	for _, message := range []string{
		"cancel the queued ones", "cancel the queued tasks", "restart the failed ones",
		"cancel the market research", "pause the running ones",
	} {
		t.Run(message, func(t *testing.T) {
			graph := openHeadStore(t)
			seedExceptBoard(t, graph)
			failNode(t, graph, "finance")
			client := &beltClient{}
			user := postUser(t, graph, "fast-path", message)
			if err := New(client, graph).answer(context.Background(), user); err != nil {
				t.Fatal(err)
			}
			if calls, tooled := client.counts(); calls != 0 || tooled != 0 {
				t.Fatalf("deterministic message cost %d model calls (%d tooled)", calls, tooled)
			}
		})
	}
}

// A quiet graph can never turn a sentence into graph control, so the loop is
// not even offered its tools.
func TestNoLiveWorkNeverInvokesTheControlLoop(t *testing.T) {
	graph := openHeadStore(t)
	client := &beltClient{plain: []string{`{"reply":"Nothing is running.","command":null}`}}
	user := postUser(t, graph, "quiet", "kill everything except the finance one")
	if err := New(client, graph).answer(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	calls, tooled := client.counts()
	if tooled != 0 {
		t.Fatalf("empty board still opened the tool belt: %d tooled calls", tooled)
	}
	if calls != 1 {
		t.Fatalf("router should have answered exactly once, got %d calls", calls)
	}
}

// The loop is never a dead end. A turn that touches nothing has no grounding in
// the board, so whatever it says is handed back to the router.
func TestControlLoopWithoutToolsFallsThroughToTheRouter(t *testing.T) {
	graph := openHeadStore(t)
	seedExceptBoard(t, graph)
	client := &beltClient{
		turns: []beltTurn{{text: "sure, whatever you say"}},
		plain: []string{`{"reply":"Routed instead.","command":null}`},
	}
	session := "fallthrough"
	user := postUser(t, graph, session, "kill everything except the finance one")
	if err := New(client, graph).answer(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	calls, tooled := client.counts()
	if tooled != 1 || calls != 2 {
		t.Fatalf("expected one tooled loop call then one router call, got %d/%d", tooled, calls)
	}
	reply := waitForAgentReply(t, graph, session, user.Seq)
	if reply.Body != "Routed instead." {
		t.Fatalf("router did not answer: %q", reply.Body)
	}
	if commands, _ := graph.PendingCommands(10); len(commands) != 0 {
		t.Fatalf("a groundless loop still journaled commands: %+v", commands)
	}
}

// The sentinel is how the loop says "this was not about the board" without
// costing the user an answer.
func TestControlLoopSentinelFallsThroughToTheRouter(t *testing.T) {
	graph := openHeadStore(t)
	seedExceptBoard(t, graph)
	client := &beltClient{
		turns: []beltTurn{
			{calls: []ai.ToolCall{beltCall("c1", beltToolBoard, map[string]any{})}},
			{text: controlNotWorkSentinel},
		},
		plain: []string{`{"reply":"Starting that.","command":null}`},
	}
	session := "sentinel"
	user := postUser(t, graph, session, "finish reading me that Auden poem")
	if err := New(client, graph).answer(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	reply := waitForAgentReply(t, graph, session, user.Seq)
	if reply.Body != "Starting that." {
		t.Fatalf("sentinel did not fall through: %q", reply.Body)
	}
}

// Tool errors are the belt's whole safety story on the model's side: a
// misreading comes back as a sentence it can act on, never as an action.
func TestControlRefusesIllegalIdsAndVerbStatusMismatches(t *testing.T) {
	graph := openHeadStore(t)
	seedExceptBoard(t, graph)
	spliceJobTree(t, graph, store.OriginSelf, store.PracticeGroup,
		spec("practice", "", "Practice question", "practice a question"))
	user := postUser(t, graph, "refuse", "kill everything except the finance one")
	run := &beltRun{head: New(&beltClient{}, graph), user: user}

	tests := []struct {
		name string
		args map[string]any
		want string
	}{
		{"unknown verb", map[string]any{"verb": "detonate", "ids": []string{"finance"}},
			"verb must be one of"},
		{"no ids", map[string]any{"verb": "cancel"}, "at least one id"},
		{"invented id", map[string]any{"verb": "cancel", "ids": []string{"ghost"}},
			`no live work with id "ghost"`},
		{"verb-status mismatch", map[string]any{"verb": "restart", "ids": []string{"finance"}},
			"only applies to failed or cancelled work"},
		{"resume what is not paused", map[string]any{"verb": "resume", "ids": []string{"finance"}},
			"only applies to paused work"},
		{"not the user's work", map[string]any{"verb": "cancel", "ids": []string{"practice"}},
			"not the user's work"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, failed := run.execute(beltToolControl, mustJSON(test.args))
			if !failed {
				t.Fatalf("illegal control succeeded: %q", result)
			}
			if !strings.Contains(result, test.want) {
				t.Fatalf("tool error = %q, want it to mention %q", result, test.want)
			}
		})
	}
	if commands, _ := graph.PendingCommands(10); len(commands) != 0 {
		t.Fatalf("a refused control still journaled: %+v", commands)
	}
	if run.acted {
		t.Fatal("a refused control counted as an action")
	}
}

// The unit rule, arrived at from ids instead of from a status word: naming a
// whole job under a verb that cannot touch its running leaf reaches the queued
// siblings individually and leaves the running one alone.
func TestControlNeverReachesThroughWorkTheVerbCannotTouch(t *testing.T) {
	graph := openHeadStore(t)
	spliceJobTree(t, graph, store.OriginUser, "",
		spec("scan", "", "Line scan", "scan the lines"),
		spec("scan-a", "scan", "First pass", "first pass"),
		spec("scan-b", "scan", "Second pass", "second pass"))
	startNode(t, graph, "scan-a")
	head := New(&beltClient{}, graph)

	set, err := head.beltSet([]string{"scan"}, store.CommandReprioritize, true)
	if err != nil {
		t.Fatal(err)
	}
	if !equalTargets(unitIDs(set), []string{"scan-b"}) {
		t.Fatalf("units = %v, want only the queued sibling", unitIDs(set))
	}

	// Cancel may touch running work, so the same ids collapse to the one root.
	set, err = head.beltSet([]string{"scan"}, store.CommandCancel, true)
	if err != nil {
		t.Fatal(err)
	}
	if !equalTargets(unitIDs(set), []string{"scan"}) || set.Affected != 3 {
		t.Fatalf("cancel units = %v affected=%d, want the whole job as one unit",
			unitIDs(set), set.Affected)
	}
}

// The three thin wrappers must journal exactly the command kinds that already
// exist, and survive a replay of the journal unchanged.
func TestSteerReviseExpediteJournalExistingCommandKinds(t *testing.T) {
	path := t.TempDir() + "/graph.db"
	graph, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = graph.Close() }()
	spliceJobTree(t, graph, store.OriginUser, "",
		spec("api", "", "API client", "write the API client"),
		spec("api-a", "api", "Draft it", "draft it"))
	startNode(t, graph, "api-a")

	user := postUser(t, graph, "belt-verbs", "hold the scans until the research lands")
	run := &beltRun{head: New(&beltClient{}, graph), user: user}

	result, failed := run.execute(beltToolSteer, mustJSON(map[string]any{
		"job": "api", "message": "prefer the v2 endpoints"}))
	if failed || !strings.Contains(result, "told 1 running worker") {
		t.Fatalf("steer = %q failed=%t", result, failed)
	}
	anchored := 0
	messages, err := graph.Messages("", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, message := range messages {
		if message.NodeID == "api-a" && strings.Contains(message.Body, "prefer the v2 endpoints") {
			anchored++
		}
	}
	if anchored != 1 {
		t.Fatalf("steer posted %d node-anchored messages, want 1", anchored)
	}

	if result, failed = run.execute(beltToolRevise, mustJSON(map[string]any{
		"job": "api", "words": "actually make it v2 only"})); failed {
		t.Fatalf("revise = %q", result)
	}
	if result, failed = run.execute(beltToolExpedite, mustJSON(map[string]any{"job": "api"})); failed {
		t.Fatalf("expedite = %q", result)
	}
	if !run.acted || run.commandSeq == 0 {
		t.Fatalf("belt did not record what it journaled: acted=%t seq=%d", run.acted, run.commandSeq)
	}

	before := beltCommandKinds(t, graph)
	want := []string{string(store.CommandRedirect), string(store.CommandExpedite)}
	if !equalTargets(before, want) {
		t.Fatalf("journaled kinds = %v, want %v", before, want)
	}
	if err := graph.Rebuild(); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if after := beltCommandKinds(t, graph); !equalTargets(after, before) {
		t.Fatalf("replay changed the belt's commands: before=%v after=%v", before, after)
	}
}

// The board is a read, so it never journals; it is also a prompt line resent
// every turn, so it stays inside its row cap.
func TestBoardReturnsCompactRowsAndRespectsItsCap(t *testing.T) {
	graph := openHeadStore(t)
	for index := 0; index < BoardRowCap+3; index++ {
		id := fmt.Sprintf("job-%02d", index)
		spliceSurgeryJob(t, graph, id, "Report "+id, "write report "+id)
	}
	head := New(&beltClient{}, graph)
	rows, err := head.boardRows("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != BoardRowCap {
		t.Fatalf("board returned %d rows, want the cap of %d", len(rows), BoardRowCap)
	}
	rendered := renderBoard(rows)
	if lines := strings.Count(rendered, "\n") + 1; lines != BoardRowCap {
		t.Fatalf("rendered %d lines for %d rows", lines, len(rows))
	}
	first := strings.SplitN(rendered, "\n", 2)[0]
	for _, want := range []string{rows[0].node.ID, "queued", "0 running", "1 queued", "$0.00"} {
		if !strings.Contains(first, want) {
			t.Fatalf("board row %q is missing %q", first, want)
		}
	}
	if commands, _ := graph.PendingCommands(10); len(commands) != 0 {
		t.Fatalf("a read journaled something: %+v", commands)
	}
}

// The board is the model's only reality, so what is not the user's work is not
// on it and can never be named.
func TestBoardShowsOnlyTheUsersOwnLiveWork(t *testing.T) {
	graph := openHeadStore(t)
	seedExceptBoard(t, graph)
	spliceJobTree(t, graph, store.OriginSelf, store.PracticeGroup,
		spec("practice", "", "Practice question", "practice a question"))
	completeNode(t, graph, "finance")
	head := New(&beltClient{}, graph)
	rows, err := head.boardRows("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.node.ID)
	}
	if containsTarget(ids, "practice") {
		t.Fatalf("board exposed the resident's own work: %v", ids)
	}
	if containsTarget(ids, "finance") {
		t.Fatalf("board showed settled work as live: %v", ids)
	}
	running, err := head.boardRows("", "running", "")
	if err != nil || len(running) != 0 {
		t.Fatalf("running board = %+v err=%v, want nothing running", running, err)
	}
	if _, err := head.boardRows("", "sideways", ""); err == nil {
		t.Fatal("board accepted a status word it does not know")
	}
}

// The trigger is meant to be broad and cheap on both sides: it fires on any
// control-ish word or any anchor to live work, and stays quiet otherwise.
func TestControlTriggerPredicate(t *testing.T) {
	tests := []struct {
		message string
		jobs    bool
		want    bool
	}{
		{"kill everything except the finance one", true, true},
		{"hold the scans until the research lands", true, true},
		{"redo the last one but cheaper", true, true},
		{"leave the market research alone", true, true},
		{"how is the market research going", true, true},
		{"what's the weather in Oslo", true, false},
		{"thanks, that helps", true, false},
		{"kill everything except the finance one", false, false},
	}
	for _, test := range tests {
		t.Run(test.message+fmt.Sprint(test.jobs), func(t *testing.T) {
			graph := openHeadStore(t)
			if test.jobs {
				seedExceptBoard(t, graph)
			}
			applies, err := New(&beltClient{}, graph).controlLoopApplies(
				store.Message{SessionID: "trigger", Role: store.RoleUser, Body: test.message})
			if err != nil {
				t.Fatal(err)
			}
			if applies != test.want {
				t.Fatalf("controlLoopApplies(%q) = %t, want %t", test.message, applies, test.want)
			}
		})
	}
}

// A model that keeps reading instead of finishing spends a bounded number of
// tools and is then told to speak.
func TestControlLoopSpendsAtMostItsToolCallCap(t *testing.T) {
	graph := openHeadStore(t)
	seedExceptBoard(t, graph)
	turns := make([]beltTurn, 0, controlToolCallCap+2)
	for index := 0; index <= controlToolCallCap; index++ {
		turns = append(turns, beltTurn{calls: []ai.ToolCall{
			beltCall(fmt.Sprintf("c%d", index), beltToolBoard, map[string]any{})}})
	}
	client := &beltClient{
		turns: append(turns, beltTurn{text: "Three jobs are waiting to start."}),
		plain: []string{`{"reply":"Routed instead.","command":null}`},
	}
	session := "cap"
	user := postUser(t, graph, session, "which of these should i drop")
	if err := New(client, graph).answer(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	if _, tooled := client.counts(); tooled > controlToolCallCap+2 {
		t.Fatalf("loop ran %d tooled turns, cap is %d tool calls", tooled, controlToolCallCap)
	}
	reply := waitForAgentReply(t, graph, session, user.Seq)
	if reply.Body != "Three jobs are waiting to start." {
		t.Fatalf("capped loop reply = %q", reply.Body)
	}
	if commands, _ := graph.PendingCommands(10); len(commands) != 0 {
		t.Fatalf("a read-only loop journaled something: %+v", commands)
	}
}

func beltCommandKinds(t *testing.T, graph *store.Store) []string {
	t.Helper()
	commands, err := graph.PendingCommands(50)
	if err != nil {
		t.Fatalf("pending commands: %v", err)
	}
	kinds := make([]string, 0, len(commands))
	for _, command := range commands {
		kinds = append(kinds, string(command.Kind))
	}
	return kinds
}

func mustJSON(value map[string]any) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return string(encoded)
}
