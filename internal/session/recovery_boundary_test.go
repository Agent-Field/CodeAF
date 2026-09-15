package session

import (
	"context"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/provider"
	"github.com/Agent-Field/codeaf/internal/roles"
	"github.com/Agent-Field/codeaf/internal/taxonomy"
)

// ── THE TURN THAT ENDED ON A ROUTING REFUSAL (2026-09-10 19:32 EDT) ─────────
//
// The router's 404 — "0 endpoints out of 1 requested are available matching
// your guardrail restrictions and data policy" — names no upstream, and this
// boundary read every 4xx that named nobody as the router reading our own bytes.
// So the transcript said `class: work, reason: the request itself was refused`
// and the turn ended with the router's sentence on the screen, when a list and
// an account setting had emptied the endpoint set and every other machine
// behind the model could have answered. The person re-sent the same text by
// hand and it worked.

// measuredPolicyRefusal is the router's own 404 as it reached the session: the
// live sentence, and the one fact about it that the transport decided at its
// refusal door — that a list or a policy emptied the set.
func measuredPolicyRefusal() *provider.APIError {
	return &provider.APIError{
		Status: 404,
		Message: "0 endpoints out of 1 requested are available matching your guardrail restrictions and data policy. " +
			"We removed them for the following reasons (an endpoint may have matched multiple reasons):\n" +
			"Paid model training violation (account settings): 1 endpoint excluded; configurable at https://openrouter.ai/settings/privacy",
		Routing: true,
	}
}

// A2, THE SESSION HALF: A ROUTING REFUSAL IS THE WIRE AND THE TURN LANDS.
func TestARoutingRefusalIsAskedAgainAndTheTurnLands(t *testing.T) {
	var calls atomic.Int64
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			calls.Add(1)
			return nil, measuredPolicyRefusal()
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			calls.Add(1)
			return textResponse("early names are emerging"), nil
		},
	}}
	path := filepath.Join(t.TempDir(), "session.jsonl")
	agent := checkpointAgent(t, completer, func(config *Config) { config.SessionFile = path })

	events, err := agent.Submit(context.Background(), "what eems like early names emerging")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	seen := collect(t, events)
	if got := calls.Load(); got != 2 {
		t.Fatalf("the model was asked %d times, want the refused request asked again once", got)
	}
	for _, event := range seen {
		if event.Kind == EventError {
			t.Fatalf("the turn ended in an error the person reads: %v", event.Err)
		}
	}
	failures := journaledEntries(t, path, "failure")
	if len(failures) == 0 {
		t.Fatal("the refused call left no failure row")
	}
	for _, entry := range failures {
		if entry.Failure == nil {
			continue
		}
		if entry.Failure.Class == string(taxonomy.Work) {
			t.Fatalf("a routing refusal was journaled as %q/%q, want the wire", entry.Failure.Class, entry.Failure.Reason)
		}
	}
}

// A2, THE EVIDENCE: the boundary reads the typed fact, never the sentence. The
// same status and the same words WITHOUT the transport's mark are still the
// router reading our own bytes, and a spent ladder's diagnosis is still an
// answer with nowhere left to go.
func TestTheBoundaryReadsARoutingRefusalAsTheWire(t *testing.T) {
	limits := taxonomy.Limits{}.Floored()
	if verdict := taxonomy.Classify(wireEvidence(measuredPolicyRefusal(), 1), limits); verdict.Class != taxonomy.Transport || !verdict.Retries() {
		t.Fatalf("a routing refusal read as %s, want transport and a retry", verdict)
	}
	// AND THE UNMARKED ONE IS THE REQUEST'S OWN SHAPE. It used to be read as
	// [taxonomy.Work] — the same word a job that could not be done gets — which
	// is what let a turn end saying "the request itself was refused" about
	// somebody's conversation. Our own bytes are a SHAPE: nothing was learned
	// about the work, nothing about the model, and the move is a different
	// request. The distinction is what a person reads in the failure row.
	unmarked := measuredPolicyRefusal()
	unmarked.Routing = false
	verdict := taxonomy.Classify(wireEvidence(unmarked, 1), limits)
	if verdict.Class != taxonomy.Shape || verdict.Action != taxonomy.ActionReshape {
		t.Fatalf("an unmarked 404 read as %s, want the request's own shape", verdict)
	}
	if !verdict.EndsTurn() {
		t.Fatalf("our own bytes did not end the request: %s", verdict)
	}
	// AND A SPENT LADDER IS A BUDGET THAT IS GONE, NOT A JUDGEMENT.
	//
	// It read as [taxonomy.Work] until 2026-09-10 — the same word a job that
	// could not be done gets — so a turn ended with the router's own sentence on
	// screen while a chain the person had configured sat unasked. Nothing about
	// the work was learned here: what was established is that no SHAPE of this
	// request will be served, which leaves exactly one move, and the move belongs
	// to the caller ([taxonomy.Evidence.Spent]).
	spent := &provider.RefusalError{Model: "m", Refusal: measuredPolicyRefusal(), Attempts: 3}
	evidence := wireEvidence(spent, 1)
	if !evidence.Spent {
		t.Fatal("a spent ladder did not say so on its evidence")
	}
	evidence.FallbackAvailable = true
	if verdict := taxonomy.Classify(evidence, limits); verdict.Class != taxonomy.Transport || !verdict.Hops() {
		t.Fatalf("a spent ladder read as %s, want the wire and the next model", verdict)
	}
	// AND WITH NOWHERE LEFT TO GO IT ENDS THE REQUEST — never the tier, and never
	// as a finding about the work.
	evidence.FallbackAvailable = false
	ended := taxonomy.Classify(evidence, limits)
	if ended.Class != taxonomy.Transport || ended.Action != taxonomy.ActionGiveUp {
		t.Fatalf("a spent ladder with no chain read as %s, want giving up on the request", ended)
	}
}

// A5: A CANCELLED ERRAND IS NOT A FAILURE.
//
// When a turn ends its side errands die with `context canceled`, and each one
// was journaled as `class: work, reason: the work did not come back done` — a
// failure row in every autopsy for a caption nobody was waiting for any more.
// The turn loop never journals a request its own context cancelled; an errand
// now keeps the same rule.
func TestACancelledErrandWritesNoFailure(t *testing.T) {
	ladder := &errandLadder{script: map[string][]errandScript{
		"tier/one": {{answer: "a caption", after: 5 * time.Second}},
	}}
	path := filepath.Join(t.TempDir(), "session.jsonl")
	agent, _ := newTestAgent(t, ladder, func(config *Config) {
		config.SessionFile = path
		config.RolesSource = tierSettings(map[string]string{
			string(roles.TierKey(roles.TierLow)): "tier/one",
		})
	})
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel)
	if _, _, err := agent.callRole(ctx, roles.RoleTitle, "session/model", nil); err == nil {
		t.Fatal("a cancelled errand answered")
	}
	for _, entry := range journaledEntries(t, path, "failure") {
		if entry.Failure != nil {
			t.Fatalf("a cancelled errand was journaled as a failure: %+v", *entry.Failure)
		}
	}
	for _, entry := range journaledEntries(t, path, "error") {
		if entry.Error != nil && strings.Contains(entry.Error.Message, "canceled") {
			t.Fatalf("a cancelled errand was journaled as an error: %+v", *entry.Error)
		}
	}
}
