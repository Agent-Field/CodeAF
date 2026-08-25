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
	// callerSeen counts the requests that were NOT a hand's.
	callerSeen int
	// callerRequests keeps them, so a test can read what the caller was sent.
	callerRequests [][]ai.Message

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
		if at < len(c.caller) {
			next = c.caller[at]
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

// theForkResult is the fork call's own tool result, read off the caller's LAST
// request — the one assembled after the burst joined.
func theForkResult(t *testing.T, completer *forkCompleter) string {
	t.Helper()
	completer.mu.Lock()
	defer completer.mu.Unlock()
	for at := len(completer.callerRequests) - 1; at >= 0; at-- {
		for _, message := range completer.callerRequests[at] {
			if strings.EqualFold(message.Role, "tool") {
				if text := messageContentText(message); strings.Contains(text, "hands, all back") {
					return text
				}
			}
		}
	}
	t.Fatalf("no fork result reached the caller; it made %d requests", len(completer.callerRequests))
	return ""
}

// ── the fork itself ─────────────────────────────────────────────────────────

// THREE HANDS, THREE SLICES, AT THE SAME TIME — and the caller gets one block
// per hand, in the order it declared them and never in the order they finished.
func TestForkRunsItsHandsAtOnceAndReportsThemInPartOrder(t *testing.T) {
	completer := newForkCompleter(3)
	completer.caller = []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("call-1", "fork", forkCall(
				forkPartJSON("the adapters", "adapters"),
				forkPartJSON("the docs", "docs"),
				forkPartJSON("the fixtures", "fixtures"),
			)), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("stitched"), nil
		},
	}
	// Hand 3 answers immediately and hands 1 and 2 take a step first, so the
	// order they FINISH in is not the order they were declared in.
	completer.hand = func(index, turn int, _ []ai.Message) (*ai.Response, error) {
		if index != 3 && turn == 1 {
			return toolResponse(fmt.Sprintf("h%d-ls", index), "ls", `{"path":"."}`), nil
		}
		return textResponse(fmt.Sprintf("hand %d says its piece", index)), nil
	}

	agent, _ := newTestAgent(t, completer, nil)
	collect(t, mustSubmit(t, agent, "do the wide thing"))

	completer.mu.Lock()
	peak := completer.peak
	completer.mu.Unlock()
	if peak < 3 {
		t.Fatalf("at most %d hands were ever in flight at once, so they ran in sequence", peak)
	}

	result := theForkResult(t, completer)
	if !strings.HasPrefix(result, "three hands, all back:") {
		t.Fatalf("the join does not open on the count:\n%s", result)
	}
	for hand, role := range []string{"the adapters", "the docs", "the fixtures"} {
		want := fmt.Sprintf("hand %d — %s", hand+1, role)
		if !strings.Contains(result, want) {
			t.Fatalf("no block for %q:\n%s", want, result)
		}
	}
	// Declared order, whatever order they came home in.
	first := strings.Index(result, "hand 1 —")
	second := strings.Index(result, "hand 2 —")
	third := strings.Index(result, "hand 3 —")
	if !(first < second && second < third) {
		t.Fatalf("the blocks are not in part order:\n%s", result)
	}
	for hand := 1; hand <= 3; hand++ {
		if !strings.Contains(result, fmt.Sprintf("hand %d says its piece", hand)) {
			t.Errorf("hand %d's own words did not reach the caller:\n%s", hand, result)
		}
	}
	if strings.Count(result, "\n  "+forkDone) != 3 {
		t.Errorf("not every hand came back done:\n%s", result)
	}
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
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("done"), nil },
	}
	completer.hand = func(index, _ int, _ []ai.Message) (*ai.Response, error) {
		return textResponse(fmt.Sprintf("hand %d saw it", index)), nil
	}

	agent, _ := newTestAgent(t, completer, func(config *Config) { config.Workspace = workspace })
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
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("done"), nil },
	}
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

	result := theForkResult(t, completer)
	if !strings.Contains(result, "wrote adapters/mine.md") {
		t.Errorf("the join does not name what hand 1 actually wrote:\n%s", result)
	}
	if strings.Contains(result, "docs/stolen.md") {
		t.Errorf("the join counts a refused write as a change:\n%s", result)
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
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("done"), nil },
	}
	completer.hand = func(index, turn int, _ []ai.Message) (*ai.Response, error) {
		if index != 1 || turn != 1 {
			return textResponse("nothing to do"), nil
		}
		// And the hand writes the way a hand writes: relative to the workspace.
		return toolResponse("in", "write", `{"path":"adapters/mine.md","content":"x"}`), nil
	}

	agent, _ := newTestAgent(t, completer, func(config *Config) { config.Workspace = workspace })
	collect(t, mustSubmit(t, agent, "split it"))

	if _, err := os.Stat(filepath.Join(workspace, "adapters", "mine.md")); err != nil {
		t.Fatalf("a hand whose scope was declared in full could not write inside it: %v", err)
	}
	result := theForkResult(t, completer)
	if !strings.Contains(result, "wrote adapters/mine.md") {
		t.Errorf("the join does not name the write that landed:\n%s", result)
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
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("done"), nil },
	}
	completer.hand = func(int, int, []ai.Message) (*ai.Response, error) {
		return textResponse("a hand ran"), nil
	}

	agent, _ := newTestAgent(t, completer, func(config *Config) { config.Workspace = workspace })
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
		text, ok := auditRefusal(refused, auditCommands)
		if ok {
			t.Fatalf("%q was allowed", refused)
		}
		if !strings.Contains(text, "an auditor") || !strings.Contains(text, "verification") {
			t.Fatalf("the auditor's refusal of %q lost its own words:\n%s", refused, text)
		}
	}
}

