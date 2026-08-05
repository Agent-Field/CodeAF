package router

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// panelServer stands in for the whole provider. It answers per model, records
// what it was asked, and never leaves the process — the entire router is
// testable without spending anything, which is the point of putting the
// verifier in front of the model rather than inside it.
type panelServer struct {
	mutex  sync.Mutex
	served []string
	reply  func(model string) (int, string)
}

func newPanel(t *testing.T, reply func(model string) (int, string)) (*httptest.Server, *panelServer) {
	t.Helper()
	panel := &panelServer{reply: reply}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		// The catalog fetch hits the same base URL. It is answered with nothing
		// usable on purpose — that is the offline case, where every price is
		// unknown and the panel is ordered as the operator wrote it — and it is
		// not a call the panel served.
		if !strings.HasSuffix(request.URL.Path, "/chat/completions") {
			writer.WriteHeader(http.StatusNotFound)
			return
		}
		payload, _ := io.ReadAll(request.Body)
		var decoded struct {
			Model string `json:"model"`
		}
		_ = json.Unmarshal(payload, &decoded)
		panel.mutex.Lock()
		panel.served = append(panel.served, decoded.Model)
		panel.mutex.Unlock()
		status, body := panel.reply(decoded.Model)
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(status)
		_, _ = writer.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return server, panel
}

func (p *panelServer) calls() []string {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	return append([]string(nil), p.served...)
}

// answer builds a well-formed completion carrying content.
func answer(model, content string) string {
	encoded, _ := json.Marshal(content)
	return `{"model":"` + model + `","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":` +
		string(encoded) + `}}],"usage":{"prompt_tokens":10,"completion_tokens":4,"total_tokens":14}}`
}

var testSchema = json.RawMessage(`{"type":"object","properties":{"stages":{"type":"array"}},"required":["stages"]}`)

func newRouter(t *testing.T, url string, panel Panel) *Router {
	t.Helper()
	dir := t.TempDir()
	router, err := New(panel, provider.Config{
		APIKey:  "test-key",
		BaseURL: url,
		// A catalog fetch against the fake server returns nothing usable, which
		// is exactly the offline case: every price is unknown and the panel is
		// ordered as written.
		HTTPClient: &http.Client{},
	}, dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = router.Close() })
	return router
}

func threeModels() Panel {
	return Panel{Models: []Spec{
		{Slug: "cheap/one", Price: 0.15},
		{Slug: "mid/two", Price: 0.90},
		{Slug: "top/three", Price: 2.50},
	}}
}

// TestCascadeEscalatesOnFormatFailureAndStopsOnSuccess is the policy itself. The
// cheap model returns prose where a schema was asked for; the router must notice
// without another model call, climb one rung, and stop the moment the answer
// parses — not run the whole panel.
func TestCascadeEscalatesOnFormatFailureAndStopsOnSuccess(t *testing.T) {
	server, panel := newPanel(t, func(model string) (int, string) {
		if model == "cheap/one" {
			return http.StatusOK, answer(model, "Sure! Here are the stages you asked for.")
		}
		return http.StatusOK, answer(model, `{"stages":[{"title":"a"}]}`)
	})
	router := newRouter(t, server.URL, threeModels())

	ctx := provider.WithCall(context.Background(), provider.ClassPlanSpine)
	response, err := router.CompleteWithMessages(ctx, userMessages("go"), ai.WithSchema(testSchema))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(response.Text(), `"stages"`) {
		t.Fatalf("response = %q, want the second rung's answer", response.Text())
	}
	if got := panel.calls(); len(got) != 2 || got[0] != "cheap/one" || got[1] != "mid/two" {
		t.Fatalf("calls = %v, want exactly cheap then mid", got)
	}
}

// TestCascadeEscalatesOnAnEmptyReply covers the runaway-reasoning mode. A model
// that spends its whole budget thinking returns 200 with no content, which is
// not an error and not an answer, and is the most expensive failure there is.
func TestCascadeEscalatesOnAnEmptyReply(t *testing.T) {
	server, panel := newPanel(t, func(model string) (int, string) {
		if model == "cheap/one" {
			return http.StatusOK, answer(model, "")
		}
		return http.StatusOK, answer(model, `{"stages":[]}`)
	})
	router := newRouter(t, server.URL, threeModels())

	ctx := provider.WithCall(context.Background(), provider.ClassPlanSpine)
	if _, err := router.CompleteWithMessages(ctx, userMessages("go"), ai.WithSchema(testSchema)); err != nil {
		t.Fatal(err)
	}
	if got := panel.calls(); len(got) != 2 {
		t.Fatalf("calls = %v, want an escalation past the empty reply", got)
	}
	rating, count := router.ledger.Rating("cheap/one", provider.ClassPlanSpine, 0)
	if count != 1 || rating >= 0 {
		t.Fatalf("cheap model rating = %.3f over %d observations, want one negative", rating, count)
	}
}

