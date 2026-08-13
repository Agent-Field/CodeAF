package head

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// An unsized head is the head that shipped before the window was a fact this
// package could reach. Every block has to come back at the literal its own
// prose argues for, because that literal is what a surface with no catalog —
// a test, a visitor client, a model the catalog cannot size — still gets.
func TestUnknownWindowKeepsEveryLiteral(t *testing.T) {
	budget := newPromptBudget(0)
	for _, check := range []struct {
		name string
		got  int
		want int
	}{
		{"thread", budget.thread, maxThreadContextBytes},
		{"thread message", budget.threadMessage, threadMessageBytes},
		{"board", budget.board, maxGraphContextBytes},
		{"board rows", budget.boardRows, BoardRowCap},
		{"depth", budget.deep, maxDeepContextBytes},
		{"depth result", budget.deepResult, deepResultBytes},
		{"depth jobs", budget.deepJobs, deepSliceLimit},
		{"depth files", budget.deepFiles, deepFileCap},
		{"notebook", budget.notebook, notebookContextBytes},
		{"re-entry", budget.reentry, threadBriefBytes},
		{"re-entry line", budget.reentryLine, threadBriefLineBytes},
		{"result read", budget.result, beltResultBytes},
		{"grounding read", budget.grounding, beltGroundingBytes},
		{"artifact read", budget.artifact, beltArtifactBytes},
		{"artifact tail", budget.artifactTail, beltArtifactTailBytes},
		{"transcript read", budget.transcript, transcriptBytes},
		{"lens page", budget.lensPage, lensPageBytes},
		{"bash page", budget.bash, bashOutputBytes},
		{"fork context", budget.fork, forkContextBytes},
		{"correction", budget.correction, correctionPreviousBytes},
	} {
		if check.got != check.want {
			t.Errorf("%s on an unsized window = %d, want the literal %d",
				check.name, check.got, check.want)
		}
	}
}

// The calibration, stated at both ends. On the window most talk slots run, the
// reserve the law keeps for reasoning is most of the window and the head stays
// exactly where it was — that is the honest reading of a full window, not a
// miscalibration. On a window with real room, every ambient block grows, and
// none of them runs away with the pot: the proportions hold.
func TestCalibrationHoldsAtBothEnds(t *testing.T) {
	ordinary := newPromptBudget(131072)
	roomy := newPromptBudget(400000)
	for _, check := range []struct {
		name         string
		today        int
		narrow, wide int
	}{
		{"thread", maxThreadContextBytes, ordinary.thread, roomy.thread},
		{"board", maxGraphContextBytes, ordinary.board, roomy.board},
		{"depth", maxDeepContextBytes, ordinary.deep, roomy.deep},
		{"notebook", notebookContextBytes, ordinary.notebook, roomy.notebook},
		{"re-entry", threadBriefBytes, ordinary.reentry, roomy.reentry},
	} {
		if check.narrow != check.today {
			t.Errorf("%s on a 128k window = %d, want today's %d — a full window buys nothing and may cost nothing",
				check.name, check.narrow, check.today)
		}
		if check.wide <= check.today {
			t.Errorf("%s on a 400k window = %d, no more than today's %d — a window with room has to pay out",
				check.name, check.wide, check.today)
		}
	}
	// The proportions are the point. Whatever the pot, the thread stays the
	// biggest ambient block and the re-entry brief stays the smallest.
	if roomy.thread <= roomy.deep || roomy.deep <= roomy.board ||
		roomy.board <= roomy.notebook || roomy.notebook <= roomy.reentry {
		t.Errorf("the ambient blocks came out in the wrong order: thread %d, depth %d, board %d, notebook %d, re-entry %d",
			roomy.thread, roomy.deep, roomy.board, roomy.notebook, roomy.reentry)
	}
}

// The whole point of the wave, said at the seam a person would notice it: a head
// speaking through a million-token model carries more of the conversation than
// one speaking through a hundred-and-thirty-thousand-token model. It is asserted
// on the rendered block rather than on the number, because the number is only
// worth having if the renderer spends it.
func TestMillionTokenWindowCarriesMoreThread(t *testing.T) {
	sized := newPromptBudget(1 << 20)
	if sized.thread <= maxThreadContextBytes {
		t.Fatalf("thread budget on a 1M window = %d, want more than the %d fallback",
			sized.thread, maxThreadContextBytes)
	}

	// Enough conversation to overflow the old ceiling several times over, so the
	// difference between the two heads is the budget and nothing else.
	const line = 64
	body := strings.Repeat("m", line-len("user: \n"))
	messages := make([]store.Message, 0, 8*maxThreadContextBytes/line)
	for index := 0; index < cap(messages); index++ {
		messages = append(messages, store.Message{Role: store.RoleUser, Body: body})
	}

	unsized := New(nil, nil).renderThread(messages)
	if len(unsized) > maxThreadContextBytes {
		t.Fatalf("unsized thread = %d bytes, over its own %d fallback", len(unsized), maxThreadContextBytes)
	}
	wide := New(nil, nil).WithContextLength(1 << 20).renderThread(messages)
	if len(wide) <= len(unsized) {
		t.Fatalf("thread on a 1M window = %d bytes, no more than the %d an unsized head rendered",
			len(wide), len(unsized))
	}
	if len(wide) > sized.thread {
		t.Errorf("thread on a 1M window = %d bytes, over its own %d budget", len(wide), sized.thread)
	}
}

// A window small enough to price a block under its literal must leave the head
// exactly where it was. The law raises ceilings; it was never licensed to lower
// one, and a tiny window is precisely where an unguarded share would.
func TestSmallWindowNeverShrinksABlock(t *testing.T) {
	for _, window := range []int{4096, 16384, 32768, 65536} {
		budget := newPromptBudget(window)
		if budget.thread < maxThreadContextBytes || budget.board < maxGraphContextBytes ||
			budget.boardRows < BoardRowCap || budget.bash < bashOutputBytes {
			t.Errorf("a %d-token window shrank the prompt: thread %d, board %d, rows %d, bash %d",
				window, budget.thread, budget.board, budget.boardRows, budget.bash)
		}
	}
}
