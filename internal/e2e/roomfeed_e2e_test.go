//go:build e2e

package e2e

// A TASK'S PAGE, IN A REAL TERMINAL, SAYING WHAT A CALL TOOK.
//
// This is the end-to-end evidence for the change that put the task room on the
// conversation's own reducer (docs/design/lens/DESIGN.md, Decision 1). The
// defect it holds shut is exactly the one the design names: a node's row never
// carried a duration, because EventToolFinished was not in room.go's switch at
// all — a fact the chat had and a task's page did not.
//
// IT IS SCRIPTED, NOT MODELLED, and that is the honest shape for this change.
// Nothing here touches the wire: the whole change is surface work over events
// the engine already sends, so a real model would add cost and flakiness and
// prove nothing this stub does not. The endpoint below speaks the same
// OpenAI-compatible dialect internal/provider writes and reads, exactly as
// test/remote/stub does for the two-machine harness, and it answers from a
// script — one batch of two `bash` calls of different lengths, then a plain
// reply.
//
// TWO CALLS OF ONE NAME IN ONE BATCH IS THE WHOLE ASSERTION, and it is what
// makes this a gate rather than a screenshot. The results of a batch land
// TOGETHER, so a page that timed a row from its own begin to its own end writes
// the SLOWEST call's span on every row in the batch. Only the call's own finish
// — session.EventToolFinished, the event room.go's copy never had — can say that
// one of them took a second and the other took four. And the two rows are told
// apart by their arguments, which is ruling 3 of the same design.
//
//	go test -tags e2e -count=1 -run TestATaskPageShowsWhatACallTook -v ./internal/e2e/

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// The words this run turns on. The command is what the node is scripted to run
// and what its row is found by; the sleep is what makes the duration a fact
// rather than a rounding.
const (
	roomProbeQuick = "sleep 1; echo PROBE-A"
	roomProbeSlow  = "sleep 4; echo PROBE-B"
	roomProbeBrief = "run both probes and report what they printed"
	roomProbeModel = "stub/scripted"
	// What each of them is spelled as (timestamps.go's tookWord: one decimal
	// under ten seconds).
	roomQuickTook = "1.0s"
	roomSlowTook  = "4.0s"
)

func TestATaskPageShowsWhatACallTook(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("no tmux on PATH: this test drives the real binary in a real terminal")
	}
	brain := startScriptedBrain(t)
	// EVERY SETUP STEP IS ANSWERED IN THE PROFILE, so the run opens on the
	// conversation rather than on the greeting: a key, a crew tier, and a daily
	// ceiling are the three things firstrun.go asks for (setupStepsFor).
	home := newHome(t, map[string]any{
		"model.talk": roomProbeModel, "model.work": roomProbeModel, "model.plan": roomProbeModel,
		"models.tiers.worker": roomProbeModel, "models.tiers.high": roomProbeModel,
		"tools.approvalMode": "allow",
		"daily_budget_usd":   0,
	})
	ws := newWorkspace(t, "probe", false)

	// The key is a string and not a secret: config.Load refuses to build a
	// session with none at all, and nothing behind this endpoint checks it.
	rig := startWithEnv(t, []string{
		"OPENROUTER_API_KEY=test-key",
		"AFORGE_BASE_URL=" + brain.URL + "/api/v1",
	}, "roomfeed", home, ws, 120, 40)

	// ONE WORKER AND NO SIZING CALL, which is what `solo` means: the node starts
	// from the brief with nothing asked of the model first, so everything below
	// is the node's own turn and not the conversation's.
	rig.lit("/task solo " + roomProbeBrief)
	rig.keys("Enter")

	// The node has to reach the call before there is anything to read. The
	// roster's row is the door and the room is behind it.
	rig.waitFor(90*time.Second, roomProbeBrief)
	openTheOnlyRoom(t, rig)

	// THE ROWS COME FIRST AND THE FIGURES COME AFTER THEM — both calls are still
	// running when the page opens, which is the point: the wait is for the
	// slower one's figure, so a page that drew the rows and never the figures
	// fails here rather than passing on the frame before the answer.
	page := rig.waitFor(90*time.Second, roomSlowTook)
	tookOn(t, page, "PROBE-A", roomQuickTook)
	tookOn(t, page, "PROBE-B", roomSlowTook)
	t.Logf("the task page, captured:\n%s", page)
}

