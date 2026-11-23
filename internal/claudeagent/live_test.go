package claudeagent

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestLiveSessionTakesASecondMessageAndAsksPermission drives the real Claude
// Code on this machine. It costs a few cents of the person's plan, so it runs
// only when CODEAF_CLAUDE_LIVE is set.
func TestLiveSessionTakesASecondMessageAndAsksPermission(t *testing.T) {
	if os.Getenv("CODEAF_CLAUDE_LIVE") == "" {
		t.Skip("set CODEAF_CLAUDE_LIVE=1 to drive the real Claude Code")
	}
	binary, err := exec.LookPath("claude")
	if err != nil {
		t.Skip("no claude on PATH")
	}
	dir := t.TempDir()
	var mu sync.Mutex
	var asked []string
	results := make(chan Event, 4)
	s, err := Start(context.Background(), Options{
		Binary: binary, Dir: dir, Model: "haiku", Stderr: os.Stderr, PermissionMode: "default",
		CanUseTool: func(_ context.Context, req PermissionRequest) Decision {
			mu.Lock()
			asked = append(asked, req.ToolName)
			mu.Unlock()
			if req.ToolName == "Bash" && strings.Contains(string(req.Input), "rm ") {
				return Decision{Message: "removing files is not allowed in this run"}
			}
			return Decision{Allow: true}
		},
		OnEvent: func(ev Event) {
			if ev.Type == "result" {
				results <- ev
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close(5 * time.Second)
	if err := s.Send("Write the word apple into a file named fruit.txt using the Write tool. Reply only 'ok'."); err != nil {
		t.Fatal(err)
	}
	first := wait(t, results)
	if err := s.Send("What word is in fruit.txt? Reply with only the word."); err != nil {
		t.Fatal(err)
	}
	second := wait(t, results)
	got, _ := os.ReadFile(dir + "/fruit.txt")
	t.Logf("first=%q second=%q asked=%v session=%s", Text(first), Text(second), asked, s.SessionID())
	if !strings.Contains(string(got), "apple") || !strings.Contains(strings.ToLower(Text(second)), "apple") {
		t.Fatalf("file %q, second answer %q", got, Text(second))
	}
	if len(asked) == 0 {
		t.Fatal("Claude Code never asked codeaf about a tool")
	}
	if s.SessionID() == "" {
		t.Fatal("no session id to resume by")
	}
}

func wait(t *testing.T, results <-chan Event) Event {
	select {
	case ev := <-results:
		return ev
	case <-time.After(3 * time.Minute):
		t.Fatal("no result within three minutes")
		return Event{}
	}
}
