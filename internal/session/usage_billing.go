package session

import (
	"crypto/rand"
	"encoding/hex"
	"sync"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/provider"
)

// ── THE FACTS A LEDGER ROW CARRIES ABOUT ITS OWN MONEY ──────────────────────
//
// A row has always said how much; a spending cap also needs to know WHICH ROW
// (so a process's own notice of a call and the same call read back off disk are
// one call and not two), WHAT KIND of money it was (so a plan's included calls
// are not stopped by a dollar cap, and nothing else is let through on a guess),
// and WHERE THE FIGURE CAME FROM (so a provider's charge and a figure somebody
// multiplied are never mistaken for each other). Each is written once, at the
// moment it is known, by whoever knows it — and absent everywhere else.

// The words [UsageLine.Price] may carry. They are a closed vocabulary spelled
// once, here, so a reader can switch on them.
const (
	usagePriceProvider = "provider"
	usagePriceReceipt  = "receipt"
	// UsagePriceList is a figure computed from counted tokens and the model's
	// published price — a record at the provider's price, never a forecast. It
	// is exported for the command-line writers that compute one.
	UsagePriceList = "list"
	// UsagePriceProvider is the provider's own charge, for the command-line
	// writers that bank a provider's usage block themselves.
	UsagePriceProvider = usagePriceProvider
)

// usagePrice states where a row's money came from, and states nothing for a
// row with no money on it: a provenance for a figure that is not there would
// be a word about nothing.
func usagePrice(usd float64, price string) string {
	if usd <= 0 {
		return ""
	}
	return price
}

// withUsageBilling writes a transport's billing statement onto a row, and
// writes nothing for a fact nobody stated — so the row stays exactly as wide as
// it was, and "absent" keeps meaning "nobody said".
func withUsageBilling(line UsageLine, fact provider.BillingFact) UsageLine {
	if !fact.Known() {
		return line
	}
	line.Billing, line.BillingBasis = string(fact.Kind), fact.Basis
	return line
}

// newUsageRowID is a fresh row name: twelve random bytes in hex. A collision
// would merge two calls in a dedupe, so it is long enough that one never
// happens on any ledger a person could accumulate.
func newUsageRowID() string {
	var raw [12]byte
	_, _ = rand.Read(raw[:])
	return hex.EncodeToString(raw[:])
}

// ── an errand's road, from its ladder to its bank ───────────────────────────
//
// An errand ([Agent.callRole]) gets its answer on a slot of its own and hands
// the ANSWER back to a caller, who banks it later through [Agent.addUsageAs].
// The road the answer came down is known only at the ladder; the bank is a
// dozen call sites away. So the ladder notes the road against the answer it is
// returning, and the bank takes it back by the same answer.
//
// IT IS BOUNDED, AND WHAT IT DROPS IS ONLY A STATEMENT, NEVER MONEY. An answer
// its caller decided not to bank stays noted until newer notes push it out; an
// answer whose note was pushed out is banked exactly as before, with no billing
// on its row — which reads as "nobody said", the conservative reading, never as
// a plan's included call.

// answerBillingNotes is how many errand answers may be waiting at once. An
// errand is banked in the breath after it returns, so a handful is the
// ordinary depth; the bound only keeps a caller that never banks from holding
// answers alive.
const answerBillingNotes = 64

var answerBillings = struct {
	mu    sync.Mutex
	notes map[*ai.Response]provider.BillingFact
	ring  [answerBillingNotes]*ai.Response
	next  int
}{notes: map[*ai.Response]provider.BillingFact{}}

// noteAnswerBilling remembers which road one returned answer came down.
func noteAnswerBilling(response *ai.Response, fact provider.BillingFact) {
	if response == nil {
		return
	}
	answerBillings.mu.Lock()
	defer answerBillings.mu.Unlock()
	if _, held := answerBillings.notes[response]; held {
		answerBillings.notes[response] = fact
		return
	}
	if old := answerBillings.ring[answerBillings.next]; old != nil {
		delete(answerBillings.notes, old)
	}
	answerBillings.ring[answerBillings.next] = response
	answerBillings.next = (answerBillings.next + 1) % answerBillingNotes
	answerBillings.notes[response] = fact
}

// takeAnswerBilling hands back, once, the road one answer was noted with. The
// second answer says the answer was noted at all — that it came off the
// transport through an errand's ladder rather than being assembled here.
func takeAnswerBilling(response *ai.Response) (provider.BillingFact, bool) {
	if response == nil {
		return provider.BillingFact{}, false
	}
	answerBillings.mu.Lock()
	defer answerBillings.mu.Unlock()
	fact, held := answerBillings.notes[response]
	if !held {
		return provider.BillingFact{}, false
	}
	delete(answerBillings.notes, response)
	for i, waiting := range answerBillings.ring {
		if waiting == response {
			answerBillings.ring[i] = nil
			break
		}
	}
	return fact, true
}
