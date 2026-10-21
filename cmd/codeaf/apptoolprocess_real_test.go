package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
)

// appProcessModel answers the real session's first turn with one bash call.
// Auxiliary naming requests carry no tools and never acquire that call.
func appProcessModel(t *testing.T, args string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Messages []struct {
				Role string `json:"role"`
			} `json:"messages"`
			Tools  []json.RawMessage `json:"tools"`
			Stream bool              `json:"stream"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		called := false
		for _, message := range request.Messages {
			called = called || message.Role == "tool"
		}
		content, finish := `"content":"done"`, "stop"
		if len(request.Tools) > 0 && !called {
			content = `"content":"Running the command.","tool_calls":[{"index":0,"id":"actual-process","type":"function","function":{"name":"bash","arguments":` + strconv.Quote(args) + `}}]`
			finish = "tool_calls"
		}
		if request.Stream {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte(`data: {"id":"process-model","choices":[{"index":0,"delta":{"role":"assistant",` + content + `},"finish_reason":"` + finish + `"}]}` + "\n\ndata: [DONE]\n\n"))
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"index":0,"message":{"role":"assistant","content":"done"},"finish_reason":"stop"}]}`))
	}))
}

// E2 and E4: the real app opener, session, subprocess, remote event codec,
// live turn pump, process store and agents.entries run together. Approval is
// answered through the app's public question door, without a wall-clock wait.
// The session's clock/steer producers have their deterministic tests in
// internal/session; the app's exhaustive transport cases sit beside this one.
func TestAppToolProcessE2RealBackgroundAndTimeoutAfterApproval(t *testing.T) {
	for _, road := range []string{"background", "timeout", "approval_timeout"} {
		t.Run(road, func(t *testing.T) {
			root := resolvedTempDir(t)
			_, workspace := wakeEnvironment(t, root)
			command := "printf 'command is running\\n'; while [ ! -f release-process ]; do sleep 0.001; done"
			arguments := map[string]any{"command": command}
			if road == "background" {
				arguments["background"] = true
			} else {
				arguments["timeout"] = 0.001
			}
			raw, err := json.Marshal(arguments)
			if err != nil {
				t.Fatal(err)
			}
			model := appProcessModel(t, string(raw))
			t.Cleanup(model.Close)
			t.Setenv("CODEAF_BASE_URL", model.URL)
			mode := "allow"
			if road == "approval_timeout" {
				mode = "prompt"
			}
			state := filepath.Join(root, "state")
			if err := os.MkdirAll(state, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(state, "config.json"), []byte(`{"tools.approvalMode":"`+mode+`"}`), 0o600); err != nil {
				t.Fatal(err)
			}
			rec := newAppRecorder()
			backend := newAppBackend(rec, appOpenInProcess)
			t.Cleanup(func() { backend.Close() })
			agent, err := mustCreate(t, backend, appwire.CreateAsk{Workspace: workspace})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := backend.Send(agent.ID, "run the command", ""); err != nil {
				t.Fatal(err)
			}
			if road == "approval_timeout" {
				i := rec.waitFor(t, 30*time.Second, appAskEvent(appwire.EventAskOpened))
				rec.mu.Lock()
				ask := rec.data[i].(appwire.AskOpened).Ask
				rec.mu.Unlock()
				if ask.Kind != appwire.AskApproval || len(ask.Options) == 0 {
					t.Fatalf("approval = %+v", ask)
				}
				if err := backend.AnswerAsk(agent.ID, ask.ID, ask.Options[0].ID, ""); err != nil {
					t.Fatal(err)
				}
			}
			rec.waitFor(t, 30*time.Second, appTurnDone)
			end := toolRow(rec, t, func(row appwire.TurnTool) bool { return row.Call == "actual-process" && row.Phase == appwire.PhaseEnd })
			list, err := backend.ListProcesses(agent.ID)
			if err != nil || len(list) != 1 || end.Process == "" || end.Process != list[0].ID || list[0].Command != command {
				t.Fatalf("live = %+v; processes.list = %+v, %v", end, list, err)
			}
			rec.waitFor(t, 3*time.Second, func(event string, data any) bool {
				changed, ok := data.(appwire.ProcessChanged)
				return event == appwire.EventProcessChanged && ok && changed.Process.ID == end.Process
			})
			if err := backend.Close(); err != nil {
				t.Fatal(err)
			}
			// A new backend has no appCall cache; it rereads the engine's own
			// journal and process shelf from the conversation on disk.
			reopened := newAppBackend(newAppRecorder(), appOpenInProcess)
			t.Cleanup(func() { reopened.Close() })
			entries, _, err := reopened.Entries(agent.ID, "", appwire.MaxEntries)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, entry := range entries {
				if entry.Kind == appwire.EntryTool && entry.Call == "actual-process" {
					found = true
					if entry.Process != end.Process {
						t.Fatalf("reopened history = %+v, live = %+v", entry, end)
					}
				}
			}
			if !found {
				t.Fatal("reopened history has no tool entry")
			}
			list, err = reopened.ListProcesses(agent.ID)
			if err != nil || len(list) != 1 || list[0].ID != end.Process {
				t.Fatalf("reopened processes.list = %+v, %v; live process = %q", list, err, end.Process)
			}
		})
	}
}
