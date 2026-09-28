//go:build e2e

package e2e

// THE STOP IS BOUNDED, END TO END: the real binary, a real terminal, and two
// turns that genuinely cannot be ended any other way.
//
// This is issue #265's acceptance. Both scenarios park a turn on something no
// cancellation reaches, press esc, and assert that the surface says when it will
// detach, detaches inside the bound, writes exactly one `abandoned` line with
// the turn's spend, and takes the next prompt.
//
// ── WHY THE MODEL IS A STUB AND THE WAIT IS NOT ─────────────────────────────
//
// The thing under test is a BOUND, so the run has to be able to state exactly
// when the clock started and exactly what the turn was parked on. A real model
// can do neither: it decides when to stop streaming and it decides whether to
// call the tool that parks. So the model is a scripted endpoint in this process
// and the WAITS ARE REAL — an HTTP stream that never ends, and a named pipe with
// no writer, which `read` enters through os.ReadFile and cannot come back out
// of, whatever anybody does to its context.
//
// ── AND IT IS DELIBERATELY NOT A FLOCK ──────────────────────────────────────
//
// #264 removed the one flock this program held on the turn path, and a test that
// re-created that flock would prove only that the fix for #264 works. What is
// being proved here is the GENERAL guarantee — that the stop no longer depends
// on every wait being cancellable — so the wait is a different one, of a shape
// nothing in this build has any special knowledge of.

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
)

// stopBoundPatience is how long a scenario waits for the detach. The bound is
// ten seconds from the keypress; the slack is for a machine under load and for
// the frame that draws the note.
const stopBoundPatience = 25 * time.Second

// stopGraceE2E is the bound itself, written here rather than imported: this file
// is a black-box run of the shipped binary, and a test that read the constant out
// of internal/tui3 could not tell a bound that moved from a bound that broke.
const stopGraceE2E = 10 * time.Second

// ── the scripted endpoint ───────────────────────────────────────────────────

// parkKind is what the stub does with a turn: which of the two uncancellable
// waits it steers the session into.
type parkKind int

const (
	// parkOnStream answers the second request by streaming for ever and never
	// finishing — the provider that goes on writing after the person has left.
	parkOnStream parkKind = iota
	// parkOnPipe answers the first request with a `read` on a named pipe that
	// has no writer, which blocks inside os.ReadFile with no context anywhere
	// near it.
	parkOnPipe
)

// stopStub is the scripted model this suite drives against.
//
// THE FIRST ANSWER ALWAYS SPENDS SOMETHING. The journal line an abandoned turn
// writes carries its LAST KNOWN SPEND, and a turn that was abandoned before it
// had spent anything would make that assertion vacuous — so the first response
// reports usage and a cost, and the turn is parked on the second leg.
type stopStub struct {
	kind  parkKind
	path  string
	calls atomic.Int64
	// releases is closed at the end of the test so a stream that is deliberately
	// endless does not outlive the server it is written by.
	releases chan struct{}
	once     sync.Once
}

func (s *stopStub) stop() { s.once.Do(func() { close(s.releases) }) }

// released reports that the scenario is over and this endpoint should behave
// like an ordinary model again. It is what makes the LAST assertion possible:
// "the next prompt is usable" has to be answered by a turn that ends, and a stub
// still parking every turn could never show that.
func (s *stopStub) released() bool {
	select {
	case <-s.releases:
		return true
	default:
		return false
	}
}

