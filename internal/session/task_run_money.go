package session

// A run's money, on its way into the conversation's books.
//
// A run the conversation handed a task to spends beside the conversation, and
// the conversation's own books hold what its work cost, so the run's money is
// folded in as it is spent — through the fold door, which writes no row on the
// machine's ledger, because the run's own workers wrote one per call.
//
// TWO READINGS OF ONE ACCOUNT, FOLDED ONCE. A program's model API meters call
// by call and hands each call over whole ([RunCharge]); the run's supervisor
// hands over its reconciled running total, which is the only thing a bash
// worker reports. The fold keeps one figure — the dollars already in the books
// — and folds each call whole as it comes, then only the part of the total that
// is beyond that figure. A call is always told before the total that holds it
// (internal/run's delegateMeter.bank), so no dollar is folded by both.

import (
	"strings"
	"sync"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// RunCharge is one priced call a run's worker made, as the run metered it: the
// model that answered, the tokens and the cached share, and what the provider
// charged — zero where the service reports no price, which is a call nobody
// could price and never a free one.
type RunCharge struct {
	Model     string
	TokensIn  int
	TokensOut int
	Cached    int
	USD       float64
}

// foldDust is the smallest remainder of a run's total the fold will write as
// a line of its own. A running total reconciled from cumulative readings and
// the same calls summed one by one differ by float rounding — around 1e-17 —
// and a journal line for that is not money.
const foldDust = 1e-9

// beltFold is the conversation's side of one run's money. Its two doors are
// called from different goroutines — a call's charge from the model API, the
// total from the run's supervisor and then from the belt when the run
// returns — so the one figure they share is held under a lock.
type beltFold struct {
	agent  *Agent
	mu     sync.Mutex
	folded float64
}

// charge folds one call whole: its tokens, its cached share, its model and
// its dollars, as ONE call, detached from whatever chat turn is running.
//
// It used to be a bare dollar figure per reading of the run's total, with no
// tokens, no model and no call, so a conversation whose program made 276 calls
// held none of them in its token and call totals; and a service that reports
// no price folded nothing at all. And it moved the running chat turn's share,
// so a turn abandoned while a run was spending was journaled with the run's
// dollars as its own.
func (f *beltFold) charge(charge RunCharge) {
	if charge.TokensIn == 0 && charge.TokensOut == 0 && charge.USD == 0 {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	cost := charge.USD
	f.agent.addDetachedFoldedUsage(&ai.Response{Usage: &ai.Usage{
		PromptTokens:         charge.TokensIn,
		CompletionTokens:     charge.TokensOut,
		CacheReadInputTokens: charge.Cached,
		Cost:                 &cost,
	}}, strings.TrimSpace(charge.Model), 1)
	f.folded += charge.USD
}

// total folds whatever the run's reconciled running total holds beyond what
// is already in the books: the whole of a bash worker's spend, and nothing
// for a program whose every call arrived through [beltFold.charge] first.
func (f *beltFold) total(total float64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delta := total - f.folded
	if delta <= foldDust {
		return
	}
	f.agent.addDetachedFoldedUsage(&ai.Response{Usage: &ai.Usage{Cost: &delta}}, "", 0)
	f.folded = total
}

// runConversation is the conversation a run's ledger rows are filed under:
// the conversation at the root of this agent's family, which is this agent's
// own journal when it is the conversation — the same answer a task node's
// rows carry as their Root.
func (a *Agent) runConversation() string {
	if root := strings.TrimSpace(a.config.rootSession); root != "" {
		return root
	}
	return strings.TrimSpace(a.journalID())
}
