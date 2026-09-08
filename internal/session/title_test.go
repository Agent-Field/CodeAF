package session

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/roles"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// ── ANSWERING THE NAMER BY THE SHAPE OF ITS REQUEST ─────────────────────────
//
// The namer is no longer a step of the turn. Since the naming moved to the
// moment the first message is ACCEPTED (title.go), it runs on a goroutine
// nothing orders against the turn's own calls — so a positional script cannot
// hold a slot for it, and a fixture that tried would be a coin toss between the
// turn eating the namer's answer and the namer eating the turn's.
//
// Every test below therefore routes it by PURPOSE. [scriptedCompleter.aside] is
// consulted before the step queue is touched (agent_test.go), so the namer's
// answer is the namer's whatever order the two goroutines run in, and the turn
// rides exactly the script the test wrote. Nothing here asserts a call INDEX;
// what is asserted is which errand got which answer, which is the fact these
// tests are about.

// namerReply is one scripted answer to the session's namer.
type namerReply struct {
	title string
	err   error
}

// namingCompleter answers the SESSION NAMER by the shape of its request and
// leaves the step queue to the turn.
//
// It wraps a [scriptedCompleter] rather than riding its aside because two of the
// facts these tests are about cannot be said through one: an aside answers with
// a response, so it cannot FAIL, and it records no model, so it cannot say which
// rung of the roles ladder the errand rode.
type namingCompleter struct {
	*scriptedCompleter
	mu      sync.Mutex
	replies []namerReply
	asked   int
	models  []string
	// gate holds every answer until the test closes it, which is how a test
	// keeps the name outstanding while it looks at the turn.
	gate chan struct{}
	// arrived is closed by the first ask, so a test waits for the namer to have
	// reached the provider instead of sleeping.
	arrived chan struct{}
	once    sync.Once
}

func naming(inner *scriptedCompleter, replies ...namerReply) *namingCompleter {
	return &namingCompleter{scriptedCompleter: inner, replies: replies, arrived: make(chan struct{})}
}

func (n *namingCompleter) CompleteWithMessages(ctx context.Context, messages []ai.Message, options ...ai.Option) (*ai.Response, error) {
	if !isTitleCall(messages) {
		return n.scriptedCompleter.CompleteWithMessages(ctx, messages, options...)
	}
	var request ai.Request
	for _, option := range options {
		_ = option(&request)
	}
	n.mu.Lock()
	at := n.asked
	n.asked++
	n.models = append(n.models, request.Model)
	gate := n.gate
	replies := n.replies
	n.mu.Unlock()
	n.once.Do(func() { close(n.arrived) })
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if len(replies) == 0 {
		return textResponse(""), nil
	}
	if at >= len(replies) {
		at = len(replies) - 1
	}
	if reply := replies[at]; reply.err != nil {
		return nil, reply.err
	}
	return textResponse(replies[at].title), nil
}

// asks is how many times the namer reached the provider.
func (n *namingCompleter) asks() int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.asked
}

// namerModel is the model one attempt rode.
func (n *namingCompleter) namerModel(at int) string {
	n.mu.Lock()
	defer n.mu.Unlock()
	if at >= len(n.models) {
		return ""
	}
	return n.models[at]
}

// waitAsks blocks until the namer has been reached want times.
func (n *namingCompleter) waitAsks(t *testing.T, want int, what string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if n.asks() >= want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("the namer was asked %d times, want %d — %s", n.asks(), want, what)
}

// oneTurn is a session's first turn and nothing else: the namer is answered by
// purpose, never by a step.
func oneTurn(answer string) []step {
	return []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse(answer), nil
		},
	}
}

// namedTurn is [oneTurn] with the namer answering title.
func namedTurn(t *testing.T, answer, title string) *namingCompleter {
	t.Helper()
	return naming(&scriptedCompleter{steps: oneTurn(answer)}, namerReply{title: title})
}

