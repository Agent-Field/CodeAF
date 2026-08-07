package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

type standingActionBackend struct {
	*fakeBackend
	requested []store.Command
}

type charterHistoryBackend struct {
	*fakeBackend
	charters []store.Charter
}

func (b *charterHistoryBackend) Charters() ([]store.Charter, error) {
	return append([]store.Charter(nil), b.charters...), nil
}

func (b *standingActionBackend) RequestCommand(command store.Command) (store.Command, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	command.Seq = int64(len(b.requested) + 100)
	command.Time = time.Now()
	command.Status = store.CommandPending
	b.requested = append(b.requested, command)
	return command, nil
}

func seededStandingSnapshot(now time.Time) store.Snapshot {
	charter := store.Provenance{
		Origin: store.OriginUser, Intent: "watch PRs on Agent-Field/aforge — review each new one, post a summary",
	}
	return store.Snapshot{Nodes: []store.Node{
		{ID: store.RootID},
		{
			ID: "pr-watch", Parent: store.RootID, Group: charterGroupMarker,
			Title: "pr-watch", Brief: "on new PR", Status: store.Done, Provenance: charter,
		},
		{ID: "pr-watch-watch", Parent: "pr-watch", Group: "charter:watch", Brief: "on new PR, polled about every 2 minutes"},
		{ID: "pr-watch-quote", Parent: "pr-watch", Group: "charter:quote", Brief: "~$0.15"},
		{ID: "pr-watch-cap", Parent: "pr-watch", Group: "charter:cap", Brief: "≤10/day"},
		{ID: "pr-watch-expiry", Parent: "pr-watch", Group: "charter:expiry", Brief: "never"},
		{
			ID: "proposal", Parent: store.RootID, Group: charterGroupMarker,
			Title: "morning-brief", Brief: "weekday mornings", Status: store.Pending,
			Provenance: store.Provenance{Origin: store.OriginSelf, Intent: "summarize the queue every morning"},
		},
		{ID: "proposal-reason", Parent: "proposal", Group: "charter:proposal", Brief: "noticed you ask this most mornings"},
		standingFiringNode("fire-1", "pr-watch", now.Add(-2*time.Hour), store.Done, "Three review notes delivered."),
		standingFiringNode("fire-2", "pr-watch", now.Add(-3*time.Hour), store.Done, "No blocking issues."),
		standingFiringNode("fire-3", "pr-watch", now.Add(-4*time.Hour), store.Failed, "GitHub was unavailable."),
		{ID: "ordinary", Parent: store.RootID, Title: "ordinary task", Status: store.Pending},
	}}
}

func standingFiringNode(id, charterID string, at time.Time, status store.Status, outcome string) store.Node {
	node := store.Node{
		ID: id, Parent: store.RootID, Group: charterFiringGroupPrefix + charterID,
		Title: id, Status: status, StartedAt: at.Add(-time.Minute), FinishedAt: at,
		Summary:    outcome,
		Provenance: store.Provenance{Origin: store.OriginTrigger, Intent: "fire " + charterID},
	}
	if status == store.Failed {
		node.Summary = ""
		node.Error = outcome
	}
	return node
}

func standingModel(backend Backend, now time.Time, snapshot store.Snapshot) *Model {
	model := New(backend, "standing")
	model.standingNow = func() time.Time { return now }
	model.snapshot = snapshot
	model.cardSnapshot = snapshot
	model.jobUsage = map[string]store.JobUsage{
		"fire-1": {Cost: 0.15}, "fire-2": {Cost: 0.09}, "fire-3": {Cost: 0.04},
	}
	return model
}

