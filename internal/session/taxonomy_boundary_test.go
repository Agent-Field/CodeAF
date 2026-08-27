package session

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/roles"
	"github.com/Agent-Field/aforge-v2/internal/taxonomy"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// ── THE FAILURES THAT WERE READ AS THE WRONG THING ──────────────────────────
//
// Every test here is one of the three misreadings the response boundary was
// written for, driven through the real machinery: the wire read as the model,
// the wire read as the end of a turn, and the wire read as something to wait
// forever for. The evidence is a five-run comparison in which the three runs
// that happened to roll four bad responses in a row spent $9.50–15.80 each, with
// 57–82% of it on a model bought to answer for somebody else's bad minute, while
// the run that never rolled four cost $2.33 in total.

// malformedUpstream is the refusal the measured runs actually drew: a 400 the
// ROUTER glossed as "Provider returned error", with the upstream named and the
// upstream's own body saying what it really was — a tool call whose arguments
// did not survive the stream. Four of them arrived in a row from four different
// endpoints serving the same model.
func malformedUpstream(upstream string) error {
	return refusalOf(400, "Provider returned error", upstream,
		`{"error":{"message":"function.arguments must be valid JSON: unterminated string"}}`)
}

// journaledFailures reads the boundary's own lines off a journal.
func journaledFailures(t *testing.T, path string) []journalFailure {
	t.Helper()
	var rows []journalFailure
	for _, entry := range journaledEntries(t, path, "failure") {
		if entry.Failure != nil {
			rows = append(rows, *entry.Failure)
		}
	}
	return rows
}

// (a) FOUR MALFORMED 400s ARE FOUR TRANSPORT RETRIES AND NOT A PURCHASE.
//
// This is the shape that started it. The tier must not move, the endpoint is the
// only thing the retry changes, and every one of the four is on the file as
// transport — so a bench can count them and nothing downstream can read them as
// evidence about the model.
func TestFourMalformedRefusalsAreTransportAndTheTierNeverMoves(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	var calls atomic.Int64
	upstreams := []string{"Together", "DeepInfra", "Fireworks", "Novita"}
	completer := &scriptedCompleter{steps: repeatedStep(6, func(context.Context, []ai.Message) (*ai.Response, error) {
		n := calls.Add(1)
		return nil, malformedUpstream(upstreams[(int(n)-1)%len(upstreams)])
	})}
	agent := checkpointAgent(t, completer, func(config *Config) { config.SessionFile = path })

	events, err := agent.Submit(context.Background(), "port the language server")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collect(t, events)

	if got := calls.Load(); got != int64(taxonomy.DefaultTransportAttempts) {
		t.Fatalf("the request was sent %d times, want the transport ladder's %d",
			got, taxonomy.DefaultTransportAttempts)
	}
	if got := agent.Model(); got != "test/model" {
		t.Fatalf("the turn ended on %q; a transport failure moved the tier", got)
	}
	rows := journaledFailures(t, path)
	if len(rows) != taxonomy.DefaultTransportAttempts {
		t.Fatalf("%d classification lines for %d attempts: %+v",
			len(rows), taxonomy.DefaultTransportAttempts, rows)
	}
	for i, row := range rows {
		if row.Class != string(taxonomy.Transport) {
			t.Fatalf("attempt %d was classified %q; nothing about who SERVED a request is evidence about who was asked",
				i+1, row.Class)
		}
		if row.Status != 400 {
			t.Errorf("attempt %d lost the status off its line", i+1)
		}
		if row.Provider == "" {
			t.Errorf("attempt %d lost the upstream off its line", i+1)
		}
	}
	// THE FIRST THREE ASK AGAIN AND THE LAST GIVES UP ON THE REQUEST — never on
	// the tier, which is the distinction the whole mechanism turns on.
	for i, row := range rows[:len(rows)-1] {
		if row.Action != string(taxonomy.ActionRetry) {
			t.Errorf("attempt %d did %q, want a retry", i+1, row.Action)
		}
	}
	if last := rows[len(rows)-1]; last.Action != string(taxonomy.ActionGiveUp) {
		t.Errorf("the spent ladder did %q, want the request given up on", last.Action)
	}
}

// AND THE RETRY IS ROTATED RATHER THAN WAITED OUT. An endpoint that answers at
// once with a mangled tool call is up and fast and broken; eight seconds of
// patience buys exactly the same request. The clock is the honest witness.
func TestAMangledToolCallIsRotatedRatherThanWaitedOut(t *testing.T) {
	verdict := taxonomy.Classify(taxonomy.Evidence{
		Status: 400, Upstream: "Together", Malformed: true, Attempt: 1,
	}, taxonomy.Limits{})
	if !verdict.Retries() {
		t.Fatalf("a mangled tool call did %q, want a retry", verdict.Action)
	}
	if verdict.Backoff != 0 {
		t.Errorf("backoff = %s, want none: the endpoint answered at once and waiting mends nothing", verdict.Backoff)
	}
	if !verdict.Rotate {
		t.Error("the retry was not asked to be served by somebody else, which is the only thing that helps")
	}
}