// awaitTitle waits for the session to have named itself. The naming runs on a
// goroutine beside the turn, so there is no point in the turn at which it has
// certainly finished — which is the feature.
func awaitTitle(t *testing.T, agent *Agent) string {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if title := agent.Title(); title != "" {
			return title
		}
		time.Sleep(time.Millisecond)
	}
	return ""
}

func titleAgent(t *testing.T, completer Completer, mutate func(*Config)) (*Agent, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "session.jsonl")
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.SessionFile = path
		if mutate != nil {
			mutate(config)
		}
	})
	return agent, path
}

// The session names itself once, writes the name to its journal, says so on
// the turn's stream, and answers Title() with it.
func TestTitleLandsInTheFileAndOnTheStream(t *testing.T) {
	completer := namedTurn(t, "the parser is fine", "  \"Tokenizer Bug Hunt.\"  ")
	agent, path := titleAgent(t, completer, nil)

	// The naming lane is taken BEFORE the turn, because a name that lands after
	// a short answer has no turn stream left to arrive on — which is the whole
	// of what this change moved (title.go).
	lane, leave := agent.WatchTitle()
	defer leave()

	events := collect(t, mustSubmit(t, agent, "why is the tokenizer slow?"))
	if got := awaitTitle(t, agent); got == "" {
		t.Fatalf("the session never named itself; turn said %v", kinds(events))
	}

	changed, ok := firstOfKind(events, EventTitleChanged)
	if !ok {
		// The name landed after the turn's hub closed, which is legal and
		// common: the standing lane is the road it took.
		select {
		case changed = <-lane:
		case <-time.After(2 * time.Second):
			t.Fatalf("no EventTitleChanged on either road; turn said %v", kinds(events))
		}
	}
	// The quotes, the trailing stop and the surrounding space are the three
	// things a model adds against the instruction.
	if changed.Text != "Tokenizer Bug Hunt" {
		t.Fatalf("title = %q, want it cleaned up", changed.Text)
	}
	if got := agent.Title(); got != "Tokenizer Bug Hunt" {
		t.Fatalf("Title() = %q", got)
	}
	if err := agent.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	titles := 0
	for _, line := range readLines(t, path) {
		if strings.Contains(line, `"type":"title"`) {
			titles++
			if !strings.Contains(line, `"title":"Tokenizer Bug Hunt"`) {
				t.Fatalf("title line = %s", line)
			}
		}
	}
	if titles != 1 {
		t.Fatalf("%d title lines, want exactly 1", titles)
	}
}

// A NAME IS WORDS, even when the model answers with a filename. The instruction
// asks for eight lowercase words and a model that has read a million
// identifiers sometimes welds them together; the welding is undone once, here,
// rather than at each of the places the name is drawn.
func TestASluggedTitleIsMintedAsWords(t *testing.T) {
	for _, row := range []struct{ said, want string }{
		{"porting_the_parser", "porting the parser"},
		{"fix-the-nil-map", "fix the nil map"},
		// A name that is already words keeps every character it has, hyphens
		// inside those words included: they are somebody's spelling, not a
		// separator this function gets to reinterpret.
		{"port-b failures", "port-b failures"},
	} {
		completer := namedTurn(t, "the parser is fine", row.said)
		agent, _ := titleAgent(t, completer, nil)
		collect(t, mustSubmit(t, agent, "why is the tokenizer slow?"))
		if got := awaitTitle(t, agent); got != row.want {
			t.Fatalf("a title answered as %q was minted %q, want %q", row.said, got, row.want)
		}
		if err := agent.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	}
}

