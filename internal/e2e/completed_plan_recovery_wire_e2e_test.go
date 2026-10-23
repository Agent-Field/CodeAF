//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
)

func TestCompletedPlanRecoversAndAcceptsOnTheRealEngine(t *testing.T) {
	runCompletedPlanRecovery(t, false)
}
func TestCompletedPlanRecoveryLiveWorkflow(t *testing.T) { runCompletedPlanRecovery(t, true) }

// The worker finishes ordinary fictional work. A harness-owned one-shot git
// hook pauses only its disposable engine after completion and before landing.
func runCompletedPlanRecovery(t *testing.T, live bool) {
	if runtime.GOOS != "linux" {
		t.Skip("owned engine identity check uses Linux proc metadata")
	}
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux required")
	}
	model := "stub/completed-plan"
	key := "fixture-completed-plan-key"
	if live {
		requireTmuxAndKey(t)
		model = "deepseek/deepseek-v4.1-flash"
		if e2eModel != model {
			t.Fatalf("requires CODEAF_E2E_MODEL=%s", model)
		}
		key = liveKey(t)
	}
	rows := map[string]any{"memory.enabled": false}
	if !live {
		var pins []string
		for _, role := range textRoles {
			pins = append(pins, string(role)+":"+model)
		}
		rows[config.KeyModelRoles] = strings.Join(pins, ",")
		for _, name := range []string{config.KeyChatModel, config.KeyTaskModel, config.KeyTierLowModel, config.KeyTierWorkerModel, config.KeyTierHighModel, config.KeyTierReflexModel, config.KeyModelFallbacks} {
			rows[name] = model
		}
	}
	home := recoveryShortHome(t, newHome(t, rows))
	ws := newWorkspace(t, "completed-plan", false)
	statesWorkBranch(t, ws)
	var mu sync.Mutex
	assignmentCalls := 0
	accepted := false
	base := "https://openrouter.ai/api/v1"
	if !live {
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
						Role    string          `json:"role"`
						Content json.RawMessage `json:"content"`
					} `json:"messages"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					http.Error(w, "fixture decode", 400)
					return
				}
				var assignment []string
				acceptRequest := false
				var receipt string
				for _, msg := range body.Messages {
					if m := freeConsentAssignment.FindStringSubmatch(string(msg.Content)); len(m) == 3 {
						assignment = m
					}
					if msg.Role == "user" && strings.Contains(string(msg.Content), "ACCEPT_RECOVERED_PROOF") {
						acceptRequest = true
					}
					if msg.Role == "tool" && strings.Contains(string(msg.Content), "work commit") {
						_ = json.Unmarshal(msg.Content, &receipt)
					}
				}
				if len(assignment) == 3 {
					mu.Lock()
					assignmentCalls++
					mu.Unlock()
					command := fmt.Sprintf("printf 'MOCK_RECOVERY_VALUE\\n' > recover.txt && printf 'grep -qx MOCK_RECOVERY_VALUE recover.txt\\n' > recover-check.txt && sh recover-check.txt && plandb done %s --agent %s --result 'MOCK_RECOVERY_VALUE verified'", assignment[1], assignment[2])
					args, _ := json.Marshal(map[string]string{"command": command})
					terminalStopResponse(w, model, "", map[string]any{"index": 0, "id": "recovery-finish-" + assignment[2], "type": "function", "function": map[string]string{"name": "bash", "arguments": string(args)}}, body.Stream)
					return
				}
				if acceptRequest && receipt == "" {
					mu.Lock()
					first := !accepted
					accepted = true
					mu.Unlock()
					if first {
						terminalStopResponse(w, model, "", map[string]any{"index": 0, "id": "accept-recovered", "type": "function", "function": map[string]string{"name": "tasks", "arguments": `{"id":"1","resolve":"accept","say":"independent saved artifact check passed"}`}}, body.Stream)
						return
					}
				}
				answer := "Acknowledged."
				if receipt != "" {
					answer = receipt
				}
				terminalStopResponse(w, model, answer, nil, body.Stream)
			default:
				http.NotFound(w, r)
			}
		}))
		defer server.Close()
		base = server.URL
	}
	env := []string{config.APIKeyEnv + "=" + key, "CODEAF_BASE_URL=" + base, "CODEAF_PROFILE_DIR=", "CODEAF_DEBUG=0", "CODEAF_CALL_LOG_BODIES=0"}
	stopRecoveryEngineOnCleanup(t, env, home, ws)
	rig := startWithEnv(t, env, "completed_recovery", home, ws, 180, 50, "chat", "--model", model, "--one-model", "--yolo")
	rig.skipSetup(t)
	intro := "This disposable fictional project tests recovery. We will preserve a finished task's files through an explicitly controlled engine restart. Acknowledge without delegating or editing files."
	rig.lit(intro)
	rig.keys("Enter")
	if answer, _ := privateMemoryAwaitAnswer(home, intro, 90*time.Second); answer == "" {
		t.Fatal("recovery introduction did not complete")
	}
	status := upgradeEngineCommand(t, binary(t), env, home, ws, "--status")
	pid, err := strconv.Atoi(upgradePID(status))
	if err != nil {
		t.Fatalf("no actual engine PID: %s", status)
	}
	assertCompletedEngineIdentity(t, pid, home, ws)
	t.Cleanup(func() {
		if completedEnginePaused(pid) && completedEngineIdentity(pid, home, ws) {
			_ = syscall.Kill(pid, syscall.SIGCONT)
		}
	})
	beforeHead := strings.TrimSpace(completedGit(t, ws, "rev-parse", "HEAD"))
	marker := installCompletedPauseHook(t, pid, home, ws)
	prompt := "/task solo write recover.txt containing exactly MOCK_RECOVERY_VALUE, write recover-check.txt containing exactly the runnable shell command grep -qx MOCK_RECOVERY_VALUE recover.txt, run that check, and finish normally using plandb done with your actual assigned task id and claimed agent and result 'MOCK_RECOVERY_VALUE verified'. Do not delegate further or make git commits yourself; leave commits and landing to the runtime."

	rig.lit(prompt)
	rig.keys("Enter")
	deadline := time.Now().Add(2 * time.Minute)
	for !completedEnginePaused(pid) {
		if time.Now().After(deadline) {
			t.Fatalf("actual completed run never reached its guarded landing pause: %s", rig.capture())
		}
		time.Sleep(20 * time.Millisecond)
	}
	// The guarded one-shot git hook stops the engine after real worker spend
	// and completion exist. Confirm that barrier and the untouched checkout.
	plan := completedPlanPath(t, home)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	cmd := guardedCommand(t, ctx, home, append(append(os.Environ(), env...), "CODEAF_HOME="+home), binary(t), "plandb", "show", "t-1", "--db", plan, "--json")
	out, err := cmd.CombinedOutput()
	cancel()
	if err != nil || !strings.Contains(string(out), `"status": "done"`) {
		t.Fatalf("paused engine has no durable done root: %v %s", err, out)
	}
	if completedGit(t, ws, "rev-parse", "HEAD") != beforeHead || fileExists(filepath.Join(ws, "recover.txt")) {
		t.Fatal("fixture did not stop before original checkout landing")
	}
	if !fileExists(marker) {
		t.Fatal("actual guarded git hook did not record its completion barrier")
	}
	originalHook := completedGit(t, ws, "rev-parse", "--git-path", "hooks/prepare-commit-msg")
	if !filepath.IsAbs(originalHook) {
		originalHook = filepath.Join(ws, originalHook)
	}
	if fileExists(originalHook) {
		t.Fatal("one-shot original hook was not removed")
	}
	beforeLedger := completedTaskLedger(t, home, env, plan)
	if beforeLedger <= 0 {
		t.Fatal("completed worker has no actual task ledger spend")
	}
	checkpointBefore := completedCheckpointCost(home)
	t.Logf("Paused completed root: task ledger=%g; running checkpoint cost=%g.", beforeLedger, checkpointBefore)
	proof := completedProofDir(t, home)
	checkCompletedProof(t, proof)
	lock := completedGit(t, proof, "rev-parse", "--git-path", "index.lock")
	if !filepath.IsAbs(lock) {
		lock = filepath.Join(proof, lock)
	}
	for until := time.Now().Add(5 * time.Second); fileExists(lock); {
		if time.Now().After(until) {
			t.Fatal("hook's git process did not release its exact index lock")
		}
		time.Sleep(20 * time.Millisecond)
	}
	beforeSpend := stopSpendSnapshot(t, home)
	if beforeSpend.Cost <= 0 {
		t.Fatal("interrupted live work recorded no spend")
	}
	mu.Lock()
	callsBefore := assignmentCalls
	mu.Unlock()
	t.Logf("Actual engine paused after durable plan completion, original checkout unlanded; recorded cost=%g.\n%s", beforeSpend.Cost, rig.capture())
	assertCompletedEngineIdentity(t, pid, home, ws)
	if err := syscall.Kill(pid, syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	for deadline = time.Now().Add(40 * time.Second); ; {
		after := upgradeEngineCommand(t, binary(t), env, home, ws, "--status")
		newPID := upgradePID(after)
		if newPID != "" && newPID != strconv.Itoa(pid) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("actual window did not reconnect: %s", after)
		}
		time.Sleep(200 * time.Millisecond)
	}
	// Inspect the saved record before explicit acceptance; no worker is allowed
	// to replay completed work or silently merge it during reconnection.
	for deadline = time.Now().Add(20 * time.Second); !completedReviewRecorded(home); {
		if time.Now().After(deadline) {
			t.Fatal("completed root did not become actionable saved work")
		}
		time.Sleep(100 * time.Millisecond)
	}
	if completedGit(t, ws, "rev-parse", "HEAD") != beforeHead {
		t.Fatal("recovery auto-landed without explicit acceptance")
	}
	if got := stopSpendSnapshot(t, home); got != beforeSpend {
		t.Fatalf("recovery changed model receipts/spend before explicit acceptance: before=%+v after=%+v", beforeSpend, got)
	}
	mu.Lock()
	nowCalls := assignmentCalls
	mu.Unlock()
	if nowCalls != callsBefore {
		t.Fatal("completed worker was rerun after engine restart")
	}
	if after := completedTaskLedger(t, home, env, plan); after != beforeLedger {
		t.Fatalf("task ledger changed across restart: %g -> %g", beforeLedger, after)
	}
	if cost := completedCheckpointCost(home); cost+1e-9 < beforeLedger {
		t.Fatalf("recovered task/index lost its actual spend: cost=%g ledger=%g", cost, beforeLedger)
	}
	checkCompletedProof(t, proof)
	// Wait for the actual restored card, then let its input settle guard expire.
	// Reading the persisted recovery record alone does not mean the UI is ready.
	screen := rig.capture()
	for until := time.Now().Add(10 * time.Second); (!strings.Contains(screen, "a accept") || !strings.Contains(screen, "n not right")) && time.Now().Before(until); {
		time.Sleep(100 * time.Millisecond)
		screen = rig.capture()
	}
	if !strings.Contains(screen, "a accept") || !strings.Contains(screen, "n not right") {
		t.Fatalf("restored UI offers no actual acceptance card: %s", screen)
	}
	time.Sleep(350 * time.Millisecond)
	t.Logf("Actual restored UI before explicit acceptance:\n%s", rig.capture())
	rig.keys("a", "Enter")
	for deadline = time.Now().Add(2 * time.Minute); completedCommitReceipt(t, home) == "" || completedGit(t, ws, "rev-parse", "HEAD") == beforeHead; {
		if time.Now().After(deadline) {
			t.Fatalf("actual explicit acceptance did not produce a landed commit receipt: %s", rig.capture())
		}
		time.Sleep(100 * time.Millisecond)
	}

	finalHead := completedGit(t, ws, "rev-parse", "HEAD")
	if finalHead == beforeHead {
		t.Fatal("explicit acceptance did not land a commit")
	}
	checkCompletedProof(t, ws)
	if cost := completedCheckpointCost(home); cost+1e-9 < beforeLedger {
		t.Fatalf("accepted task/index lost its actual spend: cost=%g ledger=%g", cost, beforeLedger)
	}
	receipt := completedCommitReceipt(t, home)
	if receipt == "" {
		t.Fatal("accepted task has no actual work commit receipt")
	}
	if err := exec.Command("git", "-C", ws, "merge-base", "--is-ancestor", receipt, "HEAD").Run(); err != nil {
		t.Fatalf("receipt commit was not landed: %s", receipt)
	}
	assertCompletedPlanPrice(t, rig, beforeLedger)
	models := callModels(t, home)
	if live {
		for _, got := range models {
			if got != model {
				t.Fatalf("unexpected live model %s", got)
			}
		}
	}
	t.Logf("PASS actual engine recovery live=%t: done root survived restart without replay, spend preserved, saved check rerun, explicit acceptance landed commit %s; %d model receipts (%s).\n%s", live, receipt, len(models), model, rig.capture())
}

func completedGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("fixture git: %v %s", err, out)
	}
	return strings.TrimSpace(string(out))
}
func fileExists(path string) bool { _, err := os.Stat(path); return err == nil }
func completedEnginePaused(pid int) bool {
	raw, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return false
	}
	end := strings.LastIndexByte(string(raw), ')')
	return end >= 0 && len(raw) > end+2 && (raw[end+2] == 'T' || raw[end+2] == 't')
}
func assertCompletedEngineIdentity(t *testing.T, pid int, home, ws string) {
	t.Helper()
	if !completedEngineIdentity(pid, home, ws) {
		t.Fatal("refusing signal: PID is not the isolated workspace/profile engine")
	}
}
func completedEngineIdentity(pid int, home, ws string) bool {
	cmd, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
	if err != nil || !strings.Contains(string(cmd), "engine\x00") || !strings.Contains(string(cmd), "--workspace\x00"+ws+"\x00") {
		return false
	}
	env, err := os.ReadFile(fmt.Sprintf("/proc/%d/environ", pid))
	return err == nil && strings.Contains(string(env), "CODEAF_HOME="+home+"\x00")
}

func completedPlanPath(t *testing.T, home string) string {
	t.Helper()
	var found string
	_ = filepath.WalkDir(home, func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && d.Name() == "plandb.db" {
			found = path
		}
		return nil
	})
	if found == "" {
		t.Fatal("actual plan database missing")
	}
	return found
}
func completedProofDir(t *testing.T, home string) string {
	t.Helper()
	var found string
	_ = filepath.WalkDir(home, func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && d.Name() == "recover.txt" {
			found = filepath.Dir(path)
		}
		return nil
	})
	if found == "" {
		t.Fatal("saved actual task artifact missing")
	}
	return found
}
func checkCompletedProof(t *testing.T, dir string) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, "recover.txt"))
	if err != nil || strings.TrimSpace(string(raw)) != "MOCK_RECOVERY_VALUE" {
		t.Fatalf("preserved proof differs: %v %q", err, raw)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", "recover-check.txt")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("actual preserved check failed: %v %s", err, out)
	}
}
func completedReviewRecorded(home string) bool {
	return strings.Contains(completedRunRecords(home), "final checks and landing were not recorded")
}
func completedRunRecords(home string) string {
	var out strings.Builder
	_ = filepath.WalkDir(home, func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && d.Name() == "tasks.json" {
			raw, _ := os.ReadFile(path)
			out.Write(raw)
		}
		return nil
	})
	return out.String()
}
func completedCommitReceipt(t *testing.T, home string) string {
	t.Helper()
	match := regexp.MustCompile(`work commit ([0-9a-f]{40})`).FindStringSubmatch(completedRunRecords(home))
	if len(match) > 1 {
		return match[1]
	}
	return ""
}

func installCompletedPauseHook(t *testing.T, pid int, home, ws string) string {
	t.Helper()
	var transcripts []string
	_ = filepath.WalkDir(home, func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && d.Name() == "transcript.jsonl" {
			transcripts = append(transcripts, path)
		}
		return nil
	})
	if len(transcripts) != 1 {
		t.Fatalf("expected exactly one current conversation before the task, got %d", len(transcripts))
	}
	plan := filepath.Join(filepath.Dir(transcripts[0]), "plandb.db")
	hook := completedGit(t, ws, "rev-parse", "--git-path", "hooks/prepare-commit-msg")
	if !filepath.IsAbs(hook) {
		hook = filepath.Join(ws, hook)
	}
	marker := filepath.Join(home, "completed-hook-fired.json")
	fixture, _ := json.Marshal(map[string]any{"pid": pid, "home": home, "workspace": ws, "binary": binary(t), "plan": plan, "marker": marker, "hook": hook})
	script := "#!/usr/bin/env python3\nimport json, os, pathlib, signal, subprocess, sys\nfixture = json.loads(" + strconv.Quote(string(fixture)) + ")\n" + `
