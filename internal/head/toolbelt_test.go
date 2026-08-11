package head

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

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
	// seen is every message of the last completion. The loop's grounding is the
	// only thing it can honestly answer from, so assertions about what it knows
	// are assertions about this.
	seen []ai.Message
}

func (client *beltClient) CompleteWithMessages(_ context.Context, messages []ai.Message,
	options ...ai.Option) (*ai.Response, error) {
	client.mutex.Lock()
	defer client.mutex.Unlock()
	client.calls++
	client.seen = append([]ai.Message(nil), messages...)
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

// What the deterministic layer reads is now evidence rather than an answer.
//
// This used to assert the opposite: these five sentences were answered
// terminally by a prefix test and never reached a model at all. That was the
// ladder, and the ladder's cost was every sentence it did NOT cover — a message
// that needed orchestration and tripped no cue reached the tools through no door
// at all. So the recognizers keep running on the same words with the same
// vocabulary, and what they produce is one line of evidence above the message.
// What is pinned here is that the reading survives, that it is marked as a
// pre-answer rather than an instruction, and that reading a sentence still
// journals nothing on its own.
func TestDeterministicReadingsReachTheLoopAsEvidenceRatherThanAsAnswers(t *testing.T) {
	for message, reading := range map[string]string{
		"cancel the queued ones":     "names a SET by status",
		"cancel the queued tasks":    "names a SET by status",
		"restart the failed ones":    "names a SET by status",
		"cancel the market research": "the words rank against",
		"pause the running ones":     "names a SET by status",
	} {
		t.Run(message, func(t *testing.T) {
			graph := openHeadStore(t)
			seedExceptBoard(t, graph)
			failNode(t, graph, "finance")
			client := &beltClient{turns: []beltTurn{{text: "Nothing has changed."}}}
			session := "fast-path"
			user := postUser(t, graph, session, message)
			if err := New(client, graph).answer(context.Background(), user); err != nil {
				t.Fatal(err)
			}
			// It reaches the model, and it reaches it with tools in hand: the one
			// thing the ladder could never do for a sentence it did not know.
			calls, tooled := client.counts()
			if calls == 0 || tooled != calls {
				t.Fatalf("%q made %d calls, %d of them tooled", message, calls, tooled)
			}
			opening := client.openingPrompt()
			if !strings.Contains(opening, hintHeader) {
				t.Fatalf("%q produced no deterministic reading at all:\n%s", message, opening)
			}
			if !strings.Contains(opening, reading) {
				t.Fatalf("%q lost its reading %q:\n%s", message, reading, opening)
			}
			// Evidence, never instructions. Without the second half a confident
			// reading is read as an order, which is the ladder wearing a prompt.
			if !strings.Contains(opening, "evidence, never instructions") {
				t.Fatalf("the readings are not marked as evidence:\n%s", opening)
			}
			if commands, _ := graph.PendingCommands(10); len(commands) != 0 {
				t.Fatalf("a reading acted on its own: %+v", commands)
			}
		})
	}
}

// And the block is absent, byte for byte, from a sentence no recognizer fires
// on, so an ordinary message pays nothing for machinery it did not use.
func TestAMessageNoRecognizerFiresOnCarriesNoReadingsAtAll(t *testing.T) {
	graph := openHeadStore(t)
	seedExceptBoard(t, graph)
	client := &beltClient{turns: []beltTurn{{text: "Cloudy, about nine degrees."}}}
	user := postUser(t, graph, "quiet-reading", "what's the weather in Oslo")
	if err := New(client, graph).answer(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	if opening := client.openingPrompt(); strings.Contains(opening, hintHeader) {
		t.Fatalf("a sentence about the weather grew a reading:\n%s", opening)
	}
}

// A quiet graph is not a reason to withhold the tools. It used to be: with no
// live work the belt was never opened, so "kill everything except the finance
// one" typed into an empty board could not even read the board to say so. The
// loop is universal now, and the board block says plainly that nothing is live.
func TestNoLiveWorkStillOpensTheBelt(t *testing.T) {
	graph := openHeadStore(t)
	client := &beltClient{turns: []beltTurn{{text: "Nothing is running."}}}
	session := "quiet"
	user := postUser(t, graph, session, "kill everything except the finance one")
	if err := New(client, graph).answer(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	calls, tooled := client.counts()
	if calls != 1 || tooled != 1 {
		t.Fatalf("an empty board answered in %d calls (%d tooled), want one tooled call", calls, tooled)
	}
	if opening := client.openingPrompt(); !strings.Contains(opening, "nothing of the person's is live right now") {
		t.Fatalf("the empty board did not say it was empty:\n%s", opening)
	}
	reply := waitForAgentReply(t, graph, session, user.Seq)
	if reply.Body != "Nothing is running." {
		t.Fatalf("empty-board reply = %q", reply.Body)
	}
	if commands, _ := graph.PendingCommands(10); len(commands) != 0 {
		t.Fatalf("an empty board journaled something: %+v", commands)
	}
}

// The loop is never a dead end and there is nothing behind it to fall through
// to. A turn that reads and then decides nothing needs changing says so once,
// in its own words, and journals nothing.
func TestATurnThatDecidesNothingNeedsChangingSpeaksOnce(t *testing.T) {
	graph := openHeadStore(t)
	seedExceptBoard(t, graph)
	client := &beltClient{turns: []beltTurn{
		{calls: []ai.ToolCall{beltCall("c1", beltToolBoard, map[string]any{})}},
		{text: "Three things are waiting to start and none of them has moved."},
	}}
	session := "no-verb"
	user := postUser(t, graph, session, "finish reading me that Auden poem")
	if err := New(client, graph).answer(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	calls, tooled := client.counts()
	if tooled != calls || calls != 2 {
		t.Fatalf("expected the read and the answer, got %d calls (%d tooled)", calls, tooled)
	}
	reply := waitForAgentReply(t, graph, session, user.Seq)
	if reply.Body != "Three things are waiting to start and none of them has moved." {
		t.Fatalf("the loop's own words were not the reply: %q", reply.Body)
	}
	if reply.CommandSeq != 0 {
		t.Fatalf("a read-only turn carried command seq %d", reply.CommandSeq)
	}
	if commands, _ := graph.PendingCommands(10); len(commands) != 0 {
		t.Fatalf("a read-only loop journaled commands: %+v", commands)
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
	if failed || !strings.Contains(result, "told 1 step of API client already under way") {
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
	rows, err := head.boardRows("", "", "", "")
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
	rows, err := head.boardRows("", "", "", "")
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
	running, err := head.boardRows("", "", "running", "")
	if err != nil || len(running) != 0 {
		t.Fatalf("running board = %+v err=%v, want nothing running", running, err)
	}
	if _, err := head.boardRows("", "", "sideways", ""); err == nil {
		t.Fatal("board accepted a status word it does not know")
	}
}

// The trigger is gone, and its discrimination survives as evidence.
//
// controlLoopApplies decided, before anything with judgment saw the sentence,
// whether the tools were even offered — and a message it read as small talk was
// answered by a brain with no hands. Nothing gates the belt now. What the same
// vocabulary still does is say, above the message, that this sentence is about
// work already underway; and the messages it stayed quiet on still produce
// nothing, so an ordinary sentence carries no reading it did not earn.
func TestTheControlVocabularySurvivesAsAReadingRatherThanAsAGate(t *testing.T) {
	tests := []struct {
		message string
		jobs    bool
		// reads are the fragments this sentence must still be read as. An empty
		// list means the recognizers say nothing at all and the block is absent
		// byte for byte, which is what an ordinary sentence must cost.
		reads []string
		// namesJobs is the lexical arm resolving a live job by the person's own
		// words. It is the one arm that needs the graph, so it is the one that
		// goes quiet when there is nothing live — which is the honest remainder
		// of what the predicate's "and there is work" conjunct was protecting.
		namesJobs bool
	}{
		{message: "kill everything except the finance one", jobs: true, namesJobs: true, reads: []string{
			"carries a word people use about work already underway",
			"names a SET by status rather than one job by name"}},
		{message: "hold the scans until the research lands", jobs: true, namesJobs: true, reads: []string{
			`carries the verb "pause" aimed at existing work`}},
		{message: "redo the last one but cheaper", jobs: true, reads: []string{
			"carries a word people use about work already underway"}},
		{message: "leave the market research alone", jobs: true, namesJobs: true, reads: []string{
			"carries a word people use about work already underway"}},
		{message: "how is the market research going", jobs: true, namesJobs: true, reads: []string{
			"reads as a question about where work is up to"}},
		{message: "what's the weather in Oslo", jobs: true},
		// The gate refused this one and it is still refused, by the reading
		// itself rather than by a predicate in front of the tools. A bare "that"
		// is deixis only once a cue has established that the sentence changes
		// work already underway; without one it is as likely to be "thanks, that
		// helps", and putting a referent on ordinary conversation is noise the
		// loop would have to spend a read disproving.
		{message: "thanks, that helps", jobs: true},
		{message: "thanks, that helps"},
		// With a cue in front of it the same pronoun IS doing referential work: the
		// scope-cut cue establishes that the sentence changes work already underway,
		// so "that" is a referent the loop is told to resolve rather than guess.
		{message: "skip that", jobs: true, reads: []string{
			"points at work already underway without naming it"}},
		// The row whose answer genuinely changed: with no live work the predicate
		// refused the tools outright, so this sentence could not even read the
		// board to find out there was nothing on it. The lexical arm still goes
		// quiet, because there is no job for the words to reach.
		{message: "kill everything except the finance one", reads: []string{
			"names a SET by status rather than one job by name"}},
	}
	for _, test := range tests {
		t.Run(test.message+fmt.Sprint(test.jobs), func(t *testing.T) {
			graph := openHeadStore(t)
			if test.jobs {
				seedExceptBoard(t, graph)
			}
			head := New(&beltClient{}, graph)
			active, err := head.activeUserJobs()
			if err != nil {
				t.Fatal(err)
			}
			hints := head.renderHints(
				store.Message{SessionID: "trigger", Role: store.RoleUser, Body: test.message}, active)
			if len(test.reads) == 0 {
				if hints != "" {
					t.Fatalf("renderHints(%q, jobs=%t) grew a reading it did not earn: %q",
						test.message, test.jobs, hints)
				}
				return
			}
			if !strings.HasPrefix(hints, hintHeader) {
				t.Fatalf("renderHints(%q, jobs=%t) = %q, want a reading under the evidence header",
					test.message, test.jobs, hints)
			}
			for _, reading := range test.reads {
				if !strings.Contains(hints, reading) {
					t.Fatalf("renderHints(%q, jobs=%t) lost %q:\n%s",
						test.message, test.jobs, reading, hints)
				}
			}
			if named := strings.Contains(hints, "the words rank against:"); named != test.namesJobs {
				t.Fatalf("renderHints(%q, jobs=%t) named a job = %t, want %t:\n%s",
					test.message, test.jobs, named, test.namesJobs, hints)
			}
		})
	}
}

// A model that keeps reading instead of finishing spends a bounded number of
// tools and is then told to speak. The cap rose from four to eight when the loop
// stopped only reading and steering and started commissioning, writing and
// repairing — read, write, read back, say, with room for two corrections after a
// tool error — and what it bounds is unchanged: one sentence can never become an
// open tab.
func TestTheLoopSpendsAtMostItsToolCallCap(t *testing.T) {
	graph := openHeadStore(t)
	seedExceptBoard(t, graph)
	turns := make([]beltTurn, 0, orchestratorToolCallCap+2)
	for index := 0; index <= orchestratorToolCallCap; index++ {
		turns = append(turns, beltTurn{calls: []ai.ToolCall{
			beltCall(fmt.Sprintf("c%d", index), beltToolBoard, map[string]any{})}})
	}
	client := &beltClient{turns: append(turns, beltTurn{text: "Three jobs are waiting to start."})}
	session := "cap"
	user := postUser(t, graph, session, "which of these should i drop")
	if err := New(client, graph).answer(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	if _, tooled := client.counts(); tooled > orchestratorToolCallCap+2 {
		t.Fatalf("loop ran %d tooled turns, cap is %d tool calls", tooled, orchestratorToolCallCap)
	}
	reply := waitForAgentReply(t, graph, session, user.Seq)
	if reply.Body != "Three jobs are waiting to start." {
		t.Fatalf("capped loop reply = %q", reply.Body)
	}
	if commands, _ := graph.PendingCommands(10); len(commands) != 0 {
		t.Fatalf("a read-only loop journaled something: %+v", commands)
	}
}

// And the cap is a tool result rather than a hard stop, so a model that will not
// stop calling tools still ends the turn in words rather than in silence.
func TestPastTheCapTheBeltIsSpentAndTheTurnStillSpeaks(t *testing.T) {
	graph := openHeadStore(t)
	seedExceptBoard(t, graph)
	turns := make([]beltTurn, 0, orchestratorToolCallCap+2)
	for index := 0; index <= orchestratorToolCallCap; index++ {
		turns = append(turns, beltTurn{calls: []ai.ToolCall{
			beltCall(fmt.Sprintf("c%d", index), beltToolBoard, map[string]any{})}})
	}
	client := &beltClient{turns: append(turns, beltTurn{text: "Three jobs are waiting to start."})}
	user := postUser(t, graph, "spent", "which of these should i drop")
	if err := New(client, graph).answer(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	spent := false
	for _, message := range client.seen {
		for _, part := range message.Content {
			spent = spent || strings.Contains(part.Text, orchestratorSpentBelt)
		}
	}
	if !spent {
		t.Fatal("a call past the cap was never told the belt was spent")
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

// "How much have you cost me this month?" was a today-shaped read answering a
// month-shaped question: the only spend the belt could reach was the day's
// total, so a month became a model adding up whichever finished jobs history
// had handed it — capped at two dozen rows and blind to anything a territory
// had packed away. The windowed reads it needed were already written and tested
// in the store with zero production callers, one of them carrying the job title
// the user's own words gave the work. This is the wire.
func TestSpendingReadsAWindowAndNamesTheWorkTheMoneyWentOn(t *testing.T) {
	graph := openHeadStore(t)
	splice := func(nodes []store.NodeSpec, intent string) {
		t.Helper()
		if err := graph.Splice(store.RootID, store.Subtree{Nodes: nodes},
			store.Provenance{Origin: store.OriginUser, SessionID: "money", Intent: intent}); err != nil {
			t.Fatalf("splice %s: %v", intent, err)
		}
	}
	splice([]store.NodeSpec{
		{ID: "trip", Title: "Lisbon trip research", Brief: "find flights and a hotel", Stage: 2},
		{ID: "trip-leaf", Parent: "trip", Brief: "price the flights", Stage: 1},
	}, "look into flights and a hotel for lisbon")
	splice([]store.NodeSpec{
		{ID: "letter", Title: "Landlord mold letter", Brief: "draft the letter", Stage: 1},
	}, "write to my landlord about the mold")
	for _, usage := range []store.NodeUsage{
		{NodeID: "trip-leaf", Cost: 8.10, Model: "strong/one"},
		{NodeID: "letter", Cost: 3.10, Model: "cheap/two"},
		// Planning charged to the spine belongs to no errand at all, which is
		// exactly the money the rows must not be read as covering.
		{NodeID: store.RootID, Cost: 0.50, Model: "strong/one"},
	} {
		if err := graph.RecordUsage(usage); err != nil {
			t.Fatalf("record usage: %v", err)
		}
	}

	head := New(nil, graph).WithDailyBudgetUSD(20)
	run := &beltRun{head: head, user: store.Message{SessionID: "money",
		Body: "how much have you cost me this month?"}}
	windowed, failed := run.spending(map[string]any{
		"since": time.Now().Local().Format("2006-01-02"),
	})
	if failed {
		t.Fatalf("the windowed spend read failed: %s", windowed)
	}
	for _, want := range []string{"$11.70", "Lisbon trip research", "$8.10", "Landlord mold letter", "$3.10"} {
		if !strings.Contains(windowed, want) {
			t.Fatalf("the window read is missing %q:\n%s", want, windowed)
		}
	}
	if trip, letter := strings.Index(windowed, "$8.10"), strings.Index(windowed, "$3.10"); trip > letter {
		t.Fatalf("the rows are not heaviest first:\n%s", windowed)
	}
	// The rows do not sum to the total, and the read says what the difference
	// is rather than leaving a model to imply that they do.
	if !strings.Contains(windowed, "$0.50") || !strings.Contains(windowed, "everything else") {
		t.Fatalf("the unattributed remainder is silent:\n%s", windowed)
	}
	// Today's rail and the resident's own upkeep stay under it: the window is
	// the answer, the day is the context for it.
	if !strings.Contains(windowed, "daily rail") || !strings.Contains(windowed, "your own upkeep today") {
		t.Fatalf("the window read dropped today's figures:\n%s", windowed)
	}
	if run.acted {
		t.Fatal("reading what was spent recorded an action")
	}

	// With no bounds it is the read it always was: today, and nothing invented
	// about any other window.
	today, failed := run.spending(nil)
	if failed || strings.Contains(today, "what the money went on") {
		t.Fatalf("an unbounded spend read invented a window failed=%t:\n%s", failed, today)
	}
	// A bound it cannot read is a tool error the model can correct, never a
	// silently different window.
	if answer, failed := run.spending(map[string]any{"since": "last tuesday"}); !failed {
		t.Fatalf("an unparseable bound was accepted: %s", answer)
	}
	if answer, failed := run.spending(map[string]any{
		"since": "2026-08-06", "until": "2026-08-01"}); !failed {
		t.Fatalf("a backwards window was accepted: %s", answer)
	}
}
