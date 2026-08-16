package tui

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// settledWorkerModel is a task page open on work that has stopped for good.
func settledWorkerModel(t *testing.T, status store.Status) (*Model, *fakeCommander) {
	t.Helper()
	model, commander := inspectedWorkerModel(t)
	model.inspectedNode.Status = status
	model.snapshot.Nodes[1].Status = status
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyTab}) // out of the steer line
	return model, commander
}

// The footer names the verb that is true of the task being read, and no other.
// "c cancel" over a job that finished an hour ago is a promise; over one the
// user stopped themselves it is the wrong word entirely.
func TestTheTaskFooterOffersTheVerbTheStateActuallyHas(t *testing.T) {
	tests := []struct {
		status store.Status
		want   string
		absent string
	}{
		{store.Running, "c stop", "r restart"},
		{store.Claimed, "c stop", "r restart"},
		{store.Pending, "c stop", "r restart"},
		{store.Cancelled, "r restart", "c stop"},
		{store.Failed, "r restart", "c stop"},
	}
	for _, test := range tests {
		t.Run(string(test.status), func(t *testing.T) {
			model, _ := settledWorkerModel(t, test.status)
			hint := model.contextHelpLine()
			if !strings.Contains(hint, test.want) || strings.Contains(hint, test.absent) {
				t.Fatalf("%s footer = %q", test.status, hint)
			}
			if items := strings.Count(hint, " · ") + 1; items > 4 {
				t.Fatalf("%s footer has %d items: %q", test.status, items, hint)
			}
		})
	}
	// Done work is read-only: there is no verb, and offering one would be
	// offering a door the journal refuses.
	model, commander := settledWorkerModel(t, store.Done)
	hint := model.contextHelpLine()
	if strings.Contains(hint, "c stop") || strings.Contains(hint, "r restart") {
		t.Fatalf("a finished task advertises a verb: %q", hint)
	}
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	if len(commander.restarted) != 0 || len(commander.cancelled) != 0 {
		t.Fatalf("a finished task acted on r/c: restarted=%v cancelled=%v",
			commander.restarted, commander.cancelled)
	}
}

// One mouth: the key journals the identical typed command a sentence would,
// through the commander, and nothing else.
func TestRestartFromTheTaskPageJournalsTheSameCommandASentenceWould(t *testing.T) {
	model, commander := settledWorkerModel(t, store.Cancelled)
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	if len(commander.restarted) != 1 || commander.restarted[0] != "worker" {
		t.Fatalf("r requested %v", commander.restarted)
	}
	if len(commander.cancelled) != 0 {
		t.Fatalf("r also cancelled %v", commander.cancelled)
	}
}

// The gates are absolute, and a keypress is not a smaller intention than a
// sentence. When the loss is large the key asks the durable question the head
// would ask and journals nothing.
func TestAGatedRestartAsksInsteadOfJournalling(t *testing.T) {
	model, commander := settledWorkerModel(t, store.Cancelled)
	commander.gated = true
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	if len(commander.restarted) != 0 {
		t.Fatalf("a gated restart journalled anyway: %v", commander.restarted)
	}
	if len(commander.confirmed) != 1 || commander.confirmed[0] != store.CommandRestart {
		t.Fatalf("the gate was consulted with %v", commander.confirmed)
	}

	stop, stopCommander := inspectedWorkerModel(t)
	stopCommander.gated = true
	_, _ = stop.Update(tea.KeyMsg{Type: tea.KeyTab})
	_, _ = stop.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	if len(stopCommander.cancelled) != 0 {
		t.Fatalf("a gated stop journalled anyway: %v", stopCommander.cancelled)
	}
	if len(stopCommander.confirmed) != 1 || stopCommander.confirmed[0] != store.CommandCancel {
		t.Fatalf("the stop gate was consulted with %v", stopCommander.confirmed)
	}
}

// A cancellation is not a failure and must not read as one. The reason is quiet
// ink rather than failure's rose, the partial the worker left is on the page
// under a label that admits what it is, and the receipt the command journalled
// is visible where the reader is actually looking.
func TestACancelledTaskReadsAsStoppedRatherThanBroken(t *testing.T) {
	model, _ := settledWorkerModel(t, store.Cancelled)
	model.inspectedNode.Error = store.UserCancelReason
	model.inspectedNode.Summary = "sections one and two are written to report.md"
	model.nodeMessages = []store.Message{{
		Seq: 1, Role: store.RoleSystem, NodeID: "worker",
		Body: "cancellation requested — 1 running step will release at the next boundary",
	}}
	model.refreshNodeView(true)

	document := ansi.Strip(model.renderActivityFeed(88))
	for _, wanted := range []string{
		"LEFT OFF",
		"sections one and two are written",
		store.UserCancelReason,
		"will release at the next boundary",
	} {
		if !strings.Contains(document, wanted) {
			t.Fatalf("the cancelled task's page is missing %q:\n%s", wanted, document)
		}
	}

	// The header wears the muted ink and says the word, not failure's rose.
	if cancelledGlyphStyle.GetForeground() == failedGlyphStyle.GetForeground() {
		t.Fatal("cancelled work is painted in failure's ink")
	}
	if cancelledGlyphStyle.GetForeground() != mutedStyle.GetForeground() {
		t.Fatal("cancelled work left the muted ink family")
	}
	if nodeEndingWord(store.Cancelled) != "cancelled" || nodeEndingWord(store.Failed) != "failed" {
		t.Fatal("the header's ending word does not follow the state")
	}
}
