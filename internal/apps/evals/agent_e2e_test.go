//go:build e2e

package evals

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/runs"
	"github.com/Agent-Field/codeaf/internal/appwire"
)

func TestLiveMarketingFanout(t *testing.T) {
	key := evalKey()
	if !strings.HasPrefix(key, "sk-or-v1-") {
		t.Skip("the profile did not resolve an OpenRouter key")
	}
	binary := os.Getenv("CODEAF_APP_EVAL_BINARY")
	if binary == "" {
		t.Skip("set CODEAF_APP_EVAL_BINARY to the built app engine")
	}
	binary, err := filepath.Abs(binary)
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	scratch := t.TempDir()
	app, err := seedApp(scratch, "testdata/marketing-desk")
	if err != nil {
		t.Fatal(err)
	}
	engine, err := launch(ctx, binary, scratch, key, builderModel)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := engine.Close(); err != nil {
			t.Error(err)
		}
	}()
	call := func(method string, params any, out any) {
		t.Helper()
		raw, err := engine.wire(ctx, method, params)
		if err != nil {
			t.Fatalf("%s: %v", method, err)
		}
		if out != nil && json.Unmarshal(raw, out) != nil {
			t.Fatalf("%s returned %s", method, raw)
		}
	}
	var created struct{ Rows []map[string]any }
	call("data.create", map[string]any{"app": app, "type": "mention", "rows": []map[string]any{
		{"text": "Can you import a CSV of contacts?"},
		{"text": "I liked the new views. Any tips for teams?"},
		{"text": "Do you have a guide for creating a simple tracker?"},
	}}, &created)
	var ids []string
	for _, row := range created.Rows {
		ids = append(ids, row["id"].(string))
	}
	var harnesses []string
	call("agents.query", appwire.AppAgentQuery{App: app, Kind: "harnesses"}, &harnesses)
	if len(harnesses) != 1 || harnesses[0] != runs.HarnessCodeaf {
		t.Fatalf("scratch harnesses = %v", harnesses)
	}
	var launched appwire.AppAILaunched
	call("agents.launch", appwire.AppAILaunch{App: app, Action: "draft-reply", Screen: "mentions", Records: ids}, &launched)
	var reviews []appwire.AppAISelection
	var cost float64
	deadline := time.Now().Add(2 * time.Minute)
	for {
		reviews, cost = nil, 0
		var result appwire.RunListResult
		call("runs.list", appwire.RunListParams{App: app, Run: "draft-reply"}, &result)
		for _, run := range result.Runs {
			if run.Queue != launched.Queue {
				continue
			}
			if run.State == runs.Incomplete || run.State == runs.YourCall {
				t.Fatalf("draft run stopped at %s: %s", run.State, run.Error)
			}
			cost += run.Cost
			if run.State == runs.Done && run.Proposal != nil {
				reviews = append(reviews, appwire.AppAISelection{ID: run.ID, ExpectedRev: "1"})
			}
		}
		if len(reviews) == 3 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("three proposals did not land")
		}
		time.Sleep(500 * time.Millisecond)
	}
	if cost <= 0 || cost > 0.09 {
		t.Fatalf("fan-out cost = $%.6f", cost)
	}
	var pending []struct {
		ID          string `json:"id"`
		ExpectedRev string `json:"expected_rev"`
		Safe        bool   `json:"safe"`
	}
	call("agents.query", appwire.AppAgentQuery{App: app, Kind: "review", Queue: launched.Queue}, &pending)
	if len(pending) != 3 {
		t.Fatalf("desk review queue = %v", pending)
	}
	for _, item := range pending {
		if !item.Safe || item.ExpectedRev != "1" {
			t.Fatalf("desk proposal = %+v", item)
		}
	}
	call("agents.review", appwire.AppAIReview{App: app, Queue: launched.Queue, Decision: "accept_safe", IDs: reviews}, nil)
	call("agents.query", appwire.AppAgentQuery{App: app, Kind: "review", Queue: launched.Queue}, &pending)
	if len(pending) != 0 {
		t.Fatalf("desk review queue after Apply = %v", pending)
	}
	for _, id := range ids {
		var result struct{ Row map[string]any }
		call("data.get", map[string]string{"app": app, "type": "mention", "id": id}, &result)
		if words, _ := result.Row["reply"].(string); strings.TrimSpace(words) == "" {
			t.Fatalf("accepted record %s has no reply", id)
		}
	}
	var approved appwire.RunStarted
	call("runs.start", appwire.RunStartParams{App: app, Run: "approve-post", Params: map[string]any{"mention": ids[0]}}, &approved)
	err = runSteps(ctx, engine, []Step{{
		Method: "runs.get", Params: map[string]any{"app": app, "id": approved.ID}, Await: 20 * time.Second,
		Expect: []Check{equal("/state", runs.YourCall), equal("/approval/kind", "send"), equal("/approval/args/destination", "fixture-marketing")},
	}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var gated runs.Run
	call("runs.get", appwire.RunGetParams{App: app, ID: approved.ID}, &gated)
	if reply, _ := gated.Approval.Args["text"].(string); strings.TrimSpace(reply) == "" {
		t.Fatalf("approval did not contain the exact reply: %+v", gated.Approval)
	}
	payload, err := json.Marshal(gated.Approval.Args)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(payload)
	effect := appwire.AppAgentPendingEffect{
		ID: gated.Approval.ID, Account: "me", Operation: "send", Destination: "fixture-marketing",
		Payload: string(payload), PayloadHash: hex.EncodeToString(hash[:]), AppVersion: strconv.Itoa(gated.AppVersion),
		ExpiresAt: gated.Approval.AskedAt.Add(30 * time.Minute).UTC().Format(time.RFC3339Nano),
	}
	answer := appwire.AppAgentEffectAnswer{App: app, Run: gated.ID, Effect: effect, Answer: "no"}
	answer.Effect.Payload += " edited"
	if _, err := engine.wire(ctx, "agents.approval", answer); err != CodeError(appwire.CodeConflict) {
		t.Fatalf("altered payload = %v", err)
	}
	answer.Effect = effect
	call("agents.approval", answer, nil)
	err = runSteps(ctx, engine, []Step{{
		Method: "runs.get", Params: map[string]any{"app": app, "id": gated.ID}, Await: 20 * time.Second,
		Expect: []Check{equal("/state", runs.Incomplete), equal("/error", "approval declined")},
	}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("three codeaf harness drafts accepted at $%.6f total", cost)
}

func TestDeterministicAppActionRunsWithNoModel(t *testing.T) {
	binary := os.Getenv("CODEAF_APP_EVAL_BINARY")
	if binary == "" {
		t.Skip("set CODEAF_APP_EVAL_BINARY to the built app engine")
	}
	scratch := t.TempDir()
	app, err := seedApp(scratch, "testdata/marketing-desk")
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	engine, err := launch(ctx, binary, scratch, "", "")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := engine.Close(); err != nil {
			t.Error(err)
		}
	}()
	raw, err := engine.wire(ctx, "agents.query", appwire.AppAgentQuery{App: app, Kind: "harnesses"})
	if err != nil || string(raw) != "[]" {
		t.Fatalf("keyless harnesses = %s, %v", raw, err)
	}
	var record string
	var started string
	vars := map[string]any{"app": app}
	err = runSteps(ctx, engine, []Step{
		{Method: "data.create", Params: map[string]any{"app": app, "type": "mention", "set": map[string]any{"text": "Docs question", "reply": "See the guide."}}, Capture: map[string]string{"record": "/rows/0/id"}},
		{Method: "agents.launch", Params: map[string]any{"app": app, "action": "draft-reply", "records": []any{"$record"}}, Error: string(appwire.CodeNotFound)},
		{Method: "runs.start", Params: map[string]any{"app": app, "run": "approve-post", "params": map[string]any{"mention": "$record"}}, Capture: map[string]string{"run": "/id"}},
		{Method: "runs.get", Params: map[string]any{"app": app, "id": "$run"}, Await: 20 * time.Second, Expect: []Check{equal("/state", runs.YourCall), equal("/approval/args/text", "See the guide.")}},
	}, vars)
	if err != nil {
		t.Fatal(err)
	}
	record, _ = vars["record"].(string)
	started, _ = vars["run"].(string)
	if record == "" || started == "" {
		t.Fatal("the deterministic action did not run")
	}
}