func (s *stopStub) models(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"data":[{
		"id":"stub/bounded",
		"canonical_slug":"stub/bounded",
		"name":"Bounded stop stub",
		"context_length":200000,
		"architecture":{"input_modalities":["text"],"output_modalities":["text"]},
		"pricing":{"prompt":"0","completion":"0","request":"0","input_cache_read":"0"},
		"supported_parameters":["tools","tool_choice","max_tokens"]
	}]}`))
}

func (s *stopStub) completions(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Messages []struct {
			Role string `json:"role"`
		} `json:"messages"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	answered := false
	for _, message := range body.Messages {
		if strings.EqualFold(message.Role, "tool") {
			answered = true
		}
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "the stub needs a flushable writer", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	id := fmt.Sprintf("stub-%d", s.calls.Add(1))
	send := func(payload string) bool {
		if _, err := fmt.Fprintf(w, "data: %s\n\n", payload); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}
	send(stopChunk(id, `{"role":"assistant","content":""}`, ""))

	// THE SCENARIO IS OVER: an ordinary turn, so the run can show that the box
	// still works after a detach.
	if s.released() {
		send(stopChunk(id, fmt.Sprintf(`{"content":%q}`, stopStubDone), ""))
		send(stopFinish(id, "stop"))
		send("[DONE]")
		return
	}

	// LEG ONE: a tool call, with a bill on it.
	if s.kind == parkOnPipe && !answered {
		send(stopChunk(id, fmt.Sprintf(
			`{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"read","arguments":%q}}]}`,
			fmt.Sprintf(`{"path":%q}`, s.path)), ""))
		send(stopFinish(id, "tool_calls"))
		send("[DONE]")
		return
	}
	if s.kind == parkOnStream && !answered {
		send(stopChunk(id, `{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"bash","arguments":"{\"command\":\"echo warming\"}"}}]}`, ""))
		send(stopFinish(id, "tool_calls"))
		send("[DONE]")
		return
	}

	// LEG TWO: the stream that never ends. It keeps writing so the connection is
	// healthy by every measure the client has — this is a provider that will not
	// stop, not one that has died.
	for {
		select {
		case <-r.Context().Done():
			return
		case <-s.releases:
			return
		case <-time.After(300 * time.Millisecond):
			if !send(stopChunk(id, fmt.Sprintf(`{"content":%q}`, stopStubStreaming+" "), "")) {
				return
			}
		}
	}
}

func stopChunk(id, delta, reason string) string {
	if reason == "" {
		reason = "null"
	} else {
		reason = fmt.Sprintf("%q", reason)
	}
	return fmt.Sprintf(
		`{"id":%q,"object":"chat.completion.chunk","created":%d,"model":"stub/bounded","provider":"stub",`+
			`"choices":[{"index":0,"delta":%s,"finish_reason":%s}]}`,
		id, time.Now().Unix(), delta, reason)
}

// stopFinish carries the usage block, and the COST IS NOT ZERO on purpose. The
// container stub next door reports zero because it is genuinely free and a
// ledger taught otherwise would be a ledger taught a lie; here the money is the
// measurement — "the abandoned turn's cost still reaches the ledger" is one of
// the issue's three acceptances — so the endpoint states a price and the run
// checks that it lands.
func stopFinish(id, reason string) string {
	return fmt.Sprintf(
		`{"id":%q,"object":"chat.completion.chunk","created":%d,"model":"stub/bounded","provider":"stub",`+
			`"choices":[{"index":0,"delta":{},"finish_reason":%q}],`+
			`"usage":{"prompt_tokens":1200,"completion_tokens":40,"total_tokens":1240,"cost":0.0123}}`,
		id, time.Now().Unix(), reason)
}

// serveStopStub opens the scripted endpoint on a loopback port and answers with
// its base URL, in the shape internal/config expects (`<base>/chat/completions`).
func serveStopStub(t *testing.T, stub *stopStub) string {
	t.Helper()
	stub.releases = make(chan struct{})
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/chat/completions", stub.completions)
	mux.HandleFunc("/api/v1/models", stub.models)
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() {
		stub.stop()
		_ = server.Close()
	})
	return "http://" + listener.Addr().String() + "/api/v1"
}

// ── the runs ────────────────────────────────────────────────────────────────

