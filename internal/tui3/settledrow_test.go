package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/session"
)

func settledRowApp(t *testing.T) (*app, *settleFake) {
	t.Helper()
	a, agent := settleApp(t)
	a.homeRoot = t.TempDir()
	a.width, a.height, a.workMode = 200, 80, config.WorkFold
	// Restored tasks need not have a proposal proving their admission turn.
	a.entries, a.turn = a.replayBlocks([]session.DisplayEntry{
		{Role: "user", Text: "review the pull request diff"},
		{Role: "assistant", Text: "I have put that out as a task.", Answer: true},
	}, chatReplay(0))
	return a, agent
}

func decideSettledRow(t *testing.T, a *app, agent *settleFake, id uint64, title, answer string, notice session.TaskNotice) session.Event {
	t.Helper()
	notice.Elapsed, notice.Merge = 42*time.Second, "inplace"
	notice.Report = "finished, but needs your look — nobody could check it in 5m0s"
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(id, title, session.TaskUnverified, notice)})
	q := landingAsk(id, session.QuestionLanding, title, askCheckReason, "accept", "not right")
	q.Asked = a.now()
	a.questionFold(session.Event{Kind: session.EventQuestion, Question: &q})
	a.questionRows(a.width)
	*agent.at = agent.at.Add(2 * questionSettle)
	drive(t, a, key(answer))
	want, state := session.TaskAccept, session.TaskDone
	notice.Report = "you took this as done"
	if answer == "n" {
		want, state = session.TaskRefute, session.TaskFailed
		notice.Ending = session.TaskEndingRefused
		notice.Report = "incomplete — you said it is not finished"
	}
	if len(agent.resolved) != 1 || agent.resolved[0].id != id || agent.resolved[0].answer != want {
		t.Fatalf("landing answer did not reach task %d: %+v", id, agent.resolved)
	}
	return update(id, title, state, notice)
}

func answerSettledRow(t *testing.T, a *app, agent *settleFake, id uint64, title, answer string, notice session.TaskNotice) {
	t.Helper()
	final := decideSettledRow(t, a, agent, id, title, answer, notice)
	drive(t, a, streamEventMsg{gen: a.gen, ev: final})
}

func wakeSettledRow(t *testing.T, a *app) {
	t.Helper()
	live := make(chan session.Event)
	t.Cleanup(func() { close(live) })
	a.follows = append(a.follows, queued{woken: true, ch: live})
	a.startFollow()
	drive(t, a, streamEventMsg{gen: a.gen, ev: toolBegin("read", "read diff-review.md")})
	drive(t, a, streamEventMsg{gen: a.gen, ev: toolEnd("read", "review retained")})
	drive(t, a, streamEventMsg{gen: a.gen, ev: text(session.EventTextDelta, "The review is retained.")})
}

func requireSettledRow(t *testing.T, a *app, id uint64, title, state string) {
	t.Helper()
	// Only open doors actually drawn in the conversation, so an unreachable
	// projected fold cannot satisfy the manual's promise.
	for {
		opened := false
		d := a.conversation()
		for _, r := range a.visible(a.bodyWidth()) {
			if r.hit == hitWorkFold && !a.workFoldOpen(d, r.turn) {
				a.openWorkfold(r.turn)
				opened = true
			}
		}
		if !opened {
			break
		}
		a.touch()
	}
	count := 0
	for _, r := range a.visible(a.bodyWidth()) {
		if r.hit == hitWorkCompletion && r.completion.task == id {
			count++
			if got := plain(r.text); !strings.Contains(got, title+" · "+state+" · open result") {
				t.Errorf("settled row = %q", got)
			}
		}
		if r.hit == hitDone && a.entries[r.entry].done.id == id {
			t.Error("superseded your-call card reappeared")
		}
	}
	if count != 1 {
		t.Errorf("task %d has %d reachable result rows, want one\n%s", id, count, strings.Join(plainRows(a), "\n"))
	}
}

func requireSettledReceipt(t *testing.T, a *app, title, answer string) {
	t.Helper()
	if got := plain(strings.Join(a.questionRows(a.width), "\n")); !strings.Contains(got, title+" → "+answer+" · you") {
		t.Errorf("decision receipt disappeared:\n%s", got)
	}
}

func TestRefusedLandingRemainsReachableInConversation(t *testing.T) {
	a, agent := settledRowApp(t)
	const title = "Review the pull request diff"
	answerSettledRow(t, a, agent, 1, title, "n", session.TaskNotice{})
	wakeSettledRow(t, a)
	requireSettledRow(t, a, 1, title, "incomplete")
	requireSettledReceipt(t, a, title, "not right")
	drive(t, a, streamEventMsg{gen: a.gen, ev: session.Event{Kind: session.EventTurnDone}})
	requireSettledRow(t, a, 1, title, "incomplete")
	requireSettledReceipt(t, a, title, "not right")
}

func TestAcceptedLandingRemainsReachableInConversation(t *testing.T) {
	a, agent := settledRowApp(t)
	const title = "Review the pull request diff"
	answerSettledRow(t, a, agent, 1, title, "a", session.TaskNotice{})
	wakeSettledRow(t, a)
	requireSettledRow(t, a, 1, title, "done")
	requireSettledReceipt(t, a, title, "accept")
	drive(t, a, streamEventMsg{gen: a.gen, ev: session.Event{Kind: session.EventTurnDone}})
	requireSettledRow(t, a, 1, title, "done")
	requireSettledReceipt(t, a, title, "accept")
}

