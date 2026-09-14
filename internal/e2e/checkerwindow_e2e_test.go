//go:build e2e

package e2e

// A CHECK THAT RAN OUT OF TIME, ON A REAL SCREEN, AGAINST A MODEL THAT THINKS
// FOREVER.
//
// This is issue #941's own acceptance, and it is the subtest the fix is not
// finished without: five of thirty-four landings in one acceptance drive read
// `your call` on work that was correct, because the checker's model call was
// cut at its thirty-second share of the reading window while a reasoning model
// thought without writing a word — twice, the second time from nothing, because
// the cut threw away everything the first had read.
//
// IT IS SCRIPTED AND IT NEEDS NO KEY, which is what makes it a gate rather than
// a drive. The whole subject is a model that never answers on its first call,
// and no real model can be asked to be that reliably: the stub below answers the
// checker's first request with a read, its second with thinking that does not
// stop until the client goes away, and the ask that follows the cut with the
// word. Everything between those three answers is the product.
//
// WHAT IT ASSERTS IS THE WIRE AND THE SCREEN TOGETHER, because #941 is a defect
// about both and the same family was unit-green and e2e-broken for a week
// (#184). On the screen: the landing says `done`, and never `your call`. On the
// wire: the cut call carried a thinking budget the window implies rather than
// nothing at all, and the ask after the cut carried what the checker had already
// read, with its thinking pass turned down as far as the model allows.
//
//	go test -tags e2e -count=1 -run 'TestTaskStatesE2E/a_check' -v ./internal/e2e/

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
)

const (
	// The model is spelled author/slug because the sheet lives under that
	// shape: `/api/v1/models/stub/checker/endpoints` is the page the lane
	// belief is primed from, and a budget is derived from a believed rate or
	// not at all (internal/provider's believedRate).
	checkStubModel = "stub/checker"
	checkStubLane  = "StubWorks"
	// What the sheet says this one machine does: a median of a hundred tokens a
	// second and a fastest reading of a hundred and thirty. The second figure is
	// the ceiling the budget assertion is made against — the most generous
	// reading of what one share of the window can hold.
	checkStubRate    = 100.0
	checkStubTopRate = 130.0
	// The share one call may hold: half of the reading window a check with
	// nothing runnable gets (internal/session's auditReadingDeadline over
	// auditCallShare, a minute cut in two). It is spelled here because the
	// engine's own constants are unexported, and it is only ever used as a
	// CEILING, so a share that later grows is the one direction this could go
	// stale in.
	checkShare = 30 * time.Second
	// The work, and the word the checker reads back out of the file it wrote.
	// The mark is what makes "the second ask carried what the first one read" a
	// fact about the request body rather than a shape.
	checkStubFile  = "hello.txt"
	checkStubMark  = "HELLO-941"
	checkStubBrief = "write a file called " + checkStubFile + " containing the word " + checkStubMark
	// The first words of internal/session's auditPrompt, which is how a request
	// from the checker is told from a request from the worker. It is the one
	// string this file shares with the engine, and it is the engine's own
	// sentence rather than a person-facing one.
	checkAuditorMark = "You are an AUDITOR"
)