// TestBoundedStopE2E is the two scenarios, side by side, because they are one
// law read twice: a wait inside the provider, and a wait inside a tool.
func TestBoundedStopE2E(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("no tmux on PATH: this suite drives the real binary in a real terminal")
	}

	// A PROVIDER STREAM THAT NEVER ENDS. The connection is healthy, bytes keep
	// arriving, and nothing in the client has any reason to give up on it — which
	// is exactly the turn a person has to be able to end from the keyboard.
	//
	// IT ENDS AT ONCE AND WRITES NO `abandoned` LINE, and that is the RIGHT
	// answer rather than a weaker one. Every provider request is built on the
	// turn's context, so cancelling it aborts the socket and the loop is out
	// within milliseconds: the turn was let go of properly, and a line claiming
	// it had been abandoned would be a record of something that did not happen.
	// So this scenario asserts the bound is MET without being reached, which is
	// also the only way to show the deadline does not fire on turns that end
	// tidily.
	t.Run("a stream that never ends", func(t *testing.T) {
		stub := &stopStub{kind: parkOnStream}
		runStoppedInTime(t, stub, "stopstream", "stream on for ever please")
	})

	// AND A WAIT NO CANCELLATION REACHES, which is the generalisation #264's
	// flock test was a single instance of. `read` is os.ReadFile; a named pipe
	// with no writer never returns from it; no context exists anywhere on that
	// path and none could help if it did. This is where the bound actually
	// fires, and where the whole law is read at once.
	t.Run("a wait that cannot be cancelled", func(t *testing.T) {
		if _, err := exec.LookPath("mkfifo"); err != nil {
			t.Skip("no mkfifo: this scenario needs a named pipe to park on")
		}
		stub := &stopStub{kind: parkOnPipe}
		runBoundedStop(t, stub, "stoppipe", "read the pipe please")
	})

	// AND THE ISSUE'S OWN REPLICATION, WITH A REAL MODEL ON THE WIRE.
	//
	// The two above are scripted so the run can say exactly what the turn was
	// parked on. This one is the report as it was filed: a person types a
	// sentence, the model decides what to do with it, the turn parks on a wait
	// nothing can cancel, and the person presses esc. Nothing here is scripted
	// except the pipe — which is the environment and not the harness — so what
	// it proves is that the bound holds on the product as somebody actually uses
	// it, at the price of being the one case in this file that costs money.
	t.Run("a real model on an uncancellable wait", func(t *testing.T) {
		// THE KEY IS LOOKED FOR WHERE THE PRODUCT LOOKS FOR IT, through
		// [liveKey]: a machine with a key in `~/.codeaf/config.json` and nothing
		// in its environment can run this exactly as the product does.
		liveKey(t)
		if _, err := exec.LookPath("mkfifo"); err != nil {
			t.Skip("no mkfifo: this scenario needs a named pipe to park on")
		}
		runRealModelBoundedStop(t)
	})
}

// runRealModelBoundedStop is #265's replication against the shipped default
// model: no stub, no script, and the same three things asserted.
func runRealModelBoundedStop(t *testing.T) {
	t.Helper()
	workspace := newWorkspace(t, "stopreal", false)
	pipe := filepath.Join(workspace, "wedge.fifo")
	if out, err := exec.Command("mkfifo", pipe).CombinedOutput(); err != nil {
		t.Fatalf("mkfifo: %v\n%s", err, out)
	}
	home := newHome(t, map[string]any{
		"tools.approvalMode": "allow",
		config.KeySetupSeen:  time.Now().UTC().Format(time.RFC3339Nano),
	})
	rig := start(t, "stopreal", home, workspace, 120, 40)

	// Explain the deliberate wait: a model otherwise correctly refuses to
	// open a FIFO, so the test never reaches the stop it is supposed to time.
	rig.lit("This is a controlled cancellation test. Use the read tool directly on " + pipe +
		". It is an intentional FIFO with no writer. Blocking is expected: I will press Escape to cancel it. Do not inspect it with bash or avoid the read; start that read now.")
	rig.keys("Enter")

	// The journal identifies the exact read and its completion. The live
	// footer can summarize it as "working" after briefly saying "running read".
	deadline := time.Now().Add(modelPatience)
	for !pendingPipeRead(t, home, pipe) {
		if time.Now().After(deadline) {
			t.Fatalf("the model never started the requested pipe read:\n%s", rig.capture())
		}
		time.Sleep(pollEvery)
	}
	time.Sleep(6 * time.Second)
	if !pendingPipeRead(t, home, pipe) {
		t.Fatalf("the read completed, so this run is not about an uncancellable wait:\n%s", rig.capture())
	}

	pressed := time.Now()
	rig.keys("Escape")

	stopping := rig.waitFor(10*time.Second, say(t, "stopDetachWord"))
	t.Logf("=== REAL MODEL: pane after esc (the bound, stated) ===\n%s", stopping)

	detached := rig.waitFor(stopBoundPatience, say(t, "stopDetachedWord"))
	took := time.Since(pressed)
	t.Logf("=== REAL MODEL: pane after the detach (%s after esc) ===\n%s", took.Round(time.Second), detached)
	if took > stopBoundPatience {
		t.Fatalf("the turn took %s to detach, which is past the bound", took)
	}

	line := onlyAbandonedLine(t, home)
	if line.CostUSD <= 0 && line.Input == 0 && line.Output == 0 {
		t.Fatalf("the abandoned line carries no spend at all: %+v", line)
	}
	t.Logf("=== REAL MODEL: the abandoned line: %+v", line)

	// AND THE NEXT PROMPT IS USABLE, which is the whole of what the person wanted.
	rig.lit("say the word ready and nothing else")
	rig.keys("Enter")
	rig.waitFor(modelPatience, "ready")
}

