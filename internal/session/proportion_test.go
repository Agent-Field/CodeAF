package session

// PROPORTION, AS TESTS: the three places this build teaches that checking is
// paid for, so its depth follows what the answer changes.
//
// The finding behind them is a measured one. Handed work whose answer turned out
// to be "this already holds", the model spent two minutes and forty-eight
// seconds and twenty-two tool calls re-deriving every requirement of something
// that was already true, and then said so — the right conclusion, reached
// through a ceremony nobody could have billed for. The teaching is not a rule
// with a number in it: nothing here counts calls, weighs a diff or thresholds
// anything, because a threshold is a rule the model games rather than a
// principle it reasons from.
//
// These are WORDING pins, in the shape task_divide_test.go pins the road's own
// sentences: the three prompts are the whole of the mechanism, so a sentence
// deleted from one of them is the mechanism deleted, and nothing else in the
// suite would notice.

import (
	"strings"
	"testing"
)

// THE WORKING-STYLE LAW, where the chat model is told how to check. It sits in
// the Verify section because that is where the question "how much proof" is
// already being answered, and a law about depth written anywhere else would be
// read after the depth had been chosen.
func TestTheWorkingStylePromptTeachesProportionateChecking(t *testing.T) {
	for _, want := range []string{
		"DEPTH OF CHECKING FOLLOWS THE SIZE OF WHAT YOUR ANSWER CHANGES",
		"An answer that changes nothing is proved by the one check that would have caught you being wrong",
		"work that rewrote something load-bearing earns the whole ladder",
		"Checking is bought with the person's time and money",
	} {
		if !strings.Contains(systemPrompt, want) {
			t.Errorf("prompts/system.md does not say %q", want)
		}
	}
	// AND IT IS TAUGHT IN THE SECTION THAT VERIFIES. The prompt is read top to
	// bottom by a model deciding what to do next; the law belongs beside the
	// deliverable-proof ladder it qualifies.
	verify := section(systemPrompt, "## 5. Verify", "## 6.")
	if !strings.Contains(verify, "DEPTH OF CHECKING FOLLOWS") {
		t.Error("the proportion law is not in the section where checking is decided")
	}
}

// THE OTHER HALF OF WHAT CHECKING COSTS: not how many calls, but how many turns
// they are spread over. A round trip is a wait the person sits through, and work
// issued one call at a time serializes what the harness would have run
// concurrently — so a model that reads three files in three turns has bought the
// same evidence for three times the wall clock. The law lives in Tool Policy's
// General section, beside the bounded-work rule it strengthens, and it is pinned
// here for the same reason the proportion sentences are: the prompt is the whole
// of the mechanism, and nothing else in the suite would notice it going.
//
// It carries no number, deliberately, and names reads/searches/checks rather
// than files — a literature review batches its lookups on exactly this law.
func TestTheSystemPromptTeachesAskingInOneBreath(t *testing.T) {
	const law = "ASK FOR EVERYTHING YOU NEED IN ONE BREATH"
	if !strings.Contains(systemPrompt, law) {
		t.Fatalf("prompts/system.md does not say %q, so nothing tells the model to batch independent calls", law)
	}
	taught := section(systemPrompt, "- "+law, "\n\n")
	if strings.ContainsAny(taught, "0123456789") {
		t.Errorf("the batching law carries a number, which reads as a budget rather than a habit:\n%s", taught)
	}
	if !strings.Contains(taught, "ONE batch of calls") {
		t.Errorf("the batching law never says what to do instead of one call per turn:\n%s", taught)
	}
}

// THE WORKER'S REPORT SIDE. A task's report is often all the person ever reads,
// and the answer "nothing needed doing" is exactly the one that tempts a worker
// into shipping the tour instead of the finding.
func TestTheTaskPromptTeachesTheNothingToDoReportLeadsWithOneCheck(t *testing.T) {
	for _, want := range []string{
		"WHEN THE ANSWER IS THAT NOTHING NEEDED DOING, LEAD WITH THE ONE CHECK THAT WOULD",
		"what you went looking for that would have made the work\nnecessary",
		"The depth\nof your checking follows the size of what your answer changes",
	} {
		if !strings.Contains(taskPrompt, want) {
			t.Errorf("prompts/task.md does not say %q", want)
		}
	}
}

// THE CHECKER'S OWN SIDE, and the one that spends the most: an auditor gathers
// its own evidence from scratch, so re-deriving a claim that changed nothing is
// a second full investigation bought to rule out nothing.
func TestTheAuditPromptTeachesTheSameProportion(t *testing.T) {
	for _, want := range []string{
		"How deep you look follows the size of what the work CHANGED",
		"take the whole ladder",
		"go hunting for the one thing that WOULD have needed doing",
	} {
		if !strings.Contains(auditPrompt, want) {
			t.Errorf("the auditor's prompt does not say %q", want)
		}
	}
	// THE HARD LAWS ARE UNTOUCHED. Proportion is about how much evidence to buy;
	// it is not a licence to judge something other than the acceptance, to trust
	// the work's own word, or to reach for a hand this belt does not have.
	for _, law := range []string{
		"You are READ-ONLY.",
		"Judge the work against its ACCEPTANCE and nothing else",
		"A claim you did not check is a claim you have not verified.",
		"When in doubt, REFUTE.",
	} {
		if !strings.Contains(auditPrompt, law) {
			t.Errorf("the auditor's prompt lost the law %q", law)
		}
	}
}

// NO THRESHOLD REACHED ANY OF THE THREE. The whole reason this is teaching and
// not mechanics is that a number in a prompt is a number the model optimises
// against — "two checks are enough", "under five calls" — and the work it is
// handed does not come with a size written on it. So the three passages carry no
// count at all: not a call budget, not a minute, not a number of rounds. What
// they carry is the question a model can answer about work it has never seen
// before, which is how much its answer changes.
func TestProportionIsTaughtAsAPrincipleAndNeverAsAThreshold(t *testing.T) {
	for _, taught := range []struct{ name, text string }{
		{"prompts/system.md", section(systemPrompt, "- DEPTH OF CHECKING FOLLOWS", "\n- ")},
		{"prompts/task.md", section(taskPrompt, "WHEN THE ANSWER IS THAT NOTHING NEEDED DOING", "\n\n")},
		{"the auditor's prompt", section(auditPrompt, "How deep you look follows", "\n\n")},
	} {
		if taught.text == "" {
			t.Errorf("%s no longer carries the proportion passage at all", taught.name)
			continue
		}
		if at := strings.IndexAny(taught.text, "0123456789"); at >= 0 {
			t.Errorf("%s puts a number in the proportion teaching: %q", taught.name, taught.text)
		}
		for _, banned := range []string{"tool call", "at most", "no more than", "fewer than", "minute"} {
			if strings.Contains(strings.ToLower(taught.text), banned) {
				t.Errorf("%s bounds checking with %q: proportion is a principle, not a budget",
					taught.name, banned)
			}
		}
	}
}

// section is the text between one lead and whatever ends it, so a test can say
// WHERE a sentence landed and what it does not carry — rather than only that the
// page holds it somewhere.
func section(text, from, to string) string {
	at := strings.Index(text, from)
	if at < 0 {
		return ""
	}
	rest := text[at:]
	if end := strings.Index(rest, to); end > 0 {
		return rest[:end]
	}
	return rest
}