// ── the budget, the interrupt, and the price of a burst ─────────────────────

// A HAND THAT NEVER STOPS IS STOPPED, and the block says so rather than reading
// as finished — the caller has to know the part was left half-done.
func TestAHandOutOfRoundsComesBackSayingSo(t *testing.T) {
	completer := newForkCompleter(0)
	completer.caller = []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("call-1", "fork", forkCall(
				forkPartJSON("the grinder", "a"),
				forkPartJSON("the quick one", "b"),
			)), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("done"), nil },
	}
	completer.hand = func(index, turn int, _ []ai.Message) (*ai.Response, error) {
		if index == 2 {
			return textResponse("hand 2 finished"), nil
		}
		// Hand 1 never answers in words. Each call is DIFFERENT so that what
		// stops it is the budget and not the loop detector (looped.go), which
		// would make this test pass for the wrong reason.
		return toolResponse(fmt.Sprintf("grind-%d", turn), "ls",
			fmt.Sprintf(`{"path":".","limit":%d}`, turn)), nil
	}

	agent, _ := newTestAgent(t, completer, nil)
	collect(t, mustSubmit(t, agent, "split it"))

	// One request per round plus the one whose batch spends the last of them:
	// the leash cancels at the step boundary, so the turn ends before asking
	// again. A hand stopped by anything else would show far fewer.
	if turns := completer.handTurns(1); turns > forkRounds+1 || turns < forkRounds {
		t.Fatalf("the grinding hand made %d requests against a budget of %d rounds", turns, forkRounds)
	}
	result := theForkResult(t, completer)
	if !strings.Contains(result, forkOutOf) {
		t.Fatalf("the grinding hand did not come back %q:\n%s", forkOutOf, result)
	}
	if !strings.Contains(result, "hand 2 finished") {
		t.Errorf("the hand that DID finish was lost with it:\n%s", result)
	}
	// AND THE CALLER IS TOLD WHAT THAT MEANS, because a block it reads as done
	// is a part nobody finishes.
	if !strings.Contains(result, "out of rounds left its part unfinished") {
		t.Errorf("the join does not say what an out-of-rounds hand costs:\n%s", result)
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
		leash.PostFeedback(context.Background(), nil, nil, nil, nil)
		if leash.spent() || stopped {
			t.Fatalf("the leash fired after %d of %d rounds", round, leash.limit)
		}
	}
	leash.PostFeedback(context.Background(), nil, nil, nil, nil)
	if !leash.spent() || !stopped {
		t.Fatalf("the leash did not fire at its limit: spent=%v stopped=%v", leash.spent(), stopped)
	}
}

