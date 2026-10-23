package main

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
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/factory"
	factoryrun "github.com/Agent-Field/codeaf/internal/factory/run"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/remote"
)

// TestAStageThatAnswersInProseReportsOnTheTurnThatMust goes through the door
// the product goes through, with a model that never calls `stage_result` of
// its own accord: every request is answered with the chat's closing words,
// unless its `tool_choice` names `stage_result`, which it then calls, as a
// provider that honours the required-tool setting does. The round's result is
// that report, made on the third reminder's turn and not by the factory.
func TestAStageThatAnswersInProseReportsOnTheTurnThatMust(t *testing.T) {
	root := resolvedTempDir(t)
	t.Setenv("HOME", filepath.Join(root, "home"))
	t.Setenv(home.EnvVar, filepath.Join(root, "state"))
	t.Setenv(config.ProfileDirEnv, "")
	t.Setenv("OPENROUTER_API_KEY", "test-key")
	var mu sync.Mutex
	forced, prose := 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			http.Error(w, "no catalog for a test", http.StatusServiceUnavailable)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		var req struct {
			recordedRequest
			ToolChoice json.RawMessage `json:"tool_choice"`
		}
		if err := json.Unmarshal(raw, &req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		var choice struct {
			Function struct {
				Name string `json:"name"`
			} `json:"function"`
		}
		_ = json.Unmarshal(req.ToolChoice, &choice)
		mu.Lock()
		must := choice.Function.Name == "stage_result"
		if must {
			forced++
		} else if len(req.Tools) > 0 {
			prose++
		}
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		if must {
			args, _ := json.Marshal(`{"done":true,"output":"planned two edits"}`)
			_, _ = w.Write([]byte(`data: {"id":"s","choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call-1","type":"function","function":{"name":"stage_result","arguments":` + string(args) + `}}]},"finish_reason":null}]}` + "\n\n" +
				`data: {"id":"s","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}` + "\n\ndata: [DONE]\n\n"))
			return
		}
		_, _ = w.Write([]byte(`data: {"id":"s","choices":[{"index":0,"delta":{"role":"assistant","content":"finishing here · what was asked is done"},"finish_reason":"stop"}]}` + "\n\ndata: [DONE]\n\n"))
	}))
	t.Cleanup(server.Close)
	t.Setenv("CODEAF_BASE_URL", server.URL)
	workspace := filepath.Join(root, "work")
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(workspace)
	freshEngineProcess(t)
	stageSettle = 20 * time.Millisecond
	t.Cleanup(func() { stageSettle = unattendedRunSettleTick })

	proc, err := openEngineProcess()
	if err != nil {
		t.Fatal(err)
	}
	launch, err := openV3Launch(proc, engineLaunchOptions(remote.Hello{Version: remote.Version, Workspace: workspace}, workspace, ""))
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(filepath.Join(root, "factory"))
	if err != nil {
		t.Fatal(err)
	}
	id, err := st.Add(context.Background(), factory.Item{
		Title: "tidy the ledger", Repo: "work", Tier: factory.TierOwner,
		Stages: []factory.Stage{{Name: "plan", Kind: factory.StageChat, Ask: "read the issue and say how", Until: "done", On: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Update(id, func(it *factory.Item) error { it.Stream = &factory.Stream{}; return nil }); err != nil {
		t.Fatal(err)
	}
	it, _ := st.Get(id)

	maker := factoryStageMaker(st, workspace, "", launch.Config)
	exec := factoryChatStage(st, "", factoryrun.NewChatExecutor(maker))
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	var lines []string
	res, err := exec.Run(ctx, factoryrun.Job{Item: it, Stage: it.Stages[0], Index: 0, Round: 1, Dir: workspace,
		Log: func(line string) { lines = append(lines, line) }})
	if err != nil {
		t.Fatalf("the round failed: %v", err)
	}
	if !res.Done || res.Output != "planned two edits" || res.Inferred || res.Reports != 1 {
		t.Fatalf("result = %+v; log %q", res, lines)
	}
	mu.Lock()
	defer mu.Unlock()
	if forced != 1 || prose < 3 {
		t.Fatalf("%d requests required stage_result and %d were free; want 1, after the brief's and two reminders' turns", forced, prose)
	}
}
