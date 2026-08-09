package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/router"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

// The plan-only non-answer, end to end. A blind UX rubric ran the same research
// journey five times against the same model: three runs came back with the right
// figures and a citation, and two came back with "I will look up the filings and
// compare" — a plan, handed over where the answer belonged, from a worker that
// had called no tools at all.
//
// That shape used to ship. The gate read a final message, saw text that promised
// nothing false, and had been told in its own instructions that silence in the
// run records never acquits nor convicts — so an empty record read as "the
// evidence is simply not available here" and it passed. The fix is structural
// where it can be: a run that was watched from end to end and called nothing now
// says so in words, which is the one record in that block that is complete
// rather than a tail, and the judge decides what to do about it.
//
// Everything below the script is the product's own: the compiler, the working
// method, the executor, the gate, the revision pass, the citation invariant, and
// the replan that splices the work a cited gap buys.
func TestAPlanWithNothingRunIsGatedAndTheAnswerReplacesIt(t *testing.T) {
	brain := newAnswerBrain(t)
	defer brain.close()

	var stdout, stderr strings.Builder
	if err := doErrand(doRequest{
		task:      answerAsk,
		timeout:   60 * time.Second,
		stdout:    &stdout,
		stderr:    &stderr,
		newClient: brain.client,
	}); err != nil {
		t.Fatalf("the errand did not settle: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
	}

	// The record the gate was handed on the draft. This is the whole of the fix
	// that is not a prompt: the judge is told the difference between a run whose
	// evidence is unavailable and a run that did nothing.
	first := brain.gatePrompt(1)
	if first == "" {
		t.Fatal("the delivery gate never ran on the first draft")
	}
	if !strings.Contains(first, unexercisedRecord) {
		t.Fatalf("the gate was not told the draft ran nothing:\n%s", first)
	}
	if !strings.Contains(first, planShapedDraft) {
		t.Fatalf("the gate was not handed the draft it is judging:\n%s", first)
	}

	// The revision is the second chance, and it is handed the law in words as
	// well as the critique — a revision that closes a gap and reports that it
	// did has moved the failure one round along rather than fixing it.
	revision := brain.leafPrompt("A reviewer compared the previous attempt")
	if revision == "" {
		t.Fatal("the named gap never bought a revision pass")
	}
	if !strings.Contains(revision, gateRevisionContract) {
		t.Fatalf("the revision was not told its message is the deliverable:\n%s", revision)
	}

	// The gap quoted the ask, so it could buy real work: the extension ran, and
	// what it produced is what the person reads.
	if got := brain.count("extension"); got != 1 {
		t.Fatalf("the cited gap bought %d extensions, want exactly 1", got)
	}
	if got := brain.count("gate"); got < 3 {
		t.Fatalf("the gate ran %d times, want at least 3 (draft, revision, repair)", got)
	}
	if !strings.Contains(stdout.String(), answerFigures) {
		t.Fatalf("what the person reads does not carry the figures:\n%s", stdout.String())
	}
	if strings.Contains(stdout.String(), planShapedDraft) {
		t.Fatalf("the plan shipped as the answer:\n%s", stdout.String())
	}
}

// The seam under the test above, on its own. Two empty slices mean two different
// things and this used to give the same answer to both — which is exactly the
// ambiguity that let a plan pass, because the gate is separately and correctly
// told that an incomplete record can never acquit.
func TestOnlyAWatchedRunThatDidNothingSaysSo(t *testing.T) {
	graph := openCacheStore(t)
	settings := config.Config{Model: "worker/model"}
	node := store.Node{ID: "job", Brief: "find the figures",
		Provenance: store.Provenance{Intent: answerAsk}}

	body := func(evidence deliveryEvidence) string {
		t.Helper()
		capture := &gateCaptureClient{model: "worker/model"}
		judgeDeliverable(context.Background(), settings,
			&liveClient{settings: settings, model: capture.model, client: capture}, graph, node,
			planShapedDraft, "", evidence, "worker/model")
		return capture.messages[len(capture.messages)-1].Content[0].Text
	}

	// Nobody was watching: no record at all, exactly as before.
	if got := body(deliveryEvidence{}); strings.Contains(got, "What actually happened") {
		t.Errorf("an unobserved run manufactured a record:\n%s", got)
	}
	// Watched, and it did nothing. The record is stated, and stated as complete.
	watched := body(deliveryEvidence{Observed: true})
	if !strings.Contains(watched, unexercisedRecord) {
		t.Errorf("a watched run that did nothing said nothing:\n%s", watched)
	}
	if !strings.Contains(watched, "the whole record of the run and not a tail of one") {
		t.Errorf("the one complete record is not named as complete:\n%s", watched)
	}
	// Watched and it did something: the ordinary tail, unchanged.
	busy := body(deliveryEvidence{Observed: true, Ran: []string{`web_search {"q":"filing"}`}})
	if strings.Contains(busy, unexercisedRecord) {
		t.Errorf("a run that did something was recorded as doing nothing:\n%s", busy)
	}
	if !strings.Contains(busy, `web_search {"q":"filing"}`) {
		t.Errorf("the tail of what ran was lost:\n%s", busy)
	}
}

// The gate already knew how to name a deliverable that describes the work
// instead of being it — a benchmark trace has it naming exactly that gap. What it
// had no words for was the same absence in the future tense, which is the shape
// of a plan, and what it had no evidence for was a run that never happened. Both
// are pinned, and the older half is pinned with them so a rewrite cannot trade
// one for the other.
func TestTheGateNamesThePlanAndTheUnexercisedRunAsMissingContent(t *testing.T) {
	for name, required := range map[string]string{
		"the deliverable is what is read":      "What you are handed IS the deliverable",
		"describing the work is an absence":    "has described the deliverable in place of being it",
		"a pointer is not the answer":          "A pointer to where the answer lives is not the answer",
		"and neither is a plan":                "is a plan for producing the answer handed over in place of the answer",
		"the plan is a gap however sound":      "it is a gap however sound the plan is",
		"one record is not partial":            "There is one record that is not partial, and it says so of itself",
		"nothing could have been found":        "anything the request needed the work to go and find is not in the deliverable and cannot be",
		"the gap is the content, not the work": "name what was to be found and never was, in those words, and never as a remark about effort or process",
		"an answerable ask is no gap":          "an unexercised run is no gap at all",
		// The diversion shape lost both losing cells of the final benchmark
		// pass: a thin message beside a fat file nobody asked for. The records
		// paragraph reads that shape against the substance rule.
		"a fat unnamed file convicts the thin message": "the run wrote a file and the deliverable's own text is thin beside it",
		"the gap is the content in the message":        "the missing element is that content itself, in the message",
		"an asked-for file is the correct shape":       "a short message beside an asked-for file convicts nothing by its length",
	} {
		if !strings.Contains(judgeDeliverablePrompt, required) {
			t.Errorf("the gate no longer states %s: %q missing", name, required)
		}
	}
	// It is still a gate and not a critic: the substance test must stay one
	// absence among the others rather than becoming a second style rubric.
	if !strings.Contains(judgeDeliverablePrompt, "This is still one absence and not a second style test") {
		t.Error("the substance test stopped being one absence among the others")
	}
	for _, required := range []string{
		"Your final message is the deliverable",
		"Nothing written in the future tense counts",
	} {
		if !strings.Contains(gateRevisionContract, required) {
			t.Errorf("the revision contract no longer states %q", required)
		}
	}
}

// ---------------------------------------------------------------------------
// The scripted brain for the run above. It is deliberately its own, not a mode
// bolted onto the one in do_test.go: this script is about a worker that does
// nothing, and that is the opposite of the script next door.

const (
	answerAsk = "look up the two filings and give me the revenue figures with a citation"
	// planShapedDraft is the failure verbatim: future tense, no tool calls, no
	// content — and nothing in it is false.
	planShapedDraft = "I will look up the two filings and compare the revenue figures, then report back with a citation."
	// answerFigures is what the work that the cited gap bought actually returns.
	answerFigures = "Revenue was $4.1B in the 2023 filing and $3.6B in the 2022 filing (Form 10-K, page 44)."
	// answerQuote is a span of the ask in the person's own words, which is the
	// only thing that lets a gap commission work.
	answerQuote = "the revenue figures with a citation"
)

type answerBrain struct {
	t      *testing.T
	server *httptest.Server
	dir    string

	mu     sync.Mutex
	counts map[string]int
	gates  []string
	leaves []string
}

func newAnswerBrain(t *testing.T) *answerBrain {
	t.Helper()
	brain := &answerBrain{t: t, dir: t.TempDir(), counts: map[string]int{}}
	brain.server = httptest.NewServer(http.HandlerFunc(brain.serve))
	t.Setenv("AFORGE_BASE_URL", brain.server.URL)
	t.Setenv("OPENROUTER_API_KEY", "test-key")
	t.Setenv("AFORGE_PROFILE_DIR", brain.dir)
	t.Setenv("AFORGE_DAILY_BUDGET", "0")
	t.Setenv("AFORGE_PRACTICE_BUDGET", "0")
	t.Setenv("AFORGE_PLAN_CONSENT", "0")
	return brain
}

func (b *answerBrain) close() { b.server.Close() }

func (b *answerBrain) client(settings config.Config, model string) (*liveClient, error) {
	panel, err := router.New(router.Panel{Models: []router.Spec{{Slug: model, Price: 0.01}}},
		provider.Config{APIKey: "test-key", BaseURL: b.server.URL}, b.dir)
	if err != nil {
		return nil, err
	}
	settings.Model = model
	return &liveClient{settings: settings, model: model, client: panel}, nil
}

func (b *answerBrain) tally(name string) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.counts[name]++
	return b.counts[name]
}