func TestStandingSectionPresenceContentAndClick(t *testing.T) {
	now := time.Date(2026, time.August, 6, 14, 0, 0, 0, time.Local)
	snapshot := seededStandingSnapshot(now)
	model := standingModel(&fakeBackend{snapshot: snapshot}, now, snapshot)
	model.setSize(90, 30)
	model.toggleGraph()
	view := model.View()
	for _, want := range []string{
		"standing",
		"⏱ pr-watch · last fired 2h · 3 today",
		"⏱ morning-brief · last fired never · 0 today · proposed",
		"tasks",
		"ordinary task",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("standing rail is missing %q:\n%s", want, view)
		}
	}
	if strings.Index(view, "standing") > strings.Index(view, "tasks") {
		t.Fatalf("standing section did not render above tasks:\n%s", view)
	}
	if strings.Contains(view, "pr-watch-watch") {
		t.Fatalf("charter metadata leaked into the task tree:\n%s", view)
	}

	if len(model.standingRows) == 0 {
		t.Fatal("standing rows were not registered as rail focus targets")
	}
	if model.selectedNodeID != standingGraphRowID("pr-watch") {
		t.Fatalf("rail opened on %q, want first standing line", model.selectedNodeID)
	}
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if model.charterCardID != "proposal" {
		t.Fatalf("standing focus traversal opened charter %q, want proposal", model.charterCardID)
	}
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	_ = model.View()
	row := model.standingRows[0]
	_, _ = model.Update(tea.MouseMsg{
		X:      model.standingRowsBounds.x + 2,
		Y:      model.graphBounds.y + row.line,
		Button: tea.MouseButtonLeft, Action: tea.MouseActionPress,
	})
	if model.charterCardID != "pr-watch" {
		t.Fatalf("clicking a standing line opened charter %q", model.charterCardID)
	}

	empty := standingModel(&fakeBackend{}, now, store.Snapshot{Nodes: []store.Node{{ID: store.RootID}}})
	empty.setSize(90, 30)
	empty.toggleGraph()
	wantTeaching := "standing\n⏱ say \"whenever…\" or \"remind me…\" to stand something up\n"
	if section := ansi.Strip(empty.renderStandingSection(80)); section != wantTeaching {
		t.Fatalf("zero-charter teaching section = %q, want %q", section, wantTeaching)
	}
	if height := empty.standingSectionHeight(); height != 3 {
		t.Fatalf("zero-charter standing height = %d, want 3", height)
	}

	retiredBackend := &charterHistoryBackend{
		fakeBackend: &fakeBackend{},
		charters:    []store.Charter{{ID: "retired", Status: store.CharterRetired}},
	}
	retired := standingModel(retiredBackend, now, store.Snapshot{Nodes: []store.Node{{ID: store.RootID}}})
	retired.setSize(90, 30)
	retired.toggleGraph()
	if section := ansi.Strip(retired.renderStandingSection(80)); section != "" {
		t.Fatalf("learned standing hint returned for retired history: %q", section)
	}
	if height := retired.standingSectionHeight(); height != 0 {
		t.Fatalf("retired charter kept empty-state space: %d", height)
	}
}

func TestStandingBreathesOnlyForSentinelOrFiringActivity(t *testing.T) {
	now := time.Date(2026, time.August, 6, 14, 0, 0, 0, time.Local)
	idleSnapshot := store.Snapshot{Nodes: []store.Node{
		{ID: store.RootID},
		{
			ID: "watch", Parent: store.RootID, Group: charterGroupMarker,
			Title: "watch", Brief: "each minute", Status: store.Done,
			Provenance: store.Provenance{Origin: store.OriginUser, Intent: "keep watch"},
		},
	}}
	model := standingModel(&fakeBackend{snapshot: idleSnapshot}, now, idleSnapshot)
	model.graphOpen = true
	model.focus = focusGraph
	model.setSize(90, 30)
	model.shimmerFrame = 0
	idleFirst := model.renderStandingSection(80)
	model.shimmerFrame = 7
	idleSecond := model.renderStandingSection(80)
	if idleFirst != idleSecond || model.standingBreathing() {
		t.Fatalf("idle charter breathed across frames:\n%s\n%s", idleFirst, idleSecond)
	}
	model.refreshGraph()
	if model.graphAnimating {
		t.Fatal("idle charter scheduled graph animation")
	}

	sentinel := idleSnapshot
	sentinel.Nodes = append([]store.Node(nil), idleSnapshot.Nodes...)
	sentinel.Nodes[1].Status = store.Running
	model.snapshot, model.cardSnapshot = sentinel, sentinel
	if !model.standingBreathing() {
		t.Fatal("evaluating sentinel did not enter breathing state")
	}
	model.refreshGraph()
	if !model.graphAnimating {
		t.Fatal("evaluating sentinel did not schedule the matte-sweep animation")
	}

	firing := idleSnapshot
	firing.Nodes = append([]store.Node(nil), idleSnapshot.Nodes...)
	firing.Nodes = append(firing.Nodes, standingFiringNode("fire", "watch", now, store.Running, "running"))
	firing.Nodes[2].FinishedAt = time.Time{}
	model.snapshot, model.cardSnapshot = firing, firing
	if !model.standingBreathing() {
		t.Fatal("active firing did not make its charter breathe")
	}
	firing.Nodes[2].Status = store.Done
	firing.Nodes[2].FinishedAt = now
	model.snapshot, model.cardSnapshot = firing, firing
	if model.standingBreathing() {
		t.Fatal("settled firing kept its charter breathing")
	}
}

