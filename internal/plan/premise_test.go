package plan

import (
	"strings"
	"testing"
)

// W6's first law: the premise is one string and there is exactly one of it.
//
// It reaches every pass that can add work, so a second copy is not a duplicate
// but a fork — one prompt keeps telling the planner width is free after the
// other has stopped. The grep is over the prompts rather than the source
// because the source is where a constant lives and the prompts are where it
// does damage.
func TestTheAgentPremiseIsStatedOnceAndReachesEveryPassThatCanAddWork(t *testing.T) {
	for name, prompt := range map[string]string{
		"spine":  spinePrompt,
		"fanout": fanoutPrompt,
		"audit":  auditPrompt,
		"revise": revisePrompt,
	} {
		if !strings.Contains(prompt, agentPremise) {
			t.Errorf("%s no longer carries the shared agent premise", name)
		}
		if got := strings.Count(prompt, agentPremise); got != 1 {
			t.Errorf("%s carries the premise %d times, want once", name, got)
		}
	}
}

// W6's actual subject. The premise used to open by declaring the workers
// "instant, free, and unlimited in number", which is the one sentence in the
// whole design that tells a planner a larger plan is never worse than a smaller
// one. Two of those three claims are false — the bill for a plan is the context
// each branch re-pays — and this test is here so that the denial cannot come
// back by way of a rewrite that meant well.
func TestThePremiseNoLongerDeniesWhatAWorkerCosts(t *testing.T) {
	for _, denial := range []string{"instant, free", "unlimited in number", "free and unlimited"} {
		if strings.Contains(agentPremise, denial) {
			t.Errorf("the premise still denies the cost of a worker: %q", denial)
		}
	}
	// What replaces it has to be the trade, not silence: the price is context,
	// the payback is the longest chain rather than the total.
	for _, want := range []string{"context", "longest chain", "never the total"} {
		if !strings.Contains(agentPremise, want) {
			t.Errorf("the premise does not state the trade it replaced the denial with: %q missing", want)
		}
	}
	// And the human-overhead denial is orthogonal to cost and load-bearing on
	// its own; W6 corrects the price, it does not hand roles and sign-off back.
	if !strings.Contains(agentPremise, "human overhead, not structure") {
		t.Error("the premise lost the human-overhead denial, which W6 does not touch")
	}
}

// Width follows the grain of the material. The incident this guards is a stage
// whose units were already enumerated in the material and which was halved into
// two parts that were each handed the whole enumeration: both did everything,
// one answer was paid for twice, and the person waited longer than for a single
// agent. The prompt must carry the principle as a reading of the inputs, and it
// must carry the counterweight in the same breath, or it becomes a licence to
// shred work that was never divisible.
func TestTheFanOutPromptCarriesTheEnumerationGrainPrinciple(t *testing.T) {
	for _, want := range []string{
		"Let the material set the width",
		"that enumeration is the split",
		"never write two parts that would each cover the same set",
		"are not made\nindependent by being separated",
	} {
		if !strings.Contains(fanoutPrompt, want) {
			t.Errorf("the fan-out prompt is missing the grain principle: %q", want)
		}
	}
	// The wall-time argument belongs beside it, because width that is only
	// cheap is not yet a reason to buy it.
	if !strings.Contains(fanoutPrompt, "longest chain") {
		t.Error("the fan-out prompt states the grain without the waiting it buys back")
	}
}

// The sizing pass names the pieces an oversized node would break into, so it is
// the second place the grain has to be read rather than invented, and the place
// that already weighed two costs against each other — both of them latency.
func TestTheSizingPromptReadsTheGrainAndPricesContext(t *testing.T) {
	prompt := sizePromptWith(Anchors())
	for _, want := range []string{
		"already\n  enumerate units that stand apart",
		"never name two pieces that would\n  each cover the whole set",
		"whatever context each new piece must be given",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("the sizing prompt is missing %q", want)
		}
	}
}

// No authored examples, no domains. Every prompt in this package is written to
// be true of work nobody has thought of yet, and evidence about what things
// actually cost arrives from the measured lines rather than from a story a
// prompt author made up. This is the house law stated as a test, scoped to the
// strings W6 wrote.
func TestTheW6PromptTextNamesNoDomain(t *testing.T) {
	prompts := map[string]string{"premise": agentPremise, "fanout-grain": fanoutPrompt}
	for name, prompt := range prompts {
		lower := strings.ToLower(prompt)
		for _, forbidden := range []string{"for example", "e.g.", "such as", "startup", "ceo", "company"} {
			if strings.Contains(lower, forbidden) {
				t.Errorf("%s reaches for an authored example or a domain: %q", name, forbidden)
			}
		}
	}
}