// tookOn asserts that the row carrying one call says what that call took. It
// reads the ROW and not the page, because a page holding "1.0s" somewhere is
// not a page that put the figure on the right call.
func tookOn(t *testing.T, page, command, took string) {
	t.Helper()
	for _, line := range strings.Split(page, "\n") {
		if !strings.Contains(line, command) {
			continue
		}
		if strings.Contains(line, took) {
			return
		}
		t.Fatalf("the row for %s says %q, want %q — a batch's rows are being timed "+
			"from the batch and not from each call's own finish:\n%s",
			command, strings.TrimSpace(line), took, page)
	}
	t.Fatalf("no row for %s on the page:\n%s", command, page)
}

// openTheOnlyRoom walks into the one node on the roster. The rail's first node
// row is the door, and `enter` on it is the same press a person makes.
func openTheOnlyRoom(t *testing.T, r *rig) {
	t.Helper()
	// ctrl+t puts the pointer on the roster; the arrow keys walk it and enter
	// opens the row under it (tasks.go).
	r.keys("C-t")
	r.keys("Enter")
	r.waitFor(20*time.Second, "room ·")
}

// ── the scripted endpoint ───────────────────────────────────────────────────

// startScriptedBrain stands in for every model call this run makes.
//
// THE SCRIPT IS ONE RULE. A turn that has not run a tool yet is answered with
// the probe call; a turn whose last word is a tool's result is answered in
// words. That terminates every turn the engine opens — the node's own, and the
// pass that checks what it left — without this file having to know how many
// there will be.
func startScriptedBrain(t *testing.T) *httptest.Server {
	t.Helper()
	var streams atomic.Int64
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/models", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"data":[{"id":%q,"canonical_slug":%q,"name":"Scripted stub",
			"context_length":200000,
			"architecture":{"input_modalities":["text"],"output_modalities":["text"]},
			"pricing":{"prompt":"0","completion":"0","request":"0","input_cache_read":"0"},
			"supported_parameters":["tools","tool_choice","max_tokens"]}]}`,
			roomProbeModel, roomProbeModel)
	})
	mux.HandleFunc("/api/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []struct {
				Role string `json:"role"`
			} `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		ranSomething := false
		for _, m := range body.Messages {
			if strings.EqualFold(m.Role, "tool") {
				ranSomething = true
			}
		}
		writeScriptedStream(w, streams.Add(1), ranSomething)
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

// writeScriptedStream is internal/provider's sse.go read backwards.
func writeScriptedStream(w http.ResponseWriter, n int64, ranSomething bool) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "the script needs a flushable writer", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	id := fmt.Sprintf("script-%d", n)
	send := func(payload string) {
		_, _ = fmt.Fprintf(w, "data: %s\n\n", payload)
		flusher.Flush()
	}
	frame := func(delta, reason string) string {
		if reason == "" {
			reason = "null"
		} else {
			reason = fmt.Sprintf("%q", reason)
		}
		return fmt.Sprintf(`{"id":%q,"object":"chat.completion.chunk","created":%d,"model":%q,`+
			`"provider":"stub","choices":[{"index":0,"delta":%s,"finish_reason":%s}],`+
			`"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2,"cost":0}}`,
			id, time.Now().Unix(), roomProbeModel, delta, reason)
	}
	send(frame(`{"role":"assistant","content":""}`, ""))
	if !ranSomething {
		send(frame(fmt.Sprintf(`{"tool_calls":[%s,%s]}`,
			bashCall(0, fmt.Sprintf("a%d", n), roomProbeQuick),
			bashCall(1, fmt.Sprintf("b%d", n), roomProbeSlow)), ""))
		send(frame(`{}`, "tool_calls"))
		send("[DONE]")
		return
	}
	send(frame(`{"content":"Both probes printed."}`, ""))
	send(frame(`{}`, "stop"))
	send("[DONE]")
}

// bashCall is one entry of a streamed tool_calls array.
func bashCall(index int, id, command string) string {
	args, _ := json.Marshal(map[string]string{"command": command})
	quoted, _ := json.Marshal(string(args))
	return fmt.Sprintf(`{"index":%d,"id":"call_%s","type":"function",`+
		`"function":{"name":"bash","arguments":%s}}`, index, id, quoted)
}