func TestCharterCardAnatomyActionsHistoryAndEsc(t *testing.T) {
	now := time.Date(2026, time.August, 6, 14, 0, 0, 0, time.Local)
	snapshot := seededStandingSnapshot(now)
	backend := &standingActionBackend{fakeBackend: &fakeBackend{snapshot: snapshot}}
	model := standingModel(backend, now, snapshot)
	model.setSize(90, 32)
	model.toggleGraph()
	model.openStandingCharter("pr-watch")

	card := model.renderGraphPane()
	for _, want := range []string{
		"‹ card · pr-watch",
		"invariant",
		"watch PRs on Agent-Field/aforge — review each new one, post a summary",
		"watch",
		"on new PR, polled about every 2 minutes",
		"rails",
		"per firing · ~$0.15",
		"cap · ≤10/day",
		"expiry · never",
		"firing history",
		"15¢ · Three review notes delivered.",
		"▸ pause", "▸ resume", "▸ retire", "▸ edit cadence",
	} {
		if !strings.Contains(card, want) {
			t.Fatalf("charter card is missing %q:\n%s", want, card)
		}
	}

	model.charterFocusIndex = 0
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if model.graphScopeID != "fire-1" || model.charterCardID != "pr-watch" {
		t.Fatalf("history did not open the firing graph: scope=%q charter=%q",
			model.graphScopeID, model.charterCardID)
	}
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if model.graphScopeID != "" || model.charterCardID != "pr-watch" || !model.graphOpen {
		t.Fatalf("esc did not return from firing graph to charter card: scope=%q charter=%q open=%v",
			model.graphScopeID, model.charterCardID, model.graphOpen)
	}

	_ = model.renderGraphPane()
	pauseIndex := -1
	for index, row := range model.charterRows {
		if row.action == "pause" {
			pauseIndex = index
			break
		}
	}
	if pauseIndex < 0 {
		t.Fatal("pause action has no focus row")
	}
	model.charterFocusIndex = pauseIndex
	_, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if command == nil {
		t.Fatal("pause action did not post a command request")
	}
	_, _ = model.Update(command())
	if len(backend.requested) != 1 {
		t.Fatalf("charter action requests = %d, want 1", len(backend.requested))
	}
	request := backend.requested[0]
	if request.Kind != store.CommandAmend || request.Target != "pr-watch" || request.Instruction != "pause" {
		t.Fatalf("unexpected charter command request: %#v", request)
	}

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if model.charterCardID != "" || !model.graphOpen || model.focus != focusGraph {
		t.Fatalf("esc did not return from charter card to rail: charter=%q open=%v focus=%v",
			model.charterCardID, model.graphOpen, model.focus)
	}
}

func TestQuestionOptionsRenderSelectAndKeepFreeTextLive(t *testing.T) {
	backend := &fakeBackend{}
	command := store.Command{
		Seq: 7, SessionID: "questions", Kind: store.CommandSplice,
		Instruction: "watch pull requests", Status: store.CommandRejected,
	}
	message := store.Message{
		Seq: 8, SessionID: "questions", Role: store.RoleAgent, CommandSeq: command.Seq,
		Body: "watch PRs on Agent-Field/aforge — review each new one, post a summary\n" +
			"fires: on new PR · costs: ~$0.15/firing, ≤10/day · expires: never\n" +
			"▸ 1 yes, stand this up  ▸ 2 change the cadence  ▸ 3 once, not standing",
	}
	cards := deriveJobCards("questions", store.Snapshot{}, []store.Message{message}, nil, nil,
		map[int64]store.Command{command.Seq: command})
	question := requireCard(t, cards, fmt.Sprintf("command:%d", command.Seq))
	if question.State != cardQuestion || len(question.Options) != 3 {
		t.Fatalf("question payload derived %#v", question)
	}

	model := New(backend, "questions")
	model.cards = cards
	model.setSize(90, 30)
	dock := model.renderCardDock(true)
	for _, want := range []string{"▸ 1 yes, stand this up", "▸ 2 change the cadence", "▸ 3 once, not standing"} {
		if !strings.Contains(dock, want) {
			t.Fatalf("option question is missing %q:\n%s", want, dock)
		}
	}
	if !model.inputFocused {
		t.Fatal("question options stole focus from the free-text input")
	}

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	if got := model.questionSelection[question.ID]; got != 1 {
		t.Fatalf("down selected option index %d, want 1", got)
	}
	_, post := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if post == nil {
		t.Fatal("enter on selected option returned no post command")
	}
	_ = post()

	_, numberPost := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'3'}})
	if numberPost == nil {
		t.Fatal("number shortcut returned no post command")
	}
	_ = numberPost()

	model.input.SetValue("use the team default instead")
	_, freeTextPost := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if freeTextPost == nil {
		t.Fatal("free-text answer returned no post command")
	}
	_ = freeTextPost()

	backend.mu.Lock()
	defer backend.mu.Unlock()
	want := []string{"change the cadence", "once, not standing", "use the team default instead"}
	if len(backend.posted) != len(want) {
		t.Fatalf("posted %d replies, want %d: %#v", len(backend.posted), len(want), backend.posted)
	}
	for index, body := range want {
		if posted := backend.posted[index]; posted.Role != store.RoleUser || posted.Body != body {
			t.Fatalf("reply %d = %#v, want user message %q", index, posted, body)
		}
	}
}

