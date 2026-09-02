package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// strictHandler answers a disable the way MiniMax M2.7's endpoint does, and
// answers anything else normally. It is the whole fixture: the adapter's job is
// to be told no once and never send that shape to this model again.
func strictHandler(recorded *capture) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		recorded.record(request)
		body := recorded.body(recorded.count() - 1)
		reasoning, _ := body["reasoning"].(map[string]any)
		if enabled, present := reasoning["enabled"].(bool); present && !enabled {
			writer.Header().Set("Content-Type", "application/json")
			writer.WriteHeader(http.StatusBadRequest)
			_, _ = writer.Write([]byte(`{"error":{"message":"Reasoning is mandatory for this endpoint and cannot be disabled.","code":400}}`))
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"model":"strict/model","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"ok"}}]}`))
	})
}

func newStrictClient(t *testing.T, model string) (*Client, *capture) {
	t.Helper()
	forgetLanes(t)
	recorded := &capture{}
	client, err := NewClient(Config{
		APIKey: "test-key", BaseURL: "http://provider.test", Model: model,
		HTTPClient:        handlerClient(strictHandler(recorded)),
		SupportsParameter: func(string, string) (bool, bool) { return true, true },
	})
	if err != nil {
		t.Fatal(err)
	}
	return client, recorded
}

func TestAdapterRecoversWhenAnEndpointRefusesToDisableReasoning(t *testing.T) {
	// The failure this repairs: every planning call on MiniMax M2.7 died on a
	// 400, three seconds in, because the harness's own economy asked a model
	// that always thinks to stop thinking.
	quirksAt(t, "strict/always-reasons")
	client, recorded := newStrictClient(t, "strict/always-reasons")
	ctx := WithConfiguredReasoningEffort(context.Background(), EffortOff)
	if _, err := client.CompleteWithMessages(ctx, userMessages("plan this")); err != nil {
		t.Fatalf("a refused disable must be repaired, not returned: %v", err)
	}
	if recorded.count() != 2 {
		t.Fatalf("sent %d requests, want the rejected one and its repair", recorded.count())
	}
	if reasoning, _ := recorded.body(0)["reasoning"].(map[string]any); reasoning["enabled"] != false {
		t.Fatalf("first request = %#v, want the disable that gets refused", recorded.body(0))
	}
	// The repair sends the lowest effort word, never the disable again and
	// never NOTHING — nothing leaves the model at its published default, which
	// on GLM 5.3 is the top of its ladder (thinking.go).
	if reasoning, _ := recorded.body(1)["reasoning"].(map[string]any); reasoning["effort"] != "low" || reasoning["enabled"] != nil {
		t.Fatalf("repaired request reasoning = %#v, want effort low and no disable", recorded.body(1)["reasoning"])
	}

	// Learned, not re-learned: the next call knows better before it is sent.
	if _, err := client.CompleteWithMessages(ctx, userMessages("plan the next thing")); err != nil {
		t.Fatal(err)
	}
	if recorded.count() != 3 {
		t.Fatalf("sent %d requests in total, want one more — the second call must not be refused again", recorded.count())
	}
	if reasoning, _ := recorded.body(2)["reasoning"].(map[string]any); reasoning["effort"] != "low" || reasoning["enabled"] != nil {
		t.Fatalf("second call reasoning = %#v, want the lowest effort without being told twice", recorded.body(2)["reasoning"])
	}
	if !ReasoningMandatory("~strict/always-reasons") {
		t.Fatal("the fact must be readable by a surface, and by the slug however it is written")
	}
}

