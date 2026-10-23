//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
)

// Each paid-looking receipt is local fake data. Request starts, including any
// checker, are counted independently from late accounting for an in-flight call.
func TestTerminalStopEndsOwnedTasksOnTheRealTerminal(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux required")
	}
	for _, mode := range []string{"page_stop", "conversation_wall", "unbounded_control"} {
		t.Run(mode, func(t *testing.T) {
			const model = "stub/terminal-stop"
			home := recoveryShortHome(t, newHome(t, map[string]any{"memory.enabled": false, config.KeyModelRoles: "chat:" + model + ",worker:" + model + ",plan:" + model + ",check:" + model, config.KeyTierLowModel: model, config.KeyTierWorkerModel: model, config.KeyTierHighModel: model}))
			ws := newWorkspace(t, "terminal-stop", false)
			var mu sync.Mutex
			starts := 0
			issued := map[string]bool{}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/models":
					_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{{"id": model, "context_length": 131072, "pricing": map[string]string{"prompt": "0.000001", "completion": "0.000001"}, "supported_parameters": []string{"tools", "tool_choice"}}}})
				case "/key", "/auth/key", "/credits":
					fmt.Fprint(w, `{"data":{"limit_remaining":100,"total_credits":100,"total_usage":0}}`)
				case "/chat/completions":
					var body struct {
						Stream   bool `json:"stream"`
						Messages []struct {
							Content json.RawMessage `json:"content"`
						} `json:"messages"`
					}
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						http.Error(w, "fixture decode", 400)
						return
					}
					var assignment []string
					for _, msg := range body.Messages {
						if match := freeConsentAssignment.FindStringSubmatch(string(msg.Content)); len(match) == 3 {
							assignment = match
							break
						}
					}
					if len(assignment) != 3 {
						terminalStopResponse(w, model, "Acknowledged.", nil, body.Stream)
						return
					}
					mu.Lock()
					starts++
					first := !issued[assignment[1]]
					issued[assignment[1]] = true
					mu.Unlock()
					if first {
						command := "i=0; while [ $i -lt 120 ]; do printf 'tick\\n' >> stop-heartbeat.txt; i=$((i+1)); sleep 1; done"
						args, _ := json.Marshal(map[string]string{"command": command})
						terminalStopResponse(w, model, "", map[string]any{"index": 0, "id": "stop-loop-" + assignment[2], "type": "function", "function": map[string]string{"name": "bash", "arguments": string(args)}}, body.Stream)
					} else {
						terminalStopResponse(w, model, "The long task is still in progress.", nil, body.Stream)
					}
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			env := []string{config.APIKeyEnv + "=fixture-terminal-stop-key", "CODEAF_BASE_URL=" + server.URL, "CODEAF_PROFILE_DIR="}
			args := []string{"chat", "--model", model, "--one-model", "--no-host", "--yolo"}
			if mode == "conversation_wall" {
				args = append(args, "--max-hours", "0.0044444444")
			}
			rig := startWithEnv(t, env, "terminal_"+mode, home, ws, 170, 48, args...)
			rig.skipSetup(t)
			rig.lit("/task solo run a slow counting loop for two minutes, writing each tick to stop-heartbeat.txt using bash; do not finish early")
			rig.keys("Enter")
			deadline := time.Now().Add(12 * time.Second)
			for stopHeartbeatBytes(home) == 0 && stopHeartbeatBytes(ws) == 0 {
				if time.Now().After(deadline) {
					t.Fatalf("owned task never started its actual loop: %s", rig.capture())
				}
				time.Sleep(pollEvery)
			}
			if mode == "unbounded_control" {
				before := stopHeartbeatBytes(home) + stopHeartbeatBytes(ws)
				time.Sleep(18 * time.Second)
				if after := stopHeartbeatBytes(home) + stopHeartbeatBytes(ws); after <= before {
					t.Fatalf("unbounded positive control stopped early: before=%d after=%d", before, after)
				}
				if strings.Contains(rig.capture(), "conversation time limit reached") {
					t.Fatal("unbounded control acquired a time limit")
				}
			}
			if mode != "conversation_wall" {
				stopOwnedTaskThroughItsPage(t, rig)
			}
			if mode == "conversation_wall" {
				rig.waitFor(35*time.Second, "conversation time limit reached")
				if screen := rig.capture(); strings.Contains(screen, "background work") && strings.Contains(screen, "ctrl+e") {
					rig.keys("C-e")
				}
				screen := rig.waitFor(10*time.Second, "a time limit you set stopped it")
				if !strings.Contains(screen, "a time limit you set stopped it") {
					t.Fatal("joined wall stop reason was not disclosed")
				}
			} else {
				rig.waitFor(20*time.Second, "stopped")
			}
			// Completion acknowledgment is the boundary: do not conflate a receipt from
			// an already-dispatched call with permission to start a checker or resume.
			mu.Lock()
			atStop := starts
			mu.Unlock()
			before := stopHeartbeatBytes(home) + stopHeartbeatBytes(ws)
			spendAtStop := stopSpendSnapshot(t, home)
			if spendAtStop.Cost <= 0 || spendAtStop.Cached == 0 {
				t.Fatalf("fixture failed to meter nonzero spend/cache: %+v", spendAtStop)
			}
			for end := time.Now().Add(10 * time.Second); time.Now().Before(end); {
				screen := rig.capture()
				if strings.Contains(screen, "a accept  n not right  s tell it") || strings.Contains(screen, "Working checking") {
					t.Fatalf("stopped task entered acceptance/checking: %s", screen)
				}
				mu.Lock()
				now := starts
				mu.Unlock()
				if now != atStop {
					t.Fatalf("new task/checker model requests after terminal stop: %d -> %d", atStop, now)
				}
				time.Sleep(200 * time.Millisecond)
			}
			if afterSpend := stopSpendSnapshot(t, home); afterSpend != spendAtStop {
				t.Fatalf("tokens/cache/spend changed after joined stop: before=%+v after=%+v", spendAtStop, afterSpend)
			}
			after := stopHeartbeatBytes(home) + stopHeartbeatBytes(ws)
			if after != before {
				t.Fatalf("owned child kept writing after stop: bytes %d -> %d", before, after)
			}
			t.Logf("PASS real terminal %s: owned child stopped, model request starts stable=%d for10s, heartbeat bytes stable=%d; no checking/acceptance/resume.\n%s", mode, atStop, after, rig.capture())
		})
	}
}

