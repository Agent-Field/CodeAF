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
	"github.com/Agent-Field/codeaf/internal/gitidentity"
	"github.com/Agent-Field/codeaf/internal/store"
)

const planRuleMark = "ORCHID_RULE_1549"
const otherPlanRuleMark = "OTHER_PROJECT_ONLY_1549"

func planRulesHome(t *testing.T, rows map[string]any) (string, string) {
	t.Helper()
	home := recoveryShortHome(t, newHome(t, rows))
	ws := newWorkspace(t, "orchid-plan", false)
	other := newWorkspace(t, "other-plan", false)
	st, err := store.Open(filepath.Join(home, "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	for _, item := range []struct{ path, text string }{{ws, "Every artifact for this project must include the exact marker " + planRuleMark + ". This is a project instruction."}, {other, "Every artifact for the unrelated project must include " + otherPlanRuleMark + "."}} {
		key, err := gitidentity.ProjectKey(item.path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = st.AddMemory(store.Memory{Owner: store.OwnerProject(key), Type: store.MemoryPreference, Origin: store.OriginPerson, Rule: true, Text: item.text}); err != nil {
			t.Fatal(err)
		}
	}
	return home, ws
}

// The model is deliberately adversarial: it writes the required marker only
// when the actual plan worker opening contains its scoped project instruction.
func TestPlanBornWorkersReceiveScopedRulesOnTheRealTerminal(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux required")
	}
	const model = "stub/plan-rules"
	home, ws := planRulesHome(t, map[string]any{"memory.enabled": false, config.KeyModelRoles: "chat:" + model + ",worker:" + model + ",plan:" + model + ",check:" + model, config.KeyTierLowModel: model, config.KeyTierWorkerModel: model, config.KeyTierHighModel: model})
	var mu sync.Mutex
	assignments := map[string]bool{}
	checkerSeen := false
	var violations []string
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
			var opening string
			for _, message := range body.Messages {
				if match := freeConsentAssignment.FindStringSubmatch(string(message.Content)); len(match) == 3 {
					assignment = match
					opening = string(message.Content)
					break
				}
			}
			if len(assignment) != 3 {
				freeConsentResponse(w, model, "Acknowledged.", nil, body.Stream)
				return
			}
			own := strings.Contains(opening, planRuleMark)
			foreign := strings.Contains(opening, otherPlanRuleMark)
			mu.Lock()
			assignments[assignment[1]] = true
			isChecker := strings.Contains(opening, "## Who checks this work")
			checkerSeen = checkerSeen || isChecker
			if !own || foreign {
				violations = append(violations, fmt.Sprintf("task %s own=%t foreign=%t", assignment[1], own, foreign))
			}
			mu.Unlock()
			marker := "MISSING_PROJECT_INSTRUCTION"
			if own && !foreign {
				marker = planRuleMark
			}
			command := "printf '" + marker + "\\n' > plan-rule-proof.txt && grep -qx '" + planRuleMark + "' plan-rule-proof.txt && plandb done " + assignment[1] + " --agent " + assignment[2] + " --result 'holds: scoped project instruction is present in the artifact'"
			if isChecker {
				command = "grep -qx '" + planRuleMark + "' plan-rule-proof.txt && plandb done " + assignment[1] + " --agent " + assignment[2] + " --result 'holds: scoped project instruction is present in the artifact'"
			}
			args, _ := json.Marshal(map[string]string{"command": command})
			freeConsentResponse(w, model, "", map[string]any{"index": 0, "id": "rules-" + assignment[2], "type": "function", "function": map[string]string{"name": "bash", "arguments": string(args)}}, body.Stream)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	env := []string{config.APIKeyEnv + "=fixture-plan-rules-key", "CODEAF_BASE_URL=" + server.URL, "CODEAF_PROFILE_DIR="}
	stopRecoveryEngineOnCleanup(t, env, home, ws)
	rig := startWithEnv(t, env, "plan_rules", home, ws, 160, 46, "chat", "--model", model, "--one-model")
	rig.skipSetup(t)
	rig.lit("/task solo write plan-rule-proof.txt containing the artifact required by this project's saved instructions, then verify its contents")
	rig.keys("Enter")
	deadline := time.Now().Add(60 * time.Second)
	for !freeConsentDoneNote(home) {
		if time.Now().After(deadline) {
			t.Fatalf("plan task did not finish: %s", rig.capture())
		}
		time.Sleep(pollEvery)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(violations) > 0 || len(assignments) < 2 || !checkerSeen {
		t.Fatalf("scoped opening violations=%v assignments=%d", violations, len(assignments))
	}
	if !planRuleArtifact(home) && !planRuleArtifact(ws) {
		t.Fatal("completed plan has no compliant actual artifact")
	}
	t.Logf("PASS real terminal: %d plan assignments received own project instruction; foreign project instruction absent; actual artifact verified.\n%s", len(assignments), rig.capture())
}

func planRuleArtifact(root string) bool {
	found := false
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && d.Name() == "plan-rule-proof.txt" {
			raw, _ := os.ReadFile(path)
			if strings.TrimSpace(string(raw)) == planRuleMark {
				found = true
			}
		}
		return nil
	})
	return found
}

