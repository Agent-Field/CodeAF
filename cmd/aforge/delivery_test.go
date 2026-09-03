package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/revision"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

// reviewVerdict is the last thing a review is for and the first thing a bound
// on the path takes away. It sits at the end of longReview deliberately: a
// deliverable that arrives without it arrived truncated, whatever its length.
const reviewVerdict = "VERDICT: request changes — the migration guard is missing."

// longReview is a deliverable bigger than the bound that used to sit on the
// node summary. The measured defect is exactly this shape: 697 seconds of PR
// review that stopped mid-word at 4,096 bytes and never reached the verdict the
// ask required, because the store clipped the record at the bound meant for
// what a reader takes out of it.
func longReview() string {
	var body strings.Builder
	body.WriteString("PR 482 review\n\n")
	for line := 1; body.Len() < 9<<10; line++ {
		fmt.Fprintf(&body, "%d. the parser change is covered by its own test and reads correctly.\n", line)
	}
	body.WriteString("\n" + reviewVerdict)
	return body.String()
}

// The whole path, followed to the last byte: what the worker said, what the
// store recorded, what the thread announced, and what the caller read on
// stdout — in both shapes stdout has.
//
// Every one of those used to end at 4,096 bytes, because Complete bounded the
// node's own summary with MaxDigestBytes. A digest bound belongs at the read
// (a dependency input, a quoted partial), never on the record: the record IS
// the deliverable.
func TestALongDeliverableSurvivesToItsFinalByte(t *testing.T) {
	for _, shape := range []struct {
		name   string
		asJSON bool
	}{{"text", false}, {"json", true}} {
		t.Run(shape.name, func(t *testing.T) {
			script := newScriptedBrain(t)
			script.longAnswer = longReview()
			defer script.close()

			var stdout, stderr strings.Builder
			if err := doErrand(doRequest{
				task: "review PR 482 and say whether to approve it", keep: true, asJSON: shape.asJSON,
				timeout: 60 * time.Second, stdout: &stdout, stderr: &stderr, newClient: script.client,
			}); err != nil {
				t.Fatalf("errand: %v\n%s", err, stderr.String())
			}
			home := keptHome(stderr.String())
			if home == "" {
				t.Fatalf("--keep never said where the store is:\n%s", stderr.String())
			}
			defer os.RemoveAll(home)

			delivered := stdout.String()
			if shape.asJSON {
				var outcome struct {
					Deliverable string `json:"deliverable"`
				}
				if err := json.Unmarshal([]byte(stdout.String()), &outcome); err != nil {
					t.Fatalf("stdout is not one JSON object: %v", err)
				}
				delivered = outcome.Deliverable
			}
			assertWhole(t, "what the caller read", delivered, script.longAnswer)

			graph, err := store.Open(filepath.Join(home, "graph.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer graph.Close()

			nodes, err := graph.Nodes()
			if err != nil {
				t.Fatal(err)
			}
			var journaled string
			for _, node := range nodes {
				if node.Parent == store.RootID && len(node.Summary) > len(journaled) {
					journaled = node.Summary
				}
			}
			assertWhole(t, "the journaled node summary", journaled, script.longAnswer)

			// The announcement is the same summary read back by the reconciler
			// and posted into the thread, so a headless run may or may not have
			// posted it before the settlement watch let go. Whichever it is, it
			// must not be a clipped one — the thread's own bound is the only
			// bound on this path. The announcement is proved on its own,
			// deterministically, in the resident's delivery tests.
			messages, err := graph.Messages("", 0, 500)
			if err != nil {
				t.Fatal(err)
			}
			for _, message := range messages {
				if strings.HasPrefix(message.Body, "PR 482 review") {
					assertWhole(t, "the announced thread message", message.Body, script.longAnswer)
				}
			}
		})
	}
}

// A repaired deliverable ships as the repair, and nothing of the argument that
// produced it ships at all.
//
// The panel read a cell that objectively passed and scored it 2/5, because the
// visible reply opened with a reservation from a dispute the system had already
// settled. The rule is structural: what is delivered is the FINAL state of the
// work; the critique, the round, and the verdict live in the gate ledger, where
// the belt tools read them on request.
func TestARepairedDeliverableShipsWithoutTheArgument(t *testing.T) {
	script := newScriptedBrain(t)
	script.revisionCloses = true
	defer script.close()

	var stdout, stderr strings.Builder
	if err := doErrand(doRequest{
		task: "write the release note and include the migration steps", keep: true,
		timeout: 60 * time.Second, stdout: &stdout, stderr: &stderr, newClient: script.client,
	}); err != nil {
		t.Fatalf("errand: %v\n%s", err, stderr.String())
	}
	if got := script.count("revision"); got != 1 {
		t.Fatalf("the revision ran %d times, want exactly 1", got)
	}
	home := keptHome(stderr.String())
	if home == "" {
		t.Fatalf("--keep never said where the store is:\n%s", stderr.String())
	}
	defer os.RemoveAll(home)

	delivered := stdout.String()
	if !strings.Contains(delivered, repairedAnswer) {
		t.Fatalf("the repaired deliverable did not ship:\n%s", delivered)
	}
	graph, err := store.Open(filepath.Join(home, "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer graph.Close()
	messages, err := graph.Messages("", 0, 500)
	if err != nil {
		t.Fatal(err)
	}
	said := delivered
	for _, message := range messages {
		said += "\n" + message.Body
	}

	// Nothing of the dispute: not the rejected draft, not the reviewer's words,
	// not a narration of the round, not a reservation.
	for _, residue := range []string{
		firstDraftAnswer,
		gateCritique,
		"a review found",
		"revised before delivering",
		"reservation",
	} {
		if strings.Contains(strings.ToLower(said), strings.ToLower(residue)) {
			t.Fatalf("the settled dispute left %q in what the person reads:\n%s", residue, said)
		}
	}

	// It is settled rather than forgotten: the ledger holds the round.
	gates, err := graph.DeliveryGateLineage(deliveredJobID(t, graph))
	if err != nil {
		t.Fatal(err)
	}
	if len(gates) == 0 {
		t.Fatal("the gate round vanished from the journal as well as from the thread")
	}
}

// The revision is model-authored, so one clause carries the same rule into the
// prompt: the revised message replaces the first attempt whole and never
// narrates the revision.
func TestTheRevisionContractForbidsNarratingTheRevision(t *testing.T) {
	for _, clause := range []string{
		"replaces the previous attempt entirely",
		"Say nothing about the review",
	} {
		if !strings.Contains(revision.GateRevisionContract, clause) {
			t.Fatalf("the revision contract lost %q:\n%s", clause, revision.GateRevisionContract)
		}
	}
}

// The honest note is kept and moved, not deleted. An ungrounded review — one
// that cannot quote the person's own words — still gets said, because a gap
// swallowed is a gap hidden. It is one short paragraph, it comes after the
// deliverable, and it is said exactly once.
func TestTheUngroundedGapNoteComesAfterTheWorkAndOnlyOnce(t *testing.T) {
	script := newScriptedBrain(t)
	script.inventedGap = true
	defer script.close()

	var stdout, stderr strings.Builder
	// Exit 2. The note is kept and moved; what it is NOT is a clean settlement.
	// A finding refused for where its words came from has been checked against
	// nothing in the world, so it still stands, and the run is handing over less
	// than it promised. See deliveredWhole and docs/design/gate/SETTLEMENT.md §2.
	err := doErrand(doRequest{
		task: "write the release note and include the migration steps", keep: true,
		timeout: 60 * time.Second, stdout: &stdout, stderr: &stderr, newClient: script.client,
	})
	var status exitStatus
	if !asExitStatus(err, &status) || status != exitIncomplete {
		t.Fatalf("errand: %v, want the partial code\n%s", err, stderr.String())
	}
	home := keptHome(stderr.String())
	if home == "" {
		t.Fatalf("--keep never said where the store is:\n%s", stderr.String())
	}
	defer os.RemoveAll(home)

	delivered := stdout.String()
	work := strings.Index(delivered, firstDraftAnswer)
	note := strings.Index(delivered, inventedGapText)
	if work < 0 || note < 0 {
		t.Fatalf("the delivery lost either the work or the note:\n%s", delivered)
	}
	if note < work {
		t.Fatalf("the review's words stand in front of the work:\n%s", delivered)
	}

	graph, err := store.Open(filepath.Join(home, "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer graph.Close()
	messages, err := graph.Messages("", 0, 500)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(delivered, inventedGapText); got != 1 {
		t.Fatalf("the note is in the delivery %d times, want exactly once", got)
	}
	// And it is never a message of its own. Posted separately it arrived
	// BEFORE the announcement — the review's words standing in front of an
	// answer the review was wrong about.
	for _, message := range messages {
		if strings.Contains(message.Body, inventedGapText) && !strings.Contains(message.Body, firstDraftAnswer) {
			t.Fatalf("the review's words were posted on their own, ahead of the work:\n%s", message.Body)
		}
	}
}

// deliveredJobID is the top-level node the errand settled on.
func deliveredJobID(t *testing.T, graph *store.Store) string {
	t.Helper()
	nodes, err := graph.Nodes()
	if err != nil {
		t.Fatal(err)
	}
	for _, node := range nodes {
		if node.Parent == store.RootID {
			return node.ID
		}
	}
	t.Fatal("the run left no top-level node")
	return ""
}

// assertWhole is the assertion the defect asks for: not "long enough" but
// "carried to the final byte". The power-of-two check is the fingerprint of the
// bug — a body that stops at a round binary number stopped because something
// clipped it, not because the work ended there.
func assertWhole(t *testing.T, where, got, want string) {
	t.Helper()
	if strings.Contains(got, want) && strings.Contains(got, reviewVerdict) {
		return
	}
	for _, suspect := range []int{4 << 10, 8 << 10, 16 << 10} {
		if len(got) == suspect || len(got) == suspect-3 {
			t.Fatalf("%s stops at %d bytes — a power-of-two clip, not an ending", where, len(got))
		}
	}
	t.Fatalf("%s carries %d bytes of the deliverable's %d and no verdict:\n…%s",
		where, len(got), len(want), tail(got, 120))
}

func tail(text string, bytes int) string {
	if len(text) <= bytes {
		return text
	}
	return text[len(text)-bytes:]
}