// (b) AN EMPTY 200 IS RETRIED AND THE TURN CARRIES ON.
//
// It used to be written down once and the loop stopped, which on one measured
// run ended the whole thing eighteen minutes in with hours of budget unspent.
func TestAnEmptyReplyIsRetriedAndTheTurnCarriesOn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) { return emptyResponse(), nil },
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return finishedResponse("the parser is ported and the suite is green", "stop"), nil
		},
	}}
	agent := checkpointAgent(t, completer, func(config *Config) { config.SessionFile = path })

	events, err := agent.Submit(context.Background(), "port the language server")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collect(t, events)

	var said bool
	for _, message := range agent.snapshot() {
		if message.Role == "assistant" && strings.Contains(messageText(message), "the suite is green") {
			said = true
		}
	}
	if !said {
		t.Fatal("an empty 200 ended the turn: the answer that came back on the retry is nowhere in the transcript")
	}
	rows := journaledFailures(t, path)
	if len(rows) != 1 {
		t.Fatalf("%d classification lines for one empty reply: %+v", len(rows), rows)
	}
	if rows[0].Class != string(taxonomy.Transport) || rows[0].Action != string(taxonomy.ActionRetry) {
		t.Fatalf("an empty reply was read as %s/%s, want transport and a retry", rows[0].Class, rows[0].Action)
	}
	// AND IT NEVER GOT INTO THE TRANSCRIPT AS SOMETHING THE MODEL SAID.
	for _, message := range agent.snapshot() {
		if message.Role == "assistant" && strings.TrimSpace(messageText(message)) == "" && len(message.ToolCalls) == 0 {
			t.Fatal("the empty reply was recorded as an assistant message")
		}
	}
}

// AND A TRANSPORT VERDICT IS NEVER ALLOWED TO END A TURN, stated once and read
// directly, so the rule survives a rewrite of the loop that reads it.
func TestATransportVerdictNeverEndsATurn(t *testing.T) {
	for _, evidence := range []taxonomy.Evidence{
		{Empty: true, Attempt: 1},
		{Empty: true, Attempt: 99},
		{Timeout: true, Attempt: 99},
		{Status: 503, Attempt: 99},
	} {
		verdict := taxonomy.Classify(evidence, taxonomy.Limits{})
		if verdict.EndsTurn() {
			t.Fatalf("%+v was allowed to end a turn as %s", evidence, verdict)
		}
	}
}

// ── the gate ────────────────────────────────────────────────────────────────

// boundaryGateAgent is a session with a crew, one node, and a journal to read the
// boundary's lines off.
func boundaryGateAgent(t *testing.T, tiers map[string]string) (*Agent, *TaskNode, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "session.jsonl")
	agent, _ := newTestAgent(t, &routedCompleter{}, func(config *Config) {
		config.SessionFile = path
		if tiers != nil {
			config.RolesSource = tierSettings(tiers)
		}
	})
	graph := &TaskGraph{nodes: map[uint64]*TaskNode{}}
	node := &TaskNode{graph: graph, id: 1,
		spec: taskSpec{title: "the greeting", model: "vendor/flash"}, state: TaskRunning}
	graph.nodes[1] = node
	return agent, node, path
}

// (c) TWO FINDINGS ON THE SAME TIER BUY ONE LIFT, AND ONE FINDING BUYS NOTHING.
//
// K is a knob and this pins the whole ladder against it: the first finding
// holds, the second lifts, and both are on the file as capability — which is the
// class that is ALLOWED to cost money.
func TestFindingsBuyOneLiftAfterTheCountIsReached(t *testing.T) {
	t.Setenv("AFORGE_RESPONSE_LIFT_AFTER", "2")
	agent, node, path := boundaryGateAgent(t, map[string]string{roles.TierKey(roles.TierHigh): carefulTier})

	first := agent.readFinding(node, io.Discard)
	if first.Class != taxonomy.Capability {
		t.Fatalf("the first finding was read as %s, want capability", first.Class)
	}
	if first.Escalates() {
		t.Fatalf("the first of two findings bought a tier: %s", first)
	}
	if got := agent.repairTierModel(node, first); got != "vendor/flash" {
		t.Fatalf("the held round runs on %q, want the model the work is already on", got)
	}

	second := agent.readFinding(node, io.Discard)
	if !second.Escalates() {
		t.Fatalf("the second finding on the same tier bought nothing: %s", second)
	}
	if got := agent.repairTierModel(node, second); got != carefulTier {
		t.Fatalf("the lifted round runs on %q, want the careful tier's %q", got, carefulTier)
	}

	rows := journaledFailures(t, path)
	if len(rows) != 2 {
		t.Fatalf("%d classification lines for two findings: %+v", len(rows), rows)
	}
	for i, row := range rows {
		if row.Class != string(taxonomy.Capability) {
			t.Errorf("finding %d was classified %q, want capability", i+1, row.Class)
		}
	}
	if rows[1].Action != string(taxonomy.ActionEscalate) {
		t.Errorf("the lift was journaled as %q", rows[1].Action)
	}
}