// One name per session: the second turn does not pay for a second one, and a
// resumed session keeps the name it already has.
func TestTheSessionIsNamedOnlyOnce(t *testing.T) {
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("first"), nil },
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("second"), nil },
	}}
	asked := naming(completer, namerReply{title: "tokenizer speed"})
	agent, path := titleAgent(t, asked, nil)

	collect(t, mustSubmit(t, agent, "why is the tokenizer slow?"))
	if got := awaitTitle(t, agent); got != "tokenizer speed" {
		t.Fatalf("the first turn left the session called %q", got)
	}
	// Title() changes under the naming goroutine before that goroutine publishes
	// its one event. Join the publication before opening a second turn, or a
	// scheduler pause in that gap makes the first turn's legitimate event arrive
	// on the second turn's hub and look like a second naming attempt.
	agent.waitForTitle()
	second := collect(t, mustSubmit(t, agent, "and the parser?"))

	if got := countKind(second, EventTitleChanged); got != 0 {
		t.Fatalf("the second turn named the session again (%d events)", got)
	}
	// THE COUNT IS THE ASSERTION AND THE ORDER IS NOT. Two turns rode the
	// script; the namer was reached once, whenever it got there.
	if got := asked.asks(); got != 1 {
		t.Fatalf("the namer was asked %d times, want 1", got)
	}
	if completer.requests() != 2 {
		t.Fatalf("requests = %d, want 2 turns — the namer took a step", completer.requests())
	}
	if err := agent.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// A resume reads the name off the file rather than paying for it again.
	resumed, err := newAgent(Config{
		Workspace: agent.config.Workspace, Model: "test/model", System: "SYSTEM", SessionFile: path,
	}, &scriptedCompleter{})
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	t.Cleanup(func() { _ = resumed.Close() })
	if got := resumed.Title(); got != "tokenizer speed" {
		t.Fatalf("resumed Title() = %q", got)
	}
	collect(t, mustSubmit(t, resumed, "carry on"))
	if got := resumed.Title(); got != "tokenizer speed" {
		t.Fatalf("the resumed session renamed itself to %q", got)
	}
}

// The namer rides internal/roles: a pin beats the tier, the tier beats the
// session model, and the turn itself is untouched by either.
func TestTitleModelFollowsTheRolesLadder(t *testing.T) {
	for _, testCase := range []struct {
		name     string
		settings map[string]string
		want     string
	}{
		{"nothing set", nil, "test/model"},
		{"the tier", map[string]string{roles.TierKey(roles.TierLow): "cheap/model"}, "cheap/model"},
		{"the pin beats the tier", map[string]string{
			roles.TierKey(roles.TierLow):       "cheap/model",
			roles.PinKey(roles.RoleTitle):      "pinned/model",
			roles.PinKey(roles.RoleCompaction): "other/model",
		}, "pinned/model"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			inner := &scriptedCompleter{steps: oneTurn("answered")}
			completer := naming(inner, namerReply{title: "a name"})
			settings := testCase.settings
			agent, _ := titleAgent(t, completer, func(config *Config) {
				config.RolesSource = func(key string) (string, bool) {
					value, ok := settings[key]
					return value, ok
				}
			})
			collect(t, mustSubmit(t, agent, "why is the tokenizer slow?"))

			if got := inner.model(0); got != "test/model" {
				t.Fatalf("the turn rode %q, want the session model", got)
			}
			completer.waitAsks(t, 1, "the namer never reached the provider")
			if got := completer.namerModel(0); got != testCase.want {
				t.Fatalf("the namer rode %q, want %q", got, testCase.want)
			}
		})
	}
}

