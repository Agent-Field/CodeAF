package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
