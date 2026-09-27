//go:build e2e

package e2e

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestCleanChat keeps this acceptance on the shipped terminal and a live model.
// --one-model covers every text errand as well as the visible conversation.
func TestCleanChat(t *testing.T) {
	requireTmuxAndKey(t)
	const model = "deepseek/deepseek-v4.1-flash"
	home := newHome(t, map[string]any{
		"model.talk": model, "model.work": model, "model.plan": model,
		"models.tiers.low": model, "models.tiers.high": model,
		"models.tiers.worker": model, "models.tiers.reflex": model,
		"models.tiers.mastermind": model, "task.model": model,
		"models.fallbacks": "", "tools.approvalMode": "allow",
		"models.crew.allowed": model, "model_pool": "off",
		"daily_budget_usd": 0, "task.audit": "off",
		"task.parallel": 1, "task.max_load": 0,
	})
	ws := newWorkspace(t, "clean-chat", false)
	t.Cleanup(func() {
		if t.Failed() {
			cleanChatAuditModels(t, home, model, "chat-failed")
		}
	})
	r := start(t, "clean_chat", home, ws, 140, 42,
		"chat", "--model", model, "--one-model", "--no-host")
	r.lit("Run bash to print CLEAN_CHAT_TOOL_OUTPUT_7319 and then sleep for 3 seconds. Then answer with these words joined by spaces: The / terminal / check / passed. Do not repeat stdout in your answer. Do not delegate this request.")
	r.keys("Enter")
	if _, active := r.glimpse(25*time.Second, "running", "thinking", "working"); active {
		cleanChatCapture(t, r, "00-active")
	}
	screen := cleanChatWaitAnswer(t, r, "The terminal check passed", 3*time.Minute)
	if strings.Contains(screen, "nothing to compact") {
		t.Fatalf("maintenance chatter leaked into the conversation:\n%s", screen)
	}
	if strings.Count(screen, "CLEAN_CHAT_TOOL_OUTPUT_7319") != 1 {
		t.Fatalf("tool details leaked outside the closed work disclosure:\n%s", screen)
	}
	cleanChatCapture(t, r, "01-chat")
	r.keys("C-e")
	// The work disclosure opens the grouped step; the step itself retains its
	// own disclosure for raw command/output detail.
	for i, line := range strings.Split(r.capture(), "\n") {
		if strings.Contains(line, "▸") && strings.Contains(line, "1 call") {
			r.mouseClick(12, i+1)
			break
		}
	}
	deadlineDetails := time.Now().Add(10 * time.Second)
	for strings.Count(r.capture(), "CLEAN_CHAT_TOOL_OUTPUT_7319") < 2 && time.Now().Before(deadlineDetails) {
		time.Sleep(pollEvery)
	}
	if screen = r.capture(); strings.Count(screen, "CLEAN_CHAT_TOOL_OUTPUT_7319") < 2 {
		t.Fatalf("expanding work lost the tool detail:\n%s", screen)
	}
	cleanChatCapture(t, r, "01-expanded")
	r.keys("C-e")
	cleanChatCapture(t, r, "01-closed")
	r.lit("/task solo write a file called hello.txt containing the word hello; no other changes")
	r.keys("Enter")
	screen = r.waitFor(6*time.Minute, say(t, "taskDoneGlyph"), say(t, "taskDoneWord"))
	head := statesHeadLine(screen, say(t, "taskDoneGlyph"), say(t, "taskDoneWord"))
	if head == "" {
		t.Fatalf("no completed task notification:\n%s", screen)
	}
	if strings.Contains(screen, "nothing checked this work") {
		t.Fatalf("the collapsed notification leaked its audit detail:\n%s", screen)
	}
	for _, line := range strings.Split(screen, "\n") {
		line = strings.TrimSpace(line)
		if strings.Contains(line, "single task 1 started") || (strings.HasPrefix(line, "·") && strings.Contains(line, "cached")) {
			t.Fatalf("closed conversation leaked operational receipt at task landing:\n%s", screen)
		}
	}
	cleanChatCapture(t, r, "02-notification")
	r.lit("/dismiss")
	r.keys("Enter")
	deadline := time.Now().Add(10 * time.Second)
	for strings.Contains(r.capture(), strings.TrimSpace(head)) && time.Now().Before(deadline) {
		time.Sleep(pollEvery)
	}
	if screen = r.capture(); strings.Contains(screen, strings.TrimSpace(head)) {
		t.Fatalf("dismiss left the completed notification visible:\n%s", screen)
	}
	cleanChatCapture(t, r, "03-dismissed")
	r.lit("/dismiss undo")
	r.keys("Enter")
	r.waitFor(10*time.Second, strings.TrimSpace(head))
	cleanChatCapture(t, r, "04-restored")
	r.quit()
	cleanChatAuditModels(t, home, model, "chat")
}