// A namer that fails leaves the session unnamed and the turn untouched. It is
// bookkeeping: nothing the person asked for went wrong.
func TestAFailedTitleNeverBreaksTheTurn(t *testing.T) {
	// A PERMANENT failure, so the ladder gives up rather than asking again: the
	// retry is for the wire, and quota is the wire saying no forever
	// (loop.go's nonRetryablePattern).
	completer := naming(&scriptedCompleter{steps: oneTurn("the answer")},
		namerReply{err: errors.New("insufficient quota")})
	agent, path := titleAgent(t, completer, nil)

	events := collect(t, mustSubmit(t, agent, "why is the tokenizer slow?"))
	completer.waitAsks(t, 1, "the namer never ran")

	if last := events[len(events)-1]; last.Kind != EventTurnDone && last.Kind != EventTitleChanged {
		t.Fatalf("turn ended with %v, want it to end normally", last.Kind)
	}
	if _, failed := firstOfKind(events, EventError); failed {
		t.Fatal("a failed namer was reported as a failed turn")
	}
	if got := agent.Title(); got != "" {
		t.Fatalf("Title() = %q, want the session to stay unnamed", got)
	}
	if got := messageText(lastMessage(agent)); got != "the answer" {
		t.Fatalf("transcript tail = %q, want the turn's own answer", got)
	}
	if got := completer.asks(); got != 1 {
		t.Fatalf("a permanent failure was asked %d times, want 1", got)
	}
	if err := agent.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	for _, line := range readLines(t, path) {
		if strings.Contains(line, `"type":"title"`) {
			t.Fatalf("a failed namer wrote a title line: %s", line)
		}
	}
}

// A session with no journal has nowhere to keep a name and nothing to be
// listed in, so it does not pay for one.
func TestAnInMemorySessionDoesNotNameItself(t *testing.T) {
	completer := namedTurn(t, "the answer", "a name")
	agent, _ := newTestAgent(t, completer, nil)

	events := collect(t, mustSubmit(t, agent, "why is the tokenizer slow?"))

	if got := completer.asks(); got != 0 {
		t.Fatalf("an in-memory session asked the namer %d times", got)
	}
	if countKind(events, EventTitleChanged) != 0 {
		t.Fatal("an in-memory session announced a name")
	}
}

// ── the naming runs BESIDE the answer ───────────────────────────────────────
//
// The three tests below are the whole of what #653 moved, said as timing: the
// name is bought when the first message is accepted, so it can land before the
// answer does, and the answer never waits for it either way.

// A NAME CAN REACH THE PERSON WHILE THE ANSWER IS STILL BEING WRITTEN. The turn
// here is held open until the session has named itself, which is what a long
// answer is: the name arrives on the turn's own stream, ahead of its end.
func TestTheNameLandsWhileTheAnswerIsStillBeingWritten(t *testing.T) {
	named := make(chan struct{})
	var agent *Agent
	completer := naming(&scriptedCompleter{steps: []step{
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			// The answer is not finished until the name has landed — a long
			// answer, in the only form a fixture can state one.
			select {
			case <-named:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			return textResponse("the parser is fine"), nil
		},
	}}, namerReply{title: "tokenizer speed"})

	agent, _ = titleAgent(t, completer, nil)
	lane, stop := agent.WatchTitle()
	defer stop()
	stream := mustSubmit(t, agent, "Investigate the parser migration regression: reproduce the failing integration suite, compare tokenization before and after the refactor, implement a compatible fix, and verify the existing API contract.")
	waitTitleEvent(t, lane)
	close(named)
	events := collect(t, stream)

	changed, ok := firstOfKind(events, EventTitleChanged)
	if !ok {
		t.Fatalf("the name did not reach the turn's own stream; got %v", kinds(events))
	}
	if changed.Text != "tokenizer speed" {
		t.Fatalf("the turn's stream carried %q", changed.Text)
	}
	// AND IT CAME FIRST. The whole point of the move is that a person waiting on
	// a long answer is not also waiting on the name of the thing they asked.
	titleAt, doneAt := -1, -1
	for at, event := range events {
		if event.Kind == EventTitleChanged && titleAt < 0 {
			titleAt = at
		}
		if event.Kind == EventTurnDone && doneAt < 0 {
			doneAt = at
		}
	}
	if doneAt >= 0 && titleAt > doneAt {
		t.Fatalf("the name landed after the turn ended: %v", kinds(events))
	}
}

