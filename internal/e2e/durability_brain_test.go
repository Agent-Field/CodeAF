//go:build e2e

package e2e

// THE SCRIPTED MODEL THE DURABILITY RUNS DRIVE THE REAL BINARY WITH.
//
// A kill is only repeatable when the moment of the kill is known, so the model
// is a script that also keeps the clock: it knows when each tool call finished
// (the next request arrives carrying that call's result, and the seat seals a
// call before it hands the result back) and when each call was started (the
// instant it sent the call). The runs read those two clocks and nothing else to
// decide when to kill.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

const durabilityModel = "stub/scripted"

// brainStep is one turn of the script: wait, then call bash with cmd. An empty
// cmd is the end of the script: the model waits `wait` and then answers in words.
type brainStep struct {
	wait time.Duration
	cmd  string
}

// forever is a wait no run outlives: the model that has nothing more to say yet.
const forever = time.Hour

// brain is the scripted endpoint and its two clocks, both indexed by the count
// of tool results the request carried.
type brain struct {
	server *httptest.Server
	script []brainStep

	mu       sync.Mutex
	finished map[int]time.Time // results carried -> when the request that carried them arrived
	started  map[int]time.Time // results carried -> when the answer to that request began
	done     chan struct{}
}

func newBrain(t *testing.T, script ...brainStep) *brain {
	t.Helper()
	b := &brain{script: script, finished: map[int]time.Time{}, started: map[int]time.Time{}, done: make(chan struct{})}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/models", b.models)
	mux.HandleFunc("/api/v1/chat/completions", b.complete)
	b.server = httptest.NewServer(mux)
	t.Cleanup(func() { close(b.done); b.server.CloseClientConnections(); b.server.Close() })
	return b
}

func (b *brain) url() string { return b.server.URL + "/api/v1" }

func (b *brain) models(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = fmt.Fprintf(w, `{"data":[{"id":%q,"canonical_slug":%q,"name":"Scripted stub","context_length":200000,
		"architecture":{"input_modalities":["text"],"output_modalities":["text"]},
		"pricing":{"prompt":"0","completion":"0","request":"0","input_cache_read":"0"},
		"supported_parameters":["tools","tool_choice","max_tokens"]}]}`, durabilityModel, durabilityModel)
}

// request is the part of a completion request the script reads.
type request struct {
	Messages []struct {
		Role    string `json:"role"`
		Content any    `json:"content"`
	} `json:"messages"`
	Tools []json.RawMessage `json:"tools"`
}

func (r request) results() int {
	n := 0
	for _, m := range r.Messages {
		if strings.EqualFold(m.Role, "tool") {
			n++
		}
	}
	return n
}

// probeWords is the one message the script answers: the conversation's own turn.
// Every other request that carries tools (the errands the chat runs for itself,
// a task's worker) is answered in words, so only one agent follows the script.
const probeWords = "run the probe"

// mine says whether the request is the conversation's own turn: the first
// message the person sent is the probe.
func (r request) mine() bool {
	for _, m := range r.Messages {
		if m.Role == "user" {
			text, _ := m.Content.(string)
			return strings.Contains(text, probeWords)
		}
	}
	return false
}

// complete answers an errand with no tools (a title, say) at once and in words,
// and the conversation's own turn from the script.
func (b *brain) complete(w http.ResponseWriter, r *http.Request) {
	var req request
	_ = json.NewDecoder(r.Body).Decode(&req)
	if len(req.Tools) == 0 || !req.mine() {
		writeWords(w, "ok")
		return
	}
	n := req.results()
	b.note(b.finished, n)
	step, ok := b.stepAt(n)
	if !ok {
		writeWords(w, "done")
		return
	}
	select {
	case <-time.After(step.wait):
	case <-r.Context().Done():
		return
	case <-b.done:
		return
	}
	b.note(b.started, n)
	if step.cmd == "" {
		writeWords(w, "done")
		return
	}
	writeBash(w, n, step.cmd)
}