// testStatesCheckRanOut is the subtest [TestTaskStatesE2E] registers.
func testStatesCheckRanOut(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("no tmux on PATH: this test drives the real binary in a real terminal")
	}
	brain := startCheckerBrain(t)
	// EVERY ROW THIS RUN DEPENDS ON IS PINNED, and three of them are pinned
	// against the person's own profile rather than against a default. A profile
	// with `effort` dialled stamps a rung on every call, and a stamped rung is a
	// stated depth the wall does not size (internal/provider's wallBudget); a
	// profile with routing off runs no lane beat, so no sheet is fetched, no
	// belief is primed and no budget can be derived from anything; a profile
	// with the task column stowed hides the roster this reads the landing off.
	home := newHome(t, map[string]any{
		"model.talk": checkStubModel, "model.work": checkStubModel, "model.plan": checkStubModel,
		config.KeyTierWorkerModel: checkStubModel,
		config.KeyTierHighModel:   checkStubModel,
		config.KeyTierLowModel:    checkStubModel,
		"tools.approvalMode":      "allow",
		"daily_budget_usd":        0,
		config.KeyEffort:          "auto",
		config.KeyRouting:         config.RoutingLatency,
		config.KeyTaskColumn:      true,
	})
	ws := newWorkspace(t, "checkerws", false)
	// The key is a string and not a secret: config.Load refuses to build a
	// session with none at all, and nothing behind this endpoint checks it.
	r := startWithEnv(t, []string{
		config.APIKeyEnv + "=stub-key",
		"AFORGE_BASE_URL=" + brain.server.URL + "/api/v1",
	}, "afe2e_states_checkran", home, ws, tuiWide, 40)
	statesPastTheDoor(t, r)

	// ONE WORKER AND NO SIZING CALL, which is what `solo` means, and a brief
	// that declares no repeatable check — so the checking window is the reading
	// one and a call's share of it is the thirty seconds #941 was cut at.
	r.lit("/task solo " + checkStubBrief)
	r.keys("Enter")

	// THE CARD IS THE WAIT. Four minutes is the worker's turn, the checker's
	// read, the whole thirty-second share thought away, and the ask after it.
	screen := r.waitFor(4*time.Minute, say(t, "taskDoneGlyph"), say(t, "taskDoneWord"))
	t.Logf("the landing card of work whose check ran out of time:\n%s", screen)

	head := statesHeadLine(screen, say(t, "taskDoneGlyph"), say(t, "taskDoneWord"))
	if head == "" {
		t.Fatalf("nothing on the screen is a head row carrying both %q and %q — work that was checked "+
			"after its first call was cut did not land done:\n%s",
			say(t, "taskDoneGlyph"), say(t, "taskDoneWord"), screen)
	}
	t.Logf("HEAD · %s", head)
	// AND THE WORD THE DEFECT PUT THERE IS NOT ON THE SCREEN. `your call` on
	// correct work is the whole of #941, and every sentence this run could write
	// is the stub's, so nothing else on this page can say it by accident.
	if strings.Contains(screen, say(t, "taskLookWord")) {
		t.Errorf("the screen says %q about work a checker verified after its first call was cut:\n%s",
			say(t, "taskLookWord"), screen)
	}
	statesNoDeletedWords(t, screen)

	// ── and what actually went out on the wire ──────────────────────────────

	// ONE INVESTIGATION AND NOT TWO. A fresh checker would arrive with an empty
	// transcript and read the file again, so the count of read requests is the
	// whole of "the cut checker was asked for its word rather than replaced" —
	// which is the defect #941 is about, and the one this cannot pass by luck.
	brain.only(t, checkAskRead)
	// THE CUT CALL MAY BE ANSWERED MORE THAN ONCE, and that is the turn loop and
	// not the ladder: a stream the deadline killed is a dead stream from above,
	// re-opened once inside the same window and cut again at once on a context
	// that has already expired. The first of them is the call that really ran.
	cuts := brain.all(checkAskCut)
	if len(cuts) == 0 {
		t.Fatalf("the checker never reached a thinking call; the endpoint saw %v", brain.kinds())
	}
	cut := cuts[0]
	if cut.finished {
		t.Errorf("the checker's second call was never cut: the stub wrote thinking until its own cap " +
			"rather than until the client went away, so nothing here is about a window")
	}
	// THE BUDGET IS THE TOLD WINDOW, IN TOKENS. Nothing said how long the call
	// had before this wave, and a model at the top of its own ladder thought
	// through the whole share (#941). The ceiling is the share at the fastest
	// rate the sheet claims for this machine: a budget above it is a call told
	// about a window that is not its own.
	ceiling := int(checkStubTopRate * checkShare.Seconds())
	switch {
	case cut.budget == 0:
		t.Errorf("the checker's call carried no thinking budget (reasoning: %s); the window it was "+
			"opened under was never told", cut.reasoning)
	case cut.budget > ceiling:
		t.Errorf("the checker's call was told %d thinking tokens, and one share of the window holds at "+
			"most %d at this machine's fastest published rate", cut.budget, ceiling)
	default:
		t.Logf("the cut call was told %d thinking tokens of the %d its share can hold", cut.budget, ceiling)
	}

	// THE ASK AFTER THE CUT IS THE SAME CHECKER, OVER WHAT IT READ. The mark is
	// in that body because the read's own result is still in the transcript the
	// second ask is sent with — a fresh checker would carry the question and
	// nothing else.
	answer := brain.only(t, checkAskAnswer)
	if !strings.Contains(answer.raw, checkStubMark) {
		t.Errorf("the ask after the cut does not carry what the checker had already read (%q is not in "+
			"its request body); the cut threw the reading away", checkStubMark)
	}
	// AND ITS THINKING PASS IS NOT PAID FOR TWICE. The ask is for the word over
	// evidence already read, so the call switches thinking off — and `off` on a
	// model that cannot stop thinking is the lowest word it publishes
	// (internal/provider's lowestEffort), never a budget.
	if answer.budget != 0 {
		t.Errorf("the ask after the cut carried a thinking budget of %d tokens; it is an answer ask over "+
			"evidence, not a second investigation", answer.budget)
	}
	if answer.effort != checkStubLowestEffort {
		t.Errorf("the ask after the cut asked for %q thinking; the lowest word this model publishes is %q",
			answer.effort, checkStubLowestEffort)
	}
	r.quit()
}

