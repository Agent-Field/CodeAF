package exec

import (
	"strings"
	"testing"
)

// The most frequent quality failure a blind UX rubric found was not a wrong
// answer — it was no answer: a final message that was a plan ("I will look up
// the filings and compare…") or a pointer to where the answer lives. Three of
// five runs of the same research journey against the same model produced the
// right figures with a citation; two produced the plan.
//
// The old contract forbade the *ending* — a summary of process, a claim that the
// work is done — and said nothing about the opening or the tense, so a message
// that had never done the work broke none of its rules. Both are stated now, and
// both are pinned here, because this paragraph is the single place the leaf is
// told what its last message is for.
func TestTheLeafFinalMessageIsTheArtifactAndNeverAPlan(t *testing.T) {
	for name, required := range map[string]string{
		"the message is the thing":                  "is the deliverable itself,\nnot a report about it",
		"it opens on the substance":                 "It\nopens on the substance",
		"an opening about the work fails":           "A message that opens on what you did, on how you went\nabout it, or on what you are about to do",
		"the future tense is the worst form of it":  "The worst form of that is work stated in the future",
		"a plan is what was to be carried out":      "a plan is what you were supposed to carry out, not what you were supposed to\nhand back",
		"and the job is simply not done yet":        "If you catch yourself writing one, the job is not done",
		"the ending stays closed too":               "never end with a statement that the work is done",
		"the split is never answer against pointer": "The split is between the answer\nand its working, never between the answer and a pointer to the answer",
	} {
		if !strings.Contains(systemPrompt, required) {
			t.Errorf("the leaf contract no longer states %s: %q missing", name, required)
		}
	}
	// Emergent, not enumerated: the law is a property of the message, so it must
	// not have arrived as a list of openings to refuse.
	for _, forbidden := range []string{"do not begin with", "avoid phrases", "forbidden words"} {
		if strings.Contains(strings.ToLower(systemPrompt), forbidden) {
			t.Errorf("the leaf contract grew a phrase list: %q", forbidden)
		}
	}
}

// Every shape of leaf that can produce a final deliverable reads the same law.
// The contract is assembled per assignment — the attribution paragraph, the
// reflex narrowing, the working method generated for this kind of work — and a
// law that lived in only some of those combinations would be a law only some
// jobs are held to. It rides in the base, so this enumerates the variants and
// asserts the base survives all of them.
func TestEveryLeafVariantCarriesTheAnswerFirstLaw(t *testing.T) {
	const law = "is the deliverable itself,\nnot a report about it"
	for name, build := range map[string]struct {
		attribution bool
		task        Task
	}{
		"a bare leaf":                       {task: Task{}},
		"a leaf under the attribution law":  {attribution: true, task: Task{}},
		"a reflex":                          {task: Task{Reflex: true}},
		"a leaf with a generated method":    {task: Task{Contract: "Read the filings first."}},
		"a reflex with a generated method":  {task: Task{Reflex: true, Contract: "Read the filings first."}},
		"the deliverable owner of a fanout": {attribution: true, task: Task{Contract: contractOfASink}},
	} {
		t.Run(name, func(t *testing.T) {
			linear := &Linear{attribution: build.attribution}
			system := linear.system(build.task)
			if !strings.Contains(system, law) {
				t.Fatalf("the answer-first law is missing from %s", name)
			}
			// The generated method now rides the head of the brief rather than
			// the tail of the system message, so the system message can be the
			// same bytes for every leaf of a run and be served warm. The law is
			// still read first — the system message is still the first message —
			// and the method must not have been dropped on the way.
			if contract := strings.TrimSpace(build.task.Contract); contract != "" {
				if strings.Contains(system, contract) {
					t.Fatalf("%s put the per-node working method back in the shared system message", name)
				}
				brief := linear.brief(build.task)
				if !strings.HasPrefix(brief, "How this particular kind of job is done well:\n"+contract) {
					t.Fatalf("%s does not lead its brief with the working method:\n%s", name, brief)
				}
			}
		})
	}
}

// contractOfASink stands in for the method the planner writes for the node whose
// output IS the deliverable — the case where a pointer is most tempting because
// the parts really are in files.
const contractOfASink = "Read every part in full, reconcile the figures, and write the merged result out."