func TestAdapterKeepsAnUnrelated400AsTheErrorItIs(t *testing.T) {
	quirksAt(t, "strict/other")
	recorded := &capture{}
	handler := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		recorded.record(request)
		writer.WriteHeader(http.StatusBadRequest)
		_, _ = writer.Write([]byte(`{"error":{"message":"context length exceeded by 12 tokens"}}`))
	})
	client, err := NewClient(Config{
		APIKey: "test-key", BaseURL: "http://provider.test", Model: "strict/other",
		HTTPClient:        handlerClient(handler),
		SupportsParameter: func(string, string) (bool, bool) { return true, true },
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := WithConfiguredReasoningEffort(context.Background(), EffortOff)
	_, err = client.CompleteWithMessages(ctx, userMessages("plan this"))
	if err == nil {
		t.Fatal("an unrelated 400 must surface as an error")
	}
	// The peeked body is put back whole: the caller's error still says what the
	// provider actually complained about.
	if !strings.Contains(err.Error(), "context length exceeded by 12 tokens") {
		t.Fatalf("error = %v, want the provider's own words", err)
	}
	if recorded.count() != 1 {
		t.Fatalf("sent %d requests, want no retry for a 400 that is ours", recorded.count())
	}
	if ReasoningMandatory("strict/other") {
		t.Fatal("an unrelated 400 must not teach a reasoning quirk")
	}
}

func TestLearnedQuirksSurviveTheProcessThatLearnedThem(t *testing.T) {
	dir := quirksAt(t, "strict/persisted")
	client, _ := newStrictClient(t, "strict/persisted")
	ctx := WithConfiguredReasoningEffort(context.Background(), EffortOff)
	if _, err := client.CompleteWithMessages(ctx, userMessages("plan this")); err != nil {
		t.Fatal(err)
	}
	// The write is off the request path, so it is completed here rather than
	// waited on — the assertion is about the file's contents, not its timing.
	quirks.save()

	raw, err := os.ReadFile(filepath.Join(dir, quirksFile))
	if err != nil {
		t.Fatalf("nothing was written down: %v", err)
	}
	var wire quirksWire
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatalf("memo is not readable: %v", err)
	}
	if _, known := wire.ReasoningMandatory["strict/persisted"]; !known {
		t.Fatalf("memo = %s, want the model that refused", raw)
	}

	// A fresh process reads it and never sends the refused shape at all.
	quirks.mandatory = map[string]time.Time{}
	LoadQuirks(dir)
	if !ReasoningMandatory("strict/persisted") {
		t.Fatal("a memo on disk must be believed at startup")
	}
	client, recorded := newStrictClient(t, "strict/persisted")
	if _, err := client.CompleteWithMessages(ctx, userMessages("plan this")); err != nil {
		t.Fatal(err)
	}
	if recorded.count() != 1 {
		t.Fatalf("sent %d requests, want one — the refusal was already known", recorded.count())
	}
}

func TestASilentlyIgnoredDisableSurvivesTheProcessThatLearnedIt(t *testing.T) {
	const model = "silent/always-thinks"
	dir := quirksAt(t, model)
	NoteReasoningDisableIgnored(model)
	quirks.save()

	raw, err := os.ReadFile(filepath.Join(dir, quirksFile))
	if err != nil {
		t.Fatalf("nothing was written down: %v", err)
	}
	var wire quirksWire
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatalf("memo is not readable: %v", err)
	}
	if _, known := wire.ReasoningDisableIgnored[model]; !known {
		t.Fatalf("memo = %s, want the model that ignored the disable", raw)
	}

	quirks.mutex.Lock()
	quirks.disableIgnored = map[string]time.Time{}
	quirks.mutex.Unlock()
	LoadQuirks(dir)
	if !ReasoningDisableIgnored(model) || !ReasoningUnavoidable(model) {
		t.Fatal("a memo on disk must restore the room this model needs")
	}
}

// quirksAt points the memo at a temporary profile for one test and forgets what
// that test learned on the way out. The memo is process-wide by design, and a
// test that left its model in it would be teaching every later test.
func quirksAt(t *testing.T, models ...string) string {
	t.Helper()
	dir := t.TempDir()
	LoadQuirks(dir)
	t.Cleanup(func() {
		// Before anything else: the memo's write is scheduled, not performed, so
		// a test that learned a fact may still have a writer inside the temp dir
		// t.TempDir is about to remove. Waiting here is what makes the removal —
		// and therefore the test — deterministic rather than load-dependent.
		quirks.settle()
		quirks.mutex.Lock()
		defer quirks.mutex.Unlock()
		for _, model := range models {
			delete(quirks.mandatory, normalizeModel(model))
			delete(quirks.disableIgnored, normalizeModel(model))
			// EVERY memo, because they are all process-wide and a fact left
			// behind by one test silently changes the request shape of the next.
			delete(quirks.noCacheControl, normalizeModel(model))
			delete(quirks.noReasoningBudget, normalizeModel(model))
			delete(quirks.noReasoningReplay, normalizeModel(model))
		}
		quirks.path = ""
	})
	return dir
}