// TestProviderFailuresNeverMoveRatings is the rule that keeps the ledger honest.
// A 429 is weather. Rating a model down for it would make the busiest model on
// the panel look like the weakest one, and the ordering would then send work
// away from it, which is the opposite of what a rate limit calls for.
func TestProviderFailuresNeverMoveRatings(t *testing.T) {
	server, _ := newPanel(t, func(model string) (int, string) {
		if model == "cheap/one" {
			return http.StatusTooManyRequests, `{"error":{"message":"rate limited"}}`
		}
		return http.StatusOK, answer(model, `{"stages":[]}`)
	})
	router := newRouter(t, server.URL, threeModels())

	ctx := provider.WithCall(context.Background(), provider.ClassPlanSpine)
	if _, err := router.CompleteWithMessages(ctx, userMessages("go"), ai.WithSchema(testSchema)); err != nil {
		t.Fatal(err)
	}
	if _, count := router.ledger.Rating("cheap/one", provider.ClassPlanSpine, 0); count != 0 {
		t.Fatalf("a rate-limited model accumulated %d observations, want none", count)
	}
}

// TestReportedVerdictSettlesTheCallSiteHalf covers the semantic hook. A reply
// that parses is not yet known to be right; only the caller can say, and until
// it does the call moves nothing.
func TestReportedVerdictSettlesTheCallSiteHalf(t *testing.T) {
	server, _ := newPanel(t, func(model string) (int, string) {
		return http.StatusOK, answer(model, `{"stages":[]}`)
	})
	router := newRouter(t, server.URL, threeModels())

	unreported := provider.WithCall(context.Background(), provider.ClassPlanBind)
	if _, err := router.CompleteWithMessages(unreported, userMessages("go"), ai.WithSchema(testSchema)); err != nil {
		t.Fatal(err)
	}
	if _, count := router.ledger.Rating("cheap/one", provider.ClassPlanBind, 0); count != 0 {
		t.Fatalf("an unreported call moved a rating after %d observations", count)
	}

	reported := provider.WithCall(context.Background(), provider.ClassPlanBind)
	if _, err := router.CompleteWithMessages(reported, userMessages("go"), ai.WithSchema(testSchema)); err != nil {
		t.Fatal(err)
	}
	provider.Report(reported, provider.VerdictSemanticFailure)
	rating, count := router.ledger.Rating("cheap/one", provider.ClassPlanBind, 0)
	if count != 1 || rating >= 0 {
		t.Fatalf("rating = %.3f over %d, want one negative from the reported failure", rating, count)
	}

	// A second report about the same call is ignored: one unit of work is one
	// observation, however many times an error path passes through Report.
	provider.Report(reported, provider.VerdictVerifiedSuccess)
	if _, count := router.ledger.Rating("cheap/one", provider.ClassPlanBind, 0); count != 1 {
		t.Fatalf("a repeated report was counted: %d observations", count)
	}
}

// TestALeafStaysOnOneModelForItsWholeLoop is the cache-lineage rule. Every turn
// of one leaf must land on the model the first turn chose, or the prefix cache
// is rewritten every turn and two conversations get spliced into one.
func TestALeafStaysOnOneModelForItsWholeLoop(t *testing.T) {
	server, panel := newPanel(t, func(model string) (int, string) {
		return http.StatusOK, answer(model, "working")
	})
	router := newRouter(t, server.URL, threeModels())

	ctx := provider.WithCall(context.Background(), provider.ClassExecLeaf)
	for range 4 {
		if _, err := router.CompleteWithMessages(ctx, userMessages("turn")); err != nil {
			t.Fatal(err)
		}
	}
	served := panel.calls()
	for _, model := range served {
		if model != served[0] {
			t.Fatalf("leaf wandered between models: %v", served)
		}
	}
	// And a retry of the same leaf climbs, which is where leaf escalation lives.
	retry := provider.WithCallAttempt(context.Background(), provider.ClassExecLeaf, 1)
	if _, err := router.CompleteWithMessages(retry, userMessages("turn")); err != nil {
		t.Fatal(err)
	}
	if got := panel.calls(); got[len(got)-1] == served[0] {
		t.Fatalf("the retry went back to %s, want the next rung up", served[0])
	}
}

