//go:build linux

package session

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/executor"
)

// fencedHome is a home folder with a key in it, outside the shared /tmp
// (which a fence on Landlock alone leaves writable) so a write into it is a
// write the fence must refuse.
func fencedHome(t *testing.T) string {
	t.Helper()
	base, err := os.MkdirTemp(".", ".fence-home-")
	if err != nil {
		t.Fatal(err)
	}
	base, _ = filepath.Abs(base)
	t.Cleanup(func() { _ = os.RemoveAll(base) })
	if err := os.MkdirAll(filepath.Join(base, ".ssh"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, ".ssh", "id_ed25519"), []byte("PRIVATE KEY"), 0o600); err != nil {
		t.Fatal(err)
	}
	return base
}

// TestAFencedConversationRefusesOutsideItsJobInOneSentence drives the real
// session and its real tools: a command that writes outside the checkout, a
// command that reads a key, and the write tool aimed outside are each refused
// with the fence's one sentence, and a command inside the checkout runs.
func TestAFencedConversationRefusesOutsideItsJobInOneSentence(t *testing.T) {
	if executor.FenceLimitLine() == executor.FenceDegradedLine {
		t.Skip("this kernel cannot fence")
	}
	home := fencedHome(t)
	outside := filepath.Join(home, "outside")
	calls := []struct{ tool, args string }{
		{"bash", fmt.Sprintf(`{"command":"touch %s"}`, outside)},
		{"bash", fmt.Sprintf(`{"command":"cat %s/.ssh/id_ed25519"}`, home)},
		{"write", fmt.Sprintf(`{"path":%q,"content":"x"}`, outside)},
		{"read", fmt.Sprintf(`{"path":%q}`, filepath.Join(home, ".ssh", "id_ed25519"))},
		{"bash", `{"command":"echo inside > inside.txt && cat inside.txt"}`},
	}
	var steps []step
	for i, c := range calls {
		c := c
		steps = append(steps, func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse(fmt.Sprint("call", i), c.tool, c.args), nil
		})
	}
	steps = append(steps, finalText("done"))
	agent, workspace := newTestAgent(t, &scriptedCompleter{steps: steps}, func(c *Config) {
		fence, err := executor.NewFence(executor.FenceOptions{Root: c.Workspace, Home: home, StateDir: filepath.Join(home, "jail")})
		if err != nil {
			t.Fatal(err)
		}
		c.Fence = fence
	})
	var outputs []string
	notices := 0
	for _, event := range collect(t, mustSubmit(t, agent, "try the fence")) {
		if event.Kind == EventToolEnd || event.Kind == EventToolFailed {
			outputs = append(outputs, event.Output)
		}
		if event.Kind == EventNotice && event.Text == executor.FenceLimitLine() {
			notices++
		}
	}
	if len(outputs) != len(calls) {
		t.Fatalf("got %d tool results for %d calls: %q", len(outputs), len(calls), outputs)
	}
	// A KEY IS REFUSED TWICE OVER: the approval floor reads a call that names
	// a credential before anything runs, and says so in its own words; the
	// fence is what holds when the call does not name it plainly.
	for i, out := range outputs[:4] {
		if !strings.Contains(out, executor.FenceLine) && !strings.Contains(out, "reaches for a credential") {
			t.Errorf("call %d (%s) was not refused: %q", i, calls[i].tool, out)
		}
		if strings.Contains(out, "PRIVATE KEY") {
			t.Errorf("call %d read the key: %q", i, out)
		}
	}
	if strings.Contains(outputs[4], executor.FenceLine) || !strings.Contains(outputs[4], "inside") {
		t.Errorf("a command inside the checkout was refused: %q", outputs[4])
	}
	if _, err := os.Stat(outside); err == nil {
		t.Error("the file outside the checkout was made")
	}
	if _, err := os.Stat(filepath.Join(workspace, "inside.txt")); err != nil {
		t.Errorf("the file inside the checkout is missing: %v", err)
	}
	if line := executor.FenceLimitLine(); line != "" && notices != 1 {
		t.Errorf("the partial-fence line was said %d times, want once", notices)
	}
}

// AN INTERACTIVE CONVERSATION HAS NO FENCE: nil Config.Fence is the host seat,
// exactly as before.
func TestAConversationWithoutAFenceRunsOnTheHost(t *testing.T) {
	var c Config
	if c.fence() != nil {
		t.Fatal("a conversation nobody fenced has a fence")
	}
	if executor.FenceOf(c.seat()) != nil {
		t.Fatal("the host seat is fenced")
	}
	if refusal := fencedFileTool(nil, "/w", "write", json.RawMessage(`{"path":"/etc/passwd"}`)); refusal != "" {
		t.Fatalf("an unfenced write was refused: %q", refusal)
	}
}
