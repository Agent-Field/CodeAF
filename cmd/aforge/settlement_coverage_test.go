package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/revision"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/verify"
)

// A GOVERNOR MAY REFUSE A ROUND. IT MAY NEVER REFUSE A FINDING.
//
// The measured run: a delivery gate's coverage finding named the one behaviour
// that was dead in the delivered tree; the repair round it asked for was refused
// `goal-already-covered` by a model reading the plan's own Done and the workers'
// own summaries; and the refusal came back as `Overturned: true` with the
// sentence "the job's own reading of what it is judged on found nothing left
// uncovered, so the review's finding is what was wrong" on the deliverable. The
// person was also told "1 behaviour the request states has no check" over two of
// them, because the finding grouped its points by the line of the request they
// were read from and the request was one line.
//
// Three things are pinned here at once, because one run produced all three and
// any of them alone still ships a dead feature: the count is the point count,
// the gap is left standing, and no governor writes the acquittal sentence.
func TestAGovernorRefusesTheRoundAndNeverTheReviewsFinding(t *testing.T) {
	script := newScriptedBrain(t)
	// The judge itself is satisfied; the coverage measurement is the only
	// finding, which is the shape that makes the acquittal the whole story.
	script.gatePasses = true
	script.writeFile = true
	script.jobCovered = true
	script.acceptancePoints = `{"points":[` +
		`{"behaviour":"the --dry-run flag swaps in the blocker at startup","quote":"Add a --dry-run flag"},` +
		`{"behaviour":"iptables never runs under --dry-run","quote":"It must never run iptables"}]}`
	defer script.close()

	workspace := t.TempDir()
	// The gate reads the project before it judges, and a run with nothing to
	// read cannot ask the coverage question at all. The reading is planted with
	// an empty roster, which is the honest shape here: the tree declares no
	// check, so nothing exercises anything and every point of the checklist is
	// open. See verify.RememberBaseline.
	verify.ForgetBaselines()
	revision.ForgetChecklists()
	verify.RememberBaseline(workspace, verify.JobKey(dryRunRequest), verify.Reading{Taken: true})

	var stdout, stderr strings.Builder
	err := doErrand(doRequest{
		task: dryRunRequest, keep: true, workspace: workspace,
		timeout: 120 * time.Second, stdout: &stdout, stderr: &stderr, newClient: script.client,
	})

	var status exitStatus
	if !asExitStatus(err, &status) || status != exitIncomplete {
		t.Fatalf("a run holding an unclosed coverage finding left with %v, want %d\nstderr:\n%s",
			err, exitIncomplete, stderr.String())
	}
	// THE COUNT THE PERSON READS IS THE POINT COUNT. Both behaviours quote the
	// same single line of the request, which is exactly the case that reported a
	// sixth of the truth.
	if !strings.Contains(stderr.String(), "2 behaviours") {
		t.Fatalf("the person was not told how many behaviours have no check:\n%s", stderr.String())
	}
	if strings.Contains(stdout.String()+stderr.String(), "the review's finding is what was wrong") {
		t.Fatalf("a governor acquitted the review's finding:\nstdout:\n%s\nstderr:\n%s",
			stdout.String(), stderr.String())
	}

	home := keptHome(stderr.String())
	if home == "" {
		t.Fatalf("the run kept no store to read back:\n%s", stderr.String())
	}
	defer os.RemoveAll(home)
	graph, err := store.Open(filepath.Join(home, "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer graph.Close()

	job := deliverableNode(t, graph)
	gates, err := graph.DeliveryGateLineage(job)
	if err != nil {
		t.Fatal(err)
	}
	if len(gates) == 0 {
		t.Fatalf("the run recorded no delivery gate at all")
	}
	last := gates[len(gates)-1]
	if last.Overturned {
		t.Fatalf("a refused round acquitted the finding: %+v", last)
	}
	if !last.Unclosed {
		t.Fatalf("the finding was not left standing: %+v", last)
	}
	if len(last.Unexercised) != 2 {
		t.Fatalf("the gate row holds %d behaviours, want one per point: %+v",
			len(last.Unexercised), last.Unexercised)
	}

	// And the growth journal either never refused, or recorded that it saw the
	// finding it was being asked to fund.
	rounds, err := graph.JobGrowthRounds(job)
	if err != nil {
		t.Fatal(err)
	}
	for _, round := range rounds {
		if round.Cause != "goal-already-covered" {
			continue
		}
		if round.Allowed && len(round.CoveredDespite) > 0 {
			continue
		}
		t.Fatalf("the coverage question refused a round bought for a live finding: %+v", round)
	}
}

// deliverableNode is the node the errand hands its work over from: the one whose
// parent is the root. Its id is the job's, and every gate and growth row of the
// job is under it or under its own split namespace.
func deliverableNode(t *testing.T, graph *store.Store) string {
	t.Helper()
	ids, err := graph.NodeIDsWithPrefix("task")
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		node, ok, err := graph.Node(id)
		if err != nil {
			t.Fatal(err)
		}
		if ok && node.Parent == store.RootID {
			return id
		}
	}
	t.Fatal("the errand left no node under the root")
	return ""
}

// The one-line request whose two behaviours the acceptance pass reads out of it.
// Two clauses, one line: the shape that collapsed six findings into one.
const dryRunRequest = "Add a --dry-run flag. It must never run iptables."
