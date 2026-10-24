//go:build e2e

package e2e

import (
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestHistoryCopyKeyboard(t *testing.T) {
	requireTmuxAndKey(t)
	const model = "deepseek/deepseek-v4.1-flash"
	if e2eModel != model {
		t.Fatal("pin every live role to " + model)
	}
	home := newHome(t, map[string]any{"model.talk": model, "models.fallbacks": "", "model_pool": "off", "daily_budget_usd": 0})
	ws := newWorkspace(t, "history-copy", false)
	r := start(t, "history_copy", home, ws, 140, 42, "chat", "--model", model, "--one-model", "--no-host")
	stream := filepath.Join(t.TempDir(), "terminal.raw")
	if out, err := exec.Command("tmux", "pipe-pane", "-O", "-t", r.name,
		"cat > '"+strings.ReplaceAll(stream, "'", "'\\''")+"'").CombinedOutput(); err != nil {
		t.Fatalf("record terminal output: %v %s", err, out)
	}
	r.lit("Reply only with FIRST_HISTORY_REPLY. No tools.")
	r.keys("Enter")
	cleanChatWaitAnswer(t, r, "FIRST_HISTORY_REPLY", 2*time.Minute)
	r.lit("Reply only with SECOND_HISTORY_REPLY.")
	r.keys("M-Enter")
	r.lit("No tools. This is the second line.")
	r.keys("Enter")
	cleanChatWaitAnswer(t, r, "SECOND_HISTORY_REPLY", 2*time.Minute)
	r.lit("UNSENT_DRAFT_7319")
	r.keys("Up")
	r.waitFor(5*time.Second, "No tools. This is the second line.")
	r.keys("Up")
	screen := r.capture()
	// Only the draft region is relevant: the transcript retains both prompts.
	lines := strings.Split(screen, "\n")
	draft := strings.Join(lines[max(0, len(lines)-8):], "\n")
	if !strings.Contains(draft, "FIRST_HISTORY_REPLY") || strings.Contains(draft, "second line") {
		t.Fatalf("second up did not reach the previous prompt:\n%s", screen)
	}
	r.keys("Down", "Down")
	r.waitFor(5*time.Second, "UNSENT_DRAFT_7319")
	r.keys("C-u")
	r.lit("/copy")
	r.keys("Enter")
	r.waitFor(5*time.Second, "enter copy")
	r.keys("End", "a", "Enter")
	r.waitFor(5*time.Second, "clipboard sent")
	deadline := time.Now().Add(5 * time.Second)
	var payload []byte
	for time.Now().Before(deadline) {
		raw, err := os.ReadFile(stream)
		if err != nil {
			t.Fatal(err)
		}
		matches := regexp.MustCompile("\\x1b\\]52;c;([A-Za-z0-9+/=]*)\\a").FindAllSubmatch(raw, -1)
		if len(matches) > 0 {
			payload, err = base64.StdEncoding.DecodeString(string(matches[len(matches)-1][1]))
			if err != nil {
				t.Fatal(err)
			}
			break
		}
		time.Sleep(pollEvery)
	}
	if strings.TrimSpace(string(payload)) != "SECOND_HISTORY_REPLY" {
		t.Fatalf("terminal clipboard payload = %q, want the selected answer", payload)
	}
	t.Log("live terminal clipboard payload: SECOND_HISTORY_REPLY")
	r.keys("Escape")
	r.quit()
	cleanChatAuditModels(t, home, model, "history-copy")
}
