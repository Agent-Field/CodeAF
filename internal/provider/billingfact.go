package provider

import (
	"context"
	"strings"
	"sync"
)

// WHAT KIND OF MONEY ONE ANSWER WAS, SAID ONLY BY WHOEVER KNOWS.
//
// A spending cap has to tell a call that was paid for by the call from a call a
// fixed plan already covered, and every guess this program could make about
// that is wrong somewhere. A zero cost is a router that did not price the call
// as often as it is a free one; a direct key is a metered vendor as often as it
// is a subscription; a `:free` suffix is a catalog's word, and an OAuth token
// says who signed in, not who pays. So THE FACT IS WRITTEN DOWN ONLY WHERE IT
// IS STATED: on the billing road the person connected through
// (`modelsource.Door.Metered`, proved at connect time) and on the one switch
// this transport makes between roads — the plan overflow the person opted into.
// Everywhere else the answer is [BillingUnknown], and a cap treats an unknown
// call as one it may have to stop.
//
// THE FACT BELONGS TO THE ATTEMPT THAT WAS ANSWERED, NOT TO THE CLIENT. A plan
// client can send its last attempt down the metered overflow road, so the fact
// is stamped per attempt ([Client.send], beside the attempt's address) and
// carried with that answer to whoever banks it. Reading the client's settings
// again at bank time would read whatever the account says NOW, which is the
// mutable-state read a bill must never depend on.

// BillingKind is the closed vocabulary of what one attempt cost the person.
type BillingKind string

const (
	// BillingMetered is a road whose use creates a charge outside any plan.
	BillingMetered BillingKind = "metered"
	// BillingIncluded is a road the bound billing door explicitly states is
	// covered by a fixed plan (`Door.Metered == false` on a named door).
	BillingIncluded BillingKind = "included"
	// BillingUnknown is every road nobody stated a billing fact for. It is the
	// zero value's meaning as well: an absent fact is never read as included.
	BillingUnknown BillingKind = "unknown"
)

// BillingFact is one attempt's billing, with the statement it came from.
//
// Basis names the source of the statement — `door:<source>/<door>` for a bound
// door, `overflow:<door>` for the plan overflow — and is empty exactly when the
// kind is unknown. The zero value is "nobody said".
type BillingFact struct {
	Kind  BillingKind
	Basis string
}

// Known reports whether somebody stated this fact. The zero value and an
// explicit unknown both answer false.
func (f BillingFact) Known() bool {
	return f.Kind == BillingMetered || f.Kind == BillingIncluded
}

// Included reports the one fact that may ever exempt a call from a money cap.
func (f BillingFact) Included() bool { return f.Kind == BillingIncluded }

// normalized answers the fact a reader may rely on: a kind outside the closed
// vocabulary, or a stated kind with no basis, is unknown.
func (f BillingFact) normalized() BillingFact {
	f.Basis = strings.TrimSpace(f.Basis)
	if !f.Known() || f.Basis == "" {
		return BillingFact{Kind: BillingUnknown}
	}
	return f
}

// MeteredDoorFact and IncludedDoorFact are the two statements a bound door can
// make. They are spelled here, once, so the basis a row carries is one string
// whichever package built the client.
func MeteredDoorFact(source, door string) BillingFact {
	return doorFact(BillingMetered, source, door)
}

// IncludedDoorFact is a named door's explicit plan statement.
func IncludedDoorFact(source, door string) BillingFact {
	return doorFact(BillingIncluded, source, door)
}

func doorFact(kind BillingKind, source, door string) BillingFact {
	source, door = strings.TrimSpace(source), strings.TrimSpace(door)
	if door == "" {
		return BillingFact{Kind: BillingUnknown}
	}
	return BillingFact{Kind: kind, Basis: "door:" + source + "/" + door}
}

// overflowFact is the plan overflow road: metered by its own definition, which
// is the whole reason a person had to opt into it.
func overflowFact(door string) BillingFact {
	door = strings.TrimSpace(door)
	if door == "" {
		door = "metered"
	}
	return BillingFact{Kind: BillingMetered, Basis: "overflow:" + door}
}

// attemptBilling is the fact one attempt of this client goes out under.
//
// The overflow road is metered whatever the bound door says. Every other
// attempt carries the client's own statement, normalized so a half-stated fact
// reads as unknown rather than as whatever its kind claimed.
func (c *Client) attemptBilling(overflow bool) BillingFact {
	if overflow {
		return overflowFact(c.config.PlanOverflowDoor)
	}
	return c.config.Billing.normalized()
}

// answeredBilling is the fact the answer in hand was produced under. With the
// call's trace it is exact. Without one it is the client's own statement —
// UNLESS this client may switch to a metered overflow, in which case nobody can
// say which road answered and the honest answer is unknown.
func (c *Client) answeredBilling(traces ...*callTrace) BillingFact {
	if len(traces) > 0 && traces[0] != nil && traces[0].attemptBilled {
		return traces[0].attemptBilling
	}
	if c.config.OverflowOnPlanPause && strings.TrimSpace(c.config.PlanOverflow) != "" {
		return BillingFact{Kind: BillingUnknown}
	}
	return c.config.Billing.normalized()
}

// ── what the caller's served slot saw ───────────────────────────────────────

// billingSeen is the billing half of a [ServedEndpoint]: every fact noted
// since the last [ServedEndpoint.TakeBilling], folded so that the answer can
// only err toward a charge. A slot is shared by a turn's sequential calls and,
// on a hedge, by two arms at once; folding rather than overwriting is what
// keeps a late-landing arm from relabelling the winner's money as a plan's.
type billingSeen struct {
	mu   sync.Mutex
	fact BillingFact
	seen bool
}

func (b *billingSeen) note(fact BillingFact) {
	fact = fact.normalized()
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.seen {
		b.fact, b.seen = fact, true
		return
	}
	b.fact = foldBilling(b.fact, fact)
}

func (b *billingSeen) take() BillingFact {
	b.mu.Lock()
	defer b.mu.Unlock()
	fact := b.fact
	b.fact, b.seen = BillingFact{}, false
	return fact
}

// foldBilling combines two facts about answers banked as one: equal facts are
// that fact, any metered fact makes the pair metered, and anything else —
// included beside unknown — is unknown. It never yields included unless both
// halves said so.
func foldBilling(a, b BillingFact) BillingFact {
	switch {
	case a == b:
		return a
	case a.Kind == BillingMetered:
		return a
	case b.Kind == BillingMetered:
		return b
	default:
		return BillingFact{Kind: BillingUnknown}
	}
}

// TakeBilling answers the billing of every answer this slot saw since the last
// take, and forgets it. The zero fact means no answer through this transport
// was noted here at all — which a bank reads as "nobody said".
func (s *ServedEndpoint) TakeBilling() BillingFact {
	if s == nil {
		return BillingFact{}
	}
	return s.billing.take()
}

// noteAnsweredBilling hands one answer's billing to the caller's slot, if the
// caller opened one.
func noteAnsweredBilling(ctx context.Context, fact BillingFact) {
	if ctx == nil {
		return
	}
	if slot, _ := ctx.Value(servedEndpointContextKey{}).(*ServedEndpoint); slot != nil {
		slot.billing.note(fact)
	}
}
