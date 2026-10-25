//go:build e2e

package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	teamstore "github.com/Agent-Field/codeaf/internal/teams"
	"time"
)

// TestChatLaunchModels asks a live chat to choose models through the shipped tools.
// Every auxiliary seat stays on the chat model; only explicitly named work changes.
func TestChatLaunchModels(t *testing.T) {
	requireTmuxAndKey(t)
	const chat = "deepseek/deepseek-v4.1-flash"
	const other = "moonshotai/kimi-k3"
	for _, kind := range []string{"quick_task", "propose_task", "start_chat", "team_start", "joined_task"} {
		t.Run(kind, func(t *testing.T) {
			home := newHome(t, map[string]any{
				"model.talk": chat, "model.work": chat, "model.plan": chat, "task.model": chat,
				"models.tiers.worker": chat, "models.tiers.mastermind": chat, "models.tiers.reflex": chat, "models.tiers.low": chat, "models.tiers.high": chat,
				"models.crew.allowed": chat + "," + other,
				"models.fallbacks":    "", "tools.approvalMode": "allow", "task.audit": "off", "task.max_load": 0, "task.autoapprove_seconds": 1, "task.min_free_mb": 0, "daily_budget_usd": 0, "model_pool": "off",
			})
			ws := newWorkspace(t, "launch-model", false)
			launch := startDefault
			if kind == "quick_task" {
				launch = start
			}
			args := []string{"chat", "--model", chat, "--no-host", "--debug"}
			if kind == "team_start" {
				sid := "55aa55aa55aa55aa"
				bucket := strings.ReplaceAll(filepath.Clean(ws), string(filepath.Separator), "-")
				dir := filepath.Join(home, "v3", "projects", bucket, sid)
				if err := os.MkdirAll(dir, 0700); err != nil {
					t.Fatal(err)
				}
				journal := filepath.Join(dir, "transcript.jsonl")
				header, _ := json.Marshal(map[string]any{"type": "session", "version": 1, "id": sid, "cwd": ws, "model": chat, "timestamp": time.Now().UTC()})
				if err := os.WriteFile(journal, append(header, '\n'), 0600); err != nil {
					t.Fatal(err)
				}
				team := teamstore.Team{ID: teamstore.NewID(), Name: "Model check", Manager: journal, Members: []teamstore.Member{{Key: journal, File: journal, Where: ws, Handle: "manager"}}}
				if err := teamstore.Save(home, []teamstore.Team{team}); err != nil {
					t.Fatal(err)
				}
				args = append(args, "--session", journal)
			}
			r := launch(t, "launch_"+kind, home, ws, 140, 45, args...)
			prompt := "Use " + kind + " to start independent work on model " + other + ". The assignment: read README.md, then report LAUNCH_MODEL_VERIFIED. No files to change. Set model explicitly on the tool call. Do not run the assignment yourself. Use load_tools if needed. If that exact tool is unavailable, report that and stop; do not substitute a different tool. End your turn after the launch receipt."
			if kind == "joined_task" {
				r.lit("Use propose_task with model deepseek/deepseek-v4.1-flash. Assignment: run bash sleep 40, then report FIRST_WORKER_DONE. No edits. Do not run it yourself. After its receipt, answer only these words joined by spaces: First / task / started.")
				r.keys("Enter")
				cleanChatWaitAnswer(t, r, "First task started", 2*time.Minute)
				prompt = "Use propose_task to start another task on model moonshotai/kimi-k3. Read README.md and report LAUNCH_MODEL_VERIFIED. No edits. Set the model argument explicitly. Let it join the current run. End your turn after its receipt."
			}
			if kind == "team_start" {
				prompt += " Give the new member handle scout."
			}
			r.lit(prompt)
			r.keys("Enter")
			deadline := time.Now().Add(5 * time.Minute)
			found := false
			for time.Now().Before(deadline) {
				raw, _ := os.ReadFile(filepath.Join(home, "logs", "calls.jsonl"))
				for _, line := range strings.Split(string(raw), "\n") {
					var row map[string]any
					if json.Unmarshal([]byte(line), &row) == nil && row["model"] == other && row["phase"] != "start" && row["status"] == float64(200) {
						found = true
					}
				}
				wantedTool := kind
				if kind == "joined_task" {
					wantedTool = "propose_task"
				}
				_, reported, _ := launchJournalEvidence(home, wantedTool, other)
				if found && reported {
					break
				}
				time.Sleep(pollEvery)
			}
			cleanChatCapture(t, r, "launch-"+kind)
			wantedTool := kind
			if kind == "joined_task" {
				wantedTool = "propose_task"
			}
			matchingCalls, reported, joined := launchJournalEvidence(home, wantedTool, other)
			if kind == "joined_task" && !joined {
				t.Error("second proposal did not join the first run")
			}
			if !reported {
				t.Error("requested-model child did not complete its answer")
			}
			if matchingCalls == 0 {
				t.Errorf("no %s call explicitly selected %s", wantedTool, other)
			}
			t.Logf("explicit %s calls on requested model: %d; result observed: %v", wantedTool, matchingCalls, reported)
			if !found {
				t.Fatalf("%s never completed a call on %s; screen:\n%s", kind, other, r.capture())
			}
			raw, _ := os.ReadFile(filepath.Join(home, "logs", "calls.jsonl"))
			counts := map[string]int{}
			safe := []map[string]any{}
			for _, line := range strings.Split(string(raw), "\n") {
				var row map[string]any
				if json.Unmarshal([]byte(line), &row) != nil || row["phase"] == "start" {
					continue
				}
				receipt := map[string]any{}
				for _, field := range []string{"id", "phase", "tag", "model", "status"} {
					if value, ok := row[field]; ok {
						receipt[field] = value
					}
				}
				safe = append(safe, receipt)
				model, _ := row["model"].(string)
				if model != "" {
					counts[model]++
					if model != chat && model != other {
						t.Errorf("unexpected model %s", model)
					}
				}
			}
			if dir := os.Getenv("CODEAF_E2E_EVIDENCE_DIR"); dir != "" {
				data, _ := json.MarshalIndent(safe, "", "  ")
				if err := os.WriteFile(filepath.Join(dir, "calls-"+kind+".json"), data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			t.Logf("completed live calls: %v", counts)
			if counts[chat] == 0 {
				t.Fatal("launching chat did not use requested DeepSeek model")
			}
		})
	}
}

// launchJournalEvidence distinguishes the child model from a parent quoting its
// instructions. For a new chat, the target model must occur in another journal.
func launchJournalEvidence(home, tool, model string) (matching int, reported, joined bool) {
	_ = filepath.WalkDir(home, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".jsonl") {
			return nil
		}
		raw, _ := os.ReadFile(path)
		currentModel := ""
		for _, line := range strings.Split(string(raw), "\n") {
			var row struct {
				Type      string `json:"type"`
				Model     string `json:"model"`
				Role      string `json:"role"`
				Content   string `json:"content"`
				ToolCalls []struct {
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"toolCalls"`
			}
			if json.Unmarshal([]byte(line), &row) != nil {
				continue
			}
			if row.Model != "" && (row.Type == "session" || row.Type == "model") {
				currentModel = row.Model
			}
			if strings.Contains(row.Content, "It joined the work already underway") {
				joined = true
			}
			for _, call := range row.ToolCalls {
				var params struct {
					Model string `json:"model"`
				}
				_ = json.Unmarshal([]byte(call.Function.Arguments), &params)
				if call.Function.Name == tool && params.Model == model {
					matching++
				}
				if currentModel == model && call.Function.Name == "bash" && strings.Contains(call.Function.Arguments, "plandb done") && strings.Contains(call.Function.Arguments, "LAUNCH_MODEL_VERIFIED") {
					reported = true
				}
			}
			if currentModel == model && row.Role == "assistant" && strings.Contains(row.Content, "LAUNCH_MODEL_VERIFIED") && len(row.ToolCalls) == 0 {
				reported = true
			}
		}
		return nil
	})
	return
}
