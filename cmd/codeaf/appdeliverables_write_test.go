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

func TestPlainAppAgentHasDeliverablesIndexAndJournal(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("CODEAF_PROFILE_DIR", t.TempDir())
	t.Setenv("OPENROUTER_API_KEY", "test-key")
	t.Setenv("CODEAF_BASE_URL", "http://127.0.0.1:1")
	freshEngineProcess(t)
	backend := newAppBackend(newAppRecorder(), appOpenInProcess)
	t.Cleanup(func() { backend.Close() })
	agent, err := mustCreate(t, backend, appwire.CreateAsk{Workspace: os.Getenv("HOME")})
	if err != nil {
		t.Fatal(err)
	}
	id, _, err := backend.mediaScope(agent.ID)
	if err != nil || id == "" {
		t.Fatalf("plain app agent has no journal identity: %q, %v", id, err)
	}
	proc := openedEngineProcess()
	if proc == nil || proc.Artifacts == "" || proc.Artifacts != appMediaIndex() {
		t.Fatal("plain app agent must write to the Files index")
	}
}

// The real app opener must give a home-folder agent both a journal and the
// index that Files reads. The model endpoint scripts only the public tools.
func TestPlainAppAgentIndexesWrittenAndEditedFiles(t *testing.T) {
	for _, road := range []string{"create", "overwrite", "edit"} {
		t.Run(road, func(t *testing.T) {
			root := resolvedTempDir(t)
			wakeEnvironment(t, root)
			workspace := os.Getenv("HOME")
			if err := os.MkdirAll(workspace, 0o700); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(filepath.Join(workspace, ".git")); !os.IsNotExist(err) {
				t.Fatal("fixture must be a home folder without Git")
			}
			file := filepath.Join(workspace, "notes.md")
			tool, args := "write", `{"path":`+strconv.Quote(file)+`,"content":"# after\n"}`
			if road != "create" {
				if err := os.WriteFile(file, []byte("# before\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if road == "edit" {
				tool, args = "edit", `{"path":`+strconv.Quote(file)+`,"oldText":"before","newText":"after"}`
			}
			model := appDeliverableModel(t, tool, args)
			t.Cleanup(model.Close)
			t.Setenv("CODEAF_BASE_URL", model.URL)
			state := filepath.Join(root, "state")
			if err := os.MkdirAll(state, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(state, "config.json"), []byte(`{"tools.approvalMode":"allow"}`), 0o600); err != nil {
				t.Fatal(err)
			}
			rec := newAppRecorder()
			backend := newAppBackend(rec, appOpenInProcess)
			t.Cleanup(func() { backend.Close() })
			agent, err := mustCreate(t, backend, appwire.CreateAsk{Workspace: workspace})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := backend.Send(agent.ID, "save the notes", ""); err != nil {
				t.Fatal(err)
			}
			rec.waitFor(t, 30*time.Second, appTurnDone)
			page, err := backend.ListDeliverables(appwire.DeliverablesQuery{Scope: "agent", Agent: agent.ID})
			if err != nil || len(page.Deliverables) != 1 {
				t.Fatalf("Files must show notes.md: %+v, %v", page, err)
			}
			text, err := backend.ReadDeliverable(agent.ID, page.Deliverables[0].ID, "", 100)
			if err != nil || text.Text != "# after\n" {
				t.Fatalf("Files must read the authored bytes: %+v, %v", text, err)
			}
		})
	}
}

func appDeliverableModel(t *testing.T, tool, args string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Messages []struct {
				Role string `json:"role"`
			} `json:"messages"`
			Tools  []json.RawMessage `json:"tools"`
			Stream bool              `json:"stream"`
		}
		if r.Method != http.MethodPost || json.NewDecoder(r.Body).Decode(&request) != nil {
			http.NotFound(w, r)
			return
		}
		called := false
		for _, message := range request.Messages {
			called = called || message.Role == "tool"
		}
		content, finish := `"content":"done"`, "stop"
		if len(request.Tools) > 0 && !called {
			content = `"tool_calls":[{"index":0,"id":"save-notes","type":"function","function":{"name":` + strconv.Quote(tool) + `,"arguments":` + strconv.Quote(args) + `}}]`
			finish = "tool_calls"
		}
		if request.Stream {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte(`data: {"choices":[{"index":0,"delta":{"role":"assistant",` + content + `},"finish_reason":"` + finish + `"}]}` + "\n\ndata: [DONE]\n\n"))
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"index":0,"message":{"role":"assistant","content":"done"},"finish_reason":"stop"}]}`))
	}))
}