// AND AN ANSWER NEVER WAITS FOR A NAME. The namer is held here for as long as
// the test likes and the turn finishes anyway; the name lands afterwards, on the
// standing lane, because the turn's hub has closed by then.
func TestASlowNameNeverDelaysTheAnswer(t *testing.T) {
	completer := naming(&scriptedCompleter{steps: oneTurn("the parser is fine")},
		namerReply{title: "tokenizer speed"})
	completer.gate = make(chan struct{})
	agent, _ := titleAgent(t, completer, nil)

	lane, leave := agent.WatchTitle()
	defer leave()

	events := collect(t, mustSubmit(t, agent, "why is the tokenizer slow?"))
	if _, ended := firstOfKind(events, EventTurnDone); !ended {
		t.Fatalf("the turn did not finish with the namer still out: %v", kinds(events))
	}
	if countKind(events, EventTitleChanged) != 0 {
		t.Fatal("the turn's stream carried a name the namer had not given yet")
	}
	if got := agent.Title(); got != "" {
		t.Fatalf("the session was named %q before the namer answered", got)
	}

	// The answer is delivered; NOW the name arrives, with no turn left to carry
	// it. The standing lane is the road it takes (title.go).
	close(completer.gate)
	select {
	case event := <-lane:
		if event.Kind != EventTitleChanged || event.Text != "tokenizer speed" {
			t.Fatalf("the naming lane carried %v %q", event.Kind, event.Text)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("a name that missed the turn never reached the standing lane")
	}
	if got := awaitTitle(t, agent); got != "tokenizer speed" {
		t.Fatalf("Title() = %q", got)
	}
}

// ── asking again ────────────────────────────────────────────────────────────

// A TRANSIENT FAILURE IS ASKED AGAIN, WITHOUT ANOTHER TURN. The person does not
// speak again between the two attempts — the errand walks its own ladder — and
// the answer they are waiting for is still being written while it does.
func TestATransientFailureIsAskedAgainWithoutAnotherTurn(t *testing.T) {
	named := make(chan struct{})
	var agent *Agent
	completer := naming(&scriptedCompleter{steps: []step{
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			select {
			case <-named:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			return textResponse("the parser is fine"), nil
		},
	}},
		namerReply{err: errors.New("connection reset by peer")},
		namerReply{title: "tokenizer speed"},
	)
	agent, _ = titleAgent(t, completer, nil)

	lane, stop := agent.WatchTitle()
	defer stop()
	stream := mustSubmit(t, agent, "Investigate the parser migration regression: reproduce the failing integration suite, compare tokenization before and after the refactor, implement a compatible fix, and verify the existing API contract.")
	waitTitleEvent(t, lane)
	close(named)
	collect(t, stream)

	if got := agent.Title(); got != "tokenizer speed" {
		t.Fatalf("Title() = %q, want the name the second attempt earned", got)
	}
	if got := completer.asks(); got != 2 {
		t.Fatalf("the namer was asked %d times, want 2 — one failure and one recovery", got)
	}
}

// AND A STOPPED GENERATION DOES NOT SUPPRESS THE SECOND TRY. One Esc allows one
// planner and one task name (interrupt_fan.go); the session's own namer is not
// on that list, because its dedup is one naming per SESSION and a retry landing
// inside a live generation used to come back as silence and leave the session
// unnamed for good.
func TestAStoppedGenerationDoesNotSuppressTheNamersSecondTry(t *testing.T) {
	completer := naming(&scriptedCompleter{steps: oneTurn("the parser is fine")},
		namerReply{err: errors.New("connection reset by peer")},
		namerReply{title: "tokenizer speed"},
	)
	agent, _ := titleAgent(t, completer, nil)
	// A generation is live, and the task namer has already spent its allowance
	// in it — the exact shape that used to leave the session unnamed.
	agent.interrupt.begin()
	if err := agent.interrupt.allow(roles.RoleTaskName); err != nil {
		t.Fatalf("the first task name in a generation was refused: %v", err)
	}

	collect(t, mustSubmit(t, agent, "why is the tokenizer slow?"))

	if got := awaitTitle(t, agent); got != "tokenizer speed" {
		t.Fatalf("Title() = %q — a stop suppressed the naming", got)
	}
	if got := completer.asks(); got != 2 {
		t.Fatalf("the namer was asked %d times, want 2", got)
	}
}

// A NAME ALREADY THERE WINS, whoever put it there. The namer is held out while
// the session is named through the door that names it, and its own answer is
// then refused: no second journal line, no event, and the name on screen does
// not change under the person.
func TestANameThatArrivesAfterAnotherOneIsRefused(t *testing.T) {
	completer := naming(&scriptedCompleter{steps: oneTurn("the parser is fine")},
		namerReply{title: "the namers own idea"})
	completer.gate = make(chan struct{})
	agent, path := titleAgent(t, completer, nil)

	collect(t, mustSubmit(t, agent, "why is the tokenizer slow?"))
	completer.waitAsks(t, 1, "the namer never reached the provider")
	if !agent.setTitleIfUnnamed("the name that was already there") {
		t.Fatal("naming an unnamed session was refused")
	}
	close(completer.gate)

	// Let the provider result reach the acceptance guard before Close can cancel
	// it. Otherwise cancellation would mask a broken existing-title check.
	waitTitleJob(t, agent)
	if err := agent.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if got := agent.Title(); got != "the name that was already there" {
		t.Fatalf("Title() = %q — the namer overwrote a name it did not give", got)
	}
	titles := []string{}
	for _, line := range readLines(t, path) {
		if strings.Contains(line, `"type":"title"`) {
			titles = append(titles, line)
		}
	}
	if len(titles) != 1 || !strings.Contains(titles[0], `"title":"the name that was already there"`) {
		t.Fatalf("title lines = %v, want the one name that won", titles)
	}
}

// CLOSE CUTS A NAME THAT IS STILL BACKING OFF, and does not wait out the ladder
// to do it. The first attempt fails on the wire, the errand sleeps, and the quit
// arrives in the middle of that sleep.
func TestCloseCutsANameThatIsStillBackingOff(t *testing.T) {
	completer := naming(&scriptedCompleter{steps: oneTurn("the parser is fine")},
		namerReply{err: errors.New("connection reset by peer")},
		namerReply{title: "tokenizer speed"},
	)
	agent, path := titleAgent(t, completer, nil)

	collect(t, mustSubmit(t, agent, "why is the tokenizer slow?"))
	completer.waitAsks(t, 1, "the namer never made its first attempt")

	if err := agent.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if agent.titleCtx.Err() == nil {
		t.Fatal("Close did not cancel the naming lifetime")
	}
	waitTitleJob(t, agent)
	if got := completer.asks(); got != 1 {
		t.Fatalf("the namer asked %d times after the session closed, want 1", got)
	}
	if got := agent.Title(); got != "" {
		t.Fatalf("a closed session named itself %q", got)
	}
	for _, line := range readLines(t, path) {
		if strings.Contains(line, `"type":"title"`) {
			t.Fatalf("a cut naming wrote a title line: %s", line)
		}
	}
}

// THE NAME'S COST IS THE SESSION'S AND NOT THE TURN'S. The errand is on nobody's
// clock — it outlives the turn that started it by construction — so its tokens
// belong to the conversation's total and to the machine's ledger, and never to
// the running turn's share, which is the figure an abandoned turn is journaled
// with (loop.go's [Agent.addDetachedUsageAs]).
func TestTheNamesCostIsTheSessionsAndNotTheTurns(t *testing.T) {
	named := make(chan struct{})
	var agent *Agent
	completer := naming(&scriptedCompleter{steps: []step{
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			select {
			case <-named:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			return textResponse("the parser is fine"), nil
		},
	}}, namerReply{title: "tokenizer speed"})
	agent, _ = titleAgent(t, completer, nil)

	lane, stop := agent.WatchTitle()
	defer stop()
	stream := mustSubmit(t, agent, "Investigate the parser migration regression: reproduce the failing integration suite, compare tokenization before and after the refactor, implement a compatible fix, and verify the existing API contract.")
	waitTitleEvent(t, lane)
	close(named)
	events := collect(t, stream)

	done, ok := firstOfKind(events, EventTurnDone)
	if !ok {
		t.Fatalf("no EventTurnDone; got %v", kinds(events))
	}
	// The turn made exactly one call, and its seal says so — the namer's call
	// landed in the middle of it and is not on this figure.
	if done.Usage.Calls != 1 {
		t.Fatalf("the turn was sealed with %d calls, want its own one", done.Usage.Calls)
	}
	// AND THE SESSION PAID FOR BOTH.
	if got := agent.Usage().Calls; got != 2 {
		t.Fatalf("the session counted %d calls, want the turn's and the namer's", got)
	}
}

func waitTitleEvent(t *testing.T, lane <-chan Event) Event {
	t.Helper()
	select {
	case event, ok := <-lane:
		if !ok || event.Kind != EventTitleChanged || event.Text == "" {
			t.Fatalf("invalid title event: %+v open=%v", event, ok)
		}
		return event
	case <-time.After(10 * time.Second):
		t.Fatal("title event did not arrive")
	}
	return Event{}
}

func waitTitleJob(t *testing.T, agent *Agent) {
	t.Helper()
	done := make(chan struct{})
	go func() { agent.titleJobs.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("naming job did not finish")
	}
}

func TestAnExistingTitleStopsAutomaticRetriesBeforeAnotherCall(t *testing.T) {
	completer := naming(&scriptedCompleter{steps: oneTurn("answer")}, namerReply{err: errors.New("connection reset by peer")})
	agent, _ := titleAgent(t, completer, nil)
	collect(t, mustSubmit(t, agent, "Investigate parser migration compatibility"))
	completer.waitAsks(t, 1, "first title attempt")
	if !agent.setTitleIfUnnamed("chosen parser title") {
		t.Fatal("existing title was refused")
	}
	waitTitleJob(t, agent)
	if got := completer.asks(); got != 1 {
		t.Fatalf("existing title still paid for %d attempts", got)
	}
	if agent.Title() != "chosen parser title" {
		t.Fatal("retry replaced existing title")
	}
}

func TestARungDeadlineCanRetryWithinTheNamingWindow(t *testing.T) {
	completer := naming(&scriptedCompleter{}, namerReply{err: context.DeadlineExceeded})
	agent, _ := titleAgent(t, completer, nil)
	_, retry := agent.askForName(context.Background(), "Fix parser compatibility", "", "test/model")
	if !retry {
		t.Fatal("a rung timeout suppressed retry while naming context remained live")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, retry = agent.askForName(ctx, "Fix parser compatibility", "", "test/model")
	if retry {
		t.Fatal("a canceled naming context requested another attempt")
	}
}

func TestCloseCancelsTheForegroundAndItsBlockedNamer(t *testing.T) {
	mainEntered, mainCanceled := make(chan struct{}), make(chan struct{})
	completer := naming(&scriptedCompleter{steps: []step{func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
		close(mainEntered)
		<-ctx.Done()
		close(mainCanceled)
		return nil, ctx.Err()
	}}}, namerReply{title: "should never land"})
	completer.gate = make(chan struct{})
	agent, _ := titleAgent(t, completer, nil)
	stream := mustSubmit(t, agent, "Investigate a failing developer integration suite")
	waitMetaPatch(t, mainEntered)
	waitMetaPatch(t, completer.arrived)
	closed := make(chan struct{})
	go func() { _ = agent.Close(); close(closed) }()
	waitMetaPatch(t, mainCanceled)
	waitMetaPatch(t, closed)
	waitTitleJob(t, agent)
	events := collect(t, stream)
	if agent.Title() != "" || countKind(events, EventTitleChanged) != 0 {
		t.Fatal("closed naming published a title")
	}
	if completer.asks() != 1 {
		t.Fatal("closing started another naming attempt")
	}
}