# Every child inherits the engine's already guarded, isolated fixture environment.
pid=fixture['pid']
command=pathlib.Path('/proc/%d/cmdline'%pid).read_bytes()
environment=pathlib.Path('/proc/%d/environ'%pid).read_bytes()
if b'engine\0' not in command or ('--workspace\0'+fixture['workspace']+'\0').encode() not in command:
    raise SystemExit(1)
if ('CODEAF_HOME='+fixture['home']+'\0').encode() not in environment:
    raise SystemExit(1)
if pathlib.Path('recover.txt').read_text().strip()!='MOCK_RECOVERY_VALUE':
    raise SystemExit(1)
if pathlib.Path('recover-check.txt').read_text().strip()!='grep -qx MOCK_RECOVERY_VALUE recover.txt':
    raise SystemExit(1)
subprocess.run(['sh','recover-check.txt'],check=True)
base=[fixture['binary'],'plandb']
root=json.loads(subprocess.check_output(base+['show','t-1','--db',fixture['plan'],'--json']))
if root.get('id')!='t-1' or root.get('status')!='done':
    raise SystemExit(1)
ledger=json.loads(subprocess.check_output(base+['spend','--by','task','--db',fixture['plan'],'--json']))
usd=sum(row['usd'] for row in ledger if row['key']=='1')
if usd<=0:
    raise SystemExit(1)