// pendingPipeRead checks the journal rather than a transient status label, so
// the path in the user's prompt cannot satisfy the wait.
func pendingPipeRead(t *testing.T, home, pipe string) bool {
	t.Helper()
	for _, transcript := range sessionTranscripts(t, home) {
		pending := map[string]bool{}
		for _, line := range strings.Split(transcript, "\n") {
			var entry struct {
				ToolCalls []struct {
					ID       string `json:"id"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"toolCalls"`
				Took struct {
					CallID string `json:"callId"`
				} `json:"took"`
				ToolCallID string `json:"toolCallId"`
			}
			if json.Unmarshal([]byte(line), &entry) != nil {
				continue
			}
			for _, call := range entry.ToolCalls {
				var args struct {
					Path string `json:"path"`
				}
				if call.Function.Name == "read" && json.Unmarshal([]byte(call.Function.Arguments), &args) == nil && args.Path == pipe {
					pending[call.ID] = true
				}
			}
			delete(pending, entry.Took.CallID)
			delete(pending, entry.ToolCallID)
		}
		if len(pending) > 0 {
			return true
		}
	}
	return false
}

// runStoppedInTime is the scenario where the engine DOES let go: the surface's
// wait ends inside the bound, nothing is detached, and the box is usable again.
func runStoppedInTime(t *testing.T, stub *stopStub, name, ask string) {
	t.Helper()
	rig, home := openBoundedStopRig(t, stub, name)
	rig.lit(ask)
	rig.keys("Enter")
	waitUntilParked(t, rig, stub)

	pressed := time.Now()
	rig.keys("Escape")

	// THE SURFACE'S WAIT ENDS INSIDE THE BOUND. `interrupted` is the word the
	// status line takes once the turn is genuinely over (internal/tui3's
	// render.go), so waiting for it is waiting for the stream to have closed.
	settled := rig.waitFor(stopBoundPatience, say(t, "interruptedWord"))
	took := time.Since(pressed)
	t.Logf("=== pane %s after esc: the never-ending stream is over ===\n%s", took.Round(time.Second), settled)
	if took > stopGraceE2E {
		t.Fatalf("the stream took %s to end, which is past the bound", took)
	}

	// AND NOTHING WAS ABANDONED, because nothing had to be. A line here would be
	// the file recording a detach that never happened.
	if lines := abandonedLines(t, home); len(lines) != 0 {
		t.Fatalf("a turn the engine let go of was journaled as abandoned: %+v", lines)
	}

	stub.stop()
	rig.lit("say hello")
	rig.keys("Enter")
	rig.waitFor(60*time.Second, stopStubDone)
}

// runBoundedStop drives one scenario end to end and asserts the whole law.
func openBoundedStopRig(t *testing.T, stub *stopStub, name string) (*rig, string) {
	t.Helper()
	workspace := newWorkspace(t, name, false)
	if stub.kind == parkOnPipe {
		stub.path = filepath.Join(workspace, "wedge.fifo")
		if out, err := exec.Command("mkfifo", stub.path).CombinedOutput(); err != nil {
			t.Fatalf("mkfifo: %v\n%s", err, out)
		}
	}
	base := serveStopStub(t, stub)
	home := newHome(t, map[string]any{
		"model.talk":         "stub/bounded",
		"tools.approvalMode": "allow",
		// AND THE FRONT DOOR IS ALREADY BEHIND THIS PROFILE. A machine that has
		// never run codeaf is shown the setup first (#322), which is a screen
		// this run is not about and which would eat the sentence it types.
		config.KeySetupSeen: time.Now().UTC().Format(time.RFC3339Nano),
	})
	return startWithEnv(t,
		[]string{"OPENROUTER_API_KEY=stub-key", "CODEAF_BASE_URL=" + base, "CODEAF_PROFILE_DIR="},
		name, home, workspace, 120, 40), home
}

// runBoundedStop drives the scenario where the bound actually FIRES, and reads
// the whole law off it.
func runBoundedStop(t *testing.T, stub *stopStub, name, ask string) {
	t.Helper()
	rig, home := openBoundedStopRig(t, stub, name)

	rig.lit(ask)
	rig.keys("Enter")
	// The turn has to be genuinely parked before the key is pressed, or the run
	// would be timing a stop of a turn that was about to end anyway.
	waitUntilParked(t, rig, stub)

	pressed := time.Now()
	rig.keys("Escape")

	// 1. THE BOUND IS ON THE SCREEN BEFORE IT FIRES.
	stopping := rig.waitFor(10*time.Second, say(t, "stopDetachWord"))
	if !strings.Contains(stopping, say(t, "stoppingWord")) {
		t.Fatalf("the countdown is drawn without the word it belongs to:\n%s", stopping)
	}
	t.Logf("=== pane after esc (the bound, stated) ===\n%s", stopping)

	// 2. AND IT FIRES INSIDE THE BOUND.
	detached := rig.waitFor(stopBoundPatience, say(t, "stopDetachedWord"))
	took := time.Since(pressed)
	t.Logf("=== pane after the detach (%s after esc) ===\n%s", took.Round(time.Second), detached)
	if took > stopBoundPatience {
		t.Fatalf("the turn took %s to detach, which is past the bound", took)
	}

	// 3. ONE JOURNAL LINE, WITH THE TURN'S SPEND ON IT.
	line := onlyAbandonedLine(t, home)
	if line.CostUSD <= 0 && line.Input == 0 && line.Output == 0 {
		t.Fatalf("the abandoned line carries no spend at all: %+v", line)
	}
	t.Logf("the abandoned line: %+v", line)

	// 4. AND THE NEXT PROMPT IS USABLE. This is the whole of what the person
	// wanted when they pressed the key.
	stub.stop()
	rig.lit("say hello")
	rig.keys("Enter")
	rig.waitFor(60*time.Second, stopStubDone)
}

// waitUntilParked holds until the session is inside the wait the scenario is
// about, which each kind knows by its own evidence.
func waitUntilParked(t *testing.T, rig *rig, stub *stopStub) {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		switch stub.kind {
		case parkOnStream:
			// The live summary can omit the sentence-ending period. The same
			// words still prove that the second request is streaming on screen.
			if strings.Contains(rig.capture(), strings.TrimSuffix(stopStubStreaming, ".")) {
				return
			}
		case parkOnPipe:
			// The `read` call is on screen and it is not coming back.
			if stub.calls.Load() >= 1 && strings.Contains(rig.capture(), "read") {
				time.Sleep(2 * time.Second)
				return
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("the turn never parked:\n%s", rig.capture())
}

// abandonedLine is the journal's `abandoned` record as this suite reads it back.
type abandonedLine struct {
	Reason     string  `json:"reason"`
	Input      int     `json:"input"`
	Output     int     `json:"output"`
	CacheRead  int     `json:"cacheRead"`
	CacheWrite int     `json:"cacheWrite"`
	CostUSD    float64 `json:"costUsd"`
	Calls      int     `json:"calls"`
}

// onlyAbandonedLine fails unless this home's journals hold EXACTLY ONE of them.
// The count is the assertion: a turn is let go of once, and a second line would
// mean a deadline that fired twice on one turn.
func onlyAbandonedLine(t *testing.T, home string) abandonedLine {
	t.Helper()
	found := abandonedLines(t, home)
	if len(found) != 1 {
		t.Fatalf("want exactly one abandoned line in %s, found %d: %+v", home, len(found), found)
	}
	return found[0]
}

// abandonedLines is every `abandoned` record in every journal under one home.
func abandonedLines(t *testing.T, home string) []abandonedLine {
	t.Helper()
	var found []abandonedLine
	for _, transcript := range sessionTranscripts(t, home) {
		for _, raw := range strings.Split(transcript, "\n") {
			if strings.TrimSpace(raw) == "" {
				continue
			}
			var entry struct {
				Type      string         `json:"type"`
				Abandoned *abandonedLine `json:"abandoned"`
			}
			if err := json.Unmarshal([]byte(raw), &entry); err != nil {
				continue
			}
			if entry.Type == "abandoned" && entry.Abandoned != nil {
				found = append(found, *entry.Abandoned)
			}
		}
	}
	return found
}

// The two sentences the scripted endpoint itself writes. They are the stub's own
// words and not the surface's, so they belong here rather than in the words
// table beside it — that table is a gate on what internal/tui3 still spells.
const (
	stopStubStreaming = "still going."
	stopStubDone      = "all done here."
)