// count is how many requests reached the handler, read under the same lock the
// handler writes them with.
func (c *capture) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.bodies)
}

// A model that cannot stop thinking, met cold — no row in the catalog, so the
// adapter learns from the 400. The repair does not send NOTHING (that leaves
// the model at its published default, which for GLM 5.3 is "max", and a
// ten-thousand-token pass then eats the answer): it sends the lowest effort
// word, and a ceiling with that pass's documented share in front of the answer.
func TestAnAlwaysThinkingModelIsSentItsLowestEffortAndRoomForIt(t *testing.T) {
	quirksAt(t, "strict/always-reasons")
	client, recorded := newStrictClient(t, "strict/always-reasons")
	ctx := WithConfiguredReasoningEffort(context.Background(), EffortOff)
	if _, err := client.CompleteWithMessages(ctx, userMessages("plan this"), ai.WithMaxTokens(1000)); err != nil {
		t.Fatal(err)
	}
	if recorded.count() != 2 {
		t.Fatalf("sent %d requests, want the rejected one and its repair", recorded.count())
	}
	if got := recorded.body(0)["max_tokens"]; got != float64(1000) {
		t.Fatalf("first request max_tokens = %v, want the caller's own 1000", got)
	}
	repaired := recorded.body(1)
	if reasoning, _ := repaired["reasoning"].(map[string]any); reasoning["effort"] != "low" {
		t.Fatalf("repaired request reasoning = %#v, want the lowest effort word, not nothing", repaired["reasoning"])
	}
	// low is a fifth of the ceiling, so 1000 tokens of answer need 1250.
	if got := repaired["max_tokens"]; got != float64(1250) {
		t.Fatalf("repaired request max_tokens = %v, want 1250", got)
	}
}