// Save the actual tmux framebuffer, including ANSI colours, as an asciicast.
// An optional directory keeps evidence outside Go's temporary test home. These
// are screen captures, not reconstructed layouts or a claim of continuous video.
func cleanChatCapture(t *testing.T, r *rig, stage string) {
	t.Helper()
	dir := os.Getenv("CODEAF_E2E_EVIDENCE_DIR")
	if dir == "" {
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	frame, err := exec.Command("tmux", "capture-pane", "-p", "-e", "-t", r.name).Output()
	if err != nil {
		t.Fatal(err)
	}
	header, _ := json.Marshal(map[string]any{"version": 2, "width": 140, "height": 42, "title": stage})
	output, _ := json.Marshal([]any{0.0, "o", "\x1b[?25l\x1b[2J\x1b[H" + strings.ReplaceAll(strings.TrimRight(string(frame), "\n"), "\n", "\r\n")})
	hold, _ := json.Marshal([]any{2.0, "o", ""})
	data := append(append(append(header, '\n'), output...), '\n')
	data = append(append(data, hold...), '\n')
	if err := os.WriteFile(filepath.Join(dir, stage+".cast"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, stage+".txt"), []byte(r.capture()), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Logf("captured real tmux frame: %s", filepath.Join(dir, stage+".cast"))
}

// TestCleanManagerReplay resumes a representative manager journal, then asks
// the live model a follow-up. The historical team delivery is a deterministic
// fixture, not a claim that a live teammate sent it during this run.
func TestCleanManagerReplay(t *testing.T) {
	requireTmuxAndKey(t)
	const model = "deepseek/deepseek-v4.1-flash"
	home := newHome(t, map[string]any{"model.talk": model, "tools.approvalMode": "allow", "daily_budget_usd": 0, "models.crew.allowed": model, "model_pool": "off"})
	ws := newWorkspace(t, "manager-replay", false)
	t.Cleanup(func() {
		if t.Failed() {
			cleanChatAuditModels(t, home, model, "manager-failed")
		}
	})
	journal := filepath.Join(t.TempDir(), "manager.jsonl")
	rows := []map[string]any{
		{"type": "session", "version": 1, "id": "abc0123456789def", "cwd": ws, "model": model, "timestamp": time.Now().UTC().Format(time.RFC3339)},
		{"type": "message", "role": "user", "content": "Check the review status with the team."},
		{"type": "message", "role": "assistant", "content": "Checking the reviewers.", "toolCalls": []map[string]any{{"id": "team1", "type": "function", "function": map[string]any{"name": "team_send", "arguments": `{"to":"@reviewer","text":"Please check the review status.","kind":"directive"}`}}}},
		{"type": "message", "role": "tool", "toolCallId": "team1", "content": "sent #1 to @reviewer"},
		{"type": "message", "role": "user", "note": true, "content": "Your team's replies started this turn; the person did not speak. Act on them: hand out what comes next, or tell the person where the work stands."},
		{"type": "message", "role": "user", "note": true, "content": "Team traffic in \"review\" for you (manager). These are the team's messages, not the person's words:\nfrom @reviewer to manager #2: PRIVATE_TEAM_RECEIPT_4721 checks passed"},
		{"type": "message", "role": "assistant", "content": "## Review ready\n\n**All checks passed.** The change is ready for your review."},
	}
	var data []byte
	for _, row := range rows {
		line, err := json.Marshal(row)
		if err != nil {
			t.Fatal(err)
		}
		data = append(append(data, line...), '\n')
	}
	if err := os.WriteFile(journal, data, 0o600); err != nil {
		t.Fatal(err)
	}
	r := start(t, "clean_manager", home, ws, 140, 42, "chat", "--session", journal, "--model", model, "--one-model", "--no-host")
	screen := r.waitFor(30*time.Second, "All checks passed.")
	for _, hidden := range []string{"PRIVATE_TEAM_RECEIPT_4721", "Your team's replies started", "## Review ready", "**All checks passed.**"} {
		if strings.Contains(screen, hidden) {
			t.Fatalf("manager replay leaked %q:\n%s", hidden, screen)
		}
	}
	cleanChatCapture(t, r, "05-manager-replay")
	r.lit("Answer with only the words Still / ready / for / review. joined by spaces. No tools or delegation.")
	r.keys("Enter")
	cleanChatWaitAnswer(t, r, "Still ready for review", 2*time.Minute)
	cleanChatCapture(t, r, "06-manager-live-followup")
	r.quit()
	cleanChatAuditModels(t, home, model, "manager")
}

func TestCleanChatFailureRecovery(t *testing.T) {
	requireTmuxAndKey(t)
	const model = "deepseek/deepseek-v4.1-flash"
	home := newHome(t, map[string]any{"model.talk": model, "tools.approvalMode": "allow", "daily_budget_usd": 0, "models.crew.allowed": model, "model_pool": "off"})
	ws := newWorkspace(t, "failure-recovery", false)
	t.Cleanup(func() {
		if t.Failed() {
			cleanChatAuditModels(t, home, model, "recovery-failed")
		}
	})
	r := start(t, "clean_recovery", home, ws, 140, 42, "chat", "--model", model, "--one-model", "--no-host")
	r.lit("Test error recovery with two separate bash calls: first run printf 'CLEAN_%s\\n' FAILURE_TRACE_7319; exit 7. This failure is intentional. Then recover with a second bash call: sleep 3; printf 'recovered\\n'. Finally answer only with Recovery / check / passed. joined by spaces. Do not echo stdout, delegate, or write files.")
	r.keys("Enter")
	deadline := time.Now().Add(3 * time.Minute)
	captured := false
	for time.Now().Before(deadline) {
		screen := r.capture()
		if strings.Contains(screen, "CLEAN_FAILURE_TRACE_7319") {
			t.Fatalf("raw failed-call output autoexpanded:\n%s", screen)
		}
		if !captured && (strings.Contains(screen, "failed") || strings.Contains(screen, "✕")) && strings.Contains(screen, "working") {
			cleanChatCapture(t, r, "07-failure-recovery-active")
			captured = true
		}
		if strings.Contains(screen, keyedWord("2", "tool calls")) && strings.Contains(screen, "idle") {
			break
		}
		time.Sleep(pollEvery)
	}
	screen := r.waitFor(2*time.Second, keyedWord("2", "tool calls"), "idle")
	if strings.Contains(screen, "CLEAN_FAILURE_TRACE_7319") {
		t.Fatalf("failed-call detail leaked after recovery:\n%s", screen)
	}
	cleanChatCapture(t, r, "08-recovery-finished")
	// The fixture requires a genuine failed tool result, not just a model that
	// answered the requested phrase without exercising the terminal path.
	journals, err := filepath.Glob(filepath.Join(home, "v3", "projects", "*", "*", "transcript.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	failed := false
	for _, p := range journals {
		raw, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), "CLEAN_FAILURE_TRACE_7319") {
			failed = true
		}
	}
	if !failed {
		t.Fatal("live model never produced the required failed-call output")
	}
	r.quit()
	cleanChatAuditModels(t, home, model, "recovery")
}

func cleanChatWaitAnswer(t *testing.T, r *rig, want string, within time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		screen := r.capture()
		if cleanChatHasAnswer(screen, want) {
			return screen
		}
		time.Sleep(pollEvery)
	}
	t.Fatalf("no assistant answer %q:\n%s", want, r.capture())
	return ""
}

func cleanChatAuditModels(t *testing.T, home, model, stage string) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(home, "logs", "calls.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var safe []map[string]any
	completed := 0
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var row map[string]any
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			t.Fatal(err)
		}
		entry := map[string]any{}
		for _, key := range []string{"ts", "id", "phase", "tag", "model", "status"} {
			if v, ok := row[key]; ok {
				entry[key] = v
			}
		}
		safe = append(safe, entry)
		if row["phase"] != "start" {
			completed++
		}
		if used, ok := row["model"].(string); ok && used != "" && used != model {
			t.Errorf("unexpected live model %q for tag %v", used, row["tag"])
		}
	}
	if len(safe) == 0 {
		t.Fatal("no live model records")
	}
	if dir := os.Getenv("CODEAF_E2E_EVIDENCE_DIR"); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		data, err := json.MarshalIndent(safe, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, stage+"-models.json"), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("%s: %d completed model requests; every call-log record checked against %s", stage, completed, model)
}

