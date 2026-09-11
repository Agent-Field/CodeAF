package session

import (
	"context"
	"sync"
	"testing"

	lanes "github.com/Agent-Field/aforge-v2/internal/lane"
	"github.com/Agent-Field/aforge-v2/internal/provider"
)

// ── A MODEL PICKED AFTER LAUNCH IS STILL LEARNED ABOUT ──────────────────────
//
// The beat's model list is settled when a session opens, from the two config
// slots, and the picker calls [Agent.SetModel]. Until [Agent.noteLaneModel] a
// model a person chose deliberately never got a sheet again for the life of the
// session, so cold start was the steady state for exactly the models people
// care most about.

// wantSheet is a sheet that records what was queued for its beat. It answers
// [lanes.Wanter] as the live sheet does, which is the seam SetModel knocks on.
type wantSheet struct {
	mu     sync.Mutex
	queue  chan string
	wanted []string
}

func newWantSheet() *wantSheet {
	return &wantSheet{queue: make(chan string, 8)}
}

func (s *wantSheet) Rows(string) []lanes.Row { return nil }

func (s *wantSheet) Refresh(context.Context, string) error { return nil }

func (s *wantSheet) Wants(model string) {
	s.mu.Lock()
	s.wanted = append(s.wanted, model)
	s.mu.Unlock()
	select {
	case s.queue <- model:
	default:
	}
}

func (s *wantSheet) Wanted() <-chan string { return s.queue }

// TestSwappingTheModelTellsTheBeat is the behaviour under the structural law of
// the same name in internal/provider.
func TestSwappingTheModelTellsTheBeat(t *testing.T) {
	sheet := newWantSheet()
	lanes.Default().SetSheet(sheet)
	t.Cleanup(func() { lanes.Default().Reset() })

	agent, err := newAgent(Config{
		Workspace: t.TempDir(),
		Model:     "talk/model",
		BaseURL:   "https://openrouter.ai/api/v1",
	}, &scriptedCompleter{})
	if err != nil {
		t.Fatalf("open the session: %v", err)
	}
	t.Cleanup(func() { _ = agent.Close() })

	agent.SetModel("somebody/picked-in-the-picker")
	// SetModel queues synchronously. Observe the receipt, not the queue the
	// real beat is consuming; two readers can make a successful delivery fail.
	sheet.mu.Lock()
	defer sheet.mu.Unlock()
	if len(sheet.wanted) != 1 || sheet.wanted[0] != "somebody/picked-in-the-picker" {
		t.Fatalf("the beat was told about %q, want the picked model exactly once", sheet.wanted)
	}
}

// TestASessionWithNoBeatTellsNobody is the other half: the three sessions that
// deliberately run no beat have nobody to tell, and a fetch started for one of
// them would be work the person asked not to have done.
func TestASessionWithNoBeatTellsNobody(t *testing.T) {
	sheet := newWantSheet()
	lanes.Default().SetSheet(sheet)
	t.Cleanup(func() { lanes.Default().Reset() })

	agent, err := newAgent(Config{
		Workspace: t.TempDir(),
		Model:     "talk/model",
		BaseURL:   "https://openrouter.ai/api/v1",
		Routing:   provider.RoutingOff,
	}, &scriptedCompleter{})
	if err != nil {
		t.Fatalf("open the session: %v", err)
	}
	t.Cleanup(func() { _ = agent.Close() })

	agent.SetModel("somebody/picked-in-the-picker")
	sheet.mu.Lock()
	defer sheet.mu.Unlock()
	if len(sheet.wanted) != 0 {
		t.Fatalf("routing off still queued a fetch for %q", sheet.wanted)
	}
}
