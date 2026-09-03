package main

import (
	"context"
	"encoding/json"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// finishedErrand gives the settlement tests the same real command, splice and
// worker ending used throughout do_test.go. The optional failure is handed to
// settleNode, so these tests do not create a second fixture vocabulary.
func finishedErrand(t *testing.T, failure string) (*store.Store, *settlementWatch, store.Node) {
	t.Helper()
	graph, err := store.Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { graph.Close() })
	session := "headless-uncollected-" + strings.ReplaceAll(t.Name(), "/", "-")
	command, err := graph.RequestCommand(store.Command{
		SessionID: session, Kind: store.CommandSplice, Instruction: "repair the parser",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := graph.ResolveCommand(command.Seq, store.CommandApplied, "spliced 1 node"); err != nil {
		t.Fatal(err)
	}
	spliceForErrand(t, graph, session, []store.NodeSpec{{
		ID: "task-1", Brief: "repair the parser", Stage: 0,
	}})
	settleNode(t, graph, "task-1", "the parser repair is complete", failure)
	node, found, err := graph.Node("task-1")
	if err != nil || !found {
		t.Fatalf("node task-1: found %t, err %v", found, err)
	}
	watcher := &settlementWatch{
		graph: graph, session: session, commandSeq: command.Seq,
		refused: make(chan planEstimate, 1), progress: io.Discard, started: time.Now(),
	}
	return graph, watcher, node
}

// TestATreeThatDoesNotBuildIsNeverReportedSettled proves B1, B2 and B3 through
// the real store reader and the JSON door. The finished-tree reading's own
// sentence reaches both answer spellings, while the existing incomplete rung
// supplies ok=false and exit 2.
func TestATreeThatDoesNotBuildIsNeverReportedSettled(t *testing.T) {
	const finding = "`go test ./...` ran on the finished tree and its suite failed to collect, so no check of it ran: undefined: findQuotationField"
	for _, shape := range []struct {
		name    string
		failure string
	}{
		{name: "the node failed", failure: "context deadline exceeded"},
		{name: "the node said done"},
	} {
		t.Run(shape.name, func(t *testing.T) {
			graph, watcher, _ := finishedErrand(t, shape.failure)
			if err := graph.RecordVerification("task-1", store.VerificationReading{
				When: store.VerificationWhenFinished, Why: finding, Command: "go test ./...", Uncollected: true,
			}); err != nil {
				t.Fatal(err)
			}
			outcome, settled, err := watcher.check()
			if err != nil || !settled {
				t.Fatalf("check = settled %t, err %v", settled, err)
			}
			if outcome.Settled || outcome.stop != stopIncomplete {
				t.Fatalf("tree ending = settled %t, stop %q", outcome.Settled, outcome.stop)
			}
			if !strings.Contains(outcome.Deliverable, finding) {
				t.Fatalf("the finished-tree finding never reached the deliverable: %q", outcome.Deliverable)
			}

			var stdout strings.Builder
			reportErr := reportErrand(doRequest{asJSON: true, stdout: &stdout, stderr: io.Discard}, outcome)
			var status exitStatus
			if !asExitStatus(reportErr, &status) || status != exitIncomplete {
				t.Fatalf("the broken tree left with %v, want exit 2", reportErr)
			}
			var printed struct {
				OK          bool       `json:"ok"`
				Stop        stopReason `json:"stop"`
				Answer      string     `json:"answer"`
				Deliverable string     `json:"deliverable"`
				Settled     bool       `json:"settled"`
			}
			if err := json.Unmarshal([]byte(stdout.String()), &printed); err != nil {
				t.Fatalf("stdout is not one JSON object: %v\n%s", err, stdout.String())
			}
			if printed.OK || printed.Settled || printed.Stop != stopIncomplete {
				t.Fatalf("printed ending = %+v", printed)
			}
			if !strings.Contains(printed.Answer, finding) || !strings.Contains(printed.Deliverable, finding) {
				t.Fatalf("the reading's words did not reach both answer spellings: %+v", printed)
			}
		})
	}
}

// TestARunWhoseCheckSaidNothingElseStillSettles proves B5 and I1. Only the
// last uncollected finished-tree reading changes settlement; an absent check, a
// cut reading, a red suite, a repaired tree and an unreadable journal retain
// their prior meanings.
func TestARunWhoseCheckSaidNothingElseStillSettles(t *testing.T) {
	const finding = "the finished tree did not collect"
	for _, shape := range []struct {
		name        string
		readings    []store.VerificationReading
		wantSettled bool
	}{
		{name: "the project declares no check", wantSettled: true},
		{name: "the reading hit its ceiling", wantSettled: true, readings: []store.VerificationReading{{
			When: store.VerificationWhenFinished, Why: "the check was killed at its ceiling",
			Command: "go test ./...", TimedOut: true,
		}}},
		{name: "the suite ran and went red", wantSettled: true, readings: []store.VerificationReading{{
			When: store.VerificationWhenFinished, Command: "go test ./...", Read: true, Named: 12, Red: 1,
		}}},
		{name: "the repair round collected after the first reading did not", wantSettled: true,
			readings: []store.VerificationReading{
				{When: store.VerificationWhenFinished, Why: finding, Command: "go test ./...", Uncollected: true},
				{When: store.VerificationWhenFinished, Command: "go test ./...", Read: true, Named: 12},
			}},
		{name: "the last reading could not collect after the first one did", wantSettled: false,
			readings: []store.VerificationReading{
				{When: store.VerificationWhenFinished, Command: "go test ./...", Read: true, Named: 12},
				{When: store.VerificationWhenFinished, Why: finding, Command: "go test ./...", Uncollected: true},
			}},
	} {
		t.Run(shape.name, func(t *testing.T) {
			graph, watcher, _ := finishedErrand(t, "")
			for _, reading := range shape.readings {
				if err := graph.RecordVerification("task-1", reading); err != nil {
					t.Fatal(err)
				}
			}
			outcome, settled, err := watcher.check()
			if err != nil || !settled {
				t.Fatalf("check = settled %t, err %v", settled, err)
			}
			if outcome.Settled != shape.wantSettled {
				t.Fatalf("ending changed = settled %t, stop %q, exit %d", outcome.Settled, outcome.stop, outcome.status())
			}
			wantStatus := exitIncomplete
			if shape.wantSettled {
				wantStatus = exitDone
			}
			if outcome.status() != wantStatus {
				t.Fatalf("ending changed = settled %t, stop %q, exit %d", outcome.Settled, outcome.stop, outcome.status())
			}
		})
	}

	t.Run("an unreadable journal decides nothing", func(t *testing.T) {
		graph, watcher, node := finishedErrand(t, "")
		if err := graph.Close(); err != nil {
			t.Fatal(err)
		}
		if reason := watcher.uncollectedReason([]store.Node{node}); reason != "" {
			t.Fatalf("an unreadable journal supplied evidence: %q", reason)
		}
	})

	t.Run("a question still settles without being ok", func(t *testing.T) {
		graph, err := store.Open(filepath.Join(t.TempDir(), "graph.db"))
		if err != nil {
			t.Fatal(err)
		}
		defer graph.Close()
		const session = "headless-uncollected-question"
		command, err := graph.RequestCommand(store.Command{
			SessionID: session, Kind: store.CommandSplice, Instruction: "repair the parser",
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := graph.AskQuestion(store.AgentQuestion{
			SessionID: session, Text: "Which parser should be repaired?",
			OriginCommandSeq: command.Seq, Urgency: store.QuestionBlocking,
		}); err != nil {
			t.Fatal(err)
		}
		if err := graph.ResolveCommand(command.Seq, store.CommandRejected, "asked the user"); err != nil {
			t.Fatal(err)
		}
		watcher := &settlementWatch{
			graph: graph, session: session, commandSeq: command.Seq,
			refused: make(chan planEstimate, 1), progress: io.Discard, started: time.Now(),
		}
		outcome, settled, err := watcher.check()
		if err != nil || !settled {
			t.Fatalf("check = settled %t, err %v", settled, err)
		}
		printed := errandEnvelope(outcome)
		if !outcome.Settled || printed.OK || outcome.stop != stopQuestion || outcome.status() != exitUnanswered {
			t.Fatalf("question ending changed: outcome %+v, envelope %+v", outcome, printed)
		}
	})
}

// TestAFailedNodeDoesNotPublishAGoErrorAsItsAnswer proves B4. The cause is
// retained, but it is no longer the whole answer: the run first says what
// happened in the same words other unfinished endings already use.
func TestAFailedNodeDoesNotPublishAGoErrorAsItsAnswer(t *testing.T) {
	_, watcher, _ := finishedErrand(t, context.DeadlineExceeded.Error())
	outcome, settled, err := watcher.check()
	if err != nil || !settled {
		t.Fatalf("check = settled %t, err %v", settled, err)
	}
	if strings.TrimSpace(outcome.Deliverable) == context.DeadlineExceeded.Error() {
		t.Fatalf("the Go error is still the whole answer: %q", outcome.Deliverable)
	}
	if !strings.HasPrefix(outcome.Deliverable, "It did not finish.") {
		t.Fatalf("the failed run does not say what happened: %q", outcome.Deliverable)
	}
	if !strings.Contains(outcome.Deliverable, context.DeadlineExceeded.Error()) {
		t.Fatalf("the failed run swallowed its cause: %q", outcome.Deliverable)
	}
}
