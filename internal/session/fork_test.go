package session

// THE FORK, TESTED WHERE IT IS LOAD-BEARING.
//
// Everything here is driven through real turns against a scripted provider,
// because every claim this verb makes is a claim about what actually reaches a
// model: that a hand opens on the caller's transcript, that it cannot write
// outside its slice, that it has no fork verb of its own, and that none of them
// outlive the turn. A test that asserted the struct would have passed on all of
// those while the running program did none of them.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/exec/bare"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// ── the scripted fork ───────────────────────────────────────────────────────

// forkCompleter answers the CALLER and the HANDS from one script, telling them
// apart by the charge a hand opens on ([forkCharge]).
//
// It is a completer of its own rather than a [scriptedCompleter] with more steps
// because the hands run CONCURRENTLY: a script indexed by arrival order would be
// answering whichever hand happened to get to the provider first, and every
// assertion below would be about a race.
type forkCompleter struct {
	mu sync.Mutex

	// caller is what the conversation is answered with, by request number.
	caller []step
	// callerTail answers every caller request past the end of the script, over
	// and over. It is what a caller that KEEPS WORKING WHILE ITS HANDS ARE OUT
	// looks like from the provider's side, and every test below that needs its
	// hands to actually run uses [keepWorking] for it: a fork returns in the
	// time it takes to spawn, so a script that says its piece and stops is a
	// turn that ended before a single hand made a request.
	callerTail step
	// callerSeen counts the requests that were NOT a hand's.
	callerSeen int
	// callerRequests keeps them, so a test can read what the caller was sent.
	callerRequests [][]ai.Message
	// agent is what these steps are driving, so a tail step can ask whether the
	// hands are home. It is set after construction; read it under the lock.
	agent *Agent

	// hand answers one hand's nth request. turn is 1 for its first.
	hand func(index, turn int, messages []ai.Message) (*ai.Response, error)
	// turns counts each hand's requests, and requests keeps the first one — the
	// only place the seeded transcript can be read.
	turns    map[int]int
	requests map[int][]ai.Message

	// meet, when set, holds every hand's FIRST request until want of them have
	// arrived, so that concurrency is proved rather than assumed.
	want int
	live int
	peak int
	all  chan struct{}
	once sync.Once
}

// handIndexOf reads which hand a request belongs to off its charge, and answers
// 0 for the caller's own. The charge is the only message a hand has that the
// caller does not.
//
// It looks for the charge ANYWHERE in the transcript rather than at the tail,
// and that is not defensive coding: a hand's own turn grows past its charge like
// any other — a nudge from the loop detector lands as a user message (looped.go)
// — so a reader that only checked the last one would start calling a hand the
// caller the moment anything was said to it.
func handIndexOf(messages []ai.Message) int {
	for index := len(messages) - 1; index >= 0; index-- {
		if !strings.EqualFold(messages[index].Role, "user") {
			continue
		}
		text := messageContentText(messages[index])
		if !strings.HasPrefix(text, "You are hand ") {
			continue
		}
		var hand, of int
		if _, err := fmt.Sscanf(text, "You are hand %d of %d", &hand, &of); err != nil {
			return 0
		}
		return hand
	}
	return 0
}

func (c *forkCompleter) CompleteWithMessages(ctx context.Context, messages []ai.Message, options ...ai.Option) (*ai.Response, error) {
	snapshot := make([]ai.Message, len(messages))
	copy(snapshot, messages)
	index := handIndexOf(snapshot)

	// AN ERRAND IS NOT A TURN. The session names itself, judges its own routing
	// and reads its exchanges for anything worth keeping, and every one of those
	// is a request through this same completer (loop.go's addAuxiliaryUsage names
	// the family). None of them carries the belt, which is what tells them apart
	// — and a test that counted them as the caller's own rounds would be counting
	// the session's housekeeping as the person's waiting.
	var request ai.Request
	for _, option := range options {
		_ = option(&request)
	}
	if len(request.Tools) == 0 {
		return textResponse("an errand nobody asked for"), nil
	}

	if index == 0 {
		c.mu.Lock()
		at := c.callerSeen
		c.callerSeen++
		c.callerRequests = append(c.callerRequests, snapshot)
		var next step
		switch {
		case at < len(c.caller):
			next = c.caller[at]
		default:
			next = c.callerTail
		}
		c.mu.Unlock()
		if next == nil {
			return textResponse("(the caller is out of script)"), nil
		}
		return next(ctx, snapshot)
	}

	c.mu.Lock()
	c.turns[index]++
	turn := c.turns[index]
	if turn == 1 {
		c.requests[index] = snapshot
	}
	c.mu.Unlock()

	if turn == 1 && c.want > 0 {
		c.gather(ctx)
	}
	if c.hand == nil {
		return textResponse("hand done"), nil
	}
	return c.hand(index, turn, snapshot)
}

// gather holds one hand until every hand has arrived, which is how "they ran at
// the same time" is asserted rather than hoped for. A fork that ran its hands in
// sequence never reaches the count, every hand leaves on the deadline instead,
// and the peak the test reads is 1.
func (c *forkCompleter) gather(ctx context.Context) {
	c.mu.Lock()
	c.live++
	if c.live > c.peak {
		c.peak = c.live
	}
	reached := c.live >= c.want
	c.mu.Unlock()
	if reached {
		c.once.Do(func() { close(c.all) })
	}
	select {
	case <-c.all:
	case <-ctx.Done():
	case <-time.After(2 * time.Second):
	}
	c.mu.Lock()
	c.live--
	c.mu.Unlock()
}

func (c *forkCompleter) FallbackModels(string) []string { return nil }

func (c *forkCompleter) handRequest(index int) []ai.Message {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.requests[index]
}

func (c *forkCompleter) handTurns(index int) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.turns[index]
}

func newForkCompleter(want int) *forkCompleter {
	return &forkCompleter{
		want:     want,
		all:      make(chan struct{}),
		turns:    map[int]int{},
		requests: map[int][]ai.Message{},
	}
}

// drive is the completer told which agent its steps are driving.
func (c *forkCompleter) drive(agent *Agent) *Agent {
	c.mu.Lock()
	c.agent = agent
	c.mu.Unlock()
	return agent
}

func (c *forkCompleter) driven() *Agent {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.agent
}

// keepWorking is the caller doing what this whole change exists to let it do:
// working while its hands are out. It answers every request with a cheap look at
// the tree — one more tool round, one more step boundary, one more chance for a
// finished hand's report to land in the turn — and says its closing piece only
// once every hand has reported.
//
// It is also, incidentally, the proof of the fifth claim: a caller that could
// not call anything between hand completions would be a caller still inside a
// barrier.
func keepWorking(completer *forkCompleter, closing string) step {
	turn := 0
	return func(context.Context, []ai.Message) (*ai.Response, error) {
		turn++
		agent := completer.driven()
		if agent != nil && agent.jobs.handsOutstanding() && turn < 400 {
			time.Sleep(2 * time.Millisecond)
			// The argument moves every time so what ends this loop is the hands
			// coming home and never the loop detector (looped.go).
			return toolResponse(fmt.Sprintf("look-%d", turn), "ls",
				fmt.Sprintf(`{"path":".","limit":%d}`, turn)), nil
		}
		return textResponse(closing), nil
	}
}

