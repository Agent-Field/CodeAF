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

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

type testServer struct {
	URL     string
	handler http.Handler
}

func newTestServer(handler http.Handler) *testServer {
	return &testServer{URL: "http://router.test", handler: handler}
}

func (s *testServer) Client() *http.Client {
	return &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		recorder := httptest.NewRecorder()
		s.handler.ServeHTTP(recorder, request)
		return recorder.Result(), nil
	})}
}

func (s *testServer) Close() {}

func newPanel(t *testing.T, reply func(model string) (int, string)) (*testServer, *panelServer) {
	t.Helper()
	panel := &panelServer{reply: reply}
	server := newTestServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
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

func newRouter(t *testing.T, server *testServer, panel Panel) *Router {
	t.Helper()
	dir := t.TempDir()
	router, err := New(panel, provider.Config{
		APIKey:  "test-key",
		BaseURL: server.URL,
		// A catalog fetch against the fake server returns nothing usable, which
		// is exactly the offline case: every price is unknown and the panel is
		// ordered as written.
		HTTPClient: server.Client(),
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
	router := newRouter(t, server, threeModels())

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
	router := newRouter(t, server, threeModels())

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
	router := newRouter(t, server, threeModels())

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
	router := newRouter(t, server, threeModels())

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
	router := newRouter(t, server, threeModels())

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
	router := newRouter(t, server, threeModels())

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
	router := newRouter(t, server, threeModels())

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
	router := newRouter(t, server, threeModels())

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
	router := newRouter(t, server, Panel{Models: models})

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
	server := newTestServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
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

	router := newRouter(t, server, threeModels())
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
	router := newRouter(t, server, Panel{Models: []Spec{{Slug: "~vendor/model-latest", Price: 1}}})

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
	router, err := New(threeModels(), provider.Config{APIKey: "k", BaseURL: server.URL, HTTPClient: server.Client()}, dir)
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

// armBPanel is the panel the routing A/B actually ran, at its real prices and
// with the roles it was written with. The numbers matter: gemma is the cheapest
// and measured 11/20 on the anchor battery, flash is the mid incumbent at 18/20,
// and kimi is the reason the panel exists — 20/20, theta +4.09, the only model
// significantly above the pack, and the declared terminal rung. Every defect
// below is reproduced against it rather than against a toy, because three of the
// four mechanisms only appear on a barbell like this one.
func armBPanel() Panel {
	return Panel{Models: []Spec{
		{Slug: "google/gemma-3-12b-it", Role: "base", Price: 0.150},
		{Slug: "qwen/qwen3-30b-a3b", Role: "base", Price: 0.193},
		{Slug: "z-ai/glm-4.7-flash", Role: "base", Price: 0.230},
		{Slug: "~deepseek/deepseek-v4-flash", Role: "mid", Price: 0.180},
		{Slug: "moonshotai/kimi-k2.6", Role: "top", Price: 2.480},
	}}
}

const (
	gemma = "google/gemma-3-12b-it"
	flash = "~deepseek/deepseek-v4-flash"
	kimi  = "moonshotai/kimi-k2.6"
)

// answering is a panel server that gives every model the same well-formed reply.
// The ordering tests never need a model to fail; what they are about is who gets
// asked.
func answering(t *testing.T) *testServer {
	t.Helper()
	server, _ := newPanel(t, func(model string) (int, string) {
		return http.StatusOK, answer(model, `{"stages":[]}`)
	})
	return server
}

// TestAFewGradedOutcomesCannotOutvoteThePrior is defect 1, at the exact numbers
// that broke.
//
// Five budget stops, all from one task, were the entire graded record behind
// flash's exec.leaf rating — the other 103 outcomes were unverified successes
// that correctly moved nothing. Those five took the rating to -0.98, which on
// Ability/price put gemma (1.79) ahead of flash (1.52) and handed every leaf on
// the panel to the weaker model. Below MinGraded the ordering must not look at
// the learned number at all.
func TestAFewGradedOutcomesCannotOutvoteThePrior(t *testing.T) {
	router := newRouter(t, answering(t), armBPanel())
	if got := slugs(router.order(provider.ClassExecLeaf))[0]; got != flash {
		t.Fatalf("cold opener = %s, want the mid model that opens on value", got)
	}

	for range 5 {
		router.ledger.Observe(flash, provider.ClassExecLeaf, 0, provider.VerdictBudgetStop)
	}
	rating, count := router.ledger.Rating(flash, provider.ClassExecLeaf, 0)
	if count != 5 || rating >= 0 {
		t.Fatalf("rating = %.3f over %d, want the five budget stops recorded", rating, count)
	}
	if got := slugs(router.order(provider.ClassExecLeaf))[0]; got != flash {
		t.Fatalf("opener = %s after %d graded observations, want %s — a rating under the "+
			"MinGraded=%d gate must not reorder anything", got, count, flash, MinGraded)
	}

	// And once the evidence is actually there, it counts. The gate delays a
	// judgement; it does not refuse to make one.
	for range 40 {
		router.ledger.Observe(flash, provider.ClassExecLeaf, 0, provider.VerdictEmptyResponse)
	}
	if got := slugs(router.order(provider.ClassExecLeaf))[0]; got != gemma {
		t.Fatalf("opener = %s after sustained graded failure, want the gate to have opened", got)
	}
}

// TestLeafRatingsAreKeyedByTheShapeOfLeaf is defect 2. exec.leaf was one global
// class, so a rating learned on t1's oversized leaves — the ones carrying 2.2M
// prompt tokens into a 300k budget — was applied unchanged to t2's document
// reading and t3's small repairs, where the demoted model had never once failed.
// That single fact is the whole of the t2 and t3 collapse.
func TestLeafRatingsAreKeyedByTheShapeOfLeaf(t *testing.T) {
	router := newRouter(t, answering(t), armBPanel())
	oversized := shaped(provider.ClassExecLeaf, "oversized")
	atomic := shaped(provider.ClassExecLeaf, "atomic")

	for range 30 {
		router.ledger.Observe(flash, oversized, 0, provider.VerdictEmptyResponse)
	}
	if got := slugs(router.order(oversized))[0]; got != gemma {
		t.Fatalf("oversized opener = %s, want the lesson to apply where it was learned", got)
	}
	if got := slugs(router.order(atomic))[0]; got != flash {
		t.Fatalf("atomic opener = %s, want %s — what one shape of leaf teaches must not "+
			"reroute the shapes it says nothing about", got, flash)
	}
	// The undivided class is untouched too, which is what stops an old ledger's
	// pooled evidence from leaking into the split one.
	if _, count := router.ledger.Rating(flash, provider.ClassExecLeaf, 0); count != 0 {
		t.Fatalf("the global exec.leaf class accumulated %d observations", count)
	}
}

// TestSuccessCannotDemoteTheTerminalRung is defect 3, and the direction is the
// surprising part: the panel lost its ceiling because the cheap model kept
// working, not because anything failed.
//
// The old rule promoted whichever model was rated highest. As flash accumulated
// positive planning observations its rating climbed past kimi's untouched
// cold-start prior, flash became the highest rated, the promotion stopped firing
// and the last rung reverted to whatever ranked third on value. Eight of eleven
// classes went from ending in kimi to ending in qwen between run 1 and run 3.
func TestSuccessCannotDemoteTheTerminalRung(t *testing.T) {
	router := newRouter(t, answering(t), armBPanel())
	cold := slugs(router.order(provider.ClassPlanExpand))
	if cold[len(cold)-1] != kimi {
		t.Fatalf("cold cascade = %v, want it to end in the declared top model", cold)
	}

	// Arm B's own record: plan.expand +2.31 over 48 observations.
	for range 48 {
		router.ledger.Observe(flash, provider.ClassPlanExpand, 0, provider.VerdictVerifiedSuccess)
	}
	rating, count := router.ledger.Rating(flash, provider.ClassPlanExpand, 0)
	if kimiRating, _ := router.ledger.Rating(kimi, provider.ClassPlanExpand, 1); rating <= kimiRating {
		t.Fatalf("flash %.2f over %d did not overtake kimi's prior %.2f — the test no longer "+
			"reproduces the condition it exists for", rating, count, kimiRating)
	}

	warm := slugs(router.order(provider.ClassPlanExpand))
	if warm[0] != flash {
		t.Fatalf("cascade = %v, want the measured-cheap model still opening", warm)
	}
	if warm[len(warm)-1] != kimi {
		t.Fatalf("cascade = %v, want it to still end in %s — a cascade whose ceiling is a "+
			"mid model has given away the reason the panel exists", warm, kimi)
	}
}

// TestLeafEscalationClimbsAbilityNotValue is defect 4. Leaf escalation used to
// take order[attempt], and order is sorted by Ability/price — so on a barbell
// panel rung 1 is the second-best *value*, which is the weakest model. Every
// escalation arm B made went from a model measured 18/20 on the anchor battery
// to one measured 11/20, which cannot help by construction.
//
// It also covers the second half of that defect: with one escalation granted,
// stepping through near-peers puts the top model out of reach of a leaf under
// any circumstances, so the one retry goes straight to the ceiling.
func TestLeafEscalationClimbsAbilityNotValue(t *testing.T) {
	server, panel := newPanel(t, func(model string) (int, string) {
		return http.StatusOK, answer(model, "working")
	})
	router := newRouter(t, server, armBPanel())

	opened := provider.WithCall(context.Background(), provider.ClassExecLeaf)
	if _, err := router.CompleteWithMessages(opened, userMessages("turn")); err != nil {
		t.Fatal(err)
	}
	retried := provider.WithCallAttempt(context.Background(), provider.ClassExecLeaf, 1)
	if _, err := router.CompleteWithMessages(retried, userMessages("turn")); err != nil {
		t.Fatal(err)
	}

	served := panel.calls()
	if len(served) != 2 || served[0] != flash {
		t.Fatalf("calls = %v, want the leaf to open on the value choice", served)
	}
	if served[1] == gemma {
		t.Fatalf("the retry escalated %s -> %s, which is 18/20 to 11/20 on the anchor "+
			"battery — a retry must be strictly stronger than what failed", served[0], served[1])
	}
	if served[1] != kimi {
		t.Fatalf("the retry went to %s, want the ceiling: one retry is all a leaf gets, "+
			"so stepping through near-peers puts %s out of reach entirely", served[1], kimi)
	}
}

// TestARetriedLeafRecordsTheChainItClimbed is defect 5. A retried leaf logged
// `rung: 1` with an empty escalation, which reads as a first attempt that
// happened to start high — and a logged choice with no record of what it was
// chosen from is auditable rather than analysable, which is the one thing this
// log exists not to be. It also cost the harness its own escalation count.
func TestARetriedLeafRecordsTheChainItClimbed(t *testing.T) {
	dir := t.TempDir()
	server, _ := newPanel(t, func(model string) (int, string) {
		return http.StatusOK, answer(model, "working")
	})
	router, err := New(armBPanel(), provider.Config{APIKey: "k", BaseURL: server.URL, HTTPClient: server.Client()}, dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx := provider.WithCacheKey(context.Background(), "run-7")
	ctx = provider.WithCallShape(ctx, provider.ClassExecLeaf, 1, "atomic")
	if _, err := router.CompleteWithMessages(ctx, userMessages("turn")); err != nil {
		t.Fatal(err)
	}
	provider.Report(ctx, provider.VerdictBudgetStop)
	if err := router.Close(); err != nil {
		t.Fatal(err)
	}

	rows := eventRows(t, dir)
	if len(rows) != 2 {
		t.Fatalf("wrote %d rows, want the attempt and its settled verdict", len(rows))
	}
	attempt := rows[0]
	if attempt.Rung != 1 || attempt.Model != kimi {
		t.Fatalf("attempt = %+v, want rung 1 served by the ceiling", attempt)
	}
	if len(attempt.Escalation) != 1 || attempt.Escalation[0] != flash {
		t.Fatalf("escalation = %v, want the rung this leaf already failed on", attempt.Escalation)
	}
	if len(attempt.Candidates) != 2 {
		t.Fatalf("candidates = %v, want the whole ladder the choice was made from", attempt.Candidates)
	}
	if attempt.Shape != "atomic" {
		t.Fatalf("shape = %q, want the population the rating was keyed on", attempt.Shape)
	}
	// The settled row has to say which run it belongs to. Without it the only way
	// to attribute a verdict to a cell is by position in an append-only file that
	// several processes write to, which is what made arm B's per-cell attribution
	// approximate in the first place.
	if rows[1].Run != "run-7" || !rows[1].Final {
		t.Fatalf("settled row = %+v, want it stamped with the run it belongs to", rows[1])
	}
}

// TestExplorationMeasuresThePanelItChoosesAgainst is defect 6. Only the chosen
// model accumulates evidence, so a model never chosen is never measured: three
// of five panel members had zero observations after nine cells and about a
// thousand calls. A prior that is never contradicted is indistinguishable from a
// fact, and that is what made every other defect invisible until it acted.
func TestExplorationMeasuresThePanelItChoosesAgainst(t *testing.T) {
	server, _ := newPanel(t, func(model string) (int, string) {
		return http.StatusOK, answer(model, `{"stages":[]}`)
	})
	router := newRouter(t, server, armBPanel())

	// Exploration only starts once the router has stopped generating evidence on
	// its own — that is, once the opener is past the gate. While the panel is
	// cold the ordinary cheapest-first cascade is already the exploration.
	for range MinGraded {
		router.ledger.Observe(flash, provider.ClassPlanExpand, 0, provider.VerdictVerifiedSuccess)
	}

	const runs = 200
	for index := range runs {
		ctx := provider.WithCacheKey(context.Background(), "explore-run")
		ctx = provider.WithCall(ctx, provider.ClassPlanExpand)
		if _, err := router.CompleteWithMessages(ctx, userMessages("go"), ai.WithSchema(testSchema)); err != nil {
			t.Fatalf("call %d: %v", index, err)
		}
		provider.Report(ctx, provider.VerdictVerifiedSuccess)
	}

	// Every member the router could otherwise never learn about. kimi is absent
	// on purpose: it is the terminal rung, which exploration must never disturb,
	// and defects 3 and 4 are what put it back within reach through the ordinary
	// escalation path.
	for _, slug := range []string{gemma, "qwen/qwen3-30b-a3b", "z-ai/glm-4.7-flash"} {
		if _, count := router.ledger.Rating(slug, provider.ClassPlanExpand, -1); count == 0 {
			t.Fatalf("%s still has no observations after %d graded calls — the router is "+
				"choosing against a model it has never measured", slug, runs)
		}
	}
	if _, count := router.ledger.Rating(kimi, provider.ClassPlanExpand, 1); count != 0 {
		t.Fatalf("the terminal rung was explored %d times; the cascade's ceiling is the one "+
			"seat that must never be spent on evidence", count)
	}
}

// TestExplorationIsDeterministicForOneRun is what makes the A/B re-runnable. An
// experiment whose treatment arm makes different choices on every replay cannot
// be compared with anything, including itself.
func TestExplorationIsDeterministicForOneRun(t *testing.T) {
	draw := func() []bool {
		server, _ := newPanel(t, func(model string) (int, string) {
			return http.StatusOK, answer(model, `{"stages":[]}`)
		})
		router := newRouter(t, server, armBPanel())
		for range MinGraded {
			router.ledger.Observe(flash, provider.ClassPlanExpand, 0, provider.VerdictVerifiedSuccess)
		}
		var drawn []bool
		for range 40 {
			ctx := provider.WithCacheKey(context.Background(), "fixed-run")
			drawn = append(drawn, router.drawExplore(ctx, provider.ClassPlanExpand))
		}
		return drawn
	}
	first, second := draw(), draw()
	explored := 0
	for index := range first {
		if first[index] != second[index] {
			t.Fatalf("draw %d differed between two replays of the same run", index)
		}
		if first[index] {
			explored++
		}
	}
	if explored == 0 || explored > len(first)/3 {
		t.Fatalf("explored %d of %d calls, want roughly one in %d", explored, len(first), exploreEvery)
	}
}

// TestExplorationWaitsForTheRouterToStopLearningOnItsOwn is the bound that keeps
// exploration from costing anything on the case that matters most. A cold panel
// is already trying its cheap models and escalating what they get wrong; adding
// forced trials there would spend budget to learn what the cascade was about to
// be told for free.
func TestExplorationWaitsForTheRouterToStopLearningOnItsOwn(t *testing.T) {
	server, panel := newPanel(t, func(model string) (int, string) {
		return http.StatusOK, answer(model, `{"stages":[]}`)
	})
	router := newRouter(t, server, armBPanel())
	for range 60 {
		ctx := provider.WithCall(context.Background(), provider.ClassPlanSpine)
		if _, err := router.CompleteWithMessages(ctx, userMessages("go"), ai.WithSchema(testSchema)); err != nil {
			t.Fatal(err)
		}
	}
	for _, model := range panel.calls() {
		if model != flash {
			t.Fatalf("a cold panel routed a call to %s; with nothing measured the cascade "+
				"is already the exploration", model)
		}
	}
}

// TestExplorationNeverTouchesALeaf is the other bound, and it is absolute. A
// leaf has no verifier, so a trial that goes wrong there is not caught for free
// — it is a whole conversation spent, and possibly a node failed.
func TestExplorationNeverTouchesALeaf(t *testing.T) {
	server, panel := newPanel(t, func(model string) (int, string) {
		return http.StatusOK, answer(model, "working")
	})
	router := newRouter(t, server, armBPanel())
	leafClass := shaped(provider.ClassExecLeaf, "atomic")
	for range MinGraded {
		router.ledger.Observe(flash, leafClass, 0, provider.VerdictVerifiedSuccess)
	}
	for range 60 {
		ctx := provider.WithCallShape(context.Background(), provider.ClassExecLeaf, 0, "atomic")
		if _, err := router.CompleteWithMessages(ctx, userMessages("turn")); err != nil {
			t.Fatal(err)
		}
	}
	for _, model := range panel.calls() {
		if model != flash {
			t.Fatalf("a leaf was routed to %s; leaves are never explored", model)
		}
	}
}

// TestTheArmBCollapseCannotReproduce runs the failure end to end.
//
// The sequence is arm B's: a shared ledger carries three runs of history, the
// only graded leaf evidence is budget stops from one oversized task, and the
// planning classes accumulate real successes for the cheap incumbent. In arm B
// that combination reordered eight of eleven classes, dropped the panel's strong
// model out of every cascade, and collapsed two working tasks to zero. Each
// assertion here is one of the four things that had to hold and did not.
func TestTheArmBCollapseCannotReproduce(t *testing.T) {
	router := newRouter(t, answering(t), armBPanel())
	oversized := shaped(provider.ClassExecLeaf, "oversized")
	atomic := shaped(provider.ClassExecLeaf, "atomic")

	before := slugs(router.order(atomic))
	// Run 1 through run 3: five budget stops on the one task whose leaves are too
	// big, and forty-eight successful planning calls on the incumbent.
	for range 5 {
		router.ledger.Observe(flash, oversized, 0, provider.VerdictBudgetStop)
	}
	for range 48 {
		router.ledger.Observe(flash, provider.ClassPlanSpine, 0, provider.VerdictVerifiedSuccess)
		router.ledger.Observe(flash, provider.ClassPlanExpand, 0, provider.VerdictVerifiedSuccess)
	}

	t.Run("leaf ordering does not move", func(t *testing.T) {
		// Two independent reasons, and the test wants both: the five outcomes are
		// under the gate, and they were recorded against a shape these leaves are
		// not. Either alone would have prevented the collapse.
		if got := slugs(router.order(atomic)); !equalSlugs(got, before) {
			t.Fatalf("leaf order moved from %v to %v on five budget stops from another task", before, got)
		}
		if got := slugs(router.order(oversized))[0]; got != flash {
			t.Fatalf("even the shape that failed reordered on %d observations, under the gate of %d",
				5, MinGraded)
		}
	})

	t.Run("the top model stays terminal", func(t *testing.T) {
		for _, class := range []provider.CallClass{provider.ClassPlanSpine, provider.ClassPlanExpand} {
			order := slugs(router.order(class))
			if order[len(order)-1] != kimi {
				t.Fatalf("%s = %v, want it to still end in %s after the incumbent's successes",
					class, order, kimi)
			}
		}
	})

	t.Run("escalation goes strictly up in ability", func(t *testing.T) {
		scored := router.rank(atomic)
		ladder := router.leafLadder(atomic)
		if len(ladder) < 2 {
			t.Fatal("the leaf has nowhere to escalate to")
		}
		for index := 1; index < len(ladder); index++ {
			below := ratingOf(scored, ladder[index-1]).rating
			above := ratingOf(scored, ladder[index]).rating
			if above <= below {
				t.Fatalf("rung %d (%s, %.2f) is not stronger than rung %d (%s, %.2f)",
					index, ladder[index].spec.Slug, above, index-1, ladder[index-1].spec.Slug, below)
			}
		}
		if ladder[len(ladder)-1].spec.Slug != kimi {
			t.Fatalf("the ladder tops out at %s, want the panel's ceiling", ladder[len(ladder)-1].spec.Slug)
		}
	})
}

func equalSlugs(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for index := range a {
		if a[index] != b[index] {
			return false
		}
	}
	return true
}

func eventRows(t *testing.T, dir string) []Event {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "router-events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var rows []Event
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var row Event
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			t.Fatal(err)
		}
		rows = append(rows, row)
	}
	return rows
}