// ── the scripted endpoint ───────────────────────────────────────────────────

// The four kinds of request this run makes, told apart by the shape of the body
// rather than by a counter, because the conversation's own calls come and go
// around the node's and a count would be about the order they happened in.
const (
	checkAskWorker = "worker"
	checkAskRead   = "read"
	checkAskCut    = "cut"
	checkAskAnswer = "answer"
)

// checkStubLowestEffort is the lowest word [checkStubBrain] publishes for this
// model, and so what a call that asked for thinking OFF actually carries.
const checkStubLowestEffort = "low"

// checkAsk is one request this endpoint answered.
type checkAsk struct {
	kind      string
	raw       string
	budget    int
	effort    string
	reasoning string
	// finished says the answer was written to the end. The forever-thinking
	// stream sets it only when its own cap ran out, which is the case where
	// nothing cut the call and the test is about nothing.
	finished bool
}

// checkerBrain stands in for every model call this run makes, and records what
// each one arrived with.
type checkerBrain struct {
	server *httptest.Server
	mu     sync.Mutex
	asks   []checkAsk
}

// only is the one request of a kind, and a failure when there was not exactly
// one — two investigations mean the cut checker was replaced rather than asked,
// which is the defect, and none means the run never got that far.
func (b *checkerBrain) only(t *testing.T, kind string) checkAsk {
	t.Helper()
	found := b.all(kind)
	if len(found) != 1 {
		t.Fatalf("the endpoint answered %d %s requests, want exactly one; it saw %v",
			len(found), kind, b.kinds())
	}
	return found[0]
}

// all is every request of a kind, oldest first.
func (b *checkerBrain) all(kind string) []checkAsk {
	b.mu.Lock()
	defer b.mu.Unlock()
	var found []checkAsk
	for _, ask := range b.asks {
		if ask.kind == kind {
			found = append(found, ask)
		}
	}
	return found
}

// kinds is the whole run in order, for a failure that has to say what did happen.
func (b *checkerBrain) kinds() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	kinds := make([]string, 0, len(b.asks))
	for _, ask := range b.asks {
		kinds = append(kinds, ask.kind)
	}
	return kinds
}

func (b *checkerBrain) record(ask checkAsk) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.asks = append(b.asks, ask)
}

// finish marks the last recorded ask as one whose answer was written to the end.
func (b *checkerBrain) finish() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.asks) > 0 {
		b.asks[len(b.asks)-1].finished = true
	}
}

// startCheckerBrain serves the three pages a run needs: the catalog, so the
// adapter knows this model thinks whether or not it is asked to; the endpoints
// page, so the lane belief holds a rate for the machine and a wall can be turned
// into a number of tokens at all; and the completions route, which is the
// script.
func startCheckerBrain(t *testing.T) *checkerBrain {
	t.Helper()
	brain := &checkerBrain{}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/models", brain.serveCatalog)
	mux.HandleFunc("GET /api/v1/models/{author}/{slug}/endpoints", brain.serveSheet)
	mux.HandleFunc("POST /api/v1/chat/completions", brain.serveCompletion)
	brain.server = httptest.NewServer(mux)
	t.Cleanup(brain.server.Close)
	return brain
}

