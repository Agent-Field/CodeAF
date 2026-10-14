package builder

import (
	"context"
	"fmt"
	"sync"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/builder/prompt"
)

// defaultBudget is the monthly cap in USD the New app sheet offers.
const defaultBudget = 20.0

// build is what the Builder remembers about one app between calls: the owner's
// settings from apps.create, the plan being edited and the spend so far. It
// holds no records and no content (rule 01).
type build struct {
	app        string
	model      string
	agentModel string
	budget     float64
	connectors []string

	mu      sync.Mutex
	plan    prompt.Plan
	saved   savedResult
	spent   float64
	cancel  context.CancelFunc
	working bool

	// The loop road's parts: the seat its worker builds in, whether the loop
	// gave this build's plan to the calls road, and the check rounds so far.
	seat     *seat
	demoted  bool
	rounds   int
	hold     bool // a rung is running whose failure must fall to the next rung, not show the card
	tint     string
	tintUsed []string
}

// demote hands the build to the calls road for good (see roadFor).
func (b *build) demote() {
	b.mu.Lock()
	b.demoted = true
	b.mu.Unlock()
}

func (b *build) isDemoted() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.demoted
}

// holdFailure makes a failed check end quietly with errRungFailed instead of
// the failed card, for as long as a rung that has a rung below it is running.
func (b *build) holdFailure(on bool) {
	b.mu.Lock()
	b.hold = on
	b.mu.Unlock()
}

func (b *build) holdingFailure() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.hold
}

// nextRound numbers the next check round, however it was asked for.
func (b *build) nextRound() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.rounds++
	return b.rounds
}

func (b *build) cost() bridge.Cost {
	b.mu.Lock()
	defer b.mu.Unlock()
	return bridge.Cost{Spent: b.spent, Cap: b.budget}
}

// charge adds one call's cost to the spend.
func (b *build) charge(usd float64) {
	b.mu.Lock()
	b.spent += usd
	b.mu.Unlock()
}

// withinBudget refuses the next call once the cap is spent (rule 10).
func (b *build) withinBudget() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.spent >= b.budget {
		return bridge.Errorf(bridge.CodeBudget,
			fmt.Sprintf("the Builder has spent $%.2f of $%.2f", b.spent, b.budget))
	}
	return nil
}

func (b *build) currentPlan() prompt.Plan {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.plan
}

func (b *build) setPlan(p prompt.Plan) {
	b.mu.Lock()
	b.plan = p
	b.mu.Unlock()
}

// begin marks the build busy and hands out a context Cancel can stop.
func (b *build) begin(parent context.Context) (context.Context, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.working {
		return nil, bridge.Errorf(bridge.CodeConflict, "the Builder is already working on this app")
	}
	ctx, cancel := context.WithCancel(parent)
	b.working, b.cancel = true, cancel
	return ctx, nil
}

func (b *build) finish() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.cancel != nil {
		b.cancel()
	}
	b.working, b.cancel = false, nil
}

// stop cancels the running step, reporting whether one was running.
func (b *build) stop() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.cancel == nil {
		return false
	}
	b.cancel()
	return true
}