pathlib.Path(fixture['marker']).write_text(json.dumps({'root':'1','task_usd':usd}))
for owned_hook in {pathlib.Path(__file__).resolve(),pathlib.Path(fixture['hook']).resolve()}:
    owned_hook.unlink(missing_ok=True)
os.kill(pid,signal.SIGSTOP) # Only the exact verified disposable engine.
raise SystemExit(1) # Let git release its index lock; no sleeper/commit survives.
`
	if err := os.WriteFile(hook, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(hook) })
	return marker
}

func completedTaskLedger(t *testing.T, home string, env []string, plan string) float64 {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := guardedCommand(t, ctx, home, append(append(os.Environ(), env...), "CODEAF_HOME="+home), binary(t), "plandb", "spend", "--by", "task", "--db", plan, "--json")
	raw, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("actual task ledger CLI: %v %s", err, raw)
	}
	var rows []struct {
		Key string  `json:"key"`
		USD float64 `json:"usd"`
	}
	if err := json.Unmarshal(raw, &rows); err != nil {
		t.Fatal(err)
	}
	var usd float64
	for _, row := range rows {
		if row.Key == "1" {
			usd += row.USD
		}
	}
	return usd
}
func completedCheckpointCost(home string) float64 {
	var cost float64
	_ = filepath.WalkDir(home, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || d.Name() != "tasks.json" {
			return nil
		}
		raw, _ := os.ReadFile(path)
		var record struct {
			Runs []struct {
				ID  uint64  `json:"id"`
				USD float64 `json:"costUsd"`
			} `json:"runs"`
		}
		if json.Unmarshal(raw, &record) == nil {
			for _, run := range record.Runs {
				if run.ID == 1 {
					cost = run.USD
				}
			}
		}
		return nil
	})
	return cost
}
func assertCompletedPlanPrice(t *testing.T, rig *rig, usd float64) {
	t.Helper()
	openTasksPlace(t, rig)
	tasksPlaceRunRow(t, rig)
	rig.keys("Enter")
	screen := rig.waitFor(15*time.Second, say(t, "roomTabsWords"))
	if !strings.Contains(screen, say(t, "roomTabsWords")) {
		t.Fatal("recovered actual plan room did not load")
	}
	price := fmt.Sprintf("$%.2f", usd)
	if usd < 0.01 {
		price = config.SubCent(usd)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		screen = rig.capture()
		for _, line := range strings.Split(screen, "\n") {
			if strings.Contains(line, "done") && strings.Contains(line, price) {
				t.Logf("Actual recovered task-room header preserves ledger price %s:\n%s", price, screen)
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("actual recovered task header lost ledger price %s: %s", price, screen)
		}
		time.Sleep(100 * time.Millisecond)
	}
}