func (b *brain) stepAt(n int) (brainStep, bool) {
	if n >= len(b.script) {
		return brainStep{}, false
	}
	return b.script[n], true
}

// note keeps the first time a count was seen: a retry of the same request is not
// a second completion.
func (b *brain) note(into map[int]time.Time, n int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := into[n]; !ok {
		into[n] = time.Now()
	}
}

// finishedAt waits for the call that made the carried count n, and answers when
// it finished. ok is false when it never did within d.
func (b *brain) finishedAt(n int, d time.Duration) (time.Time, bool) {
	return b.await(b.finished, n, d)
}

// startedAt waits for the call number n+1 to begin, and answers when it did.
func (b *brain) startedAt(n int, d time.Duration) (time.Time, bool) { return b.await(b.started, n, d) }

func (b *brain) await(from map[int]time.Time, n int, d time.Duration) (time.Time, bool) {
	for deadline := time.Now().Add(d); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		b.mu.Lock()
		at, ok := from[n]
		b.mu.Unlock()
		if ok {
			return at, true
		}
	}
	return time.Time{}, false
}

// finishTimes is every count that has been seen, with when, oldest first by count.
func (b *brain) finishTimes() map[int]time.Time {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make(map[int]time.Time, len(b.finished))
	for n, at := range b.finished {
		out[n] = at
	}
	return out
}

// completed is how many calls have returned to the model: the most results any
// request has carried.
func (b *brain) completed() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	most := 0
	for n := range b.finished {
		most = max(most, n)
	}
	return most
}

// mark is the command of call number i: it leaves a file and prints a word, so
// the tree and the transcript each carry one fact per call.
func mark(i int) string {
	return fmt.Sprintf("echo MARK-%d > m%d.txt && echo MARK-%d", i, i, i)
}

// marks is count quick calls, wait apart, then the end of the script.
func marks(count int, wait time.Duration) []brainStep {
	var steps []brainStep
	for i := 1; i <= count; i++ {
		steps = append(steps, brainStep{wait: wait, cmd: mark(i)})
	}
	return append(steps, brainStep{wait: forever})
}

func openStream(w http.ResponseWriter) (func(string), bool) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "the script needs a flushable writer", http.StatusInternalServerError)
		return nil, false
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	return func(payload string) { _, _ = fmt.Fprintf(w, "data: %s\n\n", payload); flusher.Flush() }, true
}

func chunk(delta, reason string) string {
	if reason == "" {
		reason = "null"
	} else {
		reason = fmt.Sprintf("%q", reason)
	}
	return fmt.Sprintf(`{"id":"script","object":"chat.completion.chunk","created":%d,"model":%q,"provider":"stub",`+
		`"choices":[{"index":0,"delta":%s,"finish_reason":%s}],`+
		`"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2,"cost":0}}`,
		time.Now().Unix(), durabilityModel, delta, reason)
}

func writeWords(w http.ResponseWriter, words string) {
	send, ok := openStream(w)
	if !ok {
		return
	}
	send(chunk(`{"role":"assistant","content":""}`, ""))
	send(chunk(fmt.Sprintf(`{"content":%q}`, words), ""))
	send(chunk(`{}`, "stop"))
	send("[DONE]")
}

func writeBash(w http.ResponseWriter, n int, command string) {
	send, ok := openStream(w)
	if !ok {
		return
	}
	args, _ := json.Marshal(map[string]string{"command": command})
	quoted, _ := json.Marshal(string(args))
	send(chunk(`{"role":"assistant","content":""}`, ""))
	send(chunk(fmt.Sprintf(`{"tool_calls":[{"index":0,"id":"call_%d","type":"function","function":{"name":"bash","arguments":%s}}]}`, n, quoted), ""))
	send(chunk(`{}`, "tool_calls"))
	send("[DONE]")
}