func stopHeartbeatBytes(root string) int64 {
	var size int64
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && d.Name() == "stop-heartbeat.txt" {
			if info, e := d.Info(); e == nil {
				size += info.Size()
			}
		}
		return nil
	})
	return size
}

func TestTerminalStopLiveOwnedTaskWorkflow(t *testing.T) {
	requireTmuxAndKey(t)
	const model = "deepseek/deepseek-v4.1-flash"
	if e2eModel != model {
		t.Fatalf("requires CODEAF_E2E_MODEL=%s", model)
	}
	home := recoveryShortHome(t, newHome(t, map[string]any{"memory.enabled": false}))
	ws := newWorkspace(t, "live-terminal-stop", false)
	env := []string{config.APIKeyEnv + "=" + liveKey(t), "CODEAF_BASE_URL=https://openrouter.ai/api/v1", "CODEAF_PROFILE_DIR="}
	rig := startWithEnv(t, env, "terminal_stop_live", home, ws, 170, 48, "chat", "--model", model, "--one-model", "--no-host", "--yolo")
	rig.skipSetup(t)
	prompt := "We are testing our fictional counting workflow. Acknowledge that a long-running counter should retain its partial output when I explicitly stop it."
	rig.lit(prompt)
	rig.keys("Enter")
	if answer, _ := privateMemoryAwaitAnswer(home, prompt, 90*time.Second); answer == "" {
		t.Fatal("live introduction did not complete")
	}
	rig.lit("/task solo use bash to run a counting loop for120 seconds, appending one line tick per second to stop-heartbeat.txt in your working directory. Run the loop now, wait for it, and do not mark this task done before the loop finishes.")
	rig.keys("Enter")
	deadline := time.Now().Add(2 * time.Minute)
	for stopHeartbeatBytes(home)+stopHeartbeatBytes(ws) == 0 {
		if time.Now().After(deadline) {
			t.Fatalf("live task never started its counter: %s", rig.capture())
		}
		time.Sleep(pollEvery)
	}
	stopOwnedTaskThroughItsPage(t, rig)
	rig.waitFor(20*time.Second, "stopped")
	bytesAtStop := stopHeartbeatBytes(home) + stopHeartbeatBytes(ws)
	time.Sleep(10 * time.Second)
	if got := stopHeartbeatBytes(home) + stopHeartbeatBytes(ws); got != bytesAtStop {
		t.Fatalf("live stopped child kept writing: %d -> %d", bytesAtStop, got)
	}
	screen := rig.capture()
	if strings.Contains(screen, "Working checking") || strings.Contains(screen, "a accept  n not right  s tell it") {
		t.Fatalf("live stopped task entered checking or acceptance: %s", screen)
	}
	models := callModels(t, home)
	if len(models) == 0 {
		t.Fatal("no live receipts")
	}
	for _, got := range models {
		if got != model {
			t.Fatalf("unexpected live model %s", got)
		}
	}
	t.Logf("PASS live task: actual long-running bash counter stopped through task page; partial output retained and unchanged10s; no acceptance/checking; all %d recorded calls exact %s.\n%s", len(models), model, screen)
}

