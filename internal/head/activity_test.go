package head

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// What a turn says about itself while it works.
//
// The property under test is not "an event was emitted" — it is that the person
// on the other end of it is told something they can read. So every assertion
// here is about the WORDS: that they are a sentence, that they are the turn's
// own room, and above all that they are never the arguments the model passed.

// activityWatch collects the stream events one context sees, safely.
type activityWatch struct {
	mutex  sync.Mutex
	events []provider.StreamEvent
}

func (w *activityWatch) observe(event provider.StreamEvent) {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	w.events = append(w.events, event)
}

func (w *activityWatch) of(kinds ...provider.StreamEventKind) []provider.StreamEvent {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	var out []provider.StreamEvent
	for _, event := range w.events {
		for _, kind := range kinds {
			if event.Kind == kind {
				out = append(out, event)
				break
			}
		}
	}
	return out
}

// A TOOL ROUND NARRATES ITSELF, keyed to the room the turn is answering for.
//
// It goes through Serve rather than through a hand-built context on purpose:
// the session stamp is applied by answerTurn, so a test that made its own
// context would be asserting a key nothing in production sets.
func TestAToolRoundNarratesItselfOnTheTurnsOwnStream(t *testing.T) {
	graph := openHeadStore(t)
	seedExceptBoard(t, graph)
	client := &beltClient{turns: []beltTurn{
		{calls: []ai.ToolCall{beltCall("c1", beltToolBoard, map[string]any{})}},
		{text: "Three things are running."},
	}}

	watch := &activityWatch{}
	ctx, cancel := context.WithCancel(context.Background())
	ctx = provider.WithStreamObserver(ctx, watch.observe)
	done := make(chan error, 1)
	go func() { done <- New(client, graph).Serve(ctx) }()

	const session = "narrating"
	user, err := graph.PostMessage(store.Message{
		SessionID: session, Role: store.RoleUser, Body: "what's running?"})
	if err != nil {
		t.Fatal(err)
	}
	waitForAgentReply(t, graph, session, user.Seq)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("serve returned %v, want cancellation", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("serve did not stop")
	}

	begins := watch.of(provider.StreamToolBegin)
	if len(begins) != 1 {
		t.Fatalf("a one-call round emitted %d begins, want one: %+v", len(begins), begins)
	}
	if begins[0].Session != session {
		t.Fatalf("the activity is keyed to %q, not the room being answered (%q)",
			begins[0].Session, session)
	}
	if begins[0].Delta != "looking at the work" {
		t.Fatalf("the begin says %q, not the board's gloss", begins[0].Delta)
	}
	ends := watch.of(provider.StreamToolEnd, provider.StreamToolFailed)
	if len(ends) != 1 {
		t.Fatalf("a one-call round emitted %d endings, want one: %+v", len(ends), ends)
	}
	if ends[0].Kind != provider.StreamToolEnd {
		t.Fatalf("a board read that worked ended as a failure: %+v", ends[0])
	}
	if ends[0].Session != session {
		t.Fatalf("the ending is keyed to %q, not %q", ends[0].Session, session)
	}
}

// THE GLOSS IS NEVER THE ARGUMENTS. 5.14's never-shown tier names JSON
// explicitly, and a person waiting on an answer did not ask to be shown an
// object. This is the law stated against the whole vocabulary at once rather
// than tool by tool, so a gloss added later cannot quietly leak one.
func TestAGlossIsAlwaysASentenceAndNeverTheArguments(t *testing.T) {
	calls := []struct{ name, args string }{
		{beltToolBoard, `{"status":"running"}`},
		{beltToolRecall, `{"q":"navctx rewrite","kind":"message"}`},
		{beltToolTask, `{"instruction":"rewrite the nav context so it stops leaking"}`},
		{beltToolOpen, `{"id":"task-9196"}`},
		{beltToolRead, `{"job":"task-9196","file":"regions.md"}`},
		{beltToolPlan, `{"job":"task-9196"}`},
		{beltToolWrite, `{"name":"architecture.svg","body":"<svg/>"}`},
		{beltToolChange, `{"target":"task-9196","words":"make it two columns"}`},
		{beltToolStop, `{"targets":["task-9196"]}`},
		{beltToolStatus, `{}`},
		{beltToolSay, `{"text":"one moment"}`},
		{"a_tool_nobody_named", `{"weird":true}`},
	}
	for _, call := range calls {
		gloss := toolGloss(call.name, call.args)
		if strings.TrimSpace(gloss) == "" {
			t.Errorf("%s glossed as nothing at all", call.name)
			continue
		}
		for _, banned := range []string{"{", "}", "\":", "[\""} {
			if strings.Contains(gloss, banned) {
				t.Errorf("%s glossed as machinery: %q", call.name, gloss)
			}
		}
		if strings.Contains(gloss, "\n") {
			t.Errorf("%s glossed across more than one row: %q", call.name, gloss)
		}
	}
}