// The same model with its row KNOWN: the catalog says mandatory and lists the
// words, so there is no 400 to learn from — the first request is already the
// lowest listed effort, and the disable never travels.
func TestACatalogThatSaysMandatorySendsTheLowestListedEffortFromTheFirstCall(t *testing.T) {
	quirksAt(t, "listed/always-reasons")
	recorded := &capture{}
	client, err := NewClient(Config{
		APIKey: "test-key", BaseURL: "http://provider.test", Model: "listed/always-reasons",
		HTTPClient:        handlerClient(strictHandler(recorded)),
		SupportsParameter: func(string, string) (bool, bool) { return true, true },
		ReasoningProfile: func(string) (ReasoningProfile, bool) {
			return ReasoningProfile{Mandatory: true, Efforts: []Effort{"max", "high", "low"}, Default: "max"}, true
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := WithConfiguredReasoningEffort(context.Background(), EffortOff)
	if _, err := client.CompleteWithMessages(ctx, userMessages("plan this"), ai.WithMaxTokens(1000)); err != nil {
		t.Fatal(err)
	}
	if recorded.count() != 1 {
		t.Fatalf("sent %d requests, want exactly one: the row already said no disable", recorded.count())
	}
	first := recorded.body(0)
	if reasoning, _ := first["reasoning"].(map[string]any); reasoning["effort"] != "low" || reasoning["enabled"] != nil {
		t.Fatalf("first request reasoning = %#v, want effort low and no disable", first["reasoning"])
	}
	if got := first["max_tokens"]; got != float64(1250) {
		t.Fatalf("first request max_tokens = %v, want 1250", got)
	}
}

// The ceiling follows the router's documented allocation, and the transport
// waits for the ceiling that travels rather than the caller's figure.
func TestTheWireCeilingFollowsTheDocumentedAllocationAndTheTransportWaitsForIt(t *testing.T) {
	quirksAt(t, "silent/thinker")
	client, _ := newTestClient(t, Config{Model: "sim/model", ReasoningProfile: func(model string) (ReasoningProfile, bool) {
		if model == "silent/thinker" {
			return ReasoningProfile{Mandatory: true, Default: "max"}, true
		}
		return ReasoningProfile{}, false
	}})
	cases := []struct {
		name         string
		model        string
		sent         Effort
		budget, want int
	}{
		{"no pass, the caller's figure", "sim/model", EffortNone, 0, 1000},
		{"low is a fifth", "sim/model", EffortLow, 0, 1250},
		{"medium is half", "sim/model", EffortMedium, 0, 2000},
		{"high is four fifths", "sim/model", EffortHigh, 0, 5000},
		{"a budget is exact", "sim/model", EffortHigh, 3000, 4000},
		{"nothing sent to a model that thinks at max regardless", "silent/thinker", EffortNone, 0, 20000},
	}
	for _, tc := range cases {
		if got := client.wireCeiling(tc.model, tc.sent, tc.budget, 1000); got != tc.want {
			t.Errorf("%s: wireCeiling = %d, want %d", tc.name, got, tc.want)
		}
	}
	// One source for the wait: the transport's timeout is derived from the
	// ceiling the encoder sends, so a permitted reply can never outrun it.
	answer := 10273
	request := &ai.Request{Model: "silent/thinker", MaxTokens: &answer}
	ceiling, ok := client.ceilingFor(request, callKnobs{})
	if !ok || ceiling <= answer {
		t.Fatalf("ceilingFor = %d, %v; want more than the caller's %d on a model that thinks regardless", ceiling, ok, answer)
	}
	client.velocity = newVelocityLedger()
	if got, want := client.clientFor("silent/thinker", false, ceiling).Timeout, adaptiveCompletionTimeout(ceiling, 0); got != want {
		t.Fatalf("transport timeout = %v, want %v, sized from the ceiling that travels", got, want)
	}
	if client.clientFor("silent/thinker", false, ceiling).Timeout <= client.clientFor("silent/thinker", false, answer).Timeout && ceiling/64 > 300 {
		t.Fatal("the wait did not grow with the room")
	}
}

// ignoringHandler is the other way the same fact shows: the endpoint takes the
// disable without complaint, thinks anyway, and returns nothing once the whole
// ceiling is spent. Only a ceiling with the room in it gets an answer.
func ignoringHandler(recorded *capture, needs int) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		recorded.record(request)
		body := recorded.body(recorded.count() - 1)
		ceiling, _ := body["max_tokens"].(float64)
		reasoning, _ := body["reasoning"].(map[string]any)
		writer.Header().Set("Content-Type", "application/json")
		// It thinks at its default until told a level; told one, it answers.
		if int(ceiling) < needs && reasoning["effort"] == nil {
			_, _ = writer.Write([]byte(`{"model":"quiet/model","choices":[{"index":0,"finish_reason":"length","message":{"role":"assistant","content":""}}],"usage":{"prompt_tokens":10,"completion_tokens":` + strconv.Itoa(int(ceiling)) + `}}`))
			return
		}
		_, _ = writer.Write([]byte(`{"model":"quiet/model","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"ok"}}],"usage":{"prompt_tokens":10,"completion_tokens":2}}`))
	})
}

