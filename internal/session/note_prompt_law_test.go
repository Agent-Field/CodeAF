package session

// THE TWO CLAUSES A TRIM MUST NOT CUT.
//
// The worker's page is under a byte budget that a sentence can cross, and the
// notes paragraph crossed it once already — so the next person to need room
// will come looking at this paragraph, and these are the two sentences in it
// that are not prose.
//
//   - A worker has to know that a note on a SIBLING'S task reaches that task's
//     worker, or it will not write one, and the channel has no writer. That is
//     problem 1 of docs/design/chat-coordination/DESIGN.md: a task that found
//     another task's premise wrong, with nowhere to say it.
//   - A worker has to know that a note is NOT a direction, or it will change
//     what its task is judged by on a sibling's word, with none of the version
//     check a revised assignment carries. That is the design's first hazard,
//     and it is the one way this channel can quietly make a run build the wrong
//     thing.
//
// The delivered note carries the second clause too ([run.planNoteSpoken]), and
// internal/run's own test asserts it. This is the standing half: what a worker
// knows before any note arrives.

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/effort"
)

// A NEEDLE IS MATCHED AGAINST THE SENTENCE AND NOT AGAINST THE WRAP. The pages
// are Markdown wrapped at about eighty columns, so a clause this test is about
// falls across a line break as often as not — "not an\norder" is how the page
// spells "not an order" today, and a plain Contains would report it missing
// from a page that says it. This test failed exactly that way when it was
// written, which is the failure worth keeping the note about: a needle that can
// be broken by a re-wrap is a needle that will one day fail a page with nothing
// wrong with it, or pass one that has lost the sentence. So both sides have
// their whitespace collapsed first ([oneLine], which this package already has
// for the same reason on a different string), and what is compared is the words.
func TestABeltWorkerIsTaughtBothHalvesOfTheNoteChannel(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	t.Setenv(planCLIBinEnv, filepath.Join(t.TempDir(), "stub-codeaf"))
	page := oneLine(systemTextOf(newRunBeltWorker(t, effort.None)))

	for _, law := range []struct{ needle, why string }{
		{"REACHES THAT TASK'S WORKER BETWEEN ITS STEPS",
			"a worker that does not know a note reaches a sibling will never write one, and the channel has no writer"},
		{"plandb task note",
			"the verb the outbound half is written with"},
		{"not an order",
			"a worker that reads a note as a direction changes what its task is judged by with no version check behind it"},
		{"IT DOES NOT CHANGE YOUR WORK ORDER",
			"said in the page's own voice, because it is the line the whole hazard turns on"},
		{"revised assignment",
			"and what a real change of direction looks like instead"},
	} {
		if !strings.Contains(page, oneLine(law.needle)) {
			t.Errorf("the worker's page lost %q — %s", law.needle, law.why)
		}
	}
}