// AND A FINDING WITH DEAD CALLS UNDER IT BUYS NOTHING, EVER.
//
// This is the whole mechanism in one test. The round lost its calls on the wire,
// so there was nothing for the check to pass; reading its finding as evidence
// about the model is what put 57–82% of three runs' bills on a model seven times
// the price.
func TestAFindingWithDeadCallsUnderItBuysNothing(t *testing.T) {
	agent, node, path := boundaryGateAgent(t, map[string]string{roles.TierKey(roles.TierHigh): carefulTier})
	tally := agent.tallyFor(node)
	for i := 0; i < taxonomy.DefaultTransportAttempts; i++ {
		tally.Wire()
	}

	held := agent.readFinding(node, io.Discard)
	if held.Escalates() {
		t.Fatalf("a finding about a round that never ran bought a tier: %s", held)
	}
	if got := agent.repairTierModel(node, held); got != "vendor/flash" {
		t.Fatalf("the held round runs on %q, want the model the work is already on", got)
	}
	if _, semantic, tainted := tally.Counts(); semantic != 0 || tainted != 1 {
		t.Fatalf("semantic = %d, tainted = %d; want the finding filed against the wire", semantic, tainted)
	}
	if rows := journaledFailures(t, path); len(rows) != 1 || rows[0].Action != string(taxonomy.ActionHold) {
		t.Fatalf("the hold was not journaled as one: %+v", rows)
	}
}

// (d) AND THE LIFT IS HANDED BACK WHEN THE NEXT CHECK PASSES.
//
// Nothing in this build looked for that moment before, which is why every lift
// it ever bought was permanent.
func TestAPassHandsTheLiftBack(t *testing.T) {
	agent, node, path := boundaryGateAgent(t, map[string]string{roles.TierKey(roles.TierHigh): carefulTier})
	lift := agent.readFinding(node, io.Discard)
	if got := agent.repairTierModel(node, lift); got != carefulTier {
		t.Fatalf("the round runs on %q, want the careful tier", got)
	}
	if !agent.tallyFor(node).Escalated() {
		t.Fatal("the lift was bought and not recorded")
	}

	pass := agent.readPass(node, io.Discard)
	if pass.Action != taxonomy.ActionDeescalate {
		t.Fatalf("a check that passed on a lifted tier did %q, want the tier handed back", pass.Action)
	}
	if agent.tallyFor(node).Escalated() {
		t.Fatal("the tier was not actually handed back")
	}
	// AND THE NEXT FINDING STARTS FROM THE BOTTOM AGAIN.
	if got := agent.repairTierModel(node, taxonomy.Verdict{Action: taxonomy.ActionHold}); got != "vendor/flash" {
		t.Fatalf("after the hand-back the round runs on %q, want the model the work is on", got)
	}
	rows := journaledFailures(t, path)
	if len(rows) != 2 || rows[1].Action != string(taxonomy.ActionDeescalate) {
		t.Fatalf("the hand-back is not on the file: %+v", rows)
	}
}

// (e) AND THE LIFTED TIER HAS A CEILING, PAST WHICH THE ANSWER IS THE WORK'S.
func TestASpentCapStopsBuyingAndReturnsTheWork(t *testing.T) {
	t.Setenv("AFORGE_RESPONSE_LIFT_CAP", "1.50")
	agent, node, path := boundaryGateAgent(t, map[string]string{roles.TierKey(roles.TierHigh): carefulTier})

	lift := agent.readFinding(node, io.Discard)
	if got := agent.repairTierModel(node, lift); got != carefulTier {
		t.Fatalf("the round runs on %q, want the careful tier", got)
	}
	agent.tallyFor(node).Spend(1.75)

	spent := agent.readFinding(node, io.Discard)
	if spent.Class != taxonomy.Work {
		t.Fatalf("a spent cap answered %s, want the work handed back", spent)
	}
	if spent.Action != taxonomy.ActionReport {
		t.Fatalf("a spent cap did %q, want the verdict reported and nothing bought", spent.Action)
	}
	rows := journaledFailures(t, path)
	if len(rows) != 2 || rows[1].Class != string(taxonomy.Work) {
		t.Fatalf("the ceiling is not on the file: %+v", rows)
	}
	if rows[1].SpentUSD < 1.75 {
		t.Errorf("spentUsd = %v, want what the lifted tier actually took", rows[1].SpentUSD)
	}
}