// handsAreHome waits for every hand to have reported, and fails rather than
// hanging. It reads the same count a node's landing reads.
func handsAreHome(t *testing.T, agent *Agent) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for agent.jobs.handsOutstanding() {
		if time.Now().After(deadline) {
			t.Fatal("hands were still out ten seconds after the turn ended")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// forkCall is the arguments of one fork, spelled the way a model spells them.
func forkCall(parts ...string) string {
	return `{"parts":[` + strings.Join(parts, ",") + `]}`
}

func forkPartJSON(role string, scope ...string) string {
	quoted := make([]string, len(scope))
	for index, path := range scope {
		quoted[index] = `"` + path + `"`
	}
	return `{"role":"` + role + `","scope":[` + strings.Join(quoted, ",") + `]}`
}

// theForkRoster is the fork CALL's own tool result — the roster it answers
// with, read off the caller's last request.
func theForkRoster(t *testing.T, completer *forkCompleter) string {
	t.Helper()
	completer.mu.Lock()
	defer completer.mu.Unlock()
	for at := len(completer.callerRequests) - 1; at >= 0; at-- {
		for _, message := range completer.callerRequests[at] {
			if strings.EqualFold(message.Role, "tool") {
				if text := messageContentText(message); strings.Contains(text, forkOutLead) {
					return text
				}
			}
		}
	}
	t.Fatalf("no fork roster reached the caller; it made %d requests", len(completer.callerRequests))
	return ""
}

// theHandReports is every hand's report as it reached the caller: user-role
// content from the boundary batch, IN THE ORDER THE REPORTS LANDED, read off
// the last request the caller was sent.
//
// It reads the transcript rather than the events for the reason every assertion
// in this file reads it: the claim is about what a MODEL is answered with, and a
// report that reached a surface and not the next request would satisfy an event
// assertion perfectly while being invisible to the mind it was written for.
func theHandReports(t *testing.T, completer *forkCompleter) []string {
	t.Helper()
	completer.mu.Lock()
	defer completer.mu.Unlock()
	if len(completer.callerRequests) == 0 {
		t.Fatal("the caller never made a request")
	}
	var reports []string
	for _, message := range completer.callerRequests[len(completer.callerRequests)-1] {
		if !strings.EqualFold(message.Role, "user") {
			continue
		}
		text := messageContentText(message)
		if strings.HasPrefix(text, handReportLead) || strings.HasPrefix(text, strings.ToUpper(forkOutOf)) {
			reports = append(reports, text)
			continue
		}
		if !strings.HasPrefix(text, "while you worked:") {
			continue
		}
		var report []string
		flush := func() {
			if len(report) == 0 {
				return
			}
			reports = append(reports, strings.TrimSpace(strings.Join(report, "\n")))
			report = nil
		}
		for _, line := range strings.Split(strings.TrimPrefix(text, "while you worked:"), "\n") {
			trimmed := strings.TrimSpace(line)
			var hand, of int
			_, ordinary := fmt.Sscanf(trimmed, handReportLead+"%d of %d", &hand, &of)
			_, exhausted := fmt.Sscanf(trimmed,
				strings.ToUpper(forkOutOf)+" · "+handReportLead+"%d of %d", &hand, &of)
			opening := ordinary == nil || exhausted == nil
			if opening {
				flush()
			}
			if len(report) > 0 || opening {
				report = append(report, line)
			}
		}
		flush()
	}
	return reports
}

// ── the fork itself ─────────────────────────────────────────────────────────

// THREE HANDS, THREE SLICES, AT THE SAME TIME — and the CALL COMES BACK BEFORE
// ANY OF THEM DOES, naming them.
//
// This is the whole change. The old verb answered "three hands, all back" after
// the slowest one landed; this one answers a roster while every hand is still
// working, and the caller's next thought is its own.
func TestForkReturnsBeforeItsHandsDoAndNamesThem(t *testing.T) {
	completer := newForkCompleter(3)
	completer.caller = []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("call-1", "fork", forkCall(
				forkPartJSON("the adapters", "adapters"),
				forkPartJSON("the docs", "docs"),
				forkPartJSON("the fixtures", "fixtures"),
			)), nil
		},
	}
	completer.callerTail = keepWorking(completer, "stitched")
	// Every hand is held at the gather until all three have arrived, so NONE of
	// them can have finished when the fork call answers.
	completer.hand = func(index, _ int, _ []ai.Message) (*ai.Response, error) {
		return textResponse(fmt.Sprintf("hand %d says its piece", index)), nil
	}

	agent, _ := newTestAgent(t, completer, nil)
	completer.drive(agent)
	collect(t, mustSubmit(t, agent, "do the wide thing"))
	handsAreHome(t, agent)

	completer.mu.Lock()
	peak := completer.peak
	completer.mu.Unlock()
	if peak < 3 {
		t.Fatalf("at most %d hands were ever in flight at once, so they ran in sequence", peak)
	}

	roster := theForkRoster(t, completer)
	if !strings.HasPrefix(roster, "three "+forkOutLead) {
		t.Fatalf("the call does not answer with a roster:\n%s", roster)
	}
	for hand, role := range []string{"the adapters", "the docs", "the fixtures"} {
		want := fmt.Sprintf("hand %d — %s", hand+1, role)
		if !strings.Contains(roster, want) {
			t.Fatalf("the roster does not name %q:\n%s", want, roster)
		}
	}
	// THE ROSTER IS NOT A RESULT. Nothing a hand said can be in it, because no
	// hand had said anything when it was written.
	for hand := 1; hand <= 3; hand++ {
		if strings.Contains(roster, fmt.Sprintf("hand %d says its piece", hand)) {
			t.Fatalf("the call waited for hand %d before answering:\n%s", hand, roster)
		}
	}
	// And it tells the caller the two things it would otherwise get wrong.
	for _, want := range []string{"ON ITS OWN", "KEEP WORKING"} {
		if !strings.Contains(roster, want) {
			t.Errorf("the roster does not say %q:\n%s", want, roster)
		}
	}

	// Every hand's report then arrives on its own, and each one carries that
	// hand's own words.
	reports := theHandReports(t, completer)
	if len(reports) != 3 {
		t.Fatalf("%d hand reports reached the caller, want 3:\n%s", len(reports), strings.Join(reports, "\n--\n"))
	}
	for hand := 1; hand <= 3; hand++ {
		want := fmt.Sprintf("hand %d says its piece", hand)
		if !anyContains(reports, want) {
			t.Errorf("no report carries %q:\n%s", want, strings.Join(reports, "\n--\n"))
		}
	}
}

