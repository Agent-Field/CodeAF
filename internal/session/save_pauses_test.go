package session

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/executor"
)

type stepProbeSeat struct {
	executor.Stance
	mu       sync.Mutex
	deferred []bool
	settled  []bool
}

func (s *stepProbeSeat) Around(ctx context.Context, call executor.Call, run func() ([]byte, bool)) error {
	s.mu.Lock()
	s.deferred = append(s.deferred, call.Deferred)
	s.mu.Unlock()
	return s.Stance.Around(ctx, call, run)
}

func (s *stepProbeSeat) Settle(ctx context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.settled = append(s.settled, executor.Stepping(ctx))
}

func TestSavePausesSessionMarksOnlyToolBatchSettles(t *testing.T) {
	probe := &stepProbeSeat{Stance: executor.Stance{Class: executor.HostBound}}
	completer := &scriptedCompleter{}
	for n := range 5 {
		completer.steps = append(completer.steps, func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse(fmt.Sprintf("read-%d", n), "read", fmt.Sprintf(`{"path":"note%d.txt"}`, n)), nil
		})
	}
	completer.steps = append(completer.steps, func(context.Context, []ai.Message) (*ai.Response, error) {
		return textResponse("Read all five notes."), nil
	})
	agent, workspace := newTestAgent(t, completer, func(cfg *Config) { cfg.Seat = probe })
	for n := range 5 {
		must(t, os.WriteFile(filepath.Join(workspace, fmt.Sprintf("note%d.txt", n)), []byte(fmt.Sprintf("note %d", n)), 0600))
	}
	events, err := agent.Submit(t.Context(), "Read note0.txt, note1.txt, note2.txt, note3.txt and note4.txt.")
	must(t, err)
	collect(t, events)
	probe.mu.Lock()
	defer probe.mu.Unlock()
	if !reflect.DeepEqual(probe.deferred, []bool{true, true, true, true, true}) {
		t.Fatalf("tool batches did not defer their completions: %v", probe.deferred)
	}
	if !reflect.DeepEqual(probe.settled, []bool{true, true, true, true, true, false}) {
		t.Fatalf("step or turn-end settle used the wrong kind: %v", probe.settled)
	}
}