// A BURST IS ONE ROUND OF THE PERSON'S WAITING. The meter prices a turn in
// FINISHED TOOL ROUNDS ([checkpointMeter.round], counted once per batch at the
// step boundary), and a fork is one call in one batch however much work happens
// inside it — which is right, because the meter measures the person's wait and a
// burst is one wait.
func TestAForkBurstIsOneRoundOfTheCallersOwnPrice(t *testing.T) {
	completer := newForkCompleter(0)
	completer.caller = []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("call-1", "fork", forkCall(
				forkPartJSON("one", "a"), forkPartJSON("two", "b"), forkPartJSON("three", "c"),
			)), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("stitched"), nil },
	}
	// Every hand takes three rounds of its own, so nine rounds of tool work
	// happen inside the caller's one.
	completer.hand = func(index, turn int, _ []ai.Message) (*ai.Response, error) {
		if turn <= 3 {
			return toolResponse(fmt.Sprintf("h%d-%d", index, turn), "ls",
				fmt.Sprintf(`{"path":".","limit":%d}`, turn)), nil
		}
		return textResponse(fmt.Sprintf("hand %d done", index)), nil
	}

	agent, _ := newTestAgent(t, completer, nil)
	collect(t, mustSubmit(t, agent, "split it"))

	completer.mu.Lock()
	callerRequests := completer.callerSeen
	handRequests := completer.turns[1] + completer.turns[2] + completer.turns[3]
	completer.mu.Unlock()

	// Two requests: the one that asked for the fork, and the one that read the
	// join. Exactly one finished tool round, which is one mark on the meter.
	if callerRequests != 2 {
		t.Fatalf("the caller made %d requests, so the burst cost it more than one round", callerRequests)
	}
	if handRequests < 9 {
		t.Fatalf("the hands only made %d requests between them, so the test proved nothing", handRequests)
	}

	// And the meter itself, at the unit it counts in: one batch, one round.
	meter := &checkpointMeter{}
	if meter.round(); meter.rounds != 1 {
		t.Fatalf("one finished batch counted as %d rounds", meter.rounds)
	}
}

// NO HAND OUTLIVES THE TURN THAT OPENED IT. The nursery law, and it costs
// nothing to keep because the hands run on the tool's own context.
func TestTheCallersInterruptKillsItsHands(t *testing.T) {
	completer := newForkCompleter(0)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

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
	completer.hand = func(index, turn int, _ []ai.Message) (*ai.Response, error) {
		mu.Lock()
		reached++
		mu.Unlock()
		// The person interrupts while the hands are working.
		cancel()
		return textResponse("never read"), nil
	}

	agent, _ := newTestAgent(t, completer, nil)
	events, err := agent.Submit(ctx, "split it")
	if err != nil {
		t.Fatal(err)
	}
	collect(t, events)

	mu.Lock()
	saw := reached
	mu.Unlock()
	if saw == 0 {
		t.Fatal("no hand ever started, so the interrupt proved nothing")
	}
	// The turn is over and nothing is still running. A hand that outlived it
	// would still be holding the agent open.
	if err := agent.Close(); err != nil {
		t.Fatalf("closing after the interrupt: %v", err)
	}
}

// ── the line the person reads ───────────────────────────────────────────────

// PINNED AS AN EXACT STRING, exactly as the ceiling and split lines are: it is
// read on a turn nobody asked to be interrupted on, and the wording IS the
// feature.
func TestTheForkLineIsTheLineAndCarriesNoMachinery(t *testing.T) {
	const want = "three hands on it · back when they are done"
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
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("done"), nil },
	}
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
