package tui

import (
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func settleHistoryCommand(t *testing.T, model *Model, command tea.Cmd) {
	t.Helper()
	if command == nil {
		t.Fatal("history command did not return a read")
	}
	message := command()
	if _, ok := message.(historyResultMsg); !ok {
		t.Fatalf("history command returned %T", message)
	}
	_, _ = model.Update(message)
}

func TestHistoryBareShowsMostRecentSettledJobs(t *testing.T) {
	now := time.Now().UTC()
	backend := &fakeBackend{snapshot: store.Snapshot{Nodes: []store.Node{
		{ID: store.RootID},
		{
			ID: "older", Parent: store.RootID, Status: store.Done, UpdatedSeq: 10,
			Summary: "Older digest", FinishedAt: now.Add(-48 * time.Hour),
			Provenance: store.Provenance{Intent: "Write the earlier release note"},
		},
		{
			ID: "newer", Parent: store.RootID, Status: store.Failed, UpdatedSeq: 20,
			Error: "Provider failed", FinishedAt: now.Add(-2 * time.Hour),
			Provenance: store.Provenance{Intent: "Audit the current provider"},
		},
		{ID: "live", Parent: store.RootID, Status: store.Running, UpdatedSeq: 30, Provenance: store.Provenance{Intent: "Still live"}},
	}}}
	model := New(backend, "history-bare")
	settleHistoryCommand(t, model, model.executeSlash("/history"))
	if len(model.historyEntries) != 2 || model.historyEntries[0].hit.NodeID != "newer" ||
		model.historyEntries[1].hit.NodeID != "older" {
		t.Fatalf("recent history order = %#v", model.historyEntries)
	}
	plain := ansi.Strip(model.renderMessages())
	for _, wanted := range []string{"Audit the current provider", "Write the earlier release note",
		tokens.GlyphFailed, tokens.GlyphSettled} {
		if !strings.Contains(plain, wanted) {
			t.Fatalf("bare history missing %q:\n%s", wanted, plain)
		}
	}
	if strings.Contains(plain, "Still live") {
		t.Fatalf("bare history included unsettled work:\n%s", plain)
	}
}

func TestHistorySearchUsesRecallAndSelectionExpandsDigestWithWorkspace(t *testing.T) {
	commander, _ := workspaceFixture(t)
	backend := &fakeBackend{
		recallHits: []store.RecallHit{
			{NodeID: "alpha", Intent: "Draft the launch campaign", Digest: "Wrote the campaign and verified its links.", Age: "2d ago"},
			{NodeID: "beta", Intent: "Review launch analytics", Digest: "Found the paid channel conversion gap.", Age: "1d ago"},
		},
		snapshot: store.Snapshot{Nodes: []store.Node{
			{ID: "alpha", Status: store.Done}, {ID: "beta", Status: store.Done},
		}},
	}
	model := NewWithCommander(backend, "history-search", commander)
	settleHistoryCommand(t, model, model.executeSlash("/history launch campaign"))
	if backend.recallTerms != "launch campaign" || len(model.historyEntries) != 2 {
		t.Fatalf("recall terms=%q entries=%d", backend.recallTerms, len(model.historyEntries))
	}
	if model.focus != focusChat || model.inputFocused {
		t.Fatalf("history focus=%v inputFocused=%v", model.focus, model.inputFocused)
	}

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	if model.historySelection != 1 {
		t.Fatalf("down selected history row %d, want 1", model.historySelection)
	}
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if model.historyOpen != 1 {
		t.Fatalf("enter opened history row %d, want 1", model.historyOpen)
	}
	view := model.renderMessages()
	wantWorkspaceURL := (&url.URL{Scheme: "file", Path: commander.workspace}).String()
	if !strings.Contains(ansi.Strip(view), "Found the paid channel conversion gap.") ||
		!strings.Contains(ansi.Strip(view), "▸ workspace") || !strings.Contains(view, wantWorkspaceURL) {
		t.Fatalf("expanded history digest/workspace missing:\n%s", view)
	}

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'1'}})
	if model.historyOpen != 0 || model.historySelection != 0 {
		t.Fatalf("number selected/opened (%d,%d), want (0,0)", model.historySelection, model.historyOpen)
	}
	if !strings.Contains(ansi.Strip(model.renderMessages()), "Wrote the campaign and verified its links.") {
		t.Fatal("number-selected digest did not expand")
	}
}

func TestHistoryRowsAreClickable(t *testing.T) {
	backend := &fakeBackend{recallHits: []store.RecallHit{{
		NodeID: "clickable", Intent: "Clickable remembered task", Digest: "Expanded by click.", Age: "3d ago",
	}}}
	model := New(backend, "history-click")
	settleHistoryCommand(t, model, model.executeSlash("/history clickable"))
	_ = model.View()
	if len(model.historyRows) != 1 {
		t.Fatalf("history click rows = %#v", model.historyRows)
	}
	row := model.historyRows[0]
	y := model.chatBounds.y + row.line - model.chat.YOffset
	_, _ = model.Update(tea.MouseMsg{X: model.chatBounds.x + 1, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	if model.historyOpen != 0 || !strings.Contains(ansi.Strip(model.renderMessages()), "Expanded by click.") {
		t.Fatal("click did not expand history digest")
	}
}
