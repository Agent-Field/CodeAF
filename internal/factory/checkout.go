package factory

// ── WHERE A JOB'S REPOSITORY IS CHECKED OUT ─────────────────────────────────
//
// A RUN NEVER STARTS WITHOUT A CHECKOUT, AND THE RUNNER GETS ONE ITSELF: a
// repository this machine has no folder for is cloned into the factory's own
// folder on the run's first step, over the same GitHub token the poll and the
// push use. So a start is refused only when that clone cannot happen at all
// (GitHub is not connected, or the repository is not one the factory can name
// on GitHub), and it is refused BEFORE anything is written, in one sentence
// saying why ([NeedsCheckout]). [Seam.CheckoutOf] is the one door every start
// asks, so the header's Start, the floor's own launch and the manager's
// `factory_start` refuse the same jobs in the same words, and the job's read
// says the same thing in advance (`checkout` on the wire).

// CheckoutState is how a run of a repository gets the folder it works in on
// this machine.
type CheckoutState string

const (
	// CheckoutHere is a repository this machine knows a checkout for.
	CheckoutHere CheckoutState = "here"
	// CheckoutWillClone is one with no checkout yet, which the runner clones
	// on the run's first step.
	CheckoutWillClone CheckoutState = "will_clone"
	// CheckoutMissing is one with no checkout that cannot be cloned; a start
	// of its jobs is refused with [NeedsCheckout].
	CheckoutMissing CheckoutState = "missing"
)

// The two halves a [NeedsCheckout] sentence ends with, after the
// repository's name, spelled once so the refusal, the manual and the tests
// say the same words.
const (
	// CheckoutNeedsGitHub: the repository could be cloned, but there is no
	// GitHub token to clone it with.
	CheckoutNeedsGitHub = "connect GitHub so the factory can clone it."
	// CheckoutNotOnGitHub: the repository is not one the factory can name on
	// GitHub, so there is nothing to clone it from.
	CheckoutNotOnGitHub = "watch it in Repos so the factory can clone it, or open codeaf in its folder."
)

// NeedsCheckout refuses a start whose repository is [CheckoutMissing]. It is
// a type, not a sentence to match, so the factory's service answers it 409
// `needs_checkout` wherever it surfaces, and its words name the repository
// and why it cannot be cloned (Why is one of the halves above).
type NeedsCheckout struct {
	Repo string
	Why  string
}

func (e NeedsCheckout) Error() string {
	return e.Repo + " isn't checked out on this machine · " + e.Why
}