func TestCleanChatSteering(t *testing.T) {
	requireTmuxAndKey(t)
	const model = "deepseek/deepseek-v4.1-flash"
	home := newHome(t, map[string]any{"model.talk": model, "tools.approvalMode": "allow", "daily_budget_usd": 0, "models.crew.allowed": model, "model_pool": "off"})
	ws := newWorkspace(t, "steering", false)
	t.Cleanup(func() {
		if t.Failed() {
			cleanChatAuditModels(t, home, model, "steering-failed")
		}
	})
	r := start(t, "clean_steering", home, ws, 140, 42, "chat", "--model", model, "--one-model", "--no-host")
	r.lit("Run bash sleep 8; printf 'first check finished\\n'. Then run bash sleep 3; printf 'second check finished\\n'. Between the two calls emit this exact user-facing update using the update protocol: [update] First check is complete. At the end say checks finished. Do not delegate or write files.")
	r.keys("Enter")
	r.waitFor(45*time.Second, "working")
	// Send real keyboard input while the first command is still running.
	const steer = "Also mention that no files were changed in your final answer."
	r.lit(steer)
	r.keys("Enter")
	r.waitFor(10*time.Second, steer)
	cleanChatCapture(t, r, "09-user-steering")
	interim := cleanChatWaitAnswer(t, r, "First check is complete", 2*time.Minute)
	if !strings.Contains(interim, "working") {
		t.Fatalf("interim update was not visible while work continued:\n%s", interim)
	}
	cleanChatCapture(t, r, "09-assistant-update")
	var screen string
	finished := false
	for deadline := time.Now().Add(3 * time.Minute); time.Now().Before(deadline); {
		screen = r.capture()
		if strings.Contains(screen, "idle") {
			for _, line := range strings.Split(screen, "\n") {
				if strings.Contains(strings.ToLower(line), "checks finished") && !strings.Contains(strings.ToLower(line), "say checks finished") {
					finished = true
					break
				}
			}
		}
		if finished {
			break
		}
		time.Sleep(pollEvery)
	}
	if !finished {
		t.Fatalf("steered conversation never finished:\n%s", screen)
	}
	if !strings.Contains(screen, steer) {
		t.Fatalf("steering disappeared after the turn finished:\n%s", screen)
	}
	if !strings.Contains(strings.ToLower(screen), "no files") {
		t.Fatalf("the assistant did not retain the steering request:\n%s", screen)
	}
	if !cleanChatHasAnswer(screen, "First check is complete") {
		t.Fatalf("intended assistant update disappeared after subsequent work:\n%s", screen)
	}
	// The user explicitly typed the protocol once; only that original request
	// may contain its raw marker, never the assistant's rendered update.
	if strings.Count(screen, "[update]") > 1 {
		t.Fatalf("raw assistant update marker leaked:\n%s", screen)
	}
	cleanChatCapture(t, r, "10-steering-finished")
	journals, err := filepath.Glob(filepath.Join(home, "v3", "projects", "*", "*", "transcript.jsonl"))
	if err != nil || len(journals) != 1 {
		t.Fatalf("cannot identify steering journal: %v %v", journals, err)
	}
	r.quit()
	resumed := start(t, "clean_steering_resumed", home, ws, 140, 42, "chat", "--session", journals[0], "--model", model, "--one-model", "--no-host")
	screen = cleanChatWaitAnswer(t, resumed, "First check is complete", 30*time.Second)
	if !strings.Contains(screen, steer) || !strings.Contains(strings.ToLower(screen), "checks finished") {
		t.Fatalf("reopening lost the user steer or final response:\n%s", screen)
	}
	if strings.Count(screen, "[update]") > 1 {
		t.Fatalf("reopening leaked the assistant marker:\n%s", screen)
	}
	cleanChatCapture(t, resumed, "11-steering-resumed")
	resumed.quit()
	cleanChatAuditModels(t, home, model, "steering")
}

func cleanChatHasAnswer(screen, want string) bool {
	for _, line := range strings.Split(screen, "\n") {
		// The closed task rail paints its handle in the rightmost gutter. A
		// coincident answer row still contains only the answer in the chat column.
		if strings.HasSuffix(line, "❮") {
			before := strings.TrimSuffix(line, "❮")
			if strings.HasSuffix(before, "    ") {
				line = before
			}
		}
		line = strings.Trim(strings.ReplaceAll(strings.TrimSpace(line), " / ", " "), ". ")
		if line == want {
			return true
		}
	}
	return false
}

func TestCleanChatAnswerIgnoresOnlyTheRailGutter(t *testing.T) {
	for _, tc := range []struct {
		line string
		want bool
	}{
		{"  Still ready for review", true},
		{"  Still ready for review.                    ❮", true},
		{"  Still ready for review❮", false},
		{"  Still ready for review but not yet", false},
		{"  › Still ready for review", false},
		{"  › Answer with only the words Still / ready / for / review.", false},
	} {
		if got := cleanChatHasAnswer(tc.line, "Still ready for review"); got != tc.want {
			t.Errorf("answer match for %q = %v, want %v", tc.line, got, tc.want)
		}
	}
}