func TestScopedProjectRulesLivePlanWorkflow(t *testing.T) {
	requireTmuxAndKey(t)
	const model = "deepseek/deepseek-v4.1-flash"
	if e2eModel != model {
		t.Fatalf("requires CODEAF_E2E_MODEL=%s", model)
	}
	home, ws := planRulesHome(t, map[string]any{"memory.enabled": false})
	env := []string{config.APIKeyEnv + "=" + liveKey(t), "CODEAF_BASE_URL=https://openrouter.ai/api/v1", "CODEAF_PROFILE_DIR="}
	stopRecoveryEngineOnCleanup(t, env, home, ws)
	rig := startWithEnv(t, env, "plan_rules_live", home, ws, 160, 46, "chat", "--model", model, "--one-model")
	rig.skipSetup(t)
	rig.lit("Our fictional project needs a small packaging handoff. First inspect the saved instructions for this project and acknowledge that you will use them; don't write any files yet.")
	rig.keys("Enter")
	rig.waitFor(90*time.Second, planRuleMark)
	rig.lit("/task create plan-rule-proof.txt with exactly the project-required marker, then create plan-rule-check.txt recording the actual shell command you ran to verify the proof file, run that verification, and finish only after both files have been checked. Use the saved project instructions without asking me to repeat them.")
	rig.keys("Enter")
	deadline := time.Now().Add(4 * time.Minute)
	for !freeConsentDoneNote(home) {
		if time.Now().After(deadline) {
			t.Fatalf("live plan did not complete: %s", rig.capture())
		}
		time.Sleep(pollEvery)
	}
	if !planRuleArtifact(home) && !planRuleArtifact(ws) {
		t.Fatal("live completed plan has no compliant artifact")
	}
	checked := false
	for _, root := range []string{home, ws} {
		_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err == nil && !d.IsDir() && d.Name() == "plan-rule-check.txt" {
				raw, _ := os.ReadFile(path)
				if len(strings.TrimSpace(string(raw))) > 0 && !strings.Contains(string(raw), otherPlanRuleMark) {
					checked = true
				}
			}
			return nil
		})
	}
	if !checked {
		t.Fatal("live plan did not leave its verification artifact")
	}
	models := callModels(t, home)
	if len(models) == 0 {
		t.Fatal("no live model receipts")
	}
	for _, got := range models {
		if got != model {
			t.Fatalf("unexpected live model %s", got)
		}
	}
	t.Logf("PASS live multi-step plan: scoped rule acknowledged, compliant proof and verification files, completed plan, all %d receipts exact %s.\n%s", len(models), model, rig.capture())
}