// AND THEY ARRIVE IN THE ORDER THEY FINISHED, NOT THE ORDER THEY WERE ASKED FOR.
//
// That is the difference between a stream and a join said in one assertion. A
// caller reading its hands in declaration order is a caller that waited for the
// declaration to complete; a caller reading hand 3 first is a caller that was
// handed hand 3's slice the moment it existed.
func TestEachHandsReportArrivesWhenThatHandFinishes(t *testing.T) {
	completer := newForkCompleter(0)
	// Hand 3 answers at once; hand 2 takes a step first; hand 1 takes two. So
	// they come home 3, 2, 1 — the exact reverse of how they were declared.
	completer.caller = []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("call-1", "fork", forkCall(
				forkPartJSON("the slow one", "adapters"),
				forkPartJSON("the middling one", "docs"),
				forkPartJSON("the quick one", "fixtures"),
			)), nil
		},
	}
	completer.callerTail = keepWorking(completer, "stitched")
	steps := map[int]int{1: 3, 2: 2, 3: 0}
	completer.hand = func(index, turn int, _ []ai.Message) (*ai.Response, error) {
		if turn <= steps[index] {
			time.Sleep(15 * time.Millisecond)
			return toolResponse(fmt.Sprintf("h%d-%d", index, turn), "ls",
				fmt.Sprintf(`{"path":".","limit":%d}`, turn)), nil
		}
		return textResponse(fmt.Sprintf("hand %d is finished", index)), nil
	}

	agent, _ := newTestAgent(t, completer, nil)
	completer.drive(agent)
	collect(t, mustSubmit(t, agent, "split it"))
	handsAreHome(t, agent)

	reports := theHandReports(t, completer)
	if len(reports) != 3 {
		t.Fatalf("%d hand reports reached the caller, want 3:\n%s", len(reports), strings.Join(reports, "\n--\n"))
	}
	var order []int
	for _, report := range reports {
		var hand, of int
		if _, err := fmt.Sscanf(report, "hand %d of %d", &hand, &of); err != nil {
			t.Fatalf("a report does not open on which hand it is:\n%s", report)
		}
		order = append(order, hand)
	}
	if want := []int{3, 2, 1}; !sameOrder(order, want) {
		t.Fatalf("the reports landed in order %v, want %v — that is submission order, not completion order",
			order, want)
	}
}

// AND EVERY HAND STILL OUT IS AT THE FOOT OF EVERY RESULT THE CALLER READS.
//
// It is the shell's `[1]+ Running` and it is the reason the caller never has to
// poll: three facts, one line, on every result — which hand, how old, what it
// last did.
func TestTheRunningFooterListsTheHandsStillOut(t *testing.T) {
	completer := newForkCompleter(0)
	var (
		mu      sync.Mutex
		footers []string
		once    sync.Once
	)
	release := make(chan struct{})
	completer.caller = []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("call-1", "fork", forkCall(
				forkPartJSON("the adapters", "adapters"),
				forkPartJSON("the docs", "docs"),
			)), nil
		},
	}
	completer.callerTail = func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
		// What the caller was last answered with, footer and all.
		for at := len(messages) - 1; at >= 0; at-- {
			if strings.EqualFold(messages[at].Role, "tool") {
				mu.Lock()
				footers = append(footers, messageContentText(messages[at]))
				mu.Unlock()
				break
			}
		}
		mu.Lock()
		looked := len(footers)
		mu.Unlock()
		if looked < 8 {
			time.Sleep(2 * time.Millisecond)
			return toolResponse(fmt.Sprintf("look-%d", looked), "ls",
				fmt.Sprintf(`{"path":".","limit":%d}`, looked+1)), nil
		}
		once.Do(func() { close(release) })
		return textResponse("stitched"), nil
	}
	completer.hand = func(index, turn int, _ []ai.Message) (*ai.Response, error) {
		if turn == 1 {
			// Something for the footer to quote as this hand's last line.
			return toolResponse(fmt.Sprintf("h%d-look", index), "ls", `{"path":"."}`), nil
		}
		<-release
		return textResponse(fmt.Sprintf("hand %d done", index)), nil
	}

	agent, _ := newTestAgent(t, completer, nil)
	completer.drive(agent)
	collect(t, mustSubmit(t, agent, "split it"))
	handsAreHome(t, agent)

	// THE FOOTER IS WHAT THE STRIP TAKES OFF, and reading it that way is the
	// assertion that it IS a footer rather than prose that happens to name a
	// hand — the roster the fork call answered with names both hands too, and it
	// is a result body and must survive the strip untouched.
	mu.Lock()
	read := append([]string(nil), footers...)
	mu.Unlock()
	var footer string
	for _, result := range read {
		cut := strings.TrimPrefix(result, stripJobFooter(result))
		if strings.Contains(cut, "hand 1") && strings.Contains(cut, "hand 2") && strings.Contains(cut, "· last: ") {
			footer = cut
			break
		}
	}
	if footer == "" {
		t.Fatalf("no result the caller read carried both hands in its job footer:\n%s",
			strings.Join(read, "\n--\n"))
	}
	for _, want := range []string{jobFooterLead, jobFooterRunning, "hand 1 — the adapters", "hand 2 — the docs"} {
		if !strings.Contains(footer, want) {
			t.Errorf("the footer is missing %q:\n%s", want, footer)
		}
	}
}

// AND THE CALLER CAN WORK BETWEEN THEM. The measured defect was a mind that sat
// inside a tool call for thirty-nine minutes with a finished slice in front of
// it; this asserts the opposite in the plainest terms — real tool calls made,
// and answered, while the hands were out.
func TestTheCallerWorksWhileItsHandsAreOut(t *testing.T) {
	completer := newForkCompleter(0)
	release := make(chan struct{})
	var mu sync.Mutex
	worked := 0

	completer.caller = []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("call-1", "fork", forkCall(
				forkPartJSON("the adapters", "adapters"),
				forkPartJSON("the docs", "docs"),
			)), nil
		},
	}
	var once sync.Once
	completer.callerTail = func(context.Context, []ai.Message) (*ai.Response, error) {
		mu.Lock()
		worked++
		at := worked
		mu.Unlock()
		if at <= 4 {
			return toolResponse(fmt.Sprintf("build-%d", at), "bash",
				fmt.Sprintf(`{"command":"echo built %d","timeout":5}`, at)), nil
		}
		once.Do(func() { close(release) })
		return textResponse("stitched"), nil
	}
	// The hands do not come home until the caller has done four rounds of its
	// own, so the four rounds cannot be the hands' aftermath.
	completer.hand = func(index, _ int, _ []ai.Message) (*ai.Response, error) {
		<-release
		return textResponse(fmt.Sprintf("hand %d done", index)), nil
	}

	agent, _ := newTestAgent(t, completer, nil)
	completer.drive(agent)
	collect(t, mustSubmit(t, agent, "split it"))
	handsAreHome(t, agent)

	mu.Lock()
	did := worked
	mu.Unlock()
	if did < 5 {
		t.Fatalf("the caller only got %d requests in while its hands were out", did)
	}
	if !agentSaid(t, completer, "built 4") {
		t.Fatal("the caller's own bash never ran while its hands were out")
	}
}

// anyContains reports whether any of the blocks carries want.
func anyContains(blocks []string, want string) bool {
	for _, block := range blocks {
		if strings.Contains(block, want) {
			return true
		}
	}
	return false
}

func sameOrder(got, want []int) bool {
	if len(got) != len(want) {
		return false
	}
	for index := range got {
		if got[index] != want[index] {
			return false
		}
	}
	return true
}

// agentSaid reports whether any tool result the caller was ever answered with
// carries want.
func agentSaid(t *testing.T, completer *forkCompleter, want string) bool {
	t.Helper()
	completer.mu.Lock()
	defer completer.mu.Unlock()
	for _, request := range completer.callerRequests {
		for _, message := range request {
			if strings.EqualFold(message.Role, "tool") && strings.Contains(messageContentText(message), want) {
				return true
			}
		}
	}
	return false
}