// TestCascadeOrdersByExpectedSuccessPerDollar checks the ordering rule and the
// cold-start rule at once.
//
// With nothing measured the panel is tried cheapest first, which is how a cold
// panel buys its own evidence. A cheap model that is merely *somewhat* worse
// stays first on purpose — that is the whole cascade argument, that a cheap
// failure costs almost nothing — so it takes sustained evidence to displace it,
// and once that arrives the ordering moves.
func TestCascadeOrdersByExpectedSuccessPerDollar(t *testing.T) {
	server, _ := newPanel(t, func(model string) (int, string) {
		return http.StatusOK, answer(model, `{"stages":[]}`)
	})
	router := newRouter(t, server.URL, threeModels())

	if got := slugs(router.order(provider.ClassPlanSpine)); got[0] != "cheap/one" {
		t.Fatalf("cold order = %v, want the cheapest model first", got)
	}
	for range 60 {
		router.ledger.Observe("cheap/one", provider.ClassPlanSpine, 0, provider.VerdictFormatFailure)
		router.ledger.Observe("mid/two", provider.ClassPlanSpine, 0, provider.VerdictVerifiedSuccess)
	}
	if got := slugs(router.order(provider.ClassPlanSpine)); got[0] != "mid/two" {
		t.Fatalf("measured order = %v, want the model that keeps working first", got)
	}
	// Only this class moved. Ability is not one number, and evidence from the
	// spine must not decide who writes a brief.
	if got := slugs(router.order(provider.ClassPlanBrief)); got[0] != "cheap/one" {
		t.Fatalf("brief order = %v, want spine evidence to stay in the spine class", got)
	}
}

// TestTheStrongestModelIsSeatedLast is the other half of the ordering rule. A
// cascade whose terminal rung is not the best thing available has a ceiling
// below the best single model's, which would give away the whole reason for
// cascading rather than just picking one model.
func TestTheStrongestModelIsSeatedLast(t *testing.T) {
	server, _ := newPanel(t, func(model string) (int, string) {
		return http.StatusOK, answer(model, `{"stages":[]}`)
	})
	router := newRouter(t, server.URL, threeModels())

	// Measured strong but not cheap enough to open with: by value it ranks
	// second, and it still has to be the rung of last resort.
	for range 60 {
		router.ledger.Observe("mid/two", provider.ClassPlanAudit, 0, provider.VerdictVerifiedSuccess)
	}
	got := slugs(router.order(provider.ClassPlanAudit))
	if got[0] != "cheap/one" {
		t.Fatalf("order = %v, want the cheapest model still opening", got)
	}
	if got[len(got)-1] != "mid/two" {
		t.Fatalf("order = %v, want the measured-strongest model last", got)
	}
}

// TestEveryRungFailingIsAnError guards the one case that must not be silent: if
// the whole panel produced unusable answers, the caller has to hear about it
// rather than receive the last one as though it were fine.
func TestEveryRungFailingIsAnError(t *testing.T) {
	server, panel := newPanel(t, func(model string) (int, string) {
		return http.StatusOK, answer(model, "not json at all")
	})
	router := newRouter(t, server.URL, threeModels())

	ctx := provider.WithCall(context.Background(), provider.ClassPlanSpine)
	if _, err := router.CompleteWithMessages(ctx, userMessages("go"), ai.WithSchema(testSchema)); err == nil {
		t.Fatal("a panel that failed on every rung returned no error")
	}
	if got := panel.calls(); len(got) != 3 {
		t.Fatalf("calls = %v, want all three rungs tried", got)
	}
}

// TestCascadeIsCappedAtThreeRungs bounds the worst case. A panel of six models
// must not cost six calls on one bad answer.
func TestCascadeIsCappedAtThreeRungs(t *testing.T) {
	var models []Spec
	for _, slug := range []string{"a/1", "b/2", "c/3", "d/4", "e/5", "f/6"} {
		models = append(models, Spec{Slug: slug, Price: 1})
	}
	server, panel := newPanel(t, func(model string) (int, string) {
		return http.StatusOK, answer(model, "not json")
	})
	router := newRouter(t, server.URL, Panel{Models: models})

	ctx := provider.WithCall(context.Background(), provider.ClassPlanSpine)
	_, _ = router.CompleteWithMessages(ctx, userMessages("go"), ai.WithSchema(testSchema))
	if got := panel.calls(); len(got) != maxRungs {
		t.Fatalf("calls = %v, want at most %d", got, maxRungs)
	}
}