func TestAnEmptyAnswerAtTheCeilingTeachesTheAdapterToLeaveRoom(t *testing.T) {
	quirksAt(t, "quiet/thinks-anyway")
	recorded := &capture{}
	client, err := NewClient(Config{
		APIKey: "test-key", BaseURL: "http://provider.test", Model: "quiet/thinks-anyway",
		HTTPClient:        handlerClient(ignoringHandler(recorded, 5000)),
		SupportsParameter: func(string, string) (bool, bool) { return true, true },
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := WithConfiguredReasoningEffort(context.Background(), EffortOff)
	response, err := client.CompleteWithMessages(ctx, userMessages("plan this"), ai.WithMaxTokens(1000))
	if err != nil {
		t.Fatalf("an empty answer at the ceiling must be retried with room, not returned: %v", err)
	}
	if got := response.Text(); got != "ok" {
		t.Fatalf("answer = %q, want the one the room bought", got)
	}
	if recorded.count() != 2 {
		t.Fatalf("sent %d requests, want the empty one and its retry", recorded.count())
	}
	retried := recorded.body(1)
	if reasoning, _ := retried["reasoning"].(map[string]any); reasoning["effort"] != "low" {
		t.Fatalf("retry reasoning = %#v, want the lowest effort word", retried["reasoning"])
	}
	if got := retried["max_tokens"]; got != float64(1250) {
		t.Fatalf("retry max_tokens = %v, want 1250", got)
	}
	if !ReasoningDisableIgnored("quiet/thinks-anyway") {
		t.Fatal("the fact must be remembered, so the next call is sent with room the first time")
	}

	// Once. A second call is shaped right from the start and sent exactly once.
	if _, err := client.CompleteWithMessages(ctx, userMessages("plan the next thing"), ai.WithMaxTokens(1000)); err != nil {
		t.Fatal(err)
	}
	if recorded.count() != 3 {
		t.Fatalf("sent %d requests in total, want the learned call to go out once", recorded.count())
	}
}

// The memo is forever, so it is written only on evidence: an empty answer with
// no usage block, or one cut while calling a tool, teaches nothing.
func TestAnEmptyAnswerWithoutEvidenceTeachesNothing(t *testing.T) {
	quirksAt(t, "quiet/no-evidence")
	for name, payload := range map[string]string{
		"no usage":  `{"model":"m","choices":[{"index":0,"finish_reason":"length","message":{"role":"assistant","content":""}}]}`,
		"tool call": `{"model":"m","choices":[{"index":0,"finish_reason":"length","message":{"role":"assistant","content":"","tool_calls":[{"id":"c","type":"function","function":{"name":"read","arguments":"{}"}}]}}],"usage":{"prompt_tokens":1,"completion_tokens":1000}}`,
	} {
		recorded := &capture{}
		client, err := NewClient(Config{APIKey: "k", BaseURL: "http://provider.test", Model: "quiet/no-evidence",
			HTTPClient: handlerClient(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				recorded.record(request)
				writer.Header().Set("Content-Type", "application/json")
				_, _ = writer.Write([]byte(payload))
			}))})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := client.CompleteWithMessages(context.Background(), userMessages("go"), ai.WithMaxTokens(1000)); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if recorded.count() != 1 {
			t.Fatalf("%s: sent %d requests, want one — nothing to learn, nothing to resend", name, recorded.count())
		}
		if ReasoningDisableIgnored("quiet/no-evidence") {
			t.Fatalf("%s: the memo was written on a guess", name)
		}
	}
}

// A PASS NOBODY ASKED FOR GETS A PAGE TO THINK IN. The share formula off a
// three-word answer left a model that thinks regardless a hundred-odd tokens,
// it thought through all of them, and the namer got nothing back — with the
// memo already knowing the model, so nothing asked again. A word the caller
// chose keeps the router's own allocation.
func TestAPassNobodyAskedForGetsAPageToThinkIn(t *testing.T) {
	quirksAt(t, "silent/thinker", "quiet/thinker")
	client, _ := newTestClient(t, Config{Model: "sim/model", ReasoningProfile: func(model string) (ReasoningProfile, bool) {
		switch model {
		case "silent/thinker":
			return ReasoningProfile{Mandatory: true, Default: "max"}, true
		case "quiet/thinker":
			return ReasoningProfile{Mandatory: true, Default: "high"}, true
		}
		return ReasoningProfile{}, false
	}})
	const answer = 32
	for _, tc := range []struct {
		name  string
		model string
		sent  Effort
		want  int
	}{
		{"the model's own pass at high", "quiet/thinker", EffortNone, answer + unaskedThinkingFloor},
		{"the model's own pass at max", "silent/thinker", EffortNone, answer + unaskedThinkingFloor},
		{"a word the caller chose keeps the share", "sim/model", EffortLow, 40},
		{"no pass, the caller's figure", "sim/model", EffortNone, answer},
	} {
		if got := client.wireCeiling(tc.model, tc.sent, 0, answer); got != tc.want {
			t.Errorf("%s: wireCeiling = %d, want %d", tc.name, got, tc.want)
		}
	}
	// AND A LARGE ANSWER IS UNCHANGED: the share already leaves more than a page.
	if got := client.wireCeiling("silent/thinker", EffortNone, 0, 1000); got != 20000 {
		t.Fatalf("a thousand-token answer at max = %d, want 20000", got)
	}
}