func (b *answerBrain) count(name string) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.counts[name]
}

// gatePrompt returns the body of the nth gate call, one-indexed.
func (b *answerBrain) gatePrompt(round int) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if round <= 0 || round > len(b.gates) {
		return ""
	}
	return b.gates[round-1]
}

// leafPrompt returns the first worker call carrying a fragment, which is how the
// product itself tells its three workers apart.
func (b *answerBrain) leafPrompt(fragment string) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, body := range b.leaves {
		if strings.Contains(body, fragment) {
			return body
		}
	}
	return ""
}

func (b *answerBrain) serve(writer http.ResponseWriter, request *http.Request) {
	if !strings.HasSuffix(request.URL.Path, "/chat/completions") {
		http.Error(writer, `{"error":"no"}`, http.StatusNotFound)
		return
	}
	raw, err := io.ReadAll(request.Body)
	if err != nil {
		http.Error(writer, `{"error":"unreadable"}`, http.StatusBadRequest)
		return
	}
	writer.Header().Set("Content-Type", "application/json")
	fmt.Fprint(writer, b.reply(string(raw)))
}

func (b *answerBrain) reply(body string) string {
	switch {
	case strings.Contains(body, "You are the intent compiler"):
		b.tally("compile")
		return b.say(`{"goal":"Look up the two filings and report the revenue figures with a citation.",` +
			`"scale":"task","builds_on":[],"assumptions":[],"question":"","trial_of":0}`)

	case strings.Contains(body, "You write the working method for one agent"):
		b.tally("contract")
		return b.say(`{"contract":"Read both filings before writing anything. Done means the figures and the source are in the answer."}`)

	case strings.Contains(body, "You name jobs for a narrow task list"):
		b.tally("title")
		return b.say("Revenue figures from filings")

	case strings.Contains(body, "You break a goal into its ordered stages"),
		strings.Contains(body, "settled points"):
		// Refused, which proves the documented fallback: one fresh worker on the
		// remainder rather than a plan.
		b.tally("replan")
		return b.say("no plan today")

	case strings.Contains(body, "You are the final gate"):
		b.mu.Lock()
		b.gates = append(b.gates, body)
		round := len(b.gates)
		b.counts["gate"] = round
		b.mu.Unlock()
		if round <= 2 {
			// The judge's own words, standing in for what the real one produced
			// on this shape in the benchmark trace: the missing element is the
			// content, and it is quoted from the person's own sentence.
			return b.say(fmt.Sprintf(
				`{"pass":false,"gaps":"the revenue figures and the citation are not here — the reply says only what it would go and look up, and nothing was looked up","quote":%q,"exercised":false}`,
				answerQuote))
		}
		return b.say(`{"pass":true,"gaps":"","quote":"","exercised":true}`)

	case strings.Contains(body, "You judge whether a finished job taught"):
		b.tally("distill")
		return b.say(`{"facts":[]}`)

	case strings.Contains(body, "You complete one piece of work, alone, using tools"):
		return b.leaf(body)
	}
	b.tally("other")
	return b.say("{}")
}

// leaf answers as the worker, and calls no tools in any of its three lives —
// which is the condition under test on the first two and merely convenient on
// the third.
func (b *answerBrain) leaf(body string) string {
	b.mu.Lock()
	b.leaves = append(b.leaves, body)
	b.mu.Unlock()
	switch {
	case strings.Contains(body, "Finish work a previous agent started"):
		b.tally("extension")
		return b.say(answerFigures)
	case strings.Contains(body, "A reviewer compared the previous attempt"):
		b.tally("revision")
		// The second pass repeats the failure, which is what sends the cited gap
		// on to buy the work that actually closes it.
		return b.say(planShapedDraft + " I am starting on the first one now.")
	default:
		b.tally("draft")
		return b.say(planShapedDraft)
	}
}

func (b *answerBrain) say(content string) string {
	encoded, _ := json.Marshal(content)
	return fmt.Sprintf(`{"model":"scripted","choices":[{"index":0,"finish_reason":"stop",`+
		`"message":{"role":"assistant","content":%s}}],`+
		`"usage":{"prompt_tokens":10,"completion_tokens":10,"total_tokens":20,"cost":0}}`, string(encoded))
}