// A HAND OPENS ON WHAT THE CALLER HAS ALREADY LEARNED. This is the whole
// economic argument, and the thing that would silently stop being true: what a
// hand is sent has to carry the caller's earlier TOOL RESULTS, not just its
// words.
func TestAHandIsSeededWithTheCallersOwnToolResults(t *testing.T) {
	completer := newForkCompleter(0)
	workspace := t.TempDir()
	const secret = "PELICAN-42"
	if err := os.WriteFile(filepath.Join(workspace, "note.txt"), []byte(secret+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	completer.caller = []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("read-1", "read", `{"path":"note.txt"}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponseWithText("call-1", "fork", forkCall(
				forkPartJSON("one", "a"),
				forkPartJSON("two", "b"),
			), "splitting this up"), nil
		},
	}
	completer.callerTail = keepWorking(completer, "done")
	completer.hand = func(index, _ int, _ []ai.Message) (*ai.Response, error) {
		return textResponse(fmt.Sprintf("hand %d saw it", index)), nil
	}

	agent, _ := newTestAgent(t, completer, func(config *Config) { config.Workspace = workspace })
	completer.drive(agent)
	collect(t, mustSubmit(t, agent, "read the note then split up"))

	for hand := 1; hand <= 2; hand++ {
		request := completer.handRequest(hand)
		if len(request) == 0 {
			t.Fatalf("hand %d never made a request", hand)
		}
		var carried bool
		for _, message := range request {
			if strings.Contains(messageContentText(message), secret) {
				carried = true
			}
		}
		if !carried {
			t.Fatalf("hand %d opened without the caller's tool result (%q); it was sent %v",
				hand, secret, rolesOf(request))
		}
		// AND THE CALLER'S OWN SENTENCE ABOUT WHAT IT WAS DOING SURVIVES, which
		// is the half [Agent.forkSeed] keeps when it strips the dangling call.
		if !strings.Contains(messageContentText(request[len(request)-2]), "splitting this up") &&
			!anyMessageContains(request, "splitting this up") {
			t.Errorf("hand %d lost the caller's last words", hand)
		}
		// AND NOTHING IS LEFT DANGLING. A request whose last assistant message
		// asks for a tool nothing answered is a shape providers refuse.
		for at, message := range request {
			if len(message.ToolCalls) == 0 {
				continue
			}
			if at+1 >= len(request) || !strings.EqualFold(request[at+1].Role, "tool") {
				t.Fatalf("hand %d was sent an unanswered tool call at message %d", hand, at)
			}
		}
	}
}

func anyMessageContains(messages []ai.Message, want string) bool {
	for _, message := range messages {
		if strings.Contains(messageContentText(message), want) {
			return true
		}
	}
	return false
}

// A HAND ASKS ON THE CALLER'S OWN CACHE LINEAGE. This is the verb's economy and
// it is invisible from everything else: a hand that minted a lineage of its own
// would still be correct, still pass every other test here, and would pay to
// write the caller's whole transcript cold once per hand — which is the one cost
// the fork exists not to pay.
func TestEveryHandAsksOnTheCallersCacheLineage(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	seed, system := agent.forkSeed()
	if agent.cacheKey == "" {
		t.Fatal("the caller has no lineage, so this test would pass on two empty strings")
	}
	for _, part := range []forkPart{{Role: "one", Scope: []string{"a"}}, {Role: "two", Scope: []string{"b"}}} {
		hand, err := agent.newHandAgent(part, seed, system, &handLeash{limit: forkRounds})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = hand.Close() })
		if hand.cacheKey != agent.cacheKey {
			t.Errorf("a hand asks on %q, the caller on %q", hand.cacheKey, agent.cacheKey)
		}
		wrapper, ok := hand.client.(sessionCompleter)
		if !ok {
			t.Fatalf("a hand's client is %T, so nothing stamps the key on its requests", hand.client)
		}
		if wrapper.cacheKey != agent.cacheKey {
			t.Errorf("the hand's requests carry %q, the caller's %q", wrapper.cacheKey, agent.cacheKey)
		}
	}
}

// ── the scope, which is the whole safety argument ───────────────────────────

// A HAND WRITING OUTSIDE ITS SLICE IS REFUSED, AND THE REFUSAL NAMES THE SLICE.
// It is a result it can act on rather than an error that ends its errand.
func TestAHandIsRefusedOutsideItsScopeAndIsToldWhatItOwns(t *testing.T) {
	completer := newForkCompleter(0)
	workspace := t.TempDir()

	completer.caller = []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("call-1", "fork", forkCall(
				forkPartJSON("the adapters", "adapters"),
				forkPartJSON("the docs", "docs"),
			)), nil
		},
	}
	completer.callerTail = keepWorking(completer, "done")
	completer.hand = func(index, turn int, _ []ai.Message) (*ai.Response, error) {
		if index != 1 {
			return textResponse("nothing to do"), nil
		}
		switch turn {
		case 1:
			// Hand 1 owns `adapters` and reaches into hand 2's `docs`.
			return toolResponse("out", "write", `{"path":"docs/stolen.md","content":"x"}`), nil
		case 2:
			// And then writes where it may.
			return toolResponse("in", "write", `{"path":"adapters/mine.md","content":"x"}`), nil
		}
		return textResponse("hand 1 stayed home"), nil
	}

	agent, _ := newTestAgent(t, completer, func(config *Config) { config.Workspace = workspace })
	completer.drive(agent)
	collect(t, mustSubmit(t, agent, "split it"))

	if _, err := os.Stat(filepath.Join(workspace, "docs", "stolen.md")); err == nil {
		t.Fatal("a hand wrote outside its scope")
	}
	if _, err := os.Stat(filepath.Join(workspace, "adapters", "mine.md")); err != nil {
		t.Fatalf("a hand could not write inside its own scope: %v", err)
	}

	// The refusal reaches the hand as a readable result naming what it owns.
	completer.mu.Lock()
	turns := completer.turns[1]
	completer.mu.Unlock()
	if turns < 2 {
		t.Fatalf("hand 1 stopped after %d requests, so it never read the refusal", turns)
	}

	handsAreHome(t, agent)
	reports := theHandReports(t, completer)
	if !anyContains(reports, "wrote adapters/mine.md") {
		t.Errorf("no report names what hand 1 actually wrote:\n%s", strings.Join(reports, "\n--\n"))
	}
	if anyContains(reports, "docs/stolen.md") {
		t.Errorf("a report counts a refused write as a change:\n%s", strings.Join(reports, "\n--\n"))
	}
}

// A SCOPE DECLARED IN ABSOLUTE PATHS IS THE SAME SCOPE, AND ITS WRITES LAND.
//
// This is the measured defect. A task worker forked three hands with scopes like
// "/workspace/rust-java-lsp/src/parser.rs"; the door only trimmed them, the guard
// compared them against a workspace-RELATIVE target, nothing matched, and every
// write in every hand was refused. The worker reported that the tool was refusing
// files that were plainly in its scope and never divided again.
func TestAnAbsoluteScopeUnderTheWorkspaceIsTheSameScope(t *testing.T) {
	completer := newForkCompleter(0)
	workspace := t.TempDir()

	completer.caller = []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("call-1", "fork", forkCall(
				// Spelled the way the worker in the wild spelled them: in full.
				forkPartJSON("the adapters", jsonPath(filepath.Join(workspace, "adapters"))),
				forkPartJSON("the docs", jsonPath(filepath.Join(workspace, "docs"))),
			)), nil
		},
	}
	completer.callerTail = keepWorking(completer, "done")
	completer.hand = func(index, turn int, _ []ai.Message) (*ai.Response, error) {
		if index != 1 || turn != 1 {
			return textResponse("nothing to do"), nil
		}
		// And the hand writes the way a hand writes: relative to the workspace.
		return toolResponse("in", "write", `{"path":"adapters/mine.md","content":"x"}`), nil
	}

	agent, _ := newTestAgent(t, completer, func(config *Config) { config.Workspace = workspace })
	completer.drive(agent)
	collect(t, mustSubmit(t, agent, "split it"))

	if _, err := os.Stat(filepath.Join(workspace, "adapters", "mine.md")); err != nil {
		t.Fatalf("a hand whose scope was declared in full could not write inside it: %v", err)
	}
	handsAreHome(t, agent)
	if reports := theHandReports(t, completer); !anyContains(reports, "wrote adapters/mine.md") {
		t.Errorf("no report names the write that landed:\n%s", strings.Join(reports, "\n--\n"))
	}
	// AND THE CHARGE SAYS THE SCOPE IN THE FORM THE GUARD READS IT. A hand told
	// it owns "/workspace/adapters" while the guard is matching "adapters" has
	// been handed the wrong half of the disagreement to reason about.
	charge := theHandCharge(t, completer, 1)
	if !strings.Contains(charge, "YOU MAY WRITE: adapters —") {
		t.Errorf("the hand was not told its scope in the enforced form:\n%s", charge)
	}
}

// theHandCharge is the one message a hand was handed that its caller never saw.
func theHandCharge(t *testing.T, completer *forkCompleter, hand int) string {
	t.Helper()
	request := completer.handRequest(hand)
	for index := len(request) - 1; index >= 0; index-- {
		text := messageContentText(request[index])
		if strings.HasPrefix(text, "You are hand ") {
			return text
		}
	}
	t.Fatalf("hand %d was never charged", hand)
	return ""
}

// jsonPath escapes a filesystem path for the scope literals above. Windows is
// not a target, but a temp directory with a backslash in it would otherwise
// produce a JSON document that does not parse and a test that fails for a reason
// that is not the one it is about.
func jsonPath(path string) string {
	return strings.ReplaceAll(path, `\`, `\\`)
}

// A SCOPE THAT IS NOT A SLICE OF THE WORKING COPY IS REFUSED AT THE DOOR, and
// the refusal names the offending scope and the form that would have worked.
//
// The door is where this belongs, not the guard: a scope outside the workspace
// matches no write at all, so a hand given one would spend its whole errand
// being refused one file at a time with no way to tell that the fault was in the
// declaration rather than in the file.
func TestAScopeOutsideTheWorkingCopyIsRefusedAtTheDoor(t *testing.T) {
	workspace := t.TempDir()
	for _, c := range []struct {
		name  string
		scope string
	}{
		{"somewhere else on the machine", "/etc"},
		{"an escape through the parent", "../secrets"},
		{"the whole working copy", "."},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, problem := parseForkArguments(workspace, json.RawMessage(forkCall(
				forkPartJSON("one", c.scope),
				forkPartJSON("two", "docs"),
			)))
			if problem == "" {
				t.Fatal("the scope was accepted")
			}
			if !strings.HasPrefix(problem, "not forked:") {
				t.Errorf("the refusal is not the plain answer the model reads: %q", problem)
			}
			if !strings.Contains(problem, `"`+c.scope+`"`) {
				t.Errorf("the refusal does not name the offending scope: %q", problem)
			}
			if !strings.Contains(problem, "relative to its root") || !strings.Contains(problem, workspace) {
				t.Errorf("the refusal does not say what form would have worked: %q", problem)
			}
		})
	}
}

// AND THE REFUSAL NEVER REACHES THE GUARD, because no hand is ever opened. The
// caller reads one result and redraws its scopes; nothing was spawned, nothing
// was written, and no pre-action citizen was asked about anything.
func TestARefusedScopeOpensNoHands(t *testing.T) {
	completer := newForkCompleter(0)
	workspace := t.TempDir()

	completer.caller = []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("call-1", "fork", forkCall(
				forkPartJSON("one", "/etc"),
				forkPartJSON("two", "docs"),
			)), nil
		},
	}
	completer.callerTail = keepWorking(completer, "done")
	completer.hand = func(int, int, []ai.Message) (*ai.Response, error) {
		return textResponse("a hand ran"), nil
	}

	agent, _ := newTestAgent(t, completer, func(config *Config) { config.Workspace = workspace })
	completer.drive(agent)
	collect(t, mustSubmit(t, agent, "split it"))

	for hand := 1; hand <= forkFanLimit; hand++ {
		if turns := completer.handTurns(hand); turns != 0 {
			t.Fatalf("hand %d ran %d turns on a scope the door refused", hand, turns)
		}
	}
	completer.mu.Lock()
	defer completer.mu.Unlock()
	var refusal string
	for _, request := range completer.callerRequests {
		for _, message := range request {
			if strings.EqualFold(message.Role, "tool") && strings.Contains(messageContentText(message), "not forked:") {
				refusal = messageContentText(message)
			}
		}
	}
	if refusal == "" {
		t.Fatal("the caller was never told why nothing was forked")
	}
	if strings.Contains(refusal, "outside your write scope") {
		t.Fatalf("the refusal came from the guard rather than the door:\n%s", refusal)
	}
}

// AND THE OVERLAP CHECK READS THE NORMALIZED SCOPES. Two hands that spelled one
// path two ways are two hands claiming one path, whatever they typed.
func TestOverlapIsJudgedAfterTheScopesAreNormalized(t *testing.T) {
	workspace := t.TempDir()
	_, problem := parseForkArguments(workspace, json.RawMessage(forkCall(
		forkPartJSON("one", "src/parser.rs"),
		forkPartJSON("two", jsonPath(filepath.Join(workspace, "src", "parser.rs"))),
	)))
	if problem == "" {
		t.Fatal("two hands claiming one path in two spellings were allowed")
	}
	if !strings.Contains(problem, "both claim src/parser.rs") {
		t.Errorf("the refusal does not name the shared path in one form: %q", problem)
	}
}

// AND THE ONE SHAPE THE GUARD CANNOT SAVE IS REFUSED BEFORE ANYTHING STARTS.
// Two hands sharing a path is not a wandering hand; it is two hands, both inside
// their scopes, writing one file in one working copy.
func TestOverlappingScopesAreRefusedAtTheCall(t *testing.T) {
	for _, c := range []struct {
		name  string
		parts []string
	}{
		{"the same file twice", []string{
			forkPartJSON("one", "internal/session/loop.go"),
			forkPartJSON("two", "internal/session/loop.go"),
		}},
		{"a directory over a file inside it", []string{
			forkPartJSON("one", "internal/session"),
			forkPartJSON("two", "internal/session/loop.go"),
		}},
		{"a file inside a directory somebody else took", []string{
			forkPartJSON("one", "docs/api.md"),
			forkPartJSON("two", "docs"),
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, problem := parseForkArguments("", json.RawMessage(forkCall(c.parts...)))
			if problem == "" {
				t.Fatal("the overlap was allowed")
			}
			if !strings.HasPrefix(problem, "not forked:") {
				t.Errorf("the refusal is not the plain answer the model reads: %q", problem)
			}
		})
	}

	// And genuinely separate slices are not refused, including two files that
	// merely start alike — a prefix is matched at a path boundary.
	if _, problem := parseForkArguments("", json.RawMessage(forkCall(
		forkPartJSON("one", "internal/session"),
		forkPartJSON("two", "internal/session2", "docs"),
	))); problem != "" {
		t.Fatalf("separate scopes were refused: %s", problem)
	}
}

// AND THE TWO ENDS OF THE FAN, WHICH ARE REFUSALS AND NOT ERRORS.
func TestForkRefusesAFanItCannotBe(t *testing.T) {
	for _, c := range []struct {
		name  string
		args  string
		wants string
	}{
		{"one hand", forkCall(forkPartJSON("alone", "a")), "not forked:"},
		{"five hands", forkCall(
			forkPartJSON("a", "a"), forkPartJSON("b", "b"), forkPartJSON("c", "c"),
			forkPartJSON("d", "d"), forkPartJSON("e", "e"),
		), "not forked:"},
		{"a hand with no scope", `{"parts":[{"role":"a","scope":[]},{"role":"b","scope":["b"]}]}`, "Invalid arguments:"},
		{"a hand with no role", `{"parts":[{"role":" ","scope":["a"]},{"role":"b","scope":["b"]}]}`, "Invalid arguments:"},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, problem := parseForkArguments("", json.RawMessage(c.args))
			if !strings.HasPrefix(problem, c.wants) {
				t.Fatalf("got %q, want a refusal opening %q", problem, c.wants)
			}
		})
	}
	if _, problem := parseForkArguments("", json.RawMessage(forkCall(
		forkPartJSON("a", "a"), forkPartJSON("b", "b"), forkPartJSON("c", "c"), forkPartJSON("d", "d"),
	))); problem != "" {
		t.Fatalf("a full fan of %d was refused: %s", forkFanLimit, problem)
	}
}

// ── the hand's belt ─────────────────────────────────────────────────────────

// A HAND HAS NO FORK VERB AND A BASH THAT CANNOT CHANGE ANYTHING. The first is
// the depth-one law as an ABSENCE, and the second is the hole every path gate
// has, closed rather than admitted.
func TestAHandsBeltCannotForkBuildOrWander(t *testing.T) {
	agent, workspace := newTestAgent(t, &scriptedCompleter{}, nil)
	leash := &handLeash{limit: forkRounds}
	seed, system := agent.forkSeed()
	hand, err := agent.newHandAgent(forkPart{Role: "one", Scope: []string{"a"}}, seed, system, leash)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = hand.Close() })

	byName := map[string]bare.Tool{}
	for _, tool := range hand.beltTools() {
		byName[tool.Name] = tool
	}
	for _, name := range []string{"read", "grep", "find", "ls", "edit", "write", "bash"} {
		if _, ok := byName[name]; !ok {
			t.Errorf("a hand has no %q, so it cannot do its part", name)
		}
	}
	for _, name := range []string{"fork", "propose_task", "divide_work", "watch", "jobs",
		"generate_image", "generate_video", "speak", "generate_music", "remember", "stand"} {
		if _, ok := byName[name]; ok {
			t.Errorf("a hand carries %q", name)
		}
	}

	// The bash orients and refuses everything else — a build and a test above
	// all, because the siblings are writing this same tree right now.
	for _, refused := range []string{
		"go build ./...", "go test ./...", "go vet ./...",
		"rm -rf .", "sed -i s/a/b/ x.go", "git status && rm -rf .", "cat x > y", "",
	} {
		arguments := json.RawMessage(`{"command":` + mustQuote(refused) + `}`)
		text, isError, err := byName["bash"].Execute(context.Background(), arguments)
		if err != nil {
			t.Fatalf("%q: the refusal was an error, not a result: %v", refused, err)
		}
		if !isError || !strings.HasPrefix(text, "refused:") {
			t.Fatalf("a hand ran %q: %q", refused, text)
		}
		if !strings.Contains(text, "a hand") {
			t.Errorf("the refusal of %q does not say who it is talking to:\n%s", refused, text)
		}
		if strings.Contains(text, "auditor") {
			t.Errorf("a hand was told it is an auditor:\n%s", refused)
		}
	}
	for _, allowed := range []string{"git status --porcelain", "git diff", "pwd", "cat go.mod", "head -n 5 x.go"} {
		if refusal, ok := refuseOutsideAllowlist(allowed, forkCommands, forkShell); !ok {
			t.Errorf("a hand may not run %q, which only reads: %s", allowed, refusal)
		}
	}
	_ = workspace
}

func mustQuote(text string) string {
	quoted, _ := json.Marshal(text)
	return string(quoted)
}

// AND THE AUDITOR'S OWN REFUSALS ARE UNCHANGED BY THE VOICE THE FORK ADDED.
// The gate is shared; the words are not, and the auditor's are the ones that
// were already right.
func TestTheSharedShellGateStillSpeaksInTheAuditorsVoice(t *testing.T) {
	for _, refused := range []string{"", "rm -rf .", "go test ./... && rm -rf ."} {
		text, ok := auditRefusal(refused, auditReadCommands)
		if ok {
			t.Fatalf("%q was allowed", refused)
		}
		if !strings.Contains(text, "an auditor") || !strings.Contains(text, "verification") {
			t.Fatalf("the auditor's refusal of %q lost its own words:\n%s", refused, text)
		}
	}
}

// ── the budget, the interrupt, and the price of a burst ─────────────────────

// A HAND THAT RUNS OUT OF ROUNDS REPORTS WHAT IT LEFT UNFINISHED, and the report
// LEADS with that fact.
//
// This is the second half of the measured failure. Hand 2 hit its cap at 01:11
// with the sentence "Now let me fix the file range end column" still on its lips
// — a change begun and not finished, in a file the caller was about to build.
// The report has to open on the bad news, name the files, and quote that
// sentence back verbatim, because that sentence is the only description in
// existence of what may be half made.
func TestAHandOutOfRoundsLeadsWithItAndQuotesWhatItWasDoing(t *testing.T) {
	const intent = "Now let me fix the file range end column"
	completer := newForkCompleter(0)
	workspace := t.TempDir()
	completer.caller = []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("call-1", "fork", forkCall(
				forkPartJSON("the grinder", "a"),
				forkPartJSON("the quick one", "b"),
			)), nil
		},
	}
	completer.callerTail = keepWorking(completer, "done")
	completer.hand = func(index, turn int, _ []ai.Message) (*ai.Response, error) {
		if index == 2 {
			return textResponse("hand 2 finished"), nil
		}
		// Hand 1 writes once, then narrates what it is about to do next and
		// grinds until its budget ends. Each call is DIFFERENT so that what
		// stops it is the budget and not the loop detector (looped.go), which
		// would make this test pass for the wrong reason.
		if turn == 1 {
			return toolResponse("grind-write", "write", `{"path":"a/parser.rs","content":"x"}`), nil
		}
		return toolResponseWithText(fmt.Sprintf("grind-%d", turn), "ls",
			fmt.Sprintf(`{"path":".","limit":%d}`, turn), intent), nil
	}

	agent, _ := newTestAgent(t, completer, func(config *Config) { config.Workspace = workspace })
	completer.drive(agent)
	collect(t, mustSubmit(t, agent, "split it"))
	handsAreHome(t, agent)

	// One request per round plus the one whose batch spends the last of them:
	// the leash cancels at the step boundary, so the turn ends before asking
	// again. A hand stopped by anything else would show far fewer.
	if turns := completer.handTurns(1); turns > forkRounds+1 || turns < forkRounds {
		t.Fatalf("the grinding hand made %d requests against a budget of %d rounds", turns, forkRounds)
	}

	reports := theHandReports(t, completer)
	if len(reports) != 2 {
		t.Fatalf("%d reports reached the caller, want 2:\n%s", len(reports), strings.Join(reports, "\n--\n"))
	}
	var unfinished string
	for _, report := range reports {
		if strings.Contains(report, "hand 1 of 2") {
			unfinished = report
		}
	}
	if unfinished == "" {
		t.Fatalf("the grinding hand sent no report at all:\n%s", strings.Join(reports, "\n--\n"))
	}
	// IT LEADS WITH IT. Not four lines down, where a caller reads a block as done.
	if !strings.HasPrefix(unfinished, strings.ToUpper(forkOutOf)) {
		t.Fatalf("the report does not lead with %q:\n%s", forkOutOf, unfinished)
	}
	// IT NAMES THE WRITES.
	if !strings.Contains(unfinished, "wrote a/parser.rs") {
		t.Errorf("the report does not name what the hand wrote:\n%s", unfinished)
	}
	// AND IT QUOTES THE LAST STATED INTENT, VERBATIM.
	if !strings.Contains(unfinished, "\u201c"+intent+"\u201d") {
		t.Errorf("the report does not quote the hand's last intent verbatim:\n%s", unfinished)
	}
	// AND IT SAYS WHAT THAT COSTS, in the vocabulary a landing already has for
	// work nothing looked at (withdrawn.go).
	for _, want := range []string{"UNVERIFIED", "half made", "This part is NOT done"} {
		if !strings.Contains(unfinished, want) {
			t.Errorf("the report does not say %q:\n%s", want, unfinished)
		}
	}
	// And the hand that DID finish was not lost with it.
	if !anyContains(reports, "hand 2 finished") {
		t.Errorf("the hand that finished was lost:\n%s", strings.Join(reports, "\n--\n"))
	}
}

// AND THE BUDGET IS COUNTED AT THE ROUND BOUNDARY, which is the same boundary
// the turn's own price is counted at (checkpoint.go). One batch is one round
// however many calls are in it.
func TestTheHandLeashCountsRoundsAndThenEndsTheTurn(t *testing.T) {
	stopped := false
	leash := &handLeash{limit: 3}
	leash.arm(func() { stopped = true })
	for round := 1; round <= 2; round++ {
		leash.PostFeedback(context.Background(), nil, nil, nil, nil, false)
		if leash.spent() || stopped {
			t.Fatalf("the leash fired after %d of %d rounds", round, leash.limit)
		}
	}
	leash.PostFeedback(context.Background(), nil, nil, nil, nil, false)
	if !leash.spent() || !stopped {
		t.Fatalf("the leash did not fire at its limit: spent=%v stopped=%v", leash.spent(), stopped)
	}
}

// THE FORK CALL ITSELF IS ONE ROUND AND IT IS A CHEAP ONE. The meter prices a
// turn in FINISHED TOOL ROUNDS ([checkpointMeter.round], counted once per batch
// at the step boundary), and `fork` is one call in one batch that returns in the
// time it takes to spawn — so the hands' own sixty rounds of work are not the
// caller's rounds, and the caller does not stop being able to spend its own.
func TestTheForkCallIsOneCheapRoundOfTheCallersOwnPrice(t *testing.T) {
	completer := newForkCompleter(0)
	release := make(chan struct{})
	completer.caller = []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("call-1", "fork", forkCall(
				forkPartJSON("one", "a"), forkPartJSON("two", "b"), forkPartJSON("three", "c"),
			)), nil
		},
		// THE SECOND REQUEST IS ASSEMBLED WHILE EVERY HAND IS STILL WORKING, and
		// this step is what proves it: the hands cannot finish until it runs.
		func(context.Context, []ai.Message) (*ai.Response, error) {
			close(release)
			return textResponse("stitched"), nil
		},
	}
	completer.callerTail = keepWorking(completer, "stitched")
	// Every hand takes three rounds of its own, so nine rounds of tool work
	// happen beside the caller's one.
	completer.hand = func(index, turn int, _ []ai.Message) (*ai.Response, error) {
		if turn <= 3 {
			return toolResponse(fmt.Sprintf("h%d-%d", index, turn), "ls",
				fmt.Sprintf(`{"path":".","limit":%d}`, turn)), nil
		}
		<-release
		return textResponse(fmt.Sprintf("hand %d done", index)), nil
	}

	agent, _ := newTestAgent(t, completer, nil)
	completer.drive(agent)
	collect(t, mustSubmit(t, agent, "split it"))
	handsAreHome(t, agent)

	completer.mu.Lock()
	handRequests := completer.turns[1] + completer.turns[2] + completer.turns[3]
	completer.mu.Unlock()
	if handRequests < 9 {
		t.Fatalf("the hands only made %d requests between them, so the test proved nothing", handRequests)
	}

	// And the meter itself, at the unit it counts in: one batch, one round.
	meter := &checkpointMeter{}
	if meter.round(); meter.rounds != 1 {
		t.Fatalf("one finished batch counted as %d rounds", meter.rounds)
	}
}

// A HAND OUTLIVES THE TURN, AND THE PERSON'S INTERRUPT IS WHAT ENDS IT.
//
// The two halves belong together. A hand is a stream now, so the turn ending is
// NOT what stops it — that is the whole point, and a hand bound to the turn's
// context would be dead one line after `fork` returned. What still stops it is
// the person: a background job is a command somebody asked to be left running,
// and a hand is this mind finishing a reply nobody is waiting for any more.
func TestTheCallersInterruptStopsItsHands(t *testing.T) {
	completer := newForkCompleter(0)

	var (
		mu      sync.Mutex
		reached int
	)
	completer.caller = []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("call-1", "fork", forkCall(
				forkPartJSON("one", "a"), forkPartJSON("two", "b"),
			)), nil
		},
	}
	completer.callerTail = keepWorking(completer, "done")
	completer.hand = func(index, turn int, _ []ai.Message) (*ai.Response, error) {
		mu.Lock()
		reached++
		mu.Unlock()
		// The person interrupts while the hands are working. The hand keeps
		// answering; what stops it is the interrupt reaching its context.
		completer.driven().Interrupt()
		time.Sleep(5 * time.Millisecond)
		return toolResponse(fmt.Sprintf("h%d-%d", index, turn), "ls",
			fmt.Sprintf(`{"path":".","limit":%d}`, turn)), nil
	}

	agent, _ := newTestAgent(t, completer, nil)
	completer.drive(agent)
	collect(t, mustSubmit(t, agent, "split it"))

	mu.Lock()
	saw := reached
	mu.Unlock()
	if saw == 0 {
		t.Fatal("no hand ever started, so the interrupt proved nothing")
	}
	// The hands are stopped rather than grinding to their round cap.
	handsAreHome(t, agent)
	if completer.handTurns(1) >= forkRounds {
		t.Fatalf("hand 1 made %d requests, so the interrupt did not reach it", completer.handTurns(1))
	}
	// And nothing is still running: a hand that survived would hold the agent
	// open.
	if err := agent.Close(); err != nil {
		t.Fatalf("closing after the interrupt: %v", err)
	}
}

// ── the line the person reads ───────────────────────────────────────────────

// PINNED AS AN EXACT STRING, exactly as the ceiling and split lines are: it is
// read on a turn nobody asked to be interrupted on, and the wording IS the
// feature.
func TestTheForkLineIsTheLineAndCarriesNoMachinery(t *testing.T) {
	const want = "three hands on it · each one folds in as it lands"
	if got := forkNote(3); got != want {
		t.Fatalf("the fork line reads %q, want %q", got, want)
	}
	for hands := forkHandFloor; hands <= forkFanLimit; hands++ {
		line := forkNote(hands)
		inTheHouseRegister(t, line)
		// AND IT COUNTS IN WORDS, because somebody reading a dim line reads it
		// the way they would say it.
		if strings.ContainsAny(line, "0123456789") {
			t.Errorf("the line has a numeral in it: %q", line)
		}
	}
	if forkNote(3) == checkpointSplitNote || forkNote(3) == checkpointCeilingNote {
		t.Error("the fork says what a handoff says, and they are opposite moves")
	}
}

// AND IT IS THE ONE THING THE PERSON GETS, said once, before the wait.
func TestThePersonIsToldOnceThatHandsAreOut(t *testing.T) {
	completer := newForkCompleter(0)
	completer.caller = []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("call-1", "fork", forkCall(
				forkPartJSON("one", "a"), forkPartJSON("two", "b"),
			)), nil
		},
	}
	completer.callerTail = keepWorking(completer, "done")
	completer.hand = func(index, _ int, _ []ai.Message) (*ai.Response, error) {
		return textResponse("done"), nil
	}

	agent, _ := newTestAgent(t, completer, nil)
	events := collect(t, mustSubmit(t, agent, "split it"))

	notices := 0
	for _, event := range events {
		if event.Kind == EventNotice && event.Text == forkNote(2) {
			notices++
		}
	}
	if notices != 1 {
		t.Fatalf("the person was told %d times that hands were out, want once", notices)
	}
}

// ── what a hand is told ─────────────────────────────────────────────────────

// THE ONE LINE A HAND GETS THAT THE TRANSCRIPT CANNOT CARRY: what it owns, and
// what its siblings own. Naming the neighbours is the inhibition half of this
// design and is the reason all three do not draw the same next step out of the
// same words.
func TestTheChargeNamesThePartTheScopeAndTheSiblings(t *testing.T) {
	parsed := forkArguments{
		Parts: []forkPart{
			{Role: "the adapters", Scope: []string{"adapters"}},
			{Role: "the docs", Scope: []string{"docs", "README.md"}},
			{Role: "the fixtures", Scope: []string{"testdata"}},
		},
		Note: "the release is friday",
	}
	charge := forkCharge(1, parsed)

	for _, want := range []string{
		"You are hand 2 of 3",
		"YOUR PART: the docs",
		"YOU MAY WRITE: docs, README.md",
		"hand 1 — the adapters (adapters)",
		"hand 3 — the fixtures (testdata)",
		"DO NOT REDO THEIR WORK",
		"FOR EVERY HAND: the release is friday",
		fmt.Sprintf("You have %d tool rounds", forkRounds),
	} {
		if !strings.Contains(charge, want) {
			t.Errorf("the charge does not say %q:\n%s", want, charge)
		}
	}
	// It must NOT name itself among the siblings, which would be a hand told to
	// stay out of its own files.
	if strings.Contains(charge, "hand 2 — the docs") {
		t.Errorf("the charge lists the hand among its own siblings:\n%s", charge)
	}
	// And a fork with no note says nothing about one.
	if strings.Contains(forkCharge(0, forkArguments{Parts: parsed.Parts}), "FOR EVERY HAND") {
		t.Error("an empty note still wrote a heading, which is the emptiness law")
	}
}

// ── what happens to a hand still out when the work ends ─────────────────────

// THE LANDING WAITS FOR A HAND, exactly as it waits for a part.
//
// A hand is a stream now, so a node CAN reach the end of its turn with hands
// still working — and the question the old barrier never had to answer is what
// the landing does about it. It does what this session already does with every
// other piece of work it handed out and has not heard about: it parks. The node
// does not land, the reports arrive, the model reads them, and THEN it lands.
//
// The alternative — landing on top of a hand — would throw away the very writes
// the fork was for, which is the failure this whole change is about, made worse.
func TestANodeDoesNotLandWhileAHandIsStillOut(t *testing.T) {
	completer := newForkCompleter(0)
	release := make(chan struct{})
	completer.caller = []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("call-1", "fork", forkCall(
				forkPartJSON("the adapters", "adapters"),
				forkPartJSON("the docs", "docs"),
			)), nil
		},
		// AND THE NODE STOPS TALKING WHILE THEY ARE STILL OUT. This is the shape
		// the wait exists for: nothing left to say, two hands still working.
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("the hands are out; nothing more from me until they report"), nil
		},
	}
	// The turn that reads their reports is the one the last report starts.
	completer.callerTail = func(context.Context, []ai.Message) (*ai.Response, error) {
		return textResponse("folded both slices into one"), nil
	}
	completer.hand = func(index, _ int, _ []ai.Message) (*ai.Response, error) {
		<-release
		return textResponse(fmt.Sprintf("hand %d done", index)), nil
	}

	nest := newNest(t, completer, nil)
	completer.drive(nest.node)
	done, _ := runParent(t, nest, taskLimits{maxSteps: 200, noProgress: 20})

	// The node's first turn has ended and the node has NOT landed.
	select {
	case <-done:
		t.Fatal("the node landed while its hands were still out")
	case <-time.After(400 * time.Millisecond):
	}
	if !nest.node.childrenOutstanding() {
		t.Fatal("a hand still out does not count as outstanding work, so nothing is holding the landing")
	}

	close(release)
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the node never landed after its hands reported")
	}

	// AND THE REPORTS REACHED THE MODEL rather than merely reaching the queue. A
	// landing held open for reports nobody read would be the wait without the
	// point of it.
	reports := theHandReports(t, completer)
	if len(reports) != 2 {
		t.Fatalf("%d hand reports reached the node, want 2:\n%s", len(reports), strings.Join(reports, "\n--\n"))
	}
	for hand := 1; hand <= 2; hand++ {
		if !anyContains(reports, fmt.Sprintf("hand %d done", hand)) {
			t.Errorf("hand %d's own words never reached the node:\n%s", hand, strings.Join(reports, "\n--\n"))
		}
	}
}