// TestRungsGetSeparateCacheLineages is the affinity rule. Two models cannot
// share a prefix cache: what the cheap rung wrote is not something the
// escalation target can read.
func TestRungsGetSeparateCacheLineages(t *testing.T) {
	var keys []string
	var mutex sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if !strings.HasSuffix(request.URL.Path, "/chat/completions") {
			writer.WriteHeader(http.StatusNotFound)
			return
		}
		payload, _ := io.ReadAll(request.Body)
		var decoded struct {
			Model string `json:"model"`
			Key   string `json:"prompt_cache_key"`
		}
		_ = json.Unmarshal(payload, &decoded)
		mutex.Lock()
		keys = append(keys, decoded.Key)
		mutex.Unlock()
		writer.Header().Set("Content-Type", "application/json")
		if decoded.Model == "cheap/one" {
			_, _ = writer.Write([]byte(answer(decoded.Model, "prose")))
			return
		}
		_, _ = writer.Write([]byte(answer(decoded.Model, `{"stages":[]}`)))
	}))
	defer server.Close()

	router := newRouter(t, server.URL, threeModels())
	ctx := provider.WithCacheKey(context.Background(), provider.RunCacheKey("a goal", "panel"))
	ctx = provider.WithCall(ctx, provider.ClassPlanSpine)
	if _, err := router.CompleteWithMessages(ctx, userMessages("go"), ai.WithSchema(testSchema)); err != nil {
		t.Fatal(err)
	}
	if len(keys) != 2 || keys[0] == "" || keys[0] == keys[1] {
		t.Fatalf("cache keys = %v, want one distinct key per model", keys)
	}
}

// TestLedgerKeysOnTheResolvedSnapshot covers floating aliases. The configured
// slug is a pointer; what a rating is about is the weights the provider actually
// ran, which only the response can say.
func TestLedgerKeysOnTheResolvedSnapshot(t *testing.T) {
	server, _ := newPanel(t, func(string) (int, string) {
		return http.StatusOK, answer("vendor/model-0731", "not json")
	})
	router := newRouter(t, server.URL, Panel{Models: []Spec{{Slug: "~vendor/model-latest", Price: 1}}})

	ctx := provider.WithCall(context.Background(), provider.ClassPlanSpine)
	_, _ = router.CompleteWithMessages(ctx, userMessages("go"), ai.WithSchema(testSchema))

	if _, count := router.ledger.Rating("vendor/model-0731", provider.ClassPlanSpine, 0); count != 1 {
		t.Fatal("the observation was not recorded against the dated snapshot")
	}
	if got := router.ledger.Resolve("~vendor/model-latest"); got != "vendor/model-0731" {
		t.Fatalf("Resolve = %q, want the snapshot the provider served", got)
	}
}

// TestEventsRecordTheCandidatesNotJustTheChoice is what makes the log worth
// keeping. A logged choice with no record of what it was chosen from is
// auditable and not analysable, and offline policy work needs the second.
func TestEventsRecordTheCandidatesNotJustTheChoice(t *testing.T) {
	dir := t.TempDir()
	server, _ := newPanel(t, func(model string) (int, string) {
		if model == "cheap/one" {
			return http.StatusOK, answer(model, "prose")
		}
		return http.StatusOK, answer(model, `{"stages":[]}`)
	})
	router, err := New(threeModels(), provider.Config{APIKey: "k", BaseURL: server.URL, HTTPClient: &http.Client{}}, dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx := provider.WithCall(context.Background(), provider.ClassPlanSpine)
	if _, err := router.CompleteWithMessages(ctx, userMessages("go"), ai.WithSchema(testSchema)); err != nil {
		t.Fatal(err)
	}
	provider.Report(ctx, provider.VerdictVerifiedSuccess)
	if err := router.Close(); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "router-events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 3 {
		t.Fatalf("wrote %d rows, want two attempts and one settled verdict:\n%s", len(lines), data)
	}
	var first Event
	if err := json.Unmarshal([]byte(lines[0]), &first); err != nil {
		t.Fatal(err)
	}
	if len(first.Candidates) != 3 || first.Model != "cheap/one" || first.Verdict != provider.VerdictFormatFailure {
		t.Fatalf("first row = %+v", first)
	}
	var last Event
	if err := json.Unmarshal([]byte(lines[2]), &last); err != nil {
		t.Fatal(err)
	}
	if !last.Final || last.Verdict != provider.VerdictVerifiedSuccess || last.Model != "mid/two" {
		t.Fatalf("last row = %+v, want the settled verdict against the winning rung", last)
	}
}

func userMessages(text string) []ai.Message {
	return []ai.Message{{Role: "user", Content: []ai.ContentPart{{Type: "text", Text: text}}}}
}
