package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/approval"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/factory"
	factoryrun "github.com/Agent-Field/codeaf/internal/factory/run"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/teams"
)

// stageModel is an OpenAI-shaped endpoint that plays a stage conversation
// from a script: a request carrying `stage_result` on its belt that has seen k
// tool answers is answered with the script's call k, and everything else (the
// turn after the last call, a title, a namer) with "done". The plain script is
// one call to `stage_result`.
type stageModel struct {
	server *httptest.Server
	mu     sync.Mutex
	belts  [][]string
	briefs []string
}

// stageCall is one tool call in a stage model's script, its arguments as JSON.
type stageCall struct{ name, args string }

const stageModelReport = `{"done":true,"findings":0,"output":"looked it over"}`

func newStageModel(t *testing.T) *stageModel {
	return newScriptedStageModel(t, stageCall{"stage_result", stageModelReport})
}

func newScriptedStageModel(t *testing.T, script ...stageCall) *stageModel {
	t.Helper()
	m := &stageModel{}
	m.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			http.Error(w, "no catalog for a test", http.StatusServiceUnavailable)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		var req recordedRequest
		if err := json.Unmarshal(raw, &req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		var belt []string
		for _, tool := range req.Tools {
			belt = append(belt, tool.Function.Name)
		}
		answered := 0
		for _, msg := range req.Messages {
			if msg.Role == "tool" {
				answered++
			}
		}
		var call *stageCall
		if answered < len(script) && strings.Contains(strings.Join(belt, ","), "stage_result") {
			call = &script[answered]
		}
		m.mu.Lock()
		if len(belt) > 0 {
			m.belts = append(m.belts, belt)
			m.briefs = append(m.briefs, string(raw))
		}
		m.mu.Unlock()
		if call != nil {
			args, _ := json.Marshal(call.args)
			id := fmt.Sprintf("call-%d", answered+1)
			if req.Stream {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = w.Write([]byte(`data: {"id":"s","choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"` + id + `","type":"function","function":{"name":"` + call.name + `","arguments":` + string(args) + `}}]},"finish_reason":null}]}` + "\n\n" +
					`data: {"id":"s","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}` + "\n\ndata: [DONE]\n\n"))
				return
			}
			_, _ = w.Write([]byte(`{"choices":[{"index":0,"message":{"role":"assistant","content":"","tool_calls":[{"id":"` + id + `","type":"function","function":{"name":"` + call.name + `","arguments":` + string(args) + `}}]},"finish_reason":"tool_calls"}]}`))
			return
		}
		if req.Stream {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte(`data: {"id":"s","choices":[{"index":0,"delta":{"role":"assistant","content":"done"},"finish_reason":"stop"}]}` + "\n\ndata: [DONE]\n\n"))
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"index":0,"message":{"role":"assistant","content":"done"},"finish_reason":"stop"}]}`))
	}))
	t.Cleanup(m.server.Close)
	return m
}

// TestAChatStageIsAConversationThatReportsThroughStageResult goes through the
// door the product goes through: the engine process's own launch config is the
// parent, the real maker opens the conversation, the real chat executor runs
// the round, and a model endpoint answers with one `stage_result`. It asserts
// the round's result, the item's team, the conversation's membership in it,
// and that the belt carried the stage verbs and not the card-raising ones.
func TestAChatStageIsAConversationThatReportsThroughStageResult(t *testing.T) {
	root := resolvedTempDir(t)
	t.Setenv("HOME", filepath.Join(root, "home"))
	t.Setenv(home.EnvVar, filepath.Join(root, "state"))
	t.Setenv(config.ProfileDirEnv, "")
	t.Setenv("OPENROUTER_API_KEY", "test-key")
	model := newStageModel(t)
	t.Setenv("CODEAF_BASE_URL", model.server.URL)
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
		Stages: []factory.Stage{{Name: "review", Kind: factory.StageChat, Ask: "look it over", Until: "done", On: true}},
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
	res, err := exec.Run(ctx, factoryrun.Job{Item: it, Stage: it.Stages[0], Index: 0, Round: 1, Dir: workspace, Log: func(string) {}})
	if err != nil {
		t.Fatalf("the round failed: %v", err)
	}
	if !res.Done || res.Output != "looked it over" {
		model.mu.Lock()
		t.Logf("belts: %v", model.belts)
		for _, b := range model.briefs {
			if i := strings.Index(b, `"role":"tool"`); i >= 0 {
				t.Logf("tool message: %.800s", b[i:])
			}
			if i := strings.Index(b, `"tool_calls"`); i >= 0 {
				t.Logf("tool calls: %.500s", b[i:])
			}
		}
		model.mu.Unlock()
		t.Fatalf("result = done %v, output %q; want the reported round", res.Done, res.Output)
	}
	if strings.TrimSpace(res.Chat) == "" {
		t.Fatal("the round names no conversation")
	}

	saved, _ := st.Get(id)
	if saved.Stream == nil || saved.Stream.Room == "" {
		t.Fatal("the item's team was not written onto it")
	}
	f, err := teams.Load("")
	if err != nil {
		t.Fatal(err)
	}
	var team *teams.Team
	for i := range f.Teams {
		if f.Teams[i].ID == saved.Stream.Room {
			team = &f.Teams[i]
		}
	}
	if team == nil || team.Name != saved.Ref()+" · tidy the ledger" {
		t.Fatalf("the item's team is %+v", team)
	}
	joined := false
	for _, m := range team.Members {
		if m.File == res.Chat && m.Word == saved.Ref()+" · review" {
			joined = true
		}
	}
	if !joined {
		t.Fatalf("the conversation %s is not in the item's team: %+v", res.Chat, team.Members)
	}

	model.mu.Lock()
	defer model.mu.Unlock()
	if len(model.belts) == 0 {
		t.Fatal("the model was never asked anything with a belt")
	}
	belt := strings.Join(model.belts[0], ",")
	for _, verb := range []string{"stage_result", "plan_edit"} {
		if !strings.Contains(belt, verb) {
			t.Errorf("the stage's belt carries no %s: %s", verb, belt)
		}
	}
	for _, verb := range []string{"factory_add", "factory_recipe", "factory_item"} {
		if strings.Contains(belt, verb) {
			t.Errorf("the stage's belt carries %s, which waits on a card nobody answers", verb)
		}
	}
	if !strings.Contains(model.briefs[0], "End by calling stage_result once.") {
		t.Error("the first turn did not carry the stage's brief")
	}
}

// TestAStageConversationWritesInItsCheckout goes through the real maker with
// a model that calls `write` on a file in the checkout before it reports. The
// file is there afterwards, and no tool answer the model was handed is the
// headless gate's refusal: a stage runs at the allow posture.
func TestAStageConversationWritesInItsCheckout(t *testing.T) {
	root := resolvedTempDir(t)
	t.Setenv("HOME", filepath.Join(root, "home"))
	t.Setenv(home.EnvVar, filepath.Join(root, "state"))
	t.Setenv(config.ProfileDirEnv, "")
	t.Setenv("OPENROUTER_API_KEY", "test-key")
	t.Setenv(factoryPostureEnv, "")
	workspace := filepath.Join(root, "work")
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(workspace, "NOTES.md")
	writeArgs, _ := json.Marshal(map[string]string{"path": target, "content": "written by the stage\n"})
	model := newScriptedStageModel(t, stageCall{"write", string(writeArgs)}, stageCall{"stage_result", stageModelReport})
	t.Setenv("CODEAF_BASE_URL", model.server.URL)
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
		Title: "write the notes", Repo: "work", Tier: factory.TierOwner,
		Stages: []factory.Stage{{Name: "write", Kind: factory.StageChat, Ask: "write the notes", Until: "done", On: true}},
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
	res, err := exec.Run(ctx, factoryrun.Job{Item: it, Stage: it.Stages[0], Index: 0, Round: 1, Dir: workspace, Log: func(string) {}})
	if err != nil {
		t.Fatalf("the round failed: %v", err)
	}
	model.mu.Lock()
	defer model.mu.Unlock()
	for _, brief := range model.briefs {
		if strings.Contains(brief, "no resolver is attached") {
			t.Fatalf("the stage's write was refused by the headless gate: %.600s", brief[strings.Index(brief, "no resolver"):])
		}
	}
	got, err := os.ReadFile(target)
	if err != nil || string(got) != "written by the stage\n" {
		t.Fatalf("the stage's file = %q, %v", got, err)
	}
	if !res.Done {
		t.Fatalf("the round did not report done: %+v", res)
	}
}

// TestAStagesAllowIsNotNarrowedByThePersonsRows: the person's rows ask about
// write and refuse bash, and a stage's allow gate still allows both, while
// internal/approval's critical-command floor still holds under it. The same
// rows through the ordinary headless gate do narrow, which is what makes the
// first half mean something.
func TestAStagesAllowIsNotNarrowedByThePersonsRows(t *testing.T) {
	profile := t.TempDir()
	rows, _ := json.Marshal(map[string]any{
		config.KeyToolApprovalMode: "prompt",
		config.KeyToolApprovals:    "write:prompt, bash:deny",
	})
	if err := os.WriteFile(filepath.Join(profile, "config.json"), rows, 0o600); err != nil {
		t.Fatal(err)
	}
	workspace := t.TempDir()
	plain := v3ApprovalGate{workspace: workspace, profileDir: profile, headless: true}
	gate := stageApprovalGate{rows: plain, posture: session.PostureAllow}
	write := json.RawMessage(`{"path":"x.go","content":"package x"}`)

	narrowed, _, err := plain.Build(session.PostureAllow)
	if err != nil {
		t.Fatal(err)
	}
	if d := narrowed.Check("write", write); d.Action == approval.ActionAllow {
		t.Fatalf("the person's rows did not narrow --yolo's gate (%+v); this test proves nothing", d)
	}

	policy, guardian, err := gate.Build(session.PostureAllow)
	if err != nil {
		t.Fatal(err)
	}
	if guardian {
		t.Error("the stage's allow gate stood the guardian up")
	}
	if d := policy.Check("write", write); d.Action != approval.ActionAllow {
		t.Errorf("a stage's write = %+v, want allow", d)
	}
	if d := policy.CheckBash("go test ./..."); d.Action != approval.ActionAllow {
		t.Errorf("a stage's go test = %+v, want allow", d)
	}
	if d := policy.CheckBash("rm -rf /"); d.Action == approval.ActionAllow {
		t.Error("the critical-command floor did not hold under a stage's allow")
	}
	if d := policy.Check("stage_result", json.RawMessage(`{}`)); d.Action != approval.ActionAllow {
		t.Errorf("stage_result = %+v", d)
	}
}

// TestTheStagePosturePinIsRegisteredAndRead: allow unless a posture word pins
// another, and the pin is on the plumbing list.
func TestTheStagePosturePinIsRegisteredAndRead(t *testing.T) {
	if !slices.Contains(config.OperatorEnvPins, factoryPostureEnv) {
		t.Fatalf("%s is not registered in internal/config's settings.go", factoryPostureEnv)
	}
	for value, want := range map[string]string{"": "allow", "ask": "ask", " Deny ": "deny", "yolo": "allow", "guardian": "guardian"} {
		t.Setenv(factoryPostureEnv, value)
		if got := factoryStagePosture(); got != want {
			t.Errorf("%s=%q gives %q, want %q", factoryPostureEnv, value, got, want)
		}
	}
}