func TestFrameHeightExactAcrossStandingRailStates(t *testing.T) {
	now := time.Date(2026, time.August, 6, 14, 0, 0, 0, time.Local)
	snapshot := seededStandingSnapshot(now)
	for _, width := range []int{80, 120} {
		t.Run(fmt.Sprintf("width-%d", width), func(t *testing.T) {
			model := standingModel(&fakeBackend{snapshot: snapshot}, now, snapshot)
			model.setSize(width, 30)
			if got := lipgloss.Height(model.View()); got != 30 {
				t.Fatalf("closed rail frame height = %d, want 30", got)
			}

			model.toggleGraph()
			standingHeight := model.standingSectionHeight()
			if standingHeight != 4 {
				t.Fatalf("standing section height = %d, want header + 2 rows + blank (4)", standingHeight)
			}
			if want := max(1, model.graphHeight-2-standingHeight); model.graph.Height != want {
				t.Fatalf("task viewport height = %d, want %d", model.graph.Height, want)
			}
			if got := lipgloss.Height(model.View()); got != 30 {
				t.Fatalf("standing rail frame height = %d, want 30", got)
			}

			model.openStandingCharter("pr-watch")
			if model.standingSectionHeight() != 0 || model.graph.Height != max(1, model.graphHeight-2) {
				t.Fatalf("charter card retained standing height: section=%d graph=%d",
					model.standingSectionHeight(), model.graph.Height)
			}
			if got := lipgloss.Height(model.View()); got != 30 {
				t.Fatalf("charter-card frame height = %d, want 30", got)
			}

			model.charterFocusIndex = 0
			_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
			if model.graphScopeID != "fire-1" {
				t.Fatalf("history opened scope %q, want fire-1", model.graphScopeID)
			}
			if got := lipgloss.Height(model.View()); got != 30 {
				t.Fatalf("firing-graph frame height = %d, want 30", got)
			}
		})
	}
}

type charterListingBackend struct {
	*fakeBackend
	charters []store.Charter
}

func (b *charterListingBackend) Charters() ([]store.Charter, error) { return b.charters, nil }

// The store-native path: first-class charters render without any Group:
// "charter" compatibility nodes, and firings attach through
// Provenance.CharterID alone.
func TestStoreNativeChartersRenderWithoutCompatibilityNodes(t *testing.T) {
	now := time.Date(2026, time.August, 6, 14, 0, 0, 0, time.Local)
	expires := time.Date(2026, time.September, 1, 9, 0, 0, 0, time.Local)
	charter, err := store.NewCharter(
		"pr-watch",
		"watch PRs on Agent-Field/aforge — review each new one",
		store.WatchSpec{Kind: store.WatchPoll, Poll: &store.PollWatch{
			Condition: "new PRs", Cadence: 2 * time.Minute,
		}},
		"has a new PR opened?",
		store.CharterAction{Template: "review each new PR and post a summary"},
		store.CharterRails{PerFiringBudgetUSD: 0.15, MaxFiringsPerDay: 10, ExpiresAt: &expires},
		store.CharterActive,
		store.Ratification{Origin: store.OriginUser, Evidence: "yes, stand this up"},
	)
	if err != nil {
		t.Fatalf("NewCharter: %v", err)
	}
	firing := store.Node{
		ID: "fire-native", Parent: store.RootID, Title: "review PR 41",
		Status: store.Done, StartedAt: now.Add(-time.Hour), FinishedAt: now.Add(-time.Hour),
		Summary:    "One review note delivered.",
		Provenance: store.Provenance{Origin: store.OriginTrigger, CharterID: "pr-watch"},
	}
	snapshot := store.Snapshot{Nodes: []store.Node{{ID: store.RootID}, firing}}
	backend := &charterListingBackend{fakeBackend: &fakeBackend{snapshot: snapshot}, charters: []store.Charter{charter}}
	model := standingModel(backend, now, snapshot)
	model.jobUsage = map[string]store.JobUsage{"fire-native": {Cost: 0.11}}

	charters := model.standingCharters()
	if len(charters) != 1 {
		t.Fatalf("expected one store-native charter, got %#v", charters)
	}
	got := charters[0]
	if got.ID != "pr-watch" || got.Proposed || got.State != "active" {
		t.Fatalf("unexpected projection: %#v", got)
	}
	if got.Quote != "~$0.15" || got.Cap != "≤10/day" || !strings.Contains(got.Expiry, "2026-09-01") {
		t.Fatalf("rails not projected: %#v", got)
	}
	if len(got.Firings) != 1 || got.Firings[0].Cost != 0.11 || got.Today != 1 || got.LastFired.IsZero() {
		t.Fatalf("firing not attached through CharterID: %#v", got)
	}
}