// The glosses that carry the person's own subject carry it in the product's own
// quotes, and the ones that cannot fall back to the plain shape of the call
// rather than inventing one.
func TestAGlossQuotesItsSubjectOrHonestlyHasNone(t *testing.T) {
	cases := []struct {
		name, args, want string
	}{
		{beltToolRecall, `{"q":"pricing"}`, "searching for «pricing»"},
		{beltToolRecall, `{}`, "searching what was said"},
		{beltToolBoard, `{}`, "looking at the work"},
		{beltToolBoard, `{"q":"the finance one"}`, "looking through the work for «the finance one»"},
		{beltToolTask, `{"instruction":"fix the leak"}`, "putting work in hand: «fix the leak»"},
		{beltToolPlan, `{"job":"task-1"}`, "reading the plan for «task-1»"},
		// The subject sits LAST, so the summariser's cut at the quote leaves a
		// whole phrase behind it rather than a dangling "reading what".
		{beltToolRead, `{"job":"task-1"}`, "reading a file from «task-1»"},
		{beltToolResult, `{"id":"task-1"}`, "reading what came back from «task-1»"},
		{beltToolOpen, `{"id":"task-1"}`, "opening «task-1»"},
		// Arguments the model mangled are not a reason to say something untrue.
		{beltToolOpen, `not json at all`, "opening what was found"},
		{beltToolOpen, `{"id":7}`, "opening what was found"},
	}
	for _, testCase := range cases {
		if got := toolGloss(testCase.name, testCase.args); got != testCase.want {
			t.Errorf("%s(%s) glossed %q, want %q", testCase.name, testCase.args, got, testCase.want)
		}
	}
}

// §1b. THE SUBJECT IS THE LAST THING A GLOSS SAYS, without exception.
//
// The summary row under a landed turn takes the act off the front of a gloss by
// cutting it at its subject (internal/tui2/chat/activity.go), so anything a
// gloss says AFTER its subject is thrown away and whatever preposition or
// pronoun introduced it is left hanging. That is where "read what" came from.
// A gloss that ends on its subject cannot produce one.
func TestAGlossEndsOnItsSubjectSoTheSummaryCannotDangle(t *testing.T) {
	withSubject := []struct{ name, args string }{
		{beltToolBoard, `{"id":"task-1"}`},
		{beltToolBoard, `{"q":"the finance one"}`},
		{beltToolRecall, `{"q":"pricing"}`},
		{beltToolSearch, `{"q":"pricing"}`},
		{beltToolTask, `{"instruction":"fix the leak"}`},
		{beltToolChange, `{"target":"task-1"}`},
		{beltToolManual, `{"page":"threads"}`},
		{beltToolResult, `{"id":"task-1"}`},
		{beltToolPlan, `{"job":"task-1"}`},
		{beltToolRead, `{"file":"/tmp/a.md"}`},
		{beltToolRead, `{"job":"task-1"}`},
		{beltToolOpen, `{"id":"task-1"}`},
		{beltToolWrite, `{"name":"brief.md"}`},
		{beltToolBash, `{"command":"open /tmp/a.md"}`},
	}
	for _, call := range withSubject {
		gloss := toolGloss(call.name, call.args)
		if !strings.Contains(gloss, "«") {
			t.Errorf("%s(%s) glossed without its subject: %q", call.name, call.args, gloss)
			continue
		}
		if !strings.HasSuffix(gloss, "»") {
			t.Errorf("%s(%s) said something after its subject, which the summary will drop and dangle on: %q",
				call.name, call.args, gloss)
		}
	}
}

// A LONG ARGUMENT IS A PHRASE, NOT A PARAGRAPH. The row it lands on is dim,
// indented and one of several; a whole instruction pasted onto it would push
// every row under it around while the reader was reading them.
func TestAGlossClipsALongSubjectToAPhrase(t *testing.T) {
	long := strings.Repeat("rewrite the navigation context ", 8)
	gloss := toolGloss(beltToolTask, `{"instruction":"`+long+`"}`)
	if !strings.HasPrefix(gloss, "putting work in hand: «") {
		t.Fatalf("a long instruction lost its gloss: %q", gloss)
	}
	if len([]rune(gloss)) > glossArgCap+40 {
		t.Fatalf("a long instruction was not clipped: %q", gloss)
	}
	if !strings.Contains(gloss, "…") {
		t.Fatalf("a clipped subject does not say it was clipped: %q", gloss)
	}
}

