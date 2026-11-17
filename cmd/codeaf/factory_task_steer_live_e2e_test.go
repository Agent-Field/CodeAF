//go:build e2e

package main

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
	"github.com/Agent-Field/codeaf/internal/session"
)

// steerDoor keeps what the run writes: the documents it makes and the task's
// last state.
type steerDoor struct {
	agentTestDoor
	mu    sync.Mutex
	docs  []string
	state string
}

func (d *steerDoor) Work(ctx context.Context, tool string, args json.RawMessage) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	switch tool {
	case session.WSFileCreateTool:
		d.docs = append(d.docs, string(args))
		return `{"file":{"id":"fl_1"},"said":"made the document"}`, nil
	case session.WSTaskUpdateTool:
		var a struct{ State string }
		_ = json.Unmarshal(args, &a)
		if a.State != "" {
			d.state = a.State
		}
		return `{"task":{"id":"tk_1","rev":2}}`, nil
	case session.WSTasksTool:
		return `{"task":{"id":"tk_1","rev":1,"title":"Launch blurb","state":"running","owner":{"kind":"person","id":"marco"}}}`, nil
	}
	return `{"ok":true}`, nil
}

// A STEER REACHES A RUNNING TASK ON THE REAL MODEL (L4 acceptance 2): Marco
// types "use the shorter title" while Afi works, and what Afi makes follows
// it. Opt in with CODEAF_LIVE_FACTORY=1; about a cent.
func TestLiveASteerReachesARunningTask(t *testing.T) {
	if os.Getenv(liveFactoryOptIn) != "1" {
		t.Skipf("opt-in live lane: set %s=1", liveFactoryOptIn)
	}
	if liveFactoryKey == "" {
		t.Fatal("BLOCKED: no OpenRouter key by any road the product reads; not faking a pass")
	}
	run := factoryWorkstreamAgent(nil, func() (config.Config, error) { return config.Config{Model: liveFactoryModel}, nil })
	door := &steerDoor{}
	steers := make(chan string, 1)
	var once sync.Once
	var mu sync.Mutex
	var lines []string
	ctx, cancel := context.WithTimeout(context.Background(), liveFactoryWall)
	defer cancel()
	end, err := run(ctx, serve.AgentTurn{Workstream: "ws_live", Name: "Pantry 2.4", Turn: "turn_steer", Asker: "marco", Now: time.Now(),
		Text: "Launch blurb\n\nWrite a one-paragraph launch blurb for the shopping list in Pantry 2.4 as a document in Files, titled \"Pantry 2.4 brings the shopping list you always wanted to every kitchen\". Read the workstream's files first.",
		Task: &serve.AgentTask{ID: "tk_1", Title: "Launch blurb"}, Key: liveFactoryKey, Dir: t.TempDir(), Door: door, Steers: steers,
		Step: func(kind, text, tool string, failed bool) {
			mu.Lock()
			lines = append(lines, kind+": "+text)
			mu.Unlock()
			if kind == "step" {
				once.Do(func() { steers <- "use the shorter title \"Shopping lists are here\"" })
			}
		}})
	if err != nil {
		t.Fatalf("the run failed: %v", err)
	}
	t.Logf("$%.4f; lines: %q; documents: %q; state %q", end.USD, lines, door.docs, door.state)
	if len(door.docs) == 0 || !strings.Contains(strings.Join(door.docs, "\n"), "Shopping lists are here") {
		t.Fatalf("the document did not follow the steer: %q", door.docs)
	}
}
