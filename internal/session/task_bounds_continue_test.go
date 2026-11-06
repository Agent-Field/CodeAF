package session

import (
	"context"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/plandb"
)

// The double models the engine's exhausted-ceiling door; the real supervisor's
// no-launch behavior is pinned by TestARunWhoseLimitIsAlreadySpentSeatsNoWorker.
type continuedTreeLimitEngine struct{ *beltRunDouble }

func (engine continuedTreeLimitEngine) Start(ctx context.Context, spec RunSpec) RunSummary {
	if spec.CostUSD > 0 && spec.CostUSD <= 1e-9 {
		spec.CompleterFor = nil
	}
	return engine.beltRunDouble.Start(ctx, spec)
}

// L-LOOP C4a, C4b, C4d: continuing adopts the recorded spend rather than
// replenishing the tree, keeps the configured-cap words, and charges only new work.
func TestContinuedRunKeepsItsSpentTreeAllowanceAndConfiguredLimitWords(t *testing.T) {
	for _, tc := range []struct {
		name              string
		spent, limit      float64
		want, newSpend    float64
		calls             int
		conversationBinds bool
	}{
		{name: "nearly spent", spent: 4.875, want: .125, newSpend: .125, calls: 1},
		{name: "task remainder binds", spent: 4.875, limit: 6, want: .125, newSpend: .125, calls: 1},
		{name: "conversation remainder binds", spent: 4.875, limit: 5.1875, want: .0625, newSpend: .0625, calls: 1, conversationBinds: true},
		{name: "spent", spent: 5, want: math.SmallestNonzeroFloat64},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("CODEAF_TASK_BELT", "bash")
			double := newBeltRunDouble("unfinished work")
			double.summary = RunSummary{Outcome: runLimitSentence, Limit: RunLimitCost, USD: tc.newSpend}
			double.leaveOpen = true
			registerBeltRunEngine(t, continuedTreeLimitEngine{double})
			completer := &scriptedCompleter{steps: []step{finalText("unfinished work")}}
			agent, _ := newTestAgent(t, completer, func(c *Config) {
				c.Workspace = newTestRepo(t)
				c.Place = Place{Dir: t.TempDir()}
				c.ProfileDir = t.TempDir()
				c.AskConsent = false
				c.Interactive = true
				c.Budget = Budget{USD: tc.limit}
			})
			if err := config.SetCrewTaskCap(agent.config.ProfileDir, "5"); err != nil {
				t.Fatal(err)
			}
			g := agent.graph()
			tree, err := prepareTaskTree(agent.config.Place, agent.config.Workspace, "3333dddd3333dddd", 7, "finish the work")
			if err != nil {
				t.Fatal(err)
			}
			keepInterruptedRun(t, agent, g, 7, "finish the work", runCopyOf(tree))
			store, err := plandb.Open(g.planPath(), "continued-run", "7", "finish the work", "the unfinished work", g.planChat())
			if err != nil {
				t.Fatal(err)
			}
			if err := store.AddSpend("7", "test/model", plandb.RoleWork, tc.spent, 1, 1); err != nil {
				_ = store.Close()
				t.Fatal(err)
			}
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			priorUsage := tc.spent + .25
			agent.usage.CostUSD = priorUsage
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if _, err := agent.ContinueRun(ctx, 7); err != nil {
				t.Fatal(err)
			}
			func() {
				defer endBeltRun(t, agent, double)
				select {
				case <-double.entered:
				case <-ctx.Done():
					t.Fatal("continuation never reached the engine")
				}
				double.mu.Lock()
				got := double.spec.CostUSD
				double.mu.Unlock()
				if got != tc.want {
					t.Errorf("continued allowance = %v, want %v", got, tc.want)
				}
			}()
			completer.mu.Lock()
			calls := len(completer.seen)
			completer.mu.Unlock()
			if calls != tc.calls {
				t.Errorf("continued model calls = %d, want %d", calls, tc.calls)
			}
			if got := agent.Usage().CostUSD; got != priorUsage+tc.newSpend {
				t.Errorf("conversation charged persisted spend again: %v, want %v", got, priorUsage+tc.newSpend)
			}
			row, found := runRowOf(g, 7)
			wantWords := config.CrewTaskCapAction(5)
			if !found || row.Ending != TaskEndingCostLimit || strings.Contains(row.Report, wantWords) == tc.conversationBinds {
				t.Fatalf("continued ending lost its binding limit: %+v", row)
			}
			store = beltRunStoreAt(t, agent.config.Place.Dir)
			defer store.Close()
			notes := store.Notes("7", 0)
			foundWords := false
			for _, note := range notes {
				foundWords = foundWords || strings.Contains(note.Body, wantWords)
			}
			if foundWords == tc.conversationBinds {
				t.Fatalf("continued outcome note lost its binding limit: %+v", notes)
			}
		})
	}
}