// AND A RUN THAT DIED ON THE WIRE DOES NOT MOVE THE NODE'S MODEL.
//
// task_run.go used to move it on any provider failure at all: a new worker, a
// new journal, the next rung of the fallback chain, and no road back down.
func TestARunThatDiedOnTheWireDoesNotMoveTheNode(t *testing.T) {
	agent, node, path := boundaryGateAgent(t, nil)
	if agent.movesForFailure(node, malformedUpstream("Together"), io.Discard) {
		t.Fatal("four bad responses moved a whole task onto a dearer model")
	}
	// A refusal the ROUTER made on its own account is our own bytes being read
	// and rejected: no endpoint and no model will fix it, so it is the work's.
	if !agent.movesForFailure(node, refusalOf(400, "no endpoints found that support tool use", "", ""), io.Discard) {
		t.Fatal("a request no endpoint will ever serve was held on the same model")
	}
	rows := journaledFailures(t, path)
	if len(rows) != 2 {
		t.Fatalf("%d classification lines for two run failures: %+v", len(rows), rows)
	}
	if rows[0].Class != string(taxonomy.Transport) || rows[1].Class != string(taxonomy.Work) {
		t.Fatalf("classes = %q, %q; want transport then work", rows[0].Class, rows[1].Class)
	}
}

// (f) AN ERRAND'S DEADLINE IS RETRIED ONCE, THEN DROPPED — AND WRITTEN DOWN.
//
// Nobody typed the call and nobody is waiting on it, so dropping it is right.
// What was wrong was dropping it SILENTLY: the measured version left a worker
// starting blind with no error row, no call row and no word of why.
func TestAnErrandsDeadlineIsRetriedOnceThenDroppedWithItsClassOnTheFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	timedOut := func(context.Context, []ai.Message) (*ai.Response, error) {
		return nil, fmt.Errorf("read response: %w", context.DeadlineExceeded)
	}
	completer := &scriptedCompleter{steps: repeatedStep(4, timedOut)}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.SessionFile = path
		config.RolesSource = tierSettings(map[string]string{roles.TierKey(roles.TierLow): "cheap/model"})
	})

	_, _, err := agent.callRole(context.Background(), roles.RoleTaskName, "test/model",
		[]ai.Message{textMessage("user", "name this")})
	if err == nil {
		t.Fatal("an errand nobody could answer came back as a success")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("the caller was handed %v, want the deadline it can read", err)
	}
	// ONE RUNG BELOW THE RESOLVED ONE IS THE WHOLE OF AN ERRAND'S PATIENCE.
	if got := completer.requests(); got != 2 {
		t.Fatalf("the errand made %d requests, want the rung and exactly one below it", got)
	}
	rows := journaledFailures(t, path)
	if len(rows) != 2 {
		t.Fatalf("%d classification lines for two rungs: %+v", len(rows), rows)
	}
	for i, row := range rows {
		if row.Class != string(taxonomy.Transport) {
			t.Errorf("rung %d was classified %q, want transport", i+1, row.Class)
		}
		if row.Role != string(roles.RoleTaskName) {
			t.Errorf("rung %d lost the errand's name off its line: %q", i+1, row.Role)
		}
	}
}

// AND THE DEADLINE IS RECOGNISED WHETHER IT ARRIVES AS AN ERROR OR AS WORDS.
// The adapter re-wraps its body read as a sentence, and no errors.Is can see
// through that.
func TestADeadlineIsTransportHoweverItArrives(t *testing.T) {
	for _, err := range []error{
		fmt.Errorf("read response: %w", context.DeadlineExceeded),
		errors.New("read response: context deadline exceeded"),
	} {
		evidence := wireEvidence(err, 1)
		if !evidence.Timeout {
			t.Fatalf("%v was not read as a deadline", err)
		}
		if got := taxonomy.Classify(evidence, taxonomy.Limits{}); got.Class != taxonomy.Transport {
			t.Fatalf("%v classified as %s", err, got.Class)
		}
	}
	// AND A SHAPE WITH NO STATUS AND NO DEADLINE IN IT IS STILL THE WIRE, read
	// through the one pattern this build has always kept for that ([isRetryable]).
	if evidence := wireEvidence(errors.New("socket hang up"), 1); !evidence.Wire {
		t.Fatal("a socket that hung up was not read as the connection")
	}
	// AND A TRANSCRIPT THAT OUTGREW ITS WINDOW IS NOT. The answer to that is a
	// shorter conversation, which the turn loop already tries; another endpoint
	// is a guess dressed as a rescue.
	if evidence := wireEvidence(errors.New("this model's maximum context length is 128000 tokens"), 1); evidence.Wire {
		t.Fatal("a context overflow was read as the connection")
	}
}
