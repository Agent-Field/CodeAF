package session

import (
	"context"
	"strings"
	"testing"
	"time"
)

// Both automatic entrances must hand the checker the verification that was
// declared for this request, instead of leaving it trapped in acceptance prose.
func TestAutomaticTaskChecksReachTheRealChecker(t *testing.T) {
	for _, checkpoint := range []bool{false, true} {
		name := "after answer"
		if checkpoint {
			name = "checkpoint handoff"
		}
		t.Run(name, func(t *testing.T) {
			const verdict = `{"work":true,"wide":true,"goal":"implement and check the requested repository changes","acceptance":"the requested behavior and its checks pass","checks":["sh ./verify.sh"]}`
			completer := &routeCompleter{answer: "I will implement the changes.", verdict: verdict}
			var agent *Agent
			asked := routeAsk
			if checkpoint {
				agent, _, _ = racingAgent(t, completer)
				completer.ahead, completer.aheadConfirm = verdict, verdict
				asked = routeEnumerated
			} else {
				agent, _, _ = routeAgent(t, completer)
			}
			collect(t, mustSubmit(t, agent, asked))
			node := agent.graph().node(1)
			if node == nil {
				t.Fatal("no task was admitted")
			}
			ground := t.TempDir()
			writeCheckFile(t, ground, "verify.sh", "#!/bin/sh\nprintf VERIFIED", 0o755)
			writeCheckFile(t, ground, "deploy.sh", "#!/bin/sh\nprintf REPEATED_WORK", 0o755)
			door := auditDoorFor(node, ground)
			if len(door.checks) != 1 || door.checks[0] != "sh ./verify.sh" {
				t.Fatalf("checker received %q, want the declared verification", door.checks)
			}
			if door.window() != auditDeadline {
				t.Fatal("runnable verification received only a reading window")
			}
			tool := checkerBash(t, door, ground)
			if text, refused := typedAtTheChecker(t, tool, "sh ./verify.sh"); refused || !strings.Contains(text, "VERIFIED") {
				t.Fatalf("declared verification did not execute: %q", text)
			}
			if _, refused := typedAtTheChecker(t, tool, "sh ./deploy.sh"); !refused {
				t.Fatal("an undeclared action became repeatable verification")
			}
		})
	}
}

// Old or malformed declarations must not acquire execution rights merely
// because the routing path can now carry valid verification commands.
func TestAutomaticTaskAdmissionDoesNotGrantStaleOrComposedChecks(t *testing.T) {
	for name, test := range map[string]struct {
		request string
		check   string
	}{
		"changed request":  {routeAsk + "; only report the findings now", "sh ./verify.sh"},
		"composed command": {routeAsk, "sh ./verify.sh && sh ./deploy.sh"},
	} {
		t.Run(name, func(t *testing.T) {
			agent, _, _ := routeAgent(t, &routeCompleter{})
			agent.mu.Lock()
			agent.personAsk = test.request
			agent.mu.Unlock()
			verdict := routeVerdict{Work: true, Goal: routeAsk,
				Checks: []string{test.check}, checksRequest: routeAsk}
			_, id := agent.launchRouteTask(newEventHub(), verdict, "repository review", drawnDivision{}, nil)
			node := agent.graph().node(id)
			if node == nil {
				t.Fatal("a rejected verification list should not discard the work")
			}
			ground := t.TempDir()
			writeCheckFile(t, ground, "verify.sh", "#!/bin/sh\nprintf SHOULD_NOT_RUN", 0o755)
			door := auditDoorFor(node, ground)
			if len(door.checks) != 0 {
				t.Fatalf("invalid verification became a runnable check: %q", door.checks)
			}
			if text, refused := typedAtTheChecker(t, checkerBash(t, door, ground), "sh ./verify.sh"); !refused || strings.Contains(text, "SHOULD_NOT_RUN") {
				t.Fatalf("invalid verification executed: %q", text)
			}
		})
	}
}

// A check of the assembled result belongs to the whole request. Handing out
// one remaining edit cannot let that child rerun the parent's family check.
func TestAPartialAutomaticHandoffDoesNotInheritWholeRequestChecks(t *testing.T) {
	for _, partial := range []bool{false, true} {
		name := "whole request"
		read := checkpointRead{}
		if partial {
			name = "independent remainder"
			read.ownRemainder = custodyHeldPart
		}
		t.Run(name, func(t *testing.T) {
			completer := &routeCompleter{handoff: custodyDraft}
			agent, _, _ := routeAgent(t, completer)
			agent.personAsk = routeAsk
			verdict := routeVerdict{Work: true, Goal: routeAsk,
				Checks: []string{"sh ./family-check.sh"}, checksRequest: routeAsk}
			over := agent.handOverRunningTurn(context.Background(), newEventHub(), &Usage{},
				time.Now(), agent.model, checkpointSplitNote, checkpointSeamMark, 0, nil,
				verdict, read, nil)
			if !over.moved {
				t.Fatalf("the handoff did not admit the remainder: %+v", over)
			}
			node := agent.graph().node(over.taskID)
			want := 1
			if partial {
				want = 0
			}
			if got := node.repeatableChecks(); len(got) != want {
				t.Fatalf("handoff checks = %q, want %d declarations", got, want)
			}
		})
	}
}