// serveCatalog publishes ONE MODEL THAT CANNOT STOP THINKING, which is the model
// #941 happened on: `mandatory` is the provider's own word for a row whose
// thinking pass has no off switch, and `default_effort` is where it sits when
// nobody sends a level — the top of its ladder. A request that states no rung is
// therefore a request whose pass only the wall can bound, which is the case this
// whole subtest is about.
func (b *checkerBrain) serveCatalog(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = fmt.Fprintf(w, `{"data":[{"id":%q,"canonical_slug":%q,"name":"Scripted checker stub",
		"context_length":200000,
		"architecture":{"input_modalities":["text"],"output_modalities":["text"]},
		"pricing":{"prompt":"0","completion":"0","request":"0","input_cache_read":"0"},
		"supported_parameters":["tools","tool_choice","max_tokens","reasoning","reasoning_effort","include_reasoning"],
		"reasoning":{"mandatory":true,"default_enabled":true,
			"supported_efforts":[%q,"medium","high"],"default_effort":"high"}}]}`,
		checkStubModel, checkStubModel, checkStubLowestEffort)
}

// serveSheet is the endpoints page, in the router's own field names. One machine,
// with a published median and spread: the belief is primed from it on the lane
// beat's first pass, and it is that belief the adapter turns a told window into
// a token budget with.
func (b *checkerBrain) serveSheet(w http.ResponseWriter, r *http.Request) {
	model := r.PathValue("author") + "/" + r.PathValue("slug")
	w.Header().Set("Content-Type", "application/json")
	if model != checkStubModel {
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"error":{"message":"No endpoints found for that model","code":404}}`)
		return
	}
	_, _ = fmt.Fprintf(w, `{"data":{"id":%q,"name":%q,"endpoints":[{
		"provider_name":%q,"tag":"stubworks","quantization":"fp8",
		"context_length":200000,"max_completion_tokens":8000,
		"pricing":{"prompt":"0","completion":"0","input_cache_read":"0"},
		"supports_tool_choice":{"function":true,"auto":true,"none":true,"required":true},
		"status":0,"uptime_last_30m":100,"uptime_last_5m":100,"uptime_last_1d":100,
		"supports_implicit_caching":false,
		"latency_last_30m":{"p50":300,"p75":360,"p90":440,"p99":700},
		"throughput_last_30m":{"p50":%.0f,"p75":110,"p90":120,"p99":%.0f}}]}}`,
		model, model, checkStubLane, checkStubRate, checkStubTopRate)
}

// serveCompletion is the script, and it is four rules read off the body.
//
// THE CHECKER IS TOLD FROM THE WORKER BY ITS SYSTEM PROMPT, and the checker's
// three calls are told apart by what its own transcript already holds: nothing
// yet is the investigation, a tool result is the pass that thinks, and a SECOND
// user turn is the ask that follows the cut — the engine sends exactly one
// sentence after a cut call, over the transcript the cut checker already had
// ([Agent.askForTheWord]). Counting turns rather than matching that sentence
// keeps this file from holding a second copy of it.
func (b *checkerBrain) serveCompletion(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	var body struct {
		Messages []struct {
			Role string `json:"role"`
		} `json:"messages"`
		Tools []struct {
			Function struct {
				Name string `json:"name"`
			} `json:"function"`
		} `json:"tools"`
		Reasoning json.RawMessage `json:"reasoning"`
	}
	_ = json.Unmarshal(raw, &body)
	ask := checkAsk{raw: string(raw), reasoning: string(body.Reasoning)}
	if len(body.Reasoning) > 0 {
		var knob struct {
			Effort    string `json:"effort"`
			MaxTokens int    `json:"max_tokens"`
		}
		_ = json.Unmarshal(body.Reasoning, &knob)
		ask.budget, ask.effort = knob.MaxTokens, knob.Effort
	}
	asked, ranSomething := 0, false
	for _, message := range body.Messages {
		switch strings.ToLower(message.Role) {
		case "user":
			asked++
		case "tool":
			ranSomething = true
		}
	}
	offers := func(name string) bool {
		for _, tool := range body.Tools {
			if tool.Function.Name == name {
				return true
			}
		}
		return false
	}

	switch {
	case strings.Contains(ask.raw, checkAuditorMark) && asked > 1:
		ask.kind = checkAskAnswer
		b.record(ask)
		b.say(w, fmt.Sprintf("VERIFIED — %s is in %s, which is what the work was asked for", checkStubMark, checkStubFile))
	case strings.Contains(ask.raw, checkAuditorMark) && ranSomething:
		ask.kind = checkAskCut
		b.record(ask)
		b.thinkUntilTheClientGoesAway(r, w)
	case strings.Contains(ask.raw, checkAuditorMark):
		ask.kind = checkAskRead
		b.record(ask)
		b.call(w, "read", map[string]string{"path": checkStubFile})
	case offers("write") && !ranSomething:
		ask.kind = checkAskWorker
		b.record(ask)
		b.call(w, "write", map[string]string{"path": checkStubFile, "content": checkStubMark + "\n"})
	default:
		ask.kind = checkAskWorker
		b.record(ask)
		b.say(w, "Wrote "+checkStubFile+" with "+checkStubMark+" in it.")
	}
}

// say answers in words, and call answers with one tool call. Both are
// internal/provider's sse.go read backwards.
func (b *checkerBrain) say(w http.ResponseWriter, text string) {
	send, ok := b.open(w)
	if !ok {
		return
	}
	quoted, _ := json.Marshal(text)
	send(fmt.Sprintf(`{"content":%s}`, quoted), "")
	send(`{}`, "stop")
	send("", "[DONE]")
	b.finish()
}

func (b *checkerBrain) call(w http.ResponseWriter, name string, args map[string]string) {
	send, ok := b.open(w)
	if !ok {
		return
	}
	encoded, _ := json.Marshal(args)
	quoted, _ := json.Marshal(string(encoded))
	send(fmt.Sprintf(`{"tool_calls":[{"index":0,"id":"call_%s","type":"function",`+
		`"function":{"name":%q,"arguments":%s}}]}`, name, name, quoted), "")
	send(`{}`, "tool_calls")
	send("", "[DONE]")
	b.finish()
}

// thinkUntilTheClientGoesAway is the model #941 is about: a pass that writes
// billed, streamed thinking and never reaches a word. It ends when the caller's
// own window cuts the request, which is the event this whole subtest exists to
// stage — and it holds a cap of its own so a build that never cuts fails as a
// test rather than hanging as one.
func (b *checkerBrain) thinkUntilTheClientGoesAway(r *http.Request, w http.ResponseWriter) {
	send, ok := b.open(w)
	if !ok {
		return
	}
	beat := time.NewTicker(200 * time.Millisecond)
	defer beat.Stop()
	stopAt := time.After(3 * time.Minute)
	for {
		select {
		case <-r.Context().Done():
			return
		case <-stopAt:
			b.finish()
			return
		case <-beat.C:
			send(`{"reasoning":"still weighing what the change does…"}`, "")
		}
	}
}

// open writes the stream's head and hands back the frame writer.
func (b *checkerBrain) open(w http.ResponseWriter) (func(delta, reason string), bool) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "the script needs a flushable writer", http.StatusInternalServerError)
		return nil, false
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	id := fmt.Sprintf("check-%d", time.Now().UnixNano())
	send := func(delta, reason string) {
		if reason == "[DONE]" {
			_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
			flusher.Flush()
			return
		}
		finish := "null"
		if reason != "" {
			finish = fmt.Sprintf("%q", reason)
		}
		_, _ = fmt.Fprintf(w, "data: {\"id\":%q,\"object\":\"chat.completion.chunk\",\"created\":%d,"+
			"\"model\":%q,\"provider\":%q,\"choices\":[{\"index\":0,\"delta\":%s,\"finish_reason\":%s}],"+
			"\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":1,\"total_tokens\":2,\"cost\":0}}\n\n",
			id, time.Now().Unix(), checkStubModel, checkStubLane, delta, finish)
		flusher.Flush()
	}
	send(`{"role":"assistant","content":""}`, "")
	return send, true
}