func terminalStopResponse(w http.ResponseWriter, model, text string, call any, stream bool) {
	message := map[string]any{"role": "assistant", "content": text}
	finish := "stop"
	if call != nil {
		message["tool_calls"] = []any{call}
		finish = "tool_calls"
	}
	usage := map[string]any{"prompt_tokens": 40, "completion_tokens": 10, "total_tokens": 50, "cost": 0.02, "prompt_tokens_details": map[string]int{"cached_tokens": 10}}
	if stream {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, choice := range []map[string]any{{"index": 0, "delta": message, "finish_reason": nil}, {"index": 0, "delta": map[string]any{}, "finish_reason": finish}} {
			raw, _ := json.Marshal(map[string]any{"id": "terminal-stop-fixture", "model": model, "choices": []any{choice}, "usage": usage})
			fmt.Fprintf(w, "data: %s\n\n", raw)
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"id": "terminal-stop-fixture", "model": model, "choices": []any{map[string]any{"index": 0, "message": message, "finish_reason": finish}}, "usage": usage})
}

type stopSpend struct {
	Prompt, Completion, Cached int
	Cost                       float64
}

func stopSpendSnapshot(t *testing.T, home string) stopSpend {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(home, "logs", "calls.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var total stopSpend
	for _, line := range strings.Split(string(raw), "\n") {
		var row struct {
			Prompt     int     `json:"prompt_tokens"`
			Completion int     `json:"completion_tokens"`
			Cached     int     `json:"cached_tokens"`
			Cost       float64 `json:"cost"`
		}
		if json.Unmarshal([]byte(line), &row) == nil {
			total.Prompt += row.Prompt
			total.Completion += row.Completion
			total.Cached += row.Cached
			total.Cost += row.Cost
		}
	}
	return total
}

func stopOwnedTaskThroughItsPage(t *testing.T, rig *rig) {
	t.Helper()
	openTasksPlace(t, rig)
	tasksPlaceRunRow(t, rig)
	hit, screen := rig.waitForAny(15*time.Second, say(t, "tasksEnterInsideWord"), say(t, "tasksEnterRoomWord"))
	if hit == "" {
		t.Fatalf("active task row never offered its room: %s", screen)
	}
	rig.keys("Enter")
	screen = rig.waitFor(15*time.Second, say(t, "roomTabsWords"))
	if !strings.Contains(screen, say(t, "roomTabsWords")) {
		t.Fatal("active task room did not load")
	}
	rig.keys("x")
	screen = rig.waitFor(10*time.Second, "Stop this task?", "stop it", "keep going")
	if !strings.Contains(screen, "Stop this task?") || !strings.Contains(screen, "stop it") || !strings.Contains(screen, "keep going") {
		t.Fatal("actual Stop confirmation was not displayed")
	}
	rig.keys("1")
	rig.keys("Enter")
}
