package lane

import (
	"time"

	"github.com/Agent-Field/aforge-v2/internal/lane/control"
)

// ── THE PURSE: WHAT KEEPS A RESCUE FROM BECOMING A SECOND BILL ──────────────
//
// A hedge is the one mechanism in this package that can cost real money by
// working exactly as designed. Every other part of it is arithmetic over numbers
// somebody else already paid for; this one sends a second request. So there is a
// rail in front of it, and what the rail is made of is the whole argument.
//
// ── IT USED TO BE A WINDOW, AND THE WINDOW WAS THE DEFECT ───────────────────
//
// Until 2026-09-11 it was a rolling process-wide allowance: at most two hedges
// in any twenty requests, and at most a tenth of the last hour's bill. Both
// numbers were literals, and both answered the wrong question. A count per
// window cannot tell the one request that needs a rescue from the nineteen that
// do not, so it refuses by arrival order — and the measured case is what that
// costs. On the owner's task that day the controller reached its ceiling and
// asked for a second machine; the allowance said no; one endpoint then wrote 604
// tokens in 86 seconds with a person watching an empty line, and the row carried
// `action report reason ceiling refused budget`. The rescue it refused would have
// cost about two cents against a wait worth a hundred times that.
//
// ── WHAT IT IS INSTEAD: THIS CALL'S OWN BUDGET ──────────────────────────────
//
// What a rescue costs is a fact about ONE CALL — the prompt at this model's
// price plus whatever the second arm writes, which the frontier has already
// priced on every candidate machine ([control.Alternative.Extra]) — and what it
// may cost is [control.Plan.SpendUSD], which is that call's patience converted
// through λ ([PlanFor]). Both halves belong to the plan, so the purse is a
// reading of the plan and holds no state, no clock and no history.
//
// HOW MANY ARMS ONE QUESTION MAY RUN AT ONCE IS A DIFFERENT QUESTION AND IT IS
// ANSWERED ELSEWHERE (internal/provider's `maxArms`, four: the original and
// three rescues). A purse that counted them as well would be two mechanisms for
// one shape, which is exactly how the window came to overrule the controller.

// Spending is one call's own budget as the rail the controller asks before it
// acts.
//
// It is a small adapter and not a method on [control.Plan] because the direction
// of the dependency matters: the controller may not know what this package is,
// and the plan may not grow a method whose answer depends on prices. A zero
// allowance is UNBOUNDED and not empty — see [control.Plan.SpendUSD] — because a
// plan somebody built by hand priced nothing, and reading "nobody said" as
// "nothing may be spent" is how the window's refusal would come back by the
// other door.
func Spending(plan control.Plan) control.Purse { return purse{allowance: plan.SpendUSD} }

// NoSpending is the purse of a call that may spend NOTHING on rescuing itself.
//
// It is a person's own switch and not an arithmetic answer: `SetLaneGuard(false)`
// is somebody saying "do not spend extra to keep an answer moving", and a call
// carrying this purse has no rescue to be refused rather than one it could not
// afford. It is spelled as its own constructor because zero dollars already
// means UNBOUNDED on a plan nobody priced (see [control.Plan.SpendUSD]), and one
// figure cannot honestly mean both.
func NoSpending() control.Purse { return purse{refuses: true} }

// purse is that reading. It is a value rather than a pointer because there is
// nothing in it to mutate: the question "can this call pay for one more arm" has
// the same answer however many times it is asked, which is what lets the
// controller ask it while deciding and the race ask it again at the moment the
// arm really goes out.
type purse struct {
	allowance float64
	refuses   bool
}

// Allows reports whether this call can pay for one arm costing usd.
//
// IT ASKS AND IT DOES NOT SPEND, which is the one property the two callers both
// depend on. The controller asks while it is still deciding — a reading, and one
// it may take several times over one silence — and the surface asks again before
// it draws a countdown over a rescue ([hedgeRace.affordableAlt]); a rail that
// counted either of those would charge a question for arms it never started.
// What is really spent is bounded by the allowance above and by how many arms a
// question may have at once, and neither of those is a tally kept here.
//
// The moment is unused and is kept in the signature because [control.Purse] is
// asked by a package that holds no clock: every question in it takes the moment
// as an argument, and a rail that dropped it would be the one seam in that
// design that could not be given a scripted one.
func (p purse) Allows(usd float64, _ time.Time) bool {
	if p.refuses {
		return false
	}
	if p.allowance <= 0 {
		return true
	}
	return usd <= p.allowance
}