func proposedSettledRow(t *testing.T, shape string) (*app, *settleFake, session.TaskNotice) {
	t.Helper()
	a, agent := settledRowApp(t)
	proposal := decidedProposal(7, session.TaskAnswer{Approved: true})
	proposal.Task.Title = "Port the parser"
	drive(t, a, streamEventMsg{gen: a.gen, ev: proposal})
	notice := session.TaskNotice{Merge: "inplace"}
	if shape == "run" {
		// Adaptive runs publish task updates with durable store identity too.
		notice.Program, notice.PlanTask = "do", "t-7"
	}
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(7, "Port the parser", session.TaskRunning, notice)})
	return a, agent, notice
}

func TestProposedLandingDecisionKeepsResultRow(t *testing.T) {
	// Known proposal owners already pass without the batch-render fix; keep
	// both admission shapes as proof that the repair preserves their doors.
	for _, shape := range []string{"run", "node"} {
		for _, answer := range []string{"n", "a"} {
			t.Run(shape+"/"+answer, func(t *testing.T) {
				a, agent, notice := proposedSettledRow(t, shape)
				answerSettledRow(t, a, agent, 7, "Port the parser", answer, notice)
				wakeSettledRow(t, a)
				state, word := "done", "accept"
				if answer == "n" {
					state, word = "incomplete", "not right"
				}
				requireSettledRow(t, a, 7, "Port the parser", state)
				requireSettledReceipt(t, a, "Port the parser", word)
				drive(t, a, streamEventMsg{gen: a.gen, ev: session.Event{Kind: session.EventTurnDone}})
				requireSettledRow(t, a, 7, "Port the parser", state)
				requireSettledReceipt(t, a, "Port the parser", word)
			})
		}
	}
}

func TestProposedDoneBesideAnotherCompletionKeepsBothRows(t *testing.T) {
	// Completions sharing a known owner already pass on the base; neither
	// node nor default adaptive-run admission may lose that behavior.
	for _, shape := range []string{"run", "node"} {
		t.Run(shape, func(t *testing.T) {
			a, _, notice := proposedSettledRow(t, shape)
			second := decidedProposal(8, session.TaskAnswer{Approved: true})
			second.Task.Title = "Rebuild the index"
			drive(t, a, streamEventMsg{gen: a.gen, ev: second})
			drive(t, a, streamEventMsg{gen: a.gen, ev: update(7, "Port the parser", session.TaskDone, notice)})
			drive(t, a, streamEventMsg{gen: a.gen, ev: update(8, "Rebuild the index", session.TaskDone, notice)})
			requireSettledRow(t, a, 7, "Port the parser", "done")
			requireSettledRow(t, a, 8, "Rebuild the index", "done")
		})
	}
}

func TestCompletionAtOccupiedAnchorKeepsBothRows(t *testing.T) {
	for _, shape := range []string{"run", "node"} {
		t.Run(shape, func(t *testing.T) {
			a, _, notice := proposedSettledRow(t, shape)
			// Turn 1 has no fold of its own. Turn 2's tool work begins at
			// turn 1's virtual anchor, which must retain both owners' rows.
			proposal := a.entries[2]
			a.entries[0].text = "Port the parser"
			// Keep the accepted proposal as provenance after turn 2's work.
			a.entries = []entry{a.entries[0]}
			work := foldFixture()[1:]
			for i := range work {
				work[i].turn = 2
			}
			a.entries = append(a.entries, work...)
			a.entries = append(a.entries, proposal)
			a.turn = 2
			second := decidedProposal(8, session.TaskAnswer{Approved: true})
			second.Task.Title = "Rebuild the index"
			drive(t, a, streamEventMsg{gen: a.gen, ev: second})
			drive(t, a, streamEventMsg{gen: a.gen, ev: update(7, "Port the parser", session.TaskDone, notice)})
			drive(t, a, streamEventMsg{gen: a.gen, ev: update(8, "Rebuild the index", session.TaskDone, notice)})
			requireSettledRow(t, a, 7, "Port the parser", "done")
			requireSettledRow(t, a, 8, "Rebuild the index", "done")
		})
	}
}

func TestAcceptedNestedLandingKeepsBothRowsAfterWokenTurnSettles(t *testing.T) {
	// The original seed order already passes on the base. A quick woken turn
	// can also settle before the accepted child's final update arrives.
	for _, late := range []bool{false, true} {
		name := "landing before wake"
		if late {
			name = "landing after woken turn settles"
		}
		t.Run(name, func(t *testing.T) {
			a, agent := settledRowApp(t)
			drive(t, a, streamEventMsg{gen: a.gen, ev: update(1, "Rebuild the index", session.TaskDone, session.TaskNotice{Merge: "inplace"})})
			final := decideSettledRow(t, a, agent, 2, "Port the parser", "a", session.TaskNotice{Parent: 1})
			if !late {
				drive(t, a, streamEventMsg{gen: a.gen, ev: final})
			}
			wakeSettledRow(t, a)
			drive(t, a, streamEventMsg{gen: a.gen, ev: session.Event{Kind: session.EventTurnDone}})
			if late {
				drive(t, a, streamEventMsg{gen: a.gen, ev: final})
			}
			requireSettledRow(t, a, 1, "Rebuild the index", "done")
			requireSettledRow(t, a, 2, "Port the parser", "done")
			requireSettledReceipt(t, a, "Port the parser", "accept")
		})
	}
}