// THE HINT IS CHEAP OR ABSENT. Counting rows and naming a size are things
// already in front of the caller; summarizing would be a second answer standing
// in front of the one the turn is about to give.
func TestAToolHintIsCheapOrAbsent(t *testing.T) {
	if got := toolHint(beltToolBoard, "one\ntwo\nthree", false); got != "3 rows" {
		t.Errorf("a three-row board hinted %q", got)
	}
	if got := toolHint(beltToolBoard, "", false); got != "nothing" {
		t.Errorf("an empty board hinted %q, want the honest word", got)
	}
	if got := toolHint(beltToolRecall, "only one", false); got != "1 row" {
		t.Errorf("a one-row recall hinted %q", got)
	}
	if got := toolHint(beltToolTask, "commissioned task-9", false); got != "" {
		t.Errorf("a commission hinted %q, want nothing at all", got)
	}
	if got := toolHint(beltToolRead, strings.Repeat("x", 2048), false); got != "2 KB" {
		t.Errorf("a two-kilobyte read hinted %q", got)
	}
	// A failure says one clause of what went wrong and never the whole error.
	failure := toolHint(beltToolOpen, "ERROR: there is no live work with id \"task-3\"\nread the board again", true)
	if strings.Contains(failure, "\n") || strings.Contains(failure, "ERROR") {
		t.Errorf("a failure hint carried the raw error: %q", failure)
	}
	if failure == "" {
		t.Error("a failure said nothing about itself")
	}
}

// A FAILED CALL ENDS AS A FAILURE, on the same seam, in the same order.
func TestAFailedCallEndsAsAFailure(t *testing.T) {
	graph := openHeadStore(t)
	watch := &activityWatch{}
	ctx := provider.WithStreamObserver(context.Background(), watch.observe)
	run := &beltRun{head: New(&beltClient{}, graph), user: store.Message{SessionID: "room"}}

	if _, failed := run.executeWatched(ctx, beltToolPlan, `{"job":"task-nope"}`); !failed {
		t.Fatal("reading the plan of a job that does not exist succeeded")
	}
	begins, ends := watch.of(provider.StreamToolBegin), watch.of(provider.StreamToolFailed)
	if len(begins) != 1 || len(ends) != 1 {
		t.Fatalf("a failed call emitted %d begins and %d failures", len(begins), len(ends))
	}
	if got := watch.of(provider.StreamToolEnd); len(got) != 0 {
		t.Fatalf("a failed call also reported success: %+v", got)
	}
}

// NOTHING LISTENING COSTS NOTHING AND CHANGES NOTHING. Every headless run is
// this case, and the belt's answer must be byte-identical with and without an
// observer on the context.
func TestNarrationIsInvisibleWhenNobodyIsListening(t *testing.T) {
	graph := openHeadStore(t)
	seedExceptBoard(t, graph)
	run := &beltRun{head: New(&beltClient{}, graph), user: store.Message{SessionID: "room"}}

	quiet, quietFailed := run.executeWatched(context.Background(), beltToolBoard, `{}`)
	plain, plainFailed := run.execute(beltToolBoard, `{}`)
	if quiet != plain || quietFailed != plainFailed {
		t.Fatalf("narration changed the answer:\n watched: %q %v\n plain:   %q %v",
			quiet, quietFailed, plain, plainFailed)
	}
}

// THE DELIVERY ABSORPTION SHARES THE SEAM, keyed to its own stream session.
//
// It is the same belt executor and it must be the same narration — a turn woken
// by work landing reads the result, opens the file it names and then speaks, and
// a person watching that happen deserves the same rows as a turn they typed at.
// The key is the absorption's own, which is what keeps those rows out of a live
// conversation's live region.
func TestTheDeliveryAbsorptionNarratesThroughTheSameSeam(t *testing.T) {
	graph := openHeadStore(t)
	ask := postUser(t, graph, "room", "which region grew fastest?")
	settledJob(t, graph, "task-9", "Regional growth read",
		"which region grew fastest?", "North grew 14%. Written to regions.md.")

	client := &beltClient{turns: []beltTurn{
		{calls: []ai.ToolCall{beltCall("a1", beltToolBoard, map[string]any{})}},
		{text: "North grew fastest, at 14%."},
	}}
	watch := &activityWatch{}
	ctx := provider.WithStreamObserver(context.Background(), watch.observe)

	head := New(client, graph)
	cursors := newSessionCursors(ask.Seq)
	cursors.mark("room", ask.Seq)
	if err := head.poll(ctx, cursors); err != nil {
		t.Fatalf("poll: %v", err)
	}

	begins := watch.of(provider.StreamToolBegin)
	if len(begins) == 0 {
		t.Fatal("the absorption read the board and said nothing about doing so")
	}
	want := AbsorbStreamSession("room")
	if begins[0].Session != want {
		t.Fatalf("the absorption's activity is keyed to %q, want %q", begins[0].Session, want)
	}
	if begins[0].Delta != "looking at the work" {
		t.Fatalf("the absorption's begin says %q", begins[0].Delta)
	}
	if len(watch.of(provider.StreamToolEnd, provider.StreamToolFailed)) == 0 {
		t.Fatal("the absorption's read never ended")
	}
}
